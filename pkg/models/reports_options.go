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
	"strconv"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// ReportOption is a dimension, field or time field a report can use.
type ReportOption struct {
	Name          string `json:"name" doc:"The value to send, e.g. group_by: assignee or cf_12."`
	TitleKey      string `json:"title_key" doc:"A translation key for clients that localize, e.g. reports.dimension.assignee."`
	Label         string `json:"label" doc:"A readable title: the english name of built-in options, the title of a custom field."`
	MultiValued   bool   `json:"multi_valued,omitempty" doc:"Dimensions only: a task can be part of several groups."`
	CustomFieldID int64  `json:"custom_field_id,omitempty" doc:"Set for custom fields."`
	ProjectID     int64  `json:"project_id,omitempty" doc:"The project of a custom field."`
	FieldType     string `json:"field_type,omitempty" doc:"The type of a custom field."`
}

// ReportMeasureOption is a measure with the fields it can work on.
type ReportMeasureOption struct {
	Name     string          `json:"name"`
	TitleKey string          `json:"title_key"`
	Label    string          `json:"label"`
	Unit     string          `json:"unit,omitempty" doc:"tasks for count."`
	Fields   []*ReportOption `json:"fields,omitempty" doc:"The fields sum, avg, min and max can work on."`
}

// ReportLimits are the limits of the reports of this instance.
type ReportLimits struct {
	MaxDimensions int `json:"max_dimensions"`
	MaxGroups     int `json:"max_groups"`
	MaxBuckets    int `json:"max_buckets"`
	DefaultLimit  int `json:"default_limit"`
}

// ReportOptions lists what a report request may use in a scope. Widget editors are generated from it.
type ReportOptions struct {
	ProjectID   int64                  `json:"project_id" doc:"The scope that was asked for, 0 for all projects the user can read."`
	HasWorkflow bool                   `json:"has_workflow" doc:"Whether a project of the scope has a workflow. Without one the status dimensions and the status_entered time field are not offered."`
	Dimensions  []*ReportOption        `json:"dimensions"`
	Measures    []*ReportMeasureOption `json:"measures"`
	TimeFields  []*ReportOption        `json:"time_fields"`
	Buckets     []string               `json:"buckets"`
	Limits      ReportLimits           `json:"limits"`
}

func builtinOption(group, name, label string, multi bool) *ReportOption {
	return &ReportOption{Name: name, TitleKey: "reports." + group + "." + name, Label: label, MultiValued: multi}
}

// GetReportOptions returns what a report may use for the project (0: every project the auth can read). It only names
// custom fields of projects the auth can read.
func GetReportOptions(s *xorm.Session, a web.Auth, projectID int64) (*ReportOptions, error) {
	scope := builder.Cond(nil)
	if projectID != 0 {
		if projectID < 0 {
			return nil, ErrReportInvalidParameter{Param: "project_id", Reason: "must be a project id or 0"}
		}
		can, _, err := (&Project{ID: projectID}).CanRead(s, a)
		if err != nil {
			return nil, err
		}
		if !can {
			return nil, ErrUserDoesNotHaveAccessToProject{ProjectID: projectID, UserID: a.GetID()}
		}
		scope = builder.Eq{"project_id": projectID}
	} else {
		accessible, err := accessibleProjectIDsCond(s, a, "project_id")
		if err != nil {
			return nil, err
		}
		scope = accessible
	}

	hasWorkflow, err := s.Where(scope).Exist(&Workflow{})
	if err != nil {
		return nil, err
	}
	fields := []*CustomField{}
	if err = s.Where(scope).OrderBy("project_id ASC, position ASC, id ASC").Find(&fields); err != nil {
		return nil, err
	}

	opts := &ReportOptions{
		ProjectID:   projectID,
		HasWorkflow: hasWorkflow,
		Buckets:     []string{ReportBucketDay, ReportBucketWeek, ReportBucketMonth},
		Limits: ReportLimits{
			MaxDimensions: 2,
			MaxGroups:     config.ReportsMaxGroups.GetInt(),
			MaxBuckets:    config.ReportsMaxBuckets.GetInt(),
			DefaultLimit:  reportDefaultLimit,
		},
	}

	if hasWorkflow {
		opts.Dimensions = append(opts.Dimensions,
			builtinOption("dimension", ReportDimStatus, "Status", false),
			builtinOption("dimension", ReportDimStatusCategory, "Status category", false),
		)
	}
	opts.Dimensions = append(opts.Dimensions,
		builtinOption("dimension", ReportDimAssignee, "Assignee", true),
		builtinOption("dimension", ReportDimLabel, "Label", true),
		builtinOption("dimension", ReportDimProject, "Project", false),
		builtinOption("dimension", ReportDimPriority, "Priority", false),
		builtinOption("dimension", ReportDimDone, "Done", false),
		builtinOption("dimension", ReportDimCreatedBy, "Created by", false),
	)

	numeric := []*ReportOption{builtinOption("field", ReportFieldPercentDone, "Percent done", false)}
	if _, isShare := a.(*LinkSharing); !isShare {
		numeric = append(numeric, builtinOption("field", ReportFieldTimeSpent, "Time spent (seconds)", false))
	}
	for _, f := range fields {
		opt := &ReportOption{
			Name:          customFieldFilterPrefix + strconv.FormatInt(f.ID, 10),
			TitleKey:      "reports.field.custom_field",
			Label:         f.Title,
			CustomFieldID: f.ID,
			ProjectID:     f.ProjectID,
			FieldType:     string(f.Type),
		}
		switch f.Type { //nolint:exhaustive // the other types are no dimension
		case CustomFieldTypeSelect, CustomFieldTypeMultiSelect, CustomFieldTypeUser, CustomFieldTypeCheckbox, CustomFieldTypeRating:
			dim := *opt
			dim.MultiValued = f.Type == CustomFieldTypeMultiSelect || f.Type == CustomFieldTypeUser
			opts.Dimensions = append(opts.Dimensions, &dim)
		}
		if isNumericFieldType(f.Type) {
			numeric = append(numeric, opt)
		}
	}

	opts.Measures = []*ReportMeasureOption{{Name: ReportMeasureCount, TitleKey: "reports.measure.count", Label: "Count", Unit: "tasks"}}
	for _, m := range []string{ReportMeasureSum, ReportMeasureAvg, ReportMeasureMin, ReportMeasureMax} {
		opts.Measures = append(opts.Measures, &ReportMeasureOption{
			Name:     m,
			TitleKey: "reports.measure." + m,
			Label:    map[string]string{ReportMeasureSum: "Sum", ReportMeasureAvg: "Average", ReportMeasureMin: "Minimum", ReportMeasureMax: "Maximum"}[m],
			Fields:   numeric,
		})
	}

	opts.TimeFields = []*ReportOption{
		builtinOption("time_field", ReportTimeFieldCreated, "Created", false),
		builtinOption("time_field", ReportTimeFieldDoneAt, "Completed", false),
		builtinOption("time_field", ReportTimeFieldDueDate, "Due date", false),
	}
	if hasWorkflow {
		opts.TimeFields = append(opts.TimeFields, builtinOption("time_field", ReportTimeFieldStatusEntered, "Entered current status", false))
	}
	return opts, nil
}
