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

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/require"
	"src.techknowlogick.com/xormigrate"
)

func TestFormBrandingMigration20261126100000(t *testing.T) {
	x, err := db.CreateTestEngine()
	require.NoError(t, err)
	tables := []any{forms20261125100000{}}
	require.NoError(t, x.DropTables(tables...))
	t.Cleanup(func() { require.NoError(t, x.DropTables(tables...)) })
	require.NoError(t, x.Sync2(forms20261125100000{}))

	idx := slices.IndexFunc(migrations, func(m *xormigrate.Migration) bool { return m.ID == "20261126100000" })
	require.NotEqual(t, -1, idx)
	require.NoError(t, migrations[idx].Migrate(x))

	metas, err := x.DBMetas()
	require.NoError(t, err)
	for _, m := range metas {
		if m.Name != "forms" {
			continue
		}
		for _, c := range []string{"brand_name", "brand_logo_file_id", "accent_color", "hide_footer"} {
			require.Contains(t, m.ColumnsSeq(), c)
		}
		return
	}
	require.Fail(t, "forms table not found")
}
