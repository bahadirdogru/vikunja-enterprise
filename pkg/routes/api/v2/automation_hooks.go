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
	"errors"
	"net/http"
	"strconv"
	"time"

	"code.vikunja.io/api/pkg/automation"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/humabridge"

	"github.com/danielgtaylor/huma/v2"
)

// The route of this file is public: it opts out of the global auth (publicSecurity) and is listed in
// unauthenticatedAPIPaths of the router. The secret in the URL is the credential; an optional HMAC signature of the
// body proves the sender knows a project secret as well. The route sets no cookies and honours no CSRF token.

type automationHookResult struct {
	Queued bool `json:"queued" doc:"Always true: the run starts as soon as a worker picks the event up."`
}

// RegisterAutomationHookRoutes wires the public incoming webhook onto the Huma API.
func RegisterAutomationHookRoutes(api huma.API) {
	op := huma.Operation{
		OperationID:     "automation-hook-incoming",
		Summary:         "Call an incoming webhook of an automation rule",
		Description:     "Starts the rule that owns the secret token in the URL (trigger webhook.incoming) with the JSON body as event.payload. Needs no authentication: the token is the credential. When the rule names a project secret, the call must carry the header X-Vikunja-Signature: sha256=<hex HMAC-SHA256 of the raw body, keyed with the secret> (401, code 22011 otherwise). The body is required (send {} for no payload) and is a JSON object of at most automation.webhook.maxbody bytes (400, code 22013 when it is not an object; 413 when too large). The rule runs asynchronously: the answer is 202. Unknown tokens, disabled rules and rules with another trigger are all 404 (code 22010). Calls are limited per rule (429 with Retry-After, code 22012).",
		Method:          http.MethodPost,
		Path:            "/automation/hooks/{token}",
		Tags:            []string{"automations"},
		Security:        publicSecurity,
		DefaultStatus:   http.StatusAccepted,
		MaxBodyBytes:    int64(config.AutomationWebhookMaxBody.GetInt()),
		BodyReadTimeout: 10 * time.Second,
	}
	Register(api, op, automationHookIncoming)
}

func init() { AddRouteRegistrar(RegisterAutomationHookRoutes) }

func automationHookIncoming(ctx context.Context, in *struct {
	Token     string `path:"token" doc:"The secret token of the webhook."`
	Signature string `header:"X-Vikunja-Signature" doc:"sha256=<hex HMAC-SHA256 of the body>; required when the rule names a secret."`
	RawBody   []byte
}) (*singleBody[automationHookResult], error) {
	_, err := automation.ReceiveWebhook(&automation.WebhookCall{Token: in.Token, Body: in.RawBody, Signature: in.Signature})
	if err != nil {
		var limited models.ErrAutomationHookRateLimited
		if errors.As(err, &limited) {
			if ec := humabridge.EchoContextFrom(ctx); ec != nil {
				ec.Response().Header().Set("Retry-After", strconv.FormatInt(limited.RetryAfterSeconds, 10))
			}
		}
		return nil, translateDomainError(err)
	}
	return &singleBody[automationHookResult]{Body: &automationHookResult{Queued: true}}, nil
}
