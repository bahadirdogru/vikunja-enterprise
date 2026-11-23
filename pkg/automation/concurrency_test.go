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
	"encoding/json"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// advanceClock moves the fake clock of the running test.
	advanceClock func(time.Duration)
	// lockLeft is what test.probe saw: how long the event of its run was still locked for its worker.
	lockLeft time.Duration
)

func init() {
	// test.advance stands for a rule that takes long: the clock of the engine moves on by the given seconds.
	register("test.advance", func(_ *Env, p params) (*Plan, error) {
		return &Plan{Summary: "advance", apply: func() (string, error) {
			advanceClock(time.Duration(p.int("seconds")) * time.Second)
			return "advanced", nil
		}}, nil
	})
	// test.probe records how long the lock of the event it runs for still holds.
	register("test.probe", func(env *Env, _ params) (*Plan, error) {
		return &Plan{Summary: "probe", apply: func() (string, error) {
			ev := &models.AutomationEvent{}
			if _, err := env.S.Where("task_id = ?", env.Event.TaskID).Get(ev); err != nil {
				return "", err
			}
			lockLeft = ev.LockedUntil.Sub(now())
			return "probed", nil
		}}, nil
	})
}

// Two workers can hold the same run: the lock of an event covers one run timeout but an event runs every rule of the
// project in sequence, and a manual retry puts an event back in the queue while a worker may still be busy with
// another run of it. The second worker decided which steps are left before the first one committed its step, then
// waits for the write lock of the run; when it gets it the step is done.
func TestIdempotency_ASecondWorkerOnTheSameRunDoesNotRepeatACommittedStep(t *testing.T) {
	reset(t)
	resetCounters()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
		rawAct("test.count", "key", "once"),
		rawAct("test.count", "key", "twice"),
	)
	newTask(t, projectOwned, "task", user1)
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	require.Equal(t, 1, counter("once"))

	// The second worker: same run, with the list of finished steps it read before the first worker committed.
	ev := allEvents(t)[0]
	payload := &Payload{}
	require.NoError(t, json.Unmarshal([]byte(ev.Payload), payload))
	s := db.NewSession()
	owner, err := user.GetUserByID(s, user1.ID)
	s.Close()
	require.NoError(t, err)
	rc := &runContext{ev: ev, payload: payload, rule: getRule(t, r.ID), run: run, owner: owner}
	for position, action := range rc.rule.Actions {
		stop, suspend, err := rc.executeStep(context.Background(), context.Background(), int64(position), action)
		require.NoError(t, err)
		assert.False(t, stop)
		assert.False(t, suspend)
	}

	assert.Equal(t, 1, counter("once"), "a step recorded as successful is not applied again")
	assert.Equal(t, 1, counter("twice"))
	steps := runSteps(t, run.ID)
	require.Len(t, steps, 2)
	assert.Equal(t, "counted once", steps[0].Output)
}

// An event runs every rule of its project in sequence and each run has the whole run timeout, so the lock taken
// when the event was claimed has to be renewed for each run. Otherwise another worker takes the event over while the
// first one is still working on it.
func TestWorker_TheLockOfAnEventCoversEveryRuleOfIt(t *testing.T) {
	reset(t)
	advanceClock = fakeClock(t)
	lockLeft = 0
	// The first rule uses up more than the lock of one run (run timeout + 30 s).
	insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.advance", "seconds", 100))
	second := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, rawAct("test.probe"))
	newTask(t, projectOwned, "task", user1)
	drain(t)

	runs := allRuns(t, second.ID)
	require.Len(t, runs, 1)
	require.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	assert.Positive(t, lockLeft, "the event is still locked for its worker while the second rule runs")
}
