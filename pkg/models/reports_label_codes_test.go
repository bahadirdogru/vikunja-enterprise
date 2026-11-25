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
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every name of a report group that the server writes itself has a code, and the interface (GROUP_LABEL_KEYS in
// frontend/src/helpers/charts.ts) says each code in the language of the reader. A name without a code would reach a
// Turkish reader as "Open" or "Done".
func TestEveryReportGroupNameTheServerWritesHasACode(t *testing.T) {
	codes := map[string]string{}
	for _, l := range reportCategoryLabels {
		codes[l.code] = l.label
	}
	done, err := resolveDoneLabels(nil, nil)
	require.NoError(t, err)
	checked, err := resolveCheckedLabels(nil, nil)
	require.NoError(t, err)
	for _, set := range []map[string]reportLabel{done, checked} {
		for _, l := range set {
			codes[l.code] = l.label
		}
	}
	keys := []reportKey{}
	for id := range reportPriorityNames {
		keys = append(keys, reportKey{canon: strconv.FormatInt(id, 10), value: id})
	}
	priorities, err := resolvePriorityLabels(nil, keys)
	require.NoError(t, err)
	for _, l := range priorities {
		codes[l.code] = l.label
	}
	require.Len(t, codes, 4+2+2+len(reportPriorityNames))

	ts, err := os.ReadFile("../../frontend/src/helpers/charts.ts")
	require.NoError(t, err)
	for code, label := range codes {
		assert.NotEmpty(t, code, "the name %q has no code", label)
		assert.Contains(t, string(ts), "'"+code+"':", "GROUP_LABEL_KEYS has no entry for the code %s", code)
	}
}
