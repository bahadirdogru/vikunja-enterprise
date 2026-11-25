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
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

// The error codes of API.md that no web test reached by number: each one comes back in the body of the problem with the
// documented HTTP status, and not only as a 400 that any failed validation would give. Fixtures: project 1 has the
// custom fields 1 (text), 2 (select with options 1 and 2), 3 (number up to 1000) and 4 (multi select with 3 and 4).
func TestHumaForkErrorCodesReachTheClient(t *testing.T) {
	w := newMatrixWorld(t)
	owner := w.callers[5].token
	require.Equal(t, "owner", w.callers[5].name)
	P := w.ids["P"]

	type probe struct {
		name         string
		method, path string
		body         string
		status, code int
	}
	probes := []probe{
		{"a field that does not exist", http.MethodGet, "/projects/1/custom-fields/9999", "", 404, models.ErrCodeCustomFieldDoesNotExist},
		{"a field with an invalid definition", http.MethodPost, "/projects/1/custom-fields", `{"title":"Cost","type":"number","config":{"min":10,"max":1}}`, 400, models.ErrCodeCustomFieldInvalidDefinition},
		{"the type of a field cannot change", http.MethodPut, "/projects/1/custom-fields/1", `{"title":"Reference","type":"number"}`, 400, models.ErrCodeCustomFieldTypeImmutable},
		{"a number above the maximum", http.MethodPut, "/tasks/1/custom-field-values/3", `{"value":5000}`, 400, models.ErrCodeCustomFieldInvalidValue},
		{"text where a number is expected", http.MethodPut, "/tasks/1/custom-field-values/3", `{"value":"many"}`, 400, models.ErrCodeCustomFieldInvalidValue},
		{"an option of another field as a value", http.MethodPut, "/tasks/1/custom-field-values/2", `{"value":3}`, 400, models.ErrCodeCustomFieldInvalidValue},
		{"an option that does not exist on the field", http.MethodPut, "/projects/1/custom-fields/2", `{"title":"Choice","type":"select","options":[{"id":99999,"title":"ghost"}]}`, 404, models.ErrCodeCustomFieldOptionDoesNotExist},
		{"an invalid secret name", http.MethodPut, "/projects/" + P + "/automation-secrets/1bad-name", `{"value":"x"}`, 400, models.ErrCodeAutomationSecretInvalid},
		{"a dashboard that does not exist", http.MethodGet, "/dashboards/99999", "", 404, models.ErrCodeDashboardDoesNotExist},
		{"a workflow that does not exist", http.MethodGet, "/projects/1/workflow", "", 404, models.ErrCodeWorkflowDoesNotExist},
		{"a rule that does not exist", http.MethodGet, "/projects/" + P + "/automations/99999", "", 404, models.ErrCodeAutomationRuleDoesNotExist},
		{"a run that does not exist", http.MethodGet, "/projects/" + P + "/automations/" + w.ids["R"] + "/runs/99999", "", 404, models.ErrCodeAutomationRunDoesNotExist},
		{"a secret that does not exist", http.MethodDelete, "/projects/" + P + "/automation-secrets/NOPE", "", 404, models.ErrCodeAutomationSecretDoesNotExist},
		{"a form that does not exist", http.MethodGet, "/forms/not-a-form", "", 404, models.ErrCodeFormDoesNotExist},
		{"an approval request that does not exist", http.MethodGet, "/approvals/99999", "", 404, models.ErrCodeApprovalRequestDoesNotExist},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			rec := humaRequest(t, w.e, p.method, "/api/v2"+p.path, p.body, owner, "")
			assert.Equal(t, p.status, rec.Code, rec.Body.String())
			assert.Equal(t, p.code, decodeProblem(t, rec.Body.Bytes()).Code, rec.Body.String())
		})
	}

	t.Run("a link share cannot ask for an approval, with its own code", func(t *testing.T) {
		var doc map[string]any
		require.NoError(t, json.Unmarshal([]byte(w.ids["WORKFLOW"]), &doc))
		toInt := func(k string) int64 { n, _ := strconv.ParseInt(w.ids[k], 10, 64); return n }
		doc["transitions"] = []any{map[string]any{
			"from_status_id": toInt("STATUS_TODO"), "to_status_id": toInt("STATUS_DONE"), "requires_approval": true,
			"approval": map[string]any{"mode": "any", "approvers": []any{map[string]any{"type": "user", "id": 6}}},
		}}
		body, err := json.Marshal(doc)
		require.NoError(t, err)
		rec := humaRequest(t, w.e, http.MethodPut, "/api/v2/projects/"+P+"/workflow", string(body), owner, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		share := w.callers[7] // write permission
		require.Equal(t, "share-write", share.name)
		rec = humaRequest(t, w.e, http.MethodPost, "/api/v2/tasks/"+w.ids["T"]+"/status", `{"to_status_id":`+w.ids["STATUS_DONE"]+`}`, share.token, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeApprovalLinkShare, decodeProblem(t, rec.Body.Bytes()).Code)
		db.AssertCount(t, "approval_requests", builder.Eq{"task_id": toInt("T")}, 2) // the two of the world, none new
	})

	t.Run("an approver who decided cannot decide again while the others have not", func(t *testing.T) {
		rec := humaRequest(t, w.e, http.MethodPost, "/api/v2/tasks/"+w.ids["T"]+"/approvals",
			`{"title":"Both","approver_ids":[2,6],"mode":"all"}`, owner, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		id := idOf(t, rec.Body.Bytes())
		approver := w.callers[4]
		require.Equal(t, "admin", approver.name)

		rec = humaRequest(t, w.e, http.MethodPost, "/api/v2/approvals/"+id+"/decision", `{"decision":"approved"}`, approver.token, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = humaRequest(t, w.e, http.MethodPost, "/api/v2/approvals/"+id+"/decision", `{"decision":"approved"}`, approver.token, "")
		assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeApprovalAlreadyDecided, decodeProblem(t, rec.Body.Bytes()).Code)
	})

	t.Run("a report with more rows or cells than the instance allows", func(t *testing.T) {
		config.ReportsMaxRows.Set(1)
		t.Cleanup(func() { config.ReportsMaxRows.Set(50000) })
		rec := humaRequest(t, w.e, http.MethodPost, "/api/v2/reports/tasks/export/rows", `{"project_id":1}`, owner, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeReportTooManyRows, decodeProblem(t, rec.Body.Bytes()).Code)

		config.ReportsMaxCells.Set(1)
		t.Cleanup(func() { config.ReportsMaxCells.Set(50000) })
		rec = humaRequest(t, w.e, http.MethodPost, "/api/v2/reports/tasks", `{"project_id":1,"group_by":"done"}`, owner, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeReportTooLarge, decodeProblem(t, rec.Body.Bytes()).Code)
	})
}
