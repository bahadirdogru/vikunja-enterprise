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
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DateNone clears a date in set_field actions.
const DateNone = "none"

var (
	offsetPattern  = regexp.MustCompile(`^([+-]?)(\d{1,6})([mhdw])$`)
	nowPattern     = regexp.MustCompile(`^now(?:([+-])(\d{1,6})([mhdw]))?$`)
	dateOnlyLayout = "2006-01-02"
)

// ParseOffset parses a duration like 12h, +3d or -2w. Units are m(inutes), h(ours), d(ays) and w(eeks).
func ParseOffset(s string) (time.Duration, error) {
	m := offsetPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("%q is not a duration like 12h, +3d or -2w", s)
	}
	n, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return 0, err
	}
	d := time.Duration(n) * unitDuration(m[3])
	if m[1] == "-" {
		d = -d
	}
	return d, nil
}

func unitDuration(u string) time.Duration {
	switch u {
	case "m":
		return time.Minute
	case "h":
		return time.Hour
	case "d":
		return 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

// DateSpec is a parsed date value.
type DateSpec struct {
	// None means "clear the date" (action parameters only).
	None bool
	// Absolute is set for RFC 3339 and plain dates.
	Absolute time.Time
	// Relative is true for now, now+3d, +3d; the result is now plus Offset.
	Relative bool
	Offset   time.Duration
}

// Resolve returns the point in time the spec stands for. A cleared date resolves to the zero time.
func (d DateSpec) Resolve(now time.Time) time.Time {
	switch {
	case d.None:
		return time.Time{}
	case d.Relative:
		return now.Add(d.Offset)
	default:
		return d.Absolute
	}
}

// ParseDateSpec parses an RFC 3339 timestamp, a plain date (2026-12-31, UTC), now, now+3d, now-12h and
// - when allowOffset is set - a bare offset like +3d or -2h relative to now and the word none.
func ParseDateSpec(s string, allowOffset bool) (DateSpec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DateSpec{}, fmt.Errorf("the date is empty")
	}
	if allowOffset && s == DateNone {
		return DateSpec{None: true}, nil
	}
	if m := nowPattern.FindStringSubmatch(s); m != nil {
		if m[1] == "" {
			return DateSpec{Relative: true}, nil
		}
		d, err := ParseOffset(m[1] + m[2] + m[3])
		if err != nil {
			return DateSpec{}, err
		}
		return DateSpec{Relative: true, Offset: d}, nil
	}
	if allowOffset && (s[0] == '+' || s[0] == '-') {
		d, err := ParseOffset(s)
		if err != nil {
			return DateSpec{}, err
		}
		return DateSpec{Relative: true, Offset: d}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return DateSpec{Absolute: t}, nil
	}
	if t, err := time.Parse(dateOnlyLayout, s); err == nil {
		return DateSpec{Absolute: t}, nil
	}
	hint := "an RFC 3339 timestamp, a date like 2026-12-31, now, now+3d or now-12h"
	if allowOffset {
		hint += ", an offset like +3d or none"
	}
	return DateSpec{}, fmt.Errorf("%q is not %s", s, hint)
}
