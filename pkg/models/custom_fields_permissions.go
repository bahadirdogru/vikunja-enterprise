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

// CanRead checks if the auth can read the project the field belongs to.
func (cf *CustomField) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	can, maxPermission, err := (&Project{ID: cf.ProjectID}).CanRead(s, a)
	if err != nil || !can {
		return false, maxPermission, err
	}

	field, err := getCustomFieldInProject(s, cf.ID, cf.ProjectID)
	if err != nil {
		return false, 0, err
	}
	*cf = *field
	return true, maxPermission, nil
}

// canAdminProject is the shared gate for changing definitions: admin on the project.
func (cf *CustomField) canAdminProject(s *xorm.Session, a web.Auth) (bool, error) {
	return (&Project{ID: cf.ProjectID}).IsAdmin(s, a)
}

// CanCreate needs admin access to the project.
func (cf *CustomField) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	return cf.canAdminProject(s, a)
}

// CanUpdate needs admin access to the project and a field that is part of it.
func (cf *CustomField) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	can, err := cf.canAdminProject(s, a)
	if err != nil || !can {
		return false, err
	}
	_, err = getCustomFieldInProject(s, cf.ID, cf.ProjectID)
	return err == nil, err
}

// CanDelete needs admin access to the project and a field that is part of it.
func (cf *CustomField) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	return cf.CanUpdate(s, a)
}

// CanRead checks if the auth can read the task the values belong to.
func (v *CustomFieldValue) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	return (&Task{ID: v.TaskID}).CanRead(s, a)
}

// CanUpdate needs write access to the task and a field of the task's project.
func (v *CustomFieldValue) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	can, err := (&Task{ID: v.TaskID}).CanWrite(s, a)
	if err != nil || !can {
		return false, err
	}
	// Only now that the auth may write: a missing field is not leaked to others.
	if err = v.loadTaskAndField(s); err != nil {
		return false, err
	}
	return true, nil
}

// CanDelete needs the same access as setting a value.
func (v *CustomFieldValue) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	return v.CanUpdate(s, a)
}
