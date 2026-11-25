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
	"strconv"
	"strings"

	"code.vikunja.io/api/pkg/web"
)

// ErrWorkflowDoesNotExist represents an error where a project has no workflow.
type ErrWorkflowDoesNotExist struct {
	ProjectID int64
}

// IsErrWorkflowDoesNotExist checks if an error is ErrWorkflowDoesNotExist.
func IsErrWorkflowDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrWorkflowDoesNotExist](err)
	return ok
}

func (err ErrWorkflowDoesNotExist) Error() string {
	return fmt.Sprintf("Workflow does not exist [ProjectID: %d]", err.ProjectID)
}

// ErrCodeWorkflowDoesNotExist holds the unique world-error code of this error
const ErrCodeWorkflowDoesNotExist = 21001

// HTTPError holds the http error description
func (err ErrWorkflowDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeWorkflowDoesNotExist,
		Message:  "This project has no workflow.",
	}
}

// ErrWorkflowInvalidDefinition represents an error where a workflow document is not valid.
type ErrWorkflowInvalidDefinition struct {
	Reason string
}

// IsErrWorkflowInvalidDefinition checks if an error is ErrWorkflowInvalidDefinition.
func IsErrWorkflowInvalidDefinition(err error) bool {
	_, ok := errors.AsType[ErrWorkflowInvalidDefinition](err)
	return ok
}

func (err ErrWorkflowInvalidDefinition) Error() string {
	return fmt.Sprintf("Workflow definition is invalid: %s", err.Reason)
}

// ErrCodeWorkflowInvalidDefinition holds the unique world-error code of this error
const ErrCodeWorkflowInvalidDefinition = 21002

// HTTPError holds the http error description
func (err ErrWorkflowInvalidDefinition) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeWorkflowInvalidDefinition,
		Message:  "The workflow definition is invalid: " + err.Reason,
	}
}

// ErrWorkflowStatusDoesNotExist represents an error where a status is not part of the workflow of the project.
type ErrWorkflowStatusDoesNotExist struct {
	StatusID int64
}

// IsErrWorkflowStatusDoesNotExist checks if an error is ErrWorkflowStatusDoesNotExist.
func IsErrWorkflowStatusDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrWorkflowStatusDoesNotExist](err)
	return ok
}

func (err ErrWorkflowStatusDoesNotExist) Error() string {
	return fmt.Sprintf("Workflow status does not exist [StatusID: %d]", err.StatusID)
}

// ErrCodeWorkflowStatusDoesNotExist holds the unique world-error code of this error
const ErrCodeWorkflowStatusDoesNotExist = 21003

// HTTPError holds the http error description
func (err ErrWorkflowStatusDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeWorkflowStatusDoesNotExist,
		Message:  "This status does not exist in the workflow of the project.",
	}
}

// ErrWorkflowStatusArchived represents an error where a task should move into an archived status.
type ErrWorkflowStatusArchived struct {
	StatusID int64
}

// IsErrWorkflowStatusArchived checks if an error is ErrWorkflowStatusArchived.
func IsErrWorkflowStatusArchived(err error) bool {
	_, ok := errors.AsType[ErrWorkflowStatusArchived](err)
	return ok
}

func (err ErrWorkflowStatusArchived) Error() string {
	return fmt.Sprintf("Workflow status is archived [StatusID: %d]", err.StatusID)
}

// ErrCodeWorkflowStatusArchived holds the unique world-error code of this error
const ErrCodeWorkflowStatusArchived = 21004

// HTTPError holds the http error description
func (err ErrWorkflowStatusArchived) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeWorkflowStatusArchived,
		Message:  "Tasks cannot be moved into an archived status.",
	}
}

// ErrWorkflowTransitionNotAllowed represents an error where the workflow does not list the requested change.
type ErrWorkflowTransitionNotAllowed struct {
	FromStatusID int64
	ToStatusID   int64
}

// IsErrWorkflowTransitionNotAllowed checks if an error is ErrWorkflowTransitionNotAllowed.
func IsErrWorkflowTransitionNotAllowed(err error) bool {
	_, ok := errors.AsType[ErrWorkflowTransitionNotAllowed](err)
	return ok
}

func (err ErrWorkflowTransitionNotAllowed) Error() string {
	return fmt.Sprintf("Workflow transition is not allowed [From: %d, To: %d]", err.FromStatusID, err.ToStatusID)
}

// ErrCodeWorkflowTransitionNotAllowed holds the unique world-error code of this error
const ErrCodeWorkflowTransitionNotAllowed = 21005

// HTTPError holds the http error description
func (err ErrWorkflowTransitionNotAllowed) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeWorkflowTransitionNotAllowed,
		Message:  "The workflow does not allow this status change.",
	}
}

// ErrWorkflowTransitionForbidden represents an error where the user may not perform the transition.
type ErrWorkflowTransitionForbidden struct {
	FromStatusID int64
	ToStatusID   int64
	// Reason is "permission" when the project permission is too low and "team" when the user is not in an allowed team.
	Reason string
}

// IsErrWorkflowTransitionForbidden checks if an error is ErrWorkflowTransitionForbidden.
func IsErrWorkflowTransitionForbidden(err error) bool {
	_, ok := errors.AsType[ErrWorkflowTransitionForbidden](err)
	return ok
}

func (err ErrWorkflowTransitionForbidden) Error() string {
	return fmt.Sprintf("Workflow transition is forbidden [From: %d, To: %d, Reason: %s]", err.FromStatusID, err.ToStatusID, err.Reason)
}

// ErrCodeWorkflowTransitionForbidden holds the unique world-error code of this error
const ErrCodeWorkflowTransitionForbidden = 21006

// HTTPError holds the http error description
func (err ErrWorkflowTransitionForbidden) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusForbidden,
		Code:     ErrCodeWorkflowTransitionForbidden,
		Message:  "You are not allowed to perform this status change.",
		Details: map[string]any{
			"reason": err.Reason,
		},
	}
}

// WorkflowRequirementsDetails is the machine-readable body of ErrWorkflowRequirementsNotMet.
type WorkflowRequirementsDetails struct {
	TaskID          int64    `json:"task_id" doc:"The task the change was attempted on."`
	FromStatusID    int64    `json:"from_status_id" doc:"The current status of the task, 0 when it has none."`
	ToStatusID      int64    `json:"to_status_id" doc:"The status the task should move to."`
	MissingFields   []string `json:"missing_fields" doc:"Identifiers of the fields that must be set first: assignees, due_date, start_date, end_date, description, attachments, labels or cf_<id> for a custom field."`
	CommentRequired bool     `json:"comment_required" doc:"True when the transition needs a non-empty comment."`
}

// ErrWorkflowRequirementsNotMet represents an error where a transition needs fields or a comment that are missing.
type ErrWorkflowRequirementsNotMet struct {
	WorkflowRequirementsDetails
}

// IsErrWorkflowRequirementsNotMet checks if an error is ErrWorkflowRequirementsNotMet.
func IsErrWorkflowRequirementsNotMet(err error) bool {
	_, ok := errors.AsType[ErrWorkflowRequirementsNotMet](err)
	return ok
}

func (err ErrWorkflowRequirementsNotMet) Error() string {
	return fmt.Sprintf(
		"Workflow requirements not met [TaskID: %d, To: %d, MissingFields: %s, CommentRequired: %t]",
		err.TaskID, err.ToStatusID, strings.Join(err.MissingFields, ","), err.CommentRequired,
	)
}

// ErrCodeWorkflowRequirementsNotMet holds the unique world-error code of this error
const ErrCodeWorkflowRequirementsNotMet = 21007

// HTTPError holds the http error description
func (err ErrWorkflowRequirementsNotMet) HTTPError() web.HTTPError {
	details := err.WorkflowRequirementsDetails
	if details.MissingFields == nil {
		details.MissingFields = []string{}
	}
	return web.HTTPError{
		HTTPCode: http.StatusUnprocessableEntity,
		Code:     ErrCodeWorkflowRequirementsNotMet,
		Message:  "The task does not meet the requirements of this status change.",
		Details:  details,
	}
}

// ErrWorkflowMoveTasksRequired represents an error where removed statuses still have tasks and no target was given.
type ErrWorkflowMoveTasksRequired struct {
	StatusIDs []int64
}

// IsErrWorkflowMoveTasksRequired checks if an error is ErrWorkflowMoveTasksRequired.
func IsErrWorkflowMoveTasksRequired(err error) bool {
	_, ok := errors.AsType[ErrWorkflowMoveTasksRequired](err)
	return ok
}

func (err ErrWorkflowMoveTasksRequired) Error() string {
	return fmt.Sprintf("Removed workflow statuses still have tasks, move_tasks_to is required [StatusIDs: %v]", err.StatusIDs)
}

// ErrCodeWorkflowMoveTasksRequired holds the unique world-error code of this error
const ErrCodeWorkflowMoveTasksRequired = 21008

// HTTPError holds the http error description
func (err ErrWorkflowMoveTasksRequired) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusConflict,
		Code:     ErrCodeWorkflowMoveTasksRequired,
		Message:  "Some removed statuses still have tasks. Set move_tasks_to to a status that stays in the workflow.",
		Details: map[string]any{
			"status_ids": err.StatusIDs,
		},
	}
}

// ErrWorkflowRequiredForStatusView represents an error where a kanban view should use workflow statuses as
// columns but the project has no workflow.
type ErrWorkflowRequiredForStatusView struct {
	ProjectID int64
}

// IsErrWorkflowRequiredForStatusView checks if an error is ErrWorkflowRequiredForStatusView.
func IsErrWorkflowRequiredForStatusView(err error) bool {
	_, ok := errors.AsType[ErrWorkflowRequiredForStatusView](err)
	return ok
}

func (err ErrWorkflowRequiredForStatusView) Error() string {
	return fmt.Sprintf("Status columns need a workflow [ProjectID: %d]", err.ProjectID)
}

// ErrCodeWorkflowRequiredForStatusView holds the unique world-error code of this error
const ErrCodeWorkflowRequiredForStatusView = 21009

// HTTPError holds the http error description
func (err ErrWorkflowRequiredForStatusView) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeWorkflowRequiredForStatusView,
		Message:  "A kanban view can only use workflow statuses as columns when the project has a workflow.",
	}
}

// ErrStatusViewBucketsManaged represents an error where the columns of a status mode view should be changed.
// They are the statuses of the workflow and are edited there.
type ErrStatusViewBucketsManaged struct {
	ProjectViewID int64
}

// IsErrStatusViewBucketsManaged checks if an error is ErrStatusViewBucketsManaged.
func IsErrStatusViewBucketsManaged(err error) bool {
	_, ok := errors.AsType[ErrStatusViewBucketsManaged](err)
	return ok
}

func (err ErrStatusViewBucketsManaged) Error() string {
	return fmt.Sprintf("The columns of this view are workflow statuses [ProjectViewID: %d]", err.ProjectViewID)
}

// ErrCodeStatusViewBucketsManaged holds the unique world-error code of this error
const ErrCodeStatusViewBucketsManaged = 21010

// HTTPError holds the http error description
func (err ErrStatusViewBucketsManaged) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeStatusViewBucketsManaged,
		Message:  "The columns of this view are the statuses of the project workflow. Columns, the default column, the done column and limits are changed in the workflow, not in the view.",
	}
}

// ErrWorkflowStatusMoveTooLarge represents an error where removing a status would move more tasks at once than a
// single request moves.
type ErrWorkflowStatusMoveTooLarge struct {
	StatusID int64
	Title    string
	Tasks    int64
	Limit    int64
}

// IsErrWorkflowStatusMoveTooLarge checks if an error is ErrWorkflowStatusMoveTooLarge.
func IsErrWorkflowStatusMoveTooLarge(err error) bool {
	_, ok := errors.AsType[ErrWorkflowStatusMoveTooLarge](err)
	return ok
}

func (err ErrWorkflowStatusMoveTooLarge) Error() string {
	return fmt.Sprintf("Too many tasks to move with one removed status [StatusID: %d, Tasks: %d, Limit: %d]", err.StatusID, err.Tasks, err.Limit)
}

// ErrCodeWorkflowStatusMoveTooLarge holds the unique world-error code of this error
const ErrCodeWorkflowStatusMoveTooLarge = 21011

// HTTPError holds the http error description. It is a 409: moving some of the tasks to another status first makes
// the same request work.
func (err ErrWorkflowStatusMoveTooLarge) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusConflict,
		Code:     ErrCodeWorkflowStatusMoveTooLarge,
		Message:  fmt.Sprintf("The status \"%s\" has %d tasks. At most %d tasks can be moved when a status is removed: move some of them to another status first.", err.Title, err.Tasks, err.Limit),
		I18nParams: map[string]string{
			"title": err.Title,
			"tasks": strconv.FormatInt(err.Tasks, 10),
			"limit": strconv.FormatInt(err.Limit, 10),
		},
	}
}
