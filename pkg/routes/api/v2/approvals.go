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
	"code.vikunja.io/api/pkg/web"
	"code.vikunja.io/api/pkg/web/handler"

	"github.com/danielgtaylor/huma/v2"
)

type approvalListBody struct {
	Body Paginated[*models.ApprovalRequest]
}

// statusBody is a response whose HTTP status the handler picks: 200 when the action happened, 202 when it was
// turned into an approval request.
type statusBody[T any] struct {
	Status int
	Body   *T
}

// RegisterApprovalRoutes wires the approval requests onto the Huma API.
func RegisterApprovalRoutes(api huma.API) {
	tags := []string{"approvals"}

	Register(api, huma.Operation{
		OperationID: "approvals-list",
		Summary:     "List my approvals",
		Description: "Returns the approval requests the authenticated user has to decide on or has made, newest first, paginated. Only requests whose task the user can still read are listed. Every item carries the approvers, the statuses and a compact summary of the task (id, title, identifier, project), so a list needs no further requests. Link shares have no access.",
		Method:      http.MethodGet,
		Path:        "/approvals",
		Tags:        tags,
	}, approvalsList)

	Register(api, huma.Operation{
		OperationID: "approvals-read",
		Summary:     "Get an approval request",
		Description: "Returns one approval request with its approvers and their decisions. Requires read access to the task of the request. Link shares have no access.",
		Method:      http.MethodGet,
		Path:        "/approvals/{request}",
		Tags:        tags,
	}, approvalsRead)

	Register(api, huma.Operation{
		OperationID: "approvals-decide",
		Summary:     "Decide on an approval request",
		Description: "Records the decision of the authenticated user, who has to be an approver of the request who has not decided yet. A rejection needs a comment, which is also added to the task as a comment of the approver. With mode any the first decision resolves the request; with mode all every approver has to approve and one rejection rejects. An approved transition is performed by the system on behalf of the requester after all checks ran again; when they fail the request is cancelled instead (status cancelled, cancel_reason says why) and the response is still 200. Approvers who lost access to the project are dropped first and no longer count. Errors: 24002 the request is already resolved, 24003 not an approver, 24004 already decided, 24005 a rejection without comment, 24008 the requester may not decide their own request.",
		Method:      http.MethodPost,
		Path:        "/approvals/{request}/decision",
		// Records a decision on an existing request, it creates no addressable resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, approvalsDecide)

	Register(api, huma.Operation{
		OperationID: "approvals-cancel",
		Summary:     "Cancel an approval request",
		Description: "Cancels a pending request. Only the requester and project admins may; error 24002 when it is already resolved. Requests are also cancelled automatically when the task is deleted or moved, when the workflow, a status or the transition is removed, or when the task leaves the status the request was made in; cancel_reason says which.",
		Method:      http.MethodPost,
		Path:        "/approvals/{request}/cancel",
		// Acts on an existing request, it creates no addressable resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, approvalsCancel)

	Register(api, huma.Operation{
		OperationID: "task-approvals-list",
		Summary:     "List the approval requests of a task",
		Description: "Returns every approval request of the task, newest first, paginated, pending ones included. Requires read access to the task. Link shares have no access.",
		Method:      http.MethodGet,
		Path:        "/tasks/{task}/approvals",
		Tags:        tags,
	}, taskApprovalsList)

	Register(api, huma.Operation{
		OperationID: "task-approvals-create",
		Summary:     "Request an approval for a task",
		Description: "Creates an ad-hoc approval request: a recorded decision on the task that changes no status. Needs a title and approver_ids, users who can read the task; the requester cannot be one of them. mode is any (first decision wins, default) or all. Requires write access to the task. Link shares have no access. Requests that come from a workflow transition are created by POST /tasks/{task}/status, not here.",
		Method:      http.MethodPost,
		Path:        "/tasks/{task}/approvals",
		Tags:        tags,
	}, taskApprovalsCreate)
}

func init() { AddRouteRegistrar(RegisterApprovalRoutes) }

func approvalsList(ctx context.Context, in *struct {
	Status    string `query:"status" enum:"pending,approved,rejected,cancelled" doc:"Only requests in this state."`
	Role      string `query:"role" enum:"approver,requester" doc:"approver: requests you have to decide on. requester: requests you made. Both when omitted."`
	ProjectID int64  `query:"project_id" doc:"Only requests of this project."`
	PageParams
}) (*approvalListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	filter := &models.ApprovalRequest{FilterStatus: in.Status, FilterRole: in.Role, FilterProjectID: in.ProjectID}
	return approvalListResponse(ctx, filter, a, in.Page, in.PerPage)
}

func approvalListResponse(ctx context.Context, filter *models.ApprovalRequest, a web.Auth, page, perPage int) (*approvalListBody, error) {
	result, _, total, err := handler.DoReadAll(ctx, filter, a, "", page, perPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.ApprovalRequest)
	if !ok {
		return nil, fmt.Errorf("approvalRequest.ReadAll returned unexpected type %T (expected []*models.ApprovalRequest)", result)
	}
	return &approvalListBody{Body: NewPaginated(items, total, page, perPage)}, nil
}

func approvalsRead(ctx context.Context, in *struct {
	ID int64 `path:"request"`
}) (*singleBody[models.ApprovalRequest], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	req := &models.ApprovalRequest{ID: in.ID}
	if _, err := handler.DoReadOne(ctx, req, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.ApprovalRequest]{Body: req}, nil
}

func approvalsDecide(ctx context.Context, in *struct {
	ID   int64 `path:"request"`
	Body models.ApprovalDecision
}) (*singleBody[models.ApprovalDecision], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.RequestID = in.ID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.ApprovalDecision]{Body: &in.Body}, nil
}

func approvalsCancel(ctx context.Context, in *struct {
	ID   int64 `path:"request"`
	Body models.ApprovalCancel
}) (*singleBody[models.ApprovalCancel], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.RequestID = in.ID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.ApprovalCancel]{Body: &in.Body}, nil
}

func taskApprovalsList(ctx context.Context, in *struct {
	TaskID int64  `path:"task"`
	Status string `query:"status" enum:"pending,approved,rejected,cancelled" doc:"Only requests in this state."`
	PageParams
}) (*approvalListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	filter := &models.ApprovalRequest{TaskID: in.TaskID, FilterStatus: in.Status}
	return approvalListResponse(ctx, filter, a, in.Page, in.PerPage)
}

func taskApprovalsCreate(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	Body   models.ApprovalRequest
}) (*singleBody[models.ApprovalRequest], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.TaskID = in.TaskID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.ApprovalRequest]{Body: &in.Body}, nil
}
