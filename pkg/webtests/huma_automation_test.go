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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/automation"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// Fixtures: user1 owns project 1 (custom fields 1-4), administers project 11 (workflow, task 20), has write access to
// project 10 and read access to project 9. user2 has no access to any of them.

type automationRuleDTO struct {
	ID                  int64  `json:"id"`
	ProjectID           int64  `json:"project_id"`
	Title               string `json:"title"`
	Enabled             bool   `json:"enabled"`
	Revision            int64  `json:"revision"`
	OwnerID             int64  `json:"owner_id"`
	DisabledReason      string `json:"disabled_reason"`
	ConsecutiveFailures int64  `json:"consecutive_failures"`
	LastRunStatus       string `json:"last_run_status"`
	Trigger             struct {
		Type    string         `json:"type"`
		Options map[string]any `json:"options"`
	} `json:"trigger"`
	Conditions map[string]any   `json:"conditions"`
	Actions    []map[string]any `json:"actions"`
	MaxPerm    int              `json:"max_permission"`
}

type automationRunDTO struct {
	ID         int64  `json:"id"`
	RuleID     int64  `json:"rule_id"`
	Status     string `json:"status"`
	SkipReason string `json:"skip_reason"`
	Error      string `json:"error"`
	EventName  string `json:"event_name"`
	Steps      []struct {
		Position   int64  `json:"position"`
		ActionType string `json:"action_type"`
		Status     string `json:"status"`
		Error      string `json:"error"`
		Output     string `json:"output"`
	} `json:"steps"`
}

func autoRulesPath(project int64) string {
	return "/api/v2/projects/" + strconv.FormatInt(project, 10) + "/automations"
}

func autoRulePath(project, rule int64) string {
	return autoRulesPath(project) + "/" + strconv.FormatInt(rule, 10)
}

const priorityRule = `{
	"title": "Urgent first",
	"enabled": true,
	"trigger": {"type": "task.created"},
	"conditions": {"field": "title", "op": "like", "value": "urgent"},
	"actions": [{"type": "task.set_field", "params": {"field": "priority", "value": 5}}]
}`

func createAutomationRule(t *testing.T, e http.Handler, project int64, token, body string) automationRuleDTO {
	t.Helper()
	rec := humaRequestHandler(t, e, http.MethodPost, autoRulesPath(project), body, token)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var rule automationRuleDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rule))
	return rule
}

func humaRequestHandler(t *testing.T, e http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestHumaAutomation_Catalog(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	t.Run("lists triggers, fields, operators and actions", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/automations/catalog", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var catalog struct {
			Triggers []struct {
				Type    string `json:"type"`
				Options []struct {
					Name string `json:"name"`
					Kind string `json:"kind"`
				} `json:"options"`
			} `json:"triggers"`
			Fields []struct {
				Name      string   `json:"name"`
				Operators []string `json:"operators"`
			} `json:"fields"`
			Operators []struct {
				Type string `json:"type"`
			} `json:"operators"`
			Actions []struct {
				Type   string `json:"type"`
				Params []struct {
					Name     string `json:"name"`
					Required bool   `json:"required"`
				} `json:"params"`
			} `json:"actions"`
			Placeholders []string `json:"placeholders"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &catalog))
		assert.Len(t, catalog.Triggers, 13)
		assert.Len(t, catalog.Actions, 16)
		assert.Len(t, catalog.Operators, 11)
		assert.NotEmpty(t, catalog.Fields)
		assert.Contains(t, catalog.Placeholders, "task.title")
		types := map[string]bool{}
		for _, a := range catalog.Actions {
			types[a.Type] = true
		}
		assert.True(t, types["task.set_status"])
	})

	t.Run("a project adds its custom fields", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/automations/catalog?project_id=1", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var catalog struct {
			Fields []struct {
				Name            string `json:"name"`
				Title           string `json:"title"`
				CustomFieldType string `json:"custom_field_type"`
			} `json:"fields"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &catalog))
		found := map[string]string{}
		for _, f := range catalog.Fields {
			if f.CustomFieldType != "" {
				found[f.Name] = f.CustomFieldType
			}
		}
		assert.Equal(t, map[string]string{"cf_1": "text", "cf_2": "select", "cf_3": "number", "cf_4": "multi_select"}, found)
	})

	t.Run("needs a login, and read access for a project", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/automations/catalog", "", "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/automations/catalog?project_id=1", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/automations/catalog", "", user2, "")
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestHumaAutomation_RulesCRUD(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	rule := createAutomationRule(t, e, 1, user1, priorityRule)
	assert.Equal(t, int64(1), rule.ProjectID)
	assert.Equal(t, int64(1), rule.OwnerID)
	assert.Equal(t, int64(1), rule.Revision)
	assert.True(t, rule.Enabled)
	assert.Equal(t, "task.created", rule.Trigger.Type)
	require.Len(t, rule.Actions, 1)

	t.Run("read sends an ETag and the maximum permission", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, autoRulePath(1, rule.ID), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotEmpty(t, rec.Header().Get("ETag"))
		var got automationRuleDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "Urgent first", got.Title)
		assert.Equal(t, 2, got.MaxPerm)
		assert.Equal(t, "like", got.Conditions["op"])

		req := httptest.NewRequest(http.MethodGet, autoRulePath(1, rule.ID), strings.NewReader(""))
		req.Header.Set("Authorization", "Bearer "+user1)
		req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
		cond := httptest.NewRecorder()
		e.ServeHTTP(cond, req)
		assert.Equal(t, http.StatusNotModified, cond.Code)
	})

	t.Run("list is paginated", func(t *testing.T) {
		createAutomationRule(t, e, 1, user1, strings.Replace(priorityRule, "Urgent first", "Second", 1))
		rec := humaRequest(t, e, http.MethodGet, autoRulesPath(1)+"?per_page=1&page=2", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var page struct {
			Items []automationRuleDTO `json:"items"`
			Total int64               `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
		assert.EqualValues(t, 2, page.Total)
		require.Len(t, page.Items, 1)
		assert.Equal(t, "Second", page.Items[0].Title)
	})

	t.Run("update replaces the document and bumps the revision", func(t *testing.T) {
		body := `{"title":"Renamed","enabled":false,"trigger":{"type":"task.updated","options":{"fields":["title"]}},
			"actions":[{"type":"task.add_comment","params":{"text":"changed"}}]}`
		rec := humaRequest(t, e, http.MethodPut, autoRulePath(1, rule.ID), body, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got automationRuleDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, int64(2), got.Revision)
		assert.False(t, got.Enabled)
		assert.Equal(t, "task.updated", got.Trigger.Type)
		assert.Nil(t, got.Conditions)
		db.AssertExists(t, "automation_rules", map[string]interface{}{"id": rule.ID, "title": "Renamed", "enabled": false}, false)
	})

	t.Run("patch merges", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPatch, autoRulePath(1, rule.ID), `{"enabled":true}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got automationRuleDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.True(t, got.Enabled)
		assert.Equal(t, "Renamed", got.Title)
		assert.Equal(t, int64(3), got.Revision)
	})

	t.Run("delete", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodDelete, autoRulePath(1, rule.ID), "", user1, "")
		assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodGet, autoRulePath(1, rule.ID), "", user1, "")
		require.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, models.ErrCodeAutomationRuleDoesNotExist, decodeProblem(t, rec.Body.Bytes()).Code)
	})
}

func TestHumaAutomation_ValidationErrors(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	post := func(body string) (int, problemDTO) {
		rec := humaRequest(t, e, http.MethodPost, autoRulesPath(1), body, user1, "")
		if rec.Code == http.StatusCreated {
			return rec.Code, problemDTO{}
		}
		return rec.Code, decodeProblem(t, rec.Body.Bytes())
	}
	issuePaths := func(p problemDTO) []string {
		raw, ok := p.Details["issues"].([]any)
		require.True(t, ok, "details: %v", p.Details)
		paths := []string{}
		for _, i := range raw {
			paths = append(paths, i.(map[string]any)["path"].(string))
		}
		return paths
	}

	t.Run("every problem comes back with its path", func(t *testing.T) {
		status, problem := post(`{
			"title": "bad",
			"trigger": {"type": "task.status_changed", "options": {"to_status_id": 1, "color": "red"}},
			"conditions": {"all": [{"field": "title", "op": "like", "value": "x"}, {"field": "nope", "op": "="}]},
			"actions": [
				{"type": "task.set_field", "params": {"field": "priority", "value": 2}},
				{"type": "task.set_status", "params": {"status_id": 9}},
				{"type": "task.explode"}
			]
		}`)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, models.ErrCodeAutomationRuleInvalid, problem.Code)
		paths := issuePaths(problem)
		assert.Contains(t, paths, "trigger.options.color")
		assert.Contains(t, paths, "conditions.all[1].field")
		assert.Contains(t, paths, "actions[2].type")
	})

	t.Run("ids are checked against the project", func(t *testing.T) {
		status, problem := post(`{"title":"x","trigger":{"type":"task.created"},
			"actions":[{"type":"task.set_custom_field","params":{"field_id":5,"value":"x"}},{"type":"task.move","params":{"project_id":9}}]}`)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.ElementsMatch(t, []string{"actions[0].params.field_id", "actions[1].params.project_id"}, issuePaths(problem))
	})

	t.Run("missing required parts", func(t *testing.T) {
		status, problem := post(`{"title":"x","trigger":{"type":"task.created"},"actions":[]}`)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, []string{"actions"}, issuePaths(problem))
		status, _ = post(`{"trigger":{"type":"task.created"},"actions":[]}`)
		assert.GreaterOrEqual(t, status, 400)
		status, _ = post(`{"title":"x","actions":[{"type":"task.set_done","params":{"done":true}}]}`)
		assert.GreaterOrEqual(t, status, 400)
	})

	db.AssertCount(t, "automation_rules", builder.Eq{"project_id": 1}, 0)
}

func TestHumaAutomation_Permissions(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)
	rule := createAutomationRule(t, e, 1, user1, priorityRule)

	endpoints := []struct {
		method, path, body string
	}{
		{http.MethodGet, autoRulesPath(1), ""},
		{http.MethodPost, autoRulesPath(1), priorityRule},
		{http.MethodGet, autoRulePath(1, rule.ID), ""},
		{http.MethodPut, autoRulePath(1, rule.ID), priorityRule},
		{http.MethodDelete, autoRulePath(1, rule.ID), ""},
		{http.MethodPost, autoRulePath(1, rule.ID) + "/test", `{"task_id":1}`},
		{http.MethodGet, autoRulePath(1, rule.ID) + "/runs", ""},
		{http.MethodGet, autoRulePath(1, rule.ID) + "/runs/1", ""},
		{http.MethodPost, autoRulePath(1, rule.ID) + "/runs/1/retry", ""},
	}
	for _, ep := range endpoints {
		t.Run("a stranger cannot "+ep.method+" "+ep.path, func(t *testing.T) {
			rec := humaRequest(t, e, ep.method, ep.path, ep.body, user2, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
	}

	t.Run("write access is not enough", func(t *testing.T) {
		// user1 may write to project 10 and read project 9, but is no admin there.
		for _, project := range []int64{9, 10} {
			rec := humaRequest(t, e, http.MethodGet, autoRulesPath(project), "", user1, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			rec = humaRequest(t, e, http.MethodPost, autoRulesPath(project), strings.Replace(priorityRule, "task.set_field", "task.set_field", 1), user1, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		}
		db.AssertCount(t, "automation_rules", builder.In("project_id", 9, 10), 0)
	})

	t.Run("a rule is not reachable through another project", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, autoRulePath(11, rule.ID), "", user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("unauthenticated", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, autoRulesPath(1), "", "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestHumaAutomation_EndToEnd(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	rule := createAutomationRule(t, e, 1, user1, priorityRule)

	// A task created through the API leaves an event in the outbox in the same transaction.
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/tasks", `{"title":"Urgent: print the report"}`, user1, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var task struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &task))
	db.AssertExists(t, "automation_events", map[string]interface{}{"task_id": task.ID, "name": "task.created", "status": models.AutomationEventPending}, false)

	rec = humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/tasks", `{"title":"Calm task"}`, user1, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	processed, err := automation.ProcessPending(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, processed)
	db.AssertExists(t, "tasks", map[string]interface{}{"id": task.ID, "priority": 5}, false)

	t.Run("runs", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, autoRulePath(1, rule.ID)+"/runs", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var page struct {
			Items []automationRunDTO `json:"items"`
			Total int64              `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
		require.EqualValues(t, 2, page.Total)
		assert.Equal(t, "skipped", page.Items[0].Status, "newest first: the calm task")
		assert.Equal(t, "conditions_not_met", page.Items[0].SkipReason)
		assert.Equal(t, "success", page.Items[1].Status)
		assert.Equal(t, "task.created", page.Items[1].EventName)
		assert.Empty(t, page.Items[1].Steps)

		rec = humaRequest(t, e, http.MethodGet, autoRulePath(1, rule.ID)+"/runs/"+strconv.FormatInt(page.Items[1].ID, 10), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var run automationRunDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &run))
		require.Len(t, run.Steps, 1)
		assert.Equal(t, "task.set_field", run.Steps[0].ActionType)
		assert.Equal(t, "success", run.Steps[0].Status)
		assert.Contains(t, run.Steps[0].Output, "priority to 5")

		rec = humaRequest(t, e, http.MethodGet, autoRulePath(1, rule.ID)+"/runs/99999", "", user1, "")
		require.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, models.ErrCodeAutomationRunDoesNotExist, decodeProblem(t, rec.Body.Bytes()).Code)

		// Only failed runs can be retried.
		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, rule.ID)+"/runs/"+strconv.FormatInt(page.Items[1].ID, 10)+"/retry", "", user1, "")
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeAutomationRunNotRetryable, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("a failed run can be retried", func(t *testing.T) {
		// A number field refuses text: the step fails for good.
		failing := createAutomationRule(t, e, 1, user1, `{"title":"fails","enabled":true,"trigger":{"type":"task.updated"},
			"actions":[{"type":"task.set_custom_field","params":{"field_id":3,"value":"not a number"}}]}`)
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/"+strconv.FormatInt(task.ID, 10), `{"title":"Urgent: print the report twice","project_id":1}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		_, err := automation.ProcessPending(context.Background())
		require.NoError(t, err)

		rec = humaRequest(t, e, http.MethodGet, autoRulePath(1, failing.ID)+"/runs", "", user1, "")
		var page struct {
			Items []automationRunDTO `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
		require.NotEmpty(t, page.Items)
		failedRun := page.Items[0]
		require.Equal(t, "failed", failedRun.Status, failedRun.Error)

		rec = humaRequest(t, e, http.MethodGet, autoRulePath(1, failing.ID)+"/runs/"+strconv.FormatInt(failedRun.ID, 10), "", user1, "")
		var detail automationRunDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
		require.Len(t, detail.Steps, 1)
		assert.Equal(t, "failed", detail.Steps[0].Status)
		assert.NotEmpty(t, detail.Steps[0].Error)

		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, failing.ID)+"/runs/"+strconv.FormatInt(failedRun.ID, 10)+"/retry", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var retried struct {
			Run automationRunDTO `json:"run"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &retried))
		assert.Equal(t, "running", retried.Run.Status)
	})
}

func TestHumaAutomation_DryRun(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	rule := createAutomationRule(t, e, 1, user1, `{"title":"dry","enabled":true,"trigger":{"type":"task.updated"},
		"conditions":{"field":"priority","op":"<","value":3},
		"actions":[
			{"type":"task.set_field","params":{"field":"title","value":"{{task.title}} [{{event.doer.username}}] {{task.nope}}"}},
			{"type":"task.add_label","params":{"label_id":1}}
		]}`)

	rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1", "", user1, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	before := rec.Body.String()

	rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, rule.ID)+"/test", `{"task_id":1}`, user1, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result struct {
		TaskID        int64 `json:"task_id"`
		ConditionsMet bool  `json:"conditions_met"`
		Steps         []struct {
			Position int64    `json:"position"`
			Type     string   `json:"type"`
			Summary  string   `json:"summary"`
			Warnings []string `json:"warnings"`
			Error    string   `json:"error"`
		} `json:"steps"`
		Notes []string `json:"notes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Equal(t, int64(1), result.TaskID)
	assert.True(t, result.ConditionsMet)
	require.Len(t, result.Steps, 2)
	assert.Contains(t, result.Steps[0].Summary, "task #1 [user1]")
	require.Len(t, result.Steps[0].Warnings, 1)
	assert.Contains(t, result.Steps[0].Warnings[0], "task.nope")
	assert.Contains(t, result.Steps[1].Summary, "add label 1")
	assert.NotEmpty(t, result.Notes)

	rec = humaRequest(t, e, http.MethodGet, "/api/v2/tasks/1", "", user1, "")
	assert.Equal(t, before, rec.Body.String(), "nothing was written")
	db.AssertCount(t, "automation_runs", builder.Eq{"rule_id": rule.ID}, 0)

	t.Run("validation", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, autoRulePath(1, rule.ID)+"/test", `{}`, user1, "")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		// Task 20 belongs to project 11.
		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, rule.ID)+"/test", `{"task_id":20}`, user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, 99999)+"/test", `{"task_id":1}`, user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})
}

// A change must not commit when its event cannot be written to the outbox: the handlers check the hook errors
// before they commit.
func TestHumaAutomation_ABrokenOutboxFailsTheRequest(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	createAutomationRule(t, e, 1, user1, priorityRule)
	db.AssertCount(t, "tasks", builder.Eq{"title": "lost without outbox"}, 0)

	s := db.NewSession()
	_, err = s.Exec("ALTER TABLE automation_events RENAME TO automation_events_broken")
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		s := db.NewSession()
		defer s.Close()
		_, err := s.Exec("ALTER TABLE automation_events_broken RENAME TO automation_events")
		require.NoError(t, err)
		require.NoError(t, s.Commit())
	}
	t.Cleanup(restore)

	rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/tasks", `{"title":"lost without outbox"}`, user1, "")
	assert.GreaterOrEqual(t, rec.Code, 500, rec.Body.String())
	db.AssertCount(t, "tasks", builder.Eq{"title": "lost without outbox"}, 0)

	restore()
	rec = humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/tasks", `{"title":"lost without outbox"}`, user1, "")
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}
