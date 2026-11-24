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

package automation

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// killScriptProcesses sends SIGKILL to the script processes of this process.
func killScriptProcesses(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	require.NoError(t, err)
	killed := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// pid (comm) state ppid ...
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		if len(fields) < 2 || fields[1] != strconv.Itoa(os.Getpid()) {
			continue
		}
		environ, _ := os.ReadFile("/proc/" + e.Name() + "/environ")
		if !strings.Contains(string(environ), "VIKUNJA_AUTOMATION_SCRIPT_PROCESS=1") {
			continue
		}
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			killed++
		}
	}
	return killed
}

// TestScript_AChildKilledInTheMiddleOfAHostCall: the script changed the task, then its process dies while the
// server answers the next host call. The step's transaction is rolled back and the run is recorded once.
func TestScript_AChildKilledInTheMiddleOfAHostCall(t *testing.T) {
	reset(t)
	allowLoopback(t)
	srv := countingServer(t, 200, "late")
	var killed atomic.Int64
	srv.handler = func(w http.ResponseWriter, _ *http.Request) {
		killed.Add(int64(killScriptProcesses(t)))
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}
	task, run, steps := runScript(t, `vikunja.tasks.update(task.id, {title: "changed by the script"});
		http.fetch("`+srv.url+`/slow");
		return 1`)
	assert.Positive(t, killed.Load())
	assert.EqualValues(t, 1, srv.count())
	assert.Equal(t, models.AutomationRunFailed, run.Status)
	assert.Contains(t, run.Error, "killed by the operating system")
	require.Len(t, steps, 1)
	assert.Equal(t, models.AutomationStepFailed, steps[0].Status)
	assert.Equal(t, "Original title", task.Title, "the change of the killed script was rolled back")
	assert.Len(t, allRuns(t, run.RuleID), 1)
}
