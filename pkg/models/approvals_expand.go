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

// TaskApprovalsSummary is the compact form of the pending approvals of a task that is embedded in tasks when the
// `approvals` expand option is set, so cards can show a marker without further requests.
type TaskApprovalsSummary struct {
	HasPending   bool  `json:"has_pending" doc:"Whether the task has a pending approval request."`
	PendingCount int64 `json:"pending_count" doc:"The number of pending approval requests of the task."`
}

// addApprovalsToTasks attaches the pending approval counts with one query per chunk of tasks, regardless of how
// many tasks there are. Tasks without pending requests get a zero summary.
func addApprovalsToTasks(s *xorm.Session, taskIDs []int64, taskMap map[int64]*Task) error {
	if len(taskIDs) == 0 {
		return nil
	}
	counts := map[int64]int64{}
	for _, chunk := range chunkInt64s(taskIDs, workflowBulkChunkSize) {
		rows := []struct {
			TaskID int64 `xorm:"task_id"`
			Count  int64 `xorm:"pending_count"`
		}{}
		err := s.Table("approval_requests").
			Select("task_id, COUNT(*) AS pending_count").
			Where(builder.And(builder.In("task_id", chunk), builder.Eq{"status": ApprovalStatusPending})).
			GroupBy("task_id").
			Find(&rows)
		if err != nil {
			return err
		}
		for _, r := range rows {
			counts[r.TaskID] = r.Count
		}
	}
	for _, id := range taskIDs {
		if task, has := taskMap[id]; has {
			task.Approvals = &TaskApprovalsSummary{HasPending: counts[id] > 0, PendingCount: counts[id]}
		}
	}
	return nil
}
