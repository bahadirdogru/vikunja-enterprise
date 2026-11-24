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
	"time"

	"xorm.io/xorm"
)

// AutomationWebhookSeen is the signature of an incoming webhook call that was accepted. It lives in the database,
// not in memory, because several instances can serve one database: whichever instance gets the replay must know.
// The unique index on (rule, signature) is what makes two concurrent identical calls race safely. A row is only
// needed until a call with its timestamp would be refused as stale. The row stores the timestamp the sender signed
// (not a precomputed expiry), so the cleanup uses the tolerance of the instance that runs it, plus a margin.
type AutomationWebhookSeen struct {
	ID        int64     `xorm:"bigint autoincr not null unique pk" json:"id"`
	RuleID    int64     `xorm:"bigint not null unique(rule_sig)" json:"rule_id"`
	Signature string    `xorm:"varchar(64) not null unique(rule_sig)" json:"signature"`
	SignedAt  time.Time `xorm:"DATETIME not null INDEX" json:"signed_at"`
}

// TableName returns the table name of the seen webhook signatures.
func (*AutomationWebhookSeen) TableName() string {
	return "automation_webhook_seen"
}

// RememberAutomationWebhookSignature stores an accepted signature in the session of the call, so the memory and the
// queued event commit or roll back together: a call that is refused later (payload, rate limit) does not use up its
// signature. It reports replayed=true when the rule already has the signature. The insert of a duplicate fails on
// the unique index (the second of two concurrent calls waits for the first to commit); the failing session is
// not used afterwards, the caller rolls it back, and the existence is looked up in a new one because PostgreSQL
// aborts a transaction after a failed statement.
func RememberAutomationWebhookSignature(s *xorm.Session, ruleID int64, signature string, signedAt time.Time) (replayed bool, err error) {
	// Most replays are not concurrent: a plain lookup avoids a failing statement in the common case.
	has, err := s.Where("rule_id = ? AND signature = ?", ruleID, signature).Exist(&AutomationWebhookSeen{})
	if err != nil {
		return false, err
	}
	if has {
		return true, nil
	}
	if _, err = s.Insert(&AutomationWebhookSeen{RuleID: ruleID, Signature: signature, SignedAt: signedAt}); err != nil {
		return false, err
	}
	return false, nil
}

// AutomationWebhookSignatureSeen reports whether the rule has a stored signature, in a session of its own.
func AutomationWebhookSignatureSeen(s *xorm.Session, ruleID int64, signature string) (bool, error) {
	return s.Where("rule_id = ? AND signature = ?", ruleID, signature).Exist(&AutomationWebhookSeen{})
}

// CleanupAutomationWebhookSeen deletes the signatures that were signed before the given time and returns how many.
func CleanupAutomationWebhookSeen(s *xorm.Session, signedBefore time.Time) (int64, error) {
	return s.Where("signed_at < ?", signedBefore.UTC().Format(dbTimeFormat)).Delete(&AutomationWebhookSeen{})
}
