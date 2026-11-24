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
//go:build !linux

package script

import goruntime "runtime"

// memoryLimit is only the soft limit of the Go runtime here: there is no address-space limit the process could
// rely on (macOS does not enforce one; reading the size of the address space needs procfs on the BSDs).
type memoryLimit struct {
	set bool
}

func (m *memoryLimit) apply(int64) error {
	m.set = true
	return nil
}

// spare is the soft limit minus what the runtime still holds of the operating system after the memory of the
// last script was returned.
func (m *memoryLimit) spare(limit int64) int64 {
	if !m.set {
		return -1
	}
	var ms goruntime.MemStats
	goruntime.ReadMemStats(&ms)
	return limit - int64(ms.Sys-ms.HeapReleased)
}
