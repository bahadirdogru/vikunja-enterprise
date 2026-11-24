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
)

func TestAutomationWebhookSeenMigration20261128100000(t *testing.T) {
	x, err := db.CreateTestEngine()
	require.NoError(t, err)
	require.NoError(t, x.DropTables(automationWebhookSeen20261128100000{}))
	t.Cleanup(func() { require.NoError(t, x.DropTables(automationWebhookSeen20261128100000{})) })

	idx := slices.IndexFunc(migrations, func(m *xormigrate.Migration) bool { return m.ID == "20261128100000" })
	require.NotEqual(t, -1, idx)
	require.NoError(t, migrations[idx].Migrate(x))

	metas, err := x.DBMetas()
	require.NoError(t, err)
	foundTable := false
	for _, m := range metas {
		if m.Name != "automation_webhook_seen" {
			continue
		}
		foundTable = true
		for _, c := range []string{"id", "rule_id", "signature", "signed_at"} {
			require.NotNilf(t, m.GetColumn(c), c)
		}
		hasExpiresIndex := false
		for _, index := range m.Indexes {
			if slices.Equal(index.Cols, []string{"signed_at"}) {
				hasExpiresIndex = true
			}
		}
		require.True(t, hasExpiresIndex, "the cleanup looks rows up by signing time")
	}
	require.True(t, foundTable)

	now := time.Now()
	_, err = x.Insert(&automationWebhookSeen20261128100000{RuleID: 1, Signature: "aa", SignedAt: now})
	require.NoError(t, err)
	_, err = x.Insert(&automationWebhookSeen20261128100000{RuleID: 2, Signature: "aa", SignedAt: now})
	require.NoError(t, err, "another rule may see the same signature")
	_, err = x.Insert(&automationWebhookSeen20261128100000{RuleID: 1, Signature: "aa", SignedAt: now})
	require.Error(t, err, "the unique index refuses the same signature twice for one rule")

	require.NoError(t, migrations[idx].Rollback(x))
}
