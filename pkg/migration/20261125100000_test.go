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

func TestFormsMigration20261125100000(t *testing.T) {
	x, err := db.CreateTestEngine()
	require.NoError(t, err)
	tables := []any{forms20261125100000{}, formSubmissions20261125100000{}}
	require.NoError(t, x.DropTables(tables...))
	t.Cleanup(func() { require.NoError(t, x.DropTables(tables...)) })

	idx := slices.IndexFunc(migrations, func(m *xormigrate.Migration) bool { return m.ID == "20261125100000" })
	require.NotEqual(t, -1, idx)
	require.NoError(t, migrations[idx].Migrate(x))

	metas, err := x.DBMetas()
	require.NoError(t, err)
	found := map[string][]string{}
	for _, m := range metas {
		if m.Name == "forms" || m.Name == "form_submissions" {
			found[m.Name] = m.ColumnsSeq()
		}
	}
	require.Contains(t, found, "forms")
	require.Contains(t, found, "form_submissions")
	for _, c := range []string{"project_id", "token", "access", "enabled", "disabled_reason", "owner_id", "fields", "settings", "max_submissions", "submission_count"} {
		require.Contains(t, found["forms"], c)
	}
	for _, c := range []string{"form_id", "task_id", "submitter_id", "submitter_name", "submitter_email", "ip_hash", "created"} {
		require.Contains(t, found["form_submissions"], c)
	}

	now := time.Now()
	// Settings is set on both rows: the migration struct holds the json column as a string, and PostgreSQL rejects
	// the empty string as json, which would make the second insert fail for the wrong reason.
	_, err = x.Insert(&forms20261125100000{ProjectID: 1, Title: "a", Token: "t1", Access: "public", OwnerID: 1, Fields: "[]", Settings: "{}", Created: now, Updated: now})
	require.NoError(t, err)
	_, err = x.Insert(&forms20261125100000{ProjectID: 1, Title: "b", Token: "t1", Access: "public", OwnerID: 1, Fields: "[]", Settings: "{}", Created: now, Updated: now})
	require.Error(t, err, "the unique index rejects a second form with the same token")
}
