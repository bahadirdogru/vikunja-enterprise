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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/log"
)

// Scripts run in child processes: the executable of this process, started again with childEnv set, so the script
// process is the same binary (also in the single-binary container image). The interpreter only sees its
// interrupt between JavaScript instructions; a single native call (a built-in looping over a huge array-like, an
// allocation of gigabytes, the parser on a huge eval) never sees it, and a fatal error of the Go runtime (out of
// memory, stack exhaustion) cannot be recovered. In a child process neither matters: the parent kills it at the
// deadline, and the operating system bounds its memory.
//
// A script process serves one script at a time, each with a fresh interpreter. One that finished in time goes
// back to a small pool of idle processes for the next script (starting the binary costs tens of milliseconds);
// one that did not, or that failed in any other way, is killed and never used again.
//
// The parent answers the host calls of the script with the Host of the run, on the goroutine that called Run,
// so database work stays in the transaction and under the permissions of the run.

const (
	cannotStart = "cannot start the script process"
	// killGrace is how long the parent waits after the timeout of the script for the child to report the timeout
	// itself (with the position in the source) before it kills the child.
	killGrace = 500 * time.Millisecond
	// maxStderrBytes is how much of the error output of a child is kept.
	maxStderrBytes = 16 << 10
	// maxRunsPerProcess retires a script process after this many scripts.
	maxRunsPerProcess = 1000
	// idleTimeout ends a script process that waited this long for a script.
	idleTimeout = time.Minute
)

var (
	executableOnce sync.Once
	executablePath string
	executableErr  error
)

func executable() (string, error) {
	executableOnce.Do(func() { executablePath, executableErr = os.Executable() })
	return executablePath, executableErr
}

// childEnviron is the whole environment of the script process: it gets nothing of the parent's (no database
// password, no service secret), only what the interpreter needs to behave as it would in the parent.
func childEnviron() []string {
	env := []string{childEnv + "=" + childEnvValue, "GOMAXPROCS=1"}
	for _, name := range []string{"TZ", "ZONEINFO", "SYSTEMROOT", "SystemRoot", "GOCOVERDIR"} {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// child is one script process.
type child struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	stderr    *cappedBuffer
	maxMemory int64
	runs      int
	idle      *time.Timer
	waitOnce  sync.Once
	waitErr   error
}

func startChild(maxMemory int64) (*child, error) {
	exe, err := executable()
	if err != nil {
		return nil, err
	}
	// Not bound to a context: the process outlives the run that started it and is killed explicitly.
	cmd := exec.Command(exe) //nolint:noctx // see above
	cmd.Env = childEnviron()
	c := &child{cmd: cmd, stderr: &cappedBuffer{max: maxStderrBytes}, maxMemory: maxMemory}
	cmd.Stderr = c.stderr
	configureChild(cmd)
	if c.stdin, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c.stdout = bufio.NewReaderSize(stdout, 64<<10)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *child) wait() error {
	c.waitOnce.Do(func() { c.waitErr = c.cmd.Wait() })
	return c.waitErr
}

// kill ends the process at once and reaps it.
func (c *child) kill() error {
	_ = c.cmd.Process.Kill()
	return c.wait()
}

// close lets an idle process end by itself (it exits when its input ends) and makes sure it does.
func (c *child) close() {
	_ = c.stdin.Close()
	go func() {
		t := time.AfterFunc(5*time.Second, func() { _ = c.cmd.Process.Kill() })
		_ = c.wait()
		t.Stop()
	}()
}

// processPool keeps idle script processes.
type processPool struct {
	mu   sync.Mutex
	idle []*child
}

var pool = &processPool{}

// maxIdle is how many idle script processes are kept: one per worker, plus one for test runs.
func maxIdle() int {
	n := config.AutomationWorkers.GetInt()
	if n < 1 {
		n = 1
	}
	return n + 1
}

func (p *processPool) get(maxMemory int64) *child {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.idle) > 0 {
		c := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]
		if !c.idle.Stop() {
			// The idle timer fired and is closing it.
			continue
		}
		if c.maxMemory != maxMemory {
			c.close()
			continue
		}
		return c
	}
	return nil
}

func (p *processPool) put(c *child) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c.runs >= maxRunsPerProcess || len(p.idle) >= maxIdle() {
		c.close()
		return
	}
	c.idle = time.AfterFunc(idleTimeout, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, other := range p.idle {
			if other == c {
				p.idle = append(p.idle[:i], p.idle[i+1:]...)
				break
			}
		}
		c.close()
	})
	p.idle = append(p.idle, c)
}

// Run executes the script in a script process and returns when it finished, or at the latest shortly after its
// timeout. A *Error is a failure of the script itself; any other error (a cancelled parent context) is not.
func Run(ctx context.Context, in *Input) (*Result, error) {
	started := time.Now()
	done, err := runInProcess(ctx, in, false)
	if err != nil {
		return nil, err
	}
	if done.Error != nil {
		return nil, done.Error
	}
	res := done.Result
	res.Duration = time.Since(started)
	return res, nil
}

// Check compiles a source without running it. Compiling takes more than linear time for some sources, so it
// happens in a script process as well, with the timeout of a script action.
func Check(source string) *SyntaxError {
	in := &Input{Source: source, Limits: ActionLimits()}
	done, err := runInProcess(context.Background(), in, true)
	switch {
	case err == nil && done.Error == nil:
		return done.Syntax
	case err == nil && strings.HasPrefix(done.Error.Message, cannotStart):
		// The script process cannot be started at all; scripts will not run either, but the source can still be
		// checked here.
		_, synErr := compile(source)
		return synErr
	case err == nil && done.Error.Kind == ErrTimeout:
		return &SyntaxError{Message: fmt.Sprintf("compiling the script took longer than %s; simplify it", in.Limits.Timeout), Line: 1, Column: 1}
	case err == nil:
		return &SyntaxError{Message: "the script cannot be compiled: " + done.Error.Message, Line: 1, Column: 1}
	}
	return &SyntaxError{Message: "the script cannot be compiled: " + err.Error(), Line: 1, Column: 1}
}

// runInProcess runs the script, or only compiles it, in a script process. The error is one of the parent context;
// everything else is in the outcome.
func runInProcess(ctx context.Context, in *Input, check bool) (*doneMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The deadline of the host calls is the timeout of the script; the child is killed a little later.
	runCtx, cancel := context.WithTimeout(ctx, in.Limits.Timeout)
	defer cancel()
	killCtx, cancelKill := context.WithTimeout(ctx, in.Limits.Timeout+killGrace)
	defer cancelKill()

	c := pool.get(in.Limits.MaxMemory)
	for attempt := 0; ; attempt++ {
		pooled := c != nil
		if !pooled {
			var err error
			if c, err = startChild(in.Limits.MaxMemory); err != nil {
				return &doneMessage{Error: &Error{Kind: ErrInternal, Message: cannotStart + ": " + err.Error()}}, nil
			}
		}
		done, retry, err := runIn(ctx, runCtx, killCtx, c, in, check)
		if retry && pooled && attempt == 0 {
			// An idle process that died while it waited, before it read anything of this script: nothing of the
			// script ran, so a fresh process can run it.
			c = nil
			continue
		}
		return done, err
	}
}

// runIn runs the script in the process c, which it returns to the pool or kills. retry is set when the process
// was gone before it answered anything.
func runIn(ctx, runCtx, killCtx context.Context, c *child, in *Input, check bool) (outcome *doneMessage, retry bool, err error) {
	c.runs++
	killed := make(chan bool, 1)
	finished := make(chan struct{})
	go func() {
		select {
		case <-killCtx.Done():
			_ = c.cmd.Process.Kill()
			killed <- true
		case <-finished:
			killed <- false
		}
	}()

	p := &parent{ctx: runCtx, in: in, w: c.stdin, check: check}
	done, answered, pumpErr := p.pump(c.stdout)
	close(finished)
	wasKilled := <-killed

	if done != nil && !wasKilled && !p.abandoned {
		pool.put(c)
	} else {
		_ = c.kill()
	}

	fail := func(e *Error) (*doneMessage, bool, error) { return &doneMessage{Error: e}, false, nil }
	switch {
	case done != nil:
		return done, false, nil
	case ctx.Err() != nil:
		return nil, false, ctx.Err()
	case wasKilled || killCtx.Err() != nil:
		return fail(&Error{Kind: ErrTimeout, Message: fmt.Sprintf("the script ran longer than %s and was stopped", in.Limits.Timeout)})
	case errors.Is(pumpErr, errMessageTooLarge):
		return fail(&Error{Kind: ErrThrown, Message: fmt.Sprintf("the script passed more than %d MiB to the server at once", maxMessageBytes>>20)})
	case !answered && c.stderr.String() == "":
		return &doneMessage{Error: &Error{Kind: ErrInternal, Message: "the script process ended unexpectedly: " + fmt.Sprint(c.wait())}}, true, nil
	}
	return fail(exitError(in, c.cmd.ProcessState, c.wait(), pumpErr, c.stderr.String()))
}

// exitError explains a child that ended without reporting.
func exitError(in *Input, state *os.ProcessState, waitErr, pumpErr error, stderr string) *Error {
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "out of memory"), strings.Contains(low, "cannot allocate memory"),
		strings.Contains(low, "failed to create new os thread"), strings.Contains(low, "pthread_create failed"):
		limit := "the memory limit"
		if in.Limits.MaxMemory > 0 {
			limit = fmt.Sprintf("%d MiB of memory", in.Limits.MaxMemory>>20)
		}
		return &Error{Kind: ErrMemory, Message: fmt.Sprintf("the script used more than %s and was stopped", limit)}
	case strings.Contains(low, "stack overflow"), strings.Contains(low, "stack exceeds"):
		return &Error{Kind: ErrStackOverflow, Message: "the script nested too deeply for the interpreter and was stopped"}
	case cpuLimitExceeded(state) || strings.Contains(stderr, "SIGXCPU"):
		return &Error{Kind: ErrTimeout, Message: fmt.Sprintf("the script ran longer than %s and was stopped", in.Limits.Timeout)}
	}
	detail := firstLine(stderr)
	if detail == "" {
		detail = fmt.Sprint(waitErr)
		if pumpErr != nil && !errors.Is(pumpErr, io.EOF) {
			detail += ", " + pumpErr.Error()
		}
	}
	log.Errorf("automation: the script process ended unexpectedly (%v): %s", waitErr, stderr)
	return &Error{Kind: ErrInternal, Message: "the script process ended unexpectedly: " + detail}
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// parent is the side of the run in this process: it sends the input and answers the host calls.
type parent struct {
	ctx context.Context
	in  *Input
	w   io.Writer
	// check only compiles the source.
	check bool
	// abandoned is set when the conversation ended in the middle.
	abandoned bool
}

// pump runs the conversation until the child reports its outcome or the pipe breaks. answered says whether the
// child sent anything.
func (p *parent) pump(r *bufio.Reader) (done *doneMessage, answered bool, err error) {
	start := &startMessage{
		Source:    p.in.Source,
		Event:     wireMap(p.in.Event),
		Task:      wireMap(p.in.Task),
		Project:   wireMap(p.in.Project),
		Limits:    p.in.Limits,
		Condition: p.in.Condition,
		Check:     p.check,
	}
	if err := writeMessage(p.w, &message{Start: start}); err != nil {
		return nil, false, err
	}
	for {
		m, err := readMessage(r, maxMessageBytes, false)
		if err != nil {
			return nil, answered, err
		}
		answered = true
		switch {
		case m.Done != nil:
			if m.Done.Error == nil && m.Done.Result == nil && !p.check {
				return nil, true, errors.New("the script process reported nothing")
			}
			return m.Done, true, nil
		case m.Call != nil:
			reply, panicked := p.safeAnswer(m.Call)
			if panicked != "" {
				// The child waits for an answer it will not get: it is not reused.
				p.abandoned = true
				return &doneMessage{Error: &Error{Kind: ErrInternal, Message: panicked}}, true, nil
			}
			if err := writeMessage(p.w, &message{Reply: reply}); err != nil {
				return nil, true, err
			}
		default:
			return nil, true, errors.New("unexpected message from the script process")
		}
	}
}

func failed(err error) *replyMessage {
	return &replyMessage{Failed: true, Error: err.Error()}
}

// safeAnswer is answer with the panic of a host turned into an internal error of the script, as the interpreter
// did in-process.
func (p *parent) safeAnswer(c *callMessage) (reply *replyMessage, panicked string) {
	defer func() {
		if v := recover(); v != nil {
			reply, panicked = nil, fmt.Sprintf("internal error: %v", v)
		}
	}()
	return p.answer(c), ""
}

// idsOf is how many ids (positive integers) each host call takes.
var idsOf = map[string]int{
	"TaskGet": 1, "TaskFind": 0, "TaskUpdate": 1, "TaskCreate": 1, "TaskComment": 1,
	"FieldGet": 2, "FieldSet": 2, "StatusSet": 2,
	"AssigneeAdd": 2, "AssigneeRemove": 2, "LabelAdd": 2, "LabelRemove": 2,
	"Fetch": 0, "SecretGet": 0,
}

// answer performs one host call. The child validated the arguments already; they are checked again because the
// parent does not trust what comes out of the interpreter.
func (p *parent) answer(c *callMessage) *replyMessage {
	h, ctx := p.in.Host, p.ctx
	value := func(v any, err error) *replyMessage {
		if err != nil {
			return failed(err)
		}
		return &replyMessage{Value: wireValue(v)}
	}
	plain := func(err error) *replyMessage {
		if err != nil {
			return failed(err)
		}
		return &replyMessage{}
	}
	invalid := failed(fmt.Errorf("invalid call of %s", c.Method))
	n, known := idsOf[c.Method]
	if !known || (n >= 1 && c.ID < 1) || (n >= 2 && c.ID2 < 1) {
		return invalid
	}

	switch c.Method {
	case "TaskGet":
		return value(h.TaskGet(ctx, c.ID))
	case "TaskFind":
		limit := p.in.Limits.MaxResults
		if limit <= 0 {
			limit = 200
		}
		if c.ID < 0 || c.Limit < 1 || c.Limit > limit {
			return invalid
		}
		list, err := h.TaskFind(ctx, c.Text, c.ID, c.Limit)
		if len(list) > c.Limit {
			list = list[:c.Limit]
		}
		return value(list, err)
	case "TaskUpdate":
		return value(h.TaskUpdate(ctx, c.ID, orEmpty(c.Data)))
	case "TaskCreate":
		return value(h.TaskCreate(ctx, c.ID, orEmpty(c.Data)))
	case "TaskComment":
		return plain(h.TaskComment(ctx, c.ID, c.Text))
	case "FieldGet":
		return value(h.FieldGet(ctx, c.ID, c.ID2))
	case "FieldSet":
		return plain(h.FieldSet(ctx, c.ID, c.ID2, c.Value))
	case "StatusSet":
		return plain(h.StatusSet(ctx, c.ID, c.ID2, c.Text))
	case "AssigneeAdd":
		return plain(h.AssigneeAdd(ctx, c.ID, c.ID2))
	case "AssigneeRemove":
		return plain(h.AssigneeRemove(ctx, c.ID, c.ID2))
	case "LabelAdd":
		return plain(h.LabelAdd(ctx, c.ID, c.ID2))
	case "LabelRemove":
		return plain(h.LabelRemove(ctx, c.ID, c.ID2))
	case "Fetch":
		return p.fetch(c)
	case "SecretGet":
		v, err := h.SecretGet(ctx, c.Text)
		if err != nil {
			return failed(err)
		}
		return &replyMessage{Text: v}
	}
	return invalid
}

func (p *parent) fetch(c *callMessage) *replyMessage {
	if c.Fetch == nil {
		return failed(errors.New("invalid call of Fetch"))
	}
	if c.Fetch.Headers == nil {
		c.Fetch.Headers = map[string]string{}
	}
	resp, err := p.in.Host.Fetch(p.ctx, c.Fetch)
	if err != nil {
		return failed(err)
	}
	return &replyMessage{Fetch: resp}
}

// cappedBuffer keeps the first max bytes written to it.
type cappedBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - len(b.buf); room > 0 {
		if len(p) > room {
			b.buf = append(b.buf, p[:room]...)
		} else {
			b.buf = append(b.buf, p...)
		}
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
