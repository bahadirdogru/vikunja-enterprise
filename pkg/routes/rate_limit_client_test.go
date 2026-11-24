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

package routes

import (
	"net/http"
	"testing"

	"code.vikunja.io/api/pkg/log"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The limits of the public routes (forms, incoming webhooks) count an IPv6 client by its /64 prefix.
func TestRateLimitClientKey(t *testing.T) {
	log.InitLogger()
	e := echo.New()

	tooMany := func(t *testing.T, err error) {
		t.Helper()
		var he *echo.HTTPError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, http.StatusTooManyRequests, he.Code)
	}

	t.Run("the addresses of one /64 share one limit", func(t *testing.T) {
		h, calls := rateLimitTestHandler("ip64", 2)
		for _, addr := range []string{"[2001:db8:1:2::1]:1234", "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:1234"} {
			c, _ := newRateLimitTestContext(e, addr)
			require.NoError(t, h(c))
		}
		c, _ := newRateLimitTestContext(e, "[2001:db8:1:2:1:2:3:4]:1234")
		tooMany(t, h(c))
		assert.Equal(t, 2, *calls)

		other, _ := newRateLimitTestContext(e, "[2001:db8:1:3::1]:1234")
		require.NoError(t, h(other), "another /64 has its own limit")
	})

	t.Run("IPv4 clients are counted per address", func(t *testing.T) {
		h, _ := rateLimitTestHandler("ip64", 1)
		c, _ := newRateLimitTestContext(e, "1.2.3.4:1234")
		require.NoError(t, h(c))
		c, _ = newRateLimitTestContext(e, "1.2.3.4:4321")
		tooMany(t, h(c))
		c, _ = newRateLimitTestContext(e, "1.2.3.5:1234")
		require.NoError(t, h(c))
	})

	t.Run("the limit of the other routes is unchanged", func(t *testing.T) {
		h, _ := rateLimitTestHandler("ip", 1)
		c, _ := newRateLimitTestContext(e, "[2001:db8:1:2::1]:1234")
		require.NoError(t, h(c))
		c, _ = newRateLimitTestContext(e, "[2001:db8:1:2::2]:1234")
		require.NoError(t, h(c), "the shared helper of the upstream routes still counts full addresses")
	})
}
