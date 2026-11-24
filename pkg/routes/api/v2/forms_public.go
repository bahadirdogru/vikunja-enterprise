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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"
	"code.vikunja.io/api/pkg/modules/humabridge"
	"code.vikunja.io/api/pkg/web"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
)

// The routes of this file are public: they opt out of the global auth (publicSecurity) and are listed in
// unauthenticatedAPIPaths and in optionalAuthAPIPaths of the router. A request without an Authorization header is
// anonymous; one with a header is authenticated like on any other route (an invalid token is a 401 with code 11), and
// the form decides per access mode what to do with it. The routes set no cookies and honour no CSRF token: nothing
// they do depends on ambient credentials.

type formDefinitionBody struct {
	Body *models.FormPublicDefinition
}

type formSubmitResultBody struct {
	Body *models.FormSubmitResult
}

// FormSubmitter is what the submitter says about themselves.
type FormSubmitter struct {
	Name  string `json:"name" maxLength:"250" doc:"The name, when the form asks for it."`
	Email string `json:"email" maxLength:"250" doc:"The e-mail address, when the form asks for it."`
}

// FormSubmitInput is the body of a submission.
type FormSubmitInput struct {
	Values    map[string]any `json:"values" doc:"The answers keyed by the target of the field, e.g. {\"title\": \"Laptop\", \"cf_12\": 5}. Only the fields the definition lists may be sent; anything else is rejected."`
	Submitter FormSubmitter  `json:"submitter" doc:"Name and e-mail address of the submitter, as far as the form asks for them."`
	Honeypot  string         `json:"honeypot" doc:"A field real submitters never fill: leave it empty. A non-empty value is answered like a success and nothing is created."`
}

type formMultipartInput struct {
	Token   string `path:"token" doc:"The secret token of the form."`
	RawBody huma.MultipartFormFiles[struct {
		Data  string          `form:"data" required:"true" doc:"The JSON of a plain submission as a string: {\"values\": {...}, \"submitter\": {...}, \"honeypot\": \"\"}."`
		Files []huma.FormFile `form:"files" doc:"The files, at most forms.maxattachments, each at most the smaller of files.maxsize and forms.maxattachmentsize."`
	}]
}

// RegisterFormPublicRoutes wires the public side of forms onto the Huma API.
func RegisterFormPublicRoutes(api huma.API) {
	tags := []string{"forms"}

	Register(api, huma.Operation{
		OperationID: "form-public-read",
		Summary:     "Get the public definition of a form",
		Description: "Returns what a submitter needs: the title, the description, the shown fields in order with their label, help, requiredness, kind, config and options, what to do after a submission, which details of the submitter are asked and the file limits when attachments are asked. It names no project, owner, count, hidden field or user. Public forms need no authentication; forms for signed-in users answer 401 (code 25004) without a valid Authorization header, 403 (25005) for link shares or non-members. An unknown token is 404; a form that takes no submissions answers 410 (25003) with the reason disabled, closed or full and its title, to a caller who may submit it (public forms: to everyone). For forms for signed-in users the access is checked first: an anonymous caller gets the 404 of an unknown token when the form takes no submissions, a signed-in caller who may not submit gets 403. Never cached.",
		Method:      http.MethodGet,
		Path:        "/forms/{token}",
		Tags:        tags,
		Security:    publicSecurity,
	}, formPublicRead)

	Register(api, huma.Operation{
		OperationID: "form-public-logo",
		Summary:     "Get the logo of a form",
		Description: "Streams the logo of the form as a PNG image. It is served to everyone who knows the token of the form, whatever the access mode of the form is, because a browser cannot send credentials with an image request; the logo is not project data. Sends an ETag and answers 304 to a matching If-None-Match. 404 for an unknown token and for a form without a logo.",
		Method:      http.MethodGet,
		Path:        "/forms/{token}/logo",
		Tags:        tags,
		Security:    publicSecurity,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The logo as a PNG image.",
				Content: map[string]*huma.MediaType{
					"image/png": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
				},
			},
		},
	}, formPublicLogo)

	Register(api, withFormBodyLimit(huma.Operation{
		OperationID:   "form-public-submit",
		Summary:       "Submit a form",
		Description:   "Validates the answers and creates a task in the project of the form, as the form owner, in one transaction. Answers are rejected with 422 (code 25006) and one entry per field: {field, code, message}. The response never contains the task id or project data on public forms. Public forms are limited per client address and per form (429 with Retry-After, code 25007). A non-empty honeypot is answered like a success and creates nothing. Use the multipart variant to send files.",
		Method:        http.MethodPost,
		Path:          "/forms/{token}/submissions",
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Security:      publicSecurity,
	}, int64(config.FormsMaxBodySize.GetInt())), formPublicSubmit)

	Register(api, withFormBodyLimit(huma.Operation{
		OperationID:   "form-public-submit-multipart",
		Summary:       "Submit a form with files",
		Description:   "Like the JSON submission, but as multipart/form-data: the part data holds the JSON of the submission as a string and the parts files the attachments. Everything happens in one transaction, so a rejected file creates nothing. Files are only accepted when the form asks for attachments (public forms: only when forms.public.attachments is enabled), at most forms.maxattachments of at most min(files.maxsize, forms.maxattachmentsize) each; executable and script types are rejected by extension and by sniffed content type.",
		Method:        http.MethodPost,
		Path:          "/forms/{token}/submissions/multipart",
		DefaultStatus: http.StatusOK,
		Tags:          tags,
		Security:      publicSecurity,
	}, formMultipartLimit()), formPublicSubmitMultipart)
}

func init() { AddRouteRegistrar(RegisterFormPublicRoutes) }

func withFormBodyLimit(op huma.Operation, limit int64) huma.Operation {
	op.MaxBodyBytes = limit
	op.BodyReadTimeout = 30 * time.Second
	return op
}

func formMultipartLimit() int64 {
	maxFiles, maxSize := models.FormAttachmentLimits()
	// #nosec G115 - configured values do not get near int64 max.
	return int64(config.FormsMaxBodySize.GetInt()) + int64(maxFiles)*int64(maxSize) + 1024*1024
}

// formAuth returns the auth of the request, nil when it has none.
func formAuth(ctx context.Context) web.Auth {
	ec := humabridge.EchoContextFrom(ctx)
	if ec == nil || !auth.HasAuthInContext(ec) {
		return nil
	}
	a, err := auth.GetAuthFromClaims(ec)
	if err != nil {
		return nil
	}
	return a
}

func formClientIP(ctx context.Context) string {
	if ec := humabridge.EchoContextFrom(ctx); ec != nil {
		return ec.RealIP()
	}
	return ""
}

// formError translates a domain error and adds the Retry-After header of rate limits.
func formError(ctx context.Context, err error) error {
	if limited, ok := errors.AsType[models.ErrFormRateLimited](err); ok {
		if ec := humabridge.EchoContextFrom(ctx); ec != nil {
			ec.Response().Header().Set("Retry-After", strconv.FormatInt(limited.RetryAfterSeconds, 10))
		}
	}
	return translateDomainError(err)
}

func formPublicRead(ctx context.Context, in *struct {
	Token string `path:"token" doc:"The secret token of the form."`
}) (*formDefinitionBody, error) {
	def, err := models.GetFormDefinition(in.Token, formAuth(ctx))
	if err != nil {
		return nil, formError(ctx, err)
	}
	return &formDefinitionBody{Body: def}, nil
}

func formPublicLogo(ctx context.Context, in *struct {
	Token string `path:"token" doc:"The secret token of the form."`
}) (*huma.StreamResponse, error) {
	logo, err := models.OpenFormLogo(in.Token)
	if err != nil {
		return nil, formError(ctx, err)
	}
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		defer func() { _ = logo.File.File.Close() }()
		c := humaecho.Unwrap(hctx)
		w, r := (*c).Response(), (*c).Request()
		// Replaces the global no-store: the logo may be cached, but is revalidated.
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("ETag", logo.ETag)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if etagMatches(r.Header.Get("If-None-Match"), logo.ETag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, logo.File.File)
	}}, nil
}

func formPublicSubmit(ctx context.Context, in *struct {
	Token string `path:"token" doc:"The secret token of the form."`
	Body  FormSubmitInput
}) (*formSubmitResultBody, error) {
	res, err := models.SubmitForm(ctx, &models.FormSubmitRequest{
		Token:          in.Token,
		Auth:           formAuth(ctx),
		IP:             formClientIP(ctx),
		Values:         in.Body.Values,
		SubmitterName:  in.Body.Submitter.Name,
		SubmitterEmail: in.Body.Submitter.Email,
		Honeypot:       in.Body.Honeypot,
	})
	if err != nil {
		return nil, formError(ctx, err)
	}
	return &formSubmitResultBody{Body: res}, nil
}

func formPublicSubmitMultipart(ctx context.Context, in *formMultipartInput) (*formSubmitResultBody, error) {
	data := in.RawBody.Data()
	// The limit of the JSON route: the body limit of this route has to leave room for the files.
	if len(data.Data) > config.FormsMaxBodySize.GetInt() {
		return nil, huma.NewError(http.StatusRequestEntityTooLarge, "data is too large")
	}
	var body FormSubmitInput
	if err := json.Unmarshal([]byte(data.Data), &body); err != nil {
		return nil, huma.Error422UnprocessableEntity("data is not valid JSON")
	}
	uploads := make([]*models.AttachmentToUpload, 0, len(data.Files))
	for _, f := range data.Files {
		if f.IsSet {
			uploads = append(uploads, &models.AttachmentToUpload{Reader: io.ReadSeeker(f), Filename: f.Filename, Size: uint64(f.Size)})
		}
	}
	res, err := models.SubmitForm(ctx, &models.FormSubmitRequest{
		Token:          in.Token,
		Auth:           formAuth(ctx),
		IP:             formClientIP(ctx),
		Values:         body.Values,
		SubmitterName:  body.Submitter.Name,
		SubmitterEmail: body.Submitter.Email,
		Honeypot:       body.Honeypot,
		Files:          uploads,
	})
	if err != nil {
		return nil, formError(ctx, err)
	}
	return &formSubmitResultBody{Body: res}, nil
}

// etagMatches reports whether an If-None-Match header names the ETag: a comma separated list, weak validators
// allowed (weak comparison is what the header uses), or *.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}
