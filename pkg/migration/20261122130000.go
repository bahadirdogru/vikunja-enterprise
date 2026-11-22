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

type workflows20261122130000 struct {
	ID                  int64     `xorm:"bigint autoincr not null unique pk"`
	ProjectID           int64     `xorm:"bigint not null unique"`
	RestrictTransitions bool      `xorm:"bool default false"`
	Created             time.Time `xorm:"created not null"`
	Updated             time.Time `xorm:"updated not null"`
}

func (workflows20261122130000) TableName() string {
	return "workflows"
}

type workflowStatuses20261122130000 struct {
	ID         int64   `xorm:"bigint autoincr not null unique pk"`
	WorkflowID int64   `xorm:"bigint not null INDEX"`
	Title      string  `xorm:"varchar(250) not null"`
	Color      string  `xorm:"varchar(7) null"`
	Category   string  `xorm:"varchar(20) not null"`
	Position   float64 `xorm:"double not null"`
	IsInitial  bool    `xorm:"bool default false"`
	Archived   bool    `xorm:"bool default false"`
}

func (workflowStatuses20261122130000) TableName() string {
	return "workflow_statuses"
}

type workflowTransitions20261122130000 struct {
	ID             int64  `xorm:"bigint autoincr not null unique pk"`
	WorkflowID     int64  `xorm:"bigint not null INDEX unique(workflow_transitions_pair)"`
	FromStatusID   int64  `xorm:"bigint not null unique(workflow_transitions_pair)"`
	ToStatusID     int64  `xorm:"bigint not null unique(workflow_transitions_pair)"`
	Title          string `xorm:"varchar(250) null"`
	MinPermission  string `xorm:"varchar(10) not null"`
	RequireComment bool   `xorm:"bool default false"`
}

func (workflowTransitions20261122130000) TableName() string {
	return "workflow_transitions"
}

type workflowTransitionRequiredFields20261122130000 struct {
	ID           int64  `xorm:"bigint autoincr not null unique pk"`
	TransitionID int64  `xorm:"bigint not null INDEX"`
	Field        string `xorm:"varchar(100) not null"`
}

func (workflowTransitionRequiredFields20261122130000) TableName() string {
	return "workflow_transition_required_fields"
}

type workflowTransitionTeams20261122130000 struct {
	ID           int64 `xorm:"bigint autoincr not null unique pk"`
	TransitionID int64 `xorm:"bigint not null INDEX unique(workflow_transition_teams_pair)"`
	TeamID       int64 `xorm:"bigint not null unique(workflow_transition_teams_pair)"`
}

func (workflowTransitionTeams20261122130000) TableName() string {
	return "workflow_transition_teams"
}

type taskStatuses20261122130000 struct {
	ID         int64     `xorm:"bigint autoincr not null unique pk"`
	TaskID     int64     `xorm:"bigint not null unique"`
	WorkflowID int64     `xorm:"bigint not null INDEX"`
	StatusID   int64     `xorm:"bigint not null INDEX"`
	Entered    time.Time `xorm:"not null"`
}

func (taskStatuses20261122130000) TableName() string {
	return "task_statuses"
}

type taskStatusHistory20261122130000 struct {
	ID           int64     `xorm:"bigint autoincr not null unique pk"`
	TaskID       int64     `xorm:"bigint not null INDEX"`
	FromStatusID *int64    `xorm:"bigint null"`
	ToStatusID   int64     `xorm:"bigint not null"`
	FromTitle    string    `xorm:"varchar(250) null"`
	ToTitle      string    `xorm:"varchar(250) not null"`
	DoerID       int64     `xorm:"bigint not null default 0"`
	Comment      string    `xorm:"text null"`
	Created      time.Time `xorm:"created not null"`
}

func (taskStatusHistory20261122130000) TableName() string {
	return "task_status_history"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261122130000",
		Description: "Add the workflow tables: workflows, statuses, transitions, task statuses and status history",
		Migrate: func(tx *xorm.Engine) error {
			return tx.Sync( //nolint:forbidigo // brand-new tables, nothing to drop
				workflows20261122130000{},
				workflowStatuses20261122130000{},
				workflowTransitions20261122130000{},
				workflowTransitionRequiredFields20261122130000{},
				workflowTransitionTeams20261122130000{},
				taskStatuses20261122130000{},
				taskStatusHistory20261122130000{},
			)
		},
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(
				workflows20261122130000{},
				workflowStatuses20261122130000{},
				workflowTransitions20261122130000{},
				workflowTransitionRequiredFields20261122130000{},
				workflowTransitionTeams20261122130000{},
				taskStatuses20261122130000{},
				taskStatusHistory20261122130000{},
			)
		},
	})
}
