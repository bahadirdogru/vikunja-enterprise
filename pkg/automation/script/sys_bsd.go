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
//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package script

import "os/exec"

func configureChild(*exec.Cmd) {}

// limitMemory has no hard limit here: macOS does not enforce a limit of the address space. The soft limit of the
// Go runtime (set by the caller) and the kill at the deadline remain.
func limitMemory(int64) error { return nil }
