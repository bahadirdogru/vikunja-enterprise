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
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func TestApprovals_AdHoc(t *testing.T) {
	t.Run("an ad-hoc request records a decision and changes no status", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		events.ClearDispatchedEvents()
		due := time.Now().Add(48 * time.Hour)
		req, err := tryCreateAdHoc(t, &ApprovalRequest{
			TaskID: env.task.ID, Title: "Legal check", Description: "please read page 3",
			ApproverIDs: []int64{6, 2, 6}, Mode: ApprovalModeAll, Due: &due,
		}, apU1)
		require.NoError(t, err)

		stored := readApprovalRow(t, req.ID)
		assert.Equal(t, ApprovalStatusPending, stored.Status)
		assert.Equal(t, ApprovalModeAll, stored.Mode)
		assert.Nil(t, stored.TransitionID)
		assert.Nil(t, stored.FromStatusID)
		assert.Nil(t, stored.ToStatusID)
		assert.Equal(t, "Legal check", stored.Title)
		assert.Equal(t, "please read page 3", stored.Description)
		assert.ElementsMatch(t, []int64{6, 2}, approverIDs(stored), "duplicates are collapsed")
		require.NotNil(t, stored.Due)
		assert.Len(t, events.GetDispatchedEvents("approval.requested"), 1)

		first := mustDecide(t, req.ID, apU6, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusPending, first.Status)
		last := mustDecide(t, req.ID, apU2, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, last.Status)
		assert.Equal(t, env.todo, taskStatusOf(t, env.task.ID).StatusID)
		assert.Len(t, taskHistoryOf(t, env.task.ID), 1)
	})

	t.Run("mode defaults to any", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := createAdHoc(t, env.task.ID, apU1, []int64{6, 2}, "")
		assert.Equal(t, ApprovalModeAny, req.Mode)
		resolved := mustDecide(t, req.ID, apU2, ApprovalDecisionRejected, "no")
		assert.Equal(t, ApprovalStatusRejected, resolved.Status)
	})

	t.Run("works in a project without a workflow", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		// Task 1 is in project 1, owned by user1; user2 has no access there, user6 neither: share first.
		insertRow(t, &ProjectUser{UserID: 2, ProjectID: 1, Permission: PermissionRead})
		req, err := tryCreateAdHoc(t, &ApprovalRequest{TaskID: 1, Title: "Is this ok?", ApproverIDs: []int64{2}}, apU1)
		require.NoError(t, err)
		resolved := mustDecide(t, req.ID, apU2, ApprovalDecisionApproved, "")
		assert.Equal(t, ApprovalStatusApproved, resolved.Status)
	})

	t.Run("validation", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		cases := map[string]*ApprovalRequest{
			"no title":              {TaskID: env.task.ID, ApproverIDs: []int64{6}},
			"no approvers":          {TaskID: env.task.ID, Title: "x"},
			"unknown mode":          {TaskID: env.task.ID, Title: "x", ApproverIDs: []int64{6}, Mode: "most"},
			"the requester":         {TaskID: env.task.ID, Title: "x", ApproverIDs: []int64{1, 6}},
			"approver without read": {TaskID: env.task.ID, Title: "x", ApproverIDs: []int64{6, 4}},
			"unknown user":          {TaskID: env.task.ID, Title: "x", ApproverIDs: []int64{99999}},
		}
		for name, req := range cases {
			_, err := tryCreateAdHoc(t, req, apU1)
			require.Error(t, err, name)
			assert.True(t, IsErrApprovalInvalid(err), "%s: got %T", name, err)
		}
		assert.Empty(t, approvalRowsOfTask(t, env.task.ID))
	})

	t.Run("the requester needs write access to the task", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		_, err := tryCreateAdHoc(t, &ApprovalRequest{TaskID: env.task.ID, Title: "x", ApproverIDs: []int64{6}}, apU3)
		require.Error(t, err)
		assert.True(t, IsErrGenericForbidden(err))
		_, err = tryCreateAdHoc(t, &ApprovalRequest{TaskID: env.task.ID, Title: "x", ApproverIDs: []int64{6}}, apU4)
		require.Error(t, err)
		s := db.NewSession()
		defer s.Close()
		can, err := (&ApprovalRequest{TaskID: env.task.ID}).CanCreate(s, &LinkSharing{ID: 1, ProjectID: apProject, Permission: PermissionAdmin})
		require.NoError(t, err)
		assert.False(t, can, "link shares have no access to approvals")
	})
}

func TestApprovals_ReadAndList(t *testing.T) {
	read := func(t *testing.T, id int64, a web.Auth) (bool, error) {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		can, _, err := (&ApprovalRequest{ID: id}).CanRead(s, a)
		return can, err
	}
	list := func(t *testing.T, filter *ApprovalRequest, a web.Auth, page, perPage int) ([]*ApprovalRequest, int64, error) {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		res, _, total, err := filter.ReadAll(s, a, "", page, perPage)
		if err != nil {
			return nil, 0, err
		}
		return res.([]*ApprovalRequest), total, nil
	}
	ids := func(reqs []*ApprovalRequest) []int64 {
		out := []int64{}
		for _, r := range reqs {
			out = append(out, r.ID)
		}
		return out
	}

	t.Run("a request is readable by everyone who can read the task", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		for _, a := range []user.User{*apU1, *apU2, *apU3, *apU6} {
			a := a
			can, err := read(t, req.ID, &a)
			require.NoError(t, err)
			assert.True(t, can, "user %d", a.ID)
		}
		can, err := read(t, req.ID, apU4)
		require.NoError(t, err)
		assert.False(t, can)
		can, err = read(t, req.ID, &LinkSharing{ID: 1, ProjectID: apProject, Permission: PermissionAdmin})
		require.NoError(t, err)
		assert.False(t, can, "link shares have no access to approvals")
		_, err = read(t, 99999, apU1)
		require.Error(t, err)
		assert.True(t, IsErrApprovalRequestDoesNotExist(err))

		s := db.NewSession()
		defer s.Close()
		full := &ApprovalRequest{ID: req.ID}
		_, _, err = full.CanRead(s, apU3)
		require.NoError(t, err)
		require.NoError(t, full.ReadOne(s, apU3))
		assert.ElementsMatch(t, []int64{6}, approverIDs(full))
		require.NotNil(t, full.Approvers[0].User)
		assert.Equal(t, "user6", full.Approvers[0].User.Username)
		require.NotNil(t, full.RequestedBy)
	})

	t.Run("the list holds the requests of the caller as approver or requester with a task summary", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		transition := env.request(t, apU1)
		adhoc := createAdHoc(t, env.task.ID, apU2, []int64{1}, ApprovalModeAny)

		mine, total, err := list(t, &ApprovalRequest{}, apU1, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 2, total)
		assert.Equal(t, []int64{adhoc.ID, transition.ID}, ids(mine), "newest first")
		require.NotNil(t, mine[0].Task)
		assert.Equal(t, env.task.ID, mine[0].Task.ID)
		assert.Equal(t, "needs a sign-off", mine[0].Task.Title)
		assert.Equal(t, apProject, mine[0].Task.ProjectID)
		assert.NotEmpty(t, mine[0].Task.ProjectTitle)
		assert.NotEmpty(t, mine[0].Task.Identifier)
		require.NotNil(t, mine[1].ToStatus)
		assert.Equal(t, "Doing", mine[1].ToStatus.Title)

		asApprover, _, err := list(t, &ApprovalRequest{FilterRole: "approver"}, apU1, 1, 50)
		require.NoError(t, err)
		assert.Equal(t, []int64{adhoc.ID}, ids(asApprover))
		asRequester, _, err := list(t, &ApprovalRequest{FilterRole: "requester"}, apU1, 1, 50)
		require.NoError(t, err)
		assert.Equal(t, []int64{transition.ID}, ids(asRequester))

		other, total, err := list(t, &ApprovalRequest{}, apU6, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 1, total)
		assert.Equal(t, []int64{transition.ID}, ids(other))

		none, total, err := list(t, &ApprovalRequest{}, apU3, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 0, total, "user3 can read the task but is neither approver nor requester")
		assert.Empty(t, none)
	})

	t.Run("filters: status, project, role validation and pagination", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		first := env.request(t, apU1)
		mustDecide(t, first.ID, apU6, ApprovalDecisionApproved, "")
		second := createAdHoc(t, env.task.ID, apU1, []int64{6}, ApprovalModeAny)
		third := createAdHoc(t, env.task.ID, apU1, []int64{2}, ApprovalModeAny)

		pending, total, err := list(t, &ApprovalRequest{FilterStatus: "pending"}, apU1, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 2, total)
		assert.Equal(t, []int64{third.ID, second.ID}, ids(pending))
		approved, _, err := list(t, &ApprovalRequest{FilterStatus: "approved"}, apU1, 1, 50)
		require.NoError(t, err)
		assert.Equal(t, []int64{first.ID}, ids(approved))

		inProject, total, err := list(t, &ApprovalRequest{FilterProjectID: apProject}, apU1, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 3, total)
		assert.Len(t, inProject, 3)
		elsewhere, total, err := list(t, &ApprovalRequest{FilterProjectID: 1}, apU1, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 0, total)
		assert.Empty(t, elsewhere)

		page1, total, err := list(t, &ApprovalRequest{}, apU1, 1, 2)
		require.NoError(t, err)
		assert.EqualValues(t, 3, total)
		assert.Equal(t, []int64{third.ID, second.ID}, ids(page1))
		page2, _, err := list(t, &ApprovalRequest{}, apU1, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, []int64{first.ID}, ids(page2))

		_, _, err = list(t, &ApprovalRequest{FilterStatus: "bogus"}, apU1, 1, 50)
		require.Error(t, err)
		assert.True(t, IsErrApprovalInvalid(err))
		_, _, err = list(t, &ApprovalRequest{FilterRole: "bogus"}, apU1, 1, 50)
		require.Error(t, err)
		assert.True(t, IsErrApprovalInvalid(err))
	})

	t.Run("requests whose task the caller can no longer read are not listed", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6, 2), nil)
		env.request(t, apU1)
		before, total, err := list(t, &ApprovalRequest{}, apU2, 1, 50)
		require.NoError(t, err)
		require.EqualValues(t, 1, total)
		require.Len(t, before, 1)

		s := db.NewSession()
		_, err = s.Where("user_id = ? AND project_id = ?", 2, apProject).Delete(&ProjectUser{})
		require.NoError(t, err)
		_, err = s.Where("project_id = ?", apProject).And("team_id = ?", 1).Delete(&TeamProject{})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()

		after, total, err := list(t, &ApprovalRequest{}, apU2, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 0, total)
		assert.Empty(t, after)
	})

	t.Run("requests of trashed tasks are not listed", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		env.request(t, apU1)
		s := db.NewSession()
		require.NoError(t, (&Task{ID: env.task.ID}).Delete(s, apU1))
		require.NoError(t, s.Commit())
		s.Close()
		res, total, err := list(t, &ApprovalRequest{}, apU1, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 0, total)
		assert.Empty(t, res)
	})

	t.Run("the list of a task needs read access to it and refuses link shares", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		req := env.request(t, apU1)
		reqs, total, err := list(t, &ApprovalRequest{TaskID: env.task.ID}, apU3, 1, 50)
		require.NoError(t, err)
		assert.EqualValues(t, 1, total)
		assert.Equal(t, []int64{req.ID}, ids(reqs))
		_, _, err = list(t, &ApprovalRequest{TaskID: env.task.ID}, apU4, 1, 50)
		require.Error(t, err)
		_, _, err = list(t, &ApprovalRequest{TaskID: env.task.ID}, &LinkSharing{ID: 1, ProjectID: apProject, Permission: PermissionRead}, 1, 50)
		require.Error(t, err)
		assert.True(t, IsErrGenericForbidden(err))
	})
}

func TestApprovals_ApproverSources(t *testing.T) {
	// The task is created by user6 in project 10; the requester is user1 unless a case says otherwise.
	resolve := func(t *testing.T, env *approvalEnv, requester int64, cfg *TransitionApproval) []int64 {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		task, err := GetTaskByIDSimple(s, env.task.ID)
		require.NoError(t, err)
		users, err := resolveTransitionApprovers(s, &task, requester, cfg)
		require.NoError(t, err)
		out := []int64{}
		for _, u := range users {
			out = append(out, u.ID)
		}
		return out
	}

	t.Run("a team expands to its members when the request is created", func(t *testing.T) {
		// Team 1: user1 and user2; the team has read access to project 10.
		env := newApprovalEnv(t, apSources(ApproverSource{Type: ApproverSourceTeam, ID: 1}), nil)
		req := env.request(t, apU6)
		assert.ElementsMatch(t, []int64{1, 2}, approverIDs(readApprovalRow(t, req.ID)))

		// Later changes of the team do not touch the request.
		insertRow(t, &TeamMember{TeamID: 1, UserID: 3})
		assert.ElementsMatch(t, []int64{1, 2}, approverIDs(readApprovalRow(t, req.ID)))
	})

	t.Run("the assignees of the task are asked", func(t *testing.T) {
		env := newApprovalEnv(t, apSources(ApproverSource{Type: ApproverSourceAssignees}), nil)
		assert.Empty(t, resolve(t, env, 1, env.transition.Approval))
		insertRow(t, &TaskAssginee{TaskID: env.task.ID, UserID: 2})
		insertRow(t, &TaskAssginee{TaskID: env.task.ID, UserID: 3})
		assert.Equal(t, []int64{2, 3}, resolve(t, env, 1, env.transition.Approval))
		// The requester is one of them.
		insertRow(t, &TaskAssginee{TaskID: env.task.ID, UserID: 1})
		assert.Equal(t, []int64{2, 3}, resolve(t, env, 1, env.transition.Approval))
		assert.Equal(t, []int64{2, 3, 1}, resolve(t, env, 6, env.transition.Approval))
	})

	t.Run("a user field of the task names the approver", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		field := &CustomField{ProjectID: apProject, Title: "Editor", Type: CustomFieldTypeUser, CreatedByID: 6}
		insertRow(t, field)
		cfg := apSources(ApproverSource{Type: ApproverSourceCustomField, ID: field.ID})
		assert.Empty(t, resolve(t, env, 1, cfg), "the field has no value yet")
		cfInsertValues(t, &TaskCustomFieldValue{TaskID: env.task.ID, CustomFieldID: field.ID, UserID: cfPtr(int64(2))})
		assert.Equal(t, []int64{2}, resolve(t, env, 1, cfg))

		// A field of another project or of another type resolves to nobody.
		other := &CustomField{ProjectID: apProject, Title: "Note", Type: CustomFieldTypeText, CreatedByID: 6}
		insertRow(t, other)
		assert.Empty(t, resolve(t, env, 1, apSources(ApproverSource{Type: ApproverSourceCustomField, ID: other.ID})))
		assert.Empty(t, resolve(t, env, 1, apSources(ApproverSource{Type: ApproverSourceCustomField, ID: 1})))
	})

	t.Run("sources are combined, de-duplicated in order and filtered by read access and status", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		insertRow(t, &TaskAssginee{TaskID: env.task.ID, UserID: 2})
		cfg := apSources(
			ApproverSource{Type: ApproverSourceUser, ID: 3},
			ApproverSource{Type: ApproverSourceAssignees},
			ApproverSource{Type: ApproverSourceTeam, ID: 1},
			ApproverSource{Type: ApproverSourceUser, ID: 4},     // cannot read the project
			ApproverSource{Type: ApproverSourceUser, ID: 99999}, // does not exist
			ApproverSource{Type: ApproverSourceUser, ID: 3},
		)
		assert.Equal(t, []int64{3, 2}, resolve(t, env, 1, cfg))

		cfg.AllowSelf = true
		assert.Equal(t, []int64{3, 2, 1}, resolve(t, env, 1, cfg))

		setRaw(t, "users", builder.Eq{"id": 3}, map[string]any{"status": int(user.StatusDisabled)})
		assert.Equal(t, []int64{2, 1}, resolve(t, env, 1, cfg), "disabled users are not asked")
	})
}

func TestApprovals_TaskStatusReadAndExpand(t *testing.T) {
	t.Run("the status of a task lists pending approvals and the approver preview per transition", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAll, 6, 2, 1), func(doc *Workflow) {
			doc.Transitions[0].Approval.DueIn = "1d"
		})
		read := func(a web.Auth) *TaskWorkflowStatus {
			s := db.NewSession()
			defer s.Close()
			ts := &TaskWorkflowStatus{TaskID: env.task.ID}
			can, _, err := ts.CanRead(s, a)
			require.NoError(t, err)
			require.True(t, can)
			require.NoError(t, ts.ReadOne(s, a))
			return ts
		}
		find := func(ts *TaskWorkflowStatus, to int64) *AvailableTransition {
			for _, tr := range ts.Transitions {
				if tr.ToStatus.ID == to {
					return tr
				}
			}
			return nil
		}

		ts := read(apU1)
		doing := find(ts, env.doing)
		require.NotNil(t, doing)
		assert.True(t, doing.RequiresApproval)
		assert.Equal(t, ApprovalModeAll, doing.ApprovalMode)
		previewed := []int64{}
		for _, u := range doing.Approvers {
			previewed = append(previewed, u.ID)
		}
		assert.Equal(t, []int64{6, 2}, previewed, "the requester is not asked")
		assert.NotEmpty(t, doing.Approvers[0].Username)
		done := find(ts, env.done)
		require.NotNil(t, done)
		assert.False(t, done.RequiresApproval)
		assert.Empty(t, done.Approvers)
		assert.Empty(t, ts.PendingApprovals)

		req := env.request(t, apU1)
		adhoc := createAdHoc(t, env.task.ID, apU1, []int64{6}, ApprovalModeAny)
		ts = read(apU3)
		require.Len(t, ts.PendingApprovals, 2)
		assert.Equal(t, req.ID, ts.PendingApprovals[0].ID)
		assert.Equal(t, adhoc.ID, ts.PendingApprovals[1].ID)
		assert.Len(t, ts.PendingApprovals[0].Approvers, 2, "approvers come with the pending requests")
		assert.Nil(t, find(ts, env.doing), "a reader cannot perform the transition, so it is not offered")
		asUser2 := find(read(apU2), env.doing)
		require.NotNil(t, asUser2)
		assert.Equal(t, []int64{6, 1}, func() []int64 {
			ids := []int64{}
			for _, u := range asUser2.Approvers {
				ids = append(ids, u.ID)
			}
			return ids
		}(), "user2 as requester is not asked")

		// Resolved requests drop out.
		mustDecide(t, adhoc.ID, apU6, ApprovalDecisionApproved, "")
		ts = read(apU1)
		assert.Len(t, ts.PendingApprovals, 1)

		// Link shares see neither.
		share := &LinkSharing{ID: 1, ProjectID: apProject, Permission: PermissionWrite}
		s := db.NewSession()
		defer s.Close()
		ts = &TaskWorkflowStatus{TaskID: env.task.ID}
		_, _, err := ts.CanRead(s, share)
		require.NoError(t, err)
		require.NoError(t, ts.ReadOne(s, share))
		assert.Empty(t, ts.PendingApprovals)
		for _, tr := range ts.Transitions {
			assert.Empty(t, tr.Approvers)
		}
	})

	t.Run("the approvals expand marks tasks with pending requests without per-task queries", func(t *testing.T) {
		env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)
		second := createWorkflowTask(t, apProject, &Task{Title: "second", ProjectID: apProject})
		third := createWorkflowTask(t, apProject, &Task{Title: "third", ProjectID: apProject})
		env.request(t, apU1)
		createAdHoc(t, env.task.ID, apU1, []int64{6}, ApprovalModeAny)
		createAdHoc(t, second.ID, apU1, []int64{6}, ApprovalModeAny)

		s := db.NewSession()
		defer s.Close()
		tc := &TaskCollection{ProjectID: apProject, Expand: []TaskCollectionExpandable{TaskCollectionExpandApprovals}}
		res, _, _, err := tc.ReadAll(s, apU1, "", 1, 50)
		require.NoError(t, err)
		byID := map[int64]*Task{}
		for _, task := range res.([]*Task) {
			byID[task.ID] = task
		}
		require.NotNil(t, byID[env.task.ID].Approvals)
		assert.True(t, byID[env.task.ID].Approvals.HasPending)
		assert.EqualValues(t, 2, byID[env.task.ID].Approvals.PendingCount)
		assert.EqualValues(t, 1, byID[second.ID].Approvals.PendingCount)
		require.NotNil(t, byID[third.ID].Approvals, "every task of the page carries the summary")
		assert.False(t, byID[third.ID].Approvals.HasPending)

		// Without the expand nothing is added.
		res, _, _, err = (&TaskCollection{ProjectID: apProject}).ReadAll(s, apU1, "", 1, 50)
		require.NoError(t, err)
		for _, task := range res.([]*Task) {
			assert.Nil(t, task.Approvals)
		}
		assert.NoError(t, TaskCollectionExpandApprovals.Validate())
		assert.Error(t, TaskCollectionExpandable("nope").Validate())

		// Link shares never get it.
		share := &LinkSharing{ID: 1, ProjectID: apProject, Permission: PermissionRead}
		res, _, _, err = (&TaskCollection{ProjectID: apProject, Expand: []TaskCollectionExpandable{TaskCollectionExpandApprovals}}).ReadAll(s, share, "", 1, 50)
		require.NoError(t, err)
		for _, task := range res.([]*Task) {
			assert.Nil(t, task.Approvals)
		}
	})
}
