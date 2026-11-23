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
	"context"
	"strconv"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repTime(v string) time.Time {
	tm, err := time.Parse("2006-01-02 15:04:05", v)
	if err != nil {
		panic(err)
	}
	return tm
}

type repEnv struct {
	project, project2                 int64
	num, sel, multi, chk, rating, usr int64
	optA, optB, mA, mB                int64
	stTodo, stDoing, stDone           int64
	tasks                             map[string]int64
}

func repInsertTask(t *testing.T, projectID, index int64, tk *Task) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	tk.ProjectID = projectID
	tk.Index = index
	if tk.CreatedByID == 0 {
		tk.CreatedByID = 1
	}
	if tk.Title == "" {
		tk.Title = "report task"
	}
	_, err := s.NoAutoTime().Insert(tk)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	return tk.ID
}

func repNewProject(t *testing.T, title string) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	p := &Project{Title: title}
	require.NoError(t, CreateProject(s, p, cfUser1, false, false))
	require.NoError(t, s.Commit())
	return p.ID
}

// setupReportEnv builds five tasks in a new project of user1 (see the comments) and three groups of edge case
// tasks in a second one. The cache is off so every run hits the database.
func setupReportEnv(t *testing.T) *repEnv {
	t.Helper()
	db.LoadAndAssertFixtures(t)
	config.ReportsCacheTTL.Set(0)
	t.Cleanup(func() { config.ReportsCacheTTL.Set(30 * time.Second) })
	ResetReportCache()

	env := &repEnv{tasks: map[string]int64{}}
	env.project = repNewProject(t, "Reports")
	env.project2 = repNewProject(t, "Report edges")
	p := env.project

	mustSaveWorkflow(t, baseWorkflowDoc(p), cfUser1)
	wf := readWorkflow(t, p)
	env.stTodo, env.stDoing, env.stDone = statusIDByTitle(wf, "Todo"), statusIDByTitle(wf, "Doing"), statusIDByTitle(wf, "Done")

	mk := func(title string, typ CustomFieldType, opts ...string) *CustomField {
		f := &CustomField{ProjectID: p, Title: title, Type: typ}
		for _, o := range opts {
			f.Options = append(f.Options, &CustomFieldOption{Title: o})
		}
		return createCustomField(t, f)
	}
	sel := mk("Sel", CustomFieldTypeSelect, "A", "B")
	multi := mk("Multi", CustomFieldTypeMultiSelect, "MA", "MB")
	env.sel, env.multi = sel.ID, multi.ID
	env.optA, env.optB = sel.Options[0].ID, sel.Options[1].ID
	env.mA, env.mB = multi.Options[0].ID, multi.Options[1].ID
	env.num = mk("Num", CustomFieldTypeNumber).ID
	env.chk = mk("Chk", CustomFieldTypeCheckbox).ID
	env.rating = mk("Rating", CustomFieldTypeRating).ID
	env.usr = mk("Usr", CustomFieldTypeUser).ID

	d := repTime
	env.tasks["t1"] = repInsertTask(t, p, 1, &Task{Created: d("2026-01-05 10:00:00"), Priority: 3, DueDate: d("2026-02-01 12:00:00"), PercentDone: 0.25})
	env.tasks["t2"] = repInsertTask(t, p, 2, &Task{Created: d("2026-01-11 23:30:00"), Done: true, DoneAt: d("2026-01-12 08:00:00"), Priority: 1, PercentDone: 1})
	env.tasks["t3"] = repInsertTask(t, p, 3, &Task{Created: d("2026-01-12 00:30:00"), CreatedByID: 2, PercentDone: 0.5})
	env.tasks["t4"] = repInsertTask(t, p, 4, &Task{Created: d("2026-02-28 22:00:00"), Priority: 3, DueDate: d("2026-03-01 00:00:00")})
	env.tasks["t5"] = repInsertTask(t, p, 5, &Task{Created: d("2026-03-31 23:00:00"), Done: true, DoneAt: d("2026-04-01 00:00:00"), Priority: 5})
	id := env.tasks

	rows := []any{
		&TaskAssginee{TaskID: id["t1"], UserID: 1}, &TaskAssginee{TaskID: id["t2"], UserID: 1}, &TaskAssginee{TaskID: id["t2"], UserID: 2},
		&TaskAssginee{TaskID: id["t4"], UserID: 2}, &TaskAssginee{TaskID: id["t5"], UserID: 1},
		&LabelTask{TaskID: id["t1"], LabelID: 1}, &LabelTask{TaskID: id["t2"], LabelID: 1}, &LabelTask{TaskID: id["t2"], LabelID: 2},
		&TaskStatus{TaskID: id["t1"], WorkflowID: wf.ID, StatusID: env.stTodo, Entered: d("2026-01-06 09:00:00")},
		&TaskStatus{TaskID: id["t2"], WorkflowID: wf.ID, StatusID: env.stDone, Entered: d("2026-01-12 08:00:00")},
		&TaskStatus{TaskID: id["t3"], WorkflowID: wf.ID, StatusID: env.stDoing, Entered: d("2026-02-10 09:00:00")},
		&TaskStatus{TaskID: id["t4"], WorkflowID: wf.ID, StatusID: env.stTodo, Entered: d("2026-03-02 09:00:00")},
		&TimeEntry{UserID: 1, TaskID: id["t1"], StartTime: d("2026-01-06 10:00:00"), EndTime: tp(d("2026-01-06 11:00:00"))},
		&TimeEntry{UserID: 1, TaskID: id["t1"], StartTime: d("2026-01-07 10:00:00"), EndTime: tp(d("2026-01-07 10:30:00"))},
		&TimeEntry{UserID: 1, TaskID: id["t1"], StartTime: d("2026-01-08 10:00:00")},
		&TimeEntry{UserID: 2, TaskID: id["t2"], StartTime: d("2026-01-12 10:00:00"), EndTime: tp(d("2026-01-12 12:00:00"))},
	}
	for _, r := range rows {
		insertRow(t, r)
	}
	val := func(task string, field int64, v TaskCustomFieldValue) *TaskCustomFieldValue {
		v.TaskID, v.CustomFieldID = id[task], field
		return &v
	}
	f := cfPtr[float64]
	i := cfPtr[int64]
	cfInsertValues(t,
		val("t1", env.num, TaskCustomFieldValue{ValueNumber: f(10)}),
		val("t2", env.num, TaskCustomFieldValue{ValueNumber: f(30)}),
		val("t4", env.num, TaskCustomFieldValue{ValueNumber: f(20)}),
		val("t1", env.sel, TaskCustomFieldValue{OptionID: i(env.optA)}),
		val("t2", env.sel, TaskCustomFieldValue{OptionID: i(env.optB)}),
		val("t5", env.sel, TaskCustomFieldValue{OptionID: i(env.optA)}),
		val("t1", env.multi, TaskCustomFieldValue{OptionID: i(env.mA)}),
		val("t1", env.multi, TaskCustomFieldValue{OptionID: i(env.mB)}),
		val("t2", env.multi, TaskCustomFieldValue{OptionID: i(env.mA)}),
		val("t4", env.chk, TaskCustomFieldValue{ValueBool: cfPtr(true)}),
		val("t1", env.chk, TaskCustomFieldValue{ValueBool: cfPtr(false)}),
		val("t4", env.rating, TaskCustomFieldValue{ValueNumber: f(4)}),
		val("t5", env.rating, TaskCustomFieldValue{ValueNumber: f(2)}),
		val("t1", env.usr, TaskCustomFieldValue{UserID: i(1)}),
		val("t2", env.usr, TaskCustomFieldValue{UserID: i(1)}),
		val("t2", env.usr, TaskCustomFieldValue{UserID: i(2)}),
	)

	// Edge cases for buckets: a daylight saving change in New York (2026-03-08 07:00 UTC) and a month end.
	q := env.project2
	for i, c := range []string{"2026-03-08 06:30:00", "2026-03-09 03:59:00", "2026-03-09 04:00:00", "2026-01-31 23:59:59", "2026-02-01 00:00:00"} {
		env.tasks["e"+string(rune('1'+i))] = repInsertTask(t, q, int64(i+1), &Task{Created: d(c)})
	}
	return env
}

func tp(v time.Time) *time.Time { return &v }

func runReportErr(a web.Auth, req ReportRequest) (*TaskReport, error) {
	s := db.NewSession()
	defer s.Close()
	return RunTaskReport(context.Background(), s, a, &req)
}

func runReport(t *testing.T, a web.Auth, req ReportRequest) *TaskReport {
	t.Helper()
	rep, err := runReportErr(a, req)
	require.NoError(t, err)
	return rep
}

func groupMap(groups []*ReportGroup) map[string]float64 {
	out := map[string]float64{}
	for _, g := range groups {
		out[g.Label] = g.Value
	}
	return out
}

func seriesValues(points []*ReportPoint) map[string]float64 {
	out := map[string]float64{}
	for _, p := range points {
		out[p.Bucket] = p.Value
	}
	return out
}

func TestReport_Dimensions(t *testing.T) {
	env := setupReportEnv(t)
	p := env.project
	cf := func(id int64) string { return customFieldFilterPrefix + itoa(id) }

	cases := []struct {
		name  string
		group string
		want  map[string]float64
		multi bool
	}{
		{"status", "status", map[string]float64{"Todo": 2, "Doing": 1, "Done": 1, "None": 1}, false},
		{"status_category", "status_category", map[string]float64{"Open": 2, "In progress": 1, "Done": 1, "None": 1}, false},
		{"assignee", "assignee", map[string]float64{"user1": 3, "user2": 2, "None": 1}, true},
		{"label", "label", map[string]float64{"Label #1": 2, "Label #2": 1, "None": 3}, true},
		{"project", "project", map[string]float64{"Reports": 5}, false},
		{"priority", "priority", map[string]float64{"High": 2, "Low": 1, "DO NOW": 1, "None": 1}, false},
		{"done", "done", map[string]float64{"Done": 2, "Open": 3}, false},
		{"created_by", "created_by", map[string]float64{"user1": 4, "user2": 1}, false},
		{"select", cf(env.sel), map[string]float64{"A": 2, "B": 1, "None": 2}, false},
		{"multi select", cf(env.multi), map[string]float64{"MA": 2, "MB": 1, "None": 3}, true},
		{"checkbox", cf(env.chk), map[string]float64{"Checked": 1, "Not checked": 4}, false},
		{"rating", cf(env.rating), map[string]float64{"4": 1, "2": 1, "None": 3}, false},
		{"user field", cf(env.usr), map[string]float64{"user1": 2, "user2": 1, "None": 3}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: c.group})
			assert.Equal(t, c.want, groupMap(rep.Groups))
			assert.Equal(t, int64(5), rep.Total, "total is the number of distinct tasks")
			assert.Equal(t, c.multi, rep.MultiValued)
			for _, g := range rep.Groups {
				assert.Equal(t, g.Label == "None", g.None)
				if g.None {
					assert.Nil(t, g.Key)
				}
			}
		})
	}

	t.Run("labels and colors are resolved", func(t *testing.T) {
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "status"})
		for _, g := range rep.Groups {
			if g.Label == "Todo" {
				assert.Equal(t, env.stTodo, g.Key)
			}
		}
		rep = runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "status_category"})
		keys := map[any]bool{}
		for _, g := range rep.Groups {
			keys[g.Key] = true
		}
		assert.True(t, keys["open"] && keys["in_progress"] && keys["done"])
	})

	t.Run("nested", func(t *testing.T) {
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "assignee", ThenBy: "done"})
		require.Len(t, rep.Groups, 3)
		nested := map[string]map[string]float64{}
		for _, g := range rep.Groups {
			nested[g.Label] = groupMap(g.Children)
		}
		assert.Equal(t, map[string]map[string]float64{
			"user1": {"Done": 2, "Open": 1},
			"user2": {"Done": 1, "Open": 1},
			"None":  {"Open": 1},
		}, nested)

		// A multi valued second level: a parent counts each task once even if it has several children.
		rep = runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "done", ThenBy: "assignee"})
		parents := groupMap(rep.Groups)
		assert.Equal(t, map[string]float64{"Done": 2, "Open": 3}, parents)
		for _, g := range rep.Groups {
			if g.Label == "Done" {
				assert.Equal(t, map[string]float64{"user1": 2, "user2": 1}, groupMap(g.Children))
			}
		}
	})
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func TestReport_Measures(t *testing.T) {
	env := setupReportEnv(t)
	p := env.project
	num := customFieldFilterPrefix + itoa(env.num)

	cases := []struct {
		measure, field string
		want           float64
		unit           string
	}{
		{ReportMeasureSum, num, 60, ""},
		{ReportMeasureAvg, num, 20, ""},
		{ReportMeasureMin, num, 10, ""},
		{ReportMeasureMax, num, 30, ""},
		{ReportMeasureSum, "percent_done", 1.75, ""},
		{ReportMeasureAvg, "percent_done", 0.35, ""},
		{ReportMeasureMax, "percent_done", 1, ""},
		{ReportMeasureSum, "time_spent", 12600, "seconds"},
		{ReportMeasureAvg, "time_spent", 6300, "seconds"},
		{ReportMeasureMin, "time_spent", 5400, "seconds"},
		{ReportMeasureMax, "time_spent", 7200, "seconds"},
		{ReportMeasureSum, customFieldFilterPrefix + itoa(env.rating), 6, ""},
	}
	for _, c := range cases {
		t.Run(c.measure+" "+c.field, func(t *testing.T) {
			rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, Measure: c.measure, Field: c.field})
			assert.InDelta(t, c.want, rep.Value, 0.001)
			assert.Equal(t, c.unit, rep.Unit)
		})
	}

	t.Run("count unit", func(t *testing.T) {
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p})
		assert.Equal(t, "tasks", rep.Unit)
		assert.InDelta(t, float64(5), rep.Value, 0)
	})

	t.Run("grouped sum and folding of avg", func(t *testing.T) {
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "done", Measure: ReportMeasureSum, Field: "time_spent"})
		assert.Equal(t, map[string]float64{"Open": 5400, "Done": 7200}, groupMap(rep.Groups))

		// Folded groups keep a correct average: weighted by the tasks that have a value.
		rep = runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "priority", Measure: ReportMeasureAvg, Field: "percent_done", Limit: 1})
		require.Len(t, rep.Groups, 2)
		assert.True(t, rep.Groups[1].Other)
	})

	t.Run("wrong field kind", func(t *testing.T) {
		_, err := runReportErr(cfUser1, ReportRequest{ProjectID: p, Measure: ReportMeasureSum, Field: customFieldFilterPrefix + itoa(env.sel)})
		require.Error(t, err)
		assert.True(t, IsErrReportFieldKindNotSupported(err))
		_, err = runReportErr(cfUser1, ReportRequest{ProjectID: p, GroupBy: customFieldFilterPrefix + itoa(env.num)})
		assert.True(t, IsErrReportFieldKindNotSupported(err))
	})
}

func TestReport_TopNAndOther(t *testing.T) {
	env := setupReportEnv(t)
	p := env.project

	t.Run("single valued folds by sum", func(t *testing.T) {
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "priority", Limit: 1})
		require.Len(t, rep.Groups, 2)
		assert.Equal(t, "High", rep.Groups[0].Label)
		assert.True(t, rep.Groups[1].Other)
		assert.Nil(t, rep.Groups[1].Key)
		assert.InDelta(t, float64(3), rep.Groups[1].Value, 0)
		assert.True(t, rep.Truncated)
	})

	t.Run("multi valued other counts distinct tasks", func(t *testing.T) {
		// user1 is kept; the tasks with a value other than user1 are t2, t4 (user2) and t3 (none): 3 distinct tasks.
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "assignee", Limit: 1})
		require.Len(t, rep.Groups, 2)
		assert.Equal(t, "user1", rep.Groups[0].Label)
		assert.True(t, rep.Groups[1].Other)
		assert.InDelta(t, float64(3), rep.Groups[1].Value, 0)
	})

	t.Run("nothing folded when it fits", func(t *testing.T) {
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "assignee", Limit: 3})
		assert.Len(t, rep.Groups, 3)
		assert.False(t, rep.Truncated)
	})

	t.Run("children are folded too", func(t *testing.T) {
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, GroupBy: "done", ThenBy: "assignee", Limit: 1})
		require.Len(t, rep.Groups, 2)
		open := rep.Groups[0]
		assert.Equal(t, "Open", open.Label)
		require.Len(t, open.Children, 2)
		assert.True(t, open.Children[1].Other)
		assert.Empty(t, rep.Groups[1].Children, "the other group has no children")
	})
}

func TestReport_TimeSeries(t *testing.T) {
	env := setupReportEnv(t)
	p := env.project
	tm := func(field, bucket, from, to, tz string) ReportRequest {
		return ReportRequest{ProjectID: p, Time: &ReportTime{Field: field, Bucket: bucket, From: from, To: to, Timezone: tz}}
	}

	t.Run("day buckets with zero filling", func(t *testing.T) {
		rep := runReport(t, cfUser1, tm("created", "day", "2026-01-10", "2026-01-13", "UTC"))
		assert.Equal(t, map[string]float64{"2026-01-10": 0, "2026-01-11": 1, "2026-01-12": 1, "2026-01-13": 0}, seriesValues(rep.Series))
		assert.Equal(t, int64(2), rep.Total)
		assert.Equal(t, 4, rep.Time.Buckets)
		assert.Equal(t, int64(0), rep.Excluded)
	})

	t.Run("weeks start on monday and a non UTC viewer moves tasks", func(t *testing.T) {
		rep := runReport(t, cfUser1, tm("created", "week", "2026-01-01", "2026-01-20", "UTC"))
		assert.Equal(t, map[string]float64{"2025-12-29": 0, "2026-01-05": 2, "2026-01-12": 1, "2026-01-19": 0}, seriesValues(rep.Series))
		// t2 was created Sunday 23:30 UTC, which is already Monday 02:30 in Istanbul.
		rep = runReport(t, cfUser1, tm("created", "week", "2026-01-01", "2026-01-20", "Europe/Istanbul"))
		assert.Equal(t, map[string]float64{"2025-12-29": 0, "2026-01-05": 1, "2026-01-12": 2, "2026-01-19": 0}, seriesValues(rep.Series))
		assert.Equal(t, "Europe/Istanbul", rep.Time.Timezone)
	})

	t.Run("months", func(t *testing.T) {
		rep := runReport(t, cfUser1, tm("created", "month", "2026-01-15", "2026-03-31", "UTC"))
		assert.Equal(t, map[string]float64{"2026-01-01": 3, "2026-02-01": 1, "2026-03-01": 1}, seriesValues(rep.Series))
		rep = runReport(t, cfUser1, tm("created", "month", "2026-01-15", "2026-04-01", "Europe/Istanbul"))
		assert.Equal(t, map[string]float64{"2026-01-01": 3, "2026-02-01": 0, "2026-03-01": 1, "2026-04-01": 1}, seriesValues(rep.Series))
	})

	t.Run("done_at excludes open tasks", func(t *testing.T) {
		rep := runReport(t, cfUser1, tm("done_at", "day", "2026-01-12", "2026-04-01", "UTC"))
		assert.Equal(t, int64(3), rep.Excluded)
		assert.Equal(t, int64(2), rep.Total)
		assert.InDelta(t, float64(1), seriesValues(rep.Series)["2026-01-12"], 0)
		assert.InDelta(t, float64(1), seriesValues(rep.Series)["2026-04-01"], 0, "a task at exactly midnight belongs to the bucket that starts then")
		rep = runReport(t, cfUser1, tm("done_at", "day", "2026-03-31", "2026-04-01", "America/New_York"))
		assert.InDelta(t, float64(1), seriesValues(rep.Series)["2026-03-31"], 0)
	})

	t.Run("due_date boundaries are half open", func(t *testing.T) {
		rep := runReport(t, cfUser1, tm("due_date", "month", "2026-02-01", "2026-02-28", "UTC"))
		assert.Equal(t, map[string]float64{"2026-02-01": 1}, seriesValues(rep.Series))
		rep = runReport(t, cfUser1, tm("due_date", "month", "2026-03-01", "2026-03-31", "UTC"))
		assert.Equal(t, map[string]float64{"2026-03-01": 1}, seriesValues(rep.Series))
		assert.Equal(t, int64(3), rep.Excluded)
	})

	t.Run("status_entered", func(t *testing.T) {
		rep := runReport(t, cfUser1, tm("status_entered", "month", "2026-01-01", "2026-03-01", "UTC"))
		assert.Equal(t, map[string]float64{"2026-01-01": 2, "2026-02-01": 1, "2026-03-01": 1}, seriesValues(rep.Series))
		assert.Equal(t, int64(1), rep.Excluded)
	})

	t.Run("daylight saving", func(t *testing.T) {
		req := ReportRequest{ProjectID: env.project2, Time: &ReportTime{Field: "created", Bucket: "day", From: "2026-03-08", To: "2026-03-09", Timezone: "America/New_York"}}
		rep := runReport(t, cfUser1, req)
		assert.Equal(t, map[string]float64{"2026-03-08": 2, "2026-03-09": 1}, seriesValues(rep.Series))
		assert.Equal(t, "2026-03-08T00:00:00-05:00", rep.Series[0].Start)
		assert.Equal(t, "2026-03-09T00:00:00-04:00", rep.Series[0].End)
		req.Time.Timezone = "UTC"
		rep = runReport(t, cfUser1, req)
		assert.Equal(t, map[string]float64{"2026-03-08": 1, "2026-03-09": 2}, seriesValues(rep.Series))
	})

	t.Run("month end", func(t *testing.T) {
		req := ReportRequest{ProjectID: env.project2, Time: &ReportTime{Field: "created", Bucket: "month", From: "2026-01-01", To: "2026-02-28", Timezone: "UTC"}}
		rep := runReport(t, cfUser1, req)
		assert.Equal(t, map[string]float64{"2026-01-01": 1, "2026-02-01": 1}, seriesValues(rep.Series))
	})

	t.Run("user time zone setting is the default", func(t *testing.T) {
		s := db.NewSession()
		_, err := s.ID(1).Cols("timezone").Update(&user.User{Timezone: "Europe/Istanbul"})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		rep := runReport(t, cfUser1, tm("created", "week", "2026-01-05", "2026-01-12", ""))
		assert.Equal(t, "Europe/Istanbul", rep.Time.Timezone)
		assert.Equal(t, map[string]float64{"2026-01-05": 1, "2026-01-12": 2}, seriesValues(rep.Series))
	})

	t.Run("relative dates", func(t *testing.T) {
		rep := runReport(t, cfUser1, ReportRequest{ProjectID: p, Time: &ReportTime{Field: "created", Bucket: "week"}})
		assert.Equal(t, int64(0), rep.Total)
		assert.GreaterOrEqual(t, rep.Time.Buckets, 13)
	})

	t.Run("grouped series", func(t *testing.T) {
		req := tm("created", "month", "2026-01-01", "2026-03-31", "UTC")
		req.GroupBy = "assignee"
		rep := runReport(t, cfUser1, req)
		by := map[string]map[string]float64{}
		for _, g := range rep.Groups {
			by[g.Label] = seriesValues(g.Series)
		}
		assert.Equal(t, map[string]float64{"2026-01-01": 2, "2026-02-01": 0, "2026-03-01": 1}, by["user1"])
		assert.Equal(t, map[string]float64{"2026-01-01": 1, "2026-02-01": 1, "2026-03-01": 0}, by["user2"])
		assert.Equal(t, map[string]float64{"2026-01-01": 1, "2026-02-01": 0, "2026-03-01": 0}, by["None"])
		assert.InDelta(t, float64(3), groupMap(rep.Groups)["user1"], 0)
	})

	t.Run("grouped series with folded other", func(t *testing.T) {
		req := tm("created", "month", "2026-01-01", "2026-03-31", "UTC")
		req.GroupBy = "assignee"
		req.Limit = 1
		rep := runReport(t, cfUser1, req)
		require.Len(t, rep.Groups, 2)
		other := rep.Groups[1]
		assert.True(t, other.Other)
		assert.Equal(t, map[string]float64{"2026-01-01": 2, "2026-02-01": 1, "2026-03-01": 0}, seriesValues(other.Series))
	})

	t.Run("nested series on the leaves", func(t *testing.T) {
		req := tm("created", "month", "2026-01-01", "2026-03-31", "UTC")
		req.GroupBy = "done"
		req.ThenBy = "assignee"
		rep := runReport(t, cfUser1, req)
		for _, g := range rep.Groups {
			assert.Empty(t, g.Series)
			for _, c := range g.Children {
				assert.Len(t, c.Series, 3)
			}
		}
	})

	t.Run("measure in a series", func(t *testing.T) {
		req := tm("created", "month", "2026-01-01", "2026-03-31", "UTC")
		req.Measure, req.Field = ReportMeasureSum, "percent_done"
		rep := runReport(t, cfUser1, req)
		assert.InDelta(t, 1.75, seriesValues(rep.Series)["2026-01-01"], 0.001)
	})
}

func TestReport_ValidationAndLimits(t *testing.T) {
	env := setupReportEnv(t)
	p := env.project

	cases := []struct {
		name  string
		req   ReportRequest
		param string
	}{
		{"unknown dimension", ReportRequest{GroupBy: "nope"}, "group_by"},
		{"then_by needs group_by", ReportRequest{ThenBy: "done"}, "then_by"},
		{"same dimensions", ReportRequest{GroupBy: "done", ThenBy: "done"}, "then_by"},
		{"unknown then_by", ReportRequest{GroupBy: "done", ThenBy: "x"}, "then_by"},
		{"unknown measure", ReportRequest{Measure: "median"}, "measure"},
		{"sum needs field", ReportRequest{Measure: "sum"}, "field"},
		{"count takes no field", ReportRequest{Field: "percent_done"}, "field"},
		{"unknown field", ReportRequest{Measure: "sum", Field: "title"}, "field"},
		{"negative limit", ReportRequest{Limit: -1}, "limit"},
		{"bad project", ReportRequest{ProjectID: -5}, "project_id"},
		{"bad time field", ReportRequest{Time: &ReportTime{Field: "updated"}}, "time.field"},
		{"bad bucket", ReportRequest{Time: &ReportTime{Field: "created", Bucket: "hour"}}, "time.bucket"},
		{"bad from", ReportRequest{Time: &ReportTime{Field: "created", From: "yesterday-ish"}}, "time.from"},
		{"bad to", ReportRequest{Time: &ReportTime{Field: "created", To: "2026-13-45"}}, "time.to"},
		{"to before from", ReportRequest{Time: &ReportTime{Field: "created", From: "2026-02-01", To: "2026-01-01"}}, "time.to"},
		{"bad time zone", ReportRequest{Time: &ReportTime{Field: "created", Timezone: "Mars/Base"}}, "time.timezone"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.req.ProjectID = max(c.req.ProjectID, 0)
			if c.name == "bad project" {
				c.req.ProjectID = -5
			} else {
				c.req.ProjectID = p
			}
			_, err := runReportErr(cfUser1, c.req)
			var invalid ErrReportInvalidParameter
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, c.param, invalid.Param)
			assert.Equal(t, 400, invalid.HTTPError().HTTPCode)
		})
	}

	t.Run("too many buckets", func(t *testing.T) {
		_, err := runReportErr(cfUser1, ReportRequest{ProjectID: p, Time: &ReportTime{Field: "created", Bucket: "day", From: "2024-01-01", To: "2026-01-01"}})
		require.Error(t, err)
		assert.True(t, IsErrReportTooManyBuckets(err))
		rep, err := runReportErr(cfUser1, ReportRequest{ProjectID: p, Time: &ReportTime{Field: "created", Bucket: "day", From: "2025-01-01", To: "2025-12-31"}})
		require.NoError(t, err)
		assert.Equal(t, 365, rep.Time.Buckets)
		rep, err = runReportErr(cfUser1, ReportRequest{ProjectID: p, Time: &ReportTime{Field: "created", Bucket: "month", From: "2000-01-01", To: "2026-01-01"}})
		require.NoError(t, err)
		assert.Equal(t, 313, rep.Time.Buckets)
	})

	t.Run("too many groups", func(t *testing.T) {
		_, err := runReportErr(cfUser1, ReportRequest{ProjectID: p, GroupBy: "done", Limit: 201})
		assert.True(t, IsErrReportTooManyGroups(err))
		config.ReportsMaxGroups.Set(5)
		defer config.ReportsMaxGroups.Set(200)
		_, err = runReportErr(cfUser1, ReportRequest{ProjectID: p, GroupBy: "done", Limit: 6})
		assert.True(t, IsErrReportTooManyGroups(err))
	})

	t.Run("bad filter is the filter error", func(t *testing.T) {
		_, err := runReportErr(cfUser1, ReportRequest{ProjectID: p, Filter: "nonsense = 1"})
		require.Error(t, err)
		assert.True(t, IsErrInvalidTaskField(err))
	})

	t.Run("a project that cannot be read", func(t *testing.T) {
		_, err := runReportErr(cfUser2, ReportRequest{ProjectID: p})
		require.Error(t, err)
	})

	t.Run("a cancelled context is a timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		s := db.NewSession()
		defer s.Close()
		_, err := RunTaskReport(ctx, s, cfUser1, &ReportRequest{ProjectID: p, GroupBy: "assignee"})
		require.Error(t, err)
		assert.True(t, IsErrReportTimeout(err))
	})
}

func TestReport_Cache(t *testing.T) {
	env := setupReportEnv(t)
	p := env.project
	config.ReportsCacheTTL.Set(time.Hour)
	ResetReportCache()

	first := runReport(t, cfUser1, ReportRequest{ProjectID: p})
	assert.Equal(t, int64(5), first.Total)
	repInsertTask(t, p, 99, &Task{Created: repTime("2026-01-01 00:00:00")})

	assert.Equal(t, int64(5), runReport(t, cfUser1, ReportRequest{ProjectID: p}).Total, "served from the cache within the ttl")
	assert.Equal(t, int64(6), runReport(t, cfUser1, ReportRequest{ProjectID: p, Filter: "done = false || done = true"}).Total, "another request is another entry")

	// Another viewer never gets the entry of the first: user2 can read nothing of the project.
	_, err := runReportErr(cfUser2, ReportRequest{ProjectID: p})
	require.Error(t, err)

	// An equivalent request (defaults spelled out) shares the entry.
	assert.Equal(t, int64(5), runReport(t, cfUser1, ReportRequest{ProjectID: p, Measure: "count", Limit: 20}).Total)

	ResetReportCache()
	assert.Equal(t, int64(6), runReport(t, cfUser1, ReportRequest{ProjectID: p}).Total)

	config.ReportsCacheTTL.Set(0)
	repInsertTask(t, p, 100, &Task{Created: repTime("2026-01-01 00:00:00")})
	assert.Equal(t, int64(7), runReport(t, cfUser1, ReportRequest{ProjectID: p}).Total, "a ttl of 0 disables the cache")

	t.Run("the cache is bounded", func(t *testing.T) {
		config.ReportsCacheTTL.Set(time.Hour)
		ResetReportCache()
		for i := 0; i < reportCacheSize+20; i++ {
			reportCachePut("k"+time.Duration(i).String(), &TaskReport{})
		}
		reportCache.Lock()
		defer reportCache.Unlock()
		assert.LessOrEqual(t, len(reportCache.entries), reportCacheSize)
	})
}

func TestReport_Options(t *testing.T) {
	env := setupReportEnv(t)
	opts, err := func() (*ReportOptions, error) {
		s := db.NewSession()
		defer s.Close()
		return GetReportOptions(s, cfUser1, env.project)
	}()
	require.NoError(t, err)
	assert.True(t, opts.HasWorkflow)
	names := map[string]bool{}
	for _, d := range opts.Dimensions {
		names[d.Name] = true
	}
	for _, want := range []string{"status", "assignee", "label", customFieldFilterPrefix + itoa(env.sel), customFieldFilterPrefix + itoa(env.rating)} {
		assert.True(t, names[want], want)
	}
	assert.False(t, names[customFieldFilterPrefix+itoa(env.num)], "numbers are measures, not dimensions")
	assert.Len(t, opts.Measures, 5)
	assert.Len(t, opts.TimeFields, 4)

	s := db.NewSession()
	defer s.Close()
	noWorkflow, err := GetReportOptions(s, cfUser1, 1)
	require.NoError(t, err)
	assert.False(t, noWorkflow.HasWorkflow)
	assert.Len(t, noWorkflow.TimeFields, 3)

	_, err = GetReportOptions(s, cfUser2, env.project)
	require.Error(t, err, "options of a project you cannot read")

	// Custom fields of unreadable projects are not listed in the global scope.
	all, err := GetReportOptions(s, cfUser2, 0)
	require.NoError(t, err)
	for _, d := range all.Dimensions {
		assert.NotEqual(t, env.project, d.ProjectID)
	}
}

func taskListTotal(a web.Auth, projectID int64, filter string) (int64, error) {
	s := db.NewSession()
	defer s.Close()
	tc := &TaskCollection{ProjectID: projectID, Filter: filter}
	_, _, total, err := tc.ReadAll(s, a, "", 1, 1000)
	return total, err
}

// The report of every viewer counts exactly the tasks the task list returns for the same filter, whatever the access
// comes from, and it never names a group it could not have found through the list.
func TestReport_PermissionMatrix(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	config.ReportsCacheTTL.Set(0)
	ResetReportCache()

	viewers := map[string]web.Auth{
		"owner and shares (user1)":    cfUser1,
		"team member (user2)":         cfUser2,
		"project owner (user3)":       &user.User{ID: 3},
		"no access to project 1 (u5)": &user.User{ID: 5},
		"read only link share, p1":    &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead},
		"link share, p2":              &LinkSharing{ID: 2, ProjectID: 2, Permission: PermissionWrite},
		"user with only a team (u6)":  &user.User{ID: 6},
	}
	filters := []string{
		"",
		"done = false",
		"priority > 0",
		"labels = 4",
		"cf_2 = 1",
		"cf_3 > 5",
		"cf_4 in 3,4",
		"status = 1",
		"status_category = open",
		"status != 1",
		"cf_2 = 1 || status_category = done",
	}
	for name, viewer := range viewers {
		for _, filter := range filters {
			for _, project := range []int64{0, 1, 3, 11} {
				t.Run(name+"/"+filter+"/p"+itoa(project), func(t *testing.T) {
					want, listErr := taskListTotal(viewer, project, filter)
					rep, repErr := runReportErr(viewer, ReportRequest{ProjectID: project, Filter: filter})
					if listErr != nil {
						require.Error(t, repErr, "the report must refuse what the list refuses")
						// Which of several problems is named first may differ, a refusal stays a refusal.
						var le, re web.HTTPErrorProcessor
						require.ErrorAs(t, listErr, &le)
						require.ErrorAs(t, repErr, &re)
						assert.Less(t, re.HTTPError().HTTPCode, 500)
						return
					}
					require.NoError(t, repErr)
					assert.Equal(t, want, rep.Total)
					assert.InDelta(t, float64(want), rep.Value, 0)

					// Every single valued dimension partitions exactly the same tasks.
					for _, dim := range []string{"priority", "done", "project", "status_category"} {
						g := runReport(t, viewer, ReportRequest{ProjectID: project, Filter: filter, GroupBy: dim})
						sum := 0.0
						for _, grp := range g.Groups {
							sum += grp.Value
						}
						assert.InDelta(t, float64(want), sum, 0, dim)
						assert.Equal(t, want, g.Total, dim)
					}
				})
			}
		}
	}
}

func TestReport_NoLeaks(t *testing.T) {
	env := setupReportEnv(t)
	share := &user.User{ID: 2}

	t.Run("a custom field of a project the viewer cannot read is the filter error", func(t *testing.T) {
		_, filterErr := taskListTotal(share, 0, "cf_"+itoa(env.sel)+" = 1")
		require.Error(t, filterErr)
		for _, req := range []ReportRequest{
			{Filter: "cf_" + itoa(env.sel) + " = 1"},
			{GroupBy: "cf_" + itoa(env.sel)},
			{GroupBy: "cf_" + itoa(env.sel), Time: &ReportTime{Field: "created"}},
			{Measure: "sum", Field: "cf_" + itoa(env.num)},
			{GroupBy: "done", ThenBy: "cf_" + itoa(env.usr)},
		} {
			_, err := runReportErr(share, req)
			require.Error(t, err)
			assert.True(t, IsErrInvalidTaskField(err), "%+v", req)
			assert.Equal(t, filterErr.(ErrInvalidTaskField).HTTPError().Code, err.(ErrInvalidTaskField).HTTPError().Code)
		}
		// Not even the existence of the field can be probed.
		_, err := runReportErr(share, ReportRequest{GroupBy: "cf_999999"})
		assert.True(t, IsErrInvalidTaskField(err))
	})

	t.Run("names come only from visible tasks", func(t *testing.T) {
		// user2 has no access to the report project: its assignees, labels and statuses never show up.
		for _, dim := range []string{"assignee", "label", "status", "created_by", "project"} {
			rep := runReport(t, share, ReportRequest{GroupBy: dim})
			for _, g := range rep.Groups {
				assert.NotEqual(t, "Reports", g.Label, dim)
				assert.NotEqual(t, "Todo", g.Label, dim)
			}
		}
		rep := runReport(t, share, ReportRequest{GroupBy: "label"})
		for _, g := range rep.Groups {
			assert.NotEqual(t, "Label #6 - private, attached only to private task", g.Label)
		}
	})

	t.Run("sharing the project opens it up, revoking closes it", func(t *testing.T) {
		insertRow(t, &ProjectUser{UserID: 2, ProjectID: env.project, Permission: PermissionRead})
		rep := runReport(t, share, ReportRequest{ProjectID: env.project, GroupBy: "status"})
		assert.Equal(t, int64(5), rep.Total)
		want, err := taskListTotal(share, env.project, "")
		require.NoError(t, err)
		assert.Equal(t, want, rep.Total)
		// The read only share cannot ask for tracked time through a link share, but a user can.
		rep = runReport(t, share, ReportRequest{ProjectID: env.project, Measure: "sum", Field: "time_spent"})
		assert.InDelta(t, float64(12600), rep.Value, 0)
	})
}

func TestReport_LinkShare(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	config.ReportsCacheTTL.Set(0)
	ResetReportCache()
	share := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}

	t.Run("a link share only sees its project", func(t *testing.T) {
		rep := runReport(t, share, ReportRequest{GroupBy: "project"})
		require.Len(t, rep.Groups, 1)
		assert.Equal(t, int64(1), rep.Groups[0].Key)
		want, err := taskListTotal(share, 0, "")
		require.NoError(t, err)
		assert.Equal(t, want, rep.Total)

		// Asking for another project does not escape: the share is pinned like the list is.
		other := runReport(t, share, ReportRequest{ProjectID: 3})
		assert.Equal(t, want, other.Total)
	})

	t.Run("time spent is not available", func(t *testing.T) {
		_, err := runReportErr(share, ReportRequest{Measure: "sum", Field: "time_spent"})
		require.Error(t, err)
		assert.True(t, IsErrReportNotAvailableForShare(err))
		assert.Equal(t, 403, err.(ErrReportNotAvailableForShare).HTTPError().HTTPCode)
	})

	t.Run("the cache does not mix a share with a user", func(t *testing.T) {
		config.ReportsCacheTTL.Set(time.Hour)
		defer config.ReportsCacheTTL.Set(0)
		ResetReportCache()
		asUser := runReport(t, cfUser1, ReportRequest{})
		asShare := runReport(t, share, ReportRequest{})
		assert.Greater(t, asUser.Total, asShare.Total)
		// user 1 and the link share with id 1 have the same numeric id.
		assert.Equal(t, asShare.Total, runReport(t, share, ReportRequest{}).Total)
		assert.Equal(t, asUser.Total, runReport(t, cfUser1, ReportRequest{}).Total)
	})

	t.Run("options never offer time spent", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		opts, err := GetReportOptions(s, share, 0)
		require.NoError(t, err)
		for _, m := range opts.Measures {
			for _, f := range m.Fields {
				assert.NotEqual(t, ReportFieldTimeSpent, f.Name)
			}
		}
	})
}
