// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package automation is the engine behind automation rules: the transactional outbox hook, the workers that claim
// events, the condition evaluator, the actions and the limits. Rules, runs and their API live in the models
// package; the catalog and the validation of the rule document live in the spec subpackage.
package automation

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"xorm.io/xorm"
)

// Person is the doer of an event as recorded in the payload.
type Person struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// Payload is what an outbox row stores about an event. Old and New hold the values the event knows before and
// after the change; their keys are the ones of the event.old.* / event.new.* condition fields.
type Payload struct {
	Name      string         `json:"name"`
	TaskID    int64          `json:"task_id"`
	ProjectID int64          `json:"project_id"`
	Doer      *Person        `json:"doer,omitempty"`
	Old       map[string]any `json:"old,omitempty"`
	New       map[string]any `json:"new,omitempty"`
	// Changed lists the task fields that differ between Old and New (task.updated).
	Changed []string `json:"changed,omitempty"`
	// FieldID is the custom field of a task.custom-field-value.updated event.
	FieldID int64 `json:"field_id,omitempty"`
	// Task is the last known state of the task, for events whose task is gone when the rule runs.
	Task map[string]any `json:"task,omitempty"`
}

// origin says which run caused the events written in a session.
type origin struct {
	runID int64
	depth int64
}

// sessionOrigins maps the sessions a run executes its actions in to the run. The outbox hook reads it to mark the
// events an action causes with origin_run_id and depth+1. Keyed by session instead of passed through a context
// because the hook only receives the session of the transaction.
var sessionOrigins sync.Map // *xorm.Session -> origin

func markSession(s *xorm.Session, o origin) {
	sessionOrigins.Store(s, o)
}

func unmarkSession(s *xorm.Session) {
	sessionOrigins.Delete(s)
}

func init() {
	events.RegisterTransactionalHook(outboxHook)
}

func personOf(u *user.User) *Person {
	if u == nil {
		return nil
	}
	return &Person{ID: u.ID, Username: u.Username, Name: u.GetName()}
}

func dateValue(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// taskFields is the snapshot of the built-in fields stored in event payloads.
func taskFields(t *models.Task) map[string]any {
	if t == nil {
		return nil
	}
	return map[string]any{
		"title":        t.Title,
		"description":  t.Description,
		"done":         t.Done,
		"priority":     t.Priority,
		"percent_done": t.PercentDone,
		"due_date":     dateValue(t.DueDate),
		"start_date":   dateValue(t.StartDate),
		"end_date":     dateValue(t.EndDate),
		"hex_color":    t.HexColor,
		"project_id":   t.ProjectID,
		"repeat_after": t.RepeatAfter,
	}
}

func changedFields(old, cur map[string]any) []string {
	changed := []string{}
	for _, name := range spec.UpdatedFields {
		if fmt.Sprint(old[name]) != fmt.Sprint(cur[name]) {
			changed = append(changed, name)
		}
	}
	return changed
}

// describe extracts what the outbox needs from an event. ok is false for events no trigger listens to.
func describe(event events.Event) (p *Payload, ok bool) {
	switch e := event.(type) {
	case *models.TaskCreatedEvent:
		return taskPayload(e.Name(), e.Task, e.Doer, nil, taskFields(e.Task)), e.Task != nil
	case *models.TaskUpdatedEvent:
		if e.Task == nil {
			return nil, false
		}
		var old map[string]any
		if e.OldTask != nil {
			old = taskFields(e.OldTask)
		}
		p = taskPayload(e.Name(), e.Task, e.Doer, old, taskFields(e.Task))
		if old != nil {
			p.Changed = changedFields(old, p.New)
		}
		return p, true
	case *models.TaskDeletedEvent:
		return taskPayload(e.Name(), e.Task, e.Doer, nil, taskFields(e.Task)), e.Task != nil
	case *models.TaskStatusChangedEvent:
		if e.Task == nil {
			return nil, false
		}
		p = taskPayload(e.Name(), e.Task, e.Doer, statusValues(e.FromStatus), statusValues(e.ToStatus))
		return p, true
	case *models.TaskCustomFieldValueUpdatedEvent:
		if e.Task == nil || e.CustomField == nil {
			return nil, false
		}
		p = taskPayload(e.Name(), e.Task, e.Doer, map[string]any{"value": e.OldValue}, map[string]any{"value": e.NewValue})
		p.FieldID = e.CustomField.ID
		return p, true
	case *models.TaskAssigneeCreatedEvent:
		if e.Task == nil || e.Assignee == nil {
			return nil, false
		}
		return taskPayload(e.Name(), e.Task, e.Doer, nil, map[string]any{"assignee": e.Assignee.ID}), true
	case *models.TaskAssigneeDeletedEvent:
		if e.Task == nil || e.Assignee == nil {
			return nil, false
		}
		return taskPayload(e.Name(), e.Task, e.Doer, map[string]any{"assignee": e.Assignee.ID}, nil), true
	case *models.TaskCommentCreatedEvent:
		if e.Task == nil || e.Comment == nil {
			return nil, false
		}
		return taskPayload(e.Name(), e.Task, e.Doer, nil, map[string]any{"comment": e.Comment.Comment}), true
	case *models.ApprovalRequestedEvent:
		if e.Task == nil || e.Approval == nil {
			return nil, false
		}
		return taskPayload(e.Name(), e.Task, e.Doer, approvalOld(e.Approval), approvalNew(e.Approval, "")), true
	case *models.ApprovalResolvedEvent:
		if e.Task == nil || e.Approval == nil {
			return nil, false
		}
		return taskPayload(e.Name(), e.Task, e.Doer, approvalOld(e.Approval), approvalNew(e.Approval, string(e.Outcome))), true
	case *models.FormSubmittedEvent:
		if e.Task == nil {
			return nil, false
		}
		return taskPayload(e.Name(), e.Task, e.Doer, nil, map[string]any{
			"form_id":         e.FormID,
			"form_title":      e.FormTitle,
			"submitter_name":  e.SubmitterName,
			"submitter_email": e.SubmitterEmail,
		}), true
	case *models.TaskAttachmentCreatedEvent:
		if e.Task == nil || e.Attachment == nil {
			return nil, false
		}
		newValues := map[string]any{}
		if e.Attachment.File != nil {
			newValues["attachment_name"] = e.Attachment.File.Name
		}
		return taskPayload(e.Name(), e.Task, e.Doer, nil, newValues), true
	}
	return nil, false
}

// approvalOld holds the status the task has to be in for a transition request.
func approvalOld(a *models.ApprovalRequest) map[string]any {
	if a.FromStatusID == nil {
		return nil
	}
	return map[string]any{"status": *a.FromStatusID}
}

// approvalNew holds the fields of event.new.* of the approval events. outcome is empty for a new request.
func approvalNew(a *models.ApprovalRequest, outcome string) map[string]any {
	approvers := make([]int64, 0, len(a.Approvers))
	for _, ap := range a.Approvers {
		approvers = append(approvers, ap.UserID)
	}
	values := map[string]any{
		"approval_id":    a.ID,
		"approval_title": a.Title,
		"approval_mode":  string(a.Mode),
		"ad_hoc":         a.TransitionID == nil,
		"requested_by":   a.RequestedByID,
		"approvers":      approvers,
	}
	if a.ToStatusID != nil {
		values["status"] = *a.ToStatusID
	}
	if outcome != "" {
		values["outcome"] = outcome
	}
	if a.CancelReason != "" {
		values["cancel_reason"] = a.CancelReason
	}
	return values
}

func statusValues(st *models.WorkflowStatus) map[string]any {
	if st == nil {
		return nil
	}
	return map[string]any{"status": st.ID, "status_category": string(st.Category)}
}

func taskPayload(name string, t *models.Task, doer *user.User, old, cur map[string]any) *Payload {
	return &Payload{
		Name:      name,
		TaskID:    t.ID,
		ProjectID: t.ProjectID,
		Doer:      personOf(doer),
		Old:       old,
		New:       cur,
		Task:      taskFields(t),
	}
}

// outboxHook writes an automation_events row in the transaction of the change when a rule of the project listens
// to the event. A returned error means the row could not be written; the surrounding CRUD handler then rolls the
// transaction back instead of silently losing the event.
func outboxHook(s *xorm.Session, event events.Event) error {
	if !config.AutomationEnabled.GetBool() {
		return nil
	}
	payload, ok := describe(event)
	if !ok || payload.ProjectID == 0 {
		return nil
	}

	subscribed, err := models.AutomationSubscribedEvents(s, payload.ProjectID)
	if err != nil {
		return err
	}
	if !subscribed[payload.Name] {
		return nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		// Our own payload cannot be encoded: a bug, not a database error. Do not fail the change for it.
		log.Errorf("[automation] could not encode the payload of %s: %v", payload.Name, err)
		return nil
	}

	row := &models.AutomationEvent{
		ProjectID: payload.ProjectID,
		TaskID:    payload.TaskID,
		Name:      payload.Name,
		Payload:   string(raw),
		Status:    models.AutomationEventPending,
		RunAfter:  now(),
	}
	if o, has := sessionOrigins.Load(s); has {
		row.OriginRunID = o.(origin).runID
		row.Depth = o.(origin).depth + 1
	}
	if _, err = s.Insert(row); err != nil {
		return err
	}
	log.Debugf("[automation] queued event %d (%s, task %d, depth %d)", row.ID, row.Name, row.TaskID, row.Depth)

	// The row only becomes visible when the transaction commits; wake the workers then.
	events.OnCommit(s, func() error {
		Wake()
		return nil
	})
	return nil
}
