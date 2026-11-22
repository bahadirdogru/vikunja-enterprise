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
	"sync"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorker_ConcurrentClaimsRunEveryEventExactlyOnce(t *testing.T) {
	reset(t)
	resetCounters()
	r1 := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "one"))
	r2 := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "two"))

	const tasks = 30
	for i := 0; i < tasks; i++ {
		newTask(t, projectOwned, "task", user1)
	}
	require.Len(t, allEvents(t), tasks)

	var wg sync.WaitGroup
	processed := make([]int, 4)
	for i := range processed {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := ProcessPending(context.Background())
			assert.NoError(t, err)
			processed[i] = n
		}()
	}
	wg.Wait()

	total := 0
	for _, n := range processed {
		total += n
	}
	assert.Equal(t, tasks, total, "every event is claimed by exactly one caller")
	assert.Equal(t, tasks, counter("one"))
	assert.Equal(t, tasks, counter("two"))
	assert.Len(t, allRuns(t, r1.ID), tasks)
	assert.Len(t, allRuns(t, r2.ID), tasks)
	for _, ev := range allEvents(t) {
		assert.Equal(t, models.AutomationEventDone, ev.Status)
	}
}

func TestWorker_RecoversEventsOfAWorkerThatDisappeared(t *testing.T) {
	reset(t)
	resetCounters()
	insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "x"))
	newTask(t, projectOwned, "stuck", user1)

	// Claimed by a worker that then died: processing, lock already expired.
	s := db.NewAutocommitSession()
	ev := allEvents(t)[0]
	claimed, err := claim(s, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	_, err = s.ID(ev.ID).Cols("locked_until").Update(&models.AutomationEvent{LockedUntil: time.Now().Add(-time.Minute)})
	require.NoError(t, err)
	s.Close()

	// Still locked events are left alone.
	stillLocked := newTask(t, projectOwned, "locked", user1)
	s = db.NewAutocommitSession()
	claimed, err = claim(s, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, stillLocked.ID, claimed[0].TaskID)
	s.Close()

	assert.Equal(t, 1, drain(t), "only the stuck one is recovered and run")
	assert.Equal(t, 1, counter("x"))
	events := allEvents(t)
	assert.Equal(t, models.AutomationEventDone, events[0].Status)
	assert.EqualValues(t, 2, events[0].Attempts)
	assert.Equal(t, models.AutomationEventProcessing, events[1].Status)
}

func TestWorker_StuckEventsThatUsedUpTheirAttemptsAreDead(t *testing.T) {
	reset(t)
	config.AutomationMaxAttempts.Set(2)
	insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "x"))
	newTask(t, projectOwned, "crashes every time", user1)

	s := db.NewAutocommitSession()
	defer s.Close()
	_, err := s.Where("1 = 1").Cols("status", "attempts", "locked_until").Update(&models.AutomationEvent{
		Status: models.AutomationEventProcessing, Attempts: 2, LockedUntil: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)

	assert.Equal(t, 0, drain(t))
	assert.Equal(t, models.AutomationEventDead, allEvents(t)[0].Status)
}

func TestWorker_RetriesWithBackoffThenDead(t *testing.T) {
	reset(t)
	resetCounters()
	config.AutomationMaxAttempts.Set(3)
	retryBase = time.Hour // nothing is due again during the test
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.flaky", "key", "f", "fails", 100))
	newTask(t, projectOwned, "never works", user1)

	advance := fakeClock(t)

	// Attempt 1 fails: back to pending, due after one base interval.
	assert.Equal(t, 1, drain(t))
	ev := allEvents(t)[0]
	assert.Equal(t, models.AutomationEventPending, ev.Status)
	assert.EqualValues(t, 1, ev.Attempts)
	assert.WithinDuration(t, now().Add(time.Hour), ev.RunAfter, time.Second)
	assert.Contains(t, ev.LastError, "temporary failure")
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunRunning, runs[0].Status, "a run waiting for its retry is still running")

	// Not due yet: nothing happens.
	assert.Equal(t, 0, drain(t))
	assert.Equal(t, 1, attemptsOf("f.attempts"))

	// Attempt 2: the wait doubles.
	advance(time.Hour + time.Minute)
	assert.Equal(t, 1, drain(t))
	ev = allEvents(t)[0]
	assert.Equal(t, models.AutomationEventPending, ev.Status)
	assert.EqualValues(t, 2, ev.Attempts)
	assert.WithinDuration(t, now().Add(2*time.Hour), ev.RunAfter, time.Second)

	// Attempt 3 is the last: dead, and the run fails.
	advance(3 * time.Hour)
	assert.Equal(t, 1, drain(t))
	ev = allEvents(t)[0]
	assert.Equal(t, models.AutomationEventDead, ev.Status)
	assert.EqualValues(t, 3, ev.Attempts)
	runs = allRuns(t, r.ID)
	assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
	assert.Contains(t, runs[0].Error, "temporary failure")
	assert.Equal(t, 3, attemptsOf("f.attempts"))
	assert.EqualValues(t, 1, getRule(t, r.ID).ConsecutiveFailures)

	// Dead events are not picked up again.
	advance(100 * time.Hour)
	assert.Equal(t, 0, drain(t))
}

func TestWorker_BackoffDoubles(t *testing.T) {
	retryBase = 10 * time.Second
	t.Cleanup(func() { retryBase = time.Millisecond })
	assert.Equal(t, 10*time.Second, backoff(1))
	assert.Equal(t, 20*time.Second, backoff(2))
	assert.Equal(t, 40*time.Second, backoff(3))
	assert.Equal(t, 80*time.Second, backoff(4))
}

func TestWorker_PermanentFailuresAreNotRetried(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.reject"))
	newTask(t, projectOwned, "rejected", user1)

	assert.Equal(t, 1, drain(t))
	assert.Equal(t, 1, attemptsOf("reject"))
	ev := allEvents(t)[0]
	assert.Equal(t, models.AutomationEventDone, ev.Status, "the event is handled, the run failed")
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
	assert.Contains(t, runs[0].Error, "rejected on purpose")
	steps := runSteps(t, runs[0].ID)
	require.Len(t, steps, 1)
	assert.Equal(t, models.AutomationStepFailed, steps[0].Status)
	assert.Equal(t, "rejected on purpose", steps[0].Error)
}

func TestWorker_StartedWorkersProcessEvents(t *testing.T) {
	reset(t)
	resetCounters()
	config.AutomationWorkers.Set(2)
	config.AutomationPollInterval.Set(20 * time.Millisecond)
	t.Cleanup(func() { config.AutomationWorkers.Set(4); config.AutomationPollInterval.Set(time.Second) })
	insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "w"))

	Start(context.Background())
	t.Cleanup(Stop)
	newTask(t, projectOwned, "picked up by a worker", user1)

	require.Eventually(t, func() bool { return counter("w") == 1 }, 5*time.Second, 10*time.Millisecond)
	Stop()
}

func TestWorker_CleanupDeletesOldFinishedWork(t *testing.T) {
	reset(t)
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.count", "key", "c"))
	newTask(t, projectOwned, "old", user1)
	drain(t)
	newTask(t, projectOwned, "pending", user1) // not finished: kept
	require.Len(t, allRuns(t, r.ID), 1)

	events, runs, err := Cleanup(time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 0, events+runs, "nothing is old enough")

	events, runs, err = Cleanup(time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, events)
	assert.EqualValues(t, 1, runs)
	assert.Empty(t, allRuns(t, r.ID))
	remaining := allEvents(t)
	require.Len(t, remaining, 1)
	assert.Equal(t, models.AutomationEventPending, remaining[0].Status)
}
