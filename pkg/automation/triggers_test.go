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

package automation

import (
	"bytes"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// counting inserts a rule whose only action leaves one label with the key.
func counting(t *testing.T, project int64, trigger string, options map[string]any, key string) *models.AutomationRule {
	t.Helper()
	return insertRawRuleWith(t, project, user1, trigger, options, nil, rawAct("test.count", "key", key))
}

func TestTriggers_TaskCreated(t *testing.T) {
	reset(t)
	counting(t, projectOwned, spec.TriggerTaskCreated, nil, "created")
	newTask(t, projectOwned, "a", user1)
	newTask(t, projectOwned, "b", user1)
	drain(t)
	assert.Equal(t, 2, counter("created"))
}

func TestTriggers_TaskUpdatedAndItsFieldsOption(t *testing.T) {
	reset(t)
	counting(t, projectOwned, spec.TriggerTaskUpdated, nil, "any")
	counting(t, projectOwned, spec.TriggerTaskUpdated, updatedBy("due_date"), "due")
	counting(t, projectOwned, spec.TriggerTaskUpdated, updatedBy("title", "priority"), "title_or_priority")
	task := newTask(t, projectOwned, "task", user1)
	drain(t)
	assert.Equal(t, 0, counter("any"), "creating is not updating")

	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Title = "renamed" })
	drain(t)
	assert.Equal(t, 1, counter("any"))
	assert.Equal(t, 0, counter("due"))
	assert.Equal(t, 1, counter("title_or_priority"))

	due := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.DueDate = due }, "due_date")
	drain(t)
	assert.Equal(t, 2, counter("any"))
	assert.Equal(t, 1, counter("due"))
	assert.Equal(t, 1, counter("title_or_priority"))

	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Priority = 3 }, "priority")
	drain(t)
	assert.Equal(t, 3, counter("any"))
	assert.Equal(t, 1, counter("due"))
	assert.Equal(t, 2, counter("title_or_priority"))

	// A save that changes nothing relevant does not match a fields filter.
	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Priority = 3 }, "priority")
	drain(t)
	assert.Equal(t, 2, counter("title_or_priority"))
}

func TestTriggers_CompletedAndReopened(t *testing.T) {
	reset(t)
	counting(t, projectOwned, spec.TriggerTaskCompleted, nil, "completed")
	counting(t, projectOwned, spec.TriggerTaskReopened, nil, "reopened")
	task := newTask(t, projectOwned, "task", user1)

	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Title = "no change of done" })
	drain(t)
	assert.Equal(t, 0, counter("completed")+counter("reopened"))

	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Done = true }, "done")
	drain(t)
	assert.Equal(t, 1, counter("completed"))
	assert.Equal(t, 0, counter("reopened"))

	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Done = false }, "done")
	drain(t)
	assert.Equal(t, 1, counter("completed"))
	assert.Equal(t, 1, counter("reopened"))
}

func TestTriggers_CompletedThroughAWorkflowStatus(t *testing.T) {
	reset(t)
	counting(t, projectWorkflow, spec.TriggerTaskCompleted, nil, "completed")
	// Task 20 starts in status 1 (open); status 4 is a done status. The transition rules of the fixture
	// workflow do not restrict it.
	mustCommit(t, func(s *xorm.Session) error {
		c := &models.TaskStatusChange{TaskID: 20, ToStatusID: 4, Comment: "done"}
		can, err := c.CanCreate(s, user1)
		require.NoError(t, err)
		require.True(t, can)
		return c.Create(s, user1)
	})
	drain(t)
	assert.Equal(t, 1, counter("completed"))
}

func changeStatus(t *testing.T, taskID, to int64) {
	t.Helper()
	mustCommit(t, func(s *xorm.Session) error {
		c := &models.TaskStatusChange{TaskID: taskID, ToStatusID: to, Comment: "c"}
		can, err := c.CanCreate(s, user1)
		require.NoError(t, err)
		require.True(t, can)
		return c.Create(s, user1)
	})
}

func TestTriggers_StatusChangedAndItsOptions(t *testing.T) {
	reset(t)
	counting(t, projectWorkflow, spec.TriggerTaskStatusChanged, nil, "any")
	counting(t, projectWorkflow, spec.TriggerTaskStatusChanged, map[string]any{"from_status_id": 1}, "from_1")
	counting(t, projectWorkflow, spec.TriggerTaskStatusChanged, map[string]any{"from_status_id": 2}, "from_2")
	counting(t, projectWorkflow, spec.TriggerTaskStatusChanged, map[string]any{"to_status_id": 2}, "to_2")
	counting(t, projectWorkflow, spec.TriggerTaskStatusChanged, map[string]any{"to_category": "in_progress"}, "to_in_progress")
	counting(t, projectWorkflow, spec.TriggerTaskStatusChanged, map[string]any{"to_category": "done"}, "to_done")
	counting(t, projectWorkflow, spec.TriggerTaskStatusChanged, map[string]any{"from_status_id": 1, "to_status_id": 2, "to_category": "in_progress"}, "all_three")
	counting(t, projectWorkflow, spec.TriggerTaskStatusChanged, map[string]any{"from_status_id": 1, "to_status_id": 4}, "mismatch")

	changeStatus(t, 20, 2)
	drain(t)
	want := map[string]int{"any": 1, "from_1": 1, "from_2": 0, "to_2": 1, "to_in_progress": 1, "to_done": 0, "all_three": 1, "mismatch": 0}
	for key, n := range want {
		assert.Equal(t, n, counter(key), key)
	}

	changeStatus(t, 20, 5)
	drain(t)
	assert.Equal(t, 2, counter("any"))
	assert.Equal(t, 1, counter("from_2"))
	assert.Equal(t, 1, counter("to_in_progress"))
	assert.Equal(t, 1, counter("from_1"))
}

func TestTriggers_CustomFieldChangedAndItsOption(t *testing.T) {
	reset(t)
	counting(t, projectOwned, spec.TriggerTaskCustomFieldChanged, nil, "any_field")
	counting(t, projectOwned, spec.TriggerTaskCustomFieldChanged, map[string]any{"field_id": 1}, "field_1")
	counting(t, projectOwned, spec.TriggerTaskCustomFieldChanged, map[string]any{"field_id": 3}, "field_3")
	task := newTask(t, projectOwned, "task", user1)

	setCustomField(t, task.ID, 1, "reference")
	drain(t)
	assert.Equal(t, 1, counter("any_field"))
	assert.Equal(t, 1, counter("field_1"))
	assert.Equal(t, 0, counter("field_3"))

	// Setting the same value is no change and no event.
	setCustomField(t, task.ID, 1, "reference")
	drain(t)
	assert.Equal(t, 1, counter("field_1"))

	setCustomField(t, task.ID, 3, 12.5)
	drain(t)
	assert.Equal(t, 2, counter("any_field"))
	assert.Equal(t, 1, counter("field_3"))
}

func setCustomField(t *testing.T, taskID, fieldID int64, value any) {
	t.Helper()
	mustCommit(t, func(s *xorm.Session) error {
		v := &models.CustomFieldValue{TaskID: taskID, CustomFieldID: fieldID, Value: value}
		can, err := v.CanUpdate(s, user1)
		require.NoError(t, err)
		require.True(t, can)
		return v.Update(s, user1)
	})
}

func TestTriggers_AssigneeAddedAndRemoved(t *testing.T) {
	reset(t)
	counting(t, projectOwned, spec.TriggerTaskAssigneeAdded, nil, "added")
	counting(t, projectOwned, spec.TriggerTaskAssigneeRemoved, nil, "removed")
	task := newTask(t, projectOwned, "task", user1)

	mustCommit(t, func(s *xorm.Session) error {
		return (&models.TaskAssginee{TaskID: task.ID, UserID: user1.ID}).Create(s, user1)
	})
	drain(t)
	assert.Equal(t, 1, counter("added"))
	assert.Equal(t, 0, counter("removed"))

	mustCommit(t, func(s *xorm.Session) error {
		return (&models.TaskAssginee{TaskID: task.ID, UserID: user1.ID}).Delete(s, user1)
	})
	drain(t)
	assert.Equal(t, 1, counter("added"))
	assert.Equal(t, 1, counter("removed"))
}

func TestTriggers_CommentAdded(t *testing.T) {
	reset(t)
	counting(t, projectOwned, spec.TriggerTaskCommentAdded, nil, "commented")
	task := newTask(t, projectOwned, "task", user1)
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.TaskComment{TaskID: task.ID, Comment: "hello"}).Create(s, user1)
	})
	drain(t)
	assert.Equal(t, 1, counter("commented"))
}

func TestTriggers_AttachmentAdded(t *testing.T) {
	reset(t)
	counting(t, projectOwned, spec.TriggerTaskAttachmentAdded, nil, "attached")
	task := newTask(t, projectOwned, "task", user1)
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.TaskAttachment{TaskID: task.ID}).NewAttachment(s, bytes.NewReader([]byte("abc")), "a.txt", 3, user1)
	})
	drain(t)
	assert.Equal(t, 1, counter("attached"))
}

func TestTriggers_TaskDeletedStillRunsWithTheLastKnownTask(t *testing.T) {
	reset(t)
	counting(t, projectOwned, spec.TriggerTaskDeleted, nil, "deleted")
	r := insertRawRuleWith(t, projectOwned, user1, spec.TriggerTaskDeleted, nil,
		&spec.Condition{Field: "title", Op: "=", Value: "goodbye"}, rawAct("test.count", "key", "titled"))
	task := newTask(t, projectOwned, "goodbye", user1)

	mustCommit(t, func(s *xorm.Session) error {
		tk := &models.Task{ID: task.ID}
		can, err := tk.CanDelete(s, user1)
		require.NoError(t, err)
		require.True(t, can)
		return tk.Delete(s, user1)
	})
	drain(t)
	assert.Equal(t, 1, counter("deleted"))
	assert.Equal(t, 1, counter("titled"), "conditions see the task as it was when it was deleted")
	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)
}

func TestTriggers_OtherProjectsAndEventsDoNotMatch(t *testing.T) {
	reset(t)
	counting(t, projectWorkflow, spec.TriggerTaskCreated, nil, "other_project")
	counting(t, projectOwned, spec.TriggerTaskDeleted, nil, "other_event")
	newTask(t, projectOwned, "task", user1)
	drain(t)
	assert.Equal(t, 0, counter("other_project"))
	assert.Equal(t, 0, counter("other_event"))
}
