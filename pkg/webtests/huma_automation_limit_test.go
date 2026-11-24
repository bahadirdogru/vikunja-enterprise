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

package webtests

import (
	"net/http"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHumaAutomation_RuleLimitPerProject(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	old := config.AutomationMaxRulesPerProject.GetInt()
	t.Cleanup(func() { config.AutomationMaxRulesPerProject.Set(old) })
	config.AutomationMaxRulesPerProject.Set(2)

	a := createAutomationRule(t, e, 1, user1, strings.Replace(priorityRule, "Urgent first", "A", 1))
	b := createAutomationRule(t, e, 1, user1, strings.Replace(priorityRule, "Urgent first", "B", 1))

	t.Run("the rule over the limit is a 409 with code 22014", func(t *testing.T) {
		rec := humaRequestHandler(t, e, http.MethodPost, autoRulesPath(1), priorityRule, user1)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeAutomationRuleLimitReached, decodeProblem(t, rec.Body.Bytes()).Code)
		assert.Equal(t, 22014, models.ErrCodeAutomationRuleLimitReached)
	})

	t.Run("another project is not affected", func(t *testing.T) {
		createAutomationRule(t, e, 11, user1, priorityRule)
	})

	t.Run("a project over the limit keeps working but takes no new rule", func(t *testing.T) {
		config.AutomationMaxRulesPerProject.Set(1)
		rec := humaRequest(t, e, http.MethodPatch, autoRulePath(1, a.ID), `{"title":"Renamed","enabled":false}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = humaRequestHandler(t, e, http.MethodPost, autoRulesPath(1), priorityRule, user1)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodDelete, autoRulePath(1, a.ID), "", user1, "")
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		rec = humaRequestHandler(t, e, http.MethodPost, autoRulesPath(1), priorityRule, user1)
		require.Equal(t, http.StatusConflict, rec.Code, "one rule left, the limit is 1: "+rec.Body.String())
		rec = humaRequest(t, e, http.MethodDelete, autoRulePath(1, b.ID), "", user1, "")
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		createAutomationRule(t, e, 1, user1, priorityRule)
	})
}
