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
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/script"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// runawaySource does its work in one native call of the interpreter, where the interrupt of the timeout is
// never seen: in-process it ran for days and held its worker.
const runawaySource = `Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`

// bounded runs fn in a goroutine and fails the test when it has not returned after 10 seconds, so a script that
// outlives its timeout fails the test instead of hanging the test binary.
func bounded(t *testing.T, fn func()) time.Duration {
	const limit = 10 * time.Second
	t.Helper()
	done := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return time.Since(started)
	case <-time.After(limit):
		t.Fatalf("still running after %s: a script outlived its timeout", limit)
		return 0
	}
}

func TestScript_ANativeCallCannotOutliveTheTimeout(t *testing.T) {
	t.Run("script action", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsTimeout.Set(200 * time.Millisecond)
		var run *models.AutomationRun
		var steps []*models.AutomationRunStep
		elapsed := bounded(t, func() { _, run, steps = runScript(t, runawaySource) })
		assert.Less(t, elapsed, 5*time.Second)
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		require.Len(t, steps, 1)
		assert.Contains(t, steps[0].Error, "ran longer than 200ms")
		assert.Equal(t, models.AutomationEventDone, allEvents(t)[len(allEvents(t))-1].Status, "a timeout of the script is not retried")
	})

	t.Run("script condition", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsConditionTimeout.Set(200 * time.Millisecond)
		config.AutomationScriptsTimeout.Set(time.Minute)
		var run *models.AutomationRun
		var task *models.Task
		elapsed := bounded(t, func() { run, task = runWith2(t, &spec.Condition{Script: "return " + runawaySource + " > 0"}) })
		assert.Less(t, elapsed, 5*time.Second)
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, run.Error, "ran longer than 200ms")
		assert.NotEqualValues(t, 4, task.Priority, "the action after the condition did not run")
	})

	t.Run("dry run of a script action", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsTimeout.Set(200 * time.Millisecond)
		r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, scriptAction(runawaySource))
		tk := newTask(t, projectOwned, "t", user1)
		var res *DryRunResult
		elapsed := bounded(t, func() { res = dryRun(t, r, tk.ID) })
		assert.Less(t, elapsed, 5*time.Second)
		require.Len(t, res.Steps, 1)
		assert.Contains(t, res.Steps[0].Error, "ran longer than 200ms")
	})

	t.Run("dry run of a script condition", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsConditionTimeout.Set(200 * time.Millisecond)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, &spec.Condition{Script: "return " + runawaySource + " > 0"}, setField("priority", 4)))
		tk := newTask(t, projectOwned, "t", user1)
		var res *DryRunResult
		elapsed := bounded(t, func() { res = dryRun(t, r, tk.ID) })
		assert.Less(t, elapsed, 5*time.Second)
		assert.False(t, res.ConditionsMet)
		assert.Contains(t, strings.Join(res.Notes, "\n"), "ran longer than 200ms")
	})
}

// TestScript_AFailureOfTheMachineryIsNotTheRulesFault: when no script process can be started, the run is retried
// and, when the retries ran out, fails without counting towards disabling the rule.
func TestScript_AFailureOfTheMachineryIsNotTheRulesFault(t *testing.T) {
	for name, r := range map[string]func() *models.AutomationRule{
		"script action": func() *models.AutomationRule {
			return rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`return 1`))
		},
		"script condition": func() *models.AutomationRule {
			return rule(spec.TriggerTaskCreated, nil, &spec.Condition{Script: "return true"}, setField("priority", 4))
		},
	} {
		t.Run(name, func(t *testing.T) {
			reset(t)
			retryBase = time.Hour
			advance := fakeClock(t)
			config.AutomationMaxAttempts.Set(2)
			config.AutomationMaxConsecutiveFails.Set(1)
			saved := addRule(t, projectOwned, user1, r())
			t.Cleanup(script.SetTestExecutable(filepath.Join(t.TempDir(), "missing")))

			newTask(t, projectOwned, "t", user1)
			drain(t)
			runs := allRuns(t, saved.ID)
			require.Len(t, runs, 1)
			assert.Equal(t, models.AutomationRunRunning, runs[0].Status, "temporary: retried")
			events := allEvents(t)
			assert.Contains(t, events[len(events)-1].LastError, "cannot start the script process")

			advance(time.Hour)
			drain(t)
			runs = allRuns(t, saved.ID)
			require.Len(t, runs, 1)
			assert.Equal(t, models.AutomationRunFailed, runs[0].Status, "the retries ran out")
			stored := getRule(t, saved.ID)
			assert.Zero(t, stored.ConsecutiveFailures, "not the rule's fault")
			assert.True(t, stored.Enabled)
		})
	}
}

// TestScript_ATaskTooLargeForAScriptFailsTheRunWithoutBlame: any member who can write the task can make it that
// large, so the run fails for good (running it again fails the same way) but does not count against the rule.
func TestScript_ATaskTooLargeForAScriptFailsTheRunWithoutBlame(t *testing.T) {
	reset(t)
	config.AutomationMaxConsecutiveFails.Set(1)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`return task.description.length`)))
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.Task{Title: "huge", Description: strings.Repeat("x", 9<<20), ProjectID: projectOwned}).Create(s, user1)
	})
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
	assert.Contains(t, runs[0].Error, "too large or too deeply nested to be passed to a script")
	stored := getRule(t, r.ID)
	assert.Zero(t, stored.ConsecutiveFailures)
	assert.True(t, stored.Enabled)
	events := allEvents(t)
	assert.Equal(t, models.AutomationEventDone, events[len(events)-1].Status, "not retried")
}

// TestWorker_AGivenUpEventFailsEachRunWithTheErrorOfItsOwnRule: before, every run got the first rule's error, so
// a machinery failure of one rule decided the blame of the others.
func TestWorker_AGivenUpEventFailsEachRunWithTheErrorOfItsOwnRule(t *testing.T) {
	reset(t)
	retryBase = time.Hour
	advance := fakeClock(t)
	config.AutomationMaxAttempts.Set(2)
	allowLoopback(t)
	srv := countingServer(t, 500, "boom")
	scripted := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`return 1`)))
	failing := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, httpAction(map[string]any{"url": srv.url + "/x"})))
	t.Cleanup(script.SetTestExecutable(filepath.Join(t.TempDir(), "missing")))

	newTask(t, projectOwned, "t", user1)
	drain(t)
	advance(time.Hour)
	drain(t)

	machinery := allRuns(t, scripted.ID)[0]
	assert.Equal(t, models.AutomationRunFailed, machinery.Status)
	assert.Contains(t, machinery.Error, "cannot start the script process")
	assert.Zero(t, getRule(t, scripted.ID).ConsecutiveFailures)

	own := allRuns(t, failing.ID)[0]
	assert.Equal(t, models.AutomationRunFailed, own.Status)
	assert.Contains(t, own.Error, "500")
	assert.EqualValues(t, 1, getRule(t, failing.ID).ConsecutiveFailures, "its own failure counts")
}

// TestScript_AnAnswerTooLargeForTheScriptIsNotTheRulesFault: a task the owner can read, inflated by anyone with
// write access to it, must not get the rule disabled. Caught, the script goes on.
func TestScript_AnAnswerTooLargeForTheScriptIsNotTheRulesFault(t *testing.T) {
	reset(t)
	config.AutomationMaxConsecutiveFails.Set(1)
	big := &models.Task{Title: "inflated", Description: strings.Repeat("<", 9<<20), ProjectID: projectOwned}
	mustCommit(t, func(s *xorm.Session) error { return big.Create(s, user1) })
	id := strconv.FormatInt(big.ID, 10)

	t.Run("uncaught", func(t *testing.T) {
		_, run, _ := runScript(t, `vikunja.tasks.get(`+id+`)`)
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, run.Error, "larger than 8 MiB")
		stored := getRule(t, run.RuleID)
		assert.Zero(t, stored.ConsecutiveFailures)
		assert.True(t, stored.Enabled)
		events := allEvents(t)
		assert.Equal(t, models.AutomationEventDone, events[len(events)-1].Status, "not retried")
	})
	t.Run("caught", func(t *testing.T) {
		_, run, steps := runScript(t, `try { vikunja.tasks.get(`+id+`) } catch (e) { return "caught" }`)
		assert.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, stepOutput(steps), `"caught"`)
	})
}

// TestScript_ARuleCanBeDisabledWhileScriptsCannotBeChecked: sources that are stored passed the check already.
func TestScript_ARuleCanBeDisabledWhileScriptsCannotBeChecked(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, &spec.Condition{Script: "return true"}, scriptAction(`return 1`)))
	t.Cleanup(script.SetTestExecutable(filepath.Join(t.TempDir(), "missing")))
	update := func(mutate func(*models.AutomationRule)) error {
		stored := getRule(t, r.ID)
		mutate(stored)
		return commit(t, func(s *xorm.Session) error { return stored.Update(s, user1) })
	}

	require.NoError(t, update(func(r *models.AutomationRule) { r.Enabled = false }), "disabling always works")
	assert.False(t, getRule(t, r.ID).Enabled)
	require.NoError(t, update(func(r *models.AutomationRule) { r.Title = "renamed" }), "so does editing what is not a script")

	err := update(func(r *models.AutomationRule) { r.Actions[0].Params["source"] = "return 2" })
	issues := ruleIssues(t, err)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Message, "the script could not be checked")

	fresh := rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`return 1`))
	fresh.ProjectID = projectOwned
	err = saveRule(t, fresh)
	issues = ruleIssues(t, err)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Message, "the script could not be checked", "a new rule is still checked")
}
