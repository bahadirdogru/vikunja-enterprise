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

package script

import (
	"time"

	"code.vikunja.io/api/pkg/config"
)

// Limits bounds one script run.
type Limits struct {
	// Timeout is the wall-clock limit of the whole run.
	Timeout time.Duration
	// MaxAPICalls is the number of vikunja.* calls.
	MaxAPICalls int
	// MaxHTTPRequests is the number of http.fetch calls.
	MaxHTTPRequests int
	// MaxLogBytes is the total size of log output.
	MaxLogBytes int
	// MaxCallStack is the call stack depth of the interpreter.
	MaxCallStack int
	// MaxResults caps tasks.find.
	MaxResults int
	// MaxExportNodes bounds the size of values crossing from the script to the host.
	MaxExportNodes int
	// MaxValueBytes bounds a single string crossing from the script to the host.
	MaxValueBytes int
	// MaxMemory is the memory limit of the script process in bytes (0: none). A hard limit (of the address space)
	// on Linux only; elsewhere the soft limit of the Go runtime.
	MaxMemory int64
}

// ActionLimits are the limits of a script action, from the configuration.
func ActionLimits() Limits {
	return Limits{
		Timeout:         config.AutomationScriptsTimeout.GetDuration(),
		MaxAPICalls:     config.AutomationScriptsMaxAPICalls.GetInt(),
		MaxHTTPRequests: config.AutomationScriptsMaxHTTPRequests.GetInt(),
		MaxLogBytes:     config.AutomationScriptsMaxLogSize.GetInt(),
		MaxCallStack:    config.AutomationScriptsMaxCallStack.GetInt(),
		MaxResults:      config.AutomationScriptsMaxResults.GetInt(),
		MaxExportNodes:  100000,
		MaxValueBytes:   config.AutomationScriptsMaxResponseSize.GetInt(),
		MaxMemory:       config.AutomationScriptsMaxMemory.GetInt64(),
	}
}

// ConditionLimits are the limits of a script condition: the same, with the shorter timeout.
func ConditionLimits() Limits {
	l := ActionLimits()
	l.Timeout = config.AutomationScriptsConditionTimeout.GetDuration()
	return l
}

// Enabled is the kill switch of scripts.
func Enabled() bool { return config.AutomationScriptsEnabled.GetBool() }

// MaxSource is the largest allowed script in bytes.
func MaxSource() int { return config.AutomationScriptsMaxSource.GetInt() }
