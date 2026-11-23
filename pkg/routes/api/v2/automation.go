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

	"code.vikunja.io/api/pkg/automation"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web/handler"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/conditional"
)

type automationRuleListBody struct {
	Body Paginated[*models.AutomationRule]
}

type automationRunListBody struct {
	Body Paginated[*models.AutomationRun]
}

type automationRuleReadBody struct {
	models.AutomationRule
	MaxPermission models.Permission `json:"max_permission" readOnly:"true" doc:"The maximum permission the requesting user has on the project of this rule (always 2: only project admins can read rules)."`
}

type automationTestRequest struct {
	TaskID int64 `json:"task_id" required:"true" minimum:"1" doc:"The task to test the rule with. It has to belong to the project of the rule."`
}

// RegisterAutomationRoutes wires the automation rules, their runs and the catalog onto the Huma API.
func RegisterAutomationRoutes(api huma.API) {
	tags := []string{"automations"}

	Register(api, huma.Operation{
		OperationID: "automation-catalog",
		Summary:     "Get the automation catalog",
		Description: "Machine-readable description of everything a rule can use: triggers with their options, condition fields with the operators each supports, operators and actions with their parameters (name, kind, required, enum values). The rule editor is generated from it. Pass project_id to add the custom fields of that project as cf_<id> condition fields; this needs read access to the project. Needs no other permission.",
		Method:      http.MethodGet,
		Path:        "/automations/catalog",
		Tags:        tags,
	}, automationCatalog)

	Register(api, huma.Operation{
		OperationID: "automations-list",
		Summary:     "List the automation rules of a project",
		Description: "Returns the rules of a project in creation order. Needs admin access to the project, because rules and their runs can reveal data of other users.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/automations",
		Tags:        tags,
	}, automationsList)

	Register(api, huma.Operation{
		OperationID: "automations-read",
		Summary:     "Get an automation rule",
		Description: "Returns one rule. Needs admin access to the project. Sends an ETag; pass it as If-None-Match on a later read to get a 304 Not Modified.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/automations/{rule}",
		Tags:        tags,
	}, automationsRead)

	Register(api, huma.Operation{
		OperationID: "automations-create",
		Summary:     "Create an automation rule",
		Description: "Creates a rule. The trigger, conditions and actions are validated against the catalog and every id (status, custom field, label, project) has to exist and be usable by you; problems are returned together as 400 with code 22002 and the path of each one, e.g. actions[1].params.status_id. The rule runs as you: it can never do more than you could do by hand, and it is disabled when you stop being admin of the project. Needs admin access to the project.",
		Method:      http.MethodPost,
		Path:        "/projects/{project}/automations",
		Tags:        tags,
	}, automationsCreate)

	Register(api, huma.Operation{
		OperationID: "automations-update",
		Summary:     "Update an automation rule",
		Description: "Replaces the editable fields of a rule, validated like on create. The revision increases and you become the owner the rule runs as. Saving clears a reason the system stored when it disabled the rule; send enabled=true to turn it on again. Needs admin access to the project. PUT replaces all editable fields; use PATCH for a partial update.",
		Method:      http.MethodPut,
		Path:        "/projects/{project}/automations/{rule}",
		Tags:        tags,
	}, automationsUpdate)

	Register(api, huma.Operation{
		OperationID: "automations-delete",
		Summary:     "Delete an automation rule",
		Description: "Deletes a rule with its runs and their steps. Needs admin access to the project.",
		Method:      http.MethodDelete,
		Path:        "/projects/{project}/automations/{rule}",
		Tags:        tags,
	}, automationsDelete)

	Register(api, huma.Operation{
		OperationID: "automations-test",
		Summary:     "Test an automation rule with a task",
		Description: "Dry run: evaluates the conditions against the task as it is now and plans every action as the owner of the rule - rendering templates, checking permissions and workflow transitions - without writing anything. Placeholders that do not resolve are reported as warnings. The event is simulated, so event.* fields are empty. Needs admin access to the project.",
		Method:      http.MethodPost,
		Path:        "/projects/{project}/automations/{rule}/test",
		// Performs a check, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, automationsTest)

	Register(api, huma.Operation{
		OperationID: "automation-runs-list",
		Summary:     "List the runs of an automation rule",
		Description: "Returns the runs of a rule, newest first, without their steps. Needs admin access to the project.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/automations/{rule}/runs",
		Tags:        tags,
	}, automationRunsList)

	Register(api, huma.Operation{
		OperationID: "automation-runs-read",
		Summary:     "Get an automation run with its steps",
		Description: "Returns one run with its steps: the action, its result, error, output and duration. A run is running while it executes or waits for a retry, then success, failed, skipped (a limit or the conditions stopped it; see skip_reason) or aborted. Needs admin access to the project.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/automations/{rule}/runs/{run}",
		Tags:        tags,
	}, automationRunsRead)

	Register(api, huma.Operation{
		OperationID: "automation-runs-retry",
		Summary:     "Retry a failed automation run",
		Description: "Puts a failed run back in the queue. Steps that succeeded are not executed again; the run resumes at the first step that did not. Only runs with the status failed can be retried (409 otherwise). Needs admin access to the project.",
		Method:      http.MethodPost,
		Path:        "/projects/{project}/automations/{rule}/runs/{run}/retry",
		// Queues work, it does not create a resource.
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, automationRunsRetry)
}

func init() { AddRouteRegistrar(RegisterAutomationRoutes) }

type automationCatalogBody struct {
	Body *spec.Catalog
}

func customFieldKind(t models.CustomFieldType) (kind string, multiple bool) {
	switch t {
	case models.CustomFieldTypeNumber, models.CustomFieldTypeCurrency, models.CustomFieldTypeRating, models.CustomFieldTypeProgress:
		return spec.KindNumber, false
	case models.CustomFieldTypeDate:
		return spec.KindDate, false
	case models.CustomFieldTypeCheckbox:
		return spec.KindBool, false
	case models.CustomFieldTypeSelect:
		return spec.KindEnum, false
	case models.CustomFieldTypeMultiSelect:
		return spec.KindEnum, true
	case models.CustomFieldTypeUser:
		return spec.KindUsers, true
	case models.CustomFieldTypeText, models.CustomFieldTypeLongText, models.CustomFieldTypeURL, models.CustomFieldTypeEmail:
		return spec.KindString, false
	}
	return spec.KindString, false
}

func automationCatalog(ctx context.Context, in *struct {
	ProjectID int64 `query:"project_id" doc:"Add the custom fields of this project as condition fields."`
}) (*automationCatalogBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	catalog := spec.GetCatalog()
	catalog.Script.Limits = automation.ScriptLimitDocs()
	if in.ProjectID == 0 {
		return &automationCatalogBody{Body: catalog}, nil
	}

	s := db.NewReadSession()
	defer s.Close()
	can, _, err := (&models.Project{ID: in.ProjectID}).CanRead(s, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	if !can {
		return nil, translateDomainError(models.ErrGenericForbidden{})
	}
	fields := []*models.CustomField{}
	if err = s.Where("project_id = ?", in.ProjectID).OrderBy("position ASC, id ASC").Find(&fields); err != nil {
		return nil, translateDomainError(err)
	}
	for _, f := range fields {
		kind, multiple := customFieldKind(f.Type)
		ops := spec.OperatorsForKind(kind)
		if kind == spec.KindEnum || kind == spec.KindUsers {
			ops = spec.OperatorsForKind(spec.KindUsers)
		}
		catalog.Fields = append(catalog.Fields, &spec.FieldDef{
			Name:            fmt.Sprintf("cf_%d", f.ID),
			Kind:            kind,
			Multiple:        multiple,
			Operators:       ops,
			TitleKey:        "automation.field.custom_field",
			Title:           f.Title,
			CustomFieldType: string(f.Type),
		})
	}
	return &automationCatalogBody{Body: catalog}, nil
}

func automationsList(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ListParams
}) (*automationRuleListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.AutomationRule{ProjectID: in.ProjectID}, a, in.Q, in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.AutomationRule)
	if !ok {
		return nil, fmt.Errorf("automationRules.ReadAll returned unexpected type %T (expected []*models.AutomationRule)", result)
	}
	return &automationRuleListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

func automationsRead(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"rule"`
	conditional.Params
}) (*singleReadBody[automationRuleReadBody], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	rule := &models.AutomationRule{ID: in.ID, ProjectID: in.ProjectID}
	maxPermission, err := handler.DoReadOne(ctx, rule, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body := &automationRuleReadBody{AutomationRule: *rule, MaxPermission: models.Permission(maxPermission)}
	return conditionalReadResponse(&in.Params, body, rule.Updated, maxPermission)
}

func automationsCreate(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	Body      models.AutomationRule
}) (*singleBody[models.AutomationRule], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.ProjectID = in.ProjectID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.AutomationRule]{Body: &in.Body}, nil
}

// Body matches the read shape so AutoPatch's GET→PUT echo of max_permission validates.
func automationsUpdate(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"rule"`
	Body      automationRuleReadBody
}) (*singleBody[models.AutomationRule], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	rule := &in.Body.AutomationRule
	rule.ID = in.ID // URL wins over body
	rule.ProjectID = in.ProjectID
	if err := handler.DoUpdate(ctx, rule, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.AutomationRule]{Body: rule}, nil
}

func automationsDelete(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"rule"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.AutomationRule{ID: in.ID, ProjectID: in.ProjectID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}

func automationsTest(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	ID        int64 `path:"rule"`
	Body      automationTestRequest
}) (*singleBody[automation.DryRunResult], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewReadSession()
	defer s.Close()

	// Not a plain CRUD operation, so the permission check is ours: CanRead needs project admin and loads the rule.
	rule := &models.AutomationRule{ID: in.ID, ProjectID: in.ProjectID}
	can, _, err := rule.CanRead(s, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	if !can {
		return nil, translateDomainError(models.ErrGenericForbidden{})
	}
	result, err := automation.DryRun(ctx, rule, in.Body.TaskID)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[automation.DryRunResult]{Body: result}, nil
}

func automationRunsList(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	RuleID    int64 `path:"rule"`
	PageParams
}) (*automationRunListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.AutomationRun{ProjectID: in.ProjectID, RuleID: in.RuleID}, a, "", in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.AutomationRun)
	if !ok {
		return nil, fmt.Errorf("automationRuns.ReadAll returned unexpected type %T (expected []*models.AutomationRun)", result)
	}
	return &automationRunListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

func automationRunsRead(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	RuleID    int64 `path:"rule"`
	ID        int64 `path:"run"`
}) (*singleBody[models.AutomationRun], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	run := &models.AutomationRun{ID: in.ID, RuleID: in.RuleID, ProjectID: in.ProjectID}
	if _, err := handler.DoReadOne(ctx, run, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.AutomationRun]{Body: run}, nil
}

func automationRunsRetry(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	RuleID    int64 `path:"rule"`
	ID        int64 `path:"run"`
}) (*singleBody[models.AutomationRunRetry], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	retry := &models.AutomationRunRetry{ProjectID: in.ProjectID, RuleID: in.RuleID, RunID: in.ID}
	if err := handler.DoCreate(ctx, retry, a); err != nil {
		return nil, translateDomainError(err)
	}
	automation.Wake()
	return &singleBody[models.AutomationRunRetry]{Body: retry}, nil
}
