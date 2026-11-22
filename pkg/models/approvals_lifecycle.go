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
	"xorm.io/builder"
	"xorm.io/xorm"
)

// deleteApprovalsOfCond deletes the requests that match the condition with their approvers.
func deleteApprovalsOfCond(s *xorm.Session, cond builder.Cond) error {
	ids := builder.Select("id").From("approval_requests").Where(cond)
	if _, err := s.Where(builder.In("request_id", ids)).Delete(&ApprovalApprover{}); err != nil {
		return err
	}
	_, err := s.Where(cond).Delete(&ApprovalRequest{})
	return err
}

// deleteApprovalsForTask removes the requests of a task that is purged.
func deleteApprovalsForTask(s *xorm.Session, taskID int64) error {
	return deleteApprovalsOfCond(s, builder.Eq{"task_id": taskID})
}

// deleteApprovalsForProject removes the requests of a project that is purged.
func deleteApprovalsForProject(s *xorm.Session, projectID int64) error {
	return deleteApprovalsOfCond(s, builder.Eq{"project_id": projectID})
}

// dropApproverFromPendingApprovals removes a user from the undecided approver slots of pending requests, in the
// given projects or everywhere when none are given, and looks at each request again: it may now be approved
// because everybody who is left approved, or cancelled because nobody is left.
func dropApproverFromPendingApprovals(s *xorm.Session, userID int64, projectIDs []int64, everywhere bool) error {
	pending := builder.Select("id").From("approval_requests").Where(builder.Eq{"status": ApprovalStatusPending})
	if !everywhere {
		if len(projectIDs) == 0 {
			return nil
		}
		pending = builder.Select("id").From("approval_requests").Where(builder.And(
			builder.Eq{"status": ApprovalStatusPending},
			builder.In("project_id", projectIDs),
		))
	}
	requestIDs := []int64{}
	err := s.Table("approval_approvers").
		Where(builder.And(builder.Eq{"user_id": userID}, builder.IsNull{"decision"}, builder.In("request_id", pending))).
		OrderBy("request_id ASC").
		Cols("request_id").
		Find(&requestIDs)
	if err != nil {
		return err
	}

	for _, id := range requestIDs {
		if err = lockApprovalRequest(s, id); err != nil {
			return err
		}
		req, err := getApprovalRequest(s, id, true)
		if err != nil {
			return err
		}
		if req.Status != ApprovalStatusPending {
			continue
		}
		if _, err = s.Where(builder.And(builder.Eq{"request_id": id}, builder.Eq{"user_id": userID}, builder.IsNull{"decision"})).
			Delete(&ApprovalApprover{}); err != nil {
			return err
		}
		if err = reevaluateApproval(s, req); err != nil {
			return err
		}
	}
	return nil
}

// removeUserFromApprovals is part of deleting a user: the requests they made are cancelled, they are dropped from
// the requests they were asked to decide, and their approver rows are removed.
func removeUserFromApprovals(s *xorm.Session, userID int64) error {
	if err := cancelPendingApprovals(s, builder.Eq{"requested_by_id": userID}, ApprovalCancelRequesterDeleted, ""); err != nil {
		return err
	}
	if err := dropApproverFromPendingApprovals(s, userID, nil, true); err != nil {
		return err
	}
	_, err := s.Where("user_id = ?", userID).Delete(&ApprovalApprover{})
	return err
}
