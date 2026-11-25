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
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// The documented limits of the workflow and custom field modules at their boundary: the limit is accepted and stored
// whole, one more is refused. Lengths are in characters, not bytes, so the strings are made of a two-byte letter and
// of one that takes four bytes in UTF-8: a column that counted bytes would cut or refuse them on MySQL and PostgreSQL.
func TestForkTextLimitsAreCharacters(t *testing.T) {
	for _, tc := range []struct{ name, ch string }{{"two-byte characters", "ş"}, {"four-byte characters", "𝔘"}} {
		at := strings.Repeat(tc.ch, 250)
		over := at + tc.ch

		t.Run(tc.name+" in the titles of a workflow", func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			wf := readWorkflow(t, 11)
			wf.Statuses[0].Title = at
			wf.Transitions[0].Title = at
			mustSaveWorkflow(t, wf, cfUser1)
			stored := readWorkflow(t, 11)
			assert.Equal(t, at, stored.Statuses[0].Title)
			assert.Equal(t, at, stored.Transitions[0].Title)

			wf = readWorkflow(t, 11)
			wf.Statuses[0].Title = over
			_, err := saveWorkflow(t, wf, cfUser1)
			require.Error(t, err)
			assert.True(t, IsErrWorkflowInvalidDefinition(err), "got %T: %v", err, err)

			wf = readWorkflow(t, 11)
			wf.Transitions[0].Title = over
			_, err = saveWorkflow(t, wf, cfUser1)
			require.Error(t, err)
			assert.True(t, IsErrWorkflowInvalidDefinition(err), "got %T: %v", err, err)
			assert.Equal(t, at, readWorkflow(t, 11).Statuses[0].Title, "the refused saves changed nothing")
		})

		t.Run(tc.name+" in a status change that records the titles", func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			wf := readWorkflow(t, 11)
			wf.Statuses[1].Title = at // Doing
			mustSaveWorkflow(t, wf, cfUser1)
			_, err := changeTaskStatusAs(t, 20, 2, "", cfUser1)
			require.NoError(t, err)
			history := taskHistoryOf(t, 20)
			last := history[len(history)-1]
			assert.Equal(t, at, last.ToTitle, "the history keeps the whole title")
		})

		t.Run(tc.name+" in a custom field and its options", func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			s := db.NewSession()
			defer s.Close()
			f := &CustomField{ProjectID: 1, Title: at, Type: CustomFieldTypeSelect, Options: []*CustomFieldOption{{Title: at}, {Title: "other"}}}
			require.NoError(t, f.Create(s, cfUser1))
			require.NoError(t, s.Commit())

			s2 := db.NewSession()
			defer s2.Close()
			read := &CustomField{ID: f.ID, ProjectID: 1}
			can, _, err := read.CanRead(s2, cfUser1)
			require.NoError(t, err)
			require.True(t, can)
			require.NoError(t, read.ReadOne(s2, cfUser1))
			assert.Equal(t, at, read.Title)
			require.NotEmpty(t, read.Options)
			assert.Equal(t, at, read.Options[0].Title)

			tooLong := &CustomField{ProjectID: 1, Title: over, Type: CustomFieldTypeText}
			err = tooLong.Create(s2, cfUser1)
			require.Error(t, err)
			assert.True(t, IsErrCustomFieldInvalidDefinition(err), "got %T: %v", err, err)
			_ = s2.Rollback()

			s3 := db.NewSession()
			defer s3.Close()
			longOption := &CustomField{ProjectID: 1, Title: "ok", Type: CustomFieldTypeSelect, Options: []*CustomFieldOption{{Title: over}}}
			err = longOption.Create(s3, cfUser1)
			require.Error(t, err)
			assert.True(t, IsErrCustomFieldInvalidDefinition(err), "got %T: %v", err, err)
			_ = s3.Rollback()
		})
	}
}

// "At most 50 approver sources per transition" (docs/modules/approvals.md): 50 are accepted, 51 are refused and
// nothing of the refused document is saved.
func TestWorkflow_ApproverSourcesPerTransitionLimit(t *testing.T) {
	sources := func(n int) *TransitionApproval {
		list := make([]ApproverSource, n)
		for i := range list {
			list[i] = ApproverSource{Type: ApproverSourceAssignees}
		}
		return &TransitionApproval{Mode: ApprovalModeAny, Approvers: list}
	}
	withApproval := func(t *testing.T, n int) (*Workflow, error) {
		t.Helper()
		db.LoadAndAssertFixtures(t)
		wf := readWorkflow(t, 11)
		wf.Transitions = append(wf.Transitions, &WorkflowTransition{
			FromStatusID: 1, ToStatusID: 2, MinPermission: "write", RequiresApproval: true, Approval: sources(n),
		})
		return saveWorkflow(t, wf, cfUser1)
	}

	t.Run("50 sources are accepted", func(t *testing.T) {
		_, err := withApproval(t, 50)
		require.NoError(t, err)
		db.AssertExists(t, "workflow_transitions", map[string]interface{}{"workflow_id": 1, "from_status_id": 1, "to_status_id": 2, "requires_approval": true}, false)
	})

	t.Run("51 sources are refused and nothing is saved", func(t *testing.T) {
		_, err := withApproval(t, 51)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowInvalidDefinition(err), "got %T: %v", err, err)
		assert.Contains(t, err.Error(), "50")
		db.AssertCount(t, "workflow_transitions", builder.And(builder.Eq{"workflow_id": 1, "from_status_id": 1, "to_status_id": 2}), 0)
	})
}

// "A save that would move or change more than 20000 tasks is refused": the limit itself is accepted, one more is not.
// The limit is lowered to the number of tasks the fixtures hold in the status, so the boundary is the same code path.
func TestWorkflow_SaveSizeBoundary(t *testing.T) {
	previous := maxTasksChangedBySave
	t.Cleanup(func() { maxTasksChangedBySave = previous })
	const extra = 3

	remove := func(t *testing.T, limit int64) error {
		t.Helper()
		db.LoadAndAssertFixtures(t)
		for i := 0; i < extra; i++ {
			createWorkflowTask(t, 11, &Task{Title: "bulk", ProjectID: 11})
		}
		maxTasksChangedBySave = limit
		wf := readWorkflow(t, 11)
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		wf.MoveTasksTo = wf.Statuses[0].ID
		_, err := saveWorkflow(t, wf, cfUser1)
		return err
	}

	t.Run("exactly the limit is accepted", func(t *testing.T) {
		require.NoError(t, remove(t, extra+1))                            // the three new tasks and fixture task 20
		db.AssertCount(t, "task_statuses", builder.Eq{"status_id": 1}, 0) // nothing is left on the removed status
		db.AssertCount(t, "task_status_history", builder.Like{"comment", "%status_removed%"}, extra+1)
	})

	t.Run("one task more than the limit is refused and nothing is written", func(t *testing.T) {
		err := remove(t, extra)
		require.Error(t, err)
		var tooLarge ErrWorkflowStatusMoveTooLarge
		require.ErrorAs(t, err, &tooLarge)
		assert.EqualValues(t, extra+1, tooLarge.Tasks)
		assert.EqualValues(t, extra, tooLarge.Limit)
		db.AssertCount(t, "task_status_history", builder.Like{"comment", "%status_removed%"}, 0)
		assert.Equal(t, int64(1), taskStatusOf(t, 20).StatusID)
	})
}
