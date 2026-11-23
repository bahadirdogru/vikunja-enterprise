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
	"fmt"
	"sort"
	"time"

	"github.com/dop251/goja"
)

const maxExportDepth = 32

// toJS builds a plain JavaScript value from a tree of Go values. Objects and arrays are created inside the
// interpreter (not wrapped Go maps or slices), so a script can neither reach back into Go data through them nor
// change it. Anything that is not a basic type is turned into its string form.
func toJS(vm *goja.Runtime, v any) goja.Value {
	switch val := v.(type) {
	case nil:
		return goja.Null()
	case string:
		return vm.ToValue(val)
	case bool:
		return vm.ToValue(val)
	case int:
		return vm.ToValue(int64(val))
	case int64:
		return vm.ToValue(val)
	case float64:
		return vm.ToValue(val)
	case time.Time:
		if val.IsZero() {
			return goja.Null()
		}
		return vm.ToValue(val.UTC().Format(time.RFC3339))
	case []any:
		items := make([]any, len(val))
		for i, item := range val {
			items[i] = toJS(vm, item)
		}
		return vm.NewArray(items...)
	case []int64:
		items := make([]any, len(val))
		for i, item := range val {
			items[i] = vm.ToValue(item)
		}
		return vm.NewArray(items...)
	case []string:
		items := make([]any, len(val))
		for i, item := range val {
			items[i] = vm.ToValue(item)
		}
		return vm.NewArray(items...)
	case []map[string]any:
		items := make([]any, len(val))
		for i, item := range val {
			items[i] = toJS(vm, item)
		}
		return vm.NewArray(items...)
	case map[string]any:
		obj := vm.NewObject()
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_ = obj.Set(k, toJS(vm, val[k]))
		}
		return obj
	}
	return vm.ToValue(fmt.Sprint(v))
}

// exporter turns values coming out of the interpreter into plain Go trees (map[string]any, []any, string,
// float64, bool, nil), bounded in depth and size.
type exporter struct {
	maxNodes int
	maxBytes int
	nodes    int
}

func (e *exporter) export(v goja.Value) (any, error) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, nil
	}
	return e.normalize(v.Export(), 0)
}

func (e *exporter) normalize(v any, depth int) (any, error) {
	e.nodes++
	if e.nodes > e.maxNodes {
		return nil, fmt.Errorf("the value is too large")
	}
	if depth > maxExportDepth {
		return nil, fmt.Errorf("the value is nested too deeply")
	}
	switch val := v.(type) {
	case nil:
		return nil, nil
	case string:
		if e.maxBytes > 0 && len(val) > e.maxBytes {
			return nil, fmt.Errorf("a string is longer than %d bytes", e.maxBytes)
		}
		return val, nil
	case bool, float64:
		return val, nil
	case int64:
		return float64(val), nil
	case int:
		return float64(val), nil
	case time.Time:
		if val.IsZero() {
			return nil, nil
		}
		return val.UTC().Format(time.RFC3339), nil
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			n, err := e.normalize(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			n, err := e.normalize(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	}
	// Functions, symbols, Go values: not data.
	return nil, fmt.Errorf("a value of type %T cannot be passed to the host", v)
}
