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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

type widgetReportConfig struct {
	Request ReportRequest `json:"request"`
	Compare string        `json:"compare,omitempty"`
}

type widgetCreatedVsDoneConfig struct {
	ProjectID int64  `json:"project_id,omitempty"`
	Filter    string `json:"filter,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

type widgetTaskListConfig struct {
	ProjectID int64    `json:"project_id,omitempty"`
	Filter    string   `json:"filter,omitempty"`
	SortBy    []string `json:"sort_by,omitempty"`
	OrderBy   []string `json:"order_by,omitempty"`
	Columns   []string `json:"columns,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

type widgetWorkloadConfig struct {
	ProjectID     int64  `json:"project_id,omitempty"`
	Filter        string `json:"filter,omitempty"`
	EstimateField string `json:"estimate_field,omitempty"`
}

type widgetStatusTimeConfig struct {
	ProjectID int64  `json:"project_id,omitempty"`
	Filter    string `json:"filter,omitempty"`
	GroupBy   string `json:"group_by,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

type widgetTimeTrackingConfig struct {
	ProjectID int64  `json:"project_id,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	GroupBy   string `json:"group_by,omitempty"`
}

type widgetTextConfig struct {
	Text string `json:"text"`
}

const (
	widgetMaxText      = 20000
	widgetMaxListLimit = 100
	widgetMaxColumns   = 30
)

func decodeWidgetConfig(config map[string]any, into any, path string) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return ErrDashboardInvalid{Param: path + ".config", Reason: err.Error()}
	}
	if config == nil {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(into); err != nil {
		return ErrDashboardInvalid{Param: path + ".config", Reason: err.Error()}
	}
	return nil
}

func encodeWidgetConfig(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// scopeWidgetProject pins the project of a widget to the project of a project dashboard, and checks that the author
// can read the project a personal dashboard points at.
func scopeWidgetProject(s *xorm.Session, a web.Auth, dashboardProjectID int64, projectID *int64, path string) error {
	if dashboardProjectID > 0 {
		if *projectID != 0 && *projectID != dashboardProjectID {
			return ErrDashboardInvalid{Param: path, Reason: "a project dashboard can only show its own project"}
		}
		*projectID = dashboardProjectID
		return nil
	}
	if *projectID <= 0 {
		return nil
	}
	can, _, err := (&Project{ID: *projectID}).CanRead(s, a)
	if err != nil && !IsErrProjectDoesNotExist(err) {
		return err
	}
	if err != nil || !can {
		return ErrDashboardInvalid{Param: path, Reason: "the project does not exist or you cannot read it"}
	}
	return nil
}

// widgetReportError points an error of the report validation at the widget part that caused it.
func widgetReportError(err error, path string) error {
	if e, ok := errors.AsType[ErrReportInvalidParameter](err); ok {
		return ErrDashboardInvalid{Param: path + "." + e.Param, Reason: e.Reason}
	}
	if e, ok := errors.AsType[ErrReportFieldKindNotSupported](err); ok {
		return ErrDashboardInvalid{Param: path + "." + e.Param, Reason: e.HTTPError().Message}
	}
	if e, ok := errors.AsType[ErrReportTooManyBuckets](err); ok {
		return ErrDashboardInvalid{Param: path + ".time", Reason: e.HTTPError().Message}
	}
	if e, ok := errors.AsType[ErrReportTooManyGroups](err); ok {
		return ErrDashboardInvalid{Param: path + ".limit", Reason: e.HTTPError().Message}
	}
	return err
}

func validateWidgetRequest(s *xorm.Session, a web.Auth, req ReportRequest, path string) error {
	return widgetReportError(req.Validate(s, a), path)
}

func validateWidgetFilter(s *xorm.Session, a web.Auth, filter string) error {
	tz := reportAuthTimezone(s, a)
	loc, err := reportLocation("", tz)
	if err != nil {
		return err
	}
	filters, err := getTaskFiltersFromFilterString(filter, loc.String())
	if err != nil {
		return err
	}
	return resolveCustomFieldFilters(s, a, filters)
}

func validateWidgetDate(raw, path string) error {
	if raw == "" {
		return nil
	}
	if _, err := parseReportDate(raw, time.UTC, time.Now(), path); err != nil {
		if e, ok := errors.AsType[ErrReportInvalidParameter](err); ok {
			return ErrDashboardInvalid{Param: path, Reason: e.Reason}
		}
		return err
	}
	return nil
}

// validate checks the widget and normalizes its config. It never runs a report.
//
//nolint:gocyclo
func (w *DashboardWidget) validate(s *xorm.Session, a web.Auth, dashboardProjectID int64, path string) error {
	if len([]rune(w.Title)) > 250 {
		return ErrDashboardInvalid{Param: path + ".title", Reason: "must be at most 250 characters"}
	}
	if err := validateWidgetGrid(w.X, w.Y, w.W, w.H, path); err != nil {
		return err
	}

	var normalized any
	switch w.Type {
	case WidgetTypeNumber, WidgetTypeBar, WidgetTypeDonut, WidgetTypeLine:
		c := &widgetReportConfig{}
		if err := decodeWidgetConfig(w.Config, c, path); err != nil {
			return err
		}
		if err := scopeWidgetProject(s, a, dashboardProjectID, &c.Request.ProjectID, path+".config.request.project_id"); err != nil {
			return err
		}
		if w.Type == WidgetTypeNumber && c.Compare != "" {
			// The report computes the comparison, so the compare setting travels with the request.
			c.Request.Compare = c.Compare
		}
		if err := validateWidgetShape(w.Type, c, path+".config"); err != nil {
			return err
		}
		if err := validateWidgetRequest(s, a, c.Request, path+".config.request"); err != nil {
			return err
		}
		normalized = c
	case WidgetTypeCreatedVsDone:
		c := &widgetCreatedVsDoneConfig{}
		if err := decodeWidgetConfig(w.Config, c, path); err != nil {
			return err
		}
		if err := scopeWidgetProject(s, a, dashboardProjectID, &c.ProjectID, path+".config.project_id"); err != nil {
			return err
		}
		req := ReportRequest{ProjectID: c.ProjectID, Filter: c.Filter, Time: &ReportTime{Field: ReportTimeFieldCreated, Bucket: c.Bucket, From: c.From, To: c.To}}
		if err := validateWidgetRequest(s, a, req, path+".config"); err != nil {
			return err
		}
		normalized = c
	case WidgetTypeTaskList:
		c := &widgetTaskListConfig{}
		if err := decodeWidgetConfig(w.Config, c, path); err != nil {
			return err
		}
		if err := scopeWidgetProject(s, a, dashboardProjectID, &c.ProjectID, path+".config.project_id"); err != nil {
			return err
		}
		if err := validateTaskListConfig(s, a, c, path+".config"); err != nil {
			return err
		}
		normalized = c
	case WidgetTypeWorkload:
		c := &widgetWorkloadConfig{}
		if err := decodeWidgetConfig(w.Config, c, path); err != nil {
			return err
		}
		if err := scopeWidgetProject(s, a, dashboardProjectID, &c.ProjectID, path+".config.project_id"); err != nil {
			return err
		}
		if err := validateWidgetFilter(s, a, c.Filter); err != nil {
			return err
		}
		if c.EstimateField != "" {
			req := ReportRequest{ProjectID: c.ProjectID, Measure: ReportMeasureSum, Field: c.EstimateField}
			if err := req.Validate(s, a); err != nil {
				return ErrDashboardInvalid{Param: path + ".config.estimate_field", Reason: reportErrorMessage(err)}
			}
		}
		normalized = c
	case WidgetTypeStatusTime:
		c := &widgetStatusTimeConfig{}
		if err := decodeWidgetConfig(w.Config, c, path); err != nil {
			return err
		}
		if err := scopeWidgetProject(s, a, dashboardProjectID, &c.ProjectID, path+".config.project_id"); err != nil {
			return err
		}
		if c.ProjectID <= 0 {
			return ErrDashboardInvalid{Param: path + ".config.project_id", Reason: "is required: statuses belong to the workflow of one project"}
		}
		if err := validateWidgetFilter(s, a, c.Filter); err != nil {
			return err
		}
		if c.GroupBy != "" && !slices.Contains([]string{StatusTimeGroupAssignee, StatusTimeGroupLabel}, c.GroupBy) {
			return ErrDashboardInvalid{Param: path + ".config.group_by", Reason: "must be assignee or label"}
		}
		if err := validateWidgetDates(path+".config", c.From, c.To); err != nil {
			return err
		}
		normalized = c
	case WidgetTypeTimeTracking:
		c := &widgetTimeTrackingConfig{}
		if err := decodeWidgetConfig(w.Config, c, path); err != nil {
			return err
		}
		if err := scopeWidgetProject(s, a, dashboardProjectID, &c.ProjectID, path+".config.project_id"); err != nil {
			return err
		}
		if c.GroupBy != "" && !slices.Contains([]string{TimeTrackingGroupUser, TimeTrackingGroupProject, TimeTrackingGroupLabel, TimeTrackingGroupDay}, c.GroupBy) {
			return ErrDashboardInvalid{Param: path + ".config.group_by", Reason: "must be user, project, label or day"}
		}
		if err := validateWidgetDates(path+".config", c.From, c.To); err != nil {
			return err
		}
		normalized = c
	case WidgetTypeText:
		c := &widgetTextConfig{}
		if err := decodeWidgetConfig(w.Config, c, path); err != nil {
			return err
		}
		if len([]rune(c.Text)) > widgetMaxText {
			return ErrDashboardInvalid{Param: path + ".config.text", Reason: fmt.Sprintf("must be at most %d characters", widgetMaxText)}
		}
		normalized = c
	default:
		return ErrDashboardInvalid{Param: path + ".type", Reason: fmt.Sprintf("%q is not one of %s", w.Type, widgetTypesDocumentation)}
	}

	config, err := encodeWidgetConfig(normalized)
	if err != nil {
		return err
	}
	w.Config = config
	return nil
}

func reportErrorMessage(err error) string {
	if hp, ok := err.(web.HTTPErrorProcessor); ok {
		return hp.HTTPError().Message
	}
	return err.Error()
}

func validateWidgetDates(path, from, to string) error {
	if err := validateWidgetDate(from, path+".from"); err != nil {
		return err
	}
	return validateWidgetDate(to, path+".to")
}

// validateWidgetShape checks what a number, bar, donut or line widget needs from its report.
func validateWidgetShape(widgetType string, c *widgetReportConfig, path string) error {
	req := c.Request
	invalid := func(param, reason string) error {
		return ErrDashboardInvalid{Param: path + ".request." + param, Reason: reason}
	}
	switch widgetType {
	case WidgetTypeNumber:
		if req.GroupBy != "" || req.ThenBy != "" {
			return invalid("group_by", "a number widget shows one figure, it cannot group")
		}
		if c.Compare != "" && c.Compare != "previous_period" {
			return ErrDashboardInvalid{Param: path + ".compare", Reason: "must be empty or previous_period"}
		}
		if c.Compare != "" && req.Time == nil {
			return invalid("time", "comparing with the previous period needs a time range")
		}
	case WidgetTypeBar:
		if req.GroupBy == "" {
			return invalid("group_by", "is required")
		}
		if req.Time != nil {
			return invalid("time", "a bar widget cannot be a time series, use a line widget")
		}
	case WidgetTypeDonut:
		if req.GroupBy == "" {
			return invalid("group_by", "is required")
		}
		if req.ThenBy != "" {
			return invalid("then_by", "a donut widget has one level")
		}
		if req.Time != nil {
			return invalid("time", "a donut widget cannot be a time series, use a line widget")
		}
	case WidgetTypeLine:
		if req.Time == nil {
			return invalid("time", "is required")
		}
		if req.ThenBy != "" {
			return invalid("then_by", "a line widget has one line per group_by value")
		}
	}
	if widgetType != WidgetTypeNumber && c.Compare != "" {
		return ErrDashboardInvalid{Param: path + ".compare", Reason: "is only used by number widgets"}
	}
	return nil
}

func validateTaskListConfig(s *xorm.Session, a web.Auth, c *widgetTaskListConfig, path string) error {
	if err := validateWidgetFilter(s, a, c.Filter); err != nil {
		return err
	}
	if c.Limit < 0 || c.Limit > widgetMaxListLimit {
		return ErrDashboardInvalid{Param: path + ".limit", Reason: fmt.Sprintf("must be between 0 and %d", widgetMaxListLimit)}
	}
	if len(c.Columns) > widgetMaxColumns {
		return ErrDashboardInvalid{Param: path + ".columns", Reason: fmt.Sprintf("at most %d columns", widgetMaxColumns)}
	}
	if len(c.OrderBy) > len(c.SortBy) {
		return ErrDashboardInvalid{Param: path + ".order_by", Reason: "has more entries than sort_by"}
	}
	for i, field := range c.SortBy {
		param := &sortParam{sortBy: field, orderBy: orderAscending}
		if i < len(c.OrderBy) {
			if c.OrderBy[i] != "asc" && c.OrderBy[i] != "desc" {
				return ErrDashboardInvalid{Param: fmt.Sprintf("%s.order_by[%d]", path, i), Reason: "must be asc or desc"}
			}
		}
		if err := param.validate(); err != nil {
			return ErrDashboardInvalid{Param: fmt.Sprintf("%s.sort_by[%d]", path, i), Reason: reportErrorMessage(err)}
		}
	}
	return nil
}
