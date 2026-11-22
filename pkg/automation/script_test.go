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
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

func scriptAction(source string) *spec.Action {
	return act(spec.ActionScriptRun, map[string]any{"source": source})
}

// runScript runs a script action for a new task of the project the owner administers and returns the finished run.
func runScript(t *testing.T, source string) (*models.Task, *models.AutomationRun, []*models.AutomationRunStep) {
	t.Helper()
	return runAction(t, projectOwned, user1, user1, scriptAction(source))
}

func stepOutput(steps []*models.AutomationRunStep) string {
	if len(steps) == 0 {
		return ""
	}
	return steps[0].Output
}

func TestScript_SeesTheEventTheTaskAndTheProject(t *testing.T) {
	reset(t)
	_, run, steps := runScript(t, `
		return {event: event.name, doer: event.doer.username, doerId: event.doer.id, title: task.title, project: project.title,
		        status: task.status, assignees: task.assignees, cf: task.custom_fields, done: task.done, id_is_number: typeof task.id}`)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	out := stepOutput(steps)
	assert.Contains(t, out, `"event":"task.created"`)
	assert.Contains(t, out, `"doer":"user1"`)
	assert.Contains(t, out, `"title":"Original title"`)
	assert.Contains(t, out, `"project":"Test1"`)
	assert.Contains(t, out, `"status":null`)
	assert.Contains(t, out, `"cf":{"1":null,"2":null,"3":null,"4":null}`)
	assert.Contains(t, out, `"id_is_number":"number"`)
	assert.True(t, strings.HasPrefix(out, "script finished in "), out)
}

func TestScript_TaskIsNullWhenTheTaskWasDeleted(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskDeleted, nil, nil, scriptAction(`log(JSON.stringify([task, event.name, typeof event.task_id])); return project.id`)))
	tk := newTask(t, projectOwned, "doomed", user1)
	mustCommit(t, func(s *xorm.Session) error { return tk.Delete(s, user1) })
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	steps := runSteps(t, runs[0].ID)
	require.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
	assert.Contains(t, steps[0].Output, `[null,"task.deleted","number"]`)
}

func TestScript_TasksFieldsStatusAssigneesAndLabels(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("description"), nil, scriptAction(`
		var before = vikunja.tasks.get(task.id);
		var updated = vikunja.tasks.update(task.id, {title: "T-" + before.title, priority: 4, due_date: "+2d", percent_done: 0.5,
			hex_color: "#112233", description: "literal {{task.title}}"});
		var child = vikunja.tasks.create(task.project_id, {title: "child", parent_id: task.id, assignees: [1], priority: 2, due_date: "+1d", description: "d"});
		vikunja.tasks.comment(task.id, "a <b> comment");
		vikunja.fields.set(task.id, 3, 42);
		vikunja.fields.set(task.id, 1, "text value {{task.title}}");
		vikunja.fields.set(task.id, 4, [3, 4]);
		var f = vikunja.fields.get(task.id, 3);
		vikunja.assignees.add(task.id, 1);
		vikunja.labels.add(task.id, 1); vikunja.labels.add(task.id, 2); vikunja.labels.remove(task.id, 1);
		var found = vikunja.tasks.find("title like child");
		var reread = vikunja.tasks.get(task.id);
		return {updatedTitle: updated.title, priority: updated.priority, f: f, child: child.id, childTitle: child.title,
		        found: found.map(function (x) { return x.id }), assignees: reread.assignees, labels: reread.labels,
		        cf: reread.custom_fields, due: typeof reread.due_date}`)))
	task := newTask(t, projectOwned, "Original title", user1)
	updateTask(t, user1, task.ID, func(x *models.Task) { x.Description = "go" }, "description")
	drain(t)
	run := allRuns(t, r.ID)[0]
	steps := runSteps(t, run.ID)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)

	got := getTask(t, task.ID)
	assert.Equal(t, "T-Original title", got.Title)
	assert.EqualValues(t, 4, got.Priority)
	assert.InDelta(t, 0.5, got.PercentDone, 0.001)
	assert.Equal(t, "112233", got.HexColor)
	assert.Equal(t, "literal {{task.title}}", got.Description, "text a script passes is data, not a template")
	assert.WithinDuration(t, time.Now().Add(48*time.Hour), got.DueDate, time.Minute)
	assert.Equal(t, []int64{1}, assignees(t, task.ID))

	s := newSession()
	defer s.Close()
	children := []*models.Task{}
	require.NoError(t, s.Where("title = ?", "child").Find(&children))
	require.Len(t, children, 1)
	assert.Equal(t, []int64{1}, assignees(t, children[0].ID))
	assert.EqualValues(t, 2, children[0].Priority)
	assert.Equal(t, "d", children[0].Description)
	relations := []*models.TaskRelation{}
	require.NoError(t, s.Where("task_id = ? AND other_task_id = ?", task.ID, children[0].ID).Find(&relations))
	require.Len(t, relations, 1)
	assert.Equal(t, models.RelationKindSubtask, relations[0].RelationKind)

	comments := []*models.TaskComment{}
	require.NoError(t, s.Where("task_id = ?", task.ID).Find(&comments))
	require.Len(t, comments, 1)
	assert.Equal(t, "<p>a &lt;b&gt; comment</p>", comments[0].Comment)

	values, err := models.GetCustomFieldValuesOfTask(s, got)
	require.NoError(t, err)
	byField := map[int64]any{}
	for _, v := range values {
		byField[v.CustomFieldID] = v.Value
	}
	assert.InDelta(t, 42, byField[3], 0.001)
	assert.Equal(t, "text value {{task.title}}", byField[1])
	assert.Equal(t, []int64{3, 4}, byField[4])

	labelIDs := []int64{}
	require.NoError(t, s.Table("label_tasks").Where("task_id = ?", task.ID).Cols("label_id").Find(&labelIDs))
	assert.Equal(t, []int64{2}, labelIDs)

	out := stepOutput(steps)
	assert.Contains(t, out, `"updatedTitle":"T-Original title"`)
	assert.Contains(t, out, `"f":42`)
	assert.Contains(t, out, `"labels":[2]`)
	assert.Contains(t, out, `"assignees":[1]`)
	assert.Contains(t, out, `"cf":{"1":"text value {{task.title}}","2":null,"3":42,"4":[3,4]}`)
	assert.Contains(t, out, `"due":"string"`)
	assert.Contains(t, out, "(14 vikunja calls, 0 http requests)")
}

func TestScript_StatusSetThroughTheWorkflow(t *testing.T) {
	reset(t)
	// Task 20 of project 11 is in status 1; user1 administers the project.
	r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
		scriptAction(`var before = task.status; vikunja.status.set(task.id, 2, "moved by script"); return [before.id, vikunja.tasks.get(task.id).status.id]`)))
	updateTask(t, user1, 20, func(x *models.Task) { x.Title = "trigger" }, "title")
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	assert.Contains(t, runSteps(t, run.ID)[0].Output, "result: [1,2]")

	s := newSession()
	defer s.Close()
	status, err := models.GetTaskWorkflowStatus(s, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 2, status.ID)

	t.Run("a rejected transition is a catchable error with the message of the workflow", func(t *testing.T) {
		reset(t)
		changeStatus(t, 20, 2)
		r := addRule(t, projectWorkflow, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			scriptAction(`try { vikunja.status.set(task.id, 3) } catch (e) { return e.message }`)))
		updateTask(t, user1, 20, func(x *models.Task) { x.Title = "again" }, "title")
		drain(t)
		run := allRuns(t, r.ID)[0]
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, runSteps(t, run.ID)[0].Output, "Workflow requirements not met")
	})
}

func TestScript_FindIsRestrictedToWhatTheOwnerCanRead(t *testing.T) {
	reset(t)
	// Project 9 is readable by user1; project 2 is not (task 13 belongs to it), so find must not see its tasks.
	_, run, steps := runScript(t, `
		var all = vikunja.tasks.find("done = false || done = true");
		var projects = {};
		all.forEach(function (x) { projects[x.project_id] = true });
		return {ids: Object.keys(projects).sort(), count: all.length, limited: vikunja.tasks.find("done = false || done = true", {project_id: 9}).map(function (x) { return x.id })}`)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	out := stepOutput(steps)
	assert.Contains(t, out, `"limited":[18]`)
	assert.NotContains(t, out, `"2"`, "tasks of projects the owner cannot read are not found")

	t.Run("project the owner cannot read is refused", func(t *testing.T) {
		reset(t)
		_, run, steps := runScript(t, `try { vikunja.tasks.find("done = false", {project_id: 2}); return "found" } catch (e) { return "refused" }`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		_ = steps
	})
}

func TestScript_PermissionsAreTheOnesOfTheRuleOwner(t *testing.T) {
	reset(t)
	// user1 can only read project 9 (task 18).
	before := getTask(t, 18)
	_, run, steps := runScript(t, `
		var read = vikunja.tasks.get(18);
		var out = {read: read.title};
		function attempt(name, fn) { try { fn(); out[name] = "allowed" } catch (e) { out[name] = "refused: " + e.message } }
		attempt("update", function () { vikunja.tasks.update(18, {title: "hacked"}) });
		attempt("comment", function () { vikunja.tasks.comment(18, "hi") });
		attempt("create", function () { vikunja.tasks.create(9, {title: "new"}) });
		attempt("field", function () { vikunja.fields.set(18, 3, 1) });
		attempt("assign", function () { vikunja.assignees.add(18, 1) });
		attempt("label", function () { vikunja.labels.add(18, 1) });
		attempt("status", function () { vikunja.status.set(18, 1) });
		return out`)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	out := stepOutput(steps)
	assert.Contains(t, out, `"read":"task #18"`)
	for _, name := range []string{"update", "comment", "create", "field", "assign", "label", "status"} {
		assert.Contains(t, out, `"`+name+`":"refused`, name)
		assert.NotContains(t, out, `"`+name+`":"allowed"`, name)
	}
	assert.Equal(t, before.Title, getTask(t, 18).Title)

	t.Run("an uncaught refusal fails the step", func(t *testing.T) {
		reset(t)
		_, run, steps := runScript(t, `vikunja.tasks.update(18, {title: "hacked"})`)
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		require.Len(t, steps, 1)
		assert.Equal(t, models.AutomationStepFailed, steps[0].Status)
		assert.Contains(t, steps[0].Error, "line 1")
		assert.Equal(t, before.Title, getTask(t, 18).Title)
		assert.Equal(t, models.AutomationEventDone, allEvents(t)[len(allEvents(t))-1].Status, "the failure of a script is not retried")
	})

	t.Run("a task of a project the owner cannot see is neither readable nor findable", func(t *testing.T) {
		reset(t)
		private := &models.Project{Title: "private of user2"}
		mustCommit(t, func(s *xorm.Session) error { return models.CreateProject(s, private, user2, false, false) })
		hidden := newTask(t, private.ID, "hidden task", user2)
		_, run, steps := runScript(t, `
			var out = {};
			try { vikunja.tasks.get(`+itoa(hidden.ID)+`); out.get = "allowed" } catch (e) { out.get = "refused" }
			try { vikunja.fields.get(`+itoa(hidden.ID)+`, 3); out.field = "allowed" } catch (e) { out.field = "refused" }
			try { vikunja.tasks.update(`+itoa(hidden.ID)+`, {title: "x"}); out.update = "allowed" } catch (e) { out.update = "refused" }
			out.found = vikunja.tasks.find("done = false || done = true").some(function (t) { return t.id === `+itoa(hidden.ID)+` });
			return out`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, stepOutput(steps), `"get":"refused"`)
		assert.Contains(t, stepOutput(steps), `"field":"refused"`)
		assert.Contains(t, stepOutput(steps), `"update":"refused"`)
		assert.Contains(t, stepOutput(steps), `"found":false`)
		assert.Equal(t, "hidden task", getTask(t, hidden.ID).Title)
	})

	t.Run("a task that does not exist", func(t *testing.T) {
		reset(t)
		_, run, steps := runScript(t, `try { vikunja.tasks.get(999999) } catch (e) { return e.message }`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, stepOutput(steps), "does not exist")
	})
}

func TestScript_ChangesCarryTheOriginOfTheRun(t *testing.T) {
	reset(t)
	// Events are only recorded when a rule listens to them.
	watch := act(spec.ActionStopIf, map[string]any{"condition": map[string]any{"field": "done", "op": "=", "value": true}})
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil, nil, watch))
	addRule(t, projectOwned, user1, rule(spec.TriggerTaskCommentAdded, nil, nil, watch))
	script := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
		scriptAction(`vikunja.tasks.update(task.id, {priority: 3}); vikunja.tasks.comment(task.id, "from a script")`)))
	task := newTask(t, projectOwned, "origin", user1)
	drain(t)
	runs := allRuns(t, script.ID)
	require.Len(t, runs, 1)
	require.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)

	var caused int
	var names []string
	for _, ev := range allEvents(t) {
		if ev.TaskID == task.ID && ev.Name != "task.created" {
			caused++
			names = append(names, ev.Name)
			assert.Equal(t, runs[0].ID, ev.OriginRunID, "%s was caused by the run", ev.Name)
			assert.EqualValues(t, runs[0].Depth+1, ev.Depth)
		}
	}
	assert.ElementsMatch(t, []string{"task.updated", "task.comment.created"}, names)
	assert.Equal(t, 2, caused)
}

func TestScript_ChainsAreDepthLimitedAndNeverReactToThemselves(t *testing.T) {
	reset(t)
	config.AutomationMaxDepth.Set(1)
	// A (created) sets the priority; B (priority changed) sets the description; C (description changed) would set
	// the color, but its event is at depth 2.
	a := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`vikunja.tasks.update(task.id, {priority: 2})`)))
	b := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("priority"), nil, scriptAction(`vikunja.tasks.update(task.id, {description: "by B"})`)))
	c := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("description"), nil, scriptAction(`vikunja.tasks.update(task.id, {hex_color: "#aabbcc"})`)))
	task := newTask(t, projectOwned, "chain", user1)
	drain(t)

	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, a.ID)[0].Status)
	assert.Equal(t, models.AutomationRunSuccess, allRuns(t, b.ID)[0].Status)
	cRuns := allRuns(t, c.ID)
	require.Len(t, cRuns, 1)
	assert.Equal(t, models.AutomationRunSkipped, cRuns[0].Status)
	assert.Equal(t, SkipMaxDepth, cRuns[0].SkipReason)
	got := getTask(t, task.ID)
	assert.EqualValues(t, 2, got.Priority)
	assert.Equal(t, "by B", got.Description)
	assert.Empty(t, got.HexColor)

	t.Run("a script that changes what triggers its own rule does not loop", func(t *testing.T) {
		reset(t)
		self := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, updatedBy("title"), nil,
			scriptAction(`vikunja.tasks.update(task.id, {title: task.title + "!"})`)))
		tk := newTask(t, projectOwned, "loop", user1)
		updateTask(t, user1, tk.ID, func(x *models.Task) { x.Title = "start" }, "title")
		drain(t)
		runs := allRuns(t, self.ID)
		require.Len(t, runs, 2)
		assert.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
		assert.Equal(t, models.AutomationRunSkipped, runs[1].Status)
		assert.Equal(t, SkipSelfTrigger, runs[1].SkipReason)
		assert.Equal(t, "start!", getTask(t, tk.ID).Title)
	})
}

func TestScript_DryRunRecordsAndNeverWrites(t *testing.T) {
	reset(t)
	allowLoopback(t)
	hits := countingServer(t, 200, "{}")
	setSecret(t, projectOwned, "TOKEN", "super-secret-token")

	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`
		var t = vikunja.tasks.update(task.id, {title: "dry", priority: 5});
		var c = vikunja.tasks.create(task.project_id, {title: "new child", parent_id: task.id});
		vikunja.tasks.comment(task.id, "would comment");
		vikunja.fields.set(task.id, 3, 7);
		vikunja.assignees.add(task.id, 1);
		vikunja.labels.add(task.id, 2);
		var res = http.fetch("`+hits.url+`/hook", {method: "POST", body: "payload", headers: {"X-A": "1"}});
		var secret = secrets.get("TOKEN");
		log("title now", t.title, "stub id", c.id, "status", res.status, "secret", secret);
		return {title: t.title, found: vikunja.tasks.find("done = false").length > 0, read: vikunja.tasks.get(task.id).title, field: vikunja.fields.get(task.id, 3)}`)))
	task := newTask(t, projectOwned, "real title", user1)
	before := fingerprint(t, task.ID)
	secretsBefore := countRows(t, "automation_secrets")

	res := dryRun(t, r, task.ID)
	require.Len(t, res.Steps, 1)
	step := res.Steps[0]
	require.Empty(t, step.Error)
	assert.Contains(t, step.Summary, `would call vikunja.tasks.update(`+itoa(task.ID)+`, {"priority":5,"title":"dry"}): set priority to 5; set title to "dry"`)
	assert.Contains(t, step.Summary, `would call vikunja.tasks.create(1, {"parent_id":`+itoa(task.ID)+`,"title":"new child"})`)
	assert.Contains(t, step.Summary, "would call vikunja.tasks.comment(")
	assert.Contains(t, step.Summary, "would call vikunja.fields.set("+itoa(task.ID)+", 3, 7)")
	assert.Contains(t, step.Summary, "would call vikunja.assignees.add(")
	assert.Contains(t, step.Summary, "would call vikunja.labels.add(")
	assert.Contains(t, step.Summary, `would call http.fetch("`+hits.url+`/hook"`)
	assert.Contains(t, step.Summary, "title now dry stub id -1 status 200 secret [secret TOKEN]")
	assert.Contains(t, step.Summary, `"title":"dry"`, "the stub of update shows the patch applied")
	assert.Contains(t, step.Summary, `"read":"real title"`, "reads see the real task")
	assert.NotContains(t, step.Summary, "super-secret-token", "a dry run never sees the secret")
	assert.Contains(t, step.Summary, `"found":true`)

	assert.Equal(t, before, fingerprint(t, task.ID), "nothing was written")
	assert.Equal(t, secretsBefore, countRows(t, "automation_secrets"))
	assert.EqualValues(t, 0, hits.count(), "no request was sent")
	assert.Equal(t, "real title", getTask(t, task.ID).Title)

	t.Run("a call that would fail fails in the dry run too", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`vikunja.tasks.update(18, {title: "x"})`)))
		tk := newTask(t, projectOwned, "t", user1)
		res := dryRun(t, r, tk.ID)
		assert.NotEmpty(t, res.Steps[0].Error)
		assert.Contains(t, res.Steps[0].Error, "line 1")
	})

	t.Run("a missing secret fails the dry run", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`return secrets.get("NOPE")`)))
		tk := newTask(t, projectOwned, "t", user1)
		res := dryRun(t, r, tk.ID)
		assert.Contains(t, res.Steps[0].Error, "does not exist")
	})
}

func TestScript_Condition(t *testing.T) {
	reset(t)
	runWith := func(t *testing.T, cond *spec.Condition) (*models.AutomationRun, *models.Task) {
		t.Helper()
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, cond, setField("priority", 4)))
		tk := newTask(t, projectOwned, "conditioned", user1)
		drain(t)
		return allRuns(t, r.ID)[0], getTask(t, tk.ID)
	}

	t.Run("true runs the actions", func(t *testing.T) {
		run, task := runWith(t, &spec.Condition{Script: `return task.title === "conditioned" && event.doer.username === "user1"`})
		assert.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.EqualValues(t, 4, task.Priority)
	})
	t.Run("false skips the run", func(t *testing.T) {
		run, task := runWith(t, &spec.Condition{Script: `return false`})
		assert.Equal(t, models.AutomationRunSkipped, run.Status)
		assert.Equal(t, SkipConditions, run.SkipReason)
		assert.EqualValues(t, 0, task.Priority)
	})
	t.Run("combined with comparisons", func(t *testing.T) {
		run, _ := runWith(t, &spec.Condition{All: []*spec.Condition{
			{Field: "title", Op: "like", Value: "condition"},
			{Not: []*spec.Condition{{Script: `return vikunja.tasks.find("title like nothingmatchesthis").length > 0`}}},
		}})
		assert.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	})
	t.Run("a throw fails the run", func(t *testing.T) {
		run, task := runWith(t, &spec.Condition{Script: `throw new Error("cannot decide")`})
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, run.Error, "cannot decide")
		assert.EqualValues(t, 0, task.Priority)
	})
	t.Run("returning nothing fails clearly", func(t *testing.T) {
		run, _ := runWith(t, &spec.Condition{Script: `task.title === "conditioned"`})
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, run.Error, "returned nothing")
		assert.Contains(t, run.Error, "return")
	})
	t.Run("a timeout fails the run promptly", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsConditionTimeout.Set(100 * time.Millisecond)
		config.AutomationScriptsTimeout.Set(time.Minute)
		started := time.Now()
		run, _ := runWith2(t, &spec.Condition{Script: `while (true) {}`})
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, run.Error, "ran longer than 100ms")
		assert.Less(t, time.Since(started), 5*time.Second)
	})
	t.Run("conditions are read-only even in a real run", func(t *testing.T) {
		run, task := runWith(t, &spec.Condition{Script: `vikunja.tasks.update(task.id, {title: "changed by a condition"}); return true`})
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, run.Error, "read-only")
		assert.Equal(t, "conditioned", task.Title)

		for name, src := range map[string]string{
			"create":  `vikunja.tasks.create(1, {title: "x"}); return true`,
			"comment": `vikunja.tasks.comment(task.id, "x"); return true`,
			"field":   `vikunja.fields.set(task.id, 3, 1); return true`,
			"assign":  `vikunja.assignees.add(task.id, 1); return true`,
			"label":   `vikunja.labels.add(task.id, 1); return true`,
			"fetch":   `http.fetch("https://example.com/"); return true`,
			"secret":  `secrets.get("A"); return true`,
		} {
			run, _ := runWith(t, &spec.Condition{Script: src})
			assert.Equal(t, models.AutomationRunFailed, run.Status, name)
			assert.Contains(t, run.Error, "not available in a condition script", name)
		}
	})
	t.Run("reads work", func(t *testing.T) {
		run, _ := runWith(t, &spec.Condition{Script: `return vikunja.tasks.get(task.id).title === "conditioned" && vikunja.fields.get(task.id, 3) === null`})
		assert.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	})
	t.Run("in a stop_if action", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1,
			act(spec.ActionStopIf, map[string]any{"condition": map[string]any{"script": `return task.title === "Original title"`}}),
			setField("priority", 3))
		assert.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Len(t, steps, 1, "the run stopped")
	})
	t.Run("the condition of a dry run is evaluated too", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, &spec.Condition{Script: `return false`}, setField("priority", 4)))
		tk := newTask(t, projectOwned, "t", user1)
		res := dryRun(t, r, tk.ID)
		assert.False(t, res.ConditionsMet)

	})
}

// runWith2 is runWith for a test that needs its own configuration to survive reset.
func runWith2(t *testing.T, cond *spec.Condition) (*models.AutomationRun, *models.Task) {
	t.Helper()
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, cond, setField("priority", 4)))
	tk := newTask(t, projectOwned, "conditioned", user1)
	drain(t)
	return allRuns(t, r.ID)[0], getTask(t, tk.ID)
}

func TestScript_Limits(t *testing.T) {
	t.Run("timeout fails the step promptly with the reason", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsTimeout.Set(150 * time.Millisecond)
		started := time.Now()
		_, run, steps := runScript(t, `var n = 0; while (true) { n++ }`)
		assert.Less(t, time.Since(started), 5*time.Second)
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		require.Len(t, steps, 1)
		assert.Contains(t, steps[0].Error, "ran longer than 150ms")
		assert.Equal(t, models.AutomationEventDone, allEvents(t)[len(allEvents(t))-1].Status, "a timeout of the script is not retried")
	})

	t.Run("deep recursion", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsMaxCallStack.Set(64)
		_, run, steps := runScript(t, "function f(n) { return f(n + 1) }\nf(0)")
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, steps[0].Error, "maximum call stack depth of 64")
		assert.Contains(t, steps[0].Error, "line 1, column")
	})

	t.Run("huge log output is truncated in the stored step", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsMaxLogSize.Set(2000)
		_, run, steps := runScript(t, `for (var i = 0; i < 5000; i++) log("line " + i + " " + "x".repeat(200))`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, steps[0].Output, "[log output truncated]")
		assert.Less(t, len(steps[0].Output), 3000)
	})

	t.Run("vikunja calls per run", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsMaxAPICalls.Set(5)
		_, run, steps := runScript(t, `for (var i = 0; i < 20; i++) { vikunja.tasks.get(task.id) }`)
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, steps[0].Error, "more than 5 vikunja.* calls")
	})

	t.Run("http requests per run", func(t *testing.T) {
		reset(t)
		allowLoopback(t)
		srv := countingServer(t, 200, "ok")
		config.AutomationScriptsMaxHTTPRequests.Set(2)
		_, run, steps := runScript(t, `for (var i = 0; i < 10; i++) { http.fetch("`+srv.url+`/") }`)
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, steps[0].Error, "more than 2 http.fetch requests")
		assert.EqualValues(t, 2, srv.count())
	})

	t.Run("source too large when saving", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsMaxSource.Set(100)
		r := rule(spec.TriggerTaskCreated, nil, nil, scriptAction("// "+strings.Repeat("x", 200)+"\nreturn 1"))
		r.ProjectID = projectOwned
		err := saveRule(t, r)
		issues := ruleIssues(t, err)
		require.Len(t, issues, 1)
		assert.Equal(t, "actions[0].params.source", issues[0].Path)
		assert.Contains(t, issues[0].Message, "at most 100")
	})

	t.Run("source made too large after saving fails the step", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction("// "+strings.Repeat("x", 200)+"\nreturn 1")))
		config.AutomationScriptsMaxSource.Set(100)
		newTask(t, projectOwned, "t", user1)
		drain(t)
		runs := allRuns(t, r.ID)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		assert.Contains(t, runSteps(t, runs[0].ID)[0].Error, "at most 100")
	})
}

func TestScript_KillSwitch(t *testing.T) {
	reset(t)
	t.Run("rules with scripts cannot be saved", func(t *testing.T) {
		config.AutomationScriptsEnabled.Set(false)
		for name, r := range map[string]*models.AutomationRule{
			"action":    rule(spec.TriggerTaskCreated, nil, nil, scriptAction("return 1")),
			"condition": rule(spec.TriggerTaskCreated, nil, &spec.Condition{Script: "return true"}, setField("priority", 1)),
			"stop_if":   rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionStopIf, map[string]any{"condition": map[string]any{"script": "return true"}})),
		} {
			r.ProjectID = projectOwned
			err := saveRule(t, r)
			require.Error(t, err, name)
			assert.True(t, models.IsErrAutomationScriptsDisabled(err), "%s: %v", name, err)
			assert.Equal(t, 400, err.(models.ErrAutomationScriptsDisabled).HTTPError().HTTPCode)
		}
		r := rule(spec.TriggerTaskCreated, nil, nil, setField("priority", 1))
		r.ProjectID = projectOwned
		assert.NoError(t, saveRule(t, r), "rules without scripts are not affected")
	})

	t.Run("existing rules fail the step without executing", func(t *testing.T) {
		reset(t)
		a := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
			scriptAction(`vikunja.tasks.update(task.id, {title: "script ran"})`)))
		c := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, &spec.Condition{Script: `return true`}, setField("priority", 3)))
		config.AutomationScriptsEnabled.Set(false)
		task := newTask(t, projectOwned, "untouched", user1)
		drain(t)

		runs := allRuns(t, a.ID)
		require.Len(t, runs, 1)
		assert.Equal(t, models.AutomationRunFailed, runs[0].Status)
		steps := runSteps(t, runs[0].ID)
		require.Len(t, steps, 1)
		assert.Contains(t, steps[0].Error, "scripts are disabled on this instance")
		assert.Contains(t, steps[0].Error, "automation.scripts.enabled")

		cRuns := allRuns(t, c.ID)
		assert.Equal(t, models.AutomationRunFailed, cRuns[0].Status)
		assert.Contains(t, cRuns[0].Error, "scripts are disabled")

		got := getTask(t, task.ID)
		assert.Equal(t, "untouched", got.Title)
		assert.EqualValues(t, 0, got.Priority)
	})

	t.Run("the dry run says so", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction("return 1")))
		tk := newTask(t, projectOwned, "t", user1)
		config.AutomationScriptsEnabled.Set(false)
		res := dryRun(t, r, tk.ID)
		assert.Contains(t, res.Steps[0].Error, "scripts are disabled")
	})
}

func TestScript_SyntaxErrorsAreReportedWithPathLineAndColumn(t *testing.T) {
	reset(t)
	r := rule(spec.TriggerTaskCreated, nil, nil,
		setField("priority", 1),
		setField("priority", 2),
		scriptAction("var ok = 1;\nvar b = ;"))
	r.ProjectID = projectOwned
	issues := ruleIssues(t, saveRule(t, r))
	require.Len(t, issues, 1)
	assert.Equal(t, "actions[2].params.source", issues[0].Path)
	assert.Equal(t, 2, issues[0].Line)
	assert.Equal(t, 9, issues[0].Column)
	assert.Contains(t, issues[0].Message, "syntax error")

	r = rule(spec.TriggerTaskCreated, nil, &spec.Condition{All: []*spec.Condition{{Field: "done", Op: "=", Value: false}, {Script: "return ("}}}, setField("priority", 1))
	r.ProjectID = projectOwned
	issues = ruleIssues(t, saveRule(t, r))
	require.Len(t, issues, 1)
	assert.Equal(t, "conditions.all[1].script", issues[0].Path)
	assert.Equal(t, 1, issues[0].Line)

	r = rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionStopIf, map[string]any{"condition": map[string]any{"script": "if ("}}))
	r.ProjectID = projectOwned
	issues = ruleIssues(t, saveRule(t, r))
	require.Len(t, issues, 1)
	assert.Equal(t, "actions[0].params.condition.script", issues[0].Path)
}

func TestScript_StepOutputSummarisesTheRun(t *testing.T) {
	reset(t)
	_, run, steps := runScript(t, `log("one"); log({a: [1, 2]}); return "done"`)
	require.Equal(t, models.AutomationRunSuccess, run.Status)
	assert.Contains(t, steps[0].Output, "script finished in")
	assert.Contains(t, steps[0].Output, "log:\none\n{\"a\":[1,2]}")
	assert.Contains(t, steps[0].Output, `result: "done"`)

	reset(t)
	_, _, steps = runScript(t, `return "x".repeat(40000)`)
	assert.LessOrEqual(t, len(steps[0].Output), maxOutputBytes)
}

func TestScript_UncaughtErrorsHaveTheStack(t *testing.T) {
	reset(t)
	_, run, steps := runScript(t, "var x = 1;\nfunction explode() {\n  throw new RangeError('too big');\n}\nexplode();")
	assert.Equal(t, models.AutomationRunFailed, run.Status)
	require.Len(t, steps, 1)
	assert.Contains(t, steps[0].Error, "RangeError: too big")
	assert.Contains(t, steps[0].Error, "at explode (line 3, column 9)")
	assert.Contains(t, steps[0].Error, "line 5, column 8")
	assert.Contains(t, run.Error, "RangeError: too big")
}

func TestScript_ExampleScriptsFromTheDocumentation(t *testing.T) {
	reset(t)
	// Project 1: custom field 3 is a number. When a subtask is done, the sum over the subtasks goes to the parent.
	parent := newTask(t, projectOwned, "parent", user1)
	var children []*models.Task
	for i, value := range []float64{10, 5.5, 4} {
		child := newTask(t, projectOwned, "child", user1)
		mustCommit(t, func(s *xorm.Session) error {
			rel := &models.TaskRelation{TaskID: parent.ID, OtherTaskID: child.ID, RelationKind: models.RelationKindSubtask}
			return rel.Create(s, user1)
		})
		mustCommit(t, func(s *xorm.Session) error {
			return (&models.CustomFieldValue{TaskID: child.ID, CustomFieldID: 3, Value: value}).Update(s, user1)
		})
		_ = i
		children = append(children, child)
	}
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCompleted, nil, nil, scriptAction(SumIntoParentExample)))
	updateTask(t, user1, children[0].ID, func(x *models.Task) { x.Done = true }, "done")
	drain(t)
	run := allRuns(t, r.ID)[0]
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)

	s := newSession()
	defer s.Close()
	values, err := models.GetCustomFieldValuesOfTask(s, parent)
	require.NoError(t, err)
	for _, v := range values {
		if v.CustomFieldID == 3 {
			assert.InDelta(t, 19.5, v.Value, 0.001)
		}
	}
}

// SumIntoParentExample is the first example of the documentation: when a task is done, the number field of all
// subtasks of its parent is summed into the parent.
const SumIntoParentExample = `
const FIELD = 3; // the id of a number custom field, e.g. "Hours"
const parentId = (task.relations.parenttask || [])[0];
if (!parentId) return "no parent";
let sum = 0;
for (const id of vikunja.tasks.get(parentId).relations.subtask || []) {
  const value = vikunja.fields.get(id, FIELD);
  if (typeof value === "number") sum += value;
}
vikunja.fields.set(parentId, FIELD, sum);
return {parent: parentId, sum: sum};
`
