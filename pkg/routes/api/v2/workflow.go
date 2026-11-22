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

type taskStatusHistoryListBody struct {
	Body Paginated[*models.TaskStatusHistory]
}

// RegisterWorkflowRoutes wires the project workflow document and the per-task statuses onto the Huma API.
func RegisterWorkflowRoutes(api huma.API) {
	tags := []string{"workflow"}

	Register(api, huma.Operation{
		OperationID: "workflow-read",
		Summary:     "Get the workflow of a project",
		Description: "Returns the whole workflow as one document: statuses (archived ones included) in position order and the transition rules with their required fields and teams. Requires read access to the project; 404 when the project has no workflow. Sends an ETag; pass it as If-None-Match on a later read to get a 304 Not Modified.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/workflow",
		Tags:        tags,
	}, workflowRead)

	Register(api, huma.Operation{
		OperationID: "workflow-replace",
		Summary:     "Create or replace the workflow of a project",
		Description: "Creates the workflow of a project or replaces the stored one by the submitted document, in one transaction. Requires admin access to the project. Statuses with an id are updated, statuses without one are created (a negative id lets transitions and move_tasks_to of the same document refer to them) and statuses left out are deleted, or archived when tasks moved through them. Removing a status that still has tasks needs move_tasks_to. Exactly one non-archived status must be initial, at least one must have the category done, transitions may only reference statuses of the document and required fields are built-in names or cf_<id> of a custom field of the project. When a workflow is created for a project with tasks, open tasks get the initial status and done tasks the first done status. PUT replaces all editable fields; use PATCH for a partial update.",
		Method:      http.MethodPut,
		Path:        "/projects/{project}/workflow",
		Tags:        tags,
	}, workflowReplace)

	Register(api, huma.Operation{
		OperationID: "workflow-delete",
		Summary:     "Delete the workflow of a project",
		Description: "Deletes the workflow with its statuses and transitions and clears the status of every task. Tasks keep their done flag and the status history stays readable. Requires admin access to the project.",
		Method:      http.MethodDelete,
		Path:        "/projects/{project}/workflow",
		Tags:        tags,
	}, workflowDelete)

	Register(api, huma.Operation{
		OperationID: "task-status-read",
		Summary:     "Get the status of a task",
		Description: "Returns the current status of a task and the status changes the requesting user may perform right now, each with the requirements the task does not meet yet. Requires read access to the task; 404 when its project has no workflow.",
		Method:      http.MethodGet,
		Path:        "/tasks/{task}/status",
		Tags:        tags,
	}, taskStatusRead)

	Register(api, huma.Operation{
		OperationID: "task-status-change",
		Summary:     "Change the status of a task",
		Description: "Moves the task to another status. Checks run in order: the status exists and is not archived, the workflow allows the change, the user has the permission and team of the transition, then required fields and comment. Failing requirements return 422 with code 21007 and a details object listing missing_fields and comment_required. Entering a done or cancelled status marks the task done, leaving it clears the flag; a repeating task that enters a done status re-opens in the initial status and both changes are recorded. Requires write access to the task.",
		Method:      http.MethodPost,
		Path:        "/tasks/{task}/status",
		// Performs an action on the task, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, taskStatusChange)

	Register(api, huma.Operation{
		OperationID: "task-status-history-list",
		Summary:     "List the status history of a task",
		Description: "Returns every status change of the task, newest first. Entries carry the status titles as they were when the change happened. Requires read access to the task.",
		Method:      http.MethodGet,
		Path:        "/tasks/{task}/status/history",
		Tags:        tags,
	}, taskStatusHistoryList)
}

func init() { AddRouteRegistrar(RegisterWorkflowRoutes) }

func workflowRead(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	conditional.Params
}) (*singleReadBody[models.Workflow], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	wf := &models.Workflow{ProjectID: in.ProjectID}
	maxPermission, err := handler.DoReadOne(ctx, wf, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	// The body is the model itself: embedding it in a wrapper to add max_permission breaks Huma's $schema link,
	// because the model does not implement every method of its embedded CRUD interfaces.
	return conditionalReadResponse(&in.Params, wf, wf.Updated, maxPermission)
}

func workflowReplace(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	Body      models.Workflow
}) (*singleBody[models.Workflow], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	wf := &in.Body
	wf.ProjectID = in.ProjectID // URL wins over body
	if err := handler.DoUpdate(ctx, wf, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.Workflow]{Body: wf}, nil
}

func workflowDelete(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.Workflow{ProjectID: in.ProjectID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}

func taskStatusRead(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
}) (*singleBody[models.TaskWorkflowStatus], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	status := &models.TaskWorkflowStatus{TaskID: in.TaskID}
	if _, err := handler.DoReadOne(ctx, status, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.TaskWorkflowStatus]{Body: status}, nil
}

func taskStatusChange(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	Body   models.TaskStatusChange
}) (*singleBody[models.TaskStatusChange], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.TaskID = in.TaskID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.TaskStatusChange]{Body: &in.Body}, nil
}

func taskStatusHistoryList(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	PageParams
}) (*taskStatusHistoryListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.TaskStatusHistory{TaskID: in.TaskID}, a, "", in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.TaskStatusHistory)
	if !ok {
		return nil, fmt.Errorf("taskStatusHistory.ReadAll returned unexpected type %T (expected []*models.TaskStatusHistory)", result)
	}
	return &taskStatusHistoryListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}
