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

	"code.vikunja.io/api/pkg/web"
)

// ErrAutomationScriptsDisabled represents an error where a rule with a script is saved while scripts are turned off.
type ErrAutomationScriptsDisabled struct{}

// IsErrAutomationScriptsDisabled checks if an error is ErrAutomationScriptsDisabled.
func IsErrAutomationScriptsDisabled(err error) bool {
	_, ok := errors.AsType[ErrAutomationScriptsDisabled](err)
	return ok
}

func (err ErrAutomationScriptsDisabled) Error() string {
	return "Scripts in automation rules are disabled on this instance"
}

// ErrCodeAutomationScriptsDisabled holds the unique world-error code of this error
const ErrCodeAutomationScriptsDisabled = 22005

// HTTPError holds the http error description
func (err ErrAutomationScriptsDisabled) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeAutomationScriptsDisabled,
		Message:  "Scripts in automation rules are disabled on this instance (automation.scripts.enabled).",
	}
}

// ErrAutomationSecretInvalid represents an error where the name or the value of a secret is not valid.
type ErrAutomationSecretInvalid struct {
	Reason string
}

// IsErrAutomationSecretInvalid checks if an error is ErrAutomationSecretInvalid.
func IsErrAutomationSecretInvalid(err error) bool {
	_, ok := errors.AsType[ErrAutomationSecretInvalid](err)
	return ok
}

func (err ErrAutomationSecretInvalid) Error() string {
	return "Automation secret is invalid: " + err.Reason
}

// ErrCodeAutomationSecretInvalid holds the unique world-error code of this error
const ErrCodeAutomationSecretInvalid = 22006

// HTTPError holds the http error description
func (err ErrAutomationSecretInvalid) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeAutomationSecretInvalid,
		Message:  "The secret is invalid: " + err.Reason,
	}
}

// ErrAutomationSecretDoesNotExist represents an error where a secret does not exist in the project.
type ErrAutomationSecretDoesNotExist struct {
	Name string
}

// IsErrAutomationSecretDoesNotExist checks if an error is ErrAutomationSecretDoesNotExist.
func IsErrAutomationSecretDoesNotExist(err error) bool {
	_, ok := errors.AsType[ErrAutomationSecretDoesNotExist](err)
	return ok
}

func (err ErrAutomationSecretDoesNotExist) Error() string {
	return fmt.Sprintf("the secret %q does not exist in this project", err.Name)
}

// ErrCodeAutomationSecretDoesNotExist holds the unique world-error code of this error
const ErrCodeAutomationSecretDoesNotExist = 22007

// HTTPError holds the http error description
func (err ErrAutomationSecretDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeAutomationSecretDoesNotExist,
		Message:  "This secret does not exist.",
	}
}

// ErrAutomationSecretUnreadable represents an error where the stored value of a secret cannot be decrypted,
// because service.secret changed since it was written.
type ErrAutomationSecretUnreadable struct {
	Name string
}

// IsErrAutomationSecretUnreadable checks if an error is ErrAutomationSecretUnreadable.
func IsErrAutomationSecretUnreadable(err error) bool {
	_, ok := errors.AsType[ErrAutomationSecretUnreadable](err)
	return ok
}

func (err ErrAutomationSecretUnreadable) Error() string {
	return fmt.Sprintf("the secret %q cannot be decrypted (service.secret changed since it was stored): set it again", err.Name)
}

// ErrCodeAutomationSecretUnreadable holds the unique world-error code of this error
const ErrCodeAutomationSecretUnreadable = 22008

// HTTPError holds the http error description
func (err ErrAutomationSecretUnreadable) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusConflict,
		Code:     ErrCodeAutomationSecretUnreadable,
		Message:  "The secret cannot be decrypted any more. Set it again.",
	}
}
