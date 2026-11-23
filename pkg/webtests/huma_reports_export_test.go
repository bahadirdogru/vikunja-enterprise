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

package webtests

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/license"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHumaReportExports(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	post := func(token, path, body string) (int, http.Header, []byte) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2"+path, body, token, "")
		return rec.Code, rec.Header(), rec.Body.Bytes()
	}
	records := func(t *testing.T, raw []byte) [][]string {
		t.Helper()
		require.True(t, bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")), "byte order mark")
		r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))))
		r.FieldsPerRecord = -1
		out, err := r.ReadAll()
		require.NoError(t, err)
		return out
	}

	t.Run("aggregate report as csv", func(t *testing.T) {
		code, h, raw := post(user1, "/reports/tasks/export", `{"project_id":1,"group_by":"done"}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		assert.Equal(t, "text/csv; charset=utf-8", h.Get("Content-Type"))
		assert.Contains(t, h.Get("Content-Disposition"), "attachment")
		assert.Equal(t, "no-store", h.Get("Cache-Control"))
		rec := records(t, raw)
		assert.Equal(t, []string{"Group", "Tasks"}, rec[0])
		assert.Equal(t, []string{"Open", "19"}, rec[1])
		assert.Equal(t, []string{"Done", "1"}, rec[2])

		// Same typed errors as the report.
		code, _, raw = post(user1, "/reports/tasks/export", `{"group_by":"nope"}`)
		assert.Equal(t, http.StatusBadRequest, code)
		assert.Equal(t, "group_by", problemParam(t, raw))
		code, _, _ = post(user2, "/reports/tasks/export", `{"project_id":1}`)
		assert.Equal(t, http.StatusForbidden, code)
		code, _, _ = post("", "/reports/tasks/export", `{}`)
		assert.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("tasks behind a widget as csv", func(t *testing.T) {
		code, h, raw := post(user1, "/reports/tasks/export/rows", `{"project_id":1,"filter":"done = true"}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		assert.Equal(t, "text/csv; charset=utf-8", h.Get("Content-Type"))
		rec := records(t, raw)
		assert.Equal(t, "ID", rec[0][0])
		assert.Len(t, rec, 2, "one done task in project 1")

		code, _, _ = post(user2, "/reports/tasks/export/rows", `{"project_id":1}`)
		assert.Equal(t, http.StatusForbidden, code)

		// user2 without a project: only the tasks of their own projects, never project 1's.
		code, _, raw = post(user2, "/reports/tasks/export/rows", `{}`)
		require.Equal(t, http.StatusOK, code)
		for _, row := range records(t, raw)[1:] {
			assert.NotEqual(t, "Test1", row[3], "a task of project 1")
		}
	})

	t.Run("compare", func(t *testing.T) {
		code, _, raw := post(user1, "/reports/tasks", `{"project_id":1,"compare":"previous_period","time":{"field":"created","bucket":"month","from":"2018-12-01","to":"2018-12-31","timezone":"UTC"}}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		var out struct {
			Value      float64 `json:"value"`
			Comparison struct {
				Value        float64  `json:"value"`
				Delta        float64  `json:"delta"`
				DeltaPercent *float64 `json:"delta_percent"`
			} `json:"comparison"`
		}
		require.NoError(t, json.Unmarshal(raw, &out))
		assert.Equal(t, float64(20), out.Value)
		assert.Equal(t, float64(0), out.Comparison.Value)
		assert.Equal(t, float64(20), out.Comparison.Delta)
		assert.Nil(t, out.Comparison.DeltaPercent)

		code, _, raw = post(user1, "/reports/tasks", `{"compare":"previous_period"}`)
		assert.Equal(t, http.StatusBadRequest, code)
		assert.Equal(t, "compare", problemParam(t, raw))
	})

	t.Run("status time", func(t *testing.T) {
		code, _, raw := post(user1, "/reports/status-time", `{"project_id":11}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		var out map[string]any
		require.NoError(t, json.Unmarshal(raw, &out))
		assert.Contains(t, out, "statuses")
		assert.Contains(t, out, "tasks")

		code, _, raw = post(user1, "/reports/status-time", `{}`)
		assert.GreaterOrEqual(t, code, 400, string(raw))
		code, _, _ = post(user2, "/reports/status-time", `{"project_id":1}`)
		assert.Equal(t, http.StatusForbidden, code)

		code, h, raw := post(user1, "/reports/status-time/export", `{"project_id":11,"group_by":"assignee"}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		assert.Equal(t, "text/csv; charset=utf-8", h.Get("Content-Type"))
		assert.Equal(t, "Group", records(t, raw)[0][0])
	})

	t.Run("workload", func(t *testing.T) {
		code, _, raw := post(user1, "/reports/workload", `{"project_id":1}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		var out struct {
			Buckets []string `json:"buckets"`
			Totals  struct {
				Total int64 `json:"total"`
			} `json:"totals"`
		}
		require.NoError(t, json.Unmarshal(raw, &out))
		assert.Equal(t, []string{"overdue", "this_week", "next_week", "later", "no_due_date"}, out.Buckets)
		assert.Equal(t, int64(19), out.Totals.Total, "the open tasks of project 1")

		code, _, _ = post(user2, "/reports/workload", `{"project_id":1}`)
		assert.Equal(t, http.StatusForbidden, code)

		code, h, raw := post(user1, "/reports/workload/export", `{"project_id":1}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		assert.Equal(t, "text/csv; charset=utf-8", h.Get("Content-Type"))
		rec := records(t, raw)
		assert.Equal(t, "Assignee", rec[0][0])
		assert.Equal(t, "Total", rec[len(rec)-1][0])
	})

	t.Run("time tracking", func(t *testing.T) {
		defer license.ResetForTests()

		license.ResetForTests()
		code, _, _ := post(user1, "/reports/time-tracking", `{}`)
		assert.Equal(t, http.StatusNotFound, code, "the feature is not licensed")
		code, _, _ = post(user1, "/reports/time-tracking/export", `{}`)
		assert.Equal(t, http.StatusNotFound, code)

		license.SetForTests([]license.Feature{license.FeatureTimeTracking})
		code, _, raw := post(user1, "/reports/time-tracking", `{"group_by":"project","from":"2018-01-01","to":"2018-12-31","timezone":"UTC"}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		var out struct {
			GroupBy string `json:"group_by"`
			Groups  []any  `json:"groups"`
		}
		require.NoError(t, json.Unmarshal(raw, &out))
		assert.Equal(t, "project", out.GroupBy)

		code, h, raw := post(user1, "/reports/time-tracking/export", `{"group_by":"day","from":"2018-12-01","to":"2018-12-03","timezone":"UTC"}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		assert.Equal(t, "text/csv; charset=utf-8", h.Get("Content-Type"))
		rec := records(t, raw)
		assert.Equal(t, []string{"Day", "Seconds", "Hours", "Entries"}, rec[0])
		assert.Len(t, rec, 4)

		code, _, raw = post(user1, "/reports/time-tracking", `{"group_by":"task"}`)
		assert.GreaterOrEqual(t, code, 400)
		assert.Less(t, code, 500, string(raw))

		share := &models.LinkSharing{ID: 1, Hash: "test", ProjectID: 1, Permission: models.PermissionRead, SharingType: models.SharingTypeWithoutPassword, SharedByID: 1}
		token, err := auth.NewLinkShareJWTAuthtoken(share)
		require.NoError(t, err)
		code, _, raw = post(token, "/reports/time-tracking", `{}`)
		assert.Equal(t, http.StatusForbidden, code, string(raw))
		assert.True(t, strings.Contains(string(raw), "23005"))
	})
}
