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
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/require"
	"xorm.io/xorm/schemas"
)

// runDecision and runStatusChange are the request paths without testify, so that they can run in goroutines.
func runDecision(requestID int64, a web.Auth, decision ApprovalDecisionValue) error {
	s := db.NewSession()
	defer s.Close()
	fail := func(err error) error {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	d := &ApprovalDecision{RequestID: requestID, Decision: decision, Comment: "ok"}
	if can, err := d.CanCreate(s, a); err != nil || !can {
		return fail(err)
	}
	if err := d.Create(s, a); err != nil {
		return fail(err)
	}
	if err := s.Commit(); err != nil {
		return err
	}
	events.DispatchPending(context.Background(), s)
	return nil
}

func runStatusChange(taskID, toStatusID int64, a web.Auth) error {
	s := db.NewSession()
	defer s.Close()
	fail := func(err error) error {
		_ = s.Rollback()
		events.CleanupPending(s)
		return err
	}
	c := &TaskStatusChange{TaskID: taskID, ToStatusID: toStatusID}
	if can, err := c.CanCreate(s, a); err != nil || !can {
		return fail(err)
	}
	if err := c.Create(s, a); err != nil {
		return fail(err)
	}
	if err := s.Commit(); err != nil {
		return err
	}
	events.DispatchPending(context.Background(), s)
	return nil
}

// A decision and a direct status change of one task take the same two locks, the task row and the request row. They
// have to take them in the same order, otherwise PostgreSQL aborts one of them with a deadlock (40P01) and MySQL
// with 1213. Losing the race the proper way (the request is not pending any more) is fine, a database error is not.
func TestApprovals_DecisionAndStatusChangeDoNotDeadlock(t *testing.T) {
	if db.Type() != schemas.MYSQL && db.Type() != schemas.POSTGRES {
		t.Skip("Requires database row locks")
	}
	env := newApprovalEnv(t, apCfg(ApprovalModeAny, 6), nil)

	const iterations = 150
	var failures []error
	for i := 0; i < iterations; i++ {
		req := env.request(t, apU1)

		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			errs[0] = runDecision(req.ID, apU6, ApprovalDecisionApproved)
		}()
		go func() {
			defer wg.Done()
			<-start
			errs[1] = runStatusChange(env.task.ID, env.canceled, apU1)
		}()
		close(start)
		wg.Wait()

		for _, err := range errs {
			var httpErr web.HTTPErrorProcessor
			if err != nil && !errors.As(err, &httpErr) {
				failures = append(failures, err)
			}
		}

		// Back to the start for the next round.
		s := db.NewSession()
		require.NoError(t, deleteApprovalsForTask(s, env.task.ID))
		_, err := s.Where("task_id = ?", env.task.ID).Cols("status_id").Update(&TaskStatus{StatusID: env.todo})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
	}
	require.Empty(t, failures, "%d of %d rounds failed with a database error, first: %v", len(failures), iterations, firstErr(failures))
}

func firstErr(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}
