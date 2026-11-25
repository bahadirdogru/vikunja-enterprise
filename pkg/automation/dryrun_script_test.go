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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A dry run executes the script and plans every write it makes instead of doing it (docs/modules/automation.md:
// "without writing anything"). The script below makes every kind of write the host offers; afterwards no table the
// run could touch has changed, and each write shows in the summary of the step.
func TestDryRun_ScriptWritesArePlannedNotMade(t *testing.T) {
	reset(t)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil, nil, scriptAction(`
		vikunja.tasks.update(task.id, {title: "renamed by the script", priority: 3});
		vikunja.tasks.create(task.project_id, {title: "child by the script", parent_id: task.id});
		vikunja.tasks.comment(task.id, "commented by the script");
		vikunja.fields.set(task.id, 1, "from the script");
		vikunja.assignees.add(task.id, 1);
		vikunja.assignees.remove(task.id, 1);
		vikunja.labels.add(task.id, 1);
		vikunja.labels.remove(task.id, 1);
		return "planned";`)))
	task := newTask(t, projectOwned, "dry script", user1)
	before := fingerprint(t, task.ID)
	eventsBefore := len(allEvents(t))

	res := dryRun(t, r, task.ID)

	assert.Equal(t, before, fingerprint(t, task.ID), "a dry run of a script changes nothing")
	assert.Len(t, allEvents(t), eventsBefore, "and records no events")
	require.Len(t, res.Steps, 1)
	assert.Empty(t, res.Steps[0].Error)
	assert.NotEmpty(t, res.Steps[0].Summary)
	for _, planned := range []string{"renamed by the script", "child by the script", "commented by the script", "from the script"} {
		assert.Contains(t, res.Steps[0].Summary, planned, "the plan names what the script would write")
	}
}

// Calls that need a workflow status or that a script has no right to make are reported as the step's error, not made.
func TestDryRun_ScriptStatusChangeIsCheckedLikeARealOne(t *testing.T) {
	reset(t)
	changeStatus(t, 20, 2)
	r := insertRawRule(t, projectWorkflow, user1, spec.TriggerTaskUpdated,
		scriptAction(`vikunja.status.set(task.id, 3, "from the script"); return "x"`))
	before := fingerprint(t, 20)

	res := dryRun(t, r, 20)

	assert.Equal(t, before, fingerprint(t, 20))
	require.Len(t, res.Steps, 1)
	assert.Contains(t, res.Steps[0].Error, "Workflow requirements not met", "the transition has requirements task 20 does not meet")
}
