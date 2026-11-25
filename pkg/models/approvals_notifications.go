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
	"time"

	"code.vikunja.io/api/pkg/cron"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/i18n"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/notifications"
	"code.vikunja.io/api/pkg/user"

	"github.com/ThreeDotsLabs/watermill/message"
	"xorm.io/builder"
	"xorm.io/xorm"
)

func init() {
	notifications.Register(func() notifications.PersistedNotification { return &ApprovalRequestedNotification{} })
	notifications.Register(func() notifications.PersistedNotification { return &ApprovalResolvedNotification{} })
	notifications.Register(func() notifications.PersistedNotification { return &ApprovalReminderNotification{} })
}

// ApprovalNotificationInfo is the part of a request that notifications keep. A stored notification must not grow
// with the approvers of the request.
type ApprovalNotificationInfo struct {
	ID            int64          `json:"id"`
	Title         string         `json:"title"`
	Status        ApprovalStatus `json:"status"`
	Due           *time.Time     `json:"due,omitempty"`
	ToStatusTitle string         `json:"to_status_title,omitempty"`
	CancelReason  string         `json:"cancel_reason,omitempty"`
	CancelMessage string         `json:"cancel_message,omitempty"`
}

func approvalNotificationInfo(req *ApprovalRequest) *ApprovalNotificationInfo {
	info := &ApprovalNotificationInfo{
		ID:            req.ID,
		Title:         req.Title,
		Status:        req.Status,
		Due:           req.Due,
		CancelReason:  req.CancelReason,
		CancelMessage: req.CancelMessage,
	}
	if req.ToStatus != nil {
		info.ToStatusTitle = req.ToStatus.Title
	}
	return info
}

func approvalOutcomeWord(lang string, status ApprovalStatus) string {
	return i18n.T(lang, "notifications.approval.outcome."+string(status))
}

// ApprovalRequestedNotification asks an approver to decide.
type ApprovalRequestedNotification struct {
	Doer     *user.User                `json:"doer"`
	Task     *Task                     `json:"task"`
	Project  *Project                  `json:"project"`
	Approval *ApprovalNotificationInfo `json:"approval"`
	Target   *user.User                `json:"-"`
}

// ToTitle returns the translated one-line title for ApprovalRequestedNotification
func (n *ApprovalRequestedNotification) ToTitle(lang string) string {
	return i18n.T(lang, "notifications.approval.requested.subject", n.Approval.Title, n.Task.Title, n.Task.GetFullIdentifier())
}

// ToMail returns the mail notification for ApprovalRequestedNotification
func (n *ApprovalRequestedNotification) ToMail(lang string) *notifications.Mail {
	mail := notifications.NewMail().
		From(n.Doer.GetNameAndFromEmail()).
		Greeting(i18n.T(lang, "notifications.greeting", n.Target.GetName())).
		Line(i18n.T(lang, "notifications.approval.requested.message",
			notifications.EscapeMarkdown(n.Doer.GetName()),
			notifications.EscapeMarkdown(n.Approval.Title),
			notifications.EscapeMarkdown(n.Task.Title),
			notifications.EscapeMarkdown(n.Task.GetFullIdentifier())))
	if n.Approval.Due != nil {
		mail.Line(i18n.T(lang, "notifications.approval.requested.due", n.Approval.Due.Format("2006-01-02 15:04")))
	}
	return mail.
		Action(i18n.T(lang, "notifications.common.actions.open_task"), n.Task.GetFrontendURL()).
		IncludeLinkToSettings(lang)
}

// ToDB returns the notification in a format which can be saved in the db
func (n *ApprovalRequestedNotification) ToDB() interface{} {
	return n
}

// Name returns the name of the notification
func (n *ApprovalRequestedNotification) Name() string {
	return "approval.requested"
}

// ThreadID returns the thread ID for email threading
func (n *ApprovalRequestedNotification) ThreadID() string {
	return getThreadID(n.Task.ID)
}

// ProjectID reports the project of the task, notifications of approvals are project scoped.
func (n *ApprovalRequestedNotification) ProjectID() int64 {
	return notificationProjectID(n.Task, n.Project)
}

// ApprovalResolvedNotification tells the requester, and the approvers who are no longer needed, that a request
// ended.
type ApprovalResolvedNotification struct {
	// Doer is who ended the request, empty when the system did.
	Doer     *user.User                `json:"doer,omitempty"`
	Task     *Task                     `json:"task"`
	Project  *Project                  `json:"project"`
	Approval *ApprovalNotificationInfo `json:"approval"`
	// ToRequester is true for the notification of the requester.
	ToRequester bool       `json:"to_requester"`
	Target      *user.User `json:"-"`
}

// ToTitle returns the translated one-line title for ApprovalResolvedNotification
func (n *ApprovalResolvedNotification) ToTitle(lang string) string {
	return i18n.T(lang, "notifications.approval.resolved.subject",
		n.Approval.Title, n.Task.Title, n.Task.GetFullIdentifier(), approvalOutcomeWord(lang, n.Approval.Status))
}

// ToMail returns the mail notification for ApprovalResolvedNotification
func (n *ApprovalResolvedNotification) ToMail(lang string) *notifications.Mail {
	outcome := approvalOutcomeWord(lang, n.Approval.Status)
	title := notifications.EscapeMarkdown(n.Approval.Title)
	taskTitle := notifications.EscapeMarkdown(n.Task.Title)
	identifier := notifications.EscapeMarkdown(n.Task.GetFullIdentifier())

	mail := notifications.NewMail().
		Greeting(i18n.T(lang, "notifications.greeting", n.Target.GetName()))
	if n.Doer != nil {
		mail.From(n.Doer.GetNameAndFromEmail())
	}
	switch {
	case n.ToRequester:
		mail.Line(i18n.T(lang, "notifications.approval.resolved.message_requester", title, taskTitle, identifier, outcome))
	case n.Doer != nil:
		mail.Line(i18n.T(lang, "notifications.approval.resolved.message_others", title, taskTitle, identifier, outcome, notifications.EscapeMarkdown(n.Doer.GetName())))
	default:
		mail.Line(i18n.T(lang, "notifications.approval.resolved.message_others_system", title, taskTitle, identifier, outcome))
	}
	if n.Approval.CancelMessage != "" {
		mail.Line(i18n.T(lang, "notifications.approval.resolved.reason", notifications.EscapeMarkdown(n.Approval.CancelMessage)))
	}
	return mail.
		Action(i18n.T(lang, "notifications.common.actions.open_task"), n.Task.GetFrontendURL()).
		IncludeLinkToSettings(lang)
}

// ToDB returns the notification in a format which can be saved in the db
func (n *ApprovalResolvedNotification) ToDB() interface{} {
	return n
}

// Name returns the name of the notification
func (n *ApprovalResolvedNotification) Name() string {
	return "approval.resolved"
}

// ThreadID returns the thread ID for email threading
func (n *ApprovalResolvedNotification) ThreadID() string {
	return getThreadID(n.Task.ID)
}

// ProjectID reports the project of the task.
func (n *ApprovalResolvedNotification) ProjectID() int64 {
	return notificationProjectID(n.Task, n.Project)
}

// ApprovalReminderNotification reminds an approver who has not decided that the request is overdue.
type ApprovalReminderNotification struct {
	Doer     *user.User                `json:"doer"`
	Task     *Task                     `json:"task"`
	Project  *Project                  `json:"project"`
	Approval *ApprovalNotificationInfo `json:"approval"`
	Target   *user.User                `json:"-"`
}

// ToTitle returns the translated one-line title for ApprovalReminderNotification
func (n *ApprovalReminderNotification) ToTitle(lang string) string {
	return i18n.T(lang, "notifications.approval.reminder.subject", n.Approval.Title, n.Task.Title, n.Task.GetFullIdentifier())
}

// ToMail returns the mail notification for ApprovalReminderNotification
func (n *ApprovalReminderNotification) ToMail(lang string) *notifications.Mail {
	due := ""
	if n.Approval.Due != nil {
		due = n.Approval.Due.Format("2006-01-02 15:04")
	}
	return notifications.NewMail().
		Greeting(i18n.T(lang, "notifications.greeting", n.Target.GetName())).
		Line(i18n.T(lang, "notifications.approval.reminder.message",
			notifications.EscapeMarkdown(n.Approval.Title),
			notifications.EscapeMarkdown(n.Doer.GetName()),
			notifications.EscapeMarkdown(n.Task.Title),
			notifications.EscapeMarkdown(n.Task.GetFullIdentifier()),
			due)).
		Action(i18n.T(lang, "notifications.common.actions.open_task"), n.Task.GetFrontendURL()).
		IncludeLinkToSettings(lang)
}

// ToDB returns the notification in a format which can be saved in the db
func (n *ApprovalReminderNotification) ToDB() interface{} {
	return n
}

// Name returns the name of the notification
func (n *ApprovalReminderNotification) Name() string {
	return "approval.reminder"
}

// ThreadID returns the thread ID for email threading
func (n *ApprovalReminderNotification) ThreadID() string {
	return getThreadID(n.Task.ID)
}

// ProjectID reports the project of the task.
func (n *ApprovalReminderNotification) ProjectID() int64 {
	return notificationProjectID(n.Task, n.Project)
}

// loadApprovalForNotification reloads the request, its task and project from the database: events are processed
// after the commit, and the payload of the event is not what a notification should rely on.
func loadApprovalForNotification(s *xorm.Session, requestID int64) (*ApprovalRequest, *Task, *Project, error) {
	req, err := getApprovalRequest(s, requestID, false)
	if err != nil {
		if IsErrApprovalRequestDoesNotExist(err) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, err
	}
	if err = hydrateApprovalRequest(s, req); err != nil {
		return nil, nil, nil, err
	}
	task, err := GetTaskByIDSimple(s, req.TaskID)
	if err != nil {
		if IsErrTaskDoesNotExist(err) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, err
	}
	project, err := GetProjectSimpleByID(s, task.ProjectID)
	if err != nil {
		return nil, nil, nil, err
	}
	task.setIdentifier(project)
	return req, &task, project, nil
}

// notifiableApprover loads an approver with the e-mail address and settings a notification needs.
func notifiableApprover(s *xorm.Session, id int64) (*user.User, error) {
	u, err := user.GetUserWithEmail(s, &user.User{ID: id})
	if err != nil {
		if user.IsErrUserDoesNotExist(err) || user.IsErrUserStatusError(err) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

// SendApprovalRequestedNotification represents a listener
type SendApprovalRequestedNotification struct{}

// Name defines the name for the SendApprovalRequestedNotification listener
func (l *SendApprovalRequestedNotification) Name() string {
	return "approval.requested.notification.send"
}

// Handle is executed when approval.requested is fired: every approver who has not decided is asked.
func (l *SendApprovalRequestedNotification) Handle(msg *message.Message) error {
	event := &ApprovalRequestedEvent{}
	if err := json.Unmarshal(msg.Payload, event); err != nil {
		return err
	}
	if event.Approval == nil {
		return nil
	}

	sess := db.NewSession()
	defer sess.Close()

	req, task, project, err := loadApprovalForNotification(sess, event.Approval.ID)
	if err != nil {
		return err
	}
	if req == nil || req.Status != ApprovalStatusPending {
		return nil
	}
	doer := req.RequestedBy
	if doer == nil {
		return nil
	}

	info := approvalNotificationInfo(req)
	for _, approver := range req.Approvers {
		if approver.Decision != nil {
			continue
		}
		target, err := notifiableApprover(sess, approver.UserID)
		if err != nil {
			return err
		}
		if target == nil {
			continue
		}
		n := &ApprovalRequestedNotification{Doer: doer, Task: task, Project: project, Approval: info, Target: target}
		if err = notifications.Notify(target, n, sess); err != nil {
			_ = sess.Rollback()
			return err
		}
	}
	return sess.Commit()
}

// SendApprovalResolvedNotification represents a listener
type SendApprovalResolvedNotification struct{}

// Name defines the name for the SendApprovalResolvedNotification listener
func (l *SendApprovalResolvedNotification) Name() string {
	return "approval.resolved.notification.send"
}

// Handle is executed when approval.resolved is fired: the requester learns the outcome, and the approvers who did
// not decide learn that they are no longer needed. Whoever ended the request is not told about it.
func (l *SendApprovalResolvedNotification) Handle(msg *message.Message) error {
	event := &ApprovalResolvedEvent{}
	if err := json.Unmarshal(msg.Payload, event); err != nil {
		return err
	}
	if event.Approval == nil {
		return nil
	}

	sess := db.NewSession()
	defer sess.Close()

	req, task, project, err := loadApprovalForNotification(sess, event.Approval.ID)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	// A cancelled approved request is reported as cancelled, its state is the truth.
	info := approvalNotificationInfo(req)

	// The payload of the event is not what a notification should rely on: take the person from the database.
	var doerID int64
	doer := event.Doer
	if doer != nil {
		doerID = doer.ID
		if fresh, err := user.GetUserByID(sess, doer.ID); err == nil {
			doer = fresh
		}
	}
	notified := map[int64]bool{doerID: true}

	if !notified[req.RequestedByID] {
		notified[req.RequestedByID] = true
		if target, err := notifiableApprover(sess, req.RequestedByID); err != nil {
			return err
		} else if target != nil {
			n := &ApprovalResolvedNotification{Doer: doer, Task: task, Project: project, Approval: info, ToRequester: true, Target: target}
			if err = notifications.Notify(target, n, sess); err != nil {
				_ = sess.Rollback()
				return err
			}
		}
	}
	for _, approver := range req.Approvers {
		if approver.Decision != nil || notified[approver.UserID] {
			continue
		}
		notified[approver.UserID] = true
		target, err := notifiableApprover(sess, approver.UserID)
		if err != nil {
			return err
		}
		if target == nil {
			continue
		}
		n := &ApprovalResolvedNotification{Doer: doer, Task: task, Project: project, Approval: info, Target: target}
		if err = notifications.Notify(target, n, sess); err != nil {
			_ = sess.Rollback()
			return err
		}
	}
	return sess.Commit()
}

// RegisterApprovalReminderCron registers the job that reminds approvers of requests whose due time passed, and the
// one that repairs the slots of deactivated approvers.
func RegisterApprovalReminderCron() {
	err := cron.Schedule("*/5 * * * *", func() { sendApprovalReminders(time.Now()) })
	if err != nil {
		log.Fatalf("Could not register the approval reminder cron: %s", err)
	}
	// The repair of the approver slots of deactivated accounts runs in the same place, see repairApprovalApprovers.
	if err = cron.Schedule("*/5 * * * *", repairApprovalApprovers); err != nil {
		log.Fatalf("Could not register the approval repair cron: %s", err)
	}
}

// sendApprovalReminders notifies the undecided approvers of every pending request that is overdue, once per
// request: the claim is a conditional update of reminded_at.
func sendApprovalReminders(now time.Time) {
	const logPrefix = "[Approval Reminder] "

	s := db.NewSession()
	candidates := []int64{}
	err := s.Table("approval_requests").
		Where(builder.And(
			builder.Eq{"status": ApprovalStatusPending},
			builder.NotNull{"due"},
			builder.Lt{"due": now.UTC().Format(dbTimeFormat)},
			builder.IsNull{"reminded_at"},
		)).
		OrderBy("id ASC").
		Cols("id").
		Find(&candidates)
	_ = s.Close()
	if err != nil {
		log.Errorf(logPrefix+"Could not get overdue requests: %s", err)
		return
	}

	for _, id := range candidates {
		if err := remindApprovers(id, now); err != nil {
			log.Errorf(logPrefix+"Could not remind the approvers of request %d: %s", id, err)
		}
	}
}

func remindApprovers(requestID int64, now time.Time) error {
	s := db.NewSession()
	defer s.Close()

	claimed, err := s.Where(builder.And(
		builder.Eq{"id": requestID},
		builder.Eq{"status": ApprovalStatusPending},
		builder.IsNull{"reminded_at"},
	)).Cols("reminded_at").NoAutoTime().Update(&ApprovalRequest{RemindedAt: &now})
	if err != nil {
		_ = s.Rollback()
		return err
	}
	if claimed != 1 {
		return s.Commit()
	}

	req, task, project, err := loadApprovalForNotification(s, requestID)
	if err != nil {
		_ = s.Rollback()
		return err
	}
	if req == nil || req.RequestedBy == nil {
		return s.Commit()
	}

	undecided := []int64{}
	for _, approver := range req.Approvers {
		if approver.Decision == nil {
			undecided = append(undecided, approver.UserID)
		}
	}
	allowed, err := usersWhoMayReadProject(s, req.ProjectID, undecided)
	if err != nil {
		_ = s.Rollback()
		return err
	}
	info := approvalNotificationInfo(req)
	for _, a := range allowed {
		target, err := notifiableApprover(s, a.ID)
		if err != nil {
			_ = s.Rollback()
			return err
		}
		if target == nil {
			continue
		}
		n := &ApprovalReminderNotification{Doer: req.RequestedBy, Task: task, Project: project, Approval: info, Target: target}
		if err = notifications.Notify(target, n, s); err != nil {
			_ = s.Rollback()
			return err
		}
	}
	return s.Commit()
}
