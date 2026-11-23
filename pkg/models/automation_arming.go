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
	"time"

	"code.vikunja.io/api/pkg/automation/spec"

	"xorm.io/builder"
	"xorm.io/xorm"
)

const (
	armingSeedPage = 500
	// armingSeedMax bounds the work of arming a rule. Tasks beyond it have no marker and fire once, at the pace
	// of the ticker caps.
	armingSeedMax = 20000
)

// seedAutomationDueMarkers marks the tasks of the project that already are overdue (overdue trigger) or inside the
// due soon window (due soon trigger) at the moment the rule is armed, with their current due date. The ticker fires
// for every task without a marker for its due date, so what the rule finds when it starts is not replayed, while a
// due date edited afterwards (into the window, or into the past) is a new date that fires. Markers are written
// page by page in the transaction of the save, at most armingSeedMax of them.
func seedAutomationDueMarkers(s *xorm.Session, r *AutomationRule, at time.Time) error {
	var from, to time.Time
	switch r.Trigger.Type {
	case spec.TriggerTaskOverdue:
		to = at
	case spec.TriggerTaskDueSoon:
		offsetText, _ := r.Trigger.Options["offset"].(string)
		offset, err := spec.ParseOffset(offsetText)
		if err != nil {
			return err
		}
		from, to = at, at.Add(offset)
	default:
		return nil
	}

	cond := builder.And(
		builder.Eq{"project_id": r.ProjectID, "done": false},
		builder.NotNull{"due_date"},
		builder.Lte{"due_date": to.UTC().Format(dbTimeFormat)},
	)
	if !from.IsZero() {
		cond = builder.And(cond, builder.Gt{"due_date": from.UTC().Format(dbTimeFormat)})
	}

	var lastID int64
	for seeded := 0; seeded < armingSeedMax; {
		tasks := []*Task{}
		if err := s.Where(cond).And("id > ?", lastID).OrderBy("id ASC").Limit(armingSeedPage).Cols("id", "due_date").Find(&tasks); err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}
		lastID = tasks[len(tasks)-1].ID
		seeded += len(tasks)

		ids := make([]int64, 0, len(tasks))
		for _, t := range tasks {
			ids = append(ids, t.ID)
		}
		existing := []*AutomationDueMarker{}
		if err := s.Where(builder.Eq{"rule_id": r.ID}).And(builder.In("task_id", ids)).Find(&existing); err != nil {
			return err
		}
		known := make(map[int64]*AutomationDueMarker, len(existing))
		for _, m := range existing {
			known[m.TaskID] = m
		}
		fresh := []*AutomationDueMarker{}
		for _, t := range tasks {
			m, ok := known[t.ID]
			if !ok {
				fresh = append(fresh, &AutomationDueMarker{RuleID: r.ID, TaskID: t.ID, DueDate: t.DueDate, Fired: at})
				continue
			}
			if m.DueDate.Equal(t.DueDate) {
				continue
			}
			if _, err := s.ID(m.ID).Cols("due_date", "fired").Update(&AutomationDueMarker{DueDate: t.DueDate, Fired: at}); err != nil {
				return err
			}
		}
		if len(fresh) > 0 {
			if _, err := s.Insert(&fresh); err != nil {
				return err
			}
		}
	}
	return nil
}
