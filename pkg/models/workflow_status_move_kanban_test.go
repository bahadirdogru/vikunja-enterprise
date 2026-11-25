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

// The forced move of a removed status takes the normal status change for a task whose done flag changes when the
// project has a kanban view with manual buckets (docs/modules/workflows.md), so the card follows into the done bucket
// and back. View 44 is the manual kanban view of project 11 (buckets 11 and 27); task 20 sits in bucket 11, in
// status 1 (Todo).
func TestWorkflow_RemovedStatusMoveFollowsTheManualKanbanBuckets(t *testing.T) {
	setup := func(t *testing.T) {
		t.Helper()
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		_, err := s.ID(44).Cols("done_bucket_id").Update(&ProjectView{DoneBucketID: 27})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
	}
	bucketOf := func(t *testing.T, taskID int64) int64 {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		tb := &TaskBucket{}
		has, err := s.Where("task_id = ? AND project_view_id = 44", taskID).Get(tb)
		require.NoError(t, err)
		require.True(t, has)
		return tb.BucketID
	}

	t.Run("a task that becomes done moves to the done bucket, once, with one history row and one event", func(t *testing.T) {
		setup(t)
		require.Equal(t, int64(11), bucketOf(t, 20))
		events.ClearDispatchedEvents()

		removeTodoMovingTo(t, "Done")

		assert.True(t, taskDone(t, 20))
		assert.Equal(t, int64(27), bucketOf(t, 20), "the card is in the done bucket")
		assert.Equal(t, int64(4), taskStatusOf(t, 20).StatusID)
		db.AssertCount(t, "task_status_history", builder.And(builder.Eq{"task_id": 20}, builder.Like{"comment", "%status_removed%"}), 1)
		assert.Equal(t, 1, events.CountDispatchedEvents("task.status.changed"))
		db.AssertCount(t, "task_buckets", builder.Eq{"task_id": 20, "project_view_id": 44}, 1)
	})

	t.Run("a task that stays open keeps its bucket", func(t *testing.T) {
		setup(t)
		removeTodoMovingTo(t, "Doing")

		assert.False(t, taskDone(t, 20))
		assert.Equal(t, int64(11), bucketOf(t, 20))
		assert.Equal(t, int64(2), taskStatusOf(t, 20).StatusID)
	})

	t.Run("a task that leaves done because its status is removed leaves the done bucket", func(t *testing.T) {
		setup(t)
		_, err := changeTaskStatusAs(t, 20, 4, "", cfUser1)
		require.NoError(t, err)
		require.True(t, taskDone(t, 20))
		require.Equal(t, int64(27), bucketOf(t, 20))

		wf := readWorkflow(t, 11)
		keep := wf.Statuses[:0:0]
		for _, st := range wf.Statuses {
			if st.ID != 4 {
				keep = append(keep, st)
			}
		}
		// A workflow needs a done status, so another one takes the place of the removed one.
		wf.Statuses = append(keep, &WorkflowStatus{ID: -1, Title: "Closed", Category: WorkflowCategoryDone})
		trs := wf.Transitions[:0:0]
		for _, tr := range wf.Transitions {
			if tr.ToStatusID != 4 && tr.FromStatusID != 4 {
				trs = append(trs, tr)
			}
		}
		wf.Transitions = trs
		wf.MoveTasksTo = statusIDByTitle(wf, "Doing")
		require.NotZero(t, wf.MoveTasksTo)
		mustSaveWorkflow(t, wf, cfUser1)
		requireNoTaskOnARemovedStatus(t, 11)

		assert.False(t, taskDone(t, 20))
		assert.Equal(t, int64(2), taskStatusOf(t, 20).StatusID)
		assert.NotEqual(t, int64(27), bucketOf(t, 20), "the card left the done bucket")
	})
}
