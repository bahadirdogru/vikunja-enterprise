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
	"code.vikunja.io/api/pkg/i18n"
	"code.vikunja.io/api/pkg/notifications"
	"code.vikunja.io/api/pkg/user"
)

func init() {
	notifications.Register(func() notifications.PersistedNotification { return &AutomationNotifyNotification{} })
}

// AutomationNotifyNotification is what the notify action of an automation rule sends: a text written in the rule,
// about a task or about the project of the rule.
type AutomationNotifyNotification struct {
	// Message is the text of the notification, with the placeholders of the rule already replaced.
	Message string `json:"message"`
	// RuleID and RuleTitle say which rule sent it.
	RuleID    int64      `json:"rule_id"`
	RuleTitle string     `json:"rule_title"`
	Task      *Task      `json:"task,omitempty"`
	Project   *Project   `json:"project"`
	Target    *user.User `json:"-"`
}

// ToTitle returns the one-line title: the message.
func (n *AutomationNotifyNotification) ToTitle(_ string) string {
	return n.Message
}

// ToMail returns the mail notification for AutomationNotifyNotification
func (n *AutomationNotifyNotification) ToMail(lang string) *notifications.Mail {
	mail := notifications.NewMail().
		Subject(n.Message).
		Greeting(i18n.T(lang, "notifications.greeting", n.Target.GetName())).
		Line(notifications.EscapeMarkdown(n.Message))
	if n.Task != nil {
		mail.Action(i18n.T(lang, "notifications.common.actions.open_task"), n.Task.GetFrontendURL())
	}
	return mail.IncludeLinkToSettings(lang)
}

// ToDB returns the notification in a format which can be saved in the db
func (n *AutomationNotifyNotification) ToDB() interface{} {
	return n
}

// Name returns the name of the notification
func (n *AutomationNotifyNotification) Name() string {
	return "automation.notify"
}

// ThreadID returns the thread ID for email threading
func (n *AutomationNotifyNotification) ThreadID() string {
	if n.Task != nil {
		return getThreadID(n.Task.ID)
	}
	return ""
}

// ProjectID reports the project of the rule: notifications are project scoped.
func (n *AutomationNotifyNotification) ProjectID() int64 {
	return notificationProjectID(n.Task, n.Project)
}
