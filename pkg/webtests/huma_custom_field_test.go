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
	"strconv"
	"testing"

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// Fixtures (pkg/db/fixtures/custom_fields.yml and friends): project 1 (user1 owner) has fields
// 1 text, 2 select (options 1, 2), 3 number (0..1000) and 4 multi_select (options 3, 4); task 1
// has values for 1, 2 and 4. Project 9 (user1 read) has field 5 with a value on task 18; project
// 10 (user1 write) has field 6 and task 19. user2 has no access to project 1.

type customFieldDTO struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Options   []struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	} `json:"options"`
}

type customFieldValueDTO struct {
	TaskID        int64  `json:"task_id"`
	CustomFieldID int64  `json:"custom_field_id"`
	Type          string `json:"type"`
	Value         any    `json:"value"`
}

func customFieldValuesByField(t *testing.T, body []byte) map[int64]any {
	t.Helper()
	var resp struct {
		Items []customFieldValueDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &resp), string(body))
	values := map[int64]any{}
	for _, it := range resp.Items {
		values[it.CustomFieldID] = it.Value
	}
	return values
}

func TestHumaCustomField(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	t.Run("list returns the fields with embedded options", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-fields", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var resp struct {
			Items []customFieldDTO `json:"items"`
			Total int64            `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Len(t, resp.Items, 4)
		assert.EqualValues(t, 4, resp.Total)
		assert.Equal(t, int64(1), resp.Items[0].ID)
		assert.Equal(t, "select", resp.Items[1].Type)
		assert.Len(t, resp.Items[1].Options, 2)
	})

	t.Run("list is forbidden for a stranger", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-fields", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("read one", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-fields/3", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"title":"Budget"`)
		assert.Contains(t, rec.Body.String(), `"max":1000`)
		assert.Contains(t, rec.Body.String(), `"max_permission":`)
		assert.NotEmpty(t, rec.Header().Get("ETag"))
	})

	t.Run("read forbidden for a stranger and 404 for a field of another project", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-fields/1", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-fields/5", "", user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	var createdID int64
	t.Run("create with options", func(t *testing.T) {
		body := `{"title":"Severity","type":"select","required":true,"options":[{"title":"Minor","color":"#00ff00"},{"title":"Major"}]}`
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-fields", body, user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		var created customFieldDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		createdID = created.ID
		assert.NotZero(t, createdID)
		assert.Equal(t, int64(1), created.ProjectID)
		require.Len(t, created.Options, 2)
		assert.Contains(t, rec.Body.String(), `"created_by":{"id":1,`)
	})

	t.Run("create rejects an invalid definition", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-fields", `{"title":"x","type":"text","options":[{"title":"a"}]}`, user1, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-fields", `{"title":"x","type":"bogus"}`, user1, "")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	})

	t.Run("create is forbidden for a reader, a writer and a stranger", func(t *testing.T) {
		body := `{"title":"Nope","type":"text"}`
		for _, path := range []string{
			"/api/v2/projects/9/custom-fields",  // user1 reads
			"/api/v2/projects/10/custom-fields", // user1 writes
		} {
			rec := humaRequest(t, e, http.MethodPost, path, body, user1, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, path+": "+rec.Body.String())
		}
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-fields", body, user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("update replaces the options", func(t *testing.T) {
		// Created field has two options; keep the first renamed, drop the second, add one.
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-fields/"+strconv.FormatInt(createdID, 10), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code)
		var current customFieldDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &current))
		require.Len(t, current.Options, 2)

		body := `{"title":"Severity 2","type":"select","options":[{"id":` + strconv.FormatInt(current.Options[0].ID, 10) + `,"title":"Trivial"},{"title":"Critical"}]}`
		rec = humaRequest(t, e, http.MethodPut, "/api/v2/projects/1/custom-fields/"+strconv.FormatInt(createdID, 10), body, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var updated customFieldDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
		assert.Equal(t, "Severity 2", updated.Title)
		require.Len(t, updated.Options, 2)
		assert.Equal(t, current.Options[0].ID, updated.Options[0].ID)
		assert.Equal(t, "Trivial", updated.Options[0].Title)
		assert.Equal(t, "Critical", updated.Options[1].Title)
		db.AssertMissing(t, "custom_field_options", map[string]interface{}{"id": current.Options[1].ID})
	})

	t.Run("update refuses a type change and a stranger", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/1/custom-fields/1", `{"title":"Reference","type":"number"}`, user1, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodPut, "/api/v2/projects/1/custom-fields/1", `{"title":"Hijack","type":"text"}`, user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("patch changes a single property", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPatch, "/api/v2/projects/1/custom-fields/1", `{"description":"patched"}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"description":"patched"`)
		assert.Contains(t, rec.Body.String(), `"title":"Reference"`)
	})

	t.Run("delete removes the field and its values", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodDelete, "/api/v2/projects/1/custom-fields/4", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodDelete, "/api/v2/projects/1/custom-fields/4", "", user1, "")
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		db.AssertMissing(t, "custom_fields", map[string]interface{}{"id": 4})
		db.AssertCount(t, "custom_field_options", builder.Eq{"custom_field_id": 4}, 0)
		db.AssertCount(t, "task_custom_field_values", builder.Eq{"custom_field_id": 4}, 0)

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/custom-fields/4", "", user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestHumaCustomFieldValue(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	t.Run("list includes unset fields with a null value", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1/custom-field-values", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		values := customFieldValuesByField(t, rec.Body.Bytes())
		require.Len(t, values, 4)
		assert.Equal(t, "REF-1", values[1])
		assert.EqualValues(t, 1, values[2])
		assert.Nil(t, values[3])
		assert.ElementsMatch(t, []any{3.0, 4.0}, values[4])
	})

	t.Run("list is forbidden for a stranger", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1/custom-field-values", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("a reader can list", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/18/custom-field-values", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "Visible to readers", customFieldValuesByField(t, rec.Body.Bytes())[5])
	})

	t.Run("set values of several types", func(t *testing.T) {
		tests := []struct {
			name    string
			fieldID string
			body    string
			want    any
		}{
			{"text", "1", `{"value":"REF-9"}`, "REF-9"},
			{"number", "3", `{"value":250.5}`, 250.5},
			{"select", "2", `{"value":2}`, 2.0},
			{"multi_select", "4", `{"value":[4]}`, []any{4.0}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/"+tt.fieldID, tt.body, user1, "")
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var set customFieldValueDTO
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &set))
				assert.Equal(t, tt.want, set.Value)
				assert.Equal(t, int64(1), set.TaskID)

				rec = humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1/custom-field-values", "", user1, "")
				require.Equal(t, http.StatusOK, rec.Code)
				id, err := strconv.ParseInt(tt.fieldID, 10, 64)
				require.NoError(t, err)
				assert.Equal(t, tt.want, customFieldValuesByField(t, rec.Body.Bytes())[id])
			})
		}
	})

	t.Run("set rejects invalid values", func(t *testing.T) {
		tests := []struct {
			name    string
			fieldID string
			body    string
		}{
			{"number above max", "3", `{"value":5000}`},
			{"text with a number", "1", `{"value":5}`},
			{"option of another field", "2", `{"value":3}`},
			{"multi_select as a single id", "4", `{"value":3}`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/"+tt.fieldID, tt.body, user1, "")
				assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			})
		}
	})

	t.Run("set with null clears", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/2/custom-field-values/1", `{"value":null}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		db.AssertCount(t, "task_custom_field_values", builder.And(builder.Eq{"task_id": 2}, builder.Eq{"custom_field_id": 1}), 0)
	})

	t.Run("set is forbidden for a reader and a stranger", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/18/custom-field-values/5", `{"value":"nope"}`, user1, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"value":"nope"}`, user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("a writer can set", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/19/custom-field-values/6", `{"value":"by a writer"}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("a field of another project is a 404", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/5", `{"value":"x"}`, user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("clear", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodDelete, "/api/v2/tasks/1/custom-field-values/2", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodDelete, "/api/v2/tasks/1/custom-field-values/2", "", user1, "")
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		db.AssertCount(t, "task_custom_field_values", builder.And(builder.Eq{"task_id": 1}, builder.Eq{"custom_field_id": 2}), 0)

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1/custom-field-values", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Nil(t, customFieldValuesByField(t, rec.Body.Bytes())[2])

		// Clearing again is not an error.
		rec = humaRequest(t, e, http.MethodDelete, "/api/v2/tasks/1/custom-field-values/2", "", user1, "")
		assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	})
}
