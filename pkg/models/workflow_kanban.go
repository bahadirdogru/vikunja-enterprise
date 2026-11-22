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
	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

// A kanban view in status mode has no stored buckets. Its columns are the non-archived statuses of the
// workflow of the project and the bucket id of a column is the negated status id: bucket -7 is status 7.
// Real bucket ids are positive, so a column can never be mistaken for a bucket of another view and a bucket
// id on its own (task creation has no view in the request) says what it means.

// statusBucketID is the bucket id of the column of a status.
func statusBucketID(statusID int64) int64 {
	return -statusID
}

// statusIDOfBucket returns the status a bucket id stands for.
func statusIDOfBucket(bucketID int64) (statusID int64, isStatusBucket bool) {
	if bucketID >= 0 {
		return 0, false
	}
	return -bucketID, true
}

func (d *workflowDef) statusBucket(view *ProjectView, st *WorkflowStatus) *Bucket {
	return &Bucket{
		ID:            statusBucketID(st.ID),
		Title:         st.Title,
		ProjectViewID: view.ID,
		Position:      st.Position,
		Created:       d.workflow.Created,
		Updated:       d.workflow.Updated,
		Status:        &TaskStatusSummary{ID: st.ID, Title: st.Title, Color: st.Color, Category: st.Category},
	}
}

// statusBucketsForView derives the columns of a status mode view. A project whose workflow is gone has
// no columns.
func statusBucketsForView(s *xorm.Session, view *ProjectView) ([]*Bucket, error) {
	buckets := []*Bucket{}
	d, err := loadWorkflowDef(s, view.ProjectID)
	if err != nil || d == nil {
		return buckets, err
	}
	for _, st := range d.workflow.Statuses {
		if st.Archived {
			continue
		}
		buckets = append(buckets, d.statusBucket(view, st))
	}
	return buckets, nil
}

// validateStatusModeOnCreate rejects a new status mode view of a project without a workflow.
func validateStatusModeOnCreate(s *xorm.Session, p *ProjectView) error {
	if p.ViewKind != ProjectViewKindKanban || p.BucketConfigurationMode != BucketConfigurationModeStatus {
		return nil
	}
	return requireWorkflowForStatusView(s, p.ProjectID)
}

// validateStatusModeOnUpdate checks an update that leaves the view in status mode. Entering the mode needs a
// workflow; a view that already has the mode keeps working without one (it shows no columns) so it can still
// be renamed or switched. The bucket ids cannot be set, there are no buckets.
func validateStatusModeOnUpdate(s *xorm.Session, pv, oldView *ProjectView) error {
	if oldView.ViewKind != ProjectViewKindKanban || oldView.BucketConfigurationMode != BucketConfigurationModeStatus {
		if err := requireWorkflowForStatusView(s, pv.ProjectID); err != nil {
			return err
		}
	}
	// Echoing the stored ids (or none) is fine, setting others is not.
	if (pv.DefaultBucketID != 0 && pv.DefaultBucketID != oldView.DefaultBucketID) ||
		(pv.DoneBucketID != 0 && pv.DoneBucketID != oldView.DoneBucketID) {
		return ErrStatusViewBucketsManaged{ProjectViewID: pv.ID}
	}
	return nil
}

func requireWorkflowForStatusView(s *xorm.Session, projectID int64) error {
	if projectID <= 0 {
		return ErrWorkflowRequiredForStatusView{ProjectID: projectID}
	}
	exists, err := s.Where("project_id = ?", projectID).Exist(&Workflow{})
	if err != nil {
		return err
	}
	if !exists {
		return ErrWorkflowRequiredForStatusView{ProjectID: projectID}
	}
	return nil
}

// statusModeViewOf returns the view when it exists and is in status mode.
func statusModeViewOf(s *xorm.Session, projectID, viewID int64) (*ProjectView, bool) {
	view, err := GetProjectViewByIDAndProject(s, viewID, projectID)
	if err != nil || view.ViewKind != ProjectViewKindKanban || view.BucketConfigurationMode != BucketConfigurationModeStatus {
		return nil, false
	}
	return view, true
}

// canUpdateInStatusView is TaskBucket.CanUpdate for a status mode view.
func (b *TaskBucket) canUpdateInStatusView(s *xorm.Session, a web.Auth, view *ProjectView) (bool, error) {
	if _, is := statusIDOfBucket(b.BucketID); !is {
		return false, ErrBucketDoesNotExist{BucketID: b.BucketID}
	}
	can, err := canWriteBucketProject(s, view.ProjectID, a)
	if err != nil || !can {
		return false, err
	}
	can, err = (&Task{ID: b.TaskID}).CanWrite(s, a)
	if err != nil || !can {
		return false, err
	}
	return taskFitsView(s, a, view, b.TaskID)
}

// moveToStatusColumn moves a task into the column of a status view by performing the workflow transition as the
// current user, with the same checks and typed errors as POST /tasks/{task}/status. Placing a task in the column
// it is already in is a no-op, which is how reordering inside a column works.
func (b *TaskBucket) moveToStatusColumn(s *xorm.Session, a web.Auth, view *ProjectView) error {
	statusID, is := statusIDOfBucket(b.BucketID)
	if !is {
		return ErrBucketDoesNotExist{BucketID: b.BucketID}
	}

	d, err := loadWorkflowDef(s, view.ProjectID)
	if err != nil {
		return err
	}
	if d == nil {
		return ErrWorkflowDoesNotExist{ProjectID: view.ProjectID}
	}

	task := &Task{ID: b.TaskID}
	if err = task.ReadOne(s, a); err != nil {
		return err
	}

	change := &TaskStatusChange{TaskID: task.ID, ToStatusID: statusID, task: task}
	if err = change.Create(s, a); err != nil {
		return err
	}

	st := d.statuses[statusID]
	count, err := s.Where("status_id = ?", statusID).Count(&TaskStatus{})
	if err != nil {
		return err
	}
	b.Task = task
	b.Bucket = d.statusBucket(view, st)
	b.Bucket.Count = count
	return nil
}

// validateStatusColumnOfNewTask checks the status column a new task asks for, before anything is written.
func validateStatusColumnOfNewTask(s *xorm.Session, projectID, bucketID int64) error {
	statusID, _ := statusIDOfBucket(bucketID)
	d, err := loadWorkflowDef(s, projectID)
	if err != nil {
		return err
	}
	if d == nil {
		return ErrBucketDoesNotExist{BucketID: bucketID}
	}
	st, exists := d.statuses[statusID]
	if !exists {
		return ErrWorkflowStatusDoesNotExist{StatusID: statusID}
	}
	if st.Archived {
		return ErrWorkflowStatusArchived{StatusID: statusID}
	}
	return nil
}

// moveNewTaskToStatusColumn runs after a task was created in a status column. The task starts in the initial
// status; when the column is another one the transition is attempted. A rejected transition does not undo the
// creation: the rejection is kept on the task for the route to return, see StatusColumnError.
func (t *Task) moveNewTaskToStatusColumn(s *xorm.Session, a web.Auth) error {
	statusID, is := statusIDOfBucket(t.BucketID)
	if !is {
		return nil
	}
	t.BucketID = 0

	row, err := getTaskStatusRow(s, t.ID)
	if err != nil {
		return err
	}
	if row != nil {
		t.BucketID = statusBucketID(row.StatusID)
		if row.StatusID == statusID {
			return nil
		}
	}

	change := &TaskStatusChange{TaskID: t.ID, ToStatusID: statusID, task: t}
	err = change.Create(s, a)
	switch {
	case err == nil:
		t.BucketID = statusBucketID(statusID)
		return nil
	case IsErrWorkflowTransitionNotAllowed(err), IsErrWorkflowTransitionForbidden(err), IsErrWorkflowRequirementsNotMet(err):
		t.statusColumnErr = err
		return nil
	}
	return err
}

// StatusColumnError is the workflow error of the transition a task creation in a status column attempted
// after the task was stored, nil when there was none. The task exists in the initial status then.
func (t *Task) StatusColumnError() error {
	return t.statusColumnErr
}
