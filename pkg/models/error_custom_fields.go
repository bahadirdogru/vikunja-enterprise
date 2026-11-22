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

	"code.vikunja.io/api/pkg/web"
)

// ErrCustomFieldDoesNotExist represents an error where a custom field does not exist in the project.
type ErrCustomFieldDoesNotExist struct {
	CustomFieldID int64
}

// IsErrCustomFieldDoesNotExist checks if an error is ErrCustomFieldDoesNotExist.
func IsErrCustomFieldDoesNotExist(err error) bool {
	_, ok := err.(ErrCustomFieldDoesNotExist)
	return ok
}

func (err ErrCustomFieldDoesNotExist) Error() string {
	return fmt.Sprintf("Custom field does not exist [CustomFieldID: %d]", err.CustomFieldID)
}

// ErrCodeCustomFieldDoesNotExist holds the unique world-error code of this error
const ErrCodeCustomFieldDoesNotExist = 20001

// HTTPError holds the http error description
func (err ErrCustomFieldDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeCustomFieldDoesNotExist,
		Message:  "This custom field does not exist.",
	}
}

// ErrCustomFieldInvalidDefinition represents an error where a custom field definition is not valid.
type ErrCustomFieldInvalidDefinition struct {
	Reason string
}

// IsErrCustomFieldInvalidDefinition checks if an error is ErrCustomFieldInvalidDefinition.
func IsErrCustomFieldInvalidDefinition(err error) bool {
	_, ok := err.(ErrCustomFieldInvalidDefinition)
	return ok
}

func (err ErrCustomFieldInvalidDefinition) Error() string {
	return fmt.Sprintf("Custom field definition is invalid: %s", err.Reason)
}

// ErrCodeCustomFieldInvalidDefinition holds the unique world-error code of this error
const ErrCodeCustomFieldInvalidDefinition = 20002

// HTTPError holds the http error description
func (err ErrCustomFieldInvalidDefinition) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeCustomFieldInvalidDefinition,
		Message:  "The custom field definition is invalid: " + err.Reason,
	}
}

// ErrCustomFieldTypeImmutable represents an error where the type of an existing custom field should change.
type ErrCustomFieldTypeImmutable struct {
	CustomFieldID int64
	Type          CustomFieldType
}

// IsErrCustomFieldTypeImmutable checks if an error is ErrCustomFieldTypeImmutable.
func IsErrCustomFieldTypeImmutable(err error) bool {
	_, ok := err.(ErrCustomFieldTypeImmutable)
	return ok
}

func (err ErrCustomFieldTypeImmutable) Error() string {
	return fmt.Sprintf("Custom field type cannot be changed [CustomFieldID: %d, Type: %s]", err.CustomFieldID, err.Type)
}

// ErrCodeCustomFieldTypeImmutable holds the unique world-error code of this error
const ErrCodeCustomFieldTypeImmutable = 20003

// HTTPError holds the http error description
func (err ErrCustomFieldTypeImmutable) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeCustomFieldTypeImmutable,
		Message:  "The type of a custom field cannot be changed after it was created.",
	}
}

// ErrCustomFieldOptionDoesNotExist represents an error where an option is not part of the custom field.
type ErrCustomFieldOptionDoesNotExist struct {
	CustomFieldID int64
	OptionID      int64
}

// IsErrCustomFieldOptionDoesNotExist checks if an error is ErrCustomFieldOptionDoesNotExist.
func IsErrCustomFieldOptionDoesNotExist(err error) bool {
	_, ok := err.(ErrCustomFieldOptionDoesNotExist)
	return ok
}

func (err ErrCustomFieldOptionDoesNotExist) Error() string {
	return fmt.Sprintf("Custom field option does not exist [CustomFieldID: %d, OptionID: %d]", err.CustomFieldID, err.OptionID)
}

// ErrCodeCustomFieldOptionDoesNotExist holds the unique world-error code of this error
const ErrCodeCustomFieldOptionDoesNotExist = 20004

// HTTPError holds the http error description
func (err ErrCustomFieldOptionDoesNotExist) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusNotFound,
		Code:     ErrCodeCustomFieldOptionDoesNotExist,
		Message:  "This option does not exist on the custom field.",
	}
}

// ErrCustomFieldInvalidValue represents an error where a value does not fit the type of the custom field.
type ErrCustomFieldInvalidValue struct {
	CustomFieldID int64
	Type          CustomFieldType
	Reason        string
}

// IsErrCustomFieldInvalidValue checks if an error is ErrCustomFieldInvalidValue.
func IsErrCustomFieldInvalidValue(err error) bool {
	_, ok := err.(ErrCustomFieldInvalidValue)
	return ok
}

func (err ErrCustomFieldInvalidValue) Error() string {
	return fmt.Sprintf("Custom field value is invalid [CustomFieldID: %d, Type: %s]: %s", err.CustomFieldID, err.Type, err.Reason)
}

// ErrCodeCustomFieldInvalidValue holds the unique world-error code of this error
const ErrCodeCustomFieldInvalidValue = 20005

// HTTPError holds the http error description
func (err ErrCustomFieldInvalidValue) HTTPError() web.HTTPError {
	return web.HTTPError{
		HTTPCode: http.StatusBadRequest,
		Code:     ErrCodeCustomFieldInvalidValue,
		Message:  "The value is not valid for this custom field: " + err.Reason,
	}
}
