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
	"xorm.io/builder"
	"xorm.io/xorm"
)

// TaskStatusSummary is the compact form of the workflow status of a task that is embedded in tasks when
// the `status` expand option is set.
type TaskStatusSummary struct {
	ID       int64                  `json:"id" doc:"The id of the workflow status."`
	Title    string                 `json:"title" doc:"The title of the status."`
	Color    string                 `json:"color" doc:"The hex color of the status, empty when it has none."`
	Category WorkflowStatusCategory `json:"category" enum:"open,in_progress,done,cancelled" doc:"The category of the status."`
}

// addStatusToTasks attaches the workflow status to the given tasks with two queries (status rows and status
// definitions), regardless of the number of tasks. Tasks without a status row stay without one.
func addStatusToTasks(s *xorm.Session, taskIDs []int64, taskMap map[int64]*Task) error {
	if len(taskIDs) == 0 {
		return nil
	}

	rows := []*TaskStatus{}
	if err := s.Where(builder.In("task_id", taskIDs)).Find(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	idSet := make(map[int64]struct{})
	for _, r := range rows {
		idSet[r.StatusID] = struct{}{}
	}
	statusIDs := make([]int64, 0, len(idSet))
	for id := range idSet {
		statusIDs = append(statusIDs, id)
	}

	list := []*WorkflowStatus{}
	if err := s.Where(builder.In("id", statusIDs)).Find(&list); err != nil {
		return err
	}
	statuses := make(map[int64]*WorkflowStatus, len(list))
	for _, st := range list {
		statuses[st.ID] = st
	}

	for _, r := range rows {
		task, has := taskMap[r.TaskID]
		st, found := statuses[r.StatusID]
		if !has || !found {
			continue
		}
		task.Status = &TaskStatusSummary{ID: st.ID, Title: st.Title, Color: st.Color, Category: st.Category}
	}
	return nil
}
