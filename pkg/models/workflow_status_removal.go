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
	"slices"
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// moveTasksBetweenStatuses moves all tasks of a status that is removed from the workflow to another status, as the
// status changes they are: a history row (cause status_removed, the doer is the person who saved the workflow) and a
// task.status.changed event for every task, and the done flag follows the category.
//
// It does what the normal path (runStatusChange) does, with these exceptions, which are the price of doing it in
// bulk:
//   - the rules of the transition, the approval requirement and the required fields are not checked: the move is
//     forced, nobody asked for it. Requests for the removed status are cancelled before (cancelApprovalsForStatuses).
//   - a task that is soft-deleted is moved and gets its history row and done flag, but no event.
//
// Tasks that need more than a bulk update go through the normal path one by one, with everything it does: a
// repeating task that enters a done status (its dates shift and it is reopened in the initial status instead of
// staying done), and, when the project has a kanban view with manual buckets, a task whose done flag changes (it
// moves to the done bucket or back). Every other task costs no statement of its own.
//
// Cost for N tasks, in the transaction of the removal, per chunk of workflowBulkChunkSize tasks: one read of the
// tasks, one update of the status rows, one insert of the history rows, one update of the done flags and one of the
// update times: at most 5 statements per chunk. Every event is passed to DispatchOnCommit, which runs the
// transactional hooks synchronously: with automation enabled that is one more insert (automation_events, with the
// payload of the task) per event, whether a rule listens or not, and the session keeps all events (N, 2N when done
// changes) in memory until the commit.
//
// A status with more than maxTasksMovedWithAStatus tasks is not moved: the request is refused with
// ErrWorkflowStatusMoveTooLarge and nothing is written (the transaction rolls back), so the admin first moves
// tasks to other statuses in smaller batches. The events cannot be dropped to make a big move cheaper, and a move of
// that size is better seen than started by accident. The limit is a variable only for the tests.
var maxTasksMovedWithAStatus int64 = 20000

func moveTasksBetweenStatuses(s *xorm.Session, a web.Auth, projectID int64, from, to *WorkflowStatus, removedIDs []int64) error {
	// Lock the tasks before anything else is read, in one statement, like lockApprovalRequest: a task that is
	// changed at the same moment holds this lock first (it takes the task, then the status row), so after this
	// the list of tasks in the status is the truth, nobody moves one out of it unseen, and no history row or event
	// is written for a task that left the status meanwhile.
	if _, err := s.Exec("UPDATE tasks SET id = id WHERE id IN (SELECT task_id FROM task_statuses WHERE status_id = ?)", from.ID); err != nil {
		return err
	}
	taskIDs := []int64{}
	if err := lockingSession(s).Table("task_statuses").Where("status_id = ?", from.ID).OrderBy("task_id ASC").Cols("task_id").Find(&taskIDs); err != nil {
		return err
	}
	if len(taskIDs) == 0 {
		return nil
	}
	if int64(len(taskIDs)) > maxTasksMovedWithAStatus {
		return ErrWorkflowStatusMoveTooLarge{StatusID: from.ID, Title: from.Title, Tasks: int64(len(taskIDs)), Limit: maxTasksMovedWithAStatus}
	}

	m := &forcedStatusMove{
		s: s, a: a, projectID: projectID, from: from, to: to, removedIDs: removedIDs,
		toFinal:     to.Category.isFinal(),
		doneChanges: from.Category.isFinal() != to.Category.isFinal(),
		doer:        doerFromAuth(s, a),
	}
	if m.doneChanges {
		if err := s.Where(builder.And(
			builder.Eq{"project_id": projectID},
			builder.Eq{"view_kind": ProjectViewKindKanban},
			builder.Eq{"bucket_configuration_mode": BucketConfigurationModeManual},
		)).Find(&m.manualViews); err != nil {
			return err
		}
	}
	for _, chunk := range chunkInt64s(taskIDs) {
		if err := m.moveChunk(chunk); err != nil {
			return err
		}
	}
	return nil
}

// forcedStatusMove is the state of one moveTasksBetweenStatuses.
type forcedStatusMove struct {
	s           *xorm.Session
	a           web.Auth
	projectID   int64
	from, to    *WorkflowStatus
	removedIDs  []int64
	toFinal     bool
	doneChanges bool
	manualViews []*ProjectView
	doer        *user.User
	d           *workflowDef
}

// needsNormalPath reports whether a task has to go through the normal status change.
func (m *forcedStatusMove) needsNormalPath(t *Task) bool {
	if t == nil {
		return false
	}
	return (m.doneChanges && t.Done != m.toFinal && len(m.manualViews) > 0) ||
		(m.to.Category == WorkflowCategoryDone && t.isRepeating() && !t.Done)
}

func (m *forcedStatusMove) moveChunk(chunk []int64) error {
	// Soft-deleted tasks are not in this list.
	tasks := []*Task{}
	if err := m.s.Where(builder.In("id", chunk)).Find(&tasks); err != nil {
		return err
	}
	byID := make(map[int64]*Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}

	bulk := make([]int64, 0, len(chunk))
	for _, id := range chunk {
		t := byID[id]
		if !m.needsNormalPath(t) {
			bulk = append(bulk, id)
			continue
		}
		if m.d == nil {
			var err error
			if m.d, err = loadWorkflowDef(m.s, m.projectID); err != nil {
				return err
			}
			// The workflow is read in the middle of its update: a status that is being removed still has its
			// initial flag, and it sorts first. A repeating task is reopened in the initial status, which has to
			// be the one of the submitted document, so the statuses that go away are no candidates.
			for _, st := range m.d.workflow.Statuses {
				if slices.Contains(m.removedIDs, st.ID) {
					st.IsInitial = false
				}
			}
		}
		if _, err := m.d.runStatusChange(m.s, m.a, &statusChange{Task: t, ToStatusID: m.to.ID, SkipChecks: true, Cause: historyCauseStatusRemoved}); err != nil {
			return err
		}
	}
	if len(bulk) == 0 {
		return nil
	}
	return m.moveInBulk(bulk, byID)
}

func (m *forcedStatusMove) moveInBulk(bulk []int64, byID map[int64]*Task) error {
	now := time.Now()
	if _, err := m.s.Where(builder.And(builder.In("task_id", bulk), builder.Eq{"status_id": m.from.ID})).
		Cols("status_id", "entered").
		Update(&TaskStatus{StatusID: m.to.ID, Entered: now}); err != nil {
		return err
	}
	history := make([]*TaskStatusHistory, 0, len(bulk))
	for _, id := range bulk {
		h := newHistoryEntry(id, m.from, m.to, m.a, "")
		h.Comment = historyCauseMarker + historyCauseStatusRemoved
		history = append(history, h)
	}
	if err := insertInChunks(m.s, history); err != nil {
		return err
	}

	// The done flag only where it differs, like syncTaskDoneToStatus; every other task is touched so that its
	// update time shows the change, like runStatusChange does.
	flipped := []int64{}
	touched := []int64{}
	for _, id := range bulk {
		if t := byID[id]; m.doneChanges && (t == nil || t.Done != m.toFinal) {
			flipped = append(flipped, id)
		} else {
			touched = append(touched, id)
		}
	}
	if len(flipped) > 0 {
		if err := setDoneOfTasks(m.s, flipped, m.toFinal); err != nil {
			return err
		}
	}
	if len(touched) > 0 {
		if _, err := m.s.Unscoped().Where(builder.In("id", touched)).Cols("updated").Update(&Task{}); err != nil {
			return err
		}
	}

	flippedSet := make(map[int64]bool, len(flipped))
	for _, id := range flipped {
		flippedSet[id] = true
	}
	for _, id := range bulk {
		t := byID[id]
		if t == nil {
			continue
		}
		events.DispatchOnCommit(m.s, &TaskStatusChangedEvent{Task: t, FromStatus: m.from, ToStatus: m.to, Cause: historyCauseStatusRemoved, Doer: m.doer})
		if flippedSet[id] {
			old := *t
			updated := *t
			updated.Done = m.toFinal
			updated.DoneAt = time.Time{}
			if m.toFinal {
				updated.DoneAt = now
			}
			events.DispatchOnCommit(m.s, &TaskUpdatedEvent{Task: &updated, Doer: m.doer, OldTask: &old})
		}
	}
	return nil
}

// flipDoneOfTasks sets the done flag of tasks whose status changed category without the task moving, and announces
// it like a task update does: a task.updated event with the old and the new task for every task whose flag
// changed. Tasks that are soft-deleted get the flag but no event.
func flipDoneOfTasks(s *xorm.Session, a web.Auth, taskIDs []int64, done bool) error {
	doer := doerFromAuth(s, a)
	for _, chunk := range chunkInt64s(taskIDs) {
		tasks := []*Task{}
		if err := s.Where(builder.In("id", chunk)).Find(&tasks); err != nil {
			return err
		}
		// Only the tasks whose flag differs are written, so the done date of a task that is done stays.
		byID := make(map[int64]*Task, len(tasks))
		for _, t := range tasks {
			byID[t.ID] = t
		}
		differing := make([]int64, 0, len(chunk))
		for _, id := range chunk {
			if t := byID[id]; t == nil || t.Done != done {
				differing = append(differing, id)
			}
		}
		if len(differing) == 0 {
			continue
		}
		if err := setDoneOfTasks(s, differing, done); err != nil {
			return err
		}
		now := time.Now()
		for _, t := range tasks {
			if t.Done == done {
				continue
			}
			old := *t
			updated := *t
			updated.Done = done
			updated.DoneAt = time.Time{}
			if done {
				updated.DoneAt = now
			}
			events.DispatchOnCommit(s, &TaskUpdatedEvent{Task: &updated, Doer: doer, OldTask: &old})
		}
	}
	return nil
}
