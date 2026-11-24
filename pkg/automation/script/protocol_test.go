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
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drainPool ends every idle script process, so the next run starts a new one.
func drainPool() { pool.drain() }

// fakeExecutable makes the script processes of the test the given shell script.
func fakeExecutable(t *testing.T, script string) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("the fake script process is a shell script")
	}
	path := filepath.Join(t.TempDir(), "fake-child")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700)) //nolint:gosec // it is executed
	useExecutable(t, path)
}

func useExecutable(t *testing.T, path string) {
	t.Helper()
	t.Cleanup(SetTestExecutable(path))
}

func helloLine(t *testing.T, build string) string {
	t.Helper()
	b, err := json.Marshal(&message{Hello: &helloMessage{Protocol: protocolVersion, Build: build, PID: 1}})
	require.NoError(t, err)
	return string(b)
}

// shellQuote quotes s for sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func waitForNoProcesses(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return liveProcesses() == 0 }, 10*time.Second, 10*time.Millisecond)
}

func TestHello_AProcessThatDoesNotIdentifyItselfGetsNoScript(t *testing.T) {
	old := helloTimeout
	helloTimeout = 300 * time.Millisecond
	t.Cleanup(func() { helloTimeout = old })

	for name, script := range map[string]string{
		"a binary without the script process": "echo 'Vikunja is starting...'\nsleep 30\n",
		"a binary that says nothing":          "exec sleep 30\n",
		"a binary of another build":           "echo " + shellQuote(helloLine(t, "another build")) + "\nexec sleep 30\n",
		"a binary that cannot be executed":    "",
	} {
		t.Run(name, func(t *testing.T) {
			if script == "" {
				useExecutable(t, filepath.Join(t.TempDir(), "missing"))
			} else {
				fakeExecutable(t, script)
			}
			started := time.Now()
			_, err := run(t, nil, `return 1`)
			require.ErrorIs(t, err, ErrUnavailable)
			var unavailable *UnavailableError
			require.ErrorAs(t, err, &unavailable)
			assert.Equal(t, script == "", unavailable.Start, "only a binary that cannot be executed is a failure to start")
			var se *Error
			assert.NotErrorAs(t, err, &se, "not the script's fault")
			assert.Less(t, time.Since(started), 3*time.Second)
			waitForNoProcesses(t)

			// The compile check is refused, never done in the server instead.
			synErr, err := Check(context.Background(), 0, "var a = 1;\nreturn a +")
			require.ErrorIs(t, err, ErrUnavailable)
			assert.Nil(t, synErr)
		})
	}
}

// fakeConversation runs parent.pump against a fake child.
func fakeConversation(t *testing.T, in *Input, fake func(r *bufio.Reader, w io.Writer)) (*parent, *doneMessage, error) {
	t.Helper()
	toChild, fromParent := io.Pipe()
	fromChild, toParent := io.Pipe()
	go func() {
		fake(bufio.NewReader(toChild), toParent)
		_ = toParent.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := &parent{ctx: ctx, in: in, w: fromParent, seq: 1}
	done, _, err := p.pump(bufio.NewReaderSize(fromChild, 64<<10), time.Second)
	_ = fromChild.Close()
	_ = toChild.Close()
	return p, done, err
}

func fakeInput() *Input {
	return &Input{Source: "x", Host: &fakeHost{tasks: map[int64]map[string]any{1: {"id": int64(1)}}}, Limits: ActionLimits()}
}

func send(w io.Writer, m *message) {
	_ = writeMessage(w, m)
}

// TestParent_EnforcesTheLimitsOfTheRun: the child counts and checks before it sends a call; a child that does
// not is killed and the run fails as the script's fault.
func TestParent_EnforcesTheLimitsOfTheRun(t *testing.T) {
	in := fakeInput()
	in.Limits.MaxAPICalls = 3
	in.Limits.MaxHTTPRequests = 1
	in.Limits.MaxValueBytes = 100
	in.Limits.MaxExportNodes = 50

	for name, c := range map[string]struct {
		calls []*callMessage
		want  string
	}{
		"too many vikunja calls": {
			calls: []*callMessage{{Method: "TaskGet", ID: 1}, {Method: "TaskGet", ID: 1}, {Method: "TaskGet", ID: 1}, {Method: "TaskGet", ID: 1}},
			want:  "more than 3 vikunja.* calls",
		},
		"too many requests": {
			calls: []*callMessage{{Method: "Fetch", Fetch: &FetchRequest{URL: "https://example.com"}}, {Method: "Fetch", Fetch: &FetchRequest{URL: "https://example.com"}}},
			want:  "more than 1 http.fetch requests",
		},
		"a text beyond the limit": {
			calls: []*callMessage{{Method: "TaskComment", ID: 1, Text: strings.Repeat("x", 101)}},
			want:  "beyond the limits",
		},
		"a request body beyond the limit": {
			calls: []*callMessage{{Method: "Fetch", Fetch: &FetchRequest{URL: "https://example.com", Body: strings.Repeat("x", 101)}}},
			want:  "beyond the limits",
		},
		"a value with too many nodes": {
			calls: []*callMessage{{Method: "FieldSet", ID: 1, ID2: 1, Value: make([]any, 60)}},
			want:  "beyond the limits",
		},
		"a value nested too deeply": {
			calls: []*callMessage{{Method: "TaskUpdate", ID: 1, Data: deep(40)}},
			want:  "beyond the limits",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := &fakeHost{tasks: map[int64]map[string]any{1: {"id": int64(1)}}}
			in.Host = h
			p, done, _ := fakeConversation(t, in, func(r *bufio.Reader, w io.Writer) {
				if _, err := readMessage(r, maxReplyBytes, true); err != nil {
					return
				}
				for _, call := range c.calls {
					call.Seq = 1
					send(w, &message{Call: call})
					if _, err := readMessage(r, maxReplyBytes, true); err != nil {
						return
					}
				}
				send(w, &message{Done: &doneMessage{Seq: 1, Result: &Result{}}})
			})
			assert.Nil(t, done)
			require.NotNil(t, p.violation)
			assert.Equal(t, ErrThrown, p.violation.Kind)
			assert.Contains(t, p.violation.Message, c.want)
			assert.Len(t, h.calls, len(c.calls)-1, "the call beyond the limit never reached the host")
		})
	}
}

func deep(n int) map[string]any {
	m := map[string]any{"leaf": "x"}
	for i := 0; i < n; i++ {
		m = map[string]any{"next": m}
	}
	return m
}

// TestParent_RefusesAnOversizedLineBeforeBufferingIt drives the parent with a child that sends one endless line.
func TestParent_RefusesAnOversizedLineBeforeBufferingIt(t *testing.T) {
	toChild, fromParent := io.Pipe()
	fromChild, toParent := io.Pipe()
	go func() { _, _ = io.Copy(io.Discard, toChild) }()
	go func() {
		chunk := []byte(strings.Repeat("x", 1<<20))
		for i := 0; i < 256; i++ {
			if _, err := toParent.Write(chunk); err != nil {
				return
			}
		}
	}()
	counted := &countingReader{r: fromChild}
	p := &parent{ctx: context.Background(), in: fakeInput(), w: fromParent, seq: 1}
	done, _, err := p.pump(bufio.NewReaderSize(counted, 64<<10), time.Second)
	_ = fromChild.Close()
	_ = toChild.Close()
	assert.Nil(t, done)
	require.ErrorIs(t, err, errMessageTooLarge)
	assert.LessOrEqual(t, counted.n.Load(), int64(maxMessageBytes+(64<<10)), "the parent read no more than the limit of the line")
}

type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func TestParent_RejectsMessagesOfAnotherRun(t *testing.T) {
	for name, msg := range map[string]*message{
		"a done of another run":  {Done: &doneMessage{Seq: 2, Result: &Result{}}},
		"a call of another run":  {Call: &callMessage{Seq: 0, Method: "TaskGet", ID: 1}},
		"a message that is none": {Reply: &replyMessage{}},
	} {
		t.Run(name, func(t *testing.T) {
			p, done, _ := fakeConversation(t, fakeInput(), func(r *bufio.Reader, w io.Writer) {
				if _, err := readMessage(r, maxReplyBytes, true); err != nil {
					return
				}
				send(w, msg)
			})
			assert.Nil(t, done)
			assert.Error(t, p.broken)
		})
	}
}

// TestProtocol_AProcessWithUnreadOutputIsNotReused: a fake child that answers every start with two dones.
func TestProtocol_AProcessWithUnreadOutputIsNotReused(t *testing.T) {
	doneLine := `{"done":{"seq":1,"result":{"Value":"1","Truthy":true,"Returned":true},"spare":-1,"pid":1}}`
	fakeExecutable(t, "echo "+shellQuote(helloLine(t, thisBuild()))+"\n"+
		"while read line; do printf '%s\\n%s\\n' "+shellQuote(doneLine)+" "+shellQuote(doneLine)+"; done\n")

	res, err := run(t, nil, `return 1`)
	require.NoError(t, err)
	assert.Equal(t, "1", res.Value)
	pool.mu.Lock()
	idle := len(pool.idle)
	pool.mu.Unlock()
	assert.Zero(t, idle, "the process with a second done waiting is not pooled")
	waitForNoProcesses(t)

	// A second done that arrives late is seen by the next run, which refuses the process.
	fakeExecutable(t, "echo "+shellQuote(helloLine(t, thisBuild()))+"\n"+
		"read line; echo "+shellQuote(doneLine)+"; sleep 0.2; echo "+shellQuote(doneLine)+"; read line; exec sleep 30\n")
	_, err = run(t, nil, `return 1`)
	require.NoError(t, err)
	time.Sleep(400 * time.Millisecond)
	// The leftover is the previous script's: this one runs again in a fresh process (the same fake), whose late
	// done is ignored because the run already ended.
	_, err = run(t, nil, `return 1`)
	require.NoError(t, err)
	drainPool()
	waitForNoProcesses(t)
}

// TestLimits_AProcessCannotHoldMoreThanItsShare: never more live script processes than one per worker plus the
// allowance, and the allowance of the dry runs cannot take a worker's process.
func TestLimits_AProcessCannotHoldMoreThanItsShare(t *testing.T) {
	procs.init()
	capacity := cap(procs.live)
	var peak atomic.Int64
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if n := liveProcesses(); n > peak.Load() {
				peak.Store(n)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	t.Run("more concurrent runs than processes wait for one", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, capacity+1)
		for i := 0; i < capacity+1; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := run(t, nil, `var t = Date.now(); while (Date.now() - t < 400) {} return 1`)
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
	})

	t.Run("dry runs beyond their allowance are refused as busy", func(t *testing.T) {
		blockers := make(chan error, extraProcesses)
		for i := 0; i < extraProcesses; i++ {
			go func() {
				_, err := run(t, nil, `Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`, func(in *Input) {
					in.Interactive = true
					in.ProjectID = int64(100 + i)
					in.Limits.Timeout = 1500 * time.Millisecond
				})
				blockers <- err
			}()
		}
		time.Sleep(300 * time.Millisecond)
		started := time.Now()
		_, err := run(t, nil, `return 1`, func(in *Input) {
			in.Interactive = true
			in.ProjectID = 200
			in.Limits.Timeout = 300 * time.Millisecond
		})
		var unavailable *UnavailableError
		require.ErrorAs(t, err, &unavailable)
		assert.True(t, unavailable.Busy)
		assert.Less(t, time.Since(started), 2*time.Second)

		// A worker's run is not held up by the dry runs.
		started = time.Now()
		res, err := run(t, nil, `return 1`)
		require.NoError(t, err)
		assert.Equal(t, "1", res.Value)
		assert.Less(t, time.Since(started), time.Second)

		for i := 0; i < extraProcesses; i++ {
			var se *Error
			require.ErrorAs(t, <-blockers, &se)
			assert.Equal(t, ErrTimeout, se.Kind)
		}
	})

	close(stop)
	<-sampled
	assert.LessOrEqual(t, peak.Load(), int64(capacity), "never more live processes than the cap")
	assert.Positive(t, peak.Load())
}

func TestChild_CutsWhatItSendsToTheLimits(t *testing.T) {
	res := mustRun(t, `return "x".repeat(1 << 20)`)
	assert.LessOrEqual(t, len(res.Value), maxResultBytes+len(cutMarker))
	assert.True(t, strings.HasSuffix(res.Value, cutMarker))

	_, err := run(t, nil, `throw new Error("y".repeat(1 << 20))`)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.LessOrEqual(t, len(se.Message), maxResultBytes+len(cutMarker))

	// Five strings of 1 MiB each are within the limit of a value, but the call would be larger than a message.
	res = mustRun(t, `var s = "z".repeat(1 << 20);
		try { vikunja.tasks.update(1, {a: s, b: s, c: s, d: s, e: s}); return "sent" } catch (e) { return e.message }`)
	assert.Contains(t, res.Value, "larger than 4 MiB")
}

// startBare starts a script process and sends it a start message, without the parent's kill at the deadline.
func startBare(t *testing.T, src string, budget time.Duration) *child {
	t.Helper()
	procs.init()
	procs.live <- struct{}{}
	c, err := startChild(0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.kill() })
	start := &startMessage{Seq: 1, Source: src, Limits: ActionLimits(), Budget: budget}
	require.NoError(t, writeMessage(c.stdin, &message{Start: start}))
	return c
}

func exited(c *child) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		_ = c.wait()
		close(ch)
	}()
	return ch
}

// TestChild_EndsItselfWhenNobodyKillsIt: the script process spins in one native call (with GOMAXPROCS=1, as the
// parent starts it) and is neither killed nor read from.
func TestChild_EndsItselfWhenNobodyKillsIt(t *testing.T) {
	const native = `Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`
	t.Run("after its timeout", func(t *testing.T) {
		const budget = 200 * time.Millisecond
		started := time.Now()
		c := startBare(t, native, budget)
		select {
		case <-exited(c):
		case <-time.After(budget + watchdogGrace + 5*time.Second):
			t.Fatal("the script process is still running")
		}
		assert.Equal(t, exitOverdue, c.cmd.ProcessState.ExitCode())
		assert.GreaterOrEqual(t, time.Since(started), budget+watchdogGrace)
	})
	t.Run("when the parent's end of the pipe is closed", func(t *testing.T) {
		c := startBare(t, native, time.Hour)
		time.Sleep(200 * time.Millisecond)
		_ = c.stdin.Close()
		select {
		case <-exited(c):
		case <-time.After(5 * time.Second):
			t.Fatal("the script process is still running")
		}
		assert.False(t, killedBySignal(c.cmd.ProcessState))
	})
}

func TestUnavailable_IsNotAScriptError(t *testing.T) {
	var se *Error
	assert.NotErrorAs(t, errBusy, &se)
	assert.ErrorIs(t, errBusy, ErrUnavailable)
}
