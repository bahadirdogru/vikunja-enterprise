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

type automationRules20261122140000 struct {
	ID                  int64     `xorm:"bigint autoincr not null unique pk"`
	ProjectID           int64     `xorm:"bigint not null INDEX"`
	Title               string    `xorm:"varchar(250) not null"`
	Description         string    `xorm:"longtext null"`
	Enabled             bool      `xorm:"bool default false"`
	Revision            int64     `xorm:"bigint not null default 1"`
	OwnerID             int64     `xorm:"bigint not null"`
	Trigger             string    `xorm:"json not null"`
	Conditions          string    `xorm:"json null"`
	Actions             string    `xorm:"json not null"`
	DisabledReason      string    `xorm:"varchar(100) null"`
	ConsecutiveFailures int64     `xorm:"bigint not null default 0"`
	LastRunAt           time.Time `xorm:"DATETIME null"`
	LastRunStatus       string    `xorm:"varchar(20) null"`
	Created             time.Time `xorm:"created not null"`
	Updated             time.Time `xorm:"updated not null"`
}

func (automationRules20261122140000) TableName() string {
	return "automation_rules"
}

type automationEvents20261122140000 struct {
	ID          int64     `xorm:"bigint autoincr not null unique pk"`
	ProjectID   int64     `xorm:"bigint not null INDEX"`
	TaskID      int64     `xorm:"bigint null"`
	Name        string    `xorm:"varchar(100) not null"`
	Payload     string    `xorm:"longtext not null"`
	Depth       int64     `xorm:"bigint not null default 0"`
	OriginRunID int64     `xorm:"bigint null"`
	Status      string    `xorm:"varchar(20) not null index(automation_events_poll)"`
	Attempts    int64     `xorm:"bigint not null default 0"`
	RunAfter    time.Time `xorm:"DATETIME not null index(automation_events_poll)"`
	LockedUntil time.Time `xorm:"DATETIME null"`
	LastError   string    `xorm:"text null"`
	Created     time.Time `xorm:"created not null"`
}

func (automationEvents20261122140000) TableName() string {
	return "automation_events"
}

type automationRuns20261122140000 struct {
	ID           int64     `xorm:"bigint autoincr not null unique pk"`
	RuleID       int64     `xorm:"bigint not null INDEX unique(automation_runs_event_rule)"`
	RuleRevision int64     `xorm:"bigint not null"`
	EventID      int64     `xorm:"bigint not null unique(automation_runs_event_rule)"`
	ProjectID    int64     `xorm:"bigint not null INDEX"`
	TaskID       int64     `xorm:"bigint null"`
	Status       string    `xorm:"varchar(20) not null"`
	SkipReason   string    `xorm:"varchar(100) null"`
	Error        string    `xorm:"text null"`
	Depth        int64     `xorm:"bigint not null default 0"`
	Started      time.Time `xorm:"DATETIME not null"`
	Finished     time.Time `xorm:"DATETIME null"`
}

func (automationRuns20261122140000) TableName() string {
	return "automation_runs"
}

type automationRunSteps20261122140000 struct {
	ID         int64     `xorm:"bigint autoincr not null unique pk"`
	RunID      int64     `xorm:"bigint not null INDEX unique(automation_run_steps_position)"`
	Position   int64     `xorm:"bigint not null unique(automation_run_steps_position)"`
	ActionType string    `xorm:"varchar(100) not null"`
	Status     string    `xorm:"varchar(20) not null"`
	Error      string    `xorm:"text null"`
	Output     string    `xorm:"text null"`
	DurationMs int64     `xorm:"bigint not null default 0"`
	Created    time.Time `xorm:"created not null"`
}

func (automationRunSteps20261122140000) TableName() string {
	return "automation_run_steps"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261122140000",
		Description: "Add the automation tables: rules, the event outbox, runs and run steps",
		Migrate: func(tx *xorm.Engine) error {
			return tx.Sync( //nolint:forbidigo // brand-new tables, nothing to drop
				automationRules20261122140000{},
				automationEvents20261122140000{},
				automationRuns20261122140000{},
				automationRunSteps20261122140000{},
			)
		},
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(
				automationRules20261122140000{},
				automationEvents20261122140000{},
				automationRuns20261122140000{},
				automationRunSteps20261122140000{},
			)
		},
	})
}
