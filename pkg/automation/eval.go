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

package automation

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
)

// Evaluate reports whether the condition tree holds for the subject. A nil condition holds. Comparisons are done in
// memory over the normalized snapshot; nothing is sent to the database.
func Evaluate(c *spec.Condition, sub *Subject) (bool, error) {
	if c == nil {
		return true, nil
	}
	switch {
	case len(c.All) > 0:
		for _, child := range c.All {
			ok, err := Evaluate(child, sub)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case len(c.Any) > 0:
		for _, child := range c.Any {
			ok, err := Evaluate(child, sub)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case len(c.Not) > 0:
		for _, child := range c.Not {
			ok, err := Evaluate(child, sub)
			if err != nil {
				return false, err
			}
			if ok {
				return false, nil
			}
		}
		return true, nil
	case c.IsLeaf():
		return evaluateLeaf(c, sub)
	}
	return false, fmt.Errorf("empty condition")
}

func evaluateLeaf(c *spec.Condition, sub *Subject) (bool, error) {
	if c.Script != "" {
		if sub.Script == nil {
			return false, fmt.Errorf("script conditions cannot be evaluated here")
		}
		return sub.Script(c.Script)
	}
	left, ok := sub.Lookup(c.Field)
	if !ok {
		return false, fmt.Errorf("unknown field %q", c.Field)
	}
	return compare(c.Op, left, sub.fieldKind(c.Field), c.Value, sub)
}

func isEmptyValue(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return val == ""
	case []any:
		return len(val) == 0
	}
	return false
}

// resolveOperand turns the keywords of user comparisons into ids and canonicalizes the value.
func resolveOperand(v any, sub *Subject) any {
	if s, ok := v.(string); ok {
		switch s {
		case spec.UserEventDoer:
			return float64(sub.DoerID())
		case spec.UserTaskCreator:
			return float64(sub.CreatedBy)
		}
	}
	if list, ok := v.([]any); ok {
		out := make([]any, len(list))
		for i, item := range list {
			out[i] = resolveOperand(item, sub)
		}
		return out
	}
	return canon(v)
}

func compare(op string, left any, kind string, rawRight any, sub *Subject) (bool, error) {
	switch op {
	case spec.OpIsEmpty:
		return isEmptyValue(left), nil
	case spec.OpIsNotEmpty:
		return !isEmptyValue(left), nil
	}

	// The keywords only exist for user comparisons; a title that happens to be "event_doer" stays a string.
	right := canon(rawRight)
	if kind == spec.KindNumber {
		right = resolveOperand(rawRight, sub)
	}

	if list, isList := left.([]any); isList {
		matches := func(r any) bool {
			for _, item := range list {
				if equalScalar(kind, item, r, sub) {
					return true
				}
			}
			return false
		}
		switch op {
		case spec.OpEq:
			return matches(right), nil
		case spec.OpNe:
			return !matches(right), nil
		case spec.OpIn, spec.OpNotIn:
			rights, _ := right.([]any)
			matched := false
			for _, r := range rights {
				if matches(r) {
					matched = true
					break
				}
			}
			return matched == (op == spec.OpIn), nil
		}
		return false, nil
	}

	switch op {
	case spec.OpIn, spec.OpNotIn:
		rights, _ := right.([]any)
		found := false
		if left != nil {
			for _, r := range rights {
				if equalScalar(kind, left, r, sub) {
					found = true
					break
				}
			}
		}
		return found == (op == spec.OpIn), nil
	case spec.OpEq:
		return left != nil && equalScalar(kind, left, right, sub), nil
	case spec.OpNe:
		return left == nil || !equalScalar(kind, left, right, sub), nil
	case spec.OpLike:
		str, isStr := right.(string)
		if left == nil || !isStr {
			return false, nil
		}
		return likeMatch(toString(left), str), nil
	case spec.OpGt, spec.OpGe, spec.OpLt, spec.OpLe:
		if left == nil {
			return false, nil
		}
		cmp, ok := orderScalar(kind, left, right, sub)
		if !ok {
			return false, nil
		}
		switch op {
		case spec.OpGt:
			return cmp > 0, nil
		case spec.OpGe:
			return cmp >= 0, nil
		case spec.OpLt:
			return cmp < 0, nil
		default:
			return cmp <= 0, nil
		}
	}
	return false, fmt.Errorf("unknown operator %q", op)
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case time.Time:
		return val.Format(time.RFC3339)
	}
	return fmt.Sprint(v)
}

func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return f, err == nil
	case bool:
		return 0, false
	}
	return 0, false
}

func toBool(v any) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		b, err := strconv.ParseBool(val)
		return b, err == nil
	}
	return false, false
}

// toTime reads a stored date or a date spec of a condition value.
func toTime(v any, now time.Time) (time.Time, bool) {
	switch val := v.(type) {
	case time.Time:
		return val, true
	case string:
		d, err := spec.ParseDateSpec(val, false)
		if err != nil {
			return time.Time{}, false
		}
		return d.Resolve(now), true
	}
	return time.Time{}, false
}

// effectiveKind settles the comparison kind of fields whose kind is only known by the value.
func effectiveKind(kind string, left any) string {
	if kind != spec.KindAny {
		return kind
	}
	switch left.(type) {
	case float64:
		return spec.KindNumber
	case bool:
		return spec.KindBool
	case time.Time:
		return spec.KindDate
	}
	return spec.KindString
}

func equalScalar(kind string, left, right any, sub *Subject) bool {
	switch effectiveKind(kind, left) {
	case spec.KindNumber:
		l, lok := toFloat(left)
		r, rok := toFloat(right)
		return lok && rok && math.Abs(l-r) < 1e-9
	case spec.KindBool:
		l, lok := toBool(left)
		r, rok := toBool(right)
		return lok && rok && l == r
	case spec.KindDate:
		l, lok := toTime(left, sub.Now)
		r, rok := toTime(right, sub.Now)
		return lok && rok && l.Equal(r)
	}
	return toString(left) == toString(right)
}

func orderScalar(kind string, left, right any, sub *Subject) (int, bool) {
	switch effectiveKind(kind, left) {
	case spec.KindNumber:
		l, lok := toFloat(left)
		r, rok := toFloat(right)
		if !lok || !rok {
			return 0, false
		}
		switch {
		case l < r:
			return -1, true
		case l > r:
			return 1, true
		}
		return 0, true
	case spec.KindDate:
		l, lok := toTime(left, sub.Now)
		r, rok := toTime(right, sub.Now)
		if !lok || !rok {
			return 0, false
		}
		return l.Compare(r), true
	}
	l, r := toString(left), toString(right)
	return strings.Compare(l, r), true
}

// likeMatch is a case-insensitive match with % as wildcard. Without a wildcard it is a substring match.
func likeMatch(value, pattern string) bool {
	value, pattern = strings.ToLower(value), strings.ToLower(pattern)
	if !strings.Contains(pattern, "%") {
		return strings.Contains(value, pattern)
	}
	parts := strings.Split(pattern, "%")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	re, err := regexp.Compile("^" + strings.Join(parts, ".*") + "$")
	if err != nil {
		return false
	}
	return re.MatchString(value)
}
