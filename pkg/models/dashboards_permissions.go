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
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

// canWriteDashboard: a personal dashboard is changed by its owner only, a project dashboard by project admins. Link
// shares never change dashboards.
func canWriteDashboard(s *xorm.Session, a web.Auth, stored *Dashboard) (bool, error) {
	u, is := a.(*user.User)
	if !is || u.ID <= 0 {
		return false, nil
	}
	if stored.ProjectID == 0 {
		return stored.OwnerID == u.ID, nil
	}
	return (&Project{ID: stored.ProjectID}).IsAdmin(s, a)
}

// CanRead loads the dashboard with its widgets. Personal dashboards are visible to their owner only, project dashboards
// to everyone who can read the project (link shares of that project included).
func (d *Dashboard) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	stored, err := loadDashboard(s, d.ID)
	if err != nil {
		return false, 0, err
	}

	if stored.ProjectID == 0 {
		u, is := a.(*user.User)
		if !is || u.ID != stored.OwnerID {
			return false, 0, nil
		}
		*d = *stored
		return true, int(PermissionAdmin), nil
	}

	can, maxPermission, err := (&Project{ID: stored.ProjectID}).CanRead(s, a)
	if err != nil || !can {
		return false, 0, err
	}
	// Changing it needs admin access, so that is what the client may offer.
	if _, isShare := a.(*LinkSharing); !isShare {
		if admin, err := canWriteDashboard(s, a, stored); err != nil {
			return false, 0, err
		} else if admin {
			maxPermission = int(PermissionAdmin)
		}
	}
	*d = *stored
	return true, maxPermission, nil
}

// CanCreate needs admin access to the project of a project dashboard, any user may create personal ones.
func (d *Dashboard) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	if _, err := ownerOf(a); err != nil {
		return false, nil //nolint:nilerr // anything but a user is refused, which is an answer and not an error
	}
	if d.ProjectID == 0 {
		return true, nil
	}
	return (&Project{ID: d.ProjectID}).IsAdmin(s, a)
}

// CanUpdate checks the stored dashboard, not the project id of the request.
func (d *Dashboard) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	stored, err := getDashboardByID(s, d.ID)
	if err != nil {
		return false, err
	}
	return canWriteDashboard(s, a, stored)
}

// CanDelete is the same as CanUpdate.
func (d *Dashboard) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	return d.CanUpdate(s, a)
}

// CanUpdate lets whoever may change the dashboard change its layout.
func (l *DashboardLayout) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	return (&Dashboard{ID: l.DashboardID}).CanUpdate(s, a)
}
