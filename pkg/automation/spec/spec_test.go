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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exampleValue builds a valid value for a parameter of the catalog.
func exampleValue(p Param, siblings map[string]any) any {
	kind := p.Kind
	if p.KindBy != nil {
		// Use the first (sorted) value of the deciding parameter that has a kind.
		for value, k := range p.KindBy.Kinds {
			siblings[p.KindBy.Param] = value
			kind = k
			break
		}
	}
	var v any
	switch kind {
	case KindString, KindText, KindTemplate:
		v = "text {{task.title}}"
		if p.Name == "url" {
			v = "https://example.com/hook?t={{task.id}}"
		}
		if p.Name == "cron" {
			v = "0 9 * * 1"
		}
	case KindCode:
		v = "return 1"
	case KindSecret:
		v = "API_KEY"
	case KindMap:
		v = map[string]any{"X-Token": "{{secrets.API_KEY}}"}
	case KindNumber:
		v = float64(1)
	case KindBool:
		v = true
	case KindDate:
		v = "2026-12-31T10:00:00Z"
	case KindDuration:
		v = "3d"
	case KindUser, KindUsers:
		v = float64(1)
		if len(p.Enum) > 0 {
			v = p.Enum[0]
		}
	case KindLabel, KindStatus, KindCustomField, KindProject, KindTeam:
		v = float64(1)
	case KindEnum:
		v = p.Enum[0]
	case KindCondition:
		v = map[string]any{"field": "title", "op": "=", "value": "x"}
	case KindAny:
		v = "value"
	}
	if p.Multiple {
		return []any{v}
	}
	return v
}

func exampleParams(defs []Param) map[string]any {
	out := map[string]any{}
	for _, p := range defs {
		if !p.Required {
			continue
		}
		out[p.Name] = exampleValue(p, out)
	}
	return out
}

func TestCatalog_EveryTriggerAndActionValidatesAgainstItsOwnSchema(t *testing.T) {
	for _, tr := range Triggers() {
		for _, withOptional := range []bool{false, true} {
			opts := exampleParams(tr.Options)
			if withOptional {
				opts = map[string]any{}
				for _, p := range tr.Options {
					opts[p.Name] = exampleValue(p, opts)
				}
			}
			doc := &Doc{
				Trigger: Trigger{Type: tr.Type, Options: opts},
				Actions: []*Action{{Type: ActionSetDone, Params: map[string]any{"done": true}}},
			}
			if !TriggerHasTask(doc.Trigger) {
				doc.Actions = []*Action{{Type: ActionCreateTask, Params: map[string]any{"title": "x"}}}
			}
			assert.Empty(t, Validate(doc, 25), "trigger %s with options %v", tr.Type, opts)
		}
	}

	for _, ac := range Actions() {
		for _, withOptional := range []bool{false, true} {
			params := exampleParams(ac.Params)
			if ac.Type == ActionNotify {
				params["recipients"] = []any{UserEventDoer}
			}
			if withOptional {
				params = map[string]any{}
				for _, p := range ac.Params {
					params[p.Name] = exampleValue(p, params)
				}
			}
			doc := &Doc{
				Trigger: Trigger{Type: TriggerTaskCreated},
				Actions: []*Action{{Type: ac.Type, Params: params}},
			}
			assert.Empty(t, Validate(doc, 25), "action %s with params %v", ac.Type, params)
		}
	}
}

func TestCatalog_IsConsistent(t *testing.T) {
	kinds := map[string]bool{
		KindString: true, KindText: true, KindNumber: true, KindBool: true, KindDate: true, KindDuration: true,
		KindUser: true, KindUsers: true, KindLabel: true, KindStatus: true, KindCustomField: true,
		KindProject: true, KindTemplate: true, KindCondition: true, KindEnum: true, KindAny: true,
		KindCode: true, KindSecret: true, KindMap: true, KindTeam: true,
	}
	checkParams := func(owner string, params []Param) {
		names := map[string]bool{}
		for _, p := range params {
			assert.False(t, names[p.Name], "%s: duplicate parameter %s", owner, p.Name)
			names[p.Name] = true
			assert.True(t, kinds[p.Kind], "%s.%s: unknown kind %s", owner, p.Name, p.Kind)
			assert.NotEmpty(t, p.TitleKey, "%s.%s needs a title key", owner, p.Name)
			if p.Kind == KindEnum {
				assert.NotEmpty(t, p.Enum, "%s.%s: an enum needs values", owner, p.Name)
			}
			if p.KindBy != nil {
				sibling := false
				for _, other := range params {
					sibling = sibling || other.Name == p.KindBy.Param
				}
				assert.True(t, sibling, "%s.%s: kind_by points at the unknown parameter %s", owner, p.Name, p.KindBy.Param)
				for _, k := range p.KindBy.Kinds {
					assert.True(t, kinds[k], "%s.%s: unknown kind %s", owner, p.Name, k)
				}
			}
		}
	}

	types := map[string]bool{}
	for _, tr := range Triggers() {
		assert.False(t, types[tr.Type], "duplicate trigger %s", tr.Type)
		types[tr.Type] = true
		assert.NotEmpty(t, tr.TitleKey)
		assert.NotEmpty(t, tr.Event)
		checkParams(tr.Type, tr.Options)
	}
	types = map[string]bool{}
	for _, ac := range Actions() {
		assert.False(t, types[ac.Type], "duplicate action %s", ac.Type)
		types[ac.Type] = true
		assert.NotEmpty(t, ac.TitleKey)
		checkParams(ac.Type, ac.Params)
	}

	known := map[string]bool{}
	for _, op := range Operators() {
		known[op.Type] = true
		assert.NotEmpty(t, op.TitleKey)
	}
	assert.Len(t, known, 11)
	for _, f := range Fields() {
		assert.NotEmpty(t, f.TitleKey, f.Name)
		assert.NotEmpty(t, f.Operators, f.Name)
		for _, op := range f.Operators {
			assert.True(t, known[op], "field %s lists the unknown operator %s", f.Name, op)
		}
	}

	// The catalog has to survive a JSON round trip: the frontend reads it as JSON.
	raw, err := json.Marshal(GetCatalog())
	require.NoError(t, err)
	decoded := &Catalog{}
	require.NoError(t, json.Unmarshal(raw, decoded))
	assert.Len(t, decoded.Triggers, len(Triggers()))
	assert.Len(t, decoded.Actions, len(Actions()))
}

func TestValidate_ReportsTheLocationOfEveryProblem(t *testing.T) {
	doc := &Doc{
		Trigger: Trigger{Type: "task.exploded"},
		Conditions: &Condition{All: []*Condition{
			{Field: "title", Op: "like", Value: "x"},
			{Field: "nope", Op: "=", Value: 1},
			{Field: "done", Op: ">", Value: true},
			{Any: []*Condition{{Field: "priority", Op: "in", Value: "3"}}},
		}},
		Actions: []*Action{
			{Type: ActionSetDone, Params: map[string]any{"done": true}},
			{Type: ActionSetStatus, Params: map[string]any{"status_id": "three", "bogus": 1}},
			{Type: "task.explode"},
			{Type: ActionAddComment},
		},
	}
	issues := Validate(doc, 25)
	paths := map[string]string{}
	for _, i := range issues {
		paths[i.Path] = i.Message
	}
	assert.Contains(t, paths, "trigger.type")
	assert.Contains(t, paths, "conditions.all[1].field")
	assert.Contains(t, paths, "conditions.all[2].op")
	assert.Contains(t, paths, "conditions.all[3].any[0].value")
	assert.Contains(t, paths, "actions[1].params.status_id")
	assert.Contains(t, paths, "actions[1].params.bogus")
	assert.Contains(t, paths, "actions[2].type")
	assert.Contains(t, paths, "actions[3].params.text")
	assert.NotContains(t, paths, "conditions.all[0].value")
}

func TestValidate_Structure(t *testing.T) {
	base := func() *Doc {
		return &Doc{Trigger: Trigger{Type: TriggerTaskCreated}, Actions: []*Action{{Type: ActionSetDone, Params: map[string]any{"done": true}}}}
	}

	t.Run("needs an action", func(t *testing.T) {
		d := base()
		d.Actions = nil
		assert.NotEmpty(t, Validate(d, 25))
	})
	t.Run("max actions", func(t *testing.T) {
		d := base()
		for i := 0; i < 3; i++ {
			d.Actions = append(d.Actions, d.Actions[0])
		}
		assert.Empty(t, Validate(d, 4))
		assert.Equal(t, "actions", Validate(d, 3)[0].Path)
	})
	t.Run("a condition node has exactly one form", func(t *testing.T) {
		d := base()
		d.Conditions = &Condition{Field: "title", Op: "=", Value: "x", All: []*Condition{{Field: "title", Op: "=", Value: "y"}}}
		assert.Equal(t, "conditions", Validate(d, 25)[0].Path)
		d.Conditions = &Condition{}
		assert.Equal(t, "conditions", Validate(d, 25)[0].Path)
	})
	t.Run("is_empty takes no value, the others need one", func(t *testing.T) {
		d := base()
		d.Conditions = &Condition{Field: "due_date", Op: OpIsEmpty, Value: "x"}
		assert.Equal(t, "conditions.value", Validate(d, 25)[0].Path)
		d.Conditions = &Condition{Field: "due_date", Op: OpGt}
		assert.Equal(t, "conditions.value", Validate(d, 25)[0].Path)
		d.Conditions = &Condition{Field: "due_date", Op: OpIsEmpty}
		assert.Empty(t, Validate(d, 25))
	})
	t.Run("nesting is limited", func(t *testing.T) {
		d := base()
		c := &Condition{Field: "title", Op: "=", Value: "x"}
		for i := 0; i < maxConditionDepth+1; i++ {
			c = &Condition{All: []*Condition{c}}
		}
		d.Conditions = c
		assert.NotEmpty(t, Validate(d, 25))
	})
	t.Run("custom fields and event fields", func(t *testing.T) {
		d := base()
		d.Conditions = &Condition{All: []*Condition{
			{Field: "cf_12", Op: ">", Value: 3},
			{Field: "event.old.status", Op: "=", Value: 3},
			{Field: "event.new.due_date", Op: "<", Value: "now+3d"},
			{Field: "event.doer.username", Op: "like", Value: "bot"},
			{Field: "assignees", Op: "in", Value: []any{"event_doer", 4}},
		}}
		assert.Empty(t, Validate(d, 25))
	})
	t.Run("stop_if condition is validated with its path", func(t *testing.T) {
		d := base()
		d.Actions = []*Action{{Type: ActionStopIf, Params: map[string]any{"condition": map[string]any{"field": "nope", "op": "="}}}}
		assert.Equal(t, "actions[0].params.condition.field", Validate(d, 25)[0].Path)
	})
}

func TestDateSpecs(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		in          string
		allowOffset bool
		want        time.Time
		wantErr     bool
		none        bool
	}{
		{in: "2026-12-31T10:00:00Z", want: time.Date(2026, 12, 31, 10, 0, 0, 0, time.UTC)},
		{in: "2026-12-31", want: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
		{in: "now", want: now},
		{in: "now+3d", want: now.Add(72 * time.Hour)},
		{in: "now-12h", want: now.Add(-12 * time.Hour)},
		{in: "now+2w", want: now.Add(14 * 24 * time.Hour)},
		{in: "now+30m", want: now.Add(30 * time.Minute)},
		{in: "+3d", allowOffset: true, want: now.Add(72 * time.Hour)},
		{in: "-2h", allowOffset: true, want: now.Add(-2 * time.Hour)},
		{in: "none", allowOffset: true, none: true},
		{in: "+3d", wantErr: true},
		{in: "none", wantErr: true},
		{in: "tomorrow", wantErr: true},
		{in: "now+3x", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			d, err := ParseDateSpec(tt.in, tt.allowOffset)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.none, d.None)
			if tt.none {
				assert.True(t, d.Resolve(now).IsZero())
				return
			}
			assert.True(t, tt.want.Equal(d.Resolve(now)), "%s resolved to %s", tt.in, d.Resolve(now))
		})
	}

	off, err := ParseOffset("12h")
	require.NoError(t, err)
	assert.Equal(t, 12*time.Hour, off)
	_, err = ParseOffset("h12")
	assert.Error(t, err)
}

func TestWalk_CollectsAndRemapsReferences(t *testing.T) {
	doc := &Doc{
		Trigger: Trigger{Type: TriggerTaskStatusChanged, Options: map[string]any{"from_status_id": float64(1), "to_status_id": float64(2)}},
		Conditions: &Condition{All: []*Condition{
			{Field: "cf_7", Op: "=", Value: float64(70)},
			{Field: "status", Op: "in", Value: []any{float64(1), float64(3)}},
			{Field: "labels", Op: "=", Value: float64(9)},
		}},
		Actions: []*Action{
			{Type: ActionSetStatus, Params: map[string]any{"status_id": float64(3)}},
			{Type: ActionSetCustomField, Params: map[string]any{"field_id": float64(7), "value": float64(70)}},
			{Type: ActionMove, Params: map[string]any{"project_id": float64(5)}},
			{Type: ActionAddLabel, Params: map[string]any{"label_id": float64(9)}},
			{Type: ActionStopIf, Params: map[string]any{"condition": map[string]any{"field": "status", "op": "=", "value": float64(2)}}},
		},
	}

	refs := Collect(doc)
	assert.Equal(t, []int64{1, 2, 3}, refs.Statuses)
	assert.Equal(t, []int64{7}, refs.CustomFields)
	assert.Equal(t, []int64{9}, refs.Labels)
	assert.Equal(t, []int64{5}, refs.Projects)

	copyDoc := CloneDoc(doc)
	Walk(copyDoc, Handler{
		ID: func(kind string, id int64, _ string) int64 {
			switch kind {
			case KindStatus:
				return id + 100
			case KindCustomField:
				return id + 200
			}
			return id
		},
		IsOptionField: func(id int64) bool { return id == 7 },
		Option:        func(_, id int64) int64 { return id + 1000 },
	})
	assert.InDelta(t, float64(101), copyDoc.Trigger.Options["from_status_id"], 0)
	assert.Equal(t, "cf_207", copyDoc.Conditions.All[0].Field)
	assert.InDelta(t, float64(1070), copyDoc.Conditions.All[0].Value, 0)
	assert.Equal(t, []any{float64(101), float64(103)}, copyDoc.Conditions.All[1].Value)
	assert.InDelta(t, float64(9), copyDoc.Conditions.All[2].Value, 0, "labels are not remapped")
	assert.InDelta(t, float64(207), copyDoc.Actions[1].Params["field_id"], 0)
	assert.InDelta(t, float64(1070), copyDoc.Actions[1].Params["value"], 0)
	assert.InDelta(t, float64(5), copyDoc.Actions[2].Params["project_id"], 0)
	inner, ok := ConditionFromParam(copyDoc.Actions[4].Params["condition"])
	require.True(t, ok)
	assert.InDelta(t, float64(102), inner.Value, 0)

	// The original is untouched.
	assert.Equal(t, "cf_7", doc.Conditions.All[0].Field)
}

func TestValidate_TaskLessTriggersNeedACreateTaskFirst(t *testing.T) {
	setDone := &Action{Type: ActionSetDone, Params: map[string]any{"done": true}}
	create := &Action{Type: ActionCreateTask, Params: map[string]any{"title": "x"}}
	hook := Trigger{Type: TriggerWebhookIncoming}
	projectSchedule := Trigger{Type: TriggerSchedule, Options: map[string]any{"cron": "@daily", "scope": ScopeProject}}

	for _, tr := range []Trigger{hook, projectSchedule} {
		assert.False(t, TriggerHasTask(tr), tr.Type)
		issues := Validate(&Doc{Trigger: tr, Actions: []*Action{setDone}}, 25)
		require.Len(t, issues, 1, tr.Type)
		assert.Equal(t, "actions[0].type", issues[0].Path)
		assert.Empty(t, Validate(&Doc{Trigger: tr, Actions: []*Action{create, setDone}}, 25), tr.Type)
		// Actions that do not need a task are fine at any position.
		assert.Empty(t, Validate(&Doc{Trigger: tr, Actions: []*Action{{Type: ActionWait, Params: map[string]any{"duration": "1h"}}, create}}, 25))
	}
	assert.True(t, TriggerHasTask(Trigger{Type: TriggerSchedule, Options: map[string]any{"cron": "@daily"}}))
	assert.True(t, TriggerHasTask(Trigger{Type: TriggerManual}))
}

func TestValidate_PayloadFieldsAndCron(t *testing.T) {
	good := &Condition{Field: "event.payload.customer.name", Op: OpEq, Value: "Ada"}
	bad := &Condition{Field: "event.payload.", Op: OpEq, Value: "Ada"}
	create := &Action{Type: ActionCreateTask, Params: map[string]any{"title": "x"}}
	hook := Trigger{Type: TriggerWebhookIncoming}
	assert.Empty(t, Validate(&Doc{Trigger: hook, Conditions: good, Actions: []*Action{create}}, 25))
	assert.NotEmpty(t, Validate(&Doc{Trigger: hook, Conditions: bad, Actions: []*Action{create}}, 25))

	for expr, valid := range map[string]bool{"0 9 * * 1": true, "*/15 8-18 * * 1-5": true, "@hourly": true, "0 9 * *": false, "0 0 9 * * *": false, "nope": false} {
		issues := Validate(&Doc{Trigger: Trigger{Type: TriggerSchedule, Options: map[string]any{"cron": expr}}, Actions: []*Action{{Type: ActionSetDone, Params: map[string]any{"done": true}}}}, 25)
		assert.Equal(t, valid, len(issues) == 0, expr)
	}
}
