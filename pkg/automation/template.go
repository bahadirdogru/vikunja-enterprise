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
	"regexp"
	"strconv"
	"strings"
	"time"
)

var placeholderPattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.]+)\s*\}\}`)

// Render replaces the placeholders of a text parameter with values of the subject. It is a plain substitution: there
// is no logic, no loop and nothing is executed. Unknown placeholders render empty and are added to unknown.
func Render(text string, sub *Subject, unknown *[]string) string {
	return renderWith(text, sub, unknown, nil)
}

// renderWith is Render with an additional resolver that is asked first. The http.request action uses it for
// {{secrets.NAME}}, which no other text can use.
func renderWith(text string, sub *Subject, unknown *[]string, extra func(name string) (string, bool)) string {
	return placeholderPattern.ReplaceAllStringFunc(text, func(match string) string {
		name := placeholderPattern.FindStringSubmatch(match)[1]
		if extra != nil {
			if value, ok := extra(name); ok {
				return value
			}
		}
		value, ok := placeholderValue(name, sub)
		if !ok {
			if unknown != nil {
				*unknown = append(*unknown, name)
			}
			return ""
		}
		return value
	})
}

func placeholderValue(name string, sub *Subject) (string, bool) {
	switch name {
	case "task.id":
		if sub.Task != nil {
			return strconv.FormatInt(sub.Task.ID, 10), true
		}
	case "task.title", "task.description", "task.identifier":
		if v, ok := sub.Fields[strings.TrimPrefix(name, "task.")]; ok {
			return toString(v), true
		}
	case "event.doer.name":
		if sub.Event != nil && sub.Event.Doer != nil {
			return sub.Event.Doer.Name, true
		}
		return "", true
	case "event.doer.username":
		if sub.Event != nil && sub.Event.Doer != nil {
			return sub.Event.Doer.Username, true
		}
		return "", true
	case "project.title":
		if sub.Project != nil {
			return sub.Project.Title, true
		}
	case "event.form.title", "event.form.submitter_name", "event.form.submitter_email":
		key := map[string]string{"event.form.title": "form_title", "event.form.submitter_name": "submitter_name", "event.form.submitter_email": "submitter_email"}[name]
		if sub.Event != nil {
			if v, has := sub.Event.New[key]; has {
				return toString(canon(v)), true
			}
		}
		return "", true
	case "event.approval.id", "event.approval.title", "event.approval.outcome":
		key := map[string]string{"event.approval.id": "approval_id", "event.approval.title": "approval_title", "event.approval.outcome": "outcome"}[name]
		if sub.Event != nil {
			if v, has := sub.Event.New[key]; has {
				return toString(canon(v)), true
			}
		}
		return "", true
	}
	if rest, ok := strings.CutPrefix(name, "task.cf."); ok {
		id, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return "", false
		}
		if _, known := sub.CFTypes[id]; !known {
			return "", false
		}
		return formatCustomFieldValue(sub, id), true
	}
	return "", false
}

func formatCustomFieldValue(sub *Subject, fieldID int64) string {
	v := sub.Fields[cfKey(fieldID)]
	titles := sub.CFOptions[fieldID]
	format := func(item any) string {
		if f, ok := item.(float64); ok && titles != nil {
			if title, has := titles[int64(f)]; has {
				return title
			}
		}
		if t, ok := item.(time.Time); ok {
			return t.Format(time.RFC3339)
		}
		return toString(item)
	}
	if list, ok := v.([]any); ok {
		parts := make([]string, len(list))
		for i, item := range list {
			parts[i] = format(item)
		}
		return strings.Join(parts, ", ")
	}
	if v == nil {
		return ""
	}
	return format(v)
}

// placeholderNames lists the placeholders a text uses.
func placeholderNames(text string) []string {
	names := []string{}
	for _, m := range placeholderPattern.FindAllStringSubmatch(text, -1) {
		names = append(names, m[1])
	}
	return names
}
