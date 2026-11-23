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

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/web"
)

// ErrAutomationRuleDoesNotExist represents an error where an automation rule does not exist in the project.
type ErrAutomationRuleDoesNotExist struct {
	RuleID int64
}

// IsErrAutomationRuleDoesNotExist checks if an error is ErrAutomationRuleDoesNotExist.
func IsErrAutomationRuleDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrAutomationRuleDoesNotExist](err)
	return ok
}

func (err ErrAutomationRuleDoesNotExist) Error() string {
	return fmt.Sprintf("Automation rule does not exist [RuleID: %d]", err.RuleID)
}

// ErrCodeAutomationRuleDoesNotExist holds the unique world-error code of this error
const ErrCodeAutomationRuleDoesNotExist = 22001

// HTTPError holds the http error description
func (err ErrAutomationRuleDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeAutomationRuleDoesNotExist,
		Message:  "This automation rule does not exist.",
	}
}

// AutomationRuleInvalidDetails is the machine-readable body of ErrAutomationRuleInvalid.
type AutomationRuleInvalidDetails struct {
	Issues []spec.Issue `json:"issues" doc:"Every problem of the rule with the path of the offending part, e.g. actions[1].params.status_id."`
}

// ErrAutomationRuleInvalid represents an error where a rule document does not match the catalog.
type ErrAutomationRuleInvalid struct {
	AutomationRuleInvalidDetails
}

// IsErrAutomationRuleInvalid checks if an error is ErrAutomationRuleInvalid.
func IsErrAutomationRuleInvalid(err error) bool {
	_, ok := errors.AsType[ErrAutomationRuleInvalid](err)
	return ok
}

func (err ErrAutomationRuleInvalid) Error() string {
	parts := make([]string, 0, len(err.Issues))
	for _, i := range err.Issues {
		parts = append(parts, i.Path+": "+i.Message)
	}
	return "Automation rule is invalid: " + strings.Join(parts, "; ")
}

// ErrCodeAutomationRuleInvalid holds the unique world-error code of this error
const ErrCodeAutomationRuleInvalid = 22002

// HTTPError holds the http error description
func (err ErrAutomationRuleInvalid) HTTPError() web.HTTPError {
	details := err.AutomationRuleInvalidDetails
	if details.Issues == nil {
		details.Issues = []spec.Issue{}
	}
	msg := "The automation rule is invalid."
	if len(details.Issues) > 0 {
		msg += " " + details.Issues[0].Path + ": " + details.Issues[0].Message
	}
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeAutomationRuleInvalid,
		Message:  msg,
		Details:  details,
	}
}

// ErrAutomationRunDoesNotExist represents an error where an automation run does not exist.
type ErrAutomationRunDoesNotExist struct {
	RunID int64
}

// IsErrAutomationRunDoesNotExist checks if an error is ErrAutomationRunDoesNotExist.
func IsErrAutomationRunDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrAutomationRunDoesNotExist](err)
	return ok
}

func (err ErrAutomationRunDoesNotExist) Error() string {
	return fmt.Sprintf("Automation run does not exist [RunID: %d]", err.RunID)
}

// ErrCodeAutomationRunDoesNotExist holds the unique world-error code of this error
const ErrCodeAutomationRunDoesNotExist = 22003

// HTTPError holds the http error description
func (err ErrAutomationRunDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeAutomationRunDoesNotExist,
		Message:  "This automation run does not exist.",
	}
}

// ErrAutomationRunNotRetryable represents an error where a run that did not fail should be retried.
type ErrAutomationRunNotRetryable struct {
	RunID  int64
	Status string
}

// IsErrAutomationRunNotRetryable checks if an error is ErrAutomationRunNotRetryable.
func IsErrAutomationRunNotRetryable(err error) bool {
	_, ok := errors.AsType[ErrAutomationRunNotRetryable](err)
	return ok
}

func (err ErrAutomationRunNotRetryable) Error() string {
	return fmt.Sprintf("Automation run cannot be retried [RunID: %d, Status: %s]", err.RunID, err.Status)
}

// ErrCodeAutomationRunNotRetryable holds the unique world-error code of this error
const ErrCodeAutomationRunNotRetryable = 22004

// HTTPError holds the http error description
func (err ErrAutomationRunNotRetryable) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusConflict,
		Code:     ErrCodeAutomationRunNotRetryable,
		Message:  "Only runs that failed can be retried.",
	}
}

// ErrAutomationRuleNotRunnable represents an error where a rule cannot be started the way it was asked for: it is
// disabled, or it has another trigger.
type ErrAutomationRuleNotRunnable struct {
	RuleID int64
	Reason string
}

// IsErrAutomationRuleNotRunnable checks if an error is ErrAutomationRuleNotRunnable.
func IsErrAutomationRuleNotRunnable(err error) bool {
	var target ErrAutomationRuleNotRunnable
	return errors.As(err, &target)
}

func (err ErrAutomationRuleNotRunnable) Error() string {
	return fmt.Sprintf("Automation rule cannot be run [RuleID: %d, Reason: %s]", err.RuleID, err.Reason)
}

// ErrCodeAutomationRuleNotRunnable holds the unique world-error code of this error
const ErrCodeAutomationRuleNotRunnable = 22009

// HTTPError holds the http error description
func (err ErrAutomationRuleNotRunnable) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusConflict,
		Code:     ErrCodeAutomationRuleNotRunnable,
		Message:  "This rule cannot be run this way: it is disabled or has another trigger.",
		Details:  map[string]any{"reason": err.Reason},
	}
}

// ErrAutomationHookNotFound represents an error where the token of an incoming webhook matches no enabled rule.
type ErrAutomationHookNotFound struct{}

// IsErrAutomationHookNotFound checks if an error is ErrAutomationHookNotFound.
func IsErrAutomationHookNotFound(err error) bool {
	var target ErrAutomationHookNotFound
	return errors.As(err, &target)
}

func (err ErrAutomationHookNotFound) Error() string {
	return "Automation webhook does not exist"
}

// ErrCodeAutomationHookNotFound holds the unique world-error code of this error
const ErrCodeAutomationHookNotFound = 22010

// HTTPError holds the http error description
func (err ErrAutomationHookNotFound) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeAutomationHookNotFound,
		Message:  "This webhook does not exist.",
	}
}

// ErrAutomationHookSignatureInvalid represents an error where the signature of an incoming webhook is missing or wrong.
type ErrAutomationHookSignatureInvalid struct{}

// IsErrAutomationHookSignatureInvalid checks if an error is ErrAutomationHookSignatureInvalid.
func IsErrAutomationHookSignatureInvalid(err error) bool {
	var target ErrAutomationHookSignatureInvalid
	return errors.As(err, &target)
}

func (err ErrAutomationHookSignatureInvalid) Error() string {
	return "Automation webhook signature is missing or invalid"
}

// ErrCodeAutomationHookSignatureInvalid holds the unique world-error code of this error
const ErrCodeAutomationHookSignatureInvalid = 22011

// HTTPError holds the http error description
func (err ErrAutomationHookSignatureInvalid) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusUnauthorized,
		Code:     ErrCodeAutomationHookSignatureInvalid,
		Message:  "The X-Vikunja-Signature header is missing or does not match the body.",
	}
}

// ErrAutomationHookRateLimited represents an error where a webhook was called too often.
type ErrAutomationHookRateLimited struct {
	RetryAfterSeconds int64
}

// IsErrAutomationHookRateLimited checks if an error is ErrAutomationHookRateLimited.
func IsErrAutomationHookRateLimited(err error) bool {
	var target ErrAutomationHookRateLimited
	return errors.As(err, &target)
}

func (err ErrAutomationHookRateLimited) Error() string {
	return fmt.Sprintf("Too many webhook calls [RetryAfter: %ds]", err.RetryAfterSeconds)
}

// ErrCodeAutomationHookRateLimited holds the unique world-error code of this error
const ErrCodeAutomationHookRateLimited = 22012

// HTTPError holds the http error description
func (err ErrAutomationHookRateLimited) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusTooManyRequests,
		Code:     ErrCodeAutomationHookRateLimited,
		Message:  "Too many calls, try again later.",
		Details:  map[string]any{"retry_after": err.RetryAfterSeconds},
	}
}

// ErrAutomationHookPayloadInvalid represents an error where the body of an incoming webhook is not a JSON object.
type ErrAutomationHookPayloadInvalid struct{}

// IsErrAutomationHookPayloadInvalid checks if an error is ErrAutomationHookPayloadInvalid.
func IsErrAutomationHookPayloadInvalid(err error) bool {
	var target ErrAutomationHookPayloadInvalid
	return errors.As(err, &target)
}

func (err ErrAutomationHookPayloadInvalid) Error() string {
	return "Automation webhook body is not a JSON object"
}

// ErrCodeAutomationHookPayloadInvalid holds the unique world-error code of this error
const ErrCodeAutomationHookPayloadInvalid = 22013

// HTTPError holds the http error description
func (err ErrAutomationHookPayloadInvalid) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeAutomationHookPayloadInvalid,
		Message:  "The body has to be a JSON object.",
	}
}
