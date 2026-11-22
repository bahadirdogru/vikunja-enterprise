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

type dashboards20261122160000 struct {
	ID          int64     `xorm:"bigint autoincr not null unique pk"`
	Title       string    `xorm:"varchar(250) not null"`
	Description string    `xorm:"longtext null"`
	OwnerID     int64     `xorm:"bigint not null INDEX"`
	ProjectID   int64     `xorm:"bigint not null default 0 INDEX"`
	Position    float64   `xorm:"double not null default 0"`
	Created     time.Time `xorm:"created not null"`
	Updated     time.Time `xorm:"updated not null"`
}

func (dashboards20261122160000) TableName() string {
	return "dashboards"
}

type dashboardWidgets20261122160000 struct {
	ID          int64          `xorm:"bigint autoincr not null unique pk"`
	DashboardID int64          `xorm:"bigint not null INDEX"`
	Type        string         `xorm:"varchar(40) not null"`
	Title       string         `xorm:"varchar(250) null"`
	Config      map[string]any `xorm:"json null"`
	X           int64          `xorm:"bigint not null default 0"`
	Y           int64          `xorm:"bigint not null default 0"`
	W           int64          `xorm:"bigint not null default 4"`
	H           int64          `xorm:"bigint not null default 3"`
	Created     time.Time      `xorm:"created not null"`
	Updated     time.Time      `xorm:"updated not null"`
}

func (dashboardWidgets20261122160000) TableName() string {
	return "dashboard_widgets"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261122160000",
		Description: "Add dashboards and their widgets",
		Migrate: func(tx *xorm.Engine) error {
			return tx.Sync( //nolint:forbidigo // brand-new tables, nothing to drop
				dashboards20261122160000{},
				dashboardWidgets20261122160000{},
			)
		},
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(
				dashboards20261122160000{},
				dashboardWidgets20261122160000{},
			)
		},
	})
}
