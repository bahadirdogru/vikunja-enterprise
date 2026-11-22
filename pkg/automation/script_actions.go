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
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"code.vikunja.io/api/pkg/automation/outbound"
	"code.vikunja.io/api/pkg/automation/script"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"
)

// maxHTTPOutput is how much of an answer goes into the output of a step.
const maxHTTPOutput = 2048

func (e *Env) ctx() context.Context {
	if e.Ctx != nil {
		return e.Ctx
	}
	return context.Background()
}

// mapScriptError turns a failed script into an error of the run: the script itself failed, so it will fail again
// (permanent). A cancelled context is not the script's fault and stays what it is.
func mapScriptError(err error) error {
	var scriptErr *script.Error
	if errors.As(err, &scriptErr) {
		return &ActionError{Msg: scriptErr.Error()}
	}
	return err
}

var errScriptsDisabled = &ActionError{Msg: "scripts are disabled on this instance (automation.scripts.enabled is false)"}

// scriptInput builds what a script sees of the world: the event, the task as it is now and the project.
func (e *Env) scriptInput(source string, host script.Host, limits script.Limits, condition bool) (*script.Input, error) {
	in := &script.Input{
		Source:    source,
		Event:     eventMap(e.Event),
		Host:      host,
		Limits:    limits,
		Condition: condition,
	}
	if e.Sub != nil {
		snap, err := taskSnapshot(e.S, e.Sub)
		if err != nil {
			return nil, err
		}
		in.Task = snap
	}
	project := e.Sub != nil && e.Sub.Project != nil
	var p *models.Project
	if project {
		p = e.Sub.Project
	} else {
		var err error
		if p, err = models.GetProjectSimpleByID(e.S, e.Rule.ProjectID); err != nil {
			return nil, err
		}
	}
	in.Project = map[string]any{"id": p.ID, "title": p.Title}
	return in, nil
}

// scriptCondition evaluates a script condition. It always gets the read-only host, even in a real run.
func (e *Env) scriptCondition(source string) (bool, error) {
	if !script.Enabled() {
		return false, errScriptsDisabled
	}
	in, err := e.scriptInput(source, newReadOnlyHost(e), script.ConditionLimits(), true)
	if err != nil {
		return false, err
	}
	res, err := script.Run(e.ctx(), in)
	if err != nil {
		return false, e.maskErr(mapScriptError(err))
	}
	return res.Truthy, nil
}

func scriptSummary(res *script.Result, extra []string) string {
	lines := []string{fmt.Sprintf("script finished in %d ms (%d vikunja calls, %d http requests)", res.Duration.Milliseconds(), res.APICalls, res.HTTPRequests)}
	lines = append(lines, extra...)
	if res.Log != "" {
		lines = append(lines, "log:\n"+res.Log)
	}
	if res.Returned {
		lines = append(lines, "result: "+res.Value)
	}
	return strings.Join(lines, "\n")
}

func planScript(env *Env, p params) (*Plan, error) {
	if !script.Enabled() {
		return nil, errScriptsDisabled
	}
	source := p.str("source")
	if strings.TrimSpace(source) == "" {
		return nil, actionErrorf("the script is empty")
	}
	if max := script.MaxSource(); max > 0 && len(source) > max {
		return nil, actionErrorf("the script is %d bytes long, at most %d are allowed", len(source), max)
	}
	// Loaded before anything runs, so everything the step prints can be checked against the secrets.
	if err := env.store().load(); err != nil {
		return nil, err
	}

	plan := &Plan{Summary: "run a script"}
	// The two phases build their own host: apply the one that writes, preview the one that records. Nothing
	// else decides.
	plan.apply = func() (string, error) {
		in, err := env.scriptInput(source, newLiveHost(env), script.ActionLimits(), false)
		if err != nil {
			return "", err
		}
		res, err := script.Run(env.ctx(), in)
		if err != nil {
			return "", env.maskErr(mapScriptError(err))
		}
		return env.maskOutput(scriptSummary(res, nil)), nil
	}
	plan.preview = func() (string, []string, error) {
		host := newDryHost(env)
		in, err := env.scriptInput(source, host, script.ActionLimits(), false)
		if err != nil {
			return "", nil, err
		}
		res, err := script.Run(env.ctx(), in)
		if err != nil {
			return "", nil, env.maskErr(mapScriptError(err))
		}
		return env.maskOutput(scriptSummary(res, host.calls)), nil, nil
	}
	return plan, nil
}

// httpRequestError classifies a failed outbound request for the engine's retry semantics: a request that cannot
// work as configured fails the run; a network error or a 5xx answer is temporary.
func httpRequestError(err error) error {
	var blocked *outbound.BlockedError
	var invalid *outbound.InvalidError
	switch {
	case errors.As(err, &blocked), errors.As(err, &invalid):
		return &ActionError{Msg: err.Error()}
	}
	return err
}

func planHTTPRequest(env *Env, p params) (*Plan, error) {
	store := env.store()
	if err := store.load(); err != nil {
		return nil, err
	}
	plan := &Plan{}

	var secretErr error
	resolve := func(name string) (string, bool) {
		secretName, ok := strings.CutPrefix(name, "secrets.")
		if !ok {
			return "", false
		}
		v, err := store.get(secretName)
		if err != nil && secretErr == nil {
			secretErr = err
		}
		return v, true
	}
	unknownBefore := len(env.Unknown)
	renderSecret := func(text string) string { return renderWith(text, env.Sub, &env.Unknown, resolve) }

	// What a placeholder stands for is data, not URL syntax: it is escaped.
	escaped := func(name string) (string, bool) {
		v, ok := placeholderValue(name, env.Sub)
		if !ok {
			return "", false
		}
		return strings.ReplaceAll(url.QueryEscape(v), "+", "%20"), true
	}
	rawURL := strings.TrimSpace(renderWith(p.str("url"), env.Sub, &env.Unknown, escaped))
	if _, err := outbound.ValidateURL(rawURL); err != nil {
		return nil, &ActionError{Msg: err.Error()}
	}
	method := strings.ToUpper(p.str("method"))
	if method == "" {
		method = http.MethodPost
	}
	headers := map[string]string{}
	if raw, ok := p["headers"].(map[string]any); ok {
		for k, v := range raw {
			text, _ := v.(string)
			headers[k] = renderSecret(text)
		}
	}
	body := renderSecret(p.str("body"))
	if secretErr != nil {
		return nil, secretErr
	}
	renderedWarning(env, plan, unknownBefore)

	if name := p.str("secret"); name != "" {
		key, err := store.get(name)
		if err != nil {
			return nil, err
		}
		headers[outbound.SignatureHeader] = outbound.Sign(key, []byte(body))
	}
	if body != "" && !hasHeader(headers, "Content-Type") {
		if json.Valid([]byte(body)) {
			headers["Content-Type"] = "application/json"
		} else {
			headers["Content-Type"] = "text/plain; charset=utf-8"
		}
	}
	for k, v := range headers {
		if strings.ContainsAny(k+v, "\r\n\x00") {
			return nil, actionErrorf("the header %q contains a line break", k)
		}
	}

	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	plan.Summary = fmt.Sprintf("send %s %s with %d bytes of body and the headers %s", method, rawURL, len(body), strings.Join(names, ", "))

	plan.apply = func() (string, error) {
		resp, err := outbound.Do(env.ctx(), &outbound.Request{
			URL:         rawURL,
			Method:      method,
			Headers:     headers,
			Body:        []byte(body),
			MaxResponse: maxHTTPOutput,
			MaxRequest:  config.AutomationScriptsMaxResponseSize.GetInt(),
		})
		if err != nil {
			return "", env.maskErr(httpRequestError(err))
		}
		snippet := strings.TrimSpace(string(resp.Body))
		if resp.Truncated {
			snippet += "\n[answer truncated]"
		}
		line := fmt.Sprintf("%s %s: %d %s", method, rawURL, resp.Status, http.StatusText(resp.Status))
		switch {
		case resp.Status >= 500:
			// The other side has a problem: worth trying again later.
			return "", env.maskErr(fmt.Errorf("%s\n%s", line, snippet))
		case resp.Status >= 400:
			return "", env.maskErr(&ActionError{Msg: line + "\n" + snippet})
		}
		return line + "\n" + snippet, nil
	}
	return plan, nil
}

func hasHeader(h map[string]string, name string) bool {
	for k := range h {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

// ScriptLimitDocs lists the limits of scripts with their configured values, for the catalog.
func ScriptLimitDocs() []*spec.ScriptLimit {
	secs := func(k config.Key) string { return k.GetDuration().String() }
	num := func(k config.Key) string { return strconv.Itoa(k.GetInt()) }
	return []*spec.ScriptLimit{
		{Name: "Scripts enabled", ConfigKey: "automation.scripts.enabled", Value: strconv.FormatBool(config.AutomationScriptsEnabled.GetBool()), Description: "When off, rules with scripts cannot be saved and existing ones fail without running."},
		{Name: "Script timeout", ConfigKey: "automation.scripts.timeout", Value: secs(config.AutomationScriptsTimeout), Description: "The script is stopped and the step fails."},
		{Name: "Condition script timeout", ConfigKey: "automation.scripts.conditiontimeout", Value: secs(config.AutomationScriptsConditionTimeout), Description: "The condition script is stopped and the run fails."},
		{Name: "Maximum script size (bytes)", ConfigKey: "automation.scripts.maxsource", Value: num(config.AutomationScriptsMaxSource), Description: "Larger scripts cannot be saved."},
		{Name: "vikunja.* calls per run", ConfigKey: "automation.scripts.maxapicalls", Value: num(config.AutomationScriptsMaxAPICalls), Description: "Further calls throw an error."},
		{Name: "http.fetch requests per run", ConfigKey: "automation.scripts.maxhttprequests", Value: num(config.AutomationScriptsMaxHTTPRequests), Description: "Further requests throw an error."},
		{Name: "HTTP body size (bytes)", ConfigKey: "automation.scripts.maxresponsesize", Value: num(config.AutomationScriptsMaxResponseSize), Description: "Request bodies above it are refused, response bodies are cut off."},
		{Name: "Log output per step (bytes)", ConfigKey: "automation.scripts.maxlogsize", Value: num(config.AutomationScriptsMaxLogSize), Description: "The rest of the log is dropped."},
		{Name: "Call stack depth", ConfigKey: "automation.scripts.maxcallstack", Value: num(config.AutomationScriptsMaxCallStack), Description: "Deeper recursion fails the script."},
		{Name: "Tasks returned by vikunja.tasks.find", ConfigKey: "automation.scripts.maxresults", Value: num(config.AutomationScriptsMaxResults), Description: "Further tasks are left out."},
		{Name: "HTTP request timeout", ConfigKey: "automation.scripts.httptimeout", Value: secs(config.AutomationHTTPTimeout), Description: "Applies to every outgoing request, whatever a script asks for."},
	}
}
