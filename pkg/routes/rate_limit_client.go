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
	"time"

	"github.com/labstack/echo/v5"
	"github.com/ulule/limiter/v3"
)

// perMinuteClientRateLimit is perMinuteIPRateLimit for the routes anybody can call (the public form routes and the
// incoming webhooks): an IPv6 client is counted by its /64 prefix, so one subscriber cannot spread over the 2^64
// addresses of its network to get a limit per address.
func perMinuteClientRateLimit(prefix string, limit int64) echo.MiddlewareFunc {
	rate := limiter.Rate{
		Period: 60 * time.Second,
		Limit:  limit,
	}
	return RateLimit(createRateLimiter(prefix, rate), "ip64")
}
