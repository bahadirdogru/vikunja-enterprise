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

// CanRead checks if the auth can read the project and loads the workflow of it.
func (wf *Workflow) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	can, maxPermission, err := (&Project{ID: wf.ProjectID}).CanRead(s, a)
	if err != nil || !can {
		return false, maxPermission, err
	}
	stored, err := getWorkflowByProject(s, wf.ProjectID)
	if err != nil {
		return false, 0, err
	}
	*wf = *stored
	return true, maxPermission, nil
}

// CanUpdate needs admin access to the project. It also creates a workflow, there is one per project.
func (wf *Workflow) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	return (&Project{ID: wf.ProjectID}).IsAdmin(s, a)
}

// CanDelete needs admin access to the project and an existing workflow.
func (wf *Workflow) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	can, err := wf.CanUpdate(s, a)
	if err != nil || !can {
		return false, err
	}
	_, err = getWorkflowByProject(s, wf.ProjectID)
	return err == nil, err
}

// CanRead checks if the auth can read the task.
func (ts *TaskWorkflowStatus) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	task := &Task{ID: ts.TaskID}
	can, maxPermission, err := task.CanRead(s, a)
	if err != nil || !can {
		return false, maxPermission, err
	}
	ts.task = task
	return true, maxPermission, nil
}

// CanCreate needs write access to the task. The rules of the transition itself are checked when it is performed.
func (c *TaskStatusChange) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	// Status changes of one task run one after the other: with transitions that ask for approvals two parallel
	// changes would otherwise create the same request twice. The write comes before any read, see lockApprovalRequest.
	if err := lockTaskForApproval(s, c.TaskID); err != nil {
		return false, err
	}
	task := &Task{ID: c.TaskID}
	can, err := task.CanWrite(s, a)
	if err != nil || !can {
		return false, err
	}
	stored, err := GetTaskByIDSimple(s, c.TaskID)
	if err != nil {
		return false, err
	}
	c.task = &stored
	return true, nil
}

// CanRead checks if the auth can read the task the history belongs to.
func (h *TaskStatusHistory) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	return (&Task{ID: h.TaskID}).CanRead(s, a)
}
