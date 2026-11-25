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

package automation

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"code.vikunja.io/api/pkg/automation/script"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web"
)

// What the server tells about a run (why a step failed, what it did, which warnings it has) has an English sentence
// for people who read the API and a stable code with values for the interface, which says it in the language of the
// reader (frontend/src/i18n/lang/*.json, keys automation.messages.<code>). The English sentence is kept as the
// fallback: a client that does not know a code shows it.

// P holds the values a sentence mentions, by the name of their placeholder in the translations.
type P map[string]any

func (p P) strings() map[string]string {
	if len(p) == 0 {
		return nil
	}
	out := make(map[string]string, len(p))
	for k, v := range p {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// with adds the values of another sentence.
func (p P) with(other map[string]string) P {
	for k, v := range other {
		p[k] = v
	}
	return p
}

// say is a sentence of the server with its code and values.
func say(code string, params P) *models.AutomationMessage {
	return &models.AutomationMessage{Code: code, Params: params.strings()}
}

// Codes of the errors that are not an ActionError of the actions.
const (
	// CodeUnexpected is any failure that has no sentence of its own: a database error, say. Its detail is the
	// technical text of the server, shown inside a translated sentence.
	CodeUnexpected = "unexpected"
	// CodeStepTimeout is a step that ran out of time.
	CodeStepTimeout = "step_timeout"
	// CodeTaskGone is a task that was deleted while the run was waiting.
	CodeTaskGone = "task_gone"
	// CodeScriptUnavailable is the script machinery failing, not the script.
	CodeScriptUnavailable = "script_unavailable"
)

// errorMessage gives the code and the values of an error. An error of the models with a numeric API error code gives
// that code as text ("4002"), which the interface looks up like any API error (error.<code>).
func errorMessage(err error) (code string, params map[string]string) {
	if err == nil {
		return "", nil
	}
	var masked *maskedError
	if errors.As(err, &masked) && masked.code != "" {
		return masked.code, masked.params
	}
	var actionErr *ActionError
	if errors.As(err, &actionErr) && actionErr.Code != "" {
		return actionErr.Code, actionErr.Params
	}
	var scriptErr *script.Error
	if errors.As(err, &scriptErr) {
		return "script_" + string(scriptErr.Kind), map[string]string{"message": scriptErr.Error()}
	}
	var unavailable *script.UnavailableError
	if errors.As(err, &unavailable) {
		return CodeScriptUnavailable, map[string]string{"message": unavailable.Error()}
	}
	switch {
	case errors.Is(err, ErrTaskGone):
		return CodeTaskGone, nil
	case errors.Is(err, context.DeadlineExceeded):
		return CodeStepTimeout, nil
	}
	var httpErr web.HTTPErrorProcessor
	if errors.As(err, &httpErr) {
		h := httpErr.HTTPError()
		return strconv.Itoa(h.Code), h.I18nParams
	}
	return CodeUnexpected, map[string]string{"detail": err.Error()}
}
