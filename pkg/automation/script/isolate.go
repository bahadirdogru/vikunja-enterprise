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
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/version"
)

// Scripts run in child processes: the binary of this process (on Linux the running file itself, also after an
// upgrade replaced it), started again with childEnv set, so the script process is the same build (also in the
// single-binary container image). The interpreter only sees its interrupt between JavaScript instructions; a
// single native call (a built-in looping over a huge array-like, an allocation of gigabytes, the parser on a huge
// eval) never sees it, and a fatal error of the Go runtime (out of memory, stack exhaustion) cannot be recovered.
// In a child process neither matters: the parent kills it at the deadline, it ends itself a little later if the
// parent did not (watchdog), and the operating system bounds its memory (Linux).
//
// The child is not trusted: the parent checks the hello of a new process (protocol and build), the sequence
// number of every message, the size of every line before it is buffered, and enforces the limits of the run
// (calls, requests, sizes) again. Whatever breaks the protocol is killed.
//
// A script process serves one script at a time, each with a fresh interpreter. It goes back to a small pool of
// idle processes for the next script (starting the binary costs tens of milliseconds) only after a script that
// succeeded or ended with an ordinary exception, whose memory was returned so that the next script can allocate
// about as much as in a fresh process, and with nothing left unread on its output. Any other outcome (timeout,
// stack or memory limit, internal error, a broken protocol) kills it.
//
// The number of live script processes is bounded: one per worker plus extraProcesses, which dry runs and the
// compile check share, so those can never take a worker's process. A run waits for a process within its own
// deadline; one that does not get one in time fails with a temporary error (busy) that is retried.
//
// The parent answers the host calls of the script with the Host of the run, on the goroutine that called Run,
// so database work stays in the transaction and under the permissions of the run.

const (
	// killGrace is how long the parent waits after the timeout of the script for the child to report the timeout
	// itself (with the position in the source) before it kills the child.
	killGrace = 500 * time.Millisecond
	// maxStderrBytes is how much of the error output of a child is kept per run, maxStderrLogBytes how much of
	// it is logged.
	maxStderrBytes    = 16 << 10
	maxStderrLogBytes = 2 << 10
	// maxRunsPerProcess retires a script process after this many scripts.
	maxRunsPerProcess = 1000
	// idleTimeout ends a script process that waited this long for a script.
	idleTimeout = time.Minute
	// extraProcesses are the script processes beyond one per worker, for dry runs and compile checks.
	extraProcesses = 2
	// poolMargin: a process is pooled again only when the next script can allocate at least its memory limit
	// minus this much. The Go runtime does not give address space back, so a script that grew the heap leaves
	// less; such a process is retired instead.
	poolMargin = 32 << 20
)

// ErrUnavailable is a failure of the script machinery, not of the script: the process could not be started, did
// not identify itself, broke the protocol, died for a reason the script cannot be blamed for, or no process was
// free in time. It is temporary; the run is retried and the rule is not blamed.
var ErrUnavailable = errors.New("the script could not be run")

// UnavailableError is ErrUnavailable with the detail.
type UnavailableError struct {
	Msg string
	// Busy is set when every script process was in use until the deadline.
	Busy bool
	// Start is set when no process could be started or the one started did not identify itself.
	Start bool
}

func (e *UnavailableError) Error() string        { return e.Msg }
func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

// childArg is the argument of the script process; this binary ignores it (the script process starts in init).
const childArg = "__vikunja-script-process"

var (
	// helloTimeout is how long a new process may take to identify itself.
	helloTimeout = 5 * time.Second
	// executableOverride replaces the binary of the script process in tests.
	executableOverride string
)

// SetTestExecutable makes path the binary of new script processes, for the tests of other packages; restore
// undoes it. Idle processes are ended either way.
func SetTestExecutable(path string) (restore func()) {
	pool.drain()
	executableOverride = path
	return func() {
		pool.drain()
		executableOverride = ""
	}
}

var (
	errBusy        = &UnavailableError{Msg: "the server is busy running other scripts; try again in a moment", Busy: true}
	errProjectBusy = &UnavailableError{Msg: "another test run or check of a script of this project is still running; try again in a moment", Busy: true}
)

func buildVersion() string { return version.Version }

var (
	parentBuildOnce sync.Once
	parentBuild     string
)

func thisBuild() string {
	parentBuildOnce.Do(func() { parentBuild = buildID() })
	return parentBuild
}

// childEnviron is the whole environment of the script process: it gets nothing of the parent's (no database
// password, no service secret), only what the interpreter needs to behave as it would in the parent.
func childEnviron() []string {
	env := []string{childEnv + "=" + childEnvValue, "GOMAXPROCS=1", "MALLOC_ARENA_MAX=1"}
	names := []string{"TZ", "ZONEINFO", "SYSTEMROOT", "SystemRoot"}
	if testing.Testing() {
		// Coverage of the script process in the tests.
		names = append(names, "GOCOVERDIR")
	}
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// processes bounds the live script processes.
type processes struct {
	once sync.Once
	// live holds a token for every live process, idle or busy.
	live chan struct{}
	// extra holds a token for every interactive run (dry runs, checks).
	extra chan struct{}
	count atomic.Int64

	// changed is closed (and replaced) whenever a process went back to the pool or ended, so every waiter looks
	// again; a channel of one signal lost wake-ups when two waited.
	mu      sync.Mutex
	changed chan struct{}
	// projects holds the interactive run of each project: one at a time, so one project cannot hold both
	// extra processes.
	projects map[int64]*projectSlot
}

type projectSlot struct {
	ch    chan struct{}
	users int
}

func (p *processes) waitChange() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.changed
}

func (p *processes) notify() {
	p.mu.Lock()
	defer p.mu.Unlock()
	close(p.changed)
	p.changed = make(chan struct{})
}

// enterProject waits until the project has no other interactive run, at most until done; release ends it.
func (p *processes) enterProject(projectID int64, done <-chan struct{}) (release func(), ok bool) {
	p.mu.Lock()
	slot := p.projects[projectID]
	if slot == nil {
		slot = &projectSlot{ch: make(chan struct{}, 1)}
		p.projects[projectID] = slot
	}
	slot.users++
	p.mu.Unlock()
	leave := func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		slot.users--
		if slot.users == 0 {
			delete(p.projects, projectID)
		}
	}
	select {
	case slot.ch <- struct{}{}:
		return func() {
			<-slot.ch
			leave()
		}, true
	case <-done:
		leave()
		return nil, false
	}
}

var procs = &processes{}

func (p *processes) init() {
	p.once.Do(func() {
		workers := config.AutomationWorkers.GetInt()
		if workers < 1 {
			workers = 1
		}
		p.live = make(chan struct{}, workers+extraProcesses)
		p.extra = make(chan struct{}, extraProcesses)
		p.changed = make(chan struct{})
		p.projects = map[int64]*projectSlot{}
	})
}

// liveProcesses is the number of script processes that run now.
func liveProcesses() int64 { return procs.count.Load() }

// spawner starts every child from one locked thread that never ends: on Linux the child gets SIGKILL when the
// thread that started it ends (Pdeathsig), which would otherwise be whatever thread the Go runtime retires.
var (
	spawnOnce sync.Once
	spawnCh   chan spawnRequest
)

type spawnRequest struct {
	cmd *exec.Cmd
	err chan error
}

func spawn(cmd *exec.Cmd) error {
	spawnOnce.Do(func() {
		spawnCh = make(chan spawnRequest)
		go func() {
			goruntime.LockOSThread()
			for req := range spawnCh {
				req.err <- req.cmd.Start()
			}
		}()
	})
	req := spawnRequest{cmd: cmd, err: make(chan error, 1)}
	spawnCh <- req
	return <-req.err
}

// child is one script process.
type child struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	stderr    *cappedBuffer
	maxMemory int64
	pid       int
	runs      int
	seq       uint64
	idle      *time.Timer
	waitOnce  sync.Once
	waitErr   error
	// killedHere is set when the server sent the SIGKILL (a broken conversation, the deadline).
	killedHere atomic.Bool
	exited     atomic.Bool
}

// exitWithin waits up to d for the process to end by itself, so a process that died (and closed its pipes) is
// reaped before the server kills it and its own signal is seen.
func (c *child) exitWithin(d time.Duration) {
	ended := make(chan struct{})
	go func() {
		_ = c.wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(d):
	}
}

// startChild starts a process and waits for its hello, at most until deadline. The caller holds a token of
// procs.live for it; the process gives it back when it ends.
func startChild(maxMemory int64) (*child, error) {
	unavailable := func(format string, args ...any) error {
		return &UnavailableError{Msg: "cannot start the script process: " + fmt.Sprintf(format, args...), Start: true}
	}
	release := func() { <-procs.live }
	exe, err := childExecutable()
	if executableOverride != "" {
		exe, err = executableOverride, nil
	}
	if err != nil {
		release()
		return nil, unavailable("%v", err)
	}
	// Not bound to a context: the process outlives the run that started it and is killed explicitly.
	// The argument makes a binary without the script process (an older or an upstream build at the same path,
	// outside Linux) stop with "unknown command" instead of starting a server.
	cmd := exec.Command(exe, childArg) //nolint:noctx // see above
	cmd.Env = childEnviron()
	c := &child{cmd: cmd, stderr: &cappedBuffer{max: maxStderrBytes}, maxMemory: maxMemory}
	cmd.Stderr = c.stderr
	// Wait must not hang on a pipe that something else still holds open once the process is gone.
	cmd.WaitDelay = time.Second
	configureChild(cmd)
	stdout, err := cmd.StdoutPipe()
	if err == nil {
		c.stdin, err = cmd.StdinPipe()
	}
	if err == nil {
		err = spawn(cmd)
	}
	if err != nil {
		release()
		return nil, unavailable("%v", err)
	}
	procs.count.Add(1)
	c.pid = cmd.Process.Pid
	c.stdout = bufio.NewReaderSize(stdout, 64<<10)

	wait := helloTimeout
	timer := time.AfterFunc(wait, func() { _ = cmd.Process.Kill() })
	m, err := readMessage(c.stdout, maxMessageBytes, false)
	timer.Stop()
	switch {
	case err != nil || m.Hello == nil:
		_ = c.kill()
		log.Errorf("automation: the script process (%s) did not identify itself within %s (%v): %s", exe, wait, err, c.stderr.logLine())
		return nil, &UnavailableError{Msg: "cannot start the script process: the process did not identify itself"}
	case m.Hello.Protocol != protocolVersion || m.Hello.Build != thisBuild():
		_ = c.kill()
		log.Errorf("automation: the script process (%s) is of another build (%q, protocol %d), this server is %q (protocol %d): the binary was replaced; restart Vikunja", exe, m.Hello.Build, m.Hello.Protocol, thisBuild(), protocolVersion)
		return nil, &UnavailableError{Msg: "cannot start the script process: the binary is of another build; Vikunja has to be restarted"}
	}
	return c, nil
}

func (c *child) wait() error {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
		c.exited.Store(true)
		procs.count.Add(-1)
		<-procs.live
		procs.notify()
	})
	return c.waitErr
}

// killedByOthers reports whether the process ended by a SIGKILL the server did not send.
func (c *child) killedByOthers() bool {
	return killedBySignal(c.cmd.ProcessState) && !c.killedHere.Load()
}

// kill ends the process at once and reaps it.
func (c *child) kill() error {
	if !c.exited.Load() {
		c.killedHere.Store(true)
		_ = c.cmd.Process.Kill()
	}
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

// processPool keeps idle script processes, the most recently used last.
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

// drain ends every idle process.
func (p *processPool) drain() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.idle {
		if c.idle.Stop() {
			c.close()
		}
	}
	p.idle = nil
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
	procs.notify()
}

// acquire returns an idle process or starts one, waiting for a free slot until deadline.
func acquire(ctx context.Context, deadline time.Time, maxMemory int64, fresh bool) (c *child, pooled bool, err error) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		// Taken before looking, so a change in between is not missed.
		changed := procs.waitChange()
		if c := pool.get(maxMemory); c != nil {
			if !fresh {
				return c, true, nil
			}
			// A retry needs a new process: an idle one is ended to make room when every slot is taken.
			if len(procs.live) == cap(procs.live) {
				c.close()
			} else {
				pool.put(c)
			}
		}
		select {
		case procs.live <- struct{}{}:
			if time.Until(deadline) <= 0 {
				<-procs.live
				return nil, false, errBusy
			}
			// The hello has a window of its own, not what is left of the caller's time: a process that is slow to
			// start must not look like one that cannot start.
			c, err := startChild(maxMemory)
			return c, false, err
		case <-changed:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timer.C:
			return nil, false, errBusy
		}
	}
}

// Run executes the script in a script process and returns when it finished, or at the latest shortly after its
// timeout (plus the host call in flight). A *Error is a failure of the script itself; ErrUnavailable is one of
// the machinery and temporary; any other error is the parent context's.
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

// Check compiles a source of a project without running it. Compiling takes more than linear time for some sources,
// so it happens in a script process as well, with the timeout of a script action, and never in the server: when no
// script process is available the error is ErrUnavailable and the save is refused. Otherwise the error is the
// context's.
func Check(ctx context.Context, projectID int64, source string) (*SyntaxError, error) {
	in := &Input{Source: source, Limits: ActionLimits(), Interactive: true, ProjectID: projectID}
	done, err := runInProcess(ctx, in, true)
	switch {
	case err != nil:
		return nil, err
	case done.Error == nil:
		return done.Syntax, nil
	case done.Error.Kind == ErrTimeout:
		return &SyntaxError{Message: fmt.Sprintf("compiling the script took longer than %s; simplify it", in.Limits.Timeout), Line: 1, Column: 1}, nil
	}
	return &SyntaxError{Message: "the script cannot be compiled: " + done.Error.Message, Line: 1, Column: 1}, nil
}

// Preflight starts one script process and reports why scripts cannot run, if they cannot. The process stays in the
// pool.
func Preflight() error {
	procs.init()
	select {
	case procs.live <- struct{}{}:
	default:
		return nil
	}
	c, err := startChild(ActionLimits().MaxMemory)
	if err != nil {
		return err
	}
	pool.put(c)
	return nil
}

// runInProcess runs the script, or only compiles it, in a script process. The error is the parent context's or
// an *UnavailableError; everything that is the script's own doing is in the outcome.
func runInProcess(ctx context.Context, in *Input, check bool) (*doneMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	procs.init()
	// The deadline holds from here: waiting for a process and starting it count against the script's time.
	deadline := time.Now().Add(in.Limits.Timeout)

	if in.Interactive {
		waitCtx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		release, ok := procs.enterProject(in.ProjectID, waitCtx.Done())
		if !ok {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errProjectBusy
		}
		defer release()
		select {
		case procs.extra <- struct{}{}:
			defer func() { <-procs.extra }()
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-waitCtx.Done():
			return nil, errBusy
		}
	}

	for attempt := 0; ; attempt++ {
		done, retry, err := runAttempt(ctx, in, check, deadline, attempt)
		if !retry {
			return done, err
		}
		// The pooled process failed in a way that is its own (it died while it waited, or ran out of memory
		// where a new one might not): the script runs again in a new process, with its whole timeout. Reusing the
		// first deadline made a memory error that took the timeout come back as "busy": retried, never blamed.
		deadline = time.Now().Add(in.Limits.Timeout)
	}
}

// runAttempt is one attempt of runInProcess until deadline; the first may take a pooled process, a retry gets a
// new one.
func runAttempt(ctx context.Context, in *Input, check bool, deadline time.Time, attempt int) (*doneMessage, bool, error) {
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	killCtx, cancelKill := context.WithDeadline(ctx, deadline.Add(killGrace))
	defer cancelKill()
	c, pooled, err := acquire(ctx, deadline, in.Limits.MaxMemory, attempt > 0)
	if err != nil {
		return nil, false, err
	}
	if time.Until(deadline) <= 0 {
		pool.put(c)
		return nil, false, errBusy
	}
	return runIn(ctx, runCtx, killCtx, c, in, deadline, check, pooled && attempt == 0)
}

// reusable reports whether a process may serve another script after this outcome: only after a success or an
// ordinary exception, never after the interpreter was stopped (timeout), overflowed or broke.
func reusable(done *doneMessage, maxMemory int64) bool {
	if done.Error != nil && done.Error.Kind != ErrThrown && done.Error.Kind != ErrBadResult {
		return false
	}
	return maxMemory <= 0 || done.Spare < 0 || done.Spare >= maxMemory-poolMargin
}

// runIn runs the script in the process c, which it returns to the pool or kills. retry is set when a pooled
// process was gone before it answered anything, so a fresh one can run the script.
//
// Blame is default-deny: once the start message was sent, whatever ends the run and is not positively the
// machinery's is the script's fault. The only exception is a SIGKILL the server did not send: with oom_score_adj
// 1000 the kernel kills a script process for memory pressure that anyone may have caused, while a script's own
// excess shows up as the address-space limit and the Go runtime's out-of-memory error. Such a run is not blamed;
// it is retried only when no host call with an effect outside the transaction (http.fetch) was made.
func runIn(ctx, runCtx, killCtx context.Context, c *child, in *Input, deadline time.Time, check, mayRetry bool) (outcome *doneMessage, retry bool, err error) {
	c.runs++
	c.seq++
	c.stderr.reset()
	killed := make(chan bool, 1)
	finished := make(chan struct{})
	go func() {
		select {
		case <-killCtx.Done():
			c.killedHere.Store(true)
			_ = c.cmd.Process.Kill()
			killed <- true
		case <-finished:
			killed <- false
		}
	}()

	p := &parent{ctx: runCtx, in: in, w: c.stdin, check: check, seq: c.seq}
	done, answered, pumpErr := p.pump(c.stdout, time.Until(deadline))
	close(finished)
	wasKilled := <-killed

	switch {
	case p.inputTooLarge && !wasKilled:
		// Nothing was sent; the process is as it was.
		pool.put(c)
	case done != nil && !wasKilled && reusable(done, in.Limits.MaxMemory) && c.stdout.Buffered() == 0:
		pool.put(c)
	default:
		if done == nil && !wasKilled {
			c.exitWithin(100 * time.Millisecond)
		}
		_ = c.kill()
	}

	fail := func(e *Error) (*doneMessage, bool, error) { return &doneMessage{Error: e}, false, nil }
	switch {
	case done != nil:
		if p.oversized != "" && done.Error != nil && done.Error.Kind == ErrThrown && strings.Contains(done.Error.Message, p.oversized) {
			// The script let the answer that was too large for it end the run: that is the data's doing, as a
			// task too large for the start is (anyone who can write a task the owner can read can inflate it).
			done.Error.Kind = ErrInputTooLarge
		}
		return done, false, nil
	case ctx.Err() != nil:
		return nil, false, ctx.Err()
	case p.inputTooLarge:
		return fail(&Error{Kind: ErrInputTooLarge, Message: fmt.Sprintf("the task or the event is too large or too deeply nested to be passed to a script (more than %d MiB or %d levels)", inputLimit(in.Limits)>>20, maxWireDepth)})
	case !p.started:
		// The process was gone before it got the script.
		return nil, mayRetry, &UnavailableError{Msg: "the script process ended before it got the script: " + fmt.Sprint(pumpErr)}
	case p.violation != nil:
		return fail(p.violation)
	case wasKilled || killCtx.Err() != nil:
		return fail(timeoutError(in))
	case errors.Is(pumpErr, errMessageTooLarge):
		return fail(&Error{Kind: ErrThrown, Message: fmt.Sprintf("the script passed more than %d MiB to the server at once", maxMessageBytes>>20)})
	case p.stale:
		log.Errorf("automation: the script process %d sent a message of its previous script (%v); it was killed", c.pid, p.broken)
		return nil, mayRetry, &UnavailableError{Msg: "the script process broke the protocol: " + p.broken.Error()}
	case p.broken != nil:
		log.Errorf("automation: the script process %d broke the protocol (%v); it was killed: %s", c.pid, p.broken, c.stderr.logLine())
		return fail(&Error{Kind: ErrInternal, Message: "the script process broke the protocol: " + p.broken.Error()})
	}
	waitErr := c.wait()
	stderr := c.stderr.String()
	if mayRetry && !answered && stderr == "" && !c.killedByOthers() {
		// A pooled process that ended silently: it may have died while it waited, before it read the script.
		// Nothing of the script was answered, so a fresh process runs it; if that one dies the same way, the
		// script is blamed.
		return nil, true, &UnavailableError{Msg: "the script process ended unexpectedly: " + fmt.Sprint(waitErr)}
	}
	return exitOutcome(in, c, p, waitErr, pumpErr, stderr, mayRetry)
}

func timeoutError(in *Input) *Error {
	return &Error{Kind: ErrTimeout, Message: fmt.Sprintf("the script ran longer than %s and was stopped", in.Limits.Timeout)}
}

// exitOutcome explains a child that ended without reporting, after it got the script (see runIn for the blame).
func exitOutcome(in *Input, c *child, p *parent, waitErr, pumpErr error, stderr string, pooled bool) (*doneMessage, bool, error) {
	fail := func(e *Error) (*doneMessage, bool, error) { return &doneMessage{Error: e}, false, nil }
	low := strings.ToLower(stderr)
	state := c.cmd.ProcessState
	detail := fmt.Sprint(waitErr)
	if pumpErr != nil && !errors.Is(pumpErr, io.EOF) {
		detail += ", " + pumpErr.Error()
	}
	switch {
	case state != nil && state.ExitCode() == exitOverdue:
		return fail(timeoutError(in))
	case strings.Contains(low, "out of memory"), strings.Contains(low, "cannot allocate memory"),
		strings.Contains(low, "failed to create new os thread"), strings.Contains(low, "pthread_create failed"):
		if pooled {
			// A pooled process gives about the room of a new one, not exactly (the heap of the previous script
			// may be fragmented): only a new process decides that the script itself is too large.
			if p.external {
				return fail(&Error{Kind: ErrUnattributed, Message: memoryError(in).Message + " (in a reused script process; not retried after a request)"})
			}
			return nil, true, &UnavailableError{Msg: "the reused script process ran out of memory"}
		}
		return fail(memoryError(in))
	case strings.Contains(low, "stack overflow"), strings.Contains(low, "stack exceeds"):
		return fail(&Error{Kind: ErrStackOverflow, Message: "the script nested too deeply for the interpreter and was stopped"})
	case c.killedByOthers():
		log.Warningf("automation: the script process %d was killed by a signal the server did not send (the kernel's OOM killer?)", c.pid)
		const msg = "the script process was killed by the operating system (most likely memory ran out on the server)"
		if p.external {
			// Running it again would repeat its requests.
			return fail(&Error{Kind: ErrUnattributed, Message: msg})
		}
		return nil, false, &UnavailableError{Msg: msg}
	}
	log.Errorf("automation: the script process %d ended unexpectedly (%s): %s", c.pid, detail, c.stderr.logLine())
	return fail(&Error{Kind: ErrInternal, Message: "the script process ended unexpectedly: " + detail})
}

func memoryError(in *Input) *Error {
	if in.Limits.MaxMemory > 0 {
		return &Error{Kind: ErrMemory, Message: fmt.Sprintf("the script hit the memory limit of %d MiB and was stopped", in.Limits.MaxMemory>>20)}
	}
	return &Error{Kind: ErrMemory, Message: "the script ran out of memory and was stopped"}
}

// parent is the side of the run in this process: it sends the input and answers the host calls.
type parent struct {
	ctx context.Context
	in  *Input
	w   io.Writer
	seq uint64
	// check only compiles the source.
	check bool
	// violation is a limit of the run the child did not enforce; the run fails with it.
	violation *Error
	// broken is a message that does not belong to the protocol.
	broken error
	// started is set once the start message was sent, inputTooLarge when it could not be because of its size.
	started       bool
	inputTooLarge bool
	// external is set once a host call with an effect outside the transaction (http.fetch) was made.
	external bool
	// oversized is the message of an answer that was too large for the script, if there was one.
	oversized string
	// stale is set when the first thing the process sent belongs to its previous script.
	stale  bool
	calls  int
	api    int
	fetchs int
}

// mismatch records a message of another run. Left over from the previous script of a pooled process before this
// script made any call, it is that one's doing and this script runs again in a fresh process; anything else is a
// broken protocol.
func (p *parent) mismatch(seq uint64, what string) {
	err := fmt.Errorf("%s of run %d in run %d", what, seq, p.seq)
	if seq < p.seq && p.calls == 0 {
		p.stale = true
	}
	p.broken = err
}

// pump runs the conversation until the child reports its outcome or the pipe breaks. answered says whether the
// child sent anything.
func (p *parent) pump(r *bufio.Reader, budget time.Duration) (done *doneMessage, answered bool, err error) {
	start := &startMessage{
		Seq:       p.seq,
		Source:    p.in.Source,
		Limits:    p.in.Limits,
		Budget:    budget,
		Condition: p.in.Condition,
		Check:     p.check,
	}
	if err := wireInput(p.in, start, inputLimit(p.in.Limits)); err != nil {
		p.inputTooLarge = true
		return nil, false, err
	}
	if err := writeMessage(p.w, &message{Start: start}); err != nil {
		// Nothing was written when the start itself cannot be encoded: it is the input's, not the process's.
		p.inputTooLarge = errors.Is(err, errUnencodable)
		return nil, false, err
	}
	p.started = true
	for {
		m, err := readMessage(r, maxMessageBytes, false)
		if err != nil {
			if errors.Is(err, errInvalidMessage) {
				p.broken = err
			}
			return nil, answered, err
		}
		answered = true
		switch {
		case m.Done != nil:
			if m.Done.Seq != p.seq {
				p.mismatch(m.Done.Seq, "done")
				return nil, true, nil
			}
			if m.Done.Error == nil && m.Done.Result == nil && !p.check {
				p.broken = errors.New("the script process reported nothing")
				return nil, true, nil
			}
			return m.Done, true, nil
		case m.Call != nil:
			if m.Call.Seq != p.seq {
				p.mismatch(m.Call.Seq, "call")
				return nil, true, nil
			}
			var reply *replyMessage
			p.calls++
			if p.ctx.Err() != nil {
				// No new host call after the deadline: the kill follows.
				reply = failed(errors.New("the script ran out of time"))
			} else {
				if p.violation = p.enforce(m.Call); p.violation != nil {
					return nil, true, nil //nolint:nilerr // the violation is the outcome, kept on p
				}
				if m.Call.Method == "Fetch" {
					p.external = true
				}
				var panicked string
				reply, panicked = p.safeAnswer(m.Call)
				if panicked != "" {
					// The child waits for an answer it will not get: it is not reused.
					return &doneMessage{Seq: p.seq, Error: &Error{Kind: ErrInternal, Message: panicked}}, true, nil
				}
			}
			if err := writeMessage(p.w, &message{Reply: reply}); err != nil {
				return nil, true, err
			}
		default:
			p.broken = errors.New("unexpected message from the script process")
			return nil, true, nil
		}
	}
}

// enforce applies the limits of the run to a host call again: the child counts and checks them before it sends
// a call, so a call beyond them means the child cannot be trusted. The run fails as the script's fault.
func (p *parent) enforce(c *callMessage) *Error {
	l := p.in.Limits
	if c.Method == "Fetch" {
		p.fetchs++
		if l.MaxHTTPRequests > 0 && p.fetchs > l.MaxHTTPRequests {
			return &Error{Kind: ErrThrown, Message: fmt.Sprintf("the script made more than %d http.fetch requests", l.MaxHTTPRequests)}
		}
	} else if c.Method != "SecretGet" {
		p.api++
		if l.MaxAPICalls > 0 && p.api > l.MaxAPICalls {
			return &Error{Kind: ErrThrown, Message: fmt.Sprintf("the script made more than %d vikunja.* calls", l.MaxAPICalls)}
		}
	}
	tooLarge := &Error{Kind: ErrThrown, Message: "the script passed a value to the host beyond the limits"}
	b := &bounds{maxNodes: l.MaxExportNodes, maxBytes: l.MaxValueBytes}
	if !b.text(c.Text) || !b.tree(c.Data, 0) || !b.tree(c.Value, 0) {
		return tooLarge
	}
	if f := c.Fetch; f != nil {
		if !b.text(f.URL) || !b.text(f.Body) || !b.text(f.Method) {
			return tooLarge
		}
		for k, v := range f.Headers {
			b.nodes++
			if !b.text(k) || !b.text(v) || (b.maxNodes > 0 && b.nodes > b.maxNodes) {
				return tooLarge
			}
		}
	}
	return nil
}

// bounds checks values against the limits the exporter of the child applies.
type bounds struct {
	maxNodes int
	maxBytes int
	nodes    int
}

func (b *bounds) text(s string) bool { return b.maxBytes <= 0 || len(s) <= b.maxBytes }

func (b *bounds) tree(v any, depth int) bool {
	b.nodes++
	if (b.maxNodes > 0 && b.nodes > b.maxNodes) || depth > maxExportDepth+1 {
		return false
	}
	switch val := v.(type) {
	case nil, bool, float64:
		return true
	case string:
		return b.text(val)
	case []any:
		for _, item := range val {
			if !b.tree(item, depth+1) {
				return false
			}
		}
		return true
	case map[string]any:
		for _, item := range val {
			if !b.tree(item, depth+1) {
				return false
			}
		}
		return true
	}
	return false
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
		limit := inputLimit(p.in.Limits)
		w, specials, _, err := wireValueLimited(v, limit-(4<<10))
		if err != nil {
			p.oversized = fmt.Sprintf("the answer of the server is larger than %d MiB and cannot be passed to the script", limit>>20)
			return failed(errors.New(p.oversized))
		}
		return &replyMessage{Value: w, Specials: specials}
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
	if resp != nil {
		size := jsonLen(resp.Body) + 64
		for k, v := range resp.Headers {
			size += jsonLen(k) + jsonLen(v) + 2
		}
		if limit := inputLimit(p.in.Limits); size > limit {
			p.oversized = fmt.Sprintf("the response is larger than %d MiB and cannot be passed to the script", limit>>20)
			return failed(errors.New(p.oversized))
		}
	}
	return &replyMessage{Fetch: resp}
}

// cappedBuffer keeps the first max bytes written to it since the last reset.
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

func (b *cappedBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = b.buf[:0]
}

// logLine is the start of the output as one quoted line, for the log.
func (b *cappedBuffer) logLine() string {
	s := b.String()
	if len(s) > maxStderrLogBytes {
		s = s[:maxStderrLogBytes] + cutMarker
	}
	return strconv.Quote(s)
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
