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

// childExecutable is the running binary itself, not the file at its path: after an upgrade replaced the file,
// a child of the new build would talk to the old parent.
func childExecutable() (string, error) { return "/proc/self/exe", nil }

func configureChild(cmd *exec.Cmd) {
	// The script process must not outlive the server, not even while it spins in native code. The signal is sent
	// when the thread that started the child ends, so children are started from one locked thread (spawner).
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// preferOOMKill makes the script process the first choice of the kernel's OOM killer.
func preferOOMKill() {
	_ = os.WriteFile("/proc/self/oom_score_adj", []byte("1000"), 0)
}

// memoryLimit is the address-space limit of the script process.
type memoryLimit struct {
	set     bool
	ceiling uint64
}

// apply caps the address space at what the process uses now plus limit, so the script itself can map at most
// limit more bytes. A Go program reserves address space up front; a fixed cap on the whole address space would
// depend on the version of the runtime. The hard limit cannot be raised again by the process, so it is set once.
func (m *memoryLimit) apply(limit int64) error {
	current, err := addressSpace()
	if err != nil {
		return err
	}
	m.ceiling = current + uint64(limit)
	if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: m.ceiling, Max: m.ceiling}); err != nil {
		return err
	}
	m.set = true
	return nil
}

// spare is how much address space the next script can still map. The Go runtime never unmaps its heap, so a
// script that grew it leaves less for the next one even after its memory was returned.
func (m *memoryLimit) spare(int64) int64 {
	if !m.set {
		return -1
	}
	current, err := addressSpace()
	if err != nil || current > m.ceiling {
		return 0
	}
	return int64(m.ceiling - current) //nolint:gosec // at most the limit, an int64
}

func addressSpace() (uint64, error) {
	statm, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(statm))
	if len(fields) == 0 {
		return 0, errors.New("unexpected /proc/self/statm")
	}
	pages, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, err
	}
	return pages * uint64(os.Getpagesize()), nil
}

// killedBySignal reports whether the process was ended by SIGKILL.
func killedBySignal(state *os.ProcessState) bool {
	if state == nil {
		return false
	}
	ws, ok := state.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL
}
