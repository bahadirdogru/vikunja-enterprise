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
	"unicode/utf8"
)

// The parent and the script process talk in JSON, one message per line: the child sends hello, the parent sends
// start, the child sends a call for every host call and waits for the reply, and finally sends done. Every call
// and the done carry the sequence number of their start.

// protocolVersion changes with every change of the messages. A child of another build is refused (see hello).
const protocolVersion = 2

const (
	// maxMessageBytes bounds one message of the script process, checked before it is buffered. The child keeps its
	// messages below it (results, errors and logs are cut off, a host call that would be larger throws), so a
	// larger one means the child is not ours to trust and it is killed.
	maxMessageBytes = 4 << 20
	// maxInputBytes bounds one message of the server to the child (the start with the task, the reply of a host
	// call), estimated before it is encoded so the server never builds a larger one: in-process the script got the
	// server's own values for free, here every value is copied into JSON. 8 MiB is twice the limit of the other
	// direction and holds a find of 200 tasks with 40 KiB descriptions; a fetch body (maxresponsesize) is allowed
	// six times its size in escapes.
	maxInputBytes = 8 << 20
	// maxReplyBytes bounds what the child reads.
	maxReplyBytes = 64 << 20
	// maxResultBytes is how much of the return value, the error message and the stack is kept. The runner keeps
	// 16 KiB of the output of a step anyway.
	maxResultBytes = 16 << 10
	// maxDoneLogBytes caps the log in the done message, so the message stays below maxMessageBytes even when
	// every byte has to be escaped (six bytes in JSON).
	maxDoneLogBytes = 256 << 10
)

type message struct {
	Hello *helloMessage `json:"hello,omitempty"`
	Start *startMessage `json:"start,omitempty"`
	Call  *callMessage  `json:"call,omitempty"`
	Reply *replyMessage `json:"reply,omitempty"`
	Done  *doneMessage  `json:"done,omitempty"`
}

// helloMessage is the first line of a script process: a binary of another build (an upgrade replaced the file)
// or one without the script process (it would start a server) never gets a script.
type helloMessage struct {
	Protocol int    `json:"protocol"`
	Build    string `json:"build"`
	PID      int    `json:"pid"`
}

type startMessage struct {
	Seq      uint64         `json:"seq"`
	Source   string         `json:"source"`
	Event    map[string]any `json:"event"`
	Task     map[string]any `json:"task"`
	Project  map[string]any `json:"project"`
	Specials []special      `json:"specials,omitempty"`
	Limits   Limits         `json:"limits"`
	// Budget is what is left of the timeout when the script starts: waiting for the process counted against it.
	Budget    time.Duration `json:"budget"`
	Condition bool          `json:"condition"`
	// Check only compiles the source and reports a syntax error.
	Check bool `json:"check,omitempty"`
}

// callMessage is one call of Host. Which fields are set depends on the method.
type callMessage struct {
	Seq    uint64         `json:"seq"`
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
	Value    any            `json:"value,omitempty"`
	Specials []special      `json:"specials,omitempty"`
	Text     string         `json:"text,omitempty"`
	Fetch    *FetchResponse `json:"fetch,omitempty"`
	// Error is the message of the error of the host, which the script sees as a thrown Error.
	Error  string `json:"error,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}

type doneMessage struct {
	Seq    uint64       `json:"seq"`
	Result *Result      `json:"result,omitempty"`
	Error  *Error       `json:"error,omitempty"`
	Syntax *SyntaxError `json:"syntax,omitempty"`
	// Spare is what the next script in this process could allocate before it hits the memory limit, measured
	// after the interpreter was dropped and the memory returned; -1 when unknown. The parent pools the process
	// only when it is close to the limit itself.
	Spare int64 `json:"spare"`
	// PID is the process, for the tests of the pool.
	PID int `json:"pid"`
}

var (
	errMessageTooLarge = errors.New("message too large")
	errInvalidMessage  = errors.New("invalid message")
)

// readMessage reads one line of at most limit bytes and decodes it. A longer line is refused before more than
// limit bytes of it are held.
func readMessage(r *bufio.Reader, limit int, useNumber bool) (*message, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(line)+len(chunk) > limit {
			return nil, errMessageTooLarge
		}
		if err == nil && line == nil {
			// The whole line is in the buffer of the reader: decode it from there, without a copy.
			line = chunk
			break
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
	m := &message{}
	var err error
	if useNumber {
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		err = dec.Decode(m)
	} else {
		err = json.Unmarshal(line, m)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidMessage, err)
	}
	return m, nil
}

// writeMessage encodes m as one line without HTML escapes (which turn every <, > and & into six bytes) and
// without a copy of the encoding.
func writeMessage(w io.Writer, m *message) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(m)
}

var errInputTooLarge = errors.New("too large")

// inputLimit is the largest message the server sends to the child for these limits.
func inputLimit(l Limits) int {
	return min(max(maxInputBytes, 6*l.MaxValueBytes+(1<<20)), maxReplyBytes-(64<<10))
}

// jsonLen is an upper bound of the length of s encoded as a JSON string without HTML escapes.
func jsonLen(s string) int {
	n := len(s) + 2
	valid := utf8.ValidString(s)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c < 0x20:
			n += 5
		case c == '"' || c == '\\':
			n++
		case c >= 0x80 && !valid:
			// Every byte of invalid UTF-8 may become \ufffd.
			n += 5
		case c == 0xE2 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xA8 || s[i+2] == 0xA9):
			// U+2028 and U+2029 are escaped: three bytes become six.
			n += 3
		}
	}
	return n
}

// special is a number JSON cannot hold (NaN, Infinity, -Infinity) at a path of a value: the value has null there
// and the receiver puts the number back. Path elements are map keys (strings) and list indices (numbers).
type special struct {
	Path  []any  `json:"path"`
	Value string `json:"value"`
}

// wireValueLimited turns a value of the host into a tree that survives JSON and gives the script exactly what
// toJS would have built from the original: it mirrors the cases of toJS. Numbers that are not finite become null
// in the tree and are listed in specials. It gives up (errInputTooLarge) as soon as the JSON of the value could be
// longer than limit bytes, before anything of that size was built.
func wireValueLimited(v any, limit int) (any, []special, int, error) {
	w := &wireWalker{limit: limit}
	out := w.walk(v)
	if w.over {
		return nil, nil, w.size, errInputTooLarge
	}
	return out, w.specials, w.size, nil
}

type wireWalker struct {
	path     []any
	specials []special
	// size is an upper bound of the JSON of what was walked; limit (if set) ends the walk.
	size  int
	limit int
	over  bool
}

func (w *wireWalker) add(n int) {
	w.size += n
	if w.limit > 0 && w.size > w.limit {
		w.over = true
	}
}

func (w *wireWalker) at(key any, v any) any {
	if k, ok := key.(string); ok {
		w.add(jsonLen(k) + 2)
	} else {
		w.add(1)
	}
	if w.over {
		return nil
	}
	w.path = append(w.path, key)
	out := w.walk(v)
	w.path = w.path[:len(w.path)-1]
	return out
}

func (w *wireWalker) walk(v any) any {
	if w.over {
		return nil
	}
	switch val := v.(type) {
	case nil, bool, int64, int, float64:
		w.add(24)
	case string:
		w.add(jsonLen(val))
	case time.Time:
		w.add(40)
	case []any, []int64, []string, []map[string]any, map[string]any:
		w.add(2)
	default:
		w.add(jsonLen(fmt.Sprint(v)))
	}
	if w.over {
		return nil
	}
	switch val := v.(type) {
	case nil:
		return nil
	case string, bool, int64:
		return val
	case int:
		return int64(val)
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			w.specials = append(w.specials, special{Path: append([]any{}, w.path...), Value: fmt.Sprint(val)})
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
			out[i] = w.at(i, item)
		}
		return out
	case []int64:
		out := make([]any, len(val))
		for i, item := range val {
			w.add(24)
			out[i] = item
		}
		return out
	case []string:
		out := make([]any, len(val))
		for i, item := range val {
			w.add(jsonLen(item) + 1)
			out[i] = item
		}
		return out
	case []map[string]any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = w.at(i, item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = w.at(k, item)
		}
		return out
	}
	return fmt.Sprint(v)
}

// wireInput is wireValue of the three maps of a start message; the specials are rooted at "event", "task" and
// "project". It fails with errInputTooLarge when the message could be longer than limit.
func wireInput(in *Input, start *startMessage, limit int) error {
	budget := limit - jsonLen(start.Source) - (4 << 10)
	for _, part := range []struct {
		name string
		src  map[string]any
		dst  *map[string]any
	}{
		{"event", in.Event, &start.Event},
		{"task", in.Task, &start.Task},
		{"project", in.Project, &start.Project},
	} {
		if part.src == nil {
			continue
		}
		v, specials, size, err := wireValueLimited(part.src, budget)
		if err != nil {
			return err
		}
		budget -= size
		*part.dst, _ = v.(map[string]any)
		for _, s := range specials {
			s.Path = append([]any{part.name}, s.Path...)
			start.Specials = append(start.Specials, s)
		}
	}
	return nil
}

// fromWire turns a tree decoded with UseNumber back into what toJS expects: integers become int64 (as the host
// sent them), other numbers float64, and -0 stays -0.
func fromWire(v any) any {
	switch val := v.(type) {
	case json.Number:
		if val == "-0" {
			return math.Copysign(0, -1)
		}
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

// applySpecials puts the numbers of specials back into the tree root (decoded with UseNumber). A path that does
// not lead anywhere is ignored.
func applySpecials(root any, specials []special) any {
	for _, s := range specials {
		var f float64
		switch s.Value {
		case "NaN":
			f = math.NaN()
		case "+Inf":
			f = math.Inf(1)
		case "-Inf":
			f = math.Inf(-1)
		default:
			continue
		}
		if len(s.Path) == 0 {
			root = f
			continue
		}
		setAt(root, s.Path, f)
	}
	return root
}

func setAt(node any, path []any, f float64) {
	last := len(path) == 1
	switch n := node.(type) {
	case map[string]any:
		k, ok := path[0].(string)
		if !ok {
			return
		}
		if last {
			if _, has := n[k]; has {
				n[k] = f
			}
			return
		}
		setAt(n[k], path[1:], f)
	case []any:
		num, ok := path[0].(json.Number)
		if !ok {
			return
		}
		i, err := num.Int64()
		if err != nil || i < 0 || i >= int64(len(n)) {
			return
		}
		if last {
			n[i] = f
			return
		}
		setAt(n[i], path[1:], f)
	}
}
