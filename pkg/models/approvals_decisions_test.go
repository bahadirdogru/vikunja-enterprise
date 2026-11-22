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
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func TestApprovals_Decisions(t *testing.T) {
	t.Run("mode any: the first approval performs the transition on behalf of the requester", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		req := env.request(t, apU1)
		events.ClearDispatchedEvents()

		resolved := mustDecide(t, req.ID, apU2, ApprovalDecisionApproved, "looks good")
		assert.Equal(t, ApprovalStatusApproved, resolved.Status)
		require.NotNil(t, resolved.Resolved)
		assert.EqualValues(t, 2, resolved.ResolvedByID)
		require.NotNil(t, resolved.ResolvedBy)
		assert.EqualValues(t, 2, resolved.ResolvedBy.ID)

		assert.Equal(t, env.doing, taskStatusOf(t, env.task.ID).StatusID)
		history := taskHistoryOf(t, env.task.ID)
		require.Len(t, history, 2)
		last := history[1]
		assert.EqualValues(t, 1, last.DoerID, "the history names the requester, the system acted for them")
		assert.Equal(t, "please", last.Comment, "the comment of the request is what the history keeps")
		require.NotNil(t, last.ApprovalRequestID)
		assert.Equal(t, req.ID, *last.ApprovalRequestID)

		mine := approverOf(resolved, 2)
		require.NotNil(t, mine)
		require.NotNil(t, mine.Decision)
		assert.Equal(t, ApprovalDecisionApproved, *mine.Decision)
		assert.Equal(t, "looks good", mine.Comment)
		require.NotNil(t, mine.Decided)
		assert.Nil(t, approverOf(resolved, 6).Decision, "the other approver was not needed")

		assert.Len(t, events.GetDispatchedEvents("approval.decided"), 1)
		resolvedEvents := events.GetDispatchedEvents("approval.resolved")
		require.Len(t, resolvedEvents, 1)
		ev, ok := resolvedEvents[0].(*ApprovalResolvedEvent)
		require.True(t, ok)
		assert.Equal(t, ApprovalStatusApproved, ev.Outcome)
		assert.EqualValues(t, 2, ev.Doer.ID)
		assert.Len(t, events.GetDispatchedEvents("task.status.changed"), 1)
	})

	t.Run("the approval also marks the task done when the target is a done status", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions = []*WorkflowTransition{
				{FromStatusID: -1, ToStatusID: -3, RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6)},
			}
		})
		res, err := changeTaskStatusAs(t, env.task.ID, env.done, "", apU1)
		require.NoError(t, err)
		require.NotNil(t, res.Approval)
		assert.False(t, taskDone(t, env.task.ID))
		mustDecide(t, res.Approval.ID, apU6, ApprovalDecisionApproved, "")
		assert.True(t, taskDone(t, env.task.ID))
		assert.Equal(t, env.done, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("mode any: the first rejection rejects and keeps the task where it is", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		req := env.request(t, apU1)
		resolved := mustDecide(t, req.ID, apU2, ApprovalDecisionRejected, "not now")
		assert.Equal(t, ApprovalStatusRejected, resolved.Status)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
		assert.Len(t, taskHistoryOf(t, env.task.ID), 1)
	})

	t.Run("a rejection adds the comment to the task as a comment of the approver", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU3Approver(t, env, req), ApprovalDecisionRejected, "needs <b>work</b>\nsecond line")
		s := db.NewSession()
		defer s.Close()
		comments := []*TaskComment{}
		require.NoError(t, s.Where("task_id = ?", env.task.ID).Find(&comments))
		require.Len(t, comments, 1)
		assert.Equal(t, "<p>needs &lt;b&gt;work&lt;/b&gt;<br>second line</p>", comments[0].Comment)
		assert.EqualValues(t, 2, comments[0].AuthorID)
	})

	t.Run("a rejection moves the task to the rejected status of the transition", func(t *testing.T) {
		cfg := apCfg(ApprovalModeAny, 6, 2)
		cfg.RejectedStatusID = -5
		env := newApprovalEnv(t, cfg, nil)
		stored := env.transition.Approval
		assert.Equal(t, env.rejected, stored.RejectedStatusID, "the temporary id of the document was mapped")

		req := env.request(t, apU1)
		assert.Equal(t, env.rejected, *readApprovalRow(t, req.ID).RejectedStatusID)
		resolved := mustDecide(t, req.ID, apU6, ApprovalDecisionRejected, "no")
		assert.Equal(t, ApprovalStatusRejected, resolved.Status)
		assert.Equal(t, env.rejected, taskStatusOf(t, env.task.ID).StatusID)
		history := taskHistoryOf(t, env.task.ID)
		require.Len(t, history, 2)
		assert.EqualValues(t, 6, history[1].DoerID, "the rejecting approver is the doer of the move")
		assert.Equal(t, "no", history[1].Comment)
		require.NotNil(t, history[1].ApprovalRequestID)
		assert.Equal(t, req.ID, *history[1].ApprovalRequestID)
	})

	t.Run("a rejection needs a comment and leaves everything as it was without one", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		_, err := decideAs(t, req.ID, apU6, ApprovalDecisionRejected, "   ")
		require.Error(t, err)
		assert.True(t, IsErrApprovalCommentRequired(err))
		assert.EqualValues(t, ErrCodeApprovalCommentRequired, err.(ErrApprovalCommentRequired).HTTPError().Code)
		stored := readApprovalRow(t, req.ID)
		assert.Equal(t, ApprovalStatusPending, stored.Status)
		assert.Nil(t, approverOf(stored, 6).Decision)
	})

	t.Run("an invalid decision value is refused", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		_, err := decideAs(t, req.ID, apU6, "maybe", "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalInvalid(err))
	})

	t.Run("mode all: every approver has to approve, partial decisions stay pending", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		events.ClearDispatchedEvents()

		partial := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusPending, partial.Status)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
		assert.Empty(t, events.GetDispatchedEvents("task.status.changed"))

		done := mustDecide(t, req.ID, apU2, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, done.Status)
		assert.Equal(t, env.doing, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("mode all: one rejection rejects while others have not decided", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		resolved := mustDecide(t, req.ID, apU2, ApprovalDecisionRejected, "no")
		assert.Equal(t, ApprovalStatusRejected, resolved.Status)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)

		env = newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req = env.request(t, apU1)
		resolved = mustDecide(t, req.ID, apU2, ApprovalDecisionRejected, "no")
		assert.Equal(t, ApprovalStatusRejected, resolved.Status, "the first decision may be the rejection")
		assert.Nil(t, approverOf(resolved, 6).Decision)
	})

	t.Run("who may decide", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)

		// User3 can read the task but is not an approver.
		_, err := decideAs(t, req.ID, apU3, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalNotApprover(err), "got %T", err)
		// User4 cannot read the task: the request does not exist for them.
		_, err = decideAs(t, req.ID, apU4, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequestDoesNotExist(err), "got %T", err)
		// The requester is not an approver.
		_, err = decideAs(t, req.ID, apU1, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalNotApprover(err))
		// Link shares have no access at all.
		_, err = decideAs(t, req.ID, &LinkSharing{ID: 1, ProjectID: apProject, Permission: PermissionWrite}, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.True(t, IsErrGenericForbidden(err))

		// Nothing above wrote a decision.
		stored := readApprovalRow(t, req.ID)
		for _, ap := range stored.Approvers {
			assert.Nil(t, ap.Decision)
		}

		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		_, err = decideAs(t, req.ID, apU6, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalAlreadyDecided(err))

		mustDecide(t, req.ID, apU2, ApprovalDecisionApproved, "")
		_, err = decideAs(t, req.ID, apU2, ApprovalDecisionApproved, "")
		require.Error(t, err)
		notPending, ok := err.(ErrApprovalNotPending)
		require.True(t, ok, "got %T", err)
		assert.Equal(t, ApprovalStatusApproved, notPending.Status)
		assert.Equal(t, 409, notPending.HTTPError().HTTPCode)
	})

	t.Run("self-approval", func(t *testing.T) {
		env := newApprovalEnv(t, &TransitionApproval{Approvers: apUsers(1), AllowSelf: true}, nil)
		req := env.request(t, apU1)
		resolved := mustDecide(t, req.ID, apU1, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, resolved.Status, "the transition allows it")

		// An approver row of the requester on a request that forbids it is refused when deciding.
		env = newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req = env.request(t, apU1)
		insertRow(t, &ApprovalApprover{RequestID: req.ID, UserID: 1})
		_, err := decideAs(t, req.ID, apU1, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalSelfApproval(err))
	})
}

// apU3Approver returns user2, an approver of the request.
func apU3Approver(t *testing.T, env *approvalEnv, req *ApprovalRequest) *user.User {
	t.Helper()
	return apU2
}

func TestApprovals_ApproveRechecksTheTransition(t *testing.T) {
	// Each case changes something behind the back of the hooks, which would cancel the request on their own,
	// and checks what the decision finds.
	check := func(t *testing.T, env *approvalEnv, req *ApprovalRequest, reason string) *ApprovalRequest {
		t.Helper()
		events.ClearDispatchedEvents()
		resolved := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusCancelled, resolved.Status)
		assert.Equal(t, reason, resolved.CancelReason)
		assert.NotEmpty(t, resolved.CancelMessage)
		approved := approverOf(resolved, 6)
		require.NotNil(t, approved.Decision, "the decision stands")
		assert.Equal(t, ApprovalDecisionApproved, *approved.Decision)
		assert.Len(t, taskHistoryOf(t, env.task.ID), 1, "the transition did not happen")
		resolvedEvents := events.GetDispatchedEvents("approval.resolved")
		require.Len(t, resolvedEvents, 1)
		assert.Equal(t, ApprovalStatusCancelled, resolvedEvents[0].(*ApprovalResolvedEvent).Outcome)
		return resolved
	}

	t.Run("the task left the status of the request", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		setRaw(t, "task_statuses", builder.Eq{"task_id": env.task.ID}, map[string]any{"status_id": env.canceled})
		resolved := check(t, env, req, ApprovalCancelStatusChanged)
		assert.Equal(t, env.canceled, taskStatusOf(t, env.task.ID).StatusID)
		assert.Equal(t, ApprovalStatusCancelled, resolved.Status)
	})

	t.Run("the target status was archived", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		setRaw(t, "workflow_statuses", builder.Eq{"id": env.doing}, map[string]any{"archived": true})
		check(t, env, req, ApprovalCancelTargetArchived)
	})

	t.Run("the target status is gone", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		setRaw(t, "workflow_statuses", builder.Eq{"id": env.doing}, map[string]any{"workflow_id": 999})
		check(t, env, req, ApprovalCancelStatusRemoved)
	})

	t.Run("the transition is gone", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		setRaw(t, "workflow_transitions", builder.Eq{"id": env.transition.ID}, map[string]any{"workflow_id": 999})
		check(t, env, req, ApprovalCancelTransitionRemoved)
	})

	t.Run("the workflow is gone", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		setRaw(t, "workflows", builder.Eq{"id": env.wf.ID}, map[string]any{"project_id": 999})
		check(t, env, req, ApprovalCancelWorkflowRemoved)
	})

	t.Run("a required field is not set any more", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions[0].RequiredFields = []string{WorkflowFieldDueDate}
		})
		setTaskColumns(t, env.task.ID, &Task{DueDate: time.Now().Add(time.Hour)}, "due_date")
		req := env.request(t, apU1)
		setRaw(t, "tasks", builder.Eq{"id": env.task.ID}, map[string]any{"due_date": nil})
		resolved := check(t, env, req, ApprovalCancelRequirementsNotMet)
		assert.Contains(t, resolved.CancelMessage, WorkflowFieldDueDate)
	})

	t.Run("the requester lost write access", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		setRaw(t, "users_projects", builder.And(builder.Eq{"user_id": 1}, builder.Eq{"project_id": apProject}), map[string]any{"permission": int(PermissionRead)})
		check(t, env, req, ApprovalCancelRequesterNoAccess)
	})

	t.Run("the requester left the team the transition needs", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions[0].TeamIDs = []int64{9}
		})
		req := env.request(t, apU2)
		s := db.NewSession()
		_, err := s.Where("team_id = ? AND user_id = ?", 9, 2).Delete(&TeamMember{})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		check(t, env, req, ApprovalCancelRequesterNotInTeam)
	})

	t.Run("an approval still works when the rules did not change", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions[0].RequireComment = true
		})
		req := env.request(t, apU1)
		resolved := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, resolved.Status, "the comment of the request satisfies the comment requirement")
	})
}

func TestApprovals_ApproversLosingAccess(t *testing.T) {
	t.Run("an approver without access is dropped and no longer counts for mode all", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		s := db.NewSession()
		_, err := s.Where("user_id = ? AND project_id = ?", 2, apProject).Delete(&ProjectUser{})
		require.NoError(t, err)
		_, err = s.Where("project_id = ?", apProject).And("team_id = ?", 1).Delete(&TeamProject{})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()

		resolved := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, resolved.Status)
		assert.Nil(t, approverOf(resolved, 2), "the slot is gone")
		assert.Equal(t, env.doing, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("the removed approver can no longer decide", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		req := env.request(t, apU1)
		setRaw(t, "users_projects", builder.And(builder.Eq{"user_id": 2}, builder.Eq{"project_id": apProject}), map[string]any{"project_id": 999})
		s := db.NewSession()
		_, err := s.Where("project_id = ?", apProject).And("team_id = ?", 1).Delete(&TeamProject{})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()

		_, err = decideAs(t, req.ID, apU2, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequestDoesNotExist(err), "they cannot see the task any more: %T", err)
	})

	t.Run("losing project access drops the slot at once and re-evaluates the request", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")

		s := db.NewSession()
		_, err := s.Where("user_id = ? AND project_id = ?", 2, apProject).Delete(&ProjectUser{})
		require.NoError(t, err)
		_, err = s.Where("project_id = ?", apProject).And("team_id = ?", 1).Delete(&TeamProject{})
		require.NoError(t, err)
		require.NoError(t, cleanupAfterProjectAccessLoss(s, 2, []int64{apProject}))
		require.NoError(t, s.Commit())
		s.Close()

		stored := readApprovalRow(t, req.ID)
		assert.Equal(t, ApprovalStatusApproved, stored.Status, "everybody who is left approved")
		assert.Equal(t, env.doing, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("nobody left cancels the request", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), nil)
		req := env.request(t, apU1)
		s := db.NewSession()
		_, err := s.Where("user_id = ? AND project_id = ?", 2, apProject).Delete(&ProjectUser{})
		require.NoError(t, err)
		_, err = s.Where("project_id = ?", apProject).And("team_id = ?", 1).Delete(&TeamProject{})
		require.NoError(t, err)
		require.NoError(t, cleanupAfterProjectAccessLoss(s, 2, []int64{apProject}))
		require.NoError(t, s.Commit())
		s.Close()

		stored := readApprovalRow(t, req.ID)
		assert.Equal(t, ApprovalStatusCancelled, stored.Status)
		assert.Equal(t, ApprovalCancelNoApprovers, stored.CancelReason)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
	})
}
