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

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/i18n"
	"code.vikunja.io/api/pkg/notifications"
	"code.vikunja.io/api/pkg/user"

	"github.com/ThreeDotsLabs/watermill/message"
	"xorm.io/xorm"
)

func init() {
	notifications.Register(func() notifications.PersistedNotification { return &FormSubmittedNotification{} })
}

// FormSubmittedEvent is fired when a form submission created its task. Doer is the form owner, the user the task
// was created as. It carries no token and no IP information.
type FormSubmittedEvent struct {
	FormID         int64      `json:"form_id"`
	FormTitle      string     `json:"form_title"`
	SubmissionID   int64      `json:"submission_id"`
	Task           *Task      `json:"task"`
	Doer           *user.User `json:"doer"`
	SubmitterID    *int64     `json:"submitter_id"`
	SubmitterName  string     `json:"submitter_name"`
	SubmitterEmail string     `json:"submitter_email"`
}

// Name defines the name for FormSubmittedEvent
func (e *FormSubmittedEvent) Name() string {
	return "form.submitted"
}

// FormSubmittedNotification tells a user who asked for it that a form was submitted.
type FormSubmittedNotification struct {
	FormID         int64      `json:"form_id"`
	FormTitle      string     `json:"form_title"`
	SubmitterName  string     `json:"submitter_name"`
	SubmitterEmail string     `json:"submitter_email"`
	Task           *Task      `json:"task"`
	Project        *Project   `json:"project"`
	Target         *user.User `json:"-"`
}

// ToTitle returns the translated one-line title for FormSubmittedNotification
func (n *FormSubmittedNotification) ToTitle(lang string) string {
	return i18n.T(lang, "notifications.form.submitted.subject", n.FormTitle, n.Task.Title, n.Task.GetFullIdentifier())
}

// ToMail returns the mail notification for FormSubmittedNotification
func (n *FormSubmittedNotification) ToMail(lang string) *notifications.Mail {
	return notifications.NewMail().
		Greeting(i18n.T(lang, "notifications.greeting", n.Target.GetName())).
		Line(i18n.T(lang, "notifications.form.submitted.message",
			notifications.EscapeMarkdown(n.FormTitle),
			notifications.EscapeMarkdown(n.Task.Title),
			notifications.EscapeMarkdown(n.Task.GetFullIdentifier()))).
		Action(i18n.T(lang, "notifications.common.actions.open_task"), n.Task.GetFrontendURL()).
		IncludeLinkToSettings(lang)
}

// ToDB returns the notification in a format which can be saved in the db
func (n *FormSubmittedNotification) ToDB() interface{} {
	return n
}

// Name returns the name of the notification
func (n *FormSubmittedNotification) Name() string {
	return "form.submitted"
}

// ThreadID returns the thread ID for email threading
func (n *FormSubmittedNotification) ThreadID() string {
	return getThreadID(n.Task.ID)
}

// ProjectID reports the project of the task.
func (n *FormSubmittedNotification) ProjectID() int64 {
	return notificationProjectID(n.Task, n.Project)
}

// SendFormSubmittedNotification represents a listener
type SendFormSubmittedNotification struct{}

// Name defines the name for the SendFormSubmittedNotification listener
func (l *SendFormSubmittedNotification) Name() string {
	return "form.submitted.notification.send"
}

// Handle is executed when form.submitted is fired: the users the form names are told, as far as they can still
// read the project.
func (l *SendFormSubmittedNotification) Handle(msg *message.Message) error {
	event := &FormSubmittedEvent{}
	if err := json.Unmarshal(msg.Payload, event); err != nil {
		return err
	}
	if event.Task == nil {
		return nil
	}

	sess := db.NewSession()
	defer sess.Close()

	form := &Form{}
	has, err := sess.ID(event.FormID).Get(form)
	if err != nil || !has {
		return err
	}
	normalizeForm(form)
	if len(form.Settings.NotifyUserIDs) == 0 {
		return nil
	}
	task, err := GetTaskByIDSimple(sess, event.Task.ID)
	if err != nil {
		if IsErrTaskDoesNotExist(err) {
			return nil
		}
		return err
	}
	project, err := GetProjectSimpleByID(sess, task.ProjectID)
	if err != nil {
		return err
	}
	task.setIdentifier(project)

	return notifyFormSubmitted(sess, form, event, &task, project)
}

func notifyFormSubmitted(sess *xorm.Session, form *Form, event *FormSubmittedEvent, task *Task, project *Project) error {
	for _, id := range form.Settings.NotifyUserIDs {
		target, err := notifiableApprover(sess, id)
		if err != nil {
			return err
		}
		if target == nil {
			continue
		}
		can, _, err := project.CanRead(sess, target)
		if err != nil || !can {
			continue
		}
		n := &FormSubmittedNotification{
			FormID:         form.ID,
			FormTitle:      form.Title,
			SubmitterName:  event.SubmitterName,
			SubmitterEmail: event.SubmitterEmail,
			Task:           task,
			Project:        project,
			Target:         target,
		}
		if err = notifications.Notify(target, n, sess); err != nil {
			_ = sess.Rollback()
			return err
		}
	}
	return sess.Commit()
}
