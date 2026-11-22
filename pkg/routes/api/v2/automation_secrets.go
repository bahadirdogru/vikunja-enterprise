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
)

type automationSecretListBody struct {
	Body Paginated[*models.AutomationSecret]
}

type automationSecretSetRequest struct {
	Value string `json:"value" required:"true" minLength:"1" maxLength:"8192" writeOnly:"true" doc:"The secret value, at most 8 KiB. Write-only: it is never returned by any endpoint, never shown in run output and never written to the log. The previous value is replaced."`
}

// RegisterAutomationSecretRoutes registers the routes that manage the secrets of a project.
func RegisterAutomationSecretRoutes(api huma.API) {
	tags := []string{"automations"}

	Register(api, huma.Operation{
		OperationID: "automation-secrets-list",
		Summary:     "List the secrets of a project",
		Description: "Returns the names and timestamps of the secrets of a project, never their values. unreadable is true for a secret whose stored value cannot be decrypted any more because service.secret changed; set it again to fix it. Needs admin access to the project.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/automation-secrets",
		Tags:        tags,
	}, automationSecretsList)

	Register(api, huma.Operation{
		OperationID: "automation-secrets-set",
		Summary:     "Create or replace a secret",
		Description: "Stores a value under a name (letters, digits and underscores, not starting with a digit, at most 64 characters), creating the secret or replacing its value. Scripts read it with secrets.get(name); an http.request action uses it as {{secrets.NAME}} in header values and the body and as the signing key. The value is encrypted at rest and cannot be read back. Needs admin access to the project.",
		Method:      http.MethodPut,
		Path:        "/projects/{project}/automation-secrets/{name}",
		// Create and replace share one operation.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, automationSecretsSet)

	Register(api, huma.Operation{
		OperationID: "automation-secrets-delete",
		Summary:     "Delete a secret",
		Description: "Deletes a secret. Rules that use it keep their definition and fail at the step that needs it. Needs admin access to the project.",
		Method:      http.MethodDelete,
		Path:        "/projects/{project}/automation-secrets/{name}",
		Tags:        tags,
	}, automationSecretsDelete)
}

func init() { AddRouteRegistrar(RegisterAutomationSecretRoutes) }

func automationSecretsList(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	PageParams
}) (*automationSecretListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.AutomationSecret{ProjectID: in.ProjectID}, a, "", in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.AutomationSecret)
	if !ok {
		return nil, fmt.Errorf("automationSecrets.ReadAll returned unexpected type %T (expected []*models.AutomationSecret)", result)
	}
	return &automationSecretListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

func automationSecretsSet(ctx context.Context, in *struct {
	ProjectID int64  `path:"project"`
	Name      string `path:"name" maxLength:"64" doc:"The name of the secret."`
	Body      automationSecretSetRequest
}) (*singleBody[models.AutomationSecret], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	secret := &models.AutomationSecret{ProjectID: in.ProjectID, Name: in.Name, NewValue: in.Body.Value}
	if err := handler.DoUpdate(ctx, secret, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.AutomationSecret]{Body: secret}, nil
}

func automationSecretsDelete(ctx context.Context, in *struct {
	ProjectID int64  `path:"project"`
	Name      string `path:"name" maxLength:"64" doc:"The name of the secret."`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.AutomationSecret{ProjectID: in.ProjectID, Name: in.Name}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}
