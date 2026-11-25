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

package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The report editor looks the title key of every option the server offers up in the language files of the frontend
// (`mage check:translations` cannot see these lookups, they are built from the answer of the options route).
func TestReportOptionTitleKeysAreTranslated(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	keys := map[string]bool{}
	for _, project := range []int64{0, 1, 11} { // all projects, one with custom fields, one with a workflow
		s := db.NewSession()
		opts, err := GetReportOptions(s, cfUser1, project)
		s.Close()
		require.NoError(t, err)
		for _, o := range opts.Dimensions {
			keys[o.TitleKey] = true
		}
		for _, o := range opts.TimeFields {
			keys[o.TitleKey] = true
		}
		for _, m := range opts.Measures {
			keys[m.TitleKey] = true
			for _, f := range m.Fields {
				keys[f.TitleKey] = true
			}
		}
	}
	require.NotEmpty(t, keys)
	assert.Contains(t, keys, "reports.dimension.status", "the workflow dimensions are among them")
	assert.Contains(t, keys, "reports.measure.sum")

	for _, file := range []string{"en.json", "tr-TR.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "i18n", "lang", file))
		require.NoError(t, err)
		var tree map[string]any
		require.NoError(t, json.Unmarshal(data, &tree))
		lookup := func(key string) bool {
			var node any = tree
			for _, part := range strings.Split(key, ".") {
				m, ok := node.(map[string]any)
				if !ok {
					return false
				}
				node = m[part]
			}
			s, ok := node.(string)
			return ok && s != ""
		}
		for key := range keys {
			assert.True(t, lookup(key), "%s has no text for %s", file, key)
		}
	}
}
