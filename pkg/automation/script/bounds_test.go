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
	"math"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allocated is how much the server allocated while fn ran.
func allocated(fn func()) uint64 {
	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	fn()
	goruntime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// bigHost answers every read with a large task.
type bigHost struct {
	*fakeHost
	task map[string]any
	list []map[string]any
}

func (h *bigHost) TaskGet(context.Context, int64) (map[string]any, error) { return h.task, nil }
func (h *bigHost) TaskFind(context.Context, string, int64, int) ([]map[string]any, error) {
	return h.list, nil
}

// TestInput_ATaskTooLargeForAScriptIsRefusedBeforeItIsEncoded: before, the server built the whole JSON (HTML
// escaped, six bytes per <) and the child died on it as "unavailable", retried and never blamed.
func TestInput_ATaskTooLargeForAScriptIsRefusedBeforeItIsEncoded(t *testing.T) {
	drainPool()
	big := map[string]any{"id": int64(1), "description": strings.Repeat("<", 20<<20)}
	var err error
	var pid int
	n := allocated(func() {
		in := &Input{Source: `return 1`, Task: big, Host: &fakeHost{}, Limits: ActionLimits()}
		var done *doneMessage
		done, err = runInProcess(context.Background(), in, false)
		if err == nil && done.Error != nil {
			err = done.Error
		}
	})
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrInputTooLarge, se.Kind)
	assert.False(t, se.Blamed(), "anyone who can write the task can make it that large")
	assert.Contains(t, se.Message, "too large or too deeply nested to be passed to a script")
	assert.Less(t, n, uint64(16<<20), "the server did not encode the task")

	// A large task within the limit gets through, and < is one byte, not six.
	res, err := Run(context.Background(), &Input{
		Source: `return task.description.length`,
		Task:   map[string]any{"id": int64(1), "description": strings.Repeat("<", 6<<20)},
		Host:   &fakeHost{},
		Limits: ActionLimits(),
	})
	require.NoError(t, err)
	assert.Equal(t, "6291456", res.Value)
	_, pid = runPID(t, `return 1`)
	assert.NotZero(t, pid)
}

func TestInput_ParallelRunsOnALargeTaskDoNotGrowTheServer(t *testing.T) {
	big := map[string]any{"id": int64(1), "description": strings.Repeat("<", 20<<20)}
	n := allocated(func() {
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = Run(context.Background(), &Input{Source: `return 1`, Task: big, Host: &fakeHost{}, Limits: ActionLimits()})
			}()
		}
		wg.Wait()
	})
	assert.Less(t, n, uint64(64<<20), "before: about 3 GiB")
}

// TestReply_AnAnswerTooLargeForTheScriptIsAnExceptionInTheScript: before, the child died on it ("unavailable").
func TestReply_AnAnswerTooLargeForTheScriptIsAnExceptionInTheScript(t *testing.T) {
	desc := strings.Repeat("<", 1<<20)
	h := &bigHost{fakeHost: &fakeHost{}, task: map[string]any{"id": int64(1), "description": strings.Repeat("<", 11<<20)}}
	for i := 0; i < 100; i++ {
		h.list = append(h.list, map[string]any{"id": int64(i + 1), "description": desc})
	}
	with := func(in *Input) { in.Host = h }

	res, err := run(t, nil, `try { vikunja.tasks.get(1); return "no" } catch (e) { return e.message }`, with)
	require.NoError(t, err)
	assert.Contains(t, res.Value, "larger than 8 MiB")

	n := allocated(func() {
		res, err = run(t, nil, `try { vikunja.tasks.find("done = false"); return "no" } catch (e) { return e.message }`, with)
	})
	require.NoError(t, err)
	assert.Contains(t, res.Value, "larger than 8 MiB")
	assert.Less(t, n, uint64(32<<20), "before: about 4 GiB for this call")

	// Uncaught (also rethrown) it fails the run, but it is the data's doing: anyone who can write a task the owner
	// can read can inflate it.
	for _, src := range []string{`vikunja.tasks.get(1)`, `try { vikunja.tasks.get(1) } catch (e) { throw e }`} {
		_, err = run(t, nil, src, with)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrInputTooLarge, se.Kind, src)
		assert.False(t, se.Blamed(), src)
	}

	// Caught, and the script then fails for a reason of its own: that is blamed.
	_, err = run(t, nil, `try { vikunja.tasks.get(1) } catch (e) {} throw new Error("mine")`, with)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrThrown, se.Kind)
	assert.True(t, se.Blamed())
}

// TestInput_ATooDeeplyNestedEventIsRefusedWithoutBlame: an incoming webhook can send a payload nested deeper than
// JSON is encoded; before, the encoder failed, the run was "unavailable", retried and the process killed.
func TestInput_ATooDeeplyNestedEventIsRefusedWithoutBlame(t *testing.T) {
	drainPool()
	_, pid := runPID(t, `return 1`)
	deepEvent := map[string]any{}
	node := deepEvent
	for i := 0; i < 9998; i++ {
		next := map[string]any{}
		node["a"] = next
		node = next
	}
	done, err := runInProcess(context.Background(), &Input{Source: `return 1`, Event: deepEvent, Host: &fakeHost{}, Limits: ActionLimits()}, false)
	require.NoError(t, err)
	require.NotNil(t, done.Error)
	assert.Equal(t, ErrInputTooLarge, done.Error.Kind)
	assert.Contains(t, done.Error.Message, "too deeply nested")
	_, again := runPID(t, `return 1`)
	assert.Equal(t, pid, again, "nothing was sent: the process stays in the pool")
}

func TestWire_NoHTMLEscapesAndAnHonestSizeEstimate(t *testing.T) {
	for _, s := range []string{"<>&", "a\"b\\c", "\x00\x1f", "\u2028\u2029", "é\u6f22", "\xff\xfe", strings.Repeat("<\n\"", 1000)} {
		var buf strings.Builder
		require.NoError(t, writeMessage(&buf, &message{Reply: &replyMessage{Text: s}}))
		assert.NotContains(t, buf.String(), `\u003c`)
		encoded := len(buf.String()) - len(`{"reply":{"text":}}`+"\n")
		assert.GreaterOrEqual(t, jsonLen(s), encoded, "%q", s)
	}

	// Numbers JSON cannot hold repeat their path in the specials: 1000 NaNs under 50 keys of 1000 bytes.
	v := map[string]any{}
	leaf := v
	for i := 0; i < 50; i++ {
		next := map[string]any{}
		leaf[strings.Repeat(string(rune('a'+i%26)), 1000)] = next
		leaf = next
	}
	for i := 0; i < 1000; i++ {
		leaf[strconv.Itoa(i)] = math.NaN()
	}
	tree, specials, size, err := wireValueLimited(v, 0)
	require.NoError(t, err)
	var buf strings.Builder
	require.NoError(t, writeMessage(&buf, &message{Reply: &replyMessage{Value: tree, Specials: specials}}))
	assert.GreaterOrEqual(t, size, buf.Len()-64, "the estimate is an upper bound")
}

// TestProjects_OneInteractiveRunPerProject: one project cannot hold both extra processes.
func TestProjects_OneInteractiveRunPerProject(t *testing.T) {
	spin := func(project int64, timeout time.Duration) error {
		_, err := run(t, nil, `while (true) {}`, func(in *Input) {
			in.Interactive = true
			in.ProjectID = project
			in.Limits.Timeout = timeout
		})
		return err
	}
	first := make(chan error, 1)
	go func() { first <- spin(7, 1500*time.Millisecond) }()
	time.Sleep(300 * time.Millisecond)

	started := time.Now()
	err := spin(7, 300*time.Millisecond)
	var unavailable *UnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.True(t, unavailable.Busy)
	assert.Contains(t, unavailable.Msg, "this project")
	assert.Less(t, time.Since(started), 2*time.Second)

	// Another project still gets the second extra process.
	res, err := run(t, nil, `return 1`, func(in *Input) {
		in.Interactive = true
		in.ProjectID = 8
	})
	require.NoError(t, err)
	assert.Equal(t, "1", res.Value)

	var se *Error
	require.ErrorAs(t, <-first, &se)
	assert.Equal(t, ErrTimeout, se.Kind)
	procs.mu.Lock()
	assert.Empty(t, procs.projects, "no slot is left behind")
	procs.mu.Unlock()
}
