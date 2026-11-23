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
	"slices"
	"sort"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/outbound"
	"code.vikunja.io/api/pkg/automation/script"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"
)

// The script runtime talks to a script.Host. There are three implementations and the engine, not the script,
// decides which one a script gets:
//
//   - liveHost performs changes. Only the apply phase of a real run builds it.
//   - dryHost only records what a live host would do. Only the preview of a dry run builds it.
//   - readOnlyHost refuses everything but reading. Script conditions always get it.
//
// All three share hostBase, which reads and which builds the mutations (the very plans the built-in actions use);
// they differ only in what they do with a built mutation. There is no "if dry run" anywhere else.

// hostBase has the reads and the mutation builders the hosts share.
type hostBase struct {
	env *Env
}

// taskEnv returns a copy of the environment whose subject is the given task. Text is not rendered as a template,
// because what a script passes is data.
func (h *hostBase) taskEnv(taskID int64) (*Env, error) {
	e := *h.env
	e.Literal = true
	e.Unknown = nil
	payload := &Payload{TaskID: taskID}
	if h.env.Event != nil {
		p := *h.env.Event
		p.TaskID = taskID
		// Never the name of a deletion, which would let LoadSubject answer for a task that is gone.
		p.Name = "script"
		p.Task = nil
		payload = &p
	}
	e.Event = payload
	if err := e.Reload(); err != nil {
		if errors.Is(err, ErrTaskGone) {
			return nil, actionErrorf("the task %d does not exist", taskID)
		}
		return nil, err
	}
	return &e, nil
}

// readableTaskEnv is taskEnv after a check that the owner of the rule may read the task.
func (h *hostBase) readableTaskEnv(taskID int64) (*Env, error) {
	can, _, err := (&models.Task{ID: taskID}).CanRead(h.env.S, h.env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}
	return h.taskEnv(taskID)
}

func (h *hostBase) TaskGet(_ context.Context, id int64) (map[string]any, error) {
	e, err := h.readableTaskEnv(id)
	if err != nil {
		return nil, err
	}
	return taskSnapshot(e.S, e.Sub)
}

func (h *hostBase) TaskFind(_ context.Context, filter string, projectID int64, limit int) ([]map[string]any, error) {
	if strings.TrimSpace(filter) == "" {
		return nil, actionErrorf("the filter must not be empty")
	}
	tc := &models.TaskCollection{Filter: filter, ProjectID: projectID, FilterTimezone: h.env.Auth.Timezone}
	result, _, _, err := tc.ReadAll(h.env.S, h.env.Auth, "", 1, limit)
	if err != nil {
		return nil, err
	}
	found, ok := result.([]*models.Task)
	if !ok {
		return nil, actionErrorf("the filter did not produce a list of tasks")
	}
	out := make([]map[string]any, 0, len(found))
	for _, t := range found {
		e, err := h.taskEnv(t.ID)
		if err != nil {
			return nil, err
		}
		snap, err := taskSnapshot(e.S, e.Sub)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
}

func (h *hostBase) FieldGet(_ context.Context, taskID, fieldID int64) (any, error) {
	e, err := h.readableTaskEnv(taskID)
	if err != nil {
		return nil, err
	}
	if _, known := e.Sub.CFTypes[fieldID]; !known {
		return nil, models.ErrCustomFieldDoesNotExist{CustomFieldID: fieldID}
	}
	return e.Sub.Fields[cfKey(fieldID)], nil
}

// mutation is a change a script asked for, built from the same plans the actions use. Nothing has been written
// when it exists.
type mutation struct {
	// call is how the call reads in a script, for the record of a dry run.
	call  string
	plans []*Plan
	// result reads the state after the plans were applied (live).
	result func() (map[string]any, error)
	// stub answers instead of result when nothing was applied (dry run).
	stub func() (map[string]any, error)
}

func callText(name string, args ...any) string {
	parts := make([]string, len(args))
	for i, a := range args {
		raw, err := json.Marshal(a)
		if err != nil {
			raw = []byte(fmt.Sprint(a))
		}
		if len(raw) > 300 {
			raw = append(raw[:300], []byte("...")...)
		}
		parts[i] = string(raw)
	}
	return fmt.Sprintf("%s(%s)", name, strings.Join(parts, ", "))
}

var updatableFields = []string{"title", "description", "priority", "percent_done", "hex_color", "due_date", "start_date", "end_date", "done"}

func (h *hostBase) planUpdate(id int64, patch map[string]any) (*mutation, error) {
	e, err := h.taskEnv(id)
	if err != nil {
		return nil, err
	}
	// Every plan below checks this too, but an empty patch has no plan and still answers with the task.
	if _, err := requireWritableTask(e); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	m := &mutation{call: callText("vikunja.tasks.update", id, patch)}
	for _, key := range keys {
		value := patch[key]
		var plan *Plan
		switch key {
		case "done":
			plan, err = planSetDone(e, params{"done": value})
		case "due_date", "start_date", "end_date":
			if value == nil {
				value = "none"
			}
			plan, err = planSetField(e, params{"field": key, "value": value})
		case "title", "description", "priority", "percent_done", "hex_color":
			plan, err = planSetField(e, params{"field": key, "value": value})
		default:
			return nil, actionErrorf("the field %q cannot be changed; allowed: %s", key, strings.Join(updatableFields, ", "))
		}
		if err != nil {
			return nil, err
		}
		m.plans = append(m.plans, plan)
	}
	m.result = func() (map[string]any, error) { return h.snapshotOf(id) }
	m.stub = func() (map[string]any, error) {
		snap, err := taskSnapshot(e.S, e.Sub)
		if err != nil || snap == nil {
			return snap, err
		}
		for k, v := range patch {
			snap[k] = v
		}
		return snap, nil
	}
	return m, nil
}

func (h *hostBase) snapshotOf(id int64) (map[string]any, error) {
	e, err := h.taskEnv(id)
	if err != nil {
		return nil, err
	}
	return taskSnapshot(e.S, e.Sub)
}

//nolint:gocyclo
func (h *hostBase) planCreate(projectID int64, data map[string]any) (*mutation, error) {
	env := h.env
	title, _ := data["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, actionErrorf("the title is required")
	}
	created := &models.Task{Title: title, ProjectID: projectID}
	if d, ok := data["description"].(string); ok {
		created.Description = d
	}
	if v, has := data["priority"]; has && v != nil {
		f, ok := spec.ToFloat64(v)
		if !ok || f < 0 || f > 5 || f != float64(int64(f)) {
			return nil, actionErrorf("the priority must be a whole number from 0 to 5")
		}
		created.Priority = int64(f)
	}
	if v, has := data["percent_done"]; has && v != nil {
		f, ok := spec.ToFloat64(v)
		if !ok || f < 0 || f > 1 {
			return nil, actionErrorf("percent_done must be a number from 0 to 1")
		}
		created.PercentDone = f
	}
	if v, has := data["hex_color"]; has && v != nil {
		s, _ := v.(string)
		s = strings.TrimSpace(s)
		if s != "" {
			m := hexColorPattern.FindStringSubmatch(s)
			if m == nil {
				return nil, actionErrorf("%q is not a hex color like #ff8800", s)
			}
			created.HexColor = strings.ToLower(m[1])
		}
	}
	for field, target := range map[string]*time.Time{"due_date": &created.DueDate, "start_date": &created.StartDate, "end_date": &created.EndDate} {
		v, has := data[field]
		if !has || v == nil {
			continue
		}
		s, _ := v.(string)
		d, err := spec.ParseDateSpec(s, true)
		if err != nil {
			return nil, actionErrorf("%s: %s", field, err.Error())
		}
		*target = d.Resolve(env.Now)
	}
	if v, has := data["assignees"]; has && v != nil {
		list, ok := v.([]any)
		if !ok {
			return nil, actionErrorf("assignees must be a list of user ids")
		}
		for _, item := range list {
			id, ok := spec.ToInt64(item)
			if !ok || id < 1 {
				return nil, actionErrorf("assignees must be a list of user ids")
			}
			u, err := user.GetUserByID(env.S, id)
			if err != nil {
				return nil, err
			}
			created.Assignees = append(created.Assignees, u)
		}
	}
	var parentID int64
	if v, has := data["parent_id"]; has && v != nil {
		id, ok := spec.ToInt64(v)
		if !ok || id < 1 {
			return nil, actionErrorf("parent_id must be a task id")
		}
		parentID = id
		can, err := (&models.Task{ID: parentID}).CanWrite(env.S, env.Auth)
		if err != nil {
			return nil, err
		}
		if !can {
			return nil, models.ErrGenericForbidden{}
		}
	}
	can, err := created.CanCreate(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}

	plan := &Plan{Summary: fmt.Sprintf("create the task %q in project %d", title, projectID)}
	plan.apply = func() (string, error) {
		if err := created.Create(env.S, env.Auth); err != nil {
			return "", err
		}
		if err := recordTaskOrigin(env.S, created.ID); err != nil {
			return "", err
		}
		if parentID != 0 {
			relation := &models.TaskRelation{TaskID: parentID, OtherTaskID: created.ID, RelationKind: models.RelationKindSubtask}
			can, err := relation.CanCreate(env.S, env.Auth)
			if err != nil {
				return "", err
			}
			if !can {
				return "", models.ErrGenericForbidden{}
			}
			if err := relation.Create(env.S, env.Auth); err != nil {
				return "", err
			}
		}
		return plan.Summary, nil
	}
	return &mutation{
		call:   callText("vikunja.tasks.create", projectID, data),
		plans:  []*Plan{plan},
		result: func() (map[string]any, error) { return h.snapshotOf(created.ID) },
		stub: func() (map[string]any, error) {
			return map[string]any{"id": int64(-1), "title": title, "project_id": projectID, "done": false}, nil
		},
	}, nil
}

func (h *hostBase) planComment(id int64, text string) (*mutation, error) {
	e, err := h.taskEnv(id)
	if err != nil {
		return nil, err
	}
	plan, err := planAddComment(e, params{"text": text})
	if err != nil {
		return nil, err
	}
	return &mutation{call: callText("vikunja.tasks.comment", id, text), plans: []*Plan{plan}}, nil
}

func (h *hostBase) planFieldSet(taskID, fieldID int64, value any) (*mutation, error) {
	e, err := h.taskEnv(taskID)
	if err != nil {
		return nil, err
	}
	plan, err := planSetCustomField(e, params{"field_id": fieldID, "value": value})
	if err != nil {
		return nil, err
	}
	return &mutation{call: callText("vikunja.fields.set", taskID, fieldID, value), plans: []*Plan{plan}}, nil
}

func (h *hostBase) planStatusSet(taskID, statusID int64, comment string) (*mutation, error) {
	e, err := h.taskEnv(taskID)
	if err != nil {
		return nil, err
	}
	plan, err := planSetStatus(e, params{"status_id": statusID, "comment": comment})
	if err != nil {
		return nil, err
	}
	return &mutation{call: callText("vikunja.status.set", taskID, statusID, comment), plans: []*Plan{plan}}, nil
}

func (h *hostBase) planAssignee(taskID, userID int64, add bool) (*mutation, error) {
	e, err := h.taskEnv(taskID)
	if err != nil {
		return nil, err
	}
	plan, err := planAssign(e, params{"user": userID}, add)
	if err != nil {
		return nil, err
	}
	name := map[bool]string{true: "vikunja.assignees.add", false: "vikunja.assignees.remove"}[add]
	return &mutation{call: callText(name, taskID, userID), plans: []*Plan{plan}}, nil
}

func (h *hostBase) planLabel(taskID, labelID int64, add bool) (*mutation, error) {
	e, err := h.taskEnv(taskID)
	if err != nil {
		return nil, err
	}
	plan, err := planLabel(e, params{"label_id": labelID}, add)
	if err != nil {
		return nil, err
	}
	name := map[bool]string{true: "vikunja.labels.add", false: "vikunja.labels.remove"}[add]
	return &mutation{call: callText(name, taskID, labelID), plans: []*Plan{plan}}, nil
}

// liveHost performs the changes, as the owner of the rule, in the session of the step. Because it is the session
// of the step, the events of its changes carry the origin of the run (markSession in runner.go): the depth limit
// and the self-trigger guard apply to what scripts do like to what actions do.
type liveHost struct {
	*hostBase
	fetchLimit int
}

func newLiveHost(env *Env) *liveHost {
	return &liveHost{hostBase: &hostBase{env: env}, fetchLimit: config.AutomationScriptsMaxResponseSize.GetInt()}
}

func (h *liveHost) commit(m *mutation, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	for _, plan := range m.plans {
		if plan.apply == nil {
			continue
		}
		if _, err := plan.apply(); err != nil {
			return nil, err
		}
	}
	if m.result == nil {
		return nil, nil
	}
	return m.result()
}

func (h *liveHost) TaskUpdate(_ context.Context, id int64, patch map[string]any) (map[string]any, error) {
	return h.commit(h.planUpdate(id, patch))
}

func (h *liveHost) TaskCreate(_ context.Context, projectID int64, data map[string]any) (map[string]any, error) {
	return h.commit(h.planCreate(projectID, data))
}

func (h *liveHost) TaskComment(_ context.Context, id int64, text string) error {
	_, err := h.commit(h.planComment(id, text))
	return err
}

func (h *liveHost) FieldSet(_ context.Context, taskID, fieldID int64, value any) error {
	_, err := h.commit(h.planFieldSet(taskID, fieldID, value))
	return err
}

func (h *liveHost) StatusSet(_ context.Context, taskID, statusID int64, comment string) error {
	_, err := h.commit(h.planStatusSet(taskID, statusID, comment))
	return err
}

func (h *liveHost) AssigneeAdd(_ context.Context, taskID, userID int64) error {
	_, err := h.commit(h.planAssignee(taskID, userID, true))
	return err
}

func (h *liveHost) AssigneeRemove(_ context.Context, taskID, userID int64) error {
	_, err := h.commit(h.planAssignee(taskID, userID, false))
	return err
}

func (h *liveHost) LabelAdd(_ context.Context, taskID, labelID int64) error {
	_, err := h.commit(h.planLabel(taskID, labelID, true))
	return err
}

func (h *liveHost) LabelRemove(_ context.Context, taskID, labelID int64) error {
	_, err := h.commit(h.planLabel(taskID, labelID, false))
	return err
}

func (h *liveHost) SecretGet(_ context.Context, name string) (string, error) {
	if !spec.SecretNamePattern.MatchString(name) {
		return "", actionErrorf("%q is not a valid secret name", name)
	}
	return h.env.store().get(name)
}

func (h *liveHost) Fetch(ctx context.Context, req *script.FetchRequest) (*script.FetchResponse, error) {
	resp, err := outbound.Do(ctx, &outbound.Request{
		URL:         req.URL,
		Method:      req.Method,
		Headers:     req.Headers,
		Body:        []byte(req.Body),
		Timeout:     time.Duration(req.TimeoutMs) * time.Millisecond,
		MaxResponse: h.fetchLimit,
		MaxRequest:  h.fetchLimit,
	})
	if err != nil {
		return nil, err
	}
	return &script.FetchResponse{Status: resp.Status, Headers: resp.Headers, Body: string(resp.Body), Truncated: resp.Truncated}, nil
}

// dryHost records the mutations a live host would perform and answers with stubs. It has no way to write: the only
// thing it does with a mutation is read the plans' summaries.
type dryHost struct {
	*hostBase
	calls []string
}

func newDryHost(env *Env) *dryHost {
	return &dryHost{hostBase: &hostBase{env: env}}
}

func (h *dryHost) record(m *mutation, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	summaries := []string{}
	for _, plan := range m.plans {
		summaries = append(summaries, plan.Summary)
	}
	h.calls = append(h.calls, "would call "+m.call+": "+strings.Join(summaries, "; "))
	if m.stub == nil {
		return nil, nil
	}
	return m.stub()
}

func (h *dryHost) TaskUpdate(_ context.Context, id int64, patch map[string]any) (map[string]any, error) {
	return h.record(h.planUpdate(id, patch))
}

func (h *dryHost) TaskCreate(_ context.Context, projectID int64, data map[string]any) (map[string]any, error) {
	return h.record(h.planCreate(projectID, data))
}

func (h *dryHost) TaskComment(_ context.Context, id int64, text string) error {
	_, err := h.record(h.planComment(id, text))
	return err
}

func (h *dryHost) FieldSet(_ context.Context, taskID, fieldID int64, value any) error {
	_, err := h.record(h.planFieldSet(taskID, fieldID, value))
	return err
}

func (h *dryHost) StatusSet(_ context.Context, taskID, statusID int64, comment string) error {
	_, err := h.record(h.planStatusSet(taskID, statusID, comment))
	return err
}

func (h *dryHost) AssigneeAdd(_ context.Context, taskID, userID int64) error {
	_, err := h.record(h.planAssignee(taskID, userID, true))
	return err
}

func (h *dryHost) AssigneeRemove(_ context.Context, taskID, userID int64) error {
	_, err := h.record(h.planAssignee(taskID, userID, false))
	return err
}

func (h *dryHost) LabelAdd(_ context.Context, taskID, labelID int64) error {
	_, err := h.record(h.planLabel(taskID, labelID, true))
	return err
}

func (h *dryHost) LabelRemove(_ context.Context, taskID, labelID int64) error {
	_, err := h.record(h.planLabel(taskID, labelID, false))
	return err
}

// SecretGet checks that the secret can be read and returns a placeholder, never the value.
func (h *dryHost) SecretGet(_ context.Context, name string) (string, error) {
	if !spec.SecretNamePattern.MatchString(name) {
		return "", actionErrorf("%q is not a valid secret name", name)
	}
	if err := h.env.store().exists(name); err != nil {
		return "", err
	}
	return "[secret " + name + "]", nil
}

// Fetch records the request and answers with an empty JSON object. Nothing is sent.
func (h *dryHost) Fetch(_ context.Context, req *script.FetchRequest) (*script.FetchResponse, error) {
	if _, err := outbound.ValidateURL(req.URL); err != nil {
		return nil, err
	}
	h.calls = append(h.calls, fmt.Sprintf("would call %s", callText("http.fetch", req.URL, map[string]any{"method": req.Method, "headers": headerNames(req.Headers), "body_bytes": len(req.Body)})))
	return &script.FetchResponse{Status: 200, Headers: map[string]string{"content-type": "application/json"}, Body: "{}"}, nil
}

func headerNames(h map[string]string) []string {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

// readOnlyHost is what conditions get: reading works, everything else is refused with a clear message.
type readOnlyHost struct {
	*hostBase
}

func newReadOnlyHost(env *Env) *readOnlyHost {
	return &readOnlyHost{hostBase: &hostBase{env: env}}
}

func refused(call string) error {
	return actionErrorf("%s is not available in a condition script: conditions are read-only", call)
}

func (h *readOnlyHost) TaskUpdate(context.Context, int64, map[string]any) (map[string]any, error) {
	return nil, refused("vikunja.tasks.update")
}

func (h *readOnlyHost) TaskCreate(context.Context, int64, map[string]any) (map[string]any, error) {
	return nil, refused("vikunja.tasks.create")
}
func (h *readOnlyHost) TaskComment(context.Context, int64, string) error {
	return refused("vikunja.tasks.comment")
}
func (h *readOnlyHost) FieldSet(context.Context, int64, int64, any) error {
	return refused("vikunja.fields.set")
}
func (h *readOnlyHost) StatusSet(context.Context, int64, int64, string) error {
	return refused("vikunja.status.set")
}
func (h *readOnlyHost) AssigneeAdd(context.Context, int64, int64) error {
	return refused("vikunja.assignees.add")
}
func (h *readOnlyHost) AssigneeRemove(context.Context, int64, int64) error {
	return refused("vikunja.assignees.remove")
}
func (h *readOnlyHost) LabelAdd(context.Context, int64, int64) error {
	return refused("vikunja.labels.add")
}
func (h *readOnlyHost) LabelRemove(context.Context, int64, int64) error {
	return refused("vikunja.labels.remove")
}
func (h *readOnlyHost) Fetch(context.Context, *script.FetchRequest) (*script.FetchResponse, error) {
	return nil, refused("http.fetch")
}
func (h *readOnlyHost) SecretGet(context.Context, string) (string, error) {
	return "", refused("secrets.get")
}

// Compile-time check that every host implements the whole interface.
var (
	_ script.Host = (*liveHost)(nil)
	_ script.Host = (*dryHost)(nil)
	_ script.Host = (*readOnlyHost)(nil)
)
