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
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every reason of a custom field that the server can name has a code and a text in both language files.
func TestEveryCustomFieldReasonIsTranslated(t *testing.T) {
	for _, file := range []string{"en.json", "tr-TR.json"} {
		raw, err := os.ReadFile("../../frontend/src/i18n/lang/" + file)
		require.NoError(t, err)
		var lang struct {
			CustomFields struct {
				Reasons map[string]string `json:"reasons"`
			} `json:"customFields"`
		}
		require.NoError(t, json.Unmarshal(raw, &lang))
		for _, code := range CustomFieldReasonCodes() {
			assert.NotEmpty(t, lang.CustomFields.Reasons[code], "%s has no text for the custom field reason %s (customFields.reasons.%s)", file, code, code)
		}
	}
}

// Every reason the code of custom fields writes has a code: a reason added to custom_fields.go or
// custom_field_values.go without one would reach a Turkish reader in English.
func TestEveryCustomFieldReasonInTheCodeHasACode(t *testing.T) {
	literal := regexp.MustCompile(`(?:ErrCustomFieldInvalidDefinition\{Reason: |invalidValue\(f, )"([^"]+)"`)
	for _, file := range []string{"custom_fields.go", "custom_field_values.go", "custom_field_limits.go"} {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		for _, m := range literal.FindAllStringSubmatch(string(src), -1) {
			assert.NotEmpty(t, customFieldReasonParams(m[1])["reason_code"], "%s: the reason %q has no code", file, m[1])
		}
	}
	assert.Equal(t, "unknown_type", customFieldReasonParams("unknown type foo")["reason_code"])
	assert.Equal(t, "foo", customFieldReasonParams("unknown type foo")["reason_type"])
	assert.Equal(t, "65535", customFieldReasonParams(ErrCustomFieldValueTooLong{Limit: 65535}.reason())["reason_limit"])
	assert.Empty(t, customFieldReasonParams("a reason nobody coded")["reason_code"])
}
