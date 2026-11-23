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
	"testing"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

const confidentialTitle = "confidential title of user2"

// privateProjectOfUser2 is a project user1 has no access to.
func privateProjectOfUser2(t *testing.T) *models.Project {
	t.Helper()
	private := &models.Project{Title: "private of user2"}
	mustCommit(t, func(s *xorm.Session) error { return models.CreateProject(s, private, user2, false, false) })
	return private
}

// moveAway puts a task into another project behind the back of the engine and gives it a title the owner of the
// rule has never seen, like a user of the other project would.
func moveAway(t *testing.T, s *xorm.Session, taskID, projectID int64) {
	t.Helper()
	_, err := s.ID(taskID).Cols("project_id", "title").Update(&models.Task{ProjectID: projectID, Title: confidentialTitle})
	require.NoError(t, err)
}

func init() {
	// test.moveaway moves the task of the run into the project given as project_id, without any check.
	register("test.moveaway", func(env *Env, p params) (*Plan, error) {
		return &Plan{Summary: "move away", apply: func() (string, error) {
			_, err := env.S.ID(env.Sub.Task.ID).Cols("project_id", "title").
				Update(&models.Task{ProjectID: p.int("project_id"), Title: confidentialTitle})
			return "moved", err
		}}, nil
	})
}

func TestScript_UpdateWithAnEmptyPatchNeedsWriteAccess(t *testing.T) {
	reset(t)
	hidden := newTask(t, privateProjectOfUser2(t).ID, "hidden task", user2)
	// user1 can only read project 9 (task 18).
	source := `
		var out = {};
		try { out.hidden = "allowed: " + JSON.stringify(vikunja.tasks.update(` + itoa(hidden.ID) + `, {})) } catch (e) { out.hidden = "refused" }
		try { vikunja.tasks.update(18, {}); out.readonly = "allowed" } catch (e) { out.readonly = "refused" }
		return out`

	t.Run("real run", func(t *testing.T) {
		_, run, steps := runScript(t, source)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		out := stepOutput(steps)
		assert.NotContains(t, out, "hidden task")
		assert.Contains(t, out, `"hidden":"refused"`)
		assert.Contains(t, out, `"readonly":"refused"`)
	})

	t.Run("dry run", func(t *testing.T) {
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil, nil, scriptAction(source)))
		res := dryRun(t, r, 1)
		require.Len(t, res.Steps, 1)
		require.Empty(t, res.Steps[0].Error)
		assert.NotContains(t, res.Steps[0].Summary, "hidden task")
		assert.Contains(t, res.Steps[0].Summary, `"hidden":"refused"`)
		assert.Contains(t, res.Steps[0].Summary, `"readonly":"refused"`)
	})
}

func TestRunner_ATaskTheOwnerCanNoLongerReadIsNotHandedToTheRule(t *testing.T) {
	t.Run("moved before the run starts", func(t *testing.T) {
		reset(t)
		allowLoopback(t)
		hook := countingServer(t, 200, "ok")
		private := privateProjectOfUser2(t)
		script := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`return task && task.title`)))
		condition := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil,
			&spec.Condition{Script: `throw new Error("saw " + (task && task.title))`},
			act(spec.ActionAddComment, map[string]any{"text": "never"})))
		request := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
			act(spec.ActionHTTPRequest, map[string]any{"url": hook.url + "/hook", "body": "{{task.title}}"})))

		task := newTask(t, projectOwned, "still in project 1", user1)
		// The event is queued; before a worker gets to it the task leaves for a project the owner cannot see.
		s := db.NewAutocommitSession()
		moveAway(t, s, task.ID, private.ID)
		s.Close()
		drain(t)

		for _, r := range []*models.AutomationRule{script, condition, request} {
			runs := allRuns(t, r.ID)
			require.Len(t, runs, 1)
			assert.Equal(t, models.AutomationRunSkipped, runs[0].Status, runs[0].Error)
			assert.Equal(t, SkipTaskNotFound, runs[0].SkipReason)
			assert.NotContains(t, runs[0].Error, confidentialTitle)
			for _, step := range runSteps(t, runs[0].ID) {
				assert.NotContains(t, step.Output+step.Error, confidentialTitle)
			}
		}
		assert.Zero(t, hook.count(), "nothing about the task is sent anywhere")
	})

	t.Run("moved between two steps", func(t *testing.T) {
		reset(t)
		private := privateProjectOfUser2(t)
		r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated,
			rawAct("test.moveaway", "project_id", private.ID),
			scriptAction(`return task && task.title`),
		)
		newTask(t, projectOwned, "still in project 1", user1)
		drain(t)

		runs := allRuns(t, r.ID)
		require.Len(t, runs, 1)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		assert.NotContains(t, runs[0].Error, confidentialTitle)
		steps := runSteps(t, runs[0].ID)
		require.Len(t, steps, 2)
		assert.Equal(t, models.AutomationStepFailed, steps[1].Status)
		assert.NotContains(t, steps[1].Output+steps[1].Error, confidentialTitle)
	})
}
