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
	"context"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func createTaskAs(t *testing.T, task *Task, a web.Auth) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	if err := task.Create(s, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	return nil
}

// A task that is done when it enters a workflow (created done, created in the done bucket of a manual view, moved
// in from another project) starts in the first done status. That is the done transition out of the initial status
// and has to pass its rules, or the rules of the workflow can be walked around.
func TestWorkflow_DoneStartIsChecked(t *testing.T) {
	// Project 10: owner user6, user1 writes. The approval tests share it with user2 and user3.
	doneNeedsApproval := func(doc *Workflow) {
		doc.Transitions = append(doc.Transitions, &WorkflowTransition{
			FromStatusID: -1, ToStatusID: -3, RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6),
		})
	}

	t.Run("a task cannot be created done when the done transition needs an approval", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), doneNeedsApproval)
		task := &Task{Title: "done at once", ProjectID: apProject, Done: true}
		err := createTaskAs(t, task, apU1)
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequired(err), "got %T: %v", err, err)
		db.AssertCount(t, "task_statuses", builder.Eq{"status_id": env.done}, 0)
		db.AssertCount(t, "approval_requests", builder.Eq{"project_id": apProject}, 0)
	})

	t.Run("nor in the done bucket of a manual kanban view", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), doneNeedsApproval)
		// View 40 is the manual kanban view of project 10, bucket 26 one of its buckets.
		setRaw(t, "project_views", builder.Eq{"id": 40}, map[string]any{"done_bucket_id": 26})

		err := createTaskAs(t, &Task{Title: "into the done column", ProjectID: apProject, BucketID: 26}, apU1)
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequired(err), "got %T: %v", err, err)
		db.AssertCount(t, "task_statuses", builder.Eq{"status_id": env.done}, 0)
	})

	t.Run("a restricted workflow refuses a task created done", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), func(doc *Workflow) {
			doc.RestrictTransitions = true
		})
		err := createTaskAs(t, &Task{Title: "done at once", ProjectID: apProject, Done: true}, apU1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowTransitionNotAllowed(err), "got %T: %v", err, err)
		db.AssertCount(t, "task_statuses", builder.Eq{"status_id": env.done}, 0)
	})

	t.Run("the permission of the done transition applies to the creator", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), func(doc *Workflow) {
			doc.Transitions = append(doc.Transitions, &WorkflowTransition{
				FromStatusID: -1, ToStatusID: -3, MinPermission: WorkflowTransitionPermissionAdmin,
			})
		})
		err := createTaskAs(t, &Task{Title: "done at once", ProjectID: apProject, Done: true}, apU1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowTransitionForbidden(err), "got %T: %v", err, err)

		// The admin may, and the task starts in the done status with a single history entry as before.
		task := &Task{Title: "done by the admin", ProjectID: apProject, Done: true}
		require.NoError(t, createTaskAs(t, task, apU6))
		assert.Equal(t, env.done, taskStatusOf(t, task.ID).StatusID)
		history := taskHistoryOf(t, task.ID)
		require.Len(t, history, 1)
		assert.Nil(t, history[0].FromStatusID)
		assert.Equal(t, env.done, history[0].ToStatusID)
	})

	t.Run("without a rule in the way a task is created done as before", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), nil)
		task := &Task{Title: "done at once", ProjectID: apProject, Done: true}
		require.NoError(t, createTaskAs(t, task, apU1))
		assert.Equal(t, env.done, taskStatusOf(t, task.ID).StatusID)
		assert.True(t, taskDone(t, task.ID))
		assert.Len(t, taskHistoryOf(t, task.ID), 1)
	})

	t.Run("a done task cannot be moved in past the approval", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), doneNeedsApproval)
		// Task 1 lives in project 1, which user1 owns and which has no workflow: marking it done there is free.
		require.NoError(t, updateTaskAs(t, &Task{ID: 1, Title: "task #1", Done: true}, apU1))

		err := updateTaskAs(t, &Task{ID: 1, Title: "task #1", Done: true, ProjectID: apProject}, apU1)
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequired(err), "got %T: %v", err, err)
		db.AssertCount(t, "task_statuses", builder.Eq{"status_id": env.done}, 0)
		db.AssertExists(t, "tasks", map[string]interface{}{"id": 1, "project_id": 1}, false)
	})

	t.Run("nor out and back in to skip the approval of a task of the project", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), doneNeedsApproval)
		id := env.task.ID
		// Out into user1's own project and done there in one update.
		require.NoError(t, updateTaskAs(t, &Task{ID: id, Title: env.task.Title, Done: true, ProjectID: 1}, apU1))

		err := updateTaskAs(t, &Task{ID: id, Title: env.task.Title, Done: true, ProjectID: apProject}, apU1)
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequired(err), "got %T: %v", err, err)
		db.AssertCount(t, "task_statuses", builder.Eq{"status_id": env.done}, 0)
	})

	t.Run("an open task moves in freely and a done one does where no rule is in the way", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), nil)
		require.NoError(t, updateTaskAs(t, &Task{ID: 1, Title: "task #1", ProjectID: apProject}, apU1))
		assert.Equal(t, env.todo, taskStatusOf(t, 1).StatusID)

		// Task 2 is done in the fixtures.
		require.NoError(t, updateTaskAs(t, &Task{ID: 2, Title: "task #2 done", Done: true, ProjectID: apProject}, apU1))
		assert.Equal(t, env.done, taskStatusOf(t, 2).StatusID)
		assert.True(t, taskDone(t, 2))
	})
}

// The category of a status decides the done flag of the tasks in it, also when the category is edited.
func TestWorkflow_CategoryChangeSyncsDone(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	wf := mustSaveWorkflow(t, baseWorkflowDoc(1), cfUser1)
	doing := statusIDByTitle(wf, "Doing")
	_, err := changeTaskStatusAs(t, 1, doing, "", cfUser1)
	require.NoError(t, err)
	require.False(t, taskDone(t, 1))

	setCategory := func(category WorkflowStatusCategory) {
		doc := readWorkflow(t, 1)
		for _, st := range doc.Statuses {
			if st.ID == doing {
				st.Category = category
			}
		}
		mustSaveWorkflow(t, doc, cfUser1)
	}

	setCategory(WorkflowCategoryDone)
	assert.True(t, taskDone(t, 1), "the task sits in a status that now counts as done")
	assert.False(t, taskDone(t, 3), "tasks of other statuses are untouched")

	setCategory(WorkflowCategoryCancelled)
	assert.True(t, taskDone(t, 1), "done and cancelled both count as done")

	setCategory(WorkflowCategoryInProgress)
	assert.False(t, taskDone(t, 1), "and it is open again when the status is")
	assert.True(t, taskDone(t, 2), "task 2 is in the done status")
}

// "My approvals" follows the task: a request whose task moved to a project the caller cannot read is gone, even
// when the project the request was made in is still readable.
func TestApprovals_ListFollowsTheTask(t *testing.T) {
	env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), nil)
	req := env.request(t, apU1)

	list := func(a web.Auth) []*ApprovalRequest {
		s := db.NewSession()
		defer s.Close()
		result, _, _, err := (&ApprovalRequest{}).ReadAll(s, a, "", 1, 50)
		require.NoError(t, err)
		return result.([]*ApprovalRequest)
	}
	require.Len(t, list(apU2), 1, "the approver sees the request while the task is in the project")

	// Project 9 belongs to user6 as well; user2 has no access to it.
	s := db.NewSession()
	can, _, err := (&Project{ID: 9}).CanRead(s, apU2)
	require.NoError(t, err)
	require.False(t, can)
	s.Close()
	require.NoError(t, updateTaskAs(t, &Task{ID: env.task.ID, Title: "moved and renamed in private", ProjectID: 9}, apU6))
	requireCancelled(t, req.ID, ApprovalCancelTaskMoved)

	for _, r := range list(apU2) {
		assert.NotEqual(t, req.ID, r.ID, "the request of a task user2 cannot read is listed, with the task %+v", r.Task)
	}
	// The owner can read both projects and keeps the request in the list.
	found := false
	for _, r := range list(apU6) {
		found = found || r.ID == req.ID
	}
	assert.False(t, found, "user6 is neither requester nor approver")
	found = false
	// user1 asked and can read project 9.
	for _, r := range list(apU1) {
		found = found || r.ID == req.ID
	}
	assert.True(t, found, "the requester can read the new project and still sees the request")
}

// The approval of a status change is what the server says, not what the body sent.
func TestTaskStatusChange_IgnoresApprovalOfTheBody(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()
	// Task 20 of project 11 goes from Todo (1) to Doing (2) without an approval.
	c := &TaskStatusChange{TaskID: 20, ToStatusID: 2, Approval: &ApprovalRequest{ID: 99}}
	can, err := c.CanCreate(s, cfUser1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, c.Create(s, cfUser1))
	assert.Nil(t, c.Approval, "the route answers 202 when this is set")
	require.NoError(t, s.Commit())
}

func TestTaskBucket_IgnoresApprovalOfTheBody(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()
	// View 4 is the manual kanban view of project 1, task 1 sits in bucket 1 and bucket 3 has room.
	b := &TaskBucket{TaskID: 1, ProjectViewID: 4, ProjectID: 1, BucketID: 3, Approval: &ApprovalRequest{ID: 99}}
	can, err := b.CanUpdate(s, cfUser1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, b.Update(s, cfUser1))
	assert.Nil(t, b.Approval, "the route answers 202 when this is set")
	require.NoError(t, s.Commit())
}
