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

package migration

import (
	"time"

	"src.techknowlogick.com/xormigrate"
	"xorm.io/xorm"
)

// automationWebhookSeen20261128100000 remembers the signatures of the incoming webhook calls that were accepted,
// until a call signed at that time would be refused as stale anyway (the cleanup works from the signing time), so an exact replay is refused on every
// replica. The unique index is the guard: it is what two concurrent identical calls race on.
type automationWebhookSeen20261128100000 struct {
	ID int64 `xorm:"bigint autoincr not null unique pk"`
	// The index name is short on purpose: xorm prefixes it with UQE_ and the table name, and MySQL allows 64 characters.
	RuleID    int64     `xorm:"bigint not null unique(rule_sig)"`
	Signature string    `xorm:"varchar(64) not null unique(rule_sig)"`
	SignedAt  time.Time `xorm:"DATETIME not null INDEX"`
}

func (automationWebhookSeen20261128100000) TableName() string {
	return "automation_webhook_seen"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261128100000",
		Description: "Add the table that remembers accepted incoming webhook signatures (replay protection)",
		Migrate: func(tx *xorm.Engine) error {
			return tx.Sync(automationWebhookSeen20261128100000{}) //nolint:forbidigo // a brand-new table, nothing to drop
		},
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(automationWebhookSeen20261128100000{})
		},
	})
}
