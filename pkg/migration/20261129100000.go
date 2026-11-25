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
	"src.techknowlogick.com/xormigrate"
	"xorm.io/xorm"
)

// automationRuns20261129100000 adds the machine-readable form of the error of a run: a stable code and the values the
// sentence mentions, so the interface can say it in the language of the reader. The English text stays in `error`.
type automationRuns20261129100000 struct {
	ErrorCode   string            `xorm:"varchar(100) null"`
	ErrorParams map[string]string `xorm:"json null"`
}

func (automationRuns20261129100000) TableName() string {
	return "automation_runs"
}

// automationRunSteps20261129100000 does the same for the error of a step, and adds the sentences of its output (what
// the step did and its warnings) in the same form.
type automationRunSteps20261129100000 struct {
	ErrorCode      string            `xorm:"varchar(100) null"`
	ErrorParams    map[string]string `xorm:"json null"`
	OutputMessages []map[string]any  `xorm:"json null"`
}

func (automationRunSteps20261129100000) TableName() string {
	return "automation_run_steps"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261129100000",
		Description: "Add error codes and message codes to automation runs and steps so the interface can translate them",
		Migrate: func(tx *xorm.Engine) error {
			return partialSync(tx, automationRuns20261129100000{}, automationRunSteps20261129100000{})
		},
		Rollback: func(tx *xorm.Engine) error {
			return nil
		},
	})
}
