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
			assert.Contains(t, last.Comment, "was removed from the workflow")
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
			assert.Contains(t, ev.Comment, "was removed from the workflow")
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
		db.AssertCount(t, "task_status_history", builder.And(builder.Eq{"task_id": ids[1]}, builder.Like{"comment", "%removed from the workflow%"}), 0)
	})
}
