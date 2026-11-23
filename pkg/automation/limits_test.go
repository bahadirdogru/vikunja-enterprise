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

	"xorm.io/xorm"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setField(field string, value any) *spec.Action {
	return act(spec.ActionSetField, map[string]any{"field": field, "value": value})
}

func updatedBy(fields ...string) map[string]any {
	list := make([]any, len(fields))
	for i, f := range fields {
		list[i] = f
	}
	return map[string]any{"fields": list}
}

func TestLimits_ChainDepth(t *testing.T) {
	reset(t)
	// created -> A(priority) -> B(color) -> C(percent) -> D(description)
	a := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setField("priority", 2)))
	b := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("priority"), nil, setField("hex_color", "#ff0000")))
	c := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("hex_color"), nil, setField("percent_done", 0.5)))
	d := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("percent_done"), nil, setField("description", "reached D")))

	config.AutomationMaxDepth.Set(2)
	task := newTask(t, projectOwned, "chain", user1)
	drain(t)

	got := getTask(t, task.ID)
	assert.EqualValues(t, 2, got.Priority)
	assert.Equal(t, "ff0000", got.HexColor)
	assert.InDelta(t, 0.5, got.PercentDone, 0.001)
	assert.NotEqual(t, "reached D", got.Description, "the chain stops at the maximum depth")

	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, a.ID)[0].Status)
	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, b.ID)[0].Status)
	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, c.ID)[0].Status)
	skipped := allRuns(t, d.ID)
	require.Len(t, skipped, 1, "the skip is recorded")
	assert.Equal(t, models.AutomationRunSkipped, skipped[0].Status)
	assert.Equal(t, SkipMaxDepth, skipped[0].SkipReason)
	assert.EqualValues(t, 3, skipped[0].Depth)
	assert.Empty(t, runSteps(t, skipped[0].ID))

	// With a higher limit the whole chain runs.
	reset(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setField("priority", 2)))
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("priority"), nil, setField("hex_color", "#ff0000")))
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("hex_color"), nil, setField("percent_done", 0.5)))
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("percent_done"), nil, setField("description", "reached D")))
	task = newTask(t, projectOwned, "chain", user1)
	drain(t)
	assert.Equal(t, "reached D", getTask(t, task.ID).Description)
}

func TestLimits_ARuleNeverReactsToItsOwnChanges(t *testing.T) {
	reset(t)
	self := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil, nil, setField("priority", 4)))

	task := newTask(t, projectOwned, "task", user1)
	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Title = "changed by a person" })
	drain(t)

	assert.EqualValues(t, 4, getTask(t, task.ID).Priority)
	runs := allRuns(t, self.ID)
	require.Len(t, runs, 2)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status, "the rule runs for the change of the person")
	assert.Equal(t, models.AutomationRunSkipped, runs[1].Status, "and not for what its own run changed")
	assert.Equal(t, SkipSelfTrigger, runs[1].SkipReason)
	assert.EqualValues(t, 1, runs[1].Depth)

	t.Run("also when another rule is in between", func(t *testing.T) {
		reset(t)
		a := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("title", "hex_color"), nil, setField("priority", 4)))
		b := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("priority"), nil, setField("hex_color", "#00ff00")))

		task := newTask(t, projectOwned, "task", user1)
		updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Title = "by a person" })
		drain(t)

		// person -> A (priority) -> B (color) -> would trigger A again, which is part of the chain.
		runsA := allRuns(t, a.ID)
		require.Len(t, runsA, 2)
		assert.Equal(t, models.AutomationRunSuccess, runsA[0].Status)
		assert.Equal(t, models.AutomationRunSkipped, runsA[1].Status)
		assert.Equal(t, SkipSelfTrigger, runsA[1].SkipReason)
		assert.Equal(t, models.AutomationRunSuccess, allRuns(t, b.ID)[0].Status)
	})

	t.Run("a rule still reacts to the same kind of change on another task", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil, setField("priority", 4)))
		first := newTask(t, projectOwned, "one", user1)
		second := newTask(t, projectOwned, "two", user1)
		updateTask(t, user1, first.ID, func(tk *models.Task) { tk.Title = "one!" })
		updateTask(t, user1, second.ID, func(tk *models.Task) { tk.Title = "two!" })
		drain(t)
		assert.EqualValues(t, 4, getTask(t, first.ID).Priority)
		assert.EqualValues(t, 4, getTask(t, second.ID).Priority)
		assert.Len(t, allRuns(t, r.ID), 2)
	})
}

func TestLimits_MaxActionsPerRun(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
		rawAct("test.count", "key", "a"), rawAct("test.count", "key", "b"), rawAct("test.count", "key", "c"))
	config.AutomationMaxActions.Set(2)
	newTask(t, projectOwned, "task", user1)
	drain(t)

	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
	assert.Contains(t, runs[0].Error, "at most 2")
	assert.Equal(t, 0, counter("a"), "nothing runs when the rule is over the limit")

	// The same limit applies when a rule is saved.
	config.AutomationMaxActions.Set(1)
	err := commit(t, func(s2 *xorm.Session) error {
		x := rule(spec.TriggerTaskCreated, nil, nil, setField("priority", 1), setField("priority", 2))
		x.ProjectID = projectOwned
		return x.Create(s2, user1)
	})
	assert.True(t, models.IsErrAutomationRuleInvalid(err))
}

func TestLimits_StepTimeout(t *testing.T) {
	reset(t)
	config.AutomationStepTimeout.Set(50 * time.Millisecond)
	config.AutomationMaxAttempts.Set(1)
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.slow"))
	newTask(t, projectOwned, "task", user1)

	start := time.Now()
	drain(t)
	assert.Less(t, time.Since(start), 5*time.Second)

	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
	assert.Contains(t, runs[0].Error, "deadline exceeded")
	steps := runSteps(t, runs[0].ID)
	require.Len(t, steps, 1)
	assert.Equal(t, models.AutomationStepFailed, steps[0].Status)
	assert.Equal(t, models.AutomationEventDead, allEvents(t)[0].Status, "a timeout is a temporary failure, retried until the attempts are used up")
}

func TestLimits_RunTimeout(t *testing.T) {
	reset(t)
	config.AutomationRunTimeout.Set(80 * time.Millisecond)
	config.AutomationStepTimeout.Set(time.Minute)
	config.AutomationMaxAttempts.Set(1)
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.slow"), rawAct("test.count", "key", "never"))
	newTask(t, projectOwned, "task", user1)

	start := time.Now()
	drain(t)
	assert.Less(t, time.Since(start), 5*time.Second)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
	assert.Equal(t, 0, counter("never"))
}

func TestLimits_RateLimitSkipsAndRecordsTheExcess(t *testing.T) {
	reset(t)
	resetCounters()
	config.AutomationRateLimit.Set(3)
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "x"))
	for i := 0; i < 5; i++ {
		newTask(t, projectOwned, "task", user1)
	}
	drain(t)

	runs := allRuns(t, r.ID)
	require.Len(t, runs, 5)
	var ok, skipped int
	for _, run := range runs {
		switch run.Status {
		case models.AutomationRunSuccess:
			ok++
		case models.AutomationRunSkipped:
			skipped++
			assert.Equal(t, SkipRateLimit, run.SkipReason)
		}
	}
	assert.Equal(t, 3, ok)
	assert.Equal(t, 2, skipped)
	assert.Equal(t, 3, counter("x"))

	// A minute later the limit has room again.
	advance := fakeClock(t)
	advance(2 * time.Minute)
	newTask(t, projectOwned, "later", user1)
	drain(t)
	assert.Equal(t, 4, counter("x"))
}

func TestLimits_AutoDisableAfterConsecutiveFailures(t *testing.T) {
	reset(t)
	config.AutomationMaxConsecutiveFails.Set(3)
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.reject"))
	for i := 0; i < 5; i++ {
		newTask(t, projectOwned, "task", user1)
	}
	drain(t)

	stored := getRule(t, r.ID)
	assert.False(t, stored.Enabled)
	assert.Equal(t, models.AutomationDisabledTooManyFailures, stored.DisabledReason)
	assert.EqualValues(t, 3, stored.ConsecutiveFailures)
	assert.Len(t, allRuns(t, r.ID), 3, "no run after the rule was disabled")

	// A success resets the counter.
	reset(t)
	resetCounters()
	config.AutomationMaxConsecutiveFails.Set(3)
	flaky := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.reject"))
	newTask(t, projectOwned, "task", user1)
	newTask(t, projectOwned, "task", user1)
	drain(t)
	assert.EqualValues(t, 2, getRule(t, flaky.ID).ConsecutiveFailures)
	s := db.NewSession()
	_, err := s.ID(flaky.ID).Cols("actions").Update(&models.AutomationRule{Actions: []*spec.Action{rawAct("test.count", "key", "ok")}})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	newTask(t, projectOwned, "task", user1)
	drain(t)
	after := getRule(t, flaky.ID)
	assert.EqualValues(t, 0, after.ConsecutiveFailures)
	assert.True(t, after.Enabled)
	assert.Equal(t, models.AutomationRunSuccess, after.LastRunStatus)
}

func TestLimits_OwnerWhoLostAdminAbortsAndDisablesTheRule(t *testing.T) {
	reset(t)
	resetCounters()
	// user1 administers project 11 through a share; user6 owns it.
	r := insertRawRule(t, projectWorkflow, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "x"))

	newTask(t, projectWorkflow, "while admin", user6)
	drain(t)
	require.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)

	s := db.NewSession()
	_, err := s.Where("project_id = ? AND user_id = ?", projectWorkflow, user1.ID).Delete(&models.ProjectUser{})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	newTask(t, projectWorkflow, "after", user6)
	drain(t)

	runs := allRuns(t, r.ID)
	require.Len(t, runs, 2)
	assert.Equal(t, models.AutomationRunAborted, runs[1].Status)
	assert.Equal(t, SkipOwnerNotAdmin, runs[1].SkipReason)
	assert.Equal(t, 1, counter("x"), "the second run did nothing")
	stored := getRule(t, r.ID)
	assert.False(t, stored.Enabled)
	assert.Equal(t, models.AutomationDisabledOwnerNotAdmin, stored.DisabledReason)
}

func TestLimits_OwnerWhoCannotLogInAnymoreAbortsTheRule(t *testing.T) {
	reset(t)
	r := insertRawRule(t, projectOwned, user2, spec.TriggerTaskCreated, rawAct("test.count", "key", "x"))
	s := db.NewSession()
	_, err := s.Table("users").Where("id = ?", user2.ID).Update(map[string]any{"status": int(user.StatusDisabled)})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	newTask(t, projectOwned, "task", user1)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunAborted, runs[0].Status)
	assert.Equal(t, models.AutomationDisabledOwnerUnavailable, getRule(t, r.ID).DisabledReason)
}

func TestLimits_ARuleDoesNotReactToTheTasksItCreated(t *testing.T) {
	reset(t)
	subtask := &spec.Action{Type: spec.ActionCreateSubtask, Params: map[string]any{"title": "child of {{task.title}}"}}
	self := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, subtask))

	s := newSession()
	before, err := s.Where("project_id = ?", projectOwned).Count(&models.Task{})
	_ = s.Close()
	require.NoError(t, err)

	newTask(t, projectOwned, "parent", user1)
	drain(t)

	s = newSession()
	after, err := s.Where("project_id = ?", projectOwned).Count(&models.Task{})
	_ = s.Close()
	require.NoError(t, err)
	assert.Equal(t, before+2, after, "the parent and exactly one subtask, no chain")

	runs := allRuns(t, self.ID)
	require.Len(t, runs, 2)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status)
	assert.Equal(t, models.AutomationRunSkipped, runs[1].Status)
	assert.Equal(t, SkipSelfTrigger, runs[1].SkipReason)
}
