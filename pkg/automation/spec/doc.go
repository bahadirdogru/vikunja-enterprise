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

import "encoding/json"

// Trigger is the "when" of a rule.
type Trigger struct {
	Type    string         `json:"type" doc:"The trigger type from the catalog, e.g. task.status_changed."`
	Options map[string]any `json:"options,omitempty" doc:"The options of the trigger, see the catalog for the ones each trigger accepts."`
}

// Condition is the "if" of a rule: a tree of all / any / not groups over comparisons. Exactly one of all, any,
// not or field is set on every node.
type Condition struct {
	All    []*Condition `json:"all,omitempty" doc:"True when every child is true."`
	Any    []*Condition `json:"any,omitempty" doc:"True when at least one child is true."`
	Not    []*Condition `json:"not,omitempty" doc:"True when none of the children is true."`
	Field  string       `json:"field,omitempty" doc:"The field to compare, see the catalog."`
	Op     string       `json:"op,omitempty" doc:"The operator, see the catalog for the ones each field supports."`
	Script string       `json:"script,omitempty" doc:"A JavaScript condition instead of a comparison: the body of a function that returns true or false (see the script reference of the catalog). Cannot be combined with field, op or value. Runs with a read-only API."`
	Value  any          `json:"value,omitempty" doc:"The value to compare with. A list for in and not in, absent for is_empty and is_not_empty. Dates are RFC 3339 timestamps, plain dates or relative: now, now+3d, now-12h."`
}

// Action is one step of the "then" of a rule.
type Action struct {
	Type   string         `json:"type" doc:"The action type from the catalog, e.g. task.set_field."`
	Params map[string]any `json:"params,omitempty" doc:"The parameters of the action, see the catalog for the ones each action accepts."`
}

// Doc is the whole rule document.
type Doc struct {
	Trigger    Trigger
	Conditions *Condition
	Actions    []*Action
}

// Issue is one problem of a rule document, with the path of the offending part.
type Issue struct {
	Path    string `json:"path" doc:"The location of the problem, e.g. actions[1].params.status_id."`
	Message string `json:"message" doc:"What is wrong."`
	Line    int    `json:"line,omitempty" doc:"For a syntax error in a script: the line of the error, counted from 1 in the source of the script."`
	Column  int    `json:"column,omitempty" doc:"For a syntax error in a script: the column of the error, counted from 1."`
}

// IsLeaf reports whether the node is a comparison.
func (c *Condition) IsLeaf() bool {
	return c.Field != "" || c.Script != ""
}

// CloneDoc returns a deep copy of the document.
func CloneDoc(d *Doc) *Doc {
	raw, err := json.Marshal(struct {
		Trigger    Trigger    `json:"trigger"`
		Conditions *Condition `json:"conditions"`
		Actions    []*Action  `json:"actions"`
	}{d.Trigger, d.Conditions, d.Actions})
	if err != nil {
		return d
	}
	out := struct {
		Trigger    Trigger    `json:"trigger"`
		Conditions *Condition `json:"conditions"`
		Actions    []*Action  `json:"actions"`
	}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return d
	}
	return &Doc{Trigger: out.Trigger, Conditions: out.Conditions, Actions: out.Actions}
}
