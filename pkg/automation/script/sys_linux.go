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
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func configureChild(cmd *exec.Cmd) {
	// The script process must not outlive the server, not even while it spins in native code.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// limitMemory caps the address space of the script process at what it uses now plus limit, so the script itself
// can map at most limit more bytes. A Go program reserves address space up front; a fixed cap on the whole
// address space would depend on the version of the runtime.
func limitMemory(limit int64) error {
	statm, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(statm))
	if len(fields) == 0 {
		return errors.New("unexpected /proc/self/statm")
	}
	pages, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return err
	}
	current := pages * uint64(os.Getpagesize())
	ceiling := current + uint64(limit)
	return syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: ceiling, Max: ceiling})
}
