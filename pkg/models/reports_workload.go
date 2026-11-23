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
	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

// The due buckets of the workload report, in the order of the counts.
const (
	WorkloadBucketOverdue  = "overdue"
	WorkloadBucketThisWeek = "this_week"
	WorkloadBucketNextWeek = "next_week"
	WorkloadBucketLater    = "later"
	WorkloadBucketNoDue    = "no_due_date"
)

var workloadBuckets = []string{WorkloadBucketOverdue, WorkloadBucketThisWeek, WorkloadBucketNextWeek, WorkloadBucketLater, WorkloadBucketNoDue}

// workloadNow is the clock of the workload report, replaced in tests.
var workloadNow = time.Now

// WorkloadRequest asks for the open tasks per assignee.
type WorkloadRequest struct {
	ProjectID          int64  `json:"project_id,omitempty" minimum:"-1" doc:"Limit the report to one project. Without it every project the requester can read is included. -1 is the favorites pseudo project."`
	Filter             string `json:"filter,omitempty" doc:"A task filter, the same syntax as in views. Only open tasks (done = false) are counted on top of it."`
	FilterIncludeNulls bool   `json:"filter_include_nulls,omitempty" doc:"If true, tasks whose filtered field is empty are included."`
	EstimateField      string `json:"estimate_field,omitempty" doc:"cf_<id> of a number, currency, rating or progress custom field whose values are added up per assignee and due bucket, e.g. an estimate in hours."`
	Timezone           string `json:"timezone,omitempty" doc:"An IANA time zone that decides when a week starts (Monday). Defaults to the time zone setting of the user."`
	Limit              int    `json:"limit,omitempty" minimum:"0" doc:"Keep this many assignees, the ones with the most open tasks (20 when omitted). The unassigned row is always added."`
}

// WorkloadRow is the open work of one assignee.
type WorkloadRow struct {
	Key           any       `json:"key" doc:"The user id, null for the unassigned row."`
	Label         string    `json:"label"`
	None          bool      `json:"none,omitempty" doc:"True for the row of tasks nobody is assigned to."`
	Counts        []int64   `json:"counts" doc:"Open tasks per due bucket, in the order of buckets."`
	Total         int64     `json:"total" doc:"Open tasks of the assignee."`
	Estimates     []float64 `json:"estimates,omitempty" doc:"The sum of the estimate field per due bucket, when estimate_field is set. Tasks without a value add nothing."`
	EstimateTotal float64   `json:"estimate_total,omitempty"`
}

// WorkloadReport lists the open tasks per assignee, split by when they are due.
type WorkloadReport struct {
	Buckets       []string       `json:"buckets" doc:"overdue (due before now), this_week (until Monday), next_week, later and no_due_date."`
	Rows          []*WorkloadRow `json:"rows" doc:"One row per assignee, the most loaded first, and the unassigned row last."`
	Totals        *WorkloadRow   `json:"totals" doc:"The distinct tasks, a task with several assignees is in each of their rows but once here."`
	EstimateField string         `json:"estimate_field,omitempty"`
	MultiValued   bool           `json:"multi_valued" doc:"True: a task with several assignees counts for each of them."`
	Timezone      string         `json:"timezone"`
	Truncated     bool           `json:"truncated,omitempty" doc:"True when assignees beyond the limit were left out."`
}

// workloadBoundaries returns now, the start of next week (Monday) and of the week after, in loc.
func workloadBoundaries(now time.Time, loc *time.Location) (time.Time, time.Time, time.Time) {
	n := now.In(loc)
	thisMonday := reportBucketStart(n, ReportBucketWeek)
	nextMonday := reportBucketNext(thisMonday, ReportBucketWeek)
	return n, nextMonday, reportBucketNext(nextMonday, ReportBucketWeek)
}

func workloadBucketExpr(now, nextWeek, weekAfter time.Time) string {
	return "CASE WHEN tasks.due_date IS NULL OR tasks.due_date < '1000-01-01 00:00:00' THEN 4" +
		" WHEN tasks.due_date < " + reportSQLTime(now) + " THEN 0" +
		" WHEN tasks.due_date < " + reportSQLTime(nextWeek) + " THEN 1" +
		" WHEN tasks.due_date < " + reportSQLTime(weekAfter) + " THEN 2" +
		" ELSE 3 END"
}

// RunWorkloadReport counts the open tasks the auth can read per assignee and due bucket, with two statements plus the
// lookup of the names. The tasks are the ones of readableTasksQuery.
func RunWorkloadReport(ctx context.Context, s *xorm.Session, a web.Auth, req *WorkloadRequest) (*WorkloadReport, error) {
	if req.ProjectID < -1 {
		return nil, ErrReportInvalidParameter{Param: "project_id", Reason: "must be a project id or -1 for the favorites"}
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

	filter := strings.TrimSpace(req.Filter)
	if filter == "" {
		filter = "done = false"
	} else {
		// The user filter has to be complete on its own: a text like "done = true) || (done = true" would balance
		// the parentheses of the combined filter and let done tasks through.
		if _, err = getTaskFiltersFromFilterString(filter, loc.String()); err != nil {
			return nil, err
		}
		filter = "(" + filter + ") && done = false"
	}

	reader := &customFieldReader{s: s, a: a}
	plan := &reportPlan{
		req:       ReportRequest{ProjectID: req.ProjectID, Filter: filter, Measure: ReportMeasureCount},
		measure:   ReportMeasureCount,
		unit:      "tasks",
		loc:       loc,
		limit:     limit,
		maxGroups: config.ReportsMaxGroups.GetInt(),
	}
	if req.EstimateField != "" {
		if _, isCustom := parseCustomFieldIdentifier(req.EstimateField); !isCustom {
			return nil, ErrReportInvalidParameter{Param: "estimate_field", Reason: "must be cf_<id> of a number, currency, rating or progress field"}
		}
		plan.req.Measure, plan.req.Field = ReportMeasureSum, req.EstimateField
		plan.measure = ReportMeasureSum
		if err = plan.buildMeasure(a, reader); err != nil {
			if e, ok := errors.AsType[ErrReportInvalidParameter](err); ok {
				e.Param = "estimate_field"
				return nil, e
			}
			if e, ok := errors.AsType[ErrReportFieldKindNotSupported](err); ok {
				e.Param = "estimate_field"
				return nil, e
			}
			return nil, err
		}
	}
	dim, err := newReportDim(reader, ReportDimAssignee, "group_by", 1)
	if err != nil {
		return nil, err
	}
	plan.dims = []*reportDim{dim}

	now, nextWeek, weekAfter := workloadBoundaries(workloadNow(), loc)
	plan.timeCfg = &reportTimePlan{indexExpr: workloadBucketExpr(now, nextWeek, weekAfter)}

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
	run := &reportRun{s: s, a: a, plan: plan, base: base}

	perUser, err := run.query([]*reportDim{dim}, true, nil)
	if err != nil {
		return nil, wrapReportError(err)
	}
	overall, err := run.query(nil, true, nil)
	if err != nil {
		return nil, wrapReportError(err)
	}

	hasEstimate := plan.measureExpr != ""
	newRow := func() *WorkloadRow {
		row := &WorkloadRow{Counts: make([]int64, len(workloadBuckets))}
		if hasEstimate {
			row.Estimates = make([]float64, len(workloadBuckets))
		}
		return row
	}
	fill := func(row *WorkloadRow, r *reportRow) {
		if r.bucket < 0 || r.bucket >= len(workloadBuckets) {
			return
		}
		row.Counts[r.bucket] += r.agg.n
		row.Total += r.agg.n
		if hasEstimate {
			row.Estimates[r.bucket] += r.agg.sum
			row.EstimateTotal += r.agg.sum
		}
	}

	rep := &WorkloadReport{
		Buckets:       workloadBuckets,
		Rows:          []*WorkloadRow{},
		EstimateField: req.EstimateField,
		MultiValued:   true,
		Timezone:      loc.String(),
		Totals:        newRow(),
	}
	rep.Totals.Label = "Total"
	rep.Totals.None = true
	for _, r := range overall {
		fill(rep.Totals, r)
	}

	byKey := map[string]*WorkloadRow{}
	keys := []reportKey{}
	var unassigned *WorkloadRow
	for _, r := range perUser {
		key := r.keys[0]
		if key.none {
			if unassigned == nil {
				unassigned = newRow()
				unassigned.None = true
				unassigned.Label = "Unassigned"
			}
			fill(unassigned, r)
			continue
		}
		row, ok := byKey[key.canon]
		if !ok {
			row = newRow()
			row.Key = key.value
			byKey[key.canon] = row
			keys = append(keys, key)
		}
		fill(row, r)
	}
	labels, err := dim.resolve(s, keys)
	if err != nil {
		return nil, wrapReportError(err)
	}
	for canon, row := range byKey {
		row.Label = labels[canon].label
		if row.Label == "" {
			row.Label = canon
		}
		rep.Rows = append(rep.Rows, row)
	}
	sort.SliceStable(rep.Rows, func(i, j int) bool {
		if rep.Rows[i].Total != rep.Rows[j].Total {
			return rep.Rows[i].Total > rep.Rows[j].Total
		}
		if rep.Rows[i].Label != rep.Rows[j].Label {
			return rep.Rows[i].Label < rep.Rows[j].Label
		}
		return strconv.FormatInt(reportInt(rep.Rows[i].Key), 10) < strconv.FormatInt(reportInt(rep.Rows[j].Key), 10)
	})
	if len(rep.Rows) > limit {
		rep.Rows = rep.Rows[:limit]
		rep.Truncated = true
	}
	if unassigned != nil {
		rep.Rows = append(rep.Rows, unassigned)
	}
	return rep, nil
}
