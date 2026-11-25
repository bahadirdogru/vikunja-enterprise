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
	"strconv"
	"strings"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// The built-in dimensions a report can group by.
const (
	ReportDimStatus         = "status"
	ReportDimStatusCategory = "status_category"
	ReportDimAssignee       = "assignee"
	ReportDimLabel          = "label"
	ReportDimProject        = "project"
	ReportDimPriority       = "priority"
	ReportDimDone           = "done"
	ReportDimCreatedBy      = "created_by"
)

var reportBuiltinDims = []string{
	ReportDimStatus,
	ReportDimStatusCategory,
	ReportDimAssignee,
	ReportDimLabel,
	ReportDimProject,
	ReportDimPriority,
	ReportDimDone,
	ReportDimCreatedBy,
}

type reportKeyKind int

const (
	reportKeyInt reportKeyKind = iota
	reportKeyText
	reportKeyBool
	reportKeyNumber
)

// reportKey is the value of a task for one dimension. none is the group of tasks without a value.
type reportKey struct {
	canon string
	value any
	none  bool
}

type reportLabel struct {
	label string
	color string
	// code names a label the server wrote itself (Open, Done, Low...), so the interface can say it in its own
	// language; it is empty for names that belong to the data (a project, a label, a user).
	code string
}

type reportJoin struct {
	table string
	cond  string
}

// reportDim is one way to group tasks: the joins and the expression that produce its key and the lookup of the
// label and color of a key.
type reportDim struct {
	name    string
	multi   bool
	kind    reportKeyKind
	joins   []reportJoin
	keyExpr string
	field   *CustomField
	resolve func(s *xorm.Session, keys []reportKey) (map[string]reportLabel, error)
}

func (d *reportDim) parseKey(raw any) reportKey {
	if raw == nil {
		return reportKey{none: true, canon: "\x00none"}
	}
	switch d.kind {
	case reportKeyText:
		v := reportString(raw)
		return reportKey{canon: v, value: v}
	case reportKeyBool:
		v := reportInt(raw) != 0
		return reportKey{canon: strconv.FormatBool(v), value: v}
	case reportKeyNumber:
		v := reportFloat(raw)
		return reportKey{canon: strconv.FormatFloat(v, 'g', -1, 64), value: v}
	case reportKeyInt:
		// Below, as the zero value of the kind.
	}
	v := reportInt(raw)
	return reportKey{canon: strconv.FormatInt(v, 10), value: v}
}

// sqlValues returns the non-null key values as bind values.
func (d *reportDim) sqlValues(keys []reportKey) (vals []any, hasNone bool) {
	for _, k := range keys {
		if k.none {
			hasNone = true
			continue
		}
		if b, is := k.value.(bool); is {
			if b {
				vals = append(vals, 1)
			} else {
				vals = append(vals, 0)
			}
			continue
		}
		vals = append(vals, k.value)
	}
	return vals, hasNone
}

// keyCond restricts rows to the given keys (include) or to every key except them.
func (d *reportDim) keyCond(keys []reportKey, include bool) builder.Cond {
	vals, hasNone := d.sqlValues(keys)
	var inCond, notInCond builder.Cond
	if len(vals) > 0 {
		inCond = builder.In(d.keyExpr, vals...)
		notInCond = builder.NotIn(d.keyExpr, vals...)
	}

	if include {
		conds := []builder.Cond{}
		if inCond != nil {
			conds = append(conds, inCond)
		}
		if hasNone {
			conds = append(conds, builder.IsNull{d.keyExpr})
		}
		if len(conds) == 0 {
			return builder.In(d.keyExpr, []int64{})
		}
		return builder.Or(conds...)
	}

	if hasNone {
		return builder.And(builder.NotNull{d.keyExpr}, notInCond)
	}
	if notInCond == nil {
		return nil
	}
	return builder.Or(builder.IsNull{d.keyExpr}, notInCond)
}

// newReportDim builds the dimension of a group_by or then_by name. idx makes the join aliases unique.
func newReportDim(r *customFieldReader, name, param string, idx int) (*reportDim, error) {
	alias := "d" + strconv.Itoa(idx)
	statusJoin := reportJoin{"task_statuses " + alias, alias + ".task_id = tasks.id"}

	switch name {
	case ReportDimStatus:
		return &reportDim{
			name: name, kind: reportKeyInt,
			joins:   []reportJoin{statusJoin},
			keyExpr: alias + ".status_id",
			resolve: resolveStatusLabels,
		}, nil
	case ReportDimStatusCategory:
		return &reportDim{
			name: name, kind: reportKeyText,
			joins:   []reportJoin{statusJoin, {"workflow_statuses w" + alias, "w" + alias + ".id = " + alias + ".status_id"}},
			keyExpr: "w" + alias + ".category",
			resolve: resolveStatusCategoryLabels,
		}, nil
	case ReportDimAssignee:
		return &reportDim{
			name: name, kind: reportKeyInt, multi: true,
			joins:   []reportJoin{{"task_assignees " + alias, alias + ".task_id = tasks.id"}},
			keyExpr: alias + ".user_id",
			resolve: resolveUserLabels,
		}, nil
	case ReportDimLabel:
		return &reportDim{
			name: name, kind: reportKeyInt, multi: true,
			joins:   []reportJoin{{"label_tasks " + alias, alias + ".task_id = tasks.id"}},
			keyExpr: alias + ".label_id",
			resolve: resolveLabelLabels,
		}, nil
	case ReportDimProject:
		return &reportDim{name: name, kind: reportKeyInt, keyExpr: "tasks.project_id", resolve: resolveProjectLabels}, nil
	case ReportDimPriority:
		return &reportDim{name: name, kind: reportKeyInt, keyExpr: "NULLIF(tasks.priority, 0)", resolve: resolvePriorityLabels}, nil
	case ReportDimDone:
		return &reportDim{name: name, kind: reportKeyBool, keyExpr: "CASE WHEN tasks.done THEN 1 ELSE 0 END", resolve: resolveDoneLabels}, nil
	case ReportDimCreatedBy:
		return &reportDim{name: name, kind: reportKeyInt, keyExpr: "tasks.created_by_id", resolve: resolveUserLabels}, nil
	}

	id, isCustom := parseCustomFieldIdentifier(name)
	if !isCustom {
		return nil, ErrReportInvalidParameter{
			Param:  param,
			Reason: fmt.Sprintf("%q is not a dimension, use one of %s or cf_<id>", name, strings.Join(reportBuiltinDims, ", ")),
		}
	}
	field, err := r.get(id)
	if err != nil {
		return nil, err
	}
	join := reportJoin{
		table: customFieldValuesTable + " " + alias,
		cond:  alias + ".task_id = tasks.id AND " + alias + ".custom_field_id = " + strconv.FormatInt(field.ID, 10),
	}
	dim := &reportDim{name: name, field: field, joins: []reportJoin{join}}
	switch field.Type { //nolint:exhaustive // every other type is refused as a dimension
	case CustomFieldTypeSelect, CustomFieldTypeMultiSelect:
		dim.kind, dim.keyExpr, dim.multi = reportKeyInt, alias+".option_id", field.Type == CustomFieldTypeMultiSelect
		dim.resolve = resolveOptionLabels
	case CustomFieldTypeUser:
		dim.kind, dim.keyExpr, dim.multi = reportKeyInt, alias+".user_id", true
		dim.resolve = resolveUserLabels
	case CustomFieldTypeCheckbox:
		// No stored value counts as false, like in the checkbox filter.
		dim.kind, dim.keyExpr = reportKeyBool, "CASE WHEN "+alias+".value_bool THEN 1 ELSE 0 END"
		dim.resolve = resolveCheckedLabels
	case CustomFieldTypeRating:
		dim.kind, dim.keyExpr = reportKeyNumber, alias+".value_number"
		dim.resolve = resolveNumberLabels
	default:
		return nil, ErrReportFieldKindNotSupported{Param: param, Field: name, Kind: string(field.Type)}
	}
	return dim, nil
}

func int64Keys(keys []reportKey) []int64 {
	ids := make([]int64, 0, len(keys))
	for _, k := range keys {
		if id, ok := k.value.(int64); ok && !k.none {
			ids = append(ids, id)
		}
	}
	return ids
}

func resolveStatusLabels(s *xorm.Session, keys []reportKey) (map[string]reportLabel, error) {
	out := map[string]reportLabel{}
	ids := int64Keys(keys)
	if len(ids) == 0 {
		return out, nil
	}
	statuses, err := findByIDs[WorkflowStatus](s, ids)
	if err != nil {
		return nil, err
	}
	for _, st := range statuses {
		out[strconv.FormatInt(st.ID, 10)] = reportLabel{label: st.Title, color: st.Color}
	}
	return out, nil
}

var reportCategoryLabels = map[WorkflowStatusCategory]reportLabel{
	WorkflowCategoryOpen:       {"Open", "#94a3b8", "status_category.open"},
	WorkflowCategoryInProgress: {"In progress", "#3b82f6", "status_category.in_progress"},
	WorkflowCategoryDone:       {"Done", "#22c55e", "status_category.done"},
	WorkflowCategoryCancelled:  {"Cancelled", "#ef4444", "status_category.cancelled"},
}

func resolveStatusCategoryLabels(_ *xorm.Session, keys []reportKey) (map[string]reportLabel, error) {
	out := map[string]reportLabel{}
	for _, k := range keys {
		if l, ok := reportCategoryLabels[WorkflowStatusCategory(k.canon)]; ok && !k.none {
			out[k.canon] = l
		}
	}
	return out, nil
}

// resolveUserLabels names users that are assignees, creators or user field values of tasks in the report only.
func resolveUserLabels(s *xorm.Session, keys []reportKey) (map[string]reportLabel, error) {
	out := map[string]reportLabel{}
	ids := int64Keys(keys)
	if len(ids) == 0 {
		return out, nil
	}
	users, err := usersByIDs(s, ids)
	if err != nil {
		return nil, err
	}
	for id, u := range users {
		out[strconv.FormatInt(id, 10)] = reportLabel{label: u.GetName()}
	}
	return out, nil
}

func resolveLabelLabels(s *xorm.Session, keys []reportKey) (map[string]reportLabel, error) {
	out := map[string]reportLabel{}
	ids := int64Keys(keys)
	if len(ids) == 0 {
		return out, nil
	}
	labels, err := findByIDs[Label](s, ids)
	if err != nil {
		return nil, err
	}
	for _, l := range labels {
		color := ""
		if l.HexColor != "" {
			color = "#" + strings.TrimPrefix(l.HexColor, "#")
		}
		out[strconv.FormatInt(l.ID, 10)] = reportLabel{label: l.Title, color: color}
	}
	return out, nil
}

func resolveProjectLabels(s *xorm.Session, keys []reportKey) (map[string]reportLabel, error) {
	out := map[string]reportLabel{}
	ids := int64Keys(keys)
	if len(ids) == 0 {
		return out, nil
	}
	projects, err := projectsByIDs(s, ids)
	if err != nil {
		return nil, err
	}
	for id, p := range projects {
		color := ""
		if p.HexColor != "" {
			color = "#" + strings.TrimPrefix(p.HexColor, "#")
		}
		out[strconv.FormatInt(id, 10)] = reportLabel{label: p.Title, color: color}
	}
	return out, nil
}

var reportPriorityNames = map[int64]string{1: "Low", 2: "Medium", 3: "High", 4: "Urgent", 5: "DO NOW"}

func resolvePriorityLabels(_ *xorm.Session, keys []reportKey) (map[string]reportLabel, error) {
	out := map[string]reportLabel{}
	for _, k := range keys {
		id, ok := k.value.(int64)
		if !ok || k.none {
			continue
		}
		name, known := reportPriorityNames[id]
		if !known {
			out[k.canon] = reportLabel{label: k.canon}
			continue
		}
		out[k.canon] = reportLabel{label: name, code: "priority." + k.canon}
	}
	return out, nil
}

func resolveDoneLabels(_ *xorm.Session, _ []reportKey) (map[string]reportLabel, error) {
	return map[string]reportLabel{"true": {"Done", "#22c55e", "done.true"}, "false": {"Open", "#94a3b8", "done.false"}}, nil
}

func resolveCheckedLabels(_ *xorm.Session, _ []reportKey) (map[string]reportLabel, error) {
	return map[string]reportLabel{"true": {"Checked", "", "checked.true"}, "false": {"Not checked", "", "checked.false"}}, nil
}

func resolveNumberLabels(_ *xorm.Session, keys []reportKey) (map[string]reportLabel, error) {
	out := map[string]reportLabel{}
	for _, k := range keys {
		if !k.none {
			out[k.canon] = reportLabel{label: k.canon}
		}
	}
	return out, nil
}

// resolveOptionLabels loads the options by id only. The options in the result come from values on tasks of the
// report, never from a field the viewer picked without being able to read it, because the field itself is checked
// when the dimension is built.
func resolveOptionLabels(s *xorm.Session, keys []reportKey) (map[string]reportLabel, error) {
	out := map[string]reportLabel{}
	ids := int64Keys(keys)
	if len(ids) == 0 {
		return out, nil
	}
	options, err := findByIDs[CustomFieldOption](s, ids)
	if err != nil {
		return nil, err
	}
	for _, o := range options {
		out[strconv.FormatInt(o.ID, 10)] = reportLabel{label: o.Title, color: o.Color}
	}
	return out, nil
}

// reportScalar converts the scanned value of any database driver.
func reportString(v any) string {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case string:
		return t
	}
	return fmt.Sprint(v)
}

func reportFloat(v any) float64 {
	switch t := v.(type) {
	case nil:
		return 0
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	case int:
		return float64(t)
	case float64:
		return t
	case float32:
		return float64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(reportString(v)), 64)
	return f
}

func reportInt(v any) int64 {
	switch t := v.(type) {
	case nil:
		return 0
	case int64:
		return t
	case int32:
		return int64(t)
	case int:
		return int64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	case float64:
		return int64(t)
	}
	s := strings.TrimSpace(reportString(v))
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if s == "t" || s == "true" {
		return 1
	}
	f, _ := strconv.ParseFloat(s, 64)
	return int64(f)
}
