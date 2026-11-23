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

package automation

import (
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var evalNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func evalSubject() *Subject {
	return &Subject{
		Now:       evalNow,
		CreatedBy: 3,
		Fields: map[string]any{
			"title":           "Fix the Login bug",
			"description":     "",
			"done":            false,
			"priority":        float64(3),
			"percent_done":    0.5,
			"due_date":        evalNow.Add(48 * time.Hour),
			"start_date":      nil,
			"hex_color":       "ff0000",
			"project_id":      float64(1),
			"created_by":      float64(3),
			"assignees":       []any{float64(1), float64(4)},
			"labels":          []any{},
			"status":          float64(2),
			"status_category": "in_progress",
			"cf_1":            "ref-42",
			"cf_2":            float64(1), // select: option 1
			"cf_3":            12.5,
			"cf_4":            []any{float64(3), float64(4)}, // multi_select
			"cf_5":            evalNow.Add(-24 * time.Hour),  // date
			"cf_6":            true,                          // checkbox
			"cf_7":            []any{float64(9)},             // user
			"cf_8":            nil,
		},
		CFTypes: map[int64]models.CustomFieldType{
			1: models.CustomFieldTypeText, 2: models.CustomFieldTypeSelect, 3: models.CustomFieldTypeNumber,
			4: models.CustomFieldTypeMultiSelect, 5: models.CustomFieldTypeDate, 6: models.CustomFieldTypeCheckbox,
			7: models.CustomFieldTypeUser, 8: models.CustomFieldTypeText,
		},
		Event: &Payload{
			Name:    "task.updated",
			Doer:    &Person{ID: 7, Username: "alice", Name: "Alice A"},
			Old:     map[string]any{"priority": float64(1), "due_date": "2026-11-20T12:00:00Z", "status": float64(1), "status_category": "open", "value": "a", "done": false},
			New:     map[string]any{"priority": float64(3), "due_date": nil, "status": float64(2), "status_category": "in_progress", "value": "b", "done": true},
			Task:    nil,
			Changed: []string{"priority"},
		},
	}
}

func leaf(field, op string, value any) *spec.Condition {
	return &spec.Condition{Field: field, Op: op, Value: value}
}

func TestEvaluate_OperatorsPerFieldKind(t *testing.T) {
	tests := []struct {
		name string
		c    *spec.Condition
		want bool
	}{
		// strings
		{"string =", leaf("title", "=", "Fix the Login bug"), true},
		{"string = is case sensitive", leaf("title", "=", "fix the login bug"), false},
		{"string !=", leaf("title", "!=", "other"), true},
		{"string like substring", leaf("title", "like", "login"), true},
		{"string like wildcard", leaf("title", "like", "fix%bug"), true},
		{"string like wildcard miss", leaf("title", "like", "bug%fix"), false},
		{"string like no match", leaf("title", "like", "signup"), false},
		{"string in", leaf("title", "in", []any{"x", "Fix the Login bug"}), true},
		{"string not in", leaf("title", "not in", []any{"x", "y"}), true},
		{"string not in hit", leaf("title", "not in", []any{"Fix the Login bug"}), false},
		{"string is_empty", leaf("description", "is_empty", nil), true},
		{"string is_not_empty", leaf("title", "is_not_empty", nil), true},
		{"string is_not_empty on empty", leaf("description", "is_not_empty", nil), false},
		{"hex color", leaf("hex_color", "=", "ff0000"), true},

		// numbers
		{"number =", leaf("priority", "=", 3), true},
		{"number != ", leaf("priority", "!=", 3), false},
		{"number >", leaf("priority", ">", 2), true},
		{"number > equal", leaf("priority", ">", 3), false},
		{"number >=", leaf("priority", ">=", 3), true},
		{"number <", leaf("priority", "<", 4), true},
		{"number < equal", leaf("priority", "<", 3), false},
		{"number <=", leaf("priority", "<=", 3), true},
		{"number in", leaf("priority", "in", []any{1, 3}), true},
		{"number not in", leaf("priority", "not in", []any{1, 2}), true},
		{"number float", leaf("percent_done", ">=", 0.5), true},
		{"number as string", leaf("priority", "=", "3"), true},
		{"number is_empty is false for a value", leaf("priority", "is_empty", nil), false},

		// dates
		{"date > now", leaf("due_date", ">", "now"), true},
		{"date < now+3d", leaf("due_date", "<", "now+3d"), true},
		{"date < now+1d", leaf("due_date", "<", "now+1d"), false},
		{"date >= relative", leaf("due_date", ">=", "now+48h"), true},
		{"date <= relative", leaf("due_date", "<=", "now+2d"), true},
		{"date = absolute", leaf("due_date", "=", "2026-11-24T12:00:00Z"), true},
		{"date != absolute", leaf("due_date", "!=", "2026-11-24T12:00:00Z"), false},
		{"date > plain date", leaf("due_date", ">", "2026-11-23"), true},
		{"date relative past", leaf("due_date", ">", "now-1d"), true},
		{"empty date is_empty", leaf("start_date", "is_empty", nil), true},
		{"date is_not_empty", leaf("due_date", "is_not_empty", nil), true},
		{"empty date never compares", leaf("start_date", "<", "now+1d"), false},
		{"empty date !=", leaf("start_date", "!=", "now"), true},

		// bools
		{"bool =", leaf("done", "=", false), true},
		{"bool = wrong", leaf("done", "=", true), false},
		{"bool !=", leaf("done", "!=", true), true},

		// lists
		{"assignees = contains", leaf("assignees", "=", 4), true},
		{"assignees = does not contain", leaf("assignees", "=", 5), false},
		{"assignees !=", leaf("assignees", "!=", 5), true},
		{"assignees != contained", leaf("assignees", "!=", 1), false},
		{"assignees in any", leaf("assignees", "in", []any{9, 4}), true},
		{"assignees in none", leaf("assignees", "in", []any{9, 8}), false},
		{"assignees not in", leaf("assignees", "not in", []any{9, 8}), true},
		{"assignees not in hit", leaf("assignees", "not in", []any{9, 1}), false},
		{"labels is_empty", leaf("labels", "is_empty", nil), true},
		{"assignees is_not_empty", leaf("assignees", "is_not_empty", nil), true},

		// users and keywords
		{"created_by =", leaf("created_by", "=", 3), true},
		{"created_by = task_creator", leaf("created_by", "=", "task_creator"), true},
		{"assignees = event_doer misses", leaf("assignees", "=", "event_doer"), false},
		{"assignees = task_creator", leaf("assignees", "=", "task_creator"), false},
		{"project", leaf("project_id", "=", 1), true},
		{"project in", leaf("project_id", "in", []any{2, 1}), true},

		// status
		{"status =", leaf("status", "=", 2), true},
		{"status in", leaf("status", "in", []any{1, 2}), true},
		{"status_category =", leaf("status_category", "=", "in_progress"), true},
		{"status_category in", leaf("status_category", "in", []any{"done", "cancelled"}), false},
		{"status_category not in", leaf("status_category", "not in", []any{"done", "cancelled"}), true},

		// custom fields by type
		{"cf text =", leaf("cf_1", "=", "ref-42"), true},
		{"cf text like", leaf("cf_1", "like", "REF"), true},
		{"cf select =", leaf("cf_2", "=", 1), true},
		{"cf select in", leaf("cf_2", "in", []any{2, 1}), true},
		{"cf select !=", leaf("cf_2", "!=", 2), true},
		{"cf number >", leaf("cf_3", ">", 12), true},
		{"cf number <=", leaf("cf_3", "<=", 12.4), false},
		{"cf multi_select =", leaf("cf_4", "=", 4), true},
		{"cf multi_select in", leaf("cf_4", "in", []any{9, 3}), true},
		{"cf multi_select not in", leaf("cf_4", "not in", []any{9, 8}), true},
		{"cf date <", leaf("cf_5", "<", "now"), true},
		{"cf date >", leaf("cf_5", ">", "now-2d"), true},
		{"cf checkbox =", leaf("cf_6", "=", true), true},
		{"cf user =", leaf("cf_7", "=", 9), true},
		{"cf user = doer", leaf("cf_7", "=", "event_doer"), false},
		{"cf empty is_empty", leaf("cf_8", "is_empty", nil), true},
		{"cf unset", leaf("cf_99", "is_empty", nil), true},
		{"cf unset =", leaf("cf_99", "=", "x"), false},

		// the event
		{"event doer id", leaf("event.doer.id", "=", 7), true},
		{"event doer username", leaf("event.doer.username", "=", "alice"), true},
		{"event doer username like", leaf("event.doer.username", "like", "ali"), true},
		{"event doer name", leaf("event.doer.name", "=", "Alice A"), true},
		{"event doer id in", leaf("event.doer.id", "in", []any{1, 7}), true},
		{"event old priority", leaf("event.old.priority", "=", 1), true},
		{"event new priority", leaf("event.new.priority", "=", 3), true},
		{"event priority grew", leaf("event.old.priority", "<", 3), true},
		{"event old due date", leaf("event.old.due_date", "<", "now"), true},
		{"event new due date cleared", leaf("event.new.due_date", "is_empty", nil), true},
		{"event old status", leaf("event.old.status", "=", 1), true},
		{"event new status_category", leaf("event.new.status_category", "=", "in_progress"), true},
		{"event old status_category", leaf("event.old.status_category", "in", []any{"open"}), true},
		{"event value string", leaf("event.old.value", "=", "a"), true},
		{"event value like", leaf("event.new.value", "like", "b"), true},
		{"event new done", leaf("event.new.done", "=", true), true},
		{"event missing key", leaf("event.new.assignee", "is_empty", nil), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.c, evalSubject())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEvaluate_EventDoerKeywordsResolveToTheDoer(t *testing.T) {
	sub := evalSubject()
	sub.Fields["assignees"] = []any{float64(7)}
	got, err := Evaluate(leaf("assignees", "=", "event_doer"), sub)
	require.NoError(t, err)
	assert.True(t, got)
	got, err = Evaluate(leaf("assignees", "in", []any{"event_doer", 99}), sub)
	require.NoError(t, err)
	assert.True(t, got)
}

func TestEvaluate_Groups(t *testing.T) {
	yes := leaf("done", "=", false)
	no := leaf("done", "=", true)
	tests := []struct {
		name string
		c    *spec.Condition
		want bool
	}{
		{"nil holds", nil, true},
		{"all true", &spec.Condition{All: []*spec.Condition{yes, yes}}, true},
		{"all with a false", &spec.Condition{All: []*spec.Condition{yes, no}}, false},
		{"any with a true", &spec.Condition{Any: []*spec.Condition{no, yes}}, true},
		{"any all false", &spec.Condition{Any: []*spec.Condition{no, no}}, false},
		{"not of false", &spec.Condition{Not: []*spec.Condition{no}}, true},
		{"not of true", &spec.Condition{Not: []*spec.Condition{yes}}, false},
		{"not of several is true only if none holds", &spec.Condition{Not: []*spec.Condition{no, yes}}, false},
		{"nested", &spec.Condition{All: []*spec.Condition{yes, {Any: []*spec.Condition{no, {Not: []*spec.Condition{no}}}}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.c, evalSubject())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	_, err := Evaluate(leaf("nonsense", "=", 1), evalSubject())
	require.Error(t, err)
	_, err = Evaluate(&spec.Condition{}, evalSubject())
	assert.Error(t, err)
}

func TestEvaluate_AgainstTheStoredTask(t *testing.T) {
	reset(t)
	task := newTask(t, projectOwned, "Quarterly report", user1)
	mustCommitAssign(t, task.ID)
	setCustomField(t, task.ID, 1, "ref-7")
	setCustomField(t, task.ID, 3, 20.0)
	setCustomField(t, task.ID, 2, 1) // option "Open"
	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Priority = 4; tk.DueDate = time.Now().Add(24 * time.Hour) }, "priority", "due_date")

	s := newSession()
	defer s.Close()
	sub, err := LoadSubject(s, task.ID, &Payload{Doer: &Person{ID: 1, Username: "user1"}}, time.Now())
	require.NoError(t, err)

	tests := []struct {
		name string
		c    *spec.Condition
		want bool
	}{
		{"title", leaf("title", "like", "report"), true},
		{"priority", leaf("priority", ">=", 4), true},
		{"due soon", leaf("due_date", "<", "now+2d"), true},
		{"not overdue", leaf("due_date", ">", "now"), true},
		{"assignee", leaf("assignees", "=", 1), true},
		{"creator", leaf("created_by", "=", "event_doer"), true},
		{"cf text", leaf("cf_1", "=", "ref-7"), true},
		{"cf number", leaf("cf_3", ">", 19), true},
		{"cf select", leaf("cf_2", "=", 1), true},
		{"cf multi_select unset", leaf("cf_4", "is_empty", nil), true},
		{"no status without a workflow", leaf("status", "is_empty", nil), true},
		{"identifier", leaf("identifier", "is_not_empty", nil), true},
		{"project", leaf("project_id", "=", 1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.c, sub)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("status of a task in a workflow", func(t *testing.T) {
		s := newSession()
		defer s.Close()
		sub, err := LoadSubject(s, 20, nil, time.Now())
		require.NoError(t, err)
		for _, c := range []*spec.Condition{leaf("status", "=", 1), leaf("status_category", "=", "open")} {
			got, err := Evaluate(c, sub)
			require.NoError(t, err)
			assert.True(t, got)
		}
	})

	t.Run("a missing task", func(t *testing.T) {
		s := newSession()
		defer s.Close()
		_, err := LoadSubject(s, 999999, nil, time.Now())
		assert.ErrorIs(t, err, ErrTaskGone)
	})
}

func TestTemplates(t *testing.T) {
	sub := evalSubject()
	sub.Fields["identifier"] = "PRJ-12"
	sub.Task = &models.Task{ID: 12}
	sub.Project = &models.Project{Title: "Website"}
	sub.CFOptions = map[int64]map[int64]string{2: {1: "Open"}, 4: {3: "Red", 4: "Blue"}}

	var unknown []string
	got := Render("{{task.title}} ({{ task.identifier }}) in {{project.title}} by {{event.doer.name}}/{{event.doer.username}} #{{task.id}}", sub, &unknown)
	assert.Equal(t, "Fix the Login bug (PRJ-12) in Website by Alice A/alice #12", got)
	assert.Empty(t, unknown)

	assert.Equal(t, "ref-42|Open|12.5|Red, Blue|true", Render("{{task.cf.1}}|{{task.cf.2}}|{{task.cf.3}}|{{task.cf.4}}|{{task.cf.6}}", sub, &unknown))
	assert.Equal(t, "2026-11-21T12:00:00Z", Render("{{task.cf.5}}", sub, &unknown))
	assert.Empty(t, unknown)

	unknown = nil
	assert.Equal(t, "x  y", Render("x {{nope}} {{task.cf.99}}y", sub, &unknown))
	assert.Equal(t, []string{"nope", "task.cf.99"}, unknown, "unknown placeholders render empty and are reported")

	assert.Equal(t, "{{ not closed", Render("{{ not closed", sub, nil))
	assert.Equal(t, "no placeholders {x}", Render("no placeholders {x}", sub, nil))
	// Nothing is executed: only a plain key is a placeholder, anything else stays text.
	assert.Equal(t, "{{task.title | upper}}", Render("{{task.title | upper}}", sub, nil))
}
