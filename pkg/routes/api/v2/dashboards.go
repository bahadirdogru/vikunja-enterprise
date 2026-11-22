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
	"fmt"
	"net/http"

	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web/handler"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/conditional"
)

type dashboardListBody struct {
	Body Paginated[*models.Dashboard]
}

type dashboardReadBody struct {
	models.Dashboard
	MaxPermission models.Permission `json:"max_permission" readOnly:"true" doc:"The maximum permission the requesting user has on this dashboard: 2 for the owner of a personal dashboard and for admins of the project, 0 for readers of a project dashboard (1 for write members, who cannot change it)."`
}

// RegisterDashboardRoutes wires dashboards and their layout onto the Huma API.
func RegisterDashboardRoutes(api huma.API) {
	tags := []string{"dashboards"}

	Register(api, huma.Operation{
		OperationID: "dashboards-list",
		Summary:     "List dashboards",
		Description: "Returns your personal dashboards and the dashboards of every project you can read, without their widgets (widget_count tells how many there are), ordered by position. Pass project_id to see the dashboards of one project.",
		Method:      http.MethodGet,
		Path:        "/dashboards",
		Tags:        tags,
	}, dashboardsList)

	Register(api, huma.Operation{
		OperationID: "dashboards-read",
		Summary:     "Get a dashboard",
		Description: "Returns one dashboard with all its widgets. A personal dashboard can only be read by its owner, a project dashboard by everyone who can read the project. Sends an ETag; pass it as If-None-Match on a later read to get a 304 Not Modified.",
		Method:      http.MethodGet,
		Path:        "/dashboards/{dashboard}",
		Tags:        tags,
	}, dashboardsRead)

	Register(api, huma.Operation{
		OperationID: "dashboards-create",
		Summary:     "Create a dashboard",
		Description: "Creates a personal dashboard, or a project dashboard when project_id is set (needs admin access to the project). Widgets can be sent along; every widget type and config is validated, a report inside a config is checked like POST /reports/tasks checks it but not run.",
		Method:      http.MethodPost,
		Path:        "/dashboards",
		Tags:        tags,
	}, dashboardsCreate)

	Register(api, huma.Operation{
		OperationID: "dashboards-update",
		Summary:     "Update a dashboard",
		Description: "Replaces the title, description and position and, when widgets is a list, the whole widget list in one request: widgets with the id of one of the dashboard's widgets are updated, widgets without an id are created and widgets that are missing are deleted. Omit widgets or send null to keep them. The project cannot change. A personal dashboard is changed by its owner, a project dashboard by project admins. PUT replaces all editable fields; use PATCH for a partial update.",
		Method:      http.MethodPut,
		Path:        "/dashboards/{dashboard}",
		Tags:        tags,
	}, dashboardsUpdate)

	Register(api, huma.Operation{
		OperationID: "dashboards-delete",
		Summary:     "Delete a dashboard",
		Description: "Deletes a dashboard with its widgets. A personal dashboard is deleted by its owner, a project dashboard by project admins.",
		Method:      http.MethodDelete,
		Path:        "/dashboards/{dashboard}",
		Tags:        tags,
	}, dashboardsDelete)

	Register(api, huma.Operation{
		OperationID: "dashboards-layout",
		Summary:     "Move and resize widgets",
		Description: "Stores the position and size of widgets without sending their configuration, for drag and resize. Widgets that are not listed keep theirs; a widget that is not part of the dashboard is a 404. Same permissions as updating the dashboard.",
		Method:      http.MethodPut,
		Path:        "/dashboards/{dashboard}/layout",
		Tags:        tags,
	}, dashboardsLayout)
}

func init() { AddRouteRegistrar(RegisterDashboardRoutes) }

func dashboardsList(ctx context.Context, in *struct {
	ProjectID int64 `query:"project_id" doc:"Only the dashboards of this project."`
	ListParams
}) (*dashboardListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.Dashboard{ProjectID: in.ProjectID}, a, in.Q, in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.Dashboard)
	if !ok {
		return nil, fmt.Errorf("dashboards.ReadAll returned unexpected type %T (expected []*models.Dashboard)", result)
	}
	return &dashboardListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

func dashboardsRead(ctx context.Context, in *struct {
	ID int64 `path:"dashboard"`
	conditional.Params
}) (*singleReadBody[dashboardReadBody], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	dashboard := &models.Dashboard{ID: in.ID}
	maxPermission, err := handler.DoReadOne(ctx, dashboard, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body := &dashboardReadBody{Dashboard: *dashboard, MaxPermission: models.Permission(maxPermission)}
	return conditionalReadResponse(&in.Params, body, dashboard.Updated, maxPermission)
}

func dashboardsCreate(ctx context.Context, in *struct {
	Body models.Dashboard
}) (*singleBody[models.Dashboard], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.Dashboard]{Body: &in.Body}, nil
}

// Body matches the read shape so AutoPatch's GET→PUT echo of max_permission validates.
func dashboardsUpdate(ctx context.Context, in *struct {
	ID   int64 `path:"dashboard"`
	Body dashboardReadBody
}) (*singleBody[models.Dashboard], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	dashboard := &in.Body.Dashboard
	dashboard.ID = in.ID // URL wins over body
	if err := handler.DoUpdate(ctx, dashboard, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.Dashboard]{Body: dashboard}, nil
}

func dashboardsDelete(ctx context.Context, in *struct {
	ID int64 `path:"dashboard"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.Dashboard{ID: in.ID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}

func dashboardsLayout(ctx context.Context, in *struct {
	ID   int64 `path:"dashboard"`
	Body models.DashboardLayout
}) (*singleBody[models.DashboardLayout], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.DashboardID = in.ID
	if err := handler.DoUpdate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.DashboardLayout]{Body: &in.Body}, nil
}
