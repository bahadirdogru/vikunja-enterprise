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
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runawayTimeout is the script timeout of the tests of runaway scripts, and runawayBound how long such a run may
// take at most: the timeout, the grace before the kill and plenty of room for a busy machine.
const (
	runawayTimeout = 200 * time.Millisecond
	runawayBound   = runawayTimeout + killGrace + 3*time.Second
)

type outcome struct {
	res     *Result
	err     error
	elapsed time.Duration
}

// runBounded runs a script in a goroutine and fails the test when it is still running after bound, so a script
// that outlives its timeout fails the test instead of hanging the test binary.
func runBounded(t *testing.T, bound time.Duration, run func() (*Result, error)) outcome {
	t.Helper()
	ch := make(chan outcome, 1)
	started := time.Now()
	go func() {
		res, err := run()
		ch <- outcome{res: res, err: err, elapsed: time.Since(started)}
	}()
	select {
	case o := <-ch:
		return o
	case <-time.After(bound):
		t.Fatalf("the script was still running after %s (timeout %s)", bound, runawayTimeout)
		return outcome{}
	}
}

func runawayInput(src string) *Input {
	in := &Input{Source: src, Host: &fakeHost{}, Limits: ActionLimits()}
	in.Limits.Timeout = runawayTimeout
	return in
}

// TestRunaway_TheKnownReproducer is the report: a native call that loops over a huge array-like never sees the
// interrupt of the timeout.
func TestRunaway_TheKnownReproducer(t *testing.T) {
	const src = `Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`
	for name, condition := range map[string]bool{"action": false, "condition": true} {
		t.Run(name, func(t *testing.T) {
			in := runawayInput(src)
			in.Condition = condition
			o := runBounded(t, runawayBound, func() (*Result, error) { return Run(context.Background(), in) })
			var se *Error
			require.ErrorAs(t, o.err, &se)
			assert.Equal(t, ErrTimeout, se.Kind)
			assert.Contains(t, se.Message, "ran longer than 200ms")
			assert.Less(t, o.elapsed, runawayTimeout+killGrace+2*time.Second)
		})
	}
}

// TestRunaway_NativeCallsAreStopped runs the whole survey: every one of them ends, as a timeout, as a memory error
// or as an exception the built-in throws itself, within the bound.
func TestRunaway_NativeCallsAreStopped(t *testing.T) {
	sem := make(chan struct{}, 4)
	for _, c := range runawayCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sem <- struct{}{}
			defer func() { <-sem }()
			o := runBounded(t, runawayBound, func() (*Result, error) { return Run(context.Background(), runawayInput(c.src)) })
			if o.err == nil {
				// Finishing in time is fine as well.
				return
			}
			var se *Error
			require.ErrorAs(t, o.err, &se, "%v", o.err)
			t.Logf("%s after %s: %s", se.Kind, o.elapsed.Round(time.Millisecond), firstLine(se.Message))
		})
	}
}

func TestRunaway_MemoryLimit(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("the hard memory limit is only enforced on Linux")
	}
	for name, src := range map[string]string{
		"one huge allocation":  `"x".repeat(2**30)`,
		"growing step by step": `var a = []; for (;;) { a.push("x".repeat(1 << 20) + a.length) }`,
		"typed array":          `new Float64Array(2**27)`,
	} {
		t.Run(name, func(t *testing.T) {
			in := runawayInput(src)
			in.Limits.Timeout = 20 * time.Second
			in.Limits.MaxMemory = 64 << 20
			o := runBounded(t, in.Limits.Timeout+killGrace+5*time.Second, func() (*Result, error) { return Run(context.Background(), in) })
			var se *Error
			require.ErrorAs(t, o.err, &se)
			assert.Equal(t, ErrMemory, se.Kind, se.Message)
			assert.Equal(t, "the script used more than 64 MiB of memory and was stopped", se.Message)
		})
	}
	t.Run("a normal script fits", func(t *testing.T) {
		in := runawayInput(`var a = []; for (var i = 0; i < 10000; i++) { a.push({i: i, s: "x" + i}) } return a.length`)
		in.Limits.Timeout = 5 * time.Second
		in.Limits.MaxMemory = 64 << 20
		res, err := Run(context.Background(), in)
		require.NoError(t, err)
		assert.Equal(t, "10000", res.Value)
	})
}

func TestIsolation_AProcessIsReusedOnlyAfterASuccess(t *testing.T) {
	_, err := Run(context.Background(), runawayInput(`Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`))
	require.Error(t, err)
	for i := 0; i < 3; i++ {
		res := mustRun(t, `globalThis.counter = (globalThis.counter || 0) + 1; return counter`)
		assert.Equal(t, "1", res.Value, "every script gets a fresh interpreter, also in a reused process")
	}
}

func TestIsolation_ACancelledParentStopsARunawayScript(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	in := runawayInput(`Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`)
	in.Limits.Timeout = time.Minute
	o := runBounded(t, 5*time.Second, func() (*Result, error) { return Run(ctx, in) })
	require.ErrorIs(t, o.err, context.Canceled)
}

func TestIsolation_ConcurrentRuns(t *testing.T) {
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := &fakeHost{tasks: map[int64]map[string]any{1: {"id": int64(1), "title": "One"}}}
			res, err := run(t, h, `return vikunja.tasks.get(1).title + vikunja.tasks.get(1).title`)
			if err == nil && res.Value != `"OneOne"` {
				err = assert.AnError
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

// panickingHost panics in a host call.
type panickingHost struct{ *fakeHost }

func (h *panickingHost) TaskGet(context.Context, int64) (map[string]any, error) {
	panic("the host broke")
}

func TestIsolation_APanicOfTheHostIsAnInternalError(t *testing.T) {
	_, err := run(t, nil, `vikunja.tasks.get(1)`, func(in *Input) { in.Host = &panickingHost{fakeHost: &fakeHost{}} })
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrInternal, se.Kind)
	assert.Contains(t, se.Message, "the host broke")
	mustRun(t, `return 1`)
}

// TestIsolation_ValuesCrossTheProcessBoundaryUnchanged compares what a script sees in the script process with
// what it sees in-process.
func TestIsolation_ValuesCrossTheProcessBoundaryUnchanged(t *testing.T) {
	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.FixedZone("X", 3600))
	type custom struct{ A int }
	task := map[string]any{
		"id": int64(9007199254740993), "int": 3, "float": 1.5, "whole_float": 2.0, "neg": int64(-4),
		"when": when, "zero_time": time.Time{}, "nil": nil, "flag": true, "text": "ünïcode   \"q\"",
		"ids": []int64{1, 2}, "names": []string{"a"}, "list": []any{int64(1), "b", nil, map[string]any{"c": 2.5}},
		"maps": []map[string]any{{"x": int64(1)}}, "empty_map": map[string]any{}, "nil_map": map[string]any(nil),
		"nil_slice": []map[string]any(nil), "other": custom{A: 1}, "uint": uint8(7),
	}
	h := &fakeHost{tasks: map[int64]map[string]any{1: task}}
	src := `var t = vikunja.tasks.get(1);
		var types = {};
		Object.keys(t).sort().forEach(function(k){ types[k] = typeof t[k] + (Number.isInteger(t[k]) ? ":int" : "") });
		return {task: task, fetched: t, types: types, same: JSON.stringify(t) === JSON.stringify(task)}`
	mk := func() *Input {
		return &Input{Source: src, Task: task, Event: map[string]any{"n": int64(1)}, Project: map[string]any{"id": int64(1)}, Host: h, Limits: ActionLimits()}
	}
	local, err := runLocal(context.Background(), mk())
	require.NoError(t, err)
	isolated, err := Run(context.Background(), mk())
	require.NoError(t, err)
	assert.Equal(t, local.Value, isolated.Value)
	assert.Contains(t, isolated.Value, `"same":true`)
}

func TestIsolation_ValuesFromTheScriptReachTheHostUnchanged(t *testing.T) {
	h := &fakeHost{}
	src := `vikunja.tasks.update(1, {title: "t", n: 3, f: 1.5, b: false, z: null, list: [1, "a", {x: 2}], nested: {deep: {er: "x"}}});
		vikunja.fields.set(1, 2, 0); vikunja.fields.set(1, 2, ""); vikunja.fields.set(1, 2, false); vikunja.fields.set(1, 2, null);
		vikunja.tasks.comment(1, ""); vikunja.status.set(1, 2)`
	_, err := runLocal(context.Background(), &Input{Source: src, Host: h, Limits: ActionLimits()})
	require.NoError(t, err)
	local := strings.Join(h.calls, "\n")
	h.calls = nil
	_, err = Run(context.Background(), &Input{Source: src, Host: h, Limits: ActionLimits()})
	require.NoError(t, err)
	assert.Equal(t, local, strings.Join(h.calls, "\n"))
}

func TestIsolation_ANumberThatIsNotFiniteCannotReachTheHost(t *testing.T) {
	res, err := run(t, nil, `try { vikunja.fields.set(1, 2, NaN) } catch (e) { return e.message }`)
	require.NoError(t, err)
	assert.Contains(t, res.Value, "cannot be passed to the host")
}

func TestCheck_CompilesInTheScriptProcess(t *testing.T) {
	assert.Nil(t, Check(`return 1`))
	synErr := Check("var a = 1;\nreturn a +")
	require.NotNil(t, synErr)
	assert.Equal(t, 2, synErr.Line)

	// Compiling some sources takes more than linear time; it happens in a script process with a timeout.
	for name, src := range map[string]string{
		"nesting":    strings.Repeat("[", 65000),
		"expression": strings.Repeat("1+", 32000) + "1",
		"calls":      strings.Repeat("f(", 32000) + strings.Repeat(")", 32000),
		"regexp":     "/" + strings.Repeat("(a+)+", 13000) + "/",
	} {
		t.Run(name, func(t *testing.T) {
			o := runBounded(t, ActionLimits().Timeout+killGrace+3*time.Second, func() (*Result, error) { _ = Check(src); return nil, nil })
			assert.Less(t, o.elapsed, ActionLimits().Timeout+killGrace+2*time.Second)
		})
	}
}
