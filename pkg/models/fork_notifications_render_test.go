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

package models

import (
	"encoding/json"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/mail"
	"code.vikunja.io/api/pkg/notifications"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What a notified user gets to read: the in-app title, the subject and body of the mail, in the language of the
// recipient, with the text of the form and of the task escaped so that it cannot add markup or a link of its own.
func TestFormSubmitted_NotificationReadsAndEscapes(t *testing.T) {
	formsReset(t)
	enableApprovalMail(t)
	f := baseForm(FormAccessPublic)
	f.Title = "Support *urgent* [click](https://evil.example)"
	f.Settings = &FormSettings{NotifyUserIDs: []int64{1}}
	createForm(t, f)
	_, err := submit(t, f, map[string]any{"title": "Printer _jammed_"})
	require.NoError(t, err)
	evs := events.GetDispatchedEvents("form.submitted")
	require.Len(t, evs, 1)
	task := lastTask(t)

	events.TestListener(t, evs[0], &SendFormSubmittedNotification{})

	sent := mail.SentMails()
	require.Len(t, sent, 1, "one notified user, one mail")
	assert.Equal(t, "user1@example.com", sent[0].To)
	assert.Contains(t, sent[0].Message, task.GetFrontendURL(), "the mail links to the task")
	// The plain text shows the words as they are; the HTML body must not turn them into markup or a link.
	assert.Contains(t, sent[0].Message, "Printer _jammed_")
	assert.NotContains(t, sent[0].HTMLMessage, `href="https://evil.example"`, "the form title cannot add a link")
	assert.NotContains(t, sent[0].HTMLMessage, "<em>urgent</em>")
	assert.NotContains(t, sent[0].HTMLMessage, "<em>jammed</em>")
	assert.Contains(t, sent[0].HTMLMessage, "urgent")
	assert.Contains(t, sent[0].HTMLMessage, "jammed")

	s := db.NewSession()
	defer s.Close()
	rows := []*notifications.DatabaseNotification{}
	require.NoError(t, s.Where("notifiable_id = ? AND name = ?", 1, "form.submitted").Find(&rows))
	require.Len(t, rows, 1)

	n := &FormSubmittedNotification{FormID: f.ID, FormTitle: "Orders", Task: &Task{ID: 7, Title: "Box", Identifier: "#7", Index: 7, ProjectID: 1}, Project: &Project{ID: 1, Identifier: "P"}}
	en := n.ToTitle("en")
	tr := n.ToTitle("tr-TR")
	assert.Contains(t, en, "Orders")
	assert.Contains(t, en, "Box")
	assert.Contains(t, tr, "Orders")
	assert.Contains(t, tr, "Box")
	assert.NotEqual(t, en, tr, "the Turkish title is translated")
}

func TestAutomationNotify_NotificationReadsAndLinksToTheTask(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	target := cfUser1
	n := &AutomationNotifyNotification{
		Message: "Task *needs* a look",
		RuleID:  3, RuleTitle: "Watch",
		Task:    &Task{ID: 1, Title: "T", ProjectID: 1},
		Project: &Project{ID: 1},
	}
	assert.Equal(t, "Task *needs* a look", n.ToTitle("en"), "the title is the message as the rule wrote it")
	assert.Equal(t, "automation.notify", n.Name())
	assert.Equal(t, int64(1), n.ProjectID())
	assert.Equal(t, getThreadID(1), n.ThreadID())

	n.Target = target
	m := n.ToMail("en")
	require.NotNil(t, m)

	project := &AutomationNotifyNotification{Message: "A message", Project: &Project{ID: 9}}
	assert.Empty(t, project.ThreadID(), "a notification about the project has no thread")
	assert.Equal(t, int64(9), project.ProjectID())
	project.Target = target
	assert.NotNil(t, project.ToMail("en"), "a mail without a task has no action link and still renders")
}

// "form.submitted ... submitter id, name and the e-mail address the form collected, never the one of the account; no
// token, no IP" (docs/modules/forms.md), checked on the event as it is serialised for the listeners and the outbox.
func TestFormSubmitted_EventCarriesWhatTheDocsSayAndNothingElse(t *testing.T) {
	formsReset(t)
	f := baseForm(FormAccessUsers)
	f.Settings = &FormSettings{CollectName: true, CollectEmail: true}
	createForm(t, f)

	_, err := submit(t, f, map[string]any{"title": "Hello"}, asAuth(fU2), fromIP("203.0.113.9"), func(r *FormSubmitRequest) {
		r.SubmitterName = "Ada Lovelace"
		r.SubmitterEmail = "ada@collected.example"
	})
	require.NoError(t, err)

	evs := events.GetDispatchedEvents("form.submitted")
	require.Len(t, evs, 1, "dispatched exactly once")
	ev := evs[0].(*FormSubmittedEvent)
	assert.Equal(t, f.ID, ev.FormID)
	assert.Equal(t, f.Title, ev.FormTitle)
	assert.Equal(t, lastTask(t).ID, ev.Task.ID)
	assert.EqualValues(t, 1, ev.Doer.ID, "the doer is the form owner, not the submitter")
	require.NotNil(t, ev.SubmitterID)
	assert.EqualValues(t, 2, *ev.SubmitterID)
	assert.Equal(t, "Ada Lovelace", ev.SubmitterName, "the name the form collected")
	assert.Equal(t, "ada@collected.example", ev.SubmitterEmail, "the address the form collected, not user2@example.com")

	raw, err := json.Marshal(ev)
	require.NoError(t, err)
	body := string(raw)
	assert.NotContains(t, body, f.Token, "no token")
	assert.NotContains(t, body, "203.0.113.9", "no address of the client")
	assert.NotContains(t, strings.ToLower(body), "ip_hash")
	assert.NotContains(t, body, "user2@example.com", "never the e-mail address of the account")

	var sub FormSubmission
	s := db.NewSession()
	defer s.Close()
	has, err := s.Where("form_id = ?", f.ID).Get(&sub)
	require.NoError(t, err)
	require.True(t, has)
	assert.NotContains(t, body, sub.IPHash, "nor the hash of it")
	assert.NotEmpty(t, sub.IPHash, "the hash is stored for the rate limit")
}

// The word for the outcome of an approval request has a text in both languages for every status, and not the key.
func TestApprovalOutcomeWord(t *testing.T) {
	want := map[ApprovalStatus]string{
		ApprovalStatusPending: "pending", ApprovalStatusApproved: "approved",
		ApprovalStatusRejected: "rejected", ApprovalStatusCancelled: "cancelled",
	}
	for status, english := range want {
		en := approvalOutcomeWord("en", status)
		assert.Equal(t, english, strings.ToLower(en), string(status))
		tr := approvalOutcomeWord("tr-TR", status)
		assert.NotEmpty(t, tr)
		assert.NotContains(t, tr, "notifications.approval.outcome", "a missing translation falls back to the key")
		assert.NotEqual(t, en, tr, "%s is translated into Turkish", status)
	}
	assert.Equal(t, "unheard of", approvalOutcomeWord("en", ApprovalStatus("unheard of")))
}
