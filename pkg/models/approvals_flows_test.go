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

func requireCancelled(t *testing.T, id int64, reason string) *ApprovalRequest {
	t.Helper()
	req := readApprovalRow(t, id)
	assert.Equal(t, ApprovalStatusCancelled, req.Status)
	assert.Equal(t, reason, req.CancelReason)
	assert.NotEmpty(t, req.CancelMessage)
	require.NotNil(t, req.Resolved)
	return req
}

func TestApprovals_Cancel(t *testing.T) {
	t.Run("the requester cancels", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		events.ClearDispatchedEvents()
		res, err := cancelAs(t, req.ID, apU1, "wrong task")
		require.NoError(t, err)
		assert.Equal(t, ApprovalStatusCancelled, res.Request.Status)
		stored := requireCancelled(t, req.ID, ApprovalCancelByRequester)
		assert.Equal(t, "wrong task", stored.CancelMessage)
		assert.EqualValues(t, 1, stored.ResolvedByID)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)

		resolvedEvents := events.GetDispatchedEvents("approval.resolved")
		require.Len(t, resolvedEvents, 1)
		assert.Equal(t, ApprovalStatusCancelled, resolvedEvents[0].(*ApprovalResolvedEvent).Outcome)
		assert.EqualValues(t, 1, resolvedEvents[0].(*ApprovalResolvedEvent).Doer.ID)
	})

	t.Run("a project admin cancels", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		_, err := cancelAs(t, req.ID, apU6, "")
		require.NoError(t, err)
		stored := requireCancelled(t, req.ID, ApprovalCancelByRequester)
		assert.Equal(t, approvalCancelMessages[ApprovalCancelByRequester], stored.CancelMessage)
	})

	t.Run("approvers, other writers and link shares may not", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		req := env.request(t, apU1)
		for _, a := range []user.User{*apU2, *apU3} {
			a := a
			_, err := cancelAs(t, req.ID, &a, "")
			require.Error(t, err)
			assert.True(t, IsErrGenericForbidden(err))
		}
		_, err := cancelAs(t, req.ID, apU4, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequestDoesNotExist(err))
		_, err = cancelAs(t, req.ID, &LinkSharing{ID: 1, ProjectID: apProject, Permission: PermissionAdmin}, "")
		require.Error(t, err)
		assert.True(t, IsErrGenericForbidden(err))
		assert.Equal(t, ApprovalStatusPending, readApprovalRow(t, req.ID).Status)
	})

	t.Run("a resolved request cannot be cancelled and a cancelled one cannot be decided", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		_, err := cancelAs(t, req.ID, apU1, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalNotPending(err))

		env = newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req = env.request(t, apU1)
		_, err = cancelAs(t, req.ID, apU1, "")
		require.NoError(t, err)
		_, err = decideAs(t, req.ID, apU6, ApprovalDecisionApproved, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalNotPending(err))
		_, err = cancelAs(t, req.ID, apU1, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalNotPending(err))
	})
}

func TestApprovals_AutomaticCancellation(t *testing.T) {
	t.Run("the task is deleted", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		adhoc := createAdHoc(t, env.task.ID, apU1, []int64{6}, ApprovalModeAny)
		s := db.NewSession()
		require.NoError(t, (&Task{ID: env.task.ID}).Delete(s, apU1))
		require.NoError(t, s.Commit())
		s.Close()
		requireCancelled(t, req.ID, ApprovalCancelTaskDeleted)
		requireCancelled(t, adhoc.ID, ApprovalCancelTaskDeleted)
	})

	t.Run("the task is moved to another project", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		adhoc := createAdHoc(t, env.task.ID, apU1, []int64{6}, ApprovalModeAny)
		// User1 owns project 1.
		require.NoError(t, updateTaskAs(t, &Task{ID: env.task.ID, Title: "needs a sign-off", ProjectID: 1}, apU1))
		requireCancelled(t, req.ID, ApprovalCancelTaskMoved)
		requireCancelled(t, adhoc.ID, ApprovalCancelTaskMoved)
	})

	t.Run("the task leaves the status of the request by another route", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		adhoc := createAdHoc(t, env.task.ID, apU1, []int64{6}, ApprovalModeAny)
		_, err := changeTaskStatusAs(t, env.task.ID, env.canceled, "", apU1)
		require.NoError(t, err)
		requireCancelled(t, req.ID, ApprovalCancelStatusChanged)
		assert.Equal(t, ApprovalStatusPending, readApprovalRow(t, adhoc.ID).Status, "ad-hoc requests do not depend on the status")
	})

	t.Run("the transition is removed from the workflow", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		doc := readWorkflow(t, apProject)
		doc.Transitions = nil
		mustSaveWorkflow(t, doc, apU6)
		requireCancelled(t, req.ID, ApprovalCancelTransitionRemoved)
	})

	t.Run("the transition no longer needs an approval", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		doc := readWorkflow(t, apProject)
		doc.Transitions[0].RequiresApproval = false
		saved := mustSaveWorkflow(t, doc, apU6)
		assert.Nil(t, saved.Transitions[0].Approval, "the configuration of a transition without approval is dropped")
		requireCancelled(t, req.ID, ApprovalCancelApprovalDisabled)
	})

	t.Run("a status of the request is removed", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		doc := readWorkflow(t, apProject)
		kept := []*WorkflowStatus{}
		for _, st := range doc.Statuses {
			if st.ID != env.doing {
				kept = append(kept, st)
			}
		}
		doc.Statuses = kept
		doc.Transitions = nil
		mustSaveWorkflow(t, doc, apU6)
		requireCancelled(t, req.ID, ApprovalCancelStatusRemoved)
	})

	t.Run("the workflow is deleted, ad-hoc requests stay", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		adhoc := createAdHoc(t, env.task.ID, apU1, []int64{6}, ApprovalModeAny)
		s := db.NewSession()
		require.NoError(t, (&Workflow{ProjectID: apProject}).Delete(s, apU6))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)
		s.Close()
		requireCancelled(t, req.ID, ApprovalCancelWorkflowRemoved)
		assert.Equal(t, ApprovalStatusPending, readApprovalRow(t, adhoc.ID).Status)
	})

	t.Run("an approved request is never touched again", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		_, err := changeTaskStatusAs(t, env.task.ID, env.canceled, "", apU1)
		require.NoError(t, err)
		assert.Equal(t, ApprovalStatusApproved, readApprovalRow(t, req.ID).Status)
	})
}

func createAdHoc(t *testing.T, taskID int64, a *user.User, approvers []int64, mode ApprovalMode) *ApprovalRequest {
	t.Helper()
	req, err := tryCreateAdHoc(t, &ApprovalRequest{TaskID: taskID, Title: "Check this", ApproverIDs: approvers, Mode: mode}, a)
	require.NoError(t, err)
	return req
}

func tryCreateAdHoc(t *testing.T, req *ApprovalRequest, a *user.User) (*ApprovalRequest, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	can, err := req.CanCreate(s, a)
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, err
	}
	if !can {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, ErrGenericForbidden{}
	}
	if err = req.Create(s, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, err
	}
	require.NoError(t, events.PendingHookError(s))
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	return req, nil
}

func TestApprovals_Callers(t *testing.T) {
	doneNeedsApproval := func(t *testing.T) *approvalEnv {
		t.Helper()
		return newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions = []*WorkflowTransition{
				{FromStatusID: -1, ToStatusID: -3, RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6)},
				{FromStatusID: -3, ToStatusID: -1, RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6)},
			}
		})
	}

	t.Run("a plain done update is refused with a typed conflict and creates no request", func(t *testing.T) {
		env := doneNeedsApproval(t)
		err := updateTaskAs(t, &Task{ID: env.task.ID, Title: env.task.Title, Done: true}, apU1)
		require.Error(t, err)
		required, ok := err.(ErrApprovalRequired)
		require.True(t, ok, "got %T", err)
		assert.Equal(t, env.task.ID, required.TaskID)
		assert.Equal(t, env.todo, required.FromStatusID)
		assert.Equal(t, env.done, required.ToStatusID)
		assert.True(t, required.Requestable, "the task is where it was, the approval can be requested")
		assert.Equal(t, 409, required.HTTPError().HTTPCode)
		assert.Equal(t, ErrCodeApprovalRequired, required.HTTPError().Code)

		assert.False(t, taskDone(t, env.task.ID), "the update was rolled back as a whole")
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
		assert.Empty(t, approvalRowsOfTask(t, env.task.ID), "a failed update must not create a request")
	})

	t.Run("the refusal says which status needs the approval and whether a request is already open", func(t *testing.T) {
		env := doneNeedsApproval(t)

		err := updateTaskAs(t, &Task{ID: env.task.ID, Title: env.task.Title, Done: true}, apU1)
		require.Error(t, err)
		required, ok := err.(ErrApprovalRequired)
		require.True(t, ok, "got %T", err)
		details, _ := required.HTTPError().Details.(map[string]any)
		assert.Equal(t, env.done, details["to_status_id"])
		assert.NotEmpty(t, details["to_status_title"], "the client names the status it asks for")
		assert.Equal(t, "done", details["to_status_category"])
		assert.NotEmpty(t, details["from_status_title"])
		assert.EqualValues(t, 0, details["pending_approval_id"])
		assert.Equal(t, false, details["approval_pending"])

		res, err := changeTaskStatusAs(t, env.task.ID, env.done, "", apU1)
		require.NoError(t, err)
		require.NotNil(t, res.Approval)

		err = updateTaskAs(t, &Task{ID: env.task.ID, Title: env.task.Title, Done: true}, apU1)
		required, ok = err.(ErrApprovalRequired)
		require.True(t, ok, "got %T", err)
		details, _ = required.HTTPError().Details.(map[string]any)
		assert.Equal(t, res.Approval.ID, details["pending_approval_id"])
		assert.Equal(t, true, details["approval_pending"])
		assert.Len(t, approvalRowsOfTask(t, env.task.ID), 1, "refusing the update must not create a second request")
	})

	t.Run("the explicit status change creates the request and the approval completes the task", func(t *testing.T) {
		env := doneNeedsApproval(t)
		res, err := changeTaskStatusAs(t, env.task.ID, env.done, "", apU1)
		require.NoError(t, err)
		require.NotNil(t, res.Approval)
		assert.False(t, taskDone(t, env.task.ID))
		mustDecide(t, res.Approval.ID, apU6, ApprovalDecisionApproved, "")
		assert.True(t, taskDone(t, env.task.ID))

		// Reopening needs an approval too: the checkbox is refused, the status route asks.
		err = updateTaskAs(t, &Task{ID: env.task.ID, Title: env.task.Title, Done: false}, apU1)
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequired(err))
		assert.True(t, taskDone(t, env.task.ID))
	})

	t.Run("a kanban move into a status column creates the request and the card stays", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		s := db.NewSession()
		view := &ProjectView{ProjectID: apProject, Title: "By status", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus}
		require.NoError(t, view.Create(s, apU6))
		require.NoError(t, s.Commit())
		s.Close()

		tb, err := wfMove(t, view, env.task.ID, env.doing, apU1)
		require.NoError(t, err)
		require.NotNil(t, tb.Approval)
		assert.Equal(t, ApprovalStatusPending, tb.Approval.Status)
		require.NotNil(t, tb.Bucket)
		assert.Equal(t, statusBucketID(env.todo), tb.Bucket.ID, "the response names the column the card is still in")
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
		assert.Len(t, approvalRowsOfTask(t, env.task.ID), 1)

		// Dragging again returns the same request.
		again, err := wfMove(t, view, env.task.ID, env.doing, apU1)
		require.NoError(t, err)
		assert.Equal(t, tb.Approval.ID, again.Approval.ID)
		assert.Len(t, approvalRowsOfTask(t, env.task.ID), 1)

		// Free columns still move.
		moved, err := wfMove(t, view, env.task.ID, env.canceled, apU1)
		require.NoError(t, err)
		assert.Nil(t, moved.Approval)
		assert.Equal(t, env.canceled, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("a task created in a status column that needs an approval starts in the initial status and requests it", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions = []*WorkflowTransition{
				{FromStatusID: -1, ToStatusID: -2, RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6)},
			}
		})
		s := db.NewSession()
		task := &Task{Title: "dragged in", ProjectID: apProject, BucketID: statusBucketID(env.doing)}
		require.NoError(t, task.Create(s, apU1))
		require.NoError(t, s.Commit())
		s.Close()
		require.NoError(t, task.StatusColumnError())
		assert.Equal(t, env.todo, taskStatusOf(t, task.ID).StatusID)
		assert.Equal(t, statusBucketID(env.todo), task.BucketID)
		rows := approvalRowsOfTask(t, task.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, ApprovalStatusPending, rows[0].Status)
	})

	t.Run("system transitions never create approvals: a repeating task re-opens into a status that needs one", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions = []*WorkflowTransition{
				{FromStatusID: -3, ToStatusID: -1, RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6)},
			}
		})
		setTaskColumns(t, env.task.ID, &Task{RepeatAfter: 3600}, "repeat_after")
		_, err := changeTaskStatusAs(t, env.task.ID, env.done, "", apU1)
		require.NoError(t, err)
		assert.False(t, taskDone(t, env.task.ID))
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID, "re-opened without asking")
		assert.Empty(t, approvalRowsOfTask(t, env.task.ID))
	})

	t.Run("moving tasks out of a removed status does not ask either", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions = []*WorkflowTransition{
				{FromStatusID: -2, ToStatusID: -1, RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6)},
			}
		})
		_, err := changeTaskStatusAs(t, env.task.ID, env.doing, "", apU1)
		require.NoError(t, err)
		doc := readWorkflow(t, apProject)
		kept := []*WorkflowStatus{}
		for _, st := range doc.Statuses {
			if st.ID != env.doing {
				kept = append(kept, st)
			}
		}
		doc.Statuses = kept
		doc.Transitions = nil
		doc.MoveTasksTo = env.todo
		mustSaveWorkflow(t, doc, apU6)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
		assert.Empty(t, approvalRowsOfTask(t, env.task.ID))
	})

	t.Run("admin bypass is not a thing: even a project admin asks", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), nil)
		res, err := changeTaskStatusAs(t, env.task.ID, env.doing, "", apU6)
		require.NoError(t, err)
		require.NotNil(t, res.Approval)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("Check reports the approval without writing", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		s := db.NewSession()
		defer s.Close()
		task, err := GetTaskByIDSimple(s, env.task.ID)
		require.NoError(t, err)
		change := &TaskStatusChange{TaskID: task.ID, ToStatusID: env.doing, task: &task}
		require.NoError(t, change.Check(s, apU1))
		assert.True(t, change.RequiresApproval())
		assert.Empty(t, approvalRowsOfTask(t, task.ID))

		noOne := &TaskStatusChange{TaskID: task.ID, ToStatusID: env.doing, task: &task}
		// The only approver is the requester.
		_, err = s.Where("id = ?", env.transition.ID).Cols("approval").Update(&WorkflowTransition{Approval: apCfg(ApprovalModeAny, 1)})
		require.NoError(t, err)
		err = noOne.Check(s, apU1)
		require.Error(t, err)
		assert.True(t, IsErrApprovalNoApprovers(err))
	})
}

func TestApprovals_Lifecycle(t *testing.T) {
	t.Run("purging a task removes its requests and approvers", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		s := db.NewSession()
		task, err := GetTaskByIDSimple(s, env.task.ID)
		require.NoError(t, err)
		require.NoError(t, hardDeleteTask(s, &task))
		require.NoError(t, s.Commit())
		s.Close()
		db.AssertCount(t, "approval_requests", builder.Eq{"id": req.ID}, 0)
		db.AssertCount(t, "approval_approvers", builder.Eq{"request_id": req.ID}, 0)
	})

	t.Run("purging a project removes its requests and approvers", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		adhoc := createAdHoc(t, env.task.ID, apU1, []int64{6}, ApprovalModeAll)
		s := db.NewSession()
		require.NoError(t, deleteApprovalsForProject(s, apProject))
		require.NoError(t, s.Commit())
		s.Close()
		db.AssertCount(t, "approval_requests", builder.In("id", req.ID, adhoc.ID), 0)
		db.AssertCount(t, "approval_approvers", builder.In("request_id", req.ID, adhoc.ID), 0)
	})

	t.Run("deleting a user cancels their requests and drops them from the ones they decide", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2), nil)
		mine := env.request(t, apU2)
		// User2 asked, so user2 is not an approver of it; user1 asks for another target.
		other := createAdHoc(t, env.task.ID, apU1, []int64{2, 6}, ApprovalModeAll)
		mustDecide(t, other.ID, apU6, ApprovalDecisionApproved, "")

		s := db.NewSession()
		require.NoError(t, removeUserFromApprovals(s, 2))
		require.NoError(t, s.Commit())
		events.DispatchPending(context.Background(), s)
		s.Close()

		requireCancelled(t, mine.ID, ApprovalCancelRequesterDeleted)
		stored := readApprovalRow(t, other.ID)
		assert.Equal(t, ApprovalStatusApproved, stored.Status, "everybody who is left approved")
		assert.Nil(t, approverOf(stored, 2))
		db.AssertCount(t, "approval_approvers", builder.Eq{"user_id": 2}, 0)
	})

	t.Run("deleting the only approver cancels the request", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 2), nil)
		req := env.request(t, apU1)
		s := db.NewSession()
		require.NoError(t, removeUserFromApprovals(s, 2))
		require.NoError(t, s.Commit())
		s.Close()
		requireCancelled(t, req.ID, ApprovalCancelNoApprovers)
	})

	t.Run("duplicating a project does not copy requests but remaps the transition configuration", func(t *testing.T) {
		env := newApprovalEnv(t, &TransitionApproval{
			Approvers:        append(apUsers(6), ApproverSource{Type: ApproverSourceAssignees}),
			RejectedStatusID: -5,
		}, nil)
		req := env.request(t, apU1)

		s := db.NewSession()
		statusMap, wfID, err := duplicateWorkflowForProject(s, apProject, 1, nil)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		assert.NotZero(t, wfID)

		copied := readWorkflow(t, 1)
		var tr *WorkflowTransition
		for _, c := range copied.Transitions {
			if c.RequiresApproval {
				tr = c
			}
		}
		require.NotNil(t, tr)
		require.NotNil(t, tr.Approval)
		assert.Equal(t, statusMap[env.rejected], tr.Approval.RejectedStatusID, "the rejected status points into the copy")
		assert.Len(t, tr.Approval.Approvers, 2)
		db.AssertCount(t, "approval_requests", builder.Eq{"project_id": 1}, 0)
		assert.Equal(t, ApprovalStatusPending, readApprovalRow(t, req.ID).Status)
	})
}
