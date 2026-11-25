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
	"errors"
	"sync"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
	"xorm.io/xorm/schemas"
)

func setUserStatusRaw(t *testing.T, id int64, status user.Status) {
	t.Helper()
	setRaw(t, "users", builder.Eq{"id": id}, map[string]any{"status": int(status)})
}

// newBotOf creates a bot of the owner that can read the project of the approval tests.
func newBotOf(t *testing.T, owner *user.User, name string) *BotUser {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	bot := &BotUser{User: user.User{Username: name}}
	require.NoError(t, bot.Create(s, owner))
	require.NoError(t, s.Commit())
	insertRow(t, &ProjectUser{UserID: bot.ID, ProjectID: apProject, Permission: PermissionRead})
	return bot
}

func TestApprovals_AccountsOfApprovers(t *testing.T) {
	t.Run("disabling a bot drops its slot", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		bot := newBotOf(t, &user.User{ID: 2}, "bot-approver")
		req := createAdHoc(t, env.task.ID, apU1, []int64{bot.ID}, ApprovalModeAny)

		s := db.NewSession()
		status := user.StatusDisabled
		upd := &BotUser{Status: &status, User: user.User{ID: bot.ID, Name: "bot"}}
		require.NoError(t, upd.Update(s, &user.User{ID: 2}))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)
		s.Close()

		requireCancelled(t, req.ID, ApprovalCancelNoApprovers)
	})

	t.Run("disabling the owner of a bot drops the slot of the bot, which stays active", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		bot := newBotOf(t, &user.User{ID: 2}, "bot-of-two")
		req := createAdHoc(t, env.task.ID, apU1, []int64{bot.ID}, ApprovalModeAny)

		disableAccount(t, 2)

		requireCancelled(t, req.ID, ApprovalCancelNoApprovers)
		s := db.NewSession()
		defer s.Close()
		b, err := user.GetUserByID(s, bot.ID)
		require.NoError(t, err)
		assert.Equal(t, user.StatusActive, b.Status)
	})

	t.Run("a bot of a disabled owner is not asked", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		bot := newBotOf(t, &user.User{ID: 2}, "bot-not-asked")
		setUserStatusRaw(t, 2, user.StatusDisabled)
		_, err := tryCreateAdHoc(t, &ApprovalRequest{TaskID: env.task.ID, Title: "x", ApproverIDs: []int64{bot.ID}, Mode: ApprovalModeAny}, apU1)
		require.Error(t, err)
	})

	t.Run("a login lockout does not remove the dissenting approver of an all request", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)

		// Ten bad logins lock user2 out; the colluding approver decides afterwards.
		setUserStatusRaw(t, 2, user.StatusAccountLocked)
		res := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")

		assert.Equal(t, ApprovalStatusPending, res.Status, "the locked approver still has to decide")
		require.NotNil(t, approverOf(res, 2))
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("a disabled approver is still dropped by the next decision when the cleanup has not run", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		setUserStatusRaw(t, 2, user.StatusDisabled)
		res := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, res.Status)
	})

	t.Run("a disabled approver who already decided keeps the decision", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU2, ApprovalDecisionApproved, "fine")

		disableAccount(t, 2)

		stored := readApprovalRow(t, req.ID)
		assert.Equal(t, ApprovalStatusPending, stored.Status)
		require.NotNil(t, approverOf(stored, 2))
		require.NotNil(t, approverOf(stored, 2).Decision)
		assert.Equal(t, ApprovalDecisionApproved, *approverOf(stored, 2).Decision)

		res := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, res.Status)
		assert.Equal(t, env.doing, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("enabling the requester again before the decision lets the transition happen", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		disableAccount(t, 1)
		assert.Equal(t, ApprovalStatusPending, readApprovalRow(t, req.ID).Status, "the requests of a disabled requester stay pending")
		setUserStatusRaw(t, 1, user.StatusActive)

		res := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, res.Status)
		assert.Equal(t, env.doing, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("a requester who is also the disabled approver ends the request as requester_disabled", func(t *testing.T) {
		cfg := &TransitionApproval{Mode: ApprovalModeAll, Approvers: apUsers(1, 6), AllowSelf: true}
		env := newApprovalEnv(t, cfg, nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")

		disableAccount(t, 1)

		requireCancelled(t, req.ID, ApprovalCancelRequesterDisabled)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("a requester who is a bot of a disabled owner does not get the transition", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		bot := newBotOf(t, &user.User{ID: 2}, "bot-requester")
		insertRow(t, &ProjectUser{UserID: bot.ID, ProjectID: apProject, Permission: PermissionWrite})
		req := env.request(t, &user.User{ID: bot.ID, BotOwnerID: 2})
		setUserStatusRaw(t, 2, user.StatusDisabled)

		res := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusCancelled, res.Status)
		assert.Equal(t, ApprovalCancelRequesterDisabled, res.CancelReason)
	})
}

// Two accounts that are disabled (or deleted) at the same time walk their requests in the order of the tasks, not of
// the request ids. User 2 is asked on a request of task B and then of task A, user 3 on a request of task A and then
// of task B: in request id order the first takes the lock of B and then A, the second A and then B, which
// PostgreSQL aborts with 40P01 and MySQL with 1213.
func TestApprovals_DisablingTwoAccountsDoesNotDeadlock(t *testing.T) {
	if db.Type() != schemas.MYSQL && db.Type() != schemas.POSTGRES {
		t.Skip("Requires database row locks")
	}
	env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
	taskA := env.task
	taskB := createWorkflowTask(t, apProject, &Task{Title: "second", ProjectID: apProject})
	require.Less(t, taskA.ID, taskB.ID)

	const rounds = 60
	var failures []error
	for i := 0; i < rounds; i++ {
		createAdHoc(t, taskB.ID, apU1, []int64{2}, ApprovalModeAny)
		createAdHoc(t, taskA.ID, apU1, []int64{2}, ApprovalModeAny)
		createAdHoc(t, taskA.ID, apU1, []int64{3}, ApprovalModeAny)
		createAdHoc(t, taskB.ID, apU1, []int64{3}, ApprovalModeAny)

		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for n, id := range []int64{2, 3} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				s := db.NewSession()
				defer s.Close()
				if err := dropApproversFromPendingApprovals(s, []int64{id}, nil, true); err != nil {
					_ = s.Rollback()
					events.CleanupPending(s)
					errs[n] = err
					return
				}
				errs[n] = s.Commit()
				events.DispatchPending(context.Background(), s)
			}()
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			var httpErr web.HTTPErrorProcessor
			if err != nil && !errors.As(err, &httpErr) {
				failures = append(failures, err)
			}
		}

		s := db.NewSession()
		require.NoError(t, deleteApprovalsForTask(s, taskA.ID))
		require.NoError(t, deleteApprovalsForTask(s, taskB.ID))
		require.NoError(t, s.Commit())
		s.Close()
	}
	require.Empty(t, failures, "%d database errors, first: %v", len(failures), firstErr(failures))
}
