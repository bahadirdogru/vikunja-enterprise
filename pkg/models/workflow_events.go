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

// WorkflowCreatedEvent represents a workflow being created for a project
type WorkflowCreatedEvent struct {
	Workflow *Workflow  `json:"workflow"`
	Doer     *user.User `json:"doer"`
}

// Name defines the name for WorkflowCreatedEvent
func (e *WorkflowCreatedEvent) Name() string {
	return "workflow.created"
}

// WorkflowUpdatedEvent represents a workflow being replaced
type WorkflowUpdatedEvent struct {
	Workflow *Workflow  `json:"workflow"`
	Doer     *user.User `json:"doer"`
}

// Name defines the name for WorkflowUpdatedEvent
func (e *WorkflowUpdatedEvent) Name() string {
	return "workflow.updated"
}

// WorkflowDeletedEvent represents a workflow being deleted
type WorkflowDeletedEvent struct {
	Workflow *Workflow  `json:"workflow"`
	Doer     *user.User `json:"doer"`
}

// Name defines the name for WorkflowDeletedEvent
func (e *WorkflowDeletedEvent) Name() string {
	return "workflow.deleted"
}

// TaskStatusChangedEvent represents a task moving from one workflow status to another.
// FromStatus is nil when the task had no status before.
type TaskStatusChangedEvent struct {
	Task       *Task           `json:"task"`
	FromStatus *WorkflowStatus `json:"from_status"`
	ToStatus   *WorkflowStatus `json:"to_status"`
	Comment    string          `json:"comment"`
	// Cause is set when the change is not one somebody asked for: "status_removed" when the status the task was
	// in was removed from the workflow and its tasks were moved. Empty for every other change.
	Cause string     `json:"cause,omitempty"`
	Doer  *user.User `json:"doer"`
}

// Name defines the name for TaskStatusChangedEvent
func (e *TaskStatusChangedEvent) Name() string {
	return "task.status.changed"
}
