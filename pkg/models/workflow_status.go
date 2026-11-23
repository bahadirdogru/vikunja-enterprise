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
	"sort"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// TaskStatus is the current status of a task. Tasks of projects without a workflow have no row.
type TaskStatus struct {
	ID         int64     `xorm:"bigint autoincr not null unique pk"`
	TaskID     int64     `xorm:"bigint not null unique"`
	WorkflowID int64     `xorm:"bigint not null INDEX"`
	StatusID   int64     `xorm:"bigint not null INDEX"`
	Entered    time.Time `xorm:"not null"`
}

// TableName returns the table name of task statuses.
func (*TaskStatus) TableName() string {
	return "task_statuses"
}

// TaskStatusHistory is one status change of a task. The titles are copied so the entry stays readable
// after the status or the whole workflow is gone.
type TaskStatusHistory struct {
	ID           int64      `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this history entry."`
	TaskID       int64      `xorm:"bigint not null INDEX" json:"task_id" readOnly:"true" doc:"The task the entry belongs to."`
	FromStatusID *int64     `xorm:"bigint null" json:"from_status_id" readOnly:"true" doc:"The status the task left, null for the first status of a task."`
	ToStatusID   int64      `xorm:"bigint not null" json:"to_status_id" readOnly:"true" doc:"The status the task entered."`
	FromTitle    string     `xorm:"varchar(250) null" json:"from_title" readOnly:"true" doc:"The title of the status the task left when the change happened."`
	ToTitle      string     `xorm:"varchar(250) not null" json:"to_title" readOnly:"true" doc:"The title of the status the task entered when the change happened."`
	DoerID       int64      `xorm:"bigint not null default 0" json:"-"`
	Doer         *user.User `xorm:"-" json:"doer" readOnly:"true" doc:"Who made the change. Null for changes made through a link share."`
	Comment      string     `xorm:"text null" json:"comment" readOnly:"true" doc:"The comment left with the change."`
	Created      time.Time  `xorm:"created not null" json:"created" readOnly:"true" doc:"When the change happened."`
	// ApprovalRequestID is set for changes an approval request made or caused: an approved transition (the doer is
	// the requester) or the move after a rejection (the doer is the rejecting approver).
	ApprovalRequestID *int64 `xorm:"bigint null" json:"approval_request_id" readOnly:"true" doc:"The approval request behind the change, null for changes without one."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name of the status history.
func (*TaskStatusHistory) TableName() string {
	return "task_status_history"
}

// workflowDef is a loaded workflow with lookups, used while changing statuses.
type workflowDef struct {
	workflow    *Workflow
	statuses    map[int64]*WorkflowStatus
	transitions map[[2]int64]*WorkflowTransition
}

// loadWorkflowDef returns nil without error when the project has no workflow.
func loadWorkflowDef(s *xorm.Session, projectID int64) (*workflowDef, error) {
	wf, err := getWorkflowByProject(s, projectID)
	if err != nil {
		if IsErrWorkflowDoesNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if err = loadWorkflowRelations(s, wf); err != nil {
		return nil, err
	}

	d := &workflowDef{
		workflow:    wf,
		statuses:    make(map[int64]*WorkflowStatus, len(wf.Statuses)),
		transitions: make(map[[2]int64]*WorkflowTransition, len(wf.Transitions)),
	}
	for _, st := range wf.Statuses {
		d.statuses[st.ID] = st
	}
	for _, tr := range wf.Transitions {
		d.transitions[[2]int64{tr.FromStatusID, tr.ToStatusID}] = tr
	}
	return d, nil
}

func (d *workflowDef) initialStatus() *WorkflowStatus {
	for _, st := range d.workflow.Statuses {
		if st.IsInitial && !st.Archived {
			return st
		}
	}
	return nil
}

// firstDoneStatus is the done status with the lowest position.
func (d *workflowDef) firstDoneStatus() *WorkflowStatus {
	for _, st := range d.workflow.Statuses {
		if st.Category == WorkflowCategoryDone && !st.Archived {
			return st
		}
	}
	return nil
}

// statusForNewTask picks the status a task starts in depending on its done flag.
func (d *workflowDef) statusForTask(done bool) *WorkflowStatus {
	if done {
		if st := d.firstDoneStatus(); st != nil {
			return st
		}
	}
	return d.initialStatus()
}

func (d *workflowDef) ruleFor(from, to int64) *WorkflowTransition {
	return d.transitions[[2]int64{from, to}]
}

func getTaskStatusRow(s *xorm.Session, taskID int64) (*TaskStatus, error) {
	row := &TaskStatus{}
	exists, err := s.Where("task_id = ?", taskID).Get(row)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return row, nil
}

// historyDoerID is 0 for link shares, whose id is not a user id.
func historyDoerID(a web.Auth) int64 {
	if a == nil {
		return 0
	}
	if _, isShare := a.(*LinkSharing); isShare {
		return 0
	}
	return a.GetID()
}

func insertInChunks[T any](s *xorm.Session, rows []*T) error {
	for start := 0; start < len(rows); start += workflowBulkChunkSize {
		end := min(start+workflowBulkChunkSize, len(rows))
		if _, err := s.Insert(rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func newHistoryEntry(taskID int64, from *WorkflowStatus, to *WorkflowStatus, a web.Auth, comment string) *TaskStatusHistory {
	h := &TaskStatusHistory{
		TaskID:     taskID,
		ToStatusID: to.ID,
		ToTitle:    to.Title,
		DoerID:     historyDoerID(a),
		Comment:    comment,
	}
	if from != nil {
		id := from.ID
		h.FromStatusID = &id
		h.FromTitle = from.Title
	}
	return h
}

// assignStatusesForNewWorkflow gives every task of the project a status: open tasks the initial one,
// done tasks the first done one. One insert statement per chunk of tasks, no per-task queries.
func assignStatusesForNewWorkflow(s *xorm.Session, workflowID, projectID int64) error {
	d, err := loadWorkflowDef(s, projectID)
	if err != nil || d == nil {
		return err
	}
	initial := d.statusForTask(false)
	done := d.statusForTask(true)

	// Trashed tasks too: they come back with a restore and must have a status then.
	tasks := []*Task{}
	if err = s.Unscoped().Where("project_id = ?", projectID).Cols("id", "done").Find(&tasks); err != nil {
		return err
	}

	now := time.Now()
	rows := make([]*TaskStatus, 0, len(tasks))
	for _, t := range tasks {
		st := initial
		if t.Done {
			st = done
		}
		rows = append(rows, &TaskStatus{
			TaskID:     t.ID,
			WorkflowID: workflowID,
			StatusID:   st.ID,
			Entered:    now,
		})
	}
	return insertInChunks(s, rows)
}

// assignStatusesToNewTasks gives freshly created tasks their start status and writes the first history entry.
func assignStatusesToNewTasks(s *xorm.Session, projectID int64, tasks []*Task, a web.Auth) error {
	if len(tasks) == 0 {
		return nil
	}
	d, err := loadWorkflowDef(s, projectID)
	if err != nil || d == nil {
		return err
	}
	return d.assignStartStatuses(s, tasks, a)
}

func (d *workflowDef) assignStartStatuses(s *xorm.Session, tasks []*Task, a web.Auth) error {
	now := time.Now()
	rows := make([]*TaskStatus, 0, len(tasks))
	history := make([]*TaskStatusHistory, 0, len(tasks))
	for _, t := range tasks {
		st := d.statusForTask(t.Done)
		rows = append(rows, &TaskStatus{
			TaskID:     t.ID,
			WorkflowID: d.workflow.ID,
			StatusID:   st.ID,
			Entered:    now,
		})
		history = append(history, newHistoryEntry(t.ID, nil, st, a, ""))
	}
	if err := insertInChunks(s, rows); err != nil {
		return err
	}
	return insertInChunks(s, history)
}

// resetTaskStatusForMove drops the status of a task that moved to another project and gives it the start
// status of the new project's workflow, if there is one. The history stays.
func resetTaskStatusForMove(s *xorm.Session, a web.Auth, t *Task) error {
	if err := cancelApprovalsForTask(s, t.ID, ApprovalCancelTaskMoved); err != nil {
		return err
	}
	if _, err := s.Where("task_id = ?", t.ID).Delete(&TaskStatus{}); err != nil {
		return err
	}
	d, err := loadWorkflowDef(s, t.ProjectID)
	if err != nil || d == nil {
		return err
	}
	return d.assignStartStatuses(s, []*Task{t}, a)
}

func deleteTaskStatusData(s *xorm.Session, taskID int64) error {
	if _, err := s.Where("task_id = ?", taskID).Delete(&TaskStatus{}); err != nil {
		return err
	}
	_, err := s.Where("task_id = ?", taskID).Delete(&TaskStatusHistory{})
	return err
}

// syncWorkflowAfterTaskUpdate keeps the status in line with a done change made through a task update.
// It runs after the task row was written, so required fields set in the same request count.
// wasDone is the stored state, wantsDone what the request asked for; t.Done is the final state,
// which differs from wantsDone when a repeating task re-opened.
func syncWorkflowAfterTaskUpdate(s *xorm.Session, a web.Auth, t *Task, oldProjectID int64, wasDone, wantsDone bool) error {
	if t.ProjectID != oldProjectID {
		return resetTaskStatusForMove(s, a, t)
	}
	if wasDone == wantsDone {
		return nil
	}

	d, err := loadWorkflowDef(s, t.ProjectID)
	if err != nil || d == nil {
		return err
	}

	if !wantsDone {
		initial := d.initialStatus()
		_, err = d.changeTaskStatus(s, a, &statusChange{Task: t, ToStatusID: initial.ID, FailOnApproval: true})
		return err
	}

	done := d.firstDoneStatus()
	if _, err = d.changeTaskStatus(s, a, &statusChange{Task: t, ToStatusID: done.ID, FailOnApproval: true}); err != nil {
		return err
	}
	if t.Done {
		return nil
	}

	// A repeating task re-opened: its next occurrence starts over.
	_, err = d.changeTaskStatus(s, a, &statusChange{Task: t, ToStatusID: d.initialStatus().ID, SkipChecks: true})
	return err
}

// statusChange describes one call of changeTaskStatus.
type statusChange struct {
	Task       *Task
	ToStatusID int64
	Comment    string
	// SyncDone writes the done flag of the task when the category of the new status needs a different one.
	// The task update path writes it itself.
	SyncDone bool
	// SkipChecks is for system transitions that have no actor to check, e.g. re-opening a repeating task. It also
	// skips the approval requirement: system transitions never create approvals.
	SkipChecks bool
	// BypassApproval skips only the approval requirement. The approval of a request uses it; every other check
	// still runs.
	BypassApproval bool
	// FailOnApproval returns ErrApprovalRequired instead of creating a request. Changes that are a side effect of
	// a task update (the done flag) use it: an update must not create requests, the client has to ask for the
	// status explicitly.
	FailOnApproval bool
	// ApprovalRequestID is recorded in the history entry.
	ApprovalRequestID int64
}

// statusChangeResult is the outcome of changeTaskStatus: the status changed (Changed), the task did not move
// because an approval is needed (Approval is set), or nothing was to do.
type statusChangeResult struct {
	From        *WorkflowStatus
	To          *WorkflowStatus
	Changed     bool
	DoneChanged bool
	// Approval is the pending request of the transition when it needs an approval; the task did not move.
	Approval *ApprovalRequest
	// ApprovalCreated is false when the request already existed.
	ApprovalCreated bool
	// Final is the status the task is in at the end when it differs from To: a repeating task that entered a
	// done status is back in the initial one.
	Final *WorkflowStatus
}

// changeTaskStatus is the one place that moves a task between statuses. Checks run in this order: the target
// exists and is not archived, the workflow allows the pair, the actor may perform it, then required fields
// and the comment (reported together so a client can ask for exactly what is missing).
func (d *workflowDef) changeTaskStatus(s *xorm.Session, a web.Auth, c *statusChange) (*statusChangeResult, error) {
	t := c.Task

	to, exists := d.statuses[c.ToStatusID]
	if !exists {
		return nil, ErrWorkflowStatusDoesNotExist{StatusID: c.ToStatusID}
	}
	if to.Archived {
		return nil, ErrWorkflowStatusArchived{StatusID: to.ID}
	}

	current, err := getTaskStatusRow(s, t.ID)
	if err != nil {
		return nil, err
	}
	var from *WorkflowStatus
	if current != nil {
		from = d.statuses[current.StatusID]
		if current.StatusID == to.ID {
			return &statusChangeResult{From: from, To: to}, nil
		}
	}

	if !c.SkipChecks {
		if err = d.checkTransition(s, a, t, from, to, c.Comment); err != nil {
			return nil, err
		}
		if rule := d.approvalRuleFor(from, to); rule != nil && !c.BypassApproval {
			if c.FailOnApproval {
				return nil, ErrApprovalRequired{TaskID: t.ID, FromStatusID: from.ID, ToStatusID: to.ID}
			}
			req, created, err := d.requestTransitionApproval(s, a, t, from, to, rule, c.Comment)
			if err != nil {
				return nil, err
			}
			return &statusChangeResult{From: from, To: to, Approval: req, ApprovalCreated: created}, nil
		}
	}

	now := time.Now()
	if current == nil {
		_, err = s.Insert(&TaskStatus{TaskID: t.ID, WorkflowID: d.workflow.ID, StatusID: to.ID, Entered: now})
	} else {
		_, err = s.ID(current.ID).
			Cols("workflow_id", "status_id", "entered").
			Update(&TaskStatus{WorkflowID: d.workflow.ID, StatusID: to.ID, Entered: now})
	}
	if err != nil {
		return nil, err
	}
	entry := newHistoryEntry(t.ID, from, to, a, c.Comment)
	if c.ApprovalRequestID != 0 {
		id := c.ApprovalRequestID
		entry.ApprovalRequestID = &id
	}
	if _, err = s.Insert(entry); err != nil {
		return nil, err
	}
	// Requests made for the status the task just left are stale, whichever route moved it.
	if err = cancelApprovalsAfterStatusChange(s, t.ID); err != nil {
		return nil, err
	}

	result := &statusChangeResult{From: from, To: to, Changed: true}
	if c.SyncDone {
		if result.DoneChanged, err = syncTaskDoneToStatus(s, a, t, to); err != nil {
			return nil, err
		}
	}

	events.DispatchOnCommit(s, &TaskStatusChangedEvent{
		Task:       t,
		FromStatus: from,
		ToStatus:   to,
		Comment:    c.Comment,
		Doer:       doerFromAuth(s, a),
	})
	return result, nil
}

// syncTaskDoneToStatus sets the done flag the category of the status asks for and moves the task between
// the done buckets of manual kanban views.
func syncTaskDoneToStatus(s *xorm.Session, a web.Auth, t *Task, to *WorkflowStatus) (bool, error) {
	done := to.Category.isFinal()
	if t.Done == done {
		return false, nil
	}

	t.Done = done
	t.DoneAt = time.Time{}
	if done {
		t.DoneAt = time.Now()
	}
	if _, err := s.ID(t.ID).Cols("done", "done_at").Update(&Task{Done: t.Done, DoneAt: t.DoneAt}); err != nil {
		return false, err
	}

	views := []*ProjectView{}
	err := s.Where(builder.And(
		builder.Eq{"project_id": t.ProjectID},
		builder.Eq{"view_kind": ProjectViewKindKanban},
		builder.Eq{"bucket_configuration_mode": BucketConfigurationModeManual},
	)).Find(&views)
	if err != nil {
		return false, err
	}
	if len(views) > 0 {
		if err = t.moveTaskToDoneBuckets(s, a, views); err != nil {
			return false, err
		}
	}
	return true, nil
}

// checkTransition runs the permission, required field and comment checks for a status change.
func (d *workflowDef) checkTransition(s *xorm.Session, a web.Auth, t *Task, from, to *WorkflowStatus, comment string) error {
	var fromID int64
	var rule *WorkflowTransition
	if from != nil {
		fromID = from.ID
		rule = d.ruleFor(from.ID, to.ID)
		if rule == nil && d.workflow.RestrictTransitions {
			return ErrWorkflowTransitionNotAllowed{FromStatusID: fromID, ToStatusID: to.ID}
		}
	}

	if reason, err := checkTransitionActor(s, a, t.ProjectID, rule); err != nil {
		return err
	} else if reason != "" {
		return ErrWorkflowTransitionForbidden{FromStatusID: fromID, ToStatusID: to.ID, Reason: reason}
	}

	missing, commentRequired, err := missingRequirements(s, t, rule, comment)
	if err != nil {
		return err
	}
	if len(missing) > 0 || commentRequired {
		return ErrWorkflowRequirementsNotMet{WorkflowRequirementsDetails{
			TaskID:          t.ID,
			FromStatusID:    fromID,
			ToStatusID:      to.ID,
			MissingFields:   missing,
			CommentRequired: commentRequired,
		}}
	}
	return nil
}

// Reasons of ErrWorkflowTransitionForbidden.
const (
	workflowForbiddenPermission = "permission"
	workflowForbiddenTeam       = "team"
)

// checkTransitionActor returns a non-empty reason when the actor may not perform the transition.
func checkTransitionActor(s *xorm.Session, a web.Auth, projectID int64, rule *WorkflowTransition) (reason string, err error) {
	project := &Project{ID: projectID}

	isAdmin, err := project.IsAdmin(s, a)
	if err != nil {
		return "", err
	}
	if isAdmin {
		return "", nil
	}

	if rule != nil && rule.MinPermission == WorkflowTransitionPermissionAdmin {
		return workflowForbiddenPermission, nil
	}
	canWrite, err := project.CanWrite(s, a)
	if err != nil {
		return "", err
	}
	if !canWrite {
		return workflowForbiddenPermission, nil
	}

	if rule == nil || len(rule.TeamIDs) == 0 {
		return "", nil
	}
	u, isUser := a.(*user.User)
	if !isUser {
		return workflowForbiddenTeam, nil
	}
	member, err := s.Where(builder.And(
		builder.Eq{"user_id": u.ID},
		builder.In("team_id", rule.TeamIDs),
	)).Exist(&TeamMember{})
	if err != nil {
		return "", err
	}
	if !member {
		return workflowForbiddenTeam, nil
	}
	return "", nil
}

// missingRequirements lists the required fields the task does not have yet and whether a comment is missing.
// t has to reflect the stored state of the task.
func missingRequirements(s *xorm.Session, t *Task, rule *WorkflowTransition, comment string) (missing []string, commentRequired bool, err error) {
	missing = []string{}
	if rule == nil {
		return missing, false, nil
	}

	customIDs := []int64{}
	for _, f := range rule.RequiredFields {
		if id, isCustom, ok := parseWorkflowField(f); ok && isCustom {
			customIDs = append(customIDs, id)
		}
	}
	emptyCustom, err := emptyCustomFieldsOfTask(s, t.ID, customIDs)
	if err != nil {
		return nil, false, err
	}

	for _, f := range rule.RequiredFields {
		var isMissing bool
		switch f {
		case WorkflowFieldAssignees:
			isMissing, err = notExists(s, &TaskAssginee{TaskID: t.ID})
		case WorkflowFieldLabels:
			isMissing, err = notExists(s, &LabelTask{TaskID: t.ID})
		case WorkflowFieldAttachments:
			isMissing, err = notExists(s, &TaskAttachment{TaskID: t.ID})
		case WorkflowFieldDueDate:
			isMissing = t.DueDate.IsZero()
		case WorkflowFieldStartDate:
			isMissing = t.StartDate.IsZero()
		case WorkflowFieldEndDate:
			isMissing = t.EndDate.IsZero()
		case WorkflowFieldDescription:
			isMissing = isEmptyDescription(t.Description)
		default:
			id, _, _ := parseWorkflowField(f)
			isMissing = emptyCustom[id]
		}
		if err != nil {
			return nil, false, err
		}
		if isMissing {
			missing = append(missing, f)
		}
	}

	commentRequired = rule.RequireComment && strings.TrimSpace(comment) == ""
	return missing, commentRequired, nil
}

func notExists(s *xorm.Session, bean any) (bool, error) {
	exists, err := s.Exist(bean)
	return !exists, err
}

func isEmptyDescription(d string) bool {
	d = strings.TrimSpace(d)
	return d == "" || d == "<p></p>"
}

// emptyCustomFieldsOfTask returns the ids of the given fields that have no value on the task.
// Fields that no longer exist are not reported.
func emptyCustomFieldsOfTask(s *xorm.Session, taskID int64, fieldIDs []int64) (map[int64]bool, error) {
	empty := map[int64]bool{}
	if len(fieldIDs) == 0 {
		return empty, nil
	}
	fields := []*CustomField{}
	if err := s.Where(builder.In("id", fieldIDs)).Find(&fields); err != nil {
		return nil, err
	}
	rows, err := getCustomFieldValueRows(s, taskID, fieldIDs)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if isEmptyCustomFieldValue(rowsToValue(f.Type, rows[f.ID])) {
			empty[f.ID] = true
		}
	}
	return empty, nil
}

// removeCustomFieldFromWorkflows drops requirements on deleted custom fields.
func removeCustomFieldFromWorkflows(s *xorm.Session, fieldIDs []int64) error {
	names := make([]string, 0, len(fieldIDs))
	for _, id := range fieldIDs {
		names = append(names, workflowCustomFieldName(id))
	}
	_, err := s.Where(builder.In("field", names)).Delete(&WorkflowTransitionRequiredField{})
	return err
}

// duplicateWorkflowForProject copies the definition of one project into another and returns the old to new status id map.
// customFieldMap translates cf_<id> requirements into the copied fields; requirements on unknown fields are dropped.
func duplicateWorkflowForProject(s *xorm.Session, sourceProjectID, targetProjectID int64, customFieldMap map[int64]int64) (statusMap map[int64]int64, workflowID int64, err error) {
	d, err := loadWorkflowDef(s, sourceProjectID)
	if err != nil || d == nil {
		return nil, 0, err
	}

	wf := &Workflow{
		ProjectID:           targetProjectID,
		RestrictTransitions: d.workflow.RestrictTransitions,
	}
	if _, err = s.Insert(wf); err != nil {
		return nil, 0, err
	}

	statusMap = make(map[int64]int64, len(d.workflow.Statuses))
	for _, st := range d.workflow.Statuses {
		oldID := st.ID
		cp := *st
		cp.ID = 0
		cp.WorkflowID = wf.ID
		if _, err = s.Insert(&cp); err != nil {
			return nil, 0, err
		}
		statusMap[oldID] = cp.ID
	}

	for _, tr := range d.workflow.Transitions {
		cp := *tr
		cp.ID = 0
		cp.WorkflowID = wf.ID
		cp.FromStatusID = statusMap[tr.FromStatusID]
		cp.ToStatusID = statusMap[tr.ToStatusID]
		cp.Approval = remapTransitionApproval(tr.Approval, statusMap, customFieldMap)
		if _, err = s.Insert(&cp); err != nil {
			return nil, 0, err
		}
		for _, f := range tr.RequiredFields {
			if id, isCustom, ok := parseWorkflowField(f); ok && isCustom {
				newID, mapped := customFieldMap[id]
				if !mapped {
					continue
				}
				f = workflowCustomFieldName(newID)
			}
			if _, err = s.Insert(&WorkflowTransitionRequiredField{TransitionID: cp.ID, Field: f}); err != nil {
				return nil, 0, err
			}
		}
		for _, teamID := range tr.TeamIDs {
			if _, err = s.Insert(&WorkflowTransitionTeam{TransitionID: cp.ID, TeamID: teamID}); err != nil {
				return nil, 0, err
			}
		}
	}
	return statusMap, wf.ID, nil
}

// copyTaskStatuses gives the copies of tasks the status their originals have, translated through statusMap.
func copyTaskStatuses(s *xorm.Session, taskMap map[int64]int64, statusMap map[int64]int64, workflowID int64) error {
	if len(taskMap) == 0 || len(statusMap) == 0 {
		return nil
	}
	oldIDs := make([]int64, 0, len(taskMap))
	for id := range taskMap {
		oldIDs = append(oldIDs, id)
	}
	sort.Slice(oldIDs, func(i, j int) bool { return oldIDs[i] < oldIDs[j] })

	copies := []*TaskStatus{}
	now := time.Now()
	for _, chunk := range chunkInt64s(oldIDs) {
		rows := []*TaskStatus{}
		if err := s.Where(builder.In("task_id", chunk)).Find(&rows); err != nil {
			return err
		}
		for _, r := range rows {
			newStatus, ok := statusMap[r.StatusID]
			if !ok {
				continue
			}
			copies = append(copies, &TaskStatus{
				TaskID:     taskMap[r.TaskID],
				WorkflowID: workflowID,
				StatusID:   newStatus,
				Entered:    now,
			})
		}
	}
	return insertInChunks(s, copies)
}
