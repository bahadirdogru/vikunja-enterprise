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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// The parent and the script process talk in JSON, one message per line: the parent sends start, the child sends a
// call for every host call and waits for the reply, and finally sends done.

// maxMessageBytes bounds one message of the script process. A larger one is the script's fault (it passed a huge
// value to the host or returned one) and ends the run; the parent never holds more than this of a script's output.
const maxMessageBytes = 32 << 20

type message struct {
	Start *startMessage `json:"start,omitempty"`
	Call  *callMessage  `json:"call,omitempty"`
	Reply *replyMessage `json:"reply,omitempty"`
	Done  *doneMessage  `json:"done,omitempty"`
}

type startMessage struct {
	Source    string         `json:"source"`
	Event     map[string]any `json:"event"`
	Task      map[string]any `json:"task"`
	Project   map[string]any `json:"project"`
	Limits    Limits         `json:"limits"`
	Condition bool           `json:"condition"`
	// Check only compiles the source and reports a syntax error.
	Check bool `json:"check,omitempty"`
}

// callMessage is one call of Host. Which fields are set depends on the method.
type callMessage struct {
	Method string         `json:"method"`
	ID     int64          `json:"id,omitempty"`
	ID2    int64          `json:"id2,omitempty"`
	Text   string         `json:"text,omitempty"`
	Limit  int            `json:"limit,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
	Value  any            `json:"value,omitempty"`
	Fetch  *FetchRequest  `json:"fetch,omitempty"`
}

type replyMessage struct {
	Value any            `json:"value,omitempty"`
	Text  string         `json:"text,omitempty"`
	Fetch *FetchResponse `json:"fetch,omitempty"`
	// Error is the message of the error of the host, which the script sees as a thrown Error.
	Error  string `json:"error,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}

type doneMessage struct {
	Result *Result      `json:"result,omitempty"`
	Error  *Error       `json:"error,omitempty"`
	Syntax *SyntaxError `json:"syntax,omitempty"`
}

var errMessageTooLarge = errors.New("message too large")

// readMessage reads one line of at most limit bytes and decodes it.
func readMessage(r *bufio.Reader, limit int, useNumber bool) (*message, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(line)+len(chunk) > limit {
			return nil, errMessageTooLarge
		}
		line = append(line, chunk...)
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if errors.Is(err, io.EOF) && len(line) == 0 {
				return nil, io.EOF
			}
			return nil, err
		}
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	if useNumber {
		dec.UseNumber()
	}
	m := &message{}
	if err := dec.Decode(m); err != nil {
		return nil, fmt.Errorf("invalid message: %w", err)
	}
	return m, nil
}

func writeMessage(w io.Writer, m *message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// wireValue turns a value of the host into a tree that survives JSON and gives the script exactly what toJS
// would have built from the original: it mirrors the cases of toJS. Numbers that are not finite have no JSON form
// and become null.
func wireValue(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case string, bool, int64:
		return val
	case int:
		return int64(val)
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return nil
		}
		return val
	case time.Time:
		if val.IsZero() {
			return nil
		}
		return val.UTC().Format(time.RFC3339)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = wireValue(item)
		}
		return out
	case []int64:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = item
		}
		return out
	case []string:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = item
		}
		return out
	case []map[string]any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = wireValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = wireValue(item)
		}
		return out
	}
	return fmt.Sprint(v)
}

func wireMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := wireValue(m).(map[string]any)
	return out
}

// fromWire turns a tree decoded with UseNumber back into what toJS expects: integers become int64 (as the host
// sent them), other numbers float64.
func fromWire(v any) any {
	switch val := v.(type) {
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return i
		}
		f, _ := val.Float64()
		return f
	case []any:
		for i, item := range val {
			val[i] = fromWire(item)
		}
		return val
	case map[string]any:
		for k, item := range val {
			val[k] = fromWire(item)
		}
		return val
	}
	return v
}

func fromWireMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	return fromWire(m).(map[string]any)
}
