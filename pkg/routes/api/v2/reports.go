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

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/danielgtaylor/huma/v2"
)

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
