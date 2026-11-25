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

import "strings"

// customFieldReasonCodes gives the reasons of ErrCustomFieldInvalidDefinition and ErrCustomFieldInvalidValue a
// stable code. The interface says the reason in the language of the reader (customFields.reasons.<code> in the language
// files of the frontend); a reason without a code is shown in English as the server wrote it.
var customFieldReasonCodes = map[string]string{
	"title must not be empty":                            "title_empty",
	"title must not be longer than 250 characters":       "title_too_long",
	"only select and multi_select fields have options":   "options_not_allowed",
	"option titles must be between 1 and 250 characters": "option_title_length",
	"option colors must look like #aabbcc":               "option_color",
	"min and max must be finite numbers":                 "min_max_finite",
	"min must not be greater than max":                   "min_greater_than_max",
	"precision must be between 0 and 10":                 "precision_range",
	"max_rating must be between 1 and 10":                "max_rating_range",
	"currency must be a three letter code":               "currency_code",

	"the value must not contain a NUL character":                     "nul_character",
	"expected a number":                                              "expected_number",
	"progress must be between 0 and 100":                             "progress_range",
	"rating must be a whole number between 1 and the maximum rating": "rating_range",
	"value is below the minimum":                                     "below_minimum",
	"value is above the maximum":                                     "above_maximum",
	"expected a string":                                              "expected_string",
	"expected an http or https url":                                  "expected_url",
	"expected an email address":                                      "expected_email",
	"expected an RFC 3339 date":                                      "expected_date",
	"expected an RFC 3339 date string":                               "expected_date_string",
	"expected an option id":                                          "expected_option",
	"expected an array of option ids":                                "expected_options",
	"option does not belong to this field":                           "option_not_of_field",
	"expected an array of user ids":                                  "expected_users",
	"user does not exist":                                            "user_missing",
	"user has no access to the project of the task":                  "user_no_access",
	"expected a boolean":                                             "expected_boolean",
	"unknown field type":                                             "unknown_field_type",
}

// CustomFieldReasonCodes is the list of codes the interface needs a text for.
func CustomFieldReasonCodes() []string {
	codes := make([]string, 0, len(customFieldReasonCodes)+2)
	for _, code := range customFieldReasonCodes {
		codes = append(codes, code)
	}
	return append(codes, "unknown_type", "value_too_long")
}

// customFieldReasonParams are the i18n parameters of an error with a reason: the English reason for a client that
// does not know the code, the code (reason_code), and the value a reason mentions (reason_type, reason_limit).
func customFieldReasonParams(reason string) map[string]string {
	params := map[string]string{"reason": reason}
	if code, ok := customFieldReasonCodes[reason]; ok {
		params["reason_code"] = code
	} else if typ, ok := strings.CutPrefix(reason, "unknown type "); ok {
		params["reason_code"] = "unknown_type"
		params["reason_type"] = typ
	} else if strings.HasPrefix(reason, "the value is too long, at most ") {
		params["reason_code"] = "value_too_long"
		params["reason_limit"] = strings.TrimSuffix(strings.TrimPrefix(reason, "the value is too long, at most "), " bytes are stored")
	}
	return params
}
