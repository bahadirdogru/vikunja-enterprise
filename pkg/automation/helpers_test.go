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
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// Fixture topology: user1 owns project 1 (custom fields 1-4: text, select with options 1-2, number, multi_select;
// tasks 1-12 ...) and administers project 11, which has a workflow (statuses 1-6, task 20 in status 1).
var (
	user1 = &user.User{ID: 1}
	user2 = &user.User{ID: 2}
	user6 = &user.User{ID: 6}
)

const (
	projectOwned    = int64(1)
	projectWorkflow = int64(11)
)

// reset gives the test a clean database, default limits and no cached subscriptions.
func reset(t *testing.T) {
	t.Helper()
	db.LoadAndAssertFixtures(t)
	models.ResetAutomationCache()
	config.AutomationEnabled.Set(true)
	config.AutomationMaxDepth.Set(3)
	config.AutomationMaxActions.Set(25)
	config.AutomationStepTimeout.Set(10 * time.Second)
	config.AutomationRunTimeout.Set(60 * time.Second)
	config.AutomationRateLimit.Set(60)
	config.AutomationMaxAttempts.Set(5)
	config.AutomationMaxConsecutiveFails.Set(20)
	config.AutomationCacheTTL.Set(0)
	config.AutomationScriptsEnabled.Set(true)
	config.AutomationScriptsTimeout.Set(5 * time.Second)
	config.AutomationScriptsConditionTimeout.Set(time.Second)
	config.AutomationScriptsMaxSource.Set(64 * 1024)
	config.AutomationScriptsMaxAPICalls.Set(100)
	config.AutomationScriptsMaxHTTPRequests.Set(5)
	config.AutomationScriptsMaxResponseSize.Set(1024 * 1024)
	config.AutomationScriptsMaxLogSize.Set(16 * 1024)
	config.AutomationScriptsMaxCallStack.Set(512)
	config.AutomationScriptsMaxResults.Set(200)
	config.AutomationHTTPTimeout.Set(10 * time.Second)
	retryBase = time.Millisecond
	now = time.Now
}

// commit runs fn in a transaction like the CRUD handlers do: hook errors fail the commit, pending events are
// dispatched afterwards.
func commit(t *testing.T, fn func(s *xorm.Session) error) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	if err := fn(s); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	if err := events.PendingHookError(s); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	if err := s.Commit(); err != nil {
		events.CleanupPending(s)
		return err
	}
	events.DispatchPending(context.Background(), s)
	return nil
}

func mustCommit(t *testing.T, fn func(s *xorm.Session) error) {
	t.Helper()
	require.NoError(t, commit(t, fn))
}

func rule(trigger string, options map[string]any, cond *spec.Condition, actions ...*spec.Action) *models.AutomationRule {
	return &models.AutomationRule{
		Title:      "rule",
		Enabled:    true,
		Trigger:    spec.Trigger{Type: trigger, Options: options},
		Conditions: cond,
		Actions:    actions,
	}
}

func act(typ string, params map[string]any) *spec.Action {
	return &spec.Action{Type: typ, Params: params}
}

// addRule creates a rule through the model, as the API does.
func addRule(t *testing.T, projectID int64, owner *user.User, r *models.AutomationRule) *models.AutomationRule {
	t.Helper()
	r.ProjectID = projectID
	mustCommit(t, func(s *xorm.Session) error {
		can, err := r.CanCreate(s, owner)
		require.NoError(t, err)
		require.True(t, can)
		return r.Create(s, owner)
	})
	return r
}

func newTask(t *testing.T, projectID int64, title string, doer *user.User) *models.Task {
	t.Helper()
	task := &models.Task{Title: title, ProjectID: projectID}
	mustCommit(t, func(s *xorm.Session) error { return task.Create(s, doer) })
	return task
}

func updateTask(t *testing.T, doer *user.User, id int64, mutate func(task *models.Task), fields ...string) {
	t.Helper()
	mustCommit(t, func(s *xorm.Session) error {
		task := &models.Task{ID: id}
		if len(fields) == 0 {
			stored, err := models.GetTaskByIDSimple(s, id)
			require.NoError(t, err)
			task = &stored
		}
		mutate(task)
		if len(fields) == 0 {
			return task.Update(s, doer)
		}
		return task.UpdateFields(s, doer, fields)
	})
}

func drain(t *testing.T) int {
	t.Helper()
	n, err := ProcessPending(context.Background())
	require.NoError(t, err)
	return n
}

func getTask(t *testing.T, id int64) *models.Task {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	task, err := models.GetTaskByIDSimple(s, id)
	require.NoError(t, err)
	return &task
}

func allRuns(t *testing.T, ruleID int64) []*models.AutomationRun {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	runs := []*models.AutomationRun{}
	require.NoError(t, s.Where("rule_id = ?", ruleID).OrderBy("id ASC").Find(&runs))
	return runs
}

func runSteps(t *testing.T, runID int64) []*models.AutomationRunStep {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	steps := []*models.AutomationRunStep{}
	require.NoError(t, s.Where("run_id = ?", runID).OrderBy("position ASC").Find(&steps))
	return steps
}

func allEvents(t *testing.T) []*models.AutomationEvent {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	evs := []*models.AutomationEvent{}
	require.NoError(t, s.OrderBy("id ASC").Find(&evs))
	return evs
}

func getRule(t *testing.T, id int64) *models.AutomationRule {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	r, err := models.GetAutomationRule(s, id)
	require.NoError(t, err)
	return r
}

// fakeClock freezes the engine clock; the returned function moves it forward.
func fakeClock(t *testing.T) (advance func(time.Duration)) {
	t.Helper()
	clock := time.Now()
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })
	return func(d time.Duration) { clock = clock.Add(d) }
}

func newSession() *xorm.Session { return db.NewSession() }

func mustCommitAssign(t *testing.T, taskID int64) {
	t.Helper()
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.TaskAssginee{TaskID: taskID, UserID: user1.ID}).Create(s, user1)
	})
}
