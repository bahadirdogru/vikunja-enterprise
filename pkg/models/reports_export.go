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
	"sort"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

const csvDateTimeLayout = "2006-01-02 15:04:05"

func csvNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func csvTime(t time.Time, loc *time.Location) string {
	if t.IsZero() || t.Year() <= 1 {
		return ""
	}
	return t.In(loc).Format(csvDateTimeLayout)
}

// reportValueHeader names the value column of a report export.
func reportValueHeader(rep *TaskReport) string {
	unit := rep.Unit
	switch {
	case rep.Measure == ReportMeasureCount || rep.Measure == "":
		return "Tasks"
	case unit == "seconds":
		return rep.Measure + " of " + rep.Field + " (seconds)"
	}
	return rep.Measure + " of " + rep.Field
}

// ReportCSV flattens the result of a report into a table: one row per leaf group (and bucket, for a time series).
// The groups are the ones the report returned, so the export holds exactly what the widget shows.
func ReportCSV(rep *TaskReport) *CSVTable {
	valueHeader := reportValueHeader(rep)
	table := &CSVTable{}

	if len(rep.Groups) == 0 {
		if len(rep.Series) == 0 {
			table.Header = []string{"Measure", "Value"}
			table.Rows = [][]CSVCell{{textCell(valueHeader), rawCell(csvNumber(rep.Value))}}
			if c := rep.Comparison; c != nil {
				table.Header = append(table.Header, "Previous value", "Change", "Change %")
				pct := ""
				if c.DeltaPercent != nil {
					pct = csvNumber(*c.DeltaPercent)
				}
				table.Rows[0] = append(table.Rows[0], rawCell(csvNumber(c.Value)), rawCell(csvNumber(c.Delta)), rawCell(pct))
			}
			return table
		}
		table.Header = []string{"Bucket", valueHeader}
		for _, p := range rep.Series {
			table.Rows = append(table.Rows, []CSVCell{rawCell(p.Bucket), rawCell(csvNumber(p.Value))})
		}
		return table
	}

	nested := false
	hasSeries := false
	var walk func(groups []*ReportGroup)
	walk = func(groups []*ReportGroup) {
		for _, g := range groups {
			if len(g.Children) > 0 {
				nested = true
				walk(g.Children)
			}
			if len(g.Series) > 0 {
				hasSeries = true
			}
		}
	}
	walk(rep.Groups)

	table.Header = []string{"Group"}
	if nested {
		table.Header = append(table.Header, "Subgroup")
	}
	if hasSeries {
		table.Header = append(table.Header, "Bucket")
	}
	table.Header = append(table.Header, valueHeader)

	emit := func(path []string, g *ReportGroup) {
		prefix := make([]CSVCell, 0, 3)
		for _, p := range path {
			prefix = append(prefix, textCell(p))
		}
		if nested && len(path) == 1 {
			prefix = append(prefix, textCell(""))
		}
		if hasSeries && len(g.Series) > 0 {
			for _, p := range g.Series {
				table.Rows = append(table.Rows, append(append([]CSVCell{}, prefix...), rawCell(p.Bucket), rawCell(csvNumber(p.Value))))
			}
			return
		}
		row := append([]CSVCell{}, prefix...)
		if hasSeries {
			row = append(row, rawCell(""))
		}
		table.Rows = append(table.Rows, append(row, rawCell(csvNumber(g.Value))))
	}
	for _, g := range rep.Groups {
		if len(g.Children) == 0 {
			emit([]string{g.Label}, g)
			continue
		}
		for _, c := range g.Children {
			emit([]string{g.Label, c.Label}, c)
		}
	}
	return table
}

// RunTaskReportCSV runs a report like RunTaskReport and returns it as CSV.
func RunTaskReportCSV(ctx context.Context, s *xorm.Session, a web.Auth, req *ReportRequest) ([]byte, error) {
	rep, err := RunTaskReport(ctx, s, a, req)
	if err != nil {
		return nil, err
	}
	return ReportCSV(rep).Bytes()
}

// TaskExportRequest selects the tasks to export: the scope and filter of a task list, or of the drill-down of a widget.
type TaskExportRequest struct {
	ProjectID          int64  `json:"project_id,omitempty" minimum:"-1" doc:"Limit the export to one project. Without it every project the requester can read is included. -1 is the favorites pseudo project."`
	Filter             string `json:"filter,omitempty" doc:"A task filter, the same syntax as in views, incl. status, status_category and cf_<id>."`
	FilterIncludeNulls bool   `json:"filter_include_nulls,omitempty" doc:"If true, tasks whose filtered field is empty are included."`
	Timezone           string `json:"timezone,omitempty" doc:"An IANA time zone the dates of the file are written in. Defaults to the time zone setting of the user, or the instance time zone for link shares."`
}

type taskExportCtx struct {
	s    *xorm.Session
	loc  *time.Location
	base *builder.Builder
}

// ExportTasksCSV writes the tasks of a scope and filter as CSV: the core columns and one column per custom field that
// has a value on at least one exported task. The tasks are the ones of readableTasksQuery, so the file never has a task
// the task list would not show. At most reports.maxrows tasks are exported.
//
//nolint:gocyclo
func ExportTasksCSV(ctx context.Context, s *xorm.Session, a web.Auth, req *TaskExportRequest) ([]byte, error) {
	if req.ProjectID < -1 {
		return nil, ErrReportInvalidParameter{Param: "project_id", Reason: "must be a project id or -1 for the favorites"}
	}
	loc, err := reportLocation(req.Timezone, reportAuthTimezone(s, a))
	if err != nil {
		return nil, err
	}
	filter := strings.TrimSpace(req.Filter)

	ctx, cancel := context.WithTimeout(ctx, config.ReportsTimeout.GetDuration())
	defer cancel()
	db.SetSessionContext(ctx, s)

	tf := &TaskCollection{
		ProjectID:          req.ProjectID,
		Filter:             filter,
		FilterTimezone:     loc.String(),
		FilterIncludeNulls: req.FilterIncludeNulls,
	}
	base, err := tf.readableTasksQuery(s, a)
	if err != nil {
		return nil, wrapReportError(err)
	}
	ex := &taskExportCtx{s: s, loc: loc, base: base}

	maxRows := config.ReportsMaxRows.GetInt()
	total, err := s.Where(builder.In("id", base)).Count(&Task{})
	if err != nil {
		return nil, wrapReportError(err)
	}
	if total > int64(maxRows) {
		return nil, ErrReportTooManyRows{Rows: total, Max: maxRows}
	}

	tasks := []*Task{}
	if err = s.Where(builder.In("id", base)).OrderBy("id ASC").
		Cols("id", "project_id", "index", "title", "done", "priority", "percent_done", "start_date", "due_date", "end_date",
			"done_at", "created", "created_by_id").Find(&tasks); err != nil {
		return nil, wrapReportError(err)
	}

	cols, err := ex.loadColumns(tasks)
	if err != nil {
		return nil, wrapReportError(err)
	}

	table := &CSVTable{Header: []string{
		"ID", "Identifier", "Title", "Project", "Status", "Done", "Priority", "Percent done", "Start date", "Due date", "End date",
		"Done at", "Assignees", "Labels", "Created", "Created by",
	}}
	for _, f := range cols.fields {
		table.Header = append(table.Header, cols.fieldHeaders[f.ID])
	}

	for _, t := range tasks {
		t.setIdentifier(cols.projects[t.ProjectID])
		done := "No"
		if t.Done {
			done = "Yes"
		}
		percent := ""
		if t.PercentDone > 0 {
			percent = csvNumber(t.PercentDone * 100)
		}
		priority := ""
		if t.Priority > 0 {
			priority = strconv.FormatInt(t.Priority, 10)
		}
		projectTitle := ""
		if p := cols.projects[t.ProjectID]; p != nil {
			projectTitle = p.Title
		}
		row := []CSVCell{
			rawCell(strconv.FormatInt(t.ID, 10)), textCell(t.Identifier), textCell(t.Title), textCell(projectTitle),
			textCell(cols.status[t.ID]), rawCell(done), rawCell(priority), rawCell(percent),
			rawCell(csvTime(t.StartDate, loc)), rawCell(csvTime(t.DueDate, loc)), rawCell(csvTime(t.EndDate, loc)),
			rawCell(csvTime(t.DoneAt, loc)),
			textCell(strings.Join(cols.assignees[t.ID], "; ")), textCell(strings.Join(cols.labels[t.ID], "; ")),
			rawCell(csvTime(t.Created, loc)), textCell(cols.userName(t.CreatedByID)),
		}
		for _, f := range cols.fields {
			row = append(row, cols.fieldCell(t.ID, f))
		}
		table.Rows = append(table.Rows, row)
	}
	return table.Bytes()
}

type taskExportColumns struct {
	projects     map[int64]*Project
	users        map[int64]*user.User
	status       map[int64]string
	assignees    map[int64][]string
	labels       map[int64][]string
	fields       []*CustomField
	fieldHeaders map[int64]string
	values       map[int64]map[int64][]*TaskCustomFieldValue
	options      map[int64]string
	loc          *time.Location
}

func (c *taskExportColumns) userName(id int64) string {
	if u, ok := c.users[id]; ok && u != nil {
		return u.GetName()
	}
	return ""
}

// exportIDChunk is how many ids go into one IN list: well below the 999 parameters of the oldest supported SQLite.
const exportIDChunk = 500

// uniqueIDs drops the duplicates of ids, keeping the order.
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// forIDChunks calls fn with the distinct ids in chunks that fit an IN list on every database.
func forIDChunks(ids []int64, fn func(chunk []int64) error) error {
	ids = uniqueIDs(ids)
	for start := 0; start < len(ids); start += exportIDChunk {
		if err := fn(ids[start:min(start+exportIDChunk, len(ids))]); err != nil {
			return err
		}
	}
	return nil
}

// findByIDs loads the rows with these ids, however many there are.
func findByIDs[T any](s *xorm.Session, ids []int64) ([]*T, error) {
	out := []*T{}
	err := forIDChunks(ids, func(chunk []int64) error {
		rows := []*T{}
		if err := s.Where(builder.In("id", chunk)).Find(&rows); err != nil {
			return err
		}
		out = append(out, rows...)
		return nil
	})
	return out, err
}

func usersByIDs(s *xorm.Session, ids []int64) (map[int64]*user.User, error) {
	out := map[int64]*user.User{}
	err := forIDChunks(ids, func(chunk []int64) error {
		users, err := user.GetUsersByIDs(s, chunk)
		if err != nil {
			return err
		}
		for id, u := range users {
			out[id] = u
		}
		return nil
	})
	return out, err
}

func projectsByIDs(s *xorm.Session, ids []int64) (map[int64]*Project, error) {
	out := map[int64]*Project{}
	err := forIDChunks(ids, func(chunk []int64) error {
		projects, err := GetProjectsMapByIDs(s, chunk)
		if err != nil {
			return err
		}
		for id, p := range projects {
			out[id] = p
		}
		return nil
	})
	return out, err
}

// loadColumns loads everything the rows need with a fixed number of statements: the related rows are selected through
// the same subquery as the tasks, never through a list of task ids.
//
//nolint:gocyclo
func (ex *taskExportCtx) loadColumns(tasks []*Task) (*taskExportColumns, error) {
	s := ex.s
	cols := &taskExportColumns{
		status: map[int64]string{}, assignees: map[int64][]string{}, labels: map[int64][]string{},
		fieldHeaders: map[int64]string{}, values: map[int64]map[int64][]*TaskCustomFieldValue{}, options: map[int64]string{}, loc: ex.loc,
	}
	if len(tasks) == 0 {
		cols.projects = map[int64]*Project{}
		cols.users = map[int64]*user.User{}
		return cols, nil
	}

	projectIDs := make([]int64, 0)
	userIDs := make([]int64, 0)
	seenP, seenU := map[int64]bool{}, map[int64]bool{}
	for _, t := range tasks {
		if !seenP[t.ProjectID] {
			seenP[t.ProjectID] = true
			projectIDs = append(projectIDs, t.ProjectID)
		}
		if !seenU[t.CreatedByID] {
			seenU[t.CreatedByID] = true
			userIDs = append(userIDs, t.CreatedByID)
		}
	}
	var err error
	if cols.projects, err = projectsByIDs(s, projectIDs); err != nil {
		return nil, err
	}

	assignees := []*TaskAssginee{}
	if err = s.Where(builder.In("task_id", ex.base)).OrderBy("id ASC").Find(&assignees); err != nil {
		return nil, err
	}
	for _, a := range assignees {
		if !seenU[a.UserID] {
			seenU[a.UserID] = true
			userIDs = append(userIDs, a.UserID)
		}
	}
	if cols.users, err = usersByIDs(s, userIDs); err != nil {
		return nil, err
	}
	for _, a := range assignees {
		if n := cols.userName(a.UserID); n != "" {
			cols.assignees[a.TaskID] = append(cols.assignees[a.TaskID], n)
		}
	}

	labelRows := []*LabelTask{}
	if err = s.Where(builder.In("task_id", ex.base)).OrderBy("id ASC").Find(&labelRows); err != nil {
		return nil, err
	}
	labelIDs := make([]int64, 0, len(labelRows))
	for _, l := range labelRows {
		labelIDs = append(labelIDs, l.LabelID)
	}
	labelTitles := map[int64]string{}
	labels, err := findByIDs[Label](s, labelIDs)
	if err != nil {
		return nil, err
	}
	for _, l := range labels {
		labelTitles[l.ID] = l.Title
	}
	for _, l := range labelRows {
		if title, ok := labelTitles[l.LabelID]; ok {
			cols.labels[l.TaskID] = append(cols.labels[l.TaskID], title)
		}
	}

	statusRows := []*TaskStatus{}
	if err = s.Where(builder.In("task_id", ex.base)).Find(&statusRows); err != nil {
		return nil, err
	}
	if len(statusRows) > 0 {
		ids := make([]int64, 0, len(statusRows))
		for _, r := range statusRows {
			ids = append(ids, r.StatusID)
		}
		statuses, err := findByIDs[WorkflowStatus](s, ids)
		if err != nil {
			return nil, err
		}
		titles := map[int64]string{}
		for _, st := range statuses {
			titles[st.ID] = st.Title
		}
		for _, r := range statusRows {
			cols.status[r.TaskID] = titles[r.StatusID]
		}
	}

	return cols, ex.loadCustomFields(cols)
}

func (ex *taskExportCtx) loadCustomFields(cols *taskExportColumns) error {
	s := ex.s
	rows := []*TaskCustomFieldValue{}
	if err := s.Where(builder.In("task_id", ex.base)).OrderBy("id ASC").Find(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	fieldIDs := []int64{}
	seen := map[int64]bool{}
	for _, r := range rows {
		if !seen[r.CustomFieldID] {
			seen[r.CustomFieldID] = true
			fieldIDs = append(fieldIDs, r.CustomFieldID)
		}
	}
	fields, err := findByIDs[CustomField](s, fieldIDs)
	if err != nil {
		return err
	}
	byID := map[int64]*CustomField{}
	for _, f := range fields {
		byID[f.ID] = f
	}

	optionIDs := []int64{}
	userIDs := []int64{}
	for _, r := range rows {
		if _, ok := byID[r.CustomFieldID]; !ok {
			continue
		}
		if cols.values[r.TaskID] == nil {
			cols.values[r.TaskID] = map[int64][]*TaskCustomFieldValue{}
		}
		cols.values[r.TaskID][r.CustomFieldID] = append(cols.values[r.TaskID][r.CustomFieldID], r)
		if r.OptionID != nil {
			optionIDs = append(optionIDs, *r.OptionID)
		}
		if r.UserID != nil {
			if _, known := cols.users[*r.UserID]; !known {
				userIDs = append(userIDs, *r.UserID)
			}
		}
	}
	options, err := findByIDs[CustomFieldOption](s, optionIDs)
	if err != nil {
		return err
	}
	for _, o := range options {
		cols.options[o.ID] = o.Title
	}
	if len(userIDs) > 0 {
		extra, err := usersByIDs(s, userIDs)
		if err != nil {
			return err
		}
		for id, u := range extra {
			cols.users[id] = u
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Position != fields[j].Position {
			return fields[i].Position < fields[j].Position
		}
		return fields[i].ID < fields[j].ID
	})
	titleCount := map[string]int{}
	for _, f := range fields {
		titleCount[f.Title]++
	}
	for _, f := range fields {
		header := f.Title
		if titleCount[f.Title] > 1 {
			if p, ok := cols.projects[f.ProjectID]; ok && p != nil {
				header += " (" + p.Title + ")"
			}
		}
		cols.fieldHeaders[f.ID] = "Custom field: " + header
		cols.fields = append(cols.fields, f)
	}
	return nil
}

func (c *taskExportColumns) fieldCell(taskID int64, f *CustomField) CSVCell {
	rows := c.values[taskID][f.ID]
	if len(rows) == 0 {
		return rawCell("")
	}
	switch f.Type {
	case CustomFieldTypeNumber, CustomFieldTypeCurrency, CustomFieldTypeRating, CustomFieldTypeProgress:
		if rows[0].ValueNumber != nil {
			return rawCell(csvNumber(*rows[0].ValueNumber))
		}
	case CustomFieldTypeDate:
		if rows[0].ValueDate != nil {
			return rawCell(csvTime(*rows[0].ValueDate, c.loc))
		}
	case CustomFieldTypeCheckbox:
		if rows[0].ValueBool != nil {
			if *rows[0].ValueBool {
				return rawCell("Yes")
			}
			return rawCell("No")
		}
	case CustomFieldTypeSelect, CustomFieldTypeMultiSelect:
		parts := []string{}
		for _, r := range rows {
			if r.OptionID != nil {
				parts = append(parts, c.options[*r.OptionID])
			}
		}
		return textCell(strings.Join(parts, "; "))
	case CustomFieldTypeUser:
		parts := []string{}
		for _, r := range rows {
			if r.UserID != nil {
				parts = append(parts, c.userName(*r.UserID))
			}
		}
		return textCell(strings.Join(parts, "; "))
	case CustomFieldTypeText, CustomFieldTypeLongText, CustomFieldTypeURL, CustomFieldTypeEmail:
		if rows[0].ValueText != nil {
			return textCell(*rows[0].ValueText)
		}
	}
	return rawCell("")
}

func csvInt(v int64) CSVCell { return rawCell(strconv.FormatInt(v, 10)) }

// CSV flattens the report: one row per status, and per group when it is split.
func (r *StatusTimeReport) CSV() *CSVTable {
	t := &CSVTable{}
	if r.GroupBy != "" {
		t.Header = append(t.Header, "Group")
	}
	t.Header = append(t.Header, "Status", "Tasks", "Visits", "Still in status", "Total seconds", "Average seconds", "Median seconds", "Longest seconds")
	add := func(group *StatusTimeGroup, st *StatusTimeStat) {
		var row []CSVCell
		if r.GroupBy != "" {
			row = append(row, textCell(group.Label))
		}
		row = append(row, textCell(st.Title), csvInt(st.Tasks), csvInt(st.Visits), csvInt(st.OpenTasks),
			rawCell(csvNumber(st.TotalSeconds)), rawCell(csvNumber(st.AverageSeconds)), rawCell(csvNumber(st.MedianSeconds)), rawCell(csvNumber(st.MaxSeconds)))
		t.Rows = append(t.Rows, row)
	}
	if r.GroupBy == "" {
		for _, st := range r.Statuses {
			add(nil, st)
		}
		return t
	}
	for _, g := range r.Groups {
		for _, st := range g.Statuses {
			add(g, st)
		}
	}
	return t
}

// CSV writes one row per assignee with the count per due bucket.
func (r *WorkloadReport) CSV() *CSVTable {
	t := &CSVTable{Header: []string{"Assignee", "Overdue", "Due this week", "Due next week", "Due later", "No due date", "Total"}}
	if r.EstimateField != "" {
		t.Header = append(t.Header, "Estimate overdue", "Estimate this week", "Estimate next week", "Estimate later", "Estimate no due date", "Estimate total")
	}
	rows := append(append([]*WorkloadRow{}, r.Rows...), r.Totals)
	for _, row := range rows {
		cells := []CSVCell{textCell(row.Label)}
		for _, c := range row.Counts {
			cells = append(cells, csvInt(c))
		}
		cells = append(cells, csvInt(row.Total))
		if r.EstimateField != "" {
			for _, v := range row.Estimates {
				cells = append(cells, rawCell(csvNumber(v)))
			}
			cells = append(cells, rawCell(csvNumber(row.EstimateTotal)))
		}
		t.Rows = append(t.Rows, cells)
	}
	return t
}

// CSV writes one row per group.
func (r *TimeTrackingReport) CSV() *CSVTable {
	header := "Day"
	switch r.GroupBy {
	case TimeTrackingGroupUser:
		header = "User"
	case TimeTrackingGroupProject:
		header = "Project"
	case TimeTrackingGroupLabel:
		header = "Label"
	}
	t := &CSVTable{Header: []string{header, "Seconds", "Hours", "Entries"}}
	for _, g := range r.Groups {
		t.Rows = append(t.Rows, []CSVCell{textCell(g.Label), csvInt(g.Seconds), rawCell(strconv.FormatFloat(float64(g.Seconds)/3600, 'f', 2, 64)), csvInt(g.Entries)})
	}
	return t
}
