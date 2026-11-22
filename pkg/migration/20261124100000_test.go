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

	"code.vikunja.io/api/pkg/db"

	"github.com/stretchr/testify/require"
	"src.techknowlogick.com/xormigrate"
	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

func tableMeta20261124100000(t *testing.T, x *xorm.Engine, name string) *schemas.Table {
	t.Helper()
	tables, err := x.DBMetas()
	require.NoError(t, err)
	for _, table := range tables {
		if table.Name == name {
			return table
		}
	}
	t.Fatalf("table %s not found", name)
	return nil
}

func TestApprovalsMigration20261124100000(t *testing.T) {
	x, err := db.CreateTestEngine()
	require.NoError(t, err)

	old := []any{workflowTransitions20261122130000{}, taskStatusHistory20261122130000{}}
	created := []any{approvalRequests20261124100000{}, approvalApprovers20261124100000{}}
	drop := func() {
		require.NoError(t, x.DropTables(append(old, created...)...))
	}
	t.Cleanup(drop)
	drop()
	require.NoError(t, x.Sync2(old...))

	_, err = x.Insert(&workflowTransitions20261122130000{WorkflowID: 1, FromStatusID: 1, ToStatusID: 2, MinPermission: "write", RequireComment: true})
	require.NoError(t, err)
	_, err = x.Insert(&taskStatusHistory20261122130000{TaskID: 5, ToStatusID: 2, ToTitle: "Doing", Comment: "kept"})
	require.NoError(t, err)

	transitionsBefore := tableMeta20261124100000(t, x, "workflow_transitions")
	historyBefore := tableMeta20261124100000(t, x, "task_status_history")
	require.NotEmpty(t, transitionsBefore.Indexes)

	idx := slices.IndexFunc(migrations, func(m *xormigrate.Migration) bool { return m.ID == "20261124100000" })
	require.NotEqual(t, -1, idx)
	require.NoError(t, migrations[idx].Migrate(x))

	for name, before := range map[string]*schemas.Table{"workflow_transitions": transitionsBefore, "task_status_history": historyBefore} {
		after := tableMeta20261124100000(t, x, name)
		for _, column := range before.ColumnsSeq() {
			require.NotNilf(t, after.GetColumn(column), "%s: the migration dropped column %s", name, column)
		}
		for indexName, index := range before.Indexes {
			preserved, found := after.Indexes[indexName]
			require.Truef(t, found, "%s: the migration dropped index %s", name, indexName)
			require.Equal(t, index.Type, preserved.Type)
			require.Equal(t, index.Cols, preserved.Cols)
		}
	}
	require.NotNil(t, tableMeta20261124100000(t, x, "workflow_transitions").GetColumn("requires_approval"))
	require.NotNil(t, tableMeta20261124100000(t, x, "workflow_transitions").GetColumn("approval"))
	require.NotNil(t, tableMeta20261124100000(t, x, "task_status_history").GetColumn("approval_request_id"))

	// Rows survive and get the defaults.
	transition := &struct {
		ID               int64 `xorm:"pk"`
		RequireComment   bool
		RequiresApproval bool
		Approval         *string
	}{}
	found, err := x.Table("workflow_transitions").Get(transition)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, transition.RequireComment)
	require.False(t, transition.RequiresApproval)
	require.Nil(t, transition.Approval)

	history := &struct {
		ID                int64 `xorm:"pk"`
		Comment           string
		ApprovalRequestID *int64
	}{}
	found, err = x.Table("task_status_history").Get(history)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "kept", history.Comment)
	require.Nil(t, history.ApprovalRequestID)

	// The pair of the unique index of transitions is still enforced.
	_, err = x.Insert(&workflowTransitions20261122130000{WorkflowID: 1, FromStatusID: 1, ToStatusID: 2, MinPermission: "write"})
	require.Error(t, err)

	// The new tables exist; an approver can be asked once per request.
	approvers := tableMeta20261124100000(t, x, "approval_approvers")
	require.NotNil(t, approvers.GetColumn("decision"))
	_, err = x.Insert(&approvalApprovers20261124100000{RequestID: 1, UserID: 2})
	require.NoError(t, err)
	_, err = x.Insert(&approvalApprovers20261124100000{RequestID: 1, UserID: 2})
	require.Error(t, err, "the unique index rejects a second row for the same approver")
	requests := tableMeta20261124100000(t, x, "approval_requests")
	for _, column := range []string{"task_id", "project_id", "transition_id", "from_status_id", "to_status_id", "rejected_status_id", "title", "mode", "status", "requested_by_id", "due", "reminded_at", "cancel_reason", "resolved"} {
		require.NotNilf(t, requests.GetColumn(column), "approval_requests.%s", column)
	}
}
