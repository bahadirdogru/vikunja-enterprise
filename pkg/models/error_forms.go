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

package models

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"code.vikunja.io/api/pkg/web"
)

// ErrFormDoesNotExist represents an error where a form does not exist, or the token is unknown.
type ErrFormDoesNotExist struct {
	ID int64
}

// IsErrFormDoesNotExist checks if an error is ErrFormDoesNotExist.
func IsErrFormDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrFormDoesNotExist](err)
	return ok
}

func (err ErrFormDoesNotExist) Error() string {
	return fmt.Sprintf("Form does not exist [ID: %d]", err.ID)
}

// ErrCodeFormDoesNotExist holds the unique world-error code of this error
const ErrCodeFormDoesNotExist = 25001

// HTTPError holds the http error description
func (err ErrFormDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeFormDoesNotExist,
		Message:  "This form does not exist.",
	}
}

// FormIssue is one problem of a form definition.
type FormIssue struct {
	Path    string `json:"path" doc:"Where the problem is, e.g. fields[2].target or settings.redirect_url."`
	Message string `json:"message" doc:"What is wrong."`
}

// FormInvalidDetails is the machine-readable body of ErrFormInvalid.
type FormInvalidDetails struct {
	Issues []FormIssue `json:"issues" doc:"Every problem of the form with the path of the offending part, e.g. fields[2].target."`
}

// ErrFormInvalid represents an error where a form definition is not valid.
type ErrFormInvalid struct {
	FormInvalidDetails
}

// IsErrFormInvalid checks if an error is ErrFormInvalid.
func IsErrFormInvalid(err error) bool {
	_, ok := errors.AsType[ErrFormInvalid](err)
	return ok
}

func (err ErrFormInvalid) Error() string {
	parts := make([]string, 0, len(err.Issues))
	for _, i := range err.Issues {
		parts = append(parts, i.Path+": "+i.Message)
	}
	return "Form is invalid: " + strings.Join(parts, "; ")
}

// ErrCodeFormInvalid holds the unique world-error code of this error
const ErrCodeFormInvalid = 25002

// HTTPError holds the http error description
func (err ErrFormInvalid) HTTPError() web.HTTPError {
	details := err.FormInvalidDetails
	if details.Issues == nil {
		details.Issues = []FormIssue{}
	}
	msg := "The form is invalid."
	if len(details.Issues) > 0 {
		msg += " " + details.Issues[0].Path + ": " + details.Issues[0].Message
	}
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeFormInvalid,
		Message:  msg,
		Details:  details,
	}
}

// The reasons a form is not available.
const (
	FormUnavailableDisabled = "disabled"
	FormUnavailableClosed   = "closed"
	FormUnavailableFull     = "full"
)

// FormUnavailableDetails is the body of ErrFormUnavailable. It holds the title of the form and nothing else.
type FormUnavailableDetails struct {
	Reason string `json:"reason" enum:"disabled,closed,full" doc:"Why the form takes no submissions: disabled, closed (its close date passed) or full (the maximum number of submissions was reached)."`
	Title  string `json:"title" doc:"The title of the form."`
}

// ErrFormUnavailable represents an error where a form exists but takes no submissions.
type ErrFormUnavailable struct {
	FormUnavailableDetails
	// persisted is set when the transaction that found the problem has to be committed because it stored it.
	persisted bool
}

// IsErrFormUnavailable checks if an error is ErrFormUnavailable.
func IsErrFormUnavailable(err error) bool {
	_, ok := errors.AsType[ErrFormUnavailable](err)
	return ok
}

func (err ErrFormUnavailable) Error() string {
	return "Form is not available [Reason: " + err.Reason + "]"
}

// ErrCodeFormUnavailable holds the unique world-error code of this error
const ErrCodeFormUnavailable = 25003

// HTTPError holds the http error description
func (err ErrFormUnavailable) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusGone,
		Code:     ErrCodeFormUnavailable,
		Message:  "This form does not take submissions at the moment.",
		Details:  err.FormUnavailableDetails,
	}
}

// ErrFormLoginRequired represents an error where a form is not public and the request carries no authentication.
type ErrFormLoginRequired struct{}

// IsErrFormLoginRequired checks if an error is ErrFormLoginRequired.
func IsErrFormLoginRequired(err error) bool {
	_, ok := errors.AsType[ErrFormLoginRequired](err)
	return ok
}

func (err ErrFormLoginRequired) Error() string {
	return "Form requires a login"
}

// ErrCodeFormLoginRequired holds the unique world-error code of this error
const ErrCodeFormLoginRequired = 25004

// HTTPError holds the http error description
func (err ErrFormLoginRequired) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusUnauthorized,
		Code:     ErrCodeFormLoginRequired,
		Message:  "You have to log in to use this form.",
	}
}

// ErrFormNotAllowed represents an error where the requester is signed in but may not use the form: a link share, or
// a user who cannot read the project of a members form.
type ErrFormNotAllowed struct{}

// IsErrFormNotAllowed checks if an error is ErrFormNotAllowed.
func IsErrFormNotAllowed(err error) bool {
	_, ok := errors.AsType[ErrFormNotAllowed](err)
	return ok
}

func (err ErrFormNotAllowed) Error() string {
	return "Form is not allowed for this user"
}

// ErrCodeFormNotAllowed holds the unique world-error code of this error
const ErrCodeFormNotAllowed = 25005

// HTTPError holds the http error description
func (err ErrFormNotAllowed) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusForbidden,
		Code:     ErrCodeFormNotAllowed,
		Message:  "You are not allowed to use this form.",
	}
}

// FormFieldError is the problem of one value of a submission.
type FormFieldError struct {
	Field   string `json:"field" doc:"The target of the field, e.g. title or cf_12."`
	Code    string `json:"code" doc:"required, invalid, too_long, too_many, unknown_field, file_too_large, file_type_not_allowed or attachments_disabled."`
	Message string `json:"message" doc:"What is wrong."`
}

// FormSubmissionInvalidDetails is the machine-readable body of ErrFormSubmissionInvalid.
type FormSubmissionInvalidDetails struct {
	Errors []FormFieldError `json:"errors" doc:"One entry per problem, keyed by the target of the field."`
}

// ErrFormSubmissionInvalid represents an error where the values of a submission are not valid.
type ErrFormSubmissionInvalid struct {
	FormSubmissionInvalidDetails
}

// IsErrFormSubmissionInvalid checks if an error is ErrFormSubmissionInvalid.
func IsErrFormSubmissionInvalid(err error) bool {
	_, ok := errors.AsType[ErrFormSubmissionInvalid](err)
	return ok
}

func (err ErrFormSubmissionInvalid) Error() string {
	parts := make([]string, 0, len(err.Errors))
	for _, e := range err.Errors {
		parts = append(parts, e.Field+": "+e.Code)
	}
	return "Form submission is invalid: " + strings.Join(parts, "; ")
}

// ErrCodeFormSubmissionInvalid holds the unique world-error code of this error
const ErrCodeFormSubmissionInvalid = 25006

// HTTPError holds the http error description
func (err ErrFormSubmissionInvalid) HTTPError() web.HTTPError {
	details := err.FormSubmissionInvalidDetails
	if details.Errors == nil {
		details.Errors = []FormFieldError{}
	}
	return web.HTTPError{
		HTTPCode: http.StatusUnprocessableEntity,
		Code:     ErrCodeFormSubmissionInvalid,
		Message:  "Some answers are not valid.",
		Details:  details,
	}
}

// ErrFormRateLimited represents an error where too many submissions came from one address or went to one form.
type ErrFormRateLimited struct {
	// RetryAfterSeconds is when the next submission can succeed.
	RetryAfterSeconds int64
}

// IsErrFormRateLimited checks if an error is ErrFormRateLimited.
func IsErrFormRateLimited(err error) bool {
	_, ok := errors.AsType[ErrFormRateLimited](err)
	return ok
}

func (err ErrFormRateLimited) Error() string {
	return fmt.Sprintf("Too many form submissions [RetryAfter: %ds]", err.RetryAfterSeconds)
}

// ErrCodeFormRateLimited holds the unique world-error code of this error
const ErrCodeFormRateLimited = 25007

// HTTPError holds the http error description
func (err ErrFormRateLimited) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusTooManyRequests,
		Code:     ErrCodeFormRateLimited,
		Message:  "Too many submissions, try again later.",
		Details:  map[string]any{"retry_after": err.RetryAfterSeconds},
	}
}

// ErrFormsDisabled represents an error where forms are switched off on this instance (forms.enabled).
type ErrFormsDisabled struct{}

// IsErrFormsDisabled checks if an error is ErrFormsDisabled.
func IsErrFormsDisabled(err error) bool {
	_, ok := errors.AsType[ErrFormsDisabled](err)
	return ok
}

func (err ErrFormsDisabled) Error() string {
	return "Forms are disabled on this instance"
}

// ErrCodeFormsDisabled holds the unique world-error code of this error
const ErrCodeFormsDisabled = 25008

// HTTPError holds the http error description
func (err ErrFormsDisabled) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusForbidden,
		Code:     ErrCodeFormsDisabled,
		Message:  "Forms are disabled on this instance.",
	}
}

// The reasons a logo upload is refused.
const (
	FormLogoTooLarge        = "too_large"
	FormLogoNotAnImage      = "not_an_image"
	FormLogoUnsupportedType = "unsupported_type"
)

// ErrFormLogoInvalid represents an error where an uploaded form logo is refused.
type ErrFormLogoInvalid struct {
	Reason string
}

// IsErrFormLogoInvalid checks if an error is ErrFormLogoInvalid.
func IsErrFormLogoInvalid(err error) bool {
	_, ok := err.(ErrFormLogoInvalid)
	return ok
}

func (err ErrFormLogoInvalid) Error() string {
	return fmt.Sprintf("Form logo is not valid [Reason: %s]", err.Reason)
}

// ErrCodeFormLogoInvalid holds the unique world-error code of this error
const ErrCodeFormLogoInvalid = 25009

// HTTPError holds the http error description
func (err ErrFormLogoInvalid) HTTPError() web.HTTPError {
	message := "The logo is not a usable image."
	switch err.Reason {
	case FormLogoTooLarge:
		message = "The logo is too large. It may be at most 1 MiB."
	case FormLogoNotAnImage:
		message = "The logo is not an image."
	case FormLogoUnsupportedType:
		message = "The logo has to be a PNG, JPEG, GIF or WebP image."
	}
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeFormLogoInvalid,
		Message:  message,
		Details:  map[string]any{"reason": err.Reason},
	}
}
