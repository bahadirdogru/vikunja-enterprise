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
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func secretsPath(project int64) string {
	return "/api/v2/projects/" + strconv.FormatInt(project, 10) + "/automation-secrets"
}

func TestHumaAutomationSecrets(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	const value = "hunter2-the-secret-value"

	t.Run("set creates, set again replaces, and no response has the value", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, secretsPath(1)+"/CHAT_TOKEN", `{"value":"`+value+`"}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), value)
		var got struct {
			Name        string `json:"name"`
			ProjectID   int64  `json:"project_id"`
			CreatedByID int64  `json:"created_by_id"`
			Unreadable  bool   `json:"unreadable"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "CHAT_TOKEN", got.Name)
		assert.EqualValues(t, 1, got.ProjectID)
		assert.EqualValues(t, 1, got.CreatedByID)
		assert.False(t, got.Unreadable)
		assert.NotContains(t, rec.Body.String(), `"value"`)
		assert.NotContains(t, rec.Body.String(), "v1:")

		rec = humaRequest(t, e, http.MethodPut, secretsPath(1)+"/CHAT_TOKEN", `{"value":"another-value-entirely"}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		db.AssertCount(t, "automation_secrets", builder.Eq{"project_id": 1}, 1)

		rec = humaRequest(t, e, http.MethodPut, secretsPath(1)+"/SIGNING_KEY", `{"value":"`+value+`"}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("list has names and timestamps only", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, secretsPath(1), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		for _, forbidden := range []string{value, "another-value-entirely", "v1:", `"value"`} {
			assert.NotContains(t, rec.Body.String(), forbidden)
		}
		var page struct {
			Items []struct {
				Name       string `json:"name"`
				Created    string `json:"created"`
				Updated    string `json:"updated"`
				Unreadable bool   `json:"unreadable"`
			} `json:"items"`
			Total int64 `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
		assert.EqualValues(t, 2, page.Total)
		require.Len(t, page.Items, 2)
		assert.Equal(t, "CHAT_TOKEN", page.Items[0].Name)
		assert.Equal(t, "SIGNING_KEY", page.Items[1].Name)
		assert.NotEmpty(t, page.Items[0].Created)
		assert.NotEmpty(t, page.Items[0].Updated)
	})

	t.Run("there is no way to read a value", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, secretsPath(1)+"/CHAT_TOKEN", "", user1, "")
		assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), "another-value-entirely")
	})

	t.Run("a secret whose key changed is listed as unreadable", func(t *testing.T) {
		previous := config.ServiceSecret.GetString()
		config.ServiceSecret.Set("a-different-service-secret-after-a-restart")
		defer config.ServiceSecret.Set(previous)
		rec := humaRequest(t, e, http.MethodGet, secretsPath(1), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, 2, strings.Count(rec.Body.String(), `"unreadable":true`))
	})

	t.Run("only admins of the project", func(t *testing.T) {
		for _, token := range []string{user2, ""} {
			want := http.StatusForbidden
			if token == "" {
				want = http.StatusUnauthorized
			}
			assert.Equal(t, want, humaRequest(t, e, http.MethodGet, secretsPath(1), "", token, "").Code)
			assert.Equal(t, want, humaRequest(t, e, http.MethodPut, secretsPath(1)+"/X", `{"value":"v"}`, token, "").Code)
			assert.Equal(t, want, humaRequest(t, e, http.MethodDelete, secretsPath(1)+"/CHAT_TOKEN", "", token, "").Code)
		}
		// user1 can write to project 10 and read project 9, but is no admin of either.
		for _, project := range []int64{9, 10} {
			assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodGet, secretsPath(project), "", user1, "").Code, "project %d", project)
			assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodPut, secretsPath(project)+"/X", `{"value":"v"}`, user1, "").Code, "project %d", project)
			assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodDelete, secretsPath(project)+"/X", "", user1, "").Code, "project %d", project)
		}
		db.AssertMissing(t, "automation_secrets", map[string]interface{}{"name": "X"})
	})

	t.Run("invalid names and values", func(t *testing.T) {
		for _, name := range []string{"1BAD", "has-dash", strings.Repeat("A", 65), "a.b"} {
			rec := humaRequest(t, e, http.MethodPut, secretsPath(1)+"/"+name, `{"value":"v"}`, user1, "")
			assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, rec.Code, "%s: %s", name, rec.Body.String())
		}
		rec := humaRequest(t, e, http.MethodPut, secretsPath(1)+"/OK_NAME", `{"value":""}`, user1, "")
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodPut, secretsPath(1)+"/OK_NAME", `{}`, user1, "")
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodPut, secretsPath(1)+"/OK_NAME", `{"value":"`+strings.Repeat("x", 9000)+`"}`, user1, "")
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, rec.Code, rec.Body.String())
		db.AssertMissing(t, "automation_secrets", map[string]interface{}{"name": "OK_NAME"})
	})

	t.Run("a rule can use the secret and a rule cannot name one that does not exist", func(t *testing.T) {
		body := `{"title":"Hook","enabled":true,"trigger":{"type":"task.created"},"actions":[{"type":"http.request","params":{
			"url":"https://chat.example.com/hook","method":"POST","headers":{"Authorization":"Bearer {{secrets.CHAT_TOKEN}}"},
			"body":"{\"text\":\"{{task.title}}\"}","secret":"SIGNING_KEY"}}]}`
		rule := createAutomationRule(t, e, 1, user1, body)
		assert.Equal(t, "http.request", rule.Actions[0]["type"])

		missing := strings.Replace(body, "SIGNING_KEY", "NOT_THERE", 1)
		rec := humaRequest(t, e, http.MethodPost, autoRulesPath(1), missing, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "actions[0].params.secret")
		assert.Contains(t, rec.Body.String(), "does not exist")

		forbidden := `{"title":"Leak","trigger":{"type":"task.created"},"actions":[{"type":"task.add_comment","params":{"text":"{{secrets.CHAT_TOKEN}}"}}]}`
		rec = humaRequest(t, e, http.MethodPost, autoRulesPath(1), forbidden, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "actions[0].params")
		assert.Contains(t, rec.Body.String(), "secrets")
	})

	t.Run("delete", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodDelete, secretsPath(1)+"/SIGNING_KEY", "", user1, "")
		assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		db.AssertMissing(t, "automation_secrets", map[string]interface{}{"project_id": 1, "name": "SIGNING_KEY"})
		rec = humaRequest(t, e, http.MethodDelete, secretsPath(1)+"/SIGNING_KEY", "", user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Contains(t, rec.Body.String(), "22007")
	})
}

func TestHumaAutomationScripts(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	t.Run("the catalog describes the script actions, kinds and the API", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/automations/catalog", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var catalog struct {
			Actions []struct {
				Type        string `json:"type"`
				Description string `json:"description"`
				Params      []struct {
					Name        string `json:"name"`
					Kind        string `json:"kind"`
					Language    string `json:"language"`
					Description string `json:"description"`
					Required    bool   `json:"required"`
					Default     any    `json:"default"`
				} `json:"params"`
			} `json:"actions"`
			Script struct {
				Language            string `json:"language"`
				ActionConvention    string `json:"action_convention"`
				ConditionConvention string `json:"condition_convention"`
				Limits              []struct {
					ConfigKey string `json:"config_key"`
					Value     string `json:"value"`
				} `json:"limits"`
				API []struct {
					Name        string `json:"name"`
					Signature   string `json:"signature"`
					Description string `json:"description"`
					Mutating    bool   `json:"mutating"`
					InCondition bool   `json:"in_condition"`
				} `json:"api"`
			} `json:"script"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &catalog))

		kinds := map[string]map[string]string{}
		for _, a := range catalog.Actions {
			kinds[a.Type] = map[string]string{}
			for _, p := range a.Params {
				kinds[a.Type][p.Name] = p.Kind
				if a.Type == "script.run" && p.Name == "source" {
					assert.Equal(t, "javascript", p.Language)
					assert.True(t, p.Required)
					assert.NotEmpty(t, p.Description)
				}
			}
		}
		assert.Equal(t, map[string]string{"source": "code"}, kinds["script.run"])
		assert.Equal(t, map[string]string{"url": "template", "method": "enum", "headers": "map", "body": "template", "secret": "secret"}, kinds["http.request"])

		assert.Equal(t, "javascript", catalog.Script.Language)
		assert.NotEmpty(t, catalog.Script.ActionConvention)
		assert.Contains(t, catalog.Script.ConditionConvention, "return")
		names := map[string]bool{}
		for _, a := range catalog.Script.API {
			names[a.Name] = true
			assert.NotEmpty(t, a.Signature, a.Name)
			assert.NotEmpty(t, a.Description, a.Name)
		}
		for _, want := range []string{"event", "task", "project", "vikunja.tasks.find", "http.fetch", "secrets.get", "log", "vikunja.status.set"} {
			assert.True(t, names[want], want)
		}
		limits := map[string]string{}
		for _, l := range catalog.Script.Limits {
			limits[l.ConfigKey] = l.Value
		}
		assert.Equal(t, "5s", limits["automation.scripts.timeout"])
		assert.Equal(t, "100", limits["automation.scripts.maxapicalls"])
		assert.Equal(t, "true", limits["automation.scripts.enabled"])
	})

	t.Run("a script rule is saved and a syntax error is a typed 400 with the position", func(t *testing.T) {
		ok := `{"title":"S","enabled":true,"trigger":{"type":"task.created"},
			"conditions":{"script":"return task.priority > 1"},
			"actions":[{"type":"script.run","params":{"source":"vikunja.tasks.comment(task.id, 'hi')"}}]}`
		rule := createAutomationRule(t, e, 1, user1, ok)
		assert.Equal(t, "return task.priority > 1", rule.Conditions["script"])

		bad := `{"title":"S","trigger":{"type":"task.created"},"actions":[{"type":"task.set_done","params":{"done":true}},
			{"type":"script.run","params":{"source":"var a = 1;\nvar b = ;"}}]}`
		rec := humaRequest(t, e, http.MethodPost, autoRulesPath(1), bad, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var problem struct {
			Code    int `json:"code"`
			Details struct {
				Issues []struct {
					Path    string `json:"path"`
					Message string `json:"message"`
					Line    int    `json:"line"`
					Column  int    `json:"column"`
				} `json:"issues"`
			} `json:"details"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
		assert.Equal(t, 22002, problem.Code)
		require.Len(t, problem.Details.Issues, 1)
		assert.Equal(t, "actions[1].params.source", problem.Details.Issues[0].Path)
		assert.Equal(t, 2, problem.Details.Issues[0].Line)
		assert.Equal(t, 9, problem.Details.Issues[0].Column)
	})

	t.Run("the kill switch is a typed 400", func(t *testing.T) {
		config.AutomationScriptsEnabled.Set(false)
		defer config.AutomationScriptsEnabled.Set(true)
		body := `{"title":"S","trigger":{"type":"task.created"},"actions":[{"type":"script.run","params":{"source":"return 1"}}]}`
		rec := humaRequest(t, e, http.MethodPost, autoRulesPath(1), body, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var problem struct {
			Code int `json:"code"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
		assert.Equal(t, 22005, problem.Code)

		plain := `{"title":"P","trigger":{"type":"task.created"},"actions":[{"type":"task.set_done","params":{"done":true}}]}`
		assert.Equal(t, http.StatusCreated, humaRequest(t, e, http.MethodPost, autoRulesPath(1), plain, user1, "").Code)
	})

	t.Run("test run of a script rule", func(t *testing.T) {
		body := `{"title":"S","trigger":{"type":"task.created"},"actions":[{"type":"script.run","params":{"source":"vikunja.tasks.update(task.id, {title: 'x'}); log('hello'); return task.id"}}]}`
		rule := createAutomationRule(t, e, 1, user1, body)
		rec := humaRequest(t, e, http.MethodPost, autoRulePath(1, rule.ID)+"/test", `{"task_id":1}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "would call vikunja.tasks.update(1")
		assert.Contains(t, rec.Body.String(), "hello")
		db.AssertExists(t, "tasks", map[string]interface{}{"id": 1, "title": "task #1"}, false)
	})
}
