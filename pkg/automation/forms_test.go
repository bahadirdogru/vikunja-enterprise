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
	"fmt"
	"sync"
	"testing"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

func newForm(t *testing.T, mutate func(*models.Form)) *models.Form {
	t.Helper()
	config.FormsEnabled.Set(true)
	config.FormsPublicEnabled.Set(true)
	config.FormsPublicRateLimit.Set(1000)
	config.FormsPublicFormRateLimit.Set(100000)
	f := &models.Form{
		ProjectID: projectOwned, Title: "Request", Access: models.FormAccessPublic, Enabled: true,
		Fields:   []*models.FormField{{Target: "title", Label: "Title", Required: true}},
		Settings: &models.FormSettings{},
	}
	if mutate != nil {
		mutate(f)
	}
	mustCommit(t, func(s *xorm.Session) error { return f.Create(s, user1) })
	return f
}

func submitAnon(t *testing.T, f *models.Form, title, name string) error {
	t.Helper()
	_, err := models.SubmitForm(context.Background(), &models.FormSubmitRequest{
		Token: f.Token, IP: "203.0.113.5", Values: map[string]any{"title": title}, SubmitterName: name, SubmitterEmail: "a@example.com",
	})
	return err
}

func TestForms_SubmittedEventTriggersRulesOnTheCreatedTask(t *testing.T) {
	reset(t)
	f := newForm(t, func(f *models.Form) { f.Settings.CollectName = true; f.Settings.CollectEmail = true })
	other := newForm(t, nil)

	anyRule := insertRawRuleWith(t, projectOwned, user1, spec.TriggerFormSubmitted, nil, nil,
		act(spec.ActionSetCustomField, map[string]any{"field_id": 1, "value": "from {{event.form.title}} by {{event.form.submitter_name}} <{{event.form.submitter_email}}>"}))
	counting(t, projectOwned, spec.TriggerFormSubmitted, map[string]any{"form_id": f.ID}, "this_form")
	counting(t, projectOwned, spec.TriggerFormSubmitted, map[string]any{"form_id": other.ID}, "other_form")
	insertRawRuleWith(t, projectOwned, user1, spec.TriggerFormSubmitted, nil,
		&spec.Condition{All: []*spec.Condition{
			{Field: "event.new.submitter_name", Op: "=", Value: "Ada"},
			{Field: "event.new.form_title", Op: "=", Value: "Request"},
			{Field: "event.new.form_id", Op: "=", Value: f.ID},
			{Field: "event.new.submitter_email", Op: "like", Value: "%@example.com"},
		}}, rawAct("test.count", "key", "conditions_matched"))
	counting(t, projectOwned, spec.TriggerTaskCreated, nil, "task_created")

	require.NoError(t, submitAnon(t, f, "Laptop", "Ada"))
	drain(t)

	s := db.NewSession()
	defer s.Close()
	task := &models.Task{}
	_, err := s.Where("title = ?", "Laptop").Get(task)
	require.NoError(t, err)
	require.NotZero(t, task.ID)

	assert.Equal(t, 1, counter("this_form"))
	assert.Equal(t, 0, counter("other_form"))
	assert.Equal(t, 1, counter("conditions_matched"))
	assert.Equal(t, 1, counter("task_created"), "task.created of the same submission is delivered too")

	values, err := models.GetCustomFieldValuesOfTask(s, task)
	require.NoError(t, err)
	var got any
	for _, v := range values {
		if v.CustomFieldID == 1 {
			got = v.Value
		}
	}
	assert.Equal(t, "from Request by Ada <a@example.com>", got, "the rule acted on the task the form created")
	runs := allRuns(t, anyRule.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, models.AutomationRunSuccess, runs[0].Status)
}

func TestForms_SubmittedEventIsInTheOutbox(t *testing.T) {
	reset(t)
	f := newForm(t, nil)
	counting(t, projectOwned, spec.TriggerFormSubmitted, nil, "n")
	counting(t, projectOwned, spec.TriggerTaskCreated, nil, "c")
	require.NoError(t, submitAnon(t, f, "x", ""))
	names := []string{}
	for _, e := range allEvents(t) {
		names = append(names, e.Name)
	}
	assert.Contains(t, names, "form.submitted")
	assert.Contains(t, names, "task.created")
	// The submission is the doer's: the owner of the form.
	for _, e := range allEvents(t) {
		if e.Name == "form.submitted" {
			assert.Contains(t, e.Payload, `"doer":{"id":1`)
			assert.NotContains(t, e.Payload, f.Token)
			assert.NotContains(t, e.Payload, "203.0.113.5")
			assert.NotContains(t, e.Payload, "ip_hash")
		}
	}
}

func TestForms_RuleOnTheTriggerCanBeSaved(t *testing.T) {
	reset(t)
	f := newForm(t, nil)
	r := rule(spec.TriggerFormSubmitted, map[string]any{"form_id": f.ID}, nil,
		act(spec.ActionAddComment, map[string]any{"text": "{{event.form.title}} / {{event.form.submitter_name}}"}))
	addRule(t, projectOwned, user1, r)
	require.NotZero(t, r.ID)
}

func TestForms_MaxSubmissionsHoldsUnderConcurrency(t *testing.T) {
	reset(t)
	const limit, workers = 5, 24
	f := newForm(t, func(f *models.Form) { f.Settings.MaxSubmissions = limit })
	tasksBefore := countTasks(t)

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, full := 0, 0
	var other []error
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := models.SubmitForm(context.Background(), &models.FormSubmitRequest{
				Token: f.Token, IP: fmt.Sprintf("198.51.100.%d", i), Values: map[string]any{"title": fmt.Sprintf("n%d", i)},
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case models.IsErrFormUnavailable(err):
				full++
			default:
				other = append(other, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	assert.Empty(t, other)
	assert.Equal(t, limit, ok, "exactly the maximum succeeded")
	assert.Equal(t, workers-limit, full)

	s := db.NewSession()
	defer s.Close()
	stored := &models.Form{}
	_, err := s.ID(f.ID).Get(stored)
	require.NoError(t, err)
	assert.EqualValues(t, limit, stored.SubmissionCount)
	n, err := s.Where("form_id = ?", f.ID).Count(&models.FormSubmission{})
	require.NoError(t, err)
	assert.EqualValues(t, limit, n)
	assert.Equal(t, tasksBefore+limit, countTasks(t))
}

func countTasks(t *testing.T) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Count(&models.Task{})
	require.NoError(t, err)
	return n
}
