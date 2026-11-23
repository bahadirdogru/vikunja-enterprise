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
	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

func automationTable20261127100000(t *testing.T, x *xorm.Engine, name string) *schemas.Table {
	t.Helper()
	metas, err := x.DBMetas()
	require.NoError(t, err)
	for _, m := range metas {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("table %s not found", name)
	return nil
}

func TestAutomationTriggersMigration20261127100000(t *testing.T) {
	x, err := db.CreateTestEngine()
	require.NoError(t, err)
	tables := []any{
		automationRules20261122140000{},
		automationEvents20261122140000{},
		automationDueMarkers20261127100000{},
		automationTaskOrigins20261127100000{},
	}
	require.NoError(t, x.DropTables(tables...))
	t.Cleanup(func() { require.NoError(t, x.DropTables(tables...)) })
	require.NoError(t, x.Sync2(automationRules20261122140000{}, automationEvents20261122140000{}))

	beforeRules := automationTable20261127100000(t, x, "automation_rules")
	beforeEvents := automationTable20261127100000(t, x, "automation_events")
	require.NotEmpty(t, beforeRules.Indexes)
	require.NotEmpty(t, beforeEvents.Indexes)

	now := time.Now()
	_, err = x.Insert(&automationRules20261122140000{ProjectID: 1, Title: "r", OwnerID: 1, Trigger: "{}", Actions: "[]", Created: now, Updated: now})
	require.NoError(t, err)

	idx := slices.IndexFunc(migrations, func(m *xormigrate.Migration) bool { return m.ID == "20261127100000" })
	require.NotEqual(t, -1, idx)
	require.NoError(t, migrations[idx].Migrate(x))

	for name, before := range map[string]*schemas.Table{"automation_rules": beforeRules, "automation_events": beforeEvents} {
		after := automationTable20261127100000(t, x, name)
		for _, c := range before.ColumnsSeq() {
			require.NotNilf(t, after.GetColumn(c), "migration dropped column %s.%s", name, c)
		}
		for iname, index := range before.Indexes {
			kept, found := after.Indexes[iname]
			require.Truef(t, found, "migration dropped index %s on %s", iname, name)
			require.Equal(t, index.Type, kept.Type)
		}
	}
	rules := automationTable20261127100000(t, x, "automation_rules")
	for _, c := range []string{"last_fired_at", "last_fire_note", "webhook_token_hash"} {
		require.NotNilf(t, rules.GetColumn(c), c)
	}
	require.NotNil(t, automationTable20261127100000(t, x, "automation_events").GetColumn("rule_id"))

	// The existing row survives with the new columns empty.
	got := &automationRules20261127100000{}
	has, err := x.ID(1).Get(got)
	require.NoError(t, err)
	require.True(t, has)
	require.Empty(t, got.WebhookTokenHash)

	// One marker per rule and task.
	_, err = x.Insert(&automationDueMarkers20261127100000{RuleID: 1, TaskID: 1, DueDate: now, Fired: now})
	require.NoError(t, err)
	_, err = x.Insert(&automationDueMarkers20261127100000{RuleID: 1, TaskID: 1, DueDate: now, Fired: now})
	require.Error(t, err, "the unique index allows one marker per rule and task")
}
