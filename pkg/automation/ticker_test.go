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
	"xorm.io/xorm"
)

func tick(t *testing.T) int {
	t.Helper()
	n, err := Tick(context.Background())
	require.NoError(t, err)
	return n
}

// tickConcurrently runs the ticker in several goroutines at once, like several replicas would.
func tickConcurrently(t *testing.T, n int) int {
	t.Helper()
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queued, err := Tick(context.Background())
			assert.NoError(t, err)
			mu.Lock()
			total += queued
			mu.Unlock()
		}()
	}
	wg.Wait()
	return total
}

func eventsNamed(t *testing.T, name string) []*models.AutomationEvent {
	t.Helper()
	out := []*models.AutomationEvent{}
	for _, ev := range allEvents(t) {
		if ev.Name == name {
			out = append(out, ev)
		}
	}
	return out
}

// onlyTasksOf removes the fixture tasks of a project so a test controls the tasks a schedule sees.
func clearTasks(t *testing.T, projectID int64) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	_, err := s.Exec("DELETE FROM tasks WHERE project_id = ?", projectID)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
}

func commentOf(key string) *spec.Action {
	return act(spec.ActionSetField, map[string]any{"field": "title", "value": key + " {{task.id}}"})
}

// setRuleUpdated moves the "saved at" time of a rule, which the schedule counts from, onto the fake clock.
func setRuleUpdated(t *testing.T, ruleID int64, at time.Time) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	_, err := s.ID(ruleID).Cols("updated").NoAutoTime().Update(&models.AutomationRule{Updated: at})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
}

// ---- schedule ----

func TestSchedule_FiresOncePerOccurrenceForTheMatchingTasks(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	// The assertions below depend on where the clock sits between two occurrences: put it 30s after a :00/:05 mark.
	start := now().Truncate(5 * time.Minute).Add(5*time.Minute + 30*time.Second)
	advance(start.Sub(now()))
	cond := &spec.Condition{Field: "title", Op: spec.OpLike, Value: "sched%"}
	r := addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "*/5 * * * *"}, cond, setPriority(2)))
	setRuleUpdated(t, r.ID, now())
	a := newTask(t, projectOwned, "sched a", user1)
	b := newTask(t, projectOwned, "sched b", user1)
	other := newTask(t, projectOwned, "other", user1)

	assert.Zero(t, tick(t), "nothing is due right after saving: no replay of the past")
	advance(6 * time.Minute)
	assert.Equal(t, 2, tickConcurrently(t, 4), "several replicas, one firing")
	assert.Len(t, eventsNamed(t, spec.EventSchedule), 2)
	assert.Zero(t, tick(t), "claimed")

	drain(t)
	assert.EqualValues(t, 2, getTask(t, a.ID).Priority)
	assert.EqualValues(t, 2, getTask(t, b.ID).Priority)
	assert.EqualValues(t, 0, getTask(t, other.ID).Priority)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 2)
	for _, run := range runs {
		assert.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	}
	stored := getRule(t, r.ID)
	assert.False(t, stored.LastFiredAt.IsZero())
	assert.Contains(t, stored.LastFireNote, "2 task(s) matched")

	advance(2 * time.Minute)
	assert.Zero(t, tick(t), "not due again yet")
	advance(5 * time.Minute)
	assert.Equal(t, 2, tick(t))
}

func TestSchedule_MissedOccurrencesFireOnce(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "* * * * *"}, nil, setPriority(1)))
	newTask(t, projectOwned, "t", user1)
	advance(3 * time.Hour)
	assert.Equal(t, 1, tick(t), "the server was away for 180 occurrences: one catch-up firing")
	assert.Zero(t, tick(t))
}

func TestSchedule_UsesTheServiceTimeZone(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	config.ServiceTimeZone.Set("Europe/Istanbul")
	defer config.ServiceTimeZone.Set("GMT")
	clock := fakeClock(t)
	_ = clock
	addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "0 9 * * *"}, nil, setPriority(1)))
	newTask(t, projectOwned, "t", user1)

	sched, err := spec.CronParser.Parse("0 9 * * *")
	require.NoError(t, err)
	target := sched.Next(time.Now().In(config.GetTimeZone()))
	require.Equal(t, 9, target.Hour())
	base := time.Now()
	now = func() time.Time { return target.Add(-30 * time.Second) }
	assert.Zero(t, tick(t), "09:00 in Istanbul has not come yet at 08:59:30")
	now = func() time.Time { return target.Add(30 * time.Second) }
	assert.Equal(t, 1, tick(t))
	assert.True(t, target.After(base))
}

func TestSchedule_CapsTasksPerTickAndRecordsIt(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	config.AutomationTickMaxTasks.Set(2)
	defer config.AutomationTickMaxTasks.Set(100)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "*/5 * * * *"}, nil, setPriority(1)))
	for i := 0; i < 5; i++ {
		newTask(t, projectOwned, "t", user1)
	}
	advance(6 * time.Minute)
	assert.Equal(t, 2, tick(t))
	assert.Contains(t, getRule(t, r.ID).LastFireNote, "capped")
}

func TestSchedule_ProjectScopeFiresOneRunWithoutATask(t *testing.T) {
	reset(t)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "*/5 * * * *", "scope": spec.ScopeProject}, nil,
		act(spec.ActionCreateTask, map[string]any{"title": "Weekly check"}), setPriority(2)))
	advance(6 * time.Minute)
	assert.Equal(t, 1, tick(t))
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	require.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	task := getTask(t, runs[0].TaskID)
	assert.Equal(t, "Weekly check", task.Title)
	assert.EqualValues(t, 2, task.Priority)
}

func TestSchedule_ValidationAndDisabledRules(t *testing.T) {
	reset(t)
	validateSchedules(t)
	advance := fakeClock(t)
	clearTasks(t, projectOwned)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "*/5 * * * *"}, nil, setPriority(1)))
	r.Enabled = false
	mustCommit(t, func(s *xorm.Session) error { return r.Update(s, user1) })
	newTask(t, projectOwned, "t", user1)
	advance(10 * time.Minute)
	assert.Zero(t, tick(t))
}

func validateSchedules(t *testing.T) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	defer func() { _ = s.Rollback() }()
	for _, expr := range []string{"", "* * *", "61 * * * *", "every day", "@every 1s", "@EVERY 1h", "CRON_TZ=Asia/Tokyo 0 9 * * *", "TZ=UTC 0 9 * * *"} {
		bad := rule(spec.TriggerSchedule, map[string]any{"cron": expr}, nil, setPriority(1))
		bad.ProjectID = projectOwned
		assert.True(t, models.IsErrAutomationRuleInvalid(bad.Create(s, user1)), "%q", expr)
	}
	for _, expr := range []string{"@hourly", "@daily", "@weekly", "@monthly", "@yearly", "@annually", "*/5 * * * *"} {
		good := rule(spec.TriggerSchedule, map[string]any{"cron": expr}, nil, setPriority(1))
		good.ProjectID = projectOwned
		require.NoError(t, good.Create(s, user1), expr)
	}

	// Project scope needs a create task action before anything that works on a task.
	scoped := rule(spec.TriggerSchedule, map[string]any{"cron": "@daily", "scope": spec.ScopeProject}, nil, setPriority(1))
	scoped.ProjectID = projectOwned
	assert.True(t, models.IsErrAutomationRuleInvalid(scoped.Create(s, user1)))
}

// ---- overdue and due soon ----

func dueIn(d time.Duration) func(*models.Task) {
	return func(tk *models.Task) { tk.DueDate = now().Add(d).Truncate(time.Second) }
}

func TestOverdue_FiresOncePerDueDateAndRearmsWhenItChanges(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, setPriority(5)))
	task := newTask(t, projectOwned, "late", user1)
	updateTask(t, user1, task.ID, dueIn(time.Hour), "due_date")
	done := newTask(t, projectOwned, "done", user1)
	updateTask(t, user1, done.ID, func(tk *models.Task) { tk.DueDate = now().Add(time.Hour).Truncate(time.Second); tk.Done = true }, "due_date", "done")
	undated := newTask(t, projectOwned, "undated", user1)

	assert.Zero(t, tick(t), "not overdue yet")
	advance(2 * time.Hour)
	assert.Equal(t, 1, tickConcurrently(t, 4), "several replicas, one event per task and due date")
	assert.Zero(t, tick(t))
	drain(t)
	assert.EqualValues(t, 5, getTask(t, task.ID).Priority)
	assert.EqualValues(t, 0, getTask(t, done.ID).Priority, "done tasks are never overdue")
	assert.EqualValues(t, 0, getTask(t, undated.ID).Priority)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)

	// A new due date arms the rule again; the same date does not.
	updateTask(t, user1, task.ID, func(tk *models.Task) { tk.Priority = 0 }, "priority")
	advance(time.Hour)
	assert.Zero(t, tick(t))
	updateTask(t, user1, task.ID, dueIn(30*time.Minute), "due_date")
	assert.Zero(t, tick(t), "the new date is in the future")
	advance(time.Hour)
	assert.Equal(t, 1, tick(t))
	drain(t)
	assert.EqualValues(t, 5, getTask(t, task.ID).Priority)
	assert.Len(t, allRuns(t, r.ID), 2)
}

func TestOverdue_IgnoresTasksThatWereOverdueBeforeTheRuleWasSaved(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	fakeClock(t)
	old := newTask(t, projectOwned, "old", user1)
	updateTask(t, user1, old.ID, func(tk *models.Task) { tk.DueDate = time.Now().Add(-48 * time.Hour).Truncate(time.Second) }, "due_date")
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, setPriority(5)))
	assert.Zero(t, tick(t))
}

func TestOverdue_ConditionsAreAppliedAndNonMatchingTasksAreMarked(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	cond := &spec.Condition{Field: "priority", Op: spec.OpGe, Value: float64(3)}
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, cond, commentOf("x")))
	hi := newTask(t, projectOwned, "hi", user1)
	lo := newTask(t, projectOwned, "lo", user1)
	updateTask(t, user1, hi.ID, func(tk *models.Task) { tk.DueDate = now().Add(time.Hour).Truncate(time.Second); tk.Priority = 4 }, "due_date", "priority")
	updateTask(t, user1, lo.ID, dueIn(time.Hour), "due_date")
	advance(2 * time.Hour)
	assert.Equal(t, 1, tick(t))
	// The task that did not match is not looked at again for this due date, even when it matches later.
	updateTask(t, user1, lo.ID, func(tk *models.Task) { tk.Priority = 5 }, "priority")
	assert.Zero(t, tick(t))
	drain(t)
	assert.Len(t, allRuns(t, r.ID), 1)
}

func TestOverdue_CapsPerTickAndPicksUpTheRestOnTheNextOne(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	config.AutomationTickMaxTasks.Set(2)
	defer config.AutomationTickMaxTasks.Set(100)
	advance := fakeClock(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, setPriority(5)))
	for i := 0; i < 5; i++ {
		tk := newTask(t, projectOwned, "t", user1)
		updateTask(t, user1, tk.ID, dueIn(time.Hour), "due_date")
	}
	advance(2 * time.Hour)
	assert.Equal(t, 2, tick(t))
	assert.Equal(t, 2, tick(t))
	assert.Equal(t, 1, tick(t))
	assert.Zero(t, tick(t))
	drain(t)
}

func TestDueSoon_FiresWithinTheOffsetOnce(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskDueSoon, map[string]any{"offset": "24h"}, nil, setPriority(3)))
	soon := newTask(t, projectOwned, "soon", user1)
	later := newTask(t, projectOwned, "later", user1)
	updateTask(t, user1, soon.ID, dueIn(10*time.Hour), "due_date")
	updateTask(t, user1, later.ID, dueIn(72*time.Hour), "due_date")

	assert.Equal(t, 1, tickConcurrently(t, 3))
	assert.Zero(t, tick(t))
	drain(t)
	assert.EqualValues(t, 3, getTask(t, soon.ID).Priority)
	assert.EqualValues(t, 0, getTask(t, later.ID).Priority)

	advance(49 * time.Hour)
	assert.Equal(t, 1, tick(t), "now the later task is within 24h")
	// Past its due date a task is overdue, not due soon.
	advance(30 * time.Hour)
	assert.Zero(t, tick(t))
	drain(t)
	assert.Len(t, allRuns(t, r.ID), 2)
}

func TestDueSoon_Validation(t *testing.T) {
	reset(t)
	s := db.NewSession()
	defer s.Close()
	for _, off := range []string{"", "soon", "0m"} {
		bad := rule(spec.TriggerTaskDueSoon, map[string]any{"offset": off}, nil, setPriority(1))
		bad.ProjectID = projectOwned
		assert.True(t, models.IsErrAutomationRuleInvalid(bad.Create(s, user1)), "%q", off)
	}
}

func TestDueMarkersAreRemovedWithTheRuleAndTheirTasks(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, setPriority(1)))
	tk := newTask(t, projectOwned, "t", user1)
	updateTask(t, user1, tk.ID, dueIn(time.Hour), "due_date")
	advance(2 * time.Hour)
	require.Equal(t, 1, tick(t))
	count := func() int64 {
		s := db.NewSession()
		defer s.Close()
		n, err := s.Count(&models.AutomationDueMarker{})
		require.NoError(t, err)
		return n
	}
	require.EqualValues(t, 1, count())
	mustCommit(t, func(s *xorm.Session) error { return r.Delete(s, user1) })
	assert.EqualValues(t, 0, count())
}

// ---- owner access and time zones ----

func TestTicker_OwnerWhoLostAdminDoesNotFireAndTheRuleIsDisabled(t *testing.T) {
	reset(t)
	clearTasks(t, projectWorkflow)
	advance := fakeClock(t)
	// user1 administers project 11 through a share; user6 owns it.
	sched := addRule(t, projectWorkflow, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "*/5 * * * *"}, nil, setPriority(1)))
	due := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskOverdue, nil, nil, setPriority(1)))
	soon := newTask(t, projectWorkflow, "overdue soon", user6)
	updateTask(t, user6, soon.ID, dueIn(10*time.Minute), "due_date")
	newTask(t, projectWorkflow, "plain", user6)

	s := db.NewSession()
	_, err := s.Where("project_id = ? AND user_id = ?", projectWorkflow, user1.ID).Delete(&models.ProjectUser{})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	advance(20 * time.Minute)
	assert.Zero(t, tick(t))
	assert.Empty(t, eventsNamed(t, spec.EventSchedule))
	assert.Empty(t, eventsNamed(t, spec.EventOverdue))
	for _, id := range []int64{sched.ID, due.ID} {
		stored := getRule(t, id)
		assert.False(t, stored.Enabled)
		assert.Equal(t, models.AutomationDisabledOwnerNotAdmin, stored.DisabledReason)
	}
}

func TestTicker_IdleTickWritesNothing(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "0 0 1 1 *"}, nil, setPriority(1)))
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskDueSoon, map[string]any{"offset": "1h"}, nil, setPriority(1)))
	before := len(allEvents(t))
	for i := 0; i < 3; i++ {
		assert.Zero(t, tick(t))
	}
	assert.Len(t, allEvents(t), before)
	assert.True(t, getRule(t, r.ID).LastFiredAt.IsZero())
}

// The schedule is evaluated in the time zone of the service (service.timezone), wall clock times included: a daily
// 02:30 job does not exist on the night the clocks jump forward, and robfig/cron then runs it at the first
// following wall clock time that matches, which is the next day; on the night they fall back the hour happens
// twice and the job fires once.
func TestSchedule_DaylightSavingTime(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	config.ServiceTimeZone.Set("Europe/Berlin")
	defer config.ServiceTimeZone.Set("GMT")

	at := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, berlin) }
	run := func(t *testing.T, cronExpr string, from time.Time, steps []time.Time) (fired []bool) {
		reset(t)
		clearTasks(t, projectOwned)
		clock := from
		now = func() time.Time { return clock }
		t.Cleanup(func() { now = time.Now })
		r := addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": cronExpr, "scope": spec.ScopeProject}, nil,
			act(spec.ActionCreateTask, map[string]any{"title": "x"})))
		// A rule is only considered for time after it was saved.
		setRuleUpdated(t, r.ID, from)
		for _, step := range steps {
			clock = step
			fired = append(fired, tick(t) == 1)
		}
		return fired
	}

	t.Run("spring forward: 02:30 does not exist, the job skips that day", func(t *testing.T) {
		// 2027-03-28: 02:00 CET becomes 03:00 CEST.
		got := run(t, "30 2 * * *", at(2027, 3, 27, 12, 0), []time.Time{
			at(2027, 3, 28, 1, 59), // before the gap
			at(2027, 3, 28, 3, 30), // after the gap: 02:30 never happened
			at(2027, 3, 29, 2, 31), // the next regular occurrence
		})
		assert.Equal(t, []bool{false, false, true}, got)
	})

	t.Run("spring forward: an hourly job loses the hour that does not exist", func(t *testing.T) {
		got := run(t, "0 * * * *", at(2027, 3, 28, 0, 30), []time.Time{
			at(2027, 3, 28, 1, 30), // 01:00 CET
			at(2027, 3, 28, 3, 30), // 03:00 CEST, 02:00 never existed
			at(2027, 3, 28, 3, 40), // nothing new
			at(2027, 3, 28, 4, 5),
		})
		assert.Equal(t, []bool{true, true, false, true}, got)
	})

	t.Run("fall back: 02:30 happens twice and fires once", func(t *testing.T) {
		// 2027-10-31: 03:00 CEST becomes 02:00 CET.
		first := time.Date(2027, 10, 31, 0, 31, 0, 0, time.UTC)  // 02:31 CEST, the first 02:30
		second := time.Date(2027, 10, 31, 1, 31, 0, 0, time.UTC) // 02:31 CET, the second 02:30
		got := run(t, "30 2 * * *", at(2027, 10, 30, 12, 0), []time.Time{first, second, at(2027, 11, 1, 2, 31)})
		assert.Equal(t, []bool{true, false, true}, got)
	})
}

// A schedule with an occurrence every minute and a gap of a year (more occurrences than the search walks through)
// fires once, and not again on the ticks that follow.
func TestSchedule_AGapOfAYearFiresOnceNotOnEveryTick(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	// Sit 20s into a minute, whatever the real time is.
	advance(now().Truncate(time.Minute).Add(time.Minute + 20*time.Second).Sub(now()))
	addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "* * * * *"}, nil, setPriority(1)))
	newTask(t, projectOwned, "t", user1)

	advance(365 * 24 * time.Hour)
	assert.Equal(t, 1, tick(t))
	assert.Zero(t, tick(t), "the same minute again")
	advance(10 * time.Second)
	assert.Zero(t, tick(t))
	advance(time.Minute)
	assert.Equal(t, 1, tick(t), "the next occurrence")
	assert.Zero(t, tick(t))
}

func TestLatestOccurrence_FindsTheLatestForDenseAndSparseSchedules(t *testing.T) {
	loc := time.UTC
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, loc)
	at := time.Date(2026, 3, 4, 5, 6, 30, 0, loc)
	for expr, want := range map[string]time.Time{
		"* * * * *":      time.Date(2026, 3, 4, 5, 6, 0, 0, loc),
		"*/7 * * * *":    time.Date(2026, 3, 4, 5, 0, 0, 0, loc),
		"0 9-17 * * 1-5": time.Date(2026, 3, 3, 17, 0, 0, 0, loc),
		"@yearly":        time.Date(2026, 1, 1, 0, 0, 0, 0, loc),
	} {
		sched, err := spec.CronParser.Parse(expr)
		require.NoError(t, err)
		assert.Equal(t, want, latestOccurrence(sched, base, at), expr)
	}
}

func TestDueSoon_DoesNotFireForTasksAlreadyInTheWindowWhenTheRuleStarts(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	existing := newTask(t, projectOwned, "already", user1)
	updateTask(t, user1, existing.ID, dueIn(10*time.Hour), "due_date")
	farAway := newTask(t, projectOwned, "far", user1)
	updateTask(t, user1, farAway.ID, dueIn(72*time.Hour), "due_date")
	ageTasks(t, projectOwned)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskDueSoon, map[string]any{"offset": "24h"}, nil, setPriority(3)))

	assert.Zero(t, tick(t), "the task that was in the window before the rule existed is not new")
	fresh := newTask(t, projectOwned, "fresh", user1)
	updateTask(t, user1, fresh.ID, dueIn(5*time.Hour), "due_date")
	assert.Equal(t, 1, tick(t), "a task created after the rule is in the window")

	advance(49 * time.Hour)
	assert.Equal(t, 1, tick(t), "the far task entered the window since")
	drain(t)
	assert.EqualValues(t, 0, getTask(t, existing.ID).Priority)
	assert.EqualValues(t, 3, getTask(t, fresh.ID).Priority)
	assert.EqualValues(t, 3, getTask(t, farAway.ID).Priority)
	assert.Len(t, allRuns(t, r.ID), 2)
}

// The line is when the rule was last enabled or changed its trigger, not when it was last saved.
func TestRuleArming_OnlyEnablingAndTriggerChangesMoveIt(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, setPriority(1)))
	long := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	reseed := func() {
		s := db.NewAutocommitSession()
		defer s.Close()
		_, err := s.ID(r.ID).Cols("armed_at").NoAutoTime().Update(&models.AutomationRule{ArmedAt: long})
		require.NoError(t, err)
		models.ResetAutomationCache()
	}
	armed := func() time.Time { return getRule(t, r.ID).ArmedAt }
	save := func(change func(*models.AutomationRule)) {
		stored := getRule(t, r.ID)
		change(stored)
		mustCommit(t, func(s *xorm.Session) error { return stored.Update(s, user1) })
	}
	assert.WithinDuration(t, time.Now(), armed(), time.Minute, "armed when created")

	reseed()
	save(func(x *models.AutomationRule) { x.Title = "new title"; x.Actions = []*spec.Action{setPriority(2)} })
	assert.True(t, armed().Equal(long), "editing title and actions keeps the line")

	save(func(x *models.AutomationRule) { x.Enabled = false })
	assert.True(t, armed().Equal(long), "disabling keeps the line")
	save(func(x *models.AutomationRule) { x.Enabled = true })
	assert.WithinDuration(t, time.Now(), armed(), time.Minute, "enabling moves it")

	reseed()
	save(func(x *models.AutomationRule) {
		x.Trigger = spec.Trigger{Type: spec.TriggerTaskDueSoon, Options: map[string]any{"offset": "2h"}}
	})
	assert.WithinDuration(t, time.Now(), armed(), time.Minute, "another trigger moves it")
	reseed()
	save(func(x *models.AutomationRule) { x.Trigger.Options = map[string]any{"offset": "3h"} })
	assert.WithinDuration(t, time.Now(), armed(), time.Minute, "other options move it")
}

// A task that became overdue while the server was down still fires after the rule was edited.
func TestOverdue_EditingTheRuleDoesNotDropTasksThatCrossedMeanwhile(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, setPriority(5)))
	task := newTask(t, projectOwned, "late", user1)
	updateTask(t, user1, task.ID, dueIn(time.Hour), "due_date")
	advance(2 * time.Hour)
	// The rule is saved after the task became overdue, with the ticker not having run in between.
	setRuleUpdated(t, r.ID, now())
	assert.Equal(t, 1, tick(t))
}

// ageTasks makes the tasks older than any rule created by a test, which is what separates "was there before" from
// "created after" at the resolution of the database.
func ageTasks(t *testing.T, projectID int64) {
	t.Helper()
	s := db.NewAutocommitSession()
	defer s.Close()
	_, err := s.Exec("UPDATE tasks SET created = ? WHERE project_id = ?", time.Now().Add(-72*time.Hour).UTC().Format("2006-01-02 15:04:05"), projectID)
	require.NoError(t, err)
}

// The marker seeded when the rule is armed keeps the tasks it found quiet, but a due date edited afterwards is a
// new date: it fires, even in the first offset after arming.
func TestDueSoon_ADueDateEditedIntoTheWindowAfterArmingFires(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	fakeClock(t)
	inWindow := newTask(t, projectOwned, "in window", user1)
	updateTask(t, user1, inWindow.ID, dueIn(10*time.Hour), "due_date")
	later := newTask(t, projectOwned, "later", user1)
	updateTask(t, user1, later.ID, dueIn(72*time.Hour), "due_date")
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskDueSoon, map[string]any{"offset": "24h"}, nil, setPriority(3)))

	assert.Zero(t, tick(t), "already in the window when the rule was armed")
	// Edited into the window during the first offset after arming.
	updateTask(t, user1, later.ID, dueIn(20*time.Hour), "due_date")
	assert.Equal(t, 1, tick(t))
	// The task that was in the window gets another date in it.
	updateTask(t, user1, inWindow.ID, dueIn(8*time.Hour), "due_date")
	assert.Equal(t, 1, tick(t))
	assert.Zero(t, tick(t))
	drain(t)
	assert.Len(t, allRuns(t, r.ID), 2)
}

func TestOverdue_ADueDateBackdatedAfterArmingFires(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	fakeClock(t)
	old := newTask(t, projectOwned, "old", user1)
	updateTask(t, user1, old.ID, func(tk *models.Task) { tk.DueDate = now().Add(-48 * time.Hour).Truncate(time.Second) }, "due_date")
	future := newTask(t, projectOwned, "future", user1)
	updateTask(t, user1, future.ID, dueIn(5*24*time.Hour), "due_date")
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskOverdue, nil, nil, setPriority(5)))

	assert.Zero(t, tick(t), "the task that was overdue when the rule was armed is quiet")
	updateTask(t, user1, future.ID, func(tk *models.Task) { tk.DueDate = now().Add(-24 * time.Hour).Truncate(time.Second) }, "due_date")
	assert.Equal(t, 1, tick(t), "a due date moved into the past after arming is a new overdue task")
	assert.Zero(t, tick(t))

	// Disabling and enabling arms again: what is overdue by then is not replayed.
	updateTask(t, user1, old.ID, func(tk *models.Task) { tk.DueDate = now().Add(-12 * time.Hour).Truncate(time.Second) }, "due_date")
	stored := getRule(t, r.ID)
	stored.Enabled = false
	mustCommit(t, func(s *xorm.Session) error { return stored.Update(s, user1) })
	stored = getRule(t, r.ID)
	stored.Enabled = true
	mustCommit(t, func(s *xorm.Session) error { return stored.Update(s, user1) })
	assert.Zero(t, tick(t))
}

// A schedule with gaps (the first ten days of a month) and a long outage: the walk hits its cap inside the last block
// of occurrences, and what it found first is not the latest one.
func TestSchedule_ACappedWalkStillFindsTheLatestOccurrence(t *testing.T) {
	loc := time.UTC
	sched, err := spec.CronParser.Parse("* * 1-10 * *")
	require.NoError(t, err)
	base := time.Date(2026, 1, 11, 0, 0, 0, 0, loc)
	at := time.Date(2026, 3, 14, 12, 0, 0, 0, loc)
	assert.Equal(t, time.Date(2026, 3, 10, 23, 59, 0, 0, loc), latestOccurrence(sched, base, at))
}

func TestSchedule_AGapedScheduleAfterAnOutageFiresOnce(t *testing.T) {
	reset(t)
	clearTasks(t, projectOwned)
	advance := fakeClock(t)
	loc := config.GetTimeZone()
	addRule(t, projectOwned, user1, rule(spec.TriggerSchedule, map[string]any{"cron": "* * 1-10 * *"}, nil, setPriority(1)))
	newTask(t, projectOwned, "t", user1)

	n := now().In(loc)
	target := time.Date(n.Year(), n.Month()+3, 14, 12, 0, 0, 0, loc)
	advance(target.Sub(n))
	assert.Equal(t, 1, tick(t))
	assert.Zero(t, tick(t))
	advance(time.Hour)
	assert.Zero(t, tick(t))
	advance(24 * time.Hour)
	assert.Zero(t, tick(t))
}
