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
	"errors"
	"fmt"
	"net/http"

	"code.vikunja.io/api/pkg/web"
)

// ErrApprovalRequestDoesNotExist represents an error where an approval request does not exist.
type ErrApprovalRequestDoesNotExist struct {
	ID int64
}

// IsErrApprovalRequestDoesNotExist checks if an error is ErrApprovalRequestDoesNotExist.
func IsErrApprovalRequestDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrApprovalRequestDoesNotExist](err)
	return ok
}

func (err ErrApprovalRequestDoesNotExist) Error() string {
	return fmt.Sprintf("Approval request does not exist [ID: %d]", err.ID)
}

// ErrCodeApprovalRequestDoesNotExist holds the unique world-error code of this error
const ErrCodeApprovalRequestDoesNotExist = 24001

// HTTPError holds the http error description
func (err ErrApprovalRequestDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeApprovalRequestDoesNotExist,
		Message:  "This approval request does not exist.",
	}
}

// ErrApprovalNotPending represents an error where a request that is already resolved should be decided or cancelled.
type ErrApprovalNotPending struct {
	ID     int64
	Status ApprovalStatus
}

// IsErrApprovalNotPending checks if an error is ErrApprovalNotPending.
func IsErrApprovalNotPending(err error) bool {
	_, ok := errors.AsType[ErrApprovalNotPending](err)
	return ok
}

func (err ErrApprovalNotPending) Error() string {
	return fmt.Sprintf("Approval request is not pending [ID: %d, Status: %s]", err.ID, err.Status)
}

// ErrCodeApprovalNotPending holds the unique world-error code of this error
const ErrCodeApprovalNotPending = 24002

// HTTPError holds the http error description
func (err ErrApprovalNotPending) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusConflict,
		Code:     ErrCodeApprovalNotPending,
		Message:  "This approval request is already resolved.",
		Details: map[string]any{
			"request_id": err.ID,
			"status":     err.Status,
		},
	}
}

// ErrApprovalNotApprover represents an error where a user who is not (or no longer) an approver tries to decide.
type ErrApprovalNotApprover struct {
	ID     int64
	UserID int64
}

// IsErrApprovalNotApprover checks if an error is ErrApprovalNotApprover.
func IsErrApprovalNotApprover(err error) bool {
	_, ok := errors.AsType[ErrApprovalNotApprover](err)
	return ok
}

func (err ErrApprovalNotApprover) Error() string {
	return fmt.Sprintf("User is not an approver of the request [ID: %d, UserID: %d]", err.ID, err.UserID)
}

// ErrCodeApprovalNotApprover holds the unique world-error code of this error
const ErrCodeApprovalNotApprover = 24003

// HTTPError holds the http error description
func (err ErrApprovalNotApprover) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusForbidden,
		Code:     ErrCodeApprovalNotApprover,
		Message:  "You are not an approver of this request.",
	}
}

// ErrApprovalAlreadyDecided represents an error where an approver decides a second time.
type ErrApprovalAlreadyDecided struct {
	ID     int64
	UserID int64
}

// IsErrApprovalAlreadyDecided checks if an error is ErrApprovalAlreadyDecided.
func IsErrApprovalAlreadyDecided(err error) bool {
	_, ok := errors.AsType[ErrApprovalAlreadyDecided](err)
	return ok
}

func (err ErrApprovalAlreadyDecided) Error() string {
	return fmt.Sprintf("Approver has already decided [ID: %d, UserID: %d]", err.ID, err.UserID)
}

// ErrCodeApprovalAlreadyDecided holds the unique world-error code of this error
const ErrCodeApprovalAlreadyDecided = 24004

// HTTPError holds the http error description
func (err ErrApprovalAlreadyDecided) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusConflict,
		Code:     ErrCodeApprovalAlreadyDecided,
		Message:  "You have already decided on this request.",
	}
}

// ErrApprovalCommentRequired represents an error where a rejection comes without a comment.
type ErrApprovalCommentRequired struct {
	ID int64
}

// IsErrApprovalCommentRequired checks if an error is ErrApprovalCommentRequired.
func IsErrApprovalCommentRequired(err error) bool {
	_, ok := errors.AsType[ErrApprovalCommentRequired](err)
	return ok
}

func (err ErrApprovalCommentRequired) Error() string {
	return fmt.Sprintf("A rejection needs a comment [ID: %d]", err.ID)
}

// ErrCodeApprovalCommentRequired holds the unique world-error code of this error
const ErrCodeApprovalCommentRequired = 24005

// HTTPError holds the http error description
func (err ErrApprovalCommentRequired) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusUnprocessableEntity,
		Code:     ErrCodeApprovalCommentRequired,
		Message:  "A rejection needs a comment that says why.",
	}
}

// ErrApprovalNoApprovers represents an error where nobody could be asked: the approver sources of the transition
// resolve to no user who may read the task, or the requester is the only one and may not approve their own request.
type ErrApprovalNoApprovers struct {
	TaskID     int64
	ToStatusID int64
}

// IsErrApprovalNoApprovers checks if an error is ErrApprovalNoApprovers.
func IsErrApprovalNoApprovers(err error) bool {
	_, ok := errors.AsType[ErrApprovalNoApprovers](err)
	return ok
}

func (err ErrApprovalNoApprovers) Error() string {
	return fmt.Sprintf("No approvers could be resolved [TaskID: %d, ToStatusID: %d]", err.TaskID, err.ToStatusID)
}

// ErrCodeApprovalNoApprovers holds the unique world-error code of this error
const ErrCodeApprovalNoApprovers = 24006

// HTTPError holds the http error description
func (err ErrApprovalNoApprovers) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusUnprocessableEntity,
		Code:     ErrCodeApprovalNoApprovers,
		Message:  "Nobody can approve this: the approvers of the transition resolve to no one who may read the task. Assign someone, fill the approver field or ask a project admin to change the transition.",
		Details: map[string]any{
			"task_id":      err.TaskID,
			"to_status_id": err.ToStatusID,
		},
	}
}

// ErrApprovalRequired represents an error where a task update asked for a status change that needs an approval.
// Nothing was written, no request was created: the client has to use POST /tasks/{task}/status.
type ErrApprovalRequired struct {
	TaskID       int64
	FromStatusID int64
	ToStatusID   int64
}

// IsErrApprovalRequired checks if an error is ErrApprovalRequired.
func IsErrApprovalRequired(err error) bool {
	_, ok := errors.AsType[ErrApprovalRequired](err)
	return ok
}

func (err ErrApprovalRequired) Error() string {
	return fmt.Sprintf("The status change needs an approval [TaskID: %d, From: %d, To: %d]", err.TaskID, err.FromStatusID, err.ToStatusID)
}

// ErrCodeApprovalRequired holds the unique world-error code of this error
const ErrCodeApprovalRequired = 24007

// HTTPError holds the http error description
func (err ErrApprovalRequired) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusConflict,
		Code:     ErrCodeApprovalRequired,
		Message:  "Moving the task to this status needs an approval. Nothing was changed. Request it with POST /tasks/{task}/status.",
		Details: map[string]any{
			"task_id":        err.TaskID,
			"from_status_id": err.FromStatusID,
			"to_status_id":   err.ToStatusID,
		},
	}
}

// ErrApprovalSelfApproval represents an error where the requester tries to approve their own request.
type ErrApprovalSelfApproval struct {
	ID int64
}

// IsErrApprovalSelfApproval checks if an error is ErrApprovalSelfApproval.
func IsErrApprovalSelfApproval(err error) bool {
	_, ok := errors.AsType[ErrApprovalSelfApproval](err)
	return ok
}

func (err ErrApprovalSelfApproval) Error() string {
	return fmt.Sprintf("The requester cannot decide their own request [ID: %d]", err.ID)
}

// ErrCodeApprovalSelfApproval holds the unique world-error code of this error
const ErrCodeApprovalSelfApproval = 24008

// HTTPError holds the http error description
func (err ErrApprovalSelfApproval) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusForbidden,
		Code:     ErrCodeApprovalSelfApproval,
		Message:  "You cannot decide on your own request.",
	}
}

// ErrApprovalInvalid represents an error where an ad-hoc approval request is not valid.
type ErrApprovalInvalid struct {
	Reason string
}

// IsErrApprovalInvalid checks if an error is ErrApprovalInvalid.
func IsErrApprovalInvalid(err error) bool {
	_, ok := errors.AsType[ErrApprovalInvalid](err)
	return ok
}

func (err ErrApprovalInvalid) Error() string {
	return fmt.Sprintf("Approval request is invalid: %s", err.Reason)
}

// ErrCodeApprovalInvalid holds the unique world-error code of this error
const ErrCodeApprovalInvalid = 24009

// HTTPError holds the http error description
func (err ErrApprovalInvalid) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeApprovalInvalid,
		Message:  "The approval request is invalid: " + err.Reason,
	}
}

// ErrApprovalLinkShare represents an error where a link share tries to start a status change that needs an approval.
type ErrApprovalLinkShare struct{}

// IsErrApprovalLinkShare checks if an error is ErrApprovalLinkShare.
func IsErrApprovalLinkShare(err error) bool {
	_, ok := errors.AsType[ErrApprovalLinkShare](err)
	return ok
}

func (err ErrApprovalLinkShare) Error() string {
	return "Link shares cannot request approvals"
}

// ErrCodeApprovalLinkShare holds the unique world-error code of this error
const ErrCodeApprovalLinkShare = 24010

// HTTPError holds the http error description
func (err ErrApprovalLinkShare) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusForbidden,
		Code:     ErrCodeApprovalLinkShare,
		Message:  "This status change needs an approval, which link shares cannot request.",
	}
}
