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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/labstack/echo/v5"

	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures: user1 owns project 1 (6 task fixtures of interest) and has read access to project 3; user2 has no access
// to project 1. Dashboards 1 (personal user1), 2 (personal user2), 3 (project 1), 4 (project 3, user1 and user2 read),
// 5 (project 11, user1 admin), 6 (project 10, user1 write).

func problemParam(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Details struct {
			Param string `json:"param"`
		} `json:"details"`
	}
	require.NoError(t, json.Unmarshal(body, &p))
	return p.Details.Param
}

func TestHumaReports(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	post := func(token, body string) (int, map[string]any, []byte) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/reports/tasks", body, token, "")
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out, rec.Body.Bytes()
	}

	t.Run("counts and groups", func(t *testing.T) {
		code, out, raw := post(user1, `{"project_id":1,"group_by":"done"}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		assert.InDelta(t, float64(20), out["total"], 0)
		assert.Equal(t, "count", out["measure"])
		assert.Equal(t, "tasks", out["unit"])
		groups := out["groups"].([]any)
		require.Len(t, groups, 2)
		first := groups[0].(map[string]any)
		assert.Equal(t, "Open", first["label"])
		assert.InDelta(t, float64(19), first["value"], 0)
		assert.Equal(t, false, first["key"])
	})

	t.Run("matches the task list", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/tasks?filter=done%20%3D%20false&per_page=1", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var list struct {
			Total float64 `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
		_, out, _ := post(user1, `{"project_id":1,"filter":"done = false"}`)
		assert.InDelta(t, list.Total, out["total"], 0)
	})

	t.Run("time series", func(t *testing.T) {
		code, out, raw := post(user1, `{"project_id":1,"time":{"field":"created","bucket":"month","from":"2018-11-01","to":"2018-12-31","timezone":"UTC"}}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		series := out["series"].([]any)
		require.Len(t, series, 2)
		assert.Equal(t, "2018-12-01", series[1].(map[string]any)["bucket"])
		assert.InDelta(t, float64(20), series[1].(map[string]any)["value"], 0)
		assert.InDelta(t, float64(0), series[0].(map[string]any)["value"], 0)
		assert.Equal(t, "UTC", out["time"].(map[string]any)["timezone"])
	})

	t.Run("typed errors name the parameter", func(t *testing.T) {
		for body, param := range map[string]string{
			`{"group_by":"nope"}`: "group_by",
			`{"measure":"sum"}`:   "field",
			`{"time":{"field":"created","from":"2026-02-01","to":"2026-01-01"}}`: "time.to",
			`{"time":{"field":"created","timezone":"Mars/Base"}}`:                "time.timezone",
		} {
			code, out, raw := post(user1, body)
			assert.Equal(t, http.StatusBadRequest, code, body)
			assert.InDelta(t, float64(models.ErrCodeReportInvalidParameter), out["code"], 0, body)
			assert.Equal(t, param, problemParam(t, raw), body)
		}

		code, out, _ := post(user1, `{"time":{"field":"created","bucket":"day","from":"2000-01-01"}}`)
		assert.Equal(t, http.StatusBadRequest, code)
		assert.InDelta(t, float64(models.ErrCodeReportTooManyBuckets), out["code"], 0)

		code, out, _ = post(user1, `{"group_by":"done","limit":500}`)
		assert.Equal(t, http.StatusBadRequest, code)
		assert.InDelta(t, float64(models.ErrCodeReportTooManyGroups), out["code"], 0)

		code, out, _ = post(user1, `{"measure":"sum","field":"cf_2"}`)
		assert.Equal(t, http.StatusBadRequest, code)
		assert.InDelta(t, float64(models.ErrCodeReportFieldKindNotSupported), out["code"], 0)

		// A bad enum is rejected by the schema before the model.
		code, _, _ = post(user1, `{"measure":"median"}`)
		assert.GreaterOrEqual(t, code, 400)
		assert.Less(t, code, 500)
	})

	t.Run("permissions", func(t *testing.T) {
		code, _, _ := post(user2, `{"project_id":1}`)
		assert.Equal(t, http.StatusForbidden, code)

		code, out, _ := post(user2, `{"group_by":"cf_2"}`)
		assert.Equal(t, http.StatusBadRequest, code, "a field of a project user2 cannot read")
		assert.InDelta(t, float64(models.ErrCodeInvalidTaskField), out["code"], 0)

		code, _, _ = post("", `{}`)
		assert.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("link shares", func(t *testing.T) {
		share := &models.LinkSharing{ID: 1, Hash: "test", ProjectID: 1, Permission: models.PermissionRead, SharingType: models.SharingTypeWithoutPassword, SharedByID: 1}
		token, err := auth.NewLinkShareJWTAuthtoken(share)
		require.NoError(t, err)

		code, out, raw := post(token, `{"group_by":"project"}`)
		require.Equal(t, http.StatusOK, code, string(raw))
		assert.Len(t, out["groups"], 1)

		code, out, _ = post(token, `{"measure":"sum","field":"time_spent"}`)
		assert.Equal(t, http.StatusForbidden, code)
		assert.InDelta(t, float64(models.ErrCodeReportNotAvailableForShare), out["code"], 0)
	})

	t.Run("options", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/reports/tasks/options?project_id=1", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var opts struct {
			HasWorkflow bool `json:"has_workflow"`
			Dimensions  []struct {
				Name string `json:"name"`
			} `json:"dimensions"`
			Measures   []map[string]any `json:"measures"`
			TimeFields []map[string]any `json:"time_fields"`
			Limits     map[string]any   `json:"limits"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &opts))
		assert.False(t, opts.HasWorkflow)
		names := []string{}
		for _, d := range opts.Dimensions {
			names = append(names, d.Name)
		}
		assert.Contains(t, names, "assignee")
		assert.Contains(t, names, "cf_2")
		assert.NotContains(t, names, "status")
		assert.Len(t, opts.Measures, 5)
		assert.InDelta(t, float64(200), opts.Limits["max_groups"], 0)

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/reports/tasks/options?project_id=1", "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code)
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/reports/tasks/options?project_id=11", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"has_workflow":true`)
	})
}

type dashboardDTO struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ProjectID   int64  `json:"project_id"`
	WidgetCount int64  `json:"widget_count"`
	MaxPerm     int    `json:"max_permission"`
	Owner       struct {
		ID int64 `json:"id"`
	} `json:"owner"`
	Widgets []struct {
		ID     int64          `json:"id"`
		Type   string         `json:"type"`
		Title  string         `json:"title"`
		Config map[string]any `json:"config"`
		X      int64          `json:"x"`
		W      int64          `json:"w"`
	} `json:"widgets"`
}

func humaRequestWithHeader(t *testing.T, e *echo.Echo, path, token, header, value string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(header, value)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func dashPath(id int64) string { return "/api/v2/dashboards/" + strconv.FormatInt(id, 10) }

func TestHumaDashboards(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	t.Run("list", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/dashboards", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var list struct {
			Items []dashboardDTO `json:"items"`
			Total int            `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
		ids := []int64{}
		for _, d := range list.Items {
			ids = append(ids, d.ID)
			assert.Empty(t, d.Widgets)
		}
		assert.Equal(t, []int64{1, 3, 4, 5, 6}, ids)
		assert.Equal(t, int64(2), list.Items[0].WidgetCount)

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/dashboards?project_id=3", "", user2, "")
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
		require.Len(t, list.Items, 1)
		assert.Equal(t, int64(4), list.Items[0].ID)
	})

	t.Run("read with ETag", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, dashPath(1), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var d dashboardDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &d))
		assert.Len(t, d.Widgets, 2)
		assert.Equal(t, 2, d.MaxPerm)
		assert.Equal(t, int64(1), d.Owner.ID)
		etag := rec.Header().Get("ETag")
		require.NotEmpty(t, etag)

		rec2 := humaRequestWithHeader(t, e, dashPath(1), user1, "If-None-Match", etag)
		assert.Equal(t, http.StatusNotModified, rec2.Code)

		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodGet, dashPath(1), "", user2, "").Code)
		assert.Equal(t, http.StatusNotFound, humaRequest(t, e, http.MethodGet, dashPath(9999), "", user1, "").Code)

		rec = humaRequest(t, e, http.MethodGet, dashPath(4), "", user2, "")
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &d))
		assert.Equal(t, 0, d.MaxPerm, "a reader of a project dashboard")
	})

	t.Run("create, replace, layout, delete", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/dashboards", `{"title":"Ops","widgets":[
			{"type":"bar","title":"By prio","x":0,"y":0,"w":6,"h":4,"config":{"request":{"group_by":"priority"}}},
			{"type":"text","title":"note","x":6,"y":0,"w":6,"h":4,"config":{"text":"hi"}}]}`, user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var d dashboardDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &d))
		require.Len(t, d.Widgets, 2)
		assert.Equal(t, int64(1), d.Owner.ID)

		// Replace: keep widget 0 (renamed), drop widget 1, add a new one.
		body := `{"title":"Ops 2","widgets":[
			{"id":` + strconv.FormatInt(d.Widgets[0].ID, 10) + `,"type":"bar","title":"Renamed","x":0,"y":0,"w":6,"h":4,"config":{"request":{"group_by":"done"}}},
			{"type":"number","title":"Open","x":6,"y":0,"w":3,"h":2,"config":{"request":{"filter":"done = false"}}}]}`
		rec = humaRequest(t, e, http.MethodPut, dashPath(d.ID), body, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var replaced dashboardDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &replaced))
		require.Len(t, replaced.Widgets, 2)
		assert.Equal(t, "Ops 2", replaced.Title)
		kept := false
		for _, w := range replaced.Widgets {
			assert.NotEqual(t, d.Widgets[1].ID, w.ID)
			if w.ID == d.Widgets[0].ID {
				kept = true
				assert.Equal(t, "Renamed", w.Title)
			}
		}
		assert.True(t, kept)

		// Layout only.
		rec = humaRequest(t, e, http.MethodPut, dashPath(d.ID)+"/layout", `{"items":[{"id":`+strconv.FormatInt(d.Widgets[0].ID, 10)+`,"x":3,"y":2,"w":9,"h":5}]}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodGet, dashPath(d.ID), "", user1, "")
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &replaced))
		for _, w := range replaced.Widgets {
			if w.ID == d.Widgets[0].ID {
				assert.Equal(t, int64(3), w.X)
				assert.Equal(t, int64(9), w.W)
				assert.Equal(t, "Renamed", w.Title)
			}
		}

		// A merge patch without widgets keeps them.
		rec = humaRequest(t, e, http.MethodPatch, dashPath(d.ID), `{"title":"Ops 3"}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var patched dashboardDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &patched))
		assert.Equal(t, "Ops 3", patched.Title)
		assert.Len(t, patched.Widgets, 2)

		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodPut, dashPath(d.ID)+"/layout", `{"items":[]}`, user2, "").Code)
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodDelete, dashPath(d.ID), "", user2, "").Code)
		assert.Equal(t, http.StatusNoContent, humaRequest(t, e, http.MethodDelete, dashPath(d.ID), "", user1, "").Code)
		assert.Equal(t, http.StatusNotFound, humaRequest(t, e, http.MethodGet, dashPath(d.ID), "", user1, "").Code)
	})

	t.Run("errors", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/dashboards", `{"title":"x","widgets":[{"type":"bar","w":4,"h":3,"config":{"request":{"group_by":"nope"}}}]}`, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeDashboardInvalid, problemCode(t, rec))
		assert.Equal(t, "widgets[0].config.request.group_by", problemParam(t, rec.Body.Bytes()))

		rec = humaRequest(t, e, http.MethodPost, "/api/v2/dashboards", `{"title":"x","widgets":[{"type":"pie","w":4,"h":3}]}`, user1, "")
		assert.GreaterOrEqual(t, rec.Code, 400)
		assert.Less(t, rec.Code, 500)

		rec = humaRequest(t, e, http.MethodPut, dashPath(1), `{"title":"x","widgets":[{"id":3,"type":"text","w":4,"h":3,"config":{"text":"a"}}]}`, user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, models.ErrCodeDashboardWidgetDoesNotExist, problemCode(t, rec))

		rec = humaRequest(t, e, http.MethodPut, dashPath(1)+"/layout", `{"items":[{"id":1,"x":10,"y":0,"w":4,"h":3}]}`, user1, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, models.ErrCodeDashboardInvalid, problemCode(t, rec))

		rec = humaRequest(t, e, http.MethodPut, dashPath(1)+"/layout", `{"items":[{"id":3,"x":0,"y":0,"w":4,"h":3}]}`, user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code)

		rec = humaRequest(t, e, http.MethodPut, dashPath(1), `{"title":"x","project_id":1}`, user1, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("project dashboards need project admins", func(t *testing.T) {
		// user1 administers project 11 and owns project 1, but only writes to project 10.
		for project, want := range map[int]int{1: http.StatusCreated, 11: http.StatusCreated, 10: http.StatusForbidden, 9: http.StatusForbidden} {
			rec := humaRequest(t, e, http.MethodPost, "/api/v2/dashboards", `{"title":"p","project_id":`+strconv.Itoa(project)+`}`, user1, "")
			assert.Equal(t, want, rec.Code, "project %d: %s", project, rec.Body.String())
		}
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodPut, dashPath(6), `{"title":"x"}`, user1, "").Code)
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodPut, dashPath(4), `{"title":"x"}`, user2, "").Code)
	})

	t.Run("link share", func(t *testing.T) {
		share := &models.LinkSharing{ID: 1, Hash: "test", ProjectID: 1, Permission: models.PermissionAdmin, SharingType: models.SharingTypeWithoutPassword, SharedByID: 1}
		token, err := auth.NewLinkShareJWTAuthtoken(share)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, humaRequest(t, e, http.MethodGet, dashPath(3), "", token, "").Code)
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodGet, dashPath(1), "", token, "").Code)
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodPut, dashPath(3), `{"title":"x"}`, token, "").Code)
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodPost, "/api/v2/dashboards", `{"title":"x","project_id":1}`, token, "").Code)
	})

}
