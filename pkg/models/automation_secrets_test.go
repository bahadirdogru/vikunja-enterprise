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
	"encoding/json"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setAutomationSecret(t *testing.T, project int64, name, value string, a web.Auth) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	secret := &AutomationSecret{ProjectID: project, Name: name, NewValue: value}
	can, err := secret.CanUpdate(s, a)
	if err != nil || !can {
		_ = s.Rollback()
		if err == nil {
			err = ErrGenericForbidden{}
		}
		return err
	}
	if err = secret.Update(s, a); err != nil {
		_ = s.Rollback()
		return err
	}
	return s.Commit()
}

func withServiceSecret(t *testing.T, secret string) {
	t.Helper()
	previous := config.ServiceSecret.GetString()
	config.ServiceSecret.Set(secret)
	t.Cleanup(func() { config.ServiceSecret.Set(previous) })
}

func TestAutomationSecret_EncryptionRoundTrip(t *testing.T) {
	stored, err := EncryptAutomationSecret(1, "KEY", "the value")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(stored, "v1:"), "the scheme is versioned")
	assert.NotContains(t, stored, "the value")

	plain, err := DecryptAutomationSecret(1, "KEY", stored)
	require.NoError(t, err)
	assert.Equal(t, "the value", plain)

	t.Run("a random nonce per value", func(t *testing.T) {
		again, err := EncryptAutomationSecret(1, "KEY", "the value")
		require.NoError(t, err)
		assert.NotEqual(t, stored, again)
	})

	t.Run("empty and large values and unicode", func(t *testing.T) {
		for _, v := range []string{"", "ä€😀 ünï", strings.Repeat("z", MaxAutomationSecretSize)} {
			enc, err := EncryptAutomationSecret(2, "N", v)
			require.NoError(t, err)
			dec, err := DecryptAutomationSecret(2, "N", enc)
			require.NoError(t, err)
			assert.Equal(t, v, dec)
		}
	})

	t.Run("a changed service.secret makes the value unreadable", func(t *testing.T) {
		withServiceSecret(t, "a-completely-different-service-secret")
		_, err := DecryptAutomationSecret(1, "KEY", stored)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "the value")
	})

	t.Run("the value is bound to the project and the name", func(t *testing.T) {
		_, err := DecryptAutomationSecret(1, "OTHER", stored)
		assert.Error(t, err, "another name")
		_, err = DecryptAutomationSecret(2, "KEY", stored)
		assert.Error(t, err, "another project")
	})

	t.Run("tampering and garbage", func(t *testing.T) {
		for _, bad := range []string{"", "v1:", "v1:!!!", "v2:" + stored[3:], stored[:len(stored)-4] + "AAAA", "plain text", "v1:AAAA"} {
			_, err := DecryptAutomationSecret(1, "KEY", bad)
			assert.Error(t, err, bad)
		}
	})

	t.Run("no service.secret", func(t *testing.T) {
		withServiceSecret(t, "")
		_, err := EncryptAutomationSecret(1, "KEY", "x")
		assert.Error(t, err)
	})
}

func TestAutomationSecret_CRUDAndPermissions(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	require.NoError(t, setAutomationSecret(t, 1, "API_KEY", "value-one", autoUser1))
	require.NoError(t, setAutomationSecret(t, 1, "OTHER_KEY", "value-two", autoUser1))

	// Sessions are short-lived: the shared-cache test database locks tables for the whole transaction.
	s := db.NewSession()
	row := &AutomationSecret{}
	has, err := s.Where("project_id = 1 AND name = ?", "API_KEY").Get(row)
	s.Close()
	require.NoError(t, err)
	require.True(t, has)
	assert.NotContains(t, row.Value, "value-one", "stored encrypted")
	assert.EqualValues(t, 1, row.CreatedByID)

	t.Run("list has names and timestamps, never values", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		result, count, total, err := (&AutomationSecret{ProjectID: 1}).ReadAll(s, autoUser1, "", 1, 50)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		assert.EqualValues(t, 2, total)
		items := result.([]*AutomationSecret)
		assert.Equal(t, "API_KEY", items[0].Name)
		assert.Equal(t, "OTHER_KEY", items[1].Name)
		assert.False(t, items[0].Unreadable)
		raw, err := json.Marshal(result)
		require.NoError(t, err)
		for _, secret := range []string{"value-one", "value-two", row.Value, "NewValue", `"value"`} {
			assert.NotContains(t, string(raw), secret)
		}
		assert.Contains(t, string(raw), `"name":"API_KEY"`)
		assert.Contains(t, string(raw), `"unreadable":false`)
	})

	t.Run("setting again replaces the value and keeps the secret", func(t *testing.T) {
		require.NoError(t, setAutomationSecret(t, 1, "API_KEY", "value-three", autoUser1))
		s := db.NewSession()
		defer s.Close()
		secrets, err := LoadAutomationSecrets(s, 1)
		require.NoError(t, err)
		assert.Equal(t, "value-three", secrets["API_KEY"].Value)
		assert.Equal(t, "value-two", secrets["OTHER_KEY"].Value)
		n, err := s.Where("project_id = 1").Count(&AutomationSecret{})
		require.NoError(t, err)
		assert.EqualValues(t, 2, n)
	})

	t.Run("only project admins", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		// user1 has write but no admin access to project 10 and read access to 9; user2 has no access at all.
		for _, project := range []int64{9, 10} {
			err := setAutomationSecret(t, project, "X", "v", autoUser1)
			assert.ErrorIs(t, err, ErrGenericForbidden{}, "project %d", project)
			can, err := (&AutomationSecret{ProjectID: project, Name: "X"}).CanDelete(s, autoUser1)
			require.NoError(t, err)
			assert.False(t, can)
			_, _, _, err = (&AutomationSecret{ProjectID: project}).ReadAll(s, autoUser1, "", 1, 10)
			assert.ErrorIs(t, err, ErrGenericForbidden{})
		}
		assert.Error(t, setAutomationSecret(t, 1, "X", "v", autoUser2))
		_, _, _, err := (&AutomationSecret{ProjectID: 1}).ReadAll(s, autoUser2, "", 1, 10)
		assert.ErrorIs(t, err, ErrGenericForbidden{})
		// A link share or any non-user cannot administer.
		can, _, err := (&AutomationSecret{ProjectID: 1, Name: "API_KEY"}).CanRead(s, &LinkSharing{ID: 1, ProjectID: 1})
		assert.False(t, can)
		_ = err
		db.AssertMissing(t, "automation_secrets", map[string]interface{}{"name": "X"})
	})

	t.Run("invalid names and values", func(t *testing.T) {
		for _, name := range []string{"", "1ABC", "with space", "dash-ed", strings.Repeat("A", 65), "ünicode", "a.b", "../etc"} {
			err := setAutomationSecret(t, 1, name, "v", autoUser1)
			assert.True(t, IsErrAutomationSecretInvalid(err), "%q: %v", name, err)
		}
		assert.NoError(t, setAutomationSecret(t, 1, strings.Repeat("A", 64), "v", autoUser1))
		assert.NoError(t, setAutomationSecret(t, 1, "_underscore9", "v", autoUser1))
		assert.True(t, IsErrAutomationSecretInvalid(setAutomationSecret(t, 1, "EMPTY", "", autoUser1)))
		assert.True(t, IsErrAutomationSecretInvalid(setAutomationSecret(t, 1, "HUGE", strings.Repeat("x", MaxAutomationSecretSize+1), autoUser1)))
		assert.NoError(t, setAutomationSecret(t, 1, "MAX", strings.Repeat("x", MaxAutomationSecretSize), autoUser1))
	})

	t.Run("names differing only in case are one secret", func(t *testing.T) {
		require.NoError(t, setAutomationSecret(t, 1, "CaseTest", "v", autoUser1))
		s := db.NewSession()
		defer s.Close()
		found, err := getAutomationSecretByName(s, 1, "casetest")
		require.NoError(t, err)
		assert.Nil(t, found, "the lookup is exact")
	})

	t.Run("delete", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		secret := &AutomationSecret{ProjectID: 1, Name: "OTHER_KEY"}
		require.NoError(t, secret.Delete(s, autoUser1))
		require.NoError(t, s.Commit())
		db.AssertMissing(t, "automation_secrets", map[string]interface{}{"project_id": 1, "name": "OTHER_KEY"})
		s = db.NewSession()
		defer s.Close()
		err := (&AutomationSecret{ProjectID: 1, Name: "OTHER_KEY"}).Delete(s, autoUser1)
		assert.True(t, IsErrAutomationSecretDoesNotExist(err))
	})
}

func TestAutomationSecret_UnreadableAfterTheServiceSecretChanged(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	require.NoError(t, setAutomationSecret(t, 1, "OLD", "stored-with-the-old-secret", autoUser1))

	withServiceSecret(t, "the-new-service-secret-after-a-restart")
	s := db.NewSession()
	result, _, _, err := (&AutomationSecret{ProjectID: 1}).ReadAll(s, autoUser1, "", 1, 10)
	require.NoError(t, err)
	items := result.([]*AutomationSecret)
	require.Len(t, items, 1)
	assert.True(t, items[0].Unreadable)
	raw, _ := json.Marshal(items)
	assert.Contains(t, string(raw), `"unreadable":true`)

	secrets, err := LoadAutomationSecrets(s, 1)
	require.NoError(t, err)
	require.Contains(t, secrets, "OLD")
	assert.Error(t, secrets["OLD"].Err)
	assert.True(t, IsErrAutomationSecretUnreadable(secrets["OLD"].Err))
	assert.Empty(t, secrets["OLD"].Value)
	assert.NotContains(t, secrets["OLD"].Err.Error(), "stored-with")
	s.Close()

	t.Run("setting it again fixes it", func(t *testing.T) {
		require.NoError(t, setAutomationSecret(t, 1, "OLD", "fresh", autoUser1))
		s := db.NewSession()
		defer s.Close()
		secrets, err := LoadAutomationSecrets(s, 1)
		require.NoError(t, err)
		assert.NoError(t, secrets["OLD"].Err)
		assert.Equal(t, "fresh", secrets["OLD"].Value)
	})
}

func TestAutomationSecret_RuleValidation(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	require.NoError(t, setAutomationSecret(t, 1, "HOOK_TOKEN", "t", autoUser1))
	require.NoError(t, setAutomationSecret(t, 1, "SIGNING", "s", autoUser1))

	http := func(params map[string]any) *spec.Action {
		if _, has := params["url"]; !has {
			params["url"] = "https://chat.example.com/hook"
		}
		return &spec.Action{Type: spec.ActionHTTPRequest, Params: params}
	}

	t.Run("a valid rule with secrets in headers, body and the signing secret", func(t *testing.T) {
		r := autoRule(1, spec.TriggerTaskCreated, http(map[string]any{
			"headers": map[string]any{"Authorization": "Bearer {{secrets.HOOK_TOKEN}}"},
			"body":    `{"t": "{{task.title}}", "k": "{{secrets.HOOK_TOKEN}}"}`,
			"secret":  "SIGNING",
		}))
		require.NoError(t, saveAutomationRule(t, r, autoUser1))
	})

	t.Run("a secret that does not exist is reported with its path", func(t *testing.T) {
		r := autoRule(1, spec.TriggerTaskCreated, setPriority(1), http(map[string]any{
			"headers": map[string]any{"X-Key": "{{secrets.NOPE}}"},
			"body":    "{{secrets.ALSO_NOPE}}",
			"secret":  "NOPE_TOO",
		}))
		issues := ruleIssues(t, saveAutomationRule(t, r, autoUser1))
		assert.Contains(t, issues["actions[1].params.headers.X-Key"], `"NOPE" does not exist`)
		assert.Contains(t, issues["actions[1].params.body"], `"ALSO_NOPE" does not exist`)
		assert.Contains(t, issues["actions[1].params.secret"], `"NOPE_TOO" does not exist`)
	})

	t.Run("a secret of another project does not count", func(t *testing.T) {
		require.NoError(t, setAutomationSecret(t, 11, "ELSEWHERE", "v", autoUser1))
		r := autoRule(1, spec.TriggerTaskCreated, http(map[string]any{"secret": "ELSEWHERE"}))
		issues := ruleIssues(t, saveAutomationRule(t, r, autoUser1))
		assert.Contains(t, issues["actions[0].params.secret"], "does not exist")
	})

	t.Run("placeholders for secrets are refused everywhere else", func(t *testing.T) {
		for name, a := range map[string]*spec.Action{
			"comment": {Type: spec.ActionAddComment, Params: map[string]any{"text": "token: {{secrets.HOOK_TOKEN}}"}},
			"title":   {Type: spec.ActionSetField, Params: map[string]any{"field": "title", "value": "{{secrets.HOOK_TOKEN}}"}},
			"subtask": {Type: spec.ActionCreateSubtask, Params: map[string]any{"title": "{{secrets.HOOK_TOKEN}}"}},
			"url":     http(map[string]any{"url": "https://x.example.com/{{secrets.HOOK_TOKEN}}"}),
		} {
			r := autoRule(1, spec.TriggerTaskCreated, a)
			issues := ruleIssues(t, saveAutomationRule(t, r, autoUser1))
			assert.NotEmpty(t, issues, name)
			found := false
			for _, msg := range issues {
				found = found || strings.Contains(msg, "secrets")
			}
			assert.True(t, found, "%s: %v", name, issues)
		}
	})

	t.Run("an http request without a script is not affected by the script switch", func(t *testing.T) {
		config.AutomationScriptsEnabled.Set(false)
		defer config.AutomationScriptsEnabled.Set(true)
		r := autoRule(1, spec.TriggerTaskCreated, http(map[string]any{"secret": "SIGNING"}))
		assert.NoError(t, saveAutomationRule(t, r, autoUser1))
	})
}

func TestAutomationScript_RuleValidation(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	script := func(src string) *spec.Action {
		return &spec.Action{Type: spec.ActionScriptRun, Params: map[string]any{"source": src}}
	}

	t.Run("a script rule is saved and stays as it was", func(t *testing.T) {
		r := autoRule(1, spec.TriggerTaskCreated, script("return 1"))
		r.Conditions = &spec.Condition{Script: "return task.priority > 1"}
		require.NoError(t, saveAutomationRule(t, r, autoUser1))
		stored := reloadAutomationRule(t, r.ID)
		assert.Equal(t, "return 1", stored.Actions[0].Params["source"])
		assert.Equal(t, "return task.priority > 1", stored.Conditions.Script)
	})

	t.Run("syntax errors have the path, line and column", func(t *testing.T) {
		r := autoRule(1, spec.TriggerTaskCreated, setPriority(1), script("var a = 1;\nvar b = ;"))
		err := saveAutomationRule(t, r, autoUser1)
		var invalid ErrAutomationRuleInvalid
		require.ErrorAs(t, err, &invalid)
		require.Len(t, invalid.Issues, 1)
		assert.Equal(t, "actions[1].params.source", invalid.Issues[0].Path)
		assert.Equal(t, 2, invalid.Issues[0].Line)
		assert.Equal(t, 9, invalid.Issues[0].Column)
		httpErr := invalid.HTTPError()
		assert.Equal(t, 400, httpErr.HTTPCode)
		assert.Equal(t, ErrCodeAutomationRuleInvalid, httpErr.Code)
	})

	t.Run("too large", func(t *testing.T) {
		config.AutomationScriptsMaxSource.Set(50)
		defer config.AutomationScriptsMaxSource.Set(64 * 1024)
		r := autoRule(1, spec.TriggerTaskCreated, script("// "+strings.Repeat("x", 100)))
		issues := ruleIssues(t, saveAutomationRule(t, r, autoUser1))
		assert.Contains(t, issues["actions[0].params.source"], "at most 50")
	})

	t.Run("kill switch is a typed 400 on save", func(t *testing.T) {
		config.AutomationScriptsEnabled.Set(false)
		defer config.AutomationScriptsEnabled.Set(true)
		err := saveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, script("return 1")), autoUser1)
		require.True(t, IsErrAutomationScriptsDisabled(err), "%v", err)
		assert.Equal(t, 400, err.(ErrAutomationScriptsDisabled).HTTPError().HTTPCode)
		assert.Equal(t, ErrCodeAutomationScriptsDisabled, err.(ErrAutomationScriptsDisabled).HTTPError().Code)
	})
}

func TestAutomationSecret_PurgeDeletesAndDuplicateCopiesNothing(t *testing.T) {
	files.InitTestFileFixtures(t)
	db.LoadAndAssertFixtures(t)
	require.NoError(t, setAutomationSecret(t, 1, "HOOK_TOKEN", "t", autoUser1))
	require.NoError(t, setAutomationSecret(t, 11, "KEEP", "k", autoUser1))

	withSecret := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionHTTPRequest, Params: map[string]any{
		"url": "https://chat.example.com/hook", "secret": "HOOK_TOKEN"}}), autoUser1)
	withScript := autoRule(1, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionScriptRun, Params: map[string]any{"source": "return secrets.get('HOOK_TOKEN')"}})
	withScript = mustSaveAutomationRule(t, withScript, autoUser1)
	plain := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)

	dup := &ProjectDuplicate{ProjectID: 1}
	duplicateProject(t, dup)
	newProject := dup.Project.ID

	db.AssertMissing(t, "automation_secrets", map[string]interface{}{"project_id": newProject})
	db.AssertExists(t, "automation_secrets", map[string]interface{}{"project_id": 1, "name": "HOOK_TOKEN"}, false)

	s := db.NewSession()
	copies := []*AutomationRule{}
	err := s.Where("project_id = ?", newProject).OrderBy("id ASC").Find(&copies)
	s.Close()
	require.NoError(t, err)
	require.Len(t, copies, 3)
	byTitle := map[int64]*AutomationRule{}
	for i, orig := range []*AutomationRule{withSecret, withScript, plain} {
		byTitle[orig.ID] = copies[i]
	}
	for _, orig := range []*AutomationRule{withSecret, withScript} {
		c := byTitle[orig.ID]
		assert.False(t, c.Enabled)
		assert.Equal(t, AutomationDisabledSecretsMissing, c.DisabledReason, "a rule that uses secrets stays disabled with its own reason")
	}
	assert.Equal(t, AutomationDisabledDuplicated, byTitle[plain.ID].DisabledReason)

	t.Run("enabling it fails until the secret exists", func(t *testing.T) {
		c := byTitle[withSecret.ID]
		c.Enabled = true
		issues := ruleIssues(t, saveAutomationRule(t, c, autoUser1))
		assert.Contains(t, issues["actions[0].params.secret"], "does not exist")
		require.NoError(t, setAutomationSecret(t, newProject, "HOOK_TOKEN", "new", autoUser1))
		require.NoError(t, saveAutomationRule(t, c, autoUser1))
		assert.True(t, reloadAutomationRule(t, c.ID).Enabled)
	})

	t.Run("purging the project deletes its secrets", func(t *testing.T) {
		s := db.NewSession()
		require.NoError(t, deleteAutomationForProject(s, 1))
		require.NoError(t, s.Commit())
		s.Close()
		db.AssertMissing(t, "automation_secrets", map[string]interface{}{"project_id": 1})
		db.AssertExists(t, "automation_secrets", map[string]interface{}{"project_id": 11, "name": "KEEP"}, false)
		db.AssertExists(t, "automation_secrets", map[string]interface{}{"project_id": newProject, "name": "HOOK_TOKEN"}, false)
	})
}
