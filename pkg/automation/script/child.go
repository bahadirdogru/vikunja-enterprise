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
	"math"
	"os"
	"runtime/debug"

	"github.com/dlclark/regexp2/v2"
)

// childEnv marks the script process. The parent starts its own executable with it set (see isolate.go); every
// binary that imports this package then turns into the script process here, before main or TestMain run.
const (
	childEnv      = "VIKUNJA_AUTOMATION_SCRIPT_PROCESS"
	childEnvValue = "1"
)

// childMaxStack is the Go stack limit of the script process. The interpreter has its own call stack limit; this
// only bounds the recursion of native code (the parser, deeply nested values).
const childMaxStack = 256 << 20

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
	in := bufio.NewReaderSize(stdin, 64<<10)
	out := bufio.NewWriterSize(stdout, 64<<10)
	debug.SetMaxStack(childMaxStack)
	memoryLimited := false
	for {
		m, err := readMessage(in, math.MaxInt, true)
		if errors.Is(err, io.EOF) {
			return 0
		}
		if err != nil || m.Start == nil {
			fmt.Fprintf(os.Stderr, "script process: no start message: %v\n", err)
			return 3
		}
		start := m.Start

		if !memoryLimited && start.Limits.MaxMemory > 0 {
			// The parent only hands this process scripts with the same memory limit. The soft limit makes the
			// garbage collector work harder before the hard limit of the operating system ends the process.
			debug.SetMemoryLimit(start.Limits.MaxMemory)
			if err := limitMemory(start.Limits.MaxMemory); err != nil {
				fmt.Fprintf(os.Stderr, "script process: cannot limit memory: %v\n", err)
				return 3
			}
			memoryLimited = true
		}
		limitCPU(start.Limits.Timeout)
		if start.Limits.Timeout > 0 {
			// One script at a time: the process-wide match timeout of the regexp engine can be the script's.
			regexp2.DefaultMatchTimeout = start.Limits.Timeout
		}

		done, ok := childRun(start, in, out)
		if !ok {
			// The parent is gone or sent something unreadable; nobody is listening.
			return 4
		}
		if err := writeMessage(out, &message{Done: done}); err != nil {
			return 4
		}
		if err := out.Flush(); err != nil {
			return 4
		}
	}
}

func childRun(start *startMessage, in *bufio.Reader, out *bufio.Writer) (*doneMessage, bool) {
	if start.Check {
		_, synErr := compile(start.Source)
		return &doneMessage{Syntax: synErr}, true
	}
	host := &proxyHost{in: in, out: out}
	input := &Input{
		Source:    start.Source,
		Event:     fromWireMap(start.Event),
		Task:      fromWireMap(start.Task),
		Project:   fromWireMap(start.Project),
		Host:      host,
		Limits:    start.Limits,
		Condition: start.Condition,
	}
	res, err := runLocal(context.Background(), input)
	if host.broken != nil {
		return nil, false
	}
	if err != nil {
		var se *Error
		if !errors.As(err, &se) {
			se = &Error{Kind: ErrInternal, Message: err.Error()}
		}
		return &doneMessage{Error: se}, true
	}
	return &doneMessage{Result: res}, true
}

// proxyHost forwards the host calls of the script to the parent.
type proxyHost struct {
	in     *bufio.Reader
	out    *bufio.Writer
	broken error
}

func (h *proxyHost) call(c *callMessage) (*replyMessage, error) {
	if h.broken != nil {
		return nil, h.broken
	}
	b, err := json.Marshal(&message{Call: c})
	if err != nil {
		// Nothing was sent: a number that is not finite has no JSON form.
		return nil, fmt.Errorf("the value cannot be passed to the host: %w", err)
	}
	if _, err = h.out.Write(append(b, '\n')); err == nil {
		err = h.out.Flush()
	}
	if err != nil {
		h.broken = errors.New("the connection to the server was lost")
		return nil, h.broken
	}
	m, err := readMessage(h.in, math.MaxInt, true)
	if err != nil || m.Reply == nil {
		h.broken = errors.New("the connection to the server was lost")
		return nil, h.broken
	}
	if m.Reply.Failed {
		return m.Reply, errors.New(m.Reply.Error)
	}
	return m.Reply, nil
}

func (h *proxyHost) TaskGet(_ context.Context, id int64) (map[string]any, error) {
	r, err := h.call(&callMessage{Method: "TaskGet", ID: id})
	if err != nil {
		return nil, err
	}
	m, _ := fromWire(r.Value).(map[string]any)
	return m, nil
}

func (h *proxyHost) TaskFind(_ context.Context, filter string, projectID int64, limit int) ([]map[string]any, error) {
	r, err := h.call(&callMessage{Method: "TaskFind", Text: filter, ID: projectID, Limit: limit})
	if err != nil {
		return nil, err
	}
	list, _ := fromWire(r.Value).([]any)
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
	m, _ := fromWire(r.Value).(map[string]any)
	return m, nil
}

func (h *proxyHost) TaskCreate(_ context.Context, projectID int64, data map[string]any) (map[string]any, error) {
	r, err := h.call(&callMessage{Method: "TaskCreate", ID: projectID, Data: data})
	if err != nil {
		return nil, err
	}
	m, _ := fromWire(r.Value).(map[string]any)
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
	return fromWire(r.Value), nil
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
