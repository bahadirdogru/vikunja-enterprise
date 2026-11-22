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
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures: see huma_workflow_test.go. Task 20 of project 11 is in Todo; transition Doing to Review needs
// due_date and assignees.

type statusTaskDTO struct {
	ID     int64 `json:"id"`
	Status *struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		Color    string `json:"color"`
		Category string `json:"category"`
	} `json:"status"`
}

func TestHumaTaskListStatus(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	list := func(path string, query url.Values) (int, []statusTaskDTO, []byte) {
		rec := humaRequest(t, e, http.MethodGet, path+"?"+query.Encode(), "", user1, "")
		var body struct {
			Items []statusTaskDTO `json:"items"`
		}
		if rec.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
		}
		return rec.Code, body.Items, rec.Body.Bytes()
	}

	t.Run("expand status embeds the status", func(t *testing.T) {
		code, tasks, raw := list("/api/v2/projects/11/tasks", url.Values{"expand": {"status"}})
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Len(t, tasks, 1)
		require.NotNil(t, tasks[0].Status)
		assert.Equal(t, "Todo", tasks[0].Status.Title)
		assert.Equal(t, "open", tasks[0].Status.Category)
		assert.Equal(t, "#888888", tasks[0].Status.Color)
		assert.EqualValues(t, 1, tasks[0].Status.ID)
	})
	t.Run("not embedded without the expand and absent for projects without a workflow", func(t *testing.T) {
		_, tasks, _ := list("/api/v2/projects/11/tasks", url.Values{})
		require.Len(t, tasks, 1)
		assert.Nil(t, tasks[0].Status)
		code, tasks, _ := list("/api/v2/projects/1/tasks", url.Values{"expand": {"status"}})
		require.Equal(t, http.StatusOK, code)
		for _, task := range tasks {
			assert.Nil(t, task.Status)
		}
	})
	t.Run("single task read", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/20?expand=status", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var task statusTaskDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &task))
		require.NotNil(t, task.Status)
		assert.Equal(t, "Todo", task.Status.Title)
	})
	t.Run("filter by status and category", func(t *testing.T) {
		for filter, want := range map[string][]int64{
			"status = 1":                           {20},
			"status = 2":                           {},
			"status != 2":                          {20},
			"status in 1,2":                        {20},
			"status not in 1,2":                    {},
			"status_category = open":               {20},
			"status_category != open":              {},
			"status_category in open,done":         {20},
			"status_category not in open,done":     {},
			"status = 1 && status_category = open": {20},
		} {
			code, tasks, raw := list("/api/v2/projects/11/tasks", url.Values{"filter": {filter}})
			require.Equal(t, http.StatusOK, code, "%s: %s", filter, string(raw))
			ids := []int64{}
			for _, task := range tasks {
				ids = append(ids, task.ID)
			}
			assert.ElementsMatch(t, want, ids, filter)
		}
	})
	t.Run("tasks without a status", func(t *testing.T) {
		_, positive, _ := list("/api/v2/projects/1/tasks", url.Values{"filter": {"status = 1"}})
		assert.Empty(t, positive)
		_, negative, _ := list("/api/v2/projects/1/tasks", url.Values{"filter": {"status != 1"}})
		assert.NotEmpty(t, negative)
	})
	t.Run("invalid filters are 400 problems", func(t *testing.T) {
		for filter, code := range map[string]int{
			"status = abc":           models.ErrCodeInvalidTaskFilterValue,
			"status_category = nope": models.ErrCodeInvalidTaskFilterValue,
			"status > 1":             models.ErrCodeInvalidTaskFilterComparator,
		} {
			rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/11/tasks?"+url.Values{"filter": {filter}}.Encode(), "", user1, "")
			require.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", filter, rec.Body.String())
			assert.Equal(t, code, decodeProblem(t, rec.Body.Bytes()).Code, filter)
		}
	})
	t.Run("sort by status", func(t *testing.T) {
		for _, order := range []string{"asc", "desc"} {
			code, tasks, raw := list("/api/v2/projects/11/tasks", url.Values{"sort_by": {"status"}, "order_by": {order}})
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Len(t, tasks, 1)
		}
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks?sort_by=status&order_by=desc", "", user1, "")
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
	t.Run("expand validation lists status", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/11/tasks?expand=nope", "", user1, "")
		assert.GreaterOrEqual(t, rec.Code, 400)
	})
}

func TestHumaStatusKanban(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	type column struct {
		ID     int64  `json:"id"`
		Title  string `json:"title"`
		Count  int64  `json:"count"`
		Status *struct {
			ID       int64  `json:"id"`
			Category string `json:"category"`
		} `json:"status"`
		Tasks []statusTaskDTO `json:"tasks"`
	}
	read := func(viewID int64, query string) []column {
		rec := humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/projects/11/views/%d/buckets/tasks%s", viewID, query), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body struct {
			Items []column `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return body.Items
	}

	// View creation
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/views",
		`{"title":"By status","view_kind":"kanban","bucket_configuration_mode":"status"}`, user1, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var view struct {
		ID   int64  `json:"id"`
		Mode string `json:"bucket_configuration_mode"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, "status", view.Mode)

	t.Run("a project without a workflow rejects the mode", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/views",
			`{"title":"By status","view_kind":"kanban","bucket_configuration_mode":"status"}`, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeWorkflowRequiredForStatusView, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("the bucket list is derived from the statuses", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/projects/11/views/%d/buckets", view.ID), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body struct {
			Items []column `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		ids := []int64{}
		for _, c := range body.Items {
			ids = append(ids, c.ID)
		}
		assert.Equal(t, []int64{-1, -2, -3, -4, -5}, ids)
	})

	t.Run("columns with their tasks", func(t *testing.T) {
		cols := read(view.ID, "")
		require.Len(t, cols, 5)
		assert.Equal(t, "Todo", cols[0].Title)
		require.NotNil(t, cols[0].Status)
		assert.EqualValues(t, 1, cols[0].Status.ID)
		assert.Equal(t, "open", cols[0].Status.Category)
		require.Len(t, cols[0].Tasks, 1)
		assert.EqualValues(t, 20, cols[0].Tasks[0].ID)
		assert.EqualValues(t, 1, cols[0].Count)
		assert.Empty(t, cols[1].Tasks)
	})

	move := func(bucket int64, body string) *struct {
		code int
		body []byte
	} {
		rec := humaRequest(t, e, http.MethodPut, fmt.Sprintf("/api/v2/projects/11/views/%d/buckets/%d/tasks", view.ID, bucket), body, user1, "")
		return &struct {
			code int
			body []byte
		}{rec.Code, rec.Body.Bytes()}
	}

	t.Run("a rejected move returns the typed workflow error and moves nothing", func(t *testing.T) {
		// Todo to Doing is free; Doing to Review needs due_date and assignees.
		res := move(-2, `{"task_id":20}`)
		require.Equal(t, http.StatusOK, res.code, string(res.body))
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": 20, "status_id": 2}, false)

		res = move(-3, `{"task_id":20}`)
		require.Equal(t, http.StatusUnprocessableEntity, res.code, string(res.body))
		p := decodeProblem(t, res.body)
		assert.Equal(t, models.ErrCodeWorkflowRequirementsNotMet, p.Code)
		assert.EqualValues(t, 20, p.Details["task_id"])
		assert.EqualValues(t, 3, p.Details["to_status_id"])
		assert.EqualValues(t, 2, p.Details["from_status_id"])
		assert.ElementsMatch(t, []any{"due_date", "assignees"}, p.Details["missing_fields"])
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": 20, "status_id": 2}, false)

		cols := read(view.ID, "")
		assert.Empty(t, cols[0].Tasks)
		require.Len(t, cols[1].Tasks, 1)
		assert.EqualValues(t, 20, cols[1].Tasks[0].ID)
	})

	t.Run("a move into a column that is not a status", func(t *testing.T) {
		res := move(-99, `{"task_id":20}`)
		require.Equal(t, http.StatusNotFound, res.code, string(res.body))
		assert.Equal(t, models.ErrCodeWorkflowStatusDoesNotExist, decodeProblem(t, res.body).Code)
		res = move(-6, `{"task_id":20}`)
		require.Equal(t, http.StatusBadRequest, res.code, string(res.body))
		assert.Equal(t, models.ErrCodeWorkflowStatusArchived, decodeProblem(t, res.body).Code)
	})

	t.Run("a move succeeds once the requirements are met", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":1}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodPut, "/api/v2/tasks/20", `{"title":"task #20","due_date":"2030-01-01T10:00:00Z","assignees":[{"id":1}]}`, user1, "")
		_ = rec
		res := move(-2, `{"task_id":20}`)
		require.Equal(t, http.StatusOK, res.code, string(res.body))
	})

	t.Run("bucket management is rejected", func(t *testing.T) {
		for method, path := range map[string]string{
			http.MethodPost:   fmt.Sprintf("/api/v2/projects/11/views/%d/buckets", view.ID),
			http.MethodPut:    fmt.Sprintf("/api/v2/projects/11/views/%d/buckets/-2", view.ID),
			http.MethodDelete: fmt.Sprintf("/api/v2/projects/11/views/%d/buckets/-2", view.ID),
		} {
			rec := humaRequest(t, e, method, path, `{"title":"x","limit":2}`, user1, "")
			require.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", method, rec.Body.String())
			assert.Equal(t, models.ErrCodeStatusViewBucketsManaged, decodeProblem(t, rec.Body.Bytes()).Code, method)
		}
	})

	t.Run("creating a task in a column", func(t *testing.T) {
		// Creating in the initial column.
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/tasks", `{"title":"in todo","bucket_id":-1}`, user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var created statusTaskDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": created.ID, "status_id": 1}, false)

		// Creating in Doing performs the transition.
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/tasks", `{"title":"in doing","bucket_id":-2}`, user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": created.ID, "status_id": 2}, false)

		// Creating in Review fails the transition (the workflow now only allows listed pairs, Todo to Review is not
		// listed), but the task exists in Todo.
		sess := db.NewSession()
		_, err := sess.Exec("UPDATE workflows SET restrict_transitions = ? WHERE id = 1", true)
		require.NoError(t, err)
		require.NoError(t, sess.Commit())
		sess.Close()
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/tasks", `{"title":"wants review","bucket_id":-3}`, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeWorkflowTransitionNotAllowed, decodeProblem(t, rec.Body.Bytes()).Code)
		sess = db.NewSession()
		stored := &models.Task{}
		has, err := sess.Where("title = ?", "wants review").Get(stored)
		require.NoError(t, err)
		require.True(t, has, "the task was created")
		_, err = sess.Exec("UPDATE workflows SET restrict_transitions = ? WHERE id = 1", false)
		require.NoError(t, err)
		require.NoError(t, sess.Commit())
		sess.Close()
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": stored.ID, "status_id": 1}, false)

		// A rejection that names missing fields carries them: Doing to Review needs due_date and assignees.
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/tasks", `{"title":"doing first","bucket_id":-2}`, user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		// An unknown column creates nothing.
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/tasks", `{"title":"nowhere","bucket_id":-77}`, user1, "")
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		db.AssertMissing(t, "tasks", map[string]interface{}{"title": "nowhere"})
	})

	t.Run("loading more tasks of one column", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/projects/11/views/%d/tasks?filter=%s&expand=status", view.ID, url.QueryEscape("bucket_id = -1")), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body struct {
			Items []statusTaskDTO `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.NotEmpty(t, body.Items)
		for _, task := range body.Items {
			require.NotNil(t, task.Status)
			assert.EqualValues(t, 1, task.Status.ID)
		}
	})

	t.Run("a workflow deleted later leaves an empty board", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodDelete, "/api/v2/projects/11/workflow", "", user1, "")
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		assert.Empty(t, read(view.ID, ""))
	})
}
