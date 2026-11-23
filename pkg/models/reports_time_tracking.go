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
	"errors"
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

// The groupings of the time tracking report.
const (
	TimeTrackingGroupUser    = "user"
	TimeTrackingGroupProject = "project"
	TimeTrackingGroupLabel   = "label"
	TimeTrackingGroupDay     = "day"
)

// TimeTrackingRequest asks for the tracked time in a date range.
type TimeTrackingRequest struct {
	ProjectID int64  `json:"project_id,omitempty" minimum:"0" doc:"Only the time tracked on this project and its tasks. Without it every entry you can read is included."`
	GroupBy   string `json:"group_by,omitempty" enum:"user,project,label,day" doc:"What to add the time up by, user when omitted. Days are the local days of the time zone, an entry belongs to the day it started on. A task with several labels counts its time for each of them."`
	From      string `json:"from,omitempty" doc:"The start of the range: an RFC 3339 timestamp, a date like 2026-01-31 or now-30d. The day that contains it is the first one. now-30d when omitted."`
	To        string `json:"to,omitempty" doc:"The end of the range, same formats as from, now when omitted. The day that contains it is the last one."`
	Timezone  string `json:"timezone,omitempty" doc:"An IANA time zone that decides where days begin. Defaults to the time zone setting of the user."`
	Limit     int    `json:"limit,omitempty" minimum:"0" doc:"Not for days: keep this many groups, the ones with the most time (20 when omitted); the rest is folded into one other group."`
}

// TimeTrackingGroup is the tracked time of one user, project, label or day.
type TimeTrackingGroup struct {
	Key     any    `json:"key" doc:"The user, project or label id, the date of a day, null for the none and other groups."`
	Label   string `json:"label"`
	Color   string `json:"color,omitempty" doc:"A hex color including #, when the group has one."`
	Seconds int64  `json:"seconds" doc:"The tracked time of finished entries."`
	Entries int64  `json:"entries" doc:"The number of time entries."`
	None    bool   `json:"none,omitempty" doc:"True for the entries without project or label."`
	Other   bool   `json:"other,omitempty" doc:"True for the group the groups beyond the limit are folded into."`
}

// TimeTrackingReport is the tracked time of the entries the requester can read.
type TimeTrackingReport struct {
	Groups      []*TimeTrackingGroup `json:"groups" doc:"The groups, the most time first; days in order, empty days with 0."`
	Seconds     int64                `json:"seconds" doc:"All tracked time in the range, each entry once."`
	Entries     int64                `json:"entries" doc:"The number of finished time entries in the range."`
	Running     int64                `json:"running" doc:"Timers that run and are not counted until they are stopped."`
	GroupBy     string               `json:"group_by"`
	MultiValued bool                 `json:"multi_valued" doc:"True when an entry can be part of several groups (labels)."`
	From        string               `json:"from" doc:"Start of the first day, RFC 3339."`
	To          string               `json:"to" doc:"End of the last day (exclusive), RFC 3339."`
	Timezone    string               `json:"timezone"`
	Truncated   bool                 `json:"truncated,omitempty" doc:"True when groups were folded into other."`
}

// RunTimeTrackingReport adds up the finished time entries the auth can read (readableTimeEntriesCond, the rule of the
// time entry list) that started in the range. The durations are added up in Go from at most reports.maxrows rows. Link
// shares have no time tracking access.
//
//nolint:gocyclo
func RunTimeTrackingReport(ctx context.Context, s *xorm.Session, a web.Auth, req *TimeTrackingRequest) (*TimeTrackingReport, error) {
	if _, isShare := a.(*LinkSharing); isShare {
		return nil, ErrReportNotAvailableForShare{Param: "time_tracking"}
	}
	groupBy := req.GroupBy
	if groupBy == "" {
		groupBy = TimeTrackingGroupUser
	}
	switch groupBy {
	case TimeTrackingGroupUser, TimeTrackingGroupProject, TimeTrackingGroupLabel, TimeTrackingGroupDay:
	default:
		return nil, ErrReportInvalidParameter{Param: "group_by", Reason: "must be user, project, label or day"}
	}
	if req.ProjectID < 0 {
		return nil, ErrReportInvalidParameter{Param: "project_id", Reason: "must not be negative"}
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

	loc, err := reportLocation(req.Timezone, reportAuthTimezone(s, a))
	if err != nil {
		return nil, err
	}
	now := time.Now().In(loc)
	fromRaw, toRaw := strings.TrimSpace(req.From), strings.TrimSpace(req.To)
	if fromRaw == "" {
		fromRaw = "now-30d"
	}
	if toRaw == "" {
		toRaw = "now"
	}
	from, err := parseReportDate(fromRaw, loc, now, "from")
	if err != nil {
		return nil, err
	}
	to, err := parseReportDate(toRaw, loc, now, "to")
	if err != nil {
		return nil, err
	}
	bounds, err := reportBuckets(from, to, ReportBucketDay, config.ReportsMaxBuckets.GetInt())
	if err != nil {
		if e, ok := errors.AsType[ErrReportInvalidParameter](err); ok {
			e.Param = "to"
			return nil, e
		}
		return nil, err
	}
	rangeStart, rangeEnd := bounds[0], bounds[len(bounds)-1]

	ctx, cancel := context.WithTimeout(ctx, config.ReportsTimeout.GetDuration())
	defer cancel()
	db.SetSessionContext(ctx, s)

	cond, err := readableTimeEntriesCond(s, a)
	if err != nil {
		return nil, err
	}
	if req.ProjectID > 0 {
		can, _, err := (&Project{ID: req.ProjectID}).CanRead(s, a)
		if err != nil && !IsErrProjectDoesNotExist(err) {
			return nil, err
		}
		if err != nil || !can {
			return nil, ErrProjectDoesNotExist{ID: req.ProjectID}
		}
		cond = builder.And(cond, entriesForProjectCond(builder.Eq{"project_id": req.ProjectID}))
	}
	inRange := builder.And(
		builder.Gte{"start_time": rangeStart.UTC().Format(dbTimeFormat)},
		builder.Lt{"start_time": rangeEnd.UTC().Format(dbTimeFormat)},
	)
	scope := builder.And(cond, inRange)
	finished := builder.And(scope, builder.NotNull{"end_time"})

	maxRows := config.ReportsMaxRows.GetInt()
	count, err := s.Where(finished).Count(&TimeEntry{})
	if err != nil {
		return nil, wrapReportError(err)
	}
	if count > int64(maxRows) {
		return nil, ErrReportTooManyRows{Rows: count, Max: maxRows}
	}
	running, err := s.Where(builder.And(scope, builder.IsNull{"end_time"})).Count(&TimeEntry{})
	if err != nil {
		return nil, wrapReportError(err)
	}
	entries := []*TimeEntry{}
	if err = s.Where(finished).OrderBy("start_time ASC, id ASC").Find(&entries); err != nil {
		return nil, wrapReportError(err)
	}

	rep := &TimeTrackingReport{
		Groups:      []*TimeTrackingGroup{},
		Entries:     int64(len(entries)),
		Running:     running,
		GroupBy:     groupBy,
		MultiValued: groupBy == TimeTrackingGroupLabel,
		From:        rangeStart.In(loc).Format(time.RFC3339),
		To:          rangeEnd.In(loc).Format(time.RFC3339),
		Timezone:    loc.String(),
	}
	seconds := func(e *TimeEntry) int64 {
		d := int64(e.EndTime.Sub(e.StartTime).Seconds())
		if d < 0 {
			return 0
		}
		return d
	}
	for _, e := range entries {
		rep.Seconds += seconds(e)
	}

	type acc struct {
		key     any
		label   string
		color   string
		none    bool
		seconds int64
		entries int64
	}
	groups := map[string]*acc{}
	order := []string{}
	add := func(canon string, key any, label, color string, none bool, e *TimeEntry) {
		g, ok := groups[canon]
		if !ok {
			g = &acc{key: key, label: label, color: color, none: none}
			groups[canon] = g
			order = append(order, canon)
		}
		g.seconds += seconds(e)
		g.entries++
	}

	taskProject := map[int64]int64{}
	taskLabels := map[int64][]int64{}
	if groupBy == TimeTrackingGroupProject || groupBy == TimeTrackingGroupLabel {
		taskIDs := builder.Select("task_id").From("time_entries").Where(builder.And(finished, builder.Gt{"task_id": 0}))
		tasks := []*Task{}
		if err = s.Cols("id", "project_id").Where(builder.In("id", taskIDs)).Find(&tasks); err != nil {
			return nil, wrapReportError(err)
		}
		for _, t := range tasks {
			taskProject[t.ID] = t.ProjectID
		}
		if groupBy == TimeTrackingGroupLabel {
			rows := []*LabelTask{}
			if err = s.Where(builder.In("task_id", taskIDs)).OrderBy("id ASC").Find(&rows); err != nil {
				return nil, wrapReportError(err)
			}
			for _, r := range rows {
				taskLabels[r.TaskID] = append(taskLabels[r.TaskID], r.LabelID)
			}
		}
	}
	entryProject := func(e *TimeEntry) int64 {
		if e.ProjectID > 0 {
			return e.ProjectID
		}
		return taskProject[e.TaskID]
	}

	names := map[int64]reportLabel{}
	switch groupBy {
	case TimeTrackingGroupUser:
		ids := make([]int64, 0)
		seen := map[int64]bool{}
		for _, e := range entries {
			if !seen[e.UserID] {
				seen[e.UserID] = true
				ids = append(ids, e.UserID)
			}
		}
		users, err := user.GetUsersByIDs(s, ids)
		if err != nil {
			return nil, wrapReportError(err)
		}
		for id, u := range users {
			names[id] = reportLabel{label: u.GetName()}
		}
	case TimeTrackingGroupProject:
		ids := make([]int64, 0)
		seen := map[int64]bool{}
		for _, e := range entries {
			if id := entryProject(e); id > 0 && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		projects, err := GetProjectsMapByIDs(s, ids)
		if err != nil {
			return nil, wrapReportError(err)
		}
		for id, p := range projects {
			l := reportLabel{label: p.Title}
			if p.HexColor != "" {
				l.color = "#" + strings.TrimPrefix(p.HexColor, "#")
			}
			names[id] = l
		}
	case TimeTrackingGroupLabel:
		keys := []reportKey{}
		seen := map[int64]bool{}
		for _, ids := range taskLabels {
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					keys = append(keys, reportKey{canon: strconv.FormatInt(id, 10), value: id})
				}
			}
		}
		labels, err := resolveLabelLabels(s, keys)
		if err != nil {
			return nil, wrapReportError(err)
		}
		for id := range seen {
			names[id] = labels[strconv.FormatInt(id, 10)]
		}
	}

	switch groupBy {
	case TimeTrackingGroupDay:
		perDay := make([]*TimeTrackingGroup, len(bounds)-1)
		for i := range perDay {
			day := bounds[i].In(loc).Format(reportDateLayout)
			perDay[i] = &TimeTrackingGroup{Key: day, Label: day}
		}
		for _, e := range entries {
			i := sort.Search(len(bounds), func(i int) bool { return bounds[i].After(e.StartTime) }) - 1
			if i < 0 || i >= len(perDay) {
				continue
			}
			perDay[i].Seconds += seconds(e)
			perDay[i].Entries++
		}
		rep.Groups = perDay
		return rep, nil
	case TimeTrackingGroupUser:
		for _, e := range entries {
			l := names[e.UserID]
			add(strconv.FormatInt(e.UserID, 10), e.UserID, l.label, l.color, false, e)
		}
	case TimeTrackingGroupProject:
		for _, e := range entries {
			id := entryProject(e)
			if id == 0 {
				add("none", nil, "None", "", true, e)
				continue
			}
			l := names[id]
			add(strconv.FormatInt(id, 10), id, l.label, l.color, false, e)
		}
	case TimeTrackingGroupLabel:
		for _, e := range entries {
			ids := taskLabels[e.TaskID]
			if len(ids) == 0 {
				add("none", nil, "None", "", true, e)
				continue
			}
			for _, id := range ids {
				l := names[id]
				add(strconv.FormatInt(id, 10), id, l.label, l.color, false, e)
			}
		}
	}

	list := make([]*TimeTrackingGroup, 0, len(order))
	for _, canon := range order {
		g := groups[canon]
		list = append(list, &TimeTrackingGroup{Key: g.key, Label: g.label, Color: g.color, Seconds: g.seconds, Entries: g.entries, None: g.none})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Seconds != list[j].Seconds {
			return list[i].Seconds > list[j].Seconds
		}
		return list[i].Label < list[j].Label
	})
	if len(list) > limit {
		other := &TimeTrackingGroup{Label: "Other", Other: true}
		for _, g := range list[limit:] {
			other.Seconds += g.Seconds
			other.Entries += g.Entries
		}
		list = append(list[:limit], other)
		rep.Truncated = true
	}
	rep.Groups = list
	return rep, nil
}
