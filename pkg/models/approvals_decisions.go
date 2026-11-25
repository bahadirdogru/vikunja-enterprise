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
	"strings"
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// ApprovalDecision is one approver's decision on a request.
type ApprovalDecision struct {
	RequestID int64                 `json:"request_id" readOnly:"true" doc:"The request. Taken from the URL."`
	Decision  ApprovalDecisionValue `json:"decision" enum:"approved,rejected" doc:"approved or rejected."`
	Comment   string                `json:"comment" doc:"Why. Required when rejecting; it is added to the task as a comment."`
	Request   *ApprovalRequest      `json:"request" readOnly:"true" doc:"The request after the decision: still pending, or resolved by it."`

	req *ApprovalRequest

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

// ApprovalCancel cancels a pending request.
type ApprovalCancel struct {
	RequestID int64            `json:"request_id" readOnly:"true" doc:"The request. Taken from the URL."`
	Message   string           `json:"message" doc:"Optional text stored as cancel_message."`
	Request   *ApprovalRequest `json:"request" readOnly:"true" doc:"The cancelled request."`

	req *ApprovalRequest

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

// evaluateApproval decides what the decisions so far mean: "" while the request is still open, approved,
// rejected, or cancelled when nobody is left to decide.
func evaluateApproval(mode ApprovalMode, approvers []*ApprovalApprover) ApprovalStatus {
	if len(approvers) == 0 {
		return ApprovalStatusCancelled
	}
	if mode == ApprovalModeAny {
		var first *ApprovalApprover
		for _, ap := range approvers {
			if ap.Decision == nil || ap.Decided == nil {
				continue
			}
			if first == nil || ap.Decided.Before(*first.Decided) {
				first = ap
			}
		}
		if first == nil {
			return ""
		}
		if *first.Decision == ApprovalDecisionApproved {
			return ApprovalStatusApproved
		}
		return ApprovalStatusRejected
	}

	approved := 0
	for _, ap := range approvers {
		if ap.Decision == nil {
			continue
		}
		if *ap.Decision == ApprovalDecisionRejected {
			return ApprovalStatusRejected
		}
		approved++
	}
	if approved == len(approvers) {
		return ApprovalStatusApproved
	}
	return ""
}

// dropApproversWithoutAccess removes the undecided approvers who cannot read the project any more or whose account
// is disabled or gone, so their slot no longer counts. It returns the approvers that are left.
func dropApproversWithoutAccess(s *xorm.Session, req *ApprovalRequest) ([]*ApprovalApprover, error) {
	rows, err := getApprovalApprovers(s, req.ID, true)
	if err != nil {
		return nil, err
	}
	undecided := []int64{}
	for _, row := range rows {
		if row.Decision == nil {
			undecided = append(undecided, row.UserID)
		}
	}
	// Somebody who is only locked out by failed logins keeps the slot: otherwise ten bad logins would remove an
	// approver from an "all" request and let the others decide without them.
	allowed, err := usersWhoMayReadProjectWhere(s, req.ProjectID, undecided, true)
	if err != nil {
		return nil, err
	}
	allowedIDs := make(map[int64]bool, len(allowed))
	for _, u := range allowed {
		allowedIDs[u.ID] = true
	}

	remaining := make([]*ApprovalApprover, 0, len(rows))
	for _, row := range rows {
		if row.Decision == nil && !allowedIDs[row.UserID] {
			if _, err = s.ID(row.ID).Delete(&ApprovalApprover{}); err != nil {
				return nil, err
			}
			continue
		}
		remaining = append(remaining, row)
	}
	return remaining, nil
}

// Create records the decision of the caller and resolves the request when the decisions say so.
// @Summary Decide on an approval request
func (d *ApprovalDecision) Create(s *xorm.Session, a web.Auth) error {
	req := d.req
	if req.Status != ApprovalStatusPending {
		return ErrApprovalNotPending{ID: req.ID, Status: req.Status}
	}
	if d.Decision != ApprovalDecisionApproved && d.Decision != ApprovalDecisionRejected {
		return ErrApprovalInvalid{Reason: "decision must be approved or rejected"}
	}
	comment := strings.TrimSpace(d.Comment)
	if d.Decision == ApprovalDecisionRejected && comment == "" {
		return ErrApprovalCommentRequired{ID: req.ID}
	}

	approver, err := user.GetUserByID(s, a.GetID())
	if err != nil {
		return err
	}
	rows, err := dropApproversWithoutAccess(s, req)
	if err != nil {
		return err
	}
	var mine *ApprovalApprover
	for _, row := range rows {
		if row.UserID == approver.ID {
			mine = row
		}
	}
	switch {
	case mine == nil:
		return ErrApprovalNotApprover{ID: req.ID, UserID: approver.ID}
	case mine.Decision != nil:
		return ErrApprovalAlreadyDecided{ID: req.ID, UserID: approver.ID}
	case approver.ID == req.RequestedByID && !req.AllowSelf:
		return ErrApprovalSelfApproval{ID: req.ID}
	}

	now := time.Now().UTC()
	decision := d.Decision
	claimed, err := s.Where(builder.And(builder.Eq{"id": mine.ID}, builder.IsNull{"decision"})).
		Cols("decision", "comment", "decided").
		Update(&ApprovalApprover{Decision: &decision, Comment: comment, Decided: &now})
	if err != nil {
		return err
	}
	if claimed != 1 {
		return ErrApprovalAlreadyDecided{ID: req.ID, UserID: approver.ID}
	}
	mine.Decision, mine.Comment, mine.Decided = &decision, comment, &now

	task, err := GetTaskByIDSimple(s, req.TaskID)
	if err != nil {
		return err
	}

	outcome := evaluateApproval(req.Mode, rows)
	var resolvedAs ApprovalStatus
	if outcome != "" {
		if resolvedAs, err = resolveApproval(s, req, outcome, approver, comment); err != nil {
			return err
		}
	}

	if err = hydrateApprovalRequest(s, req); err != nil {
		return err
	}
	events.DispatchOnCommit(s, &ApprovalDecidedEvent{Approval: req, Task: &task, Decision: decision, Comment: comment, Doer: approver})
	if resolvedAs != "" {
		var doer *user.User
		if resolvedAs != ApprovalStatusCancelled {
			doer = approver
		}
		events.DispatchOnCommit(s, &ApprovalResolvedEvent{Approval: req, Task: &task, Outcome: resolvedAs, Doer: doer})
	}
	d.Request = req
	return nil
}

// resolveApproval claims a pending request for the outcome and runs what the outcome means. The claim is a
// conditional update on status = pending: one transaction wins, every other one gets ErrApprovalNotPending. It
// returns the final state, which is cancelled when an approved transition no longer holds. It does not
// dispatch the resolved event, the caller does once the request is hydrated.
func resolveApproval(s *xorm.Session, req *ApprovalRequest, outcome ApprovalStatus, resolver *user.User, comment string) (ApprovalStatus, error) {
	if outcome == ApprovalStatusCancelled {
		reason := ApprovalCancelNoApprovers
		if err := claimCancellation(s, req, reason, approvalCancelMessages[reason], resolver); err != nil {
			return "", err
		}
		return ApprovalStatusCancelled, nil
	}

	now := time.Now().UTC()
	var resolverID int64
	if resolver != nil {
		resolverID = resolver.ID
	}
	claimed, err := s.Where(builder.And(builder.Eq{"id": req.ID}, builder.Eq{"status": ApprovalStatusPending})).
		Cols("status", "resolved", "resolved_by_id").
		Update(&ApprovalRequest{Status: outcome, Resolved: &now, ResolvedByID: resolverID})
	if err != nil {
		return "", err
	}
	if claimed != 1 {
		return "", ErrApprovalNotPending{ID: req.ID, Status: ApprovalStatus("resolved")}
	}
	req.Status, req.Resolved, req.ResolvedByID = outcome, &now, resolverID

	task, err := GetTaskByIDSimple(s, req.TaskID)
	if err != nil {
		return "", err
	}

	switch outcome {
	case ApprovalStatusApproved:
		if req.ToStatusID == nil {
			return outcome, nil
		}
		reason, message, err := executeApprovedTransition(s, req, &task)
		if err != nil {
			return "", err
		}
		if reason != "" {
			// The decision stands in the approvers' rows, the transition did not happen.
			if err = turnApprovedIntoCancelled(s, req, reason, message); err != nil {
				return "", err
			}
			return ApprovalStatusCancelled, nil
		}
	case ApprovalStatusRejected:
		if err = applyRejection(s, req, &task, resolver, comment); err != nil {
			return "", err
		}
	case ApprovalStatusPending, ApprovalStatusCancelled:
		// Nothing happens to the task.
	}
	return outcome, nil
}

// applyRejection moves the task to the rejected status when the transition names one and adds the reason to
// the task as a comment of the rejecting approver.
func applyRejection(s *xorm.Session, req *ApprovalRequest, task *Task, rejecter *user.User, comment string) error {
	if rejecter == nil {
		// Resolved without a deciding person: take the approver who rejected.
		rows, err := getApprovalApprovers(s, req.ID, false)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Decision != nil && *row.Decision == ApprovalDecisionRejected {
				if rejecter, err = user.GetUserByID(s, row.UserID); err != nil {
					return err
				}
				comment = row.Comment
				break
			}
		}
		if rejecter == nil {
			return nil
		}
	}

	if req.ToStatusID != nil && req.RejectedStatusID != nil && req.FromStatusID != nil {
		d, err := loadWorkflowDef(s, task.ProjectID)
		if err != nil {
			return err
		}
		if d != nil && task.ProjectID == req.ProjectID {
			target := d.statuses[*req.RejectedStatusID]
			current, err := getTaskStatusRow(s, task.ID)
			if err != nil {
				return err
			}
			if target != nil && !target.Archived && current != nil && current.StatusID == *req.FromStatusID {
				// A system transition: the rules of the pair do not apply, the history names the rejecting
				// approver and the request.
				if _, err = d.runStatusChange(s, rejecter, &statusChange{
					Task:              task,
					ToStatusID:        target.ID,
					Comment:           comment,
					SkipChecks:        true,
					ApprovalRequestID: req.ID,
				}); err != nil {
					return err
				}
			}
		}
	}

	if strings.TrimSpace(comment) == "" {
		return nil
	}
	// Not TaskComment.Create: approvers may only be able to read the task.
	taskComment := &TaskComment{TaskID: task.ID, Comment: approvalCommentHTML(comment), AuthorID: rejecter.ID}
	if _, err := s.Insert(taskComment); err != nil {
		return err
	}
	taskComment.Author = rejecter
	events.DispatchOnCommit(s, &TaskCommentCreatedEvent{Task: task, Comment: taskComment, Doer: rejecter})
	return nil
}

// claimCancellation turns a pending request into cancelled. The conditional update is the claim: when another
// transaction resolved or cancelled the request first, this one gets ErrApprovalNotPending.
func claimCancellation(s *xorm.Session, req *ApprovalRequest, reason, message string, by *user.User) error {
	now := time.Now().UTC()
	var byID int64
	if by != nil {
		byID = by.ID
	}
	claimed, err := s.Where(builder.And(builder.Eq{"id": req.ID}, builder.Eq{"status": ApprovalStatusPending})).
		Cols("status", "resolved", "resolved_by_id", "cancel_reason", "cancel_message").
		Update(&ApprovalRequest{Status: ApprovalStatusCancelled, Resolved: &now, ResolvedByID: byID, CancelReason: reason, CancelMessage: message})
	if err != nil {
		return err
	}
	if claimed != 1 {
		return ErrApprovalNotPending{ID: req.ID, Status: ApprovalStatus("resolved")}
	}
	req.Status, req.Resolved, req.ResolvedByID, req.CancelReason, req.CancelMessage = ApprovalStatusCancelled, &now, byID, reason, message
	return nil
}

// turnApprovedIntoCancelled records that an approved transition could not be performed.
func turnApprovedIntoCancelled(s *xorm.Session, req *ApprovalRequest, reason, message string) error {
	if message == "" {
		message = approvalCancelMessages[reason]
	}
	claimed, err := s.Where(builder.And(builder.Eq{"id": req.ID}, builder.Eq{"status": ApprovalStatusApproved})).
		Cols("status", "cancel_reason", "cancel_message").
		Update(&ApprovalRequest{Status: ApprovalStatusCancelled, CancelReason: reason, CancelMessage: message})
	if err != nil {
		return err
	}
	if claimed != 1 {
		return ErrApprovalNotPending{ID: req.ID, Status: ApprovalStatusApproved}
	}
	req.Status, req.CancelReason, req.CancelMessage = ApprovalStatusCancelled, reason, message
	return nil
}

// Create cancels the request.
// @Summary Cancel an approval request
func (c *ApprovalCancel) Create(s *xorm.Session, a web.Auth) error {
	req := c.req
	if req.Status != ApprovalStatusPending {
		return ErrApprovalNotPending{ID: req.ID, Status: req.Status}
	}
	by, err := user.GetUserByID(s, a.GetID())
	if err != nil {
		return err
	}
	message := strings.TrimSpace(c.Message)
	if message == "" {
		message = approvalCancelMessages[ApprovalCancelByRequester]
	}
	if err = cancelApprovalRequest(s, req, ApprovalCancelByRequester, message, by); err != nil {
		return err
	}
	if err = hydrateApprovalRequest(s, req); err != nil {
		return err
	}
	c.Request = req
	return nil
}

// cancelApprovalRequest cancels one pending request and announces it.
func cancelApprovalRequest(s *xorm.Session, req *ApprovalRequest, reason, message string, by *user.User) error {
	if message == "" {
		message = approvalCancelMessages[reason]
	}
	if err := claimCancellation(s, req, reason, message, by); err != nil {
		return err
	}
	return announceCancellation(s, req, by)
}

func announceCancellation(s *xorm.Session, req *ApprovalRequest, by *user.User) error {
	if err := hydrateApprovalRequest(s, req); err != nil {
		return err
	}
	var task *Task
	if t, err := GetTaskByIDSimple(s, req.TaskID); err == nil {
		task = &t
	} else if !IsErrTaskDoesNotExist(err) {
		return err
	}
	events.DispatchOnCommit(s, &ApprovalResolvedEvent{Approval: req, Task: task, Outcome: ApprovalStatusCancelled, Doer: by})
	return nil
}

// cancelPendingApprovals cancels every pending request that matches the condition. The requests are found without
// a lock and then taken one by one in the order of their tasks (lockApprovalRequest: the task, then the request,
// like every other path), so a decision on one of them that holds its task and waits for the request cannot meet
// this one that holds the request and waits for the task. A request somebody resolved first is skipped.
func cancelPendingApprovals(s *xorm.Session, cond builder.Cond, reason string) error {
	candidates := []*ApprovalRequest{}
	if err := s.Cols("id", "task_id").Where(builder.And(cond, builder.Eq{"status": ApprovalStatusPending})).OrderBy("task_id ASC, id ASC").Find(&candidates); err != nil {
		return err
	}
	for _, c := range candidates {
		if err := lockApprovalRequest(s, c.ID); err != nil {
			return err
		}
		req, err := getApprovalRequest(s, c.ID, true)
		if IsErrApprovalRequestDoesNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if req.Status != ApprovalStatusPending {
			continue
		}
		err = cancelApprovalRequest(s, req, reason, "", nil)
		if IsErrApprovalNotPending(err) {
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// reevaluateApproval looks at a pending request again after its approvers changed: it resolves when the
// remaining decisions say so and cancels when nobody is left.
func reevaluateApproval(s *xorm.Session, req *ApprovalRequest) error {
	rows, err := getApprovalApprovers(s, req.ID, true)
	if err != nil {
		return err
	}
	outcome := evaluateApproval(req.Mode, rows)
	if outcome == "" {
		return nil
	}
	task, taskErr := GetTaskByIDSimple(s, req.TaskID)
	if taskErr != nil && !IsErrTaskDoesNotExist(taskErr) {
		return taskErr
	}
	final, err := resolveApproval(s, req, outcome, nil, "")
	if IsErrApprovalNotPending(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = hydrateApprovalRequest(s, req); err != nil {
		return err
	}
	var taskRef *Task
	if taskErr == nil {
		taskRef = &task
	}
	events.DispatchOnCommit(s, &ApprovalResolvedEvent{Approval: req, Task: taskRef, Outcome: final})
	return nil
}
