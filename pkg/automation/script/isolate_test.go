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
	"strings"
	"sync"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"

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

// TestRunaway_NativeCallsAreStopped runs the whole survey: every one of them ends the way it is expected to (a
// timeout, a memory error, the built-in's own exception) within the bound. A failure of the machinery or an
// unexpected internal error fails the test.
func TestRunaway_NativeCallsAreStopped(t *testing.T) {
	sem := make(chan struct{}, 4)
	for _, c := range runawayCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sem <- struct{}{}
			defer func() { <-sem }()
			o := runBounded(t, runawayBound, func() (*Result, error) { return Run(context.Background(), runawayInput(c.src)) })
			if c.want == wantFinish {
				require.NoError(t, o.err)
				return
			}
			var se *Error
			require.ErrorAs(t, o.err, &se, "%v", o.err)
			t.Logf("%s after %s: %s", se.Kind, o.elapsed.Round(time.Millisecond), firstLine(se.Message))
			switch c.want {
			case wantTimeout:
				assert.Equal(t, ErrTimeout, se.Kind, se.Message)
			case wantMemory:
				assert.Equal(t, ErrMemory, se.Kind, se.Message)
			case wantLimit:
				assert.Contains(t, []ErrorKind{ErrTimeout, ErrMemory}, se.Kind, se.Message)
			case wantThrown:
				assert.Equal(t, ErrThrown, se.Kind, se.Message)
			case wantThrownOrTimeout:
				assert.Contains(t, []ErrorKind{ErrThrown, ErrTimeout}, se.Kind, se.Message)
			case wantFinish:
				// Checked above.
			case wantGojaBug:
				assert.Equal(t, ErrInternal, se.Kind, se.Message)
				assert.Contains(t, se.Message, "nil pointer dereference")
			}
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

// runPID runs a script with the default limits and returns the outcome and the process that ran it.
func runPID(t *testing.T, src string) (*doneMessage, int) {
	t.Helper()
	in := &Input{Source: src, Host: &fakeHost{}, Limits: ActionLimits()}
	done, err := runInProcess(context.Background(), in, false)
	require.NoError(t, err)
	return done, done.PID
}

func TestIsolation_AProcessIsReusedOnlyAfterASuccessOrAnException(t *testing.T) {
	drainPool()
	_, first := runPID(t, `return 1`)
	_, again := runPID(t, `return 2`)
	assert.Equal(t, first, again, "a process is reused after a success")

	done, afterThrow := runPID(t, `throw new Error("x")`)
	require.NotNil(t, done.Error)
	assert.Equal(t, ErrThrown, done.Error.Kind)
	assert.Equal(t, first, afterThrow)
	_, next := runPID(t, `return 3`)
	assert.Equal(t, first, next, "a process is reused after an ordinary exception")

	short := func(in *Input) { in.Limits.Timeout = runawayTimeout }
	for name, c := range map[string]struct {
		src    string
		kind   ErrorKind
		mutate func(*Input)
	}{
		"the interpreter's timeout": {src: `for (;;) {}`, kind: ErrTimeout, mutate: short},
		"the stack limit":           {src: `function f() { return f() } f()`, kind: ErrStackOverflow, mutate: func(*Input) {}},
		"a kill at the deadline":    {src: `Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`, kind: ErrTimeout, mutate: short},
	} {
		t.Run(name, func(t *testing.T) {
			_, before := runPID(t, `return 1`)
			in := &Input{Source: c.src, Host: &fakeHost{}, Limits: ActionLimits()}
			c.mutate(in)
			done, err := runInProcess(context.Background(), in, false)
			require.NoError(t, err)
			require.NotNil(t, done.Error)
			assert.Equal(t, c.kind, done.Error.Kind)
			res, after := runPID(t, `globalThis.counter = (globalThis.counter || 0) + 1; return counter`)
			assert.NotEqual(t, before, after, "the process is not reused")
			assert.Equal(t, "1", res.Result.Value, "every script gets a fresh interpreter")
		})
	}
}

// TestIsolation_APooledProcessStartsClean: the memory of a script that finished cleanly is gone before the next
// script runs in the same process, or the process is retired. Before, the second of two scripts that allocate
// close to the limit failed with the memory error because the first one's heap stayed.
func TestIsolation_APooledProcessStartsClean(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("the hard memory limit is only enforced on Linux")
	}
	const nearLimit = `var a = new Float64Array(20 * 2**20); a[a.length - 1] = 1; return a.length`
	drainPool()
	t.Run("a script that grew the heap retires its process", func(t *testing.T) {
		done, first := runPID(t, nearLimit)
		require.Nil(t, done.Error, "%v", done.Error)
		assert.Less(t, done.Spare, ActionLimits().MaxMemory-poolMargin)
		done, second := runPID(t, nearLimit)
		require.Nil(t, done.Error, "%v", done.Error)
		assert.NotEqual(t, first, second, "the process that grew was retired")
	})
	t.Run("a pooled process gives the next script the whole limit", func(t *testing.T) {
		drainPool()
		done, first := runPID(t, `var a = new Float64Array(2**20); return a.length`)
		require.Nil(t, done.Error, "%v", done.Error)
		done, second := runPID(t, nearLimit)
		require.Nil(t, done.Error, "%v", done.Error)
		assert.Equal(t, first, second, "a small script leaves its process in the pool")
	})
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
		"nan": math.NaN(), "inf": math.Inf(1), "ninf": math.Inf(-1), "negzero": math.Copysign(0, -1),
		"specials": []any{math.NaN(), map[string]any{"inf": math.Inf(1)}},
	}
	h := &fakeHost{tasks: map[int64]map[string]any{1: task}}
	src := `var t = vikunja.tasks.get(1);
		var types = {};
		Object.keys(t).sort().forEach(function(k){ types[k] = typeof t[k] + (Number.isInteger(t[k]) ? ":int" : "") });
		var specials = function(v) {
			return [Number.isNaN(v.nan), v.inf === Infinity, v.ninf === -Infinity, Object.is(v.negzero, -0),
				Number.isNaN(v.specials[0]), v.specials[1].inf === Infinity].join()
		};
		return {task: task, fetched: t, types: types, same: JSON.stringify(t) === JSON.stringify(task),
			specials: specials(t), taskSpecials: specials(task)}`
	mk := func() *Input {
		return &Input{Source: src, Task: task, Event: map[string]any{"n": int64(1)}, Project: map[string]any{"id": int64(1)}, Host: h, Limits: ActionLimits()}
	}
	local, err := runLocal(context.Background(), mk())
	require.NoError(t, err)
	isolated, err := Run(context.Background(), mk())
	require.NoError(t, err)
	assert.Equal(t, local.Value, isolated.Value)
	assert.Contains(t, isolated.Value, `"same":true`)
	assert.Contains(t, isolated.Value, `"specials":"true,true,true,true,true,true","taskSpecials":"true,true,true,true,true,true"`)
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
	assert.Nil(t, check(t, `return 1`))
	synErr := check(t, "var a = 1;\nreturn a +")
	require.NotNil(t, synErr)
	assert.Equal(t, 2, synErr.Line)

	// Compiling some sources takes more than linear time; it happens in a script process with a timeout.
	old := config.AutomationScriptsTimeout.GetDuration()
	config.AutomationScriptsTimeout.Set(time.Second)
	t.Cleanup(func() { config.AutomationScriptsTimeout.Set(old) })
	for name, c := range map[string]struct {
		src  string
		want string
	}{
		"nesting": {src: strings.Repeat("[", 65000), want: "Maximum nesting depth exceeded"},
		"calls":   {src: strings.Repeat("f(", 32000) + strings.Repeat(")", 32000), want: "Maximum nesting depth exceeded"},
		"regexp":  {src: "/" + strings.Repeat("(a+)+", 13000) + "/"},
		// Takes far longer than the timeout to compile (32000 terms took 6 s).
		"expression": {src: strings.Repeat("1+", 200000) + "1", want: "compiling the script took longer than 1s; simplify it"},
	} {
		t.Run(name, func(t *testing.T) {
			var synErr *SyntaxError
			var err error
			o := runBounded(t, time.Second+killGrace+3*time.Second, func() (*Result, error) {
				synErr, err = Check(context.Background(), c.src)
				return nil, nil
			})
			require.NoError(t, err)
			if c.want == "" {
				assert.Nil(t, synErr)
				return
			}
			require.NotNil(t, synErr)
			assert.Contains(t, synErr.Message, c.want)
			assert.Less(t, o.elapsed, time.Second+killGrace+2*time.Second)
		})
	}
}

func TestCheck_EndsWithTheRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	started := time.Now()
	_, err := Check(ctx, strings.Repeat("1+", 200000)+"1")
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(started), 3*time.Second)
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
