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
	"errors"
	"fmt"
	"io"
	"os"
	goruntime "runtime"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// childEnv marks the script process. The parent starts its own executable with it set (see isolate.go); every
// binary that imports this package then turns into the script process here, before main or TestMain run.
const (
	childEnv      = "VIKUNJA_AUTOMATION_SCRIPT_PROCESS"
	childEnvValue = "1"
)

const (
	// childMaxStack is the Go stack limit of the script process. The interpreter has its own call stack limit;
	// this only bounds the recursion of native code (the parser, deeply nested values).
	childMaxStack = 256 << 20
	// watchdogGrace is how long after the timeout of its script the process ends itself when the parent has not
	// killed it (the parent kills after killGrace). The parent being gone ends it as well.
	watchdogGrace = 1500 * time.Millisecond
	watchdogTick  = 50 * time.Millisecond
)

// Exit codes of the script process.
const (
	exitParentGone = 4
	exitProtocol   = 3
	// exitOverdue is the watchdog: the script outlived its timeout and the parent did not kill it.
	exitOverdue = 5
)

func init() {
	if os.Getenv(childEnv) != childEnvValue {
		return
	}
	os.Exit(childMain(os.Stdin, os.Stdout))
}

// childMain serves scripts one after the other until the parent closes the pipe: it reads a start message, runs
// the script with a host that forwards every call to the parent and writes the outcome. Each script gets a fresh
// interpreter; the parent kills the process when a script does not finish in time and starts a new one.
func childMain(stdin io.Reader, stdout io.Writer) int {
	out := bufio.NewWriterSize(stdout, 64<<10)
	debug.SetMaxStack(childMaxStack)
	// The kernel ends this process before the server when memory runs out.
	preferOOMKill()

	if err := writeMessage(out, &message{Hello: &helloMessage{Protocol: protocolVersion, Build: buildID(), PID: os.Getpid()}}); err != nil {
		return exitParentGone
	}
	if err := out.Flush(); err != nil {
		return exitParentGone
	}

	// The input is read on its own goroutine, so the end of it (the parent closed it or is gone) ends the
	// process even while the script is inside a native call.
	in := make(chan *message)
	go func() {
		r := bufio.NewReaderSize(stdin, 64<<10)
		for {
			m, err := readMessage(r, maxReplyBytes, true)
			if errors.Is(err, io.EOF) {
				os.Exit(0)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "script process: unreadable input: %v\n", err)
				os.Exit(exitProtocol)
			}
			in <- m
		}
	}()
	w := startWatchdog()

	var limit memoryLimit
	heap := newHeapCleaner()
	for {
		m := <-in
		if m.Start == nil {
			fmt.Fprintln(os.Stderr, "script process: expected a start message")
			return exitProtocol
		}
		start := m.Start
		if !limit.set && start.Limits.MaxMemory > 0 {
			// The parent only hands this process scripts with the same memory limit. The soft limit makes the
			// garbage collector work harder before the hard limit of the operating system ends the process.
			debug.SetMemoryLimit(start.Limits.MaxMemory)
			if err := limit.apply(start.Limits.MaxMemory); err != nil {
				fmt.Fprintf(os.Stderr, "script process: cannot limit memory: %v\n", err)
				return exitProtocol
			}
		}
		deadline := time.Now().Add(start.Budget)
		if start.Budget > 0 {
			// One script at a time: the process-wide match timeout of the regexp engine can be the script's.
			regexp2.DefaultMatchTimeout = start.Budget
			w.arm(deadline.Add(watchdogGrace))
		}

		done := childRun(start, deadline, in, out)
		w.disarm()
		done.Seq = start.Seq
		done.PID = os.Getpid()
		done.Spare = heap.cleanUp(limit.spare(start.Limits.MaxMemory))
		capDone(done)
		if err := writeMessage(out, &message{Done: done}); err != nil {
			return exitParentGone
		}
		if err := out.Flush(); err != nil {
			return exitParentGone
		}
	}
}

// cleanupThreshold is how much garbage a script may leave for the next one before the heap is collected and
// returned to the operating system between the two. A collection costs milliseconds (the heap of the binary is
// large), so scripts that allocated little are not charged for it.
const cleanupThreshold = 8 << 20

// heapCleaner collects the heap between two scripts when the last one left much garbage, so the next script
// starts as a fresh process would: the Go runtime does not collect before a growth of the heap fails at the
// address-space limit, and it never returns address space.
type heapCleaner struct {
	clean uint64
}

func newHeapCleaner() *heapCleaner {
	debug.FreeOSMemory()
	var ms goruntime.MemStats
	goruntime.ReadMemStats(&ms)
	return &heapCleaner{clean: ms.HeapAlloc}
}

// cleanUp runs after a script (nothing of it is referenced any more) and returns spare less the garbage that is
// left for the next script.
func (h *heapCleaner) cleanUp(spare int64) int64 {
	var ms goruntime.MemStats
	goruntime.ReadMemStats(&ms)
	if ms.HeapAlloc > h.clean+cleanupThreshold {
		debug.FreeOSMemory()
		goruntime.ReadMemStats(&ms)
		if ms.HeapAlloc < h.clean {
			h.clean = ms.HeapAlloc
		}
	}
	if spare < 0 {
		return spare
	}
	if ms.HeapAlloc > h.clean {
		spare -= int64(min(ms.HeapAlloc-h.clean, uint64(spare))) //nolint:gosec // at most spare
	}
	return max(spare, 0)
}

// buildID identifies the build: a parent only talks to a child of the same one.
func buildID() string {
	id := goruntime.Version()
	if info, ok := debug.ReadBuildInfo(); ok {
		id += " " + info.Main.Path + "@" + info.Main.Version
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" || s.Key == "vcs.time" || s.Key == "vcs.modified" {
				id += " " + s.Key + "=" + s.Value
			}
		}
	}
	return id + " " + buildVersion()
}

// watchdog ends the process when its script outlived the timeout and the parent did not kill it, or when the
// parent is gone. It runs on its own goroutine; the scheduler preempts a goroutine inside a native call, so it
// runs while the script spins there.
type watchdog struct {
	deadline atomic.Int64
}

func startWatchdog() *watchdog {
	w := &watchdog{}
	ppid := os.Getppid()
	go func() {
		t := time.NewTicker(watchdogTick)
		defer t.Stop()
		for range t.C {
			if os.Getppid() != ppid {
				os.Exit(exitParentGone)
			}
			if d := w.deadline.Load(); d != 0 && time.Now().UnixNano() > d {
				fmt.Fprintln(os.Stderr, "script process: the script outlived its timeout")
				os.Exit(exitOverdue)
			}
		}
	}()
	return w
}

func (w *watchdog) arm(at time.Time) { w.deadline.Store(at.UnixNano()) }
func (w *watchdog) disarm()          { w.deadline.Store(0) }

func childRun(start *startMessage, deadline time.Time, in <-chan *message, out *bufio.Writer) *doneMessage {
	if start.Check {
		_, synErr := compile(start.Source)
		return &doneMessage{Syntax: synErr}
	}
	host := &proxyHost{seq: start.Seq, in: in, out: out}
	parts := map[string]any{
		"event":   fromWireMap(start.Event),
		"task":    fromWireMap(start.Task),
		"project": fromWireMap(start.Project),
	}
	applySpecials(parts, start.Specials)
	input := &Input{
		Source:    start.Source,
		Host:      host,
		Limits:    start.Limits,
		Condition: start.Condition,
		deadline:  deadline,
	}
	input.Event, _ = parts["event"].(map[string]any)
	input.Task, _ = parts["task"].(map[string]any)
	input.Project, _ = parts["project"].(map[string]any)
	res, err := runLocal(context.Background(), input)
	if err != nil {
		var se *Error
		if !errors.As(err, &se) {
			se = &Error{Kind: ErrInternal, Message: err.Error()}
		}
		return &doneMessage{Error: se}
	}
	return &doneMessage{Result: res}
}

// capDone keeps the done message below maxMessageBytes: the runner keeps 16 KiB of a step's output, so nothing is
// lost that would have been shown.
func capDone(d *doneMessage) {
	if d.Result != nil {
		d.Result.Value = cut(d.Result.Value, maxResultBytes)
		d.Result.Log = cut(d.Result.Log, maxDoneLogBytes)
	}
	if d.Error != nil {
		d.Error.Message = cut(d.Error.Message, maxResultBytes)
		d.Error.Stack = cut(d.Error.Stack, maxResultBytes)
	}
	if d.Syntax != nil {
		d.Syntax.Message = cut(d.Syntax.Message, maxResultBytes)
	}
}

const cutMarker = "...[cut off]"

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + cutMarker
}

// proxyHost forwards the host calls of the script to the parent.
type proxyHost struct {
	seq uint64
	in  <-chan *message
	out *bufio.Writer
}

func (h *proxyHost) call(c *callMessage) (*replyMessage, error) {
	c.Seq = h.seq
	b, err := json.Marshal(&message{Call: c})
	if err != nil {
		// Nothing was sent: a number that is not finite has no JSON form.
		return nil, fmt.Errorf("the value cannot be passed to the host: %w", err)
	}
	if len(b) >= maxMessageBytes {
		return nil, fmt.Errorf("the arguments of the call are larger than %d MiB", maxMessageBytes>>20)
	}
	if _, err = h.out.Write(append(b, '\n')); err == nil {
		err = h.out.Flush()
	}
	if err != nil {
		// The parent is gone; nobody would read the outcome.
		os.Exit(exitParentGone)
	}
	m := <-h.in
	if m.Reply == nil {
		fmt.Fprintln(os.Stderr, "script process: expected a reply")
		os.Exit(exitProtocol)
	}
	if m.Reply.Failed {
		return m.Reply, errors.New(m.Reply.Error)
	}
	return m.Reply, nil
}

// value is the value of a reply, with the numbers JSON cannot hold put back.
func (r *replyMessage) value() any {
	return applySpecials(fromWire(r.Value), r.Specials)
}

func (h *proxyHost) TaskGet(_ context.Context, id int64) (map[string]any, error) {
	r, err := h.call(&callMessage{Method: "TaskGet", ID: id})
	if err != nil {
		return nil, err
	}
	m, _ := r.value().(map[string]any)
	return m, nil
}

func (h *proxyHost) TaskFind(_ context.Context, filter string, projectID int64, limit int) ([]map[string]any, error) {
	r, err := h.call(&callMessage{Method: "TaskFind", Text: filter, ID: projectID, Limit: limit})
	if err != nil {
		return nil, err
	}
	list, _ := r.value().([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		m, _ := item.(map[string]any)
		out = append(out, m)
	}
	return out, nil
}

func (h *proxyHost) TaskUpdate(_ context.Context, id int64, patch map[string]any) (map[string]any, error) {
	r, err := h.call(&callMessage{Method: "TaskUpdate", ID: id, Data: patch})
	if err != nil {
		return nil, err
	}
	m, _ := r.value().(map[string]any)
	return m, nil
}

func (h *proxyHost) TaskCreate(_ context.Context, projectID int64, data map[string]any) (map[string]any, error) {
	r, err := h.call(&callMessage{Method: "TaskCreate", ID: projectID, Data: data})
	if err != nil {
		return nil, err
	}
	m, _ := r.value().(map[string]any)
	return m, nil
}

func (h *proxyHost) TaskComment(_ context.Context, id int64, text string) error {
	_, err := h.call(&callMessage{Method: "TaskComment", ID: id, Text: text})
	return err
}

func (h *proxyHost) FieldGet(_ context.Context, taskID, fieldID int64) (any, error) {
	r, err := h.call(&callMessage{Method: "FieldGet", ID: taskID, ID2: fieldID})
	if err != nil {
		return nil, err
	}
	return r.value(), nil
}

func (h *proxyHost) FieldSet(_ context.Context, taskID, fieldID int64, value any) error {
	_, err := h.call(&callMessage{Method: "FieldSet", ID: taskID, ID2: fieldID, Value: value})
	return err
}

func (h *proxyHost) StatusSet(_ context.Context, taskID, statusID int64, comment string) error {
	_, err := h.call(&callMessage{Method: "StatusSet", ID: taskID, ID2: statusID, Text: comment})
	return err
}

func (h *proxyHost) AssigneeAdd(_ context.Context, taskID, userID int64) error {
	_, err := h.call(&callMessage{Method: "AssigneeAdd", ID: taskID, ID2: userID})
	return err
}

func (h *proxyHost) AssigneeRemove(_ context.Context, taskID, userID int64) error {
	_, err := h.call(&callMessage{Method: "AssigneeRemove", ID: taskID, ID2: userID})
	return err
}

func (h *proxyHost) LabelAdd(_ context.Context, taskID, labelID int64) error {
	_, err := h.call(&callMessage{Method: "LabelAdd", ID: taskID, ID2: labelID})
	return err
}

func (h *proxyHost) LabelRemove(_ context.Context, taskID, labelID int64) error {
	_, err := h.call(&callMessage{Method: "LabelRemove", ID: taskID, ID2: labelID})
	return err
}

func (h *proxyHost) Fetch(_ context.Context, req *FetchRequest) (*FetchResponse, error) {
	r, err := h.call(&callMessage{Method: "Fetch", Fetch: req})
	if err != nil {
		return nil, err
	}
	return r.Fetch, nil
}

func (h *proxyHost) SecretGet(_ context.Context, name string) (string, error) {
	r, err := h.call(&callMessage{Method: "SecretGet", Text: name})
	if err != nil {
		return "", err
	}
	return r.Text, nil
}
