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

// Disabling an account must never be vetoed by what happens to its approval requests: it is the thing an admin does
// to a compromised account. The status change commits on its own; the cleanup of the approver slots follows in a
// transaction of its own (events.OnCommit runs it synchronously right after the commit, in the web request, in
// the CLI and in the tests) and its failure is logged. Until it has run the slots still exist, which is safe: a
// disabled account cannot decide, and the next decision on the request drops it (dropApproversWithoutAccess).
//
// The seam is the transactional event hook, so no upstream code calls into the approvals: the admin routes and the
// CLI (ChangeUserStatus) dispatch AdminUserStatusChangedEvent, the owner of a bot disabling the bot dispatches
// BotStatusChangedEvent.
func init() {
	events.RegisterTransactionalHook(approvalsAccountHook)
}

func approvalsAccountHook(s *xorm.Session, event events.Event) error {
	switch e := event.(type) {
	case *AdminUserStatusChangedEvent:
		if e.User != nil && e.NewStatus == user.StatusDisabled {
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
