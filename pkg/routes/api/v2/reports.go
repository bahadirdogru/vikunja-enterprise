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

package apiv2

import (
	"context"
	"net/http"
	"strconv"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
)

// csvResponses documents the CSV body of the export routes.
func csvResponses(description string) map[string]*huma.Response {
	return map[string]*huma.Response{
		"200": {
			Description: description,
			Content: map[string]*huma.MediaType{
				"text/csv": {Schema: &huma.Schema{Type: huma.TypeString}},
			},
		},
	}
}

// csvStream writes a finished CSV file. The bytes are computed before the stream starts, so every error is still a
// regular error response.
func csvStream(filename string, body []byte) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		c := humaecho.Unwrap(hctx)
		resp := (*c).Response()
		resp.Header().Set("Content-Type", "text/csv; charset=utf-8")
		resp.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
		resp.Header().Set("Cache-Control", "no-store")
		resp.Header().Set("X-Content-Type-Options", "nosniff")
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.Write(body)
	}}
}

// RegisterReportRoutes wires the report endpoints onto the Huma API.
func RegisterReportRoutes(api huma.API) {
	tags := []string{"reports"}

	Register(api, huma.Operation{
		OperationID: "reports-tasks",
		Summary:     "Aggregate tasks",
		Description: "Counts tasks or sums, averages and finds the smallest and largest value of a numeric field (percent_done, time_spent, or a number, currency, rating or progress custom field), optionally grouped by up to two dimensions and optionally as a time series. The tasks are the ones the task list would show for the same project and filter: a report can never include a task you cannot read. A task that has several values of a dimension (assignee, label, multi select, user field) is counted in each of its groups, multi_valued says so and total is the number of distinct tasks. Tasks without a value form the none group (key null). Only the largest groups (limit, 20 by default) are kept, the rest is folded into one other group. For a time series, days, weeks (starting Monday) and months are computed in the time zone of the request (default: your time zone setting); empty buckets are returned with 0 and tasks without the date are counted in excluded. time_spent sums finished time entries and ignores running timers; it is not available through link shares. Results are cached for 30 seconds per user. Needs no permission beyond read access to the project, if one is given.",
		Method:      http.MethodPost,
		Path:        "/reports/tasks",
		// Computes a result, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, reportsTasks)

	Register(api, huma.Operation{
		OperationID: "reports-tasks-export",
		Summary:     "Export an aggregate report as CSV",
		Description: "Runs the same report as POST /reports/tasks (same permissions, the same tasks, the same limits) and returns the groups, the time buckets or the single figure as a CSV file: UTF-8 with a byte order mark so Excel reads it correctly, comma separated, cells that start with =, +, - or @ are prefixed with a single quote so a spreadsheet does not run them. One row per leaf group (and bucket).",
		Method:      http.MethodPost,
		Path:        "/reports/tasks/export",
		// Computes a file, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Responses:     csvResponses("The report as a CSV file."),
	}, reportsTasksExport)

	Register(api, huma.Operation{
		OperationID: "reports-tasks-export-rows",
		Summary:     "Export the tasks behind a report as CSV",
		Description: "Returns the tasks of a project scope and filter (what the drill-down of a widget lists) as a CSV file: the core columns (id, identifier, title, project, status, done, priority, percent done, dates, assignees, labels, created, created by) and one column per custom field that has a value on an exported task. The tasks are the ones the task list would show for the same project and filter: nothing you cannot read is exported. At most reports.maxrows tasks (50000 by default), more is a typed 400 (23010). Same file format as the aggregate export.",
		Method:      http.MethodPost,
		Path:        "/reports/tasks/export/rows",
		// Computes a file, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Responses:     csvResponses("The tasks as a CSV file."),
	}, reportsTasksExportRows)

	Register(api, huma.Operation{
		OperationID: "reports-status-time",
		Summary:     "Time tasks spent in each status",
		Description: "From the status history of a project: per workflow status the number of tasks, how often it was entered, and the total, average, median and longest time per task. A visit that has not ended counts until now (or the end of the window). Optionally split by assignee or label. Only tasks you can read are counted (same scope and filter as a task list). The history is read in memory; more than reports.maxrows rows is a typed 400 (23010).",
		Method:      http.MethodPost,
		Path:        "/reports/status-time",
		// Computes a result, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, reportsStatusTime)

	Register(api, huma.Operation{
		OperationID: "reports-status-time-export",
		Summary:     "Export the time in status report as CSV",
		Description: "The same report as POST /reports/status-time as a CSV file, one row per status (and group).",
		Method:      http.MethodPost,
		Path:        "/reports/status-time/export",
		// Computes a file, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Responses:     csvResponses("The report as a CSV file."),
	}, reportsStatusTimeExport)

	Register(api, huma.Operation{
		OperationID: "reports-workload",
		Summary:     "Open tasks per assignee",
		Description: "Counts the open tasks (done = false) you can read per assignee, split by due date: overdue, this week (until Monday), next week, later and without a due date. Tasks nobody is assigned to form their own row. Optionally adds up a numeric custom field (an estimate) per assignee and bucket. A task with several assignees counts for each of them, totals counts it once. Same scope and filter as a task list.",
		Method:      http.MethodPost,
		Path:        "/reports/workload",
		// Computes a result, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, reportsWorkload)

	Register(api, huma.Operation{
		OperationID: "reports-workload-export",
		Summary:     "Export the workload report as CSV",
		Description: "The same report as POST /reports/workload as a CSV file, one row per assignee.",
		Method:      http.MethodPost,
		Path:        "/reports/workload/export",
		// Computes a file, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Responses:     csvResponses("The report as a CSV file."),
	}, reportsWorkloadExport)

	registerGated(api, huma.Operation{
		OperationID: "reports-time-tracking",
		Summary:     "Tracked time per user, project, label or day",
		Description: "Adds up the finished time entries in a date range, by user, project, label or day. The entries are the ones the time entry list shows you (entries of projects you can read, your own entries without a project); running timers are not counted. Needs the time tracking feature (404 when it is not licensed) and is not available through link shares (403, 23005). More than reports.maxrows entries is a typed 400 (23010).",
		Method:      http.MethodPost,
		Path:        "/reports/time-tracking",
		// Computes a result, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, reportsTimeTracking)

	registerGated(api, huma.Operation{
		OperationID: "reports-time-tracking-export",
		Summary:     "Export the time tracking report as CSV",
		Description: "The same report as POST /reports/time-tracking as a CSV file, one row per group.",
		Method:      http.MethodPost,
		Path:        "/reports/time-tracking/export",
		// Computes a file, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Responses:     csvResponses("The report as a CSV file."),
	}, reportsTimeTrackingExport)

	Register(api, huma.Operation{
		OperationID: "reports-tasks-options",
		Summary:     "List what a task report can use",
		Description: "Returns the dimensions, measures, numeric fields and time fields a report may use for a project (or for all projects you can read), including its custom fields by type, whether a workflow exists and the limits of this instance. Widget editors are generated from it. Only custom fields of projects you can read are listed.",
		Method:      http.MethodGet,
		Path:        "/reports/tasks/options",
		Tags:        tags,
	}, reportsTasksOptions)
}

func init() { AddRouteRegistrar(RegisterReportRoutes) }

func reportsTasks(ctx context.Context, in *struct {
	Body models.ReportRequest
}) (*singleBody[models.TaskReport], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	report, err := models.RunTaskReport(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.TaskReport]{Body: report}, nil
}

func reportsTasksOptions(ctx context.Context, in *struct {
	ProjectID int64 `query:"project_id" doc:"Only what applies to this project. Without it, every project you can read."`
}) (*singleBody[models.ReportOptions], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	options, err := models.GetReportOptions(s, a, in.ProjectID)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.ReportOptions]{Body: options}, nil
}

func reportsTasksExport(ctx context.Context, in *struct {
	Body models.ReportRequest
}) (*huma.StreamResponse, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	body, err := models.RunTaskReportCSV(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return csvStream("report.csv", body), nil
}

func reportsTasksExportRows(ctx context.Context, in *struct {
	Body models.TaskExportRequest
}) (*huma.StreamResponse, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	body, err := models.ExportTasksCSV(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return csvStream("tasks.csv", body), nil
}

func reportsStatusTime(ctx context.Context, in *struct {
	Body models.StatusTimeRequest
}) (*singleBody[models.StatusTimeReport], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	report, err := models.RunStatusTimeReport(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.StatusTimeReport]{Body: report}, nil
}

func reportsStatusTimeExport(ctx context.Context, in *struct {
	Body models.StatusTimeRequest
}) (*huma.StreamResponse, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	report, err := models.RunStatusTimeReport(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body, err := report.CSV().Bytes()
	if err != nil {
		return nil, translateDomainError(err)
	}
	return csvStream("status-time.csv", body), nil
}

func reportsWorkload(ctx context.Context, in *struct {
	Body models.WorkloadRequest
}) (*singleBody[models.WorkloadReport], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	report, err := models.RunWorkloadReport(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.WorkloadReport]{Body: report}, nil
}

func reportsWorkloadExport(ctx context.Context, in *struct {
	Body models.WorkloadRequest
}) (*huma.StreamResponse, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	report, err := models.RunWorkloadReport(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body, err := report.CSV().Bytes()
	if err != nil {
		return nil, translateDomainError(err)
	}
	return csvStream("workload.csv", body), nil
}

func reportsTimeTracking(ctx context.Context, in *struct {
	Body models.TimeTrackingRequest
}) (*singleBody[models.TimeTrackingReport], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	report, err := models.RunTimeTrackingReport(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.TimeTrackingReport]{Body: report}, nil
}

func reportsTimeTrackingExport(ctx context.Context, in *struct {
	Body models.TimeTrackingRequest
}) (*huma.StreamResponse, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	report, err := models.RunTimeTrackingReport(ctx, s, a, &in.Body)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body, err := report.CSV().Bytes()
	if err != nil {
		return nil, translateDomainError(err)
	}
	return csvStream("time-tracking.csv", body), nil
}
