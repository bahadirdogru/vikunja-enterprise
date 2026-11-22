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

type customFields20261122120000 struct {
	ID          int64          `xorm:"bigint autoincr not null unique pk"`
	ProjectID   int64          `xorm:"bigint not null INDEX"`
	Title       string         `xorm:"varchar(250) not null"`
	Description string         `xorm:"text null"`
	Type        string         `xorm:"varchar(30) not null"`
	Required    bool           `xorm:"bool default false"`
	Config      map[string]any `xorm:"json null"`
	Position    float64        `xorm:"double not null"`
	CreatedByID int64          `xorm:"bigint not null"`
	Created     time.Time      `xorm:"created not null"`
	Updated     time.Time      `xorm:"updated not null"`
}

func (customFields20261122120000) TableName() string {
	return "custom_fields"
}

type customFieldOptions20261122120000 struct {
	ID            int64   `xorm:"bigint autoincr not null unique pk"`
	CustomFieldID int64   `xorm:"bigint not null INDEX"`
	Title         string  `xorm:"varchar(250) not null"`
	Color         string  `xorm:"varchar(7) null"`
	Position      float64 `xorm:"double not null"`
}

func (customFieldOptions20261122120000) TableName() string {
	return "custom_field_options"
}

type taskCustomFieldValues20261122120000 struct {
	ID            int64      `xorm:"bigint autoincr not null unique pk"`
	TaskID        int64      `xorm:"bigint not null INDEX"`
	CustomFieldID int64      `xorm:"bigint not null INDEX"`
	ValueText     *string    `xorm:"text null"`
	ValueNumber   *float64   `xorm:"double null"`
	ValueDate     *time.Time `xorm:"DATETIME null"`
	ValueBool     *bool      `xorm:"bool null"`
	OptionID      *int64     `xorm:"bigint null INDEX"`
	UserID        *int64     `xorm:"bigint null INDEX"`
	Created       time.Time  `xorm:"created not null"`
	Updated       time.Time  `xorm:"updated not null"`
}

func (taskCustomFieldValues20261122120000) TableName() string {
	return "task_custom_field_values"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261122120000",
		Description: "Add custom_fields, custom_field_options and task_custom_field_values tables",
		Migrate: func(tx *xorm.Engine) error {
			return tx.Sync( //nolint:forbidigo // brand-new tables, nothing to drop
				customFields20261122120000{},
				customFieldOptions20261122120000{},
				taskCustomFieldValues20261122120000{},
			)
		},
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(
				customFields20261122120000{},
				customFieldOptions20261122120000{},
				taskCustomFieldValues20261122120000{},
			)
		},
	})
}
