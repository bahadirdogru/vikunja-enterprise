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
	"fmt"
	"html"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// Project 11 has the fixture workflow: statuses 1 Todo, 2 Doing, 3 Review, 4 Done, 5 Dropped. Task 20 is in Todo.
// The transition Todo to Dropped (fixture transition 3) is made to ask for an approval of user6 and user2.

func shareWorkflowProject(t *testing.T) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	for _, id := range []int64{2, 3} {
		_, err := s.Insert(&models.ProjectUser{UserID: id, ProjectID: projectWorkflow, Permission: models.PermissionWrite})
		require.NoError(t, err)
	}
	require.NoError(t, s.Commit())
}

func requireApprovalForDropping(t *testing.T, mode models.ApprovalMode, approvers ...int64) {
	t.Helper()
	shareWorkflowProject(t)
	sources := []models.ApproverSource{}
	for _, id := range approvers {
		sources = append(sources, models.ApproverSource{Type: models.ApproverSourceUser, ID: id})
	}
	s := db.NewSession()
	defer s.Close()
	_, err := s.ID(3).Cols("requires_approval", "approval").Update(&models.WorkflowTransition{
		RequiresApproval: true,
		Approval:         &models.TransitionApproval{Mode: mode, Approvers: sources},
	})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	models.ResetAutomationCache()
}

func requestDropping(t *testing.T, taskID int64, by *user.User) *models.ApprovalRequest {
	t.Helper()
	var req *models.ApprovalRequest
	mustCommit(t, func(s *xorm.Session) error {
		c := &models.TaskStatusChange{TaskID: taskID, ToStatusID: 5, Comment: "c"}
		can, err := c.CanCreate(s, by)
		require.NoError(t, err)
		require.True(t, can)
		if err = c.Create(s, by); err != nil {
			return err
		}
		req = c.Approval
		return nil
	})
	require.NotNil(t, req, "the transition must ask for an approval")
	return req
}

func decide(t *testing.T, requestID int64, by *user.User, decision models.ApprovalDecisionValue, comment string) *models.ApprovalDecision {
	t.Helper()
	d := &models.ApprovalDecision{RequestID: requestID, Decision: decision, Comment: comment}
	mustCommit(t, func(s *xorm.Session) error {
		can, err := d.CanCreate(s, by)
		require.NoError(t, err)
		require.True(t, can)
		return d.Create(s, by)
	})
	return d
}

func approvalRows(t *testing.T, taskID int64) []*models.ApprovalRequest {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	rows := []*models.ApprovalRequest{}
	require.NoError(t, s.Where("task_id = ?", taskID).OrderBy("id ASC").Find(&rows))
	return rows
}

func TestApprovalTriggers_Requested(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6, 2)
	counting(t, projectWorkflow, spec.TriggerApprovalRequested, nil, "requested")
	// Conditions can use the fields of the request.
	insertRawRuleWith(t, projectWorkflow, user1, spec.TriggerApprovalRequested, nil,
		&spec.Condition{Field: "event.new.approval_mode", Op: "=", Value: "all"}, rawAct("test.count", "key", "mode_all"))
	insertRawRuleWith(t, projectWorkflow, user1, spec.TriggerApprovalRequested, nil,
		&spec.Condition{All: []*spec.Condition{
			{Field: "event.new.ad_hoc", Op: "=", Value: false},
			{Field: "event.new.status", Op: "=", Value: 5},
			{Field: "event.old.status", Op: "=", Value: 1},
			{Field: "event.new.requested_by", Op: "=", Value: 1},
			{Field: "event.new.approvers", Op: "in", Value: []any{2}},
			{Field: "event.doer.id", Op: "=", Value: 1},
		}}, rawAct("test.count", "key", "transition_fields"))

	requestDropping(t, 20, user1)
	drain(t)
	assert.Equal(t, 1, counter("requested"))
	assert.Equal(t, 0, counter("mode_all"))
	assert.Equal(t, 1, counter("transition_fields"))

	// An ad-hoc request carries the same trigger.
	mustCommit(t, func(s *xorm.Session) error {
		req := &models.ApprovalRequest{TaskID: 20, Title: "Check", ApproverIDs: []int64{6}, Mode: models.ApprovalModeAll}
		can, err := req.CanCreate(s, user1)
		require.NoError(t, err)
		require.True(t, can)
		return req.Create(s, user1)
	})
	drain(t)
	assert.Equal(t, 2, counter("requested"))
	assert.Equal(t, 1, counter("mode_all"))
	assert.Equal(t, 1, counter("transition_fields"))

	t.Run("a repeated ask does not fire again", func(t *testing.T) {
		requestDropping(t, 20, user1)
		drain(t)
		assert.Equal(t, 2, counter("requested"))
	})
}

func TestApprovalTriggers_ResolvedAndItsOutcomeOption(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6, 2)
	counting(t, projectWorkflow, spec.TriggerApprovalResolved, nil, "default")
	counting(t, projectWorkflow, spec.TriggerApprovalResolved, map[string]any{"outcome": "any"}, "any")
	counting(t, projectWorkflow, spec.TriggerApprovalResolved, map[string]any{"outcome": "approved"}, "approved")
	counting(t, projectWorkflow, spec.TriggerApprovalResolved, map[string]any{"outcome": "rejected"}, "rejected")
	counting(t, projectWorkflow, spec.TriggerApprovalResolved, map[string]any{"outcome": "cancelled"}, "cancelled")
	insertRawRuleWith(t, projectWorkflow, user1, spec.TriggerApprovalResolved, nil,
		&spec.Condition{Field: "event.new.outcome", Op: "=", Value: "approved"}, rawAct("test.count", "key", "condition_approved"))

	// An approval.
	req := requestDropping(t, 20, user1)
	drain(t)
	assert.Equal(t, 0, counter("default"), "a request that is only made is not resolved")
	decide(t, req.ID, user6, models.ApprovalDecisionApproved, "")
	drain(t)
	for key, n := range map[string]int{"default": 1, "any": 1, "approved": 1, "rejected": 0, "cancelled": 0, "condition_approved": 1} {
		assert.Equal(t, n, counter(key), key)
	}

	// A rejection (task 20 is in Dropped now; use a new one).
	task := newTask(t, projectWorkflow, "second", user1)
	req = requestDropping(t, task.ID, user1)
	decide(t, req.ID, user2, models.ApprovalDecisionRejected, "no")
	drain(t)
	for key, n := range map[string]int{"default": 2, "any": 2, "approved": 1, "rejected": 1, "cancelled": 0, "condition_approved": 1} {
		assert.Equal(t, n, counter(key), key)
	}

	// A cancellation.
	task = newTask(t, projectWorkflow, "third", user1)
	req = requestDropping(t, task.ID, user1)
	mustCommit(t, func(s *xorm.Session) error {
		c := &models.ApprovalCancel{RequestID: req.ID}
		can, err := c.CanCreate(s, user1)
		require.NoError(t, err)
		require.True(t, can)
		return c.Create(s, user1)
	})
	drain(t)
	for key, n := range map[string]int{"default": 3, "any": 3, "approved": 1, "rejected": 1, "cancelled": 1} {
		assert.Equal(t, n, counter(key), key)
	}

	// An automatic cancellation: the task left the status.
	task = newTask(t, projectWorkflow, "fourth", user1)
	requestDropping(t, task.ID, user1)
	changeStatus(t, task.ID, 2)
	drain(t)
	assert.Equal(t, 4, counter("default"))
	assert.Equal(t, 2, counter("cancelled"))

	t.Run("the outcome option is validated", func(t *testing.T) {
		issues := spec.Validate(&spec.Doc{
			Trigger: spec.Trigger{Type: spec.TriggerApprovalResolved, Options: map[string]any{"outcome": "maybe"}},
			Actions: []*spec.Action{act(spec.ActionAddComment, map[string]any{"text": "x"})},
		}, 25)
		require.NotEmpty(t, issues)
		assert.Equal(t, "trigger.options.outcome", issues[0].Path)
	})
}

func TestApprovalTriggers_EventFieldsInTemplates(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6)
	r := addRule(t, projectWorkflow, user1, rule(spec.TriggerApprovalResolved, map[string]any{"outcome": "approved"}, nil,
		act(spec.ActionAddComment, map[string]any{"text": "{{event.approval.title}} was {{event.approval.outcome}} (#{{event.approval.id}}) by {{event.doer.username}}"})))
	req := requestDropping(t, 20, user1)
	decide(t, req.ID, user6, models.ApprovalDecisionApproved, "")
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	s := db.NewSession()
	defer s.Close()
	comments := []*models.TaskComment{}
	require.NoError(t, s.Where("task_id = ?", 20).Find(&comments))
	found := false
	for _, c := range comments {
		if html.UnescapeString(stripP(c.Comment)) == fmt.Sprintf("Todo → Dropped was approved (#%d) by user6", req.ID) {
			found = true
		}
	}
	assert.True(t, found, "the comment of the action names the request, the outcome and the doer")
}

func stripP(html string) string {
	html = strings.TrimPrefix(html, "<p>")
	return strings.TrimSuffix(html, "</p>")
}

func TestApprovalAction_Request(t *testing.T) {
	reset(t)
	shareWorkflowProject(t)
	task := newTask(t, projectWorkflow, "Cover draft", user6)
	mustCommitAssign(t, task.ID)
	r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
		act(spec.ActionApprovalRequest, map[string]any{
			"approvers":   []any{6, "assignees", "task_creator", 2, float64(2)},
			"mode":        "all",
			"title":       "Review {{task.title}}",
			"description": "Please check {{task.identifier}}",
			"due_offset":  "2d",
		})))
	updateTask(t, user1, task.ID, func(x *models.Task) { x.Title = "Cover v2" }, "title")
	drain(t)

	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	rows := approvalRows(t, task.ID)
	require.Len(t, rows, 1)
	assert.Equal(t, fmt.Sprintf("approval requested (#%d)", rows[0].ID), runSteps(t, run.ID)[0].Output)
	assert.Equal(t, "Review Cover v2", rows[0].Title)
	assert.Equal(t, models.ApprovalModeAll, rows[0].Mode)
	assert.Equal(t, int64(1), rows[0].RequestedByID, "the owner of the rule asks")
	assert.Nil(t, rows[0].TransitionID, "ad-hoc: no status change when it resolves")
	require.NotNil(t, rows[0].Due)
	assert.Contains(t, rows[0].Description, "Please check")

	approvers := []int64{}
	func() {
		// Closed before the next test resets the database: an open transaction would block that on MySQL.
		s := db.NewSession()
		defer s.Close()
		require.NoError(t, s.Table("approval_approvers").Where("request_id = ?", rows[0].ID).OrderBy("user_id").Cols("user_id").Find(&approvers))
	}()
	// The owner (user1, who is also the assignee) is not asked; 6 is creator and listed, 2 listed twice.
	assert.Equal(t, []int64{2, 6}, approvers)

	t.Run("it resolves without touching the status", func(t *testing.T) {
		decide(t, rows[0].ID, user6, models.ApprovalDecisionApproved, "")
		decide(t, rows[0].ID, user2, models.ApprovalDecisionApproved, "")
		stored := approvalRows(t, task.ID)[0]
		assert.Equal(t, models.ApprovalStatusApproved, stored.Status)
		s := db.NewSession()
		defer s.Close()
		status, err := models.GetTaskWorkflowStatus(s, task.ID)
		require.NoError(t, err)
		assert.EqualValues(t, 1, status.ID)
	})

	t.Run("the dry run plans it without writing", func(t *testing.T) {
		reset(t)
		shareWorkflowProject(t)
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, nil, nil,
			act(spec.ActionApprovalRequest, map[string]any{"approvers": []any{6}, "title": "Check {{task.title}}"})))
		res := dryRun(t, r, 20)
		require.Len(t, res.Steps, 1)
		assert.Empty(t, res.Steps[0].Error)
		assert.Contains(t, res.Steps[0].Summary, "request approval")
		assert.Contains(t, res.Steps[0].Summary, "Check task #20")
		assert.Empty(t, approvalRows(t, 20))
	})

	t.Run("a step that has nobody to ask fails", func(t *testing.T) {
		reset(t)
		shareWorkflowProject(t)
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			act(spec.ActionApprovalRequest, map[string]any{"approvers": []any{"assignees", 1}, "title": "x"})))
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "go" }, "title")
		drain(t)
		runs := allRuns(t, r.ID)
		require.Len(t, runs, 1)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		assert.Contains(t, runSteps(t, runs[0].ID)[0].Error, "nobody is left to ask")
		assert.Empty(t, approvalRows(t, 20))
	})

	t.Run("an approver who cannot read the task fails the step with the typed error", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			act(spec.ActionApprovalRequest, map[string]any{"approvers": []any{4}, "title": "x"})))
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "go" }, "title")
		drain(t)
		runs := allRuns(t, r.ID)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		assert.Contains(t, runSteps(t, runs[0].ID)[0].Error, "Approval request is invalid")
	})

	t.Run("the catalog validates the parameters", func(t *testing.T) {
		valid := &spec.Doc{
			Trigger: spec.Trigger{Type: spec.TriggerTaskCreated},
			Actions: []*spec.Action{act(spec.ActionApprovalRequest, map[string]any{"approvers": []any{1, "assignees", "event_doer", "task_creator"}, "title": "t", "mode": "any", "due_offset": "3d"})},
		}
		assert.Empty(t, spec.Validate(valid, 25))
		for name, params := range map[string]map[string]any{
			"no approvers":    {"title": "t"},
			"no title":        {"approvers": []any{1}},
			"empty approvers": {"approvers": []any{}, "title": "t"},
			"unknown keyword": {"approvers": []any{"everyone"}, "title": "t"},
			"unknown mode":    {"approvers": []any{1}, "title": "t", "mode": "most"},
			"bad due offset":  {"approvers": []any{1}, "title": "t", "due_offset": "soon"},
			"not a user id":   {"approvers": []any{-1}, "title": "t"},
		} {
			doc := &spec.Doc{Trigger: spec.Trigger{Type: spec.TriggerTaskCreated}, Actions: []*spec.Action{act(spec.ActionApprovalRequest, params)}}
			assert.NotEmpty(t, spec.Validate(doc, 25), name)
		}
	})
}

func TestApprovalAction_SetStatusRequestsAnApproval(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6)
	r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
		act(spec.ActionSetStatus, map[string]any{"status_id": 5, "comment": "dropped by {{event.doer.username}}"})))

	t.Run("the dry run says it would request approval", func(t *testing.T) {
		res := dryRun(t, r, 20)
		require.Len(t, res.Steps, 1)
		assert.Empty(t, res.Steps[0].Error)
		assert.Contains(t, res.Steps[0].Summary, "would request approval")
		assert.Empty(t, approvalRows(t, 20))
	})

	t.Run("the step finishes successfully and the request belongs to the owner of the rule", func(t *testing.T) {
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger" }, "title")
		drain(t)
		run := allRuns(t, r.ID)[0]
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		rows := approvalRows(t, 20)
		require.Len(t, rows, 1)
		assert.Equal(t, fmt.Sprintf("approval requested (#%d)", rows[0].ID), runSteps(t, run.ID)[0].Output)
		assert.Equal(t, int64(1), rows[0].RequestedByID)
		assert.Equal(t, "dropped by user1", rows[0].Comment)

		s := db.NewSession()
		defer s.Close()
		status, err := models.GetTaskWorkflowStatus(s, 20)
		require.NoError(t, err)
		assert.EqualValues(t, 1, status.ID, "the task did not move")

		// Running the rule again returns the same request.
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger again" }, "title")
		drain(t)
		assert.Len(t, approvalRows(t, 20), 1)
		assert.Contains(t, runSteps(t, allRuns(t, r.ID)[1].ID)[0].Output, fmt.Sprintf("(#%d)", rows[0].ID))
	})

	t.Run("a script asks the same way", func(t *testing.T) {
		reset(t)
		requireApprovalForDropping(t, models.ApprovalModeAny, 6)
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			scriptAction(`vikunja.status.set(task.id, 5, "by script"); return vikunja.tasks.get(task.id).status.id`)))
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger" }, "title")
		drain(t)
		run := allRuns(t, r.ID)[0]
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, runSteps(t, run.ID)[0].Output, "result: 1")
		rows := approvalRows(t, 20)
		require.Len(t, rows, 1)
		assert.Equal(t, "by script", rows[0].Comment)
	})

	t.Run("nobody to ask fails the step", func(t *testing.T) {
		reset(t)
		requireApprovalForDropping(t, models.ApprovalModeAny, 1) // only the owner of the rule
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			act(spec.ActionSetStatus, map[string]any{"status_id": 5})))
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger" }, "title")
		drain(t)
		runs := allRuns(t, r.ID)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		assert.Contains(t, runSteps(t, runs[0].ID)[0].Error, "No approvers")
		assert.Empty(t, approvalRows(t, 20))
	})

	t.Run("the done checkbox of an action is refused for a transition that needs an approval", func(t *testing.T) {
		reset(t)
		shareWorkflowProject(t)
		s := db.NewSession()
		_, err := s.Insert(&models.WorkflowTransition{
			WorkflowID: 1, FromStatusID: 1, ToStatusID: 4, MinPermission: models.WorkflowTransitionPermissionWrite,
			RequiresApproval: true,
			Approval:         &models.TransitionApproval{Approvers: []models.ApproverSource{{Type: models.ApproverSourceUser, ID: 6}}},
		})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		models.ResetAutomationCache()
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			act(spec.ActionSetDone, map[string]any{"done": true})))
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger" }, "title")
		drain(t)
		runs := allRuns(t, r.ID)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		assert.Contains(t, runSteps(t, runs[0].ID)[0].Error, "needs an approval")
		assert.False(t, getTask(t, 20).Done)
		assert.Empty(t, approvalRows(t, 20), "a failed step creates no request")
	})
}

func TestApprovalAction_OriginAndDepthOfEventsAnActionCauses(t *testing.T) {
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6)
	// Rule A asks for an approval when a title changes; rule B listens for new requests.
	a := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
		act(spec.ActionApprovalRequest, map[string]any{"approvers": []any{6}, "title": "check"})))
	b := counting(t, projectWorkflow, spec.TriggerApprovalRequested, nil, "b")
	updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger" }, "title")
	drain(t)

	runsA := allRuns(t, a.ID)
	require.Len(t, runsA, 1)
	require.Equal(t, models.AutomationRunSuccess, runsA[0].Status, runsA[0].Error)
	runsB := allRuns(t, b.ID)
	require.Len(t, runsB, 1)
	assert.EqualValues(t, 1, runsB[0].Depth, "the request was caused by a run")
	var requested *models.AutomationEvent
	for _, ev := range allEvents(t) {
		if ev.Name == "approval.requested" {
			requested = ev
		}
	}
	require.NotNil(t, requested)
	assert.Equal(t, runsA[0].ID, requested.OriginRunID)
	assert.EqualValues(t, 1, requested.Depth)
	assert.Equal(t, 1, counter("b"))

	// The same through set_status.
	reset(t)
	requireApprovalForDropping(t, models.ApprovalModeAny, 6)
	a = addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
		act(spec.ActionSetStatus, map[string]any{"status_id": 5})))
	counting(t, projectWorkflow, spec.TriggerApprovalRequested, nil, "b2")
	updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger" }, "title")
	drain(t)
	runsA = allRuns(t, a.ID)
	require.Len(t, runsA, 1)
	requested = nil
	for _, ev := range allEvents(t) {
		if ev.Name == "approval.requested" {
			requested = ev
		}
	}
	require.NotNil(t, requested)
	assert.Equal(t, runsA[0].ID, requested.OriginRunID)
	assert.EqualValues(t, 1, requested.Depth)
	assert.Equal(t, 1, counter("b2"))
}
