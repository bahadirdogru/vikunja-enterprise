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
	"regexp"
	"strconv"

	"code.vikunja.io/api/pkg/db"

	"xorm.io/builder"
	"xorm.io/xorm/schemas"
)

// The filter and sort names of the workflow status of a task.
const (
	taskPropertyStatus         string = "status"
	taskPropertyStatusCategory string = "status_category"

	taskStatusesTable = "task_statuses"
)

// workflowStatusFilter holds what a `status` or `status_category` filter needs. Both are parsed
// without database access: statuses are matched by id and categories by their fixed names.
type workflowStatusFilter struct {
	byCategory bool
	ids        []int64
	categories []WorkflowStatusCategory
}

var statusComparators = []taskFilterComparator{
	taskFilterComparatorEquals, taskFilterComparatorNotEquals, taskFilterComparatorIn, taskFilterComparatorNotIn,
}

// parseWorkflowStatusFilter validates the comparator and value of a status or status_category filter.
func parseWorkflowStatusFilter(field string, comparator taskFilterComparator, raw string) (*workflowStatusFilter, error) {
	if !comparatorIn(comparator, statusComparators...) {
		return nil, ErrInvalidTaskFilterComparator{Comparator: comparator}
	}
	badValue := ErrInvalidTaskFilterValue{Field: field, Value: raw}

	isList := comparatorIn(comparator, taskFilterComparatorIn, taskFilterComparatorNotIn)
	parts := splitFilterList(raw)
	if len(parts) == 0 || (!isList && len(parts) != 1) {
		return nil, badValue
	}

	f := &workflowStatusFilter{byCategory: field == taskPropertyStatusCategory}
	for _, p := range parts {
		if f.byCategory {
			c := WorkflowStatusCategory(p)
			if !c.isValid() {
				return nil, badValue
			}
			f.categories = append(f.categories, c)
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, badValue
		}
		f.ids = append(f.ids, id)
	}
	return f, nil
}

// taskStatusExists is a sub-select over the status row of the task row aliased taskAlias, narrowed by extra.
func taskStatusExists(taskAlias string, extra builder.Cond) *builder.Builder {
	cond := builder.Expr(taskStatusesTable + ".task_id = " + taskAlias + ".id")
	full := cond
	if extra != nil {
		full = builder.And(cond, extra)
	}
	return builder.Select("1").From(taskStatusesTable).Where(full)
}

// workflowStatusFilterCond builds the condition of a status filter against the task table aliased
// taskAlias. Positive comparators become EXISTS, negative ones NOT EXISTS of the positive match,
// so tasks without a status match them and never match a positive one.
func workflowStatusFilterCond(f *taskFilter, includeNulls bool, taskAlias string) builder.Cond {
	s := f.status
	negate := f.comparator == taskFilterComparatorNotEquals || f.comparator == taskFilterComparatorNotIn

	var match builder.Cond
	if s.byCategory {
		categories := make([]string, 0, len(s.categories))
		for _, c := range s.categories {
			categories = append(categories, string(c))
		}
		match = builder.In(taskStatusesTable+".status_id",
			builder.Select("id").From("workflow_statuses").Where(builder.In("category", categories)))
	} else {
		match = builder.In(taskStatusesTable+".status_id", s.ids)
	}

	sub := taskStatusExists(taskAlias, match)
	cond := builder.Exists(sub)
	if negate {
		cond = builder.NotExists(sub)
	}
	if includeNulls {
		cond = builder.Or(cond, builder.NotExists(taskStatusExists(taskAlias, nil)))
	}
	return cond
}

// workflowStatusSortExpr is a scalar sub-select returning the position of the status of a task, NULL
// when it has none.
const workflowStatusSortExpr = "(SELECT ws.`position` FROM " + taskStatusesTable +
	" tst INNER JOIN workflow_statuses ws ON ws.id = tst.status_id WHERE tst.task_id = tasks.id)"

// workflowStatusOrderPart is the ORDER BY part of a status sort. Tasks without a status sort last in
// both directions, using the same per-database workarounds as built-in columns.
func workflowStatusOrderPart(param *sortParam) string {
	part := workflowStatusSortExpr + " " + param.orderBy.String()
	if db.Type() == schemas.MYSQL {
		part = workflowStatusSortExpr + " IS NULL, " + part
	}
	if db.Type() == schemas.POSTGRES || db.Type() == schemas.SQLITE {
		part += " NULLS LAST"
	}
	return part
}

func isStatusFilterField(field string) bool {
	return field == taskPropertyStatus || field == taskPropertyStatusCategory
}

// rewriteBucketFilterForStatusView turns the bucket_id conditions of a request on a status mode view into
// status conditions, since the bucket id of a status column is the id of the status.
func rewriteBucketFilterForStatusView(filter string) string {
	return statusBucketFilterRegex.ReplaceAllString(filter, taskPropertyStatus+" = ${1}")
}

var statusBucketFilterRegex = regexp.MustCompile(`bucket_id\s*=\s*-(\d+)`)
