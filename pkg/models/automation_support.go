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
	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

// UpdateFields updates only the given columns of a task (and the relation fields assignees, reminders and
// is_favorite), keeping everything else as stored. It is what a PATCH of a single field boils down to, without
// reading the whole task first. The caller checks CanUpdate.
func (t *Task) UpdateFields(s *xorm.Session, a web.Auth, fields []string) error {
	return t.updateSingleTask(s, a, fields)
}

// GetTaskWorkflowStatus returns the current workflow status of a task, nil when the task has none.
func GetTaskWorkflowStatus(s *xorm.Session, taskID int64) (*WorkflowStatus, error) {
	row, err := getTaskStatusRow(s, taskID)
	if err != nil || row == nil {
		return nil, err
	}
	status := &WorkflowStatus{}
	has, err := s.ID(row.StatusID).Get(status)
	if err != nil || !has {
		return nil, err
	}
	return status, nil
}

// GetCustomFieldValuesOfTask returns one entry per custom field of the task's project, unset fields with a nil
// value. It does not check permissions.
func GetCustomFieldValuesOfTask(s *xorm.Session, task *Task) ([]*CustomFieldValue, error) {
	fields := []*CustomField{}
	if err := s.Where("project_id = ?", task.ProjectID).OrderBy("position ASC, id ASC").Find(&fields); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(fields))
	for _, f := range fields {
		ids = append(ids, f.ID)
	}
	rows, err := getCustomFieldValueRows(s, task.ID, ids)
	if err != nil {
		return nil, err
	}
	values := make([]*CustomFieldValue, 0, len(fields))
	for _, f := range fields {
		values = append(values, &CustomFieldValue{
			TaskID:        task.ID,
			CustomFieldID: f.ID,
			Type:          f.Type,
			Value:         rowsToValue(f.Type, rows[f.ID]),
		})
	}
	return values, nil
}

// CanAdminProject reports whether a user may administer a project (project admin or instance admin).
func CanAdminProject(s *xorm.Session, a web.Auth, projectID int64) (bool, error) {
	return (&Project{ID: projectID}).IsAdmin(s, a)
}

// DisableAutomationRule disables a rule and stores why. It returns false when the rule was disabled already.
func DisableAutomationRule(s *xorm.Session, ruleID int64, reason string) (bool, error) {
	rule, err := GetAutomationRule(s, ruleID)
	if err != nil {
		return false, err
	}
	if !rule.Enabled {
		return false, nil
	}
	_, err = s.ID(ruleID).Cols("enabled", "disabled_reason", "revision").Update(&AutomationRule{
		Enabled:        false,
		DisabledReason: reason,
		Revision:       rule.Revision + 1,
	})
	if err != nil {
		return false, err
	}
	invalidateAutomationRules(s, rule.ProjectID)
	return true, nil
}

// Validate checks that the value fits the type of the custom field and the task, without writing anything.
func (v *CustomFieldValue) Validate(s *xorm.Session) error {
	if err := v.loadTaskAndField(s); err != nil {
		return err
	}
	_, err := buildCustomFieldValueRows(s, v.field, v.task, v.Value)
	return err
}

// Check runs every check of the status change - the target exists and is not archived, the workflow allows the
// pair, the actor may perform it, required fields and comment - without changing anything.
func (c *TaskStatusChange) Check(s *xorm.Session, a web.Auth) error {
	t := c.task
	d, err := loadWorkflowDef(s, t.ProjectID)
	if err != nil {
		return err
	}
	if d == nil {
		return ErrWorkflowDoesNotExist{ProjectID: t.ProjectID}
	}
	to, exists := d.statuses[c.ToStatusID]
	if !exists {
		return ErrWorkflowStatusDoesNotExist{StatusID: c.ToStatusID}
	}
	if to.Archived {
		return ErrWorkflowStatusArchived{StatusID: to.ID}
	}
	current, err := getTaskStatusRow(s, t.ID)
	if err != nil {
		return err
	}
	var from *WorkflowStatus
	if current != nil {
		from = d.statuses[current.StatusID]
		if current.StatusID == to.ID {
			return nil
		}
	}
	return d.checkTransition(s, a, t, from, to, c.Comment)
}
