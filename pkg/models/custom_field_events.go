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

// CustomFieldCreatedEvent represents a custom field being created
type CustomFieldCreatedEvent struct {
	CustomField *CustomField `json:"custom_field"`
	Doer        *user.User   `json:"doer"`
}

// Name defines the name for CustomFieldCreatedEvent
func (e *CustomFieldCreatedEvent) Name() string {
	return "custom-field.created"
}

// CustomFieldUpdatedEvent represents a custom field being updated
type CustomFieldUpdatedEvent struct {
	CustomField *CustomField `json:"custom_field"`
	Doer        *user.User   `json:"doer"`
}

// Name defines the name for CustomFieldUpdatedEvent
func (e *CustomFieldUpdatedEvent) Name() string {
	return "custom-field.updated"
}

// CustomFieldDeletedEvent represents a custom field being deleted
type CustomFieldDeletedEvent struct {
	CustomField *CustomField `json:"custom_field"`
	Doer        *user.User   `json:"doer"`
}

// Name defines the name for CustomFieldDeletedEvent
func (e *CustomFieldDeletedEvent) Name() string {
	return "custom-field.deleted"
}

// TaskCustomFieldValueUpdatedEvent represents the value of a custom field on a task being set or cleared.
// OldValue and NewValue use the API shape of the field's type and are nil when unset.
type TaskCustomFieldValueUpdatedEvent struct {
	Task        *Task        `json:"task"`
	CustomField *CustomField `json:"custom_field"`
	OldValue    any          `json:"old_value"`
	NewValue    any          `json:"new_value"`
	Doer        *user.User   `json:"doer"`
}

// Name defines the name for TaskCustomFieldValueUpdatedEvent
func (e *TaskCustomFieldValueUpdatedEvent) Name() string {
	return "task.custom-field-value.updated"
}
