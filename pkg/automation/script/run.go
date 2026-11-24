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

// Package script runs the JavaScript of automation rules in an embedded, pure Go interpreter (goja).
//
// Every run gets a fresh interpreter in a child process (isolate.go), which is killed at the deadline and runs
// under a memory limit; the host calls of the script are answered by the parent over a pipe.
// The only globals are the standard ECMAScript built-ins plus event, task, project, vikunja, http, secrets, log and
// console.log. Everything handed to the script is built as plain JavaScript objects; no Go value with methods or pointers is ever exposed. Everything
// the script can do beyond computing goes through the Host interface, which the caller implements.
package script

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2/v2"
	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/file"
	"github.com/dop251/goja/parser"
)

func init() {
	// The interpreter cannot interrupt a regular expression that backtracks catastrophically (it is native code),
	// so the regexp engine itself gets a limit. Only the interpreter uses this package of regexp2. The script
	// process lowers it to the timeout of its script.
	regexp2.DefaultMatchTimeout = 2 * time.Second
}

const (
	// wrapperPrefix starts the implicit function the source is the body of. It stays on the first line of the
	// source so line numbers are the ones of the source; only columns of line 1 shift by len(wrapperPrefix).
	wrapperPrefix = `(function(){"use strict";`
	wrapperSuffix = "\n})()"
	scriptName    = "script"
	// maxLogArgBytes caps one argument of log().
	maxLogArgBytes = 4096
	logTruncated   = "\n[log output truncated]"
)

// SyntaxError is a script that does not compile.
type SyntaxError struct {
	Message string
	Line    int
	Column  int
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

// isWrapperCall reports whether the program is exactly one call of one function literal.
func isWrapperCall(tree *ast.Program) bool {
	if len(tree.Body) != 1 {
		return false
	}
	stmt, ok := tree.Body[0].(*ast.ExpressionStatement)
	if !ok {
		return false
	}
	call, ok := stmt.Expression.(*ast.CallExpression)
	if !ok || len(call.ArgumentList) != 0 {
		return false
	}
	_, ok = call.Callee.(*ast.FunctionLiteral)
	return ok
}

func compile(source string) (*goja.Program, *SyntaxError) {
	wrapped := wrapperPrefix + source + wrapperSuffix
	lines := strings.Count(source, "\n") + 1
	// Parse first: the source has to be exactly the body of the function. A source that closes the wrapper and
	// opens another one is valid JavaScript but would run outside the function, so the program must be one
	// statement.
	tree, err := parser.ParseFile(nil, scriptName, wrapped, 0)
	if err == nil && !isWrapperCall(tree) {
		return nil, &SyntaxError{Message: "unbalanced braces: the script closes the function it is the body of", Line: 1, Column: 1}
	}
	var prg *goja.Program
	if err == nil {
		prg, err = goja.CompileAST(tree, false)
		if err == nil {
			return prg, nil
		}
	}
	synErr := &SyntaxError{Message: err.Error(), Line: 1, Column: 1}
	var list parser.ErrorList
	var cse *goja.CompilerSyntaxError
	var file *file.File
	var offset int
	switch {
	case errors.As(err, &list) && len(list) > 0:
		synErr.Message, file, offset = list[0].Message, list[0].File, list[0].Offset
	case errors.As(err, &cse):
		synErr.Message, file, offset = cse.Message, cse.File, cse.Offset
	}
	if file != nil {
		pos := file.Position(offset)
		synErr.Line, synErr.Column = pos.Line, pos.Column
	}
	if synErr.Line == 1 {
		synErr.Column -= len(wrapperPrefix)
	}
	if synErr.Line > lines {
		// The error is at the end of the source (an unfinished expression): point at its last character.
		synErr.Line = lines
		synErr.Column = len([]rune(source[strings.LastIndex(source, "\n")+1:])) + 1
	}
	if synErr.Column < 1 {
		synErr.Column = 1
	}
	return nil, synErr
}

// ErrorKind says why a script failed.
type ErrorKind string

const (
	// ErrThrown is an uncaught exception, including errors of host calls the script did not catch.
	ErrThrown ErrorKind = "thrown"
	// ErrTimeout is the wall-clock limit.
	ErrTimeout ErrorKind = "timeout"
	// ErrStackOverflow is the call stack limit.
	ErrStackOverflow ErrorKind = "stack_overflow"
	// ErrMemory is the memory limit of the script process.
	ErrMemory ErrorKind = "memory"
	// ErrBadResult is a condition script that did not return a value.
	ErrBadResult ErrorKind = "bad_result"
	// ErrInternal is a bug.
	ErrInternal ErrorKind = "internal"
)

// Error is a failed script. It is a failure of the script, not of the infrastructure: running it again fails the
// same way.
type Error struct {
	Kind    ErrorKind
	Message string
	// Stack is the JavaScript stack with line and column of the source.
	Stack string
}

func (e *Error) Error() string {
	if e.Stack != "" {
		return e.Message + "\n" + e.Stack
	}
	return e.Message
}

// Input is one script run.
type Input struct {
	Source  string
	Event   map[string]any
	Task    map[string]any
	Project map[string]any
	Host    Host
	Limits  Limits
	// Condition requires the script to return a value and reports it as a boolean.
	Condition bool
	// Interactive is a dry run or a check: it shares the few script processes beyond the workers' and never
	// takes one of theirs.
	Interactive bool

	// deadline replaces the timeout in the script process, where part of the timeout may be gone.
	deadline time.Time
}

// Result is what a finished script produced.
type Result struct {
	// Value is the return value as JSON ("" when the script returned undefined).
	Value string
	// Truthy is the return value converted to a boolean.
	Truthy bool
	// Returned is false when the script returned undefined.
	Returned bool
	// Log is the output of log().
	Log          string
	APICalls     int
	HTTPRequests int
	Duration     time.Duration
}

type runtime struct {
	vm       *goja.Runtime
	ctx      context.Context
	in       *Input
	log      strings.Builder
	logFull  bool
	api      int
	requests int
	exp      *exporter
	jsonStr  goja.Callable
	jsonPars goja.Callable
}

// runLocal executes the script in this process. Only the script process (see isolate.go) calls it: a single call
// of native code of the interpreter never sees the interrupt, so in this process a script can outlive its timeout
// and exhaust memory.
func runLocal(ctx context.Context, in *Input) (res *Result, err error) {
	started := time.Now()
	prg, synErr := compile(in.Source)
	if synErr != nil {
		return nil, &Error{Kind: ErrThrown, Message: "syntax error: " + synErr.Error()}
	}

	var runCtx context.Context
	var cancel context.CancelFunc
	if in.deadline.IsZero() {
		runCtx, cancel = context.WithTimeout(ctx, in.Limits.Timeout)
	} else {
		runCtx, cancel = context.WithDeadline(ctx, in.deadline)
	}
	defer cancel()

	vm := goja.New()
	if in.Limits.MaxCallStack > 0 {
		vm.SetMaxCallStackSize(in.Limits.MaxCallStack)
	}
	r := &runtime{
		vm:  vm,
		ctx: runCtx,
		in:  in,
		exp: &exporter{maxNodes: in.Limits.MaxExportNodes, maxBytes: in.Limits.MaxValueBytes},
	}

	defer func() {
		if p := recover(); p != nil {
			res, err = nil, &Error{Kind: ErrInternal, Message: fmt.Sprintf("internal error: %v", p)}
		}
	}()

	if err := r.install(); err != nil {
		return nil, &Error{Kind: ErrInternal, Message: err.Error()}
	}

	stop := context.AfterFunc(runCtx, func() { vm.Interrupt("timeout") })
	defer stop()

	value, runErr := vm.RunProgram(prg)
	if runErr != nil {
		return nil, r.failure(ctx, runCtx, runErr)
	}

	res = &Result{Log: r.log.String(), APICalls: r.api, HTTPRequests: r.requests, Duration: time.Since(started)}
	if value == nil || goja.IsUndefined(value) {
		if in.Condition {
			return nil, &Error{Kind: ErrBadResult, Message: "the condition script returned nothing: end it with `return true` or `return false` (a bare expression is not enough)"}
		}
		return res, nil
	}
	res.Returned = true
	res.Truthy = value.ToBoolean()
	res.Value = r.stringify(value)
	return res, nil
}

// failure converts what the interpreter reported into an error.
func (r *runtime) failure(parent, run context.Context, err error) error {
	var interrupted *goja.InterruptedError
	var overflow *goja.StackOverflowError
	var exc *goja.Exception
	switch {
	case errors.As(err, &interrupted):
		if parent.Err() != nil {
			return parent.Err()
		}
		if errors.Is(run.Err(), context.DeadlineExceeded) {
			return &Error{Kind: ErrTimeout, Message: fmt.Sprintf("the script ran longer than %s and was stopped", r.in.Limits.Timeout), Stack: cleanStack(interrupted.String(), r.sourceLines())}
		}
		return &Error{Kind: ErrTimeout, Message: "the script was stopped"}
	case errors.As(err, &overflow):
		return &Error{Kind: ErrStackOverflow, Message: fmt.Sprintf("the script exceeded the maximum call stack depth of %d", r.in.Limits.MaxCallStack), Stack: cleanStack(overflow.String(), r.sourceLines())}
	case errors.As(err, &exc):
		msg := "uncaught exception"
		if v := exc.Value(); v != nil {
			msg = v.String()
		}
		return &Error{Kind: ErrThrown, Message: msg, Stack: cleanStack(exc.String(), r.sourceLines())}
	}
	return &Error{Kind: ErrInternal, Message: err.Error()}
}

var stackPositionPattern = regexp.MustCompile(scriptName + `:(\d+):(\d+)\(\d+\)`)

// cleanStack keeps the frames of the script, with the line and column of the source: the interpreter names the
// compiled position as "script:line:col(pc)"; the program counter is dropped, the column of line 1 corrected and
// the frame of the wrapper itself left out.
func cleanStack(stack string, sourceLines int) string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(stack), "\n") {
		line = strings.TrimSpace(line)
		m := stackPositionPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		l, _ := strconv.Atoi(m[1])
		c, _ := strconv.Atoi(m[2])
		if l > sourceLines {
			continue
		}
		if l == 1 {
			c -= len(wrapperPrefix)
		}
		if c < 1 {
			c = 1
		}
		line = stackPositionPattern.ReplaceAllString(line, fmt.Sprintf("line %d, column %d", l, c))
		out = append(out, line)
		if len(out) >= 10 {
			break
		}
	}
	return strings.Join(out, "\n")
}

func (r *runtime) sourceLines() int { return strings.Count(r.in.Source, "\n") + 1 }

// stringify returns the JSON of a value, or its string form when it has none.
func (r *runtime) stringify(v goja.Value) (out string) {
	defer func() {
		if recover() != nil {
			out = "<unprintable>"
		}
	}()
	res, err := r.jsonStr(goja.Undefined(), v)
	if err != nil || res == nil || goja.IsUndefined(res) {
		return truncateLogArg(v.String())
	}
	return res.String()
}

func truncateLogArg(s string) string {
	if len(s) <= maxLogArgBytes {
		return s
	}
	return s[:maxLogArgBytes] + "..."
}

func (r *runtime) throw(format string, args ...any) {
	obj, err := r.vm.New(r.vm.Get("Error"), r.vm.ToValue(fmt.Sprintf(format, args...)))
	if err != nil {
		panic(r.vm.NewTypeError(fmt.Sprintf(format, args...)))
	}
	panic(obj)
}

func (r *runtime) throwErr(err error) {
	r.throw("%s", err.Error())
}

func (r *runtime) install() error {
	vm := r.vm
	jsonObj := vm.Get("JSON").ToObject(vm)
	var ok bool
	if r.jsonStr, ok = goja.AssertFunction(jsonObj.Get("stringify")); !ok {
		return errors.New("JSON.stringify is missing")
	}
	if r.jsonPars, ok = goja.AssertFunction(jsonObj.Get("parse")); !ok {
		return errors.New("JSON.parse is missing")
	}

	freeze, err := vm.RunString(`(function(root){
		var f = function(o){
			if (o === null || (typeof o !== 'object' && typeof o !== 'function') || Object.isFrozen(o)) { return o; }
			Object.freeze(o);
			Object.getOwnPropertyNames(o).forEach(function(k){ f(o[k]); });
			return o;
		};
		return f(root);
	})`)
	if err != nil {
		return err
	}
	deepFreeze, _ := goja.AssertFunction(freeze)

	define := func(name string, value goja.Value) {
		_ = vm.GlobalObject().DefineDataProperty(name, value, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE)
	}
	frozen := func(v goja.Value) goja.Value {
		out, err := deepFreeze(goja.Undefined(), v)
		if err != nil {
			return v
		}
		return out
	}

	define("event", frozen(toJS(vm, orEmpty(r.in.Event))))
	taskValue := goja.Null()
	if r.in.Task != nil {
		taskValue = frozen(toJS(vm, r.in.Task))
	}
	define("task", taskValue)
	define("project", frozen(toJS(vm, orEmpty(r.in.Project))))

	logFn := vm.ToValue(r.logCall)
	define("log", logFn)
	console := vm.NewObject()
	_ = console.Set("log", logFn)
	define("console", frozen(console))

	namespace := func(members map[string]goja.Value) *goja.Object {
		o := vm.NewObject()
		for k, v := range members {
			_ = o.Set(k, v)
		}
		return o
	}
	h := r.in.Host
	tasks := namespace(map[string]goja.Value{
		"get": r.apiFn(func(c goja.FunctionCall) goja.Value {
			t, err := h.TaskGet(r.ctx, r.id(c, 0, "id"))
			return r.result(toJS(vm, t), err)
		}),
		"find": r.apiFn(func(c goja.FunctionCall) goja.Value {
			filter := r.text(c, 0, "filter")
			var projectID int64
			if opt := c.Argument(1); !goja.IsUndefined(opt) && !goja.IsNull(opt) {
				m := r.object(opt, "options")
				if p, has := m["project_id"]; has && p != nil {
					projectID = r.idValue(p, "options.project_id")
				}
			}
			limit := r.in.Limits.MaxResults
			if limit <= 0 {
				limit = 200
			}
			list, err := h.TaskFind(r.ctx, filter, projectID, limit)
			if len(list) > limit {
				list = list[:limit]
			}
			return r.result(toJS(vm, list), err)
		}),
		"update": r.apiFn(func(c goja.FunctionCall) goja.Value {
			t, err := h.TaskUpdate(r.ctx, r.id(c, 0, "id"), r.objectArg(c, 1, "patch"))
			return r.result(toJS(vm, t), err)
		}),
		"create": r.apiFn(func(c goja.FunctionCall) goja.Value {
			t, err := h.TaskCreate(r.ctx, r.id(c, 0, "projectId"), r.objectArg(c, 1, "data"))
			return r.result(toJS(vm, t), err)
		}),
		"comment": r.apiFn(func(c goja.FunctionCall) goja.Value {
			return r.result(goja.Undefined(), h.TaskComment(r.ctx, r.id(c, 0, "id"), r.text(c, 1, "text")))
		}),
	})
	fields := namespace(map[string]goja.Value{
		"get": r.apiFn(func(c goja.FunctionCall) goja.Value {
			v, err := h.FieldGet(r.ctx, r.id(c, 0, "taskId"), r.id(c, 1, "fieldId"))
			return r.result(toJS(vm, v), err)
		}),
		"set": r.apiFn(func(c goja.FunctionCall) goja.Value {
			taskID, fieldID := r.id(c, 0, "taskId"), r.id(c, 1, "fieldId")
			value, err := r.exp.export(c.Argument(2))
			if err != nil {
				r.throwErr(err)
			}
			return r.result(goja.Undefined(), h.FieldSet(r.ctx, taskID, fieldID, value))
		}),
	})
	status := namespace(map[string]goja.Value{
		"set": r.apiFn(func(c goja.FunctionCall) goja.Value {
			comment := ""
			if a := c.Argument(2); !goja.IsUndefined(a) && !goja.IsNull(a) {
				comment = r.text(c, 2, "comment")
			}
			return r.result(goja.Undefined(), h.StatusSet(r.ctx, r.id(c, 0, "taskId"), r.id(c, 1, "statusId"), comment))
		}),
	})
	pair := func(fn func(ctx context.Context, a, b int64) error, second string) goja.Value {
		return r.apiFn(func(c goja.FunctionCall) goja.Value {
			return r.result(goja.Undefined(), fn(r.ctx, r.id(c, 0, "taskId"), r.id(c, 1, second)))
		})
	}
	assignees := namespace(map[string]goja.Value{
		"add":    pair(h.AssigneeAdd, "userId"),
		"remove": pair(h.AssigneeRemove, "userId"),
	})
	labels := namespace(map[string]goja.Value{
		"add":    pair(h.LabelAdd, "labelId"),
		"remove": pair(h.LabelRemove, "labelId"),
	})
	define("vikunja", frozen(namespace(map[string]goja.Value{
		"tasks": tasks, "fields": fields, "status": status, "assignees": assignees, "labels": labels,
	})))
	define("http", frozen(namespace(map[string]goja.Value{"fetch": vm.ToValue(r.fetch)})))
	define("secrets", frozen(namespace(map[string]goja.Value{
		"get": vm.ToValue(func(c goja.FunctionCall) goja.Value {
			v, err := h.SecretGet(r.ctx, r.text(c, 0, "name"))
			if err != nil {
				r.throwErr(err)
			}
			return vm.ToValue(v)
		}),
	})))
	return nil
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// apiFn wraps a vikunja.* function with the call counter.
func (r *runtime) apiFn(impl func(c goja.FunctionCall) goja.Value) goja.Value {
	return r.vm.ToValue(func(c goja.FunctionCall) goja.Value {
		r.api++
		if limit := r.in.Limits.MaxAPICalls; limit > 0 && r.api > limit {
			r.throw("the script made more than %d vikunja.* calls", limit)
		}
		return impl(c)
	})
}

func (r *runtime) result(v goja.Value, err error) goja.Value {
	if err != nil {
		r.throwErr(err)
	}
	return v
}

func (r *runtime) id(c goja.FunctionCall, i int, name string) int64 {
	return r.idValue(c.Argument(i).Export(), name)
}

func (r *runtime) idValue(v any, name string) int64 {
	var f float64
	switch n := v.(type) {
	case int64:
		f = float64(n)
	case float64:
		f = n
	default:
		r.throw("%s must be a positive integer", name)
	}
	if f != float64(int64(f)) || f < 1 || f > 1<<53 {
		r.throw("%s must be a positive integer", name)
	}
	return int64(f)
}

func (r *runtime) text(c goja.FunctionCall, i int, name string) string {
	v := c.Argument(i)
	s, ok := v.Export().(string)
	if !ok {
		r.throw("%s must be a string", name)
	}
	if r.in.Limits.MaxValueBytes > 0 && len(s) > r.in.Limits.MaxValueBytes {
		r.throw("%s is longer than %d bytes", name, r.in.Limits.MaxValueBytes)
	}
	return s
}

func (r *runtime) object(v goja.Value, name string) map[string]any {
	out, err := r.exp.export(v)
	if err != nil {
		r.throw("%s: %s", name, err.Error())
	}
	m, ok := out.(map[string]any)
	if !ok {
		r.throw("%s must be an object", name)
	}
	return m
}

func (r *runtime) objectArg(c goja.FunctionCall, i int, name string) map[string]any {
	return r.object(c.Argument(i), name)
}

// logCall implements log(...args).
func (r *runtime) logCall(c goja.FunctionCall) goja.Value {
	parts := make([]string, len(c.Arguments))
	for i, a := range c.Arguments {
		switch {
		case goja.IsUndefined(a):
			parts[i] = "undefined"
		case goja.IsNull(a):
			parts[i] = "null"
		default:
			if s, ok := a.Export().(string); ok {
				parts[i] = truncateLogArg(s)
			} else if _, isObj := a.(*goja.Object); isObj {
				parts[i] = truncateLogArg(r.stringify(a))
			} else {
				parts[i] = truncateLogArg(a.String())
			}
		}
	}
	if r.logFull {
		return goja.Undefined()
	}
	line := strings.Join(parts, " ")
	if r.log.Len() > 0 {
		line = "\n" + line
	}
	if limit := r.in.Limits.MaxLogBytes; limit > 0 && r.log.Len()+len(line) > limit {
		room := limit - r.log.Len()
		if room > 0 {
			r.log.WriteString(line[:room])
		}
		r.log.WriteString(logTruncated)
		r.logFull = true
		return goja.Undefined()
	}
	r.log.WriteString(line)
	return goja.Undefined()
}

// fetch implements http.fetch(url, options).
func (r *runtime) fetch(c goja.FunctionCall) goja.Value {
	r.requests++
	if limit := r.in.Limits.MaxHTTPRequests; limit > 0 && r.requests > limit {
		r.throw("the script made more than %d http.fetch requests", limit)
	}
	req := &FetchRequest{URL: r.text(c, 0, "url"), Method: "GET", Headers: map[string]string{}}
	if opt := c.Argument(1); !goja.IsUndefined(opt) && !goja.IsNull(opt) {
		m := r.object(opt, "options")
		if v, has := m["method"]; has && v != nil {
			s, ok := v.(string)
			if !ok {
				r.throw("options.method must be a string")
			}
			req.Method = strings.ToUpper(s)
		}
		if v, has := m["body"]; has && v != nil {
			s, ok := v.(string)
			if !ok {
				r.throw("options.body must be a string (use JSON.stringify for objects)")
			}
			req.Body = s
		}
		if v, has := m["timeout"]; has && v != nil {
			f, ok := v.(float64)
			if !ok || f < 0 {
				r.throw("options.timeout must be a number of milliseconds")
			}
			req.TimeoutMs = int64(f)
		}
		if v, has := m["headers"]; has && v != nil {
			headers, ok := v.(map[string]any)
			if !ok {
				r.throw("options.headers must be an object")
			}
			for k, hv := range headers {
				s, ok := hv.(string)
				if !ok {
					r.throw("options.headers.%s must be a string", k)
				}
				req.Headers[k] = s
			}
		}
	}
	resp, err := r.in.Host.Fetch(r.ctx, req)
	if err != nil {
		r.throwErr(err)
	}
	if resp == nil {
		r.throw("no response")
	}
	headers := map[string]any{}
	for k, v := range resp.Headers {
		headers[k] = v
	}
	obj := r.vm.NewObject()
	_ = obj.Set("status", resp.Status)
	_ = obj.Set("ok", resp.Status >= 200 && resp.Status < 300)
	_ = obj.Set("headers", toJS(r.vm, headers))
	_ = obj.Set("body", resp.Body)
	_ = obj.Set("truncated", resp.Truncated)
	_ = obj.Set("json", r.vm.ToValue(func(goja.FunctionCall) goja.Value {
		if resp.Truncated {
			r.throw("the response body was cut off and is not complete JSON")
		}
		v, err := r.jsonPars(goja.Undefined(), r.vm.ToValue(resp.Body))
		if err != nil {
			var exc *goja.Exception
			if errors.As(err, &exc) {
				panic(exc.Value())
			}
			r.throwErr(err)
		}
		return v
	}))
	return obj
}
