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
	"code.vikunja.io/api/pkg/web"
	"fmt"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/user"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// Which internal status changes never create approvals: a change with SkipChecks (re-opening a repeating task into
// its initial status, the move after a rejection, assigning the first status), the approved transition itself
// (BypassApproval) and the move of tasks out of removed statuses. Changes that are a side effect of a task update
// (the done flag) fail with ErrApprovalRequired instead of creating a request.

// approvalRuleFor returns the transition rule when moving from one status to the other needs an approval.
func (d *workflowDef) approvalRuleFor(from, to *WorkflowStatus) *WorkflowTransition {
	if from == nil || to == nil {
		return nil
	}
	rule := d.ruleFor(from.ID, to.ID)
	if rule == nil || !rule.RequiresApproval {
		return nil
	}
	return rule
}

// resolveTransitionApprovers resolves the approver sources of a transition to the users who are asked now:
// de-duplicated in the order of the sources, active, able to read the task, and without the requester unless the
// transition allows self-approval.
func resolveTransitionApprovers(s *xorm.Session, task *Task, requesterID int64, cfg *TransitionApproval) ([]*user.User, error) {
	if cfg == nil {
		return []*user.User{}, nil
	}
	ids := []int64{}
	for _, src := range cfg.Approvers {
		switch src.Type {
		case ApproverSourceUser:
			ids = append(ids, src.ID)
		case ApproverSourceTeam:
			members, err := teamMemberIDs(s, src.ID)
			if err != nil {
				return nil, err
			}
			ids = append(ids, members...)
		case ApproverSourceAssignees:
			assignees := []int64{}
			if err := s.Table("task_assignees").Where("task_id = ?", task.ID).OrderBy("id ASC").Cols("user_id").Find(&assignees); err != nil {
				return nil, err
			}
			ids = append(ids, assignees...)
		case ApproverSourceCustomField:
			field := &CustomField{}
			has, err := s.Where(builder.And(builder.Eq{"id": src.ID}, builder.Eq{"project_id": task.ProjectID})).Get(field)
			if err != nil {
				return nil, err
			}
			if !has || field.Type != CustomFieldTypeUser {
				continue
			}
			rows, err := getCustomFieldValueRows(s, task.ID, []int64{field.ID})
			if err != nil {
				return nil, err
			}
			for _, row := range rows[field.ID] {
				if row.UserID != nil {
					ids = append(ids, *row.UserID)
				}
			}
		}
	}
	if !cfg.AllowSelf {
		kept := ids[:0]
		for _, id := range ids {
			if id != requesterID {
				kept = append(kept, id)
			}
		}
		ids = kept
	}
	return usersWhoMayReadProject(s, task.ProjectID, ids)
}

// requestTransitionApproval creates the pending request of a transition, or returns the one that already exists
// for the same task and target status. The other checks of the transition have run before.
func (d *workflowDef) requestTransitionApproval(s *xorm.Session, a web.Auth, task *Task, from, to *WorkflowStatus, rule *WorkflowTransition, comment string) (*ApprovalRequest, bool, error) {
	if isLinkShareAuth(a) {
		return nil, false, ErrApprovalLinkShare{}
	}
	requester, err := user.GetUserByID(s, a.GetID())
	if err != nil {
		return nil, false, err
	}

	// Serialize creations for the task, then look for a pending request with a locking read.
	if err = lockTaskForApproval(s, task.ID); err != nil {
		return nil, false, err
	}
	existing := &ApprovalRequest{}
	has, err := lockingSession(s).Where(builder.And(
		builder.Eq{"task_id": task.ID},
		builder.Eq{"to_status_id": to.ID},
		builder.Eq{"status": ApprovalStatusPending},
	)).Get(existing)
	if err != nil {
		return nil, false, err
	}
	if has {
		if err = hydrateApprovalRequest(s, existing); err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}

	cfg := rule.Approval
	approvers, err := resolveTransitionApprovers(s, task, requester.ID, cfg)
	if err != nil {
		return nil, false, err
	}
	if len(approvers) == 0 {
		return nil, false, ErrApprovalNoApprovers{TaskID: task.ID, ToStatusID: to.ID}
	}

	req := &ApprovalRequest{
		TaskID:        task.ID,
		ProjectID:     task.ProjectID,
		TransitionID:  &rule.ID,
		FromStatusID:  &from.ID,
		ToStatusID:    &to.ID,
		Title:         strings.TrimSpace(rule.Title),
		Comment:       comment,
		Mode:          cfg.Mode,
		Status:        ApprovalStatusPending,
		AllowSelf:     cfg.AllowSelf,
		RequestedByID: requester.ID,
	}
	if req.Title == "" {
		req.Title = fmt.Sprintf("%s → %s", from.Title, to.Title)
	}
	if !req.Mode.isValid() {
		req.Mode = ApprovalModeAny
	}
	if cfg.RejectedStatusID != 0 {
		id := cfg.RejectedStatusID
		req.RejectedStatusID = &id
	}
	if cfg.DueIn != "" {
		if offset, err := spec.ParseOffset(cfg.DueIn); err == nil && offset > 0 {
			due := time.Now().Add(offset).UTC()
			req.Due = &due
		}
	}
	if err = insertApprovalRequest(s, req, approvers); err != nil {
		return nil, false, err
	}
	if err = dispatchApprovalRequested(s, req, task, requester); err != nil {
		return nil, false, err
	}
	return req, true, nil
}

// executeApprovedTransition performs the transition of an approved request as the system on behalf of the
// requester: the approval requirement is bypassed, everything else is checked again at this moment. It returns a
// cancel reason and text, and no error, when the transition cannot happen any more.
func executeApprovedTransition(s *xorm.Session, req *ApprovalRequest, task *Task) (reason, message string, err error) {
	if task.ProjectID != req.ProjectID {
		return ApprovalCancelTaskMoved, "", nil
	}
	d, err := loadWorkflowDef(s, task.ProjectID)
	if err != nil {
		return "", "", err
	}
	if d == nil {
		return ApprovalCancelWorkflowRemoved, "", nil
	}
	to := d.statuses[*req.ToStatusID]
	switch {
	case to == nil:
		return ApprovalCancelStatusRemoved, "", nil
	case to.Archived:
		return ApprovalCancelTargetArchived, "", nil
	}
	if req.TransitionID != nil {
		found := false
		for _, tr := range d.workflow.Transitions {
			if tr.ID == *req.TransitionID {
				found = true
			}
		}
		if !found {
			return ApprovalCancelTransitionRemoved, "", nil
		}
	}
	current, err := getTaskStatusRow(s, task.ID)
	if err != nil {
		return "", "", err
	}
	if current == nil || req.FromStatusID == nil || current.StatusID != *req.FromStatusID {
		return ApprovalCancelStatusChanged, "", nil
	}

	requester, err := user.GetUserByID(s, req.RequestedByID)
	if err != nil {
		if user.IsErrUserDoesNotExist(err) {
			return ApprovalCancelRequesterDeleted, "", nil
		}
		if !user.IsErrUserStatusError(err) {
			return "", "", err
		}
	}
	if requester == nil {
		return ApprovalCancelRequesterNoAccess, "", nil
	}

	_, err = d.runStatusChange(s, requester, &statusChange{
		Task:              task,
		ToStatusID:        to.ID,
		Comment:           req.Comment,
		BypassApproval:    true,
		ApprovalRequestID: req.ID,
	})
	switch e := err.(type) {
	case nil:
		return "", "", nil
	case ErrWorkflowTransitionNotAllowed:
		return ApprovalCancelTransitionRemoved, "", nil
	case ErrWorkflowTransitionForbidden:
		if e.Reason == workflowForbiddenTeam {
			return ApprovalCancelRequesterNotInTeam, "", nil
		}
		return ApprovalCancelRequesterNoAccess, "", nil
	case ErrWorkflowRequirementsNotMet:
		parts := []string{}
		if len(e.MissingFields) > 0 {
			parts = append(parts, "missing fields: "+strings.Join(e.MissingFields, ", "))
		}
		if e.CommentRequired {
			parts = append(parts, "a comment is required")
		}
		return ApprovalCancelRequirementsNotMet, approvalCancelMessages[ApprovalCancelRequirementsNotMet] + " (" + strings.Join(parts, "; ") + ")", nil
	case ErrWorkflowStatusDoesNotExist:
		return ApprovalCancelStatusRemoved, "", nil
	case ErrWorkflowStatusArchived:
		return ApprovalCancelTargetArchived, "", nil
	}
	return "", "", err
}

// validateTransitionApproval checks and normalizes the approval part of a transition of a submitted document.
// docIDs are the status ids of the document, archived ones in archivedInDoc.
func validateTransitionApproval(s *xorm.Session, projectID int64, tr *WorkflowTransition, docIDs, archivedInDoc map[int64]bool) error {
	invalid := func(reason string) error {
		return ErrWorkflowInvalidDefinition{Reason: reason}
	}
	if !tr.RequiresApproval {
		tr.Approval = nil
		return nil
	}
	cfg := tr.Approval
	if cfg == nil {
		return invalid("a transition that requires approval needs an approval configuration")
	}
	if cfg.Mode == "" {
		cfg.Mode = ApprovalModeAny
	}
	if !cfg.Mode.isValid() {
		return invalid("approval mode must be any or all")
	}
	if len(cfg.Approvers) == 0 {
		return invalid("a transition that requires approval needs at least one approver source")
	}
	if len(cfg.Approvers) > 50 {
		return invalid("a transition may have at most 50 approver sources")
	}
	if cfg.DueIn != "" {
		offset, err := spec.ParseOffset(cfg.DueIn)
		if err != nil || offset <= 0 {
			return invalid("approval due_in must be a positive duration like 12h, 3d or 2w")
		}
	}
	if cfg.RejectedStatusID != 0 {
		if !docIDs[cfg.RejectedStatusID] {
			return invalid("the rejected status must be a status of this workflow")
		}
		if archivedInDoc[cfg.RejectedStatusID] {
			return invalid("the rejected status must not be archived")
		}
		if cfg.RejectedStatusID == tr.ToStatusID {
			return invalid("the rejected status must differ from the target status")
		}
	}

	seen := map[ApproverSource]bool{}
	sources := make([]ApproverSource, 0, len(cfg.Approvers))
	for _, src := range cfg.Approvers {
		switch src.Type {
		case ApproverSourceAssignees:
			src.ID = 0
		case ApproverSourceUser, ApproverSourceTeam, ApproverSourceCustomField:
			if src.ID <= 0 {
				return invalid("approver sources of type " + string(src.Type) + " need an id")
			}
		default:
			return invalid("unknown approver source type " + string(src.Type))
		}
		if seen[src] {
			continue
		}
		seen[src] = true

		switch src.Type {
		case ApproverSourceUser:
			users, err := usersWhoMayReadProject(s, projectID, []int64{src.ID})
			if err != nil {
				return err
			}
			if len(users) != 1 {
				return invalid(fmt.Sprintf("approver user %d does not exist or cannot read the project", src.ID))
			}
		case ApproverSourceTeam:
			ok, err := teamHasProjectAccess(s, src.ID, projectID)
			if err != nil {
				return err
			}
			if !ok {
				return invalid(fmt.Sprintf("approver team %d does not exist or has no access to the project", src.ID))
			}
		case ApproverSourceCustomField:
			field := &CustomField{}
			has, err := s.Where(builder.And(builder.Eq{"id": src.ID}, builder.Eq{"project_id": projectID})).Get(field)
			if err != nil {
				return err
			}
			if !has || field.Type != CustomFieldTypeUser {
				return invalid(fmt.Sprintf("approver custom field %d must be a user field of the project", src.ID))
			}
		}
		sources = append(sources, src)
	}
	cfg.Approvers = sources
	return nil
}

// teamHasProjectAccess reports whether the team exists and the project, or one of its parents, is shared with it.
func teamHasProjectAccess(s *xorm.Session, teamID, projectID int64) (bool, error) {
	exists, err := s.Where("id = ?", teamID).Exist(&Team{})
	if err != nil || !exists {
		return false, err
	}
	ancestors := builder.Select("ancestor_id").From("project_ancestors").Where(builder.Eq{"project_id": projectID})
	return s.Where(builder.And(builder.Eq{"team_id": teamID}, builder.In("project_id", ancestors))).Exist(&TeamProject{})
}

// remapTransitionApproval translates the ids of an approval configuration of a duplicated workflow.
func remapTransitionApproval(cfg *TransitionApproval, statusMap, customFieldMap map[int64]int64) *TransitionApproval {
	cp := cfg.clone()
	if cp == nil {
		return nil
	}
	if cp.RejectedStatusID != 0 {
		cp.RejectedStatusID = statusMap[cp.RejectedStatusID]
	}
	sources := make([]ApproverSource, 0, len(cp.Approvers))
	for _, src := range cp.Approvers {
		if src.Type == ApproverSourceCustomField {
			newID, mapped := customFieldMap[src.ID]
			if !mapped {
				continue
			}
			src.ID = newID
		}
		sources = append(sources, src)
	}
	cp.Approvers = sources
	return cp
}

// The automatic cancellations. Each is called where the thing it reacts to happens.

// cancelApprovalsAfterStatusChange cancels the pending transition requests of a task that just changed its
// status: they were made for the status it left.
func cancelApprovalsAfterStatusChange(s *xorm.Session, taskID int64) error {
	return cancelPendingApprovals(s, builder.And(builder.Eq{"task_id": taskID}, builder.NotNull{"transition_id"}),
		ApprovalCancelStatusChanged, "")
}

// cancelApprovalsForTask cancels every pending request of a task, ad-hoc ones included.
func cancelApprovalsForTask(s *xorm.Session, taskID int64, reason string) error {
	return cancelPendingApprovals(s, builder.Eq{"task_id": taskID}, reason, "")
}

// cancelApprovalsForStatuses cancels pending requests that involve removed or archived statuses.
func cancelApprovalsForStatuses(s *xorm.Session, statusIDs []int64) error {
	if len(statusIDs) == 0 {
		return nil
	}
	return cancelPendingApprovals(s, builder.Or(
		builder.In("from_status_id", statusIDs),
		builder.In("to_status_id", statusIDs),
	), ApprovalCancelStatusRemoved, "")
}

// cancelApprovalsForTransitions cancels pending requests of transitions that were removed or no longer need approval.
func cancelApprovalsForTransitions(s *xorm.Session, transitionIDs []int64, reason string) error {
	if len(transitionIDs) == 0 {
		return nil
	}
	return cancelPendingApprovals(s, builder.In("transition_id", transitionIDs), reason, "")
}

// cancelApprovalsForWorkflow cancels the pending transition requests of a project whose workflow is removed.
func cancelApprovalsForWorkflow(s *xorm.Session, projectID int64) error {
	return cancelPendingApprovals(s, builder.And(builder.Eq{"project_id": projectID}, builder.NotNull{"transition_id"}),
		ApprovalCancelWorkflowRemoved, "")
}
