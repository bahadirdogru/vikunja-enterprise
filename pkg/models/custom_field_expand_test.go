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

func TestAddCustomFieldsToTasks(t *testing.T) {
	env := setupCFFilterEnv(t)
	// task 1 additionally gets a second user so the multi-user shape is covered
	cfInsertValues(t, &TaskCustomFieldValue{TaskID: 1, CustomFieldID: env.user, UserID: cfPtr(int64(2))})

	s := db.NewSession()
	defer s.Close()

	taskMap := map[int64]*Task{1: {ID: 1}, 2: {ID: 2}, 5: {ID: 5}}
	require.NoError(t, addCustomFieldsToTasks(s, []int64{1, 2, 5}, taskMap))

	byField := func(task int64) map[int64]*TaskCustomFieldSummary {
		m := map[int64]*TaskCustomFieldSummary{}
		for _, c := range taskMap[task].CustomFields {
			m[c.CustomFieldID] = c
		}
		return m
	}

	t.Run("all value shapes, ordered by field position", func(t *testing.T) {
		got := taskMap[1].CustomFields
		require.Len(t, got, 8)
		// fixture fields (text, select, multi_select) come first, then the fields created by the env
		assert.Equal(t, []int64{1, 2, 4, env.num, env.date, env.chk, env.txt, env.user},
			[]int64{got[0].CustomFieldID, got[1].CustomFieldID, got[2].CustomFieldID, got[3].CustomFieldID, got[4].CustomFieldID, got[5].CustomFieldID, got[6].CustomFieldID, got[7].CustomFieldID})

		m := byField(1)
		assert.Equal(t, CustomFieldTypeText, m[1].Type)
		assert.Equal(t, "REF-1", m[1].Value)
		assert.Equal(t, int64(1), m[2].Value)
		assert.Equal(t, []int64{3, 4}, m[4].Value)
		assert.InDelta(t, 10.0, m[env.num].Value, 0.0001)
		assert.Equal(t, true, m[env.chk].Value)
		assert.Equal(t, []int64{1, 2}, m[env.user].Value)
		assert.NotNil(t, m[env.date].Value)
	})
	t.Run("false checkbox counts as a value", func(t *testing.T) {
		c := byField(2)[env.chk]
		require.NotNil(t, c)
		assert.Equal(t, false, c.Value)
	})
	t.Run("task without values gets an empty list", func(t *testing.T) {
		assert.NotNil(t, taskMap[5].CustomFields)
		assert.Empty(t, taskMap[5].CustomFields)
	})
}

func TestAddCustomFieldsToTasks_ViaCollection(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	tc := &TaskCollection{ProjectID: 1, Expand: []TaskCollectionExpandable{TaskCollectionExpandCustomFields}}
	res, _, _, err := tc.ReadAll(s, &user.User{ID: 1}, "", 1, 50)
	require.NoError(t, err)
	tasks, ok := res.([]*Task)
	require.True(t, ok)
	for _, task := range tasks {
		if task.ID == 1 {
			assert.Len(t, task.CustomFields, 3)
		}
		assert.NotNil(t, task.CustomFields)
	}

	// without the expand nothing is attached
	tc = &TaskCollection{ProjectID: 1}
	res, _, _, err = tc.ReadAll(s, &user.User{ID: 1}, "", 1, 50)
	require.NoError(t, err)
	for _, task := range res.([]*Task) {
		assert.Nil(t, task.CustomFields)
	}

	// single read
	task := &Task{ID: 1, Expand: []TaskCollectionExpandable{TaskCollectionExpandCustomFields}}
	require.NoError(t, task.ReadOne(s, &user.User{ID: 1}))
	assert.Len(t, task.CustomFields, 3)
}
