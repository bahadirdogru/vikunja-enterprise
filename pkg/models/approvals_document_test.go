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

	"code.vikunja.io/api/pkg/db"
	"xorm.io/builder"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovals_Document(t *testing.T) {
	setup := func(t *testing.T) (field *CustomField, textField *CustomField) {
		t.Helper()
		db.LoadAndAssertFixtures(t)
		insertRow(t, &ProjectUser{UserID: 2, ProjectID: apProject, Permission: PermissionWrite})
		insertRow(t, &TeamProject{TeamID: 1, ProjectID: apProject, Permission: PermissionRead})
		field = &CustomField{ProjectID: apProject, Title: "Editor", Type: CustomFieldTypeUser, CreatedByID: 6}
		textField = &CustomField{ProjectID: apProject, Title: "Note", Type: CustomFieldTypeText, CreatedByID: 6}
		insertRow(t, field)
		insertRow(t, textField)
		return field, textField
	}
	docWith := func(tr *WorkflowTransition) *Workflow {
		doc := baseWorkflowDoc(apProject)
		doc.Statuses = append(doc.Statuses, &WorkflowStatus{ID: -5, Title: "Rejected", Category: WorkflowCategoryOpen})
		tr.FromStatusID, tr.ToStatusID = -1, -2
		doc.Transitions = []*WorkflowTransition{tr}
		return doc
	}

	t.Run("the approval configuration round-trips through the document", func(t *testing.T) {
		field, _ := setup(t)
		doc := docWith(&WorkflowTransition{
			Title:            "Send to print",
			RequiresApproval: true,
			Approval: &TransitionApproval{
				Mode: ApprovalModeAll,
				Approvers: []ApproverSource{
					{Type: ApproverSourceUser, ID: 6},
					{Type: ApproverSourceUser, ID: 6}, // duplicate
					{Type: ApproverSourceTeam, ID: 1},
					{Type: ApproverSourceAssignees, ID: 77}, // the id is not used
					{Type: ApproverSourceCustomField, ID: field.ID},
				},
				RejectedStatusID: -5,
				AllowSelf:        true,
				DueIn:            "2w",
			},
		})
		saved := mustSaveWorkflow(t, doc, apU6)
		tr := saved.Transitions[0]
		assert.True(t, tr.RequiresApproval)
		require.NotNil(t, tr.Approval)
		assert.Equal(t, ApprovalModeAll, tr.Approval.Mode)
		assert.Equal(t, []ApproverSource{
			{Type: ApproverSourceUser, ID: 6},
			{Type: ApproverSourceTeam, ID: 1},
			{Type: ApproverSourceAssignees},
			{Type: ApproverSourceCustomField, ID: field.ID},
		}, tr.Approval.Approvers)
		assert.Equal(t, statusIDByTitle(saved, "Rejected"), tr.Approval.RejectedStatusID)
		assert.True(t, tr.Approval.AllowSelf)
		assert.Equal(t, "2w", tr.Approval.DueIn)

		read := readWorkflow(t, apProject)
		assert.Equal(t, tr.Approval, read.Transitions[0].Approval, "what is read is what was written")
		assert.True(t, read.Transitions[0].RequiresApproval)
	})

	t.Run("the mode defaults to any and the other transitions have no approval", func(t *testing.T) {
		setup(t)
		doc := docWith(&WorkflowTransition{RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceUser, ID: 6})})
		doc.Transitions = append(doc.Transitions, &WorkflowTransition{FromStatusID: -2, ToStatusID: -3})
		saved := mustSaveWorkflow(t, doc, apU6)
		assert.Equal(t, ApprovalModeAny, saved.Transitions[0].Approval.Mode)
		assert.False(t, saved.Transitions[1].RequiresApproval)
		assert.Nil(t, saved.Transitions[1].Approval)
	})

	t.Run("a transition without requires_approval stores no configuration", func(t *testing.T) {
		setup(t)
		doc := docWith(&WorkflowTransition{RequiresApproval: false, Approval: apCfg(ApprovalModeAll, 6)})
		saved := mustSaveWorkflow(t, doc, apU6)
		assert.Nil(t, saved.Transitions[0].Approval)
		assert.Nil(t, readWorkflow(t, apProject).Transitions[0].Approval)
	})

	t.Run("updating a kept transition changes its approval and keeps its id", func(t *testing.T) {
		setup(t)
		saved := mustSaveWorkflow(t, docWith(&WorkflowTransition{RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6)}), apU6)
		id := saved.Transitions[0].ID

		saved.Transitions[0].Approval = apCfg(ApprovalModeAll, 2)
		saved.Transitions[0].Approval.DueIn = "12h"
		again := mustSaveWorkflow(t, saved, apU6)
		assert.Equal(t, id, again.Transitions[0].ID)
		assert.Equal(t, ApprovalModeAll, again.Transitions[0].Approval.Mode)
		assert.Equal(t, "12h", again.Transitions[0].Approval.DueIn)
		assert.Equal(t, apUsers(2), again.Transitions[0].Approval.Approvers)

		again.Transitions[0].RequiresApproval = false
		off := mustSaveWorkflow(t, again, apU6)
		assert.False(t, off.Transitions[0].RequiresApproval)
		assert.Nil(t, off.Transitions[0].Approval)
	})

	t.Run("invalid configurations are refused", func(t *testing.T) {
		field, textField := setup(t)
		other := &CustomField{ProjectID: 1, Title: "Elsewhere", Type: CustomFieldTypeUser, CreatedByID: 1}
		insertRow(t, other)

		cases := map[string]*WorkflowTransition{
			"no configuration":                    {RequiresApproval: true},
			"no sources":                          {RequiresApproval: true, Approval: &TransitionApproval{}},
			"unknown mode":                        {RequiresApproval: true, Approval: &TransitionApproval{Mode: "some", Approvers: apUsers(6)}},
			"unknown source type":                 {RequiresApproval: true, Approval: apSources(ApproverSource{Type: "group", ID: 1})},
			"user without an id":                  {RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceUser})},
			"user without access":                 {RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceUser, ID: 4})},
			"user that is unknown":                {RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceUser, ID: 99999})},
			"team that is unknown":                {RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceTeam, ID: 99999})},
			"team without access":                 {RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceTeam, ID: 9})},
			"text field":                          {RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceCustomField, ID: textField.ID})},
			"field of other project":              {RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceCustomField, ID: other.ID})},
			"field that is unknown":               {RequiresApproval: true, Approval: apSources(ApproverSource{Type: ApproverSourceCustomField, ID: 99999})},
			"rejected status of another workflow": {RequiresApproval: true, Approval: &TransitionApproval{Approvers: apUsers(6), RejectedStatusID: 99999}},
			"rejected status is the target":       {RequiresApproval: true, Approval: &TransitionApproval{Approvers: apUsers(6), RejectedStatusID: -2}},
			"due_in garbage":                      {RequiresApproval: true, Approval: &TransitionApproval{Approvers: apUsers(6), DueIn: "soon"}},
			"due_in zero":                         {RequiresApproval: true, Approval: &TransitionApproval{Approvers: apUsers(6), DueIn: "0h"}},
			"due_in negative":                     {RequiresApproval: true, Approval: &TransitionApproval{Approvers: apUsers(6), DueIn: "-2d"}},
		}
		_ = field
		for name, tr := range cases {
			_, err := saveWorkflow(t, docWith(tr), apU6)
			require.Error(t, err, name)
			assert.True(t, IsErrWorkflowInvalidDefinition(err), "%s: got %T %v", name, err, err)
		}
		db.AssertCount(t, "workflows", builder.Eq{"project_id": apProject}, 0)
	})

	t.Run("an archived status cannot be the rejected status", func(t *testing.T) {
		setup(t)
		saved := mustSaveWorkflow(t, docWith(&WorkflowTransition{RequiresApproval: true, Approval: apCfg(ApprovalModeAny, 6)}), apU6)
		rejected := statusIDByTitle(saved, "Rejected")
		// Give the status history, then drop it: it gets archived.
		task := createWorkflowTask(t, apProject, &Task{Title: "t", ProjectID: apProject})
		_, err := changeTaskStatusAs(t, task.ID, rejected, "", apU6)
		require.NoError(t, err)
		_, err = changeTaskStatusAs(t, task.ID, statusIDByTitle(saved, "Todo"), "", apU6)
		require.NoError(t, err)
		doc := readWorkflow(t, apProject)
		doc.Transitions[0].Approval.RejectedStatusID = 0
		for _, st := range doc.Statuses {
			if st.ID == rejected {
				doc.Statuses = append(doc.Statuses[:0:0], filterStatuses(doc.Statuses, rejected)...)
				break
			}
		}
		mustSaveWorkflow(t, doc, apU6)
		doc = readWorkflow(t, apProject)
		doc.Transitions[0].Approval.RejectedStatusID = rejected
		_, err = saveWorkflow(t, doc, apU6)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowInvalidDefinition(err))
	})
}

func filterStatuses(in []*WorkflowStatus, drop int64) []*WorkflowStatus {
	out := []*WorkflowStatus{}
	for _, st := range in {
		if st.ID != drop {
			out = append(out, st)
		}
	}
	return out
}
