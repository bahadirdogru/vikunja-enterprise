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
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// Fixture topology (pkg/db/fixtures): user1 owns project 1 (fields 1-4, tasks 1-12 and more, task 2 is done),
// has write access to project 10 (owner user6, task 19) and admin access to project 11, which has a
// workflow in the fixtures (statuses 1-6, transitions 1-3, task 20 is in status 1). Team 1 holds user1
// and user2, team 9 holds user2 only.
var wfOwner6 = &user.User{ID: 6}

func baseWorkflowDoc(projectID int64) *Workflow {
	return &Workflow{
		ProjectID: projectID,
		Statuses: []*WorkflowStatus{
			{ID: -1, Title: "Todo", Category: WorkflowCategoryOpen, IsInitial: true},
			{ID: -2, Title: "Doing", Category: WorkflowCategoryInProgress},
			{ID: -3, Title: "Done", Category: WorkflowCategoryDone},
			{ID: -4, Title: "Cancelled", Category: WorkflowCategoryCancelled},
		},
	}
}

func saveWorkflow(t *testing.T, wf *Workflow, a web.Auth) (*Workflow, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()

	can, err := wf.CanUpdate(s, a)
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	if !can {
		_ = s.Rollback()
		return nil, ErrGenericForbidden{}
	}
	if err = wf.Update(s, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, err
	}
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	return wf, nil
}

func mustSaveWorkflow(t *testing.T, wf *Workflow, a web.Auth) *Workflow {
	t.Helper()
	saved, err := saveWorkflow(t, wf, a)
	require.NoError(t, err)
	return saved
}

func readWorkflow(t *testing.T, projectID int64) *Workflow {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	// user6 owns 9 and 10, user1 owns project 1 and everything copied from it and is admin on 11.
	var reader web.Auth = cfUser1
	if projectID == 9 || projectID == 10 {
		reader = wfOwner6
	}
	wf := &Workflow{ProjectID: projectID}
	can, _, err := wf.CanRead(s, reader)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, wf.ReadOne(s, reader))
	return wf
}

func statusIDByTitle(wf *Workflow, title string) int64 {
	for _, st := range wf.Statuses {
		if st.Title == title {
			return st.ID
		}
	}
	return 0
}

func changeTaskStatusAs(t *testing.T, taskID, toStatusID int64, comment string, a web.Auth) (*TaskStatusChange, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()

	c := &TaskStatusChange{TaskID: taskID, ToStatusID: toStatusID, Comment: comment}
	can, err := c.CanCreate(s, a)
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	if !can {
		_ = s.Rollback()
		return nil, ErrGenericForbidden{}
	}
	if err = c.Create(s, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, err
	}
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	return c, nil
}

func taskStatusOf(t *testing.T, taskID int64) *TaskStatus {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	row, err := getTaskStatusRow(s, taskID)
	require.NoError(t, err)
	return row
}

func taskDone(t *testing.T, taskID int64) bool {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	task, err := GetTaskByIDSimple(s, taskID)
	require.NoError(t, err)
	return task.Done
}

func taskHistoryOf(t *testing.T, taskID int64) []*TaskStatusHistory {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	rows := []*TaskStatusHistory{}
	require.NoError(t, s.Where("task_id = ?", taskID).OrderBy("id ASC").Find(&rows))
	return rows
}

func createWorkflowTask(t *testing.T, projectID int64, task *Task) *Task {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	require.NoError(t, task.Create(s, wfOwner6))
	require.NoError(t, s.Commit())
	task.ProjectID = projectID
	return task
}

func updateTaskAs(t *testing.T, task *Task, a web.Auth) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	if err := task.Update(s, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	return nil
}

func requireRequirements(t *testing.T, err error) ErrWorkflowRequirementsNotMet {
	t.Helper()
	require.Error(t, err)
	req, ok := err.(ErrWorkflowRequirementsNotMet)
	require.True(t, ok, "expected ErrWorkflowRequirementsNotMet, got %T: %v", err, err)
	return req
}

func TestWorkflow_Document(t *testing.T) {
	t.Run("create assigns real ids and maps temporary ones", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		events.ClearDispatchedEvents()

		doc := baseWorkflowDoc(1)
		doc.RestrictTransitions = true
		doc.Transitions = []*WorkflowTransition{
			{FromStatusID: -1, ToStatusID: -2, Title: "Start", RequiredFields: []string{"due_date", "cf_2", "due_date"}, TeamIDs: []int64{1, 1}},
			{FromStatusID: -2, ToStatusID: -3, MinPermission: WorkflowTransitionPermissionAdmin, RequireComment: true},
		}
		saved := mustSaveWorkflow(t, doc, cfUser1)

		require.NotZero(t, saved.ID)
		require.Len(t, saved.Statuses, 4)
		for _, st := range saved.Statuses {
			assert.Positive(t, st.ID)
			assert.Equal(t, saved.ID, st.WorkflowID)
		}
		assert.Less(t, saved.Statuses[0].Position, saved.Statuses[1].Position, "submitted order becomes the position")
		require.Len(t, saved.Transitions, 2)
		assert.Equal(t, saved.Statuses[0].ID, saved.Transitions[0].FromStatusID)
		assert.Equal(t, saved.Statuses[1].ID, saved.Transitions[0].ToStatusID)
		assert.Equal(t, []string{"due_date", "cf_2"}, saved.Transitions[0].RequiredFields, "duplicates are dropped")
		assert.Equal(t, []int64{1}, saved.Transitions[0].TeamIDs)
		assert.Equal(t, WorkflowTransitionPermissionWrite, saved.Transitions[0].MinPermission, "write is the default")
		assert.True(t, saved.Transitions[1].RequireComment)

		again := readWorkflow(t, 1)
		assert.True(t, again.RestrictTransitions)
		assert.Len(t, again.Statuses, 4)
		assert.Len(t, again.Transitions, 2)
		events.AssertDispatched(t, &WorkflowCreatedEvent{})
	})

	invalidCases := []struct {
		name   string
		mutate func(wf *Workflow)
	}{
		{"no statuses", func(wf *Workflow) { wf.Statuses = nil }},
		{"no initial status", func(wf *Workflow) { wf.Statuses[0].IsInitial = false }},
		{"two initial statuses", func(wf *Workflow) { wf.Statuses[1].IsInitial = true }},
		{"no done status", func(wf *Workflow) { wf.Statuses[2].Category = WorkflowCategoryInProgress }},
		{"only a cancelled status counts as no done status", func(wf *Workflow) {
			wf.Statuses[2].Category = WorkflowCategoryCancelled
		}},
		{"unknown category", func(wf *Workflow) { wf.Statuses[1].Category = "paused" }},
		{"empty title", func(wf *Workflow) { wf.Statuses[1].Title = "  " }},
		{"bad color", func(wf *Workflow) { wf.Statuses[1].Color = "red" }},
		{"duplicate status ids", func(wf *Workflow) { wf.Statuses[1].ID = -1 }},
		{"transition to unknown status", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: 999}}
		}},
		{"transition to a status of another workflow", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: 1}}
		}},
		{"transition to itself", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -1}}
		}},
		{"duplicate pair", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{
				{FromStatusID: -1, ToStatusID: -2},
				{FromStatusID: -1, ToStatusID: -2},
			}
		}},
		{"bad min permission", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -2, MinPermission: "owner"}}
		}},
		{"unknown required field", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -2, RequiredFields: []string{"priority"}}}
		}},
		{"malformed custom field name", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -2, RequiredFields: []string{"cf_x"}}}
		}},
		{"custom field of another project", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -2, RequiredFields: []string{"cf_5"}}}
		}},
		{"unknown custom field", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -2, RequiredFields: []string{"cf_999"}}}
		}},
		{"unknown team", func(wf *Workflow) {
			wf.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -2, TeamIDs: []int64{9999}}}
		}},
		{"move_tasks_to not in the document", func(wf *Workflow) { wf.MoveTasksTo = 9999 }},
	}
	for _, tc := range invalidCases {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			doc := baseWorkflowDoc(1)
			tc.mutate(doc)
			_, err := saveWorkflow(t, doc, cfUser1)
			require.Error(t, err)
			assert.True(t,
				IsErrWorkflowInvalidDefinition(err) || IsErrWorkflowStatusDoesNotExist(err),
				"unexpected error %T: %v", err, err)
			db.AssertCount(t, "workflows", builder.Eq{"project_id": 1}, 0)
		})
	}

	t.Run("accepts known built-in names and custom fields of the project", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		doc := baseWorkflowDoc(1)
		doc.Transitions = []*WorkflowTransition{{
			FromStatusID: -1,
			ToStatusID:   -2,
			RequiredFields: []string{
				"assignees", "due_date", "start_date", "end_date", "description", "attachments", "labels", "cf_1", "cf_4",
			},
		}}
		saved := mustSaveWorkflow(t, doc, cfUser1)
		assert.Len(t, saved.Transitions[0].RequiredFields, 9)
	})

	t.Run("a failing document changes nothing", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		doc := baseWorkflowDoc(11)
		doc.Statuses = doc.Statuses[:2]
		_, err := saveWorkflow(t, doc, cfUser1)
		require.Error(t, err)
		// Still the fixture workflow.
		db.AssertCount(t, "workflow_statuses", builder.Eq{"workflow_id": 1}, 6)
		db.AssertCount(t, "workflow_transitions", builder.Eq{"workflow_id": 1}, 3)
	})

	t.Run("replace keeps ids, updates in place and dispatches updated", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		events.ClearDispatchedEvents()

		wf := readWorkflow(t, 11)
		require.Len(t, wf.Statuses, 6)
		wf.RestrictTransitions = true
		wf.Statuses[1].Title = "In progress"
		wf.Statuses[1].Color = "#112233"
		// The archived status stays in the echoed document.
		saved := mustSaveWorkflow(t, wf, cfUser1)

		assert.Equal(t, int64(1), saved.ID)
		assert.True(t, saved.RestrictTransitions)
		assert.Equal(t, "In progress", saved.Statuses[1].Title)
		assert.Equal(t, int64(2), saved.Statuses[1].ID)
		assert.True(t, saved.Statuses[5].Archived)
		require.Len(t, saved.Transitions, 3)
		assert.Equal(t, int64(1), saved.Transitions[0].ID, "unchanged pairs keep their transition id")
		assert.Equal(t, []string{"due_date", "assignees"}, saved.Transitions[0].RequiredFields)
		assert.Equal(t, []int64{9}, saved.Transitions[2].TeamIDs)
		events.AssertDispatched(t, &WorkflowUpdatedEvent{})
		db.AssertCount(t, "workflows", builder.Eq{"project_id": 11}, 1)
	})

	t.Run("replace swaps transitions without tripping the unique pair", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		wf := readWorkflow(t, 11)
		wf.Transitions = []*WorkflowTransition{
			{ID: 1, FromStatusID: 3, ToStatusID: 2, Title: "Back"},
			{ID: 2, FromStatusID: 2, ToStatusID: 3, Title: "Forth"},
		}
		saved := mustSaveWorkflow(t, wf, cfUser1)
		require.Len(t, saved.Transitions, 2)
		assert.Equal(t, "Back", saved.Transitions[0].Title)
		assert.Empty(t, saved.Transitions[0].RequiredFields)
		db.AssertCount(t, "workflow_transition_required_fields", builder.Eq{"transition_id": 1}, 0)
		db.AssertCount(t, "workflow_transition_teams", builder.Eq{"transition_id": 3}, 0)
	})

	t.Run("replace with an unknown status id is rejected", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		wf := readWorkflow(t, 11)
		wf.Statuses[0].ID = 777
		_, err := saveWorkflow(t, wf, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowStatusDoesNotExist(err))
	})

	t.Run("an archived status cannot be made initial or used in transitions", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		wf := readWorkflow(t, 11)
		wf.Statuses[5].IsInitial = true
		_, err := saveWorkflow(t, wf, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowInvalidDefinition(err))

		wf = readWorkflow(t, 11)
		wf.Transitions = append(wf.Transitions, &WorkflowTransition{FromStatusID: 1, ToStatusID: 6})
		_, err = saveWorkflow(t, wf, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowInvalidDefinition(err))
	})
}

func TestWorkflow_RemovingStatuses(t *testing.T) {
	t.Run("a status without tasks or history is deleted", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		wf := readWorkflow(t, 11)
		// Doing and Review have no tasks and no history; their transitions go with them.
		wf.Statuses = []*WorkflowStatus{wf.Statuses[0], wf.Statuses[3], wf.Statuses[4], wf.Statuses[5]}
		wf.Transitions = []*WorkflowTransition{wf.Transitions[2]}
		mustSaveWorkflow(t, wf, cfUser1)

		db.AssertMissing(t, "workflow_statuses", map[string]interface{}{"id": 2})
		db.AssertMissing(t, "workflow_statuses", map[string]interface{}{"id": 3})
		db.AssertMissing(t, "workflow_transitions", map[string]interface{}{"id": 1})
		db.AssertCount(t, "workflow_transition_required_fields", builder.Eq{"transition_id": 1}, 0)
	})

	t.Run("a status that still has tasks needs move_tasks_to", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		wf := readWorkflow(t, 11)
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		_, err := saveWorkflow(t, wf, cfUser1)
		require.Error(t, err)
		moveErr, ok := err.(ErrWorkflowMoveTasksRequired)
		require.True(t, ok, "got %T: %v", err, err)
		assert.Equal(t, []int64{1}, moveErr.StatusIDs)
		assert.Equal(t, int64(1), taskStatusOf(t, 20).StatusID, "nothing changed")
		db.AssertExists(t, "workflow_statuses", map[string]interface{}{"id": 1}, false)
	})

	t.Run("tasks move to the target and a status with history is archived", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		wf := readWorkflow(t, 11)
		// Todo goes away. It has the task and history, Doing takes over as initial status.
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		wf.MoveTasksTo = 2
		saved := mustSaveWorkflow(t, wf, cfUser1)

		assert.Equal(t, int64(2), taskStatusOf(t, 20).StatusID)
		var archivedTodo *WorkflowStatus
		for _, st := range saved.Statuses {
			if st.ID == 1 {
				archivedTodo = st
			}
		}
		require.NotNil(t, archivedTodo, "the status stays readable")
		assert.True(t, archivedTodo.Archived)
		assert.False(t, archivedTodo.IsInitial)
		// The history keeps pointing at it with the stored titles.
		history := taskHistoryOf(t, 20)
		assert.Equal(t, "Todo", history[1].ToTitle)
	})

	t.Run("move target may be a new status and the done flag follows the category", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		// Task 20 goes to a done status first.
		_, err := changeTaskStatusAs(t, 20, 4, "", cfUser1)
		require.NoError(t, err)
		require.True(t, taskDone(t, 20))

		wf := readWorkflow(t, 11)
		// Done is replaced by a new in-progress status that receives the task.
		wf.Statuses = []*WorkflowStatus{
			wf.Statuses[0],
			wf.Statuses[1],
			{ID: -7, Title: "Finished", Category: WorkflowCategoryDone},
			{ID: -8, Title: "Parked", Category: WorkflowCategoryOpen},
		}
		wf.Transitions = nil
		wf.MoveTasksTo = -8
		saved := mustSaveWorkflow(t, wf, cfUser1)

		parked := statusIDByTitle(saved, "Parked")
		require.NotZero(t, parked)
		assert.Equal(t, parked, taskStatusOf(t, 20).StatusID)
		assert.False(t, taskDone(t, 20), "leaving a done category clears done")
	})

	t.Run("move_tasks_to must not be archived", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		wf := readWorkflow(t, 11)
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		wf.MoveTasksTo = 6
		_, err := saveWorkflow(t, wf, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowInvalidDefinition(err))
	})
}

func TestWorkflow_InitialAssignmentOnEnable(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	tasks := []*Task{}
	s := db.NewSession()
	require.NoError(t, s.Unscoped().Where("project_id = ?", 1).Find(&tasks))
	s.Close()
	require.Greater(t, len(tasks), 5)

	doneCount := 0
	for _, task := range tasks {
		if task.Done {
			doneCount++
		}
	}
	require.Positive(t, doneCount, "fixture project 1 has done tasks")

	doc := baseWorkflowDoc(1)
	// A second done status further down: the first by position wins.
	doc.Statuses = append(doc.Statuses, &WorkflowStatus{ID: -9, Title: "Shipped", Category: WorkflowCategoryDone})
	saved := mustSaveWorkflow(t, doc, cfUser1)

	todo := statusIDByTitle(saved, "Todo")
	done := statusIDByTitle(saved, "Done")
	db.AssertCount(t, "task_statuses", builder.Eq{"workflow_id": saved.ID}, int64(len(tasks)))
	db.AssertCount(t, "task_statuses", builder.Eq{"workflow_id": saved.ID, "status_id": done}, int64(doneCount))
	db.AssertCount(t, "task_statuses", builder.Eq{"workflow_id": saved.ID, "status_id": todo}, int64(len(tasks)-doneCount))
	// No per-task history for the bulk assignment.
	db.AssertCount(t, "task_status_history", builder.In("task_id", 1, 2, 3), 0)
}

func TestWorkflow_Delete(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	events.ClearDispatchedEvents()

	s := db.NewSession()
	defer s.Close()
	wf := &Workflow{ProjectID: 11}
	can, err := wf.CanDelete(s, cfUser1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, wf.Delete(s, cfUser1))
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)

	db.AssertCount(t, "workflows", builder.Eq{"project_id": 11}, 0)
	db.AssertCount(t, "workflow_statuses", builder.Eq{"workflow_id": 1}, 0)
	db.AssertCount(t, "workflow_transitions", builder.Eq{"workflow_id": 1}, 0)
	db.AssertCount(t, "workflow_transition_required_fields", builder.Eq{"transition_id": 1}, 0)
	db.AssertCount(t, "workflow_transition_teams", builder.Eq{"transition_id": 3}, 0)
	db.AssertCount(t, "task_statuses", builder.Eq{"task_id": 20}, 0)
	// History survives with its titles.
	history := taskHistoryOf(t, 20)
	require.Len(t, history, 2)
	assert.Equal(t, "Todo", history[1].ToTitle)
	assert.Equal(t, "Legacy", history[1].FromTitle)
	events.AssertDispatched(t, &WorkflowDeletedEvent{})

	s2 := db.NewSession()
	defer s2.Close()
	_, err = (&Workflow{ProjectID: 11}).CanDelete(s2, cfUser1)
	require.Error(t, err)
	assert.True(t, IsErrWorkflowDoesNotExist(err))
}

func TestWorkflow_Permissions(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	user1Admin := cfUser1 // admin on project 11
	stranger := cfUser2
	readerOfNine := cfUser1 // read on project 9

	t.Run("admin may read and replace", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		wf := &Workflow{ProjectID: 11}
		can, _, err := wf.CanRead(s, user1Admin)
		require.NoError(t, err)
		assert.True(t, can)
		can, err = wf.CanUpdate(s, user1Admin)
		require.NoError(t, err)
		assert.True(t, can)
		can, err = wf.CanDelete(s, user1Admin)
		require.NoError(t, err)
		assert.True(t, can)
	})
	t.Run("a stranger can do nothing", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		wf := &Workflow{ProjectID: 11}
		can, _, err := wf.CanRead(s, stranger)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = wf.CanUpdate(s, stranger)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = wf.CanDelete(s, stranger)
		require.NoError(t, err)
		assert.False(t, can)
	})
	t.Run("read access alone does not change the definition", func(t *testing.T) {
		// Project 9: user1 reads. It has no workflow, so create one as its owner first.
		db.LoadAndAssertFixtures(t)
		mustSaveWorkflow(t, baseWorkflowDoc(9), wfOwner6)

		s2 := db.NewSession()
		defer s2.Close()
		wf := &Workflow{ProjectID: 9}
		can, _, err := wf.CanRead(s2, readerOfNine)
		require.NoError(t, err)
		assert.True(t, can)
		can, err = wf.CanUpdate(s2, readerOfNine)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = wf.CanDelete(s2, readerOfNine)
		require.NoError(t, err)
		assert.False(t, can)
	})
	t.Run("write access alone does not change the definition", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		wf := &Workflow{ProjectID: 10}
		can, err := wf.CanUpdate(s, cfUser1)
		require.NoError(t, err)
		assert.False(t, can)
	})
	t.Run("a missing workflow reads as not found", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		wf := &Workflow{ProjectID: 1}
		_, _, err := wf.CanRead(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowDoesNotExist(err))
	})
	t.Run("link shares follow their permission", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s2 := db.NewSession()
		defer s2.Close()

		admin := &LinkSharing{ID: 99, ProjectID: 11, Permission: PermissionAdmin}
		can, err := (&Workflow{ProjectID: 11}).CanUpdate(s2, admin)
		require.NoError(t, err)
		assert.True(t, can)

		write := &LinkSharing{ID: 98, ProjectID: 11, Permission: PermissionWrite}
		can, err = (&Workflow{ProjectID: 11}).CanUpdate(s2, write)
		require.NoError(t, err)
		assert.False(t, can)
		can, _, err = (&Workflow{ProjectID: 11}).CanRead(s2, write)
		require.NoError(t, err)
		assert.True(t, can)

		other := &LinkSharing{ID: 97, ProjectID: 1, Permission: PermissionAdmin}
		can, _, err = (&Workflow{ProjectID: 11}).CanRead(s2, other)
		require.NoError(t, err)
		assert.False(t, can, "a share of another project reads nothing")
	})
}

func TestWorkflow_TransitionChecks(t *testing.T) {
	// Project 10: user6 owns it, user1 has write access. Each case builds its own workflow there.
	setup := func(t *testing.T, restrict bool, transitions ...*WorkflowTransition) (wf *Workflow, task *Task) {
		t.Helper()
		db.LoadAndAssertFixtures(t)
		doc := baseWorkflowDoc(10)
		doc.RestrictTransitions = restrict
		doc.Transitions = transitions
		wf = mustSaveWorkflow(t, doc, wfOwner6)
		task = createWorkflowTask(t, 10, &Task{Title: "checked", ProjectID: 10})
		return wf, task
	}
	ids := func(wf *Workflow) (todo, doing, done, cancelled int64) {
		return statusIDByTitle(wf, "Todo"), statusIDByTitle(wf, "Doing"), statusIDByTitle(wf, "Done"), statusIDByTitle(wf, "Cancelled")
	}
	pair := func(from, to int, fn func(tr *WorkflowTransition)) *WorkflowTransition {
		tr := &WorkflowTransition{FromStatusID: int64(from), ToStatusID: int64(to)}
		if fn != nil {
			fn(tr)
		}
		return tr
	}

	t.Run("a new task starts in the initial status with a history entry", func(t *testing.T) {
		wf, task := setup(t, false)
		todo, _, _, _ := ids(wf)
		row := taskStatusOf(t, task.ID)
		require.NotNil(t, row)
		assert.Equal(t, todo, row.StatusID)
		assert.Equal(t, wf.ID, row.WorkflowID)
		history := taskHistoryOf(t, task.ID)
		require.Len(t, history, 1)
		assert.Nil(t, history[0].FromStatusID)
		assert.Equal(t, "Todo", history[0].ToTitle)
		assert.Equal(t, int64(6), history[0].DoerID)
	})

	t.Run("a task created done starts in the first done status", func(t *testing.T) {
		wf, _ := setup(t, false)
		_, _, done, _ := ids(wf)
		doneTask := createWorkflowTask(t, 10, &Task{Title: "already done", ProjectID: 10, Done: true})
		assert.Equal(t, done, taskStatusOf(t, doneTask.ID).StatusID)
	})

	t.Run("unrestricted workflows allow any change", func(t *testing.T) {
		wf, task := setup(t, false)
		_, doing, done, cancelled := ids(wf)
		for _, target := range []int64{done, doing, cancelled} {
			res, err := changeTaskStatusAs(t, task.ID, target, "", cfUser1)
			require.NoError(t, err)
			assert.Equal(t, target, res.Status.ID)
			assert.Equal(t, target, taskStatusOf(t, task.ID).StatusID)
		}
	})

	t.Run("a change to the current status is a no-op", func(t *testing.T) {
		wf, task := setup(t, false)
		todo, _, _, _ := ids(wf)
		_, err := changeTaskStatusAs(t, task.ID, todo, "", cfUser1)
		require.NoError(t, err)
		assert.Len(t, taskHistoryOf(t, task.ID), 1)
	})

	t.Run("restricted workflows only allow listed pairs", func(t *testing.T) {
		wf, task := setup(t, true, pair(-1, -2, nil))
		_, doing, done, _ := ids(wf)

		_, err := changeTaskStatusAs(t, task.ID, done, "", cfUser1)
		require.Error(t, err)
		notAllowed, ok := err.(ErrWorkflowTransitionNotAllowed)
		require.True(t, ok, "got %T", err)
		assert.Equal(t, done, notAllowed.ToStatusID)
		assert.False(t, taskDone(t, task.ID))

		_, err = changeTaskStatusAs(t, task.ID, doing, "", cfUser1)
		require.NoError(t, err)
		// The listed pair is one way.
		_, err = changeTaskStatusAs(t, task.ID, statusIDByTitle(wf, "Todo"), "", cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowTransitionNotAllowed(err))
	})

	t.Run("unrestricted workflows apply the rules of listed pairs only", func(t *testing.T) {
		wf, task := setup(t, false, pair(-1, -2, func(tr *WorkflowTransition) { tr.RequireComment = true }))
		_, doing, done, _ := ids(wf)
		_, err := changeTaskStatusAs(t, task.ID, done, "", cfUser1)
		require.NoError(t, err, "unlisted pairs have no rules")
		task2 := createWorkflowTask(t, 10, &Task{Title: "second", ProjectID: 10})
		_, err = changeTaskStatusAs(t, task2.ID, doing, "", cfUser1)
		require.Error(t, err)
		assert.True(t, requireRequirements(t, err).CommentRequired)
	})

	t.Run("an admin transition needs more than write access", func(t *testing.T) {
		wf, task := setup(t, false, pair(-1, -2, func(tr *WorkflowTransition) {
			tr.MinPermission = WorkflowTransitionPermissionAdmin
		}))
		_, doing, _, _ := ids(wf)

		_, err := changeTaskStatusAs(t, task.ID, doing, "", cfUser1)
		require.Error(t, err)
		forbidden, ok := err.(ErrWorkflowTransitionForbidden)
		require.True(t, ok, "got %T: %v", err, err)
		assert.Equal(t, workflowForbiddenPermission, forbidden.Reason)
		assert.Len(t, taskHistoryOf(t, task.ID), 1)

		_, err = changeTaskStatusAs(t, task.ID, doing, "", wfOwner6)
		require.NoError(t, err)
	})

	t.Run("a team limits the transition to its members and project admins", func(t *testing.T) {
		wf, task := setup(t, false, pair(-1, -2, func(tr *WorkflowTransition) { tr.TeamIDs = []int64{9} }))
		_, doing, _, _ := ids(wf)

		// user1 has write access but is not in team 9.
		_, err := changeTaskStatusAs(t, task.ID, doing, "", cfUser1)
		require.Error(t, err)
		forbidden, ok := err.(ErrWorkflowTransitionForbidden)
		require.True(t, ok, "got %T: %v", err, err)
		assert.Equal(t, workflowForbiddenTeam, forbidden.Reason)

		// The project owner is an admin and passes.
		_, err = changeTaskStatusAs(t, task.ID, doing, "", wfOwner6)
		require.NoError(t, err)
	})

	t.Run("a member of an allowed team passes", func(t *testing.T) {
		wf, task := setup(t, false, pair(-1, -2, func(tr *WorkflowTransition) { tr.TeamIDs = []int64{9, 1} }))
		_, doing, _, _ := ids(wf)
		_, err := changeTaskStatusAs(t, task.ID, doing, "", cfUser1)
		require.NoError(t, err)
	})

	t.Run("a link share is no team member", func(t *testing.T) {
		wf, task := setup(t, false, pair(-1, -2, func(tr *WorkflowTransition) { tr.TeamIDs = []int64{1} }))
		_, doing, _, _ := ids(wf)
		share := &LinkSharing{ID: 5, ProjectID: 10, Permission: PermissionWrite}
		_, err := changeTaskStatusAs(t, task.ID, doing, "", share)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowTransitionForbidden(err))

		share.Permission = PermissionAdmin
		_, err = changeTaskStatusAs(t, task.ID, doing, "", share)
		require.NoError(t, err)
		history := taskHistoryOf(t, task.ID)
		assert.Equal(t, int64(0), history[len(history)-1].DoerID, "a share id is not a user id")
	})

	t.Run("a reader cannot change a status", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		doc := baseWorkflowDoc(9)
		wf := mustSaveWorkflow(t, doc, wfOwner6)
		_, err := changeTaskStatusAs(t, 18, statusIDByTitle(wf, "Doing"), "", cfUser1)
		require.Error(t, err)
		assert.True(t, err == ErrGenericForbidden{} || IsErrGenericForbidden(err), "got %T: %v", err, err)
	})

	requiredFieldCases := []struct {
		field   string
		satisfy func(t *testing.T, taskID int64)
	}{
		{WorkflowFieldDueDate, func(t *testing.T, taskID int64) {
			setTaskColumns(t, taskID, &Task{DueDate: time.Now().Add(time.Hour)}, "due_date")
		}},
		{WorkflowFieldStartDate, func(t *testing.T, taskID int64) {
			setTaskColumns(t, taskID, &Task{StartDate: time.Now()}, "start_date")
		}},
		{WorkflowFieldEndDate, func(t *testing.T, taskID int64) {
			setTaskColumns(t, taskID, &Task{EndDate: time.Now().Add(time.Hour)}, "end_date")
		}},
		{WorkflowFieldDescription, func(t *testing.T, taskID int64) {
			setTaskColumns(t, taskID, &Task{Description: "<p>Do the thing</p>"}, "description")
		}},
		{WorkflowFieldAssignees, func(t *testing.T, taskID int64) {
			insertRow(t, &TaskAssginee{TaskID: taskID, UserID: 1})
		}},
		{WorkflowFieldLabels, func(t *testing.T, taskID int64) {
			insertRow(t, &LabelTask{TaskID: taskID, LabelID: 1})
		}},
		{WorkflowFieldAttachments, func(t *testing.T, taskID int64) {
			insertRow(t, &TaskAttachment{TaskID: taskID, FileID: 1})
		}},
		{"cf_1", func(t *testing.T, taskID int64) {
			_, err := setCustomFieldValue(t, taskID, 1, "REF-9", cfUser1)
			require.NoError(t, err)
		}},
		{"cf_4", func(t *testing.T, taskID int64) {
			_, err := setCustomFieldValue(t, taskID, 4, []any{float64(3)}, cfUser1)
			require.NoError(t, err)
		}},
	}
	for _, tc := range requiredFieldCases {
		t.Run("required "+tc.field, func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			files.InitTestFileFixtures(t)
			doc := baseWorkflowDoc(1)
			doc.Transitions = []*WorkflowTransition{pair(-1, -2, func(tr *WorkflowTransition) {
				tr.RequiredFields = []string{tc.field}
			})}
			wf := mustSaveWorkflow(t, doc, cfUser1)
			doing := statusIDByTitle(wf, "Doing")
			// Task 10 has no dates, description, assignees, labels, attachments or custom field values.
			taskID := int64(10)

			_, err := changeTaskStatusAs(t, taskID, doing, "", cfUser1)
			req := requireRequirements(t, err)
			assert.Equal(t, []string{tc.field}, req.MissingFields)
			assert.False(t, req.CommentRequired)
			assert.Equal(t, taskID, req.TaskID)
			assert.Equal(t, doing, req.ToStatusID)
			assert.Equal(t, statusIDByTitle(wf, "Todo"), req.FromStatusID)
			assert.Equal(t, statusIDByTitle(wf, "Todo"), taskStatusOf(t, taskID).StatusID)

			tc.satisfy(t, taskID)
			_, err = changeTaskStatusAs(t, taskID, doing, "", cfUser1)
			require.NoError(t, err)
			assert.Equal(t, doing, taskStatusOf(t, taskID).StatusID)
		})
	}

	t.Run("an empty custom field value does not count", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		doc := baseWorkflowDoc(1)
		doc.Transitions = []*WorkflowTransition{pair(-1, -2, func(tr *WorkflowTransition) {
			tr.RequiredFields = []string{"cf_4"}
		})}
		wf := mustSaveWorkflow(t, doc, cfUser1)
		doing := statusIDByTitle(wf, "Doing")
		_, err := setCustomFieldValue(t, 10, 4, []any{}, cfUser1)
		require.NoError(t, err)
		_, err = changeTaskStatusAs(t, 10, doing, "", cfUser1)
		assert.Equal(t, []string{"cf_4"}, requireRequirements(t, err).MissingFields)

		// Task 1 already has values for field 4.
		_, err = changeTaskStatusAs(t, 1, doing, "", cfUser1)
		require.NoError(t, err)
	})

	t.Run("a required comment must not be blank", func(t *testing.T) {
		wf, task := setup(t, false, pair(-1, -2, func(tr *WorkflowTransition) { tr.RequireComment = true }))
		_, doing, _, _ := ids(wf)
		for _, blank := range []string{"", "   \n"} {
			_, err := changeTaskStatusAs(t, task.ID, doing, blank, cfUser1)
			req := requireRequirements(t, err)
			assert.True(t, req.CommentRequired)
			assert.Empty(t, req.MissingFields)
		}
		_, err := changeTaskStatusAs(t, task.ID, doing, "Looks good to start", cfUser1)
		require.NoError(t, err)
	})

	t.Run("missing fields and the comment are reported together", func(t *testing.T) {
		wf, task := setup(t, false, pair(-1, -2, func(tr *WorkflowTransition) {
			tr.RequireComment = true
			tr.RequiredFields = []string{"due_date", "assignees"}
		}))
		_, doing, _, _ := ids(wf)
		_, err := changeTaskStatusAs(t, task.ID, doing, "", cfUser1)
		req := requireRequirements(t, err)
		assert.Equal(t, []string{"due_date", "assignees"}, req.MissingFields)
		assert.True(t, req.CommentRequired)
		http := req.HTTPError()
		assert.Equal(t, 422, http.HTTPCode)
		assert.Equal(t, ErrCodeWorkflowRequirementsNotMet, http.Code)
		assert.Equal(t, req.WorkflowRequirementsDetails, http.Details)
	})

	t.Run("requirements are checked after the permission", func(t *testing.T) {
		wf, task := setup(t, false, pair(-1, -2, func(tr *WorkflowTransition) {
			tr.MinPermission = WorkflowTransitionPermissionAdmin
			tr.RequiredFields = []string{"due_date"}
		}))
		_, doing, _, _ := ids(wf)
		_, err := changeTaskStatusAs(t, task.ID, doing, "", cfUser1)
		assert.True(t, IsErrWorkflowTransitionForbidden(err), "got %T: %v", err, err)
	})

	t.Run("the target must exist, belong to the workflow and not be archived", func(t *testing.T) {
		wf, task := setup(t, false)
		_ = wf
		_, err := changeTaskStatusAs(t, task.ID, 9999, "", cfUser1)
		assert.True(t, IsErrWorkflowStatusDoesNotExist(err), "got %T", err)
		// Status 2 belongs to the workflow of project 11.
		_, err = changeTaskStatusAs(t, task.ID, 2, "", cfUser1)
		assert.True(t, IsErrWorkflowStatusDoesNotExist(err), "got %T", err)
		// Status 6 is archived (fixture workflow), checked on project 11's task.
		_, err = changeTaskStatusAs(t, 20, 6, "", cfUser1)
		assert.True(t, IsErrWorkflowStatusArchived(err), "got %T", err)
	})

	t.Run("a project without a workflow has no status", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		_, err := changeTaskStatusAs(t, 1, 1, "", cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowDoesNotExist(err))
	})
}

func setTaskColumns(t *testing.T, taskID int64, values *Task, cols ...string) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	_, err := s.ID(taskID).Cols(cols...).Update(values)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
}

func insertRow(t *testing.T, bean any) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	_, err := s.Insert(bean)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
}

func TestWorkflow_ChangeRecording(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	events.ClearDispatchedEvents()

	// Fixture task 20: Todo(1). The pair 2 to 3 needs due date and assignee, 3 to 4 an admin and a comment.
	res, err := changeTaskStatusAs(t, 20, 2, "starting", cfUser1)
	require.NoError(t, err)
	require.NotNil(t, res.FromStatusID)
	assert.Equal(t, int64(1), *res.FromStatusID)
	assert.False(t, res.Done)

	history := taskHistoryOf(t, 20)
	require.Len(t, history, 3)
	last := history[2]
	require.NotNil(t, last.FromStatusID)
	assert.Equal(t, int64(1), *last.FromStatusID)
	assert.Equal(t, int64(2), last.ToStatusID)
	assert.Equal(t, "Todo", last.FromTitle)
	assert.Equal(t, "Doing", last.ToTitle)
	assert.Equal(t, "starting", last.Comment)
	assert.Equal(t, int64(1), last.DoerID)
	assert.WithinDuration(t, time.Now(), last.Created, time.Minute)
	assert.Equal(t, int64(2), taskStatusOf(t, 20).StatusID)

	events.AssertDispatched(t, &TaskStatusChangedEvent{})
	dispatched := events.GetDispatchedEvents("task.status.changed")
	require.Len(t, dispatched, 1)
	ev := dispatched[0].(*TaskStatusChangedEvent)
	assert.Equal(t, int64(20), ev.Task.ID)
	assert.Equal(t, "Todo", ev.FromStatus.Title)
	assert.Equal(t, "Doing", ev.ToStatus.Title)
	assert.Equal(t, "starting", ev.Comment)
	assert.Equal(t, int64(1), ev.Doer.ID)

	// The fixture rules: Doing to Review needs a due date and an assignee.
	_, err = changeTaskStatusAs(t, 20, 3, "", cfUser1)
	assert.Equal(t, []string{"due_date", "assignees"}, requireRequirements(t, err).MissingFields)
	assert.Len(t, taskHistoryOf(t, 20), 3, "a rejected change leaves no history")
	assert.Equal(t, 1, events.CountDispatchedEvents("task.status.changed"), "and no event")

	setTaskColumns(t, 20, &Task{DueDate: time.Now().Add(time.Hour)}, "due_date")
	insertRow(t, &TaskAssginee{TaskID: 20, UserID: 1})
	_, err = changeTaskStatusAs(t, 20, 3, "", cfUser1)
	require.NoError(t, err)

	// Review to Done: admin only (user1 is admin on project 11) and a comment.
	_, err = changeTaskStatusAs(t, 20, 4, "", cfUser1)
	assert.True(t, requireRequirements(t, err).CommentRequired)
	res, err = changeTaskStatusAs(t, 20, 4, "approved", cfUser1)
	require.NoError(t, err)
	assert.True(t, res.Done)
	assert.True(t, taskDone(t, 20), "entering a done status marks the task done")

	// Leaving done clears the flag again, also into a cancelled one.
	_, err = changeTaskStatusAs(t, 20, 5, "", cfUser1)
	require.NoError(t, err)
	assert.True(t, taskDone(t, 20), "cancelled counts as done")
	_, err = changeTaskStatusAs(t, 20, 2, "", cfUser1)
	require.NoError(t, err)
	assert.False(t, taskDone(t, 20))
}

func TestWorkflow_AvailableTransitions(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	read := func(a web.Auth) *TaskWorkflowStatus {
		s := db.NewSession()
		defer s.Close()
		st := &TaskWorkflowStatus{TaskID: 20}
		can, _, err := st.CanRead(s, a)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, st.ReadOne(s, a))
		return st
	}

	st := read(cfUser1)
	require.NotNil(t, st.Status)
	assert.Equal(t, "Todo", st.Status.Title)
	assert.Equal(t, int64(1), st.WorkflowID)
	require.NotNil(t, st.Entered)
	// Unrestricted: everything but the current and the archived status. Dropped (5) is limited to team 9; admin passes.
	titles := []string{}
	for _, tr := range st.Transitions {
		titles = append(titles, tr.ToStatus.Title)
	}
	assert.Equal(t, []string{"Doing", "Review", "Done", "Dropped"}, titles)
	for _, tr := range st.Transitions {
		if tr.ToStatus.Title == "Doing" {
			assert.Empty(t, tr.MissingFields)
		}
	}

	// From Doing the listed Review pair shows its unmet requirements and its label.
	_, err := changeTaskStatusAs(t, 20, 2, "", cfUser1)
	require.NoError(t, err)
	st = read(cfUser1)
	for _, tr := range st.Transitions {
		if tr.ToStatus.Title == "Review" {
			assert.Equal(t, "Send to review", tr.Title)
			assert.Equal(t, []string{"due_date", "assignees"}, tr.MissingFields)
		}
	}

	// Restricted: only the listed pairs from Doing.
	wf := readWorkflow(t, 11)
	wf.RestrictTransitions = true
	mustSaveWorkflow(t, wf, cfUser1)
	st = read(cfUser1)
	require.Len(t, st.Transitions, 1)
	assert.Equal(t, "Review", st.Transitions[0].ToStatus.Title)
}

func TestWorkflow_DoneSync(t *testing.T) {
	// Project 1: owned by user1.
	enable := func(t *testing.T, transitions ...*WorkflowTransition) *Workflow {
		t.Helper()
		db.LoadAndAssertFixtures(t)
		doc := baseWorkflowDoc(1)
		doc.Transitions = transitions
		return mustSaveWorkflow(t, doc, cfUser1)
	}

	t.Run("marking a task done moves it to the first done status", func(t *testing.T) {
		wf := enable(t)
		events.ClearDispatchedEvents()
		task := &Task{ID: 1, Title: "task #1", Done: true}
		require.NoError(t, updateTaskAs(t, task, cfUser1))

		assert.True(t, taskDone(t, 1))
		assert.Equal(t, statusIDByTitle(wf, "Done"), taskStatusOf(t, 1).StatusID)
		history := taskHistoryOf(t, 1)
		require.Len(t, history, 1, "enabling assigns without history, the done change adds one")
		assert.Equal(t, "Done", history[0].ToTitle)
		assert.Equal(t, 1, events.CountDispatchedEvents("task.status.changed"))
		assert.Equal(t, 1, events.CountDispatchedEvents("task.updated"))
	})

	t.Run("un-doing moves the task back to the initial status", func(t *testing.T) {
		wf := enable(t)
		// Task 2 is done and sits in the done status.
		require.Equal(t, statusIDByTitle(wf, "Done"), taskStatusOf(t, 2).StatusID)
		task := &Task{ID: 2, Title: "task #2 done", Done: false}
		require.NoError(t, updateTaskAs(t, task, cfUser1))

		assert.False(t, taskDone(t, 2))
		assert.Equal(t, statusIDByTitle(wf, "Todo"), taskStatusOf(t, 2).StatusID)
		history := taskHistoryOf(t, 2)
		require.Len(t, history, 1)
		assert.Equal(t, "Done", history[0].FromTitle)
		assert.Equal(t, "Todo", history[0].ToTitle)
	})

	t.Run("the checks apply to the checkbox", func(t *testing.T) {
		wf := enable(t, &WorkflowTransition{
			FromStatusID:   -1,
			ToStatusID:     -3,
			RequiredFields: []string{"due_date"},
			RequireComment: true,
		})
		// Task 1 has no due date.
		task := &Task{ID: 1, Title: "task #1", Done: true}
		err := updateTaskAs(t, task, cfUser1)
		req := requireRequirements(t, err)
		assert.Equal(t, []string{"due_date"}, req.MissingFields)
		assert.True(t, req.CommentRequired, "the checkbox has no comment to give")
		assert.False(t, taskDone(t, 1), "the whole update rolled back")
		assert.Equal(t, statusIDByTitle(wf, "Todo"), taskStatusOf(t, 1).StatusID)
		assert.Empty(t, taskHistoryOf(t, 1))
	})

	t.Run("fields set in the same update count", func(t *testing.T) {
		enable(t, &WorkflowTransition{FromStatusID: -1, ToStatusID: -3, RequiredFields: []string{"due_date"}})
		task := &Task{ID: 1, Title: "task #1", Done: true, DueDate: time.Now().Add(24 * time.Hour)}
		require.NoError(t, updateTaskAs(t, task, cfUser1))
		assert.True(t, taskDone(t, 1))
	})

	t.Run("a restricted workflow can refuse the checkbox", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		doc := baseWorkflowDoc(1)
		doc.RestrictTransitions = true
		doc.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -2}}
		mustSaveWorkflow(t, doc, cfUser1)
		err := updateTaskAs(t, &Task{ID: 1, Title: "task #1", Done: true}, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowTransitionNotAllowed(err))
		assert.False(t, taskDone(t, 1))
	})

	t.Run("bulk updates go through the same path", func(t *testing.T) {
		wf := enable(t)
		s := db.NewSession()
		defer s.Close()
		_, err := updateTasks(s, cfUser1, &Task{Done: true}, []int64{1, 3}, []string{"done"})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		for _, id := range []int64{1, 3} {
			assert.True(t, taskDone(t, id))
			assert.Equal(t, statusIDByTitle(wf, "Done"), taskStatusOf(t, id).StatusID)
		}
	})

	t.Run("a bulk update that fails a check fails as a whole", func(t *testing.T) {
		enable(t, &WorkflowTransition{FromStatusID: -1, ToStatusID: -3, RequiredFields: []string{"assignees"}})
		insertRow(t, &TaskAssginee{TaskID: 1, UserID: 1})
		s := db.NewSession()
		defer s.Close()
		_, err := updateTasks(s, cfUser1, &Task{Done: true}, []int64{1, 3}, []string{"done"})
		require.Error(t, err)
		assert.True(t, IsErrWorkflowRequirementsNotMet(err))
		_ = s.Rollback()
		assert.False(t, taskDone(t, 1))
	})

	t.Run("a repeating task is checked and recorded as done, then returns to the initial status", func(t *testing.T) {
		wf := enable(t)
		due := time.Now().Add(-time.Hour)
		repeating := createWorkflowTask(t, 1, &Task{Title: "weekly", ProjectID: 1, RepeatAfter: 3600, DueDate: due})
		events.ClearDispatchedEvents()

		task := &Task{ID: repeating.ID, Title: "weekly", Done: true, RepeatAfter: 3600, DueDate: due}
		require.NoError(t, updateTaskAs(t, task, cfUser1))

		assert.False(t, taskDone(t, repeating.ID), "the task re-opened")
		assert.Equal(t, statusIDByTitle(wf, "Todo"), taskStatusOf(t, repeating.ID).StatusID)
		history := taskHistoryOf(t, repeating.ID)
		require.Len(t, history, 3, "creation, to done, back to initial")
		assert.Equal(t, "Done", history[1].ToTitle)
		assert.Equal(t, "Done", history[2].FromTitle)
		assert.Equal(t, "Todo", history[2].ToTitle)
		assert.Equal(t, 2, events.CountDispatchedEvents("task.status.changed"))

		s := db.NewSession()
		defer s.Close()
		stored, err := GetTaskByIDSimple(s, repeating.ID)
		require.NoError(t, err)
		assert.True(t, stored.DueDate.After(due), "the next occurrence is scheduled")
		assert.False(t, stored.DoneAt.IsZero(), "done_at keeps the completion")
	})

	t.Run("a repeating task still needs its requirements to be done", func(t *testing.T) {
		enable(t, &WorkflowTransition{FromStatusID: -1, ToStatusID: -3, RequiredFields: []string{"labels"}})
		repeating := createWorkflowTask(t, 1, &Task{Title: "weekly", ProjectID: 1, RepeatAfter: 3600})
		err := updateTaskAs(t, &Task{ID: repeating.ID, Title: "weekly", Done: true, RepeatAfter: 3600}, cfUser1)
		assert.True(t, IsErrWorkflowRequirementsNotMet(err), "got %T: %v", err, err)
	})

	t.Run("projects without a workflow behave as before", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		require.NoError(t, updateTaskAs(t, &Task{ID: 1, Title: "task #1", Done: true}, cfUser1))
		assert.True(t, taskDone(t, 1))
		db.AssertCount(t, "task_statuses", builder.Eq{"task_id": 1}, 0)
		db.AssertCount(t, "task_status_history", builder.Eq{"task_id": 1}, 0)
	})

	t.Run("the status endpoint re-opens a repeating task too", func(t *testing.T) {
		wf := enable(t)
		due := time.Now().Add(-time.Hour)
		repeating := createWorkflowTask(t, 1, &Task{Title: "weekly", ProjectID: 1, RepeatAfter: 3600, DueDate: due})
		res, err := changeTaskStatusAs(t, repeating.ID, statusIDByTitle(wf, "Done"), "", cfUser1)
		require.NoError(t, err)
		assert.False(t, res.Done)
		assert.Equal(t, statusIDByTitle(wf, "Todo"), res.Status.ID)
		assert.Len(t, taskHistoryOf(t, repeating.ID), 3)
		assert.False(t, taskDone(t, repeating.ID))
	})
}

func TestWorkflow_ProjectMove(t *testing.T) {
	t.Run("moving into a project without a workflow clears the status and keeps the history", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		// Task 20 lives in project 11 (admin for user1); project 10 has no workflow, user1 may write there.
		task := &Task{ID: 20, Title: "task #20", ProjectID: 10}
		require.NoError(t, updateTaskAs(t, task, cfUser1))
		assert.Nil(t, taskStatusOf(t, 20))
		assert.Len(t, taskHistoryOf(t, 20), 2)
	})

	t.Run("moving into a project with a workflow starts in its initial status", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		wf := mustSaveWorkflow(t, baseWorkflowDoc(10), wfOwner6)
		// Move task 20 to Doing first so the reset is visible.
		_, err := changeTaskStatusAs(t, 20, 2, "", cfUser1)
		require.NoError(t, err)

		require.NoError(t, updateTaskAs(t, &Task{ID: 20, Title: "task #20", ProjectID: 10}, cfUser1))
		row := taskStatusOf(t, 20)
		require.NotNil(t, row)
		assert.Equal(t, wf.ID, row.WorkflowID)
		assert.Equal(t, statusIDByTitle(wf, "Todo"), row.StatusID)
		history := taskHistoryOf(t, 20)
		assert.Len(t, history, 4, "the old entries stay, the new start is added")
		assert.Equal(t, "Doing", history[2].ToTitle)
		assert.Equal(t, "Todo", history[3].ToTitle)
	})
}

func TestWorkflow_Cleanup(t *testing.T) {
	t.Run("purging a task removes its status and history", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		require.NoError(t, hardDeleteTask(s, &Task{ID: 20, ProjectID: 11}))
		require.NoError(t, s.Commit())
		db.AssertCount(t, "task_statuses", builder.Eq{"task_id": 20}, 0)
		db.AssertCount(t, "task_status_history", builder.Eq{"task_id": 20}, 0)
	})

	t.Run("trashing a task keeps its status for the restore", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		require.NoError(t, (&Task{ID: 20}).Delete(s, cfUser1))
		require.NoError(t, s.Commit())
		db.AssertCount(t, "task_statuses", builder.Eq{"task_id": 20}, 1)
	})

	t.Run("purging a project removes its workflow", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		require.NoError(t, hardDeleteProject(s, &Project{ID: 11}, nil))
		require.NoError(t, s.Commit())

		db.AssertCount(t, "workflows", builder.Eq{"project_id": 11}, 0)
		db.AssertCount(t, "workflow_statuses", builder.Eq{"workflow_id": 1}, 0)
		db.AssertCount(t, "workflow_transitions", builder.Eq{"workflow_id": 1}, 0)
		db.AssertCount(t, "workflow_transition_required_fields", builder.In("transition_id", 1, 2, 3), 0)
		db.AssertCount(t, "workflow_transition_teams", builder.In("transition_id", 1, 2, 3), 0)
		db.AssertCount(t, "task_statuses", builder.Eq{"workflow_id": 1}, 0)
		db.AssertCount(t, "task_status_history", builder.Eq{"task_id": 20}, 0)
	})

	t.Run("deleting a custom field drops the requirement on it", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		doc := baseWorkflowDoc(1)
		doc.Transitions = []*WorkflowTransition{{FromStatusID: -1, ToStatusID: -2, RequiredFields: []string{"cf_1", "due_date"}}}
		mustSaveWorkflow(t, doc, cfUser1)

		s := db.NewSession()
		defer s.Close()
		require.NoError(t, (&CustomField{ID: 1, ProjectID: 1}).Delete(s, cfUser1))
		require.NoError(t, s.Commit())

		wf := readWorkflow(t, 1)
		assert.Equal(t, []string{"due_date"}, wf.Transitions[0].RequiredFields)
	})
}

func TestWorkflow_Duplicate(t *testing.T) {
	t.Run("duplicating a project copies the workflow with remapped ids", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		files.InitTestFileFixtures(t)

		doc := baseWorkflowDoc(1)
		doc.RestrictTransitions = true
		doc.Transitions = []*WorkflowTransition{{
			FromStatusID:   -1,
			ToStatusID:     -2,
			Title:          "Start",
			RequiredFields: []string{"cf_2"},
			TeamIDs:        []int64{1},
		}}
		source := mustSaveWorkflow(t, doc, cfUser1)
		_, err := changeTaskStatusAs(t, 1, statusIDByTitle(source, "Doing"), "", cfUser1)
		require.NoError(t, err)

		s := db.NewSession()
		defer s.Close()
		pd := &ProjectDuplicate{ProjectID: 1}
		can, err := pd.CanCreate(s, cfUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, pd.Create(s, cfUser1))
		require.NoError(t, s.Commit())

		copied := readWorkflow(t, pd.Project.ID)
		assert.NotEqual(t, source.ID, copied.ID)
		assert.True(t, copied.RestrictTransitions)
		require.Len(t, copied.Statuses, 4)
		for i, st := range copied.Statuses {
			assert.NotEqual(t, source.Statuses[i].ID, st.ID, "own status rows")
			assert.Equal(t, source.Statuses[i].Title, st.Title)
			assert.Equal(t, copied.ID, st.WorkflowID)
		}
		require.Len(t, copied.Transitions, 1)
		assert.Equal(t, statusIDByTitle(copied, "Todo"), copied.Transitions[0].FromStatusID)
		assert.Equal(t, statusIDByTitle(copied, "Doing"), copied.Transitions[0].ToStatusID)
		assert.Equal(t, []int64{1}, copied.Transitions[0].TeamIDs)

		// cf_2 now names the copied field, not the original.
		s2 := db.NewSession()
		defer s2.Close()
		result, _, _, err := (&CustomField{ProjectID: pd.Project.ID}).ReadAll(s2, cfUser1, "", 0, 0)
		require.NoError(t, err)
		copiedStageID := int64(0)
		for _, f := range result.([]*CustomField) {
			if f.Title == "Stage" {
				copiedStageID = f.ID
			}
		}
		require.NotZero(t, copiedStageID)
		assert.Equal(t, []string{workflowCustomFieldName(copiedStageID)}, copied.Transitions[0].RequiredFields)

		// Tasks keep their status in the copy: task 1 was in Doing, the done task 2 is in Done.
		copiedTasks := []*Task{}
		require.NoError(t, s2.Where("project_id = ?", pd.Project.ID).Find(&copiedTasks))
		byTitle := map[string]*Task{}
		for _, ct := range copiedTasks {
			byTitle[ct.Title] = ct
		}
		require.NotNil(t, byTitle["task #1"])
		require.NotNil(t, byTitle["task #2 done"])
		assert.Equal(t, statusIDByTitle(copied, "Doing"), taskStatusOf(t, byTitle["task #1"].ID).StatusID)
		assert.Equal(t, statusIDByTitle(copied, "Done"), taskStatusOf(t, byTitle["task #2 done"].ID).StatusID)
		// The source is untouched.
		assert.Equal(t, statusIDByTitle(source, "Doing"), taskStatusOf(t, 1).StatusID)
	})

	t.Run("a task duplicate starts in the initial status", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		files.InitTestFileFixtures(t)
		wf := mustSaveWorkflow(t, baseWorkflowDoc(1), cfUser1)
		_, err := changeTaskStatusAs(t, 1, statusIDByTitle(wf, "Doing"), "", cfUser1)
		require.NoError(t, err)

		s := db.NewSession()
		defer s.Close()
		td := &TaskDuplicate{TaskID: 1}
		require.NoError(t, td.Create(s, cfUser1))
		require.NoError(t, s.Commit())

		row := taskStatusOf(t, td.Task.ID)
		require.NotNil(t, row)
		assert.Equal(t, statusIDByTitle(wf, "Todo"), row.StatusID)
		assert.Len(t, taskHistoryOf(t, td.Task.ID), 1)
	})
}

func TestTaskStatusChange_Permissions(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	mustSaveWorkflow(t, baseWorkflowDoc(10), wfOwner6)

	t.Run("write access to the task is needed", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		can, err := (&TaskStatusChange{TaskID: 19}).CanCreate(s, cfUser1)
		require.NoError(t, err)
		assert.True(t, can)
		// Task 18 is in project 9 where user1 only reads.
		can, err = (&TaskStatusChange{TaskID: 18}).CanCreate(s, cfUser1)
		require.NoError(t, err)
		assert.False(t, can)
		// user2 has no access to project 10.
		can, err = (&TaskStatusChange{TaskID: 19}).CanCreate(s, cfUser2)
		require.NoError(t, err)
		assert.False(t, can)
	})
	t.Run("reading status and history needs read access", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		can, _, err := (&TaskWorkflowStatus{TaskID: 19}).CanRead(s, cfUser1)
		require.NoError(t, err)
		assert.True(t, can)
		can, _, err = (&TaskWorkflowStatus{TaskID: 19}).CanRead(s, cfUser2)
		require.NoError(t, err)
		assert.False(t, can)
		can, _, err = (&TaskStatusHistory{TaskID: 19}).CanRead(s, cfUser2)
		require.NoError(t, err)
		assert.False(t, can)

		_, _, _, err = (&TaskStatusHistory{TaskID: 19}).ReadAll(s, cfUser2, "", 0, 0)
		require.Error(t, err)
	})
	t.Run("history lists newest first with the doer", func(t *testing.T) {
		wf := readWorkflow(t, 10)
		_, err := changeTaskStatusAs(t, 19, statusIDByTitle(wf, "Todo"), "", cfUser1)
		require.NoError(t, err) // already there: no entry
		_, err = changeTaskStatusAs(t, 19, statusIDByTitle(wf, "Doing"), "", cfUser1)
		require.NoError(t, err)
		_, err = changeTaskStatusAs(t, 19, statusIDByTitle(wf, "Done"), "go", cfUser1)
		require.NoError(t, err)

		s2 := db.NewSession()
		defer s2.Close()
		result, count, total, err := (&TaskStatusHistory{TaskID: 19}).ReadAll(s2, cfUser1, "", 1, 50)
		require.NoError(t, err)
		entries := result.([]*TaskStatusHistory)
		require.Len(t, entries, 2)
		assert.Equal(t, 2, count)
		assert.EqualValues(t, 2, total)
		assert.Equal(t, "Done", entries[0].ToTitle)
		assert.Equal(t, "go", entries[0].Comment)
		require.NotNil(t, entries[0].Doer)
		assert.Equal(t, int64(1), entries[0].Doer.ID)
		assert.Equal(t, "Doing", entries[1].ToTitle)
	})
}

func TestWorkflow_KanbanDoneBucket(t *testing.T) {
	// View 44 is the manual kanban view of project 11 (buckets 11 and 27); task 20 sits in bucket 11.
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
		_, err := s.Where("task_id = ? AND project_view_id = 44", taskID).Get(tb)
		require.NoError(t, err)
		return tb.BucketID
	}

	t.Run("dragging into the done bucket changes the status", func(t *testing.T) {
		setup(t)
		s := db.NewSession()
		defer s.Close()
		tb := &TaskBucket{BucketID: 27, TaskID: 20, ProjectViewID: 44, ProjectID: 11}
		require.NoError(t, tb.Update(s, cfUser1))
		require.NoError(t, s.Commit())

		assert.True(t, taskDone(t, 20))
		assert.Equal(t, int64(4), taskStatusOf(t, 20).StatusID)
		assert.Len(t, taskHistoryOf(t, 20), 3)
	})

	t.Run("dragging out of the done bucket goes back to the initial status", func(t *testing.T) {
		setup(t)
		for _, target := range []int64{27, 11} {
			s := db.NewSession()
			tb := &TaskBucket{BucketID: target, TaskID: 20, ProjectViewID: 44, ProjectID: 11}
			require.NoError(t, tb.Update(s, cfUser1))
			require.NoError(t, s.Commit())
			s.Close()
		}
		assert.False(t, taskDone(t, 20))
		assert.Equal(t, int64(1), taskStatusOf(t, 20).StatusID)
	})

	t.Run("a drag the workflow rejects fails", func(t *testing.T) {
		setup(t)
		wf := readWorkflow(t, 11)
		wf.RestrictTransitions = true
		mustSaveWorkflow(t, wf, cfUser1)

		s := db.NewSession()
		defer s.Close()
		tb := &TaskBucket{BucketID: 27, TaskID: 20, ProjectViewID: 44, ProjectID: 11}
		err := tb.Update(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowTransitionNotAllowed(err), "got %T: %v", err, err)
		_ = s.Rollback()
		assert.False(t, taskDone(t, 20))
	})

	t.Run("marking done through the task moves bucket and status once", func(t *testing.T) {
		setup(t)
		require.NoError(t, updateTaskAs(t, &Task{ID: 20, Title: "task #20", Done: true}, cfUser1))
		assert.Equal(t, int64(27), bucketOf(t, 20))
		assert.Equal(t, int64(4), taskStatusOf(t, 20).StatusID)
		assert.Len(t, taskHistoryOf(t, 20), 3)
	})

	t.Run("a status change through the endpoint moves the bucket", func(t *testing.T) {
		setup(t)
		_, err := changeTaskStatusAs(t, 20, 4, "", cfUser1)
		require.NoError(t, err)
		assert.Equal(t, int64(27), bucketOf(t, 20))
		assert.True(t, taskDone(t, 20))

		_, err = changeTaskStatusAs(t, 20, 2, "", cfUser1)
		require.NoError(t, err)
		assert.Equal(t, int64(11), bucketOf(t, 20))
		assert.False(t, taskDone(t, 20))
	})
}
