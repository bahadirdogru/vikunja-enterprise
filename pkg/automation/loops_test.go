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

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countProjectTasks(t *testing.T, projectID int64) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("project_id = ?", projectID).Count(&models.Task{})
	require.NoError(t, err)
	return n
}

func createSoonDue(title string) *spec.Action {
	return act(spec.ActionCreateTask, map[string]any{"title": title, "due_offset": "+1m"})
}

// runTicks lets the clock run for a while, ticking and processing after every step.
func runTicks(t *testing.T, advance func(time.Duration), steps int, step time.Duration) {
	t.Helper()
	for i := 0; i < steps; i++ {
		advance(step)
		tick(t)
		drain(t)
	}
}

// A rule whose action creates tasks that are overdue (or due soon) themselves must not feed itself.
func TestOverdue_TasksCreatedByTheRuleDoNotFireItAgain(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, createSoonDue("followup")))
	task := newTask(t, projectOwned, "late", user1)
	updateTask(t, user1, task.ID, dueIn(time.Hour), "due_date")

	runTicks(t, advance, 12, 2*time.Minute)
	runTicks(t, advance, 1, 2*time.Hour)
	before := countProjectTasks(t, projectOwned)
	runTicks(t, advance, 12, 2*time.Minute)

	assert.EqualValues(t, 2, before, "the late task and the one task the rule created for it")
	assert.Equal(t, before, countProjectTasks(t, projectOwned), "no further tasks")
	var skipped int
	for _, run := range allRuns(t, r.ID) {
		if run.SkipReason == SkipSelfTrigger {
			skipped++
		}
	}
	assert.Equal(t, 1, skipped, "the created task fired the rule once and the run was skipped")
}

func TestDueSoon_TasksCreatedByTheRuleDoNotFireItAgain(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskDueSoon, map[string]any{"offset": "24h"}, nil, createSoonDue("followup")))
	task := newTask(t, projectOwned, "soon", user1)
	updateTask(t, user1, task.ID, dueIn(time.Hour), "due_date")

	runTicks(t, advance, 30, 20*time.Second)
	assert.EqualValues(t, 2, countProjectTasks(t, projectOwned))
}

// Two rules that create tasks for each other are stopped by the depth limit.
func TestOverdue_ChainsOfCreatedTasksAcrossRulesAreBoundedByTheDepth(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, createSoonDue("from a")))
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, createSoonDue("from b")))
	task := newTask(t, projectOwned, "late", user1)
	updateTask(t, user1, task.ID, dueIn(time.Hour), "due_date")

	runTicks(t, advance, 40, 2*time.Minute)
	settled := countProjectTasks(t, projectOwned)
	runTicks(t, advance, 20, 2*time.Minute)
	assert.Equal(t, settled, countProjectTasks(t, projectOwned), "the chain ended")
	assert.Less(t, settled, int64(30))
}

func TestSchedule_TasksCreatedByTheRuleAreNotItsTargetsAgain(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "* * * * *"}, nil, act(spec.ActionCreateTask, map[string]any{"title": "copy"})))
	newTask(t, projectOwned, "seed", user1)

	runTicks(t, advance, 10, time.Minute)
	assert.EqualValues(t, 11, countProjectTasks(t, projectOwned), "the seed task is the only target of ten firings; the copies are not targets, or the number would double every minute")
}
