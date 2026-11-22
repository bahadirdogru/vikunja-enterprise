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
	"strconv"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// runAction creates a validated rule with the actions for task.created, creates a task and runs it. It returns the
// task and the finished run with its steps.
func runAction(t *testing.T, project int64, owner, doer interface{ GetID() int64 }, actions ...*spec.Action) (*models.Task, *models.AutomationRun, []*models.AutomationRunStep) {
	t.Helper()
	ownerUser := user1
	if owner.GetID() == user2.ID {
		ownerUser = user2
	}
	doerUser := user1
	if doer.GetID() == user6.ID {
		doerUser = user6
	}
	r := addRule(t, project, ownerUser, rule(spec.TriggerTaskCreated, nil, nil, actions...))
	task := newTask(t, project, "Original title", doerUser)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	return getTask(t, task.ID), runs[0], runSteps(t, runs[0].ID)
}

func TestActions_SetField(t *testing.T) {
	reset(t)
	due := time.Now().Add(72 * time.Hour)
	task, run, steps := runAction(t, projectOwned, user1, user1,
		setField("title", "{{task.title}} - checked by {{event.doer.username}}"),
		setField("description", "Task {{task.id}} in {{project.title}}"),
		setField("priority", 4),
		setField("percent_done", 0.25),
		setField("hex_color", "#AABBCC"),
		setField("due_date", "+3d"),
		setField("start_date", "2026-12-24T09:00:00Z"),
		setField("end_date", "now+10d"),
	)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	assert.Len(t, steps, 8)
	assert.Equal(t, "Original title - checked by user1", task.Title)
	assert.Equal(t, "Task "+itoa(task.ID)+" in Test1", task.Description)
	assert.EqualValues(t, 4, task.Priority)
	assert.InDelta(t, 0.25, task.PercentDone, 0.0001)
	assert.Equal(t, "aabbcc", task.HexColor)
	assert.WithinDuration(t, due, task.DueDate, time.Minute)
	assert.True(t, time.Date(2026, 12, 24, 9, 0, 0, 0, time.UTC).Equal(task.StartDate))
	assert.WithinDuration(t, time.Now().Add(240*time.Hour), task.EndDate, time.Minute)

	t.Run("none clears a date and an empty color clears the color", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			setField("due_date", "none"), setField("hex_color", "")))
		tk := newTask(t, projectOwned, "task", user1)
		updateTask(t, user1, tk.ID, func(x *models.Task) { x.DueDate = time.Now().Add(time.Hour); x.HexColor = "ff0000" }, "due_date", "hex_color")
		updateTask(t, user1, tk.ID, func(x *models.Task) { x.Title = "trigger" }, "title")
		drain(t)
		got := getTask(t, tk.ID)
		assert.True(t, got.DueDate.IsZero())
		assert.Empty(t, got.HexColor)
		assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)
	})

	t.Run("invalid results fail the step with a readable error", func(t *testing.T) {
		for name, a := range map[string]*spec.Action{
			"priority out of range": setField("priority", 9),
			"empty title":           setField("title", "{{nope}}"),
			"bad color":             setField("hex_color", "red"),
			"percent over 1":        setField("percent_done", 1.5),
		} {
			t.Run(name, func(t *testing.T) {
				reset(t)
				_, run, steps := runAction(t, projectOwned, user1, user1, a)
				assert.Equal(t, models.AutomationRunFailed, run.Status)
				require.Len(t, steps, 1)
				assert.Equal(t, models.AutomationStepFailed, steps[0].Status)
				assert.NotEmpty(t, steps[0].Error)
			})
		}
	})
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func TestActions_SetCustomField(t *testing.T) {
	reset(t)
	// Project 1: 1 text, 2 select (options 1, 2), 3 number, 4 multi_select (options 3, 4).
	task, run, _ := runAction(t, projectOwned, user1, user1,
		act(spec.ActionSetCustomField, map[string]any{"field_id": 1, "value": "ref for {{task.title}}"}),
		act(spec.ActionSetCustomField, map[string]any{"field_id": 2, "value": 2}),
		act(spec.ActionSetCustomField, map[string]any{"field_id": 3, "value": 99.5}),
		act(spec.ActionSetCustomField, map[string]any{"field_id": 4, "value": []any{3, 4}}),
	)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)

	s := newSession()
	defer s.Close()
	values, err := models.GetCustomFieldValuesOfTask(s, task)
	require.NoError(t, err)
	byField := map[int64]any{}
	for _, v := range values {
		byField[v.CustomFieldID] = v.Value
	}
	assert.Equal(t, "ref for Original title", byField[1])
	assert.EqualValues(t, 2, byField[2])
	assert.InDelta(t, 99.5, byField[3], 0.001)
	assert.Equal(t, []int64{3, 4}, byField[4])

	t.Run("a null value clears the field", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			act(spec.ActionSetCustomField, map[string]any{"field_id": 1})))
		tk := newTask(t, projectOwned, "task", user1)
		setCustomField(t, tk.ID, 1, "filled")
		updateTask(t, user1, tk.ID, func(x *models.Task) { x.Title = "go" }, "title")
		drain(t)
		assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[len(allRuns(t, r.ID))-1].Status)
		s := newSession()
		defer s.Close()
		values, err := models.GetCustomFieldValuesOfTask(s, tk)
		require.NoError(t, err)
		assert.Nil(t, values[0].Value)
	})

	t.Run("dates, checkboxes and users", func(t *testing.T) {
		reset(t)
		var ids = map[string]int64{}
		mustCommit(t, func(s *xorm.Session) error {
			for _, f := range []*models.CustomField{
				{ProjectID: projectOwned, Title: "When", Type: models.CustomFieldTypeDate},
				{ProjectID: projectOwned, Title: "Flag", Type: models.CustomFieldTypeCheckbox},
				{ProjectID: projectOwned, Title: "Who", Type: models.CustomFieldTypeUser},
			} {
				if err := f.Create(s, user1); err != nil {
					return err
				}
				ids[f.Title] = f.ID
			}
			return nil
		})
		tk, run, _ := runAction(t, projectOwned, user1, user1,
			act(spec.ActionSetCustomField, map[string]any{"field_id": ids["When"], "value": "+2d"}),
			act(spec.ActionSetCustomField, map[string]any{"field_id": ids["Flag"], "value": true}),
			act(spec.ActionSetCustomField, map[string]any{"field_id": ids["Who"], "value": []any{1}}),
		)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		s := newSession()
		defer s.Close()
		values, err := models.GetCustomFieldValuesOfTask(s, tk)
		require.NoError(t, err)
		got := map[int64]any{}
		for _, v := range values {
			got[v.CustomFieldID] = v.Value
		}
		assert.WithinDuration(t, time.Now().Add(48*time.Hour), got[ids["When"]].(time.Time), time.Minute)
		assert.Equal(t, true, got[ids["Flag"]])
		assert.Equal(t, []int64{1}, got[ids["Who"]])
	})

	t.Run("a value that does not fit the type fails the step", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1,
			act(spec.ActionSetCustomField, map[string]any{"field_id": 3, "value": "not a number"}))
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, models.AutomationStepFailed, steps[0].Status)
	})
}

func TestActions_SetDone(t *testing.T) {
	reset(t)
	task, run, _ := runAction(t, projectOwned, user1, user1, act(spec.ActionSetDone, map[string]any{"done": true}))
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	assert.True(t, task.Done)

	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil, act(spec.ActionSetDone, map[string]any{"done": false})))
	updateTask(t, user1, task.ID, func(x *models.Task) { x.Title = "reopen it" }, "title")
	drain(t)
	assert.False(t, getTask(t, task.ID).Done)
	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)
}

func TestActions_SetStatus(t *testing.T) {
	reset(t)
	// Task 20 of project 11 is in status 1 (Todo).
	r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
		act(spec.ActionSetStatus, map[string]any{"status_id": 2, "comment": "moved by {{event.doer.username}}"})))
	updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger" }, "title")
	drain(t)
	require.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status, allRuns(t, r.ID)[0].Error)

	s := newSession()
	defer s.Close()
	status, err := models.GetTaskWorkflowStatus(s, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 2, status.ID)
	history := []*models.TaskStatusHistory{}
	require.NoError(t, s.Where("task_id = 20").OrderBy("id DESC").Find(&history))
	assert.Equal(t, "moved by user1", history[0].Comment)

	t.Run("a rejected transition fails the step with the error of the workflow", func(t *testing.T) {
		reset(t)
		changeStatus(t, 20, 2)
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			act(spec.ActionSetStatus, map[string]any{"status_id": 3})))
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "again" }, "title")
		drain(t)
		runs := allRuns(t, r.ID)
		require.Len(t, runs, 1)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		steps := runSteps(t, runs[0].ID)
		require.Len(t, steps, 1)
		assert.Contains(t, steps[0].Error, "Workflow requirements not met")
		assert.Contains(t, steps[0].Error, "due_date")
		assert.Equal(t, models.AutomationEventDone, allEvents(t)[len(allEvents(t))-1].Status, "a rejected action is not retried")
		status, err := models.GetTaskWorkflowStatus(s, 20)
		require.NoError(t, err)
		_ = status
	})

	t.Run("being in the status already is not an error", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			act(spec.ActionSetStatus, map[string]any{"status_id": 1})))
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "same" }, "title")
		drain(t)
		assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)
		assert.Contains(t, runSteps(t, allRuns(t, r.ID)[0].ID)[0].Output, "nothing to do")
	})
}

func assignees(t *testing.T, taskID int64) []int64 {
	t.Helper()
	s := newSession()
	defer s.Close()
	ids := []int64{}
	require.NoError(t, s.Table("task_assignees").Where("task_id = ?", taskID).OrderBy("user_id").Cols("user_id").Find(&ids))
	return ids
}

func TestActions_AssignAndUnassign(t *testing.T) {
	reset(t)
	task, run, _ := runAction(t, projectOwned, user1, user1,
		act(spec.ActionAssign, map[string]any{"user": "event_doer"}),
	)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	assert.Equal(t, []int64{1}, assignees(t, task.ID))

	t.Run("task_creator, explicit ids and idempotence", func(t *testing.T) {
		reset(t)
		// Project 11: user6 owns it and creates the task, user1 (the owner of the rule) administers it.
		tk, run, steps := runAction(t, projectWorkflow, user1, user6,
			act(spec.ActionAssign, map[string]any{"user": "task_creator"}),
			act(spec.ActionAssign, map[string]any{"user": 1}),
			act(spec.ActionAssign, map[string]any{"user": 1}),
		)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Equal(t, []int64{1, 6}, assignees(t, tk.ID))
		assert.Contains(t, steps[2].Output, "nothing to do")
	})

	t.Run("unassign", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			act(spec.ActionUnassign, map[string]any{"user": 1}), act(spec.ActionUnassign, map[string]any{"user": "event_doer"})))
		tk := newTask(t, projectOwned, "task", user1)
		mustCommitAssign(t, tk.ID)
		require.Equal(t, []int64{1}, assignees(t, tk.ID))
		updateTask(t, user1, tk.ID, func(x *models.Task) { x.Title = "go" }, "title")
		drain(t)
		assert.Empty(t, assignees(t, tk.ID))
		assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)
	})

	t.Run("a user without access to the project cannot be assigned", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1, act(spec.ActionAssign, map[string]any{"user": 2}))
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, steps[0].Error, "access")
	})
}

func TestActions_Labels(t *testing.T) {
	reset(t)
	labelsOf := func(taskID int64) []int64 {
		s := newSession()
		defer s.Close()
		ids := []int64{}
		require.NoError(t, s.Table("label_tasks").Where("task_id = ?", taskID).OrderBy("label_id").Cols("label_id").Find(&ids))
		return ids
	}
	task, run, _ := runAction(t, projectOwned, user1, user1,
		act(spec.ActionAddLabel, map[string]any{"label_id": 1}),
		act(spec.ActionAddLabel, map[string]any{"label_id": 2}),
		act(spec.ActionAddLabel, map[string]any{"label_id": 2}),
		act(spec.ActionRemoveLabel, map[string]any{"label_id": 1}),
		act(spec.ActionRemoveLabel, map[string]any{"label_id": 1}),
	)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	assert.Equal(t, []int64{2}, labelsOf(task.ID))
}

func TestActions_AddComment(t *testing.T) {
	reset(t)
	task, run, _ := runAction(t, projectOwned, user1, user1,
		act(spec.ActionAddComment, map[string]any{"text": "Created by {{event.doer.name}}: <b>{{task.title}}</b>\nsecond line"}))
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	s := newSession()
	defer s.Close()
	comments := []*models.TaskComment{}
	require.NoError(t, s.Where("task_id = ?", task.ID).Find(&comments))
	require.Len(t, comments, 1)
	assert.Equal(t, "<p>Created by user1: &lt;b&gt;Original title&lt;/b&gt;<br>second line</p>", comments[0].Comment, "text is escaped, not interpreted as HTML")
	assert.EqualValues(t, 1, comments[0].AuthorID, "the comment is written by the owner of the rule")
}

func TestActions_CreateSubtaskAndRelated(t *testing.T) {
	reset(t)
	// Triggered by updates: a rule that creates tasks on task.created would react to its own tasks until the
	// depth limit stops it.
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
		act(spec.ActionCreateSubtask, map[string]any{"title": "Review {{task.title}}", "description": "for {{event.doer.username}}", "assignee": "event_doer", "due_offset": "3d"}),
		act(spec.ActionCreateRelated, map[string]any{"title": "Follow up", "relation_kind": "blocking"}),
		act(spec.ActionCreateRelated, map[string]any{"title": "Plain related"}),
	))
	task := newTask(t, projectOwned, "Original title", user1)
	updateTask(t, user1, task.ID, func(x *models.Task) { x.Title = "Renamed" }, "title")
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	run := runs[0]
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	task = getTask(t, task.ID)

	s := newSession()
	defer s.Close()
	relations := []*models.TaskRelation{}
	require.NoError(t, s.Where("task_id = ?", task.ID).OrderBy("id").Find(&relations))
	require.Len(t, relations, 3)
	assert.Equal(t, models.RelationKindSubtask, relations[0].RelationKind)
	assert.Equal(t, models.RelationKindBlocking, relations[1].RelationKind)
	assert.Equal(t, models.RelationKindRelated, relations[2].RelationKind)

	sub := getTask(t, relations[0].OtherTaskID)
	assert.Equal(t, "Review Renamed", sub.Title)
	assert.Equal(t, "for user1", sub.Description)
	assert.Equal(t, projectOwned, sub.ProjectID)
	assert.WithinDuration(t, time.Now().Add(72*time.Hour), sub.DueDate, time.Minute)
	assert.Equal(t, []int64{1}, assignees(t, sub.ID))
	assert.Equal(t, "Follow up", getTask(t, relations[1].OtherTaskID).Title)
}

func TestActions_Move(t *testing.T) {
	reset(t)
	// Project 10: user1 can write, user6 owns it.
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionMove, map[string]any{"project_id": 10})))
	task := newTask(t, projectOwned, "to be moved", user1)
	drain(t)
	assert.EqualValues(t, 10, getTask(t, task.ID).ProjectID)
	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status, allRuns(t, r.ID)[0].Error)

	t.Run("a target the owner cannot write to is rejected when saving the rule", func(t *testing.T) {
		reset(t)
		err := commit(t, func(s *xorm.Session) error {
			x := rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionMove, map[string]any{"project_id": 9}))
			x.ProjectID = projectOwned
			return x.Create(s, user1)
		})
		require.True(t, models.IsErrAutomationRuleInvalid(err), "%v", err)
		var invalid models.ErrAutomationRuleInvalid
		require.ErrorAs(t, err, &invalid)
		assert.Equal(t, "actions[0].params.project_id", invalid.Issues[0].Path)
	})

	t.Run("an action cannot do what its owner cannot", func(t *testing.T) {
		reset(t)
		// Project 9: user1 only has read access. The rule is stored directly, as if access was revoked afterwards.
		r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, act(spec.ActionMove, map[string]any{"project_id": 9}))
		task := newTask(t, projectOwned, "stays", user1)
		drain(t)
		assert.Equal(t, projectOwned, getTask(t, task.ID).ProjectID)
		runs := allRuns(t, r.ID)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		assert.Contains(t, runSteps(t, runs[0].ID)[0].Error, "Forbidden")
	})
}

func TestActions_StopIf(t *testing.T) {
	reset(t)
	resetCounters()
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		setField("priority", 2),
		act(spec.ActionStopIf, map[string]any{"condition": map[string]any{"field": "priority", "op": ">=", "value": 2}}),
		setField("priority", 5),
	))
	task := newTask(t, projectOwned, "task", user1)
	drain(t)
	assert.EqualValues(t, 2, getTask(t, task.ID).Priority, "the third action never ran")
	runs := allRuns(t, r.ID)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status)
	steps := runSteps(t, runs[0].ID)
	assert.Len(t, steps, 2)

	t.Run("continues when the condition does not hold", func(t *testing.T) {
		reset(t)
		task2, run, _ := runAction(t, projectOwned, user1, user1,
			act(spec.ActionStopIf, map[string]any{"condition": map[string]any{"field": "priority", "op": ">=", "value": 2}}),
			setField("priority", 5))
		assert.Equal(t, models.AutomationRunSuccess, run.Status)
		assert.EqualValues(t, 5, task2.Priority)
	})
}

func TestActions_ConditionsDecideBeforeTheFirstAction(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil,
		&spec.Condition{All: []*spec.Condition{leaf("title", "like", "urgent"), {Not: []*spec.Condition{leaf("done", "=", true)}}}},
		setField("priority", 5)))
	urgent := newTask(t, projectOwned, "Urgent: server down", user1)
	calm := newTask(t, projectOwned, "Water the plants", user1)
	drain(t)
	assert.EqualValues(t, 5, getTask(t, urgent.ID).Priority)
	assert.EqualValues(t, 0, getTask(t, calm.ID).Priority)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 2)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status)
	assert.Equal(t, models.AutomationRunSkipped, runs[1].Status)
	assert.Equal(t, SkipConditions, runs[1].SkipReason)
}

func TestActions_UnknownPlaceholdersAreReported(t *testing.T) {
	reset(t)
	_, run, steps := runAction(t, projectOwned, user1, user1, act(spec.ActionAddComment, map[string]any{"text": "hello {{task.nope}}!"}))
	assert.Equal(t, models.AutomationRunSuccess, run.Status)
	assert.Contains(t, steps[0].Output, "unknown placeholder {{task.nope}}")
}

func TestActions_ATaskThatVanishedSkipsTheRun(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setField("priority", 1)))
	task := newTask(t, projectOwned, "gone before the worker comes", user1)
	s := db.NewSession()
	_, err := s.Unscoped().ID(task.ID).Delete(&models.Task{})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunSkipped, runs[0].Status)
	assert.Equal(t, SkipTaskNotFound, runs[0].SkipReason)
}

func TestActions_EveryCatalogActionHasAnImplementationAndViceVersa(t *testing.T) {
	for _, def := range spec.Actions() {
		assert.Contains(t, registry, def.Type, "the catalog lists %s but nothing implements it", def.Type)
	}
	for typ := range registry {
		if len(typ) > 5 && typ[:5] == "test." {
			continue
		}
		assert.NotNil(t, spec.ActionByType(typ), "%s is implemented but not in the catalog", typ)
	}
}
