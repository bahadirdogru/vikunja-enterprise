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
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// Topology of the approval tests: project 10 is owned by user6 (admin); user1 has write access through the fixtures.
// The tests share it with user2 (write) and user3 (read). User4 has no access. Team 1 holds user1 and user2.
var (
	apU1 = &user.User{ID: 1}
	apU2 = &user.User{ID: 2}
	apU3 = &user.User{ID: 3}
	apU4 = &user.User{ID: 4}
	apU6 = &user.User{ID: 6}
)

const apProject = int64(10)

type approvalEnv struct {
	wf       *Workflow
	task     *Task
	todo     int64
	doing    int64
	done     int64
	canceled int64
	rejected int64
	// transition is the Todo to Doing transition that asks for approval.
	transition *WorkflowTransition
}

func apSources(sources ...ApproverSource) *TransitionApproval {
	return &TransitionApproval{Approvers: sources}
}

func apUsers(ids ...int64) []ApproverSource {
	out := make([]ApproverSource, 0, len(ids))
	for _, id := range ids {
		out = append(out, ApproverSource{Type: ApproverSourceUser, ID: id})
	}
	return out
}

func apCfg(mode ApprovalMode, userIDs ...int64) *TransitionApproval {
	return &TransitionApproval{Mode: mode, Approvers: apUsers(userIDs...)}
}

// newApprovalEnv loads the fixtures, shares project 10 and saves a workflow whose Todo to Doing transition needs the
// given approval. The done status is reached through a second transition that needs approval as well when
// doneNeedsApproval is set.
func newApprovalEnv(t *testing.T, cfg *TransitionApproval, tweak func(doc *Workflow)) *approvalEnv {
	t.Helper()
	db.LoadAndAssertFixtures(t)
	insertRow(t, &ProjectUser{UserID: 2, ProjectID: apProject, Permission: PermissionWrite})
	insertRow(t, &ProjectUser{UserID: 3, ProjectID: apProject, Permission: PermissionRead})
	insertRow(t, &TeamProject{TeamID: 1, ProjectID: apProject, Permission: PermissionRead})

	doc := baseWorkflowDoc(apProject)
	doc.Statuses = append(doc.Statuses, &WorkflowStatus{ID: -5, Title: "Rejected", Category: WorkflowCategoryOpen})
	if cfg != nil && cfg.RejectedStatusID == 0 {
		// Tests that need the rejected status set it themselves.
		cfg.RejectedStatusID = 0
	}
	doc.Transitions = []*WorkflowTransition{
		{FromStatusID: -1, ToStatusID: -2, Title: "Start work", RequiresApproval: true, Approval: cfg},
	}
	if tweak != nil {
		tweak(doc)
	}
	wf := mustSaveWorkflow(t, doc, apU6)
	task := createWorkflowTask(t, apProject, &Task{Title: "needs a sign-off", ProjectID: apProject})

	env := &approvalEnv{
		wf:       wf,
		task:     task,
		todo:     statusIDByTitle(wf, "Todo"),
		doing:    statusIDByTitle(wf, "Doing"),
		done:     statusIDByTitle(wf, "Done"),
		canceled: statusIDByTitle(wf, "Cancelled"),
		rejected: statusIDByTitle(wf, "Rejected"),
	}
	for _, tr := range wf.Transitions {
		if tr.FromStatusID == env.todo && tr.ToStatusID == env.doing {
			env.transition = tr
		}
	}
	return env
}

// request asks for the Todo to Doing transition as the given user and returns the pending request.
func (e *approvalEnv) request(t *testing.T, a web.Auth) *ApprovalRequest {
	t.Helper()
	res, err := changeTaskStatusAs(t, e.task.ID, e.doing, "please", a)
	require.NoError(t, err)
	require.NotNil(t, res.Approval, "the transition must ask for an approval")
	return res.Approval
}

func decideAs(t *testing.T, requestID int64, a web.Auth, decision ApprovalDecisionValue, comment string) (*ApprovalDecision, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()

	d := &ApprovalDecision{RequestID: requestID, Decision: decision, Comment: comment}
	can, err := d.CanCreate(s, a)
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
	if err = d.Create(s, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, err
	}
	require.NoError(t, events.PendingHookError(s))
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	return d, nil
}

func mustDecide(t *testing.T, requestID int64, a web.Auth, decision ApprovalDecisionValue, comment string) *ApprovalRequest {
	t.Helper()
	d, err := decideAs(t, requestID, a, decision, comment)
	require.NoError(t, err)
	return d.Request
}

func cancelAs(t *testing.T, requestID int64, a web.Auth, message string) (*ApprovalCancel, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()

	c := &ApprovalCancel{RequestID: requestID, Message: message}
	can, err := c.CanCreate(s, a)
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
	if err = c.Create(s, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, err
	}
	require.NoError(t, s.Commit())
	events.DispatchPending(context.Background(), s)
	return c, nil
}

func readApprovalRow(t *testing.T, id int64) *ApprovalRequest {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	req, err := getApprovalRequest(s, id, false)
	require.NoError(t, err)
	require.NoError(t, hydrateApprovalRequest(s, req))
	return req
}

func approvalRowsOfTask(t *testing.T, taskID int64) []*ApprovalRequest {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	rows := []*ApprovalRequest{}
	require.NoError(t, s.Where("task_id = ?", taskID).OrderBy("id ASC").Find(&rows))
	return rows
}

func approverIDs(req *ApprovalRequest) []int64 {
	ids := []int64{}
	for _, ap := range req.Approvers {
		ids = append(ids, ap.UserID)
	}
	return ids
}

func approverOf(req *ApprovalRequest, userID int64) *ApprovalApprover {
	for _, ap := range req.Approvers {
		if ap.UserID == userID {
			return ap
		}
	}
	return nil
}

// setRaw writes straight to a table, bypassing the hooks that would cancel the request: it simulates the state a
// decision finds after something changed behind the back of the model.
func setRaw(t *testing.T, table string, cond builder.Cond, values map[string]any) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	_, err := s.Table(table).Where(cond).Update(values)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
}

func TestApprovals_TransitionRequest(t *testing.T) {
	t.Run("the transition does not move the task but creates a pending request", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		events.ClearDispatchedEvents()

		res, err := changeTaskStatusAs(t, env.task.ID, env.doing, "please", apU1)
		require.NoError(t, err)
		require.NotNil(t, res.Approval)
		assert.Equal(t, env.todo, res.Status.ID, "the response shows the status the task is still in")
		assert.False(t, res.Done)

		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
		assert.Len(t, taskHistoryOf(t, env.task.ID), 1, "no history entry for a change that did not happen")

		req := readApprovalRow(t, res.Approval.ID)
		assert.Equal(t, ApprovalStatusPending, req.Status)
		assert.Equal(t, ApprovalModeAny, req.Mode)
		assert.Equal(t, env.task.ID, req.TaskID)
		assert.Equal(t, apProject, req.ProjectID)
		assert.EqualValues(t, 1, req.RequestedByID)
		require.NotNil(t, req.TransitionID)
		assert.Equal(t, env.transition.ID, *req.TransitionID)
		require.NotNil(t, req.FromStatusID)
		assert.Equal(t, env.todo, *req.FromStatusID)
		require.NotNil(t, req.ToStatusID)
		assert.Equal(t, env.doing, *req.ToStatusID)
		assert.Equal(t, "Start work", req.Title)
		assert.Equal(t, "please", req.Comment)
		assert.ElementsMatch(t, []int64{6, 2}, approverIDs(req))
		assert.Nil(t, req.Due)
		require.NotNil(t, req.Task)
		assert.Equal(t, "needs a sign-off", req.Task.Title)
		assert.Equal(t, apProject, req.Task.ProjectID)
		require.NotNil(t, req.ToStatus)
		assert.Equal(t, "Doing", req.ToStatus.Title)

		dispatched := events.GetDispatchedEvents("approval.requested")
		require.Len(t, dispatched, 1)
		requested, ok := dispatched[0].(*ApprovalRequestedEvent)
		require.True(t, ok)
		assert.Equal(t, req.ID, requested.Approval.ID)
		assert.Equal(t, env.task.ID, requested.Task.ID)
		assert.EqualValues(t, 1, requested.Doer.ID)
		assert.Len(t, requested.Approval.Approvers, 2)
	})

	t.Run("a due_in duration sets the due time", func(t *testing.T) {
		cfg := apCfg(ApprovalModeAny, 6)
		cfg.DueIn = "3d"
		env := newApprovalEnv(t, cfg, nil)
		before := time.Now()
		req := env.request(t, apU1)
		stored := readApprovalRow(t, req.ID)
		require.NotNil(t, stored.Due)
		assert.WithinDuration(t, before.Add(72*time.Hour), *stored.Due, time.Minute)
	})

	t.Run("a request that is already pending is returned instead of a duplicate", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		events.ClearDispatchedEvents()
		first := env.request(t, apU1)
		second := env.request(t, apU1)
		other := env.request(t, apU2)
		assert.Equal(t, first.ID, second.ID)
		assert.Equal(t, first.ID, other.ID, "the request is per task and target, whoever asks")
		assert.Len(t, approvalRowsOfTask(t, env.task.ID), 1)
		assert.Len(t, events.GetDispatchedEvents("approval.requested"), 1, "the repeated asks announce nothing")
	})

	t.Run("a pending request for another target on the same task is allowed", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions = append(doc.Transitions, &WorkflowTransition{
				FromStatusID: -1, ToStatusID: -3, RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6),
			})
		})
		first := env.request(t, apU1)
		res, err := changeTaskStatusAs(t, env.task.ID, env.done, "", apU1)
		require.NoError(t, err)
		require.NotNil(t, res.Approval)
		assert.NotEqual(t, first.ID, res.Approval.ID)
		assert.Len(t, approvalRowsOfTask(t, env.task.ID), 2)
	})

	t.Run("the other checks run first and nothing is created when they fail", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions[0].RequireComment = true
			doc.Transitions[0].RequiredFields = []string{WorkflowFieldDueDate}
		})
		_, err := changeTaskStatusAs(t, env.task.ID, env.doing, "", apU1)
		details := requireRequirements(t, err)
		assert.Equal(t, []string{WorkflowFieldDueDate}, details.MissingFields)
		assert.True(t, details.CommentRequired)
		assert.Empty(t, approvalRowsOfTask(t, env.task.ID))

		setTaskColumns(t, env.task.ID, &Task{DueDate: time.Now().Add(time.Hour)}, "due_date")
		res, err := changeTaskStatusAs(t, env.task.ID, env.doing, "now it is complete", apU1)
		require.NoError(t, err)
		require.NotNil(t, res.Approval)
	})

	t.Run("the permission and team of the transition apply to the requester", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), func(doc *Workflow) {
			doc.Transitions[0].TeamIDs = []int64{9}
		})
		_, err := changeTaskStatusAs(t, env.task.ID, env.doing, "", apU1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowTransitionForbidden(err))
		_, err = changeTaskStatusAs(t, env.task.ID, env.doing, "", apU3)
		require.Error(t, err, "read access is not enough")
		assert.Empty(t, approvalRowsOfTask(t, env.task.ID))
		// User2 is in team 9, project admins always may.
		res, err := changeTaskStatusAs(t, env.task.ID, env.doing, "", apU2)
		require.NoError(t, err)
		require.NotNil(t, res.Approval)
	})

	t.Run("nobody to ask is a typed error and nothing is created", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 1), nil)
		_, err := changeTaskStatusAs(t, env.task.ID, env.doing, "", apU1)
		require.Error(t, err)
		noApprovers, ok := err.(ErrApprovalNoApprovers)
		require.True(t, ok, "got %T", err)
		assert.Equal(t, env.task.ID, noApprovers.TaskID)
		assert.Equal(t, env.doing, noApprovers.ToStatusID)
		assert.Equal(t, 422, noApprovers.HTTPError().HTTPCode)
		assert.Equal(t, ErrCodeApprovalNoApprovers, noApprovers.HTTPError().Code)
		assert.Empty(t, approvalRowsOfTask(t, env.task.ID))
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
	})

	t.Run("the requester is excluded unless the transition allows self-approval", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 1, 6), nil)
		req := env.request(t, apU1)
		assert.Equal(t, []int64{6}, approverIDs(readApprovalRow(t, req.ID)))

		env = newApprovalEnv(t, &TransitionApproval{Mode: ApprovalModeAny, Approvers: apUsers(1, 6), AllowSelf: true}, nil)
		req = env.request(t, apU1)
		stored := readApprovalRow(t, req.ID)
		assert.ElementsMatch(t, []int64{1, 6}, approverIDs(stored))
		assert.True(t, stored.AllowSelf)
	})

	t.Run("link shares cannot request an approval", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		d, err := loadWorkflowDefForTest(t, apProject)
		require.NoError(t, err)
		s := db.NewSession()
		defer s.Close()
		task, err := GetTaskByIDSimple(s, env.task.ID)
		require.NoError(t, err)
		_, _, err = d.requestTransitionApproval(s, &LinkSharing{ID: 1, ProjectID: apProject, Permission: PermissionWrite}, &task,
			d.statuses[env.todo], d.statuses[env.doing], env.transition, "")
		require.Error(t, err)
		assert.True(t, IsErrApprovalLinkShare(err))
	})
}

func loadWorkflowDefForTest(t *testing.T, projectID int64) (*workflowDef, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	return loadWorkflowDef(s, projectID)
}
