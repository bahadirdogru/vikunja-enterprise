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
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"

	"github.com/jszwedko/go-datemath"
	"xorm.io/xorm/schemas"
)

// The time fields a time series can bucket by.
const (
	ReportTimeFieldCreated       = "created"
	ReportTimeFieldDoneAt        = "done_at"
	ReportTimeFieldDueDate       = "due_date"
	ReportTimeFieldStatusEntered = "status_entered"
)

// The bucket sizes of a time series.
const (
	ReportBucketDay   = "day"
	ReportBucketWeek  = "week"
	ReportBucketMonth = "month"
)

const (
	reportDefaultFrom = "now-90d"
	reportDefaultTo   = "now"
	reportDateLayout  = "2006-01-02"
)

var reportPlainDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func isReportTimeField(f string) bool {
	switch f {
	case ReportTimeFieldCreated, ReportTimeFieldDoneAt, ReportTimeFieldDueDate, ReportTimeFieldStatusEntered:
		return true
	}
	return false
}

func isReportBucket(b string) bool {
	return b == ReportBucketDay || b == ReportBucketWeek || b == ReportBucketMonth
}

// reportTimeColumn is the column of a time field and the join it needs.
func reportTimeColumn(field string) (column, joinTable, joinCond string) {
	switch field {
	case ReportTimeFieldDoneAt:
		return "tasks.done_at", "", ""
	case ReportTimeFieldDueDate:
		return "tasks.due_date", "", ""
	case ReportTimeFieldStatusEntered:
		return "ts_time.entered", "task_statuses ts_time", "ts_time.task_id = tasks.id"
	}
	return "tasks.created", "", ""
}

// parseReportDate reads an RFC 3339 timestamp, a plain date (midnight in loc), now, now+3d, now-90d, or a date math
// expression like now/d.
func parseReportDate(raw string, loc *time.Location, now time.Time, param string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	bad := func(reason string) error {
		return ErrReportInvalidParameter{Param: param, Reason: reason}
	}
	if raw == "" {
		return time.Time{}, bad("the date is empty")
	}
	if reportPlainDate.MatchString(raw) {
		t, err := time.ParseInLocation(reportDateLayout, raw, loc)
		if err != nil {
			return time.Time{}, bad(fmt.Sprintf("%q is not a valid date", raw))
		}
		return t, nil
	}
	if ds, err := spec.ParseDateSpec(raw, false); err == nil {
		// A timestamp carries its own offset; the buckets are cut in the time zone of the report.
		return ds.Resolve(now).In(loc), nil
	}

	var budget businessDayBudget
	if err := budget.spend(raw); err != nil {
		return time.Time{}, bad(err.Error())
	}
	expr, err := safeDatemathParse(raw)
	if err != nil {
		return time.Time{}, bad(fmt.Sprintf("%q is not an RFC 3339 timestamp, a date like 2026-12-31, now, now-90d or a date math expression", raw))
	}
	return expr.Time(datemath.WithLocation(loc), datemath.WithNow(now), datemath.WithStartOfWeek(time.Monday)).In(loc), nil
}

// reportDayStart is the first instant of a calendar day in loc. Where daylight saving time starts at midnight
// (America/Havana, America/Santiago) that hour does not exist and time.Date answers with 23:00 of the day before; the
// day then starts at the change of the offset.
func reportDayStart(y int, m time.Month, d int, loc *time.Location) time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if t.Day() != time.Date(y, m, d, 12, 0, 0, 0, loc).Day() {
		if _, end := t.ZoneBounds(); !end.IsZero() {
			return end
		}
	}
	return t
}

// reportBucketStart truncates t to the start of its bucket in t's own location. Weeks start on Monday.
func reportBucketStart(t time.Time, bucket string) time.Time {
	y, m, d := t.Date()
	loc := t.Location()
	switch bucket {
	case ReportBucketMonth:
		return reportDayStart(y, m, 1, loc)
	case ReportBucketWeek:
		back := (int(t.Weekday()) + 6) % 7
		return reportDayStart(y, m, d-back, loc)
	}
	return reportDayStart(y, m, d, loc)
}

// reportBucketNext is the start of the bucket after the one starting at start. It adds calendar days, so a bucket
// across a daylight saving change is 23 or 25 hours long and still starts at local midnight.
func reportBucketNext(start time.Time, bucket string) time.Time {
	y, m, d := start.Date()
	switch bucket {
	case ReportBucketMonth:
		return reportDayStart(y, m+1, 1, start.Location())
	case ReportBucketWeek:
		return reportDayStart(y, m, d+7, start.Location())
	}
	return reportDayStart(y, m, d+1, start.Location())
}

// reportBuckets returns the boundaries of the buckets covering from to to: bucket i is [b[i], b[i+1]).
// The first bucket is the one containing from, the last one the one containing to.
func reportBuckets(from, to time.Time, bucket string, maxBuckets int) ([]time.Time, error) {
	if to.Before(from) {
		return nil, ErrReportInvalidParameter{Param: "time.to", Reason: "must not be before time.from"}
	}
	last := reportBucketStart(to, bucket)
	cur := reportBucketStart(from, bucket)
	bounds := []time.Time{cur}
	for !cur.After(last) {
		cur = reportBucketNext(cur, bucket)
		bounds = append(bounds, cur)
		if len(bounds)-1 > maxBuckets {
			return nil, ErrReportTooManyBuckets{Buckets: len(bounds) - 1, Max: maxBuckets}
		}
	}
	return bounds, nil
}

func reportSQLTime(t time.Time) string {
	return "'" + t.UTC().Format(dbTimeFormat) + "'"
}

// reportBucketIndexExpr returns an expression that evaluates to the index of the bucket the column falls into, for
// columns inside [bounds[0], bounds[len-1]). It is a balanced tree of CASE expressions over the bucket boundaries,
// so the database compares at most log2(buckets) values per row and the boundaries are exact in every time zone, with
// no dependency on the date functions of the database. The boundaries are formatted by us, never user text.
func reportBucketIndexExpr(column string, bounds []time.Time) string {
	var build func(lo, hi int) string
	build = func(lo, hi int) string {
		if hi-lo <= 1 {
			return strconv.Itoa(lo)
		}
		mid := (lo + hi) / 2
		return "CASE WHEN " + column + " < " + reportSQLTime(bounds[mid]) + " THEN " + build(lo, mid) + " ELSE " + build(mid, hi) + " END"
	}
	return build(0, len(bounds)-1)
}

// reportSecondsBetween is the one dialect specific expression of the reports: the length of a time entry in seconds.
func reportSecondsBetween(startCol, endCol string) string {
	if db.Type() == schemas.POSTGRES {
		return "EXTRACT(EPOCH FROM (" + endCol + " - " + startCol + "))"
	}
	if db.Type() == schemas.MYSQL {
		return "TIMESTAMPDIFF(SECOND, " + startCol + ", " + endCol + ")"
	}
	return "(strftime('%s', " + endCol + ") - strftime('%s', " + startCol + "))"
}

// reportLocation returns the time zone a report is bucketed in: the requested one, else the setting of the user,
// else the service time zone. Link shares have no setting.
func reportLocation(name string, userTimezone string) (*time.Location, error) {
	if name == "" {
		name = userTimezone
	}
	if name == "" {
		return config.GetTimeZone(), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrReportInvalidParameter{Param: "time.timezone", Reason: fmt.Sprintf("%q is not a known IANA time zone", name)}
	}
	return loc, nil
}
