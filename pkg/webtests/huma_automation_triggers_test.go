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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation"
	"code.vikunja.io/api/pkg/automation/outbound"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hookRuleBody = `{
	"title": "Tickets",
	"enabled": true,
	"trigger": {"type": "webhook.incoming"},
	"actions": [
		{"type": "task.create", "params": {"title": "Ticket {{event.payload.id}}"}},
		{"type": "task.set_field", "params": {"field": "priority", "value": 3}}
	]
}`

func TestHumaAutomation_IncomingWebhook(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	rec := humaRequest(t, e, http.MethodPost, autoRulesPath(1), hookRuleBody, user1, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		ID         int64  `json:"id"`
		Token      string `json:"webhook_token"`
		URL        string `json:"webhook_url"`
		HasWebhook bool   `json:"has_webhook"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.NotEmpty(t, created.Token, "the token is shown once, on creation")
	assert.True(t, created.HasWebhook)
	assert.True(t, strings.HasSuffix(created.URL, "/api/v2/automation/hooks/"+created.Token), created.URL)
	hookPath := "/api/v2/automation/hooks/" + created.Token

	t.Run("reading the rule never shows the token again", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, autoRulePath(1, created.ID), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.NotContains(t, rec.Body.String(), created.Token)
		assert.NotContains(t, rec.Body.String(), "webhook_token")
		assert.Contains(t, rec.Body.String(), `"has_webhook":true`)
		db.AssertMissing(t, "automation_rules", map[string]interface{}{"webhook_token_hash": created.Token})
	})

	t.Run("the endpoint needs no authentication and answers 202", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, hookPath, `{"id": 77}`, "", "")
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		processed, err := automation.ProcessPending(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, processed)
		db.AssertExists(t, "tasks", map[string]interface{}{"title": "Ticket 77", "priority": 3}, false)
	})

	t.Run("an empty object is fine, a non object is a 400", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, hookPath, "{}", "", "")
		assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodPost, hookPath, `[1]`, "", "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeAutomationHookPayloadInvalid, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("an oversized body is refused", func(t *testing.T) {
		big := `{"x": "` + strings.Repeat("a", config.AutomationWebhookMaxBody.GetInt()+10) + `"}`
		rec := humaRequest(t, e, http.MethodPost, hookPath, big, "", "")
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	})

	t.Run("unknown tokens are 404", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/automation/hooks/nope", `{}`, "", "")
		require.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, models.ErrCodeAutomationHookNotFound, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("regenerating kills the old URL and needs project admin", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, autoRulePath(1, created.ID)+"/webhook-token", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, created.ID)+"/webhook-token", "", user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var fresh struct {
			Token string `json:"token"`
			URL   string `json:"url"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &fresh))
		require.NotEmpty(t, fresh.Token)
		assert.NotEqual(t, created.Token, fresh.Token)
		assert.Contains(t, fresh.URL, fresh.Token)
		rec = humaRequest(t, e, http.MethodPost, hookPath, `{}`, "", "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/automation/hooks/"+fresh.Token, `{}`, "", "")
		assert.Equal(t, http.StatusAccepted, rec.Code)
	})

	t.Run("rate limit per rule answers 429 with Retry-After", func(t *testing.T) {
		limited := createAutomationRule(t, e, 1, user1, hookRuleBody)
		_ = limited
		rec := humaRequest(t, e, http.MethodPost, autoRulePath(1, limited.ID)+"/webhook-token", "", user1, "")
		require.Equal(t, http.StatusCreated, rec.Code)
		var fresh struct {
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &fresh))
		config.AutomationWebhookRateLimit.Set(2)
		defer config.AutomationWebhookRateLimit.Set(60)
		for i := 0; i < 2; i++ {
			rec = humaRequest(t, e, http.MethodPost, "/api/v2/automation/hooks/"+fresh.Token, `{}`, "", "")
			require.Equal(t, http.StatusAccepted, rec.Code)
		}
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/automation/hooks/"+fresh.Token, `{}`, "", "")
		require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
		assert.NotEmpty(t, rec.Header().Get("Retry-After"))
		assert.Equal(t, models.ErrCodeAutomationHookRateLimited, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("a rule without the webhook trigger cannot regenerate", func(t *testing.T) {
		plain := createAutomationRule(t, e, 1, user1, priorityRule)
		rec := humaRequest(t, e, http.MethodPost, autoRulePath(1, plain.ID)+"/webhook-token", "", user1, "")
		assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	})

	t.Run("a rule that works on a task without creating one is refused", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, autoRulesPath(1), `{"title":"x","trigger":{"type":"webhook.incoming"},"actions":[{"type":"task.set_done","params":{"done":true}}]}`, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeAutomationRuleInvalid, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("dry run takes a simulated payload", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, autoRulePath(1, created.ID)+"/test", `{"payload": {"id": 5}}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "Ticket 5")
	})
}

func TestHumaAutomation_IncomingWebhookSignature(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	rec := humaRequest(t, e, http.MethodPut, "/api/v2/projects/1/automation-secrets/HOOK_KEY", `{"value":"s3cret"}`, user1, "")
	require.Less(t, rec.Code, 300, rec.Body.String())
	body := strings.Replace(hookRuleBody, `"trigger": {"type": "webhook.incoming"}`, `"trigger": {"type": "webhook.incoming", "options": {"secret": "HOOK_KEY"}}`, 1)
	rec = humaRequest(t, e, http.MethodPost, autoRulesPath(1), body, user1, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		Token string `json:"webhook_token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	payload := `{"id": 1}`
	bodyOnly := hmac.New(sha256.New, []byte("s3cret"))
	bodyOnly.Write([]byte(payload))
	oldScheme := "sha256=" + hex.EncodeToString(bodyOnly.Sum(nil))

	post := func(body, ts, sig string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/automation/hooks/"+created.Token, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if sig != "" {
			req.Header.Set("X-Vikunja-Signature", sig)
		}
		if ts != "" {
			req.Header.Set("X-Vikunja-Timestamp", ts)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}
	sign := func(body string, at time.Time) (string, string) {
		return automation.WebhookSignature("s3cret", at, []byte(body))
	}

	ts, good := sign(payload, time.Now())
	assert.Equal(t, http.StatusUnauthorized, post(payload, "", "").Code)
	assert.Equal(t, http.StatusUnauthorized, post(payload, ts, "sha256=00").Code)
	assert.Equal(t, http.StatusUnauthorized, post(payload, ts, oldScheme).Code, "the signature of the body alone is not accepted any more")
	assert.Equal(t, http.StatusUnauthorized, post(payload, "", good).Code, "no timestamp")
	assert.Equal(t, http.StatusUnauthorized, post(`{"id": 2}`, ts, good).Code, "tampered body")

	oldTS, oldSig := sign(payload, time.Now().Add(-time.Hour))
	stale := post(payload, oldTS, oldSig)
	assert.Equal(t, http.StatusUnauthorized, stale.Code, "stale timestamp")
	assert.Equal(t, models.ErrCodeAutomationHookSignatureInvalid, decodeProblem(t, stale.Body.Bytes()).Code)
	futureTS, futureSig := sign(payload, time.Now().Add(time.Hour))
	assert.Equal(t, http.StatusUnauthorized, post(payload, futureTS, futureSig).Code, "timestamp in the future")

	assert.Equal(t, http.StatusAccepted, post(payload, ts, good).Code)
	replay := post(payload, ts, good)
	assert.Equal(t, http.StatusUnauthorized, replay.Code, "the same call again is a replay")
	wrong := post(payload, ts, "sha256="+strings.Repeat("0", 64))
	assert.JSONEq(t, wrong.Body.String(), replay.Body.String(), "a replay answers like a wrong signature")
	ts2, good2 := sign(payload, time.Now().Add(time.Second))
	assert.Equal(t, http.StatusAccepted, post(payload, ts2, good2).Code, "a new timestamp is a new call")
}

func TestHumaAutomation_ManualTrigger(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	manual := createAutomationRule(t, e, 1, user1, `{"title":"Escalate","enabled":true,"trigger":{"type":"manual","options":{"label":"Escalate now"}},
		"actions":[{"type":"task.set_field","params":{"field":"priority","value":5}}]}`)
	off := createAutomationRule(t, e, 1, user1, `{"title":"Off","enabled":false,"trigger":{"type":"manual"},
		"actions":[{"type":"task.set_field","params":{"field":"priority","value":1}}]}`)
	other := createAutomationRule(t, e, 1, user1, priorityRule)

	rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/tasks", `{"title":"Server is down"}`, user1, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var task struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &task))
	taskBody := `{"task_id": ` + strconv.FormatInt(task.ID, 10) + `}`

	t.Run("the list shows enabled manual rules with their button text", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/manual-automations", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var list []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Label string `json:"label"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
		require.Len(t, list, 1)
		assert.Equal(t, manual.ID, list[0].ID)
		assert.Equal(t, "Escalate now", list[0].Label)
		assert.NotContains(t, rec.Body.String(), "actions")

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/manual-automations", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("run: queued, then in the run history", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, autoRulePath(1, manual.ID)+"/run", taskBody, user1, "")
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"status":"queued"`)
		_, err := automation.ProcessPending(context.Background())
		require.NoError(t, err)
		db.AssertExists(t, "tasks", map[string]interface{}{"id": task.ID, "priority": 5}, false)

		rec = humaRequest(t, e, http.MethodGet, autoRulePath(1, manual.ID)+"/runs", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code)
		var page struct {
			Items []automationRunDTO `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
		require.Len(t, page.Items, 1)
		assert.Equal(t, "success", page.Items[0].Status)
		assert.Equal(t, "rule.manual", page.Items[0].EventName)
	})

	t.Run("refusals", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, autoRulePath(1, off.ID)+"/run", taskBody, user1, "")
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeAutomationRuleNotRunnable, decodeProblem(t, rec.Body.Bytes()).Code)
		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, other.ID)+"/run", taskBody, user1, "")
		assert.Equal(t, http.StatusConflict, rec.Code)
		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, manual.ID)+"/run", taskBody, user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, "no write access to the task")
		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, manual.ID)+"/run", `{"task_id": 0}`, user1, "")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		rec = humaRequest(t, e, http.MethodPost, autoRulePath(1, manual.ID)+"/run", taskBody, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestHumaAutomation_CatalogHasTheNewTriggersAndActions(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	rec := humaRequest(t, e, http.MethodGet, "/api/v2/automations/catalog", "", user1, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	for _, want := range []string{`"manual"`, `"schedule"`, `"task.overdue"`, `"task.due_soon"`, `"webhook.incoming"`, `"task.create"`, `"wait"`, `"notify"`, `event.payload.`} {
		assert.Contains(t, body, want)
	}
}

func TestAutomation_RequestsSentByTheEngineAreRefusedByTheAPI(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	rec := humaRequest(t, e, http.MethodPost, autoRulesPath(1), hookRuleBody, user1, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		Token string `json:"webhook_token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	send := func(method, path, body string, marked bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+user1)
		if marked {
			req.Header.Set(outbound.MarkerHeader, "1")
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}
	hook := "/api/v2/automation/hooks/" + created.Token

	t.Run("the hook endpoint", func(t *testing.T) {
		assert.Equal(t, http.StatusLoopDetected, send(http.MethodPost, hook, `{}`, true).Code)
		assert.Equal(t, http.StatusAccepted, send(http.MethodPost, hook, `{}`, false).Code)
	})
	t.Run("the v2 task API", func(t *testing.T) {
		assert.Equal(t, http.StatusLoopDetected, send(http.MethodPost, "/api/v2/projects/1/tasks", `{"title":"v2 from a rule"}`, true).Code)
		db.AssertMissing(t, "tasks", map[string]interface{}{"title": "v2 from a rule"})
		assert.Equal(t, http.StatusCreated, send(http.MethodPost, "/api/v2/projects/1/tasks", `{"title":"from a person"}`, false).Code)
	})
	t.Run("the v1 task API", func(t *testing.T) {
		assert.Equal(t, http.StatusLoopDetected, send(http.MethodPut, "/api/v1/projects/1/tasks", `{"title":"from a rule"}`, true).Code)
		assert.Equal(t, http.StatusCreated, send(http.MethodPut, "/api/v1/projects/1/tasks", `{"title":"from a person"}`, false).Code)
		db.AssertMissing(t, "tasks", map[string]interface{}{"title": "from a rule"})
	})
}
