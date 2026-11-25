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
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/modules/custom-fields.md: "An unknown field id, a field of a project the caller cannot read and a malformed
// identifier in a filter all answer the same 400 (4016), so field ids of other projects cannot be probed." The status
// alone is not the promise: the code and the shape of the answer are the same for the three, and the answer of the
// caller who may read the field differs from them by the filter being valid.
func TestHumaCustomFieldIdsCannotBeProbed(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2) // no access to project 1, which owns the custom fields 1 to 4

	get := func(token, filter string) (int, problemDTO, string) {
		q := url.Values{"filter": {filter}}
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/tasks?"+q.Encode(), "", token, "")
		var p problemDTO
		if rec.Code >= 400 {
			p = decodeProblem(t, rec.Body.Bytes())
		}
		return rec.Code, p, rec.Body.String()
	}

	code, _, body := get(user1, "cf_1 = REF-1")
	require.Equal(t, http.StatusOK, code, body)

	statusUnreadable, unreadable, _ := get(user2, "cf_1 = REF-1")
	statusUnknown, unknown, _ := get(user2, "cf_999999 = REF-1")
	statusMalformed, malformed, _ := get(user2, "cf_abc = REF-1")
	statusOfOwner, owner, _ := get(user1, "cf_999999 = REF-1")

	for name, status := range map[string]int{"unreadable": statusUnreadable, "unknown": statusUnknown, "malformed": statusMalformed, "unknown for the owner": statusOfOwner} {
		assert.Equal(t, http.StatusBadRequest, status, name)
	}
	assert.NotZero(t, unreadable.Code)
	assert.Equal(t, unknown.Code, unreadable.Code, "a field of a project the caller cannot read answers like an unknown one")
	assert.Equal(t, unknown.Code, malformed.Code, "and so does a malformed identifier")
	assert.Equal(t, unknown.Code, owner.Code, "whoever asks")
	// The message may name the identifier that was asked for, and must say nothing else about it.
	strip := func(p problemDTO, id string) string { return strings.ReplaceAll(p.Detail, id, "<field>") }
	assert.Equal(t, strip(unknown, "cf_999999"), strip(unreadable, "cf_1"))
}
