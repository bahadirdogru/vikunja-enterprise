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

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// FormAccess says who may submit a form.
type FormAccess string

// The access modes of a form.
const (
	// FormAccessPublic is anyone with the link, no account.
	FormAccessPublic FormAccess = "public"
	// FormAccessUsers is every signed-in user. Link shares are not users.
	FormAccessUsers FormAccess = "users"
	// FormAccessMembers is signed-in users who can read the project.
	FormAccessMembers FormAccess = "members"
)

func (a FormAccess) isValid() bool {
	return a == FormAccessPublic || a == FormAccessUsers || a == FormAccessMembers
}

// The reasons the system stores on a form it disabled.
const (
	FormDisabledOwnerNotWriter     = "owner_not_writer"
	FormDisabledOwnerUnavailable   = "owner_unavailable"
	FormDisabledCustomFieldDeleted = "custom_field_deleted"
	FormDisabledStatusDeleted      = "status_deleted"
	FormDisabledLabelDeleted       = "label_deleted"
	FormDisabledDuplicated         = "duplicated"
	FormDisabledUnmappedReference  = "duplicated_unmapped_reference"
)

// The built-in targets of a form field. Custom fields are cf_<id>.
const (
	FormTargetTitle       = "title"
	FormTargetDescription = "description"
	FormTargetDueDate     = "due_date"
	FormTargetPriority    = "priority"
	FormTargetLabels      = "labels"
	FormTargetAttachments = "attachments"
)

var formBuiltinTargets = []string{
	FormTargetTitle, FormTargetDescription, FormTargetDueDate, FormTargetPriority, FormTargetLabels, FormTargetAttachments,
}

const (
	formTokenBytes      = 32
	formMaxFields       = 50
	formMaxTextLen      = 10000
	formMaxLongTextLen  = 50000
	formMaxTitleLen     = 250
	formMaxLabelLen     = 250
	formMaxHelpLen      = 1000
	formMaxMessageLen   = 2000
	formMaxURLLen       = 2000
	formMaxListLen      = 100
	formMaxPriority     = 5
	formMaxTitleDefault = 1000
)

var customFieldTarget = regexp.MustCompile(`^cf_([1-9][0-9]{0,17})$`)

// FormField is one entry of a form: what to ask for and where the answer goes.
type FormField struct {
	Target   string  `json:"target" doc:"Where the answer goes: title, description, due_date, priority, labels, attachments or cf_<id> for a custom field of the project. A target can appear once."`
	Label    string  `json:"label" maxLength:"250" doc:"The question as the submitter sees it. Required for fields that are shown."`
	Help     string  `json:"help" maxLength:"1000" doc:"Optional help text shown with the field."`
	Required bool    `json:"required" doc:"Whether the submitter has to answer. A required hidden field needs a default."`
	Default  any     `json:"default" doc:"The prefilled value of a shown field and the value of a hidden one. Validated like a real answer. For title and description it may use the placeholders {{submitter.name}}, {{submitter.email}}, {{form.title}} and {{date}}; for labels it is a list of label ids."`
	Hidden   bool    `json:"hidden" doc:"Hidden fields are not shown and always take their default. This is how a form presets a label or custom field."`
	Options  []int64 `json:"options,omitempty" doc:"Only for a shown labels field: the ids of the labels the submitter may pick. Not allowed on public forms."`
}

// FormSettings are the settings of a form.
type FormSettings struct {
	InitialStatusID    int64      `json:"initial_status_id" doc:"The workflow status the created tasks start in. 0 keeps the initial status of the workflow."`
	DefaultAssigneeIDs []int64    `json:"default_assignee_ids" doc:"Users assigned to every created task. They have to be able to read the project."`
	SuccessMessage     string     `json:"success_message" maxLength:"2000" doc:"Shown after a submission. Plain text."`
	RedirectURL        string     `json:"redirect_url" maxLength:"2000" doc:"An http or https address the submitter is sent to after a submission."`
	SubmitAnother      bool       `json:"submit_another" doc:"Whether the confirmation offers to submit the form again."`
	CloseDate          *time.Time `json:"close_date" doc:"After this time the form takes no submissions. Null for never."`
	MaxSubmissions     int64      `json:"max_submissions" minimum:"0" doc:"The form takes no submissions after this many. 0 for no limit."`
	NotifyUserIDs      []int64    `json:"notify_user_ids" doc:"Users who get a notification for every submission. They have to be able to read the project."`
	CollectName        bool       `json:"collect_name" doc:"Ask the submitter for a name. It is stored on the submission and added to the task description."`
	NameRequired       bool       `json:"name_required" doc:"Whether the name has to be given. Needs collect_name."`
	CollectEmail       bool       `json:"collect_email" doc:"Ask the submitter for an e-mail address. It is stored on the submission and added to the task description."`
	EmailRequired      bool       `json:"email_required" doc:"Whether the e-mail address has to be given. Needs collect_email."`
}

// Form is a request form of a project. A submission becomes a task in the project, created as the owner.
type Form struct {
	ID          int64      `xorm:"bigint autoincr not null unique pk" json:"id" param:"form" readOnly:"true" doc:"The unique, numeric id of this form."`
	ProjectID   int64      `xorm:"bigint not null INDEX" json:"project_id" param:"project" readOnly:"true" doc:"The project the tasks of this form are created in. Taken from the URL."`
	Title       string     `xorm:"varchar(250) not null" json:"title" minLength:"1" maxLength:"250" doc:"The title of the form."`
	Description string     `xorm:"longtext null" json:"description" doc:"Rich text (HTML) shown above the form. It is sanitized when saved."`
	Token       string     `xorm:"varchar(64) not null unique" json:"token" readOnly:"true" doc:"The secret in the link of the form. Regenerate it with POST .../token; the old link stops working."`
	Access      FormAccess `xorm:"varchar(10) not null" json:"access" enum:"public,users,members" doc:"Who may submit: public (anyone with the link), users (every signed-in user) or members (users who can read the project)."`
	Enabled     bool       `xorm:"bool default false" json:"enabled" doc:"Whether the form takes submissions. A form the system disabled has a disabled_reason; saving it with enabled=true clears the reason."`
	OwnerID     int64      `xorm:"bigint not null" json:"owner_id" readOnly:"true" doc:"The user the tasks are created as. Always the user who last saved the form. The form is disabled when this user can no longer write to the project."`

	Fields   []*FormField  `xorm:"json not null" json:"fields" doc:"What the form asks, in order."`
	Settings *FormSettings `xorm:"json null" json:"settings" doc:"Settings of the form."`

	DisabledReason  string `xorm:"varchar(100) null" json:"disabled_reason" readOnly:"true" doc:"Set when the system disabled the form: owner_not_writer, owner_unavailable, custom_field_deleted, status_deleted, label_deleted, duplicated or duplicated_unmapped_reference."`
	MaxSubmissions  int64  `xorm:"bigint not null default 0" json:"-"`
	SubmissionCount int64  `xorm:"bigint not null default 0" json:"submission_count" readOnly:"true" doc:"How many submissions the form took."`

	Created time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this form was created. You cannot change this value."`
	Updated time.Time `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this form was last updated. You cannot change this value."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name of forms.
func (*Form) TableName() string {
	return "forms"
}

// FormSubmission records one submission of a form.
type FormSubmission struct {
	ID             int64     `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this submission."`
	FormID         int64     `xorm:"bigint not null INDEX" json:"form_id" param:"form" readOnly:"true" doc:"The form. Taken from the URL."`
	TaskID         int64     `xorm:"bigint not null INDEX" json:"task_id" readOnly:"true" doc:"The task the submission created."`
	SubmitterID    *int64    `xorm:"bigint null" json:"submitter_id" readOnly:"true" doc:"The user who submitted, null for anonymous submissions."`
	SubmitterName  string    `xorm:"varchar(250) null" json:"submitter_name" readOnly:"true" doc:"The name the submitter gave, or the display name of the signed-in submitter."`
	SubmitterEmail string    `xorm:"varchar(250) null" json:"submitter_email" readOnly:"true" doc:"The e-mail address the submitter gave, or the one of the signed-in submitter."`
	IPHash         string    `xorm:"varchar(64) null INDEX" json:"-"`
	Created        time.Time `xorm:"DATETIME not null" json:"created" readOnly:"true" doc:"When the form was submitted."`

	ProjectID int64 `xorm:"-" json:"-" param:"project"`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name of form submissions.
func (*FormSubmission) TableName() string {
	return "form_submissions"
}

// formClock is the clock of the form rate limits and close dates; tests replace it.
var formClock = time.Now

func newFormToken() (string, error) {
	b := make([]byte, formTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func getFormInProject(s *xorm.Session, formID, projectID int64) (*Form, error) {
	f := &Form{}
	has, err := s.Where("id = ? AND project_id = ?", formID, projectID).Get(f)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrFormDoesNotExist{ID: formID}
	}
	return f, nil
}

// GetFormByToken loads a form by its secret token.
func GetFormByToken(s *xorm.Session, token string) (*Form, error) {
	if token == "" || len(token) > 64 {
		return nil, ErrFormDoesNotExist{}
	}
	f := &Form{}
	has, err := s.Where("token = ?", token).Get(f)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrFormDoesNotExist{}
	}
	return f, nil
}

func normalizeForm(f *Form) {
	if f.Fields == nil {
		f.Fields = []*FormField{}
	}
	for i, field := range f.Fields {
		if field == nil {
			f.Fields[i] = &FormField{}
		}
	}
	if f.Settings == nil {
		f.Settings = &FormSettings{}
	}
	if f.Settings.DefaultAssigneeIDs == nil {
		f.Settings.DefaultAssigneeIDs = []int64{}
	}
	if f.Settings.NotifyUserIDs == nil {
		f.Settings.NotifyUserIDs = []int64{}
	}
}

// customFieldIDOfTarget parses cf_<id>.
func customFieldIDOfTarget(target string) (int64, bool) {
	m := customFieldTarget.FindStringSubmatch(target)
	if m == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	return id, err == nil
}

func formLabelUsable(s *xorm.Session, a web.Auth, id int64) (bool, error) {
	label, err := getLabelByIDSimple(s, id)
	if err != nil {
		if IsErrLabelDoesNotExist(err) {
			return false, nil
		}
		return false, err
	}
	has, _, err := label.hasAccessToLabel(s, a)
	return has, err
}

type formIssues []FormIssue

func (i *formIssues) add(path, format string, args ...any) {
	*i = append(*i, FormIssue{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (i *formIssues) err() error {
	if len(*i) == 0 {
		return nil
	}
	return ErrFormInvalid{FormInvalidDetails{Issues: *i}}
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
}

// Validate checks the definition against the project and the saving user. All problems are returned together,
// each with the path of the offending part.
func (f *Form) Validate(s *xorm.Session, a web.Auth) error {
	normalizeForm(f)
	f.Title = strings.TrimSpace(f.Title)
	f.Description = sanitizeFormHTML(f.Description)

	var issues formIssues
	if f.Title == "" {
		issues.add("title", "is required")
	} else if utf8.RuneCountInString(f.Title) > 250 {
		issues.add("title", "may be at most 250 characters long")
	}
	if !f.Access.isValid() {
		issues.add("access", "must be public, users or members")
	}
	if f.Access == FormAccessPublic && !config.FormsPublicEnabled.GetBool() {
		issues.add("access", "public forms are disabled on this instance")
	}
	if len(f.Fields) > formMaxFields {
		issues.add("fields", "a form may have at most %d fields", formMaxFields)
		return issues.err()
	}

	customFields := map[int64]*CustomField{}
	cfs := []*CustomField{}
	if err := s.Where("project_id = ?", f.ProjectID).Find(&cfs); err != nil {
		return err
	}
	for _, cf := range cfs {
		customFields[cf.ID] = cf
	}

	seen := map[string]bool{}
	titleAsked := false
	for i, field := range f.Fields {
		path := fmt.Sprintf("fields[%d]", i)
		if err := f.validateField(s, a, path, field, customFields, seen, &issues); err != nil {
			return err
		}
		if field.Target == FormTargetTitle {
			titleAsked = true
		}
	}
	if !titleAsked && len(issues) == 0 || !titleAsked && !hasIssueAt(issues, "fields") {
		issues.add("fields", "the title has to be asked or have a default")
	}

	if err := f.validateSettings(s, &issues); err != nil {
		return err
	}
	f.MaxSubmissions = f.Settings.MaxSubmissions
	return issues.err()
}

func hasIssueAt(issues formIssues, prefix string) bool {
	for _, i := range issues {
		if strings.HasPrefix(i.Path, prefix) {
			return true
		}
	}
	return false
}

//nolint:gocyclo
func (f *Form) validateField(s *xorm.Session, a web.Auth, path string, field *FormField, customFields map[int64]*CustomField, seen map[string]bool, issues *formIssues) error {
	isBuiltin := slices.Contains(formBuiltinTargets, field.Target)
	var cf *CustomField
	if !isBuiltin {
		id, ok := customFieldIDOfTarget(field.Target)
		if !ok {
			issues.add(path+".target", "must be one of %s or cf_<id>", strings.Join(formBuiltinTargets, ", "))
			return nil
		}
		cf = customFields[id]
		if cf == nil {
			issues.add(path+".target", "the custom field does not exist in this project")
			return nil
		}
	}
	if seen[field.Target] {
		issues.add(path+".target", "is already used by another field")
		return nil
	}
	seen[field.Target] = true

	field.Label = strings.TrimSpace(field.Label)
	field.Help = strings.TrimSpace(field.Help)
	if utf8.RuneCountInString(field.Label) > formMaxLabelLen {
		issues.add(path+".label", "may be at most %d characters long", formMaxLabelLen)
	}
	if utf8.RuneCountInString(field.Help) > formMaxHelpLen {
		issues.add(path+".help", "may be at most %d characters long", formMaxHelpLen)
	}
	if !field.Hidden && field.Label == "" {
		issues.add(path+".label", "is required for a field that is shown")
	}
	if field.Hidden && field.Required && isEmptyFormDefault(field.Default) {
		issues.add(path+".default", "a required hidden field needs a default")
	}
	if field.Target == FormTargetTitle && (field.Hidden || !field.Required) && isEmptyFormDefault(field.Default) {
		issues.add(path+".default", "the title has to be asked or have a default")
	}
	if len(field.Options) > 0 && field.Target != FormTargetLabels {
		issues.add(path+".options", "only a labels field has options")
	}

	if f.Access == FormAccessPublic && !field.Hidden {
		switch {
		case field.Target == FormTargetLabels:
			issues.add(path+".target", "labels cannot be asked on a public form; hide the field and give it a default")
		case cf != nil && cf.Type == CustomFieldTypeUser:
			issues.add(path+".target", "user fields cannot be asked on a public form; hide the field and give it a default")
		}
	}

	switch field.Target {
	case FormTargetAttachments:
		switch {
		case field.Hidden:
			issues.add(path+".hidden", "attachments cannot be hidden")
		case !config.ServiceEnableTaskAttachments.GetBool():
			issues.add(path+".target", "task attachments are disabled on this instance")
		case f.Access == FormAccessPublic && !config.FormsPublicAttachments.GetBool():
			issues.add(path+".target", "attachments on public forms are disabled on this instance")
		}
		if !isEmptyFormDefault(field.Default) {
			issues.add(path+".default", "attachments have no default")
		}
	case FormTargetLabels:
		if err := f.validateLabelsField(s, a, path, field, issues); err != nil {
			return err
		}
	default:
		if isEmptyFormDefault(field.Default) {
			return nil
		}
		if msg := f.validateDefault(s, field, cf); msg != "" {
			issues.add(path+".default", "%s", msg)
		}
	}
	return nil
}

func (f *Form) validateLabelsField(s *xorm.Session, a web.Auth, path string, field *FormField, issues *formIssues) error {
	if !field.Hidden {
		if len(field.Options) == 0 {
			issues.add(path+".options", "a shown labels field needs the labels to choose from")
		}
		for i, id := range field.Options {
			ok, err := formLabelUsable(s, a, id)
			if err != nil {
				return err
			}
			if !ok {
				issues.add(fmt.Sprintf("%s.options[%d]", path, i), "the label does not exist or you cannot use it")
			}
		}
	}
	if isEmptyFormDefault(field.Default) {
		return nil
	}
	ids, ok := toIntSlice(field.Default)
	if !ok || len(ids) > formMaxListLen {
		issues.add(path+".default", "must be a list of label ids")
		return nil
	}
	for i, id := range uniqueInts(ids) {
		ok, err := formLabelUsable(s, a, id)
		if err != nil {
			return err
		}
		if !ok {
			issues.add(fmt.Sprintf("%s.default[%d]", path, i), "the label does not exist or you cannot use it")
			continue
		}
		if !field.Hidden && !slices.Contains(field.Options, id) {
			issues.add(fmt.Sprintf("%s.default[%d]", path, i), "the default has to be one of the options")
		}
	}
	return nil
}

func isEmptyFormDefault(v any) bool {
	if v == nil {
		return true
	}
	if str, ok := v.(string); ok {
		return strings.TrimSpace(str) == ""
	}
	return isEmptyCustomFieldValue(v)
}

// validateDefault checks the default of a built-in or custom field target with the validators real answers use.
// It returns a message for a bad default.
func (f *Form) validateDefault(s *xorm.Session, field *FormField, cf *CustomField) string {
	if cf != nil {
		if str, ok := field.Default.(string); ok {
			if msg := checkFormTemplate(str); msg != "" {
				return msg
			}
		}
		if err := formValidateCustomFieldValue(s, cf, f.ProjectID, field.Default); err != nil {
			return formCustomFieldMessage(err)
		}
		return ""
	}

	switch field.Target {
	case FormTargetTitle, FormTargetDescription:
		str, ok := field.Default.(string)
		if !ok {
			return "must be a string"
		}
		limit := formMaxTitleDefault
		if field.Target == FormTargetDescription {
			limit = formMaxLongTextLen
		}
		if utf8.RuneCountInString(str) > limit {
			return fmt.Sprintf("may be at most %d characters long", limit)
		}
		return checkFormTemplate(str)
	case FormTargetDueDate:
		if _, ok := parseFormDate(field.Default); !ok {
			return "must be an RFC 3339 date or a date like 2026-12-31"
		}
	case FormTargetPriority:
		if _, ok := parseFormPriority(field.Default); !ok {
			return fmt.Sprintf("must be a whole number between 0 and %d", formMaxPriority)
		}
	}
	return ""
}

// formValidateCustomFieldValue validates a value for a custom field of the project with the same validators real
// values use. The task only has to carry the project: user fields check access to it.
func formValidateCustomFieldValue(s *xorm.Session, cf *CustomField, projectID int64, value any) error {
	if cf.Config == nil && cf.Type == "" {
		return nil
	}
	_, err := buildCustomFieldValueRows(s, cf, &Task{ProjectID: projectID}, value)
	return err
}

func formCustomFieldMessage(err error) string {
	if e, ok := errors.AsType[ErrCustomFieldInvalidValue](err); ok {
		return e.Reason
	}
	return "is not valid for this field"
}

func parseFormDate(v any) (time.Time, bool) {
	str, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	str = strings.TrimSpace(str)
	if t, err := time.Parse(time.RFC3339, str); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02", str); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}

func parseFormPriority(v any) (int64, bool) {
	n, ok := toInt(v)
	if !ok || n < 0 || n > formMaxPriority {
		return 0, false
	}
	return n, true
}

func (f *Form) validateSettings(s *xorm.Session, issues *formIssues) error {
	st := f.Settings
	if st.InitialStatusID != 0 {
		d, err := loadWorkflowDef(s, f.ProjectID)
		if err != nil {
			return err
		}
		var status *WorkflowStatus
		if d != nil {
			status = d.statuses[st.InitialStatusID]
		}
		switch {
		case status == nil:
			issues.add("settings.initial_status_id", "the status does not belong to the workflow of the project")
		case status.Archived:
			issues.add("settings.initial_status_id", "the status is archived")
		}
	}
	if utf8.RuneCountInString(st.SuccessMessage) > formMaxMessageLen {
		issues.add("settings.success_message", "may be at most %d characters long", formMaxMessageLen)
	}
	st.RedirectURL = strings.TrimSpace(st.RedirectURL)
	if st.RedirectURL != "" && (len(st.RedirectURL) > formMaxURLLen || !isHTTPURL(st.RedirectURL)) {
		issues.add("settings.redirect_url", "must be an http or https address")
	}
	if st.MaxSubmissions < 0 {
		issues.add("settings.max_submissions", "must not be negative")
	}
	if st.NameRequired && !st.CollectName {
		issues.add("settings.name_required", "needs collect_name")
	}
	if st.EmailRequired && !st.CollectEmail {
		issues.add("settings.email_required", "needs collect_email")
	}
	st.DefaultAssigneeIDs = uniqueInts(st.DefaultAssigneeIDs)
	st.NotifyUserIDs = uniqueInts(st.NotifyUserIDs)
	for _, list := range []struct {
		path string
		ids  []int64
	}{{"settings.default_assignee_ids", st.DefaultAssigneeIDs}, {"settings.notify_user_ids", st.NotifyUserIDs}} {
		if len(list.ids) > formMaxListLen {
			issues.add(list.path, "may have at most %d users", formMaxListLen)
			continue
		}
		for i, id := range list.ids {
			ok, err := userCanReadProject(s, id, f.ProjectID)
			if err != nil {
				return err
			}
			if !ok {
				issues.add(fmt.Sprintf("%s[%d]", list.path, i), "the user does not exist or cannot read the project")
			}
		}
	}
	return nil
}

func userCanReadProject(s *xorm.Session, userID, projectID int64) (bool, error) {
	u, err := user.GetUserByID(s, userID)
	if err != nil {
		if user.IsErrUserDoesNotExist(err) || user.IsErrUserStatusError(err) {
			return false, nil
		}
		return false, err
	}
	can, _, err := (&Project{ID: projectID}).CanRead(s, u)
	if err != nil {
		if IsErrProjectDoesNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return can, nil
}

func (f *Form) checkFeature() error {
	if !config.FormsEnabled.GetBool() {
		return ErrFormsDisabled{}
	}
	return nil
}

// Create validates and stores a form. The creator becomes its owner and a new token is generated.
// @Summary Create a form
func (f *Form) Create(s *xorm.Session, a web.Auth) error {
	ownerID, err := ownerOf(a)
	if err != nil {
		return err
	}
	if err = f.checkFeature(); err != nil {
		return err
	}
	if err = f.Validate(s, a); err != nil {
		return err
	}
	if f.Token, err = newFormToken(); err != nil {
		return err
	}
	f.ID = 0
	f.OwnerID = ownerID
	f.DisabledReason = ""
	f.SubmissionCount = 0
	if _, err = s.Insert(f); err != nil {
		return err
	}
	return nil
}

// ReadOne returns one form; CanRead already loaded it.
// @Summary Get a form
func (f *Form) ReadOne(_ *xorm.Session, _ web.Auth) error {
	normalizeForm(f)
	return nil
}

// ReadAll returns the forms of a project. Needs admin access: forms carry the secret token.
// @Summary List the forms of a project
func (f *Form) ReadAll(s *xorm.Session, a web.Auth, search string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	// DoReadAll skips the Can* checks.
	if f.ProjectID < 1 {
		return nil, 0, 0, ErrProjectDoesNotExist{ID: f.ProjectID}
	}
	can, _, err := canAdministerForms(s, a, f.ProjectID)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	var cond builder.Cond = builder.Eq{"project_id": f.ProjectID}
	if search != "" {
		cond = cond.And(db.ILIKE("title", search))
	}
	total, err := s.Where(cond).Count(&Form{})
	if err != nil {
		return nil, 0, 0, err
	}
	forms := []*Form{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where(cond).OrderBy("id ASC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&forms); err != nil {
		return nil, 0, 0, err
	}
	for _, form := range forms {
		normalizeForm(form)
	}
	return forms, len(forms), total, nil
}

// Update replaces the editable fields of a form. The user who saves becomes the owner; the token stays.
// @Summary Update a form
func (f *Form) Update(s *xorm.Session, a web.Auth) error {
	ownerID, err := ownerOf(a)
	if err != nil {
		return err
	}
	existing, err := getFormInProject(s, f.ID, f.ProjectID)
	if err != nil {
		return err
	}
	if err = f.checkFeature(); err != nil {
		return err
	}
	if err = f.Validate(s, a); err != nil {
		return err
	}
	f.OwnerID = ownerID
	// Saving by hand clears what the system stored.
	f.DisabledReason = ""
	if _, err = s.ID(f.ID).
		Cols("title", "description", "access", "enabled", "owner_id", "fields", "settings", "disabled_reason", "max_submissions").
		Update(f); err != nil {
		return err
	}
	stored, err := getFormInProject(s, f.ID, f.ProjectID)
	if err != nil {
		return err
	}
	*f = *stored
	f.Token = existing.Token
	normalizeForm(f)
	return nil
}

// Delete removes a form with its submissions. The tasks it created stay.
// @Summary Delete a form
func (f *Form) Delete(s *xorm.Session, _ web.Auth) error {
	if _, err := s.Where("form_id = ?", f.ID).Delete(&FormSubmission{}); err != nil {
		return err
	}
	_, err := s.ID(f.ID).Delete(&Form{})
	return err
}

// FormToken is the action that replaces the secret token of a form.
type FormToken struct {
	ID        int64 `json:"-" param:"form"`
	ProjectID int64 `json:"-" param:"project"`
	// Token is the new token.
	Token string `json:"token" readOnly:"true" doc:"The new secret token. The old link stops working."`

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

// Create generates a new token. The old one is gone for good.
// @Summary Regenerate the token of a form
func (t *FormToken) Create(s *xorm.Session, _ web.Auth) error {
	token, err := newFormToken()
	if err != nil {
		return err
	}
	if _, err = s.ID(t.ID).Cols("token").Update(&Form{Token: token}); err != nil {
		return err
	}
	t.Token = token
	return nil
}

// ReadAll lists the submissions of a form, newest first. The salted IP hash is never part of the answer.
// @Summary List the submissions of a form
func (sub *FormSubmission) ReadAll(s *xorm.Session, a web.Auth, _ string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	// DoReadAll skips the Can* checks.
	can, _, err := canAdministerForms(s, a, sub.ProjectID)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}
	if _, err = getFormInProject(s, sub.FormID, sub.ProjectID); err != nil {
		return nil, 0, 0, err
	}

	cond := builder.Eq{"form_id": sub.FormID}
	total, err := s.Where(cond).Count(&FormSubmission{})
	if err != nil {
		return nil, 0, 0, err
	}
	rows := []*FormSubmission{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where(cond).OrderBy("id DESC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&rows); err != nil {
		return nil, 0, 0, err
	}
	return rows, len(rows), total, nil
}

// FormTargetInfo describes one thing a form can ask for in a project.
type FormTargetInfo struct {
	Target        string               `json:"target" doc:"The value of a field's target: title, description, due_date, priority, labels, attachments or cf_<id>."`
	Kind          string               `json:"kind" doc:"What kind of answer it takes: text, rich_text, date, priority, labels, files, or the custom field type."`
	Title         string               `json:"title" doc:"The title of the custom field; empty for built-in targets."`
	CustomFieldID int64                `json:"custom_field_id,omitempty" doc:"The id of the custom field, for cf_<id> targets."`
	Config        *CustomFieldConfig   `json:"config,omitempty" doc:"The type specific settings of the custom field."`
	Options       []*CustomFieldOption `json:"options,omitempty" doc:"The choices of a select or multi_select custom field."`
	PublicVisible bool                 `json:"public_visible" doc:"Whether a public form may ask for it. False for labels and user fields, which would need project data pickers, and for attachments unless forms.public.attachments is enabled."`
	PublicHidden  bool                 `json:"public_hidden" doc:"Whether a public form may preset it as a hidden field."`
}

// FormOptions lists what the forms of a project can ask for. The builder is generated from it.
type FormOptions struct {
	ProjectID int64 `json:"-" param:"project"`

	Targets []*FormTargetInfo `json:"targets" readOnly:"true" doc:"The built-in targets followed by the custom fields of the project, in their order."`
	// Public says whether public forms exist on this instance and may ask for attachments.
	PublicEnabled     bool `json:"public_enabled" readOnly:"true" doc:"Whether public forms can be saved on this instance (forms.public.enabled)."`
	PublicAttachments bool `json:"public_attachments" readOnly:"true" doc:"Whether public forms may ask for attachments (forms.public.attachments)."`

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

// ReadOne fills the options.
// @Summary List what a form of the project can ask for
func (o *FormOptions) ReadOne(s *xorm.Session, _ web.Auth) error {
	o.PublicEnabled = config.FormsPublicEnabled.GetBool()
	o.PublicAttachments = config.FormsPublicAttachments.GetBool() && config.ServiceEnableTaskAttachments.GetBool()
	o.Targets = []*FormTargetInfo{
		{Target: FormTargetTitle, Kind: "text", PublicVisible: true, PublicHidden: true},
		{Target: FormTargetDescription, Kind: "rich_text", PublicVisible: true, PublicHidden: true},
		{Target: FormTargetDueDate, Kind: "date", PublicVisible: true, PublicHidden: true},
		{Target: FormTargetPriority, Kind: "priority", PublicVisible: true, PublicHidden: true},
		{Target: FormTargetLabels, Kind: "labels", PublicVisible: false, PublicHidden: true},
	}
	if config.ServiceEnableTaskAttachments.GetBool() {
		o.Targets = append(o.Targets, &FormTargetInfo{Target: FormTargetAttachments, Kind: "files", PublicVisible: o.PublicAttachments})
	}

	fields := []*CustomField{}
	if err := s.Where("project_id = ?", o.ProjectID).OrderBy("position ASC, id ASC").Find(&fields); err != nil {
		return err
	}
	ids := make([]int64, 0, len(fields))
	for _, cf := range fields {
		ids = append(ids, cf.ID)
	}
	options, err := getCustomFieldOptions(s, ids)
	if err != nil {
		return err
	}
	for _, cf := range fields {
		info := &FormTargetInfo{
			Target:        "cf_" + strconv.FormatInt(cf.ID, 10),
			Kind:          string(cf.Type),
			Title:         cf.Title,
			CustomFieldID: cf.ID,
			Config:        cf.Config,
			Options:       options[cf.ID],
			PublicVisible: cf.Type != CustomFieldTypeUser,
			PublicHidden:  true,
		}
		o.Targets = append(o.Targets, info)
	}
	return nil
}
