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
	"context"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/user"

	"xorm.io/xorm"
)

// Deactivating an account must never be vetoed by what happens to its approval requests: it is the thing an admin
// does to a compromised account. The status change commits on its own; the cleanup of the approver slots follows in a
// transaction of its own (events.OnCommit runs it synchronously right after the commit, in the web request, in the
// CLI and in the tests) and its failure is logged. If the callback fails or the process ends before it ran, the
// periodic repair (repairApprovalApprovers, in the approval cron) finds the requests again.
//
// The seam is the transactional event hook, so no upstream code calls into the approvals: the admin routes and the
// CLI (ChangeUserStatus) dispatch AdminUserStatusChangedEvent, the owner of a bot disabling the bot dispatches
// BotStatusChangedEvent. The first event is only ever made by an admin: the lockout after failed logins does not
// dispatch it, so every status other than active in it is an admin's decision (disabled, waiting for the e-mail
// address to be confirmed, locked), and the slot goes. A lockout by failed logins alone never drops a slot, see
// usersWhoMayReadProjectWhere. Whether an account in the status "locked" was locked by an admin or by failed logins
// is not stored, so only the hook can tell: when it was missed, the repair keeps the slot of a locked account.
func init() {
	events.RegisterTransactionalHook(approvalsAccountHook)
}

func approvalsAccountHook(s *xorm.Session, event events.Event) error {
	switch e := event.(type) {
	case *AdminUserStatusChangedEvent:
		if e.User != nil && e.NewStatus != user.StatusActive {
			id := e.User.ID
			// The bots of a disabled owner stay active but cannot authenticate any more: their slots go too.
			events.OnCommit(s, func() error { return cleanupApprovalsOfDisabledAccount(id, true) })
		}
	case *BotStatusChangedEvent:
		if e.Bot != nil && e.NewStatus == user.StatusDisabled {
			id := e.Bot.ID
			events.OnCommit(s, func() error { return cleanupApprovalsOfDisabledAccount(id, false) })
		}
	}
	return nil
}

// cleanupApprovalsOfDisabledAccount drops the account, and with withBots the bots it owns, from the undecided
// slots of the pending requests, in a transaction of its own.
func cleanupApprovalsOfDisabledAccount(userID int64, withBots bool) (err error) {
	s := db.NewSession()
	defer s.Close()
	defer func() {
		if err != nil {
			_ = s.Rollback()
			events.CleanupPending(s)
			log.Errorf("Could not remove the disabled account %d from its approvals: %s", userID, err)
		}
	}()

	ids := []int64{userID}
	if withBots {
		bots := []int64{}
		if err = s.Table("users").Where("bot_owner_id = ?", userID).Cols("id").Find(&bots); err != nil {
			return err
		}
		ids = append(ids, bots...)
	}
	if err = dropApproversFromPendingApprovals(s, ids, nil, true); err != nil {
		return err
	}
	if err = s.Commit(); err != nil {
		return err
	}
	events.DispatchPending(context.Background(), s)
	return nil
}

// approvalRepairBatch is the number of requests one run of the repair looks at.
const approvalRepairBatch = 100

// repairApprovalApprovers finds pending requests that still wait for an account that cannot decide any more
// (disabled, waiting for its e-mail address to be confirmed, a bot whose owner is one of those) and drops the slot,
// like the cleanup after the status change does. It is the repair for a cleanup that failed or never ran: without it
// a request whose only approver was deactivated stays pending, because nobody is left who could decide and so drop
// it. A locked account keeps its slot (see approvalsAccountHook). It is safe to run on several instances at once:
// each request is handled in a transaction of its own that locks it first, and a second run finds nothing to do.
func repairApprovalApprovers() {
	ids := []int64{}
	s := db.NewReadSession()
	err := s.SQL(`SELECT r.id FROM approval_requests r
WHERE r.status = ? AND EXISTS (
	SELECT 1 FROM approval_approvers a
	JOIN users u ON u.id = a.user_id
	LEFT JOIN users o ON o.id = u.bot_owner_id
	WHERE a.request_id = r.id AND a.decision IS NULL
	AND (u.status IN (?, ?) OR (u.bot_owner_id > 0 AND o.status IN (?, ?))))
ORDER BY r.task_id ASC, r.id ASC LIMIT ?`,
		ApprovalStatusPending,
		user.StatusDisabled, user.StatusEmailConfirmationRequired, user.StatusDisabled, user.StatusEmailConfirmationRequired,
		approvalRepairBatch).Find(&ids)
	_ = s.Close()
	if err != nil {
		log.Errorf("[Approval Repair] Could not look for requests with approvers who cannot decide: %s", err)
		return
	}
	for _, id := range ids {
		if err := repairApprovalRequest(id); err != nil {
			log.Errorf("[Approval Repair] Could not repair request %d: %s", id, err)
		}
	}
}

func repairApprovalRequest(id int64) (err error) {
	s := db.NewSession()
	defer s.Close()
	defer func() {
		if err != nil {
			_ = s.Rollback()
			events.CleanupPending(s)
		}
	}()
	if err = lockApprovalRequest(s, id); err != nil {
		return err
	}
	req, err := getApprovalRequest(s, id, true)
	if IsErrApprovalRequestDoesNotExist(err) {
		return s.Commit()
	}
	if err != nil {
		return err
	}
	if req.Status != ApprovalStatusPending {
		return s.Commit()
	}
	if _, err = dropApproversWithoutAccess(s, req); err != nil {
		return err
	}
	if err = reevaluateApproval(s, req); err != nil {
		return err
	}
	if err = s.Commit(); err != nil {
		return err
	}
	events.DispatchPending(context.Background(), s)
	return nil
}
