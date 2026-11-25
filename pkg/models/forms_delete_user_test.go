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
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "The address is cleared when the account is deleted" (docs/modules/forms.md), through the real DeleteUser flow and
// not through the helper that does the update: the submissions stay, the task they created stays, and only the
// identity of the deleted account is gone.
func TestFormSubmit_DeleteUserAnonymisesItsSubmissions(t *testing.T) {
	formsReset(t)
	f := createForm(t, baseForm(FormAccessUsers))
	_, err := submit(t, f, map[string]any{"title": "from user 2"}, asAuth(fU2))
	require.NoError(t, err)
	task2 := lastTask(t)
	_, err = submit(t, f, map[string]any{"title": "from user 1"}, asAuth(fU1))
	require.NoError(t, err)
	task1 := lastTask(t)

	submissionOf := func(task *Task) *FormSubmission {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		sub := &FormSubmission{}
		has, err := s.Where("task_id = ?", task.ID).Get(sub)
		require.NoError(t, err)
		require.True(t, has)
		return sub
	}
	before := submissionOf(task2)
	require.NotNil(t, before.SubmitterID)
	require.Equal(t, int64(2), *before.SubmitterID)
	require.Equal(t, "user2", before.SubmitterName)

	s := db.NewSession()
	require.NoError(t, DeleteUser(s, &user.User{ID: 2}))
	require.NoError(t, s.Commit())
	s.Close()

	after := submissionOf(task2)
	assert.Equal(t, before.ID, after.ID, "the submission itself is kept")
	assert.Equal(t, f.ID, after.FormID)
	assert.Nil(t, after.SubmitterID)
	assert.Empty(t, after.SubmitterName)
	assert.Empty(t, after.SubmitterEmail)

	other := submissionOf(task1)
	require.NotNil(t, other.SubmitterID, "another user's submission is not touched")
	assert.Equal(t, int64(1), *other.SubmitterID)
	assert.Equal(t, "user1", other.SubmitterName)

	db.AssertExists(t, "tasks", map[string]interface{}{"id": task2.ID}, false)
}
