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

package migration

import (
	"slices"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"src.techknowlogick.com/xormigrate"
	"xorm.io/xorm"
)

// The migrations that only create the tables of a module (custom fields, workflows, automation, dashboards, automation
// secrets) run on an empty schema: the tables and the columns the code relies on exist, the unique indexes that guard
// the invariants of the module hold, running the migration again is harmless and the rollback removes the tables.
func TestForkTableMigrations(t *testing.T) {
	now := time.Now()
	one := int64(1)

	cases := []struct {
		id      string
		tables  map[string][]string
		beans   []any
		inserts func(t *testing.T, x *xorm.Engine)
	}{
		{
			id: "20261122120000",
			tables: map[string][]string{
				"custom_fields":            {"project_id", "title", "type", "required", "config", "position", "created_by_id"},
				"custom_field_options":     {"custom_field_id", "title", "color", "position"},
				"task_custom_field_values": {"task_id", "custom_field_id", "value_text", "value_number", "value_date", "value_bool", "option_id", "user_id"},
			},
			beans: []any{customFields20261122120000{}, customFieldOptions20261122120000{}, taskCustomFieldValues20261122120000{}},
		},
		{
			id: "20261122130000",
			tables: map[string][]string{
				"workflows":                           {"project_id", "restrict_transitions"},
				"workflow_statuses":                   {"workflow_id", "title", "color", "category", "position", "is_initial", "archived"},
				"workflow_transitions":                {"workflow_id", "from_status_id", "to_status_id", "min_permission", "require_comment"},
				"workflow_transition_required_fields": {"transition_id", "field"},
				"workflow_transition_teams":           {"transition_id", "team_id"},
				"task_statuses":                       {"task_id", "workflow_id", "status_id", "entered"},
				"task_status_history":                 {"task_id", "from_status_id", "to_status_id", "from_title", "to_title", "doer_id", "comment"},
			},
			beans: []any{workflows20261122130000{}, workflowStatuses20261122130000{}, workflowTransitions20261122130000{},
				workflowTransitionRequiredFields20261122130000{}, workflowTransitionTeams20261122130000{}, taskStatuses20261122130000{},
				taskStatusHistory20261122130000{}},
			inserts: func(t *testing.T, x *xorm.Engine) {
				_, err := x.Insert(&workflows20261122130000{ProjectID: 7})
				require.NoError(t, err)
				_, err = x.Insert(&workflows20261122130000{ProjectID: 7})
				require.Error(t, err, "a project has at most one workflow")

				_, err = x.Insert(&workflowTransitions20261122130000{WorkflowID: 1, FromStatusID: 1, ToStatusID: 2, MinPermission: "write"})
				require.NoError(t, err)
				_, err = x.Insert(&workflowTransitions20261122130000{WorkflowID: 1, FromStatusID: 1, ToStatusID: 2, MinPermission: "admin"})
				require.Error(t, err, "a pair of statuses has at most one transition")
				_, err = x.Insert(&workflowTransitions20261122130000{WorkflowID: 1, FromStatusID: 2, ToStatusID: 1, MinPermission: "write"})
				require.NoError(t, err, "the reverse pair is another transition")

				_, err = x.Insert(&workflowTransitionTeams20261122130000{TransitionID: 1, TeamID: 3})
				require.NoError(t, err)
				_, err = x.Insert(&workflowTransitionTeams20261122130000{TransitionID: 1, TeamID: 3})
				require.Error(t, err, "a team is listed once per transition")

				_, err = x.Insert(&taskStatuses20261122130000{TaskID: 9, WorkflowID: 1, StatusID: 1, Entered: now})
				require.NoError(t, err)
				_, err = x.Insert(&taskStatuses20261122130000{TaskID: 9, WorkflowID: 1, StatusID: 2, Entered: now})
				require.Error(t, err, "a task has one status")
			},
		},
		{
			id: "20261122140000",
			tables: map[string][]string{
				"automation_rules":     {"project_id", "title", "enabled", "revision", "owner_id", "trigger", "conditions", "actions", "disabled_reason", "consecutive_failures"},
				"automation_events":    {"project_id", "task_id", "name", "payload", "depth", "status", "attempts", "run_after", "locked_until"},
				"automation_runs":      {"rule_id", "rule_revision", "event_id", "project_id", "status", "skip_reason", "error", "started", "finished"},
				"automation_run_steps": {"run_id", "position", "action_type", "status", "error", "output"},
			},
			beans: []any{automationRules20261122140000{}, automationEvents20261122140000{}, automationRuns20261122140000{}, automationRunSteps20261122140000{}},
			inserts: func(t *testing.T, x *xorm.Engine) {
				_, err := x.Insert(&automationRuns20261122140000{RuleID: 1, EventID: 5, ProjectID: 1, Status: "ok", Started: now})
				require.NoError(t, err)
				_, err = x.Insert(&automationRuns20261122140000{RuleID: 1, EventID: 5, ProjectID: 1, Status: "ok", Started: now})
				require.Error(t, err, "an event runs a rule once")
				_, err = x.Insert(&automationRuns20261122140000{RuleID: 2, EventID: 5, ProjectID: 1, Status: "ok", Started: now})
				require.NoError(t, err, "another rule runs the same event")

				_, err = x.Insert(&automationRunSteps20261122140000{RunID: 1, Position: 0, ActionType: "x", Status: "ok"})
				require.NoError(t, err)
				_, err = x.Insert(&automationRunSteps20261122140000{RunID: 1, Position: 0, ActionType: "x", Status: "ok"})
				require.Error(t, err, "a run has one step per position")
			},
		},
		{
			id: "20261122160000",
			tables: map[string][]string{
				"dashboards":        {"title", "owner_id", "project_id", "position"},
				"dashboard_widgets": {"dashboard_id", "type", "title", "config"},
			},
			beans: []any{dashboards20261122160000{}, dashboardWidgets20261122160000{}},
		},
		{
			id:     "20261123100000",
			tables: map[string][]string{"automation_secrets": {"project_id", "name", "value", "created_by_id"}},
			beans:  []any{automationSecrets20261123100000{}},
			inserts: func(t *testing.T, x *xorm.Engine) {
				_, err := x.Insert(&automationSecrets20261123100000{ProjectID: 1, Name: "KEY", Value: "a", CreatedByID: one})
				require.NoError(t, err)
				_, err = x.Insert(&automationSecrets20261123100000{ProjectID: 1, Name: "KEY", Value: "b", CreatedByID: one})
				require.Error(t, err, "a secret name is unique in a project")
				_, err = x.Insert(&automationSecrets20261123100000{ProjectID: 2, Name: "KEY", Value: "b", CreatedByID: one})
				require.NoError(t, err, "another project may use the name")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			x, err := db.CreateTestEngine()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, x.DropTables(tc.beans...)) })
			require.NoError(t, x.DropTables(tc.beans...))

			idx := slices.IndexFunc(migrations, func(m *xormigrate.Migration) bool { return m.ID == tc.id })
			require.NotEqual(t, -1, idx)
			m := migrations[idx]

			columns := func() map[string][]string {
				metas, err := x.DBMetas()
				require.NoError(t, err)
				found := map[string][]string{}
				for _, meta := range metas {
					if _, ok := tc.tables[meta.Name]; ok {
						found[meta.Name] = meta.ColumnsSeq()
					}
				}
				return found
			}
			require.Empty(t, columns(), "the tables do not exist before the migration")

			require.NoError(t, m.Migrate(x))
			found := columns()
			for table, want := range tc.tables {
				require.Contains(t, found, table)
				for _, column := range want {
					assert.Contains(t, found[table], column, "%s.%s", table, column)
				}
			}
			if tc.inserts != nil {
				tc.inserts(t, x)
			}

			require.NoError(t, m.Migrate(x), "running the migration again on its own tables is harmless")

			require.NoError(t, m.Rollback(x))
			assert.Empty(t, columns(), "the rollback removes the tables")
		})
	}
}
