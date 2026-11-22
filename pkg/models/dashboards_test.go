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
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// Fixture dashboards: 1 personal of user1, 2 personal of user2, 3 project 1 (user1 owner), 4 project 3 (user1 and
// user2 read), 5 project 11 (user1 admin), 6 project 10 (user1 write).

func dashCan(t *testing.T, a web.Auth, d *Dashboard) (read, write bool) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	read, _, err := (&Dashboard{ID: d.ID}).CanRead(s, a)
	require.NoError(t, err)
	write, err = (&Dashboard{ID: d.ID}).CanUpdate(s, a)
	require.NoError(t, err)
	del, err := (&Dashboard{ID: d.ID}).CanDelete(s, a)
	require.NoError(t, err)
	assert.Equal(t, write, del)
	return read, write
}

func TestDashboard_Permissions(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	u3 := &user.User{ID: 3}
	share := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionAdmin}

	cases := []struct {
		name        string
		a           web.Auth
		id          int64
		read, write bool
	}{
		{"owner of a personal dashboard", cfUser1, 1, true, true},
		{"someone else's personal dashboard", cfUser2, 1, false, false},
		{"project owner", cfUser1, 3, true, true},
		{"reader of a project", cfUser2, 4, true, false},
		{"reader that is project owner changes it? no, only admins", cfUser1, 4, true, false},
		{"admin of the project", cfUser1, 5, true, true},
		{"write member cannot change", cfUser1, 6, true, false},
		{"stranger", cfUser2, 3, false, false},
		{"stranger on another project", cfUser2, 5, false, false},
		{"owner of project 3 is admin", u3, 4, true, true},
		{"link share reads its project", share, 3, true, false},
		{"link share cannot read other projects", share, 4, false, false},
		{"link share cannot read personal ones", share, 1, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			read, write := dashCan(t, c.a, &Dashboard{ID: c.id})
			assert.Equal(t, c.read, read)
			assert.Equal(t, c.write, write)
		})
	}

	t.Run("missing", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		_, _, err := (&Dashboard{ID: 9999}).CanRead(s, cfUser1)
		assert.True(t, IsErrDashboardDoesNotExist(err))
		_, err = (&Dashboard{ID: 9999}).CanUpdate(s, cfUser1)
		assert.True(t, IsErrDashboardDoesNotExist(err))
	})

	t.Run("create", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		for _, c := range []struct {
			name string
			a    web.Auth
			pid  int64
			want bool
		}{
			{"personal", cfUser2, 0, true},
			{"project admin (owner)", cfUser1, 1, true},
			{"admin share of project 11", cfUser1, 11, true},
			{"write member", cfUser1, 10, false},
			{"reader", cfUser1, 9, false},
			{"stranger", cfUser2, 1, false},
			{"link share personal", share, 0, false},
			{"link share project", share, 1, false},
		} {
			can, err := (&Dashboard{ProjectID: c.pid}).CanCreate(s, c.a)
			require.NoError(t, err, c.name)
			assert.Equal(t, c.want, can, c.name)
		}
	})

	t.Run("max permission", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		for id, want := range map[int64]int{1: 2, 3: 2, 4: 0, 5: 2, 6: 1} {
			_, perm, err := (&Dashboard{ID: id}).CanRead(s, cfUser1)
			require.NoError(t, err)
			assert.Equal(t, want, perm, id)
		}
	})
}

func dashList(t *testing.T, a web.Auth, projectID int64, search string) []int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	res, _, total, err := (&Dashboard{ProjectID: projectID}).ReadAll(s, a, search, 1, 50)
	require.NoError(t, err)
	ids := []int64{}
	for _, d := range res.([]*Dashboard) {
		ids = append(ids, d.ID)
		assert.Nil(t, d.Widgets, "lists carry no widgets")
	}
	assert.Equal(t, int64(len(ids)), total)
	return ids
}

func TestDashboard_List(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	assert.Equal(t, []int64{1, 3, 4, 5, 6}, dashList(t, cfUser1, 0, ""))
	assert.Equal(t, []int64{2, 4}, dashList(t, cfUser2, 0, ""))
	assert.Equal(t, []int64{4}, dashList(t, cfUser1, 3, ""))
	assert.Equal(t, []int64{}, dashList(t, cfUser2, 1, ""), "a project the user cannot read has no dashboards for them")
	assert.Equal(t, []int64{3}, dashList(t, &LinkSharing{ID: 1, ProjectID: 1}, 0, ""))
	assert.Equal(t, []int64{5}, dashList(t, cfUser1, 0, "Project 11"))

	s := db.NewSession()
	defer s.Close()
	res, _, _, err := (&Dashboard{}).ReadAll(s, cfUser1, "", 1, 50)
	require.NoError(t, err)
	for _, d := range res.([]*Dashboard) {
		if d.ID == 1 {
			assert.Equal(t, int64(2), d.WidgetCount)
			assert.Equal(t, int64(1), d.Owner.ID)
		}
	}
}

func saveDash(t *testing.T, a web.Auth, d *Dashboard) (*Dashboard, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	var err error
	if d.ID == 0 {
		err = d.Create(s, a)
	} else {
		err = d.Update(s, a)
	}
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	require.NoError(t, s.Commit())
	return d, nil
}

func readDash(t *testing.T, id int64) *Dashboard {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	d, err := loadDashboard(s, id)
	require.NoError(t, err)
	return d
}

func numberWidget(title string) *DashboardWidget {
	return &DashboardWidget{Type: WidgetTypeNumber, Title: title, W: 4, H: 3, Config: map[string]any{"request": map[string]any{"filter": "done = false"}}}
}

func TestDashboard_CRUD(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	d, err := saveDash(t, cfUser1, &Dashboard{Title: "Mine", Widgets: []*DashboardWidget{numberWidget("A"), numberWidget("B")}})
	require.NoError(t, err)
	assert.Equal(t, int64(1), d.Owner.ID)
	require.Len(t, d.Widgets, 2)
	assert.Equal(t, int64(2), d.WidgetCount)
	a, b := d.Widgets[0], d.Widgets[1]
	assert.Equal(t, d.ID, a.DashboardID)
	assert.Equal(t, int64(0), d.ProjectID)

	t.Run("replace keeps ids, creates and deletes", func(t *testing.T) {
		a.Title = "A renamed"
		a.X = 6
		updated, err := saveDash(t, cfUser1, &Dashboard{
			ID:      d.ID,
			Title:   "Mine v2",
			Widgets: []*DashboardWidget{a, {Type: WidgetTypeText, Title: "new", W: 12, H: 2, Config: map[string]any{"text": "hi"}}},
		})
		require.NoError(t, err)
		assert.Equal(t, "Mine v2", updated.Title)
		require.Len(t, updated.Widgets, 2)
		ids := []int64{updated.Widgets[0].ID, updated.Widgets[1].ID}
		assert.Contains(t, ids, a.ID)
		assert.NotContains(t, ids, b.ID, "the widget that was left out is deleted")
		db.AssertMissing(t, "dashboard_widgets", map[string]any{"id": b.ID})
		for _, w := range updated.Widgets {
			if w.ID == a.ID {
				assert.Equal(t, "A renamed", w.Title)
				assert.Equal(t, int64(6), w.X)
			}
		}
	})

	t.Run("null widgets keep them, an empty list clears them", func(t *testing.T) {
		kept, err := saveDash(t, cfUser1, &Dashboard{ID: d.ID, Title: "Mine v3"})
		require.NoError(t, err)
		assert.Len(t, kept.Widgets, 2)
		cleared, err := saveDash(t, cfUser1, &Dashboard{ID: d.ID, Title: "Mine v3", Widgets: []*DashboardWidget{}})
		require.NoError(t, err)
		assert.Empty(t, cleared.Widgets)
	})

	t.Run("a widget of another dashboard cannot be claimed", func(t *testing.T) {
		_, err := saveDash(t, cfUser1, &Dashboard{ID: d.ID, Title: "x", Widgets: []*DashboardWidget{{ID: 3, Type: WidgetTypeText, W: 2, H: 2}}})
		require.Error(t, err)
		assert.True(t, IsErrDashboardWidgetDoesNotExist(err))
		db.AssertExists(t, "dashboard_widgets", map[string]any{"id": 3, "dashboard_id": 3}, false)
		_, err = saveDash(t, cfUser1, &Dashboard{ID: d.ID, Title: "x", Widgets: []*DashboardWidget{{ID: 777, Type: WidgetTypeText, W: 2, H: 2}}})
		assert.True(t, IsErrDashboardWidgetDoesNotExist(err))
	})

	t.Run("the project cannot change", func(t *testing.T) {
		_, err := saveDash(t, cfUser1, &Dashboard{ID: d.ID, Title: "x", ProjectID: 1})
		require.Error(t, err)
		assert.True(t, IsErrDashboardInvalid(err))
		// Leaving it out of a project dashboard is fine.
		got, err := saveDash(t, cfUser1, &Dashboard{ID: 3, Title: "Renamed"})
		require.NoError(t, err)
		assert.Equal(t, int64(1), got.ProjectID)
	})

	t.Run("delete", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		require.NoError(t, (&Dashboard{ID: 1}).Delete(s, cfUser1))
		require.NoError(t, s.Commit())
		db.AssertMissing(t, "dashboards", map[string]any{"id": 1})
		db.AssertMissing(t, "dashboard_widgets", map[string]any{"dashboard_id": 1})
	})

	t.Run("ordering by position then id", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		first, err := saveDash(t, cfUser1, &Dashboard{Title: "First", Position: -1})
		require.NoError(t, err)
		ids := dashList(t, cfUser1, 0, "")
		assert.Equal(t, 6, len(ids))
		assert.Equal(t, first.ID, ids[0])
	})
}

func TestDashboard_Layout(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	layout := func(a web.Auth, items ...*DashboardLayoutItem) error {
		s := db.NewSession()
		defer s.Close()
		l := &DashboardLayout{DashboardID: 1, Items: items}
		can, err := l.CanUpdate(s, a)
		if err != nil {
			return err
		}
		if !can {
			return ErrGenericForbidden{}
		}
		if err = l.Update(s, a); err != nil {
			_ = s.Rollback()
			return err
		}
		return s.Commit()
	}

	before := readDash(t, 1)
	require.NoError(t, layout(cfUser1, &DashboardLayoutItem{ID: 1, X: 8, Y: 5, W: 4, H: 6}))
	after := readDash(t, 1)
	for _, w := range after.Widgets {
		switch w.ID {
		case 1:
			assert.Equal(t, []int64{8, 5, 4, 6}, []int64{w.X, w.Y, w.W, w.H})
			assert.Equal(t, "Open tasks", w.Title)
			assert.Equal(t, WidgetTypeNumber, w.Type, "only the grid changes")
		case 2:
			assert.Equal(t, int64(4), w.X, "widgets that are not listed keep their place")
		}
	}
	assert.True(t, !after.Updated.Before(before.Updated))

	assert.True(t, IsErrDashboardWidgetDoesNotExist(layout(cfUser1, &DashboardLayoutItem{ID: 3, X: 0, Y: 0, W: 4, H: 3})), "a widget of another dashboard")
	assert.True(t, IsErrDashboardInvalid(layout(cfUser1, &DashboardLayoutItem{ID: 1, X: 10, Y: 0, W: 4, H: 3})), "wider than the grid")
	assert.True(t, IsErrDashboardInvalid(layout(cfUser1, &DashboardLayoutItem{ID: 1, X: 0, Y: -1, W: 4, H: 3})))
	assert.True(t, IsErrDashboardInvalid(layout(cfUser1, &DashboardLayoutItem{ID: 1, X: 0, Y: 0, W: 0, H: 3})))
	assert.True(t, IsErrDashboardInvalid(layout(cfUser1, &DashboardLayoutItem{ID: 1, X: 0, Y: 0, W: 4, H: 3}, &DashboardLayoutItem{ID: 1, X: 0, Y: 0, W: 4, H: 3})))
	assert.ErrorIs(t, layout(cfUser2), ErrGenericForbidden{}, "the dashboard of someone else")
	// A failed layout changes nothing.
	for _, w := range readDash(t, 1).Widgets {
		if w.ID == 1 {
			assert.Equal(t, int64(8), w.X)
		}
	}
}

func widgetOf(typ string, cfg map[string]any) *DashboardWidget {
	return &DashboardWidget{Type: typ, W: 4, H: 3, Config: cfg}
}

func TestDashboard_Validation(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	req := func(m map[string]any) map[string]any { return map[string]any{"request": m} }

	invalid := []struct {
		name   string
		d      *Dashboard
		param  string
		actors web.Auth
	}{
		{"empty title", &Dashboard{Title: ""}, "title", cfUser1},
		{"unknown type", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("pie", nil)}}, "widgets[0].type", cfUser1},
		{"unknown config key", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("text", map[string]any{"txt": "a"})}}, "widgets[0].config", cfUser1},
		{"wrong config value type", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("text", map[string]any{"text": 5})}}, "widgets[0].config", cfUser1},
		{"text too long", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("text", map[string]any{"text": string(make([]byte, 20001))})}}, "widgets[0].config.text", cfUser1},
		{"report parameter", &Dashboard{Title: "x", Widgets: []*DashboardWidget{numberWidget("ok"), widgetOf("bar", req(map[string]any{"group_by": "nope"}))}}, "widgets[1].config.request.group_by", cfUser1},
		{"report measure", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("number", req(map[string]any{"measure": "sum"}))}}, "widgets[0].config.request.field", cfUser1},
		{"report time", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("line", req(map[string]any{"time": map[string]any{"field": "created", "from": "x"}}))}}, "widgets[0].config.request.time.from", cfUser1},
		{"too many buckets", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("line", req(map[string]any{"time": map[string]any{"field": "created", "from": "2000-01-01"}}))}}, "widgets[0].config.request.time", cfUser1},
		{"number cannot group", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("number", req(map[string]any{"group_by": "done"}))}}, "widgets[0].config.request.group_by", cfUser1},
		{"bar needs a group", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("bar", req(map[string]any{}))}}, "widgets[0].config.request.group_by", cfUser1},
		{"line needs time", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("line", req(map[string]any{"group_by": "done"}))}}, "widgets[0].config.request.time", cfUser1},
		{"donut has one level", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("donut", req(map[string]any{"group_by": "done", "then_by": "priority"}))}}, "widgets[0].config.request.then_by", cfUser1},
		{"compare needs time", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("number", map[string]any{"request": map[string]any{}, "compare": "previous_period"})}}, "widgets[0].config.request.time", cfUser1},
		{"grid overflow", &Dashboard{Title: "x", Widgets: []*DashboardWidget{{Type: "text", X: 10, W: 4, H: 1}}}, "widgets[0].w", cfUser1},
		{"project of a personal widget the user cannot read", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("task_list", map[string]any{"project_id": 3})}}, "widgets[0].config.project_id", &user.User{ID: 5}},
		{"project dashboard of another project", &Dashboard{Title: "x", ProjectID: 1, Widgets: []*DashboardWidget{widgetOf("task_list", map[string]any{"project_id": 3})}}, "widgets[0].config.project_id", cfUser1},
		{"task list limit", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("task_list", map[string]any{"limit": 101})}}, "widgets[0].config.limit", cfUser1},
		{"task list sort", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("task_list", map[string]any{"sort_by": []string{"nope"}})}}, "widgets[0].config.sort_by[0]", cfUser1},
		{"status time needs a project", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("status_time", map[string]any{})}}, "widgets[0].config.project_id", cfUser1},
		{"time tracking group", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("time_tracking", map[string]any{"group_by": "x"})}}, "widgets[0].config.group_by", cfUser1},
		{"time tracking date", &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("time_tracking", map[string]any{"from": "bogus"})}}, "widgets[0].config.from", cfUser1},
	}
	for _, c := range invalid {
		t.Run(c.name, func(t *testing.T) {
			_, err := saveDash(t, c.actors, c.d)
			var e ErrDashboardInvalid
			require.ErrorAs(t, err, &e, "%v", err)
			assert.Equal(t, c.param, e.Param)
			assert.Equal(t, 400, e.HTTPError().HTTPCode)
		})
	}

	t.Run("a filter with an unreadable custom field is the filter error", func(t *testing.T) {
		_, err := saveDash(t, cfUser2, &Dashboard{Title: "x", Widgets: []*DashboardWidget{widgetOf("task_list", map[string]any{"filter": "cf_1 = x"})}})
		assert.True(t, IsErrInvalidTaskField(err))
	})

	t.Run("too many widgets", func(t *testing.T) {
		ws := make([]*DashboardWidget, 51)
		for i := range ws {
			ws[i] = widgetOf("text", map[string]any{"text": "a"})
		}
		_, err := saveDash(t, cfUser1, &Dashboard{Title: "x", Widgets: ws})
		assert.True(t, IsErrDashboardInvalid(err))
	})

	t.Run("every type validates and is not executed", func(t *testing.T) {
		valid := []*DashboardWidget{
			widgetOf("number", map[string]any{"request": map[string]any{"measure": "avg", "field": "percent_done"}}),
			widgetOf("number", map[string]any{"request": map[string]any{"time": map[string]any{"field": "created"}}, "compare": "previous_period"}),
			widgetOf("bar", req(map[string]any{"group_by": "assignee", "then_by": "done", "limit": 5})),
			widgetOf("donut", req(map[string]any{"group_by": "priority"})),
			widgetOf("line", req(map[string]any{"group_by": "done", "time": map[string]any{"field": "done_at", "bucket": "week", "from": "now-90d", "to": "now"}})),
			widgetOf("created_vs_done", map[string]any{"bucket": "week", "from": "now-90d", "to": "now", "filter": "done = false"}),
			widgetOf("task_list", map[string]any{"filter": "due_date < now", "sort_by": []string{"due_date", "id"}, "order_by": []string{"asc"}, "columns": []string{"title"}, "limit": 10}),
			widgetOf("workload", map[string]any{"estimate_field": "cf_3", "project_id": 1}),
			widgetOf("status_time", map[string]any{"project_id": 11, "from": "2026-01-01"}),
			widgetOf("time_tracking", map[string]any{"group_by": "user", "from": "now-30d"}),
			widgetOf("text", map[string]any{"text": "# hi"}),
		}
		d, err := saveDash(t, cfUser1, &Dashboard{Title: "All types", Widgets: valid})
		require.NoError(t, err)
		assert.Len(t, d.Widgets, len(valid))
	})
}

func TestDashboard_ProjectDashboardPinsItsProject(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	d, err := saveDash(t, cfUser1, &Dashboard{Title: "P1", ProjectID: 1, Widgets: []*DashboardWidget{
		widgetOf("bar", map[string]any{"request": map[string]any{"group_by": "priority"}}),
		widgetOf("task_list", map[string]any{"project_id": 1}),
	}})
	require.NoError(t, err)
	assert.Equal(t, float64(1), d.Widgets[0].Config["request"].(map[string]any)["project_id"])
	assert.Equal(t, float64(1), d.Widgets[1].Config["project_id"])
	assert.Equal(t, int64(1), d.Owner.ID)
}

func TestDashboard_Lifecycle(t *testing.T) {
	t.Run("purging a project deletes its dashboards", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		require.NoError(t, hardDeleteProject(s, &Project{ID: 11}, nil))
		require.NoError(t, s.Commit())
		db.AssertMissing(t, "dashboards", map[string]any{"id": 5})
		db.AssertMissing(t, "dashboard_widgets", map[string]any{"dashboard_id": 5})
		db.AssertExists(t, "dashboards", map[string]any{"id": 1}, false)
		db.AssertExists(t, "dashboard_widgets", map[string]any{"dashboard_id": 1, "id": 1}, false)
	})

	t.Run("deleting a user deletes personal dashboards only", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		require.NoError(t, deletePersonalDashboardsOfUser(s, 1))
		require.NoError(t, s.Commit())
		db.AssertMissing(t, "dashboards", map[string]any{"id": 1})
		db.AssertMissing(t, "dashboard_widgets", map[string]any{"dashboard_id": 1})
		db.AssertExists(t, "dashboards", map[string]any{"id": 3}, false)
		db.AssertExists(t, "dashboards", map[string]any{"id": 2}, false)
	})

	t.Run("deleting a user through the real path", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		u, err := user.GetUserByID(s, 2)
		require.NoError(t, err)
		require.NoError(t, DeleteUser(s, u))
		require.NoError(t, s.Commit())
		db.AssertMissing(t, "dashboards", map[string]any{"id": 2})
	})

	t.Run("duplicating a project copies its dashboards with remapped ids", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		files.InitTestFileFixtures(t)

		doc := baseWorkflowDoc(1)
		source := mustSaveWorkflow(t, doc, cfUser1)
		todo := statusIDByTitle(source, "Todo")

		cfg := map[string]any{
			"request": map[string]any{
				"project_id": 1,
				"group_by":   "cf_2",
				"then_by":    "status",
				"filter":     "cf_2 = 1 && status in " + itoa(todo) + " && cf_3 > 5 && status_category = open",
			},
		}
		_, err := saveDash(t, cfUser1, &Dashboard{ID: 3, Title: "Project 1 dashboard", Widgets: []*DashboardWidget{
			widgetOf("bar", cfg),
			widgetOf("workload", map[string]any{"project_id": 1, "estimate_field": "cf_3"}),
			widgetOf("text", map[string]any{"text": "cf_2 stays"}),
		}})
		require.NoError(t, err)

		s := db.NewSession()
		defer s.Close()
		pd := &ProjectDuplicate{ProjectID: 1}
		can, err := pd.CanCreate(s, cfUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, pd.Create(s, cfUser1))
		require.NoError(t, s.Commit())

		copied := []*Dashboard{}
		s2 := db.NewSession()
		defer s2.Close()
		require.NoError(t, s2.Where("project_id = ?", pd.Project.ID).Find(&copied))
		require.Len(t, copied, 1)
		full := readDash(t, copied[0].ID)
		assert.Equal(t, int64(1), full.OwnerID)
		require.Len(t, full.Widgets, 3)

		newFields := []*CustomField{}
		require.NoError(t, s2.Where("project_id = ?", pd.Project.ID).OrderBy("id ASC").Find(&newFields))
		fieldMap := map[string]int64{}
		for _, f := range newFields {
			fieldMap[string(f.Type)] = f.ID
		}
		newWf := readWorkflow(t, pd.Project.ID)
		newTodo := statusIDByTitle(newWf, "Todo")
		require.NotEqual(t, todo, newTodo)

		raw, _ := json.Marshal(full.Widgets[0].Config)
		var got struct {
			Request ReportRequest `json:"request"`
		}
		require.NoError(t, json.Unmarshal(raw, &got))
		assert.Equal(t, pd.Project.ID, got.Request.ProjectID)
		assert.Equal(t, "cf_"+itoa(fieldMap["select"]), got.Request.GroupBy, "the group field is the copy")
		assert.NotEqual(t, "cf_2", got.Request.GroupBy)
		assert.Equal(t, "status", got.Request.ThenBy)
		assert.Contains(t, got.Request.Filter, "status in "+itoa(newTodo))
		assert.Contains(t, got.Request.Filter, "cf_"+itoa(fieldMap["select"])+" = ")
		assert.Contains(t, got.Request.Filter, "cf_"+itoa(fieldMap["number"])+" > 5", "a number is not an option id")
		assert.Contains(t, got.Request.Filter, "status_category = open")

		assert.Equal(t, "cf_"+itoa(fieldMap["number"]), full.Widgets[1].Config["estimate_field"])
		assert.Equal(t, float64(pd.Project.ID), full.Widgets[1].Config["project_id"])
		assert.Equal(t, "cf_2 stays", full.Widgets[2].Config["text"], "free text is not rewritten")

		db.AssertExists(t, "dashboards", map[string]any{"id": 3}, false)
	})
}

func TestDashboardRemap_Filter(t *testing.T) {
	m := &dashboardRemap{
		fields:       map[int64]int64{2: 20, 3: 30},
		options:      map[int64]int64{1: 11, 2: 12},
		statuses:     map[int64]int64{5: 50, 6: 60},
		optionFields: map[int64]bool{2: true},
	}
	cases := map[string]string{
		"cf_2 = 1":                             "cf_20 = 11",
		"cf_2 in 1, 2":                         "cf_20 in 11, 12",
		"cf_2 != 2 || done = false":            "cf_20 != 12 || done = false",
		"cf_3 > 2":                             "cf_30 > 2",
		"cf_3 = 2":                             "cf_30 = 2",
		"status = 5":                           "status = 50",
		"status not in 5,6":                    "status not in 50,60",
		"status_category = done":               "status_category = done",
		"cf_9 = 1":                             "cf_9 = 1",
		"(cf_2 = 1 && cf_3 < 4) || status = 6": "(cf_20 = 11 && cf_30 < 4) || status = 60",
		"":                                     "",
	}
	for in, want := range cases {
		assert.Equal(t, want, m.filter(in), in)
	}
	_ = builder.Eq{}
}
