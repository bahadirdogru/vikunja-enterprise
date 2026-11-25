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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// WorkflowStatusCategory groups statuses; it decides what the done flag of a task is.
type WorkflowStatusCategory string

// The status categories.
const (
	WorkflowCategoryOpen       WorkflowStatusCategory = "open"
	WorkflowCategoryInProgress WorkflowStatusCategory = "in_progress"
	WorkflowCategoryDone       WorkflowStatusCategory = "done"
	WorkflowCategoryCancelled  WorkflowStatusCategory = "cancelled"
)

func (c WorkflowStatusCategory) isValid() bool {
	switch c {
	case WorkflowCategoryOpen, WorkflowCategoryInProgress, WorkflowCategoryDone, WorkflowCategoryCancelled:
		return true
	}
	return false
}

// isFinal reports whether tasks in this category count as done.
func (c WorkflowStatusCategory) isFinal() bool {
	return c == WorkflowCategoryDone || c == WorkflowCategoryCancelled
}

// The minimum project permission a transition needs.
const (
	WorkflowTransitionPermissionWrite = "write"
	WorkflowTransitionPermissionAdmin = "admin"
)

// The built-in names a transition can require. Custom fields are cf_<id>.
const (
	WorkflowFieldAssignees   = "assignees"
	WorkflowFieldDueDate     = "due_date"
	WorkflowFieldStartDate   = "start_date"
	WorkflowFieldEndDate     = "end_date"
	WorkflowFieldDescription = "description"
	WorkflowFieldAttachments = "attachments"
	WorkflowFieldLabels      = "labels"

	workflowCustomFieldPrefix = "cf_"
	workflowPositionStep      = 65536
	workflowBulkChunkSize     = 200
)

var (
	workflowBuiltinFields = []string{
		WorkflowFieldAssignees,
		WorkflowFieldDueDate,
		WorkflowFieldStartDate,
		WorkflowFieldEndDate,
		WorkflowFieldDescription,
		WorkflowFieldAttachments,
		WorkflowFieldLabels,
	}
	workflowColorRegex = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// WorkflowStatus is one status of a workflow.
type WorkflowStatus struct {
	ID         int64                  `xorm:"bigint autoincr not null unique pk" json:"id" doc:"The unique, numeric id of this status. Send 0 or omit it to create a status. A negative id creates a status too and lets transitions and move_tasks_to of the same document refer to it before it has a real id."`
	WorkflowID int64                  `xorm:"bigint not null INDEX" json:"workflow_id" readOnly:"true" doc:"The workflow this status belongs to."`
	Title      string                 `xorm:"varchar(250) not null" json:"title" minLength:"1" maxLength:"250" doc:"The title of the status."`
	Color      string                 `xorm:"varchar(7) null" json:"color" maxLength:"7" doc:"Optional hex color including the leading #, e.g. #ff8800."`
	Category   WorkflowStatusCategory `xorm:"varchar(20) not null" json:"category" enum:"open,in_progress,done,cancelled" doc:"The category of the status. Tasks in a done or cancelled status are marked done."`
	Position   float64                `xorm:"double not null" json:"position" doc:"The position of the status in pickers. Statuses go in the submitted order when it is 0."`
	IsInitial  bool                   `xorm:"bool default false" json:"is_initial" doc:"Whether new tasks start in this status. Exactly one non-archived status must be the initial one."`
	Archived   bool                   `xorm:"bool default false" json:"archived" readOnly:"true" doc:"Archived statuses are hidden from pickers and keep their history. Set by the server when a status that has history is removed from the document."`
}

// TableName returns the table name of workflow statuses.
func (*WorkflowStatus) TableName() string {
	return "workflow_statuses"
}

// WorkflowTransition is the rule set of one from to pair of statuses.
type WorkflowTransition struct {
	ID               int64               `xorm:"bigint autoincr not null unique pk" json:"id" doc:"The unique, numeric id of this transition. Send 0 or omit it to create one."`
	WorkflowID       int64               `xorm:"bigint not null INDEX unique(workflow_transitions_pair)" json:"workflow_id" readOnly:"true" doc:"The workflow this transition belongs to."`
	FromStatusID     int64               `xorm:"bigint not null unique(workflow_transitions_pair)" json:"from_status_id" doc:"The status tasks leave. A negative id refers to a new status of the same document."`
	ToStatusID       int64               `xorm:"bigint not null unique(workflow_transitions_pair)" json:"to_status_id" doc:"The status tasks enter. A negative id refers to a new status of the same document."`
	Title            string              `xorm:"varchar(250) null" json:"title" maxLength:"250" doc:"Optional button label, e.g. Send to design."`
	MinPermission    string              `xorm:"varchar(10) not null" json:"min_permission" enum:"write,admin" doc:"The lowest project permission that may perform the transition. write when omitted."`
	RequireComment   bool                `xorm:"bool default false" json:"require_comment" doc:"Whether the person performing the transition has to leave a comment."`
	RequiresApproval bool                `xorm:"bool default false" json:"requires_approval" doc:"Whether performing the transition only asks for an approval: the task does not move until the approvers agreed (see approval). The permission, team, required field and comment checks run first."`
	Approval         *TransitionApproval `xorm:"json null" json:"approval" doc:"The approval configuration, required when requires_approval is set and ignored (stored as null) otherwise."`
	RequiredFields   []string            `xorm:"-" json:"required_fields" doc:"Fields that must be set on the task before the transition: assignees, due_date, start_date, end_date, description, attachments, labels or cf_<id> for a custom field of the project."`
	TeamIDs          []int64             `xorm:"-" json:"team_ids" doc:"When not empty, only members of one of these teams, or project admins, may perform the transition."`
}

// TableName returns the table name of workflow transitions.
func (*WorkflowTransition) TableName() string {
	return "workflow_transitions"
}

// WorkflowTransitionRequiredField is one field a transition requires.
type WorkflowTransitionRequiredField struct {
	ID           int64  `xorm:"bigint autoincr not null unique pk"`
	TransitionID int64  `xorm:"bigint not null INDEX"`
	Field        string `xorm:"varchar(100) not null"`
}

// TableName returns the table name of required fields.
func (*WorkflowTransitionRequiredField) TableName() string {
	return "workflow_transition_required_fields"
}

// WorkflowTransitionTeam limits a transition to the members of a team.
type WorkflowTransitionTeam struct {
	ID           int64 `xorm:"bigint autoincr not null unique pk"`
	TransitionID int64 `xorm:"bigint not null INDEX unique(workflow_transition_teams_pair)"`
	TeamID       int64 `xorm:"bigint not null unique(workflow_transition_teams_pair)"`
}

// TableName returns the table name of transition teams.
func (*WorkflowTransitionTeam) TableName() string {
	return "workflow_transition_teams"
}

// Workflow is the whole workflow definition of a project, handled as one document.
type Workflow struct {
	ID                  int64 `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this workflow."`
	ProjectID           int64 `xorm:"bigint not null unique" json:"project_id" readOnly:"true" doc:"The project this workflow belongs to. Taken from the URL."`
	RestrictTransitions bool  `xorm:"bool default false" json:"restrict_transitions" doc:"When false every status change is allowed and the listed transitions only add rules for their pairs. When true only the listed transitions are allowed."`

	Statuses    []*WorkflowStatus     `xorm:"-" json:"statuses" doc:"The statuses of the workflow ordered by position, archived ones included. On write the submitted list replaces the stored one: statuses with an id are updated, statuses without one are created and missing ones are deleted, or archived when they have history."`
	Transitions []*WorkflowTransition `xorm:"-" json:"transitions" doc:"The transition rules. On write the submitted list replaces the stored one."`
	MoveTasksTo int64                 `xorm:"-" json:"move_tasks_to,omitempty" doc:"Write only. The status, by id or by the negative id of a new status of the same document, that receives the tasks of every status the submitted document removes. Required when a removed status still has tasks."`

	Created time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this workflow was created. You cannot change this value."`
	Updated time.Time `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this workflow was last updated. You cannot change this value."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name of workflows.
func (*Workflow) TableName() string {
	return "workflows"
}

func getWorkflowByProject(s *xorm.Session, projectID int64) (*Workflow, error) {
	wf := &Workflow{}
	exists, err := s.Where("project_id = ?", projectID).Get(wf)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrWorkflowDoesNotExist{ProjectID: projectID}
	}
	return wf, nil
}

func loadWorkflowRelations(s *xorm.Session, wf *Workflow) error {
	wf.Statuses = []*WorkflowStatus{}
	if err := s.Where("workflow_id = ?", wf.ID).OrderBy("position ASC, id ASC").Find(&wf.Statuses); err != nil {
		return err
	}

	wf.Transitions = []*WorkflowTransition{}
	if err := s.Where("workflow_id = ?", wf.ID).OrderBy("id ASC").Find(&wf.Transitions); err != nil {
		return err
	}
	if len(wf.Transitions) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(wf.Transitions))
	byID := make(map[int64]*WorkflowTransition, len(wf.Transitions))
	for _, tr := range wf.Transitions {
		tr.RequiredFields = []string{}
		tr.TeamIDs = []int64{}
		ids = append(ids, tr.ID)
		byID[tr.ID] = tr
	}

	fields := []*WorkflowTransitionRequiredField{}
	if err := s.Where(builder.In("transition_id", ids)).OrderBy("id ASC").Find(&fields); err != nil {
		return err
	}
	for _, f := range fields {
		byID[f.TransitionID].RequiredFields = append(byID[f.TransitionID].RequiredFields, f.Field)
	}

	teams := []*WorkflowTransitionTeam{}
	if err := s.Where(builder.In("transition_id", ids)).OrderBy("id ASC").Find(&teams); err != nil {
		return err
	}
	for _, t := range teams {
		byID[t.TransitionID].TeamIDs = append(byID[t.TransitionID].TeamIDs, t.TeamID)
	}
	return nil
}

// ReadOne adds statuses and transitions; CanRead already loaded the workflow row.
// @Summary Get the workflow of a project
func (wf *Workflow) ReadOne(s *xorm.Session, _ web.Auth) error {
	return loadWorkflowRelations(s, wf)
}

// workflowDocPlan is the result of validating a submitted document against the stored state.
type workflowDocPlan struct {
	existing    *Workflow
	stored      map[int64]*WorkflowStatus
	removed     []*WorkflowStatus
	moveTarget  *WorkflowStatus // set when a status with tasks is removed
	moveTempID  int64
	activeCount int
}

//nolint:gocyclo
func (wf *Workflow) validateDocument(s *xorm.Session, existing *Workflow) (*workflowDocPlan, error) {
	plan := &workflowDocPlan{
		existing: existing,
		stored:   map[int64]*WorkflowStatus{},
	}
	invalid := func(reason string) (*workflowDocPlan, error) {
		return nil, ErrWorkflowInvalidDefinition{Reason: reason}
	}

	if existing != nil {
		stored := []*WorkflowStatus{}
		if err := s.Where("workflow_id = ?", existing.ID).Find(&stored); err != nil {
			return nil, err
		}
		for _, st := range stored {
			plan.stored[st.ID] = st
		}
	}

	if len(wf.Statuses) == 0 {
		return invalid("a workflow needs at least one status")
	}

	docIDs := map[int64]bool{}
	activeInitial, activeDone := 0, 0
	archivedInDoc := map[int64]bool{}
	for _, st := range wf.Statuses {
		if st == nil {
			return invalid("statuses must not be null")
		}
		st.Title = strings.TrimSpace(st.Title)
		if st.Title == "" || len([]rune(st.Title)) > 250 {
			return invalid("status titles must be between 1 and 250 characters")
		}
		if st.Color != "" && !workflowColorRegex.MatchString(st.Color) {
			return invalid("status colors must look like #aabbcc")
		}
		if !st.Category.isValid() {
			return invalid("unknown status category " + string(st.Category))
		}

		if st.ID != 0 {
			if docIDs[st.ID] {
				return invalid("status ids must be unique within the document")
			}
			docIDs[st.ID] = true
		}

		archived := false
		if st.ID > 0 {
			storedStatus, exists := plan.stored[st.ID]
			if !exists {
				return nil, ErrWorkflowStatusDoesNotExist{StatusID: st.ID}
			}
			archived = storedStatus.Archived
		}
		if archived {
			archivedInDoc[st.ID] = true
			if st.IsInitial {
				return invalid("an archived status cannot be the initial status")
			}
			continue
		}

		plan.activeCount++
		if st.IsInitial {
			activeInitial++
		}
		if st.Category == WorkflowCategoryDone {
			activeDone++
		}
	}
	if activeInitial != 1 {
		return invalid("exactly one status must be the initial status")
	}
	if activeDone < 1 {
		return invalid("at least one status needs the category done")
	}

	if err := wf.validateTransitions(s, docIDs, archivedInDoc); err != nil {
		return nil, err
	}

	removedIDs := []int64{}
	for id, st := range plan.stored {
		if !docIDs[id] {
			plan.removed = append(plan.removed, st)
			removedIDs = append(removedIDs, id)
		}
	}
	sort.Slice(plan.removed, func(i, j int) bool { return plan.removed[i].ID < plan.removed[j].ID })

	if wf.MoveTasksTo != 0 {
		target := wf.findStatusByDocID(wf.MoveTasksTo)
		if target == nil {
			return invalid("move_tasks_to must be a status of the submitted document")
		}
		if archivedInDoc[wf.MoveTasksTo] {
			return invalid("move_tasks_to must not be an archived status")
		}
		plan.moveTarget = target
		plan.moveTempID = wf.MoveTasksTo
	}

	if len(removedIDs) > 0 {
		withTasks := []int64{}
		err := s.Table("task_statuses").
			Distinct("status_id").
			Where(builder.In("status_id", removedIDs)).
			Find(&withTasks)
		if err != nil {
			return nil, err
		}
		if len(withTasks) > 0 && plan.moveTarget == nil {
			sort.Slice(withTasks, func(i, j int) bool { return withTasks[i] < withTasks[j] })
			return nil, ErrWorkflowMoveTasksRequired{StatusIDs: withTasks}
		}
	}

	return plan, nil
}

func (wf *Workflow) findStatusByDocID(id int64) *WorkflowStatus {
	for _, st := range wf.Statuses {
		if st.ID == id {
			return st
		}
	}
	return nil
}

func (wf *Workflow) validateTransitions(s *xorm.Session, docIDs, archivedInDoc map[int64]bool) error {
	seenPairs := map[[2]int64]bool{}
	seenIDs := map[int64]bool{}
	customFieldIDs := []int64{}
	teamIDs := []int64{}

	for _, tr := range wf.Transitions {
		if tr == nil {
			return ErrWorkflowInvalidDefinition{Reason: "transitions must not be null"}
		}
		if !docIDs[tr.FromStatusID] || !docIDs[tr.ToStatusID] {
			return ErrWorkflowInvalidDefinition{Reason: "transitions must reference statuses of this workflow"}
		}
		if archivedInDoc[tr.FromStatusID] || archivedInDoc[tr.ToStatusID] {
			return ErrWorkflowInvalidDefinition{Reason: "transitions must not reference archived statuses"}
		}
		if tr.FromStatusID == tr.ToStatusID {
			return ErrWorkflowInvalidDefinition{Reason: "a transition needs two different statuses"}
		}
		pair := [2]int64{tr.FromStatusID, tr.ToStatusID}
		if seenPairs[pair] {
			return ErrWorkflowInvalidDefinition{Reason: "a from and to status pair must not be listed twice"}
		}
		seenPairs[pair] = true
		if tr.ID > 0 {
			if seenIDs[tr.ID] {
				return ErrWorkflowInvalidDefinition{Reason: "transition ids must be unique within the document"}
			}
			seenIDs[tr.ID] = true
		}

		tr.Title = strings.TrimSpace(tr.Title)
		if len([]rune(tr.Title)) > 250 {
			return ErrWorkflowInvalidDefinition{Reason: "transition titles must not be longer than 250 characters"}
		}
		switch tr.MinPermission {
		case "":
			tr.MinPermission = WorkflowTransitionPermissionWrite
		case WorkflowTransitionPermissionWrite, WorkflowTransitionPermissionAdmin:
		default:
			return ErrWorkflowInvalidDefinition{Reason: "min_permission must be write or admin"}
		}

		fields := make([]string, 0, len(tr.RequiredFields))
		seenFields := map[string]bool{}
		for _, f := range tr.RequiredFields {
			if seenFields[f] {
				continue
			}
			seenFields[f] = true
			id, isCustom, ok := parseWorkflowField(f)
			if !ok {
				return ErrWorkflowInvalidDefinition{Reason: "unknown required field " + f}
			}
			if isCustom {
				customFieldIDs = append(customFieldIDs, id)
			}
			fields = append(fields, f)
		}
		tr.RequiredFields = fields
		tr.TeamIDs = uniqueInts(tr.TeamIDs)
		teamIDs = append(teamIDs, tr.TeamIDs...)

		if err := validateTransitionApproval(s, wf.ProjectID, tr, docIDs, archivedInDoc); err != nil {
			return err
		}
	}

	if len(customFieldIDs) > 0 {
		customFieldIDs = uniqueInts(customFieldIDs)
		count, err := s.Where(builder.And(
			builder.Eq{"project_id": wf.ProjectID},
			builder.In("id", customFieldIDs),
		)).Count(&CustomField{})
		if err != nil {
			return err
		}
		if int(count) != len(customFieldIDs) {
			return ErrWorkflowInvalidDefinition{Reason: "required custom fields must belong to the project of the workflow"}
		}
	}
	if len(teamIDs) > 0 {
		teamIDs = uniqueInts(teamIDs)
		count, err := s.Where(builder.In("id", teamIDs)).Count(&Team{})
		if err != nil {
			return err
		}
		if int(count) != len(teamIDs) {
			return ErrWorkflowInvalidDefinition{Reason: "some of the teams do not exist"}
		}
	}
	return nil
}

func workflowCustomFieldName(id int64) string {
	return workflowCustomFieldPrefix + strconv.FormatInt(id, 10)
}

// parseWorkflowField returns the custom field id for cf_<id> names. ok is false for unknown names.
func parseWorkflowField(name string) (customFieldID int64, isCustom, ok bool) {
	for _, b := range workflowBuiltinFields {
		if name == b {
			return 0, false, true
		}
	}
	rest, found := strings.CutPrefix(name, workflowCustomFieldPrefix)
	if !found {
		return 0, false, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != rest {
		return 0, false, false
	}
	return id, true, true
}

// Update creates the workflow of the project or replaces the stored one by the submitted document.
// @Summary Create or replace the workflow of a project
func (wf *Workflow) Update(s *xorm.Session, a web.Auth) (err error) {
	existing, err := getWorkflowByProject(s, wf.ProjectID)
	if err != nil && !IsErrWorkflowDoesNotExist(err) {
		return err
	}
	if err != nil {
		existing = nil
	}

	plan, err := wf.validateDocument(s, existing)
	if err != nil {
		return err
	}

	created := existing == nil
	if created {
		wf.ID = 0
		if _, err = s.Insert(wf); err != nil {
			return err
		}
	} else {
		wf.ID = existing.ID
		if _, err = s.ID(wf.ID).Cols("restrict_transitions").Update(wf); err != nil {
			return err
		}
	}

	idMap, err := wf.saveStatuses(s, a, plan)
	if err != nil {
		return err
	}
	if err = wf.removeStatuses(s, a, plan, idMap); err != nil {
		return err
	}
	if err = wf.saveTransitions(s, idMap); err != nil {
		return err
	}

	if created {
		if err = assignStatusesForNewWorkflow(s, wf.ID, wf.ProjectID); err != nil {
			return err
		}
	}

	stored, err := getWorkflowByProject(s, wf.ProjectID)
	if err != nil {
		return err
	}
	*wf = *stored
	if err = loadWorkflowRelations(s, wf); err != nil {
		return err
	}

	doer := doerFromAuth(s, a)
	if created {
		events.DispatchOnCommit(s, &WorkflowCreatedEvent{Workflow: wf, Doer: doer})
	} else {
		events.DispatchOnCommit(s, &WorkflowUpdatedEvent{Workflow: wf, Doer: doer})
	}
	return nil
}

// saveStatuses writes the submitted statuses and returns the submitted id to real id map.
func (wf *Workflow) saveStatuses(s *xorm.Session, a web.Auth, plan *workflowDocPlan) (map[int64]int64, error) {
	idMap := make(map[int64]int64, len(wf.Statuses))
	for i, st := range wf.Statuses {
		submittedID := st.ID
		if st.Position == 0 {
			st.Position = float64(i+1) * workflowPositionStep
		}
		st.WorkflowID = wf.ID

		if st.ID > 0 {
			stored := plan.stored[st.ID]
			if stored.Archived {
				// Archived statuses stay as they are; only their title may be fixed.
				st.Archived = true
				st.IsInitial = false
				if _, err := s.ID(st.ID).Cols("title", "color", "position").Update(st); err != nil {
					return nil, err
				}
			} else {
				st.Archived = false
				if _, err := s.ID(st.ID).Cols("title", "color", "category", "position", "is_initial").Update(st); err != nil {
					return nil, err
				}
				// The category decides the done flag of the tasks in the status, also when it is edited.
				if stored.Category.isFinal() != st.Category.isFinal() {
					taskIDs := []int64{}
					if err := s.Table("task_statuses").Where("status_id = ?", st.ID).Cols("task_id").Find(&taskIDs); err != nil {
						return nil, err
					}
					if err := flipDoneOfTasks(s, a, taskIDs, st.Category.isFinal()); err != nil {
						return nil, err
					}
				}
			}
			idMap[submittedID] = st.ID
			continue
		}

		st.ID = 0
		st.Archived = false
		if _, err := s.Insert(st); err != nil {
			return nil, err
		}
		if submittedID != 0 {
			idMap[submittedID] = st.ID
		}
	}
	return idMap, nil
}

// removeStatuses moves the tasks of removed statuses and then deletes or archives the statuses.
func (wf *Workflow) removeStatuses(s *xorm.Session, a web.Auth, plan *workflowDocPlan, idMap map[int64]int64) error {
	if len(plan.removed) == 0 {
		return nil
	}

	var target *WorkflowStatus
	if plan.moveTarget != nil {
		target = plan.moveTarget
		target.ID = idMap[plan.moveTempID]
	}

	removedIDs := make([]int64, 0, len(plan.removed))
	for _, st := range plan.removed {
		removedIDs = append(removedIDs, st.ID)
	}
	if err := disableAutomationRulesForStatuses(s, wf.ProjectID, removedIDs); err != nil {
		return err
	}
	if err := disableFormsForStatuses(s, wf.ProjectID, removedIDs); err != nil {
		return err
	}
	if err := cancelApprovalsForStatuses(s, removedIDs); err != nil {
		return err
	}

	for _, st := range plan.removed {
		if target != nil {
			if err := moveTasksBetweenStatuses(s, a, wf.ProjectID, st, target); err != nil {
				return err
			}
		}

		hasHistory, err := s.Where(builder.Or(
			builder.Eq{"from_status_id": st.ID},
			builder.Eq{"to_status_id": st.ID},
		)).Exist(&TaskStatusHistory{})
		if err != nil {
			return err
		}
		if hasHistory {
			st.Archived = true
			st.IsInitial = false
			if _, err = s.ID(st.ID).Cols("archived", "is_initial").Update(st); err != nil {
				return err
			}
			continue
		}
		if _, err = s.ID(st.ID).Delete(&WorkflowStatus{}); err != nil {
			return err
		}
	}
	return nil
}

// setDoneOfTasks writes the done flag of tasks whose status, or the category of it, changed in bulk. Trashed
// tasks too: they come back with a restore.
func setDoneOfTasks(s *xorm.Session, taskIDs []int64, done bool) error {
	doneAt := time.Time{}
	if done {
		doneAt = time.Now()
	}
	for _, chunk := range chunkInt64s(taskIDs) {
		_, err := s.Unscoped().
			Where(builder.In("id", chunk)).
			Cols("done", "done_at").
			Update(&Task{Done: done, DoneAt: doneAt})
		if err != nil {
			return err
		}
	}
	return nil
}

// saveTransitions replaces the stored transitions by the submitted ones. Transitions whose pair did not change keep their id.
//
//nolint:gocyclo
func (wf *Workflow) saveTransitions(s *xorm.Session, idMap map[int64]int64) error {
	stored := []*WorkflowTransition{}
	if err := s.Where("workflow_id = ?", wf.ID).Find(&stored); err != nil {
		return err
	}
	storedByID := make(map[int64]*WorkflowTransition, len(stored))
	for _, tr := range stored {
		storedByID[tr.ID] = tr
	}

	for _, tr := range wf.Transitions {
		tr.FromStatusID = idMap[tr.FromStatusID]
		tr.ToStatusID = idMap[tr.ToStatusID]
		if tr.Approval != nil && tr.Approval.RejectedStatusID != 0 {
			tr.Approval.RejectedStatusID = idMap[tr.Approval.RejectedStatusID]
		}
	}

	keep := map[int64]bool{}
	for _, tr := range wf.Transitions {
		old, exists := storedByID[tr.ID]
		if exists && old.FromStatusID == tr.FromStatusID && old.ToStatusID == tr.ToStatusID {
			keep[tr.ID] = true
		}
	}

	removed := []int64{}
	for id := range storedByID {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i] < removed[j] })
	if err := cancelApprovalsForTransitions(s, removed, ApprovalCancelTransitionRemoved); err != nil {
		return err
	}
	disabled := []int64{}
	for _, tr := range wf.Transitions {
		if keep[tr.ID] && storedByID[tr.ID].RequiresApproval && !tr.RequiresApproval {
			disabled = append(disabled, tr.ID)
		}
	}
	if err := cancelApprovalsForTransitions(s, disabled, ApprovalCancelApprovalDisabled); err != nil {
		return err
	}
	if len(removed) > 0 {
		if _, err := s.Where(builder.In("transition_id", removed)).Delete(&WorkflowTransitionRequiredField{}); err != nil {
			return err
		}
		if _, err := s.Where(builder.In("transition_id", removed)).Delete(&WorkflowTransitionTeam{}); err != nil {
			return err
		}
		if _, err := s.Where(builder.In("id", removed)).Delete(&WorkflowTransition{}); err != nil {
			return err
		}
	}

	for _, tr := range wf.Transitions {
		tr.WorkflowID = wf.ID
		if keep[tr.ID] {
			_, err := s.ID(tr.ID).
				Cols("title", "min_permission", "require_comment", "requires_approval", "approval").
				Update(tr)
			if err != nil {
				return err
			}
			if _, err = s.Where("transition_id = ?", tr.ID).Delete(&WorkflowTransitionRequiredField{}); err != nil {
				return err
			}
			if _, err = s.Where("transition_id = ?", tr.ID).Delete(&WorkflowTransitionTeam{}); err != nil {
				return err
			}
		} else {
			tr.ID = 0
			if _, err := s.Insert(tr); err != nil {
				return err
			}
		}

		for _, f := range tr.RequiredFields {
			if _, err := s.Insert(&WorkflowTransitionRequiredField{TransitionID: tr.ID, Field: f}); err != nil {
				return err
			}
		}
		for _, teamID := range tr.TeamIDs {
			if _, err := s.Insert(&WorkflowTransitionTeam{TransitionID: tr.ID, TeamID: teamID}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Delete removes the workflow with its statuses, transitions and the current task statuses.
// The history stays; it carries the status titles.
// @Summary Delete the workflow of a project
func (wf *Workflow) Delete(s *xorm.Session, a web.Auth) error {
	existing, err := getWorkflowByProject(s, wf.ProjectID)
	if err != nil {
		return err
	}
	if err = loadWorkflowRelations(s, existing); err != nil {
		return err
	}
	if err = cancelApprovalsForWorkflow(s, wf.ProjectID); err != nil {
		return err
	}
	if err = deleteWorkflowByID(s, existing.ID); err != nil {
		return err
	}
	events.DispatchOnCommit(s, &WorkflowDeletedEvent{
		Workflow: existing,
		Doer:     doerFromAuth(s, a),
	})
	return nil
}

func deleteWorkflowByID(s *xorm.Session, workflowID int64) error {
	stored := &Workflow{}
	if has, err := s.ID(workflowID).Get(stored); err != nil {
		return err
	} else if has {
		statusIDs := []int64{}
		if err := s.Table("workflow_statuses").Where("workflow_id = ?", workflowID).Cols("id").Find(&statusIDs); err != nil {
			return err
		}
		if err := disableAutomationRulesForStatuses(s, stored.ProjectID, statusIDs); err != nil {
			return err
		}
		if err := disableFormsForStatuses(s, stored.ProjectID, statusIDs); err != nil {
			return err
		}
	}

	transitionIDs := []int64{}
	if err := s.Table("workflow_transitions").Where("workflow_id = ?", workflowID).Cols("id").Find(&transitionIDs); err != nil {
		return err
	}
	if len(transitionIDs) > 0 {
		if _, err := s.Where(builder.In("transition_id", transitionIDs)).Delete(&WorkflowTransitionRequiredField{}); err != nil {
			return err
		}
		if _, err := s.Where(builder.In("transition_id", transitionIDs)).Delete(&WorkflowTransitionTeam{}); err != nil {
			return err
		}
	}
	if _, err := s.Where("workflow_id = ?", workflowID).Delete(&WorkflowTransition{}); err != nil {
		return err
	}
	if _, err := s.Where("workflow_id = ?", workflowID).Delete(&WorkflowStatus{}); err != nil {
		return err
	}
	if _, err := s.Where("workflow_id = ?", workflowID).Delete(&TaskStatus{}); err != nil {
		return err
	}
	_, err := s.ID(workflowID).Delete(&Workflow{})
	return err
}

// deleteWorkflowForProject is used when a project is purged.
func deleteWorkflowForProject(s *xorm.Session, projectID int64) error {
	wf, err := getWorkflowByProject(s, projectID)
	if err != nil {
		if IsErrWorkflowDoesNotExist(err) {
			return nil
		}
		return err
	}
	return deleteWorkflowByID(s, wf.ID)
}

// chunkInt64s cuts ids into the pieces a bulk statement takes at once.
func chunkInt64s(in []int64) [][]int64 {
	chunks := [][]int64{}
	for start := 0; start < len(in); start += workflowBulkChunkSize {
		end := min(start+workflowBulkChunkSize, len(in))
		chunks = append(chunks, in[start:end])
	}
	return chunks
}
