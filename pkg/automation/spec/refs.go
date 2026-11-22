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
	"slices"
)

// Handler is called for every id a rule document refers to. It returns the id to put in its place.
type Handler struct {
	// ID maps an id of the given kind (status, custom_field, label or project). path locates it in the document.
	ID func(kind string, id int64, path string) int64
	// IsOptionField reports whether the values of the custom field are option ids (select, multi_select).
	IsOptionField func(fieldID int64) bool
	// Option maps an option id of a custom field.
	Option func(fieldID, optionID int64) int64
}

// Walk visits every id of the document and rewrites it with the handler. The document is changed in place.
func Walk(d *Doc, h Handler) {
	if h.ID == nil {
		h.ID = func(_ string, id int64, _ string) int64 { return id }
	}
	if h.IsOptionField == nil {
		h.IsOptionField = func(int64) bool { return false }
	}
	if h.Option == nil {
		h.Option = func(_, id int64) int64 { return id }
	}
	w := &walker{h: h}
	if t := TriggerByType(d.Trigger.Type); t != nil {
		w.params("trigger.options", t.Options, d.Trigger.Options)
	}
	if d.Conditions != nil {
		w.condition("conditions", d.Conditions)
	}
	for i, a := range d.Actions {
		if a == nil {
			continue
		}
		def := ActionByType(a.Type)
		if def == nil {
			continue
		}
		if a.Type == ActionSetCustomField {
			// Before field_id is rewritten: the option map is keyed by the old field id.
			w.setCustomFieldValue(a.Params)
		}
		w.params(fmt.Sprintf("actions[%d].params", i), def.Params, a.Params)
	}
}

type walker struct{ h Handler }

func isRefKind(kind string) bool {
	switch kind {
	case KindStatus, KindCustomField, KindLabel, KindProject:
		return true
	}
	return false
}

func (w *walker) mapValue(kind, path string, v any) any {
	switch val := v.(type) {
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = w.mapValue(kind, fmt.Sprintf("%s[%d]", path, i), item)
		}
		return out
	default:
		if id, ok := ToInt64(v); ok {
			return float64(w.h.ID(kind, id, path))
		}
	}
	return v
}

func (w *walker) params(path string, defs []Param, values map[string]any) {
	for _, p := range defs {
		val, ok := values[p.Name]
		if !ok || val == nil {
			continue
		}
		pp := path + "." + p.Name
		switch {
		case p.Kind == KindCondition:
			if c, ok := ConditionFromParam(val); ok {
				w.condition(pp, c)
				values[p.Name] = c
			}
		case p.KindBy != nil:
			sibling, _ := values[p.KindBy.Param].(string)
			if kind := p.KindBy.Kinds[sibling]; isRefKind(kind) {
				values[p.Name] = w.mapValue(kind, pp, val)
			}
		case isRefKind(p.Kind):
			values[p.Name] = w.mapValue(p.Kind, pp, val)
		}
	}
}

func (w *walker) setCustomFieldValue(params map[string]any) {
	id, ok := ToInt64(params["field_id"])
	if !ok || params["value"] == nil || !w.h.IsOptionField(id) {
		return
	}
	params["value"] = w.mapOptions(id, params["value"])
}

func (w *walker) condition(path string, c *Condition) {
	for name, group := range map[string][]*Condition{"all": c.All, "any": c.Any, "not": c.Not} {
		for i, child := range group {
			if child != nil {
				w.condition(fmt.Sprintf("%s.%s[%d]", path, name, i), child)
			}
		}
	}
	if id, ok := CustomFieldID(c.Field); ok {
		if c.Value != nil && w.h.IsOptionField(id) {
			c.Value = w.mapOptions(id, c.Value)
		}
		c.Field = fmt.Sprintf("cf_%d", w.h.ID(KindCustomField, id, path+".field"))
		return
	}
	if c.Value == nil {
		return
	}
	if def := FieldByName(c.Field); def != nil && isRefKind(def.Kind) {
		c.Value = w.mapValue(def.Kind, path+".value", c.Value)
	}
}

func (w *walker) mapOptions(fieldID int64, v any) any {
	switch val := v.(type) {
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = w.mapOptions(fieldID, item)
		}
		return out
	default:
		if id, ok := ToInt64(v); ok {
			return float64(w.h.Option(fieldID, id))
		}
	}
	return v
}

// Refs are the ids a rule document refers to.
type Refs struct {
	CustomFields []int64
	Statuses     []int64
	Labels       []int64
	Projects     []int64
}

// Empty reports whether the document refers to nothing.
func (r Refs) Empty() bool {
	return len(r.CustomFields)+len(r.Statuses)+len(r.Labels)+len(r.Projects) == 0
}

// Collect returns the ids of the document, each once and sorted. The document is not changed.
func Collect(d *Doc) Refs {
	sets := map[string]map[int64]bool{KindCustomField: {}, KindStatus: {}, KindLabel: {}, KindProject: {}}
	Walk(CloneDoc(d), Handler{ID: func(kind string, id int64, _ string) int64 {
		sets[kind][id] = true
		return id
	}})
	list := func(m map[int64]bool) []int64 {
		out := make([]int64, 0, len(m))
		for id := range m {
			out = append(out, id)
		}
		slices.Sort(out)
		return out
	}
	return Refs{
		CustomFields: list(sets[KindCustomField]),
		Statuses:     list(sets[KindStatus]),
		Labels:       list(sets[KindLabel]),
		Projects:     list(sets[KindProject]),
	}
}
