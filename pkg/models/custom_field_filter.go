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
	"reflect"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

// customFieldFilterPrefix starts the identifier of a custom field in filter and sort
// strings: cf_<fieldID>.
const customFieldFilterPrefix = "cf_"

const customFieldValuesTable = "task_custom_field_values"

// parseCustomFieldIdentifier returns the field id of a cf_<id> identifier.
func parseCustomFieldIdentifier(name string) (id int64, ok bool) {
	rest, found := strings.CutPrefix(name, customFieldFilterPrefix)
	if !found || rest == "" {
		return 0, false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

// customFieldFilter holds what a cf_<id> filter needs. The parser has no database access, so
// the raw value is kept until resolveCustomFieldFilters knows the type of the field.
type customFieldFilter struct {
	fieldID int64
	raw     string
	loc     *time.Location
	budget  *businessDayBudget

	// Set by resolve.
	field   *CustomField
	numbers []float64
	dates   []time.Time
	texts   []string
	bools   []bool
	ids     []int64
	names   []string
}

func (c *customFieldFilter) identifier() string {
	return customFieldFilterPrefix + strconv.FormatInt(c.fieldID, 10)
}

// customFieldReader loads custom fields the caller may read. A field that does not exist and
// one the caller cannot read produce the same error, so field ids of other projects cannot
// be probed.
type customFieldReader struct {
	s      *xorm.Session
	a      web.Auth
	fields map[int64]*CustomField
}

func (r *customFieldReader) get(id int64) (*CustomField, error) {
	unknown := ErrInvalidTaskField{TaskField: customFieldFilterPrefix + strconv.FormatInt(id, 10)}
	if f, seen := r.fields[id]; seen {
		if f == nil {
			return nil, unknown
		}
		return f, nil
	}
	if r.fields == nil {
		r.fields = map[int64]*CustomField{}
	}

	field := &CustomField{}
	exists, err := r.s.Where("id = ?", id).Get(field)
	if err != nil {
		return nil, err
	}
	if exists && r.a != nil {
		can, _, err := (&Project{ID: field.ProjectID}).CanRead(r.s, r.a)
		if err != nil && !IsErrProjectDoesNotExist(err) {
			return nil, err
		}
		exists = err == nil && can
	}
	if !exists {
		r.fields[id] = nil
		return nil, unknown
	}
	r.fields[id] = field
	return field, nil
}

// resolveCustomFieldFilters validates every cf_<id> filter (recursing into groups) against
// its field and converts the values to native types. It is idempotent. With a nil auth the
// read check is skipped; only use that for conditions that are restricted to readable
// projects some other way.
func resolveCustomFieldFilters(s *xorm.Session, a web.Auth, filters []*taskFilter) error {
	return resolveCustomFieldFiltersWith(&customFieldReader{s: s, a: a}, filters)
}

func resolveCustomFieldFiltersWith(r *customFieldReader, filters []*taskFilter) error {
	for _, f := range filters {
		if nested, is := f.value.([]*taskFilter); is {
			if err := resolveCustomFieldFiltersWith(r, nested); err != nil {
				return err
			}
			continue
		}
		if f.customField == nil {
			continue
		}
		field, err := r.get(f.customField.fieldID)
		if err != nil {
			return err
		}
		if err := f.customField.resolve(f, field); err != nil {
			return err
		}
	}
	return nil
}

func comparatorIn(c taskFilterComparator, allowed ...taskFilterComparator) bool {
	for _, a := range allowed {
		if a == c {
			return true
		}
	}
	return false
}

var (
	cfOrderedComparators = []taskFilterComparator{
		taskFilterComparatorEquals, taskFilterComparatorNotEquals, taskFilterComparatorGreater,
		taskFilterComparatorGreateEquals, taskFilterComparatorLess, taskFilterComparatorLessEquals,
	}
	cfEqualityComparators = []taskFilterComparator{
		taskFilterComparatorEquals, taskFilterComparatorNotEquals,
	}
	cfTextComparators = []taskFilterComparator{
		taskFilterComparatorEquals, taskFilterComparatorNotEquals, taskFilterComparatorLike,
	}
	cfSetComparators = []taskFilterComparator{
		taskFilterComparatorEquals, taskFilterComparatorNotEquals, taskFilterComparatorIn, taskFilterComparatorNotIn,
	}
)

func splitFilterList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

//nolint:gocyclo
func (c *customFieldFilter) resolve(f *taskFilter, field *CustomField) error {
	c.field = field
	c.numbers, c.dates, c.texts, c.bools, c.ids, c.names = nil, nil, nil, nil, nil, nil
	badValue := ErrInvalidTaskFilterValue{Field: c.identifier(), Value: c.raw}

	var allowed []taskFilterComparator
	switch field.Type {
	case CustomFieldTypeNumber, CustomFieldTypeCurrency, CustomFieldTypeRating, CustomFieldTypeProgress, CustomFieldTypeDate:
		allowed = cfOrderedComparators
	case CustomFieldTypeCheckbox:
		allowed = cfEqualityComparators
	case CustomFieldTypeText, CustomFieldTypeLongText, CustomFieldTypeURL, CustomFieldTypeEmail:
		allowed = cfTextComparators
	case CustomFieldTypeSelect, CustomFieldTypeMultiSelect, CustomFieldTypeUser:
		allowed = cfSetComparators
	default:
		return ErrInvalidTaskField{TaskField: c.identifier()}
	}
	if !comparatorIn(f.comparator, allowed...) {
		return ErrInvalidTaskFilterComparator{Comparator: f.comparator}
	}

	raw := strings.TrimSpace(c.raw)
	isList := comparatorIn(f.comparator, taskFilterComparatorIn, taskFilterComparatorNotIn)
	switch field.Type {
	case CustomFieldTypeNumber, CustomFieldTypeCurrency, CustomFieldTypeRating, CustomFieldTypeProgress:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return badValue
		}
		c.numbers = []float64{n}
	case CustomFieldTypeDate:
		// Same parsing as the built-in date fields: date math, formats and timezone.
		dueDate, _ := reflect.TypeOf(Task{}).FieldByName("DueDate")
		v, err := getValueForField(dueDate, raw, c.loc, c.budget)
		if err != nil {
			return badValue
		}
		c.dates = []time.Time{v.(time.Time)}
	case CustomFieldTypeCheckbox:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return badValue
		}
		c.bools = []bool{b}
	case CustomFieldTypeText, CustomFieldTypeLongText, CustomFieldTypeURL, CustomFieldTypeEmail:
		c.texts = []string{raw}
	case CustomFieldTypeSelect, CustomFieldTypeMultiSelect:
		parts := splitFilterList(raw)
		if len(parts) == 0 || (!isList && len(parts) != 1) {
			return badValue
		}
		for _, p := range parts {
			id, err := strconv.ParseInt(p, 10, 64)
			if err != nil {
				return badValue
			}
			c.ids = append(c.ids, id)
		}
	case CustomFieldTypeUser:
		parts := splitFilterList(raw)
		if len(parts) == 0 || (!isList && len(parts) != 1) {
			return badValue
		}
		// Like the assignees filter, a value that is not a number is a username.
		for _, p := range parts {
			if id, err := strconv.ParseInt(p, 10, 64); err == nil {
				c.ids = append(c.ids, id)
				continue
			}
			c.names = append(c.names, p)
		}
	}
	f.value = c.raw
	return nil
}

// customFieldValueExists is a sub-select over the value rows of one field of the task row
// aliased taskAlias, narrowed by extra.
func customFieldValueExists(fieldID int64, taskAlias string, extra builder.Cond) *builder.Builder {
	cond := builder.And(
		builder.Expr(customFieldValuesTable+".task_id = "+taskAlias+".id"),
		builder.Eq{customFieldValuesTable + ".custom_field_id": fieldID},
	)
	if extra != nil {
		cond = builder.And(cond, extra)
	}
	return builder.Select("1").From(customFieldValuesTable).Where(cond)
}

func orderedCond(column string, cmp taskFilterComparator, v interface{}) builder.Cond {
	switch cmp {
	case taskFilterComparatorEquals:
		return builder.Eq{column: v}
	case taskFilterComparatorGreater:
		return builder.Gt{column: v}
	case taskFilterComparatorGreateEquals:
		return builder.Gte{column: v}
	case taskFilterComparatorLess:
		return builder.Lt{column: v}
	case taskFilterComparatorLessEquals:
		return builder.Lte{column: v}
	case taskFilterComparatorInvalid, taskFilterComparatorNotEquals, taskFilterComparatorLike, taskFilterComparatorIn, taskFilterComparatorNotIn:
		// No ordering comparison.
	}
	return nil
}

// customFieldFilterCond builds the condition of a resolved cf_<id> filter against the task
// table aliased taskAlias. Positive comparators become EXISTS, negative ones NOT EXISTS of
// the positive match, so tasks without any value match them.
//
//nolint:gocyclo
func customFieldFilterCond(f *taskFilter, includeNulls bool, taskAlias string) (builder.Cond, error) {
	c := f.customField
	if c.field == nil {
		return nil, ErrInvalidTaskField{TaskField: c.identifier()}
	}

	col := func(name string) string { return customFieldValuesTable + "." + name }
	negate := f.comparator == taskFilterComparatorNotEquals || f.comparator == taskFilterComparatorNotIn
	positive := f.comparator
	if negate {
		positive = taskFilterComparatorEquals
		if f.comparator == taskFilterComparatorNotIn {
			positive = taskFilterComparatorIn
		}
	}

	var match builder.Cond
	switch c.field.Type {
	case CustomFieldTypeNumber, CustomFieldTypeCurrency, CustomFieldTypeRating, CustomFieldTypeProgress:
		match = orderedCond(col("value_number"), positive, c.numbers[0])
	case CustomFieldTypeDate:
		match = orderedCond(col("value_date"), positive, c.dates[0])
	case CustomFieldTypeCheckbox:
		// No stored value counts as false, so asking for false is "no true row".
		wantTrue := c.bools[0] == (f.comparator == taskFilterComparatorEquals)
		cond := builder.Exists(customFieldValueExists(c.fieldID, taskAlias, builder.Eq{col("value_bool"): true}))
		if !wantTrue {
			cond = builder.Not{cond}
		}
		return withCustomFieldNulls(cond, c.fieldID, taskAlias, includeNulls), nil
	case CustomFieldTypeText, CustomFieldTypeLongText, CustomFieldTypeURL, CustomFieldTypeEmail:
		if positive == taskFilterComparatorLike {
			match = &builder.Like{col("value_text"), "%" + c.texts[0] + "%"}
		} else {
			match = builder.Eq{col("value_text"): c.texts[0]}
		}
	case CustomFieldTypeSelect, CustomFieldTypeMultiSelect:
		match = builder.In(col("option_id"), c.ids)
	case CustomFieldTypeUser:
		var conds []builder.Cond
		if len(c.ids) > 0 {
			conds = append(conds, builder.In(col("user_id"), c.ids))
		}
		if len(c.names) > 0 {
			conds = append(conds, builder.In(col("user_id"), builder.Select("id").From("users").Where(builder.In("username", c.names))))
		}
		match = builder.Or(conds...)
	default:
		return nil, ErrInvalidTaskField{TaskField: c.identifier()}
	}

	sub := customFieldValueExists(c.fieldID, taskAlias, match)
	cond := builder.Exists(sub)
	if negate {
		cond = builder.NotExists(sub)
	}
	return withCustomFieldNulls(cond, c.fieldID, taskAlias, includeNulls), nil
}

func withCustomFieldNulls(cond builder.Cond, fieldID int64, taskAlias string, includeNulls bool) builder.Cond {
	if !includeNulls {
		return cond
	}
	return builder.Or(cond, builder.NotExists(customFieldValueExists(fieldID, taskAlias, nil)))
}

// resolveCustomFieldSorts loads the field of every cf_<id> sort parameter. Types that hold
// several values per task cannot be sorted by.
func resolveCustomFieldSorts(s *xorm.Session, a web.Auth, sorts []*sortParam) error {
	r := &customFieldReader{s: s, a: a}
	for _, p := range sorts {
		id, ok := parseCustomFieldIdentifier(p.sortBy)
		if !ok {
			continue
		}
		field, err := r.get(id)
		if err != nil {
			return err
		}
		if field.Type == CustomFieldTypeMultiSelect || field.Type == CustomFieldTypeUser {
			return ErrInvalidTaskField{TaskField: p.sortBy}
		}
		p.customField = field
	}
	return nil
}

// customFieldSortExpr is a scalar sub-select returning the sort key of a task's value, NULL
// when it has none. Single-value types have at most one row per task.
func customFieldSortExpr(field *CustomField) string {
	from := customFieldValuesTable + " cfv"
	column := ""
	switch field.Type {
	case CustomFieldTypeNumber, CustomFieldTypeCurrency, CustomFieldTypeRating, CustomFieldTypeProgress:
		column = "cfv.value_number"
	case CustomFieldTypeDate:
		column = "cfv.value_date"
	case CustomFieldTypeCheckbox:
		column = "cfv.value_bool"
	case CustomFieldTypeText, CustomFieldTypeLongText, CustomFieldTypeURL, CustomFieldTypeEmail:
		column = "cfv.value_text"
	case CustomFieldTypeSelect:
		// Options sort by their position in the field, not by their id.
		from += " INNER JOIN custom_field_options cfo ON cfo.id = cfv.option_id"
		column = "cfo.`position`"
	case CustomFieldTypeMultiSelect, CustomFieldTypeUser:
		// Never get here: fields with several values per task are refused as a sort key.
	}
	return "(SELECT " + column + " FROM " + from +
		" WHERE cfv.task_id = tasks.id AND cfv.custom_field_id = " + strconv.FormatInt(field.ID, 10) + ")"
}

// customFieldOrderPart is the ORDER BY part of a cf_<id> sort. Tasks without a value sort
// last in both directions, using the same per-database workarounds as built-in columns.
func customFieldOrderPart(param *sortParam) string {
	expr := customFieldSortExpr(param.customField)
	part := expr + " " + param.orderBy.String()
	if db.Type() == schemas.MYSQL {
		part = expr + " IS NULL, " + part
	}
	if db.Type() == schemas.POSTGRES || db.Type() == schemas.SQLITE {
		part += " NULLS LAST"
	}
	return part
}

// deleteCustomFieldValuesOfProject removes the values a task holds for fields of a project.
// It runs when the task leaves that project.
func deleteCustomFieldValuesOfProject(s *xorm.Session, taskID, projectID int64) error {
	fieldIDs := builder.Select("id").From("custom_fields").Where(builder.Eq{"project_id": projectID})
	_, err := s.Where(builder.And(
		builder.Eq{"task_id": taskID},
		builder.In("custom_field_id", fieldIDs),
	)).Delete(&TaskCustomFieldValue{})
	return err
}
