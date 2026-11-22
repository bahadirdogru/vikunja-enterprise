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
	"sort"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// TaskCustomFieldSummary is the compact form of a custom field value that is embedded in tasks
// when the `custom_fields` expand option is set.
type TaskCustomFieldSummary struct {
	CustomFieldID int64           `json:"custom_field_id" doc:"The custom field the value belongs to."`
	Type          CustomFieldType `json:"type" doc:"The type of the field, which decides the shape of value."`
	Value         any             `json:"value" doc:"The value in the same shape as the per-task custom field values endpoint."`
}

// addCustomFieldsToTasks attaches the custom field values to the given tasks with two queries
// (value rows and field definitions), regardless of the number of tasks. Only fields with a value
// are included, ordered by the position of the field.
func addCustomFieldsToTasks(s *xorm.Session, taskIDs []int64, taskMap map[int64]*Task) error {
	if len(taskIDs) == 0 {
		return nil
	}

	rows := []*TaskCustomFieldValue{}
	if err := s.Where(builder.In("task_id", taskIDs)).OrderBy("id ASC").Find(&rows); err != nil {
		return err
	}

	fieldIDSet := make(map[int64]struct{})
	for _, r := range rows {
		fieldIDSet[r.CustomFieldID] = struct{}{}
	}
	fieldIDs := make([]int64, 0, len(fieldIDSet))
	for id := range fieldIDSet {
		fieldIDs = append(fieldIDs, id)
	}

	fields := make(map[int64]*CustomField, len(fieldIDs))
	if len(fieldIDs) > 0 {
		list := []*CustomField{}
		if err := s.Where(builder.In("id", fieldIDs)).Find(&list); err != nil {
			return err
		}
		for _, f := range list {
			fields[f.ID] = f
		}
	}

	grouped := make(map[int64]map[int64][]*TaskCustomFieldValue)
	for _, r := range rows {
		if _, has := fields[r.CustomFieldID]; !has {
			continue
		}
		if grouped[r.TaskID] == nil {
			grouped[r.TaskID] = make(map[int64][]*TaskCustomFieldValue)
		}
		grouped[r.TaskID][r.CustomFieldID] = append(grouped[r.TaskID][r.CustomFieldID], r)
	}

	for _, taskID := range taskIDs {
		task, has := taskMap[taskID]
		if !has {
			continue
		}
		summaries := []*TaskCustomFieldSummary{}
		for fieldID, fieldRows := range grouped[taskID] {
			f := fields[fieldID]
			value := rowsToValue(f.Type, fieldRows)
			if value == nil {
				continue
			}
			summaries = append(summaries, &TaskCustomFieldSummary{CustomFieldID: fieldID, Type: f.Type, Value: value})
		}
		sort.Slice(summaries, func(i, j int) bool {
			fi, fj := fields[summaries[i].CustomFieldID], fields[summaries[j].CustomFieldID]
			if fi.Position != fj.Position {
				return fi.Position < fj.Position
			}
			return fi.ID < fj.ID
		})
		task.CustomFields = summaries
	}
	return nil
}
