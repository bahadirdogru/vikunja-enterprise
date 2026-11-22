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

type approvalRequests20261124100000 struct {
	ID               int64      `xorm:"bigint autoincr not null unique pk"`
	TaskID           int64      `xorm:"bigint not null INDEX"`
	ProjectID        int64      `xorm:"bigint not null INDEX"`
	TransitionID     *int64     `xorm:"bigint null"`
	FromStatusID     *int64     `xorm:"bigint null"`
	ToStatusID       *int64     `xorm:"bigint null"`
	RejectedStatusID *int64     `xorm:"bigint null"`
	Title            string     `xorm:"varchar(250) not null"`
	Description      string     `xorm:"longtext null"`
	Comment          string     `xorm:"longtext null"`
	Mode             string     `xorm:"varchar(10) not null"`
	Status           string     `xorm:"varchar(20) not null INDEX"`
	AllowSelf        bool       `xorm:"bool default false"`
	RequestedByID    int64      `xorm:"bigint not null INDEX"`
	Due              *time.Time `xorm:"DATETIME null"`
	RemindedAt       *time.Time `xorm:"DATETIME null"`
	ResolvedByID     int64      `xorm:"bigint not null default 0"`
	CancelReason     string     `xorm:"varchar(50) null"`
	CancelMessage    string     `xorm:"text null"`
	Created          time.Time  `xorm:"created not null"`
	Updated          time.Time  `xorm:"updated not null"`
	Resolved         *time.Time `xorm:"DATETIME null"`
}

func (approvalRequests20261124100000) TableName() string {
	return "approval_requests"
}

type approvalApprovers20261124100000 struct {
	ID        int64      `xorm:"bigint autoincr not null unique pk"`
	RequestID int64      `xorm:"bigint not null INDEX unique(approval_approvers_pair)"`
	UserID    int64      `xorm:"bigint not null INDEX unique(approval_approvers_pair)"`
	Decision  *string    `xorm:"varchar(20) null"`
	Comment   string     `xorm:"longtext null"`
	Decided   *time.Time `xorm:"DATETIME null"`
}

func (approvalApprovers20261124100000) TableName() string {
	return "approval_approvers"
}

type workflowTransitions20261124100000 struct {
	RequiresApproval bool           `xorm:"bool default false"`
	Approval         map[string]any `xorm:"json null"`
}

func (workflowTransitions20261124100000) TableName() string {
	return "workflow_transitions"
}

type taskStatusHistory20261124100000 struct {
	ApprovalRequestID *int64 `xorm:"bigint null"`
}

func (taskStatusHistory20261124100000) TableName() string {
	return "task_status_history"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261124100000",
		Description: "Add the approval tables and the approval columns of workflow transitions and the status history",
		Migrate: func(tx *xorm.Engine) error {
			if err := tx.Sync( //nolint:forbidigo // brand-new tables, nothing to drop
				approvalRequests20261124100000{},
				approvalApprovers20261124100000{},
			); err != nil {
				return err
			}
			return partialSync(tx,
				workflowTransitions20261124100000{},
				taskStatusHistory20261124100000{},
			)
		},
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(
				approvalRequests20261124100000{},
				approvalApprovers20261124100000{},
			)
		},
	})
}
