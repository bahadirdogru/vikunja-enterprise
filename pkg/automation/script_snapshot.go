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
	"strconv"

	"code.vikunja.io/api/pkg/models"

	"xorm.io/xorm"
)

// taskSnapshot is the task as a script sees it: plain values named like the API. It returns nil for a subject
// without a task.
func taskSnapshot(s *xorm.Session, sub *Subject) (map[string]any, error) {
	if sub == nil || sub.Task == nil {
		return nil, nil
	}
	f := sub.Fields
	snap := map[string]any{
		"id":           sub.Task.ID,
		"title":        f["title"],
		"description":  f["description"],
		"done":         f["done"],
		"priority":     f["priority"],
		"percent_done": f["percent_done"],
		"due_date":     f["due_date"],
		"start_date":   f["start_date"],
		"end_date":     f["end_date"],
		"done_at":      f["done_at"],
		"created":      f["created"],
		"updated":      f["updated"],
		"hex_color":    f["hex_color"],
		"identifier":   f["identifier"],
		"project_id":   f["project_id"],
		"created_by":   f["created_by"],
		"assignees":    f["assignees"],
		"labels":       f["labels"],
		"status":       nil,
	}
	status, err := models.GetTaskWorkflowStatus(s, sub.Task.ID)
	if err != nil {
		return nil, err
	}
	if status != nil {
		snap["status"] = map[string]any{"id": status.ID, "title": status.Title, "category": string(status.Category)}
	}
	relations := []*models.TaskRelation{}
	if err = s.Where("task_id = ?", sub.Task.ID).OrderBy("other_task_id ASC").Find(&relations); err != nil {
		return nil, err
	}
	related := map[string]any{}
	for _, rel := range relations {
		kind := string(rel.RelationKind)
		list, _ := related[kind].([]any)
		related[kind] = append(list, rel.OtherTaskID)
	}
	snap["relations"] = related
	custom := map[string]any{}
	for id := range sub.CFTypes {
		custom[strconv.FormatInt(id, 10)] = f[cfKey(id)]
	}
	snap["custom_fields"] = custom
	return snap, nil
}

// eventMap is the trigger event as a script sees it.
func eventMap(p *Payload) map[string]any {
	if p == nil {
		return map[string]any{}
	}
	m := map[string]any{
		"name":       p.Name,
		"task_id":    p.TaskID,
		"project_id": p.ProjectID,
		"doer":       nil,
		"old":        orMap(p.Old),
		"new":        orMap(p.New),
		"changed":    stringsToAny(p.Changed),
		"field_id":   p.FieldID,
	}
	if p.Doer != nil {
		m["doer"] = map[string]any{"id": p.Doer.ID, "username": p.Doer.Username, "name": p.Doer.Name}
	}
	return m
}

func orMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func stringsToAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
