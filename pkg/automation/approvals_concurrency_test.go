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

package automation

import (
	"sync"
	"testing"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// These tests run in this package because its TestMain gives them a file based SQLite database in WAL mode (or the
// database of the environment): the default in-memory database cannot take concurrent writers. They are meant to
// run with -race.

const concurrencyRounds = 12

// goroutineCommit runs fn in a transaction like the CRUD handlers do, without calling into testing from a goroutine.
func goroutineCommit(fn func(s *xorm.Session) error) error {
	s := db.NewSession()
	defer s.Close()
	if err := fn(s); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	if err := events.PendingHookError(s); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	if err := s.Commit(); err != nil {
		events.CleanupPending(s)
		return err
	}
	// The fake event dispatcher of the tests appends to a shared slice, which -race rightly flags when sessions
	// commit in parallel. The outbox rows the automation tests look at are written in the transaction, so the
	// queued events are dropped instead of dispatched.
	events.CleanupPending(s)
	return nil
}

func tryDecide(requestID int64, by *user.User, decision models.ApprovalDecisionValue, comment string) error {
	return goroutineCommit(func(s *xorm.Session) error {
		d := &models.ApprovalDecision{RequestID: requestID, Decision: decision, Comment: comment}
		can, err := d.CanCreate(s, by)
		if err != nil {
			return err
		}
		if !can {
			return models.ErrGenericForbidden{}
		}
		return d.Create(s, by)
	})
}

func tryCancel(requestID int64, by *user.User) error {
	return goroutineCommit(func(s *xorm.Session) error {
		c := &models.ApprovalCancel{RequestID: requestID}
		can, err := c.CanCreate(s, by)
		if err != nil {
			return err
		}
		if !can {
			return models.ErrGenericForbidden{}
		}
		return c.Create(s, by)
	})
}

func tryRequestDropping(taskID int64, by *user.User) (int64, error) {
	var id int64
	err := goroutineCommit(func(s *xorm.Session) error {
		c := &models.TaskStatusChange{TaskID: taskID, ToStatusID: 5, Comment: "c"}
		can, err := c.CanCreate(s, by)
		if err != nil {
			return err
		}
		if !can {
			return models.ErrGenericForbidden{}
		}
		if err = c.Create(s, by); err != nil {
			return err
		}
		if c.Approval != nil {
			id = c.Approval.ID
		}
		return nil
	})
	return id, err
}

// race runs the functions at the same moment and returns their errors in order.
func race(fns ...func() error) []error {
	errs := make([]error, len(fns))
	ready := sync.WaitGroup{}
	ready.Add(len(fns))
	start := make(chan struct{})
	done := sync.WaitGroup{}
	done.Add(len(fns))
	for i, fn := range fns {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			errs[i] = fn()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return errs
}

func statusOf(t *testing.T, taskID int64) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	status, err := models.GetTaskWorkflowStatus(s, taskID)
	require.NoError(t, err)
	if status == nil {
		return 0
	}
	return status.ID
}

func historyTo(t *testing.T, taskID, statusID int64) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("task_id = ? AND to_status_id = ?", taskID, statusID).Count(&models.TaskStatusHistory{})
	require.NoError(t, err)
	return n
}

func requestState(t *testing.T, id int64) *models.ApprovalRequest {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	req := &models.ApprovalRequest{}
	has, err := s.ID(id).Get(req)
	require.NoError(t, err)
	require.True(t, has)
	return req
}

func decidedCount(t *testing.T, requestID int64) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("request_id = ?", requestID).And("decision IS NOT NULL").Count(&models.ApprovalApprover{})
	require.NoError(t, err)
	return n
}

func TestApprovalConcurrency_BothApproversOfModeAllDecideAtOnce(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAll, 6, 2)
	counting(t, projectWorkflow, spec.TriggerApprovalResolved, nil, "resolved")

	for round := 0; round < concurrencyRounds; round++ {
		task := newTask(t, projectWorkflow, "all at once", user1)
		req := requestDropping(t, task.ID, user1)
		errs := race(
			func() error { return tryDecide(req.ID, user6, models.ApprovalDecisionApproved, "") },
			func() error { return tryDecide(req.ID, user2, models.ApprovalDecisionApproved, "") },
		)
		require.NoError(t, errs[0], "round %d", round)
		require.NoError(t, errs[1], "round %d", round)

		stored := requestState(t, req.ID)
		assert.Equal(t, models.ApprovalStatusApproved, stored.Status, "round %d: the second decision completes the set whoever it is", round)
		assert.EqualValues(t, 2, decidedCount(t, req.ID))
		assert.EqualValues(t, 5, statusOf(t, task.ID))
		assert.EqualValues(t, 1, historyTo(t, task.ID, 5), "the transition happened exactly once")
	}
	drain(t)
	assert.Equal(t, concurrencyRounds, counter("resolved"), "one resolved event per request")
}

func TestApprovalConcurrency_ApproveAndRejectOfModeAnyRaceForTheRequest(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6, 2)
	counting(t, projectWorkflow, spec.TriggerApprovalResolved, nil, "resolved")

	won := map[models.ApprovalStatus]int{}
	for round := 0; round < concurrencyRounds; round++ {
		task := newTask(t, projectWorkflow, "approve or reject", user1)
		req := requestDropping(t, task.ID, user1)
		errs := race(
			func() error { return tryDecide(req.ID, user6, models.ApprovalDecisionApproved, "") },
			func() error { return tryDecide(req.ID, user2, models.ApprovalDecisionRejected, "no") },
		)
		failures := 0
		for _, err := range errs {
			if err != nil {
				failures++
				assert.True(t, models.IsErrApprovalNotPending(err), "round %d: the loser gets the typed conflict, got %T %v", round, err, err)
			}
		}
		assert.Equal(t, 1, failures, "round %d: exactly one decision wins", round)

		stored := requestState(t, req.ID)
		won[stored.Status]++
		assert.EqualValues(t, 1, decidedCount(t, req.ID), "the loser's decision was not recorded")
		switch stored.Status {
		case models.ApprovalStatusApproved:
			assert.EqualValues(t, 5, statusOf(t, task.ID))
			assert.EqualValues(t, 1, historyTo(t, task.ID, 5))
		case models.ApprovalStatusRejected:
			assert.EqualValues(t, 1, statusOf(t, task.ID))
			assert.EqualValues(t, 0, historyTo(t, task.ID, 5))
		default:
			t.Fatalf("round %d: unexpected state %s", round, stored.Status)
		}
	}
	drain(t)
	assert.Equal(t, concurrencyRounds, counter("resolved"))
	t.Logf("approvals won %d rounds, rejections %d", won[models.ApprovalStatusApproved], won[models.ApprovalStatusRejected])
}

func TestApprovalConcurrency_ADecisionRacingACancel(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6)
	counting(t, projectWorkflow, spec.TriggerApprovalResolved, nil, "resolved")

	for round := 0; round < concurrencyRounds; round++ {
		task := newTask(t, projectWorkflow, "decision or cancel", user1)
		req := requestDropping(t, task.ID, user1)
		errs := race(
			func() error { return tryDecide(req.ID, user6, models.ApprovalDecisionApproved, "") },
			func() error { return tryCancel(req.ID, user1) },
		)
		failures := 0
		for _, err := range errs {
			if err != nil {
				failures++
				assert.True(t, models.IsErrApprovalNotPending(err), "round %d: got %T %v", round, err, err)
			}
		}
		assert.Equal(t, 1, failures, "round %d: exactly one of them wins", round)

		stored := requestState(t, req.ID)
		switch stored.Status {
		case models.ApprovalStatusApproved:
			assert.NoError(t, errs[0])
			assert.EqualValues(t, 5, statusOf(t, task.ID))
			assert.EqualValues(t, 1, historyTo(t, task.ID, 5))
		case models.ApprovalStatusCancelled:
			assert.NoError(t, errs[1])
			assert.Equal(t, models.ApprovalCancelByRequester, stored.CancelReason)
			assert.EqualValues(t, 1, statusOf(t, task.ID), "a cancelled request moved nothing")
			assert.EqualValues(t, 0, historyTo(t, task.ID, 5))
			assert.EqualValues(t, 0, decidedCount(t, req.ID), "the approval of the loser was not recorded")
		default:
			t.Fatalf("round %d: unexpected state %s", round, stored.Status)
		}
	}
	drain(t)
	assert.Equal(t, concurrencyRounds, counter("resolved"))
}

func TestApprovalConcurrency_ARejectionAndAnApprovalOfModeAll(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAll, 6, 2)
	for round := 0; round < concurrencyRounds; round++ {
		task := newTask(t, projectWorkflow, "reject and approve", user1)
		req := requestDropping(t, task.ID, user1)
		errs := race(
			func() error { return tryDecide(req.ID, user6, models.ApprovalDecisionApproved, "") },
			func() error { return tryDecide(req.ID, user2, models.ApprovalDecisionRejected, "no") },
		)
		// The rejection always ends it; the approval may land before it or find the request resolved.
		require.NoError(t, errs[1], "round %d", round)
		if errs[0] != nil {
			assert.True(t, models.IsErrApprovalNotPending(errs[0]), "round %d: got %T %v", round, errs[0], errs[0])
		}
		assert.Equal(t, models.ApprovalStatusRejected, requestState(t, req.ID).Status)
		assert.EqualValues(t, 1, statusOf(t, task.ID))
		assert.EqualValues(t, 0, historyTo(t, task.ID, 5))
	}
}

func TestApprovalConcurrency_ParallelRequestsForTheSameTargetCreateOne(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6)
	for round := 0; round < concurrencyRounds; round++ {
		task := newTask(t, projectWorkflow, "asked twice", user1)
		ids := make([]int64, 3)
		errs := race(
			func() (err error) { ids[0], err = tryRequestDropping(task.ID, user1); return },
			func() (err error) { ids[1], err = tryRequestDropping(task.ID, user2); return },
			func() (err error) { ids[2], err = tryRequestDropping(task.ID, user1); return },
		)
		for i, err := range errs {
			require.NoError(t, err, "round %d, caller %d", round, i)
		}
		assert.Equal(t, ids[0], ids[1])
		assert.Equal(t, ids[1], ids[2])
		assert.Len(t, approvalRows(t, task.ID), 1, "round %d", round)
	}
}
