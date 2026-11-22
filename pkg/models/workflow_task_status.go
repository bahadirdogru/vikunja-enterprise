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

package models

import (
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

// AvailableTransition is a status change the requesting user may perform on a task right now.
type AvailableTransition struct {
	ToStatus       *WorkflowStatus `json:"to_status" readOnly:"true" doc:"The status the task would enter."`
	Title          string          `json:"title" readOnly:"true" doc:"The button label of the transition, empty for plain changes."`
	RequireComment bool            `json:"require_comment" readOnly:"true" doc:"Whether a comment has to be sent with the change."`
	MissingFields  []string        `json:"missing_fields" readOnly:"true" doc:"The required fields the task does not have yet. The change is rejected until they are set."`
}

// TaskWorkflowStatus is the current status of a task with the changes available to the requesting user.
type TaskWorkflowStatus struct {
	TaskID      int64                  `json:"task_id" readOnly:"true" doc:"The task. Taken from the URL."`
	WorkflowID  int64                  `json:"workflow_id" readOnly:"true" doc:"The workflow of the project of the task."`
	Status      *WorkflowStatus        `json:"status" readOnly:"true" doc:"The current status, null when the task has none."`
	Entered     *time.Time             `json:"entered" readOnly:"true" doc:"When the task entered its current status."`
	Transitions []*AvailableTransition `json:"transitions" readOnly:"true" doc:"The changes the requesting user may perform now: the workflow allows them and the user has the permission and team they need. Each lists the requirements the task does not meet yet."`

	task *Task

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

// ReadOne loads the status and the changes available to the user; CanRead loaded the task.
// @Summary Get the status of a task
func (ts *TaskWorkflowStatus) ReadOne(s *xorm.Session, a web.Auth) error {
	task := ts.task
	d, err := loadWorkflowDef(s, task.ProjectID)
	if err != nil {
		return err
	}
	if d == nil {
		return ErrWorkflowDoesNotExist{ProjectID: task.ProjectID}
	}

	ts.WorkflowID = d.workflow.ID
	ts.Transitions = []*AvailableTransition{}

	row, err := getTaskStatusRow(s, task.ID)
	if err != nil {
		return err
	}
	var from *WorkflowStatus
	if row != nil {
		from = d.statuses[row.StatusID]
		ts.Status = from
		entered := row.Entered
		ts.Entered = &entered
	}

	for _, to := range d.workflow.Statuses {
		if to.Archived || (from != nil && from.ID == to.ID) {
			continue
		}
		var rule *WorkflowTransition
		if from != nil {
			rule = d.ruleFor(from.ID, to.ID)
			if rule == nil && d.workflow.RestrictTransitions {
				continue
			}
		}
		reason, err := checkTransitionActor(s, a, task.ProjectID, rule)
		if err != nil {
			return err
		}
		if reason != "" {
			continue
		}
		missing, _, err := missingRequirements(s, task, rule, "")
		if err != nil {
			return err
		}
		available := &AvailableTransition{
			ToStatus:      to,
			MissingFields: missing,
		}
		if rule != nil {
			available.Title = rule.Title
			available.RequireComment = rule.RequireComment
		}
		ts.Transitions = append(ts.Transitions, available)
	}
	return nil
}

// TaskStatusChange asks to move a task to another status.
type TaskStatusChange struct {
	TaskID       int64           `json:"task_id" readOnly:"true" doc:"The task. Taken from the URL."`
	ToStatusID   int64           `json:"to_status_id" doc:"The status the task should enter."`
	Comment      string          `json:"comment" doc:"A comment that is stored in the history of the task. Required when the transition says so."`
	FromStatusID *int64          `json:"from_status_id" readOnly:"true" doc:"The status the task left, null when it had none."`
	Status       *WorkflowStatus `json:"status" readOnly:"true" doc:"The status the task is in after the change."`
	Done         bool            `json:"done" readOnly:"true" doc:"Whether the task is done after the change."`

	task *Task

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

// Create performs the status change with all checks of the workflow.
// @Summary Change the status of a task
func (c *TaskStatusChange) Create(s *xorm.Session, a web.Auth) error {
	t := c.task
	d, err := loadWorkflowDef(s, t.ProjectID)
	if err != nil {
		return err
	}
	if d == nil {
		return ErrWorkflowDoesNotExist{ProjectID: t.ProjectID}
	}

	to := d.statuses[c.ToStatusID]
	reopens := to != nil && to.Category == WorkflowCategoryDone && t.isRepeating() && !t.Done

	result, err := d.changeTaskStatus(s, a, &statusChange{
		Task:       t,
		ToStatusID: c.ToStatusID,
		Comment:    c.Comment,
		SyncDone:   !reopens,
	})
	if err != nil {
		return err
	}
	c.FromStatusID = nil
	if result.From != nil {
		id := result.From.ID
		c.FromStatusID = &id
	}
	c.Status = result.To

	if reopens && result.Changed {
		if err = reopenRepeatingTask(s, t); err != nil {
			return err
		}
		initial := d.initialStatus()
		if _, err = d.changeTaskStatus(s, a, &statusChange{Task: t, ToStatusID: initial.ID, SkipChecks: true}); err != nil {
			return err
		}
		c.Status = initial
	}

	c.Done = t.Done
	if result.Changed {
		if err = updateTaskLastUpdated(s, &Task{ID: t.ID}); err != nil {
			return err
		}
		if result.DoneChanged || reopens {
			// The done flag is what changed, so consumers can tell completing from reopening.
			oldTask := *t
			if result.DoneChanged {
				oldTask.Done = !t.Done
			}
			events.DispatchOnCommit(s, &TaskUpdatedEvent{Task: t, Doer: doerFromAuth(s, a), OldTask: &oldTask})
		}
	}
	return nil
}

// reopenRepeatingTask does what marking a repeating task done does: shift its dates and reminders and keep it open.
func reopenRepeatingTask(s *xorm.Session, t *Task) error {
	reminders, err := getRemindersForTasks(s, []int64{t.ID})
	if err != nil {
		return err
	}
	old := *t
	old.Reminders = reminders

	t.Done = true
	updateDone(&old, t)
	if err = old.updateReminders(s, t); err != nil {
		return err
	}

	_, err = s.ID(t.ID).
		Cols("done", "done_at", "due_date", "start_date", "end_date", "description").
		Update(t)
	return err
}

// ReadAll returns the status history of a task, newest first.
// @Summary List the status history of a task
func (h *TaskStatusHistory) ReadAll(s *xorm.Session, a web.Auth, _ string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	// DoReadAll skips the Can* checks.
	can, _, err := (&Task{ID: h.TaskID}).CanRead(s, a)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	total, err := s.Where("task_id = ?", h.TaskID).Count(&TaskStatusHistory{})
	if err != nil {
		return nil, 0, 0, err
	}

	entries := []*TaskStatusHistory{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where("task_id = ?", h.TaskID).OrderBy("id DESC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&entries); err != nil {
		return nil, 0, 0, err
	}

	doerIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		if e.DoerID > 0 {
			doerIDs = append(doerIDs, e.DoerID)
		}
	}
	doers, err := user.GetUsersByIDs(s, doerIDs)
	if err != nil {
		return nil, 0, 0, err
	}
	for _, e := range entries {
		e.Doer = doers[e.DoerID]
	}
	return entries, len(entries), total, nil
}
