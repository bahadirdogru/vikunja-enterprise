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
	"context"
	"testing"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fingerprint captures everything an action could change.
func fingerprint(t *testing.T, taskID int64) map[string]any {
	t.Helper()
	s := newSession()
	defer s.Close()
	out := map[string]any{}
	for _, table := range []string{
		"tasks", "task_comments", "labels", "label_tasks", "task_assignees", "task_relations",
		"task_custom_field_values", "task_statuses", "task_status_history", "automation_events", "automation_runs",
		"automation_run_steps", "automation_rules", "project_views", "task_buckets", "task_positions",
	} {
		n, err := s.Table(table).Count()
		require.NoError(t, err)
		out[table] = n
	}
	task, err := models.GetTaskByIDSimple(s, taskID)
	require.NoError(t, err)
	out["task"] = task
	return out
}

func dryRun(t *testing.T, r *models.AutomationRule, taskID int64) *DryRunResult {
	t.Helper()
	s := newSession()
	defer s.Close()
	stored, err := models.GetAutomationRule(s, r.ID)
	require.NoError(t, err)
	res, err := DryRun(context.Background(), stored, taskID)
	require.NoError(t, err)
	return res
}

func TestDryRun_PlansEveryActionWithoutWriting(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil,
		&spec.Condition{Field: "title", Op: "like", Value: "dry"},
		setField("title", "{{task.title}} (checked)"),
		setField("priority", 5),
		setField("due_date", "+3d"),
		act(spec.ActionSetCustomField, map[string]any{"field_id": 1, "value": "hello {{project.title}}"}),
		act(spec.ActionSetDone, map[string]any{"done": true}),
		act(spec.ActionAssign, map[string]any{"user": "event_doer"}),
		act(spec.ActionAddLabel, map[string]any{"label_id": 1}),
		act(spec.ActionAddComment, map[string]any{"text": "from {{event.doer.name}} {{task.bogus}}"}),
		act(spec.ActionCreateSubtask, map[string]any{"title": "Sub of {{task.title}}"}),
		act(spec.ActionCreateRelated, map[string]any{"title": "Rel", "relation_kind": "blocking"}),
		act(spec.ActionMove, map[string]any{"project_id": 10}),
		act(spec.ActionStopIf, map[string]any{"condition": map[string]any{"field": "priority", "op": ">", "value": 100}}),
	))
	task := newTask(t, projectOwned, "dry run me", user1)
	before := fingerprint(t, task.ID)
	eventsBefore := len(allEvents(t))

	res := dryRun(t, r, task.ID)

	assert.Equal(t, before, fingerprint(t, task.ID), "a dry run changes nothing")
	assert.Len(t, allEvents(t), eventsBefore, "and records no events")
	assert.True(t, res.ConditionsMet)
	require.Len(t, res.Steps, 12)
	for _, st := range res.Steps {
		assert.Empty(t, st.Error, "step %d (%s)", st.Position, st.Type)
		assert.NotEmpty(t, st.Summary)
	}
	assert.Contains(t, res.Steps[0].Summary, `"dry run me (checked)"`, "templates are resolved")
	assert.Contains(t, res.Steps[1].Summary, "priority to 5")
	assert.Contains(t, res.Steps[3].Summary, "hello Test1")
	assert.Contains(t, res.Steps[5].Summary, "assign user 1")
	assert.Contains(t, res.Steps[7].Warnings[0], "task.bogus")
	assert.Contains(t, res.Steps[8].Summary, `"Sub of dry run me"`)
	assert.Contains(t, res.Steps[10].Summary, "project 10")
	assert.False(t, res.Steps[11].Stops)
	assert.NotEmpty(t, res.Notes)
}

func TestDryRun_ReportsWhatWouldFail(t *testing.T) {
	reset(t)
	changeStatus(t, 20, 2)
	r := insertRawRule(t, projectWorkflow, user1, spec.TriggerTaskUpdated,
		act(spec.ActionSetStatus, map[string]any{"status_id": 3}),
		act(spec.ActionMove, map[string]any{"project_id": 9}),
		act(spec.ActionSetField, map[string]any{"field": "priority", "value": 9}),
		act(spec.ActionSetCustomField, map[string]any{"field_id": 999, "value": "x"}),
		act(spec.ActionAssign, map[string]any{"user": 2}),
		act(spec.ActionAddLabel, map[string]any{"label_id": 6}),
		act(spec.ActionStopIf, map[string]any{"condition": map[string]any{"field": "done", "op": "=", "value": false}}),
		act(spec.ActionSetDone, map[string]any{"done": true}),
	)
	before := fingerprint(t, 20)
	res := dryRun(t, r, 20)
	assert.Equal(t, before, fingerprint(t, 20))

	require.Len(t, res.Steps, 8)
	assert.Contains(t, res.Steps[0].Error, "Workflow requirements not met")
	assert.Contains(t, res.Steps[1].Error, "Forbidden")
	assert.Contains(t, res.Steps[2].Error, "priority")
	assert.Contains(t, res.Steps[3].Error, "ustom field")
	assert.NotEmpty(t, res.Steps[4].Error)
	assert.NotEmpty(t, res.Steps[5].Error)
	assert.True(t, res.Steps[6].Stops, "the condition holds, so the run would end here")
	assert.Empty(t, res.Steps[7].Error)
}

func TestDryRun_ConditionsAndTaskOfAnotherProject(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, leaf("title", "like", "never matches"), setField("priority", 1)))
	task := newTask(t, projectOwned, "something", user1)
	res := dryRun(t, r, task.ID)
	assert.False(t, res.ConditionsMet)
	require.Len(t, res.Steps, 1, "the actions are planned anyway")
	assert.Contains(t, res.Notes[len(res.Notes)-1], "conditions do not hold")

	s := newSession()
	defer s.Close()
	stored, err := models.GetAutomationRule(s, r.ID)
	require.NoError(t, err)
	_, err = DryRun(context.Background(), stored, 20)
	assert.True(t, models.IsErrTaskDoesNotExist(err), "a task of another project is not revealed")
	_, err = DryRun(context.Background(), stored, 999999)
	assert.True(t, models.IsErrTaskDoesNotExist(err))
}
