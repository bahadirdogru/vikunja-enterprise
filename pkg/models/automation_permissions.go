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

// canAdministerAutomation is the gate for everything about automation: admin access to the project. Rules and runs
// can reveal data of other users (conditions, comments created for them, errors), so reading needs it too.
func canAdministerAutomation(s *xorm.Session, a web.Auth, projectID int64) (bool, int, error) {
	if _, err := ownerOf(a); err != nil {
		return false, 0, nil //nolint:nilerr // anything but a user is refused, which is an answer and not an error
	}
	can, err := (&Project{ID: projectID}).IsAdmin(s, a)
	if IsErrProjectDoesNotExist(err) {
		// The same refusal as for a project the caller cannot administer: it must not tell what exists.
		return false, 0, nil
	}
	if err != nil || !can {
		return false, 0, err
	}
	return true, int(PermissionAdmin), nil
}

// CanRead needs admin access to the project and loads the rule.
func (r *AutomationRule) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	can, maxPermission, err := canAdministerAutomation(s, a, r.ProjectID)
	if err != nil || !can {
		return false, 0, err
	}
	stored, err := getAutomationRuleInProject(s, r.ID, r.ProjectID)
	if err != nil {
		return false, 0, err
	}
	*r = *stored
	return true, maxPermission, nil
}

// CanCreate needs admin access to the project.
func (r *AutomationRule) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	can, _, err := canAdministerAutomation(s, a, r.ProjectID)
	return can, err
}

// CanUpdate needs admin access to the project and a rule that is part of it.
func (r *AutomationRule) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	can, _, err := canAdministerAutomation(s, a, r.ProjectID)
	if err != nil || !can {
		return false, err
	}
	_, err = getAutomationRuleInProject(s, r.ID, r.ProjectID)
	return err == nil, err
}

// CanDelete needs admin access to the project and a rule that is part of it.
func (r *AutomationRule) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	return r.CanUpdate(s, a)
}

// CanRead needs admin access to the project and loads the run.
func (r *AutomationRun) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	can, maxPermission, err := canAdministerAutomation(s, a, r.ProjectID)
	if err != nil || !can {
		return false, 0, err
	}
	stored, err := getAutomationRunOfRule(s, r.ID, r.RuleID, r.ProjectID)
	if err != nil {
		return false, 0, err
	}
	*r = *stored
	return true, maxPermission, nil
}

// CanCreate needs admin access to the project and a run of a rule of it.
func (rr *AutomationRunRetry) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	can, _, err := canAdministerAutomation(s, a, rr.ProjectID)
	if err != nil || !can {
		return false, err
	}
	_, err = getAutomationRunOfRule(s, rr.RunID, rr.RuleID, rr.ProjectID)
	return err == nil, err
}
