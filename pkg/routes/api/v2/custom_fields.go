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

type customFieldListBody struct {
	Body Paginated[*models.CustomField]
}

type customFieldValueListBody struct {
	Body Paginated[*models.CustomFieldValue]
}

type customFieldReadBody struct {
	models.CustomField
	MaxPermission models.Permission `json:"max_permission" readOnly:"true" doc:"The maximum permission the requesting user has on the project of this field (0=read, 1=read/write, 2=admin)."`
}

// RegisterCustomFieldRoutes wires the custom field definitions and the per-task values onto the Huma API.
func RegisterCustomFieldRoutes(api huma.API) {
	tags := []string{"custom-fields"}

	Register(api, huma.Operation{
		OperationID: "custom-fields-list",
		Summary:     "List the custom fields of a project",
		Description: "Returns the custom field definitions of a project in their position order, with their options embedded. Requires read access to the project.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/custom-fields",
		Tags:        tags,
	}, customFieldsList)

	Register(api, huma.Operation{
		OperationID: "custom-fields-read",
		Summary:     "Get a custom field",
		Description: "Returns one custom field definition of a project. Sends an ETag; pass it as If-None-Match on a later read to get a 304 Not Modified.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/custom-fields/{field}",
		Tags:        tags,
	}, customFieldsRead)

	Register(api, huma.Operation{
		OperationID: "custom-fields-create",
		Summary:     "Create a custom field",
		Description: "Creates a custom field in a project, with its options for select and multi_select. Requires admin access to the project.",
		Method:      http.MethodPost,
		Path:        "/projects/{project}/custom-fields",
		Tags:        tags,
	}, customFieldsCreate)

	Register(api, huma.Operation{
		OperationID: "custom-fields-update",
		Summary:     "Update a custom field",
		Description: "Updates a custom field. Requires admin access to the project. The type cannot change. The submitted options replace the stored ones: options with an id are updated, options without one are created and options left out are deleted together with every value that references them. PUT replaces all editable fields; use PATCH for a partial update.",
		Method:      http.MethodPut,
		Path:        "/projects/{project}/custom-fields/{field}",
		Tags:        tags,
	}, customFieldsUpdate)

	Register(api, huma.Operation{
		OperationID: "custom-fields-delete",
		Summary:     "Delete a custom field",
		Description: "Deletes a custom field with its options and all task values stored for it. Requires admin access to the project.",
		Method:      http.MethodDelete,
		Path:        "/projects/{project}/custom-fields/{field}",
		Tags:        tags,
	}, customFieldsDelete)

	Register(api, huma.Operation{
		OperationID: "task-custom-field-values-list",
		Summary:     "List the custom field values of a task",
		Description: "Returns one entry per custom field of the task's project. Fields without a value are included with a null value so clients can render the full form. Requires read access to the task.",
		Method:      http.MethodGet,
		Path:        "/tasks/{task}/custom-field-values",
		Tags:        tags,
	}, taskCustomFieldValuesList)

	Register(api, huma.Operation{
		OperationID: "task-custom-field-values-set",
		Summary:     "Set a custom field value on a task",
		Description: "Sets the value of one custom field on a task, replacing the previous one. The JSON shape of value depends on the field type. Sending null, or an empty array for multi_select and user, clears the value. Requires write access to the task.",
		Method:      http.MethodPut,
		Path:        "/tasks/{task}/custom-field-values/{field}",
		// Sets a value in place, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, taskCustomFieldValuesSet)

	Register(api, huma.Operation{
		OperationID: "task-custom-field-values-clear",
		Summary:     "Clear a custom field value on a task",
		Description: "Removes the value of one custom field from a task. Clearing an unset value is not an error. Requires write access to the task.",
		Method:      http.MethodDelete,
		Path:        "/tasks/{task}/custom-field-values/{field}",
		Tags:        tags,
	}, taskCustomFieldValuesClear)
}

func init() { AddRouteRegistrar(RegisterCustomFieldRoutes) }

func customFieldsList(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ListParams
}) (*customFieldListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.CustomField{ProjectID: in.ProjectID}, a, in.Q, in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.CustomField)
	if !ok {
		return nil, fmt.Errorf("customFields.ReadAll returned unexpected type %T (expected []*models.CustomField)", result)
	}
	return &customFieldListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

func customFieldsRead(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"field"`
	conditional.Params
}) (*singleReadBody[customFieldReadBody], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	field := &models.CustomField{ID: in.ID, ProjectID: in.ProjectID}
	maxPermission, err := handler.DoReadOne(ctx, field, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body := &customFieldReadBody{CustomField: *field, MaxPermission: models.Permission(maxPermission)}
	return conditionalReadResponse(&in.Params, body, field.Updated, maxPermission)
}

func customFieldsCreate(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	Body      models.CustomField
}) (*singleBody[models.CustomField], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.ProjectID = in.ProjectID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.CustomField]{Body: &in.Body}, nil
}

// Body matches the read shape so AutoPatch's GET→PUT echo of max_permission validates.
func customFieldsUpdate(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"field"`
	Body      customFieldReadBody
}) (*singleBody[models.CustomField], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	field := &in.Body.CustomField
	field.ID = in.ID // URL wins over body
	field.ProjectID = in.ProjectID
	if err := handler.DoUpdate(ctx, field, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.CustomField]{Body: field}, nil
}

func customFieldsDelete(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"field"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.CustomField{ID: in.ID, ProjectID: in.ProjectID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}

func taskCustomFieldValuesList(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	PageParams
}) (*customFieldValueListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.CustomFieldValue{TaskID: in.TaskID}, a, "", in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.CustomFieldValue)
	if !ok {
		return nil, fmt.Errorf("customFieldValues.ReadAll returned unexpected type %T (expected []*models.CustomFieldValue)", result)
	}
	return &customFieldValueListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

func taskCustomFieldValuesSet(ctx context.Context, in *struct {
	TaskID  int64 `path:"task"`
	FieldID int64 `path:"field"`
	Body    models.CustomFieldValue
}) (*singleBody[models.CustomFieldValue], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.TaskID = in.TaskID // URL wins over body
	in.Body.CustomFieldID = in.FieldID
	if err := handler.DoUpdate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.CustomFieldValue]{Body: &in.Body}, nil
}

func taskCustomFieldValuesClear(ctx context.Context, in *struct {
	TaskID  int64 `path:"task"`
	FieldID int64 `path:"field"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.CustomFieldValue{TaskID: in.TaskID, CustomFieldID: in.FieldID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}
