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

type formListBody struct {
	Body Paginated[*models.Form]
}

type formSubmissionListBody struct {
	Body Paginated[*models.FormSubmission]
}

type formReadBody struct {
	models.Form
	MaxPermission models.Permission `json:"max_permission" readOnly:"true" doc:"The maximum permission the requesting user has on the project of this form (always 2: only project admins can read forms)."`
}

// RegisterFormRoutes wires the form administration onto the Huma API. The public side lives in forms_public.go.
func RegisterFormRoutes(api huma.API) {
	tags := []string{"forms"}

	Register(api, huma.Operation{
		OperationID: "forms-options",
		Summary:     "List what a form of the project can ask for",
		Description: "Returns the targets a field of a form can have in this project: the built-in ones and the custom fields with their type, config and options, and whether a public form may ask for each (public_visible) or preset it as a hidden field (public_hidden). The form builder is generated from it. Needs admin access to the project.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/forms/options",
		Tags:        tags,
	}, formsOptions)

	Register(api, huma.Operation{
		OperationID: "forms-list",
		Summary:     "List the forms of a project",
		Description: "Returns the forms of a project in creation order, with their secret tokens. Needs admin access to the project; link shares have no access.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/forms",
		Tags:        tags,
	}, formsList)

	Register(api, huma.Operation{
		OperationID: "forms-read",
		Summary:     "Get a form",
		Description: "Returns one form. Needs admin access to the project. Sends an ETag; pass it as If-None-Match on a later read to get a 304 Not Modified.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/forms/{form}",
		Tags:        tags,
	}, formsRead)

	Register(api, huma.Operation{
		OperationID: "forms-create",
		Summary:     "Create a form",
		Description: "Creates a form and generates its token. The fields and settings are validated against the project and the problems are returned together as 400 with code 25002 and the path of each one, e.g. fields[2].target. The tasks of the form are created as you; the form is disabled when you can no longer write to the project. Needs admin access to the project.",
		Method:      http.MethodPost,
		Path:        "/projects/{project}/forms",
		Tags:        tags,
	}, formsCreate)

	Register(api, huma.Operation{
		OperationID: "forms-update",
		Summary:     "Update a form",
		Description: "Replaces the editable fields of a form, validated like on create. You become the owner the tasks are created as. Saving clears a reason the system stored when it disabled the form; send enabled=true to turn it on again. The token stays. Needs admin access to the project. PUT replaces all editable fields; use PATCH for a partial update.",
		Method:      http.MethodPut,
		Path:        "/projects/{project}/forms/{form}",
		Tags:        tags,
	}, formsUpdate)

	Register(api, huma.Operation{
		OperationID: "forms-delete",
		Summary:     "Delete a form",
		Description: "Deletes a form with its submission records. The tasks it created are kept. Needs admin access to the project.",
		Method:      http.MethodDelete,
		Path:        "/projects/{project}/forms/{form}",
		Tags:        tags,
	}, formsDelete)

	Register(api, huma.Operation{
		OperationID: "forms-token",
		Summary:     "Regenerate the token of a form",
		Description: "Replaces the secret token. The old link stops working immediately. Needs admin access to the project.",
		Method:      http.MethodPost,
		Path:        "/projects/{project}/forms/{form}/token",
		Tags:        tags,
	}, formsToken)

	Register(api, huma.Operation{
		OperationID: "form-submissions-list",
		Summary:     "List the submissions of a form",
		Description: "Returns the submissions of a form, newest first: the created task, the submitter and the time. The IP hash is never returned. Needs admin access to the project.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/forms/{form}/submissions",
		Tags:        tags,
	}, formSubmissionsList)
}

func init() { AddRouteRegistrar(RegisterFormRoutes) }

func formsOptions(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
}) (*singleBody[models.FormOptions], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	opts := &models.FormOptions{ProjectID: in.ProjectID}
	if _, err = handler.DoReadOne(ctx, opts, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.FormOptions]{Body: opts}, nil
}

func formsList(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ListParams
}) (*formListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.Form{ProjectID: in.ProjectID}, a, in.Q, in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.Form)
	if !ok {
		return nil, fmt.Errorf("forms.ReadAll returned unexpected type %T (expected []*models.Form)", result)
	}
	return &formListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

func formsRead(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"form"`
	conditional.Params
}) (*singleReadBody[formReadBody], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	form := &models.Form{ID: in.ID, ProjectID: in.ProjectID}
	maxPermission, err := handler.DoReadOne(ctx, form, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body := &formReadBody{Form: *form, MaxPermission: models.Permission(maxPermission)}
	return conditionalReadResponse(&in.Params, body, form.Updated, maxPermission)
}

func formsCreate(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	Body      models.Form
}) (*singleBody[models.Form], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.ProjectID = in.ProjectID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.Form]{Body: &in.Body}, nil
}

// Body matches the read shape so AutoPatch's GET→PUT echo of max_permission validates.
func formsUpdate(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"form"`
	Body      formReadBody
}) (*singleBody[models.Form], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	form := &in.Body.Form
	form.ID = in.ID // URL wins over body
	form.ProjectID = in.ProjectID
	if err := handler.DoUpdate(ctx, form, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.Form]{Body: form}, nil
}

func formsDelete(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"form"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.Form{ID: in.ID, ProjectID: in.ProjectID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}

func formsToken(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"form"`
}) (*singleBody[models.FormToken], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	t := &models.FormToken{ID: in.ID, ProjectID: in.ProjectID}
	if err := handler.DoCreate(ctx, t, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.FormToken]{Body: t}, nil
}

func formSubmissionsList(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"form"`
	PageParams
}) (*formSubmissionListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.FormSubmission{FormID: in.ID, ProjectID: in.ProjectID}, a, "", in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.FormSubmission)
	if !ok {
		return nil, fmt.Errorf("formSubmissions.ReadAll returned unexpected type %T (expected []*models.FormSubmission)", result)
	}
	return &formSubmissionListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}
