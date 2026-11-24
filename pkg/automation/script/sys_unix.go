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
//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package script

import (
	"os"
	"syscall"
	"time"
)

// limitCPU is a second line behind the kill of the parent: the operating system ends the script process when the
// script has used its timeout of CPU time and some more, even if the parent is gone. The process serves one
// script after the other, so the soft limit is moved on for every script; the hard limit is not touched.
func limitCPU(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return
	}
	used := time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_CPU, &rl); err != nil {
		return
	}
	secs := uint64((used+timeout)/time.Second) + 2
	setLimit(&rl.Cur, secs)
	_ = syscall.Setrlimit(syscall.RLIMIT_CPU, &rl)
}

// setLimit sets a field of syscall.Rlimit, which is signed on some systems and unsigned on others.
func setLimit[T ~int64 | ~uint64](field *T, v uint64) {
	*field = T(v)
}

// cpuLimitExceeded reports whether the operating system ended the process for its CPU time.
func cpuLimitExceeded(state *os.ProcessState) bool {
	if state == nil {
		return false
	}
	ws, ok := state.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGXCPU
}
