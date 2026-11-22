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
	"context"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// Fixture topology: user1 owns project 1 (custom fields 1-4), has read access to project 9, write access to 10 and
// admin access to 11 (workflow with statuses 1-6, 6 archived). user2 has no access to any of them.
var (
	autoUser1 = &user.User{ID: 1}
	autoUser2 = &user.User{ID: 2}
)

func autoRule(project int64, trigger string, actions ...*spec.Action) *AutomationRule {
	return &AutomationRule{
		ProjectID: project,
		Title:     "rule",
		Enabled:   true,
		Trigger:   spec.Trigger{Type: trigger},
		Actions:   actions,
	}
}

func setPriority(v int) *spec.Action {
	return &spec.Action{Type: spec.ActionSetField, Params: map[string]any{"field": "priority", "value": v}}
}

func saveAutomationRule(t *testing.T, r *AutomationRule, a web.Auth) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	var can bool
	var err error
	if r.ID == 0 {
		can, err = r.CanCreate(s, a)
	} else {
		can, err = r.CanUpdate(s, a)
	}
	if err != nil {
		_ = s.Rollback()
		return err
	}
	if !can {
		_ = s.Rollback()
		return ErrGenericForbidden{}
	}
	if r.ID == 0 {
		err = r.Create(s, a)
	} else {
		err = r.Update(s, a)
	}
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	return nil
}

func mustSaveAutomationRule(t *testing.T, r *AutomationRule, a web.Auth) *AutomationRule {
	t.Helper()
	require.NoError(t, saveAutomationRule(t, r, a))
	return r
}

func ruleIssues(t *testing.T, err error) map[string]string {
	t.Helper()
	require.Error(t, err)
	var invalid ErrAutomationRuleInvalid
	require.ErrorAs(t, err, &invalid, "got %T: %v", err, err)
	paths := map[string]string{}
	for _, i := range invalid.Issues {
		paths[i.Path] = i.Message
	}
	return paths
}

func TestAutomationRule_Create(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	r := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(3)), autoUser1)
	assert.NotZero(t, r.ID)
	assert.EqualValues(t, 1, r.Revision)
	assert.EqualValues(t, 1, r.OwnerID)
	assert.True(t, r.Enabled)
	assert.Empty(t, r.DisabledReason)
	db.AssertExists(t, "automation_rules", map[string]interface{}{"id": r.ID, "project_id": 1, "owner_id": 1, "title": "rule"}, false)

	t.Run("the project of the URL wins and server fields cannot be set", func(t *testing.T) {
		in := autoRule(1, spec.TriggerTaskCreated, setPriority(1))
		in.ID = 9999
		in.Revision = 77
		in.OwnerID = 5
		in.ConsecutiveFailures = 8
		in.DisabledReason = "something"
		s := db.NewSession()
		require.NoError(t, in.Create(s, autoUser1))
		require.NoError(t, s.Commit())
		s.Close()
		saved := in
		assert.NotEqual(t, int64(9999), saved.ID)
		assert.EqualValues(t, 1, saved.Revision)
		assert.EqualValues(t, 1, saved.OwnerID)
		assert.EqualValues(t, 0, saved.ConsecutiveFailures)
		assert.Empty(t, saved.DisabledReason)
	})

	t.Run("the document is stored as it was sent", func(t *testing.T) {
		in := autoRule(1, spec.TriggerTaskUpdated,
			setPriority(2),
			&spec.Action{Type: spec.ActionAddComment, Params: map[string]any{"text": "hi {{task.title}}"}})
		in.Trigger.Options = map[string]any{"fields": []any{"title", "priority"}}
		in.Conditions = &spec.Condition{All: []*spec.Condition{
			{Field: "priority", Op: ">", Value: float64(1)},
			{Not: []*spec.Condition{{Field: "done", Op: "=", Value: true}}},
		}}
		saved := mustSaveAutomationRule(t, in, autoUser1)

		s := db.NewSession()
		defer s.Close()
		stored, err := GetAutomationRule(s, saved.ID)
		require.NoError(t, err)
		assert.Equal(t, spec.TriggerTaskUpdated, stored.Trigger.Type)
		assert.Equal(t, []any{"title", "priority"}, stored.Trigger.Options["fields"])
		require.NotNil(t, stored.Conditions)
		require.Len(t, stored.Conditions.All, 2)
		assert.Equal(t, ">", stored.Conditions.All[0].Op)
		require.Len(t, stored.Actions, 2)
		assert.Equal(t, "hi {{task.title}}", stored.Actions[1].Params["text"])
	})
}

func TestAutomationRule_Validation(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	t.Run("lists every problem with its path", func(t *testing.T) {
		bad := autoRule(1, "task.exploded",
			setPriority(1),
			&spec.Action{Type: spec.ActionSetStatus, Params: map[string]any{"status_id": "three"}},
			&spec.Action{Type: "task.explode"},
		)
		bad.Conditions = &spec.Condition{Field: "nope", Op: "="}
		paths := ruleIssues(t, saveAutomationRule(t, bad, autoUser1))
		assert.Contains(t, paths, "trigger.type")
		assert.Contains(t, paths, "conditions.field")
		assert.Contains(t, paths, "actions[1].params.status_id")
		assert.Contains(t, paths, "actions[2].type")
		db.AssertCount(t, "automation_rules", builder.Eq{"project_id": 1}, 0)
	})

	t.Run("a title is required", func(t *testing.T) {
		r := autoRule(1, spec.TriggerTaskCreated, setPriority(1))
		r.Title = ""
		assert.Contains(t, ruleIssues(t, saveAutomationRule(t, r, autoUser1)), "title")
	})

	t.Run("custom fields have to belong to the project", func(t *testing.T) {
		// Field 5 belongs to project 9.
		r := autoRule(1, spec.TriggerTaskCustomFieldChanged,
			&spec.Action{Type: spec.ActionSetCustomField, Params: map[string]any{"field_id": 5, "value": "x"}})
		r.Trigger.Options = map[string]any{"field_id": 6}
		r.Conditions = &spec.Condition{Field: "cf_5", Op: "=", Value: "x"}
		paths := ruleIssues(t, saveAutomationRule(t, r, autoUser1))
		assert.Contains(t, paths, "trigger.options.field_id")
		assert.Contains(t, paths, "conditions.field")
		assert.Contains(t, paths, "actions[0].params.field_id")

		ok := autoRule(1, spec.TriggerTaskCustomFieldChanged,
			&spec.Action{Type: spec.ActionSetCustomField, Params: map[string]any{"field_id": 1, "value": "x"}})
		ok.Trigger.Options = map[string]any{"field_id": 3}
		ok.Conditions = &spec.Condition{Field: "cf_1", Op: "=", Value: "x"}
		mustSaveAutomationRule(t, ok, autoUser1)
	})

	t.Run("statuses have to belong to the workflow of the project", func(t *testing.T) {
		// Project 1 has no workflow.
		r := autoRule(1, spec.TriggerTaskStatusChanged, &spec.Action{Type: spec.ActionSetStatus, Params: map[string]any{"status_id": 2}})
		r.Trigger.Options = map[string]any{"to_status_id": 2}
		paths := ruleIssues(t, saveAutomationRule(t, r, autoUser1))
		assert.Contains(t, paths, "trigger.options.to_status_id")
		assert.Contains(t, paths, "actions[0].params.status_id")

		ok := autoRule(11, spec.TriggerTaskStatusChanged, &spec.Action{Type: spec.ActionSetStatus, Params: map[string]any{"status_id": 2}})
		ok.Trigger.Options = map[string]any{"to_status_id": 4, "to_category": "done"}
		mustSaveAutomationRule(t, ok, autoUser1)

		archived := autoRule(11, spec.TriggerTaskStatusChanged, &spec.Action{Type: spec.ActionSetStatus, Params: map[string]any{"status_id": 6}})
		assert.Contains(t, ruleIssues(t, saveAutomationRule(t, archived, autoUser1)), "actions[0].params.status_id")
	})

	t.Run("labels have to be usable by the user", func(t *testing.T) {
		// Label 6 is private to another user, label 1 is the user's own.
		r := autoRule(1, spec.TriggerTaskCreated,
			&spec.Action{Type: spec.ActionAddLabel, Params: map[string]any{"label_id": 6}},
			&spec.Action{Type: spec.ActionAddLabel, Params: map[string]any{"label_id": 1}},
			&spec.Action{Type: spec.ActionAddLabel, Params: map[string]any{"label_id": 9999}})
		paths := ruleIssues(t, saveAutomationRule(t, r, autoUser1))
		assert.Contains(t, paths, "actions[0].params.label_id")
		assert.NotContains(t, paths, "actions[1].params.label_id")
		assert.Contains(t, paths, "actions[2].params.label_id")
	})

	t.Run("the target of a move needs write access", func(t *testing.T) {
		read := autoRule(1, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionMove, Params: map[string]any{"project_id": 9}})
		assert.Contains(t, ruleIssues(t, saveAutomationRule(t, read, autoUser1)), "actions[0].params.project_id")
		none := autoRule(1, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionMove, Params: map[string]any{"project_id": 2}})
		assert.Contains(t, ruleIssues(t, saveAutomationRule(t, none, autoUser1)), "actions[0].params.project_id")
		gone := autoRule(1, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionMove, Params: map[string]any{"project_id": 99999}})
		assert.Contains(t, ruleIssues(t, saveAutomationRule(t, gone, autoUser1)), "actions[0].params.project_id")
		write := autoRule(1, spec.TriggerTaskCreated, &spec.Action{Type: spec.ActionMove, Params: map[string]any{"project_id": 10}})
		mustSaveAutomationRule(t, write, autoUser1)
	})

	t.Run("references in conditions are checked as well", func(t *testing.T) {
		r := autoRule(1, spec.TriggerTaskCreated, setPriority(1))
		r.Conditions = &spec.Condition{Field: "status", Op: "=", Value: float64(4)}
		assert.Contains(t, ruleIssues(t, saveAutomationRule(t, r, autoUser1)), "conditions.value")
	})

	t.Run("the error carries a stable code and the issues as details", func(t *testing.T) {
		err := saveAutomationRule(t, autoRule(1, "nope", setPriority(1)), autoUser1)
		httpErr := err.(web.HTTPErrorProcessor).HTTPError()
		assert.Equal(t, ErrCodeAutomationRuleInvalid, httpErr.Code)
		assert.Equal(t, 400, httpErr.HTTPCode)
		details, ok := httpErr.Details.(AutomationRuleInvalidDetails)
		require.True(t, ok)
		assert.Equal(t, "trigger.type", details.Issues[0].Path)
	})
}

func TestAutomationRule_Update(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	r := mustSaveAutomationRule(t, autoRule(11, spec.TriggerTaskCreated, setPriority(1)), autoUser1)

	update := &AutomationRule{ID: r.ID, ProjectID: 11, Title: "renamed", Enabled: true, Trigger: spec.Trigger{Type: spec.TriggerTaskDeleted}, Actions: []*spec.Action{setPriority(2)}}
	saved := mustSaveAutomationRule(t, update, autoUser1)
	assert.EqualValues(t, 2, saved.Revision)
	assert.Equal(t, "renamed", saved.Title)
	assert.Equal(t, spec.TriggerTaskDeleted, saved.Trigger.Type)

	t.Run("the user who saves becomes the owner", func(t *testing.T) {
		// user6 owns project 11 and is therefore admin.
		by6 := &AutomationRule{ID: r.ID, ProjectID: 11, Title: "by owner", Enabled: true, Trigger: spec.Trigger{Type: spec.TriggerTaskDeleted}, Actions: []*spec.Action{setPriority(2)}}
		saved := mustSaveAutomationRule(t, by6, &user.User{ID: 6})
		assert.EqualValues(t, 6, saved.OwnerID)
		assert.EqualValues(t, 3, saved.Revision)
	})

	t.Run("saving clears a reason the system stored and the failure count", func(t *testing.T) {
		s := db.NewSession()
		_, err := s.ID(r.ID).Cols("enabled", "disabled_reason", "consecutive_failures").Update(&AutomationRule{Enabled: false, DisabledReason: AutomationDisabledTooManyFailures, ConsecutiveFailures: 20})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()

		enable := &AutomationRule{ID: r.ID, ProjectID: 11, Title: "again", Enabled: true, Trigger: spec.Trigger{Type: spec.TriggerTaskDeleted}, Actions: []*spec.Action{setPriority(2)}}
		saved := mustSaveAutomationRule(t, enable, autoUser1)
		assert.True(t, saved.Enabled)
		assert.Empty(t, saved.DisabledReason)
		assert.EqualValues(t, 0, saved.ConsecutiveFailures)
	})

	t.Run("a rule of another project does not exist for this URL", func(t *testing.T) {
		wrong := &AutomationRule{ID: r.ID, ProjectID: 1, Title: "x", Trigger: spec.Trigger{Type: spec.TriggerTaskDeleted}, Actions: []*spec.Action{setPriority(2)}}
		err := saveAutomationRule(t, wrong, autoUser1)
		assert.True(t, IsErrAutomationRuleDoesNotExist(err), "%v", err)
	})

	t.Run("an invalid update changes nothing", func(t *testing.T) {
		bad := &AutomationRule{ID: r.ID, ProjectID: 11, Title: "bad", Enabled: true, Trigger: spec.Trigger{Type: "nope"}, Actions: []*spec.Action{setPriority(2)}}
		require.Error(t, saveAutomationRule(t, bad, autoUser1))
		db.AssertExists(t, "automation_rules", map[string]interface{}{"id": r.ID, "title": "again"}, false)
	})
}

func TestAutomationRule_Permissions(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	r := mustSaveAutomationRule(t, autoRule(10, spec.TriggerTaskCreated, setPriority(1)), &user.User{ID: 6}) // owner of project 10

	tests := []struct {
		name  string
		auth  web.Auth
		admin bool
	}{
		{"owner", &user.User{ID: 6}, true},
		{"user with write access only", autoUser1, false},
		{"user without access", autoUser2, false},
		{"link share", &LinkSharing{ID: 1, ProjectID: 10, Permission: PermissionAdmin}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := db.NewSession()
			defer s.Close()

			read := &AutomationRule{ID: r.ID, ProjectID: 10}
			can, _, err := read.CanRead(s, tt.auth)
			if tt.admin {
				require.NoError(t, err)
				assert.True(t, can)
			} else {
				assert.False(t, can)
			}

			create := &AutomationRule{ProjectID: 10}
			can, _ = create.CanCreate(s, tt.auth)
			assert.Equal(t, tt.admin, can, "create")
			update := &AutomationRule{ID: r.ID, ProjectID: 10}
			can, _ = update.CanUpdate(s, tt.auth)
			assert.Equal(t, tt.admin, can, "update")
			can, _ = update.CanDelete(s, tt.auth)
			assert.Equal(t, tt.admin, can, "delete")

			_, _, _, err = (&AutomationRule{ProjectID: 10}).ReadAll(s, tt.auth, "", 1, 50)
			if tt.admin {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}

	t.Run("a project that does not exist", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		_, err := (&AutomationRule{ProjectID: 99999}).CanCreate(s, autoUser1)
		assert.Error(t, err)
	})
}

func TestAutomationRule_ReadAll(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	for i := 0; i < 3; i++ {
		r := autoRule(1, spec.TriggerTaskCreated, setPriority(1))
		r.Title = []string{"alpha", "beta", "gamma"}[i]
		mustSaveAutomationRule(t, r, autoUser1)
	}
	mustSaveAutomationRule(t, autoRule(11, spec.TriggerTaskCreated, setPriority(1)), autoUser1)

	s := db.NewSession()
	defer s.Close()
	result, count, total, err := (&AutomationRule{ProjectID: 1}).ReadAll(s, autoUser1, "", 1, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.EqualValues(t, 3, total)
	assert.Equal(t, "alpha", result.([]*AutomationRule)[0].Title)

	result, _, total, err = (&AutomationRule{ProjectID: 1}).ReadAll(s, autoUser1, "bet", 1, 50)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "beta", result.([]*AutomationRule)[0].Title)
}

func insertAutomationRun(t *testing.T, ruleID, projectID int64, status string, steps ...string) *AutomationRun {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	ev := &AutomationEvent{ProjectID: projectID, TaskID: 1, Name: "task.created", Payload: "{}", Status: AutomationEventDone, RunAfter: time.Now()}
	_, err := s.Insert(ev)
	require.NoError(t, err)
	run := &AutomationRun{RuleID: ruleID, RuleRevision: 1, EventID: ev.ID, ProjectID: projectID, TaskID: 1, Status: status, Started: time.Now(), Finished: time.Now()}
	_, err = s.Insert(run)
	require.NoError(t, err)
	for i, st := range steps {
		_, err = s.Insert(&AutomationRunStep{RunID: run.ID, Position: int64(i), ActionType: spec.ActionSetField, Status: st, Output: "out"})
		require.NoError(t, err)
	}
	require.NoError(t, s.Commit())
	return run
}

func TestAutomationRun_ReadAndRetry(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	r := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	failed := insertAutomationRun(t, r.ID, 1, AutomationRunFailed, AutomationStepSuccess, AutomationStepFailed)
	ok := insertAutomationRun(t, r.ID, 1, AutomationRunSuccess, AutomationStepSuccess)

	t.Run("list newest first, with the event name", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		result, count, total, err := (&AutomationRun{RuleID: r.ID, ProjectID: 1}).ReadAll(s, autoUser1, "", 1, 50)
		require.NoError(t, err)
		runs := result.([]*AutomationRun)
		require.Equal(t, 2, count)
		assert.EqualValues(t, 2, total)
		assert.Equal(t, ok.ID, runs[0].ID)
		assert.Equal(t, failed.ID, runs[1].ID)
		assert.Equal(t, "task.created", runs[0].EventName)
		assert.Empty(t, runs[0].Steps, "the list has no steps")
	})

	t.Run("one run with its steps in order", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		run := &AutomationRun{ID: failed.ID, RuleID: r.ID, ProjectID: 1}
		can, _, err := run.CanRead(s, autoUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, run.ReadOne(s, autoUser1))
		require.Len(t, run.Steps, 2)
		assert.Equal(t, AutomationStepSuccess, run.Steps[0].Status)
		assert.Equal(t, AutomationStepFailed, run.Steps[1].Status)
	})

	t.Run("runs of other rules or projects are not found", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		_, _, err := (&AutomationRun{ID: failed.ID, RuleID: r.ID + 1, ProjectID: 1}).CanRead(s, autoUser1)
		assert.True(t, IsErrAutomationRunDoesNotExist(err))
		_, _, err = (&AutomationRun{ID: failed.ID, RuleID: r.ID, ProjectID: 11}).CanRead(s, autoUser1)
		assert.True(t, IsErrAutomationRunDoesNotExist(err))
		_, _, _, err = (&AutomationRun{RuleID: r.ID + 1, ProjectID: 1}).ReadAll(s, autoUser1, "", 1, 50)
		assert.True(t, IsErrAutomationRuleDoesNotExist(err))
	})

	t.Run("without admin access nothing is readable", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		can, _, _ := (&AutomationRun{ID: failed.ID, RuleID: r.ID, ProjectID: 1}).CanRead(s, autoUser2)
		assert.False(t, can)
		_, _, _, err := (&AutomationRun{RuleID: r.ID, ProjectID: 1}).ReadAll(s, autoUser2, "", 1, 50)
		assert.Error(t, err)
		can, _ = (&AutomationRunRetry{ProjectID: 1, RuleID: r.ID, RunID: failed.ID}).CanCreate(s, autoUser2)
		assert.False(t, can)
	})

	t.Run("retry puts a failed run back in the queue", func(t *testing.T) {
		s := db.NewSession()
		_, err := s.ID(failed.EventID).Cols("status", "attempts").Update(&AutomationEvent{Status: AutomationEventDead, Attempts: 5})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()

		s = db.NewSession()
		defer s.Close()
		retry := &AutomationRunRetry{ProjectID: 1, RuleID: r.ID, RunID: failed.ID}
		can, err := retry.CanCreate(s, autoUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, retry.Create(s, autoUser1))
		require.NoError(t, s.Commit())
		assert.Equal(t, AutomationRunRunning, retry.Run.Status)
		db.AssertExists(t, "automation_runs", map[string]interface{}{"id": failed.ID, "status": AutomationRunRunning}, false)
		db.AssertExists(t, "automation_events", map[string]interface{}{"id": failed.EventID, "status": AutomationEventPending, "attempts": 0}, false)
		db.AssertExists(t, "automation_run_steps", map[string]interface{}{"run_id": failed.ID, "position": 0, "status": AutomationStepSuccess}, false)
	})

	t.Run("only failed runs can be retried", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		err := (&AutomationRunRetry{ProjectID: 1, RuleID: r.ID, RunID: ok.ID}).Create(s, autoUser1)
		require.True(t, IsErrAutomationRunNotRetryable(err), "%v", err)
		assert.Equal(t, 409, err.(web.HTTPErrorProcessor).HTTPError().HTTPCode)
	})
}

func TestAutomationRule_DeleteRemovesRuns(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	r := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	other := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	run := insertAutomationRun(t, r.ID, 1, AutomationRunSuccess, AutomationStepSuccess)
	keep := insertAutomationRun(t, other.ID, 1, AutomationRunSuccess, AutomationStepSuccess)

	s := db.NewSession()
	del := &AutomationRule{ID: r.ID, ProjectID: 1}
	can, err := del.CanDelete(s, autoUser1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, del.Delete(s, autoUser1))
	require.NoError(t, s.Commit())
	s.Close()

	db.AssertMissing(t, "automation_rules", map[string]interface{}{"id": r.ID})
	db.AssertMissing(t, "automation_runs", map[string]interface{}{"id": run.ID})
	db.AssertMissing(t, "automation_run_steps", map[string]interface{}{"run_id": run.ID})
	db.AssertExists(t, "automation_runs", map[string]interface{}{"id": keep.ID}, false)
	db.AssertExists(t, "automation_run_steps", map[string]interface{}{"run_id": keep.ID}, false)
}

func TestAutomationCache(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	ResetAutomationCache()
	config.AutomationCacheTTL.Set(time.Hour)
	t.Cleanup(func() { config.AutomationCacheTTL.Set(0); ResetAutomationCache() })

	subscribed := func() map[string]bool {
		s := db.NewSession()
		defer s.Close()
		names, err := AutomationSubscribedEvents(s, 1)
		require.NoError(t, err)
		return names
	}

	assert.Empty(t, subscribed())
	r := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCompleted, setPriority(1)), autoUser1)
	assert.Equal(t, map[string]bool{"task.updated": true}, subscribed(), "completed is derived from the updated event")

	other := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskStatusChanged, setPriority(1)), autoUser1)
	assert.Len(t, subscribed(), 2)

	r.Enabled = false
	mustSaveAutomationRule(t, r, autoUser1)
	assert.Equal(t, map[string]bool{"task.status.changed": true}, subscribed())

	s := db.NewSession()
	require.NoError(t, (&AutomationRule{ID: other.ID, ProjectID: 1}).Delete(s, autoUser1))
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	s.Close()
	assert.Empty(t, subscribed())

	t.Run("a rollback leaves the cache as it was", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		rule := autoRule(1, spec.TriggerTaskDeleted, setPriority(1))
		require.NoError(t, rule.Create(s, autoUser1))
		require.NoError(t, s.Rollback())
		events.CleanupPending(s)
		assert.Empty(t, subscribed(), "rolled back and not visible")
	})
}
