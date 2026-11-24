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
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
