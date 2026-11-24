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
	"code.vikunja.io/api/pkg/config"

	"xorm.io/xorm"
)

// maxCustomFieldTextBytes is what a text value may hold, in bytes (not characters). value_text is a text column:
// 65535 bytes on MySQL, where a longer value is refused or cut off depending on the SQL mode, unlimited on
// PostgreSQL and SQLite. The limit is the smallest of the three, so a value that is stored on one database is
// stored on all of them.
const maxCustomFieldTextBytes = 65535

const (
	defaultCustomFieldsMaxFieldsPerProject = 100
	defaultCustomFieldsMaxOptionsPerField  = 200
)

// customFieldLimit reads a limit that has to be greater than 0: a value of 0 or less is read as the default, like
// the limits of automation.
func customFieldLimit(key config.Key, fallback int64) int64 {
	if limit := int64(key.GetInt()); limit > 0 {
		return limit
	}
	return fallback
}

// checkCustomFieldCanBeAdded refuses to add a field to a project that already has customfields.maxfieldsperproject
// of them. Only adding is checked: a project that is over the limit (it was lowered after the fields were made, or
// the project was duplicated) keeps its fields, which can still be edited and deleted. The count and the insert are
// not locked together, so concurrent creates can overshoot by about the number of requests in flight; that is
// accepted for a limit that protects against a runaway client, like the one of automation rules.
func checkCustomFieldCanBeAdded(s *xorm.Session, projectID int64) error {
	limit := customFieldLimit(config.CustomFieldsMaxFieldsPerProject, defaultCustomFieldsMaxFieldsPerProject)
	count, err := s.Where("project_id = ?", projectID).Count(&CustomField{})
	if err != nil {
		return err
	}
	if count >= limit {
		return ErrCustomFieldLimitReached{ProjectID: projectID, Limit: limit}
	}
	return nil
}

// checkCustomFieldOptionCount refuses a list of options that is longer than customfields.maxoptionsperfield and
// than the stored one: a field that is over the limit keeps its options and can be edited and shrunk, but not
// grown.
func checkCustomFieldOptionCount(fieldID int64, submitted int, stored int) error {
	limit := customFieldLimit(config.CustomFieldsMaxOptionsPerField, defaultCustomFieldsMaxOptionsPerField)
	if int64(submitted) > limit && submitted > stored {
		return ErrCustomFieldOptionLimitReached{CustomFieldID: fieldID, Limit: limit}
	}
	return nil
}

// checkCustomFieldTextLength refuses a text value longer than the column can hold. The length is in bytes: a
// multi-byte character counts with all of its bytes.
func checkCustomFieldTextLength(f *CustomField, value string) error {
	if len(value) > maxCustomFieldTextBytes {
		return ErrCustomFieldValueTooLong{CustomFieldID: f.ID, Type: f.Type, Length: len(value), Limit: maxCustomFieldTextBytes}
	}
	return nil
}
