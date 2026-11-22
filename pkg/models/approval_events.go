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

import "code.vikunja.io/api/pkg/user"

// ApprovalRequestedEvent represents a new approval request.
type ApprovalRequestedEvent struct {
	Approval *ApprovalRequest `json:"approval"`
	Task     *Task            `json:"task"`
	// Doer is the requester.
	Doer *user.User `json:"doer"`
}

// Name defines the name for ApprovalRequestedEvent
func (e *ApprovalRequestedEvent) Name() string {
	return "approval.requested"
}

// ApprovalDecidedEvent represents one approver deciding. The request may still be pending afterwards.
type ApprovalDecidedEvent struct {
	Approval *ApprovalRequest      `json:"approval"`
	Task     *Task                 `json:"task"`
	Decision ApprovalDecisionValue `json:"decision"`
	Comment  string                `json:"comment"`
	// Doer is the approver.
	Doer *user.User `json:"doer"`
}

// Name defines the name for ApprovalDecidedEvent
func (e *ApprovalDecidedEvent) Name() string {
	return "approval.decided"
}

// ApprovalResolvedEvent represents a request ending: approved, rejected or cancelled.
type ApprovalResolvedEvent struct {
	Approval *ApprovalRequest `json:"approval"`
	Task     *Task            `json:"task"`
	Outcome  ApprovalStatus   `json:"outcome"`
	// Doer is who ended it: the deciding approver, the canceller, or nil when the system did.
	Doer *user.User `json:"doer"`
}

// Name defines the name for ApprovalResolvedEvent
func (e *ApprovalResolvedEvent) Name() string {
	return "approval.resolved"
}
