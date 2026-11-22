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
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"xorm.io/xorm"
)

// ActionError is a failure caused by the rule or the state of the task, not by the infrastructure. Retrying it
// would fail the same way, so the run fails right away.
type ActionError struct {
	Msg string
}

func (e *ActionError) Error() string { return e.Msg }

func actionErrorf(format string, args ...any) error {
	return &ActionError{Msg: fmt.Sprintf(format, args...)}
}

// Env is everything an action works with.
type Env struct {
	// Ctx is cancelled when the step runs out of time.
	Ctx  context.Context
	S    *xorm.Session
	Auth *user.User
	Rule *models.AutomationRule
	// Event is the payload of the event that started the run.
	Event *Payload
	Now   time.Time
	// Sub is the current state of the task. Run it through Reload before each action.
	Sub *Subject
	// Unknown collects the placeholders that did not resolve.
	Unknown []string
	// Literal turns off template rendering: text parameters are used as they are. Scripts set it, because the
	// text they pass is data, not a template.
	Literal bool

	secrets *secretStore
}

// Reload refreshes the subject so an action sees what the previous ones did.
func (e *Env) Reload() error {
	taskID := int64(0)
	if e.Event != nil {
		taskID = e.Event.TaskID
	}
	sub, err := LoadSubject(e.S, taskID, e.Event, e.Now)
	if err != nil {
		return err
	}
	sub.Script = e.scriptCondition
	e.Sub = sub
	return nil
}

func (e *Env) render(text string) string {
	if e.Literal {
		return text
	}
	return Render(text, e.Sub, &e.Unknown)
}

// Plan is what an action would do. Planning reads but never writes; apply performs the writes. A dry run only
// plans, a real run plans and then applies.
type Plan struct {
	// Summary says in a sentence what the action does with the current state.
	Summary string
	// Warnings are problems that do not stop the action, e.g. a placeholder that rendered empty.
	Warnings []string
	// Stop ends the run successfully (flow.stop_if).
	Stop bool

	apply func() (string, error)
	// preview, when set, is what a dry run does instead of only reading Summary: it runs the action against a
	// host that records what it would do. Only the dry run calls it, only the runner calls apply.
	preview func() (summary string, warnings []string, err error)
}

type action struct {
	typ  string
	plan func(env *Env, p params) (*Plan, error)
}

type params map[string]any

func (p params) has(name string) bool { return p[name] != nil }

func (p params) str(name string) string {
	s, _ := p[name].(string)
	return s
}

func (p params) int(name string) int64 {
	id, _ := spec.ToInt64(p[name])
	return id
}

func (p params) float(name string) float64 {
	f, _ := spec.ToFloat64(p[name])
	return f
}

func (p params) bool(name string) bool {
	b, _ := p[name].(bool)
	return b
}

var registry = map[string]*action{}

func register(typ string, plan func(env *Env, p params) (*Plan, error)) {
	registry[typ] = &action{typ: typ, plan: plan}
}

// ActionTypes returns the types that have an implementation.
func ActionTypes() []string {
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}

func init() {
	register(spec.ActionSetField, planSetField)
	register(spec.ActionSetCustomField, planSetCustomField)
	register(spec.ActionSetDone, planSetDone)
	register(spec.ActionSetStatus, planSetStatus)
	register(spec.ActionAssign, func(e *Env, p params) (*Plan, error) { return planAssign(e, p, true) })
	register(spec.ActionUnassign, func(e *Env, p params) (*Plan, error) { return planAssign(e, p, false) })
	register(spec.ActionAddLabel, func(e *Env, p params) (*Plan, error) { return planLabel(e, p, true) })
	register(spec.ActionRemoveLabel, func(e *Env, p params) (*Plan, error) { return planLabel(e, p, false) })
	register(spec.ActionAddComment, planAddComment)
	register(spec.ActionCreateSubtask, func(e *Env, p params) (*Plan, error) { return planCreateTask(e, p, true) })
	register(spec.ActionCreateRelated, func(e *Env, p params) (*Plan, error) { return planCreateTask(e, p, false) })
	register(spec.ActionMove, planMove)
	register(spec.ActionStopIf, planStopIf)
	register(spec.ActionScriptRun, planScript)
	register(spec.ActionHTTPRequest, planHTTPRequest)
	register(spec.ActionApprovalRequest, planApprovalRequest)
}

// requireWritableTask checks that the task exists and the owner of the rule may change it.
func requireWritableTask(env *Env) (*models.Task, error) {
	if env.Sub == nil || env.Sub.Task == nil {
		return nil, actionErrorf("the task does not exist any more")
	}
	task := &models.Task{ID: env.Sub.Task.ID}
	can, err := task.CanWrite(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}
	return env.Sub.Task, nil
}

func resolveUserParam(env *Env, v any) (int64, error) {
	if s, ok := v.(string); ok {
		switch s {
		case spec.UserEventDoer:
			if id := env.Sub.DoerID(); id != 0 {
				return id, nil
			}
			return 0, actionErrorf("the event has no doer")
		case spec.UserTaskCreator:
			if env.Sub.CreatedBy != 0 {
				return env.Sub.CreatedBy, nil
			}
			return 0, actionErrorf("the task has no creator")
		}
		return 0, actionErrorf("%q is not a user", s)
	}
	id, ok := spec.ToInt64(v)
	if !ok || id <= 0 {
		return 0, actionErrorf("not a user id")
	}
	return id, nil
}

func renderedWarning(env *Env, plan *Plan, before int) {
	for _, name := range env.Unknown[before:] {
		plan.Warnings = append(plan.Warnings, "unknown placeholder {{"+name+"}} renders empty")
	}
}

var hexColorPattern = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)

func planSetField(env *Env, p params) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	field := p.str("field")
	value := p["value"]
	update := &models.Task{ID: task.ID, ProjectID: task.ProjectID}
	plan := &Plan{}
	unknownBefore := len(env.Unknown)
	changed := true

	switch field {
	case "title":
		s, _ := value.(string)
		update.Title = strings.TrimSpace(env.render(s))
		if update.Title == "" {
			return nil, actionErrorf("the title would be empty")
		}
		changed = update.Title != task.Title
		plan.Summary = fmt.Sprintf("set title to %q", update.Title)
	case "description":
		s, _ := value.(string)
		update.Description = env.render(s)
		changed = update.Description != task.Description
		plan.Summary = "set the description"
	case "priority":
		f, ok := spec.ToFloat64(value)
		if !ok || f < 0 || f > 5 || f != float64(int64(f)) {
			return nil, actionErrorf("the priority must be a whole number from 0 to 5")
		}
		update.Priority = int64(f)
		changed = update.Priority != task.Priority
		plan.Summary = fmt.Sprintf("set priority to %d", update.Priority)
	case "percent_done":
		f, ok := spec.ToFloat64(value)
		if !ok || f < 0 || f > 1 {
			return nil, actionErrorf("percent_done must be a number from 0 to 1")
		}
		update.PercentDone = f
		changed = update.PercentDone != task.PercentDone
		plan.Summary = fmt.Sprintf("set percent_done to %v", f)
	case "hex_color":
		s, _ := value.(string)
		s = strings.TrimSpace(s)
		if s == "" {
			update.HexColor = ""
		} else {
			m := hexColorPattern.FindStringSubmatch(s)
			if m == nil {
				return nil, actionErrorf("%q is not a hex color like #ff8800", s)
			}
			update.HexColor = strings.ToLower(m[1])
		}
		changed = update.HexColor != task.HexColor
		plan.Summary = fmt.Sprintf("set hex_color to %q", update.HexColor)
	case "due_date", "start_date", "end_date":
		s, _ := value.(string)
		d, err := spec.ParseDateSpec(s, true)
		if err != nil {
			return nil, actionErrorf("%s", err.Error())
		}
		at := d.Resolve(env.Now)
		var current time.Time
		switch field {
		case "due_date":
			update.DueDate, current = at, task.DueDate
		case "start_date":
			update.StartDate, current = at, task.StartDate
		default:
			update.EndDate, current = at, task.EndDate
		}
		changed = !at.Equal(current)
		if at.IsZero() {
			plan.Summary = "clear " + field
		} else {
			plan.Summary = fmt.Sprintf("set %s to %s", field, at.UTC().Format(time.RFC3339))
		}
	default:
		return nil, actionErrorf("the field %q cannot be set", field)
	}
	renderedWarning(env, plan, unknownBefore)

	if !changed {
		plan.Summary = "nothing to do: " + plan.Summary + " (already so)"
		return plan, nil
	}
	plan.apply = func() (string, error) {
		if err := update.UpdateFields(env.S, env.Auth, []string{field}); err != nil {
			return "", err
		}
		return plan.Summary, nil
	}
	return plan, nil
}

func planSetCustomField(env *Env, p params) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	fieldID := p.int("field_id")
	fieldType, known := env.Sub.CFTypes[fieldID]
	if !known {
		return nil, models.ErrCustomFieldDoesNotExist{CustomFieldID: fieldID}
	}

	plan := &Plan{}
	unknownBefore := len(env.Unknown)
	value := p["value"]
	if s, isString := value.(string); isString {
		switch fieldType {
		case models.CustomFieldTypeDate:
			d, err := spec.ParseDateSpec(s, true)
			if err != nil {
				return nil, actionErrorf("%s", err.Error())
			}
			at := d.Resolve(env.Now)
			if at.IsZero() {
				value = nil
			} else {
				value = at.UTC().Format(time.RFC3339)
			}
		case models.CustomFieldTypeText, models.CustomFieldTypeLongText, models.CustomFieldTypeURL, models.CustomFieldTypeEmail:
			value = env.render(s)
		}
	}
	renderedWarning(env, plan, unknownBefore)

	v := &models.CustomFieldValue{TaskID: task.ID, CustomFieldID: fieldID, Value: value}
	can, err := v.CanUpdate(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}
	if err := v.Validate(env.S); err != nil {
		return nil, err
	}
	if value == nil {
		plan.Summary = fmt.Sprintf("clear custom field %d", fieldID)
	} else {
		plan.Summary = fmt.Sprintf("set custom field %d to %v", fieldID, value)
	}
	plan.apply = func() (string, error) {
		if err := v.Update(env.S, env.Auth); err != nil {
			return "", err
		}
		return plan.Summary, nil
	}
	return plan, nil
}

func planSetDone(env *Env, p params) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	done := p.bool("done")
	plan := &Plan{Summary: fmt.Sprintf("mark the task as %s", map[bool]string{true: "done", false: "not done"}[done])}
	if task.Done == done {
		plan.Summary = "nothing to do: " + plan.Summary + " (already so)"
		return plan, nil
	}
	update := &models.Task{ID: task.ID, ProjectID: task.ProjectID, Done: done}
	plan.apply = func() (string, error) {
		if err := update.UpdateFields(env.S, env.Auth, []string{"done"}); err != nil {
			return "", err
		}
		return plan.Summary, nil
	}
	return plan, nil
}

func planSetStatus(env *Env, p params) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	unknownBefore := len(env.Unknown)
	change := &models.TaskStatusChange{TaskID: task.ID, ToStatusID: p.int("status_id"), Comment: env.render(p.str("comment"))}
	renderedWarning(env, plan, unknownBefore)
	can, err := change.CanCreate(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}
	if current, ok := env.Sub.Fields["status"].(float64); ok && int64(current) == change.ToStatusID {
		plan.Summary = fmt.Sprintf("nothing to do: the task is in status %d already", change.ToStatusID)
		return plan, nil
	}
	if err := change.Check(env.S, env.Auth); err != nil {
		return nil, err
	}
	plan.Summary = fmt.Sprintf("change the status to %d", change.ToStatusID)
	if change.RequiresApproval() {
		// The transition asks for an approval instead of moving the task; the owner of the rule is the requester.
		plan.Summary = fmt.Sprintf("would request approval for the status change to %d", change.ToStatusID)
	}
	plan.apply = func() (string, error) {
		if err := change.Create(env.S, env.Auth); err != nil {
			return "", err
		}
		if change.Approval != nil {
			return fmt.Sprintf("approval requested (#%d)", change.Approval.ID), nil
		}
		return plan.Summary, nil
	}
	return plan, nil
}

// planApprovalRequest asks people to approve the task, ad hoc: no status changes with the answer.
func planApprovalRequest(env *Env, p params) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	unknownBefore := len(env.Unknown)
	title := strings.TrimSpace(env.render(p.str("title")))
	description := env.render(p.str("description"))
	renderedWarning(env, plan, unknownBefore)
	if title == "" {
		return nil, actionErrorf("the title would be empty")
	}

	approvers := []int64{}
	add := func(id int64) {
		if id != env.Auth.ID && !containsInt(approvers, id) {
			approvers = append(approvers, id)
		}
	}
	list, _ := p["approvers"].([]any)
	for _, item := range list {
		if s, ok := item.(string); ok && s == spec.UserAssignees {
			assignees, _ := env.Sub.Fields["assignees"].([]any)
			for _, a := range assignees {
				if id, ok := a.(float64); ok {
					add(int64(id))
				}
			}
			continue
		}
		id, err := resolveUserParam(env, item)
		if err != nil {
			return nil, err
		}
		add(id)
	}
	if len(approvers) == 0 {
		return nil, actionErrorf("nobody is left to ask: the approvers are empty or only the owner of the rule")
	}

	request := &models.ApprovalRequest{
		TaskID:      task.ID,
		Title:       title,
		Description: description,
		Mode:        models.ApprovalMode(p.str("mode")),
		ApproverIDs: approvers,
	}
	if p.has("due_offset") {
		offset, err := spec.ParseOffset(p.str("due_offset"))
		if err != nil {
			return nil, actionErrorf("%s", err.Error())
		}
		due := env.Now.Add(offset)
		request.Due = &due
	}
	can, err := request.CanCreate(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}

	plan.Summary = fmt.Sprintf("request approval %q from %d user(s)", title, len(approvers))
	plan.apply = func() (string, error) {
		if err := request.Create(env.S, env.Auth); err != nil {
			return "", err
		}
		return fmt.Sprintf("approval requested (#%d)", request.ID), nil
	}
	return plan, nil
}

func containsInt(list []int64, id int64) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

func containsID(list any, id int64) bool {
	items, _ := list.([]any)
	for _, item := range items {
		if f, ok := item.(float64); ok && int64(f) == id {
			return true
		}
	}
	return false
}

func planAssign(env *Env, p params, add bool) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	userID, err := resolveUserParam(env, p["user"])
	if err != nil {
		return nil, err
	}
	assigned := containsID(env.Sub.Fields["assignees"], userID)
	verb := map[bool]string{true: "assign", false: "unassign"}[add]
	plan := &Plan{Summary: fmt.Sprintf("%s user %d", verb, userID)}
	if assigned == add {
		plan.Summary = "nothing to do: " + plan.Summary + " (already so)"
		return plan, nil
	}

	la := &models.TaskAssginee{TaskID: task.ID, UserID: userID}
	if add {
		can, err := la.CanCreate(env.S, env.Auth)
		if err != nil {
			return nil, err
		}
		if !can {
			return nil, models.ErrGenericForbidden{}
		}
		// The assignee needs to see the project; the model reports it on apply, check it here too.
		assignee, err := user.GetUserByID(env.S, userID)
		if err != nil {
			return nil, err
		}
		canRead, _, err := (&models.Project{ID: task.ProjectID}).CanRead(env.S, assignee)
		if err != nil {
			return nil, err
		}
		if !canRead {
			return nil, models.ErrUserDoesNotHaveAccessToProject{ProjectID: task.ProjectID, UserID: userID}
		}
	} else {
		can, err := la.CanDelete(env.S, env.Auth)
		if err != nil {
			return nil, err
		}
		if !can {
			return nil, models.ErrGenericForbidden{}
		}
	}
	plan.apply = func() (string, error) {
		var err error
		if add {
			err = la.Create(env.S, env.Auth)
		} else {
			err = la.Delete(env.S, env.Auth)
		}
		if err != nil {
			return "", err
		}
		return plan.Summary, nil
	}
	return plan, nil
}

func planLabel(env *Env, p params, add bool) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	labelID := p.int("label_id")
	has := containsID(env.Sub.Fields["labels"], labelID)
	verb := map[bool]string{true: "add", false: "remove"}[add]
	plan := &Plan{Summary: fmt.Sprintf("%s label %d", verb, labelID)}
	if has == add {
		plan.Summary = "nothing to do: " + plan.Summary + " (already so)"
		return plan, nil
	}
	lt := &models.LabelTask{TaskID: task.ID, LabelID: labelID}
	var can bool
	if add {
		can, err = lt.CanCreate(env.S, env.Auth)
	} else {
		can, err = lt.CanDelete(env.S, env.Auth)
	}
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}
	plan.apply = func() (string, error) {
		var err error
		if add {
			err = lt.Create(env.S, env.Auth)
		} else {
			err = lt.Delete(env.S, env.Auth)
		}
		if err != nil {
			return "", err
		}
		return plan.Summary, nil
	}
	return plan, nil
}

// textToHTML turns rendered plain text into the HTML comments are stored as.
func textToHTML(text string) string {
	escaped := html.EscapeString(text)
	paragraphs := strings.Split(strings.ReplaceAll(escaped, "\r\n", "\n"), "\n\n")
	for i, p := range paragraphs {
		paragraphs[i] = "<p>" + strings.ReplaceAll(strings.TrimSpace(p), "\n", "<br>") + "</p>"
	}
	return strings.Join(paragraphs, "")
}

func planAddComment(env *Env, p params) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	unknownBefore := len(env.Unknown)
	text := strings.TrimSpace(env.render(p.str("text")))
	renderedWarning(env, plan, unknownBefore)
	if text == "" {
		return nil, actionErrorf("the comment would be empty")
	}
	comment := &models.TaskComment{TaskID: task.ID, Comment: textToHTML(text)}
	can, err := comment.CanCreate(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}
	plan.Summary = fmt.Sprintf("add the comment %q", text)
	plan.apply = func() (string, error) {
		if err := comment.Create(env.S, env.Auth); err != nil {
			return "", err
		}
		return plan.Summary, nil
	}
	return plan, nil
}

func planCreateTask(env *Env, p params, subtask bool) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	unknownBefore := len(env.Unknown)
	title := strings.TrimSpace(env.render(p.str("title")))
	description := env.render(p.str("description"))
	renderedWarning(env, plan, unknownBefore)
	if title == "" {
		return nil, actionErrorf("the title would be empty")
	}

	created := &models.Task{Title: title, Description: description, ProjectID: task.ProjectID}
	if p.has("due_offset") {
		offset, err := spec.ParseOffset(p.str("due_offset"))
		if err != nil {
			return nil, actionErrorf("%s", err.Error())
		}
		created.DueDate = env.Now.Add(offset)
	}
	if p.has("assignee") {
		id, err := resolveUserParam(env, p["assignee"])
		if err != nil {
			return nil, err
		}
		assignee, err := user.GetUserByID(env.S, id)
		if err != nil {
			return nil, err
		}
		created.Assignees = []*user.User{assignee}
	}

	kind := models.RelationKindSubtask
	if !subtask {
		kind = models.RelationKind(p.str("relation_kind"))
		if kind == "" {
			kind = models.RelationKindRelated
		}
	}
	can, err := created.CanCreate(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}

	what := "related task"
	if subtask {
		what = "subtask"
	}
	plan.Summary = fmt.Sprintf("create the %s %q", what, title)
	plan.apply = func() (string, error) {
		if err := created.Create(env.S, env.Auth); err != nil {
			return "", err
		}
		relation := &models.TaskRelation{TaskID: task.ID, OtherTaskID: created.ID, RelationKind: kind}
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
		return fmt.Sprintf("%s (task %d)", plan.Summary, created.ID), nil
	}
	return plan, nil
}

func planMove(env *Env, p params) (*Plan, error) {
	task, err := requireWritableTask(env)
	if err != nil {
		return nil, err
	}
	target := p.int("project_id")
	plan := &Plan{Summary: fmt.Sprintf("move the task to project %d", target)}
	if target == task.ProjectID {
		plan.Summary = "nothing to do: " + plan.Summary + " (already there)"
		return plan, nil
	}
	update := &models.Task{ID: task.ID, ProjectID: target}
	// CanUpdate also checks write access to the target project.
	can, err := update.CanUpdate(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}
	plan.apply = func() (string, error) {
		if err := update.UpdateFields(env.S, env.Auth, []string{"project_id"}); err != nil {
			return "", err
		}
		return plan.Summary, nil
	}
	return plan, nil
}

func planStopIf(env *Env, p params) (*Plan, error) {
	c, ok := spec.ConditionFromParam(p["condition"])
	if !ok {
		return nil, actionErrorf("the condition is invalid")
	}
	holds, err := Evaluate(c, env.Sub)
	if err != nil {
		return nil, actionErrorf("%s", err.Error())
	}
	if holds {
		return &Plan{Summary: "the condition holds: stop the run", Stop: true}, nil
	}
	return &Plan{Summary: "the condition does not hold: continue"}, nil
}
