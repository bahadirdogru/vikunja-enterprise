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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requeue(t *testing.T, eventID int64) {
	t.Helper()
	s := db.NewAutocommitSession()
	defer s.Close()
	_, err := s.ID(eventID).Cols("status", "attempts", "run_after", "locked_until").
		Update(&models.AutomationEvent{Status: models.AutomationEventPending, RunAfter: time.Now().Add(-time.Second)})
	require.NoError(t, err)
}

func TestIdempotency_ReprocessingAHandledEventDoesNothing(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "once"), rawAct("test.count", "key", "twice"))
	newTask(t, projectOwned, "task", user1)
	require.Equal(t, 1, drain(t))
	require.Equal(t, 1, counter("once"))
	require.Len(t, allRuns(t, r.ID), 1)

	// The same event delivered again - a lost acknowledgement, a replica that raced.
	requeue(t, allEvents(t)[0].ID)
	assert.Equal(t, 1, drain(t))

	assert.Equal(t, 1, counter("once"), "no action is performed twice")
	assert.Equal(t, 1, counter("twice"))
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1, "no second run")
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status)
	assert.Len(t, runSteps(t, runs[0].ID), 2)
}

func TestIdempotency_UniqueIndexRejectsASecondRunForTheSameEventAndRule(t *testing.T) {
	reset(t)
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "x"))
	newTask(t, projectOwned, "task", user1)
	drain(t)

	s := db.NewAutocommitSession()
	defer s.Close()
	_, err := s.Insert(&models.AutomationRun{
		RuleID: r.ID, EventID: allEvents(t)[0].ID, ProjectID: projectOwned, Status: models.AutomationRunRunning, Started: time.Now(),
	})
	require.Error(t, err, "(event_id, rule_id) is unique")

	_, err = s.Insert(&models.AutomationRunStep{RunID: allRuns(t, r.ID)[0].ID, Position: 0, ActionType: "x", Status: "success"})
	assert.Error(t, err, "(run_id, position) is unique")
}

func TestIdempotency_APartiallyExecutedRunResumesAtTheFirstUnfinishedStep(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
		rawAct("test.count", "key", "first"),
		rawAct("test.flaky", "key", "second", "fails", 1),
		rawAct("test.count", "key", "third"),
	)
	retryBase = time.Hour
	advance := fakeClock(t)
	newTask(t, projectOwned, "task", user1)

	// First attempt: step 0 commits, step 1 fails temporarily, step 2 is never reached.
	assert.Equal(t, 1, drain(t))
	assert.Equal(t, 1, counter("first"))
	assert.Equal(t, 0, counter("second"))
	assert.Equal(t, 0, counter("third"))
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunRunning, runs[0].Status)
	steps := runSteps(t, runs[0].ID)
	require.Len(t, steps, 2)
	assert.Equal(t, models.AutomationStepSuccess, steps[0].Status)
	assert.Equal(t, models.AutomationStepFailed, steps[1].Status)
	assert.Contains(t, steps[1].Error, "temporary failure")

	// The retry resumes at step 1; step 0 is not executed again.
	advance(2 * time.Hour)
	assert.Equal(t, 1, drain(t))
	assert.Equal(t, 1, counter("first"), "the step that succeeded is not executed again")
	assert.Equal(t, 1, counter("second"))
	assert.Equal(t, 1, counter("third"))
	runs = allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status)
	steps = runSteps(t, runs[0].ID)
	require.Len(t, steps, 3)
	for _, st := range steps {
		assert.Equal(t, models.AutomationStepSuccess, st.Status)
		assert.Empty(t, st.Error, "a successful retry clears the error of the step")
	}
	assert.EqualValues(t, 0, getRule(t, r.ID).ConsecutiveFailures)
}

func TestIdempotency_ConditionsAreNotEvaluatedAgainWhenResuming(t *testing.T) {
	reset(t)
	resetCounters()
	// The first action changes what the condition looks at. A resumed run must not stop because of it.
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
		act(spec.ActionSetField, map[string]any{"field": "priority", "value": 5}),
		rawAct("test.flaky", "key", "after", "fails", 1),
	)
	s := db.NewSession()
	r.Conditions = &spec.Condition{Field: "priority", Op: "<", Value: 5}
	_, err := s.ID(r.ID).Cols("conditions").Update(r)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	newTask(t, projectOwned, "task", user1)
	drain(t) // the retry follows at once: the backoff is 1ms
	assert.Equal(t, 1, counter("after"))
	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)
}

func TestIdempotency_ManualRetryOfAFailedRunResumes(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
		rawAct("test.count", "key", "a"),
		rawAct("test.reject"),
		rawAct("test.count", "key", "c"),
	)
	newTask(t, projectOwned, "task", user1)
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunFailed, run.Status)
	assert.Equal(t, 1, counter("a"))
	assert.Equal(t, 0, counter("c"))

	retry := &models.AutomationRunRetry{ProjectID: projectOwned, RuleID: r.ID, RunID: run.ID}
	mustCommit(t, func(s2 *xorm.Session) error {
		can, err := retry.CanCreate(s2, user1)
		require.NoError(t, err)
		require.True(t, can)
		return retry.Create(s2, user1)
	})
	assert.Equal(t, models.AutomationRunRunning, getRun(t, run.ID).Status)

	// The step that is broken for good fails again, the one before it is not repeated.
	drain(t)
	assert.Equal(t, 1, counter("a"))
	assert.Equal(t, 2, attemptsOf("reject"))
	assert.Equal(t, models.AutomationRunFailed, getRun(t, run.ID).Status)
	assert.Equal(t, 0, counter("c"))

	// Only failed runs can be retried.
	good := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "ok"))
	newTask(t, projectOwned, "task 2", user1)
	drain(t)
	goodRun := allRuns(t, good.ID)[0]
	require.Equal(t, models.AutomationRunSuccess, goodRun.Status)
	err := commit(t, func(s2 *xorm.Session) error {
		return (&models.AutomationRunRetry{ProjectID: projectOwned, RuleID: good.ID, RunID: goodRun.ID}).Create(s2, user1)
	})
	assert.True(t, models.IsErrAutomationRunNotRetryable(err))
}

func getRun(t *testing.T, id int64) *models.AutomationRun {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	run := &models.AutomationRun{}
	has, err := s.ID(id).Get(run)
	require.NoError(t, err)
	require.True(t, has)
	return run
}

func replaceActions(t *testing.T, r *models.AutomationRule, actions ...*spec.Action) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	_, err := s.ID(r.ID).Cols("actions", "revision").Update(&models.AutomationRule{Actions: actions, Revision: r.Revision + 1})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
}

func retryRun(t *testing.T, r *models.AutomationRule, run *models.AutomationRun) {
	t.Helper()
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.AutomationRunRetry{ProjectID: projectOwned, RuleID: r.ID, RunID: run.ID}).Create(s, user1)
	})
}

func TestIdempotency_RetryAfterTheRuleWasFixed(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "a"), rawAct("test.reject"))
	newTask(t, projectOwned, "task", user1)
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunFailed, run.Status)

	// The broken second step is replaced; the first one is what ran before.
	replaceActions(t, r, rawAct("test.count", "key", "a"), rawAct("test.count", "key", "fixed"))
	retryRun(t, r, run)
	drain(t)

	assert.Equal(t, 1, counter("a"), "the step that succeeded is not repeated")
	assert.Equal(t, 1, counter("fixed"))
	after := getRun(t, run.ID)
	assert.Equal(t, models.AutomationRunSuccess, after.Status)
	assert.EqualValues(t, 2, after.RuleRevision)
}

func TestIdempotency_RetryIsRefusedWhenTheRecordedStepsNoLongerMatchTheRule(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "a"), rawAct("test.reject"))
	newTask(t, projectOwned, "task", user1)
	drain(t)
	run := allRuns(t, r.ID)[0]

	// A new first action shifts everything: step 0 no longer is what it was.
	replaceActions(t, r, act(spec.ActionSetField, map[string]any{"field": "priority", "value": 1}), rawAct("test.count", "key", "a"), rawAct("test.count", "key", "b"))
	retryRun(t, r, run)
	drain(t)

	after := getRun(t, run.ID)
	assert.Equal(t, models.AutomationRunAborted, after.Status)
	assert.Equal(t, SkipRuleChanged, after.SkipReason)
	assert.Equal(t, 1, counter("a"))
	assert.Equal(t, 0, counter("b"))
}

func TestRetry_ARunThatFailedAfterItsWaitContinuesOnceFromThere(t *testing.T) {
	reset(t)
	resetCounters()
	advance := fakeClock(t)
	config.AutomationMaxAttempts.Set(1)
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
		rawAct("test.count", "key", "before"),
		rawAct(spec.ActionWait, "duration", "1h"),
		rawAct("test.flaky", "key", "after", "fails", 1),
		rawAct("test.count", "key", "last"),
	)
	newTask(t, projectOwned, "task", user1)
	drain(t)
	advance(2 * time.Hour)
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunFailed, run.Status, "the step after the wait failed")
	assert.Equal(t, 1, counter("before"))
	assert.Equal(t, 0, counter("after"))

	retryRun(t, r, run)
	assert.Equal(t, models.AutomationRunRunning, getRun(t, run.ID).Status)
	drain(t)
	drain(t)

	after := getRun(t, run.ID)
	assert.Equal(t, models.AutomationRunSuccess, after.Status, after.Error)
	assert.Equal(t, 1, counter("before"), "the steps before the wait are not repeated")
	assert.Equal(t, 1, counter("after"))
	assert.Equal(t, 1, counter("last"))
	assert.Len(t, runSteps(t, run.ID), 4)
}

func TestRetry_AReapedRunInItsWaitIsContinued(t *testing.T) {
	reset(t)
	resetCounters()
	advance := fakeClock(t)
	config.AutomationWaitMax.Set(2 * time.Hour)
	defer config.AutomationWaitMax.Set(30 * 24 * time.Hour)
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
		rawAct(spec.ActionWait, "duration", "1h"),
		rawAct("test.count", "key", "after"),
	)
	newTask(t, projectOwned, "task", user1)
	drain(t)
	// The resume event is lost, the reaper ends the run.
	s := db.NewAutocommitSession()
	_, err := s.Where("name = ?", spec.EventResume).Cols("status").Update(&models.AutomationEvent{Status: models.AutomationEventDead})
	require.NoError(t, err)
	s.Close()
	advance(40 * time.Hour)
	n, err := ReapStuckRuns(now())
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunFailed, run.Status)

	retryRun(t, r, run)
	drain(t)
	drain(t)
	after := getRun(t, run.ID)
	assert.Equal(t, models.AutomationRunSuccess, after.Status, after.Error)
	assert.Equal(t, 1, counter("after"))
}

func TestIdempotency_RetryOfOneRunDoesNotRerunTheEvent(t *testing.T) {
	reset(t)
	resetCounters()
	failing := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
		rawAct("test.count", "key", "a"),
		rawAct("test.reject"),
		rawAct("test.count", "key", "c"),
	)
	// A rule that was switched off when the event happened and one that did not exist yet: neither has a run for it.
	off := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "off"))
	setRuleEnabled(t, off, false)

	newTask(t, projectOwned, "task", user1)
	drain(t)
	run := allRuns(t, failing.ID)[0]
	require.Equal(t, models.AutomationRunFailed, run.Status)
	eventsBefore := allEvents(t)
	require.Len(t, eventsBefore, 1)
	eventID := eventsBefore[0].ID
	require.Equal(t, models.AutomationEventDone, eventsBefore[0].Status)

	late := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "late"))
	setRuleEnabled(t, off, true)

	retryRun(t, failing, run)
	drain(t)

	assert.Equal(t, 0, counter("late"), "a rule created after the event does not run for it")
	assert.Equal(t, 0, counter("off"), "a rule that was off when the event happened does not run for it")
	assert.Empty(t, allRuns(t, late.ID))
	assert.Empty(t, allRuns(t, off.ID))
	assert.Equal(t, 1, counter("a"), "the step that succeeded is not repeated")
	assert.Equal(t, 0, counter("c"))
	assert.Equal(t, models.AutomationRunFailed, getRun(t, run.ID).Status, "the broken step failed again")
	assert.Equal(t, 2, attemptsOf("reject"))
	for _, ev := range allEvents(t) {
		if ev.ID == eventID {
			assert.Equal(t, models.AutomationEventDone, ev.Status, "the original event is left as it was")
		}
	}

	// The run belongs to the event it was made for: nothing else was written for it.
	runs := allRuns(t, failing.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, eventID, runs[0].EventID)
}

func setRuleEnabled(t *testing.T, r *models.AutomationRule, enabled bool) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	_, err := s.ID(r.ID).Cols("enabled").Update(&models.AutomationRule{Enabled: enabled})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	models.ResetAutomationCache()
}

func resumeEventsOf(t *testing.T, ruleID int64) int {
	t.Helper()
	n := 0
	for _, ev := range allEvents(t) {
		if ev.Name == spec.EventResume && ev.RuleID == ruleID {
			n++
		}
	}
	return n
}

func TestIdempotency_TwoRetriesOfOneRunQueueOneEvent(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.reject"))
	newTask(t, projectOwned, "task", user1)
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunFailed, run.Status)

	// Both requests loaded the run while it was failed; the second one finds it running when it writes.
	first := &models.AutomationRunRetry{ProjectID: projectOwned, RuleID: r.ID, RunID: run.ID}
	second := &models.AutomationRunRetry{ProjectID: projectOwned, RuleID: r.ID, RunID: run.ID}
	s1, s2 := db.NewSession(), db.NewSession()
	defer s1.Close()
	defer s2.Close()
	require.NoError(t, first.Create(s1, user1))
	require.NoError(t, s1.Commit())
	err := second.Create(s2, user1)
	assert.True(t, models.IsErrAutomationRunNotRetryable(err), "got %v", err)
	assert.Equal(t, 1, resumeEventsOf(t, r.ID))
}

func TestIdempotency_RetryWithoutTheEventIsRefused(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.reject"))
	newTask(t, projectOwned, "task", user1)
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunFailed, run.Status)

	s := db.NewSession()
	_, err := s.ID(run.EventID).Delete(&models.AutomationEvent{})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	err = commit(t, func(s2 *xorm.Session) error {
		return (&models.AutomationRunRetry{ProjectID: projectOwned, RuleID: r.ID, RunID: run.ID}).Create(s2, user1)
	})
	require.Error(t, err)
	assert.True(t, models.IsErrAutomationRunEventGone(err), "got %v", err)
	assert.Equal(t, models.AutomationRunFailed, getRun(t, run.ID).Status, "the run is left as it was")
	assert.Zero(t, resumeEventsOf(t, r.ID))
}
