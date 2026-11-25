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
	"net/http/httptest"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The per-minute floor of the public routes (public forms and the incoming webhooks share the setting
// forms.public.requestratelimit, each route group has its own counter) counts an IPv6 client by its /64 prefix, through
// the real router.
func TestPublicRoutesCountIPv6ClientsByPrefix(t *testing.T) {
	for _, route := range []struct{ name, method, path, body string }{
		{"forms", http.MethodGet, "/api/v2/forms/nope", ""},
		{"incoming webhooks", http.MethodPost, "/api/v2/automation/hooks/nope", "{}"},
	} {
		t.Run(route.name, func(t *testing.T) {
			config.FormsPublicRequestRateLimit.Set(3)
			t.Cleanup(func() { config.FormsPublicRequestRateLimit.Set(60) })
			e, err := setupTestEnv()
			require.NoError(t, err)

			from := func(remote string) int {
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				req.Header.Set("Content-Type", "application/json")
				req.RemoteAddr = remote
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				return rec.Code
			}

			// Three different addresses of one /64 use up one budget of three.
			for i, addr := range []string{"[2001:db8:1:2::1]:4000", "[2001:db8:1:2::2]:4000", "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:4000"} {
				assert.NotEqual(t, http.StatusTooManyRequests, from(addr), "request %d is within the floor", i+1)
			}
			assert.Equal(t, http.StatusTooManyRequests, from("[2001:db8:1:2::99]:4000"), "a fourth address of the same /64 is refused")
			assert.NotEqual(t, http.StatusTooManyRequests, from("[2001:db8:1:3::1]:4000"), "another /64 has its own budget")

			// IPv4 stays per address.
			for i := 0; i < 3; i++ {
				assert.NotEqual(t, http.StatusTooManyRequests, from("198.51.100.1:4000"))
			}
			assert.Equal(t, http.StatusTooManyRequests, from("198.51.100.1:4001"))
			assert.NotEqual(t, http.StatusTooManyRequests, from("198.51.100.2:4000"))
		})
	}
}
