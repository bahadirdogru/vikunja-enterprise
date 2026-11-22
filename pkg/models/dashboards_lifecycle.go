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

package models

import (
	"regexp"
	"strconv"
	"strings"

	"xorm.io/xorm"
)

// The filter terms whose ids belong to a duplicated project: a custom field id anywhere, the option ids compared to a
// select field and the status ids compared to status.
var dashboardFilterRefs = regexp.MustCompile(`(cf_(\d+)|status)(\s*(?:!=|=|not in|in)\s*)(\d+(?:\s*,\s*\d+)*)|cf_(\d+)`)

type dashboardRemap struct {
	fields       map[int64]int64
	options      map[int64]int64
	statuses     map[int64]int64
	optionFields map[int64]bool
	srcProject   int64
	dstProject   int64
}

func (m *dashboardRemap) idList(list string, mapping map[int64]int64) string {
	parts := strings.Split(list, ",")
	for i, p := range parts {
		trimmed := strings.TrimSpace(p)
		if id, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			if to, ok := mapping[id]; ok {
				parts[i] = strings.Replace(p, trimmed, strconv.FormatInt(to, 10), 1)
			}
		}
	}
	return strings.Join(parts, ",")
}

// filter rewrites the cf_<id>, option and status ids of a filter string. Ids without a copy stay as they are.
func (m *dashboardRemap) filter(f string) string {
	return dashboardFilterRefs.ReplaceAllStringFunc(f, func(match string) string {
		sub := dashboardFilterRefs.FindStringSubmatch(match)
		if sub[5] != "" {
			id, _ := strconv.ParseInt(sub[5], 10, 64)
			if to, ok := m.fields[id]; ok {
				return "cf_" + strconv.FormatInt(to, 10)
			}
			return match
		}
		if sub[1] == "status" {
			return "status" + sub[3] + m.idList(sub[4], m.statuses)
		}
		fieldID, _ := strconv.ParseInt(sub[2], 10, 64)
		name, values := sub[1], sub[4]
		if to, ok := m.fields[fieldID]; ok {
			name = "cf_" + strconv.FormatInt(to, 10)
		}
		if m.optionFields[fieldID] {
			values = m.idList(values, m.options)
		}
		return name + sub[3] + values
	})
}

func (m *dashboardRemap) value(key string, v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, inner := range t {
			t[k] = m.value(k, inner)
		}
		return t
	case []any:
		for i, inner := range t {
			t[i] = m.value(key, inner)
		}
		return t
	case string:
		if key == "filter" {
			return m.filter(t)
		}
		if id, ok := parseCustomFieldIdentifier(t); ok {
			if to, found := m.fields[id]; found {
				return customFieldFilterPrefix + strconv.FormatInt(to, 10)
			}
		}
		return t
	case float64:
		if key == "project_id" && int64(t) == m.srcProject {
			return float64(m.dstProject)
		}
	}
	return v
}

// duplicateDashboardsForProject copies the dashboards of a project and their widgets into its duplicate. The copies
// belong to the user who duplicated the project; custom field, option and status ids in their configs are replaced
// with the ids of the copies.
func duplicateDashboardsForProject(s *xorm.Session, doerID, sourceProjectID, targetProjectID int64, fieldMap, optionMap, statusMap map[int64]int64) error {
	dashboards := []*Dashboard{}
	if err := s.Where("project_id = ?", sourceProjectID).OrderBy("id ASC").Find(&dashboards); err != nil {
		return err
	}
	if len(dashboards) == 0 {
		return nil
	}

	remap := &dashboardRemap{
		fields:       fieldMap,
		options:      optionMap,
		statuses:     statusMap,
		optionFields: map[int64]bool{},
		srcProject:   sourceProjectID,
		dstProject:   targetProjectID,
	}
	fields := []*CustomField{}
	if err := s.Where("project_id = ?", sourceProjectID).Find(&fields); err != nil {
		return err
	}
	for _, f := range fields {
		if f.Type.hasOptions() {
			remap.optionFields[f.ID] = true
		}
	}

	for _, d := range dashboards {
		widgets, err := loadDashboardWidgets(s, d.ID)
		if err != nil {
			return err
		}
		clone := &Dashboard{Title: d.Title, Description: d.Description, OwnerID: doerID, ProjectID: targetProjectID, Position: d.Position}
		if _, err = s.Insert(clone); err != nil {
			return err
		}
		for _, w := range widgets {
			cfg, _ := remap.value("", w.Config).(map[string]any)
			copied := &DashboardWidget{
				DashboardID: clone.ID,
				Type:        w.Type,
				Title:       w.Title,
				Config:      cfg,
				X:           w.X,
				Y:           w.Y,
				W:           w.W,
				H:           w.H,
			}
			if _, err = s.Insert(copied); err != nil {
				return err
			}
		}
	}
	return nil
}
