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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/notifications"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

func setPriority(n int) *spec.Action {
	return act(spec.ActionSetField, map[string]any{"field": "priority", "value": float64(n)})
}

func privateProject(t *testing.T) *models.Project {
	t.Helper()
	p := &models.Project{Title: "private of user2"}
	mustCommit(t, func(s *xorm.Session) error { return models.CreateProject(s, p, user2, false, false) })
	return p
}

func lastEventOf(t *testing.T, name string) *models.AutomationEvent {
	t.Helper()
	var last *models.AutomationEvent
	for _, ev := range allEvents(t) {
		if ev.Name == name {
			last = ev
		}
	}
	require.NotNil(t, last, "no %s event", name)
	return last
}

// ---- manual ----

func TestManual_RunsThroughTheOutboxAndShowsInTheHistory(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerManual, map[string]any{"label": "Raise"}, nil, setPriority(4)))
	task := newTask(t, projectOwned, "manual", user1)
	drain(t)
	require.Empty(t, allRuns(t, r.ID), "a manual rule does not react to task events")

	ev, err := RunManually(user1, projectOwned, r.ID, task.ID)
	require.NoError(t, err)
	assert.Equal(t, spec.EventManual, ev.Name)
	assert.Equal(t, r.ID, ev.RuleID)
	drain(t)

	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	assert.Equal(t, task.ID, runs[0].TaskID)
	assert.EqualValues(t, 4, getTask(t, task.ID).Priority)
}

func TestManual_ConditionsStillApplyAndOtherRulesDoNotSeeTheEvent(t *testing.T) {
	reset(t)
	cond := &spec.Condition{Field: "title", Op: spec.OpEq, Value: "yes"}
	r := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, cond, setPriority(2)))
	other := counting(t, projectOwned, spec.TriggerTaskCreated, nil, "created")
	task := newTask(t, projectOwned, "no", user1)
	drain(t)
	createdBefore := counter("created")

	_, err := RunManually(user1, projectOwned, r.ID, task.ID)
	require.NoError(t, err)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunSkipped, runs[0].Status)
	assert.Equal(t, SkipConditions, runs[0].SkipReason)
	assert.Equal(t, createdBefore, counter("created"))
	assert.Len(t, allRuns(t, other.ID), 1, "only the creation event ran the other rule")
}

func TestManual_Refusals(t *testing.T) {
	reset(t)
	disabled := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, nil, setPriority(1)))
	disabled.Enabled = false
	mustCommit(t, func(s *xorm.Session) error { return disabled.Update(s, user1) })
	wrongTrigger := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setPriority(1)))
	manual := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, nil, setPriority(1)))
	task := newTask(t, projectOwned, "t", user1)

	_, err := RunManually(user1, projectOwned, disabled.ID, task.ID)
	assert.True(t, models.IsErrAutomationRuleNotRunnable(err), "disabled: %v", err)
	_, err = RunManually(user1, projectOwned, wrongTrigger.ID, task.ID)
	assert.True(t, models.IsErrAutomationRuleNotRunnable(err), "other trigger: %v", err)
	_, err = RunManually(user1, projectOwned, manual.ID+1000, task.ID)
	assert.True(t, models.IsErrAutomationRuleDoesNotExist(err), "unknown rule: %v", err)

	// A task of another project is not found, and one the person cannot write is refused.
	_, err = RunManually(user1, projectWorkflow, manual.ID, task.ID)
	assert.True(t, models.IsErrAutomationRuleDoesNotExist(err), "rule of another project: %v", err)
	private := privateProject(t)
	privateRule := addRule(t, private.ID, user2, rule(spec.TriggerManual, nil, nil, setPriority(1)))
	privateTask := newTask(t, private.ID, "p", user2)
	_, err = RunManually(user1, private.ID, privateRule.ID, privateTask.ID)
	require.Error(t, err, "user1 has no access to the project")
	_, err = RunManually(user1, projectOwned, manual.ID, privateTask.ID)
	assert.True(t, models.IsErrTaskDoesNotExist(err), "task of another project: %v", err)
	_, err = RunManually(user2, private.ID, privateRule.ID, privateTask.ID)
	require.NoError(t, err)

	config.AutomationEnabled.Set(false)
	defer config.AutomationEnabled.Set(true)
	_, err = RunManually(user1, projectOwned, manual.ID, task.ID)
	assert.True(t, models.IsErrAutomationRuleNotRunnable(err), "kill switch: %v", err)
}

// Someone without write access to the project learns nothing about its rules and tasks, or whether they exist.
func TestManual_OutsidersGetOneAnswer(t *testing.T) {
	reset(t)
	private := privateProject(t)
	manual := addRule(t, private.ID, user2, rule(spec.TriggerManual, nil, nil, setPriority(1)))
	disabled := addRule(t, private.ID, user2, rule(spec.TriggerManual, nil, nil, setPriority(1)))
	disabled.Enabled = false
	mustCommit(t, func(s *xorm.Session) error { return disabled.Update(s, user2) })
	other := addRule(t, private.ID, user2, rule(spec.TriggerTaskCreated, nil, nil, setPriority(1)))
	task := newTask(t, private.ID, "p", user2)

	for name, call := range map[string]func() error{
		"rule and task":    func() error { _, err := RunManually(user1, private.ID, manual.ID, task.ID); return err },
		"disabled rule":    func() error { _, err := RunManually(user1, private.ID, disabled.ID, task.ID); return err },
		"other trigger":    func() error { _, err := RunManually(user1, private.ID, other.ID, task.ID); return err },
		"unknown rule":     func() error { _, err := RunManually(user1, private.ID, manual.ID+1000, task.ID); return err },
		"unknown task":     func() error { _, err := RunManually(user1, private.ID, manual.ID, task.ID+1000); return err },
		"unknown project":  func() error { _, err := RunManually(user1, 99999, manual.ID, task.ID); return err },
		"task of an other": func() error { _, err := RunManually(user1, private.ID, manual.ID, 1); return err },
		"rule of an other": func() error { _, err := RunManually(user1, projectOwned, manual.ID, task.ID); return err },
	} {
		err := call()
		if name == "rule of an other" {
			// user1 may write in this project: the rule is not found there, which says nothing about the private one.
			assert.True(t, models.IsErrAutomationRuleDoesNotExist(err), "%s: %v", name, err)
			continue
		}
		assert.True(t, models.IsErrGenericForbidden(err), "%s: %v", name, err)
	}
	assert.Empty(t, eventsNamed(t, spec.EventManual))
}

func TestManual_ListShowsEnabledManualRulesToWriters(t *testing.T) {
	reset(t)
	addRule(t, projectOwned, user1, rule(spec.TriggerManual, map[string]any{"label": "Raise"}, nil, setPriority(1)))
	titled := rule(spec.TriggerManual, nil, nil, setPriority(1))
	titled.Title = "Plain title"
	addRule(t, projectOwned, user1, titled)
	off := rule(spec.TriggerManual, nil, nil, setPriority(1))
	off.Enabled = false
	addRule(t, projectOwned, user1, off)
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, setPriority(1)))

	s := db.NewReadSession()
	defer s.Close()
	list, err := models.ListAutomationManualRules(s, user1, projectOwned)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "Raise", list[0].Label)
	assert.Equal(t, "Plain title", list[1].Label)

	_, err = models.ListAutomationManualRules(s, user2, projectOwned)
	assert.Error(t, err, "no access to the project")
}

// ---- the loop guard still holds ----

func TestManual_SelfTriggerGuardStillApplies(t *testing.T) {
	reset(t)
	manual := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, nil, setPriority(5)))
	// A rule that reacts to its own changes: updated -> set priority -> updated ...
	loop := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil, nil, act(spec.ActionSetField, map[string]any{"field": "title", "value": "x{{task.id}}"})))
	task := newTask(t, projectOwned, "t", user1)
	_, err := RunManually(user1, projectOwned, manual.ID, task.ID)
	require.NoError(t, err)
	for i := 0; i < 10 && drain(t) > 0; i++ {
		continue
	}
	var reasons []string
	for _, run := range allRuns(t, loop.ID) {
		reasons = append(reasons, run.Status+":"+run.SkipReason)
	}
	assert.NotEmpty(t, reasons, "the manual run's change reaches the other rule")
	assert.LessOrEqual(t, len(reasons), 3, "no endless loop: %v", reasons)
}

// ---- create task, webhook payload ----

func hookRule(t *testing.T, options map[string]any, actions ...*spec.Action) (*models.AutomationRule, string) {
	t.Helper()
	r := addRule(t, projectOwned, user1, rule(spec.TriggerWebhookIncoming, options, nil, actions...))
	require.NotEmpty(t, r.WebhookToken, "the token is returned once, on creation")
	return r, r.WebhookToken
}

func createFromPayload() *spec.Action {
	return act(spec.ActionCreateTask, map[string]any{"title": "Ticket {{event.payload.id}}: {{event.payload.subject}}", "description": "from {{event.payload.customer.name}}"})
}

func TestWebhook_CreatesATaskFromThePayloadAndContinuesWithIt(t *testing.T) {
	reset(t)
	r, token := hookRule(t, nil, createFromPayload(), setPriority(3),
		act(spec.ActionAddComment, map[string]any{"text": "created by {{event.payload.customer.name}}"}))

	ev, err := ReceiveWebhook(&WebhookCall{Token: token, Body: []byte(`{"id": 42, "subject": "Printer", "customer": {"name": "Ada"}}`)})
	require.NoError(t, err)
	assert.EqualValues(t, 0, ev.TaskID)
	drain(t)

	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	require.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	require.NotZero(t, runs[0].TaskID, "the run continues with the created task")
	task := getTask(t, runs[0].TaskID)
	assert.Equal(t, "Ticket 42: Printer", task.Title)
	assert.Equal(t, "from Ada", task.Description)
	assert.Equal(t, projectOwned, task.ProjectID)
	assert.EqualValues(t, 3, task.Priority, "the actions after the create act on the new task")
	steps := runSteps(t, runs[0].ID)
	require.Len(t, steps, 3)
}

func TestWebhook_ConditionsOnThePayloadDecide(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerWebhookIncoming, nil,
		&spec.Condition{Field: "event.payload.severity", Op: spec.OpEq, Value: "high"}, createFromPayload()))
	_, err := ReceiveWebhook(&WebhookCall{Token: r.WebhookToken, Body: []byte(`{"id": 1, "subject": "a", "customer": {"name": "x"}, "severity": "low"}`)})
	require.NoError(t, err)
	_, err = ReceiveWebhook(&WebhookCall{Token: r.WebhookToken, Body: []byte(`{"id": 2, "subject": "b", "customer": {"name": "x"}, "severity": "high"}`)})
	require.NoError(t, err)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 2)
	assert.Equal(t, models.AutomationRunSkipped, runs[0].Status)
	assert.Equal(t, models.AutomationRunSuccess, runs[1].Status)
}

func TestWebhook_TokenIsStoredAsAHashAndRegeneration(t *testing.T) {
	reset(t)
	r, token := hookRule(t, nil, createFromPayload())
	stored := getRule(t, r.ID)
	assert.Equal(t, models.HashAutomationWebhookToken(token), stored.WebhookTokenHash)
	assert.NotContains(t, stored.WebhookTokenHash, token)
	assert.Empty(t, stored.WebhookToken, "never read back")
	assert.Contains(t, r.WebhookURL, "/api/v2/automation/hooks/"+token)

	// Saving the rule keeps the token and does not show it again.
	r.Title = "renamed"
	mustCommit(t, func(s *xorm.Session) error { return r.Update(s, user1) })
	assert.Empty(t, r.WebhookToken)
	assert.Equal(t, stored.WebhookTokenHash, getRule(t, r.ID).WebhookTokenHash)

	var fresh *models.AutomationWebhookToken
	mustCommit(t, func(s *xorm.Session) error {
		fresh = &models.AutomationWebhookToken{ID: r.ID, ProjectID: projectOwned}
		can, err := fresh.CanCreate(s, user1)
		require.NoError(t, err)
		require.True(t, can)
		return fresh.Create(s, user1)
	})
	require.NotEqual(t, token, fresh.Token)
	_, err := ReceiveWebhook(&WebhookCall{Token: token, Body: []byte(`{}`)})
	assert.True(t, models.IsErrAutomationHookNotFound(err), "the old URL is dead: %v", err)
	_, err = ReceiveWebhook(&WebhookCall{Token: fresh.Token, Body: []byte(`{}`)})
	require.NoError(t, err)

	// Another trigger drops the token.
	r.Trigger = spec.Trigger{Type: spec.TriggerTaskCreated}
	r.Actions = []*spec.Action{setPriority(1)}
	mustCommit(t, func(s *xorm.Session) error { return r.Update(s, user1) })
	assert.Empty(t, getRule(t, r.ID).WebhookTokenHash)
}

func TestWebhook_Refusals(t *testing.T) {
	reset(t)
	r, token := hookRule(t, nil, createFromPayload())

	for _, bad := range []string{"", "nope", token + "x"} {
		_, err := ReceiveWebhook(&WebhookCall{Token: bad, Body: []byte(`{}`)})
		assert.True(t, models.IsErrAutomationHookNotFound(err), "token %q: %v", bad, err)
	}
	_, err := ReceiveWebhook(&WebhookCall{Token: token, Body: []byte(`[1,2]`)})
	assert.True(t, models.IsErrAutomationHookPayloadInvalid(err), "array: %v", err)
	_, err = ReceiveWebhook(&WebhookCall{Token: token, Body: []byte(`not json`)})
	assert.True(t, models.IsErrAutomationHookPayloadInvalid(err), "garbage: %v", err)
	_, err = ReceiveWebhook(&WebhookCall{Token: token, Body: nil})
	require.NoError(t, err, "an empty body is an empty payload")

	r.Enabled = false
	mustCommit(t, func(s *xorm.Session) error { return r.Update(s, user1) })
	_, err = ReceiveWebhook(&WebhookCall{Token: token, Body: []byte(`{}`)})
	assert.True(t, models.IsErrAutomationHookNotFound(err), "disabled rules look like absent ones: %v", err)
}

func TestWebhook_RateLimitPerRule(t *testing.T) {
	reset(t)
	config.AutomationWebhookRateLimit.Set(3)
	defer config.AutomationWebhookRateLimit.Set(60)
	_, token := hookRule(t, nil, createFromPayload())
	for i := 0; i < 3; i++ {
		_, err := ReceiveWebhook(&WebhookCall{Token: token, Body: []byte(`{}`)})
		require.NoError(t, err)
	}
	_, err := ReceiveWebhook(&WebhookCall{Token: token, Body: []byte(`{}`)})
	var limited models.ErrAutomationHookRateLimited
	require.ErrorAs(t, err, &limited)
	assert.Positive(t, limited.RetryAfterSeconds)

	// Another rule has its own budget.
	_, other := hookRule(t, nil, createFromPayload())
	_, err = ReceiveWebhook(&WebhookCall{Token: other, Body: []byte(`{}`)})
	assert.NoError(t, err)
}

func TestWebhook_UnsignedCallsAreRefusedBeforeTheRateLimit(t *testing.T) {
	reset(t)
	config.AutomationWebhookRateLimit.Set(1)
	defer config.AutomationWebhookRateLimit.Set(60)
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.AutomationSecret{ProjectID: projectOwned, Name: "HOOK_KEY", NewValue: "s3cret"}).Update(s, user1)
	})
	_, token := hookRule(t, map[string]any{"secret": "HOOK_KEY"}, createFromPayload())
	body := []byte(`{}`)
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	_, err := ReceiveWebhook(&WebhookCall{Token: token, Body: body, Signature: good})
	require.NoError(t, err)
	// The budget is used up: the sender is limited, anyone else is just refused.
	_, err = ReceiveWebhook(&WebhookCall{Token: token, Body: body, Signature: good})
	assert.True(t, models.IsErrAutomationHookRateLimited(err), "%v", err)
	_, err = ReceiveWebhook(&WebhookCall{Token: token, Body: body, Signature: "sha256=00"})
	assert.True(t, models.IsErrAutomationHookSignatureInvalid(err), "%v", err)
	_, err = ReceiveWebhook(&WebhookCall{Token: token, Body: body})
	assert.True(t, models.IsErrAutomationHookSignatureInvalid(err), "%v", err)
}

func TestWebhook_SignatureWithAProjectSecret(t *testing.T) {
	reset(t)
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.AutomationSecret{ProjectID: projectOwned, Name: "HOOK_KEY", NewValue: "s3cret"}).Update(s, user1)
	})
	_, token := hookRule(t, map[string]any{"secret": "HOOK_KEY"}, createFromPayload())
	body := []byte(`{"id": 7}`)
	sign := func(key string, b []byte) string {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write(b)
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	for name, sig := range map[string]string{"missing": "", "wrong key": sign("other", body), "other body": sign("s3cret", []byte(`{}`)), "no prefix": "abcd", "not hex": "sha256=zz"} {
		_, err := ReceiveWebhook(&WebhookCall{Token: token, Body: body, Signature: sig})
		assert.True(t, models.IsErrAutomationHookSignatureInvalid(err), "%s: %v", name, err)
	}
	_, err := ReceiveWebhook(&WebhookCall{Token: token, Body: body, Signature: sign("s3cret", body)})
	assert.NoError(t, err)
}

func TestWebhook_ValidationRejectsActionsThatNeedATask(t *testing.T) {
	reset(t)
	bad := rule(spec.TriggerWebhookIncoming, nil, nil, setPriority(1))
	bad.ProjectID = projectOwned
	s := db.NewSession()
	defer s.Close()
	err := bad.Create(s, user1)
	require.Error(t, err)
	assert.True(t, models.IsErrAutomationRuleInvalid(err))
	assert.Contains(t, err.Error(), "create task")

	ok := rule(spec.TriggerWebhookIncoming, nil, nil, act(spec.ActionHTTPRequest, map[string]any{"url": "https://example.com"}), createFromPayload(), setPriority(1))
	ok.ProjectID = projectOwned
	require.NoError(t, ok.Create(s, user1))

	missingSecret := rule(spec.TriggerWebhookIncoming, map[string]any{"secret": "NOPE"}, nil, createFromPayload())
	missingSecret.ProjectID = projectOwned
	assert.True(t, models.IsErrAutomationRuleInvalid(missingSecret.Create(s, user1)))
}

func TestWebhook_DryRunWithASimulatedPayload(t *testing.T) {
	reset(t)
	r, _ := hookRule(t, nil, createFromPayload(), setPriority(3))
	res, err := DryRunWith(context.Background(), r, 0, map[string]any{"id": float64(9), "subject": "S", "customer": map[string]any{"name": "N"}})
	require.NoError(t, err)
	require.Len(t, res.Steps, 2)
	assert.Contains(t, res.Steps[0].Summary, `Ticket 9: S`)
	assert.Empty(t, res.Steps[0].Error)
	assert.Contains(t, res.Steps[1].Summary, "task created above")
	assert.Zero(t, counterOfTasks(t), "nothing was written")
}

func counterOfTasks(t *testing.T) int {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("title LIKE ?", "Ticket%").Count(&models.Task{})
	require.NoError(t, err)
	return int(n)
}

func TestPayloadInTemplatesConditionsAndScripts(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerWebhookIncoming, nil,
		&spec.Condition{Script: `return event.payload.items.length === 2 && event.payload.items[1] === "b"`},
		act(spec.ActionCreateTask, map[string]any{"title": "{{event.payload.items.1}} {{event.payload.flag}} {{event.payload.nested.deep}} [{{event.payload.missing}}]"})))
	_, err := ReceiveWebhook(&WebhookCall{Token: r.WebhookToken, Body: []byte(`{"items": ["a", "b"], "flag": true, "nested": {"deep": 1.5}}`)})
	require.NoError(t, err)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	require.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	assert.Equal(t, "b true 1.5 []", getTask(t, runs[0].TaskID).Title)
	assert.Contains(t, runSteps(t, runs[0].ID)[0].Output, "warning: unknown placeholder {{event.payload.missing}}")
}

// ---- wait ----

func TestWait_SurvivesAndContinuesAfterTheDelay(t *testing.T) {
	reset(t)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		setPriority(2), act(spec.ActionWait, map[string]any{"duration": "2d"}), setPriority(5)))
	task := newTask(t, projectOwned, "w", user1)
	drain(t)

	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunRunning, runs[0].Status, "waiting is running with a continuation")
	assert.EqualValues(t, 2, getTask(t, task.ID).Priority)
	steps := runSteps(t, runs[0].ID)
	require.Len(t, steps, 2)
	assert.Contains(t, steps[1].Output, waitOutput)
	resume := lastEventOf(t, spec.EventResume)
	assert.Equal(t, models.AutomationEventPending, resume.Status)
	assert.True(t, resume.RunAfter.After(now().Add(47*time.Hour)))

	// Nothing happens before the time is up, however often the workers look.
	advance(47 * time.Hour)
	assert.Zero(t, drain(t))
	assert.EqualValues(t, 2, getTask(t, task.ID).Priority)

	// "Restart": the rest is only in the database.
	advance(2 * time.Hour)
	assert.Equal(t, 1, drain(t))
	assert.EqualValues(t, 5, getTask(t, task.ID).Priority)
	runs = allRuns(t, r.ID)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	assert.Len(t, runSteps(t, runs[0].ID), 3)
}

func TestWait_RuleDisabledWhileWaitingAbortsTheRun(t *testing.T) {
	reset(t)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		act(spec.ActionWait, map[string]any{"duration": "1h"}), setPriority(5)))
	task := newTask(t, projectOwned, "w", user1)
	drain(t)
	r.Enabled = false
	mustCommit(t, func(s *xorm.Session) error { return r.Update(s, user1) })
	advance(2 * time.Hour)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunAborted, runs[0].Status)
	assert.Equal(t, SkipRuleDisabled, runs[0].SkipReason)
	assert.EqualValues(t, 0, getTask(t, task.ID).Priority)
}

func TestWait_TwoWaitsAndConditionsAreNotEvaluatedAgain(t *testing.T) {
	reset(t)
	advance := fakeClock(t)
	cond := &spec.Condition{Field: "title", Op: spec.OpEq, Value: "start"}
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, cond,
		act(spec.ActionWait, map[string]any{"duration": "1h"}),
		act(spec.ActionSetField, map[string]any{"field": "title", "value": "changed"}),
		act(spec.ActionWait, map[string]any{"duration": "1h"}),
		setPriority(4)))
	task := newTask(t, projectOwned, "start", user1)
	drain(t)
	advance(61 * time.Minute)
	drain(t)
	assert.Equal(t, "changed", getTask(t, task.ID).Title)
	advance(61 * time.Minute)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	assert.EqualValues(t, 4, getTask(t, task.ID).Priority)
}

func TestWait_AnotherDeliveryOfTheOriginalEventDoesNotSkipThePause(t *testing.T) {
	reset(t)
	fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		act(spec.ActionWait, map[string]any{"duration": "1h"}), setPriority(5)))
	task := newTask(t, projectOwned, "w", user1)
	drain(t)
	original := allEvents(t)[0]
	require.Equal(t, "task.created", original.Name)

	// Put the original event back in the queue, as a retry of a sibling rule's failure would.
	s := db.NewAutocommitSession()
	_, err := s.ID(original.ID).Cols("status").Update(&models.AutomationEvent{Status: models.AutomationEventPending})
	require.NoError(t, err)
	s.Close()
	drain(t)
	assert.EqualValues(t, 0, getTask(t, task.ID).Priority, "still waiting")
	assert.Equal(t, models.AutomationRunRunning, allRuns(t, r.ID)[0].Status)
}

// A late delivery of the original event (after the wait is over, before the resume event ran) must not continue the
// run: the post-wait steps belong to the resume event and are applied once.
func TestWait_AnotherDeliveryOfTheOriginalEventAfterTheWaitDoesNotContinue(t *testing.T) {
	reset(t)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		act(spec.ActionWait, map[string]any{"duration": "1h"}), act(spec.ActionAddComment, map[string]any{"text": "after the wait"})))
	task := newTask(t, projectOwned, "w", user1)
	drain(t)
	original := allEvents(t)[0]
	require.Equal(t, "task.created", original.Name)
	resume := lastEventOf(t, spec.EventResume)

	setStatus := func(id int64, status string) {
		s := db.NewAutocommitSession()
		defer s.Close()
		_, err := s.ID(id).Cols("status").Update(&models.AutomationEvent{Status: status})
		require.NoError(t, err)
	}
	advance(2 * time.Hour)
	setStatus(resume.ID, models.AutomationEventDead) // keep the resume event out of reach for now
	setStatus(original.ID, models.AutomationEventPending)
	assert.Equal(t, 1, drain(t))
	assert.Len(t, runSteps(t, allRuns(t, r.ID)[0].ID), 1, "only the wait: the original event does not continue past it")
	assert.Equal(t, models.AutomationRunRunning, allRuns(t, r.ID)[0].Status)

	setStatus(resume.ID, models.AutomationEventPending)
	assert.Equal(t, 1, drain(t))
	// Delivered again once more, by either event.
	setStatus(resume.ID, models.AutomationEventPending)
	setStatus(original.ID, models.AutomationEventPending)
	drain(t)
	run := allRuns(t, r.ID)[0]
	assert.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	assert.Len(t, runSteps(t, run.ID), 2)
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("task_id = ? AND comment LIKE ?", task.ID, "%after the wait%").Count(&models.TaskComment{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the step after the wait was applied once")
}

func TestWait_Validation(t *testing.T) {
	reset(t)
	s := db.NewSession()
	defer s.Close()
	for _, d := range []string{"30s", "0m", "abc", "31d"} {
		bad := rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionWait, map[string]any{"duration": d}))
		bad.ProjectID = projectOwned
		assert.True(t, models.IsErrAutomationRuleInvalid(bad.Create(s, user1)), d)
	}
	for _, d := range []string{"1m", "12h", "30d", "4w"} {
		good := rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionWait, map[string]any{"duration": d}))
		good.ProjectID = projectOwned
		if d == "4w" {
			// 28 days is fine.
			require.NoError(t, good.Create(s, user1), d)
			continue
		}
		require.NoError(t, good.Create(s, user1), d)
	}
	config.AutomationWaitMax.Set(2 * time.Hour)
	defer config.AutomationWaitMax.Set(30 * 24 * time.Hour)
	long := rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionWait, map[string]any{"duration": "3h"}))
	long.ProjectID = projectOwned
	assert.True(t, models.IsErrAutomationRuleInvalid(long.Create(s, user1)))
}

func TestWait_DryRunSaysWouldWaitAndWritesNothing(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, nil, act(spec.ActionWait, map[string]any{"duration": "2d"}), setPriority(3)))
	task := newTask(t, projectOwned, "w", user1)
	before := len(allEvents(t))
	res, err := DryRun(context.Background(), r, task.ID)
	require.NoError(t, err)
	require.Len(t, res.Steps, 2)
	assert.Contains(t, res.Steps[0].Summary, "would wait 2d")
	assert.Contains(t, res.Steps[1].Summary, "priority to 3")
	assert.Len(t, allEvents(t), before)
}

// ---- notify ----

func notificationsOf(t *testing.T, userID int64) []*notifications.DatabaseNotification {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	rows := []*notifications.DatabaseNotification{}
	require.NoError(t, s.Where("notifiable_id = ? AND name = ?", userID, "automation.notify").OrderBy("id ASC").Find(&rows))
	return rows
}

func fullUser(t *testing.T, id int64) *user.User {
	t.Helper()
	s := db.NewReadSession()
	defer s.Close()
	u, err := user.GetUserByID(s, id)
	require.NoError(t, err)
	return u
}

func notifyAction(params map[string]any) *spec.Action { return act(spec.ActionNotify, params) }

func TestNotify_AssigneesCreatorAndSpecificUsers(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, nil,
		notifyAction(map[string]any{"recipients": []any{"assignees", "task_creator", float64(user1.ID)}, "message": "Look at {{task.title}}"})))
	task := newTask(t, projectOwned, "Water the plants", user1)
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.TaskAssginee{TaskID: task.ID, UserID: user1.ID}).Create(s, user1)
	})
	before := len(notificationsOf(t, user1.ID))
	_, err := RunManually(user1, projectOwned, r.ID, task.ID)
	require.NoError(t, err)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	rows := notificationsOf(t, user1.ID)
	require.Len(t, rows, before+1, "the same person named three times is notified once")
	payload, ok := rows[len(rows)-1].Notification.(map[string]any)
	if !ok {
		raw, err := json.Marshal(rows[len(rows)-1].Notification)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &payload))
	}
	assert.Equal(t, "Look at Water the plants", payload["message"])
	assert.EqualValues(t, r.ID, payload["rule_id"])
}

func TestNotify_SkipsPeopleWithoutAccessAndFailsWhenNobodyIsLeft(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, nil,
		notifyAction(map[string]any{"recipients": []any{float64(user1.ID), float64(user2.ID)}, "message": "hi"})))
	task := newTask(t, projectOwned, "t", user1)
	res, err := DryRun(context.Background(), r, task.ID)
	require.NoError(t, err)
	require.Len(t, res.Steps, 1)
	assert.Empty(t, res.Steps[0].Error)
	assert.Contains(t, res.Steps[0].Summary, "notify 1 user(s)")
	require.NotEmpty(t, res.Steps[0].Warnings)
	assert.Contains(t, res.Steps[0].Warnings[0], "skipped")

	nobody := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, nil,
		notifyAction(map[string]any{"recipients": []any{float64(user2.ID)}, "message": "hi"})))
	_, err = RunManually(user1, projectOwned, nobody.ID, task.ID)
	require.NoError(t, err)
	drain(t)
	run := allRuns(t, nobody.ID)[0]
	assert.Equal(t, models.AutomationRunFailed, run.Status)
	assert.Contains(t, run.Error, "nobody to notify")
}

func TestNotify_Team(t *testing.T) {
	reset(t)
	team := &models.Team{Name: "crew"}
	full := fullUser(t, user1.ID)
	mustCommit(t, func(s *xorm.Session) error { return team.Create(s, full) })
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.TeamProject{TeamID: team.ID, ProjectID: projectOwned, Permission: models.PermissionRead}).Create(s, full)
	})
	r := addRule(t, projectOwned, user1, rule(spec.TriggerManual, nil, nil, notifyAction(map[string]any{"team_id": float64(team.ID), "message": "team news"})))
	task := newTask(t, projectOwned, "t", user1)
	before := len(notificationsOf(t, user1.ID))
	_, err := RunManually(user1, projectOwned, r.ID, task.ID)
	require.NoError(t, err)
	drain(t)
	require.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status, allRuns(t, r.ID)[0].Error)
	assert.Len(t, notificationsOf(t, user1.ID), before+1, "the creator of the team is a member")
}

func TestNotify_Validation(t *testing.T) {
	reset(t)
	s := db.NewSession()
	defer s.Close()
	nobody := rule(spec.TriggerManual, nil, nil, notifyAction(map[string]any{"message": "x"}))
	nobody.ProjectID = projectOwned
	assert.True(t, models.IsErrAutomationRuleInvalid(nobody.Create(s, user1)), "needs a recipient or a team")

	// Recipients that need a task are refused by a trigger without one.
	noTask := rule(spec.TriggerWebhookIncoming, nil, nil, notifyAction(map[string]any{"recipients": []any{"assignees"}, "message": "x"}))
	noTask.ProjectID = projectOwned
	assert.True(t, models.IsErrAutomationRuleInvalid(noTask.Create(s, user1)))
	fine := rule(spec.TriggerWebhookIncoming, nil, nil, notifyAction(map[string]any{"recipients": []any{float64(user1.ID)}, "message": "{{event.payload.x}}"}))
	fine.ProjectID = projectOwned
	assert.NoError(t, fine.Create(s, user1))
}

func TestWait_AProjectCannotPileUpWaitingRuns(t *testing.T) {
	reset(t)
	fakeClock(t)
	config.AutomationMaxPendingWaits.Set(2)
	defer config.AutomationMaxPendingWaits.Set(1000)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		act(spec.ActionWait, map[string]any{"duration": "1h"}), setPriority(5)))
	for i := 0; i < 3; i++ {
		newTask(t, projectOwned, "w", user1)
	}
	drain(t)

	statuses := map[string]int{}
	var failed *models.AutomationRun
	for _, run := range allRuns(t, r.ID) {
		statuses[run.Status]++
		if run.Status == models.AutomationRunFailed {
			failed = run
		}
	}
	assert.Equal(t, map[string]int{models.AutomationRunRunning: 2, models.AutomationRunFailed: 1}, statuses)
	require.NotNil(t, failed)
	assert.Contains(t, failed.Error, "automation.maxpendingwaits")
	assert.Len(t, eventsNamed(t, spec.EventResume), 2)
}

func TestWait_CleanupKeepsTheEventAndRunOfAWaitingRun(t *testing.T) {
	reset(t)
	advance := fakeClock(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		act(spec.ActionWait, map[string]any{"duration": "2d"}), setPriority(5)))
	task := newTask(t, projectOwned, "w", user1)
	drain(t)

	// The retention (a day, say) is shorter than the wait: the original event is old and done, but the run needs it.
	advance(24 * time.Hour)
	events, runs, err := Cleanup(now().Add(time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 0, runs)
	assert.EqualValues(t, 0, events)
	assert.Len(t, eventsNamed(t, "task.created"), 1)

	advance(25 * time.Hour)
	assert.Equal(t, 1, drain(t))
	assert.EqualValues(t, 5, getTask(t, task.ID).Priority, "the run continued from the kept event")
	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)

	// Once the run is over, the event goes with the next cleanup.
	events, runs, err = Cleanup(now().Add(time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, runs)
	assert.Positive(t, events)
}

func TestWait_RunsStuckInRunningAreEndedWellPastTheLongestWait(t *testing.T) {
	reset(t)
	advance := fakeClock(t)
	config.AutomationWaitMax.Set(2 * time.Hour)
	defer config.AutomationWaitMax.Set(30 * 24 * time.Hour)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		act(spec.ActionWait, map[string]any{"duration": "1h"}), setPriority(5)))
	newTask(t, projectOwned, "stuck", user1)
	drain(t)
	// The resume event is lost: nothing will ever continue the run.
	s := db.NewAutocommitSession()
	_, err := s.Where("name = ?", spec.EventResume).Cols("status").Update(&models.AutomationEvent{Status: models.AutomationEventDead})
	require.NoError(t, err)
	s.Close()

	advance(10 * time.Hour)
	n, err := ReapStuckRuns(now())
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "within the longest wait plus the grace period")
	assert.Equal(t, models.AutomationRunRunning, allRuns(t, r.ID)[0].Status)

	advance(30 * time.Hour)
	n, err = ReapStuckRuns(now())
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	run := allRuns(t, r.ID)[0]
	assert.Equal(t, models.AutomationRunFailed, run.Status)
	assert.Contains(t, run.Error, "automation.waitmax")
	assert.False(t, run.Finished.IsZero())

	n, err = ReapStuckRuns(now())
	require.NoError(t, err)
	assert.EqualValues(t, 0, n)
}

// An archived project must not tell an outsider that it exists: the refusal is the one of a project that is not there.
func TestManual_AnOutsiderGetsTheSameRefusalForAnArchivedProject(t *testing.T) {
	reset(t)
	const archivedProject = int64(22)
	_, err := RunManually(user6, archivedProject, 1, 1)
	require.Error(t, err)
	_, missing := RunManually(user6, 99999, 1, 1)
	require.Error(t, missing)
	assert.IsType(t, models.ErrGenericForbidden{}, err)
	assert.IsType(t, missing, err)
}
