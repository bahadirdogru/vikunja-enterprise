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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/automation/outbound"
	"code.vikunja.io/api/pkg/automation/script"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codeSources are the files that name a code of a message. The codes they name are looked up in the language files:
// a code without a text would show the English sentence of the server to a Turkish reader.
var codeCalls = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:say|actionErr)\("([a-z_]+)"`),
	regexp.MustCompile(`\.(?:say|warn)\("([a-z_]+)"`),
	regexp.MustCompile(`\bCode:\s*"([a-z_]+)"`),
	regexp.MustCompile(`\binvalid\("([a-z_]+)"`),
	regexp.MustCompile(`\bErrorCode(?:,\s*step\.ErrorParams)?\s*[:=]\s*"([a-z_]+)"`),
}

// namedCodes reads every code the server can store in a run: the literals in the package and in the lifecycle of
// the models, the codes built from a name (set_<date field>, script_<kind>) and the constants of messages.go.
func namedCodes(t *testing.T) []string {
	t.Helper()
	found := map[string]bool{}
	var files []string
	for _, pattern := range []string{"*.go", "outbound/*.go", "../models/automation*.go"} {
		m, err := filepath.Glob(pattern)
		require.NoError(t, err)
		files = append(files, m...)
	}
	for _, f := range files {
		if filepath.Ext(f) != ".go" || regexp.MustCompile(`_test\.go$`).MatchString(f) {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, re := range codeCalls {
			for _, m := range re.FindAllStringSubmatch(string(src), -1) {
				// "clear_"+field: a code built from a name, listed below.
				if strings.HasSuffix(m[1], "_") {
					continue
				}
				found[m[1]] = true
			}
		}
	}
	found[outbound.CodeBlocked] = true
	for _, c := range []string{CodeUnexpected, CodeStepTimeout, CodeTaskGone, CodeScriptUnavailable} {
		found[c] = true
	}
	for _, field := range []string{"due_date", "start_date", "end_date"} {
		found["set_"+field] = true
		found["clear_"+field] = true
	}
	for _, kind := range []script.ErrorKind{script.ErrThrown, script.ErrTimeout, script.ErrStackOverflow, script.ErrMemory,
		script.ErrBadResult, script.ErrInternal, script.ErrInputTooLarge, script.ErrUnattributed} {
		found["script_"+string(kind)] = true
	}
	out := make([]string, 0, len(found))
	for c := range found {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func TestEveryMessageCodeTheServerStoresIsTranslated(t *testing.T) {
	codes := namedCodes(t)
	require.Greater(t, len(codes), 60, "the scan found suspiciously few codes: did the call shapes change?")

	for _, file := range []string{"en.json", "tr-TR.json"} {
		t.Run(file, func(t *testing.T) {
			keys := flattenI18n(t, file)
			for _, code := range codes {
				assert.True(t, keys["automation.messages."+code], "%s has no text for the message code %s (automation.messages.%s)", file, code, code)
			}
		})
	}
}

func TestErrorMessageGivesTheCodeOfAnError(t *testing.T) {
	t.Run("an action error keeps its code and values", func(t *testing.T) {
		code, params := errorMessage(actionErr("title_empty", P{"a": 1}, "the title would be empty"))
		assert.Equal(t, "title_empty", code)
		assert.Equal(t, map[string]string{"a": "1"}, params)
	})
	t.Run("an error of the models gives its numeric code and values", func(t *testing.T) {
		code, params := errorMessage(models.ErrTaskDoesNotExist{ID: 5})
		assert.Equal(t, "4002", code)
		assert.Empty(t, params)
		code, params = errorMessage(wrapActionErr(models.ErrCustomFieldDoesNotExist{CustomFieldID: 3}))
		assert.Equal(t, "20001", code)
		assert.Empty(t, params)
	})
	t.Run("a failed script is framed by its kind", func(t *testing.T) {
		code, params := errorMessage(mapScriptError(&script.Error{Kind: script.ErrThrown, Message: "boom"}))
		assert.Equal(t, "script_thrown", code)
		assert.Equal(t, "boom", params["message"])
	})
	t.Run("the machinery of the scripts failing is not the script", func(t *testing.T) {
		code, _ := errorMessage(&script.UnavailableError{Msg: "busy", Busy: true})
		assert.Equal(t, CodeScriptUnavailable, code)
	})
	t.Run("a deadline and a deleted task have their own codes", func(t *testing.T) {
		code, _ := errorMessage(fmt.Errorf("step: %w", context.DeadlineExceeded))
		assert.Equal(t, CodeStepTimeout, code)
		code, _ = errorMessage(ErrTaskGone)
		assert.Equal(t, CodeTaskGone, code)
	})
	t.Run("anything else is unexpected, with the technical text as detail", func(t *testing.T) {
		code, params := errorMessage(errors.New("pq: deadlock detected"))
		assert.Equal(t, CodeUnexpected, code)
		assert.Equal(t, "pq: deadlock detected", params["detail"])
	})
	t.Run("no error has no code", func(t *testing.T) {
		code, params := errorMessage(nil)
		assert.Empty(t, code)
		assert.Nil(t, params)
	})
}

func TestMaskedErrorKeepsNoSecretInItsValues(t *testing.T) {
	env := &Env{secrets: &secretStore{loaded: true, accessed: map[string]bool{"TOKEN": true}, values: map[string]*models.AutomationSecretValue{"TOKEN": {Value: "s3cr3t-value"}}}}
	err := env.maskErr(actionErr("request_failed", P{"response": "denied for s3cr3t-value"}, "denied for s3cr3t-value"))
	code, params := errorMessage(err)
	assert.Equal(t, "request_failed", code)
	assert.NotContains(t, params["response"], "s3cr3t-value")
	assert.NotContains(t, err.Error(), "s3cr3t-value")
}

func TestRunsAndStepsStoreTheCodeOfWhatHappened(t *testing.T) {
	t.Run("a failed step and its run carry the code and the values of the failure", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1, act(spec.ActionSetField, map[string]any{"field": "title", "value": "   "}))
		require.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, "the title would be empty", run.Error, "the English text stays for readers of the API")
		assert.Equal(t, "title_empty", run.ErrorCode)
		require.Len(t, steps, 1)
		assert.Equal(t, "title_empty", steps[0].ErrorCode)
	})

	t.Run("the values of a failure are stored", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1, act(spec.ActionSetField, map[string]any{"field": "hex_color", "value": "blue"}))
		require.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, "hex_color_invalid", run.ErrorCode)
		assert.Equal(t, map[string]string{"value": "blue"}, run.ErrorParams)
		assert.Equal(t, map[string]string{"value": "blue"}, steps[0].ErrorParams)
	})

	t.Run("a refusal of a model is stored with its numeric API code", func(t *testing.T) {
		reset(t)
		run, _ := runRaw(t, rawAct(spec.ActionSetCustomField, "field_id", 987654, "value", "x"))
		require.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, "20001", run.ErrorCode, run.Error)
	})

	t.Run("a successful step stores what it did as a sentence code", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1, act(spec.ActionSetField, map[string]any{"field": "title", "value": "Yeni başlık"}))
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		require.Len(t, steps, 1)
		require.Len(t, steps[0].OutputMessages, 1)
		assert.Equal(t, "set_title", steps[0].OutputMessages[0].Code)
		assert.Equal(t, map[string]string{"title": "Yeni başlık"}, steps[0].OutputMessages[0].Params)
		assert.Empty(t, run.ErrorCode)
	})

	t.Run("a rule that changes nothing says so with the code of what it would have done", func(t *testing.T) {
		reset(t)
		_, _, steps := runAction(t, projectOwned, user1, user1, act(spec.ActionSetDone, map[string]any{"done": false}))
		require.Len(t, steps, 1)
		require.Len(t, steps[0].OutputMessages, 1)
		assert.Equal(t, "nothing_to_do", steps[0].OutputMessages[0].Code)
		assert.Equal(t, "mark_not_done", steps[0].OutputMessages[0].Params["inner"])
	})

	t.Run("a refused answer of another server is framed, not translated", func(t *testing.T) {
		reset(t)
		allowLoopback(t)
		srv := countingServer(t, 404, "no such hook")
		run, steps := runHTTP(t, map[string]any{"url": srv.url + "/x"})
		require.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, "request_failed", steps[0].ErrorCode)
		assert.Equal(t, "404", steps[0].ErrorParams["status"])
		assert.Contains(t, steps[0].ErrorParams["response"], "no such hook")
		assert.Empty(t, steps[0].OutputMessages, "an HTTP answer is data")
	})

	t.Run("an address that is not allowed has a code of its own", func(t *testing.T) {
		reset(t)
		run, steps := runRaw(t, rawAct(spec.ActionHTTPRequest, "url", "ftp://example.com/x"))
		require.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, "url_scheme", steps[0].ErrorCode)
	})

	t.Run("a script that throws is framed with its own words inside", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1, scriptAction(`throw new Error("kendi hatam")`))
		require.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, "script_thrown", run.ErrorCode)
		assert.Contains(t, steps[0].ErrorParams["message"], "kendi hatam")
	})

	t.Run("a dry run says the same in codes", func(t *testing.T) {
		reset(t)
		task := newTask(t, projectOwned, "deneme", user1)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil,
			act(spec.ActionSetField, map[string]any{"field": "title", "value": "Yeni {{unknown}}"}),
			act(spec.ActionSetField, map[string]any{"field": "priority", "value": 9}),
		))
		result, err := DryRun(context.Background(), r, task.ID)
		require.NoError(t, err)
		require.Len(t, result.Steps, 2)
		assert.Equal(t, "set_title", result.Steps[0].SummaryMessage.Code)
		require.Len(t, result.Steps[0].WarningMessages, 1)
		assert.Equal(t, "unknown_placeholder", result.Steps[0].WarningMessages[0].Code)
		assert.Equal(t, "priority_invalid", result.Steps[1].ErrorCode)
		require.GreaterOrEqual(t, len(result.NoteMessages), 2)
		assert.Equal(t, "note_simulated", result.NoteMessages[0].Code)
		assert.Len(t, result.NoteMessages, len(result.Notes))
	})
}

// runRaw runs actions that the editor would not let a person save, as a rule stored without validation.
func runRaw(t *testing.T, actions ...*spec.Action) (*models.AutomationRun, []*models.AutomationRunStep) {
	t.Helper()
	r := insertRawRule(t, projectOwned, user1, spec.TriggerTaskCreated, actions...)
	newTask(t, projectOwned, "Original title", user1)
	drain(t)
	runs := allRuns(t, r.ID)
	require.Len(t, runs, 1)
	return runs[0], runSteps(t, runs[0].ID)
}
