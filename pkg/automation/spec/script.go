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

package spec

import (
	"fmt"
	"regexp"
	"strings"
)

// SecretNamePattern is what the name of a project secret has to look like.
var SecretNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

var secretPlaceholderPattern = regexp.MustCompile(`\{\{\s*secrets\.([A-Za-z0-9_.]*)\s*\}\}`)

// ScriptRef is the source of one script in a rule and where it is.
type ScriptRef struct {
	Path   string
	Source string
	// Condition is true for the script of a condition (read-only API, short timeout).
	Condition bool
}

// Scripts returns every script of the document in document order: script conditions and script.run actions.
func Scripts(d *Doc) []ScriptRef {
	var out []ScriptRef
	if d.Conditions != nil {
		walkConditions("conditions", d.Conditions, func(path string, c *Condition) {
			if c.Script != "" {
				out = append(out, ScriptRef{Path: path + ".script", Source: c.Script, Condition: true})
			}
		})
	}
	for i, a := range d.Actions {
		if a == nil {
			continue
		}
		path := fmt.Sprintf("actions[%d].params", i)
		switch a.Type {
		case ActionScriptRun:
			if src, ok := a.Params["source"].(string); ok {
				out = append(out, ScriptRef{Path: path + ".source", Source: src})
			}
		case ActionStopIf:
			if c, ok := ConditionFromParam(a.Params["condition"]); ok {
				walkConditions(path+".condition", c, func(p string, c *Condition) {
					if c.Script != "" {
						out = append(out, ScriptRef{Path: p + ".script", Source: c.Script, Condition: true})
					}
				})
			}
		}
	}
	return out
}

// HasScripts reports whether the document contains any script.
func HasScripts(d *Doc) bool { return len(Scripts(d)) > 0 }

func walkConditions(path string, c *Condition, fn func(path string, c *Condition)) {
	if c == nil {
		return
	}
	fn(path, c)
	for name, group := range map[string][]*Condition{"all": c.All, "any": c.Any, "not": c.Not} {
		for i, child := range group {
			walkConditions(fmt.Sprintf("%s.%s[%d]", path, name, i), child, fn)
		}
	}
}

// SecretRef is a reference to a project secret by name.
type SecretRef struct {
	Name string
	Path string
}

// SecretRefs returns the secrets the document refers to statically: the secret parameter of http.request and the
// {{secrets.NAME}} placeholders of its headers and body. A script can also ask for a secret by a name it builds at
// run time; that cannot be seen here.
func SecretRefs(d *Doc) []SecretRef {
	var out []SecretRef
	for i, a := range d.Actions {
		if a == nil || a.Type != ActionHTTPRequest {
			continue
		}
		path := fmt.Sprintf("actions[%d].params", i)
		if name, ok := a.Params["secret"].(string); ok && name != "" {
			out = append(out, SecretRef{Name: name, Path: path + ".secret"})
		}
		if body, ok := a.Params["body"].(string); ok {
			for _, m := range secretPlaceholderPattern.FindAllStringSubmatch(body, -1) {
				out = append(out, SecretRef{Name: m[1], Path: path + ".body"})
			}
		}
		if headers, ok := a.Params["headers"].(map[string]any); ok {
			for name, v := range headers {
				if text, ok := v.(string); ok {
					for _, m := range secretPlaceholderPattern.FindAllStringSubmatch(text, -1) {
						out = append(out, SecretRef{Name: m[1], Path: path + ".headers." + name})
					}
				}
			}
		}
	}
	return out
}

// UsesSecrets reports whether the document refers to secrets, statically or in the source of a script.
func UsesSecrets(d *Doc) bool {
	if len(SecretRefs(d)) > 0 {
		return true
	}
	for _, s := range Scripts(d) {
		if strings.Contains(s.Source, "secrets") {
			return true
		}
	}
	return false
}

// SecretPlaceholders returns the names used in {{secrets.NAME}} placeholders of a text.
func SecretPlaceholders(text string) []string {
	var names []string
	for _, m := range secretPlaceholderPattern.FindAllStringSubmatch(text, -1) {
		names = append(names, m[1])
	}
	return names
}

// ScriptDoc is the reference for the JavaScript of rules. The editor generates its help panel from it.
type ScriptDoc struct {
	Language            string          `json:"language" doc:"Always javascript (ECMAScript 5.1 plus most of ES6, pure Go interpreter)."`
	ActionConvention    string          `json:"action_convention" doc:"How a script action returns a result."`
	ConditionConvention string          `json:"condition_convention" doc:"How a script condition returns its answer."`
	Limits              []*ScriptLimit  `json:"limits" doc:"The limits a script runs under. The values are the ones configured on this instance."`
	API                 []*ScriptAPIDef `json:"api" doc:"Every global and function available to scripts."`
}

// ScriptLimit is one limit of scripts.
type ScriptLimit struct {
	Name        string `json:"name" doc:"The name of the limit."`
	ConfigKey   string `json:"config_key" doc:"The configuration key that sets it."`
	Value       string `json:"value" doc:"The configured value."`
	Description string `json:"description" doc:"What happens when the limit is reached."`
}

// ScriptAPIDef describes one global or function of the script API.
type ScriptAPIDef struct {
	Name        string `json:"name" doc:"The name as written in a script, e.g. vikunja.tasks.update."`
	Signature   string `json:"signature" doc:"The signature, e.g. vikunja.tasks.update(id, patch) -> task."`
	Description string `json:"description" doc:"Plain English description."`
	Mutating    bool   `json:"mutating" doc:"True when the call changes data or contacts another system. A test run records these instead of performing them."`
	InCondition bool   `json:"in_condition" doc:"Whether the call is available in a script condition (read-only)."`
}

// ScriptReference returns the static part of the script reference. Limits are filled in by the caller from the
// configuration.
func ScriptReference() *ScriptDoc {
	api := func(name, sig, desc string, mutating, inCondition bool) *ScriptAPIDef {
		return &ScriptAPIDef{Name: name, Signature: sig, Description: desc, Mutating: mutating, InCondition: inCondition}
	}
	return &ScriptDoc{
		Language:            "javascript",
		ActionConvention:    "The source is the body of a function: write statements and use return to give a result. The result is stored as JSON in the output of the step. A thrown error fails the step; the message and the line and column are recorded. The code runs in strict mode.",
		ConditionConvention: "The source is the body of a function that must return a value with return; it is converted to true or false. A script that returns nothing (undefined) or ends with a bare expression instead of return fails with a clear error. Condition scripts have a read-only API and a shorter timeout.",
		Limits:              []*ScriptLimit{},
		API: []*ScriptAPIDef{
			api("event", "event: {name, task_id, project_id, doer: {id, username, name}, old, new, changed, field_id, payload}", "The trigger event. Read-only (frozen). old and new hold the values the event knows (see the condition fields event.old.* and event.new.*). payload is the JSON body of an incoming webhook, {} for other triggers.", false, true),
			api("task", "task: {id, title, description, done, priority, percent_done, due_date, start_date, end_date, done_at, created, updated, hex_color, identifier, project_id, created_by, assignees, labels, status, custom_fields, relations} | null", "The task the rule runs for, as it is when the script starts. Dates are RFC 3339 strings or null; assignees and labels are lists of ids; status is {id, title, category} or null; custom_fields maps the id of a custom field to its value; relations maps a relation kind (subtask, parenttask, related, blocking, ...) to the list of task ids. Frozen. null for task.deleted.", false, true),
			api("project", "project: {id, title}", "The project of the rule. Frozen.", false, true),
			api("vikunja.tasks.get", "vikunja.tasks.get(id) -> task", "Reads a task the rule owner may read. Same shape as the global task.", false, true),
			api("vikunja.tasks.find", "vikunja.tasks.find(filter, options?) -> task[]", "Finds tasks with the filter syntax of the task list, for example \"done = false && cf_12 > 5\". Only tasks the rule owner may read are returned, at most the configured maximum. options.project_id limits the search to one project. The result is not paginated.", false, true),
			api("vikunja.tasks.update", "vikunja.tasks.update(id, patch) -> task", "Changes a task as the rule owner. patch may contain title, description, priority (0-5), percent_done (0-1), hex_color, done, and due_date, start_date, end_date as an RFC 3339 string, a plain date, a relative value (+3d, -12h, now+1w) or \"none\" to clear. Plain text is used as it is; {{placeholders}} are not replaced.", true, false),
			api("vikunja.tasks.create", "vikunja.tasks.create(projectId, data) -> task", "Creates a task in a project the rule owner may write to. data: title (required), description, priority, percent_done, hex_color, due_date, start_date, end_date (as in update), assignees (list of user ids) and parent_id (makes the new task a subtask of that task).", true, false),
			api("vikunja.tasks.comment", "vikunja.tasks.comment(id, text)", "Adds a comment (plain text) to a task.", true, false),
			api("vikunja.fields.get", "vikunja.fields.get(taskId, fieldId) -> value", "Reads the value of a custom field of a task: a number, string, boolean, a list for multi-select and user fields, or null.", false, true),
			api("vikunja.fields.set", "vikunja.fields.set(taskId, fieldId, value)", "Sets a custom field of a task. Select fields take option ids, date fields take date strings, null clears the value.", true, false),
			api("vikunja.status.set", "vikunja.status.set(taskId, statusId, comment?)", "Moves a task to a workflow status through the normal transition checks of the project.", true, false),
			api("vikunja.assignees.add", "vikunja.assignees.add(taskId, userId)", "Assigns a user to a task. Does nothing when the user is assigned already.", true, false),
			api("vikunja.assignees.remove", "vikunja.assignees.remove(taskId, userId)", "Removes an assignee from a task.", true, false),
			api("vikunja.labels.add", "vikunja.labels.add(taskId, labelId)", "Adds a label to a task.", true, false),
			api("vikunja.labels.remove", "vikunja.labels.remove(taskId, labelId)", "Removes a label from a task.", true, false),
			api("http.fetch", "http.fetch(url, {method?, headers?, body?, timeout?}) -> {status, ok, headers, body, truncated, json()}", "Sends an HTTP request. Only http and https; private, loopback and link-local addresses are blocked; at most three redirects. method defaults to GET, headers is an object of strings, body a string, timeout in milliseconds (capped). body of the answer is cut off at the configured size. A network error or a blocked address throws; an HTTP error status does not. json() parses the body.", true, false),
			api("secrets.get", "secrets.get(name) -> string", "Returns the value of a project secret. Throws when the secret does not exist or cannot be decrypted. The value is masked in all stored output and errors. In a test run it returns a placeholder.", false, false),
			api("log", "log(...args)", "Writes to the output of the step. Objects are written as JSON. console.log is an alias. The total size is limited.", false, true),
		},
	}
}
