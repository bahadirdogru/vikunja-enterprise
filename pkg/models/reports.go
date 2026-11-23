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
	"fmt"
	"math"
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

// The measures of a report.
const (
	ReportMeasureCount = "count"
	ReportMeasureSum   = "sum"
	ReportMeasureAvg   = "avg"
	ReportMeasureMin   = "min"
	ReportMeasureMax   = "max"

	// ReportFieldPercentDone and ReportFieldTimeSpent are the built-in fields of sum, avg, min and max.
	ReportFieldPercentDone = "percent_done"
	ReportFieldTimeSpent   = "time_spent"

	reportDefaultLimit = 20
)

// ReportTime turns a report into a time series.
type ReportTime struct {
	Field    string `json:"field" enum:"created,done_at,due_date,status_entered" doc:"The date every task is placed in a bucket by. Tasks where it is empty are left out and reported in excluded. status_entered is the moment the task entered its current workflow status."`
	Bucket   string `json:"bucket,omitempty" enum:"day,week,month" doc:"The size of a bucket, day when omitted. Weeks start on Monday."`
	From     string `json:"from,omitempty" doc:"The start of the range: an RFC 3339 timestamp, a date like 2026-01-31 (midnight in the time zone of the report), or now, now-90d, now-12h. now-90d when omitted. The bucket that contains it is the first one."`
	To       string `json:"to,omitempty" doc:"The end of the range, same formats as from, now when omitted. The bucket that contains it is the last one."`
	Timezone string `json:"timezone,omitempty" doc:"An IANA time zone like Europe/Istanbul that decides where days, weeks and months begin. Defaults to the time zone setting of the user, or the instance time zone for link shares."`
}

// ReportRequest asks for numbers over the tasks the requester can read.
type ReportRequest struct {
	ProjectID          int64       `json:"project_id,omitempty" minimum:"-1" doc:"Limit the report to one project. Without it every project the requester can read is included. -1 is the favorites pseudo project."`
	Filter             string      `json:"filter,omitempty" doc:"A task filter, the same syntax as in views, incl. status, status_category and cf_<id>."`
	FilterIncludeNulls bool        `json:"filter_include_nulls,omitempty" doc:"If true, tasks whose filtered field is empty are included."`
	GroupBy            string      `json:"group_by,omitempty" doc:"A dimension: status, status_category, assignee, label, project, priority, done, created_by or cf_<id> of a select, multi select, user, checkbox or rating field."`
	ThenBy             string      `json:"then_by,omitempty" doc:"A second dimension, nested under the groups of group_by. Needs group_by."`
	Measure            string      `json:"measure,omitempty" enum:"count,sum,avg,min,max" doc:"What every group shows, count of tasks when omitted."`
	Field              string      `json:"field,omitempty" doc:"The numeric field sum, avg, min and max work on: percent_done, time_spent (seconds of finished time entries) or cf_<id> of a number, currency, rating or progress field. Tasks without a value are ignored."`
	Time               *ReportTime `json:"time,omitempty" doc:"Makes the report a time series: values are split into buckets of this date field."`
	Limit              int         `json:"limit,omitempty" minimum:"0" doc:"Keep this many groups per level (20 when omitted); the rest is folded into one other group."`
	Compare            string      `json:"compare,omitempty" enum:"previous_period" doc:"previous_period also computes the measure for the period of equal length right before the time range (the same number of buckets, shifted back) and returns it as comparison. Needs time and cannot be combined with group_by."`
}

// ReportComparison is the value of the previous period next to the value of the requested one.
type ReportComparison struct {
	Value        float64  `json:"value" doc:"The measure over the previous period."`
	Total        int64    `json:"total" doc:"The number of distinct tasks of the previous period."`
	Delta        float64  `json:"delta" doc:"value of the report minus the value of the previous period."`
	DeltaPercent *float64 `json:"delta_percent" doc:"The change in percent of the previous value. null when the previous value is 0."`
	From         string   `json:"from" doc:"Start of the previous period, RFC 3339."`
	To           string   `json:"to" doc:"End of the previous period (exclusive), RFC 3339."`
}

// ReportPoint is the value of one time bucket.
type ReportPoint struct {
	Bucket string  `json:"bucket" doc:"The local date the bucket starts on, e.g. 2026-11-02."`
	Start  string  `json:"start" doc:"The start of the bucket as RFC 3339 timestamp in the time zone of the report."`
	End    string  `json:"end" doc:"The end of the bucket (exclusive), RFC 3339."`
	Value  float64 `json:"value" doc:"The measure of the tasks in the bucket. Empty buckets are returned with 0."`
}

// ReportGroup is one group of tasks.
type ReportGroup struct {
	Key      any            `json:"key" doc:"The identity of the group: an id, a category name, a number or a boolean, depending on the dimension. null for the none and other groups."`
	Label    string         `json:"label" doc:"The display name, resolved by the server."`
	Color    string         `json:"color,omitempty" doc:"A hex color including #, when the group has one."`
	Value    float64        `json:"value" doc:"The measure of the tasks of the group."`
	None     bool           `json:"none,omitempty" doc:"True for the group of tasks that have no value for the dimension."`
	Other    bool           `json:"other,omitempty" doc:"True for the group the remaining groups beyond the limit are folded into. It has no children."`
	Series   []*ReportPoint `json:"series,omitempty" doc:"The values per time bucket, for time series."`
	Children []*ReportGroup `json:"children,omitempty" doc:"The groups of then_by inside this group."`
}

// ReportTimeInfo describes the time range a time series covered.
type ReportTimeInfo struct {
	Field    string `json:"field"`
	Bucket   string `json:"bucket"`
	From     string `json:"from" doc:"Start of the first bucket, RFC 3339."`
	To       string `json:"to" doc:"End of the last bucket (exclusive), RFC 3339."`
	Timezone string `json:"timezone"`
	Buckets  int    `json:"buckets"`
}

// TaskReport is the result of a report.
type TaskReport struct {
	Groups      []*ReportGroup    `json:"groups" doc:"The groups, largest value first. Empty without group_by."`
	Series      []*ReportPoint    `json:"series,omitempty" doc:"The time series of all tasks, for a time series without group_by."`
	Total       int64             `json:"total" doc:"The number of distinct tasks the report counted, also when a task is part of several groups."`
	Value       float64           `json:"value" doc:"The measure over all counted tasks."`
	Measure     string            `json:"measure"`
	Field       string            `json:"field,omitempty"`
	Unit        string            `json:"unit" doc:"What value counts: tasks, seconds or the unit of the field (empty)."`
	MultiValued bool              `json:"multi_valued" doc:"True when a task can be part of several groups (assignee, label, multi select, user fields), so group values can add up to more than total."`
	Excluded    int64             `json:"excluded,omitempty" doc:"Time series only: tasks that were left out because the date field is empty."`
	Time        *ReportTimeInfo   `json:"time,omitempty"`
	Truncated   bool              `json:"truncated,omitempty" doc:"True when groups were folded into other."`
	Comparison  *ReportComparison `json:"comparison,omitempty" doc:"The previous period, when compare is previous_period."`
}

// reportAgg accumulates the aggregates of a group, enough to fold groups together for every measure.
type reportAgg struct {
	n   int64
	sum float64
	cnt int64
	min *float64
	max *float64
}

func (a *reportAgg) add(o *reportAgg) {
	a.n += o.n
	a.sum += o.sum
	a.cnt += o.cnt
	if o.min != nil && (a.min == nil || *o.min < *a.min) {
		v := *o.min
		a.min = &v
	}
	if o.max != nil && (a.max == nil || *o.max > *a.max) {
		v := *o.max
		a.max = &v
	}
}

func (a *reportAgg) value(measure string) float64 {
	switch measure {
	case ReportMeasureSum:
		return a.sum
	case ReportMeasureAvg:
		if a.cnt == 0 {
			return 0
		}
		return a.sum / float64(a.cnt)
	case ReportMeasureMin:
		if a.min == nil {
			return 0
		}
		return *a.min
	case ReportMeasureMax:
		if a.max == nil {
			return 0
		}
		return *a.max
	}
	return float64(a.n)
}

type reportRow struct {
	keys   []reportKey
	bucket int
	agg    reportAgg
}

// reportPlan is a validated request.
type reportPlan struct {
	req         ReportRequest
	dims        []*reportDim
	measure     string
	measureExpr string
	measureJoin *reportJoin
	unit        string
	loc         *time.Location
	timeCfg     *reportTimePlan
	cmpCfg      *reportTimePlan
	limit       int
	maxGroups   int
}

type reportTimePlan struct {
	field     string
	bucket    string
	column    string
	join      *reportJoin
	bounds    []time.Time
	indexExpr string
	// rangeCond restricts the tasks to the reported range. It is nil for a plan that only buckets (the workload
	// report), which then reads every task of the base query.
	rangeCond builder.Cond
}

func isNumericFieldType(t CustomFieldType) bool {
	return t == CustomFieldTypeNumber || t == CustomFieldTypeCurrency || t == CustomFieldTypeRating || t == CustomFieldTypeProgress
}

func reportAuthTimezone(s *xorm.Session, a web.Auth) string {
	if _, isShare := a.(*LinkSharing); isShare {
		return ""
	}
	u, err := user.GetUserByID(s, a.GetID())
	if err != nil {
		return ""
	}
	return u.Timezone
}

func (req ReportRequest) normalized() ReportRequest {
	if req.Measure == "" {
		req.Measure = ReportMeasureCount
	}
	if req.Limit == 0 {
		req.Limit = reportDefaultLimit
	}
	if req.Time != nil {
		t := *req.Time
		if t.Bucket == "" {
			t.Bucket = ReportBucketDay
		}
		if t.From == "" {
			t.From = reportDefaultFrom
		}
		if t.To == "" {
			t.To = reportDefaultTo
		}
		req.Time = &t
	}
	req.Filter = strings.TrimSpace(req.Filter)
	return req
}

// Validate checks everything about the request that does not need the tasks: names, combinations, fields (which have
// to be readable for the auth), the filter and the time range. Dashboards use it to check the reports of their widgets
// without running them.
func (req ReportRequest) Validate(s *xorm.Session, a web.Auth) error {
	_, err := buildReportPlan(s, a, req)
	return err
}

//nolint:gocyclo
func buildReportPlan(s *xorm.Session, a web.Auth, in ReportRequest) (*reportPlan, error) {
	req := in.normalized()
	plan := &reportPlan{req: req, measure: req.Measure, maxGroups: config.ReportsMaxGroups.GetInt()}

	if req.ProjectID < -1 {
		return nil, ErrReportInvalidParameter{Param: "project_id", Reason: "must be a project id or -1 for the favorites"}
	}
	if req.Limit < 0 {
		return nil, ErrReportInvalidParameter{Param: "limit", Reason: "must not be negative"}
	}
	if req.Limit > plan.maxGroups {
		return nil, ErrReportTooManyGroups{Limit: req.Limit, Max: plan.maxGroups}
	}
	plan.limit = req.Limit

	loc, err := reportLocation(timeZoneName(req.Time), reportAuthTimezone(s, a))
	if err != nil {
		return nil, err
	}
	plan.loc = loc

	// The filter is parsed and its custom fields resolved against what the auth can read, exactly as the task list does.
	filters, err := getTaskFiltersFromFilterString(req.Filter, loc.String())
	if err != nil {
		return nil, err
	}
	if err = resolveCustomFieldFilters(s, a, filters); err != nil {
		return nil, err
	}

	reader := &customFieldReader{s: s, a: a}
	if req.GroupBy == "" && req.ThenBy != "" {
		return nil, ErrReportInvalidParameter{Param: "then_by", Reason: "needs group_by"}
	}
	if req.GroupBy != "" && req.GroupBy == req.ThenBy {
		return nil, ErrReportInvalidParameter{Param: "then_by", Reason: "must differ from group_by"}
	}
	for i, p := range []struct{ param, name string }{{"group_by", req.GroupBy}, {"then_by", req.ThenBy}} {
		if p.name == "" {
			continue
		}
		dim, err := newReportDim(reader, p.name, p.param, i+1)
		if err != nil {
			return nil, err
		}
		plan.dims = append(plan.dims, dim)
	}

	if err = plan.buildMeasure(a, reader); err != nil {
		return nil, err
	}
	if req.Time != nil {
		if err = plan.buildTime(); err != nil {
			return nil, err
		}
	}
	if err = plan.buildCompare(); err != nil {
		return nil, err
	}
	return plan, nil
}

func timeZoneName(t *ReportTime) string {
	if t == nil {
		return ""
	}
	return t.Timezone
}

func (p *reportPlan) buildMeasure(a web.Auth, reader *customFieldReader) error {
	req := p.req
	p.unit = "tasks"
	switch req.Measure {
	case ReportMeasureCount:
		if req.Field != "" {
			return ErrReportInvalidParameter{Param: "field", Reason: "is only used with sum, avg, min and max"}
		}
		return nil
	case ReportMeasureSum, ReportMeasureAvg, ReportMeasureMin, ReportMeasureMax:
	default:
		return ErrReportInvalidParameter{Param: "measure", Reason: fmt.Sprintf("%q is not one of count, sum, avg, min, max", req.Measure)}
	}

	p.unit = ""
	switch req.Field {
	case "":
		return ErrReportInvalidParameter{Param: "field", Reason: "is required for " + req.Measure}
	case ReportFieldPercentDone:
		p.measureExpr = "tasks.percent_done"
	case ReportFieldTimeSpent:
		if _, isShare := a.(*LinkSharing); isShare {
			// Link shares have no time tracking access, so their reports must not show it either.
			return ErrReportNotAvailableForShare{Param: "field"}
		}
		p.measureExpr = "(SELECT SUM(" + reportSecondsBetween("te.start_time", "te.end_time") +
			") FROM time_entries te WHERE te.task_id = tasks.id AND te.end_time IS NOT NULL)"
		p.unit = "seconds"
	default:
		id, ok := parseCustomFieldIdentifier(req.Field)
		if !ok {
			return ErrReportInvalidParameter{Param: "field", Reason: fmt.Sprintf("%q is not percent_done, time_spent or cf_<id>", req.Field)}
		}
		field, err := reader.get(id)
		if err != nil {
			return err
		}
		if !isNumericFieldType(field.Type) {
			return ErrReportFieldKindNotSupported{Param: "field", Field: req.Field, Kind: string(field.Type)}
		}
		p.measureJoin = &reportJoin{
			table: customFieldValuesTable + " m",
			cond:  "m.task_id = tasks.id AND m.custom_field_id = " + strconv.FormatInt(field.ID, 10),
		}
		p.measureExpr = "m.value_number"
	}
	return nil
}

func (p *reportPlan) buildTime() error {
	t := p.req.Time
	if !isReportTimeField(t.Field) {
		return ErrReportInvalidParameter{Param: "time.field", Reason: fmt.Sprintf("%q is not one of created, done_at, due_date, status_entered", t.Field)}
	}
	if !isReportBucket(t.Bucket) {
		return ErrReportInvalidParameter{Param: "time.bucket", Reason: fmt.Sprintf("%q is not one of day, week, month", t.Bucket)}
	}
	now := time.Now().In(p.loc)
	from, err := parseReportDate(t.From, p.loc, now, "time.from")
	if err != nil {
		return err
	}
	to, err := parseReportDate(t.To, p.loc, now, "time.to")
	if err != nil {
		return err
	}
	bounds, err := reportBuckets(from, to, t.Bucket, config.ReportsMaxBuckets.GetInt())
	if err != nil {
		return err
	}

	p.timeCfg = newReportTimePlan(t.Field, t.Bucket, bounds)
	return nil
}

func newReportTimePlan(field, bucket string, bounds []time.Time) *reportTimePlan {
	column, joinTable, joinCond := reportTimeColumn(field)
	tp := &reportTimePlan{
		field:     field,
		bucket:    bucket,
		column:    column,
		bounds:    bounds,
		indexExpr: reportBucketIndexExpr(column, bounds),
		rangeCond: builder.And(
			builder.Gte{column: bounds[0].UTC().Format(dbTimeFormat)},
			builder.Lt{column: bounds[len(bounds)-1].UTC().Format(dbTimeFormat)},
		),
	}
	if joinTable != "" {
		tp.join = &reportJoin{joinTable, joinCond}
	}
	return tp
}

// buildCompare plans the previous period: as many buckets as the range has, shifted back by that many buckets, so a
// range of three months is compared with the three months before it, whatever their length in days.
func (p *reportPlan) buildCompare() error {
	switch p.req.Compare {
	case "":
		return nil
	case "previous_period":
	default:
		return ErrReportInvalidParameter{Param: "compare", Reason: fmt.Sprintf("%q is not previous_period", p.req.Compare)}
	}
	if p.timeCfg == nil {
		return ErrReportInvalidParameter{Param: "compare", Reason: "needs time"}
	}
	if len(p.dims) > 0 {
		return ErrReportInvalidParameter{Param: "compare", Reason: "cannot be combined with group_by"}
	}
	tp := p.timeCfg
	n := len(tp.bounds) - 1
	start := tp.bounds[0]
	y, m, d := start.Date()
	switch tp.bucket {
	case ReportBucketMonth:
		start = time.Date(y, m-time.Month(n), 1, 0, 0, 0, 0, start.Location())
	case ReportBucketWeek:
		start = time.Date(y, m, d-7*n, 0, 0, 0, 0, start.Location())
	default:
		start = time.Date(y, m, d-n, 0, 0, 0, 0, start.Location())
	}
	bounds := []time.Time{start}
	for i := 0; i < n; i++ {
		bounds = append(bounds, reportBucketNext(bounds[i], tp.bucket))
	}
	p.cmpCfg = newReportTimePlan(tp.field, tp.bucket, bounds)
	return nil
}

// reportRun executes a plan.
type reportRun struct {
	s    *xorm.Session
	a    web.Auth
	plan *reportPlan
	base *builder.Builder
}

func (r *reportRun) selectColumns(groupDims []*reportDim, withBucket bool) (cols []string, groupBy string) {
	groups := []string{}
	for i, d := range groupDims {
		alias := "k" + strconv.Itoa(i)
		// Drivers scan a NULL into the zero value of the column type, so the flag tells "none" from 0.
		cols = append(cols, d.keyExpr+" AS "+alias, "CASE WHEN "+d.keyExpr+" IS NULL THEN 1 ELSE 0 END AS "+alias+"n")
		groups = append(groups, alias, alias+"n")
	}
	if withBucket {
		cols = append(cols, r.plan.timeCfg.indexExpr+" AS b")
		groups = append(groups, "b")
	}
	cols = append(cols, "COUNT(DISTINCT tasks.id) AS n")
	if m := r.plan.measureExpr; m != "" {
		cols = append(cols,
			"SUM("+m+") AS s",
			"COUNT("+m+") AS c",
			"MIN("+m+") AS mn",
			"MAX("+m+") AS mx",
		)
	}
	return cols, strings.Join(groups, ", ")
}

// query runs one grouped statement over the readable tasks: the rows are grouped by groupDims and the time bucket and
// restricted to extra.
func (r *reportRun) query(groupDims []*reportDim, withBucket bool, extra builder.Cond) ([]*reportRow, error) {
	cols, groupBy := r.selectColumns(groupDims, withBucket)
	q := builder.Select(cols...).From("tasks")
	for _, d := range groupDims {
		for _, j := range d.joins {
			q = q.LeftJoin(j.table, j.cond)
		}
	}
	if j := r.plan.measureJoin; j != nil {
		q = q.LeftJoin(j.table, j.cond)
	}
	tp := r.plan.timeCfg
	if tp != nil && tp.join != nil {
		q = q.LeftJoin(tp.join.table, tp.join.cond)
	}

	conds := []builder.Cond{builder.In("tasks.id", r.base)}
	if tp != nil && tp.rangeCond != nil {
		conds = append(conds, tp.rangeCond)
	}
	if extra != nil {
		conds = append(conds, extra)
	}
	q = q.Where(builder.And(conds...))
	if groupBy != "" {
		q = q.GroupBy(groupBy)
	}

	raw, err := r.s.QueryInterface(q)
	if err != nil {
		return nil, wrapReportError(err)
	}

	rows := make([]*reportRow, 0, len(raw))
	for _, m := range raw {
		row := &reportRow{agg: reportAgg{n: reportInt(m["n"])}}
		for i, d := range groupDims {
			alias := "k" + strconv.Itoa(i)
			if reportInt(m[alias+"n"]) != 0 {
				row.keys = append(row.keys, d.parseKey(nil))
				continue
			}
			row.keys = append(row.keys, d.parseKey(m[alias]))
		}
		if withBucket {
			row.bucket = int(reportInt(m["b"]))
		}
		if r.plan.measureExpr != "" {
			row.agg.sum = reportFloat(m["s"])
			row.agg.cnt = reportInt(m["c"])
			if row.agg.cnt > 0 {
				mn, mx := reportFloat(m["mn"]), reportFloat(m["mx"])
				row.agg.min, row.agg.max = &mn, &mx
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func wrapReportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrReportTimeout{}
	}
	return err
}

// reportNode is a group while it is being assembled.
type reportNode struct {
	key      reportKey
	label    reportLabel
	agg      reportAgg
	children []*reportNode
	other    bool
}

func (r *reportRun) resolveLabels(d *reportDim, nodes []*reportNode) error {
	keys := make([]reportKey, 0, len(nodes))
	for _, n := range nodes {
		keys = append(keys, n.key)
	}
	labels, err := d.resolve(r.s, keys)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.key.none {
			continue
		}
		l, ok := labels[n.key.canon]
		if !ok {
			l = reportLabel{label: n.key.canon}
		}
		n.label = l
	}
	return nil
}

// foldTop keeps the limit largest groups and merges the rest into one other group.
func (r *reportRun) foldTop(nodes []*reportNode) (kept []*reportNode, rest []*reportNode) {
	measure := r.plan.measure
	sort.SliceStable(nodes, func(i, j int) bool {
		vi, vj := nodes[i].agg.value(measure), nodes[j].agg.value(measure)
		if vi != vj {
			return vi > vj
		}
		if nodes[i].label.label != nodes[j].label.label {
			return nodes[i].label.label < nodes[j].label.label
		}
		return nodes[i].key.canon < nodes[j].key.canon
	})
	if len(nodes) <= r.plan.limit {
		return nodes, nil
	}
	return nodes[:r.plan.limit], nodes[r.plan.limit:]
}

func nodesFromRows(rows []*reportRow, keyIndex int) []*reportNode {
	nodes := make([]*reportNode, 0, len(rows))
	for _, row := range rows {
		nodes = append(nodes, &reportNode{key: row.keys[keyIndex], agg: row.agg})
	}
	return nodes
}

func nodeKeys(nodes []*reportNode) []reportKey {
	keys := make([]reportKey, 0, len(nodes))
	for _, n := range nodes {
		keys = append(keys, n.key)
	}
	return keys
}

func (r *reportRun) point(i int, v float64) *ReportPoint {
	tp := r.plan.timeCfg
	start := tp.bounds[i].In(r.plan.loc)
	return &ReportPoint{
		Bucket: start.Format(reportDateLayout),
		Start:  start.Format(time.RFC3339),
		End:    tp.bounds[i+1].In(r.plan.loc).Format(time.RFC3339),
		Value:  v,
	}
}

// series turns the aggregates per bucket into points, 0 for empty buckets.
func (r *reportRun) series(cells map[int]*reportAgg) []*ReportPoint {
	n := len(r.plan.timeCfg.bounds) - 1
	points := make([]*ReportPoint, 0, n)
	for i := 0; i < n; i++ {
		v := 0.0
		if c, ok := cells[i]; ok {
			v = c.value(r.plan.measure)
		}
		points = append(points, r.point(i, v))
	}
	return points
}

func seriesOf(rows []*reportRow) map[int]*reportAgg {
	cells := map[int]*reportAgg{}
	for _, row := range rows {
		c, ok := cells[row.bucket]
		if !ok {
			c = &reportAgg{}
			cells[row.bucket] = c
		}
		c.add(&row.agg)
	}
	return cells
}

func leafID(keys []reportKey) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k.canon)
	}
	return strings.Join(parts, "\x1f")
}

// execute runs the queries of a report. Without group_by it is one statement for the totals; each dimension level
// adds one grouped statement, a time series one more, so the number of statements never depends on the data.
//
//nolint:gocyclo
func (r *reportRun) execute() (*TaskReport, error) {
	plan := r.plan
	rep := &TaskReport{
		Groups:  []*ReportGroup{},
		Measure: plan.measure,
		Field:   plan.req.Field,
		Unit:    plan.unit,
	}
	for _, d := range plan.dims {
		if d.multi {
			rep.MultiValued = true
		}
	}

	totals, err := r.query(nil, false, nil)
	if err != nil {
		return nil, err
	}
	var overall reportAgg
	if len(totals) > 0 {
		overall = totals[0].agg
	}
	rep.Total = overall.n
	rep.Value = overall.value(plan.measure)

	if plan.cmpCfg != nil {
		if rep.Comparison, err = r.compare(rep); err != nil {
			return nil, err
		}
	}

	if tp := plan.timeCfg; tp != nil {
		rep.Time = &ReportTimeInfo{
			Field:    tp.field,
			Bucket:   tp.bucket,
			From:     tp.bounds[0].In(plan.loc).Format(time.RFC3339),
			To:       tp.bounds[len(tp.bounds)-1].In(plan.loc).Format(time.RFC3339),
			Timezone: plan.loc.String(),
			Buckets:  len(tp.bounds) - 1,
		}
		excluded, err := r.countExcluded()
		if err != nil {
			return nil, err
		}
		rep.Excluded = excluded
	}

	if len(plan.dims) == 0 {
		if plan.timeCfg != nil {
			rows, err := r.query(nil, true, nil)
			if err != nil {
				return nil, err
			}
			rep.Series = r.series(seriesOf(rows))
		}
		return rep, nil
	}

	d1 := plan.dims[0]
	rows, err := r.query([]*reportDim{d1}, false, nil)
	if err != nil {
		return nil, err
	}
	level1 := nodesFromRows(rows, 0)
	if err = r.resolveLabels(d1, level1); err != nil {
		return nil, err
	}
	kept, rest := r.foldTop(level1)

	var otherNode *reportNode
	if len(rest) > 0 {
		rep.Truncated = true
		otherNode = &reportNode{other: true, label: reportLabel{label: "Other"}}
		if d1.multi {
			// A task with several of the folded values must count once in other, so ask the database.
			otherRows, err := r.query(nil, false, r.foldedTasksCond(d1, kept))
			if err != nil {
				return nil, err
			}
			if len(otherRows) > 0 {
				otherNode.agg = otherRows[0].agg
			}
		} else {
			for _, n := range rest {
				otherNode.agg.add(&n.agg)
			}
		}
	}

	if len(plan.dims) == 2 {
		if err = r.attachChildren(kept); err != nil {
			return nil, err
		}
	}

	var series map[string]map[int]*reportAgg
	var otherSeries map[int]*reportAgg
	if plan.timeCfg != nil {
		if series, otherSeries, err = r.fetchSeries(kept, otherNode != nil); err != nil {
			return nil, err
		}
	}

	rep.Groups = r.toGroups(kept, 1, series)
	if otherNode != nil {
		g := r.toGroup(otherNode)
		if otherSeries != nil {
			g.Series = r.series(otherSeries)
		}
		rep.Groups = append(rep.Groups, g)
	}
	return rep, nil
}

// compare runs the totals of the previous period.
func (r *reportRun) compare(rep *TaskReport) (*ReportComparison, error) {
	prevPlan := *r.plan
	prevPlan.timeCfg = r.plan.cmpCfg
	prev := &reportRun{s: r.s, a: r.a, plan: &prevPlan, base: r.base}
	rows, err := prev.query(nil, false, nil)
	if err != nil {
		return nil, err
	}
	var agg reportAgg
	if len(rows) > 0 {
		agg = rows[0].agg
	}
	tp := r.plan.cmpCfg
	c := &ReportComparison{
		Value: agg.value(r.plan.measure),
		Total: agg.n,
		From:  tp.bounds[0].In(r.plan.loc).Format(time.RFC3339),
		To:    tp.bounds[len(tp.bounds)-1].In(r.plan.loc).Format(time.RFC3339),
	}
	c.Delta = rep.Value - c.Value
	if c.Value != 0 {
		pct := c.Delta / math.Abs(c.Value) * 100
		c.DeltaPercent = &pct
	}
	return c, nil
}

func (r *reportRun) toGroup(n *reportNode) *ReportGroup {
	g := &ReportGroup{Label: n.label.label, Color: n.label.color, Value: n.agg.value(r.plan.measure), Other: n.other}
	switch {
	case n.other:
	case n.key.none:
		g.None = true
		g.Label = "None"
	default:
		g.Key = n.key.value
	}
	return g
}

// toGroups converts nodes of the given level (1 or 2) and attaches the series to the leaf level.
func (r *reportRun) toGroups(nodes []*reportNode, level int, series map[string]map[int]*reportAgg) []*ReportGroup {
	return r.toGroupsPath(nodes, level, nil, series)
}

func (r *reportRun) toGroupsPath(nodes []*reportNode, level int, path []reportKey, series map[string]map[int]*reportAgg) []*ReportGroup {
	groups := make([]*ReportGroup, 0, len(nodes)+1)
	leaf := level == len(r.plan.dims)
	for _, n := range nodes {
		g := r.toGroup(n)
		keys := append(append([]reportKey{}, path...), n.key)
		if leaf && series != nil && !n.other {
			g.Series = r.series(series[leafID(keys)])
		}
		if !leaf {
			g.Children = r.toGroupsPath(n.children, level+1, keys, series)
		}
		groups = append(groups, g)
	}
	return groups
}

func (r *reportRun) countExcluded() (int64, error) {
	tp := r.plan.timeCfg
	q := builder.Select("COUNT(DISTINCT tasks.id) AS n").From("tasks")
	if tp.join != nil {
		q = q.LeftJoin(tp.join.table, tp.join.cond)
	}
	q = q.Where(builder.And(builder.In("tasks.id", r.base), builder.IsNull{tp.column}))
	raw, err := r.s.QueryInterface(q)
	if err != nil {
		return 0, wrapReportError(err)
	}
	if len(raw) == 0 {
		return 0, nil
	}
	return reportInt(raw[0]["n"]), nil
}

// attachChildren loads the second level for the kept groups of the first one.
func (r *reportRun) attachChildren(parents []*reportNode) error {
	if len(parents) == 0 {
		return nil
	}
	d1, d2 := r.plan.dims[0], r.plan.dims[1]
	rows, err := r.query([]*reportDim{d1, d2}, false, d1.keyCond(nodeKeys(parents), true))
	if err != nil {
		return err
	}

	byParent := map[string][]*reportNode{}
	all := []*reportNode{}
	for _, row := range rows {
		n := &reportNode{key: row.keys[1], agg: row.agg}
		byParent[row.keys[0].canon] = append(byParent[row.keys[0].canon], n)
		all = append(all, n)
	}
	if err = r.resolveLabels(d2, all); err != nil {
		return err
	}

	for _, p := range parents {
		kids, rest := r.foldTop(byParent[p.key.canon])
		p.children = kids
		if len(rest) > 0 {
			o := &reportNode{other: true, label: reportLabel{label: "Other"}}
			for _, n := range rest {
				o.agg.add(&n.agg)
			}
			p.children = append(p.children, o)
		}
	}
	return nil
}

// fetchSeries loads the time buckets of the kept leaf groups in one statement, plus one for the folded rest.
func (r *reportRun) fetchSeries(kept []*reportNode, hasOther bool) (map[string]map[int]*reportAgg, map[int]*reportAgg, error) {
	dims := r.plan.dims
	out := map[string]map[int]*reportAgg{}

	var extra builder.Cond
	groupDims := dims
	conds := []builder.Cond{dims[0].keyCond(nodeKeys(kept), true)}
	if len(dims) == 2 {
		leaves := []reportKey{}
		for _, p := range kept {
			for _, c := range p.children {
				if !c.other {
					leaves = append(leaves, c.key)
				}
			}
		}
		conds = append(conds, dims[1].keyCond(leaves, true))
	}
	extra = builder.And(conds...)

	rows, err := r.query(groupDims, true, extra)
	if err != nil {
		return nil, nil, err
	}
	perLeaf := map[string][]*reportRow{}
	for _, row := range rows {
		id := leafID(row.keys)
		perLeaf[id] = append(perLeaf[id], row)
	}
	for id, rs := range perLeaf {
		out[id] = seriesOf(rs)
	}

	var otherSeries map[int]*reportAgg
	if hasOther && len(dims) == 1 {
		otherRows, err := r.query(nil, true, r.foldedTasksCond(dims[0], kept))
		if err != nil {
			return nil, nil, err
		}
		otherSeries = seriesOf(otherRows)
	}
	return out, otherSeries, nil
}

// foldedTasksCond selects the tasks that have a value of d outside the kept groups. It is a sub-select and not a join,
// so a task with several folded values is one row and its measure counts once in the other group.
func (r *reportRun) foldedTasksCond(d *reportDim, kept []*reportNode) builder.Cond {
	sub := builder.Select("tasks.id").From("tasks")
	for _, j := range d.joins {
		sub = sub.LeftJoin(j.table, j.cond)
	}
	conds := []builder.Cond{builder.In("tasks.id", r.base)}
	if folded := d.keyCond(nodeKeys(kept), false); folded != nil {
		conds = append(conds, folded)
	}
	return builder.In("tasks.id", sub.Where(builder.And(conds...)))
}

// RunTaskReport aggregates the tasks the auth can read. The set of tasks is built by the same code as the task list
// (TaskCollection.readableTasksQuery), so a report can never count a task the list would not show.
func RunTaskReport(ctx context.Context, s *xorm.Session, a web.Auth, req *ReportRequest) (*TaskReport, error) {
	plan, err := buildReportPlan(s, a, *req)
	if err != nil {
		return nil, err
	}

	cacheKey := reportCacheKey(a, plan)
	if rep, ok := reportCacheGet(cacheKey); ok {
		return rep, nil
	}

	ctx, cancel := context.WithTimeout(ctx, config.ReportsTimeout.GetDuration())
	defer cancel()
	db.SetSessionContext(ctx, s)

	tf := &TaskCollection{
		ProjectID:          plan.req.ProjectID,
		Filter:             plan.req.Filter,
		FilterTimezone:     plan.loc.String(),
		FilterIncludeNulls: plan.req.FilterIncludeNulls,
	}
	base, err := tf.readableTasksQuery(s, a)
	if err != nil {
		return nil, wrapReportError(err)
	}

	rep, err := (&reportRun{s: s, a: a, plan: plan, base: base}).execute()
	if err != nil {
		return nil, wrapReportError(err)
	}
	reportCachePut(cacheKey, rep)
	return rep, nil
}
