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
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type limitProblem struct {
	Code       int               `json:"code"`
	I18nParams map[string]string `json:"i18n_params"`
}

func TestHumaCustomFieldLimits(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)

	problemOf := func(t *testing.T, body []byte) limitProblem {
		t.Helper()
		var p limitProblem
		require.NoError(t, json.Unmarshal(body, &p), string(body))
		return p
	}

	t.Run("the limit of fields answers 409 with the limit as parameter", func(t *testing.T) {
		config.CustomFieldsMaxFieldsPerProject.Set(4)
		t.Cleanup(func() { config.CustomFieldsMaxFieldsPerProject.Set(100) })

		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-fields", `{"title":"one more","type":"text"}`, user1, "")
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		p := problemOf(t, rec.Body.Bytes())
		assert.Equal(t, 20006, p.Code)
		assert.Equal(t, "4", p.I18nParams["limit"])
	})

	t.Run("the limit of options answers 400 with the limit as parameter", func(t *testing.T) {
		config.CustomFieldsMaxOptionsPerField.Set(2)
		t.Cleanup(func() { config.CustomFieldsMaxOptionsPerField.Set(200) })

		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/custom-fields", `{"title":"many","type":"select","options":[{"title":"a"},{"title":"b"},{"title":"c"}]}`, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		p := problemOf(t, rec.Body.Bytes())
		assert.Equal(t, 20007, p.Code)
		assert.Equal(t, "2", p.I18nParams["limit"])
	})

	t.Run("a text value of more than 65535 bytes answers 400 with 20008", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"value":"`+strings.Repeat("a", 65535)+`"}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String()[:min(200, rec.Body.Len())])

		rec = humaRequest(t, e, http.MethodPut, "/api/v2/tasks/1/custom-field-values/1", `{"value":"`+strings.Repeat("€", 21846)+`"}`, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		p := problemOf(t, rec.Body.Bytes())
		assert.Equal(t, 20008, p.Code)
		assert.Equal(t, "65535", p.I18nParams["limit"])
	})
}
