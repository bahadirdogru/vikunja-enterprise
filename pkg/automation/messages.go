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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

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

// What is stored and sent as values of a sentence is bounded like the English text next to it (Error is cut to 4000
// bytes, Output to 16 KiB): a task description of a megabyte must not become a megabyte per run, and a row must fit
// the text column of every database: the values of an error are at most 4000 bytes (24 KB as JSON, whatever the
// escaping), the sentences of a step at most the size of the output (16 KiB as JSON). The bound is applied where a sentence leaves the engine (stored in a run or
// step, sent in a test), after the secrets were masked: a cut before the mask could split a secret into a piece the
// masker no longer finds.
const (
	maxParamRunes        = 1000
	maxErrorParamsBytes  = 4000
	maxMessageParamBytes = 700
	maxOutputMessages    = 20
)

// clipRunes cuts text to at most max runes, on a rune boundary, and says so with an ellipsis.
func clipRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
}

// boundParams cuts every value to maxParamRunes runes and the values together to totalBytes bytes (the longest
// value is halved until they fit).
func boundParams(params map[string]string, totalBytes int) map[string]string {
	if len(params) == 0 {
		return params
	}
	out := make(map[string]string, len(params))
	size := 0
	for k, v := range params {
		out[k] = clipRunes(v, maxParamRunes)
		size += len(k) + len(out[k])
	}
	for size > totalBytes {
		longest := ""
		for k, v := range out {
			if len(v) > len(out[longest]) {
				longest = k
			}
		}
		v := out[longest]
		if len(v) <= 4 {
			break // only keys are left; they are code, not data
		}
		cut := clipRunes(v, utf8.RuneCountInString(v)/2)
		if len(cut) >= len(v) {
			break
		}
		size -= len(v) - len(cut)
		out[longest] = cut
	}
	return out
}

// boundMessages is the list of sentences of a step as it is stored or sent: at most maxOutputMessages of them, the
// values of each cut to maxMessageParamBytes.
func boundMessages(list []*models.AutomationMessage) []*models.AutomationMessage {
	if len(list) > maxOutputMessages {
		list = list[:maxOutputMessages]
	}
	out := make([]*models.AutomationMessage, 0, len(list))
	for _, m := range list {
		if m == nil {
			continue
		}
		out = append(out, &models.AutomationMessage{Code: m.Code, Params: boundParams(m.Params, maxMessageParamBytes)})
	}
	// What is stored is the JSON of the list, and JSON escaping can make a value six times longer (control
	// characters): the list is bounded as it is stored, to what the output of a step may be, which every database's
	// text column holds (MySQL TEXT: 64 KiB).
	for len(out) > 0 {
		encoded, err := json.Marshal(out)
		if err != nil || len(encoded) <= maxOutputBytes {
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

// maskMessages removes the secrets of the step from the values of sentences, before they are bounded.
func (e *Env) maskMessages(list []*models.AutomationMessage) []*models.AutomationMessage {
	out := make([]*models.AutomationMessage, 0, len(list))
	for _, m := range list {
		if m == nil {
			continue
		}
		masked := &models.AutomationMessage{Code: m.Code}
		if len(m.Params) > 0 {
			masked.Params = make(map[string]string, len(m.Params))
			for k, v := range m.Params {
				masked.Params[k] = e.maskOutput(v)
			}
		}
		out = append(out, masked)
	}
	return out
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

// errorMessage gives the code and the bounded values of an error, as they are stored. The error must have been
// through Env.maskErr already: the cut comes after the mask, or a cut could split a secret into a piece the masker no
// longer finds. Only the places that store or send a failure call it (TestOnlyTheStoringCodeBoundsAnError); everything
// that builds or maps an error uses errorMessageRaw, which gives the values whole.
func errorMessage(err error) (code string, params map[string]string) {
	code, params = errorMessageRaw(err)
	return code, boundParams(params, maxErrorParamsBytes)
}

// errorMessageRaw gives the code and the values of an error. An error of the models with a numeric API error code gives
// that code as text ("4002"), which the interface looks up like any API error (error.<code>).
func errorMessageRaw(err error) (code string, params map[string]string) {
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
