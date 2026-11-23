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
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// The groupings of the time in status report.
const (
	StatusTimeGroupAssignee = "assignee"
	StatusTimeGroupLabel    = "label"
)

// statusTimeNow is the clock of the time in status report, replaced in tests.
var statusTimeNow = time.Now

// StatusTimeRequest asks how long tasks of a project stayed in each workflow status.
type StatusTimeRequest struct {
	ProjectID          int64  `json:"project_id" minimum:"1" doc:"The project: statuses belong to the workflow of one project."`
	Filter             string `json:"filter,omitempty" doc:"A task filter, the same syntax as in views, incl. status, status_category and cf_<id>."`
	FilterIncludeNulls bool   `json:"filter_include_nulls,omitempty" doc:"If true, tasks whose filtered field is empty are included."`
	GroupBy            string `json:"group_by,omitempty" enum:"assignee,label" doc:"Also split the result by assignee or label. A task with several of them counts in each group."`
	From               string `json:"from,omitempty" doc:"Only count time after this moment: an RFC 3339 timestamp, a date like 2026-01-31, or now-90d. All of the history when omitted."`
	To                 string `json:"to,omitempty" doc:"Only count time before this moment, same formats as from, now when omitted. Later moments are cut to now."`
	Timezone           string `json:"timezone,omitempty" doc:"An IANA time zone that plain dates are read in. Defaults to the time zone setting of the user."`
	Limit              int    `json:"limit,omitempty" minimum:"0" doc:"With group_by: keep this many groups, the ones with the most tasks (20 when omitted)."`
}

// StatusTimeStat is the time tasks spent in one status.
type StatusTimeStat struct {
	StatusID       int64   `json:"status_id" doc:"The workflow status. A status that was removed keeps its id and the title it had."`
	Title          string  `json:"title"`
	Color          string  `json:"color,omitempty" doc:"A hex color including #, when the status has one."`
	Category       string  `json:"category,omitempty" doc:"The category of the status, empty for a status that no longer exists."`
	Tasks          int64   `json:"tasks" doc:"The number of tasks that spent time in the status."`
	Visits         int64   `json:"visits" doc:"How often tasks entered the status; a task that came back counts again."`
	OpenTasks      int64   `json:"open_tasks" doc:"How many of the tasks are still in the status. Their time runs until now."`
	TotalSeconds   float64 `json:"total_seconds" doc:"All time spent in the status."`
	AverageSeconds float64 `json:"average_seconds" doc:"The time per task: all visits of a task added up, averaged over the tasks."`
	MedianSeconds  float64 `json:"median_seconds" doc:"The median of the time per task."`
	MaxSeconds     float64 `json:"max_seconds" doc:"The longest time a single task spent in the status."`
}

// StatusTimeGroup is the result for the tasks of one assignee or label.
type StatusTimeGroup struct {
	Key      any               `json:"key" doc:"The user or label id, null for the tasks without one."`
	Label    string            `json:"label"`
	Color    string            `json:"color,omitempty"`
	None     bool              `json:"none,omitempty" doc:"True for the tasks that have no assignee or label."`
	Tasks    int64             `json:"tasks" doc:"The number of tasks of the group that spent time in any status."`
	Statuses []*StatusTimeStat `json:"statuses"`
}

// StatusTimeReport is the time in status of the readable tasks of a project.
type StatusTimeReport struct {
	Statuses    []*StatusTimeStat  `json:"statuses" doc:"Every status the tasks were in, in the order of the workflow."`
	Groups      []*StatusTimeGroup `json:"groups" doc:"The same per assignee or label, largest first. Empty without group_by."`
	Tasks       int64              `json:"tasks" doc:"The number of tasks that were counted."`
	GroupBy     string             `json:"group_by,omitempty"`
	MultiValued bool               `json:"multi_valued" doc:"True when a task can be part of several groups."`
	From        string             `json:"from,omitempty" doc:"The start of the window, RFC 3339. Empty when the whole history is counted."`
	To          string             `json:"to" doc:"The end of the window, RFC 3339. Open visits are counted until then."`
	Timezone    string             `json:"timezone"`
	Truncated   bool               `json:"truncated,omitempty" doc:"True when groups beyond the limit were left out."`
}

type statusInterval struct {
	statusID int64
	start    time.Time
	end      time.Time
	open     bool
}

type statusAgg struct {
	perTask map[int64]float64
	visits  int64
	open    map[int64]bool
}

func newStatusAgg() *statusAgg {
	return &statusAgg{perTask: map[int64]float64{}, open: map[int64]bool{}}
}

type statusTimeMeta struct {
	title    string
	color    string
	category string
	position float64
	known    bool
}

type statusAggSet map[int64]*statusAgg

func (set statusAggSet) add(taskID int64, iv statusInterval, seconds float64) {
	a, ok := set[iv.statusID]
	if !ok {
		a = newStatusAgg()
		set[iv.statusID] = a
	}
	a.perTask[taskID] += seconds
	a.visits++
	if iv.open {
		a.open[taskID] = true
	}
}

// RunStatusTimeReport computes how long the readable tasks of a project spent in each status from the status history.
// The intervals are computed in Go from the history rows, so it needs no date arithmetic of the database; the number of
// rows read is bounded by reports.maxrows. A visit that has no later entry is open and counts until now (or the end of
// the window).
//
//nolint:gocyclo
func RunStatusTimeReport(ctx context.Context, s *xorm.Session, a web.Auth, req *StatusTimeRequest) (*StatusTimeReport, error) {
	if req.ProjectID <= 0 {
		return nil, ErrReportInvalidParameter{Param: "project_id", Reason: "is required: statuses belong to the workflow of one project"}
	}
	if req.Limit < 0 {
		return nil, ErrReportInvalidParameter{Param: "limit", Reason: "must not be negative"}
	}
	limit := req.Limit
	if limit == 0 {
		limit = reportDefaultLimit
	}
	if max := config.ReportsMaxGroups.GetInt(); limit > max {
		return nil, ErrReportTooManyGroups{Limit: limit, Max: max}
	}
	switch req.GroupBy {
	case "", StatusTimeGroupAssignee, StatusTimeGroupLabel:
	default:
		return nil, ErrReportInvalidParameter{Param: "group_by", Reason: "must be assignee or label"}
	}

	loc, err := reportLocation(req.Timezone, reportAuthTimezone(s, a))
	if err != nil {
		return nil, err
	}
	now := statusTimeNow()
	var winFrom time.Time
	winTo := now
	if strings.TrimSpace(req.From) != "" {
		if winFrom, err = parseReportDate(req.From, loc, now, "from"); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(req.To) != "" {
		to, err := parseReportDate(req.To, loc, now, "to")
		if err != nil {
			return nil, err
		}
		if to.Before(winTo) {
			winTo = to
		}
	}
	if !winFrom.IsZero() && winTo.Before(winFrom) {
		return nil, ErrReportInvalidParameter{Param: "to", Reason: "must not be before from"}
	}

	ctx, cancel := context.WithTimeout(ctx, config.ReportsTimeout.GetDuration())
	defer cancel()
	db.SetSessionContext(ctx, s)

	tf := &TaskCollection{
		ProjectID:          req.ProjectID,
		Filter:             strings.TrimSpace(req.Filter),
		FilterTimezone:     loc.String(),
		FilterIncludeNulls: req.FilterIncludeNulls,
	}
	base, err := tf.readableTasksQuery(s, a)
	if err != nil {
		return nil, wrapReportError(err)
	}

	maxRows := config.ReportsMaxRows.GetInt()
	histCount, err := s.Where(builder.In("task_id", base)).Count(&TaskStatusHistory{})
	if err != nil {
		return nil, wrapReportError(err)
	}
	if histCount > int64(maxRows) {
		return nil, ErrReportTooManyRows{Rows: histCount, Max: maxRows}
	}
	history := []*TaskStatusHistory{}
	if err = s.Where(builder.In("task_id", base)).OrderBy("task_id ASC, created ASC, id ASC").Find(&history); err != nil {
		return nil, wrapReportError(err)
	}
	current := []*TaskStatus{}
	if err = s.Where(builder.In("task_id", base)).Find(&current); err != nil {
		return nil, wrapReportError(err)
	}

	byTask := map[int64][]*TaskStatusHistory{}
	titles := map[int64]string{}
	for _, h := range history {
		byTask[h.TaskID] = append(byTask[h.TaskID], h)
		titles[h.ToStatusID] = h.ToTitle
	}
	currentOf := map[int64]*TaskStatus{}
	for _, c := range current {
		currentOf[c.TaskID] = c
	}

	// intervals per task
	taskIntervals := map[int64][]statusInterval{}
	build := func(taskID int64, status int64, start time.Time, end time.Time, open bool) {
		if start.Before(winFrom) {
			start = winFrom
		}
		if end.After(winTo) {
			end = winTo
		}
		if !end.After(start) {
			return
		}
		taskIntervals[taskID] = append(taskIntervals[taskID], statusInterval{statusID: status, start: start, end: end, open: open})
	}
	for taskID, hs := range byTask {
		for i, h := range hs {
			if i+1 < len(hs) {
				build(taskID, h.ToStatusID, h.Created, hs[i+1].Created, false)
			} else {
				build(taskID, h.ToStatusID, h.Created, now, true)
			}
		}
	}
	// A task without history (it got its status before the history existed) is counted from the time it entered it.
	for taskID, c := range currentOf {
		if len(byTask[taskID]) == 0 {
			build(taskID, c.StatusID, c.Entered, now, true)
		}
	}

	overall := statusAggSet{}
	taskIDs := make([]int64, 0, len(taskIntervals))
	for taskID, ivs := range taskIntervals {
		taskIDs = append(taskIDs, taskID)
		for _, iv := range ivs {
			overall.add(taskID, iv, iv.end.Sub(iv.start).Seconds())
		}
	}

	rep := &StatusTimeReport{
		Groups:   []*StatusTimeGroup{},
		Tasks:    int64(len(taskIDs)),
		GroupBy:  req.GroupBy,
		To:       winTo.In(loc).Format(time.RFC3339),
		Timezone: loc.String(),
	}
	if !winFrom.IsZero() {
		rep.From = winFrom.In(loc).Format(time.RFC3339)
	}

	// Status metadata of every status that occurs, also removed ones.
	statusIDs := make([]int64, 0, len(overall))
	for id := range overall {
		statusIDs = append(statusIDs, id)
	}
	meta, err := loadStatusTimeMeta(s, statusIDs, titles)
	if err != nil {
		return nil, wrapReportError(err)
	}
	rep.Statuses = statusStats(overall, meta)

	if req.GroupBy == "" {
		return rep, nil
	}

	rep.MultiValued = true
	groupsOf, err := loadStatusTimeGroupKeys(s, base, req.GroupBy)
	if err != nil {
		return nil, wrapReportError(err)
	}
	perGroup := map[int64]statusAggSet{}
	const noneKey = int64(0)
	groupTasks := map[int64]map[int64]bool{}
	for _, taskID := range taskIDs {
		keys := groupsOf[taskID]
		if len(keys) == 0 {
			keys = []int64{noneKey}
		}
		for _, k := range keys {
			set, ok := perGroup[k]
			if !ok {
				set = statusAggSet{}
				perGroup[k] = set
				groupTasks[k] = map[int64]bool{}
			}
			groupTasks[k][taskID] = true
			for _, iv := range taskIntervals[taskID] {
				set.add(taskID, iv, iv.end.Sub(iv.start).Seconds())
			}
		}
	}

	keys := make([]reportKey, 0, len(perGroup))
	for k := range perGroup {
		if k == noneKey {
			continue
		}
		keys = append(keys, reportKey{canon: strconv.FormatInt(k, 10), value: k})
	}
	resolve := resolveUserLabels
	if req.GroupBy == StatusTimeGroupLabel {
		resolve = resolveLabelLabels
	}
	labels, err := resolve(s, keys)
	if err != nil {
		return nil, wrapReportError(err)
	}

	for k, set := range perGroup {
		g := &StatusTimeGroup{Tasks: int64(len(groupTasks[k])), Statuses: statusStats(set, meta)}
		if k == noneKey {
			g.None = true
			g.Label = "None"
		} else {
			g.Key = k
			l, ok := labels[strconv.FormatInt(k, 10)]
			if !ok {
				// The user or label is gone or not visible: the tasks stay, the name is not shown.
				continue
			}
			g.Label, g.Color = l.label, l.color
		}
		rep.Groups = append(rep.Groups, g)
	}
	sort.SliceStable(rep.Groups, func(i, j int) bool {
		if rep.Groups[i].Tasks != rep.Groups[j].Tasks {
			return rep.Groups[i].Tasks > rep.Groups[j].Tasks
		}
		return rep.Groups[i].Label < rep.Groups[j].Label
	})
	if len(rep.Groups) > limit {
		rep.Groups = rep.Groups[:limit]
		rep.Truncated = true
	}
	return rep, nil
}

func loadStatusTimeMeta(s *xorm.Session, ids []int64, historyTitles map[int64]string) (map[int64]*statusTimeMeta, error) {
	meta := map[int64]*statusTimeMeta{}
	for _, id := range ids {
		meta[id] = &statusTimeMeta{title: historyTitles[id], position: 1e18 + float64(id)}
	}
	if len(ids) == 0 {
		return meta, nil
	}
	statuses := []*WorkflowStatus{}
	if err := s.Where(builder.In("id", ids)).Find(&statuses); err != nil {
		return nil, err
	}
	for _, st := range statuses {
		meta[st.ID] = &statusTimeMeta{title: st.Title, color: st.Color, category: string(st.Category), position: st.Position, known: true}
	}
	return meta, nil
}

func statusStats(set statusAggSet, meta map[int64]*statusTimeMeta) []*StatusTimeStat {
	out := make([]*StatusTimeStat, 0, len(set))
	for id, agg := range set {
		m := meta[id]
		st := &StatusTimeStat{
			StatusID: id, Title: m.title, Color: m.color, Category: m.category,
			Tasks: int64(len(agg.perTask)), Visits: agg.visits, OpenTasks: int64(len(agg.open)),
		}
		values := make([]float64, 0, len(agg.perTask))
		for _, v := range agg.perTask {
			values = append(values, v)
			st.TotalSeconds += v
			if v > st.MaxSeconds {
				st.MaxSeconds = v
			}
		}
		if n := len(values); n > 0 {
			st.AverageSeconds = st.TotalSeconds / float64(n)
			sort.Float64s(values)
			if n%2 == 1 {
				st.MedianSeconds = values[n/2]
			} else {
				st.MedianSeconds = (values[n/2-1] + values[n/2]) / 2
			}
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		mi, mj := meta[out[i].StatusID], meta[out[j].StatusID]
		if mi.position != mj.position {
			return mi.position < mj.position
		}
		return out[i].StatusID < out[j].StatusID
	})
	return out
}

// loadStatusTimeGroupKeys returns the assignees or labels of the tasks, through the subquery of the readable tasks.
func loadStatusTimeGroupKeys(s *xorm.Session, base *builder.Builder, groupBy string) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if groupBy == StatusTimeGroupLabel {
		rows := []*LabelTask{}
		if err := s.Where(builder.In("task_id", base)).OrderBy("id ASC").Find(&rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.TaskID] = append(out[r.TaskID], r.LabelID)
		}
		return out, nil
	}
	rows := []*TaskAssginee{}
	if err := s.Where(builder.In("task_id", base)).OrderBy("id ASC").Find(&rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TaskID] = append(out[r.TaskID], r.UserID)
	}
	return out, nil
}
