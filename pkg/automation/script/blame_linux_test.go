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
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBlame_AScriptCannotLaunderItsFailureAsTheMachinerys: whatever ends a process after it got the script and
// is not positively the machinery's is the script's fault. Before, an exit the parent did not recognise was
// "unavailable": retried five times and never counted.
func TestBlame_AScriptCannotLaunderItsFailureAsTheMachinerys(t *testing.T) {
	hello := "echo " + shellQuote(helloLine(t, thisBuild())) + "\n"
	for name, after := range map[string]string{
		"an exit code it can reach":   "read line; echo 'script process: unreadable input: message too large' >&2; exit 3\n",
		"a silent exit":               "read line; exit 0\n",
		"closing its own output":      "read line; exec >&-; sleep 0.2; exit 0\n",
		"garbage instead of messages": "read line; echo 'not json'; exec sleep 30\n",
	} {
		t.Run(name, func(t *testing.T) {
			fakeExecutable(t, hello+after)
			_, err := run(t, nil, `return 1`)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrUnavailable)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, ErrInternal, se.Kind)
			assert.True(t, se.Blamed())
			waitForNoProcesses(t)
		})
	}
}

// killChildren sends SIGKILL to the script processes of this process.
func killChildren(t *testing.T) {
	entries, err := os.ReadDir("/proc")
	require.NoError(t, err)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		if len(fields) > 1 && fields[1] == strconv.Itoa(os.Getpid()) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// killingHost kills its script processes in the middle of a host call, as the kernel's OOM killer would.
type killingHost struct {
	*fakeHost
	t *testing.T
}

func (h *killingHost) TaskGet(context.Context, int64) (map[string]any, error) {
	killChildren(h.t)
	time.Sleep(200 * time.Millisecond)
	return map[string]any{"id": int64(1)}, nil
}

func (h *killingHost) Fetch(context.Context, *FetchRequest) (*FetchResponse, error) {
	killChildren(h.t)
	time.Sleep(200 * time.Millisecond)
	return &FetchResponse{Status: 200}, nil
}

// TestBlame_ASigkillTheServerDidNotSendIsNotTheScripts: it is not counted; it is retried only when no request
// left the server.
func TestBlame_ASigkillTheServerDidNotSendIsNotTheScripts(t *testing.T) {
	drainPool()
	h := &killingHost{fakeHost: &fakeHost{}, t: t}
	with := func(in *Input) { in.Host = h }

	_, err := run(t, nil, `vikunja.tasks.get(1); return 1`, with)
	require.ErrorIs(t, err, ErrUnavailable, "nothing left the server: retried")

	_, err = run(t, nil, `http.fetch("https://example.com"); return 1`, with)
	var se *Error
	require.ErrorAs(t, err, &se, "a retry would send the request again")
	assert.Equal(t, ErrUnattributed, se.Kind)
	assert.False(t, se.Blamed())
	waitForNoProcesses(t)
}
