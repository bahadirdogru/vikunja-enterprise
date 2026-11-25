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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"code.vikunja.io/api/pkg/automation/outbound"
	"code.vikunja.io/api/pkg/automation/script"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
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
	// The codes built from a name are derived from the source that names them, so a new date field or a new kind of
	// script error without a text fails this test.
	actions, err := os.ReadFile("actions.go")
	require.NoError(t, err)
	dateCase := regexp.MustCompile(`case ((?:"[a-z_]+_date",?\s*)+):`).FindStringSubmatch(string(actions))
	require.NotNil(t, dateCase, "the case of the date fields of the set field action was not found")
	dateFields := regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(dateCase[1], -1)
	require.Len(t, dateFields, 3)
	for _, f := range dateFields {
		found["set_"+f[1]] = true
		found["clear_"+f[1]] = true
	}
	scriptSrc, err := os.ReadFile("script/run.go")
	require.NoError(t, err)
	kinds := regexp.MustCompile(`\bErr\w+\s+ErrorKind = "([a-z_]+)"`).FindAllStringSubmatch(string(scriptSrc), -1)
	require.GreaterOrEqual(t, len(kinds), 8)
	for _, k := range kinds {
		found["script_"+k[1]] = true
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

func newTaskWithDescription(t *testing.T, description string) *models.Task {
	t.Helper()
	task := &models.Task{Title: "Original title", ProjectID: projectOwned, Description: description}
	mustCommit(t, func(s *xorm.Session) error { return task.Create(s, user1) })
	return task
}

func jsonSize(t *testing.T, v any) int {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return len(raw)
}

func TestValuesOfSentencesAreBounded(t *testing.T) {
	t.Run("a task description of a megabyte does not become a megabyte per step", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, act(spec.ActionAddComment, map[string]any{"text": "{{task.description}}"})))
		newTaskWithDescription(t, strings.Repeat("ş", 500_000))
		drain(t)
		runs := allRuns(t, r.ID)
		require.Len(t, runs, 1)
		require.Equal(t, models.AutomationRunSuccess, runs[0].Status, runs[0].Error)
		steps := runSteps(t, runs[0].ID)
		require.Len(t, steps, 1)
		require.NotEmpty(t, steps[0].OutputMessages)
		text := steps[0].OutputMessages[0].Params["text"]
		assert.LessOrEqual(t, utf8.RuneCountInString(text), maxParamRunes)
		assert.True(t, utf8.ValidString(text))
		assert.LessOrEqual(t, jsonSize(t, steps[0].OutputMessages), 16*1024)
		assert.LessOrEqual(t, len(steps[0].Output), maxOutputBytes)
	})

	t.Run("a thrown script error of tens of kilobytes is cut in the values of the run and the step", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1, scriptAction(`throw new Error("ç".repeat(30000))`))
		require.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, "script_thrown", run.ErrorCode)
		for _, params := range []map[string]string{run.ErrorParams, steps[0].ErrorParams} {
			assert.LessOrEqual(t, jsonSize(t, params), maxErrorParamsBytes+64)
			assert.True(t, utf8.ValidString(params["message"]))
		}
		assert.LessOrEqual(t, len(run.Error), 4000)
	})

	t.Run("a cut keeps multi-byte characters whole and says so", func(t *testing.T) {
		cut := boundParams(map[string]string{"v": strings.Repeat("ğ", 3000)}, maxErrorParamsBytes)
		assert.True(t, utf8.ValidString(cut["v"]))
		assert.Equal(t, maxParamRunes, utf8.RuneCountInString(cut["v"]))
		assert.True(t, strings.HasSuffix(cut["v"], "…"))
		assert.Equal(t, "kısa", boundParams(map[string]string{"v": "kısa"}, maxErrorParamsBytes)["v"])
	})

	t.Run("the values together stay within the bound", func(t *testing.T) {
		in := map[string]string{}
		for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
			in[k] = strings.Repeat("ü", 1000)
		}
		out := boundParams(in, maxErrorParamsBytes)
		size := 0
		for k, v := range out {
			size += len(k) + len(v)
			assert.True(t, utf8.ValidString(v))
		}
		assert.LessOrEqual(t, size, maxErrorParamsBytes)
	})

	t.Run("a secret is masked before the cut, so a cut cannot leave a piece of it", func(t *testing.T) {
		secret := "S3CR3T-VALUE-XYZ"
		env := &Env{secrets: &secretStore{loaded: true, accessed: map[string]bool{"TOKEN": true}, values: map[string]*models.AutomationSecretValue{"TOKEN": {Value: secret}}}}
		// The secret starts a few characters before the cut at 1000 runes.
		long := strings.Repeat("a", maxParamRunes-4) + secret + strings.Repeat("b", 50)
		err := env.maskErr(actionErr("request_failed", P{"response": long}, "%s", long))
		_, params := errorMessage(err)
		assert.NotContains(t, params["response"], "S3CR3T")
		assert.NotContains(t, params["response"], "S3C")
		// The sentences of a step are masked the same way.
		msgs := boundMessages(env.maskMessages([]*models.AutomationMessage{{Code: "add_comment", Params: map[string]string{"text": long}}}))
		assert.NotContains(t, msgs[0].Params["text"], "S3C")
	})

	t.Run("a step has at most a bounded number of sentences", func(t *testing.T) {
		many := make([]*models.AutomationMessage, 50)
		for i := range many {
			many[i] = say("unknown_placeholder", P{"name": strconv.Itoa(i)})
		}
		assert.Len(t, boundMessages(many), maxOutputMessages)
	})
}

func TestALimitInTheWaitIsSentInSecondsNotAsAGoDuration(t *testing.T) {
	reset(t)
	run, steps := runRaw(t, rawAct(spec.ActionWait, "duration", "9999d"))
	require.Equal(t, models.AutomationRunFailed, run.Status)
	assert.Equal(t, "wait_too_long", run.ErrorCode)
	_, err := strconv.ParseInt(steps[0].ErrorParams["max"], 10, 64)
	assert.NoError(t, err, "the limit is a number of seconds, the interface formats it: %q", steps[0].ErrorParams["max"])
}

func TestSentencesStayTheLeadingLinesOfTheOutput(t *testing.T) {
	t.Run("a summary is one line, whatever the value", func(t *testing.T) {
		plan := &Plan{}
		plan.say("set_custom_field", P{"field": 1, "value": "a\nb"}, "set custom field %d to %v", 1, "a\nb\r\nc")
		assert.NotContains(t, plan.Summary, "\n")
		assert.NotContains(t, plan.Summary, "\r")
		assert.Equal(t, "a\nb", plan.Msg.Params["value"], "the value of the code is the value itself")
	})

	t.Run("the warnings of an HTTP step come before the answer, so the sentences are its leading lines", func(t *testing.T) {
		reset(t)
		allowLoopback(t)
		srv := countingServer(t, 200, "line one\nline two")
		run, steps := runHTTP(t, map[string]any{"url": srv.url + "/x", "headers": map[string]any{"X-Note": "{{no.such.value}}"}})
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		require.Len(t, steps[0].OutputMessages, 1)
		assert.Equal(t, "unknown_placeholder", steps[0].OutputMessages[0].Code)
		lines := strings.Split(steps[0].Output, "\n")
		assert.True(t, strings.HasPrefix(lines[0], "warning: unknown placeholder"), lines[0])
		assert.Contains(t, steps[0].Output, "line two")
	})
}

func TestTruncateCutsOnACharacterBoundary(t *testing.T) {
	text := strings.Repeat("ş", 3000) // two bytes each
	for _, n := range []int{4000, 4001, 1, 2, 3} {
		cut := truncate(text, n)
		assert.True(t, utf8.ValidString(cut), "cut at %d bytes", n)
		assert.LessOrEqual(t, len(cut), n)
	}
	assert.Equal(t, "kısa", truncate("kısa", 4000))
	assert.Empty(t, truncate("ş", 1))
}

// A script that reads a secret and throws it as the end of a long text: the secret starts a few characters before the
// cut at 1000 runes. Masked after the cut, a piece of it would be left; it must not show anywhere, not even in part.
func TestASecretInAThrownScriptErrorNeverSurvivesTheCut(t *testing.T) {
	const secret = "S3CR3T-VALUE-XYZ"
	throwing := `throw new Error("a".repeat(985) + secrets.get("TOKEN") + "b".repeat(40))`
	noPiece := func(t *testing.T, where string, params map[string]string) {
		t.Helper()
		for k, v := range params {
			assert.NotContains(t, v, "S3C", "%s: %s", where, k)
		}
	}

	t.Run("an action of a run", func(t *testing.T) {
		reset(t)
		setSecret(t, projectOwned, "TOKEN", secret)
		_, run, steps := runAction(t, projectOwned, user1, user1, scriptAction(throwing))
		require.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Equal(t, "script_thrown", run.ErrorCode)
		assert.Contains(t, run.ErrorParams["message"], "***", "the secret is masked, not cut")
		noPiece(t, "run", run.ErrorParams)
		noPiece(t, "step", steps[0].ErrorParams)
		assert.NotContains(t, run.Error, "S3C")
	})

	t.Run("a test run of an action", func(t *testing.T) {
		reset(t)
		setSecret(t, projectOwned, "TOKEN", secret)
		task := newTask(t, projectOwned, "dry", user1)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil, nil, scriptAction(throwing)))
		res, err := DryRun(context.Background(), r, task.ID)
		require.NoError(t, err)
		noPiece(t, "dry run step", res.Steps[0].ErrorParams)
		assert.NotContains(t, res.Steps[0].Error, "S3C")
	})

	t.Run("the error of a script is mapped whole, the cut comes after the mask", func(t *testing.T) {
		// A condition cannot read secrets, so this is the path of a secret that reaches a failing script by another way
		// (a value of the task, say) and is masked by the secrets the step loaded.
		env := &Env{secrets: &secretStore{loaded: true, accessed: map[string]bool{"TOKEN": true}, values: map[string]*models.AutomationSecretValue{"TOKEN": {Value: secret}}}}
		thrown := &script.Error{Kind: script.ErrThrown, Message: strings.Repeat("a", 985) + secret + strings.Repeat("b", 40)}
		_, params := errorMessage(env.maskErr(mapScriptError(thrown)))
		noPiece(t, "mapped", params)
		assert.Contains(t, params["message"], "***")
	})
}

// errorMessage cuts the values of an error, so it must only be called with an error that was masked first. The places
// that store or send a failure (the runner and the dry run) get theirs from Env.maskErr or from errors that cannot
// hold a secret; code that builds or maps an error must use errorMessageRaw. A new caller elsewhere fails here.
func TestOnlyTheStoringCodeBoundsAnError(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	allowed := map[string]bool{"runner.go": true, "dryrun.go": true, "messages.go": true}
	call := regexp.MustCompile(`\berrorMessage\(`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		assert.False(t, call.Match(src), "%s calls errorMessage, which cuts: use errorMessageRaw and let the storing code cut after the mask", f)
	}
}

func TestWhatAnotherServerAnswersIsStoredAsValidText(t *testing.T) {
	cases := map[string]string{
		"a body cut inside a character": "a" + strings.Repeat("ş", 1500), // 3001 bytes, the limit is 2048
		"a Latin-1 page":                "caf\xe9 \xe8 d\xe9j\xe0",
	}
	for name, body := range cases {
		for _, status := range []int{200, 404} {
			t.Run(name+" "+strconv.Itoa(status), func(t *testing.T) {
				reset(t)
				allowLoopback(t)
				srv := countingServer(t, status, body)
				run, steps := runHTTP(t, map[string]any{"url": srv.url + "/x"})
				require.Len(t, steps, 1)
				assert.True(t, utf8.ValidString(steps[0].Output), "output")
				assert.True(t, utf8.ValidString(steps[0].Error), "error")
				assert.True(t, utf8.ValidString(run.Error), "run error")
				for _, p := range []map[string]string{steps[0].ErrorParams, run.ErrorParams} {
					for k, v := range p {
						assert.True(t, utf8.ValidString(v), "param %s", k)
					}
				}
				assert.EqualValues(t, 1, srv.count(), "the request is sent once")
			})
		}
	}

	t.Run("a long message of a notification is cut on a character", func(t *testing.T) {
		reset(t)
		_, run, steps := runAction(t, projectOwned, user1, user1, notifyAction(map[string]any{"message": "a" + strings.Repeat("ş", 900), "recipients": []any{"task_creator"}}))
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.True(t, utf8.ValidString(steps[0].Output))
		assert.True(t, utf8.ValidString(steps[0].OutputMessages[0].Params["message"]))
	})
}

func TestTheNotesOfATestRunAreBounded(t *testing.T) {
	reset(t)
	task := newTask(t, projectOwned, "dry", user1)
	r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskUpdated, nil, &spec.Condition{Script: `throw new Error("ç".repeat(30000))`}, setField("priority", 4)))
	res, err := DryRun(context.Background(), r, task.ID)
	require.NoError(t, err)
	var note *models.AutomationMessage
	for _, n := range res.NoteMessages {
		if n.Code == "note_conditions_fail" {
			note = n
		}
	}
	require.NotNil(t, note)
	size := 0
	for _, v := range note.Params {
		size += len(v)
		assert.True(t, utf8.ValidString(v))
	}
	assert.LessOrEqual(t, size, maxMessageParamBytes+64)
}
