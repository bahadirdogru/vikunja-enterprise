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
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

const automationWebhookTokenBytes = 32

// AutomationDueMarker remembers for which due date a rule fired for a task, so an overdue or due soon rule fires
// once per task and due date: changing the due date arms it again.
type AutomationDueMarker struct {
	ID      int64     `xorm:"bigint autoincr not null unique pk" json:"id"`
	RuleID  int64     `xorm:"bigint not null unique(automation_due_markers_rule_task)" json:"rule_id"`
	TaskID  int64     `xorm:"bigint not null unique(automation_due_markers_rule_task) INDEX" json:"task_id"`
	DueDate time.Time `xorm:"DATETIME not null" json:"due_date"`
	Fired   time.Time `xorm:"DATETIME not null" json:"fired"`
}

// TableName returns the table name of the due markers.
func (*AutomationDueMarker) TableName() string {
	return "automation_due_markers"
}

// AutomationTaskOrigin remembers which run created a task. The time based triggers (overdue, due soon, schedule)
// start their runs from the clock instead of from an event, so the origin of the events the outbox hook writes does
// not reach them: without this a rule that creates tasks which are overdue themselves would feed itself. An event
// started for such a task carries the run as its origin and one more level of depth, so the loop guards (a rule does
// not react to what its own run caused; the maximum depth) apply to it like to every other chain.
type AutomationTaskOrigin struct {
	ID     int64 `xorm:"bigint autoincr not null unique pk" json:"id"`
	TaskID int64 `xorm:"bigint not null unique" json:"task_id"`
	RunID  int64 `xorm:"bigint not null INDEX" json:"run_id"`
	// Depth is the depth of the events that start for the task.
	Depth   int64     `xorm:"bigint not null default 0" json:"depth"`
	Created time.Time `xorm:"created not null" json:"created"`
}

// TableName returns the table name of the task origins.
func (*AutomationTaskOrigin) TableName() string {
	return "automation_task_origins"
}

// HashAutomationWebhookToken is what is stored of the secret in a webhook URL.
func HashAutomationWebhookToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newAutomationWebhookToken() (string, error) {
	b := make([]byte, automationWebhookTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AutomationWebhookURL is the public URL of an incoming webhook with the given token.
func AutomationWebhookURL(token string) string {
	base := config.ServicePublicURL.GetString()
	if base != "" && !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + "api/v2/automation/hooks/" + token
}

// fillComputed sets the fields that are not stored.
func (r *AutomationRule) fillComputed() {
	r.HasWebhook = r.WebhookTokenHash != ""
}

// setWebhookToken replaces the token of the rule; the new secret is returned in WebhookToken only.
func (r *AutomationRule) setWebhookToken() error {
	token, err := newAutomationWebhookToken()
	if err != nil {
		return err
	}
	r.WebhookTokenHash = HashAutomationWebhookToken(token)
	r.WebhookToken = token
	r.WebhookURL = AutomationWebhookURL(token)
	return nil
}

// ensureWebhookToken gives a rule with the webhook trigger a token when it has none and takes it away from a rule
// that has another trigger.
func (r *AutomationRule) ensureWebhookToken() error {
	if r.Trigger.Type != spec.TriggerWebhookIncoming {
		r.WebhookTokenHash = ""
		return nil
	}
	if r.WebhookTokenHash == "" {
		return r.setWebhookToken()
	}
	return nil
}

// validAutomationWebhookToken reports whether a string has the shape of a generated token: 32 random bytes as
// unpadded base64url.
func validAutomationWebhookToken(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(automationWebhookTokenBytes) {
		return false
	}
	for _, c := range token {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// GetAutomationRuleByWebhookToken finds the rule a webhook token belongs to. nil when there is none.
func GetAutomationRuleByWebhookToken(s *xorm.Session, token string) (*AutomationRule, error) {
	// Anything that is not shaped like a token we generate never reaches the database.
	if !validAutomationWebhookToken(token) {
		return nil, nil
	}
	hash := HashAutomationWebhookToken(token)
	rule := &AutomationRule{}
	has, err := s.Where("webhook_token_hash = ?", hash).Get(rule)
	if err != nil || !has {
		return nil, err
	}
	// The lookup is by the hash of the token, so no byte of the stored secret is compared early; this is the final
	// check in constant time anyway.
	if subtle.ConstantTimeCompare([]byte(rule.WebhookTokenHash), []byte(hash)) != 1 {
		return nil, nil
	}
	return rule, nil
}

// AutomationWebhookToken is the action that replaces the secret URL of an incoming webhook.
type AutomationWebhookToken struct {
	ID        int64 `json:"-" param:"rule"`
	ProjectID int64 `json:"-" param:"project"`
	// Token is the new secret.
	Token string `json:"token" readOnly:"true" doc:"The new secret of the webhook URL. The old URL stops working. It is only shown now: the server stores a hash."`
	URL   string `json:"url" readOnly:"true" doc:"The new webhook URL."`

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

// CanCreate needs admin access to the project and a rule with the webhook trigger in it.
func (t *AutomationWebhookToken) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	can, _, err := canAdministerAutomation(s, a, t.ProjectID)
	if err != nil || !can {
		return false, err
	}
	rule, err := getAutomationRuleInProject(s, t.ID, t.ProjectID)
	if err != nil {
		return false, err
	}
	if rule.Trigger.Type != spec.TriggerWebhookIncoming {
		return false, ErrAutomationRuleNotRunnable{RuleID: rule.ID, Reason: "the rule has no incoming webhook trigger"}
	}
	return true, nil
}

// Create generates a new token. The old one is gone for good.
// @Summary Regenerate the webhook URL of a rule
func (t *AutomationWebhookToken) Create(s *xorm.Session, _ web.Auth) error {
	rule := &AutomationRule{ID: t.ID}
	if err := rule.setWebhookToken(); err != nil {
		return err
	}
	if _, err := s.ID(t.ID).Cols("webhook_token_hash").Update(rule); err != nil {
		return err
	}
	t.Token = rule.WebhookToken
	t.URL = rule.WebhookURL
	return nil
}

// AutomationManualRule is what a person who can edit tasks sees of a manual rule: enough to show the button.
type AutomationManualRule struct {
	ID    int64  `json:"id" doc:"The id of the rule."`
	Title string `json:"title" doc:"The title of the rule."`
	Label string `json:"label" doc:"The text of the button: the label option of the trigger, the title of the rule when it has none."`
}

// ListAutomationManualRules returns the enabled rules with the manual trigger of a project. Needs write access to
// the project; the rules are only listed by title, never their conditions or actions.
func ListAutomationManualRules(s *xorm.Session, a web.Auth, projectID int64) ([]*AutomationManualRule, error) {
	can, err := (&Project{ID: projectID}).CanWrite(s, a)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, ErrGenericForbidden{}
	}
	rules := []*AutomationRule{}
	if err = s.Where("project_id = ? AND enabled = ?", projectID, true).OrderBy("id ASC").Cols("id", "title", "trigger").Find(&rules); err != nil {
		return nil, err
	}
	out := []*AutomationManualRule{}
	for _, rule := range rules {
		if rule.Trigger.Type != spec.TriggerManual {
			continue
		}
		label, _ := rule.Trigger.Options["label"].(string)
		if strings.TrimSpace(label) == "" {
			label = rule.Title
		}
		out = append(out, &AutomationManualRule{ID: rule.ID, Title: rule.Title, Label: label})
	}
	return out, nil
}
