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
	"net/http"
	"testing"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/files"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setRuleLimit(t *testing.T, limit int) {
	t.Helper()
	old := config.AutomationMaxRulesPerProject.GetInt()
	config.AutomationMaxRulesPerProject.Set(limit)
	t.Cleanup(func() { config.AutomationMaxRulesPerProject.Set(old) })
}

func countProjectRules(t *testing.T, projectID int64) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("project_id = ?", projectID).Count(&AutomationRule{})
	require.NoError(t, err)
	return n
}

func TestAutomationRule_Limit(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	existing := countProjectRules(t, 1)
	setRuleLimit(t, int(existing)+2)

	first := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(2)), autoUser1)

	t.Run("the rule over the limit is refused with its own error", func(t *testing.T) {
		err := saveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(3)), autoUser1)
		require.Error(t, err)
		assert.True(t, IsErrAutomationRuleLimitReached(err), "got %T: %v", err, err)
		he := err.(ErrAutomationRuleLimitReached).HTTPError()
		assert.Equal(t, ErrCodeAutomationRuleLimitReached, he.Code)
		assert.Equal(t, http.StatusConflict, he.HTTPCode)
		assert.Equal(t, existing+2, countProjectRules(t, 1))
	})

	t.Run("another project has its own count", func(t *testing.T) {
		mustSaveAutomationRule(t, autoRule(11, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	})

	t.Run("deleting a rule makes room again", func(t *testing.T) {
		s := db.NewSession()
		require.NoError(t, (&AutomationRule{ID: first.ID, ProjectID: 1}).Delete(s, autoUser1))
		require.NoError(t, s.Commit())
		s.Close()
		mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(4)), autoUser1)
	})

	t.Run("an invalid rule over the limit is still refused for the limit first", func(t *testing.T) {
		bad := autoRule(1, spec.TriggerTaskCreated, setPriority(1))
		bad.Title = ""
		assert.True(t, IsErrAutomationRuleLimitReached(saveAutomationRule(t, bad, autoUser1)))
	})
}

func TestAutomationRule_ProjectOverTheLimitKeepsWorking(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	a := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	b := mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(2)), autoUser1)
	mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(3)), autoUser1)

	// The limit is lowered below what the project has.
	setRuleLimit(t, 1)
	require.Greater(t, countProjectRules(t, 1), int64(1))

	t.Run("new rules are refused", func(t *testing.T) {
		assert.True(t, IsErrAutomationRuleLimitReached(saveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(9)), autoUser1)))
	})
	t.Run("existing rules can be edited and disabled", func(t *testing.T) {
		edit := autoRule(1, spec.TriggerTaskCreated, setPriority(5))
		edit.ID = a.ID
		edit.Title = "renamed"
		require.NoError(t, saveAutomationRule(t, edit, autoUser1))

		off := autoRule(1, spec.TriggerTaskCreated, setPriority(2))
		off.ID = b.ID
		off.Enabled = false
		require.NoError(t, saveAutomationRule(t, off, autoUser1))
		db.AssertExists(t, "automation_rules", map[string]interface{}{"id": a.ID, "title": "renamed"}, false)
		db.AssertExists(t, "automation_rules", map[string]interface{}{"id": b.ID, "enabled": false}, false)
	})
	t.Run("existing rules can be deleted, and adding works again below the limit", func(t *testing.T) {
		for {
			rules := []*AutomationRule{}
			s := db.NewSession()
			require.NoError(t, s.Where("project_id = ?", 1).Find(&rules))
			s.Close()
			if len(rules) == 0 {
				break
			}
			s = db.NewSession()
			require.NoError(t, rules[0].Delete(s, autoUser1))
			require.NoError(t, s.Commit())
			s.Close()
		}
		mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
		assert.True(t, IsErrAutomationRuleLimitReached(saveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)))
	})
}

func TestAutomationRule_LimitWhenDuplicatingAProject(t *testing.T) {
	files.InitTestFileFixtures(t)
	db.LoadAndAssertFixtures(t)
	mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(1)), autoUser1)
	mustSaveAutomationRule(t, autoRule(1, spec.TriggerTaskCreated, setPriority(2)), autoUser1)
	rules := countProjectRules(t, 1)
	require.GreaterOrEqual(t, rules, int64(2))

	t.Run("a project over the limit cannot be duplicated, nothing is half copied", func(t *testing.T) {
		setRuleLimit(t, int(rules)-1)
		dup := &ProjectDuplicate{ProjectID: 1}
		s := db.NewSession()
		defer s.Close()
		can, err := dup.CanCreate(s, autoUser1)
		require.NoError(t, err)
		require.True(t, can)
		err = dup.Create(s, autoUser1)
		_ = s.Rollback()
		require.Error(t, err)
		assert.True(t, IsErrAutomationRuleLimitReached(err), "got %T: %v", err, err)
	})

	t.Run("a project within the limit is duplicated with all rules", func(t *testing.T) {
		setRuleLimit(t, int(rules))
		dup := &ProjectDuplicate{ProjectID: 1}
		duplicateProject(t, dup)
		assert.Equal(t, rules, countProjectRules(t, dup.Project.ID))
	})
}
