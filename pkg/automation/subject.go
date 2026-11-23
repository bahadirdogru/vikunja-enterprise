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

package automation

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// ErrTaskGone is returned when the task of an event does not exist any more.
var ErrTaskGone = errors.New("the task does not exist any more")

// Subject is the state conditions and templates are evaluated against: the task as it is right now plus the
// payload of the event. Values are canonical: numbers are float64, dates time.Time (UTC), lists []any.
type Subject struct {
	Task    *models.Task
	Project *models.Project
	// Fields holds the task fields by their catalog name, including status, status_category, assignees, labels
	// and cf_<id>.
	Fields map[string]any
	// CFTypes holds the type of every custom field of the project by id.
	CFTypes map[int64]models.CustomFieldType
	// CFOptions holds the option titles of select fields.
	CFOptions map[int64]map[int64]string
	Event     *Payload
	Now       time.Time
	CreatedBy int64
	// Gone is set when the task does not exist any more and Fields come from the event payload.
	Gone bool
	// Script evaluates the source of a script condition. It is set by whoever owns the session the conditions are
	// evaluated in (the runner, the dry run); a subject without it cannot evaluate script conditions.
	Script func(source string) (bool, error)
}

// canon converts any stored or decoded value to the canonical form.
func canon(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case int:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case float32:
		return float64(val)
	case float64, bool, string:
		return val
	case time.Time:
		if val.IsZero() {
			return nil
		}
		return val.UTC()
	case []int64:
		out := make([]any, len(val))
		for i, id := range val {
			out[i] = float64(id)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = canon(item)
		}
		return out
	}
	return fmt.Sprint(v)
}

func idsToList(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = float64(id)
	}
	return out
}

func cfKey(id int64) string { return "cf_" + strconv.FormatInt(id, 10) }

// LoadSubject loads the current state of the task of an event. The session must see committed data.
func LoadSubject(s *xorm.Session, taskID int64, ev *Payload, now time.Time) (*Subject, error) {
	sub := &Subject{
		Fields:    map[string]any{},
		CFTypes:   map[int64]models.CustomFieldType{},
		CFOptions: map[int64]map[int64]string{},
		Event:     ev,
		Now:       now,
	}
	if taskID == 0 {
		// A run without a task still knows its project (templates, the create task action).
		if ev != nil && ev.ProjectID != 0 {
			if project, err := models.GetProjectSimpleByID(s, ev.ProjectID); err == nil {
				sub.Project = project
			}
		}
		return sub, nil
	}

	task, err := models.GetTaskByIDSimple(s, taskID)
	if err != nil {
		if models.IsErrTaskDoesNotExist(err) {
			// Only a deletion has a task that is allowed to be gone: the payload remembers how it was.
			if ev != nil && ev.Task != nil && ev.Name == "task.deleted" {
				sub.Gone = true
				for k, v := range ev.Task {
					sub.Fields[k] = canon(v)
				}
				for _, name := range []string{"due_date", "start_date", "end_date"} {
					if str, ok := sub.Fields[name].(string); ok {
						if t, err := time.Parse(time.RFC3339, str); err == nil {
							sub.Fields[name] = t.UTC()
						}
					}
				}
				return sub, nil
			}
			return nil, ErrTaskGone
		}
		return nil, err
	}
	sub.Task = &task
	sub.CreatedBy = task.CreatedByID

	project, err := models.GetProjectSimpleByID(s, task.ProjectID)
	if err != nil {
		return nil, err
	}
	sub.Project = project

	identifier := "#" + strconv.FormatInt(task.Index, 10)
	if project.Identifier != "" {
		identifier = project.Identifier + "-" + strconv.FormatInt(task.Index, 10)
	}

	f := sub.Fields
	f["title"] = task.Title
	f["description"] = task.Description
	f["done"] = task.Done
	f["priority"] = canon(task.Priority)
	f["percent_done"] = canon(task.PercentDone)
	f["due_date"] = canon(task.DueDate)
	f["start_date"] = canon(task.StartDate)
	f["end_date"] = canon(task.EndDate)
	f["done_at"] = canon(task.DoneAt)
	f["created"] = canon(task.Created)
	f["updated"] = canon(task.Updated)
	f["hex_color"] = task.HexColor
	f["identifier"] = identifier
	f["project_id"] = canon(task.ProjectID)
	f["created_by"] = canon(task.CreatedByID)

	assignees := []int64{}
	if err = s.Table("task_assignees").Where("task_id = ?", taskID).OrderBy("id ASC").Cols("user_id").Find(&assignees); err != nil {
		return nil, err
	}
	f["assignees"] = idsToList(assignees)
	labels := []int64{}
	if err = s.Table("label_tasks").Where("task_id = ?", taskID).OrderBy("id ASC").Cols("label_id").Find(&labels); err != nil {
		return nil, err
	}
	f["labels"] = idsToList(labels)

	status, err := models.GetTaskWorkflowStatus(s, taskID)
	if err != nil {
		return nil, err
	}
	f["status"], f["status_category"] = nil, nil
	if status != nil {
		f["status"] = canon(status.ID)
		f["status_category"] = string(status.Category)
	}

	values, err := models.GetCustomFieldValuesOfTask(s, &task)
	if err != nil {
		return nil, err
	}
	selectFields := []int64{}
	for _, v := range values {
		sub.CFTypes[v.CustomFieldID] = v.Type
		f[cfKey(v.CustomFieldID)] = canon(v.Value)
		if v.Type == models.CustomFieldTypeSelect || v.Type == models.CustomFieldTypeMultiSelect {
			selectFields = append(selectFields, v.CustomFieldID)
		}
	}
	if len(selectFields) > 0 {
		options := []*models.CustomFieldOption{}
		if err = s.Where(builder.In("custom_field_id", selectFields)).Find(&options); err != nil {
			return nil, err
		}
		for _, o := range options {
			if sub.CFOptions[o.CustomFieldID] == nil {
				sub.CFOptions[o.CustomFieldID] = map[int64]string{}
			}
			sub.CFOptions[o.CustomFieldID][o.ID] = o.Title
		}
	}
	return sub, nil
}

// payloadValue walks a dotted path through the decoded JSON body of a webhook. Numbers are indexes into lists.
func payloadValue(root map[string]any, path string) any {
	var cur any = root
	for _, key := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[key]
			if !ok {
				return nil
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(node) {
				return nil
			}
			cur = node[i]
		default:
			return nil
		}
	}
	return cur
}

// DoerID is the id of the user who caused the event, 0 when unknown.
func (sub *Subject) DoerID() int64 {
	if sub.Event != nil && sub.Event.Doer != nil {
		return sub.Event.Doer.ID
	}
	return 0
}

// fieldKind returns the comparison kind of a condition field.
func (sub *Subject) fieldKind(name string) string {
	if id, ok := spec.CustomFieldID(name); ok {
		switch sub.CFTypes[id] {
		case models.CustomFieldTypeNumber, models.CustomFieldTypeCurrency, models.CustomFieldTypeRating,
			models.CustomFieldTypeProgress, models.CustomFieldTypeSelect, models.CustomFieldTypeMultiSelect,
			models.CustomFieldTypeUser:
			return spec.KindNumber
		case models.CustomFieldTypeDate:
			return spec.KindDate
		case models.CustomFieldTypeCheckbox:
			return spec.KindBool
		case models.CustomFieldTypeText, models.CustomFieldTypeLongText, models.CustomFieldTypeURL, models.CustomFieldTypeEmail:
			return spec.KindString
		case "":
			return spec.KindAny
		}
		return spec.KindString
	}
	if def := spec.FieldByName(name); def != nil {
		switch def.Kind {
		case spec.KindUsers, spec.KindUser, spec.KindLabel, spec.KindStatus, spec.KindProject:
			return spec.KindNumber
		case spec.KindText:
			return spec.KindString
		case spec.KindEnum:
			return spec.KindString
		}
		return def.Kind
	}
	return spec.KindAny
}

// Lookup returns the value of a condition field. ok is false when the field is unknown.
func (sub *Subject) Lookup(name string) (value any, ok bool) {
	switch {
	case name == "event.doer.id":
		if id := sub.DoerID(); id != 0 {
			return float64(id), true
		}
		return nil, true
	case name == "event.doer.username":
		if sub.Event != nil && sub.Event.Doer != nil {
			return sub.Event.Doer.Username, true
		}
		return nil, true
	case name == "event.doer.name":
		if sub.Event != nil && sub.Event.Doer != nil {
			return sub.Event.Doer.Name, true
		}
		return nil, true
	case spec.IsPayloadField(name):
		if sub.Event == nil {
			return nil, true
		}
		return canon(payloadValue(sub.Event.Hook, strings.TrimPrefix(name, "event.payload."))), true
	case strings.HasPrefix(name, "event.old."), strings.HasPrefix(name, "event.new."):
		if sub.Event == nil {
			return nil, true
		}
		values := sub.Event.Old
		if strings.HasPrefix(name, "event.new.") {
			values = sub.Event.New
		}
		key := name[len("event.old."):]
		v := canon(values[key])
		if def := spec.FieldByName(name); def != nil && def.Kind == spec.KindDate {
			if str, isStr := v.(string); isStr {
				if t, err := time.Parse(time.RFC3339, str); err == nil {
					v = t.UTC()
				}
			}
		}
		return v, true
	}
	if _, isCF := spec.CustomFieldID(name); isCF {
		return sub.Fields[name], true
	}
	if spec.FieldByName(name) != nil {
		return sub.Fields[name], true
	}
	return nil, false
}
