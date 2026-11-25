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
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func TestWorkflow_RemovedStatusMoveIsAStatusChange(t *testing.T) {
	setup := func(t *testing.T, extra int) []int64 {
		t.Helper()
		db.LoadAndAssertFixtures(t)
		ids := []int64{20}
		for i := 0; i < extra; i++ {
			task := createWorkflowTask(t, 11, &Task{Title: "extra", ProjectID: 11})
			ids = append(ids, task.ID)
		}
		return ids
	}

	t.Run("every moved task gets a history row and an event marked with the removal", func(t *testing.T) {
		ids := setup(t, 3)
		for _, id := range ids {
			require.Equal(t, int64(1), taskStatusOf(t, id).StatusID)
		}
		before := map[int64]int{}
		for _, id := range ids {
			before[id] = len(taskHistoryOf(t, id))
		}

		wf := readWorkflow(t, 11)
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		wf.MoveTasksTo = 2
		events.ClearDispatchedEvents()
		mustSaveWorkflow(t, wf, cfUser1)
		requireNoTaskOnARemovedStatus(t, 11)

		for _, id := range ids {
			assert.Equal(t, int64(2), taskStatusOf(t, id).StatusID)
			history := taskHistoryOf(t, id)
			require.Len(t, history, before[id]+1, "task %d", id)
			last := history[len(history)-1]
			assert.Equal(t, "Todo", last.FromTitle)
			assert.Equal(t, "Doing", last.ToTitle)
			assert.Equal(t, int64(2), last.ToStatusID)
			require.NotNil(t, last.FromStatusID)
			assert.Equal(t, int64(1), *last.FromStatusID)
			assert.Equal(t, int64(1), last.DoerID)
			cause, comment := splitHistoryCause(last.Comment)
			assert.Equal(t, "status_removed", cause)
			assert.Empty(t, comment)
		}

		changed := events.GetDispatchedEvents("task.status.changed")
		require.Len(t, changed, len(ids))
		seen := map[int64]bool{}
		for _, e := range changed {
			ev := e.(*TaskStatusChangedEvent)
			seen[ev.Task.ID] = true
			require.NotNil(t, ev.FromStatus)
			assert.Equal(t, "Todo", ev.FromStatus.Title)
			assert.Equal(t, "Doing", ev.ToStatus.Title)
			assert.Equal(t, "status_removed", ev.Cause)
			assert.Empty(t, ev.Comment)
			require.NotNil(t, ev.Doer)
			assert.Equal(t, int64(1), ev.Doer.ID)
		}
		for _, id := range ids {
			assert.True(t, seen[id], "event for task %d", id)
		}
		assert.Zero(t, events.CountDispatchedEvents("task.updated"), "the done flag did not change")
	})

	t.Run("a category change also fires the task update like a status change does", func(t *testing.T) {
		ids := setup(t, 0)
		wf := readWorkflow(t, 11)
		// Todo goes away, Done (a done category) receives the task.
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		doneID := statusIDByTitle(wf, "Done")
		require.NotZero(t, doneID)
		wf.MoveTasksTo = doneID
		events.ClearDispatchedEvents()
		mustSaveWorkflow(t, wf, cfUser1)

		assert.True(t, taskDone(t, ids[0]))
		assert.Equal(t, 1, events.CountDispatchedEvents("task.status.changed"))
		updated := events.GetDispatchedEvents("task.updated")
		require.Len(t, updated, 1)
		ev := updated[0].(*TaskUpdatedEvent)
		assert.True(t, ev.Task.Done)
		require.NotNil(t, ev.OldTask)
		assert.False(t, ev.OldTask.Done)
	})

	t.Run("a failed save writes nothing", func(t *testing.T) {
		ids := setup(t, 1)
		wf := readWorkflow(t, 11)
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		// No move target: the save is refused and nothing is written.
		events.ClearDispatchedEvents()
		_, err := saveWorkflow(t, wf, cfUser1)
		require.Error(t, err)
		assert.Zero(t, events.CountDispatchedEvents("task.status.changed"))
		db.AssertCount(t, "task_status_history", builder.And(builder.Eq{"task_id": ids[1]}, builder.Like{"comment", "%status_removed%"}), 0)
	})
}

// requireNoTaskOnARemovedStatus fails when a task of the workflow of the project is on a status that is archived or
// gone: it would be off every board column.
func requireNoTaskOnARemovedStatus(t *testing.T, projectID int64) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.SQL("SELECT COUNT(*) FROM task_statuses ts JOIN workflows w ON w.id = ts.workflow_id LEFT JOIN workflow_statuses ws ON ws.id = ts.status_id WHERE w.project_id = ? AND (ws.id IS NULL OR ws.archived = ?)", projectID, true).Count()
	require.NoError(t, err)
	require.Zero(t, n, "tasks are left on a removed or archived status")
}

func removeTodoMovingTo(t *testing.T, targetTitle string) {
	t.Helper()
	wf := readWorkflow(t, 11)
	wf.Statuses = wf.Statuses[1:]
	wf.Statuses[0].IsInitial = true
	wf.Transitions = nil
	wf.MoveTasksTo = statusIDByTitle(wf, targetTitle)
	require.NotZero(t, wf.MoveTasksTo)
	mustSaveWorkflow(t, wf, cfUser1)
	requireNoTaskOnARemovedStatus(t, 11)
}

func TestWorkflow_RemovedStatusMoveDetails(t *testing.T) {
	t.Run("more tasks than one chunk are all moved with a history row and an event each", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		const extra = workflowBulkChunkSize + 5
		ids := []int64{20}
		for i := 0; i < extra; i++ {
			ids = append(ids, createWorkflowTask(t, 11, &Task{Title: "bulk", ProjectID: 11}).ID)
		}
		events.ClearDispatchedEvents()
		removeTodoMovingTo(t, "Doing")

		db.AssertCount(t, "task_statuses", builder.Eq{"status_id": 2}, int64(len(ids)))
		db.AssertCount(t, "task_status_history", builder.And(builder.In("task_id", ids), builder.Like{"comment", "%status_removed%"}), int64(len(ids)))
		assert.Equal(t, len(ids), events.CountDispatchedEvents("task.status.changed"))
	})

	t.Run("a trashed task is moved and gets history but no event", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		trashed := createWorkflowTask(t, 11, &Task{Title: "trashed", ProjectID: 11})
		setRaw(t, "tasks", builder.Eq{"id": trashed.ID}, map[string]any{"deleted_at": time.Now().UTC().Format("2006-01-02 15:04:05")})
		events.ClearDispatchedEvents()
		removeTodoMovingTo(t, "Doing")

		assert.Equal(t, int64(2), taskStatusOf(t, trashed.ID).StatusID)
		db.AssertCount(t, "task_status_history", builder.And(builder.Eq{"task_id": trashed.ID}, builder.Like{"comment", "%status_removed%"}), 1)
		for _, e := range events.GetDispatchedEvents("task.status.changed") {
			assert.NotEqual(t, trashed.ID, e.(*TaskStatusChangedEvent).Task.ID)
		}
		assert.Equal(t, 1, events.CountDispatchedEvents("task.status.changed"), "only task 20")
	})

	t.Run("a repeating task that enters a done status is reopened like a status change does", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		due := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
		repeating := createWorkflowTask(t, 11, &Task{Title: "repeats", ProjectID: 11, RepeatAfter: 86400, DueDate: due})
		removeTodoMovingTo(t, "Done")

		assert.False(t, taskDone(t, repeating.ID), "a repeating task is not done for good")
		assert.Equal(t, int64(2), taskStatusOf(t, repeating.ID).StatusID, "it is reopened in the initial status of the new document, not in the removed one")
		s := db.NewSession()
		defer s.Close()
		stored, err := GetTaskByIDSimple(s, repeating.ID)
		require.NoError(t, err)
		assert.True(t, stored.DueDate.After(due), "the due date moved on")
		assert.True(t, taskDone(t, 20), "a plain task is done")
	})

	t.Run("the update time of a moved task changes also when done does not", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		setRaw(t, "tasks", builder.Eq{"id": 20}, map[string]any{"updated": "2000-01-01 00:00:00"})
		removeTodoMovingTo(t, "Doing")
		s := db.NewSession()
		defer s.Close()
		stored, err := GetTaskByIDSimple(s, 20)
		require.NoError(t, err)
		assert.Greater(t, stored.Updated.Year(), 2000)
	})

	t.Run("a comment that starts like the marker cannot fake a cause", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		_, err := changeTaskStatusAs(t, 20, 2, "\x1estatus_removed", cfUser1)
		require.NoError(t, err)
		history := taskHistoryOf(t, 20)
		cause, comment := splitHistoryCause(history[len(history)-1].Comment)
		assert.Empty(t, cause)
		assert.Equal(t, "status_removed", comment)
	})
}

func TestWorkflow_CategoryChangeAnnouncesTheDoneFlag(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	// Task 20 is in Todo; Todo becomes a done status.
	wf := readWorkflow(t, 11)
	wf.Statuses[0].Category = WorkflowCategoryDone
	wf.Statuses[1].IsInitial = true
	wf.Statuses[0].IsInitial = false
	events.ClearDispatchedEvents()
	mustSaveWorkflow(t, wf, cfUser1)

	assert.True(t, taskDone(t, 20))
	updated := events.GetDispatchedEvents("task.updated")
	require.Len(t, updated, 1)
	ev := updated[0].(*TaskUpdatedEvent)
	assert.EqualValues(t, 20, ev.Task.ID)
	assert.True(t, ev.Task.Done)
	assert.False(t, ev.OldTask.Done)
}

func TestWorkflow_RemovedStatusWithTooManyTasksIsRefused(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	for i := 0; i < 3; i++ {
		createWorkflowTask(t, 11, &Task{Title: "many", ProjectID: 11})
	}
	previous := maxTasksChangedBySave
	maxTasksChangedBySave = 3
	t.Cleanup(func() { maxTasksChangedBySave = previous })

	wf := readWorkflow(t, 11)
	wf.Statuses = wf.Statuses[1:]
	wf.Statuses[0].IsInitial = true
	wf.Transitions = nil
	wf.MoveTasksTo = wf.Statuses[0].ID
	events.ClearDispatchedEvents()
	_, err := saveWorkflow(t, wf, cfUser1)
	require.Error(t, err)
	assert.True(t, IsErrWorkflowStatusMoveTooLarge(err), "%v", err)
	assert.Equal(t, int64(1), taskStatusOf(t, 20).StatusID, "nothing was moved")
	assert.Zero(t, events.CountDispatchedEvents("task.status.changed"))
	db.AssertExists(t, "workflow_statuses", map[string]interface{}{"id": 1}, false)
}

func TestWorkflow_RefusedSaveWritesNothing(t *testing.T) {
	previous := maxTasksChangedBySave
	t.Cleanup(func() { maxTasksChangedBySave = previous })

	t.Run("a rule that names the removed status stays enabled", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		onTodo := autoRule(11, spec.TriggerTaskStatusChanged, setPriority(1))
		onTodo.Trigger.Options = map[string]any{"from_status_id": 1}
		mustSaveAutomationRule(t, onTodo, autoUser1)

		maxTasksChangedBySave = 0
		wf := readWorkflow(t, 11)
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		wf.MoveTasksTo = wf.Statuses[0].ID
		_, err := saveWorkflow(t, wf, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowStatusMoveTooLarge(err), "%v", err)
		assert.True(t, reloadAutomationRule(t, onTodo.ID).Enabled, "the rule was not disabled")
		assert.Equal(t, int64(1), taskStatusOf(t, 20).StatusID)
	})

	t.Run("a pending approval request is not cancelled", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)

		maxTasksChangedBySave = 0
		wf := readWorkflow(t, apProject)
		var kept []*WorkflowStatus
		for _, st := range wf.Statuses {
			if st.ID != env.todo {
				kept = append(kept, st)
			}
		}
		wf.Statuses = kept
		wf.Transitions = nil
		wf.MoveTasksTo = env.doing
		for _, st := range wf.Statuses {
			st.IsInitial = st.ID == env.doing
		}
		_, err := saveWorkflow(t, wf, wfOwner6)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowStatusMoveTooLarge(err), "%v", err)
		assert.Equal(t, ApprovalStatusPending, readApprovalRow(t, req.ID).Status)
	})

	t.Run("a category change over the limit is refused too", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		maxTasksChangedBySave = 0
		wf := readWorkflow(t, 11)
		wf.Statuses[0].Category = WorkflowCategoryDone
		_, err := saveWorkflow(t, wf, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowStatusMoveTooLarge(err), "%v", err)
		assert.False(t, taskDone(t, 20))
	})
}
