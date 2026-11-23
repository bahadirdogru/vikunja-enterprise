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
	"strconv"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/files"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func reloadAutomationRule(t *testing.T, id int64) *AutomationRule {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	r, err := GetAutomationRule(s, id)
	require.NoError(t, err)
	return r
}

func TestAutomation_ProjectPurgeDeletesEverything(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	r := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	keep := mustSaveAutomationRule(t, autoRule(11, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	run := insertAutomationRun(t, r.ID, 1, AutomationRunSuccess, AutomationStepSuccess)
	keepRun := insertAutomationRun(t, keep.ID, 11, AutomationRunSuccess, AutomationStepSuccess)

	s := db.NewSession()
	require.NoError(t, deleteAutomationForProject(s, 1))
	require.NoError(t, s.Commit())
	s.Close()

	db.AssertMissing(t, "automation_rules", map[string]interface{}{"id": r.ID})
	db.AssertMissing(t, "automation_runs", map[string]interface{}{"id": run.ID})
	db.AssertMissing(t, "automation_run_steps", map[string]interface{}{"run_id": run.ID})
	db.AssertCount(t, "automation_events", builder.Eq{"project_id": 1}, 0)
	db.AssertExists(t, "automation_rules", map[string]interface{}{"id": keep.ID}, false)
	db.AssertExists(t, "automation_runs", map[string]interface{}{"id": keepRun.ID}, false)
	db.AssertExists(t, "automation_run_steps", map[string]interface{}{"run_id": keepRun.ID}, false)
}

func TestAutomation_PurgingTheProjectThroughTheNormalPath(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	// A fresh project so nothing else holds on to it.
	s := db.NewSession()
	project := &Project{Title: "to purge"}
	require.NoError(t, CreateProject(s, project, autoUser1, false, false))
	require.NoError(t, s.Commit())
	s.Close()

	r := mustSaveAutomationRule(t, autoRule(project.ID, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	insertAutomationRun(t, r.ID, project.ID, AutomationRunSuccess, AutomationStepSuccess)

	s = db.NewSession()
	require.NoError(t, hardDeleteProject(s, project, autoUser1))
	require.NoError(t, s.Commit())
	s.Close()

	db.AssertMissing(t, "automation_rules", map[string]interface{}{"project_id": project.ID})
	db.AssertCount(t, "automation_events", builder.Eq{"project_id": project.ID}, 0)
	db.AssertCount(t, "automation_runs", builder.Eq{"project_id": project.ID}, 0)
}

func TestAutomation_DuplicatingAProjectCopiesRulesDisabled(t *testing.T) {
	files.InitTestFileFixtures(t)
	db.LoadAndAssertFixtures(t)
	// Project 1: custom fields 1 (text), 2 (select, options 1 and 2), 3 (number). No workflow.
	r := autoRule(1, spec.TriggerTaskCustomFieldChanged,
		&spec.Action{Type: spec.ActionSetCustomField, Params: map[string]any{"field_id": 2, "value": 2}},
		&spec.Action{Type: spec.ActionSetCustomField, Params: map[string]any{"field_id": 3, "value": 7}},
		&spec.Action{Type: spec.ActionAddLabel, Params: map[string]any{"label_id": 1}},
		setPriority(2),
	)
	r.Trigger.Options = map[string]any{"field_id": 2}
	r.Conditions = &spec.Condition{All: []*spec.Condition{
		{Field: "cf_2", Op: "=", Value: float64(1)},
		{Field: "cf_3", Op: ">", Value: float64(5)},
	}}
	r = mustSaveAutomationRule(t, r, autoUser1)

	dup := &ProjectDuplicate{ProjectID: 1}
	duplicateProject(t, dup)

	newProject := dup.Project.ID
	copies := []*AutomationRule{}
	s := db.NewSession()
	defer s.Close()
	require.NoError(t, s.Where("project_id = ?", newProject).Find(&copies))
	require.Len(t, copies, 1)
	c := copies[0]

	assert.False(t, c.Enabled, "copies are disabled")
	assert.Equal(t, AutomationDisabledDuplicated, c.DisabledReason)
	assert.EqualValues(t, 1, c.OwnerID)
	assert.EqualValues(t, 1, c.Revision)
	assert.Equal(t, r.Title, c.Title)

	// The custom fields and options of the copy have new ids; the rule points at them.
	newFields := []*CustomField{}
	require.NoError(t, s.Where("project_id = ?", newProject).OrderBy("position ASC, id ASC").Find(&newFields))
	require.Len(t, newFields, 4)
	newOptions := []*CustomFieldOption{}
	require.NoError(t, s.Where("custom_field_id = ?", newFields[1].ID).OrderBy("position ASC").Find(&newOptions))
	require.Len(t, newOptions, 2)

	assert.EqualValues(t, newFields[1].ID, c.Trigger.Options["field_id"])
	assert.EqualValues(t, newFields[1].ID, c.Actions[0].Params["field_id"])
	assert.EqualValues(t, newOptions[1].ID, c.Actions[0].Params["value"], "option ids are remapped")
	assert.EqualValues(t, newFields[2].ID, c.Actions[1].Params["field_id"])
	assert.EqualValues(t, 7, c.Actions[1].Params["value"], "plain values are kept")
	assert.EqualValues(t, 1, c.Actions[2].Params["label_id"], "labels are shared")
	assert.Equal(t, "cf_"+itoa64(newFields[1].ID), c.Conditions.All[0].Field)
	assert.EqualValues(t, newOptions[0].ID, c.Conditions.All[0].Value)
	assert.Equal(t, "cf_"+itoa64(newFields[2].ID), c.Conditions.All[1].Field)

	// The original is untouched.
	orig := reloadAutomationRule(t, r.ID)
	assert.True(t, orig.Enabled)
	assert.EqualValues(t, 2, orig.Actions[0].Params["field_id"])
}

func TestAutomation_DuplicatingRemapsStatusesAndFlagsWhatItCannotMap(t *testing.T) {
	files.InitTestFileFixtures(t)
	db.LoadAndAssertFixtures(t)
	r := autoRule(11, spec.TriggerTaskStatusChanged, &spec.Action{Type: spec.ActionSetStatus, Params: map[string]any{"status_id": 2}})
	r.Trigger.Options = map[string]any{"to_status_id": 4}
	r.Conditions = &spec.Condition{Field: "status_category", Op: "=", Value: "done"}
	mustSaveAutomationRule(t, r, autoUser1)

	dup := &ProjectDuplicate{ProjectID: 11}
	duplicateProject(t, dup)

	s := db.NewSession()
	copies := []*AutomationRule{}
	require.NoError(t, s.Where("project_id = ?", dup.Project.ID).Find(&copies))
	require.Len(t, copies, 1)
	statuses := []*WorkflowStatus{}
	require.NoError(t, s.Join("INNER", "workflows", "workflows.id = workflow_statuses.workflow_id").
		Where("workflows.project_id = ?", dup.Project.ID).OrderBy("workflow_statuses.position ASC").Find(&statuses))
	require.Len(t, statuses, 6)
	assert.Equal(t, AutomationDisabledDuplicated, copies[0].DisabledReason)
	assert.EqualValues(t, statuses[3].ID, copies[0].Trigger.Options["to_status_id"], "Done of the copy")
	assert.EqualValues(t, statuses[1].ID, copies[0].Actions[0].Params["status_id"], "Doing of the copy")
	s.Close()

	t.Run("a reference without a counterpart is flagged", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		// Stored directly: project 1 has no workflow, the rule refers to a status all the same.
		s := db.NewSession()
		defer s.Close()
		_, err := s.Insert(&AutomationRule{
			ProjectID: 1, Title: "dangling", Enabled: true, Revision: 1, OwnerID: 1,
			Trigger: spec.Trigger{Type: spec.TriggerTaskStatusChanged, Options: map[string]any{"to_status_id": float64(4)}},
			Actions: []*spec.Action{setPriority(1)},
		})
		require.NoError(t, err)
		require.NoError(t, duplicateAutomationRulesForProject(s, 1, 1, 3, nil, nil, nil))
		copied := []*AutomationRule{}
		require.NoError(t, s.Where("project_id = 3").Find(&copied))
		require.Len(t, copied, 1)
		assert.False(t, copied[0].Enabled)
		assert.Equal(t, AutomationDisabledUnmappedReference, copied[0].DisabledReason)
	})
}

func itoa64(i int64) string { return strconv.FormatInt(i, 10) }

func TestAutomation_DeletingACustomFieldDisablesRulesThatUseIt(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	uses := autoRule(1, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionSetCustomField, Params: map[string]any{"field_id": 1, "value": "x"}})
	inCondition := autoRule(1, spec.TriggerTaskCreated, setPriority(1))
	inCondition.Conditions = &spec.Condition{Field: "cf_1", Op: "=", Value: "x"}
	inTrigger := autoRule(1, spec.TriggerTaskCustomFieldChanged, setPriority(1))
	inTrigger.Trigger.Options = map[string]any{"field_id": 1}
	unrelated := autoRule(1, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionSetCustomField, Params: map[string]any{"field_id": 3, "value": 1}})
	for _, r := range []*AutomationRule{uses, inCondition, inTrigger, unrelated} {
		mustSaveAutomationRule(t, r, autoUser1)
	}

	// The delete is not blocked by the rules.
	s := db.NewSession()
	field := &CustomField{ID: 1, ProjectID: 1}
	can, err := field.CanDelete(s, autoUser1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, field.Delete(s, autoUser1))
	require.NoError(t, s.Commit())
	s.Close()
	db.AssertMissing(t, "custom_fields", map[string]interface{}{"id": 1})

	for _, r := range []*AutomationRule{uses, inCondition, inTrigger} {
		stored := reloadAutomationRule(t, r.ID)
		assert.False(t, stored.Enabled)
		assert.Equal(t, AutomationDisabledCustomFieldDeleted, stored.DisabledReason)
		assert.EqualValues(t, 2, stored.Revision)
	}
	assert.True(t, reloadAutomationRule(t, unrelated.ID).Enabled)

	t.Run("enabling it again is refused until the reference is fixed", func(t *testing.T) {
		stored := reloadAutomationRule(t, uses.ID)
		stored.Enabled = true
		err := saveAutomationRule(t, stored, autoUser1)
		assert.Contains(t, ruleIssues(t, err), "actions[0].params.field_id")
	})
}

func TestAutomation_RemovingOrDeletingStatusesDisablesRulesThatUseThem(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	status := func(id int) map[string]any { return map[string]any{"status_id": id} }
	onDoing := autoRule(11, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionSetStatus, Params: status(2)})
	onReview := autoRule(11, spec.TriggerTaskStatusChanged, setPriority(1))
	onReview.Trigger.Options = map[string]any{"from_status_id": 3}
	onDone := autoRule(11, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionSetStatus, Params: status(4)})
	for _, r := range []*AutomationRule{onDoing, onReview, onDone} {
		mustSaveAutomationRule(t, r, autoUser1)
	}

	// Doing and Review have no tasks and no history: they are deleted with the document change.
	wf := readWorkflow(t, 11)
	wf.Statuses = []*WorkflowStatus{wf.Statuses[0], wf.Statuses[3], wf.Statuses[4], wf.Statuses[5]}
	wf.Transitions = []*WorkflowTransition{wf.Transitions[2]}
	mustSaveWorkflow(t, wf, autoUser1)

	for _, r := range []*AutomationRule{onDoing, onReview} {
		stored := reloadAutomationRule(t, r.ID)
		assert.False(t, stored.Enabled)
		assert.Equal(t, AutomationDisabledStatusDeleted, stored.DisabledReason)
	}
	assert.True(t, reloadAutomationRule(t, onDone.ID).Enabled)

	t.Run("deleting the whole workflow", func(t *testing.T) {
		s := db.NewSession()
		wfDel := &Workflow{ProjectID: 11}
		can, err := wfDel.CanDelete(s, autoUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, wfDel.Delete(s, autoUser1))
		require.NoError(t, s.Commit())
		s.Close()
		stored := reloadAutomationRule(t, onDone.ID)
		assert.False(t, stored.Enabled)
		assert.Equal(t, AutomationDisabledStatusDeleted, stored.DisabledReason)
	})
}

func TestAutomation_RetentionCleanup(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	r := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	old := insertAutomationRun(t, r.ID, 1, AutomationRunSuccess, AutomationStepSuccess)
	running := insertAutomationRun(t, r.ID, 1, AutomationRunRunning)
	s := db.NewSession()
	_, err := s.ID(running.ID).Cols("finished").Update(&AutomationRun{Finished: time.Now().Add(-time.Hour)})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	s = db.NewSession()
	events, runs, err := cleanupAutomation(s, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 0, events+runs)

	events, runs, err = cleanupAutomation(s, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	assert.EqualValues(t, 1, runs, "a running run is never cleaned up")
	assert.EqualValues(t, 1, events, "the event of a running run stays: the run may continue from it")
	db.AssertMissing(t, "automation_runs", map[string]interface{}{"id": old.ID})
	db.AssertMissing(t, "automation_run_steps", map[string]interface{}{"run_id": old.ID})
	_ = running
}

func duplicateProject(t *testing.T, dup *ProjectDuplicate) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	can, err := dup.CanCreate(s, autoUser1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, dup.Create(s, autoUser1))
	require.NoError(t, s.Commit())
}
