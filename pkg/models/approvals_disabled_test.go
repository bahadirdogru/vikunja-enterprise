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
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// disableAccount disables an account the way the admin routes and the CLI do.
func disableAccount(t *testing.T, id int64) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	target, err := GetUserAsAdmin(s, id)
	require.NoError(t, err)
	require.NoError(t, ChangeUserStatus(s, nil, target, user.StatusDisabled))
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
}

func TestApprovals_DisabledAccounts(t *testing.T) {
	t.Run("an approved transition is not performed for a disabled requester", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		setRaw(t, "users", builder.Eq{"id": 1}, map[string]any{"status": int(user.StatusDisabled)})

		res := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")

		assert.Equal(t, ApprovalStatusCancelled, res.Status)
		assert.Equal(t, "requester_disabled", res.CancelReason)
		stored := requireCancelled(t, req.ID, "requester_disabled")
		require.NotNil(t, approverOf(stored, 6))
		assert.Equal(t, ApprovalDecisionApproved, *approverOf(stored, 6).Decision, "the decision stays recorded")
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID, "the task did not move")
	})

	t.Run("a request whose only approver is disabled is cancelled", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), nil)
		req := env.request(t, apU1)

		disableAccount(t, 2)

		requireCancelled(t, req.ID, ApprovalCancelNoApprovers)
		db.AssertCount(t, "approval_approvers", builder.Eq{"request_id": req.ID}, 0)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("a disabled approver is dropped and the others can still resolve", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")

		disableAccount(t, 2)

		stored := readApprovalRow(t, req.ID)
		assert.Equal(t, ApprovalStatusApproved, stored.Status, "everybody who is left approved")
		assert.Nil(t, approverOf(stored, 2))
		assert.Equal(t, env.doing, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("a request of a disabled requester whose last approver is dropped is not performed", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		setRaw(t, "users", builder.Eq{"id": 1}, map[string]any{"status": int(user.StatusDisabled)})

		disableAccount(t, 2)

		requireCancelled(t, req.ID, "requester_disabled")
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("a disabled approver cannot decide", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		req := env.request(t, apU1)
		setRaw(t, "users", builder.Eq{"id": 2}, map[string]any{"status": int(user.StatusDisabled)})

		_, err := decideAs(t, req.ID, apU2, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.Equal(t, ApprovalStatusPending, readApprovalRow(t, req.ID).Status)
	})
}
