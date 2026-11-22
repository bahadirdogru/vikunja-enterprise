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

// canAdministerForms is the gate for the admin side of forms: admin access to the project, never for link shares.
func canAdministerForms(s *xorm.Session, a web.Auth, projectID int64) (bool, int, error) {
	if _, err := ownerOf(a); err != nil {
		return false, 0, nil
	}
	can, err := (&Project{ID: projectID}).IsAdmin(s, a)
	if err != nil || !can {
		return false, 0, err
	}
	return true, int(PermissionAdmin), nil
}

// CanRead needs admin access to the project and loads the form.
func (f *Form) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	can, maxPermission, err := canAdministerForms(s, a, f.ProjectID)
	if err != nil || !can {
		return false, 0, err
	}
	stored, err := getFormInProject(s, f.ID, f.ProjectID)
	if err != nil {
		return false, 0, err
	}
	*f = *stored
	return true, maxPermission, nil
}

// CanCreate needs admin access to the project.
func (f *Form) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	can, _, err := canAdministerForms(s, a, f.ProjectID)
	return can, err
}

// CanUpdate needs admin access to the project and a form that is part of it.
func (f *Form) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	can, _, err := canAdministerForms(s, a, f.ProjectID)
	if err != nil || !can {
		return false, err
	}
	_, err = getFormInProject(s, f.ID, f.ProjectID)
	return err == nil, err
}

// CanDelete needs admin access to the project and a form that is part of it.
func (f *Form) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	return f.CanUpdate(s, a)
}

// CanCreate (regenerating the token) needs admin access to the project and a form that is part of it.
func (t *FormToken) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	return (&Form{ID: t.ID, ProjectID: t.ProjectID}).CanUpdate(s, a)
}

// CanRead needs admin access to the project and a form that is part of it.
func (sub *FormSubmission) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	can, maxPermission, err := canAdministerForms(s, a, sub.ProjectID)
	if err != nil || !can {
		return false, 0, err
	}
	_, err = getFormInProject(s, sub.FormID, sub.ProjectID)
	return err == nil, maxPermission, err
}

// CanRead needs admin access to the project.
func (o *FormOptions) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	return canAdministerForms(s, a, o.ProjectID)
}
