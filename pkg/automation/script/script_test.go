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
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	config.InitDefaultConfig()
	m.Run()
}

// fakeHost records calls and answers with canned data.
type fakeHost struct {
	mu    sync.Mutex
	calls []string
	tasks map[int64]map[string]any
	fail  error
	fetch func(req *FetchRequest) (*FetchResponse, error)
}

func (h *fakeHost) record(format string, args ...any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, fmt.Sprintf(format, args...))
	return h.fail
}

func (h *fakeHost) TaskGet(_ context.Context, id int64) (map[string]any, error) {
	if err := h.record("get %d", id); err != nil {
		return nil, err
	}
	if t, ok := h.tasks[id]; ok {
		return t, nil
	}
	return nil, errors.New("task not found")
}

func (h *fakeHost) TaskFind(_ context.Context, filter string, projectID int64, limit int) ([]map[string]any, error) {
	if err := h.record("find %s %d %d", filter, projectID, limit); err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for i := 0; i < limit+50; i++ {
		out = append(out, map[string]any{"id": int64(i + 1)})
	}
	return out, nil
}

func (h *fakeHost) TaskUpdate(_ context.Context, id int64, patch map[string]any) (map[string]any, error) {
	return map[string]any{"id": id}, h.record("update %d %v", id, patch)
}

func (h *fakeHost) TaskCreate(_ context.Context, projectID int64, data map[string]any) (map[string]any, error) {
	return map[string]any{"id": int64(99)}, h.record("create %d %v", projectID, data)
}

func (h *fakeHost) TaskComment(_ context.Context, id int64, text string) error {
	return h.record("comment %d %s", id, text)
}

func (h *fakeHost) FieldGet(_ context.Context, taskID, fieldID int64) (any, error) {
	return 42.0, h.record("field.get %d %d", taskID, fieldID)
}

func (h *fakeHost) FieldSet(_ context.Context, taskID, fieldID int64, value any) error {
	return h.record("field.set %d %d %v", taskID, fieldID, value)
}

func (h *fakeHost) StatusSet(_ context.Context, taskID, statusID int64, comment string) error {
	return h.record("status.set %d %d %s", taskID, statusID, comment)
}

func (h *fakeHost) AssigneeAdd(_ context.Context, taskID, userID int64) error {
	return h.record("assignee.add %d %d", taskID, userID)
}

func (h *fakeHost) AssigneeRemove(_ context.Context, taskID, userID int64) error {
	return h.record("assignee.remove %d %d", taskID, userID)
}

func (h *fakeHost) LabelAdd(_ context.Context, taskID, labelID int64) error {
	return h.record("label.add %d %d", taskID, labelID)
}

func (h *fakeHost) LabelRemove(_ context.Context, taskID, labelID int64) error {
	return h.record("label.remove %d %d", taskID, labelID)
}

func (h *fakeHost) Fetch(_ context.Context, req *FetchRequest) (*FetchResponse, error) {
	if err := h.record("fetch %s %s", req.Method, req.URL); err != nil {
		return nil, err
	}
	if h.fetch != nil {
		return h.fetch(req)
	}
	return &FetchResponse{Status: 200, Headers: map[string]string{"content-type": "application/json"}, Body: `{"ok":true,"n":3}`}, nil
}

func (h *fakeHost) SecretGet(_ context.Context, name string) (string, error) {
	if name == "MISSING" {
		return "", errors.New("the secret does not exist")
	}
	return "s3cret-" + name, h.record("secret %s", name)
}

func run(t *testing.T, h *fakeHost, source string, mutate ...func(*Input)) (*Result, error) {
	t.Helper()
	if h == nil {
		h = &fakeHost{tasks: map[int64]map[string]any{1: {"id": int64(1), "title": "One"}}}
	}
	in := &Input{
		Source:  source,
		Event:   map[string]any{"name": "task.updated", "doer": map[string]any{"id": int64(1), "username": "u"}},
		Task:    map[string]any{"id": int64(1), "title": "T", "custom_fields": map[string]any{"3": 2.0}},
		Project: map[string]any{"id": int64(1), "title": "P"},
		Host:    h,
		Limits:  ActionLimits(),
	}
	for _, m := range mutate {
		m(in)
	}
	return Run(context.Background(), in)
}

func mustRun(t *testing.T, source string) *Result {
	t.Helper()
	res, err := run(t, nil, source)
	require.NoError(t, err)
	return res
}

func TestRun_ReturnValueAndGlobals(t *testing.T) {
	res := mustRun(t, `return {event: event.name, doer: event.doer.username, task: task.title, project: project.title, cf: task.custom_fields["3"]}`)
	assert.JSONEq(t, `{"event":"task.updated","doer":"u","task":"T","project":"P","cf":2}`, res.Value)
	assert.True(t, res.Returned)

	res = mustRun(t, `var x = 1;`)
	assert.False(t, res.Returned, "no return, no result")
	assert.Empty(t, res.Value)

	t.Run("task is null when there is none", func(t *testing.T) {
		res, err := run(t, nil, `return task === null`, func(in *Input) { in.Task = nil })
		require.NoError(t, err)
		assert.Equal(t, "true", res.Value)
	})

	t.Run("host values become plain JavaScript values", func(t *testing.T) {
		h := &fakeHost{tasks: map[int64]map[string]any{1: {
			"id": int64(1), "title": "One", "due_date": time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC),
			"assignees": []any{1.0, 2.0}, "custom_fields": map[string]any{"5": []any{3.0}}, "status": nil,
		}}}
		res, err := run(t, h, `const t = vikunja.tasks.get(1); return [t.due_date, t.assignees.length, Array.isArray(t.assignees), t.custom_fields["5"][0], t.status, typeof t.id]`)
		require.NoError(t, err)
		assert.JSONEq(t, `["2026-12-01T10:00:00Z", 2, true, 3, null, "number"]`, res.Value)
	})
}

func TestSandbox_NothingBeyondTheDocumentedGlobals(t *testing.T) {
	res := mustRun(t, `
		var names = Object.getOwnPropertyNames(globalThis);
		var bad = ["require", "process", "module", "exports", "Buffer", "setTimeout", "setInterval", "setImmediate", "clearTimeout",
			"fetch", "XMLHttpRequest", "WebSocket", "WebAssembly", "window", "global", "self", "document", "navigator", "performance",
			"os", "fs", "exec", "importScripts", "structuredClone", "queueMicrotask"].filter(function (n) { return names.indexOf(n) !== -1 || typeof globalThis[n] !== "undefined"; });
		return {bad: bad, globals: names.filter(function (n) { return ["event","task","project","vikunja","http","secrets","log","console"].indexOf(n) !== -1 }).sort()}`)
	assert.JSONEq(t, `{"bad":[],"globals":["console","event","http","log","project","secrets","task","vikunja"]}`, res.Value)

	t.Run("console has only log", func(t *testing.T) {
		res := mustRun(t, `return Object.keys(console).concat([typeof console.error, typeof console.warn, typeof console.info])`)
		assert.JSONEq(t, `["log","undefined","undefined","undefined"]`, res.Value)
	})
}

func TestSandbox_ConstructorEscapesYieldOnlyTheVMGlobals(t *testing.T) {
	for name, src := range map[string]string{
		"function constructor":       `return typeof globalThis.constructor.constructor("return process")()`,
		"function constructor this":  `return typeof (function(){}).constructor("return this")().process`,
		"this in a function":         `return typeof (function(){ return this; })()`,
		"indirect eval":              `return typeof (0, eval)("typeof process === 'undefined' ? undefined : process")`,
		"require via eval":           `return typeof eval("typeof require")`,
		"constructor chain":          `return [].constructor.constructor("return typeof require")()`,
		"Reflect and Proxy":          `return typeof Reflect.get(globalThis, "process")`,
		"prototype of host function": `return typeof Object.getPrototypeOf(vikunja.tasks.get).constructor("return typeof process")()`,
	} {
		t.Run(name, func(t *testing.T) {
			res, err := run(t, nil, src)
			if err != nil {
				// Throwing is fine too; what must not happen is getting hold of something.
				return
			}
			assert.Contains(t, []string{`"undefined"`, `"string"`, `"object"`, `"function"`}, res.Value)
			assert.NotContains(t, res.Value, "process")
		})
	}

	t.Run("the global object of an escape has no process or require", func(t *testing.T) {
		res := mustRun(t, `var g = (function(){}).constructor("return this")(); return [typeof g.process, typeof g.require, typeof g.setTimeout, g.event === event]`)
		assert.JSONEq(t, `["undefined","undefined","undefined",true]`, res.Value)
	})
}

func TestSandbox_NoGoObjectsAreReachable(t *testing.T) {
	h := &fakeHost{tasks: map[int64]map[string]any{1: {"id": int64(1), "title": "One", "assignees": []any{1.0}}}}
	res, err := run(t, h, `
		var out = {};
		out.protoEvent = Object.getPrototypeOf(event) === Object.prototype;
		out.protoTask = Object.getPrototypeOf(task) === Object.prototype;
		var t = vikunja.tasks.get(1);
		out.protoGot = Object.getPrototypeOf(t) === Object.prototype;
		out.arrayGot = Object.getPrototypeOf(t.assignees) === Array.prototype;
		out.keysEvent = Object.keys(event).sort().join();
		out.vikunja = Object.keys(vikunja).sort().join();
		out.tasks = Object.keys(vikunja.tasks).sort().join();
		out.fnType = typeof vikunja.tasks.get;
		out.fnCtor = vikunja.tasks.get.constructor === Function;
		out.fnProps = Object.getOwnPropertyNames(vikunja.tasks.get).sort().join();
		out.symbols = Object.getOwnPropertySymbols(vikunja.tasks).length;
		try { vikunja.tasks.get(999) } catch (e) {
			out.errIsError = e instanceof Error;
			out.errProps = Object.getOwnPropertyNames(e).sort().join();
			out.errValue = typeof e.value;
		}
		out.secretsFn = typeof secrets.get;
		out.httpKeys = Object.keys(http).join();
		return out`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"protoEvent":true,"protoTask":true,"protoGot":true,"arrayGot":true,
		"keysEvent":"doer,name",
		"vikunja":"assignees,fields,labels,status,tasks","tasks":"comment,create,find,get,update",
		"fnType":"function","fnCtor":true,"fnProps":"length,name","symbols":0,
		"errIsError":true,"errProps":"message,stack","errValue":"undefined","secretsFn":"function","httpKeys":"fetch"}`, res.Value)
}

func TestSandbox_EventAndTaskAreFrozenAndMutationDoesNotReachTheApp(t *testing.T) {
	for name, src := range map[string]string{
		"event field":       `event.name = "x"`,
		"event nested":      `event.doer.id = 99`,
		"task field":        `task.title = "x"`,
		"task custom field": `task.custom_fields["3"] = 5`,
		"project":           `project.title = "x"`,
		"adding a key":      `task.extra = 1`,
		"vikunja replaced":  `vikunja.tasks = {}`,
		"global replaced":   `event = {}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := run(t, nil, src)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Equal(t, ErrThrown, se.Kind)
			assert.Contains(t, se.Message, "TypeError")
		})
	}

	t.Run("a task returned by the host is a copy", func(t *testing.T) {
		stored := map[string]any{"id": int64(1), "title": "One", "assignees": []any{1.0}}
		h := &fakeHost{tasks: map[int64]map[string]any{1: stored}}
		_, err := run(t, h, `var t = vikunja.tasks.get(1); t.title = "changed"; t.assignees.push(9); t.x = 1;`)
		require.NoError(t, err)
		assert.Equal(t, "One", stored["title"])
		assert.Equal(t, []any{1.0}, stored["assignees"])
		assert.NotContains(t, stored, "x")
	})

	t.Run("two runs share nothing", func(t *testing.T) {
		mustRun(t, `globalThis.leak = 42; Object.prototype.polluted = true; Array.prototype.polluted = true;`)
		res := mustRun(t, `return [typeof globalThis.leak, typeof ({}).polluted, typeof [].polluted]`)
		assert.JSONEq(t, `["undefined","undefined","undefined"]`, res.Value)
	})
}

func TestLimits_Timeout(t *testing.T) {
	for name, src := range map[string]string{
		"infinite loop":              `while (true) {}`,
		"loop that catches":          `try { while (true) {} } catch (e) { return "caught" }`,
		"loop in a finally":          `try { return 1 } finally { for (;;) {} }`,
		"endless recursion of calls": `function f() { return [1].map(f) } f()`,
	} {
		t.Run(name, func(t *testing.T) {
			started := time.Now()
			_, err := run(t, nil, src, func(in *Input) { in.Limits.Timeout = 150 * time.Millisecond })
			elapsed := time.Since(started)
			var se *Error
			require.ErrorAs(t, err, &se)
			assert.Contains(t, []ErrorKind{ErrTimeout, ErrStackOverflow}, se.Kind)
			assert.Less(t, elapsed, 2*time.Second, "returns promptly")
		})
	}

	t.Run("a blocking host call is released by the context", func(t *testing.T) {
		h := &fakeHost{fetch: func(*FetchRequest) (*FetchResponse, error) { return nil, nil }}
		blocked := &blockingHost{fakeHost: h}
		started := time.Now()
		_, err := run(t, nil, `http.fetch("https://example.com/")`, func(in *Input) {
			in.Host = blocked
			in.Limits.Timeout = 100 * time.Millisecond
		})
		require.Error(t, err)
		assert.Less(t, time.Since(started), 2*time.Second)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	})

	t.Run("a cancelled parent context is not the fault of the script", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		in := &Input{Source: `while (true) {}`, Host: &fakeHost{}, Limits: ActionLimits()}
		_, err := Run(ctx, in)
		require.ErrorIs(t, err, context.Canceled)
		var se *Error
		assert.False(t, errors.As(err, &se))
	})

	t.Run("a catastrophic regular expression is stopped by the regexp engine", func(t *testing.T) {
		started := time.Now()
		_, err := run(t, nil, `return /(a+)+$/.test("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab")`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
		assert.Less(t, time.Since(started), 10*time.Second)
	})
}

// blockingHost blocks in Fetch until the context ends.
type blockingHost struct{ *fakeHost }

func (h *blockingHost) Fetch(ctx context.Context, _ *FetchRequest) (*FetchResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestLimits_CallStack(t *testing.T) {
	_, err := run(t, nil, "function f(n) { return f(n + 1) + 1 }\nf(0)", func(in *Input) { in.Limits.MaxCallStack = 100 })
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrStackOverflow, se.Kind)
	assert.Contains(t, se.Message, "100")
	assert.Contains(t, se.Stack, "line 1, column")

	res, err := run(t, nil, "function f(n) { return n === 0 ? 0 : 1 + f(n - 1) }\nreturn f(50)", func(in *Input) { in.Limits.MaxCallStack = 100 })
	require.NoError(t, err)
	assert.Equal(t, "50", res.Value)
}

func TestLimits_Log(t *testing.T) {
	res := mustRun(t, `log("hello", {a: 1}, [1, 2], null, undefined, 3, true); console.log("again")`)
	assert.Equal(t, "hello {\"a\":1} [1,2] null undefined 3 true\nagain", res.Log)

	t.Run("total output is truncated with a marker", func(t *testing.T) {
		res, err := run(t, nil, `for (var i = 0; i < 1000; i++) { log("line " + i + " " + "x".repeat(100)) }`, func(in *Input) { in.Limits.MaxLogBytes = 1000 })
		require.NoError(t, err)
		assert.True(t, strings.HasSuffix(res.Log, "[log output truncated]"))
		assert.LessOrEqual(t, len(res.Log), 1000+len("\n[log output truncated]"))
		assert.Equal(t, 1, strings.Count(res.Log, "[log output truncated]"))
	})

	t.Run("one huge argument is cut", func(t *testing.T) {
		res := mustRun(t, `log("y".repeat(1000000))`)
		assert.LessOrEqual(t, len(res.Log), maxLogArgBytes+3)
	})

	t.Run("an object that cannot be stringified does not break log", func(t *testing.T) {
		res := mustRun(t, `var o = {}; o.self = o; log(o); log("after")`)
		assert.Contains(t, res.Log, "after")
	})
}

func TestLimits_APICalls(t *testing.T) {
	h := &fakeHost{tasks: map[int64]map[string]any{1: {"id": int64(1)}}}
	_, err := run(t, h, `for (var i = 0; i < 10; i++) { vikunja.tasks.get(1) }`, func(in *Input) { in.Limits.MaxAPICalls = 3 })
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Contains(t, se.Message, "more than 3 vikunja.* calls")
	assert.Len(t, h.calls, 3, "the fourth call never reached the host")

	t.Run("the error can be caught but the limit stays", func(t *testing.T) {
		h := &fakeHost{tasks: map[int64]map[string]any{1: {"id": int64(1)}}}
		res, err := run(t, h, `var n = 0; for (var i = 0; i < 10; i++) { try { vikunja.tasks.get(1); n++ } catch (e) {} } return n`, func(in *Input) { in.Limits.MaxAPICalls = 4 })
		require.NoError(t, err)
		assert.Equal(t, "4", res.Value)
		assert.Equal(t, 10, res.APICalls)
	})

	t.Run("fields, secrets and fetch count separately", func(t *testing.T) {
		h := &fakeHost{}
		res, err := run(t, h, `secrets.get("A"); secrets.get("B"); http.fetch("https://example.com/a"); log(1)`, func(in *Input) { in.Limits.MaxAPICalls = 1 })
		require.NoError(t, err)
		assert.Equal(t, 0, res.APICalls)
		assert.Equal(t, 1, res.HTTPRequests)
	})
}

func TestLimits_HTTPRequests(t *testing.T) {
	h := &fakeHost{}
	_, err := run(t, h, `for (var i = 0; i < 10; i++) { http.fetch("https://example.com/" + i) }`, func(in *Input) { in.Limits.MaxHTTPRequests = 2 })
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Contains(t, se.Message, "more than 2 http.fetch requests")
	assert.Len(t, h.calls, 2)
}

func TestLimits_ValuesCrossingTheBoundaryAreBounded(t *testing.T) {
	t.Run("find returns at most the limit", func(t *testing.T) {
		res, err := run(t, nil, `return vikunja.tasks.find("done = false").length`, func(in *Input) { in.Limits.MaxResults = 7 })
		require.NoError(t, err)
		assert.Equal(t, "7", res.Value, "a host that returns more is cut")
	})
	t.Run("a huge string argument is refused", func(t *testing.T) {
		_, err := run(t, nil, `vikunja.tasks.comment(1, "z".repeat(5000))`, func(in *Input) { in.Limits.MaxValueBytes = 1000 })
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Contains(t, se.Message, "longer than 1000 bytes")
	})
	t.Run("a huge nested value is refused", func(t *testing.T) {
		_, err := run(t, nil, `var a = []; for (var i = 0; i < 300; i++) a.push({x: [1, 2, 3]}); vikunja.fields.set(1, 2, a)`, func(in *Input) { in.Limits.MaxExportNodes = 100 })
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Contains(t, se.Message, "too large")
	})
	t.Run("a value nested too deeply is refused", func(t *testing.T) {
		_, err := run(t, nil, `var o = {}; var c = o; for (var i = 0; i < 100; i++) { c.n = {}; c = c.n } vikunja.fields.set(1, 2, o)`)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Contains(t, se.Message, "nested too deeply")
	})
	t.Run("a function is not data", func(t *testing.T) {
		_, err := run(t, nil, `vikunja.fields.set(1, 2, function () {})`)
		require.Error(t, err)
	})
}

func TestRun_Errors(t *testing.T) {
	t.Run("an uncaught error has the stack with line and column of the source", func(t *testing.T) {
		_, err := run(t, nil, "var a = 1;\n\nfunction boom() {\n    throw new TypeError('bad thing');\n}\nboom();")
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrThrown, se.Kind)
		assert.Equal(t, "TypeError: bad thing", se.Message)
		assert.Contains(t, se.Stack, "at boom (line 4, column 11)")
		assert.Contains(t, se.Stack, "line 6, column 5")
		assert.NotContains(t, se.Stack, "line 7", "the wrapper is not part of the stack")
	})

	t.Run("a first line column does not include the wrapper", func(t *testing.T) {
		_, err := run(t, nil, "var a = 1; throw new Error('x')")
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Contains(t, se.Stack, "line 1, column 18")
	})

	t.Run("host errors are catchable Errors with the message of the host", func(t *testing.T) {
		h := &fakeHost{fail: errors.New("Forbidden")}
		res, err := run(t, h, `try { vikunja.tasks.update(1, {title: "x"}); return "no" } catch (e) { return [e instanceof Error, e.message] }`)
		require.NoError(t, err)
		assert.JSONEq(t, `[true, "Forbidden"]`, res.Value)

		_, err = run(t, h, `vikunja.tasks.update(1, {title: "x"})`)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, "Error: Forbidden", se.Message)
		assert.Contains(t, se.Stack, "line 1")
	})

	t.Run("syntax errors have line and column", func(t *testing.T) {
		synErr := Check("var a = 1;\nvar b = ;")
		require.NotNil(t, synErr)
		assert.Equal(t, 2, synErr.Line)
		assert.Equal(t, 9, synErr.Column)
		assert.NotEmpty(t, synErr.Message)

		synErr = Check("return 1 +")
		require.NotNil(t, synErr)
		assert.Equal(t, 1, synErr.Line)

		assert.Nil(t, Check("return 1"), "return is fine in the implicit function")
		assert.Nil(t, Check("// only a comment"))
	})

	t.Run("a source that tries to close the wrapper does not escape it", func(t *testing.T) {
		assert.NotNil(t, Check("}); process.exit(); (function(){"))
		assert.NotNil(t, Check("})()\n(function(){"))
	})

	t.Run("bad arguments are TypeErrors-like errors", func(t *testing.T) {
		for _, src := range []string{
			`vikunja.tasks.get()`, `vikunja.tasks.get(0)`, `vikunja.tasks.get(-3)`, `vikunja.tasks.get(1.5)`, `vikunja.tasks.get("1")`,
			`vikunja.tasks.get(NaN)`, `vikunja.tasks.update(1)`, `vikunja.tasks.update(1, "x")`, `vikunja.tasks.comment(1, 5)`,
			`secrets.get(1)`, `http.fetch()`, `http.fetch("https://x", {headers: {a: 1}})`, `http.fetch("https://x", {body: {}})`,
		} {
			_, err := run(t, nil, src)
			require.Error(t, err, src)
		}
	})
}

func TestRun_Condition(t *testing.T) {
	cond := func(src string) (*Result, error) {
		return run(t, nil, src, func(in *Input) { in.Condition = true; in.Limits = ConditionLimits() })
	}
	res, err := cond(`return task.custom_fields["3"] > 1`)
	require.NoError(t, err)
	assert.True(t, res.Truthy)

	res, err = cond(`return false`)
	require.NoError(t, err)
	assert.False(t, res.Truthy)

	for src, truthy := range map[string]bool{`return 0`: false, `return ""`: false, `return null`: false, `return 1`: true, `return "x"`: true, `return []`: true} {
		res, err = cond(src)
		require.NoError(t, err, src)
		assert.Equal(t, truthy, res.Truthy, src)
	}

	t.Run("nothing returned fails clearly", func(t *testing.T) {
		for _, src := range []string{`task.priority > 3`, `var x = true;`, `if (true) { true }`, `return`} {
			_, err := cond(src)
			var se *Error
			require.ErrorAs(t, err, &se, src)
			assert.Equal(t, ErrBadResult, se.Kind)
			assert.Contains(t, se.Message, "return")
		}
	})

	t.Run("throws and timeouts", func(t *testing.T) {
		_, err := cond(`throw new Error("nope")`)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrThrown, se.Kind)

		started := time.Now()
		_, err = run(t, nil, `while (true) {}`, func(in *Input) {
			in.Condition = true
			in.Limits = ConditionLimits()
			in.Limits.Timeout = 50 * time.Millisecond
		})
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrTimeout, se.Kind)
		assert.Less(t, time.Since(started), time.Second)
	})
}

func TestHostCalls(t *testing.T) {
	h := &fakeHost{tasks: map[int64]map[string]any{1: {"id": int64(1)}}}
	date := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	_, err := run(t, h, `
		vikunja.tasks.update(1, {title: "T2", priority: 3, due_date: new Date(Date.UTC(2026, 4, 1, 12))});
		vikunja.tasks.create(2, {title: "child", parent_id: 1, assignees: [1, 2]});
		vikunja.tasks.comment(1, "hi");
		vikunja.tasks.find("done = false", {project_id: 4});
		vikunja.fields.get(1, 7);
		vikunja.fields.set(1, 7, [1, 2]);
		vikunja.status.set(1, 3, "why");
		vikunja.status.set(1, 3);
		vikunja.assignees.add(1, 5); vikunja.assignees.remove(1, 5);
		vikunja.labels.add(1, 6); vikunja.labels.remove(1, 6);
		`)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"update 1 map[due_date:" + date.Format(time.RFC3339) + " priority:3 title:T2]",
		"create 2 map[assignees:[1 2] parent_id:1 title:child]",
		"comment 1 hi",
		"find done = false 4 200",
		"field.get 1 7",
		"field.set 1 7 [1 2]",
		"status.set 1 3 why",
		"status.set 1 3 ",
		"assignee.add 1 5", "assignee.remove 1 5",
		"label.add 1 6", "label.remove 1 6",
	}, h.calls)
}

func TestFetch(t *testing.T) {
	h := &fakeHost{}
	h.fetch = func(req *FetchRequest) (*FetchResponse, error) {
		return &FetchResponse{Status: 201, Headers: map[string]string{"x-a": "b"}, Body: `{"id": 7, "list": [1, 2]}`}, nil
	}
	res, err := run(t, h, `
		var r = http.fetch("https://example.com/x", {method: "post", headers: {"X-T": "1"}, body: JSON.stringify({a: 1}), timeout: 2500});
		return [r.status, r.ok, r.headers["x-a"], r.json().id, r.json().list.length, r.truncated, typeof r.body]`)
	require.NoError(t, err)
	assert.JSONEq(t, `[201, true, "b", 7, 2, false, "string"]`, res.Value)

	t.Run("the request reaches the host as plain data", func(t *testing.T) {
		var got *FetchRequest
		h.fetch = func(req *FetchRequest) (*FetchResponse, error) {
			got = req
			return &FetchResponse{Status: 200, Body: "x"}, nil
		}
		_, err := run(t, h, `http.fetch("https://example.com/", {method: "put", headers: {A: "1"}, body: "b", timeout: 1500})`)
		require.NoError(t, err)
		assert.Equal(t, &FetchRequest{URL: "https://example.com/", Method: "PUT", Headers: map[string]string{"A": "1"}, Body: "b", TimeoutMs: 1500}, got)
	})

	t.Run("json() on a body that is not JSON throws a catchable error", func(t *testing.T) {
		h.fetch = func(req *FetchRequest) (*FetchResponse, error) {
			return &FetchResponse{Status: 200, Body: "<html>"}, nil
		}
		res, err := run(t, h, `try { http.fetch("https://example.com/").json() } catch (e) { return e.name }`)
		require.NoError(t, err)
		assert.Equal(t, `"SyntaxError"`, res.Value)
	})

	t.Run("json() on a truncated body refuses", func(t *testing.T) {
		h.fetch = func(req *FetchRequest) (*FetchResponse, error) {
			return &FetchResponse{Status: 200, Body: `{"a"`, Truncated: true}, nil
		}
		res, err := run(t, h, `try { http.fetch("https://example.com/").json() } catch (e) { return e.message }`)
		require.NoError(t, err)
		assert.Contains(t, res.Value, "cut off")
	})

	t.Run("a failing request throws and can be caught", func(t *testing.T) {
		h.fetch = func(req *FetchRequest) (*FetchResponse, error) { return nil, errors.New("the address is not allowed") }
		res, err := run(t, h, `try { http.fetch("http://127.0.0.1/") } catch (e) { return e.message }`)
		require.NoError(t, err)
		assert.Equal(t, `"the address is not allowed"`, res.Value)
	})
}

func TestRun_Async(t *testing.T) {
	// Promises exist in the interpreter but there is no event loop and no timer: nothing outside the run can
	// resume a script later.
	res, err := run(t, nil, `var out = []; Promise.resolve(1).then(function (v) { out.push(v) }); return typeof setTimeout`)
	require.NoError(t, err)
	assert.Equal(t, `"undefined"`, res.Value)
}
