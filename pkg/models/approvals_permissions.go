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

// Approvals are invisible to link shares: they cannot read, request, decide or cancel anything.

// CanRead checks that the auth can read the task of the request and loads the request row.
func (r *ApprovalRequest) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	if isLinkShareAuth(a) {
		return false, 0, nil
	}
	req, err := getApprovalRequest(s, r.ID, false)
	if err != nil {
		return false, 0, err
	}
	can, maxPermission, err := (&Task{ID: req.TaskID}).CanRead(s, a)
	if IsErrTaskDoesNotExist(err) {
		return false, 0, ErrApprovalRequestDoesNotExist{ID: r.ID}
	}
	if err != nil || !can {
		return false, maxPermission, err
	}
	*r = *req
	return true, maxPermission, nil
}

// CanCreate needs write access to the task, for ad-hoc requests.
func (r *ApprovalRequest) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	if isLinkShareAuth(a) {
		return false, nil
	}
	return (&Task{ID: r.TaskID}).CanWrite(s, a)
}

// loadRequestForAction locks the request first and then reads it with a locking read. It reports the request as
// missing for users who cannot read its task.
func loadRequestForAction(s *xorm.Session, a web.Auth, id int64) (*ApprovalRequest, error) {
	if err := lockApprovalRequest(s, id); err != nil {
		return nil, err
	}
	req, err := getApprovalRequest(s, id, true)
	if err != nil {
		return nil, err
	}
	can, _, err := (&Task{ID: req.TaskID}).CanRead(s, a)
	if IsErrTaskDoesNotExist(err) {
		return nil, ErrApprovalRequestDoesNotExist{ID: id}
	}
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, ErrApprovalRequestDoesNotExist{ID: id}
	}
	return req, nil
}

// CanCreate lets everyone who can read the task try to decide; the approver check is made when deciding, so
// the client gets a typed error.
func (d *ApprovalDecision) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	if isLinkShareAuth(a) {
		return false, nil
	}
	req, err := loadRequestForAction(s, a, d.RequestID)
	if err != nil {
		return false, err
	}
	d.req = req
	return true, nil
}

// CanCreate lets the requester and project admins cancel a request.
func (c *ApprovalCancel) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	if isLinkShareAuth(a) {
		return false, nil
	}
	req, err := loadRequestForAction(s, a, c.RequestID)
	if err != nil {
		return false, err
	}
	if req.RequestedByID != a.GetID() {
		isAdmin, err := (&Project{ID: req.ProjectID}).IsAdmin(s, a)
		if err != nil || !isAdmin {
			return false, err
		}
	}
	c.req = req
	return true, nil
}
