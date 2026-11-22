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

// Package spec is the pure-data part of the automation feature: the rule document, the catalog of
// triggers, condition fields, operators and actions, and the structural validation of a rule against that
// catalog. It knows nothing about the database; the models package checks the ids a rule refers to and the
// automation package executes the rules.
package spec

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The kinds of values options, params and condition fields can have.
const (
	KindString      = "string"
	KindText        = "text"
	KindNumber      = "number"
	KindBool        = "bool"
	KindDate        = "date"
	KindDuration    = "duration"
	KindUser        = "user"
	KindUsers       = "users"
	KindLabel       = "label"
	KindStatus      = "status"
	KindCustomField = "custom_field"
	KindProject     = "project"
	KindTemplate    = "template"
	KindCondition   = "condition"
	// KindCode is source code; Param.Language says which language (javascript).
	KindCode = "code"
	// KindSecret is the name of a project secret (never its value).
	KindSecret = "secret"
	// KindMap is an object whose values are strings (for example HTTP headers); the values are templates.
	KindMap = "map"
	// KindEnum is a string out of a fixed list (Enum).
	KindEnum = "enum"
	// KindAny is a value whose shape depends on another parameter (see KindBy) or on a custom field.
	KindAny = "any"
)

// The operators of a comparison.
const (
	OpEq         = "="
	OpNe         = "!="
	OpGt         = ">"
	OpGe         = ">="
	OpLt         = "<"
	OpLe         = "<="
	OpLike       = "like"
	OpIn         = "in"
	OpNotIn      = "not in"
	OpIsEmpty    = "is_empty"
	OpIsNotEmpty = "is_not_empty"
)

// The trigger types.
const (
	TriggerTaskCreated            = "task.created"
	TriggerTaskUpdated            = "task.updated"
	TriggerTaskCompleted          = "task.completed"
	TriggerTaskReopened           = "task.reopened"
	TriggerTaskDeleted            = "task.deleted"
	TriggerTaskStatusChanged      = "task.status_changed"
	TriggerTaskCustomFieldChanged = "task.custom_field_changed"
	TriggerTaskAssigneeAdded      = "task.assignee_added"
	TriggerTaskAssigneeRemoved    = "task.assignee_removed"
	TriggerTaskCommentAdded       = "task.comment_added"
	TriggerTaskAttachmentAdded    = "task.attachment_added"
	TriggerApprovalRequested      = "approval.requested"
	TriggerApprovalResolved       = "approval.resolved"
)

// The outcomes the approval.resolved trigger can wait for.
const (
	OutcomeAny       = "any"
	OutcomeApproved  = "approved"
	OutcomeRejected  = "rejected"
	OutcomeCancelled = "cancelled"
)

// ApprovalOutcomes are the values of the outcome option and field.
var ApprovalOutcomes = []string{OutcomeApproved, OutcomeRejected, OutcomeCancelled}

// The action types.
const (
	ActionSetField        = "task.set_field"
	ActionSetCustomField  = "task.set_custom_field"
	ActionSetDone         = "task.set_done"
	ActionSetStatus       = "task.set_status"
	ActionAssign          = "task.assign"
	ActionUnassign        = "task.unassign"
	ActionAddLabel        = "task.add_label"
	ActionRemoveLabel     = "task.remove_label"
	ActionAddComment      = "task.add_comment"
	ActionCreateSubtask   = "task.create_subtask"
	ActionCreateRelated   = "task.create_related"
	ActionMove            = "task.move"
	ActionStopIf          = "flow.stop_if"
	ActionScriptRun       = "script.run"
	ActionHTTPRequest     = "http.request"
	ActionApprovalRequest = "approval.request"
)

// The keywords the user kind accepts besides a user id.
const (
	UserEventDoer   = "event_doer"
	UserTaskCreator = "task_creator"
	// UserAssignees stands for every assignee of the task; only list parameters accept it.
	UserAssignees = "assignees"
)

// Param describes one option of a trigger or one parameter of an action.
type Param struct {
	Name     string   `json:"name" doc:"The key in options or params."`
	Kind     string   `json:"kind" doc:"The kind of value: string, text, number, bool, date, duration, user, users, label, status, custom_field, project, template, condition, enum, code (source code, see language), secret (the name of a project secret), map (an object with string values, e.g. headers; the values are templates) or any."`
	Required bool     `json:"required" doc:"Whether the key must be present."`
	Multiple bool     `json:"multiple,omitempty" doc:"Whether the value is a list of values of this kind."`
	Enum     []string `json:"enum,omitempty" doc:"The allowed values of an enum. For user and users these are the keywords accepted besides user ids."`
	TitleKey string   `json:"title_key" doc:"Translation key of the label."`
	KindBy   *KindBy  `json:"kind_by,omitempty" doc:"Set when the kind of the value depends on another parameter."`
	// Language is the language of a code parameter.
	Language string `json:"language,omitempty" doc:"For the kind code: the language of the source, so the editor can highlight it. Always javascript at the moment."`
	// Description is plain English help text for the editor, shown next to the field.
	Description string `json:"description,omitempty" doc:"Plain English help for the parameter, for the editor to show beside the field."`
	// Default is the value that is used when the parameter is omitted.
	Default any `json:"default,omitempty" doc:"The value used when the parameter is left out."`
}

// KindBy maps the value of a sibling parameter to the kind of this parameter's value.
type KindBy struct {
	Param string            `json:"param" doc:"The sibling parameter that decides the kind."`
	Kinds map[string]string `json:"kinds" doc:"The kind of the value for each value of that parameter."`
}

// TriggerDef is a trigger of the catalog.
type TriggerDef struct {
	Type     string  `json:"type" doc:"The stable identifier of the trigger."`
	TitleKey string  `json:"title_key" doc:"Translation key of the label."`
	Event    string  `json:"event" doc:"The internal event the trigger listens to. Several triggers can share one."`
	Options  []Param `json:"options" doc:"The options of the trigger."`
}

// FieldDef is a field conditions can compare.
type FieldDef struct {
	Name      string   `json:"name" doc:"The field name. Fields with a pattern (cf_<id>, event.old.<key>) are described by Pattern."`
	Pattern   string   `json:"pattern,omitempty" doc:"Set for families of fields, e.g. cf_<id>."`
	Kind      string   `json:"kind" doc:"The kind of value the field has."`
	Multiple  bool     `json:"multiple,omitempty" doc:"Whether the field holds a list of values."`
	Enum      []string `json:"enum,omitempty" doc:"The allowed values of an enum field."`
	Operators []string `json:"operators" doc:"The operators that can be used with this field."`
	TitleKey  string   `json:"title_key" doc:"Translation key of the label."`
	Title     string   `json:"title,omitempty" doc:"The title of the custom field, set for cf_<id> fields of a project."`
	// CustomFieldType is the type of the custom field behind a cf_<id> entry, which decides the shape of the value.
	CustomFieldType string `json:"custom_field_type,omitempty" doc:"The type of the custom field, set for cf_<id> fields of a project. select and multi_select compare option ids, user compares user ids."`
}

// OperatorDef is a comparison operator of the catalog.
type OperatorDef struct {
	Type     string `json:"type" doc:"The stable identifier of the operator."`
	TitleKey string `json:"title_key" doc:"Translation key of the label."`
	NoValue  bool   `json:"no_value,omitempty" doc:"Set when the comparison takes no value."`
	List     bool   `json:"list,omitempty" doc:"Set when the value is a list."`
}

// ActionDef is an action of the catalog.
type ActionDef struct {
	Type     string  `json:"type" doc:"The stable identifier of the action."`
	TitleKey string  `json:"title_key" doc:"Translation key of the label."`
	Params   []Param `json:"params" doc:"The parameters of the action."`
	// Description is plain English help text for the editor.
	Description string `json:"description,omitempty" doc:"Plain English help for the action, for the editor to show."`
}

// Catalog is the machine-readable description of everything a rule can use.
type Catalog struct {
	Triggers     []*TriggerDef  `json:"triggers" doc:"All triggers."`
	Fields       []*FieldDef    `json:"fields" doc:"All fields conditions can compare. Custom fields are added for a project when requested with project_id."`
	Operators    []*OperatorDef `json:"operators" doc:"All comparison operators."`
	Actions      []*ActionDef   `json:"actions" doc:"All actions."`
	Placeholders []string       `json:"placeholders" doc:"The placeholders text parameters of kind template may contain. cf.<id> stands for a custom field."`
	Script       *ScriptDoc     `json:"script" doc:"Reference for the JavaScript of script actions and script conditions: conventions, limits and every global and function. The editor's help panel is generated from it."`
}

var allOps = []string{OpEq, OpNe, OpGt, OpGe, OpLt, OpLe, OpLike, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty}

var operatorsByKind = map[string][]string{
	KindString:      {OpEq, OpNe, OpLike, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty},
	KindText:        {OpEq, OpNe, OpLike, OpIsEmpty, OpIsNotEmpty},
	KindNumber:      {OpEq, OpNe, OpGt, OpGe, OpLt, OpLe, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty},
	KindDate:        {OpEq, OpNe, OpGt, OpGe, OpLt, OpLe, OpIsEmpty, OpIsNotEmpty},
	KindBool:        {OpEq, OpNe},
	KindUser:        {OpEq, OpNe, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty},
	KindUsers:       {OpEq, OpNe, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty},
	KindLabel:       {OpEq, OpNe, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty},
	KindStatus:      {OpEq, OpNe, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty},
	KindProject:     {OpEq, OpNe, OpIn, OpNotIn},
	KindEnum:        {OpEq, OpNe, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty},
	KindAny:         allOps,
	KindCustomField: {OpEq, OpNe},
}

// OperatorsForKind returns the operators allowed for a field kind.
func OperatorsForKind(kind string) []string {
	return operatorsByKind[kind]
}

// Operators lists every operator.
func Operators() []*OperatorDef {
	return []*OperatorDef{
		{Type: OpEq, TitleKey: "automation.op.eq"},
		{Type: OpNe, TitleKey: "automation.op.ne"},
		{Type: OpGt, TitleKey: "automation.op.gt"},
		{Type: OpGe, TitleKey: "automation.op.ge"},
		{Type: OpLt, TitleKey: "automation.op.lt"},
		{Type: OpLe, TitleKey: "automation.op.le"},
		{Type: OpLike, TitleKey: "automation.op.like"},
		{Type: OpIn, TitleKey: "automation.op.in", List: true},
		{Type: OpNotIn, TitleKey: "automation.op.not_in", List: true},
		{Type: OpIsEmpty, TitleKey: "automation.op.is_empty", NoValue: true},
		{Type: OpIsNotEmpty, TitleKey: "automation.op.is_not_empty", NoValue: true},
	}
}

// TaskStatusCategories are the values of status_category.
var TaskStatusCategories = []string{"open", "in_progress", "done", "cancelled"}

// UpdatedFields are the task fields the fields option of task.updated and event.old.* / event.new.* know.
var UpdatedFields = []string{
	"title", "description", "done", "priority", "percent_done", "due_date", "start_date", "end_date",
	"hex_color", "project_id", "repeat_after",
}

var updatedFieldKinds = map[string]string{
	"title": KindString, "description": KindText, "done": KindBool, "priority": KindNumber,
	"percent_done": KindNumber, "due_date": KindDate, "start_date": KindDate, "end_date": KindDate,
	"hex_color": KindString, "project_id": KindProject, "repeat_after": KindNumber,
}

// SetFieldFields are the task fields the task.set_field action can write.
var SetFieldFields = []string{"title", "description", "priority", "due_date", "start_date", "end_date", "percent_done", "hex_color"}

// RelationKinds are the relation kinds task.create_related can use.
// HTTPMethods are the methods of the http.request action.
var HTTPMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

var RelationKinds = []string{"related", "blocking", "blocked", "precedes", "follows", "duplicates", "duplicateof"}

func title(prefix, name string) string { return "automation." + prefix + "." + name }

// Triggers returns the trigger catalog.
func Triggers() []*TriggerDef {
	return []*TriggerDef{
		{Type: TriggerTaskCreated, TitleKey: title("trigger", "task_created"), Event: "task.created", Options: []Param{}},
		{Type: TriggerTaskUpdated, TitleKey: title("trigger", "task_updated"), Event: "task.updated", Options: []Param{
			{Name: "fields", Kind: KindEnum, Multiple: true, Enum: UpdatedFields, TitleKey: title("option", "fields")},
		}},
		{Type: TriggerTaskCompleted, TitleKey: title("trigger", "task_completed"), Event: "task.updated", Options: []Param{}},
		{Type: TriggerTaskReopened, TitleKey: title("trigger", "task_reopened"), Event: "task.updated", Options: []Param{}},
		{Type: TriggerTaskDeleted, TitleKey: title("trigger", "task_deleted"), Event: "task.deleted", Options: []Param{}},
		{Type: TriggerTaskStatusChanged, TitleKey: title("trigger", "task_status_changed"), Event: "task.status.changed", Options: []Param{
			{Name: "from_status_id", Kind: KindStatus, TitleKey: title("option", "from_status_id")},
			{Name: "to_status_id", Kind: KindStatus, TitleKey: title("option", "to_status_id")},
			{Name: "to_category", Kind: KindEnum, Enum: TaskStatusCategories, TitleKey: title("option", "to_category")},
		}},
		{Type: TriggerTaskCustomFieldChanged, TitleKey: title("trigger", "task_custom_field_changed"), Event: "task.custom-field-value.updated", Options: []Param{
			{Name: "field_id", Kind: KindCustomField, TitleKey: title("option", "field_id")},
		}},
		{Type: TriggerTaskAssigneeAdded, TitleKey: title("trigger", "task_assignee_added"), Event: "task.assignee.created", Options: []Param{}},
		{Type: TriggerTaskAssigneeRemoved, TitleKey: title("trigger", "task_assignee_removed"), Event: "task.assignee.deleted", Options: []Param{}},
		{Type: TriggerTaskCommentAdded, TitleKey: title("trigger", "task_comment_added"), Event: "task.comment.created", Options: []Param{}},
		{Type: TriggerTaskAttachmentAdded, TitleKey: title("trigger", "task_attachment_added"), Event: "task.attachment.created", Options: []Param{}},
		{Type: TriggerApprovalRequested, TitleKey: title("trigger", "approval_requested"), Event: "approval.requested", Options: []Param{}},
		{Type: TriggerApprovalResolved, TitleKey: title("trigger", "approval_resolved"), Event: "approval.resolved", Options: []Param{
			{Name: "outcome", Kind: KindEnum, Enum: append([]string{OutcomeAny}, ApprovalOutcomes...), TitleKey: title("option", "outcome"), Default: OutcomeAny},
		}},
	}
}

// TriggerByType looks a trigger up.
func TriggerByType(t string) *TriggerDef {
	for _, d := range Triggers() {
		if d.Type == t {
			return d
		}
	}
	return nil
}

// EventNames returns every internal event name some trigger listens to.
func EventNames() []string {
	seen := map[string]bool{}
	names := []string{}
	for _, d := range Triggers() {
		if !seen[d.Event] {
			seen[d.Event] = true
			names = append(names, d.Event)
		}
	}
	sort.Strings(names)
	return names
}

func field(name, kind string, extra ...func(*FieldDef)) *FieldDef {
	f := &FieldDef{Name: name, Kind: kind, Operators: OperatorsForKind(kind), TitleKey: title("field", strings.ReplaceAll(name, ".", "_"))}
	for _, e := range extra {
		e(f)
	}
	return f
}

func multiple(f *FieldDef) { f.Multiple = true }

func enumOf(values ...string) func(*FieldDef) {
	return func(f *FieldDef) { f.Enum = values }
}

func pattern(p string) func(*FieldDef) {
	return func(f *FieldDef) { f.Pattern = p; f.Name = p }
}

// Fields returns the static condition fields: task fields and the event payload.
func Fields() []*FieldDef {
	fields := []*FieldDef{
		field("title", KindString),
		field("description", KindText),
		field("done", KindBool),
		field("priority", KindNumber),
		field("percent_done", KindNumber),
		field("due_date", KindDate),
		field("start_date", KindDate),
		field("end_date", KindDate),
		field("done_at", KindDate),
		field("created", KindDate),
		field("updated", KindDate),
		field("hex_color", KindString),
		field("identifier", KindString),
		field("project_id", KindProject),
		field("created_by", KindUser),
		field("assignees", KindUsers, multiple),
		field("labels", KindLabel, multiple),
		field("status", KindStatus),
		field("status_category", KindEnum, enumOf(TaskStatusCategories...)),
		field("cf_<id>", KindCustomField, pattern("cf_<id>"), func(f *FieldDef) { f.Operators = allOps }),
		field("event.doer.id", KindUser),
		field("event.doer.username", KindString),
		field("event.doer.name", KindString),
	}
	for _, side := range []string{"old", "new"} {
		for _, name := range UpdatedFields {
			fields = append(fields, field("event."+side+"."+name, updatedFieldKinds[name]))
		}
		fields = append(fields,
			field("event."+side+".status", KindStatus),
			field("event."+side+".status_category", KindEnum, enumOf(TaskStatusCategories...)),
			field("event."+side+".value", KindAny),
			field("event."+side+".assignee", KindUser),
		)
	}
	fields = append(fields,
		field("event.new.comment", KindText),
		field("event.new.attachment_name", KindString),
		field("event.new.approval_id", KindNumber),
		field("event.new.approval_title", KindString),
		field("event.new.approval_mode", KindEnum, enumOf("any", "all")),
		field("event.new.ad_hoc", KindBool),
		field("event.new.requested_by", KindUser),
		field("event.new.approvers", KindUsers, multiple),
		field("event.new.outcome", KindEnum, enumOf(ApprovalOutcomes...)),
		field("event.new.cancel_reason", KindString),
	)
	return fields
}

var (
	customFieldPattern = regexp.MustCompile(`^cf_([1-9][0-9]{0,17})$`)
	staticFields       map[string]*FieldDef
)

func init() {
	staticFields = map[string]*FieldDef{}
	for _, f := range Fields() {
		if f.Pattern == "" {
			staticFields[f.Name] = f
		}
	}
}

// CustomFieldID parses cf_<id>.
func CustomFieldID(name string) (int64, bool) {
	m := customFieldPattern.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	return id, err == nil
}

// FieldByName returns the definition of a static condition field. Custom fields (cf_<id>) have no static kind
// and are described by the pattern entry; use CustomFieldID to detect them.
func FieldByName(name string) *FieldDef {
	return staticFields[name]
}

// Placeholders are the template placeholders text parameters understand.
func Placeholders() []string {
	return []string{
		"task.id", "task.title", "task.description", "task.identifier", "task.cf.<id>",
		"event.doer.name", "event.doer.username", "project.title",
		"event.approval.id", "event.approval.title", "event.approval.outcome",
	}
}

func userParam(name string, required bool) Param {
	return Param{Name: name, Kind: KindUser, Required: required, Enum: []string{UserEventDoer, UserTaskCreator}, TitleKey: title("param", name)}
}

// Actions returns the action catalog.
func Actions() []*ActionDef {
	return []*ActionDef{
		{Type: ActionSetField, TitleKey: title("action", "set_field"), Params: []Param{
			{Name: "field", Kind: KindEnum, Required: true, Enum: SetFieldFields, TitleKey: title("param", "field")},
			{Name: "value", Kind: KindAny, Required: true, TitleKey: title("param", "value"), KindBy: &KindBy{Param: "field", Kinds: map[string]string{
				"title": KindTemplate, "description": KindTemplate, "priority": KindNumber, "due_date": KindDate,
				"start_date": KindDate, "end_date": KindDate, "percent_done": KindNumber, "hex_color": KindString,
			}}},
		}},
		{Type: ActionSetCustomField, TitleKey: title("action", "set_custom_field"), Params: []Param{
			{Name: "field_id", Kind: KindCustomField, Required: true, TitleKey: title("param", "field_id")},
			{Name: "value", Kind: KindAny, TitleKey: title("param", "value")},
		}},
		{Type: ActionSetDone, TitleKey: title("action", "set_done"), Params: []Param{
			{Name: "done", Kind: KindBool, Required: true, TitleKey: title("param", "done")},
		}},
		{Type: ActionSetStatus, TitleKey: title("action", "set_status"), Params: []Param{
			{Name: "status_id", Kind: KindStatus, Required: true, TitleKey: title("param", "status_id")},
			{Name: "comment", Kind: KindTemplate, TitleKey: title("param", "comment")},
		}},
		{Type: ActionAssign, TitleKey: title("action", "assign"), Params: []Param{userParam("user", true)}},
		{Type: ActionUnassign, TitleKey: title("action", "unassign"), Params: []Param{userParam("user", true)}},
		{Type: ActionAddLabel, TitleKey: title("action", "add_label"), Params: []Param{
			{Name: "label_id", Kind: KindLabel, Required: true, TitleKey: title("param", "label_id")},
		}},
		{Type: ActionRemoveLabel, TitleKey: title("action", "remove_label"), Params: []Param{
			{Name: "label_id", Kind: KindLabel, Required: true, TitleKey: title("param", "label_id")},
		}},
		{Type: ActionAddComment, TitleKey: title("action", "add_comment"), Params: []Param{
			{Name: "text", Kind: KindTemplate, Required: true, TitleKey: title("param", "text")},
		}},
		{Type: ActionCreateSubtask, TitleKey: title("action", "create_subtask"), Params: []Param{
			{Name: "title", Kind: KindTemplate, Required: true, TitleKey: title("param", "title")},
			{Name: "description", Kind: KindTemplate, TitleKey: title("param", "description")},
			userParam("assignee", false),
			{Name: "due_offset", Kind: KindDuration, TitleKey: title("param", "due_offset")},
		}},
		{Type: ActionCreateRelated, TitleKey: title("action", "create_related"), Params: []Param{
			{Name: "title", Kind: KindTemplate, Required: true, TitleKey: title("param", "title")},
			{Name: "description", Kind: KindTemplate, TitleKey: title("param", "description")},
			userParam("assignee", false),
			{Name: "due_offset", Kind: KindDuration, TitleKey: title("param", "due_offset")},
			{Name: "relation_kind", Kind: KindEnum, Enum: RelationKinds, TitleKey: title("param", "relation_kind")},
		}},
		{Type: ActionMove, TitleKey: title("action", "move"), Params: []Param{
			{Name: "project_id", Kind: KindProject, Required: true, TitleKey: title("param", "project_id")},
		}},
		{Type: ActionStopIf, TitleKey: title("action", "stop_if"), Params: []Param{
			{Name: "condition", Kind: KindCondition, Required: true, TitleKey: title("param", "condition")},
		}},
		{Type: ActionScriptRun, TitleKey: title("action", "script_run"),
			Description: "Runs JavaScript with the permissions of the rule owner. The source is the body of a function: use return to give a result, which is stored (as JSON) in the output of the step. A thrown error fails the step. See the script reference for the globals and the limits.",
			Params: []Param{
				{Name: "source", Kind: KindCode, Language: "javascript", Required: true, TitleKey: title("param", "source"),
					Description: "The body of a function, for example: `const parent = vikunja.tasks.get(task.id); return parent.title;`. Statements run in strict mode. Not a template: {{placeholders}} are not replaced."},
			}},
		{Type: ActionApprovalRequest, TitleKey: title("action", "approval_request"),
			Description: "Asks people to approve the task: an ad-hoc approval request that changes no status. The request is made by the owner of the rule, who cannot be one of the approvers. The approvers need to be able to read the task.",
			Params: []Param{
				{Name: "approvers", Kind: KindUsers, Required: true, Multiple: true, Enum: []string{UserEventDoer, UserTaskCreator, UserAssignees}, TitleKey: title("param", "approvers")},
				{Name: "mode", Kind: KindEnum, Enum: []string{"any", "all"}, Default: "any", TitleKey: title("param", "mode"),
					Description: "any: the first decision resolves the request. all: every approver has to approve, one rejection rejects."},
				{Name: "title", Kind: KindTemplate, Required: true, TitleKey: title("param", "title")},
				{Name: "description", Kind: KindTemplate, TitleKey: title("param", "description")},
				{Name: "due_offset", Kind: KindDuration, TitleKey: title("param", "due_offset")},
			}},
		{Type: ActionHTTPRequest, TitleKey: title("action", "http_request"),
			Description: "Sends an HTTP request to an external service, for example a chat webhook. Private, loopback and link-local addresses are blocked. A 5xx answer or a network error is retried like any temporary failure; a 4xx answer fails the run. The status and the first 2 KiB of the answer are stored in the step output.",
			Params: []Param{
				{Name: "url", Kind: KindTemplate, Required: true, TitleKey: title("param", "url"),
					Description: "The address, http or https. Placeholders are replaced; {{secrets.NAME}} is not allowed here."},
				{Name: "method", Kind: KindEnum, Enum: HTTPMethods, Default: "POST", TitleKey: title("param", "method"),
					Description: "The HTTP method. Defaults to POST."},
				{Name: "headers", Kind: KindMap, TitleKey: title("param", "headers"),
					Description: "Request headers: header name to value. Values are templates and may contain {{secrets.NAME}}. Host, Content-Length, Transfer-Encoding, Connection and Upgrade cannot be set."},
				{Name: "body", Kind: KindTemplate, TitleKey: title("param", "body"),
					Description: "The request body. Placeholders and {{secrets.NAME}} are replaced. The Content-Type is application/json when the body is valid JSON and no Content-Type header is given, otherwise text/plain."},
				{Name: "secret", Kind: KindSecret, TitleKey: title("param", "secret"),
					Description: "The name of a project secret. When set, the request carries the header X-Vikunja-Signature: sha256=<hex HMAC-SHA256 of the body, keyed with the secret>."},
			}},
	}
}

// ActionByType looks an action up.
func ActionByType(t string) *ActionDef {
	for _, d := range Actions() {
		if d.Type == t {
			return d
		}
	}
	return nil
}

// GetCatalog returns the whole catalog.
func GetCatalog() *Catalog {
	return &Catalog{
		Triggers:     Triggers(),
		Fields:       Fields(),
		Operators:    Operators(),
		Actions:      Actions(),
		Placeholders: Placeholders(),
		Script:       ScriptReference(),
	}
}
