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
	"errors"
	"fmt"
	"net/http"

	"code.vikunja.io/api/pkg/web"
)

// ReportErrorDetails is the machine-readable body of the report and dashboard errors that blame a request part.
type ReportErrorDetails struct {
	Param string `json:"param" doc:"The offending request parameter, e.g. group_by, time.from or widgets[1].config.request.measure."`
}

// ErrReportInvalidParameter represents an error where a parameter of a report request is not valid.
type ErrReportInvalidParameter struct {
	Param  string
	Reason string
}

// IsErrReportInvalidParameter checks if an error is ErrReportInvalidParameter.
func IsErrReportInvalidParameter(err error) bool {
	_, ok := errors.AsType[ErrReportInvalidParameter](err)
	return ok
}

func (err ErrReportInvalidParameter) Error() string {
	return fmt.Sprintf("Report parameter is invalid [Param: %s, Reason: %s]", err.Param, err.Reason)
}

// ErrCodeReportInvalidParameter holds the unique world-error code of this error
const ErrCodeReportInvalidParameter = 23001

// HTTPError holds the http error description
func (err ErrReportInvalidParameter) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeReportInvalidParameter,
		Message:  fmt.Sprintf("The report parameter '%s' is invalid: %s", err.Param, err.Reason),
		Details:  ReportErrorDetails{Param: err.Param},
	}
}

// ErrReportTooManyBuckets represents an error where a time series would have more buckets than allowed.
type ErrReportTooManyBuckets struct {
	Buckets int
	Max     int
}

// IsErrReportTooManyBuckets checks if an error is ErrReportTooManyBuckets.
func IsErrReportTooManyBuckets(err error) bool {
	_, ok := errors.AsType[ErrReportTooManyBuckets](err)
	return ok
}

func (err ErrReportTooManyBuckets) Error() string {
	return fmt.Sprintf("Report has too many time buckets [Buckets: %d, Max: %d]", err.Buckets, err.Max)
}

// ErrCodeReportTooManyBuckets holds the unique world-error code of this error
const ErrCodeReportTooManyBuckets = 23002

// HTTPError holds the http error description
func (err ErrReportTooManyBuckets) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeReportTooManyBuckets,
		Message:  fmt.Sprintf("The time range would produce at least %d buckets, at most %d are allowed. Narrow time.from/time.to or use a larger time.bucket.", err.Buckets, err.Max),
		Details:  ReportErrorDetails{Param: "time"},
	}
}

// ErrReportTooManyGroups represents an error where a report asks for more groups than allowed.
type ErrReportTooManyGroups struct {
	Limit int
	Max   int
}

// IsErrReportTooManyGroups checks if an error is ErrReportTooManyGroups.
func IsErrReportTooManyGroups(err error) bool {
	_, ok := errors.AsType[ErrReportTooManyGroups](err)
	return ok
}

func (err ErrReportTooManyGroups) Error() string {
	return fmt.Sprintf("Report asks for too many groups [Limit: %d, Max: %d]", err.Limit, err.Max)
}

// ErrCodeReportTooManyGroups holds the unique world-error code of this error
const ErrCodeReportTooManyGroups = 23003

// HTTPError holds the http error description
func (err ErrReportTooManyGroups) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeReportTooManyGroups,
		Message:  fmt.Sprintf("A report returns at most %d groups per level, %d were requested.", err.Max, err.Limit),
		Details:  ReportErrorDetails{Param: "limit"},
	}
}

// ErrReportFieldKindNotSupported represents an error where a field cannot be used the way the report asks for.
type ErrReportFieldKindNotSupported struct {
	Param string
	Field string
	Kind  string
}

// IsErrReportFieldKindNotSupported checks if an error is ErrReportFieldKindNotSupported.
func IsErrReportFieldKindNotSupported(err error) bool {
	_, ok := errors.AsType[ErrReportFieldKindNotSupported](err)
	return ok
}

func (err ErrReportFieldKindNotSupported) Error() string {
	return fmt.Sprintf("Report field kind is not supported [Param: %s, Field: %s, Kind: %s]", err.Param, err.Field, err.Kind)
}

// ErrCodeReportFieldKindNotSupported holds the unique world-error code of this error
const ErrCodeReportFieldKindNotSupported = 23004

// HTTPError holds the http error description
func (err ErrReportFieldKindNotSupported) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeReportFieldKindNotSupported,
		Message:  fmt.Sprintf("The field '%s' (type %s) cannot be used for '%s'.", err.Field, err.Kind, err.Param),
		Details:  ReportErrorDetails{Param: err.Param},
	}
}

// ErrReportNotAvailableForShare represents an error where a report part is not available to link shares.
type ErrReportNotAvailableForShare struct {
	Param string
}

// IsErrReportNotAvailableForShare checks if an error is ErrReportNotAvailableForShare.
func IsErrReportNotAvailableForShare(err error) bool {
	_, ok := errors.AsType[ErrReportNotAvailableForShare](err)
	return ok
}

func (err ErrReportNotAvailableForShare) Error() string {
	return fmt.Sprintf("Report part is not available for link shares [Param: %s]", err.Param)
}

// ErrCodeReportNotAvailableForShare holds the unique world-error code of this error
const ErrCodeReportNotAvailableForShare = 23005

// HTTPError holds the http error description
func (err ErrReportNotAvailableForShare) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusForbidden,
		Code:     ErrCodeReportNotAvailableForShare,
		Message:  fmt.Sprintf("'%s' is not available through a link share.", err.Param),
		Details:  ReportErrorDetails(err),
	}
}

// ErrDashboardDoesNotExist represents an error where a dashboard does not exist.
type ErrDashboardDoesNotExist struct {
	DashboardID int64
}

// IsErrDashboardDoesNotExist checks if an error is ErrDashboardDoesNotExist.
func IsErrDashboardDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrDashboardDoesNotExist](err)
	return ok
}

func (err ErrDashboardDoesNotExist) Error() string {
	return fmt.Sprintf("Dashboard does not exist [DashboardID: %d]", err.DashboardID)
}

// ErrCodeDashboardDoesNotExist holds the unique world-error code of this error
const ErrCodeDashboardDoesNotExist = 23006

// HTTPError holds the http error description
func (err ErrDashboardDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeDashboardDoesNotExist,
		Message:  "This dashboard does not exist.",
	}
}

// ErrDashboardWidgetDoesNotExist represents an error where a widget is not part of the dashboard.
type ErrDashboardWidgetDoesNotExist struct {
	DashboardID int64
	WidgetID    int64
}

// IsErrDashboardWidgetDoesNotExist checks if an error is ErrDashboardWidgetDoesNotExist.
func IsErrDashboardWidgetDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrDashboardWidgetDoesNotExist](err)
	return ok
}

func (err ErrDashboardWidgetDoesNotExist) Error() string {
	return fmt.Sprintf("Dashboard widget does not exist [DashboardID: %d, WidgetID: %d]", err.DashboardID, err.WidgetID)
}

// ErrCodeDashboardWidgetDoesNotExist holds the unique world-error code of this error
const ErrCodeDashboardWidgetDoesNotExist = 23007

// HTTPError holds the http error description
func (err ErrDashboardWidgetDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeDashboardWidgetDoesNotExist,
		Message:  "This widget is not part of the dashboard.",
	}
}

// ErrDashboardInvalid represents an error where a dashboard or one of its widgets is not valid.
type ErrDashboardInvalid struct {
	Param  string
	Reason string
}

// IsErrDashboardInvalid checks if an error is ErrDashboardInvalid.
func IsErrDashboardInvalid(err error) bool {
	_, ok := errors.AsType[ErrDashboardInvalid](err)
	return ok
}

func (err ErrDashboardInvalid) Error() string {
	return fmt.Sprintf("Dashboard is invalid [Param: %s, Reason: %s]", err.Param, err.Reason)
}

// ErrCodeDashboardInvalid holds the unique world-error code of this error
const ErrCodeDashboardInvalid = 23008

// HTTPError holds the http error description
func (err ErrDashboardInvalid) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeDashboardInvalid,
		Message:  fmt.Sprintf("The dashboard is invalid at '%s': %s", err.Param, err.Reason),
		Details:  ReportErrorDetails{Param: err.Param},
	}
}

// ErrReportTimeout represents an error where the queries of a report took longer than allowed.
type ErrReportTimeout struct{}

// IsErrReportTimeout checks if an error is ErrReportTimeout.
func IsErrReportTimeout(err error) bool {
	_, ok := errors.AsType[ErrReportTimeout](err)
	return ok
}

func (err ErrReportTimeout) Error() string {
	return "Report timed out"
}

// ErrCodeReportTimeout holds the unique world-error code of this error
const ErrCodeReportTimeout = 23009

// HTTPError holds the http error description
func (err ErrReportTimeout) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusServiceUnavailable,
		Code:     ErrCodeReportTimeout,
		Message:  "The report took too long. Narrow the filter or the time range.",
	}
}

// ErrReportTooManyRows represents an error where a report or export would have to read more rows than allowed.
type ErrReportTooManyRows struct {
	Rows int64
	Max  int
}

// IsErrReportTooManyRows checks if an error is ErrReportTooManyRows.
func IsErrReportTooManyRows(err error) bool {
	_, ok := err.(ErrReportTooManyRows)
	return ok
}

func (err ErrReportTooManyRows) Error() string {
	return fmt.Sprintf("Report would read too many rows [Rows: %d, Max: %d]", err.Rows, err.Max)
}

// ErrCodeReportTooManyRows holds the unique world-error code of this error
const ErrCodeReportTooManyRows = 23010

// HTTPError holds the http error description
func (err ErrReportTooManyRows) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeReportTooManyRows,
		Message:  fmt.Sprintf("This would need more than the allowed %d rows. Narrow the filter, the project or the time range.", err.Max),
	}
}
