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

package spec

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cronCasesFile is shared with frontend/src/helpers/automationCron.test.ts: both parsers have to give the same verdict.
const cronCasesFile = "../../../frontend/src/helpers/automationCron.cases.json"

func TestParseCronSharedCases(t *testing.T) {
	raw, err := os.ReadFile(cronCasesFile)
	require.NoError(t, err)
	var table struct {
		Cases []struct {
			Expr  string `json:"expr"`
			Valid bool   `json:"valid"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases)

	for _, c := range table.Cases {
		t.Run(c.Expr, func(t *testing.T) {
			_, err := ParseCron(c.Expr)
			if c.Valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
