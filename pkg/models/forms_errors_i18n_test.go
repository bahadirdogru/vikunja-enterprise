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

// The public page of a form says the answer errors of the server in its own language by their code
// (forms.public.errors.server.<code>); the English text next to the code is the fallback for other clients.
func TestEveryFormAnswerErrorCodeIsTranslated(t *testing.T) {
	src, err := os.ReadFile("forms_submit.go")
	require.NoError(t, err)
	found := map[string]bool{}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`FormFieldError\{[^}]*Code: "([a-z_]+)"`),
		regexp.MustCompile(`\b(?:fail|one)\([^"]*"([a-z_]+)", "`),
		regexp.MustCompile(`\bfail\([^,]+, "([a-z_]+)"`),
	} {
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			found[m[1]] = true
		}
	}
	// The codes the documentation of FormFieldError lists, in case a site builds its code at run time.
	for _, c := range []string{"required", "invalid", "too_long", "too_many", "unknown_field", "file_too_large", "file_type_not_allowed", "attachments_disabled"} {
		found[c] = true
	}
	require.GreaterOrEqual(t, len(found), 8)

	for _, file := range []string{"en.json", "tr-TR.json"} {
		raw, err := os.ReadFile("../../frontend/src/i18n/lang/" + file)
		require.NoError(t, err)
		var lang struct {
			Forms struct {
				Public struct {
					Errors struct {
						Server map[string]string `json:"server"`
					} `json:"errors"`
				} `json:"public"`
			} `json:"forms"`
		}
		require.NoError(t, json.Unmarshal(raw, &lang))
		for code := range found {
			assert.NotEmpty(t, lang.Forms.Public.Errors.Server[code], "%s has no text for the form answer error %s (forms.public.errors.server.%s)", file, code, code)
		}
	}
}
