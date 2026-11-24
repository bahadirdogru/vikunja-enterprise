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
//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package script

import (
	"os"
	"os/exec"
	"time"
)

// On Windows and the other systems the script process is killed at the deadline, but has no hard memory limit
// beyond the soft limit of the Go runtime.

func configureChild(*exec.Cmd) {}

func limitMemory(int64) error { return nil }

func limitCPU(time.Duration) {}

func cpuLimitExceeded(*os.ProcessState) bool { return false }
