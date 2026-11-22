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

type forms20261125100000 struct {
	ID              int64     `xorm:"bigint autoincr not null unique pk"`
	ProjectID       int64     `xorm:"bigint not null INDEX"`
	Title           string    `xorm:"varchar(250) not null"`
	Description     string    `xorm:"longtext null"`
	Token           string    `xorm:"varchar(64) not null unique"`
	Access          string    `xorm:"varchar(10) not null"`
	Enabled         bool      `xorm:"bool default false"`
	DisabledReason  string    `xorm:"varchar(100) null"`
	OwnerID         int64     `xorm:"bigint not null"`
	Fields          string    `xorm:"json not null"`
	Settings        string    `xorm:"json null"`
	MaxSubmissions  int64     `xorm:"bigint not null default 0"`
	SubmissionCount int64     `xorm:"bigint not null default 0"`
	Created         time.Time `xorm:"created not null"`
	Updated         time.Time `xorm:"updated not null"`
}

func (forms20261125100000) TableName() string {
	return "forms"
}

type formSubmissions20261125100000 struct {
	ID             int64     `xorm:"bigint autoincr not null unique pk"`
	FormID         int64     `xorm:"bigint not null INDEX"`
	TaskID         int64     `xorm:"bigint not null INDEX"`
	SubmitterID    *int64    `xorm:"bigint null"`
	SubmitterName  string    `xorm:"varchar(250) null"`
	SubmitterEmail string    `xorm:"varchar(250) null"`
	IPHash         string    `xorm:"varchar(64) null INDEX"`
	Created        time.Time `xorm:"DATETIME not null"`
}

func (formSubmissions20261125100000) TableName() string {
	return "form_submissions"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261125100000",
		Description: "Add the forms and form submissions tables",
		Migrate: func(tx *xorm.Engine) error {
			return tx.Sync( //nolint:forbidigo // brand-new tables, nothing to drop
				forms20261125100000{},
				formSubmissions20261125100000{},
			)
		},
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(
				forms20261125100000{},
				formSubmissions20261125100000{},
			)
		},
	})
}
