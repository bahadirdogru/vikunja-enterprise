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
	"bytes"
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseCSV checks the byte order mark and returns the records.
func parseCSV(t *testing.T, raw []byte) [][]string {
	t.Helper()
	require.True(t, bytes.HasPrefix(raw, []byte(csvBOM)), "the file starts with a UTF-8 byte order mark")
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte(csvBOM))))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	require.NoError(t, err)
	return records
}

func csvColumn(t *testing.T, records [][]string, header string) int {
	t.Helper()
	for i, h := range records[0] {
		if h == header {
			return i
		}
	}
	require.Failf(t, "column not found", "%q in %v", header, records[0])
	return -1
}

func TestCSV_Encoding(t *testing.T) {
	t.Run("formula injection guard", func(t *testing.T) {
		for _, v := range []string{"=1+1", "+1", "-1", "@SUM(A1)", "\tcmd", "\rcmd"} {
			assert.Equal(t, "'"+v, csvSafeCell(v), v)
		}
		for _, v := range []string{"", "plain", "a=b", "1+1", "'quoted"} {
			assert.Equal(t, v, csvSafeCell(v), v)
		}
	})

	t.Run("escaping, byte order mark and Turkish characters", func(t *testing.T) {
		table := &CSVTable{
			Header: []string{"Başlık", "=header"},
			Rows: [][]CSVCell{
				{textCell("Çalışma, \"alıntı\"\nikinci satır"), textCell("=HYPERLINK(\"http://evil\")")},
				{textCell("İğdır şğüöç"), rawCell("-5")},
				{textCell("-dash"), textCell("@user")},
			},
		}
		raw, err := table.Bytes()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(string(raw), csvBOM))
		assert.Contains(t, string(raw), "\r\n", "CRLF line ends, as Excel expects")
		records := parseCSV(t, raw)
		require.Len(t, records, 4)
		assert.Equal(t, []string{"Başlık", "'=header"}, records[0])
		assert.Equal(t, "Çalışma, \"alıntı\"\nikinci satır", records[1][0])
		assert.Equal(t, "'=HYPERLINK(\"http://evil\")", records[1][1])
		assert.Equal(t, "İğdır şğüöç", records[2][0])
		assert.Equal(t, "-5", records[2][1], "numbers formatted by the server are not guarded")
		assert.Equal(t, []string{"'-dash", "'@user"}, records[3])
	})

	t.Run("invalid utf-8 is replaced", func(t *testing.T) {
		raw, err := (&CSVTable{Header: []string{"a"}, Rows: [][]CSVCell{{textCell("x\xffy")}}}).Bytes()
		require.NoError(t, err)
		assert.Equal(t, "x�y", parseCSV(t, raw)[1][0])
	})
}

func TestReport_CSV(t *testing.T) {
	env := setupReportEnv(t)
	run := func(a web.Auth, req ReportRequest) [][]string {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		raw, err := RunTaskReportCSV(context.Background(), s, a, &req)
		require.NoError(t, err)
		return parseCSV(t, raw)
	}

	t.Run("a single figure", func(t *testing.T) {
		rec := run(cfUser1, ReportRequest{ProjectID: env.project})
		assert.Equal(t, [][]string{{"Measure", "Value"}, {"Tasks", "5"}}, rec)
	})

	t.Run("groups equal the report", func(t *testing.T) {
		req := ReportRequest{ProjectID: env.project, GroupBy: "done"}
		rep := runReport(t, cfUser1, req)
		rec := run(cfUser1, req)
		assert.Equal(t, []string{"Group", "Tasks"}, rec[0])
		require.Len(t, rec, len(rep.Groups)+1)
		for i, g := range rep.Groups {
			assert.Equal(t, g.Label, rec[i+1][0])
			assert.Equal(t, csvNumber(g.Value), rec[i+1][1])
		}
	})

	t.Run("nested groups and sums", func(t *testing.T) {
		rec := run(cfUser1, ReportRequest{ProjectID: env.project, GroupBy: "done", ThenBy: "priority", Measure: "sum", Field: "cf_" + itoa(env.num)})
		assert.Equal(t, []string{"Group", "Subgroup", "sum of cf_" + itoa(env.num)}, rec[0])
		assert.Greater(t, len(rec), 2)
		for _, row := range rec[1:] {
			assert.Len(t, row, 3)
		}
	})

	t.Run("time series", func(t *testing.T) {
		req := ReportRequest{ProjectID: env.project, Time: &ReportTime{Field: "created", Bucket: "month", From: "2026-01-01", To: "2026-03-31", Timezone: "UTC"}}
		rec := run(cfUser1, req)
		assert.Equal(t, [][]string{{"Bucket", "Tasks"}, {"2026-01-01", "3"}, {"2026-02-01", "1"}, {"2026-03-01", "1"}}, rec)

		req.GroupBy = "done"
		rec = run(cfUser1, req)
		assert.Equal(t, []string{"Group", "Bucket", "Tasks"}, rec[0])
		assert.Len(t, rec, 1+2*3)
	})

	t.Run("a group named like a formula is neutralized", func(t *testing.T) {
		label := &Label{Title: "=cmd|' /C calc'!A0", CreatedByID: 1}
		s := db.NewSession()
		_, err := s.Insert(label)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		insertRow(t, &LabelTask{TaskID: env.tasks["t3"], LabelID: label.ID})
		rec := run(cfUser1, ReportRequest{ProjectID: env.project, GroupBy: "label"})
		found := false
		for _, row := range rec[1:] {
			assert.False(t, strings.HasPrefix(row[0], "="), row[0])
			if row[0] == "'=cmd|' /C calc'!A0" {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("a viewer without access gets no numbers", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		_, err := RunTaskReportCSV(context.Background(), s, cfUser2, &ReportRequest{ProjectID: env.project})
		require.Error(t, err)
		rec := run(cfUser2, ReportRequest{})
		for _, row := range rec {
			assert.NotContains(t, strings.Join(row, ","), "5", "user2 sees none of the five tasks of the project")
		}
	})
}

func TestReport_ExportTasksCSV(t *testing.T) {
	env := setupReportEnv(t)
	export := func(a web.Auth, req TaskExportRequest) ([][]string, error) {
		s := db.NewSession()
		defer s.Close()
		raw, err := ExportTasksCSV(context.Background(), s, a, &req)
		if err != nil {
			return nil, err
		}
		return parseCSV(t, raw), nil
	}
	idsOf := func(rec [][]string) map[string]bool {
		out := map[string]bool{}
		col := csvColumn(t, rec, "ID")
		for _, row := range rec[1:] {
			out[row[col]] = true
		}
		return out
	}

	t.Run("core and custom field columns", func(t *testing.T) {
		// A title that would run as a formula, and Turkish characters with a comma and a quote.
		s := db.NewSession()
		_, err := s.ID(env.tasks["t1"]).Cols("title").Update(&Task{Title: "=SUM(A1:A9)"})
		require.NoError(t, err)
		_, err = s.ID(env.tasks["t2"]).Cols("title").Update(&Task{Title: "Çağrı, \"öncelikli\" şirket"})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()

		rec, err := export(cfUser1, TaskExportRequest{ProjectID: env.project, Timezone: "UTC"})
		require.NoError(t, err)
		require.Len(t, rec, 6)
		for _, h := range []string{"ID", "Identifier", "Title", "Project", "Status", "Done", "Priority", "Due date", "Assignees", "Labels", "Created by"} {
			csvColumn(t, rec, h)
		}
		titleCol, idCol := csvColumn(t, rec, "Title"), csvColumn(t, rec, "ID")
		byID := map[string][]string{}
		for _, row := range rec[1:] {
			byID[row[idCol]] = row
		}
		t1, t2 := byID[itoa(env.tasks["t1"])], byID[itoa(env.tasks["t2"])]
		require.NotNil(t, t1)
		require.NotNil(t, t2)
		assert.Equal(t, "'=SUM(A1:A9)", t1[titleCol])
		assert.Equal(t, "Çağrı, \"öncelikli\" şirket", t2[titleCol])
		assert.Equal(t, "Reports", t1[csvColumn(t, rec, "Project")])
		assert.Equal(t, "Todo", t1[csvColumn(t, rec, "Status")])
		assert.Equal(t, "Done", t2[csvColumn(t, rec, "Status")])
		assert.Equal(t, "No", t1[csvColumn(t, rec, "Done")])
		assert.Equal(t, "Yes", t2[csvColumn(t, rec, "Done")])
		assert.Equal(t, "2026-02-01 12:00:00", t1[csvColumn(t, rec, "Due date")])
		assert.Equal(t, "25", t1[csvColumn(t, rec, "Percent done")])
		assert.Equal(t, "3", t1[csvColumn(t, rec, "Priority")])

		// Custom fields: numbers, select option titles, multi select, checkbox, user.
		assert.Equal(t, "10", t1[csvColumn(t, rec, "Custom field: Num")])
		assert.Equal(t, "A", t1[csvColumn(t, rec, "Custom field: Sel")])
		assert.Equal(t, "B", t2[csvColumn(t, rec, "Custom field: Sel")])
		assert.Equal(t, "MA; MB", t1[csvColumn(t, rec, "Custom field: Multi")])
		assert.Equal(t, "No", t1[csvColumn(t, rec, "Custom field: Chk")])
		assert.NotEmpty(t, t1[csvColumn(t, rec, "Custom field: Usr")])
		assert.Empty(t, byID[itoa(env.tasks["t3"])][csvColumn(t, rec, "Custom field: Num")])
	})

	t.Run("it exports exactly the tasks the list shows", func(t *testing.T) {
		for _, filter := range []string{"", "done = false", "priority > 0", "cf_" + itoa(env.num) + " > 15", "status_category = open"} {
			for _, viewer := range []web.Auth{cfUser1, &user.User{ID: 3}, &user.User{ID: 5}, &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}} {
				for _, project := range []int64{0, 1, env.project} {
					rec, err := export(viewer, TaskExportRequest{ProjectID: project, Filter: filter})
					want, listErr := taskListTotal(viewer, project, filter)
					if listErr != nil {
						require.Error(t, err, "the export refuses what the list refuses")
						continue
					}
					require.NoError(t, err)
					assert.Equal(t, int(want), len(rec)-1, "viewer %v project %d filter %q", viewer, project, filter)
				}
			}
		}
	})

	t.Run("a user without access to the project gets nothing", func(t *testing.T) {
		_, err := export(cfUser2, TaskExportRequest{ProjectID: env.project})
		require.Error(t, err)

		rec, err := export(cfUser2, TaskExportRequest{})
		require.NoError(t, err)
		for id := range idsOf(rec) {
			for _, own := range env.tasks {
				assert.NotEqual(t, itoa(own), id, "a task of a project user2 cannot read")
			}
		}
		// Custom fields of the project do not show up as columns either.
		for _, h := range rec[0] {
			assert.NotContains(t, h, "Custom field: Num")
		}

		// A field of a project the viewer cannot read is the filter error, not a probe.
		_, err = export(cfUser2, TaskExportRequest{Filter: "cf_" + itoa(env.sel) + " = 1"})
		require.Error(t, err)
		assert.True(t, IsErrInvalidTaskField(err))
	})

	t.Run("sharing opens it, the file follows", func(t *testing.T) {
		insertRow(t, &ProjectUser{UserID: 2, ProjectID: env.project, Permission: PermissionRead})
		rec, err := export(cfUser2, TaskExportRequest{ProjectID: env.project})
		require.NoError(t, err)
		assert.Len(t, rec, 6)
	})

	t.Run("the row cap is a typed error", func(t *testing.T) {
		config.ReportsMaxRows.Set(3)
		defer config.ReportsMaxRows.Set(50000)
		_, err := export(cfUser1, TaskExportRequest{ProjectID: env.project})
		require.Error(t, err)
		require.True(t, IsErrReportTooManyRows(err))
		assert.Equal(t, ErrCodeReportTooManyRows, err.(ErrReportTooManyRows).HTTPError().Code)
		// A filter that brings it below the cap works.
		rec, err := export(cfUser1, TaskExportRequest{ProjectID: env.project, Filter: "done = true"})
		require.NoError(t, err)
		assert.Len(t, rec, 3)
	})

	t.Run("validation", func(t *testing.T) {
		_, err := export(cfUser1, TaskExportRequest{ProjectID: -5})
		assert.True(t, IsErrReportInvalidParameter(err))
		_, err = export(cfUser1, TaskExportRequest{Timezone: "Mars/Base"})
		assert.True(t, IsErrReportInvalidParameter(err))
		_, err = export(cfUser1, TaskExportRequest{Filter: "bogus ="})
		require.Error(t, err)
	})
}

func TestReport_ComparePreviousPeriod(t *testing.T) {
	env := setupReportEnv(t)
	req := func(from, to, bucket string) ReportRequest {
		return ReportRequest{
			ProjectID: env.project, Compare: "previous_period",
			Time: &ReportTime{Field: "created", Bucket: bucket, From: from, To: to, Timezone: "UTC"},
		}
	}

	t.Run("months", func(t *testing.T) {
		// Feb and Mar: t4 and t5; the two months before: Dec and Jan: t1, t2, t3.
		rep := runReport(t, cfUser1, req("2026-02-01", "2026-03-31", "month"))
		assert.Equal(t, float64(2), rep.Value)
		require.NotNil(t, rep.Comparison)
		assert.Equal(t, float64(3), rep.Comparison.Value)
		assert.Equal(t, int64(3), rep.Comparison.Total)
		assert.Equal(t, float64(-1), rep.Comparison.Delta)
		require.NotNil(t, rep.Comparison.DeltaPercent)
		assert.InDelta(t, -33.3333, *rep.Comparison.DeltaPercent, 0.001)
		assert.Equal(t, "2025-12-01T00:00:00Z", rep.Comparison.From)
		assert.Equal(t, "2026-02-01T00:00:00Z", rep.Comparison.To)
	})

	t.Run("days, weeks and a previous period without tasks", func(t *testing.T) {
		rep := runReport(t, cfUser1, req("2026-01-12", "2026-01-12", "day"))
		assert.Equal(t, float64(1), rep.Value, "t3")
		require.NotNil(t, rep.Comparison)
		assert.Equal(t, float64(1), rep.Comparison.Value, "t2 was created the day before")
		assert.Equal(t, float64(0), rep.Comparison.Delta)
		require.NotNil(t, rep.Comparison.DeltaPercent)
		assert.Equal(t, float64(0), *rep.Comparison.DeltaPercent)

		// Week of Jan 5 (Mon) .. Jan 11: t1, t2; the week before has nothing: no percentage.
		rep = runReport(t, cfUser1, req("2026-01-05", "2026-01-11", "week"))
		assert.Equal(t, float64(2), rep.Value)
		assert.Equal(t, float64(0), rep.Comparison.Value)
		assert.Equal(t, float64(2), rep.Comparison.Delta)
		assert.Nil(t, rep.Comparison.DeltaPercent)
		assert.Equal(t, "2025-12-29T00:00:00Z", rep.Comparison.From)
	})

	t.Run("sums over the previous period", func(t *testing.T) {
		r := req("2026-02-01", "2026-03-31", "month")
		r.Measure, r.Field = "sum", "cf_"+itoa(env.num)
		rep := runReport(t, cfUser1, r)
		assert.Equal(t, float64(20), rep.Value)
		assert.Equal(t, float64(40), rep.Comparison.Value)
	})

	t.Run("without compare there is no comparison", func(t *testing.T) {
		r := req("2026-02-01", "2026-03-31", "month")
		r.Compare = ""
		assert.Nil(t, runReport(t, cfUser1, r).Comparison)
	})

	t.Run("validation", func(t *testing.T) {
		_, err := runReportErr(cfUser1, ReportRequest{Compare: "previous_period"})
		require.True(t, IsErrReportInvalidParameter(err))
		assert.Equal(t, "compare", err.(ErrReportInvalidParameter).Param)

		r := req("2026-02-01", "2026-03-31", "month")
		r.GroupBy = "done"
		_, err = runReportErr(cfUser1, r)
		require.True(t, IsErrReportInvalidParameter(err))
		assert.Equal(t, "compare", err.(ErrReportInvalidParameter).Param)

		r = req("2026-02-01", "2026-03-31", "month")
		r.Compare = "last_year"
		_, err = runReportErr(cfUser1, r)
		require.True(t, IsErrReportInvalidParameter(err))
	})

	t.Run("both periods only count readable tasks", func(t *testing.T) {
		_, err := runReportErr(cfUser2, req("2026-02-01", "2026-03-31", "month"))
		require.Error(t, err, "the project is not readable")

		r := req("2026-01-01", "2026-03-31", "month")
		r.ProjectID = 0
		rep := runReport(t, cfUser2, r)
		require.NotNil(t, rep.Comparison)
		// None of the five tasks of the private report project, in either period.
		own, err := taskListTotal(cfUser2, 0, "created >= 2026-01-01 && created < 2026-04-01")
		require.NoError(t, err)
		assert.Equal(t, own, rep.Total)
		prev, err := taskListTotal(cfUser2, 0, "created >= 2025-10-01 && created < 2026-01-01")
		require.NoError(t, err)
		assert.Equal(t, prev, rep.Comparison.Total)
	})

	t.Run("the cache keeps requests with and without compare apart", func(t *testing.T) {
		config.ReportsCacheTTL.Set(time.Hour)
		defer config.ReportsCacheTTL.Set(0)
		ResetReportCache()
		with := req("2026-02-01", "2026-03-31", "month")
		without := with
		without.Compare = ""
		assert.NotNil(t, runReport(t, cfUser1, with).Comparison)
		assert.Nil(t, runReport(t, cfUser1, without).Comparison)
		assert.NotNil(t, runReport(t, cfUser1, with).Comparison)
	})
}

func insertHistory(t *testing.T, taskID int64, from *int64, to int64, toTitle string, at string) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	h := &TaskStatusHistory{TaskID: taskID, FromStatusID: from, ToStatusID: to, ToTitle: toTitle, DoerID: 1, Created: repTime(at)}
	_, err := s.NoAutoTime().Insert(h)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
}

func runStatusTime(a web.Auth, req StatusTimeRequest) (*StatusTimeReport, error) {
	s := db.NewSession()
	defer s.Close()
	return RunStatusTimeReport(context.Background(), s, a, &req)
}

func statStats(rep []*StatusTimeStat) map[string]*StatusTimeStat {
	out := map[string]*StatusTimeStat{}
	for _, st := range rep {
		out[st.Title] = st
	}
	return out
}

func TestReport_StatusTime(t *testing.T) {
	env := setupReportEnv(t)
	now := repTime("2026-03-01 00:00:00")
	statusTimeNow = func() time.Time { return now }
	t.Cleanup(func() { statusTimeNow = time.Now })
	id := env.tasks
	todo, doing, done := env.stTodo, env.stDoing, env.stDone
	i64 := cfPtr[int64]

	// t1: Todo 24h, Doing 24h, Todo again until now (open).
	insertHistory(t, id["t1"], nil, todo, "Todo", "2026-01-06 09:00:00")
	insertHistory(t, id["t1"], i64(todo), doing, "Doing", "2026-01-07 09:00:00")
	insertHistory(t, id["t1"], i64(doing), todo, "Todo", "2026-01-08 09:00:00")
	// t2: Todo for 8h, then Done (open).
	insertHistory(t, id["t2"], nil, todo, "Todo", "2026-01-12 00:00:00")
	insertHistory(t, id["t2"], i64(todo), done, "Done", "2026-01-12 08:00:00")
	// t3 has no history: its status row (Doing since 2026-02-10) is an open visit. t4's status was entered after "now"
	// and t5 has no status at all: neither counts.
	openT1 := now.Sub(repTime("2026-01-08 09:00:00")).Seconds()
	openT2 := now.Sub(repTime("2026-01-12 08:00:00")).Seconds()
	openT3 := now.Sub(repTime("2026-02-10 09:00:00")).Seconds()

	t.Run("average, median, total and open visits", func(t *testing.T) {
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, Timezone: "UTC"})
		require.NoError(t, err)
		assert.Equal(t, int64(3), rep.Tasks)
		st := statStats(rep.Statuses)
		require.Len(t, st, 3)

		// Todo: t1 = 86400 + openT1 over 2 visits, t2 = 28800.
		t1Todo := 86400 + openT1
		assert.Equal(t, int64(2), st["Todo"].Tasks)
		assert.Equal(t, int64(3), st["Todo"].Visits)
		assert.Equal(t, int64(1), st["Todo"].OpenTasks)
		assert.InDelta(t, t1Todo+28800, st["Todo"].TotalSeconds, 0.01)
		assert.InDelta(t, (t1Todo+28800)/2, st["Todo"].AverageSeconds, 0.01)
		assert.InDelta(t, (t1Todo+28800)/2, st["Todo"].MedianSeconds, 0.01, "the median of two is their mean")
		assert.InDelta(t, t1Todo, st["Todo"].MaxSeconds, 0.01)

		assert.Equal(t, int64(2), st["Doing"].Tasks)
		assert.InDelta(t, 86400+openT3, st["Doing"].TotalSeconds, 0.01)
		assert.Equal(t, int64(1), st["Doing"].OpenTasks)
		assert.InDelta(t, (86400+openT3)/2, st["Doing"].AverageSeconds, 0.01)

		assert.Equal(t, int64(1), st["Done"].Tasks)
		assert.InDelta(t, openT2, st["Done"].TotalSeconds, 0.01)
		assert.Equal(t, int64(1), st["Done"].OpenTasks)

		// Ordered like the workflow.
		assert.Equal(t, []string{"Todo", "Doing", "Done"}, []string{rep.Statuses[0].Title, rep.Statuses[1].Title, rep.Statuses[2].Title})
	})

	t.Run("a median of an odd number of tasks", func(t *testing.T) {
		insertHistory(t, id["t5"], nil, todo, "Todo", "2026-02-27 00:00:00")
		insertRow(t, &TaskStatus{TaskID: id["t5"], WorkflowID: readWorkflow(t, env.project).ID, StatusID: todo, Entered: repTime("2026-02-27 00:00:00")})
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project})
		require.NoError(t, err)
		todoStat := statStats(rep.Statuses)["Todo"]
		assert.Equal(t, int64(3), todoStat.Tasks)
		// t5: 2 days; t2: 8h; t1: > 50 days. The median is t5.
		assert.InDelta(t, 2*86400, todoStat.MedianSeconds, 0.01)
		cleanup := db.NewSession()
		defer cleanup.Close()
		_, err = cleanup.Where("task_id = ?", id["t5"]).Delete(&TaskStatusHistory{})
		require.NoError(t, err)
		_, err = cleanup.Where("task_id = ?", id["t5"]).Delete(&TaskStatus{})
		require.NoError(t, err)
		require.NoError(t, cleanup.Commit())
	})

	t.Run("the window cuts visits", func(t *testing.T) {
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, From: "2026-01-07", To: "2026-01-07", Timezone: "UTC"})
		require.NoError(t, err)
		st := statStats(rep.Statuses)
		// A plain date to names the whole day. t1 only: Todo 2026-01-07 00:00 .. 09:00, Doing 09:00 .. 2026-01-08 00:00.
		assert.Equal(t, int64(1), rep.Tasks)
		assert.InDelta(t, 9*3600, st["Todo"].TotalSeconds, 0.01)
		assert.InDelta(t, 15*3600, st["Doing"].TotalSeconds, 0.01)
		assert.Equal(t, int64(0), st["Doing"].OpenTasks, "the visit ended inside the window")
		assert.NotContains(t, st, "Done")

		_, err = runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, From: "2026-02-01", To: "2026-01-01"})
		require.Error(t, err)
		assert.True(t, IsErrReportInvalidParameter(err))
	})

	t.Run("filter", func(t *testing.T) {
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, Filter: "done = true"})
		require.NoError(t, err)
		assert.Equal(t, int64(1), rep.Tasks)
		assert.Contains(t, statStats(rep.Statuses), "Done")
		assert.Equal(t, int64(1), statStats(rep.Statuses)["Todo"].Tasks)
	})

	t.Run("by assignee", func(t *testing.T) {
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, GroupBy: "assignee"})
		require.NoError(t, err)
		assert.True(t, rep.MultiValued)
		byLabel := map[string]*StatusTimeGroup{}
		for _, g := range rep.Groups {
			byLabel[g.Label] = g
		}
		// user1: t1, t2; user2: t2; nobody: t3.
		u1 := byLabel[cfUser1Name(t)]
		require.NotNil(t, u1)
		assert.Equal(t, int64(2), u1.Tasks)
		assert.Equal(t, int64(2), statStats(u1.Statuses)["Todo"].Tasks)
		none := byLabel["None"]
		require.NotNil(t, none)
		assert.True(t, none.None)
		assert.Equal(t, int64(1), none.Tasks)
		assert.Equal(t, []string{"Doing"}, []string{none.Statuses[0].Title})
		assert.Len(t, rep.Groups, 3)
		assert.Equal(t, int64(2), rep.Groups[0].Tasks, "largest first")
	})

	t.Run("by label", func(t *testing.T) {
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, GroupBy: "label"})
		require.NoError(t, err)
		// label 1: t1, t2; label 2: t2; no label: t3.
		counts := map[string]int64{}
		for _, g := range rep.Groups {
			counts[g.Label] = g.Tasks
		}
		assert.Equal(t, int64(1), counts["None"])
		assert.Len(t, rep.Groups, 3)
		limited, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, GroupBy: "label", Limit: 1})
		require.NoError(t, err)
		assert.Len(t, limited.Groups, 1)
		assert.True(t, limited.Truncated)
	})

	t.Run("permissions", func(t *testing.T) {
		_, err := runStatusTime(cfUser2, StatusTimeRequest{ProjectID: env.project})
		require.Error(t, err, "a project the viewer cannot read")
		// A link share is pinned to its own project, like in the task list: it never sees the history of ours.
		shareRep, err := runStatusTime(&LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}, StatusTimeRequest{ProjectID: env.project})
		require.NoError(t, err)
		assert.Equal(t, int64(0), shareRep.Tasks)
		assert.Empty(t, shareRep.Statuses)
		_, err = runStatusTime(cfUser2, StatusTimeRequest{ProjectID: env.project, Filter: "cf_" + itoa(env.sel) + " = 1"})
		require.Error(t, err)

		// Read access shows exactly the readable tasks; a task list agrees on how many there are.
		insertRow(t, &ProjectUser{UserID: 2, ProjectID: env.project, Permission: PermissionRead})
		rep, err := runStatusTime(cfUser2, StatusTimeRequest{ProjectID: env.project})
		require.NoError(t, err)
		assert.Equal(t, int64(3), rep.Tasks)
		listed, err := taskListTotal(cfUser2, env.project, "")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, listed, rep.Tasks)
	})

	t.Run("validation and the row cap", func(t *testing.T) {
		_, err := runStatusTime(cfUser1, StatusTimeRequest{})
		require.True(t, IsErrReportInvalidParameter(err))
		assert.Equal(t, "project_id", err.(ErrReportInvalidParameter).Param)
		_, err = runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, GroupBy: "priority"})
		require.True(t, IsErrReportInvalidParameter(err))

		config.ReportsMaxRows.Set(2)
		defer config.ReportsMaxRows.Set(50000)
		_, err = runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project})
		require.Error(t, err)
		assert.True(t, IsErrReportTooManyRows(err))
	})

	t.Run("csv", func(t *testing.T) {
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, GroupBy: "assignee"})
		require.NoError(t, err)
		raw, err := rep.CSV().Bytes()
		require.NoError(t, err)
		rec := parseCSV(t, raw)
		assert.Equal(t, "Group", rec[0][0])
		assert.Greater(t, len(rec), 3)
	})
}

func cfUser1Name(t *testing.T) string {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	u, err := user.GetUserByID(s, 1)
	require.NoError(t, err)
	return u.GetName()
}

func runWorkload(a web.Auth, req WorkloadRequest) (*WorkloadReport, error) {
	s := db.NewSession()
	defer s.Close()
	return RunWorkloadReport(context.Background(), s, a, &req)
}

func TestReport_Workload(t *testing.T) {
	env := setupReportEnv(t)
	// Wednesday 2026-01-14, 10:00: this week ends Monday the 19th, next week Monday the 26th.
	workloadNow = func() time.Time { return repTime("2026-01-14 10:00:00") }
	t.Cleanup(func() { workloadNow = time.Now })
	id := env.tasks

	setDue := func(task string, due string) {
		s := db.NewSession()
		defer s.Close()
		_, err := s.NoAutoTime().ID(id[task]).Cols("due_date").Update(&Task{DueDate: repTime(due)})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
	}
	setDue("t1", "2026-02-01 12:00:00") // later, user1, estimate 10
	setDue("t3", "2026-01-13 09:00:00") // overdue, nobody
	setDue("t4", "2026-01-15 09:00:00") // this week, user2, estimate 20
	// A done task is never counted, also not when it is overdue.
	setDue("t2", "2026-01-01 09:00:00")
	t6 := repInsertTask(t, env.project, 6, &Task{Created: repTime("2026-01-01 00:00:00"), DueDate: repTime("2026-01-20 09:00:00")})
	t7 := repInsertTask(t, env.project, 7, &Task{Created: repTime("2026-01-01 00:00:00"), DueDate: repTime("2026-01-19 00:00:00")})
	insertRow(t, &TaskAssginee{TaskID: t6, UserID: 1})
	insertRow(t, &TaskAssginee{TaskID: t6, UserID: 2})
	insertRow(t, &TaskAssginee{TaskID: t7, UserID: 1})
	cfInsertValues(t, &TaskCustomFieldValue{TaskID: t6, CustomFieldID: env.num, ValueNumber: cfPtr(5.0)})
	// t5 is done and assigned to user1: not counted.

	rowOf := func(rep *WorkloadReport, label string) *WorkloadRow {
		for _, r := range rep.Rows {
			if r.Label == label {
				return r
			}
		}
		return nil
	}
	name1 := cfUser1Name(t)

	t.Run("count per assignee and due bucket", func(t *testing.T) {
		rep, err := runWorkload(cfUser1, WorkloadRequest{ProjectID: env.project, Timezone: "UTC"})
		require.NoError(t, err)
		assert.Equal(t, []string{"overdue", "this_week", "next_week", "later", "no_due_date"}, rep.Buckets)
		assert.True(t, rep.MultiValued)

		// user1: t1 (later), t6 (next week), t7 (due Monday the 19th 00:00: next week).
		u1 := rowOf(rep, name1)
		require.NotNil(t, u1)
		assert.Equal(t, []int64{0, 0, 2, 1, 0}, u1.Counts)
		assert.Equal(t, int64(3), u1.Total)
		// user2: t4 (this week), t6 (next week).
		var u2 *WorkloadRow
		for _, r := range rep.Rows {
			if r.Key == int64(2) {
				u2 = r
			}
		}
		require.NotNil(t, u2)
		assert.Equal(t, []int64{0, 1, 1, 0, 0}, u2.Counts)
		// Unassigned is its own row, last: t3 overdue.
		last := rep.Rows[len(rep.Rows)-1]
		assert.True(t, last.None)
		assert.Equal(t, "Unassigned", last.Label)
		assert.Equal(t, []int64{1, 0, 0, 0, 0}, last.Counts)
		assert.Len(t, rep.Rows, 3)
		// Totals count a task with two assignees once: t1, t3, t4, t6, t7.
		assert.Equal(t, int64(5), rep.Totals.Total)
		assert.Equal(t, []int64{1, 1, 2, 1, 0}, rep.Totals.Counts)
		assert.Nil(t, rep.Totals.Estimates)
		// Most loaded first.
		assert.Equal(t, name1, rep.Rows[0].Label)
	})

	t.Run("tasks without a due date", func(t *testing.T) {
		t8 := repInsertTask(t, env.project, 8, &Task{Created: repTime("2026-01-01 00:00:00")})
		defer func() {
			s := db.NewSession()
			defer s.Close()
			_, err := s.ID(t8).Delete(&Task{})
			require.NoError(t, err)
			require.NoError(t, s.Commit())
		}()
		rep, err := runWorkload(cfUser1, WorkloadRequest{ProjectID: env.project})
		require.NoError(t, err)
		assert.Equal(t, int64(1), rowOf(rep, "Unassigned").Counts[4])
		assert.Equal(t, int64(1), rep.Totals.Counts[4])
	})

	t.Run("estimate sums", func(t *testing.T) {
		rep, err := runWorkload(cfUser1, WorkloadRequest{ProjectID: env.project, EstimateField: "cf_" + itoa(env.num)})
		require.NoError(t, err)
		u1 := rowOf(rep, name1)
		// t1 = 10 (later), t6 = 5 (next week), t7 no value.
		assert.Equal(t, []float64{0, 0, 5, 10, 0}, u1.Estimates)
		assert.Equal(t, float64(15), u1.EstimateTotal)
		assert.Equal(t, float64(35), rep.Totals.EstimateTotal, "t1 10 + t4 20 + t6 5, the task with two assignees once")

		_, err = runWorkload(cfUser1, WorkloadRequest{ProjectID: env.project, EstimateField: "cf_" + itoa(env.sel)})
		require.Error(t, err)
		assert.True(t, IsErrReportFieldKindNotSupported(err))
		assert.Equal(t, "estimate_field", err.(ErrReportFieldKindNotSupported).Param)
		_, err = runWorkload(cfUser1, WorkloadRequest{ProjectID: env.project, EstimateField: "percent_done"})
		require.True(t, IsErrReportInvalidParameter(err))
	})

	t.Run("filter and limit", func(t *testing.T) {
		rep, err := runWorkload(cfUser1, WorkloadRequest{ProjectID: env.project, Filter: "priority = 3"})
		require.NoError(t, err)
		assert.Equal(t, int64(2), rep.Totals.Total, "t1 and t4 have priority 3")

		rep, err = runWorkload(cfUser1, WorkloadRequest{ProjectID: env.project, Limit: 1})
		require.NoError(t, err)
		assert.True(t, rep.Truncated)
		assert.Len(t, rep.Rows, 2, "one assignee and the unassigned row")
		assert.Equal(t, int64(5), rep.Totals.Total)
	})

	t.Run("permissions", func(t *testing.T) {
		_, err := runWorkload(cfUser2, WorkloadRequest{ProjectID: env.project})
		require.Error(t, err)
		_, err = runWorkload(cfUser2, WorkloadRequest{EstimateField: "cf_" + itoa(env.num)})
		require.Error(t, err, "a field of a project user2 cannot read is the filter error")
		assert.True(t, IsErrInvalidTaskField(err))
		_, err = runWorkload(cfUser2, WorkloadRequest{Filter: "cf_" + itoa(env.sel) + " = 1"})
		require.Error(t, err)

		// Without a project, user2 gets numbers of the projects user2 can read only, and none of ours.
		rep, err := runWorkload(cfUser2, WorkloadRequest{})
		require.NoError(t, err)
		want, err := taskListTotal(cfUser2, 0, "done = false")
		require.NoError(t, err)
		assert.Equal(t, want, rep.Totals.Total)
		for _, r := range rep.Rows {
			assert.NotEqual(t, name1+"-private", r.Label)
		}

		share := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}
		rep, err = runWorkload(share, WorkloadRequest{ProjectID: env.project})
		require.NoError(t, err)
		wantShare, err := taskListTotal(share, 0, "done = false")
		require.NoError(t, err)
		assert.Equal(t, wantShare, rep.Totals.Total, "a link share is pinned to its project")

		insertRow(t, &ProjectUser{UserID: 2, ProjectID: env.project, Permission: PermissionRead})
		rep, err = runWorkload(cfUser2, WorkloadRequest{ProjectID: env.project})
		require.NoError(t, err)
		assert.Equal(t, int64(5), rep.Totals.Total)
	})

	t.Run("every viewer: the totals are the task list of open tasks", func(t *testing.T) {
		for _, viewer := range []web.Auth{cfUser1, cfUser2, &user.User{ID: 3}, &user.User{ID: 5}, &user.User{ID: 6}} {
			for _, filter := range []string{"", "priority > 0", "labels = 4"} {
				for _, project := range []int64{0, 1, 3, 11} {
					listFilter := "done = false"
					if filter != "" {
						listFilter = "(" + filter + ") && done = false"
					}
					want, listErr := taskListTotal(viewer, project, listFilter)
					rep, err := runWorkload(viewer, WorkloadRequest{ProjectID: project, Filter: filter})
					if listErr != nil {
						require.Error(t, err)
						continue
					}
					require.NoError(t, err)
					assert.Equal(t, want, rep.Totals.Total, "viewer %v project %d filter %q", viewer, project, filter)
					sum := int64(0)
					for _, c := range rep.Totals.Counts {
						sum += c
					}
					assert.Equal(t, want, sum)
				}
			}
		}
	})

	t.Run("csv", func(t *testing.T) {
		rep, err := runWorkload(cfUser1, WorkloadRequest{ProjectID: env.project, EstimateField: "cf_" + itoa(env.num)})
		require.NoError(t, err)
		raw, err := rep.CSV().Bytes()
		require.NoError(t, err)
		rec := parseCSV(t, raw)
		assert.Equal(t, "Assignee", rec[0][0])
		assert.Equal(t, "Total", rec[len(rec)-1][0])
		assert.Len(t, rec[0], 13)
	})
}

func runTimeTracking(a web.Auth, req TimeTrackingRequest) (*TimeTrackingReport, error) {
	s := db.NewSession()
	defer s.Close()
	return RunTimeTrackingReport(context.Background(), s, a, &req)
}

func TestReport_TimeTracking(t *testing.T) {
	env := setupReportEnv(t)
	id := env.tasks
	// A time entry on the project itself, one without any container and a second project for user2.
	insertRow(t, &TimeEntry{UserID: 1, ProjectID: env.project, StartTime: repTime("2026-01-12 15:00:00"), EndTime: tp(repTime("2026-01-12 16:00:00"))})
	insertRow(t, &TimeEntry{UserID: 1, StartTime: repTime("2026-01-13 09:00:00"), EndTime: tp(repTime("2026-01-13 09:10:00"))})
	insertRow(t, &TimeEntry{UserID: 1, TaskID: id["t1"], StartTime: repTime("2026-02-20 09:00:00"), EndTime: tp(repTime("2026-02-20 10:00:00"))})
	rng := func(req TimeTrackingRequest) TimeTrackingRequest {
		req.From, req.To, req.Timezone = "2026-01-01", "2026-01-31", "UTC"
		return req
	}
	name1 := cfUser1Name(t)
	byLabel := func(rep *TimeTrackingReport) map[string]int64 {
		out := map[string]int64{}
		for _, g := range rep.Groups {
			out[g.Label] = g.Seconds
		}
		return out
	}

	t.Run("by user", func(t *testing.T) {
		rep, err := runTimeTracking(cfUser1, rng(TimeTrackingRequest{GroupBy: "user"}))
		require.NoError(t, err)
		// user1: 1h + 30m + 1h (project) + 10m (private); user2: 2h. The running timer is not counted.
		assert.Equal(t, int64(5400+3600+600), byLabel(rep)[name1])
		assert.Equal(t, int64(7200), rep.Groups[len(rep.Groups)-1].Seconds, "user2 has the least")
		assert.Equal(t, int64(5400+3600+600+7200), rep.Seconds)
		assert.Equal(t, int64(5), rep.Entries)
		assert.Equal(t, int64(1), rep.Running)
		assert.Equal(t, "UTC", rep.Timezone)
		assert.Equal(t, "2026-01-01T00:00:00Z", rep.From)
		assert.Equal(t, "2026-02-01T00:00:00Z", rep.To)
	})

	t.Run("by project", func(t *testing.T) {
		rep, err := runTimeTracking(cfUser1, rng(TimeTrackingRequest{GroupBy: "project"}))
		require.NoError(t, err)
		got := byLabel(rep)
		assert.Equal(t, int64(5400+7200+3600), got["Reports"], "entries on tasks and on the project")
		assert.Equal(t, int64(600), got["None"], "the private entry without project")
		scoped, err := runTimeTracking(cfUser1, rng(TimeTrackingRequest{GroupBy: "project", ProjectID: env.project}))
		require.NoError(t, err)
		assert.Equal(t, int64(16200), scoped.Seconds)
		assert.Len(t, scoped.Groups, 1)
	})

	t.Run("by label", func(t *testing.T) {
		rep, err := runTimeTracking(cfUser1, rng(TimeTrackingRequest{GroupBy: "label"}))
		require.NoError(t, err)
		assert.True(t, rep.MultiValued)
		got := byLabel(rep)
		// label 1 is on t1 and t2, label 2 on t2.
		assert.Equal(t, int64(5400+7200), got["Label #1"])
		assert.Equal(t, int64(7200), got["Label #2"])
		assert.Equal(t, int64(3600+600), got["None"], "the project entry and the private one")
		assert.Equal(t, int64(5400+7200+3600+600), rep.Seconds, "each entry once in the total")
	})

	t.Run("by day, empty days are zero", func(t *testing.T) {
		rep, err := runTimeTracking(cfUser1, TimeTrackingRequest{GroupBy: "day", From: "2026-01-05", To: "2026-01-13", Timezone: "UTC"})
		require.NoError(t, err)
		require.Len(t, rep.Groups, 9)
		got := byLabel(rep)
		assert.Equal(t, int64(3600), got["2026-01-06"])
		assert.Equal(t, int64(1800), got["2026-01-07"])
		assert.Equal(t, int64(0), got["2026-01-08"], "the running timer is not counted")
		assert.Equal(t, int64(7200+3600), got["2026-01-12"])
		assert.Equal(t, int64(600), got["2026-01-13"])
		assert.Equal(t, "2026-01-05", rep.Groups[0].Label)

		// Days are local days: 22:00 UTC is already the next day in Istanbul.
		insertRow(t, &TimeEntry{UserID: 1, TaskID: id["t3"], StartTime: repTime("2026-01-20 22:00:00"), EndTime: tp(repTime("2026-01-20 22:30:00"))})
		rep, err = runTimeTracking(cfUser1, TimeTrackingRequest{GroupBy: "day", From: "2026-01-20", To: "2026-01-21", Timezone: "Europe/Istanbul"})
		require.NoError(t, err)
		got = byLabel(rep)
		assert.Equal(t, int64(0), got["2026-01-20"])
		assert.Equal(t, int64(1800), got["2026-01-21"])
	})

	t.Run("by label, an entry with several folded labels counts once in other", func(t *testing.T) {
		// The entry of t3 (30 minutes, from the subtest above) gets two labels that are both folded. Label 1 is kept.
		// Other: the entry of t2 (label 2), the entry of t3 (labels 4 and 5) and the two entries without label.
		insertRow(t, &LabelTask{TaskID: id["t3"], LabelID: 4})
		insertRow(t, &LabelTask{TaskID: id["t3"], LabelID: 5})
		rep, err := runTimeTracking(cfUser1, rng(TimeTrackingRequest{GroupBy: "label", Limit: 1}))
		require.NoError(t, err)
		require.Len(t, rep.Groups, 2)
		assert.Equal(t, int64(5400+7200), rep.Groups[0].Seconds)
		require.True(t, rep.Groups[1].Other)
		assert.Equal(t, int64(7200+1800+3600+600), rep.Groups[1].Seconds)
		assert.Equal(t, int64(4), rep.Groups[1].Entries)
	})

	t.Run("the entry list and the report agree", func(t *testing.T) {
		for _, viewer := range []web.Auth{cfUser1, cfUser2, &user.User{ID: 3}, &user.User{ID: 5}} {
			s := db.NewSession()
			te := &TimeEntry{}
			res, _, _, err := te.ReadAll(s, viewer, "", 1, 1000)
			require.NoError(t, err)
			s.Close()
			var want, wantEntries int64
			for _, e := range res.([]*TimeEntry) {
				if e.EndTime == nil || e.StartTime.Before(repTime("2026-01-01 00:00:00")) || !e.StartTime.Before(repTime("2026-02-01 00:00:00")) {
					continue
				}
				want += int64(e.EndTime.Sub(e.StartTime).Seconds())
				wantEntries++
			}
			rep, err := runTimeTracking(viewer, rng(TimeTrackingRequest{GroupBy: "user"}))
			require.NoError(t, err)
			assert.Equal(t, want, rep.Seconds, "viewer %v", viewer)
			assert.Equal(t, wantEntries, rep.Entries, "viewer %v", viewer)
		}
	})

	t.Run("nobody sees the time of a project they cannot read", func(t *testing.T) {
		rep, err := runTimeTracking(cfUser2, rng(TimeTrackingRequest{GroupBy: "user"}))
		require.NoError(t, err)
		for _, g := range rep.Groups {
			assert.NotEqual(t, name1, g.Label, "user1's entries belong to a project user2 cannot read")
		}
		// user2's own entry is on a task of that project, so user2 does not see it either, as in the entry list.
		assert.Equal(t, int64(0), rep.Seconds)

		_, err = runTimeTracking(cfUser2, rng(TimeTrackingRequest{ProjectID: env.project}))
		require.Error(t, err, "a project the viewer cannot read")

		insertRow(t, &ProjectUser{UserID: 2, ProjectID: env.project, Permission: PermissionRead})
		rep, err = runTimeTracking(cfUser2, rng(TimeTrackingRequest{GroupBy: "user"}))
		require.NoError(t, err)
		assert.Equal(t, int64(5400+7200+3600+1800), rep.Seconds, "the private entry of user1 stays private")
	})

	t.Run("link shares have no time tracking", func(t *testing.T) {
		_, err := runTimeTracking(&LinkSharing{ID: 1, ProjectID: env.project, Permission: PermissionAdmin}, rng(TimeTrackingRequest{}))
		require.Error(t, err)
		assert.True(t, IsErrReportNotAvailableForShare(err))
		assert.Equal(t, 403, err.(ErrReportNotAvailableForShare).HTTPError().HTTPCode)
	})

	t.Run("validation and caps", func(t *testing.T) {
		_, err := runTimeTracking(cfUser1, TimeTrackingRequest{GroupBy: "task"})
		require.True(t, IsErrReportInvalidParameter(err))
		_, err = runTimeTracking(cfUser1, TimeTrackingRequest{From: "2026-02-01", To: "2026-01-01"})
		require.True(t, IsErrReportInvalidParameter(err))
		_, err = runTimeTracking(cfUser1, TimeTrackingRequest{From: "2020-01-01", To: "2026-01-01"})
		require.Error(t, err)
		assert.True(t, IsErrReportTooManyBuckets(err))
		_, err = runTimeTracking(cfUser1, TimeTrackingRequest{From: "bogus"})
		require.True(t, IsErrReportInvalidParameter(err))

		config.ReportsMaxRows.Set(2)
		defer config.ReportsMaxRows.Set(50000)
		_, err = runTimeTracking(cfUser1, rng(TimeTrackingRequest{}))
		require.Error(t, err)
		assert.True(t, IsErrReportTooManyRows(err))
	})

	t.Run("limit folds the rest", func(t *testing.T) {
		rep, err := runTimeTracking(cfUser1, rng(TimeTrackingRequest{GroupBy: "user", Limit: 1}))
		require.NoError(t, err)
		require.Len(t, rep.Groups, 2)
		assert.True(t, rep.Groups[1].Other)
		assert.True(t, rep.Truncated)
		assert.Equal(t, rep.Seconds, rep.Groups[0].Seconds+rep.Groups[1].Seconds)
	})

	t.Run("csv", func(t *testing.T) {
		rep, err := runTimeTracking(cfUser1, rng(TimeTrackingRequest{GroupBy: "user"}))
		require.NoError(t, err)
		raw, err := rep.CSV().Bytes()
		require.NoError(t, err)
		rec := parseCSV(t, raw)
		assert.Equal(t, []string{"User", "Seconds", "Hours", "Entries"}, rec[0])
		assert.Len(t, rec, len(rep.Groups)+1)
	})
}

// dropWorkflow removes the workflow of a project like deleting it does: the statuses and the current statuses of the
// tasks go, the history stays.
func dropWorkflow(t *testing.T, projectID int64) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	wf := &Workflow{}
	has, err := s.Where("project_id = ?", projectID).Get(wf)
	require.NoError(t, err)
	require.True(t, has)
	for _, bean := range []any{&TaskStatus{}, &WorkflowStatus{}, &WorkflowTransition{}} {
		_, err = s.Where("workflow_id = ?", wf.ID).Delete(bean)
		require.NoError(t, err)
	}
	_, err = s.Where("id = ?", wf.ID).Delete(&Workflow{})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
}

func statusIDsOf(rep *StatusTimeReport) map[int64]bool {
	out := map[int64]bool{}
	for _, st := range rep.Statuses {
		out[st.StatusID] = true
	}
	return out
}

func TestReport_StatusTimeForeignHistory(t *testing.T) {
	env := setupReportEnv(t)
	id := env.tasks
	i64 := cfPtr[int64]
	now := time.Now().Add(time.Hour)
	statusTimeNow = func() time.Time { return now }
	t.Cleanup(func() { statusTimeNow = time.Now })

	insertHistory(t, id["t1"], nil, env.stTodo, "Todo", "2026-01-06 09:00:00")
	insertHistory(t, id["t1"], i64(env.stTodo), env.stDoing, "Doing", "2026-01-07 09:00:00")

	t.Run("a deleted workflow leaves nothing to count", func(t *testing.T) {
		dropWorkflow(t, env.project)
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project})
		require.NoError(t, err)
		assert.Equal(t, int64(0), rep.Tasks)
		assert.Empty(t, rep.Statuses)
	})

	t.Run("a workflow created again does not extend the old statuses", func(t *testing.T) {
		mustSaveWorkflow(t, baseWorkflowDoc(env.project), cfUser1)
		wf := readWorkflow(t, env.project)
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project})
		require.NoError(t, err)
		require.NotEmpty(t, rep.Statuses)
		for _, st := range rep.Statuses {
			assert.Equal(t, wf.ID, statusWorkflowID(t, st.StatusID), "status %d belongs to the new workflow", st.StatusID)
			assert.Less(t, st.MaxSeconds, float64(3*3600), "a task started in the new workflow a moment ago")
		}
	})

	t.Run("history of the project a task came from stays out", func(t *testing.T) {
		other := repNewProject(t, "Other workflow")
		mustSaveWorkflow(t, baseWorkflowDoc(other), cfUser1)
		owf := readWorkflow(t, other)
		moved := repInsertTask(t, other, 1, &Task{Created: repTime("2026-01-01 00:00:00")})
		insertRow(t, &TaskStatus{TaskID: moved, WorkflowID: owf.ID, StatusID: statusIDByTitle(owf, "Todo"), Entered: repTime("2026-01-01 00:00:00")})
		insertHistory(t, moved, nil, statusIDByTitle(owf, "Todo"), "Todo", "2026-01-01 00:00:00")
		insertHistory(t, moved, i64(statusIDByTitle(owf, "Todo")), statusIDByTitle(owf, "Doing"), "Doing", "2026-01-02 00:00:00")

		// Move it into the reported project the way a task update does.
		s := db.NewSession()
		defer s.Close()
		_, err := s.Where("id = ?", moved).Cols("project_id", "index").Update(&Task{ProjectID: env.project, Index: 99})
		require.NoError(t, err)
		require.NoError(t, resetTaskStatusForMove(s, cfUser1, &Task{ID: moved, ProjectID: env.project}))
		require.NoError(t, s.Commit())

		wf := readWorkflow(t, env.project)
		rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project})
		require.NoError(t, err)
		for id := range statusIDsOf(rep) {
			assert.Equal(t, wf.ID, statusWorkflowID(t, id), "no status of the other project is reported")
		}
		todo := statStats(rep.Statuses)["Todo"]
		require.NotNil(t, todo)
		assert.Less(t, todo.MaxSeconds, float64(3*3600), "it entered the project a moment ago")
	})
}

func statusWorkflowID(t *testing.T, statusID int64) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	st := &WorkflowStatus{}
	has, err := s.Where("id = ?", statusID).Get(st)
	require.NoError(t, err)
	require.True(t, has, "status %d exists", statusID)
	return st.WorkflowID
}

func TestReport_StatusTimeWorkflowAddedLate(t *testing.T) {
	env := setupReportEnv(t)
	id := env.tasks
	now := repTime("2026-03-01 00:00:00")
	statusTimeNow = func() time.Time { return now }
	t.Cleanup(func() { statusTimeNow = time.Now })

	// The workflow was added on 2026-01-01, t1 (created 2026-01-05 10:00) got Todo without a history entry and moved
	// to Doing on 2026-01-10. Its Todo time starts when both the workflow and the task existed.
	s := db.NewSession()
	_, err := s.Exec("UPDATE workflows SET created = ? WHERE project_id = ?", "2026-01-01 00:00:00", env.project)
	require.NoError(t, err)
	_, err = s.Exec("UPDATE task_statuses SET status_id = ?, entered = ? WHERE task_id = ?", env.stDoing, "2026-01-10 00:00:00", id["t1"])
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	insertHistory(t, id["t1"], cfPtr(env.stTodo), env.stDoing, "Doing", "2026-01-10 00:00:00")

	rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, Timezone: "UTC"})
	require.NoError(t, err)
	st := statStats(rep.Statuses)
	require.Contains(t, st, "Todo")
	assert.InDelta(t, repTime("2026-01-10 00:00:00").Sub(repTime("2026-01-05 10:00:00")).Seconds(), st["Todo"].TotalSeconds, 0.01)
	assert.Equal(t, int64(1), st["Todo"].Tasks)
	assert.Equal(t, int64(0), st["Todo"].OpenTasks)
	assert.Contains(t, st, "Doing")
}

func TestReport_StatusTimeUnnamedGroups(t *testing.T) {
	env := setupReportEnv(t)
	id := env.tasks
	now := repTime("2026-03-01 00:00:00")
	statusTimeNow = func() time.Time { return now }
	t.Cleanup(func() { statusTimeNow = time.Now })

	// t3 only has a label that does not exist anymore: it counts with the tasks that have no label.
	insertRow(t, &LabelTask{TaskID: id["t3"], LabelID: 99999})
	rep, err := runStatusTime(cfUser1, StatusTimeRequest{ProjectID: env.project, GroupBy: "label"})
	require.NoError(t, err)
	var none *StatusTimeGroup
	for _, g := range rep.Groups {
		assert.NotEqual(t, any(int64(99999)), g.Key)
		if g.None {
			none = g
		}
	}
	require.NotNil(t, none)
	assert.Equal(t, int64(1), none.Tasks)
	assert.Equal(t, int64(3), rep.Tasks)
}

func TestReport_ExportIDChunks(t *testing.T) {
	ids := make([]int64, 0, 3000)
	for i := range 1500 {
		ids = append(ids, int64(i+1), int64(i+1))
	}
	seen := map[int64]bool{}
	chunks := 0
	require.NoError(t, forIDChunks(ids, func(chunk []int64) error {
		chunks++
		assert.LessOrEqual(t, len(chunk), exportIDChunk, "an IN list must stay below 999 parameters")
		for _, id := range chunk {
			assert.False(t, seen[id], "an id is looked up once")
			seen[id] = true
		}
		return nil
	}))
	assert.Len(t, seen, 1500)
	assert.Equal(t, 3, chunks)

	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()
	labels, err := findByIDs[Label](s, ids)
	require.NoError(t, err)
	assert.NotEmpty(t, labels, "the fixture labels are found among 1500 ids")
	assert.Len(t, uniqueIDs(ids), 1500)
}
