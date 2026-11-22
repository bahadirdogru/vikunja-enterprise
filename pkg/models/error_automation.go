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
	_, ok := err.(ErrAutomationRuleDoesNotExist)
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
	_, ok := err.(ErrAutomationRuleInvalid)
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
	_, ok := err.(ErrAutomationRunDoesNotExist)
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
	_, ok := err.(ErrAutomationRunNotRetryable)
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
