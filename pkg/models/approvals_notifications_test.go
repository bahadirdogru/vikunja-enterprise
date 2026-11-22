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
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/mail"
	"code.vikunja.io/api/pkg/notifications"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func approvalNotificationsOf(t *testing.T, userID int64, name string) []*notifications.DatabaseNotification {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	rows := []*notifications.DatabaseNotification{}
	require.NoError(t, s.Where("notifiable_id = ? AND name = ?", userID, name).OrderBy("id ASC").Find(&rows))
	return rows
}

func payloadOf(t *testing.T, n *notifications.DatabaseNotification) map[string]any {
	t.Helper()
	raw, err := json.Marshal(n.Notification)
	require.NoError(t, err)
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func enableApprovalMail(t *testing.T) {
	t.Helper()
	old := config.MailerEnabled.GetBool()
	config.MailerEnabled.Set(true)
	mail.ResetSent()
	t.Cleanup(func() {
		config.MailerEnabled.Set(old)
		mail.ResetSent()
	})
}

func TestApprovals_Notifications(t *testing.T) {
	t.Run("approvers are asked in the app and by mail when the mailer is on", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), func(doc *Workflow) {
			doc.Transitions[0].Approval.DueIn = "2d"
		})
		req := env.request(t, apU1)
		enableApprovalMail(t)

		events.TestListener(t, &ApprovalRequestedEvent{Approval: &ApprovalRequest{ID: req.ID}}, &SendApprovalRequestedNotification{})

		for _, id := range []int64{6, 2} {
			rows := approvalNotificationsOf(t, id, "approval.requested")
			require.Len(t, rows, 1, "user %d", id)
			assert.EqualValues(t, apProject, rows[0].ProjectID)
			payload := payloadOf(t, rows[0])
			assert.EqualValues(t, 1, payload["doer"].(map[string]any)["id"])
			assert.EqualValues(t, env.task.ID, payload["task"].(map[string]any)["id"])
			assert.Equal(t, "Start work", payload["approval"].(map[string]any)["title"])
		}
		assert.Empty(t, approvalNotificationsOf(t, 1, "approval.requested"), "the requester is not asked")
		assert.Empty(t, approvalNotificationsOf(t, 3, "approval.requested"))

		sent := mail.SentMails()
		require.Len(t, sent, 2)
		assert.Contains(t, sent[0].Subject, "Approval requested")
		assert.Contains(t, sent[0].Subject, "needs a sign-off")
		assert.Contains(t, sent[0].Message, "asks for your approval")
		assert.Contains(t, sent[0].Message, "Please decide until")
		assert.Contains(t, sent[0].Message, "tasks/")
	})

	t.Run("nothing is sent for a request that is not pending any more", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		events.TestListener(t, &ApprovalRequestedEvent{Approval: &ApprovalRequest{ID: req.ID}}, &SendApprovalRequestedNotification{})
		assert.Empty(t, approvalNotificationsOf(t, 6, "approval.requested"))
	})

	t.Run("the requester hears the outcome and undecided approvers learn they are not needed", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		req := env.request(t, apU1)
		resolved := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		enableApprovalMail(t)

		events.TestListener(t, &ApprovalResolvedEvent{Approval: &ApprovalRequest{ID: req.ID}, Outcome: resolved.Status, Doer: apU6}, &SendApprovalResolvedNotification{})

		toRequester := approvalNotificationsOf(t, 1, "approval.resolved")
		require.Len(t, toRequester, 1)
		payload := payloadOf(t, toRequester[0])
		assert.Equal(t, true, payload["to_requester"])
		assert.Equal(t, "approved", payload["approval"].(map[string]any)["status"])
		assert.EqualValues(t, 6, payload["doer"].(map[string]any)["id"])

		toOther := approvalNotificationsOf(t, 2, "approval.resolved")
		require.Len(t, toOther, 1, "the approver who did not decide is told someone else did")
		assert.NotEqual(t, true, payloadOf(t, toOther[0])["to_requester"])
		assert.Empty(t, approvalNotificationsOf(t, 6, "approval.resolved"), "the one who decided is not told")

		sent := mail.SentMails()
		require.Len(t, sent, 2)
		assert.Contains(t, sent[0].Subject, "was approved")
		assert.Contains(t, sent[0].Message, "Your approval request")
		assert.Contains(t, sent[1].Message, "by user6")
		assert.Contains(t, sent[1].Message, "You do not need to decide any more.")
	})

	t.Run("a rejection and a cancellation by the requester", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionRejected, "no")
		events.TestListener(t, &ApprovalResolvedEvent{Approval: &ApprovalRequest{ID: req.ID}, Outcome: ApprovalStatusRejected, Doer: apU6}, &SendApprovalResolvedNotification{})
		assert.Len(t, approvalNotificationsOf(t, 1, "approval.resolved"), 1)
		assert.Len(t, approvalNotificationsOf(t, 2, "approval.resolved"), 1)

		env = newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		req = env.request(t, apU1)
		_, err := cancelAs(t, req.ID, apU1, "my mistake")
		require.NoError(t, err)
		events.TestListener(t, &ApprovalResolvedEvent{Approval: &ApprovalRequest{ID: req.ID}, Outcome: ApprovalStatusCancelled, Doer: apU1}, &SendApprovalResolvedNotification{})
		assert.Empty(t, approvalNotificationsOf(t, 1, "approval.resolved"), "whoever cancelled knows")
		rows := approvalNotificationsOf(t, 6, "approval.resolved")
		require.Len(t, rows, 1)
		assert.Equal(t, "my mistake", payloadOf(t, rows[0])["approval"].(map[string]any)["cancel_message"])
	})

	t.Run("a system cancellation tells the requester and the approvers", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		_, err := changeTaskStatusAs(t, env.task.ID, env.canceled, "", apU1)
		require.NoError(t, err)
		events.TestListener(t, &ApprovalResolvedEvent{Approval: &ApprovalRequest{ID: req.ID}, Outcome: ApprovalStatusCancelled}, &SendApprovalResolvedNotification{})
		require.Len(t, approvalNotificationsOf(t, 1, "approval.resolved"), 1)
		require.Len(t, approvalNotificationsOf(t, 6, "approval.resolved"), 1)
	})

	t.Run("the listeners react to the events the model dispatches", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		events.ClearDispatchedEvents()
		env.request(t, apU1)
		requested := events.GetDispatchedEvents("approval.requested")
		require.Len(t, requested, 1)
		listener := &SendApprovalRequestedNotification{}
		assert.Equal(t, "approval.requested.notification.send", listener.Name())
		events.TestListener(t, requested[0], listener)
		assert.Len(t, approvalNotificationsOf(t, 6, "approval.requested"), 1)
	})

	t.Run("titles and mail bodies are translated strings", func(t *testing.T) {
		task := &Task{ID: 5, Title: "Cover", Index: 5}
		info := &ApprovalNotificationInfo{ID: 1, Title: "Print", Status: ApprovalStatusRejected, CancelMessage: "too late"}
		doer := apU6
		n := &ApprovalResolvedNotification{Doer: doer, Task: task, Project: &Project{ID: apProject}, Approval: info, ToRequester: true, Target: apU1}
		assert.Equal(t, `The approval "Print" on "Cover" (#5) was rejected`, n.ToTitle("en"))
		opts, err := notifications.RenderMail(n.ToMail("en"), "en")
		require.NoError(t, err)
		assert.Contains(t, opts.Message, `Your approval request "Print" on the task "Cover" (#5) was rejected.`)
		assert.Contains(t, opts.Message, "Reason: too late")

		reminder := &ApprovalReminderNotification{Doer: apU1, Task: task, Project: &Project{ID: apProject}, Approval: info, Target: apU6}
		assert.Contains(t, reminder.ToTitle("en"), "Reminder")
		assert.EqualValues(t, apProject, reminder.ProjectID())
		assert.Equal(t, "approval.reminder", reminder.Name())
	})
}

func TestApprovals_ReminderCron(t *testing.T) {
	setup := func(t *testing.T, dueIn string) (*approvalEnv, *ApprovalRequest) {
		t.Helper()
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), func(doc *Workflow) {
			doc.Transitions[0].Approval.DueIn = dueIn
		})
		return env, env.request(t, apU1)
	}
	pastDue := func(t *testing.T, id int64) {
		t.Helper()
		setRaw(t, "approval_requests", builder.Eq{"id": id}, map[string]any{"due": time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05")})
	}

	t.Run("undecided approvers are reminded once when the due time passed", func(t *testing.T) {
		env, req := setup(t, "1d")
		_ = env
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		enableApprovalMail(t)

		sendApprovalReminders(time.Now())
		assert.Empty(t, approvalNotificationsOf(t, 2, "approval.reminder"), "not due yet")

		pastDue(t, req.ID)
		sendApprovalReminders(time.Now())
		rows := approvalNotificationsOf(t, 2, "approval.reminder")
		require.Len(t, rows, 1)
		assert.Empty(t, approvalNotificationsOf(t, 6, "approval.reminder"), "user6 decided already")
		assert.Empty(t, approvalNotificationsOf(t, 1, "approval.reminder"))
		assert.EqualValues(t, apProject, rows[0].ProjectID)
		sent := mail.SentMails()
		require.Len(t, sent, 1)
		assert.Contains(t, sent[0].Message, "still waiting for your decision")

		stored := &ApprovalRequest{}
		s := db.NewSession()
		_, err := s.ID(req.ID).Get(stored)
		require.NoError(t, err)
		_ = s.Close()
		require.NotNil(t, stored.RemindedAt)

		sendApprovalReminders(time.Now().Add(time.Hour))
		sendApprovalReminders(time.Now().Add(48 * time.Hour))
		assert.Len(t, approvalNotificationsOf(t, 2, "approval.reminder"), 1, "once per request")
	})

	t.Run("resolved, cancelled and requests without a due time are left alone", func(t *testing.T) {
		_, resolved := setup(t, "1d")
		mustDecide(t, resolved.ID, apU6, ApprovalDecisionRejected, "no")
		pastDue(t, resolved.ID)

		_, cancelled := setup(t, "1d")
		_, err := cancelAs(t, cancelled.ID, apU1, "")
		require.NoError(t, err)
		pastDue(t, cancelled.ID)

		_, noDue := setup(t, "")
		sendApprovalReminders(time.Now().Add(96 * time.Hour))
		for _, id := range []int64{6, 2} {
			assert.Empty(t, approvalNotificationsOf(t, id, "approval.reminder"))
		}
		_ = noDue
	})

	t.Run("an approver who lost access is not reminded", func(t *testing.T) {
		_, req := setup(t, "1d")
		pastDue(t, req.ID)
		setRaw(t, "users_projects", builder.And(builder.Eq{"user_id": 2}, builder.Eq{"project_id": apProject}), map[string]any{"project_id": 999})
		s := db.NewSession()
		_, err := s.Where("project_id = ?", apProject).And("team_id = ?", 1).Delete(&TeamProject{})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		sendApprovalReminders(time.Now())
		assert.Empty(t, approvalNotificationsOf(t, 2, "approval.reminder"))
		assert.Len(t, approvalNotificationsOf(t, 6, "approval.reminder"), 1)
	})
}
