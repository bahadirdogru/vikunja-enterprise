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

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientRateLimitKey(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7":                      "203.0.113.7",
		"::ffff:203.0.113.7":               "203.0.113.7",
		"2001:db8:1:2:3:4:5:6":             "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff:ffff:ffff:ffff": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":                  "2001:db8:1:3::/64",
		"2001:DB8:1:2::1%eth0":             "2001:db8:1:2::/64",
		"::1":                              "::/64",
		"":                                 "",
		"@":                                "@",
		"not an address":                   "not an address",
	}
	for in, want := range cases {
		assert.Equal(t, want, ClientRateLimitKey(in), in)
	}
	assert.Equal(t, ClientRateLimitKey("2001:db8:1:2::1"), ClientRateLimitKey("2001:db8:1:2:aaaa:bbbb:cccc:dddd"), "one /64 is one client")
	assert.NotEqual(t, ClientRateLimitKey("2001:db8:1:2::1"), ClientRateLimitKey("2001:db8:1:3::1"), "two /64 are two clients")
}
