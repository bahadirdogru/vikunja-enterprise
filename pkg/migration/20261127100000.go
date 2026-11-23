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

// The partial structs below only carry the new columns: Sync adds what is missing and leaves everything else of
// the existing tables alone.
type automationEvents20261127100000 struct {
	ID     int64 `xorm:"bigint autoincr not null unique pk"`
	RuleID int64 `xorm:"bigint null INDEX"`
}

func (automationEvents20261127100000) TableName() string {
	return "automation_events"
}

type automationRules20261127100000 struct {
	ID               int64     `xorm:"bigint autoincr not null unique pk"`
	LastFiredAt      time.Time `xorm:"DATETIME null"`
	LastFireNote     string    `xorm:"varchar(250) null"`
	WebhookTokenHash string    `xorm:"varchar(64) null INDEX"`
}

func (automationRules20261127100000) TableName() string {
	return "automation_rules"
}

type automationDueMarkers20261127100000 struct {
	ID      int64     `xorm:"bigint autoincr not null unique pk"`
	RuleID  int64     `xorm:"bigint not null unique(automation_due_markers_rule_task)"`
	TaskID  int64     `xorm:"bigint not null unique(automation_due_markers_rule_task) INDEX"`
	DueDate time.Time `xorm:"DATETIME not null"`
	Fired   time.Time `xorm:"DATETIME not null"`
}

func (automationDueMarkers20261127100000) TableName() string {
	return "automation_due_markers"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261127100000",
		Description: "Add the columns for manual, schedule, due and webhook triggers and the due markers table",
		Migrate: func(tx *xorm.Engine) error {
			// The existing tables only get their new columns and plain indexes: a plain Sync would drop every index
			// the partial structs do not declare.
			if err := partialSync(tx, automationEvents20261127100000{}, automationRules20261127100000{}); err != nil {
				return err
			}
			return tx.Sync( //nolint:forbidigo // a brand-new table, nothing to drop
				automationDueMarkers20261127100000{},
			)
		},
		Rollback: func(tx *xorm.Engine) error {
			// The added columns stay: they are nullable and harmless to older code.
			return tx.DropTables(automationDueMarkers20261127100000{})
		},
	})
}
