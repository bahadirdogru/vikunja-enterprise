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
	"encoding/json"
	"math"
	"net/mail"
	"net/url"
	"reflect"
	"sort"
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// TaskCustomFieldValue is one stored value row. Single-value types have at most one row per
// (task, field); multi_select and user fields have one row per selected option or user.
type TaskCustomFieldValue struct {
	ID            int64      `xorm:"bigint autoincr not null unique pk" json:"id"`
	TaskID        int64      `xorm:"bigint not null INDEX" json:"task_id"`
	CustomFieldID int64      `xorm:"bigint not null INDEX" json:"custom_field_id"`
	ValueText     *string    `xorm:"text null" json:"value_text"`
	ValueNumber   *float64   `xorm:"double null" json:"value_number"`
	ValueDate     *time.Time `xorm:"DATETIME null" json:"value_date"`
	ValueBool     *bool      `xorm:"bool null" json:"value_bool"`
	OptionID      *int64     `xorm:"bigint null INDEX" json:"option_id"`
	UserID        *int64     `xorm:"bigint null INDEX" json:"user_id"`
	Created       time.Time  `xorm:"created not null" json:"created"`
	Updated       time.Time  `xorm:"updated not null" json:"updated"`
}

// TableName returns the table name of custom field values.
func (*TaskCustomFieldValue) TableName() string {
	return "task_custom_field_values"
}

// CustomFieldValue is the API view of a task's value for one custom field.
type CustomFieldValue struct {
	TaskID        int64           `json:"task_id" readOnly:"true" doc:"The task the value belongs to. Taken from the URL."`
	CustomFieldID int64           `json:"custom_field_id" readOnly:"true" doc:"The custom field the value belongs to. Taken from the URL."`
	Type          CustomFieldType `json:"type" readOnly:"true" doc:"The type of the field, which decides the shape of value."`
	Value         any             `json:"value" doc:"The value, null when unset. A string for text, long_text, url and email, a number for number, currency, rating and progress, an RFC 3339 string for date, a boolean for checkbox, an option id for select, an array of option ids for multi_select and an array of user ids for user. Sending null or, for multi_select and user, an empty array clears the value."`

	task  *Task
	field *CustomField

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

func getCustomFieldValueRows(s *xorm.Session, taskID int64, fieldIDs []int64) (map[int64][]*TaskCustomFieldValue, error) {
	result := make(map[int64][]*TaskCustomFieldValue, len(fieldIDs))
	if len(fieldIDs) == 0 {
		return result, nil
	}
	rows := []*TaskCustomFieldValue{}
	err := s.Where(builder.And(
		builder.Eq{"task_id": taskID},
		builder.In("custom_field_id", fieldIDs),
	)).OrderBy("id ASC").Find(&rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.CustomFieldID] = append(result[r.CustomFieldID], r)
	}
	return result, nil
}

// rowsToValue turns stored rows into the JSON value of the field's type, nil when unset.
func rowsToValue(t CustomFieldType, rows []*TaskCustomFieldValue) any {
	if len(rows) == 0 {
		return nil
	}
	switch t {
	case CustomFieldTypeText, CustomFieldTypeLongText, CustomFieldTypeURL, CustomFieldTypeEmail:
		if rows[0].ValueText == nil {
			return nil
		}
		return *rows[0].ValueText
	case CustomFieldTypeNumber, CustomFieldTypeCurrency, CustomFieldTypeRating, CustomFieldTypeProgress:
		if rows[0].ValueNumber == nil {
			return nil
		}
		return *rows[0].ValueNumber
	case CustomFieldTypeDate:
		if rows[0].ValueDate == nil {
			return nil
		}
		return rows[0].ValueDate.UTC()
	case CustomFieldTypeCheckbox:
		if rows[0].ValueBool == nil {
			return nil
		}
		return *rows[0].ValueBool
	case CustomFieldTypeSelect:
		if rows[0].OptionID == nil {
			return nil
		}
		return *rows[0].OptionID
	case CustomFieldTypeMultiSelect:
		ids := []int64{}
		for _, r := range rows {
			if r.OptionID != nil {
				ids = append(ids, *r.OptionID)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		return ids
	case CustomFieldTypeUser:
		ids := []int64{}
		for _, r := range rows {
			if r.UserID != nil {
				ids = append(ids, *r.UserID)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		return ids
	}
	return nil
}

func invalidValue(f *CustomField, reason string) error {
	return ErrCustomFieldInvalidValue{CustomFieldID: f.ID, Type: f.Type, Reason: reason}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func toInt(v any) (int64, bool) {
	f, ok := toFloat(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return 0, false
	}
	return int64(f), true
}

func toIntSlice(v any) ([]int64, bool) {
	switch list := v.(type) {
	case []int64:
		return list, true
	case []int:
		out := make([]int64, 0, len(list))
		for _, i := range list {
			out = append(out, int64(i))
		}
		return out, true
	case []any:
		out := make([]int64, 0, len(list))
		for _, e := range list {
			i, ok := toInt(e)
			if !ok {
				return nil, false
			}
			out = append(out, i)
		}
		return out, true
	}
	return nil, false
}

func uniqueInts(in []int64) []int64 {
	seen := make(map[int64]bool, len(in))
	out := make([]int64, 0, len(in))
	for _, i := range in {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	return out
}

func isEmptyCustomFieldValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Slice && rv.Len() == 0
}

func validateNumberValue(f *CustomField, v any) (float64, error) {
	n, ok := toFloat(v)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, invalidValue(f, "expected a number")
	}

	switch f.Type { //nolint:exhaustive // only the numeric types get here, the default is number and currency
	case CustomFieldTypeProgress:
		if n < 0 || n > 100 {
			return 0, invalidValue(f, "progress must be between 0 and 100")
		}
	case CustomFieldTypeRating:
		maxRating := float64(defaultCustomFieldMaxRating)
		if f.Config != nil && f.Config.MaxRating != nil {
			maxRating = float64(*f.Config.MaxRating)
		}
		if n != math.Trunc(n) || n < 1 || n > maxRating {
			return 0, invalidValue(f, "rating must be a whole number between 1 and the maximum rating")
		}
	default:
		if f.Config != nil {
			if f.Config.Min != nil && n < *f.Config.Min {
				return 0, invalidValue(f, "value is below the minimum")
			}
			if f.Config.Max != nil && n > *f.Config.Max {
				return 0, invalidValue(f, "value is above the maximum")
			}
		}
	}
	return n, nil
}

func validateURLValue(f *CustomField, v any) (string, error) {
	str, ok := v.(string)
	if !ok {
		return "", invalidValue(f, "expected a string")
	}
	u, err := url.Parse(str)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", invalidValue(f, "expected an http or https url")
	}
	return str, checkCustomFieldTextLength(f, str)
}

func validateEmailValue(f *CustomField, v any) (string, error) {
	str, ok := v.(string)
	if !ok {
		return "", invalidValue(f, "expected a string")
	}
	addr, err := mail.ParseAddress(str)
	if err != nil || addr.Address != str {
		return "", invalidValue(f, "expected an email address")
	}
	return str, checkCustomFieldTextLength(f, str)
}

func validateDateValue(f *CustomField, v any) (time.Time, error) {
	switch d := v.(type) {
	case time.Time:
		return d.UTC(), nil
	case string:
		parsed, err := time.Parse(time.RFC3339, d)
		if err != nil {
			return time.Time{}, invalidValue(f, "expected an RFC 3339 date")
		}
		return parsed.UTC(), nil
	}
	return time.Time{}, invalidValue(f, "expected an RFC 3339 date string")
}

func validateOptionValues(s *xorm.Session, f *CustomField, v any) ([]int64, error) {
	var ids []int64
	if f.Type == CustomFieldTypeSelect {
		id, ok := toInt(v)
		if !ok {
			return nil, invalidValue(f, "expected an option id")
		}
		ids = []int64{id}
	} else {
		list, ok := toIntSlice(v)
		if !ok {
			return nil, invalidValue(f, "expected an array of option ids")
		}
		ids = uniqueInts(list)
	}

	options, err := getCustomFieldOptions(s, []int64{f.ID})
	if err != nil {
		return nil, err
	}
	valid := make(map[int64]bool, len(options[f.ID]))
	for _, o := range options[f.ID] {
		valid[o.ID] = true
	}
	for _, id := range ids {
		if !valid[id] {
			return nil, invalidValue(f, "option does not belong to this field")
		}
	}
	return ids, nil
}

func validateUserValues(s *xorm.Session, f *CustomField, task *Task, v any) ([]int64, error) {
	list, ok := toIntSlice(v)
	if !ok {
		return nil, invalidValue(f, "expected an array of user ids")
	}
	ids := uniqueInts(list)
	for _, id := range ids {
		u, err := user.GetUserByID(s, id)
		if err != nil {
			if user.IsErrUserDoesNotExist(err) || user.IsErrUserStatusError(err) {
				return nil, invalidValue(f, "user does not exist")
			}
			return nil, err
		}
		can, _, err := (&Project{ID: task.ProjectID}).CanRead(s, u)
		if err != nil {
			return nil, err
		}
		if !can {
			return nil, invalidValue(f, "user has no access to the project of the task")
		}
	}
	return ids, nil
}

// buildCustomFieldValueRows validates raw against the field and returns the rows to store.
// A nil result means the value is cleared.
func buildCustomFieldValueRows(s *xorm.Session, f *CustomField, task *Task, raw any) ([]*TaskCustomFieldValue, error) {
	if isEmptyCustomFieldValue(raw) {
		return nil, nil
	}

	row := func() *TaskCustomFieldValue {
		return &TaskCustomFieldValue{
			TaskID:        task.ID,
			CustomFieldID: f.ID,
		}
	}

	switch f.Type {
	case CustomFieldTypeText, CustomFieldTypeLongText:
		str, ok := raw.(string)
		if !ok {
			return nil, invalidValue(f, "expected a string")
		}
		if err := checkCustomFieldTextLength(f, str); err != nil {
			return nil, err
		}
		r := row()
		r.ValueText = &str
		return []*TaskCustomFieldValue{r}, nil

	case CustomFieldTypeURL, CustomFieldTypeEmail:
		var str string
		var err error
		if f.Type == CustomFieldTypeURL {
			str, err = validateURLValue(f, raw)
		} else {
			str, err = validateEmailValue(f, raw)
		}
		if err != nil {
			return nil, err
		}
		r := row()
		r.ValueText = &str
		return []*TaskCustomFieldValue{r}, nil

	case CustomFieldTypeNumber, CustomFieldTypeCurrency, CustomFieldTypeRating, CustomFieldTypeProgress:
		n, err := validateNumberValue(f, raw)
		if err != nil {
			return nil, err
		}
		r := row()
		r.ValueNumber = &n
		return []*TaskCustomFieldValue{r}, nil

	case CustomFieldTypeDate:
		d, err := validateDateValue(f, raw)
		if err != nil {
			return nil, err
		}
		r := row()
		r.ValueDate = &d
		return []*TaskCustomFieldValue{r}, nil

	case CustomFieldTypeCheckbox:
		b, ok := raw.(bool)
		if !ok {
			return nil, invalidValue(f, "expected a boolean")
		}
		r := row()
		r.ValueBool = &b
		return []*TaskCustomFieldValue{r}, nil

	case CustomFieldTypeSelect, CustomFieldTypeMultiSelect:
		ids, err := validateOptionValues(s, f, raw)
		if err != nil {
			return nil, err
		}
		rows := make([]*TaskCustomFieldValue, 0, len(ids))
		for _, id := range ids {
			r := row()
			r.OptionID = new(int64)
			*r.OptionID = id
			rows = append(rows, r)
		}
		return rows, nil

	case CustomFieldTypeUser:
		ids, err := validateUserValues(s, f, task, raw)
		if err != nil {
			return nil, err
		}
		rows := make([]*TaskCustomFieldValue, 0, len(ids))
		for _, id := range ids {
			r := row()
			r.UserID = new(int64)
			*r.UserID = id
			rows = append(rows, r)
		}
		return rows, nil
	}
	return nil, invalidValue(f, "unknown field type")
}

// loadTaskAndField resolves the task and a field of the task's project, once per request.
func (v *CustomFieldValue) loadTaskAndField(s *xorm.Session) error {
	if v.task == nil {
		task, err := GetTaskByIDSimple(s, v.TaskID)
		if err != nil {
			return err
		}
		v.task = &task
	}
	if v.field == nil {
		field, err := getCustomFieldInProject(s, v.CustomFieldID, v.task.ProjectID)
		if err != nil {
			return err
		}
		v.field = field
	}
	return nil
}

// ReadAll returns one entry per field of the task's project, unset fields with a null value.
// @Summary List the custom field values of a task
func (v *CustomFieldValue) ReadAll(s *xorm.Session, a web.Auth, _ string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	// DoReadAll skips the Can* checks.
	task := &Task{ID: v.TaskID}
	can, _, err := task.CanRead(s, a)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	total, err := s.Where("project_id = ?", task.ProjectID).Count(&CustomField{})
	if err != nil {
		return nil, 0, 0, err
	}

	fields := []*CustomField{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where("project_id = ?", task.ProjectID).OrderBy("position ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&fields); err != nil {
		return nil, 0, 0, err
	}

	fieldIDs := make([]int64, 0, len(fields))
	for _, f := range fields {
		fieldIDs = append(fieldIDs, f.ID)
	}
	rows, err := getCustomFieldValueRows(s, task.ID, fieldIDs)
	if err != nil {
		return nil, 0, 0, err
	}

	entries := make([]*CustomFieldValue, 0, len(fields))
	for _, f := range fields {
		entries = append(entries, &CustomFieldValue{
			TaskID:        task.ID,
			CustomFieldID: f.ID,
			Type:          f.Type,
			Value:         rowsToValue(f.Type, rows[f.ID]),
		})
	}
	return entries, len(entries), total, nil
}

// Update sets the value of the field on the task. A null or empty value clears it.
// @Summary Set a custom field value
func (v *CustomFieldValue) Update(s *xorm.Session, a web.Auth) error {
	if err := v.loadTaskAndField(s); err != nil {
		return err
	}
	return v.store(s, a, v.Value)
}

// Delete clears the value of the field on the task.
// @Summary Clear a custom field value
func (v *CustomFieldValue) Delete(s *xorm.Session, a web.Auth) error {
	if err := v.loadTaskAndField(s); err != nil {
		return err
	}
	return v.store(s, a, nil)
}

func (v *CustomFieldValue) store(s *xorm.Session, a web.Auth, raw any) error {
	// The checks before read the task without a lock. Holding its row from here on makes writers of one task wait
	// for each other, so two of them cannot both find no row and store one each for a single-value field, and a
	// task that was moved in the meantime does not get a value for a field of the project it left.
	locked, err := GetTaskSimple(lockingSession(s), &Task{ID: v.task.ID})
	if err != nil {
		return err
	}
	if locked.ProjectID != v.field.ProjectID {
		return ErrCustomFieldDoesNotExist{CustomFieldID: v.field.ID}
	}

	newRows, err := buildCustomFieldValueRows(s, v.field, v.task, raw)
	if err != nil {
		return err
	}

	oldRowsByField, err := getCustomFieldValueRows(s, v.task.ID, []int64{v.field.ID})
	if err != nil {
		return err
	}
	oldValue := rowsToValue(v.field.Type, oldRowsByField[v.field.ID])
	newValue := rowsToValue(v.field.Type, newRows)

	v.Type = v.field.Type
	v.Value = newValue
	if reflect.DeepEqual(oldValue, newValue) {
		return nil
	}

	_, err = s.Where(builder.And(
		builder.Eq{"task_id": v.task.ID},
		builder.Eq{"custom_field_id": v.field.ID},
	)).Delete(&TaskCustomFieldValue{})
	if err != nil {
		return err
	}
	if len(newRows) > 0 {
		if _, err = s.Insert(&newRows); err != nil {
			return err
		}
	}

	if err = updateTaskLastUpdated(s, &Task{ID: v.task.ID}); err != nil {
		return err
	}

	events.DispatchOnCommit(s, &TaskCustomFieldValueUpdatedEvent{
		Task:        v.task,
		CustomField: v.field,
		OldValue:    oldValue,
		NewValue:    newValue,
		Doer:        doerFromAuth(s, a),
	})
	return nil
}

// duplicateCustomFieldsForProject copies the definitions of one project into another and
// returns the old to new field id map plus the old to new option id map.
func duplicateCustomFieldsForProject(s *xorm.Session, sourceProjectID, targetProjectID int64) (fieldMap, optionMap map[int64]int64, err error) {
	fieldMap = map[int64]int64{}
	optionMap = map[int64]int64{}

	fields := []*CustomField{}
	if err = s.Where("project_id = ?", sourceProjectID).OrderBy("position ASC, id ASC").Find(&fields); err != nil {
		return nil, nil, err
	}
	if len(fields) == 0 {
		return fieldMap, optionMap, nil
	}

	ids := make([]int64, 0, len(fields))
	for _, f := range fields {
		ids = append(ids, f.ID)
	}
	options, err := getCustomFieldOptions(s, ids)
	if err != nil {
		return nil, nil, err
	}

	for _, f := range fields {
		oldID := f.ID
		f.ID = 0
		f.ProjectID = targetProjectID
		if _, err = s.Insert(f); err != nil {
			return nil, nil, err
		}
		fieldMap[oldID] = f.ID

		for _, o := range options[oldID] {
			oldOptionID := o.ID
			o.ID = 0
			o.CustomFieldID = f.ID
			if _, err = s.Insert(o); err != nil {
				return nil, nil, err
			}
			optionMap[oldOptionID] = o.ID
		}
	}
	return fieldMap, optionMap, nil
}

// copyCustomFieldValues copies the values of tasks into their copies. fieldMap and optionMap
// translate the ids when the copy lives in another project; nil maps keep them (same project).
// User values are only copied for users who can read the target project.
func copyCustomFieldValues(s *xorm.Session, taskMap map[int64]int64, targetProjectID int64, fieldMap, optionMap map[int64]int64) error {
	if len(taskMap) == 0 {
		return nil
	}
	oldTaskIDs := make([]int64, 0, len(taskMap))
	for id := range taskMap {
		oldTaskIDs = append(oldTaskIDs, id)
	}
	sort.Slice(oldTaskIDs, func(i, j int) bool { return oldTaskIDs[i] < oldTaskIDs[j] })

	rows := []*TaskCustomFieldValue{}
	if err := s.Where(builder.In("task_id", oldTaskIDs)).OrderBy("id ASC").Find(&rows); err != nil {
		return err
	}

	userAccess := map[int64]bool{}
	copies := make([]*TaskCustomFieldValue, 0, len(rows))
	for _, r := range rows {
		if fieldMap != nil {
			newField, ok := fieldMap[r.CustomFieldID]
			if !ok {
				continue
			}
			r.CustomFieldID = newField
		}
		if r.OptionID != nil && optionMap != nil {
			newOption, ok := optionMap[*r.OptionID]
			if !ok {
				continue
			}
			r.OptionID = &newOption
		}
		if r.UserID != nil {
			has, seen := userAccess[*r.UserID]
			if !seen {
				u, err := user.GetUserByID(s, *r.UserID)
				if err != nil {
					if user.IsErrUserDoesNotExist(err) || user.IsErrUserStatusError(err) {
						userAccess[*r.UserID] = false
						continue
					}
					return err
				}
				has, _, err = (&Project{ID: targetProjectID}).CanRead(s, u)
				if err != nil {
					return err
				}
				userAccess[*r.UserID] = has
			}
			if !has {
				continue
			}
		}
		r.ID = 0
		r.TaskID = taskMap[r.TaskID]
		copies = append(copies, r)
	}

	if len(copies) == 0 {
		return nil
	}
	_, err := s.Insert(&copies)
	return err
}
