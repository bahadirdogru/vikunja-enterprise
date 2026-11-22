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

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scriptDoc(actions ...*Action) *Doc {
	return &Doc{Trigger: Trigger{Type: TriggerTaskCreated}, Actions: actions}
}

func paths(issues []Issue) []string {
	out := make([]string, len(issues))
	for i, is := range issues {
		out[i] = is.Path
	}
	return out
}

func TestValidate_ScriptAction(t *testing.T) {
	ok := scriptDoc(&Action{Type: ActionScriptRun, Params: map[string]any{"source": "return 1"}})
	assert.Empty(t, Validate(ok, 25))

	for name, params := range map[string]map[string]any{
		"missing source": {},
		"empty source":   {"source": "  \n"},
		"not a string":   {"source": 5},
	} {
		doc := scriptDoc(&Action{Type: ActionScriptRun, Params: params})
		issues := Validate(doc, 25)
		require.NotEmpty(t, issues, name)
		assert.Equal(t, "actions[0].params.source", issues[0].Path, name)
	}
}

func TestValidate_ScriptCondition(t *testing.T) {
	doc := scriptDoc(&Action{Type: ActionSetDone, Params: map[string]any{"done": true}})
	doc.Conditions = &Condition{All: []*Condition{{Field: "done", Op: "=", Value: false}, {Script: "return task.priority > 2"}}}
	assert.Empty(t, Validate(doc, 25))

	doc.Conditions = &Condition{Script: "return true", Field: "title", Op: "=", Value: "x"}
	assert.Equal(t, []string{"conditions"}, paths(Validate(doc, 25)), "a script cannot be combined with a comparison")

	doc.Conditions = &Condition{Script: "   "}
	assert.Equal(t, []string{"conditions.script"}, paths(Validate(doc, 25)))

	t.Run("scripts are found with their paths", func(t *testing.T) {
		d := scriptDoc(
			&Action{Type: ActionSetDone, Params: map[string]any{"done": true}},
			&Action{Type: ActionScriptRun, Params: map[string]any{"source": "log(1)"}},
			&Action{Type: ActionStopIf, Params: map[string]any{"condition": map[string]any{"any": []any{
				map[string]any{"field": "done", "op": "=", "value": true},
				map[string]any{"script": "return false"},
			}}}},
		)
		d.Conditions = &Condition{Not: []*Condition{{Script: "return true"}}}
		var got []string
		for _, s := range Scripts(d) {
			got = append(got, s.Path)
		}
		assert.Equal(t, []string{"conditions.not[0].script", "actions[1].params.source", "actions[2].params.condition.any[1].script"}, got)
		assert.True(t, HasScripts(d))
		assert.False(t, HasScripts(scriptDoc(&Action{Type: ActionSetDone, Params: map[string]any{"done": true}})))
	})
}

func TestValidate_HTTPRequest(t *testing.T) {
	valid := func() *Action {
		return &Action{Type: ActionHTTPRequest, Params: map[string]any{
			"url":     "https://chat.example.com/hook/{{task.id}}",
			"method":  "POST",
			"headers": map[string]any{"Authorization": "Bearer {{secrets.CHAT_TOKEN}}", "X-Project": "{{project.title}}"},
			"body":    `{"text": "{{task.title}}", "key": "{{ secrets.CHAT_KEY }}"}`,
			"secret":  "SIGNING_KEY",
		}}
	}
	assert.Empty(t, Validate(scriptDoc(valid()), 25))

	cases := map[string]struct {
		change func(a *Action)
		path   string
	}{
		"not http":                         {func(a *Action) { a.Params["url"] = "ftp://x.example.com" }, "actions[0].params.url"},
		"url may start with a placeholder": {func(a *Action) { a.Params["url"] = "{{task.title}}" }, ""},
		"secret in the url":                {func(a *Action) { a.Params["url"] = "https://x.example.com/{{secrets.CHAT_TOKEN}}" }, "actions[0].params.url"},
		"bad method":                       {func(a *Action) { a.Params["method"] = "TRACE" }, "actions[0].params.method"},
		"bad header name":                  {func(a *Action) { a.Params["headers"] = map[string]any{"Bad Name": "x"} }, "actions[0].params.headers.Bad Name"},
		"forbidden header":                 {func(a *Action) { a.Params["headers"] = map[string]any{"Host": "x"} }, "actions[0].params.headers.Host"},
		"header value not text":            {func(a *Action) { a.Params["headers"] = map[string]any{"X": 5} }, "actions[0].params.headers.X"},
		"bad secret name":                  {func(a *Action) { a.Params["secret"] = "1bad" }, "actions[0].params.secret"},
		"bad placeholder name":             {func(a *Action) { a.Params["body"] = "{{secrets.bad-name}}" }, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a := valid()
			c.change(a)
			issues := Validate(scriptDoc(a), 25)
			if c.path == "" {
				assert.Empty(t, issues)
				return
			}
			require.NotEmpty(t, issues)
			assert.Equal(t, c.path, issues[0].Path)
		})
	}
}

func TestValidate_SecretsAreOnlyAllowedInHTTPHeadersAndBody(t *testing.T) {
	for name, a := range map[string]*Action{
		"comment": {Type: ActionAddComment, Params: map[string]any{"text": "the key is {{secrets.KEY}}"}},
		"title":   {Type: ActionSetField, Params: map[string]any{"field": "title", "value": "{{ secrets.KEY }}"}},
		"subtask": {Type: ActionCreateSubtask, Params: map[string]any{"title": "x", "description": "{{secrets.KEY}}"}},
		"status":  {Type: ActionSetStatus, Params: map[string]any{"status_id": 1, "comment": "{{secrets.KEY}}"}},
		"custom":  {Type: ActionSetCustomField, Params: map[string]any{"field_id": 1, "value": "{{secrets.KEY}}"}},
		"script":  {Type: ActionScriptRun, Params: map[string]any{"source": "return '{{secrets.KEY}}'"}},
	} {
		issues := Validate(scriptDoc(a), 25)
		require.NotEmpty(t, issues, name)
		assert.Contains(t, issues[0].Message, "secrets", name)
	}
	assert.Empty(t, Validate(scriptDoc(&Action{Type: ActionAddComment, Params: map[string]any{"text": "a {{task.title}} and secrets.KEY without braces"}}), 25))
}

func TestSecretRefs(t *testing.T) {
	d := scriptDoc(&Action{Type: ActionHTTPRequest, Params: map[string]any{
		"url": "https://x.example.com", "secret": "SIGN", "body": "{{secrets.BODY_KEY}}", "headers": map[string]any{"A": "{{secrets.HDR}}"},
	}})
	got := map[string]string{}
	for _, r := range SecretRefs(d) {
		got[r.Name] = r.Path
	}
	assert.Equal(t, map[string]string{
		"SIGN": "actions[0].params.secret", "BODY_KEY": "actions[0].params.body", "HDR": "actions[0].params.headers.A",
	}, got)
	assert.True(t, UsesSecrets(d))
	assert.True(t, UsesSecrets(scriptDoc(&Action{Type: ActionScriptRun, Params: map[string]any{"source": "return secrets.get('A')"}})))
	assert.False(t, UsesSecrets(scriptDoc(&Action{Type: ActionSetDone, Params: map[string]any{"done": true}})))
}

func TestScriptReference(t *testing.T) {
	ref := ScriptReference()
	names := map[string]*ScriptAPIDef{}
	for _, a := range ref.API {
		assert.NotEmpty(t, a.Signature, a.Name)
		assert.NotEmpty(t, a.Description, a.Name)
		names[a.Name] = a
	}
	for _, want := range []string{
		"event", "task", "project", "vikunja.tasks.get", "vikunja.tasks.update", "vikunja.tasks.create", "vikunja.tasks.comment",
		"vikunja.tasks.find", "vikunja.fields.get", "vikunja.fields.set", "vikunja.status.set", "vikunja.assignees.add",
		"vikunja.assignees.remove", "vikunja.labels.add", "vikunja.labels.remove", "http.fetch", "secrets.get", "log",
	} {
		assert.Contains(t, names, want)
	}
	assert.False(t, names["vikunja.tasks.update"].InCondition)
	assert.True(t, names["vikunja.tasks.update"].Mutating)
	assert.True(t, names["vikunja.tasks.get"].InCondition)
	assert.NotEmpty(t, ref.ConditionConvention)

	cat := GetCatalog()
	require.NotNil(t, cat.Script)
	for _, ac := range cat.Actions {
		if ac.Type == ActionScriptRun {
			assert.Equal(t, "javascript", ac.Params[0].Language)
			assert.NotEmpty(t, ac.Params[0].Description)
		}
	}
}
