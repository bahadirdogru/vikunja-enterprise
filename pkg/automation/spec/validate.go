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

package spec

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

const (
	maxConditionDepth = 8
	maxConditionNodes = 100
	maxStringLength   = 20000
	maxListLength     = 200
)

// ToInt64 converts a decoded JSON number to an integer.
func ToInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n != math.Trunc(n) || math.Abs(n) > 1<<53 {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// ToFloat64 converts a decoded JSON number to a float.
func ToFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

type validator struct {
	issues []Issue
	nodes  int
}

func (v *validator) add(path, format string, args ...any) {
	v.issues = append(v.issues, Issue{Path: path, Message: fmt.Sprintf(format, args...)})
}

// Validate checks the structure of a rule document against the catalog and returns all problems found, each with
// the path of the offending part. It does not check that ids exist.
func Validate(d *Doc, maxActions int) []Issue {
	v := &validator{}

	trigger := TriggerByType(d.Trigger.Type)
	switch {
	case d.Trigger.Type == "":
		v.add("trigger.type", "is required")
	case trigger == nil:
		v.add("trigger.type", "unknown trigger %q", d.Trigger.Type)
	default:
		v.params("trigger.options", trigger.Options, d.Trigger.Options)
	}

	if d.Conditions != nil {
		v.condition("conditions", d.Conditions, 1)
	}

	switch {
	case len(d.Actions) == 0:
		v.add("actions", "at least one action is required")
	case maxActions > 0 && len(d.Actions) > maxActions:
		v.add("actions", "a rule may have at most %d actions", maxActions)
	}
	for i, a := range d.Actions {
		path := fmt.Sprintf("actions[%d]", i)
		if a == nil {
			v.add(path, "is null")
			continue
		}
		def := ActionByType(a.Type)
		switch {
		case a.Type == "":
			v.add(path+".type", "is required")
		case def == nil:
			v.add(path+".type", "unknown action %q", a.Type)
		default:
			v.params(path+".params", def.Params, a.Params)
		}
	}
	return v.issues
}

func (v *validator) params(path string, defs []Param, values map[string]any) {
	known := map[string]Param{}
	for _, p := range defs {
		known[p.Name] = p
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if _, ok := known[name]; !ok {
			v.add(path+"."+name, "unknown parameter")
		}
	}
	for _, p := range defs {
		val, present := values[p.Name]
		if !present || val == nil {
			if p.Required {
				v.add(path+"."+p.Name, "is required")
			}
			continue
		}
		kind := p.Kind
		if p.KindBy != nil {
			sibling, _ := values[p.KindBy.Param].(string)
			k, ok := p.KindBy.Kinds[sibling]
			if !ok {
				// The sibling is missing or invalid; that is reported on its own.
				continue
			}
			kind = k
		}
		v.value(path+"."+p.Name, kind, p.Multiple, p.Enum, val, true)
	}
}

// value validates one value of a kind. keywords allows the user keywords when the kind is user.
func (v *validator) value(path, kind string, multiple bool, enum []string, val any, allowOffset bool) {
	if multiple {
		list, ok := val.([]any)
		if !ok {
			v.add(path, "must be a list")
			return
		}
		if len(list) == 0 || len(list) > maxListLength {
			v.add(path, "must have between 1 and %d entries", maxListLength)
			return
		}
		for i, item := range list {
			v.value(fmt.Sprintf("%s[%d]", path, i), kind, false, enum, item, allowOffset)
		}
		return
	}

	switch kind {
	case KindString, KindText, KindTemplate:
		s, ok := val.(string)
		switch {
		case !ok:
			v.add(path, "must be a string")
		case len(s) > maxStringLength:
			v.add(path, "must be at most %d characters long", maxStringLength)
		}
	case KindNumber:
		if _, ok := ToFloat64(val); !ok {
			v.add(path, "must be a number")
		}
	case KindBool:
		if _, ok := val.(bool); !ok {
			v.add(path, "must be true or false")
		}
	case KindDate:
		s, ok := val.(string)
		if !ok {
			v.add(path, "must be a date string")
			return
		}
		if _, err := ParseDateSpec(s, allowOffset); err != nil {
			v.add(path, "%s", err.Error())
		}
	case KindDuration:
		s, ok := val.(string)
		if !ok {
			v.add(path, "must be a duration string like 3d or 12h")
			return
		}
		if _, err := ParseOffset(s); err != nil {
			v.add(path, "%s", err.Error())
		}
	case KindUser, KindUsers:
		if s, ok := val.(string); ok {
			if !slices.Contains(enum, s) {
				v.add(path, "must be a user id or one of %s", strings.Join(enum, ", "))
			}
			return
		}
		if id, ok := ToInt64(val); !ok || id <= 0 {
			v.add(path, "must be a user id")
		}
	case KindLabel, KindStatus, KindCustomField, KindProject:
		if id, ok := ToInt64(val); !ok || id <= 0 {
			v.add(path, "must be an id (a positive integer)")
		}
	case KindEnum:
		s, ok := val.(string)
		if !ok || !slices.Contains(enum, s) {
			v.add(path, "must be one of %s", strings.Join(enum, ", "))
		}
	case KindCondition:
		c, ok := asCondition(val)
		if !ok {
			v.add(path, "must be a condition")
			return
		}
		v.condition(path, c, 1)
	case KindAny:
		switch list := val.(type) {
		case string, bool, float64, int, int64, json.Number:
		case []any:
			// Multiple choice and user fields take lists of ids.
			if len(list) > maxListLength {
				v.add(path, "must have at most %d entries", maxListLength)
			}
			for i, item := range list {
				switch item.(type) {
				case string, bool, float64, int, int64, json.Number:
				default:
					v.add(fmt.Sprintf("%s[%d]", path, i), "must be a string, number or boolean")
				}
			}
		default:
			v.add(path, "must be a string, number, boolean or a list of them")
		}
	default:
		v.add(path, "has the unknown kind %q", kind)
	}
}

// asCondition converts the decoded JSON of a condition parameter.
func asCondition(val any) (*Condition, bool) {
	if c, ok := val.(*Condition); ok {
		return c, c != nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, false
	}
	c := &Condition{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, false
	}
	return c, true
}

// ConditionFromParam decodes a condition kept in an action parameter.
func ConditionFromParam(val any) (*Condition, bool) {
	return asCondition(val)
}

func (v *validator) condition(path string, c *Condition, depth int) {
	v.nodes++
	if v.nodes > maxConditionNodes {
		if v.nodes == maxConditionNodes+1 {
			v.add(path, "the conditions have more than %d nodes", maxConditionNodes)
		}
		return
	}
	if depth > maxConditionDepth {
		v.add(path, "the conditions are nested deeper than %d levels", maxConditionDepth)
		return
	}

	forms := 0
	if len(c.All) > 0 {
		forms++
	}
	if len(c.Any) > 0 {
		forms++
	}
	if len(c.Not) > 0 {
		forms++
	}
	if c.IsLeaf() {
		forms++
	}
	if forms != 1 {
		v.add(path, "must have exactly one of all, any, not or field")
		return
	}

	switch {
	case len(c.All) > 0:
		v.children(path+".all", c.All, depth)
	case len(c.Any) > 0:
		v.children(path+".any", c.Any, depth)
	case len(c.Not) > 0:
		v.children(path+".not", c.Not, depth)
	default:
		v.leaf(path, c)
	}
}

func (v *validator) children(path string, children []*Condition, depth int) {
	for i, child := range children {
		childPath := fmt.Sprintf("%s[%d]", path, i)
		if child == nil {
			v.add(childPath, "is null")
			continue
		}
		v.condition(childPath, child, depth+1)
	}
}

// FieldKind resolves the kind of a condition field. For custom fields the kind is KindCustomField.
func FieldKind(name string) (def *FieldDef, ok bool) {
	if _, isCF := CustomFieldID(name); isCF {
		return &FieldDef{Name: name, Kind: KindCustomField, Operators: allOps}, true
	}
	def, ok = staticFields[name]
	return def, ok
}

func (v *validator) leaf(path string, c *Condition) {
	def, ok := FieldKind(c.Field)
	if !ok {
		v.add(path+".field", "unknown field %q", c.Field)
		return
	}
	if c.Op == "" {
		v.add(path+".op", "is required")
		return
	}
	if !slices.Contains(def.Operators, c.Op) {
		v.add(path+".op", "the operator %q cannot be used with the field %q", c.Op, c.Field)
		return
	}

	switch c.Op {
	case OpIsEmpty, OpIsNotEmpty:
		if c.Value != nil {
			v.add(path+".value", "must not be set for %s", c.Op)
		}
		return
	}
	if c.Value == nil {
		v.add(path+".value", "is required")
		return
	}

	kind := def.Kind
	enum := def.Enum
	switch kind {
	case KindCustomField:
		kind = KindAny
	case KindUsers:
		kind = KindUser
	case KindUser:
	}
	if kind == KindUser {
		enum = []string{UserEventDoer, UserTaskCreator}
	}

	if c.Op == OpIn || c.Op == OpNotIn {
		v.value(path+".value", kind, true, enum, c.Value, false)
		return
	}
	if c.Op == OpLike {
		if _, ok := c.Value.(string); !ok {
			v.add(path+".value", "must be a string")
		}
		return
	}
	v.value(path+".value", kind, false, enum, c.Value, false)
}
