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
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// Fixtures: project 11 (user1 admin, user6 owner) has a workflow with statuses 1 Todo (initial), 2 Doing,
// 3 Review, 4 Done, 5 Dropped (cancelled) and 6 Legacy (archived); task 20 is in Todo. Project 1 is owned by
// user1 and has no workflow, project 10 gives user1 write access and project 9 read access. user2 has no
// access to projects 1 and 11.

type workflowDTO struct {
	ID                  int64 `json:"id"`
	ProjectID           int64 `json:"project_id"`
	RestrictTransitions bool  `json:"restrict_transitions"`
	Statuses            []struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Category  string `json:"category"`
		IsInitial bool   `json:"is_initial"`
		Archived  bool   `json:"archived"`
	} `json:"statuses"`
	Transitions []struct {
		ID             int64    `json:"id"`
		FromStatusID   int64    `json:"from_status_id"`
		ToStatusID     int64    `json:"to_status_id"`
		RequiredFields []string `json:"required_fields"`
		TeamIDs        []int64  `json:"team_ids"`
	} `json:"transitions"`
}

type problemDTO struct {
	Status  int            `json:"status"`
	Code    int            `json:"code"`
	Detail  string         `json:"detail"`
	Details map[string]any `json:"details"`
}

func decodeProblem(t *testing.T, body []byte) problemDTO {
	t.Helper()
	var p problemDTO
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	return p
}

func TestHumaWorkflow(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	t.Run("read returns the whole document with an ETag", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/11/workflow", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotEmpty(t, rec.Header().Get("ETag"))

		var wf workflowDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wf))
		assert.Equal(t, int64(11), wf.ProjectID)
		require.Len(t, wf.Statuses, 6)
		assert.Equal(t, "Todo", wf.Statuses[0].Title)
		assert.True(t, wf.Statuses[0].IsInitial)
		assert.True(t, wf.Statuses[5].Archived)
		require.Len(t, wf.Transitions, 3)
		assert.Equal(t, []string{"due_date", "assignees"}, wf.Transitions[0].RequiredFields)
		assert.Equal(t, []int64{9}, wf.Transitions[2].TeamIDs)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/projects/11/workflow", strings.NewReader(""))
		req.Header.Set("Authorization", "Bearer "+user1)
		req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
		cond := httptest.NewRecorder()
		e.ServeHTTP(cond, req)
		assert.Equal(t, http.StatusNotModified, cond.Code)
	})

	t.Run("read is forbidden for a stranger and 404 without a workflow", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/11/workflow", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/workflow", "", user1, "")
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeWorkflowDoesNotExist, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	createBody := `{
		"restrict_transitions": true,
		"statuses": [
			{"id": -1, "title": "Backlog", "category": "open", "is_initial": true},
			{"id": -2, "title": "Working", "category": "in_progress"},
			{"id": -3, "title": "Finished", "category": "done"}
		],
		"transitions": [
			{"from_status_id": -1, "to_status_id": -2, "title": "Start", "required_fields": ["due_date", "cf_2"], "require_comment": true},
			{"from_status_id": -2, "to_status_id": -3, "min_permission": "admin"}
		]
	}`

	t.Run("replace is forbidden for readers, writers and strangers", func(t *testing.T) {
		for _, projectPath := range []string{"/api/v2/projects/9/workflow", "/api/v2/projects/10/workflow"} {
			rec := humaRequest(t, e, http.MethodPut, projectPath, createBody, user1, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, projectPath+": "+rec.Body.String())
		}
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/1/workflow", createBody, user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		db.AssertCount(t, "workflows", builder.In("project_id", 1, 9, 10), 0)
	})

	t.Run("replace creates the workflow and assigns the existing tasks", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/1/workflow", createBody, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var wf workflowDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wf))
		assert.Equal(t, int64(1), wf.ProjectID)
		assert.True(t, wf.RestrictTransitions)
		require.Len(t, wf.Statuses, 3)
		assert.Positive(t, wf.Statuses[0].ID)
		require.Len(t, wf.Transitions, 2)
		assert.Equal(t, wf.Statuses[0].ID, wf.Transitions[0].FromStatusID)
		assert.Equal(t, wf.Statuses[2].ID, wf.Transitions[1].ToStatusID)

		// Task 1 is open, task 2 is done.
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": 1, "status_id": wf.Statuses[0].ID}, false)
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": 2, "status_id": wf.Statuses[2].ID}, false)

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/workflow", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("replace validates the document", func(t *testing.T) {
		tests := []struct {
			name string
			body string
		}{
			{"two initial statuses", `{"statuses":[
				{"id":-1,"title":"A","category":"open","is_initial":true},
				{"id":-2,"title":"B","category":"done","is_initial":true}]}`},
			{"no done status", `{"statuses":[{"id":-1,"title":"A","category":"open","is_initial":true}]}`},
			{"transition to a status of another workflow", `{"statuses":[
				{"id":-1,"title":"A","category":"open","is_initial":true},
				{"id":-2,"title":"B","category":"done"}],
				"transitions":[{"from_status_id":-1,"to_status_id":1}]}`},
			{"duplicate pair", `{"statuses":[
				{"id":-1,"title":"A","category":"open","is_initial":true},
				{"id":-2,"title":"B","category":"done"}],
				"transitions":[{"from_status_id":-1,"to_status_id":-2},{"from_status_id":-1,"to_status_id":-2}]}`},
			{"unknown required field", `{"statuses":[
				{"id":-1,"title":"A","category":"open","is_initial":true},
				{"id":-2,"title":"B","category":"done"}],
				"transitions":[{"from_status_id":-1,"to_status_id":-2,"required_fields":["priority"]}]}`},
			{"custom field of another project", `{"statuses":[
				{"id":-1,"title":"A","category":"open","is_initial":true},
				{"id":-2,"title":"B","category":"done"}],
				"transitions":[{"from_status_id":-1,"to_status_id":-2,"required_fields":["cf_5"]}]}`},
			{"unknown team", `{"statuses":[
				{"id":-1,"title":"A","category":"open","is_initial":true},
				{"id":-2,"title":"B","category":"done"}],
				"transitions":[{"from_status_id":-1,"to_status_id":-2,"team_ids":[9999]}]}`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/11/workflow", tt.body, user1, "")
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
				assert.Equal(t, models.ErrCodeWorkflowInvalidDefinition, decodeProblem(t, rec.Body.Bytes()).Code)
			})
		}
		// Nothing changed.
		db.AssertCount(t, "workflow_statuses", builder.Eq{"workflow_id": 1}, 6)
		db.AssertCount(t, "workflow_transitions", builder.Eq{"workflow_id": 1}, 3)
	})

	t.Run("replace rejects an invalid enum before the model runs", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/11/workflow",
			`{"statuses":[{"id":-1,"title":"A","category":"paused","is_initial":true}]}`, user1, "")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	})

	t.Run("removing a status with tasks needs move_tasks_to", func(t *testing.T) {
		// Todo (1) holds task 20. The document drops it.
		body := `{"statuses":[
			{"id":2,"title":"Doing","category":"in_progress","is_initial":true},
			{"id":4,"title":"Done","category":"done"}]}`
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/11/workflow", body, user1, "")
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		p := decodeProblem(t, rec.Body.Bytes())
		assert.Equal(t, models.ErrCodeWorkflowMoveTasksRequired, p.Code)
		assert.Equal(t, []any{float64(1)}, p.Details["status_ids"])

		withTarget := `{"move_tasks_to":2,"statuses":[
			{"id":2,"title":"Doing","category":"in_progress","is_initial":true},
			{"id":4,"title":"Done","category":"done"}]}`
		rec = humaRequest(t, e, http.MethodPut, "/api/v2/projects/11/workflow", withTarget, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": 20, "status_id": 2}, false)
		// Todo has history and stays as an archived status; Dropped and Review had none and are gone.
		db.AssertExists(t, "workflow_statuses", map[string]interface{}{"id": 1, "archived": true}, false)
		db.AssertMissing(t, "workflow_statuses", map[string]interface{}{"id": 5})
	})

	t.Run("patch merges", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPatch, "/api/v2/projects/11/workflow", `{"restrict_transitions":true}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var wf workflowDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wf))
		assert.True(t, wf.RestrictTransitions)
		assert.Len(t, wf.Statuses, 4, "statuses are untouched by the patch")
	})

	t.Run("delete is forbidden for a stranger and works for an admin", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodDelete, "/api/v2/projects/1/workflow", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodDelete, "/api/v2/projects/1/workflow", "", user1, "")
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		db.AssertCount(t, "workflows", builder.Eq{"project_id": 1}, 0)
		db.AssertCount(t, "task_statuses", builder.Eq{"task_id": 1}, 0)

		rec = humaRequest(t, e, http.MethodDelete, "/api/v2/projects/1/workflow", "", user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})
}

func TestHumaTaskStatus(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)
	user6 := humaTokenFor(t, &testuser6)

	type statusDTO struct {
		TaskID     int64 `json:"task_id"`
		WorkflowID int64 `json:"workflow_id"`
		Status     *struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"status"`
		Transitions []struct {
			Title          string   `json:"title"`
			RequireComment bool     `json:"require_comment"`
			MissingFields  []string `json:"missing_fields"`
			ToStatus       struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
			} `json:"to_status"`
		} `json:"transitions"`
	}

	t.Run("read shows the status and the available changes", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/20/status", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var st statusDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &st))
		require.NotNil(t, st.Status)
		assert.Equal(t, "Todo", st.Status.Title)
		assert.Equal(t, int64(20), st.TaskID)
		titles := []string{}
		for _, tr := range st.Transitions {
			titles = append(titles, tr.ToStatus.Title)
		}
		assert.Equal(t, []string{"Doing", "Review", "Done", "Dropped"}, titles)
	})

	t.Run("read is forbidden for a stranger and 404 without a workflow", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/20/status", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1/status", "", user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("change to a status without requirements", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":2,"comment":"go"}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var res struct {
			TaskID       int64  `json:"task_id"`
			FromStatusID *int64 `json:"from_status_id"`
			Done         bool   `json:"done"`
			Status       struct {
				Title string `json:"title"`
			} `json:"status"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, int64(20), res.TaskID)
		require.NotNil(t, res.FromStatusID)
		assert.Equal(t, int64(1), *res.FromStatusID)
		assert.Equal(t, "Doing", res.Status.Title)
		assert.False(t, res.Done)
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": 20, "status_id": 2}, false)
		db.AssertExists(t, "task_status_history", map[string]interface{}{
			"task_id":      20,
			"to_status_id": 2,
			"from_title":   "Todo",
			"to_title":     "Doing",
			"comment":      "go",
		}, false)
	})

	t.Run("unmet requirements return 422 with machine-readable details", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":3}`, user1, "")
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		p := decodeProblem(t, rec.Body.Bytes())
		assert.Equal(t, models.ErrCodeWorkflowRequirementsNotMet, p.Code)
		assert.Equal(t, []any{"due_date", "assignees"}, p.Details["missing_fields"])
		assert.Equal(t, false, p.Details["comment_required"])
		assert.EqualValues(t, 20, p.Details["task_id"])
		assert.EqualValues(t, 2, p.Details["from_status_id"])
		assert.EqualValues(t, 3, p.Details["to_status_id"])
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": 20, "status_id": 2}, false)

		// The read shows the same requirements ahead of time.
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/tasks/20/status", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code)
		var st statusDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &st))
		found := false
		for _, tr := range st.Transitions {
			if tr.ToStatus.Title == "Review" {
				found = true
				assert.Equal(t, "Send to review", tr.Title)
				assert.Equal(t, []string{"due_date", "assignees"}, tr.MissingFields)
			}
		}
		assert.True(t, found)
	})

	t.Run("a required comment is part of the details", func(t *testing.T) {
		// Satisfy the Review requirements, move there, then try Done (comment required, admin).
		s := db.NewSession()
		_, err := s.ID(20).Cols("due_date").Update(&models.Task{DueDate: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)
		_, err = s.Insert(&models.TaskAssginee{TaskID: 20, UserID: 1})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()

		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":3}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":4}`, user1, "")
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		p := decodeProblem(t, rec.Body.Bytes())
		assert.Equal(t, true, p.Details["comment_required"])
		assert.Equal(t, []any{}, p.Details["missing_fields"])

		rec = humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":4,"comment":"approved"}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"done":true`)
		db.AssertExists(t, "tasks", map[string]interface{}{"id": 20, "done": true}, false)
	})

	t.Run("an archived or foreign status is rejected", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":6}`, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeWorkflowStatusArchived, decodeProblem(t, rec.Body.Bytes()).Code)

		rec = humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":9999}`, user1, "")
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeWorkflowStatusDoesNotExist, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("a transition limited to a team is forbidden for outsiders", func(t *testing.T) {
		// Todo to Dropped is limited to team 9; user6 owns project 11 and is an admin so passes,
		// user1 is an admin too. Use task 20 back in Todo through the owner first.
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":1}`, user6, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":5}`, user6, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("a stranger and a reader cannot change a status", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":2}`, user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		// Task 18 is in project 9 where user1 only reads, and project 9 has no workflow: forbidden comes first.
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/tasks/18/status", `{"to_status_id":2}`, user1, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("a project without a workflow answers 404", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/tasks/1/status", `{"to_status_id":2}`, user1, "")
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeWorkflowDoesNotExist, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("the doer needs the permission of an admin-only transition", func(t *testing.T) {
		// Project 10 gives user1 write access. Create a workflow there as the owner with an admin-only pair.
		body := `{"statuses":[
			{"id":-1,"title":"Todo","category":"open","is_initial":true},
			{"id":-2,"title":"Done","category":"done"}],
			"transitions":[{"from_status_id":-1,"to_status_id":-2,"min_permission":"admin"}]}`
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/10/workflow", body, user6, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var wf workflowDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wf))

		doneID := wf.Statuses[1].ID
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/tasks/19/status", `{"to_status_id":`+strconv.FormatInt(doneID, 10)+`}`, user1, "")
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		p := decodeProblem(t, rec.Body.Bytes())
		assert.Equal(t, models.ErrCodeWorkflowTransitionForbidden, p.Code)
		assert.Equal(t, "permission", p.Details["reason"])

		rec = humaRequest(t, e, http.MethodPost, "/api/v2/tasks/19/status", `{"to_status_id":`+strconv.FormatInt(doneID, 10)+`}`, user6, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("a restricted workflow refuses unlisted pairs", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPatch, "/api/v2/projects/11/workflow", `{"restrict_transitions":true}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		// Task 20 is in Dropped now; nothing lists a pair from there.
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/tasks/20/status", `{"to_status_id":2}`, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeWorkflowTransitionNotAllowed, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("history lists the changes newest first", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/20/status/history", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp struct {
			Items []struct {
				ToTitle   string `json:"to_title"`
				FromTitle string `json:"from_title"`
				Comment   string `json:"comment"`
				Doer      *struct {
					ID int64 `json:"id"`
				} `json:"doer"`
			} `json:"items"`
			Total int64 `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.GreaterOrEqual(t, len(resp.Items), 6)
		assert.Equal(t, "Dropped", resp.Items[0].ToTitle)
		assert.Equal(t, "Todo", resp.Items[0].FromTitle)
		require.NotNil(t, resp.Items[0].Doer)
		assert.Equal(t, int64(6), resp.Items[0].Doer.ID)
		assert.Equal(t, resp.Total, int64(len(resp.Items)))
		last := resp.Items[len(resp.Items)-1]
		assert.Equal(t, "Legacy", last.ToTitle, "denormalised titles keep archived statuses readable")

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/tasks/20/status/history", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})
}
