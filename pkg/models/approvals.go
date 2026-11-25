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
	"html"
	"sort"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// ApprovalStatus is the state of an approval request.
type ApprovalStatus string

// The states of an approval request.
const (
	ApprovalStatusPending   ApprovalStatus = "pending"
	ApprovalStatusApproved  ApprovalStatus = "approved"
	ApprovalStatusRejected  ApprovalStatus = "rejected"
	ApprovalStatusCancelled ApprovalStatus = "cancelled"
)

// ApprovalMode says how many approvers have to agree.
type ApprovalMode string

// The approval modes.
const (
	// ApprovalModeAny resolves with the first decision, whatever it is.
	ApprovalModeAny ApprovalMode = "any"
	// ApprovalModeAll needs every approver to approve; one rejection rejects.
	ApprovalModeAll ApprovalMode = "all"
)

func (m ApprovalMode) isValid() bool {
	return m == ApprovalModeAny || m == ApprovalModeAll
}

// ApprovalDecisionValue is what an approver decides.
type ApprovalDecisionValue string

// The decisions.
const (
	ApprovalDecisionApproved ApprovalDecisionValue = "approved"
	ApprovalDecisionRejected ApprovalDecisionValue = "rejected"
)

// The reasons a request is cancelled, machine readable in cancel_reason.
const (
	ApprovalCancelByRequester        = "cancelled_by_requester"
	ApprovalCancelTaskDeleted        = "task_deleted"
	ApprovalCancelTaskMoved          = "task_moved"
	ApprovalCancelWorkflowRemoved    = "workflow_removed"
	ApprovalCancelStatusRemoved      = "status_removed"
	ApprovalCancelTransitionRemoved  = "transition_removed"
	ApprovalCancelApprovalDisabled   = "approval_disabled"
	ApprovalCancelStatusChanged      = "status_changed"
	ApprovalCancelTargetArchived     = "target_archived"
	ApprovalCancelRequirementsNotMet = "requirements_not_met"
	ApprovalCancelRequesterNoAccess  = "requester_lost_access"
	ApprovalCancelRequesterNotInTeam = "requester_not_in_team"
	ApprovalCancelRequesterDeleted   = "requester_deleted"
	ApprovalCancelRequesterDisabled  = "requester_disabled"
	ApprovalCancelNoApprovers        = "no_approvers"
)

// approvalCancelMessages are the human texts stored with the reasons.
var approvalCancelMessages = map[string]string{
	ApprovalCancelByRequester:        "The request was cancelled.",
	ApprovalCancelTaskDeleted:        "The task was deleted.",
	ApprovalCancelTaskMoved:          "The task was moved to another project.",
	ApprovalCancelWorkflowRemoved:    "The workflow of the project was removed.",
	ApprovalCancelStatusRemoved:      "A status of the request was removed from the workflow.",
	ApprovalCancelTransitionRemoved:  "The transition of the request no longer exists.",
	ApprovalCancelApprovalDisabled:   "The transition no longer needs an approval.",
	ApprovalCancelStatusChanged:      "The task left the status the request was made in.",
	ApprovalCancelTargetArchived:     "The target status was archived.",
	ApprovalCancelRequirementsNotMet: "The task no longer meets the requirements of the transition.",
	ApprovalCancelRequesterNoAccess:  "The requester may no longer perform this transition.",
	ApprovalCancelRequesterNotInTeam: "The requester is no longer in a team that may perform this transition.",
	ApprovalCancelRequesterDeleted:   "The requester no longer exists.",
	ApprovalCancelRequesterDisabled:  "The requester's account is disabled.",
	ApprovalCancelNoApprovers:        "Nobody who may read the task is left to decide.",
}

// ApproverSourceType is where the approvers of a transition come from.
type ApproverSourceType string

// The approver source types.
const (
	ApproverSourceUser        ApproverSourceType = "user"
	ApproverSourceTeam        ApproverSourceType = "team"
	ApproverSourceAssignees   ApproverSourceType = "assignees"
	ApproverSourceCustomField ApproverSourceType = "custom_field"
)

// ApproverSource is one way to name approvers on a transition.
type ApproverSource struct {
	Type ApproverSourceType `json:"type" enum:"user,team,assignees,custom_field" doc:"user: the user with this id. team: the members of the team with this id, expanded when the request is created. assignees: the assignees of the task. custom_field: the users in the user custom field with this id on the task."`
	ID   int64              `json:"id,omitempty" doc:"The user, team or custom field id. Not used by assignees."`
}

// TransitionApproval is the approval configuration of a workflow transition with requires_approval.
type TransitionApproval struct {
	Mode             ApprovalMode     `json:"mode" enum:"any,all" doc:"any: the first decision resolves the request. all: every approver has to approve, one rejection rejects. any when omitted."`
	Approvers        []ApproverSource `json:"approvers" doc:"Where the approvers come from. They are resolved to concrete users when the request is created, de-duplicated and without anyone who cannot read the task. At least one source is required."`
	RejectedStatusID int64            `json:"rejected_status_id,omitempty" doc:"The status a rejected task moves to, by id or by the negative id of a new status of the same document. 0 or omitted: the task stays where it is."`
	AllowSelf        bool             `json:"allow_self" doc:"Whether the requester may be one of the approvers and approve their own request."`
	DueIn            string           `json:"due_in,omitempty" doc:"Optional time the approvers get to decide, like 12h, 3d or 2w. The approvers are reminded once when it has passed."`
}

// clone returns a deep copy.
func (a *TransitionApproval) clone() *TransitionApproval {
	if a == nil {
		return nil
	}
	cp := *a
	cp.Approvers = append([]ApproverSource{}, a.Approvers...)
	return &cp
}

// ApprovalTaskSummary is the compact form of the task of a request, so lists need no further lookups.
type ApprovalTaskSummary struct {
	ID           int64  `json:"id" doc:"The id of the task."`
	Title        string `json:"title" doc:"The title of the task."`
	Identifier   string `json:"identifier" doc:"The human readable identifier of the task, e.g. PRJ-12 or #12."`
	ProjectID    int64  `json:"project_id" doc:"The project of the task."`
	ProjectTitle string `json:"project_title" doc:"The title of the project."`
}

// ApprovalRequest is one request for a recorded decision by named people. A request made by a workflow
// transition carries the statuses of the transition; an ad-hoc request has none.
type ApprovalRequest struct {
	ID               int64          `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this request."`
	TaskID           int64          `xorm:"bigint not null INDEX" json:"task_id" readOnly:"true" doc:"The task the request is about. Taken from the URL on create."`
	ProjectID        int64          `xorm:"bigint not null INDEX" json:"project_id" readOnly:"true" doc:"The project of the task when the request was made."`
	TransitionID     *int64         `xorm:"bigint null" json:"transition_id" readOnly:"true" doc:"The workflow transition that asked for the approval, null for ad-hoc requests."`
	FromStatusID     *int64         `xorm:"bigint null" json:"from_status_id" readOnly:"true" doc:"The status the task has to be in, null for ad-hoc requests."`
	ToStatusID       *int64         `xorm:"bigint null" json:"to_status_id" readOnly:"true" doc:"The status the task enters when the request is approved, null for ad-hoc requests."`
	RejectedStatusID *int64         `xorm:"bigint null" json:"rejected_status_id" readOnly:"true" doc:"The status the task moves to when the request is rejected, null when it stays."`
	Title            string         `xorm:"varchar(250) not null" json:"title" maxLength:"250" doc:"What is asked. Required for ad-hoc requests; for transitions it is the title of the transition or built from the status titles."`
	Description      string         `xorm:"longtext null" json:"description" doc:"Optional details for the approvers."`
	Comment          string         `xorm:"longtext null" json:"comment" readOnly:"true" doc:"The comment sent with the status change that created the request. It is stored in the history when the change happens."`
	Mode             ApprovalMode   `xorm:"varchar(10) not null" json:"mode" enum:"any,all" doc:"any: the first decision resolves the request. all: every approver has to approve, one rejection rejects. any when omitted."`
	Status           ApprovalStatus `xorm:"varchar(20) not null INDEX" json:"status" readOnly:"true" enum:"pending,approved,rejected,cancelled" doc:"The state of the request."`
	AllowSelf        bool           `xorm:"bool default false" json:"allow_self" readOnly:"true" doc:"Whether the requester may approve their own request."`
	RequestedByID    int64          `xorm:"bigint not null INDEX" json:"-"`
	Due              *time.Time     `xorm:"DATETIME null" json:"due" doc:"When the approvers should have decided. Optional."`
	RemindedAt       *time.Time     `xorm:"DATETIME null" json:"-"`
	ResolvedByID     int64          `xorm:"bigint not null default 0" json:"-"`
	CancelReason     string         `xorm:"varchar(50) null" json:"cancel_reason" readOnly:"true" doc:"Why the request was cancelled, machine readable: cancelled_by_requester, task_deleted, task_moved, workflow_removed, status_removed, transition_removed, approval_disabled, status_changed, target_archived, requirements_not_met, requester_lost_access, requester_not_in_team, requester_deleted, requester_disabled or no_approvers. Empty unless cancelled."`
	CancelMessage    string         `xorm:"text null" json:"cancel_message" readOnly:"true" doc:"A human readable text for cancel_reason, with the details of a failed re-check."`
	Created          time.Time      `xorm:"created not null" json:"created" readOnly:"true" doc:"When the request was made."`
	Updated          time.Time      `xorm:"updated not null" json:"updated" readOnly:"true" doc:"When the request was last changed."`
	Resolved         *time.Time     `xorm:"DATETIME null" json:"resolved" readOnly:"true" doc:"When the request was approved, rejected or cancelled."`

	ApproverIDs    []int64              `xorm:"-" json:"approver_ids,omitempty" writeOnly:"true" doc:"Ad-hoc requests only: the users who are asked. They need read access to the task; the requester cannot be one of them."`
	Approvers      []*ApprovalApprover  `xorm:"-" json:"approvers" readOnly:"true" doc:"The approvers with their decisions. Approvers who lost access to the project are removed."`
	RequestedBy    *user.User           `xorm:"-" json:"requested_by" readOnly:"true" doc:"Who asked."`
	ResolvedBy     *user.User           `xorm:"-" json:"resolved_by" readOnly:"true" doc:"Who resolved or cancelled the request, null when the system did."`
	Task           *ApprovalTaskSummary `xorm:"-" json:"task" readOnly:"true" doc:"The task in short."`
	FromStatus     *TaskStatusSummary   `xorm:"-" json:"from_status" readOnly:"true" doc:"The status the task has to be in."`
	ToStatus       *TaskStatusSummary   `xorm:"-" json:"to_status" readOnly:"true" doc:"The status the task enters when approved."`
	RejectedStatus *TaskStatusSummary   `xorm:"-" json:"rejected_status" readOnly:"true" doc:"The status the task enters when rejected."`

	// Filters of the list routes.
	FilterStatus    string `xorm:"-" json:"-"`
	FilterRole      string `xorm:"-" json:"-"`
	FilterProjectID int64  `xorm:"-" json:"-"`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name of approval requests.
func (*ApprovalRequest) TableName() string {
	return "approval_requests"
}

// ApprovalApprover is one person asked to decide, with the decision once they made it.
type ApprovalApprover struct {
	ID        int64                  `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this entry."`
	RequestID int64                  `xorm:"bigint not null INDEX unique(approval_approvers_pair)" json:"request_id" readOnly:"true" doc:"The request this approver belongs to."`
	UserID    int64                  `xorm:"bigint not null INDEX unique(approval_approvers_pair)" json:"user_id" readOnly:"true" doc:"The approver."`
	Decision  *ApprovalDecisionValue `xorm:"varchar(20) null" json:"decision" readOnly:"true" enum:"approved,rejected" doc:"What the approver decided, null while undecided."`
	Comment   string                 `xorm:"longtext null" json:"comment" readOnly:"true" doc:"The comment sent with the decision."`
	Decided   *time.Time             `xorm:"DATETIME null" json:"decided" readOnly:"true" doc:"When the approver decided."`

	User *user.User `xorm:"-" json:"user" readOnly:"true" doc:"The approver."`
}

// TableName returns the table name of approvers.
func (*ApprovalApprover) TableName() string {
	return "approval_approvers"
}

// isLinkShareAuth reports whether the auth is a link share, which has no access to approvals at all.
func isLinkShareAuth(a web.Auth) bool {
	_, is := a.(*LinkSharing)
	return is
}

// lockApprovalRequest takes the write lock of a request before anything is read, after the lock of its task.
// The order is task, then request, everywhere: a direct status change locks the task first and then looks at the
// pending requests (to cancel them), and a decision moves the task. Locking the request first deadlocked the two on
// PostgreSQL (40P01) and MySQL (1213). The task is found by a subquery, so the first statement is still a write:
// that keeps SQLite from failing a transaction that read and then writes after another writer committed
// (busy_timeout cannot fix that). On PostgreSQL and MySQL the writes are row locks that serialize decisions and
// cancellations of one request. The reads that follow have to be locking reads for the same reason, see
// lockingSession.
func lockApprovalRequest(s *xorm.Session, id int64) error {
	if _, err := s.Exec("UPDATE tasks SET id = id WHERE id IN (SELECT task_id FROM approval_requests WHERE id = ?)", id); err != nil {
		return err
	}
	_, err := s.Table("approval_requests").Where("id = ?", id).Update(map[string]any{"id": id})
	return err
}

// lockTaskForApproval serializes the creation of requests of one task.
func lockTaskForApproval(s *xorm.Session, taskID int64) error {
	_, err := s.Table("tasks").Where("id = ?", taskID).Update(map[string]any{"id": taskID})
	return err
}

// getApprovalRequest loads one request row. locked makes the read a locking read.
func getApprovalRequest(s *xorm.Session, id int64, locked bool) (*ApprovalRequest, error) {
	if locked {
		s = lockingSession(s)
	}
	req := &ApprovalRequest{}
	has, err := s.ID(id).Get(req)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrApprovalRequestDoesNotExist{ID: id}
	}
	return req, nil
}

func getApprovalApprovers(s *xorm.Session, requestID int64, locked bool) ([]*ApprovalApprover, error) {
	if locked {
		s = lockingSession(s)
	}
	rows := []*ApprovalApprover{}
	if err := s.Where("request_id = ?", requestID).OrderBy("id ASC").Find(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// hydrateApprovalRequests fills the approvers, users, task and status summaries of requests with a constant
// number of queries.
func hydrateApprovalRequests(s *xorm.Session, reqs []*ApprovalRequest) error {
	if len(reqs) == 0 {
		return nil
	}
	reqIDs := make([]int64, 0, len(reqs))
	taskIDs := []int64{}
	statusIDs := []int64{}
	userIDs := []int64{}
	byID := make(map[int64]*ApprovalRequest, len(reqs))
	for _, r := range reqs {
		reqIDs = append(reqIDs, r.ID)
		taskIDs = append(taskIDs, r.TaskID)
		userIDs = append(userIDs, r.RequestedByID, r.ResolvedByID)
		for _, id := range []*int64{r.FromStatusID, r.ToStatusID, r.RejectedStatusID} {
			if id != nil {
				statusIDs = append(statusIDs, *id)
			}
		}
		r.Approvers = []*ApprovalApprover{}
		byID[r.ID] = r
	}

	approvers := []*ApprovalApprover{}
	for _, chunk := range chunkInt64s(reqIDs) {
		rows := []*ApprovalApprover{}
		if err := s.Where(builder.In("request_id", chunk)).OrderBy("id ASC").Find(&rows); err != nil {
			return err
		}
		approvers = append(approvers, rows...)
	}
	for _, a := range approvers {
		userIDs = append(userIDs, a.UserID)
		byID[a.RequestID].Approvers = append(byID[a.RequestID].Approvers, a)
	}

	users, err := user.GetUsersByIDs(s, uniqueInts(userIDs))
	if err != nil {
		return err
	}
	for _, a := range approvers {
		a.User = users[a.UserID]
	}

	tasks := map[int64]*Task{}
	projectIDs := []int64{}
	for _, chunk := range chunkInt64s(uniqueInts(taskIDs)) {
		list := []*Task{}
		// Unscoped: the history of a request outlives a trashed task.
		if err := s.Unscoped().Where(builder.In("id", chunk)).Find(&list); err != nil {
			return err
		}
		for _, t := range list {
			tasks[t.ID] = t
			projectIDs = append(projectIDs, t.ProjectID)
		}
	}
	projects, err := GetProjectsMapByIDs(s, uniqueInts(projectIDs))
	if err != nil {
		return err
	}

	statuses := map[int64]*TaskStatusSummary{}
	if len(statusIDs) > 0 {
		list := []*WorkflowStatus{}
		if err := s.Where(builder.In("id", uniqueInts(statusIDs))).Find(&list); err != nil {
			return err
		}
		for _, st := range list {
			statuses[st.ID] = &TaskStatusSummary{ID: st.ID, Title: st.Title, Color: st.Color, Category: st.Category}
		}
	}
	summary := func(id *int64) *TaskStatusSummary {
		if id == nil {
			return nil
		}
		return statuses[*id]
	}

	for _, r := range reqs {
		r.RequestedBy = users[r.RequestedByID]
		r.ResolvedBy = users[r.ResolvedByID]
		r.FromStatus, r.ToStatus, r.RejectedStatus = summary(r.FromStatusID), summary(r.ToStatusID), summary(r.RejectedStatusID)
		if t, has := tasks[r.TaskID]; has {
			p := projects[t.ProjectID]
			t.setIdentifier(p)
			r.Task = &ApprovalTaskSummary{ID: t.ID, Title: t.Title, Identifier: t.GetFullIdentifier(), ProjectID: t.ProjectID}
			if p != nil {
				r.Task.ProjectTitle = p.Title
			}
		}
	}
	return nil
}

// hydrateApprovalRequest loads everything of a single request.
func hydrateApprovalRequest(s *xorm.Session, req *ApprovalRequest) error {
	return hydrateApprovalRequests(s, []*ApprovalRequest{req})
}

// usersWhoMayReadProject returns the given users who exist, are active and can read the project. A bot counts
// only while its owner is active too: a bot of a disabled owner cannot authenticate.
func usersWhoMayReadProject(s *xorm.Session, projectID int64, userIDs []int64) ([]*user.User, error) {
	return usersWhoMayReadProjectWhere(s, projectID, userIDs, false)
}

// usersWhoMayReadProjectWhere is usersWhoMayReadProject; keepLocked also accepts accounts that are locked out by
// failed logins. The lockout is temporary and anybody can cause it, so an approver who is only locked out keeps
// their slot (see dropApproversWithoutAccess). A disabled account, one that waits for its e-mail address to be
// confirmed (how admins freeze one) and a deleted one do not.
func usersWhoMayReadProjectWhere(s *xorm.Session, projectID int64, userIDs []int64, keepLocked bool) ([]*user.User, error) {
	userIDs = uniqueInts(userIDs)
	if len(userIDs) == 0 {
		return []*user.User{}, nil
	}
	users, err := user.GetUsersByIDs(s, userIDs)
	if err != nil {
		return nil, err
	}
	ownerIDs := []int64{}
	for _, u := range users {
		if u.IsBot() {
			ownerIDs = append(ownerIDs, u.BotOwnerID)
		}
	}
	owners, err := user.GetUsersByIDs(s, uniqueInts(ownerIDs))
	if err != nil {
		return nil, err
	}
	eligible := func(u *user.User) bool {
		return u.Status == user.StatusActive || (keepLocked && u.Status == user.StatusAccountLocked)
	}
	result := make([]*user.User, 0, len(userIDs))
	for _, id := range userIDs {
		u, has := users[id]
		if !has || !eligible(u) {
			continue
		}
		if u.IsBot() {
			if owner, ok := owners[u.BotOwnerID]; !ok || !eligible(owner) {
				continue
			}
		}
		can, _, err := (&Project{ID: projectID}).CanRead(s, &user.User{ID: id})
		if err != nil {
			if IsErrProjectDoesNotExist(err) {
				return result, nil
			}
			return nil, err
		}
		if can {
			result = append(result, u)
		}
	}
	return result, nil
}

// approvalCommentHTML turns a plain text decision comment into the HTML task comments are stored as.
func approvalCommentHTML(text string) string {
	escaped := html.EscapeString(strings.TrimSpace(text))
	paragraphs := strings.Split(strings.ReplaceAll(escaped, "\r\n", "\n"), "\n\n")
	for i, p := range paragraphs {
		paragraphs[i] = "<p>" + strings.ReplaceAll(strings.TrimSpace(p), "\n", "<br>") + "</p>"
	}
	return strings.Join(paragraphs, "")
}

// newApprovalRows creates the approver rows of a new request.
func newApprovalRows(requestID int64, approvers []*user.User) []*ApprovalApprover {
	rows := make([]*ApprovalApprover, 0, len(approvers))
	for _, u := range approvers {
		rows = append(rows, &ApprovalApprover{RequestID: requestID, UserID: u.ID, User: u})
	}
	return rows
}

// dispatchApprovalRequested stores nothing, it announces a request that was just written.
func dispatchApprovalRequested(s *xorm.Session, req *ApprovalRequest, task *Task, doer *user.User) error {
	if err := hydrateApprovalRequest(s, req); err != nil {
		return err
	}
	events.DispatchOnCommit(s, &ApprovalRequestedEvent{Approval: req, Task: task, Doer: doer})
	return nil
}

// ReadOne loads the request; CanRead loaded the row.
// @Summary Get an approval request
func (r *ApprovalRequest) ReadOne(s *xorm.Session, _ web.Auth) error {
	return hydrateApprovalRequest(s, r)
}

// ReadAll lists the requests of a task (TaskID set) or the requests the caller is part of: "my approvals".
// @Summary List approval requests
func (r *ApprovalRequest) ReadAll(s *xorm.Session, a web.Auth, _ string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	// DoReadAll skips the Can* checks.
	if isLinkShareAuth(a) {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	cond := builder.NewCond()
	switch r.FilterStatus {
	case "":
	case string(ApprovalStatusPending), string(ApprovalStatusApproved), string(ApprovalStatusRejected), string(ApprovalStatusCancelled):
		cond = cond.And(builder.Eq{"status": r.FilterStatus})
	default:
		return nil, 0, 0, ErrApprovalInvalid{Reason: "status must be pending, approved, rejected or cancelled"}
	}

	if r.TaskID != 0 {
		can, _, err := (&Task{ID: r.TaskID}).CanRead(s, a)
		if err != nil {
			return nil, 0, 0, err
		}
		if !can {
			return nil, 0, 0, ErrGenericForbidden{}
		}
		cond = cond.And(builder.Eq{"task_id": r.TaskID})
	} else {
		cond, err = r.myApprovalsCond(s, a, cond)
		if err != nil {
			return nil, 0, 0, err
		}
	}

	total, err := s.Where(cond).Count(&ApprovalRequest{})
	if err != nil {
		return nil, 0, 0, err
	}
	reqs := []*ApprovalRequest{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where(cond).OrderBy("id DESC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&reqs); err != nil {
		return nil, 0, 0, err
	}
	if err = hydrateApprovalRequests(s, reqs); err != nil {
		return nil, 0, 0, err
	}
	return reqs, len(reqs), total, nil
}

// myApprovalsCond restricts the list to requests the caller asked or has to decide on, whose task they can still
// read. The readable projects are resolved first, so pagination and totals stay exact.
func (r *ApprovalRequest) myApprovalsCond(s *xorm.Session, a web.Auth, cond builder.Cond) (builder.Cond, error) {
	uid := a.GetID()
	asApprover := builder.In("id", builder.Select("request_id").From("approval_approvers").Where(builder.Eq{"user_id": uid}))
	asRequester := builder.Eq{"requested_by_id": uid}
	var role builder.Cond
	switch r.FilterRole {
	case "":
		role = builder.Or(asApprover, asRequester)
	case "approver":
		role = asApprover
	case "requester":
		role = asRequester
	default:
		return nil, ErrApprovalInvalid{Reason: "role must be approver or requester"}
	}
	cond = cond.And(role)
	if r.FilterProjectID != 0 {
		cond = cond.And(builder.Eq{"project_id": r.FilterProjectID})
	}

	// The task decides who may read a request, not the project the request was made in: the task may have moved
	// since (which cancelled the request but keeps it in the lists). Trashed tasks are gone for the lists.
	requested := builder.Select("task_id").From("approval_requests").Where(cond)
	projectIDs := []int64{}
	err := s.Table("tasks").
		Where(builder.And(builder.IsNull{"deleted_at"}, builder.In("id", requested))).
		Distinct("project_id").
		Find(&projectIDs)
	if err != nil {
		return nil, err
	}
	readable := []int64{}
	perms, err := checkReadPermissionsForProjects(s, a, projectIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range projectIDs {
		if p, has := perms[id]; has && p.canRead {
			readable = append(readable, id)
		}
	}
	sort.Slice(readable, func(i, j int) bool { return readable[i] < readable[j] })
	if len(readable) == 0 {
		// An empty In list is dropped by the builder; this matches nothing.
		return cond.And(builder.Eq{"id": 0}), nil
	}
	return cond.And(builder.In("task_id", builder.Select("id").From("tasks").Where(builder.And(
		builder.IsNull{"deleted_at"},
		builder.In("project_id", readable),
	)))), nil
}

// Create makes an ad-hoc request: a recorded decision on a task that changes no status.
// @Summary Request an approval for a task
func (r *ApprovalRequest) Create(s *xorm.Session, a web.Auth) error {
	requester, err := user.GetUserByID(s, a.GetID())
	if err != nil {
		return err
	}
	task, err := GetTaskByIDSimple(s, r.TaskID)
	if err != nil {
		return err
	}

	title := strings.TrimSpace(r.Title)
	if title == "" || len([]rune(title)) > 250 {
		return ErrApprovalInvalid{Reason: "the title is required and may have at most 250 characters"}
	}
	mode := r.Mode
	if mode == "" {
		mode = ApprovalModeAny
	}
	if !mode.isValid() {
		return ErrApprovalInvalid{Reason: "mode must be any or all"}
	}
	ids := uniqueInts(r.ApproverIDs)
	if len(ids) == 0 || len(ids) > 50 {
		return ErrApprovalInvalid{Reason: "between 1 and 50 approvers are required"}
	}
	for _, id := range ids {
		if id == requester.ID {
			return ErrApprovalInvalid{Reason: "the requester cannot be an approver"}
		}
	}
	approvers, err := usersWhoMayReadProject(s, task.ProjectID, ids)
	if err != nil {
		return err
	}
	if len(approvers) != len(ids) {
		return ErrApprovalInvalid{Reason: "every approver must be an active user who can read the task"}
	}

	if err = lockTaskForApproval(s, task.ID); err != nil {
		return err
	}
	req := &ApprovalRequest{
		TaskID:        task.ID,
		ProjectID:     task.ProjectID,
		Title:         title,
		Description:   r.Description,
		Mode:          mode,
		Status:        ApprovalStatusPending,
		RequestedByID: requester.ID,
		Due:           r.Due,
	}
	if err = insertApprovalRequest(s, req, approvers); err != nil {
		return err
	}
	if err = dispatchApprovalRequested(s, req, &task, requester); err != nil {
		return err
	}
	*r = *req
	return nil
}

// insertApprovalRequest writes a new pending request with its approver rows.
func insertApprovalRequest(s *xorm.Session, req *ApprovalRequest, approvers []*user.User) error {
	req.ID = 0
	if _, err := s.Insert(req); err != nil {
		return err
	}
	rows := newApprovalRows(req.ID, approvers)
	for _, row := range rows {
		if _, err := s.Insert(row); err != nil {
			return err
		}
	}
	return nil
}
