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
	"slices"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/require"
	"src.techknowlogick.com/xormigrate"
	"xorm.io/xorm/schemas"
)

func TestAutomationMessageCodesMigration20261129100000(t *testing.T) {
	x, err := db.CreateTestEngine()
	require.NoError(t, err)

	old := []any{automationRuns20261122140000{}, automationRunSteps20261122140000{}}
	drop := func() { require.NoError(t, x.DropTables(old...)) }
	t.Cleanup(drop)
	drop()
	require.NoError(t, x.Sync2(old...))

	_, err = x.Insert(&automationRuns20261122140000{RuleID: 1, RuleRevision: 1, EventID: 1, ProjectID: 1, Status: "failed", Error: "kept", Started: time.Now()})
	require.NoError(t, err)
	_, err = x.Insert(&automationRunSteps20261122140000{RunID: 1, Position: 0, ActionType: "task.set_field", Status: "failed", Error: "kept too"})
	require.NoError(t, err)

	meta := func(name string) *schemas.Table {
		tables, err := x.DBMetas()
		require.NoError(t, err)
		for _, table := range tables {
			if table.Name == name {
				return table
			}
		}
		t.Fatalf("table %s not found", name)
		return nil
	}
	before := map[string]*schemas.Table{"automation_runs": meta("automation_runs"), "automation_run_steps": meta("automation_run_steps")}

	idx := slices.IndexFunc(migrations, func(m *xormigrate.Migration) bool { return m.ID == "20261129100000" })
	require.NotEqual(t, -1, idx)
	require.NoError(t, migrations[idx].Migrate(x))

	for name, b := range before {
		after := meta(name)
		for _, column := range b.ColumnsSeq() {
			require.NotNilf(t, after.GetColumn(column), "%s: the migration dropped column %s", name, column)
		}
		for indexName, index := range b.Indexes {
			preserved, found := after.Indexes[indexName]
			require.Truef(t, found, "%s: the migration dropped index %s", name, indexName)
			require.Equal(t, index.Type, preserved.Type)
			require.Equal(t, index.Cols, preserved.Cols)
		}
	}
	for _, c := range []string{"error_code", "error_params"} {
		require.NotNil(t, meta("automation_runs").GetColumn(c), c)
		require.NotNil(t, meta("automation_run_steps").GetColumn(c), c)
	}
	require.NotNil(t, meta("automation_run_steps").GetColumn("output_messages"))

	// Rows written before the codes keep their text and have no code.
	run := &struct {
		ID        int64 `xorm:"pk"`
		Error     string
		ErrorCode *string
	}{}
	found, err := x.Table("automation_runs").Get(run)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "kept", run.Error)
	require.Nil(t, run.ErrorCode)

	// A step is still unique by its position in the run.
	_, err = x.Insert(&automationRunSteps20261122140000{RunID: 1, Position: 0, ActionType: "task.set_field", Status: "failed"})
	require.Error(t, err)
}
