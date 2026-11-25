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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A form that names a status of the workflow stops when that status is removed (reason status_deleted). A save the
// 20000 task limit refuses writes nothing, so the form must stay as it is: the forms case of
// TestWorkflow_RefusedSaveWritesNothing.
func TestWorkflow_RemovingTheStatusOfAForm(t *testing.T) {
	removeTodo := func(t *testing.T) error {
		t.Helper()
		wf := readWorkflow(t, 11)
		wf.Statuses = wf.Statuses[1:]
		wf.Statuses[0].IsInitial = true
		wf.Transitions = nil
		wf.MoveTasksTo = wf.Statuses[0].ID
		_, err := saveWorkflow(t, wf, cfUser1)
		return err
	}
	newForm := func(t *testing.T) (named, other *Form) {
		t.Helper()
		formsReset(t)
		named = baseForm(FormAccessMembers)
		named.ProjectID = 11
		named.Settings.InitialStatusID = 1
		createForm(t, named)
		// A form of the same project that does not name the status is not affected either way.
		other = baseForm(FormAccessMembers)
		other.ProjectID = 11
		createForm(t, other)
		return named, other
	}

	t.Run("a refused save leaves the forms enabled and untouched", func(t *testing.T) {
		named, other := newForm(t)
		previous := maxTasksChangedBySave
		maxTasksChangedBySave = 0
		t.Cleanup(func() { maxTasksChangedBySave = previous })

		err := removeTodo(t)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowStatusMoveTooLarge(err), "%v", err)

		for _, f := range []*Form{named, other} {
			stored := reloadForm(t, f.ID)
			assert.True(t, stored.Enabled, "form %d", f.ID)
			assert.Empty(t, stored.DisabledReason, "form %d", f.ID)
		}
		assert.Equal(t, int64(1), reloadForm(t, named.ID).Settings.InitialStatusID)
		assert.Equal(t, int64(1), taskStatusOf(t, 20).StatusID, "nothing was moved")
	})

	t.Run("a save that goes through disables exactly the forms that name the status", func(t *testing.T) {
		named, other := newForm(t)

		require.NoError(t, removeTodo(t))

		stored := reloadForm(t, named.ID)
		assert.False(t, stored.Enabled)
		assert.Equal(t, FormDisabledStatusDeleted, stored.DisabledReason)
		untouched := reloadForm(t, other.ID)
		assert.True(t, untouched.Enabled)
		assert.Empty(t, untouched.DisabledReason)
	})
}
