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

package webtests

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures: project 1 has the text field 1 (task 1 = REF-1, task 2 = REF-2), the select field 2
// (task 1 = option 1), the number field 3 (no values) and the multi_select field 4.
func TestHumaCustomFieldFilterAndSort(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	list := func(token, path string, query url.Values) (int, []int64) {
		rec := humaRequest(t, e, http.MethodGet, path+"?"+query.Encode(), "", token, "")
		var body struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		}
		if rec.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
		}
		ids := make([]int64, 0, len(body.Items))
		for _, i := range body.Items {
			ids = append(ids, i.ID)
		}
		return rec.Code, ids
	}

	t.Run("filter by text value", func(t *testing.T) {
		code, ids := list(user1, "/api/v2/projects/1/tasks", url.Values{"filter": {"cf_1 like REF && done = false"}})
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, []int64{1}, ids, "task 2 is done")
	})

	t.Run("filter and sort together", func(t *testing.T) {
		code, ids := list(user1, "/api/v2/projects/1/tasks", url.Values{
			"filter":   {"cf_1 like REF"},
			"sort_by":  {"cf_1"},
			"order_by": {"desc"},
		})
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, []int64{2, 1}, ids)

		code, ids = list(user1, "/api/v2/projects/1/tasks", url.Values{"sort_by": {"cf_1"}, "order_by": {"asc"}})
		require.Equal(t, http.StatusOK, code)
		require.Greater(t, len(ids), 2)
		assert.Equal(t, []int64{1, 2}, ids[:2])
		assert.NotContains(t, ids[:2], int64(3), "tasks without a value sort last")
	})

	t.Run("negative comparator matches tasks without a value", func(t *testing.T) {
		code, ids := list(user1, "/api/v2/projects/1/tasks", url.Values{"filter": {"cf_2 != 1"}})
		require.Equal(t, http.StatusOK, code)
		assert.NotContains(t, ids, int64(1))
		assert.Contains(t, ids, int64(2))
	})

	t.Run("across projects", func(t *testing.T) {
		code, ids := list(user1, "/api/v2/tasks", url.Values{"filter": {"cf_5 = 'Visible to readers'"}})
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, []int64{18}, ids)
	})

	t.Run("invalid filters are 400", func(t *testing.T) {
		for _, filter := range []string{"cf_3 = abc", "cf_1 > 5", "cf_2 like 1", "cf_999999 = 1", "cf_x = 1"} {
			code, _ := list(user1, "/api/v2/projects/1/tasks", url.Values{"filter": {filter}})
			assert.Equal(t, http.StatusBadRequest, code, filter)
		}
	})

	t.Run("sorting by a multi value or unknown field is 400", func(t *testing.T) {
		for _, field := range []string{"cf_4", "cf_999999"} {
			code, _ := list(user1, "/api/v2/projects/1/tasks", url.Values{"sort_by": {field}})
			assert.Equal(t, http.StatusBadRequest, code, field)
		}
	})

	t.Run("a field of an unreadable project is rejected like an unknown one", func(t *testing.T) {
		// user2 has no access to project 1.
		code, _ := list(user2, "/api/v2/tasks", url.Values{"filter": {"cf_1 = REF-1"}})
		assert.Equal(t, http.StatusBadRequest, code)
		code, _ = list(user2, "/api/v2/tasks", url.Values{"filter": {"cf_999999 = REF-1"}})
		assert.Equal(t, http.StatusBadRequest, code)
		code, _ = list(user2, "/api/v2/tasks", url.Values{"sort_by": {"cf_1"}})
		assert.Equal(t, http.StatusBadRequest, code)
	})
}
