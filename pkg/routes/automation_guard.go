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

	"code.vikunja.io/api/pkg/automation/outbound"

	"github.com/labstack/echo/v5"
)

// rejectAutomationRequests refuses every API request that the automation engine sent itself (http.request, http.fetch).
// The engine marks its outgoing requests; without this a rule that holds an API token could call the task API of this
// server and create tasks that no run, origin or depth accounts for, and the incoming webhook endpoint would let a rule
// start rules. A proxy that strips the header, or a second hop through an external service that calls back without it,
// is outside what this can see.
func rejectAutomationRequests() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Request().Header.Get(outbound.MarkerHeader) != "" {
				return echo.NewHTTPError(http.StatusLoopDetected, "requests sent by an automation rule cannot call the API of this server")
			}
			return next(c)
		}
	}
}
