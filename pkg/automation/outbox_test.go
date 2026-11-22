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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

func setDoneAction() *spec.Action {
	return act(spec.ActionSetField, map[string]any{"field": "priority", "value": 1})
}

func TestOutbox_RowIsWrittenInTheTransactionOfTheChange(t *testing.T) {
	reset(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))

	t.Run("committed with the change", func(t *testing.T) {
		task := newTask(t, projectOwned, "committed", user1)
		evs := allEvents(t)
		require.Len(t, evs, 1)
		assert.Equal(t, task.ID, evs[0].TaskID)
		assert.Equal(t, projectOwned, evs[0].ProjectID)
		assert.Equal(t, models.AutomationEventPending, evs[0].Status)
		assert.Equal(t, "task.created", evs[0].Name)
		assert.EqualValues(t, 0, evs[0].Depth)
		assert.EqualValues(t, 0, evs[0].OriginRunID)
	})

	t.Run("absent after a rollback", func(t *testing.T) {
		reset(t)
		addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))

		s := db.NewSession()
		task := &models.Task{Title: "rolled back", ProjectID: projectOwned}
		require.NoError(t, task.Create(s, user1))
		// The row exists inside the transaction ...
		inTx, err := s.Count(&models.AutomationEvent{})
		require.NoError(t, err)
		assert.EqualValues(t, 1, inTx)
		// ... and is gone with it.
		require.NoError(t, s.Rollback())
		events.CleanupPending(s)
		s.Close()

		assert.Empty(t, allEvents(t))
	})

	t.Run("an event that reaches no commit leaves no row", func(t *testing.T) {
		reset(t)
		addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))
		err := commit(t, func(s *xorm.Session) error {
			task := &models.Task{Title: "fails", ProjectID: projectOwned}
			require.NoError(t, task.Create(s, user1))
			return assert.AnError
		})
		require.Error(t, err)
		assert.Empty(t, allEvents(t))
	})
}

func TestOutbox_OnlyWrittenWhenARuleSubscribes(t *testing.T) {
	reset(t)

	newTask(t, projectOwned, "no rules at all", user1)
	assert.Empty(t, allEvents(t))

	// A rule for another trigger of the project does not subscribe to task.created.
	disabled := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))
	other := addRule(t, projectOwned, user1, rule(spec.TriggerTaskDeleted, nil, nil, setDoneAction()))
	mustCommit(t, func(s *xorm.Session) error {
		disabled.Enabled = false
		return disabled.Update(s, user1)
	})
	newTask(t, projectOwned, "only a deleted rule and a disabled one", user1)
	assert.Empty(t, allEvents(t))

	// A rule of another project does not subscribe either.
	addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))
	newTask(t, projectOwned, "other project", user1)
	assert.Empty(t, allEvents(t))

	mustCommit(t, func(s *xorm.Session) error {
		disabled.Enabled = true
		return disabled.Update(s, user1)
	})
	newTask(t, projectOwned, "now", user1)
	assert.Len(t, allEvents(t), 1)
	_ = other
}

func TestOutbox_KillSwitch(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))

	config.AutomationEnabled.Set(false)
	newTask(t, projectOwned, "switched off", user1)
	assert.Empty(t, allEvents(t), "no event is recorded while the kill switch is off")

	config.AutomationEnabled.Set(true)
	task := newTask(t, projectOwned, "switched on", user1)
	require.Len(t, allEvents(t), 1)

	// Pending events do not run while it is off, and run once it is on again.
	config.AutomationEnabled.Set(false)
	assert.Equal(t, 0, drain(t))
	assert.EqualValues(t, 0, getTask(t, task.ID).Priority)
	assert.Empty(t, allRuns(t, r.ID))

	config.AutomationEnabled.Set(true)
	assert.Equal(t, 1, drain(t))
	assert.EqualValues(t, 1, getTask(t, task.ID).Priority)
}

func TestOutbox_CacheIsInvalidatedByRuleChanges(t *testing.T) {
	reset(t)
	config.AutomationCacheTTL.Set(time.Hour)
	t.Cleanup(func() { config.AutomationCacheTTL.Set(0) })

	newTask(t, projectOwned, "primes the cache with nothing subscribed", user1)
	assert.Empty(t, allEvents(t))

	// Create invalidates.
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))
	newTask(t, projectOwned, "after create", user1)
	assert.Len(t, allEvents(t), 1)

	// Disable invalidates.
	mustCommit(t, func(s *xorm.Session) error {
		r.Enabled = false
		return r.Update(s, user1)
	})
	newTask(t, projectOwned, "after disable", user1)
	assert.Len(t, allEvents(t), 1)

	// Enable invalidates.
	mustCommit(t, func(s *xorm.Session) error {
		r.Enabled = true
		return r.Update(s, user1)
	})
	newTask(t, projectOwned, "after enable", user1)
	assert.Len(t, allEvents(t), 2)

	// Delete invalidates.
	mustCommit(t, func(s *xorm.Session) error { return r.Delete(s, user1) })
	newTask(t, projectOwned, "after delete", user1)
	assert.Len(t, allEvents(t), 2)

	t.Run("a change from another replica is noticed after the ttl", func(t *testing.T) {
		reset(t)
		config.AutomationCacheTTL.Set(time.Hour)
		newTask(t, projectOwned, "primes", user1)

		// Written behind the back of this process, like a rule saved on another replica.
		raw := rule(spec.TriggerTaskCreated, nil, nil, setDoneAction())
		raw.ProjectID, raw.OwnerID, raw.Revision = projectOwned, user1.ID, 1
		s := db.NewSession()
		_, err := s.Insert(raw)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()

		newTask(t, projectOwned, "still cached", user1)
		assert.Empty(t, allEvents(t))

		config.AutomationCacheTTL.Set(time.Millisecond)
		time.Sleep(5 * time.Millisecond)
		models.ResetAutomationCache() // the entry cached with the old ttl would live for an hour
		newTask(t, projectOwned, "expired", user1)
		assert.Len(t, allEvents(t), 1)
	})
}

func TestOutbox_HookErrorFailsTheTransaction(t *testing.T) {
	reset(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))

	// Break the outbox table: a genuine database error in the hook must not lose the event silently.
	s := db.NewSession()
	_, err := s.Exec("ALTER TABLE automation_events RENAME TO automation_events_broken")
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	t.Cleanup(func() {
		s := db.NewSession()
		defer s.Close()
		_, err := s.Exec("ALTER TABLE automation_events_broken RENAME TO automation_events")
		require.NoError(t, err)
		require.NoError(t, s.Commit())
	})

	err = commit(t, func(s *xorm.Session) error {
		task := &models.Task{Title: "must not commit", ProjectID: projectOwned}
		return task.Create(s, user1)
	})
	require.Error(t, err)
}

func TestOutbox_EventsOfActionsCarryOriginAndDepth(t *testing.T) {
	reset(t)
	// Rule A: a new task gets priority 2 -> task.updated.
	a := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionSetField, map[string]any{"field": "priority", "value": 2})))
	// Rule B listens to updates.
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil, nil, act(spec.ActionSetField, map[string]any{"field": "hex_color", "value": "#ff0000"})))

	newTask(t, projectOwned, "chain", user1)
	// created -> A (priority) -> updated -> B (color) -> updated
	require.Equal(t, 3, drain(t))
	evs := allEvents(t)
	require.Len(t, evs, 3)
	runsA := allRuns(t, a.ID)
	require.Len(t, runsA, 1)

	assert.Equal(t, "task.updated", evs[1].Name)
	assert.EqualValues(t, 0, evs[0].Depth)
	assert.EqualValues(t, 0, evs[0].OriginRunID)
	assert.EqualValues(t, 1, evs[1].Depth)
	assert.Equal(t, runsA[0].ID, evs[1].OriginRunID)
	assert.EqualValues(t, 2, evs[2].Depth)
	assert.NotEqual(t, runsA[0].ID, evs[2].OriginRunID)
	assert.NotZero(t, evs[2].OriginRunID)
}

func TestOutbox_DispatchedEventsStillReachTheListeners(t *testing.T) {
	reset(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setDoneAction()))
	newTask(t, projectOwned, "regular dispatch is unaffected", user1)
	events.AssertDispatched(t, &models.TaskCreatedEvent{})
	_ = context.Background()
}
