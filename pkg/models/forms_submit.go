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
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/i18n"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/c2h5oh/datasize"
	"github.com/gabriel-vasile/mimetype"
	"github.com/microcosm-cc/bluemonday"
	"xorm.io/builder"
	"xorm.io/xorm"
)

// formRateWindow is the window of the per IP and per form submission limits.
const formRateWindow = time.Hour

// The part of the sanitizer every rich text of a form goes through: basic formatting and links. No images, no
// forms, no styles, no event handlers, only http, https and mailto links.
var formHTMLPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "strong", "b", "em", "i", "u", "s", "del", "ul", "ol", "li", "blockquote", "code", "pre",
		"h1", "h2", "h3", "h4", "h5", "h6", "hr", "table", "thead", "tbody", "tr", "th", "td")
	p.AllowStandardURLs()
	p.AllowAttrs("href").OnElements("a")
	p.RequireNoFollowOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

var formHTMLTag = regexp.MustCompile(`<\s*/?\s*[a-zA-Z][^>]*>`)

func sanitizeFormHTML(in string) string {
	return strings.TrimSpace(formHTMLPolicy.Sanitize(in))
}

// cleanFormText removes control characters (NUL included) from plain text. Newlines and tabs stay when multiline.
func cleanFormText(in string, multiline bool) string {
	in = strings.ReplaceAll(in, "\r\n", "\n")
	in = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			if multiline {
				return r
			}
			return ' '
		case r == '\r':
			return ' '
		case r < 0x20 || r == 0x7f || r == 0xfffd:
			return -1
		}
		return r
	}, in)
	return strings.TrimSpace(in)
}

// formRichText turns an answer of a rich text field into the HTML a task description holds. Text that does not
// look like HTML is escaped and put into paragraphs; HTML is sanitized.
func formRichText(in string) string {
	in = strings.ReplaceAll(in, "\x00", "")
	if !formHTMLTag.MatchString(in) {
		in = strings.TrimSpace(in)
		if in == "" {
			return ""
		}
		escaped := html.EscapeString(strings.ReplaceAll(in, "\r\n", "\n"))
		paragraphs := strings.Split(escaped, "\n\n")
		for i, p := range paragraphs {
			paragraphs[i] = "<p>" + strings.ReplaceAll(strings.TrimSpace(p), "\n", "<br>") + "</p>"
		}
		return strings.Join(paragraphs, "")
	}
	return sanitizeFormHTML(in)
}

var formPlaceholder = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.]+)\s*\}\}`)

var formPlaceholderNames = []string{"submitter.name", "submitter.email", "form.title", "date"}

// checkFormTemplate returns a message for unknown placeholders.
func checkFormTemplate(tpl string) string {
	for _, m := range formPlaceholder.FindAllStringSubmatch(tpl, -1) {
		if !slices.Contains(formPlaceholderNames, m[1]) {
			return fmt.Sprintf("unknown placeholder {{%s}}, use one of %s", m[1], strings.Join(formPlaceholderNames, ", "))
		}
	}
	return ""
}

// renderFormTemplate is a plain substitution: nothing is evaluated, unknown placeholders render empty.
func renderFormTemplate(tpl string, vars map[string]string, escape func(string) string) string {
	return formPlaceholder.ReplaceAllStringFunc(tpl, func(match string) string {
		v := vars[formPlaceholder.FindStringSubmatch(match)[1]]
		if escape != nil {
			return escape(v)
		}
		return v
	})
}

// FormPublicOption is a choice of a select field or a label of a labels field.
type FormPublicOption struct {
	ID    int64  `json:"id" doc:"The id to submit."`
	Title string `json:"title" doc:"The text to show."`
	Color string `json:"color,omitempty" doc:"A hex color including the leading #, when there is one."`
}

// FormPublicField is a field the submitter fills in.
type FormPublicField struct {
	Target    string             `json:"target" doc:"The key of the answer in values."`
	Label     string             `json:"label" doc:"The question."`
	Help      string             `json:"help" doc:"Help text, may be empty."`
	Required  bool               `json:"required" doc:"Whether an answer is required."`
	Kind      string             `json:"kind" doc:"How to render it: text, rich_text, date, priority, labels, files or the custom field type (text, long_text, number, currency, date, checkbox, select, multi_select, user, url, email, rating, progress)."`
	Default   any                `json:"default,omitempty" doc:"The prefilled value."`
	MaxLength int                `json:"max_length,omitempty" doc:"The maximum length in characters of a text answer."`
	Config    *CustomFieldConfig `json:"config,omitempty" doc:"Type specific settings of a custom field: min, max, precision, currency, max_rating."`
	Options   []FormPublicOption `json:"options,omitempty" doc:"The choices of a select, multi_select or labels field."`
}

// FormPublicSuccess is what to do after a submission.
type FormPublicSuccess struct {
	Message       string `json:"message" doc:"Text to show after the submission."`
	RedirectURL   string `json:"redirect_url" doc:"Where to send the submitter, empty for nowhere."`
	SubmitAnother bool   `json:"submit_another" doc:"Whether to offer another submission."`
}

// FormPublicSubmitter says which details of the submitter are asked.
type FormPublicSubmitter struct {
	CollectName   bool `json:"collect_name" doc:"Whether to ask for a name (submitter.name)."`
	NameRequired  bool `json:"name_required" doc:"Whether the name is required."`
	CollectEmail  bool `json:"collect_email" doc:"Whether to ask for an e-mail address (submitter.email)."`
	EmailRequired bool `json:"email_required" doc:"Whether the e-mail address is required."`
}

// FormPublicAttachments are the limits of the files a form accepts.
type FormPublicAttachments struct {
	MaxFiles    int    `json:"max_files" doc:"The maximum number of files."`
	MaxFileSize uint64 `json:"max_file_size" doc:"The maximum size of one file in bytes."`
}

// FormPublicDefinition is everything a submitter may learn about a form: what to ask and what to do afterwards.
// It names no project, owner, count, hidden field or user.
type FormPublicDefinition struct {
	Title       string                 `json:"title" doc:"The title of the form."`
	Description string                 `json:"description" doc:"Sanitized rich text (HTML) shown above the form."`
	Fields      []*FormPublicField     `json:"fields" doc:"The fields to show, in order. Hidden fields are not part of it."`
	Success     FormPublicSuccess      `json:"success" doc:"What to do after a submission."`
	Submitter   FormPublicSubmitter    `json:"submitter" doc:"Which details of the submitter are asked."`
	Attachments *FormPublicAttachments `json:"attachments,omitempty" doc:"The file limits. Only present when the form asks for attachments."`
	Branding    FormPublicBranding     `json:"branding" doc:"How the public page looks. All parts are optional."`
}

// FormPublicBranding is what the public page needs to look like the form's owner, not like the application.
type FormPublicBranding struct {
	Name        string `json:"name" doc:"The brand name to show, empty for none."`
	AccentColor string `json:"accent_color" doc:"The accent color as #rrggbb, empty for the default."`
	HasLogo     bool   `json:"has_logo" doc:"Whether a logo exists. Load it from GET /forms/{token}/logo."`
	HideFooter  bool   `json:"hide_footer" doc:"Whether the 'Powered by' footer link is hidden."`
}

// FormAttachmentLimits returns the number of files and the size of a file a submission may carry.
func FormAttachmentLimits() (maxFiles int, maxSize uint64) {
	maxFiles = config.FormsMaxAttachments.GetInt()
	var size datasize.ByteSize
	if err := size.UnmarshalText([]byte(config.FormsMaxAttachmentSize.GetString())); err != nil || size == 0 {
		size = 10 * datasize.MB
	}
	maxSize = uint64(size)
	if fileMax := config.GetMaxFileSizeInMBytes() * uint64(datasize.MB); fileMax > 0 && fileMax < maxSize {
		maxSize = fileMax
	}
	return
}

func formUnavailableReason(f *Form) string {
	switch {
	case !config.FormsEnabled.GetBool(), !f.Enabled:
		return FormUnavailableDisabled
	case f.Access == FormAccessPublic && !config.FormsPublicEnabled.GetBool():
		return FormUnavailableDisabled
	case f.Settings != nil && f.Settings.CloseDate != nil && !f.Settings.CloseDate.IsZero() && !formClock().Before(*f.Settings.CloseDate):
		return FormUnavailableClosed
	case f.MaxSubmissions > 0 && f.SubmissionCount >= f.MaxSubmissions:
		return FormUnavailableFull
	}
	return ""
}

func unavailable(f *Form, reason string) ErrFormUnavailable {
	return ErrFormUnavailable{FormUnavailableDetails: FormUnavailableDetails{Reason: reason, Title: f.Title}}
}

// formSubmitterUser returns the signed-in user of an auth, nil for anonymous requests and link shares.
func formSubmitterUser(a web.Auth) *user.User {
	u, ok := a.(*user.User)
	if !ok || u == nil || u.ID <= 0 {
		return nil
	}
	return u
}

// checkFormAccess applies the access mode of the form to the auth of the request, which is nil when there is none.
func checkFormAccess(s *xorm.Session, f *Form, a web.Auth) error {
	if f.Access == FormAccessPublic {
		return nil
	}
	if a == nil {
		return ErrFormLoginRequired{}
	}
	u := formSubmitterUser(a)
	if u == nil {
		return ErrFormNotAllowed{}
	}
	if f.Access == FormAccessMembers {
		can, _, err := (&Project{ID: f.ProjectID}).CanRead(s, u)
		if err != nil && !IsErrProjectDoesNotExist(err) {
			return err
		}
		if err != nil || !can {
			return ErrFormNotAllowed{}
		}
	}
	return nil
}

// GetFormDefinition returns the public definition of the form with the token. a is the auth of the request, nil
// when there is none.
func GetFormDefinition(token string, a web.Auth) (*FormPublicDefinition, error) {
	s := db.NewReadSession()
	defer s.Close()

	f, err := GetFormByToken(s, token)
	if err != nil {
		return nil, err
	}
	normalizeForm(f)
	if reason := formUnavailableReason(f); reason != "" {
		return nil, unavailable(f, reason)
	}
	if err = checkFormAccess(s, f, a); err != nil {
		return nil, err
	}
	return buildFormDefinition(s, f)
}

func buildFormDefinition(s *xorm.Session, f *Form) (*FormPublicDefinition, error) {
	cfs := []*CustomField{}
	if err := s.Where("project_id = ?", f.ProjectID).Find(&cfs); err != nil {
		return nil, err
	}
	byID := map[int64]*CustomField{}
	ids := []int64{}
	for _, cf := range cfs {
		byID[cf.ID] = cf
		ids = append(ids, cf.ID)
	}
	options, err := getCustomFieldOptions(s, ids)
	if err != nil {
		return nil, err
	}

	def := &FormPublicDefinition{
		Title:       f.Title,
		Description: sanitizeFormHTML(f.Description),
		Fields:      []*FormPublicField{},
		Success: FormPublicSuccess{
			Message:       f.Settings.SuccessMessage,
			RedirectURL:   f.Settings.RedirectURL,
			SubmitAnother: f.Settings.SubmitAnother,
		},
		Branding: FormPublicBranding{
			Name:        f.BrandName,
			AccentColor: f.AccentColor,
			HasLogo:     f.BrandLogoFileID != 0,
			HideFooter:  f.HideFooter,
		},
		Submitter: FormPublicSubmitter{
			CollectName:   f.Settings.CollectName,
			NameRequired:  f.Settings.CollectName && f.Settings.NameRequired,
			CollectEmail:  f.Settings.CollectEmail,
			EmailRequired: f.Settings.CollectEmail && f.Settings.EmailRequired,
		},
	}
	for _, field := range f.Fields {
		if field.Hidden {
			continue
		}
		pf := &FormPublicField{Target: field.Target, Label: field.Label, Help: field.Help, Required: field.Required}
		if str, ok := field.Default.(string); !ok || !strings.Contains(str, "{{") {
			if !isEmptyFormDefault(field.Default) {
				pf.Default = field.Default
			}
		}
		switch field.Target {
		case FormTargetTitle:
			pf.Kind, pf.MaxLength = "text", formMaxTitleLen
		case FormTargetDescription:
			pf.Kind, pf.MaxLength = "rich_text", formMaxLongTextLen
		case FormTargetDueDate:
			pf.Kind = "date"
		case FormTargetPriority:
			pf.Kind = "priority"
		case FormTargetLabels:
			pf.Kind = "labels"
			labels := []*Label{}
			if len(field.Options) > 0 {
				if err = s.Where(builder.In("id", field.Options)).Find(&labels); err != nil {
					return nil, err
				}
			}
			for _, id := range field.Options {
				for _, l := range labels {
					if l.ID == id {
						pf.Options = append(pf.Options, FormPublicOption{ID: l.ID, Title: l.Title, Color: l.HexColor})
					}
				}
			}
		case FormTargetAttachments:
			pf.Kind = "files"
			maxFiles, maxSize := FormAttachmentLimits()
			def.Attachments = &FormPublicAttachments{MaxFiles: maxFiles, MaxFileSize: maxSize}
		default:
			id, _ := customFieldIDOfTarget(field.Target)
			cf := byID[id]
			if cf == nil {
				continue
			}
			pf.Kind = string(cf.Type)
			pf.Config = cf.Config
			switch cf.Type { //nolint:exhaustive // the other types have neither a length nor options
			case CustomFieldTypeText, CustomFieldTypeURL, CustomFieldTypeEmail:
				pf.MaxLength = formMaxTextLen
			case CustomFieldTypeLongText:
				pf.MaxLength = formMaxLongTextLen
			case CustomFieldTypeSelect, CustomFieldTypeMultiSelect:
				for _, o := range options[cf.ID] {
					pf.Options = append(pf.Options, FormPublicOption{ID: o.ID, Title: o.Title, Color: o.Color})
				}
			}
		}
		def.Fields = append(def.Fields, pf)
	}
	return def, nil
}

// FormSubmitRequest is one submission.
type FormSubmitRequest struct {
	Token string
	// Auth is the auth of the request, nil when there is none.
	Auth web.Auth
	// IP is the client address as the router determined it. It is only stored as a salted hash.
	IP             string
	Values         map[string]any
	SubmitterName  string
	SubmitterEmail string
	Honeypot       string
	Files          []*AttachmentToUpload

	uploadedFileIDs []int64
	publicForm      bool
}

// FormSubmitResult is the answer to a submission.
type FormSubmitResult struct {
	Message       string `json:"message" doc:"Text to show to the submitter."`
	RedirectURL   string `json:"redirect_url" doc:"Where to send the submitter, empty for nowhere."`
	SubmitAnother bool   `json:"submit_another" doc:"Whether to offer another submission."`
	// TaskID is only set for signed-in submitters who can read the project, never for public submissions.
	TaskID *int64 `json:"task_id,omitempty" doc:"The created task. Only sent to signed-in submitters who can read the project."`
}

func formIPHash(ip string) string {
	key := hmac.New(sha256.New, []byte(config.ServiceSecret.GetString()))
	key.Write([]byte("vikunja-forms-ip-hash-v1"))
	mac := hmac.New(sha256.New, key.Sum(nil))
	mac.Write([]byte(ip))
	return hex.EncodeToString(mac.Sum(nil))
}

// SubmitForm validates a submission and creates the task, all in one transaction.
func SubmitForm(ctx context.Context, req *FormSubmitRequest) (*FormSubmitResult, error) {
	s := db.NewSession()
	defer s.Close()

	res, err := submitForm(s, req)
	if err != nil {
		var un ErrFormUnavailable
		if errors.As(err, &un) && un.persisted {
			events.CleanupPending(s)
			if cerr := s.Commit(); cerr != nil {
				return nil, cerr
			}
			return nil, err
		}
		_ = s.Rollback()
		events.CleanupPending(s)
		req.removeUploads()
		return nil, err
	}
	if err = events.PendingHookError(s); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		req.removeUploads()
		return nil, err
	}
	if err = s.Commit(); err != nil {
		events.CleanupPending(s)
		req.removeUploads()
		return nil, err
	}
	events.DispatchPending(ctx, s)
	return res, nil
}

// removeUploads deletes the blobs of files stored before the transaction failed.
func (r *FormSubmitRequest) removeUploads() {
	for _, id := range r.uploadedFileIDs {
		if err := files.DeleteBlob(id); err != nil {
			log.Errorf("Could not remove the file %d of a failed form submission: %s", id, err)
		}
	}
	r.uploadedFileIDs = nil
}

type formAnswers struct {
	title       string
	description string
	hasDesc     bool
	due         time.Time
	priority    int64
	labels      []int64
	cfs         []formCFAnswer
}

type formCFAnswer struct {
	id    int64
	value any
}

func submitForm(s *xorm.Session, req *FormSubmitRequest) (*FormSubmitResult, error) {
	// A write first takes the lock of the form: on PostgreSQL and MySQL a row lock that serializes the
	// submissions of one form, on SQLite the database lock, so nothing that is read can change under us.
	if !isFormToken(req.Token) {
		return nil, ErrFormDoesNotExist{}
	}
	if _, err := s.Exec("UPDATE forms SET token = token WHERE token = ?", req.Token); err != nil {
		return nil, err
	}
	form, err := GetFormByToken(s, req.Token)
	if err != nil {
		return nil, err
	}
	normalizeForm(form)
	req.publicForm = form.Access == FormAccessPublic
	if reason := formUnavailableReason(form); reason != "" {
		return nil, unavailable(form, reason)
	}
	if err = checkFormAccess(s, form, req.Auth); err != nil {
		return nil, err
	}

	result := &FormSubmitResult{
		Message:       form.Settings.SuccessMessage,
		RedirectURL:   form.Settings.RedirectURL,
		SubmitAnother: form.Settings.SubmitAnother,
	}
	if req.Honeypot != "" {
		// A bot gets the answer of a success and nothing is created.
		return result, nil
	}

	now := formClock().UTC()
	ipHash := ""
	if req.IP != "" {
		ipHash = formIPHash(req.IP)
	}
	if form.Access == FormAccessPublic {
		if err = checkFormRateLimits(s, form, ipHash, now); err != nil {
			return nil, err
		}
	}

	owner, err := loadFormOwner(s, form)
	if err != nil {
		return nil, err
	}

	submitter := formSubmitterUser(req.Auth)
	if submitter != nil {
		// The claims of a token only carry the id and the username: the name and e-mail address come from the account.
		// GetUserByID leaves the address out; only this lookup returns it.
		full, uerr := user.GetUserWithEmail(s, &user.User{ID: submitter.ID})
		switch {
		case uerr == nil:
			submitter = full
		case user.IsErrUserDoesNotExist(uerr):
			return nil, ErrFormNotAllowed{}
		default:
			return nil, uerr
		}
	}
	// What the submitter says about themselves only counts when the form asks for it: it is validated only then,
	// and a signed-in submitter is otherwise recorded with what their account knows. The e-mail address of the
	// account is not told to anybody but the people who may read the submissions: it is neither put into the task nor
	// into the event, whose audience is wider (everybody who can read the task, rules and webhooks).
	var name, email, accountEmail string
	var fieldErrs []FormFieldError
	switch {
	case form.Settings.CollectName:
		name = cleanFormText(req.SubmitterName, false)
	case submitter != nil:
		name = submitter.GetName()
	}
	if form.Settings.CollectEmail {
		email = cleanFormText(req.SubmitterEmail, false)
	}
	if submitter != nil && form.Access != FormAccessPublic {
		// A public form is anonymous by nature: a signed-in visitor is not asked whether they want their account
		// address kept.
		accountEmail = submitter.Email
	}
	fieldErrs = append(fieldErrs, validateSubmitter(form.Settings, name, email)...)

	answers, attachErrs, err := collectFormAnswers(s, form, req, name, email, now, owner)
	if err != nil {
		return nil, err
	}
	fieldErrs = append(fieldErrs, attachErrs...)
	if len(fieldErrs) > 0 {
		return nil, ErrFormSubmissionInvalid{FormSubmissionInvalidDetails{Errors: fieldErrs}}
	}

	res, err := s.Exec("UPDATE forms SET submission_count = submission_count + 1 WHERE id = ? AND (max_submissions = 0 OR submission_count < max_submissions)", form.ID)
	if err != nil {
		return nil, err
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		return nil, unavailable(form, FormUnavailableFull)
	}

	task, err := createFormTask(s, form, req, owner, answers, name, email)
	if err != nil {
		return nil, err
	}

	var submitterID *int64
	if submitter != nil {
		id := submitter.ID
		submitterID = &id
	}
	sub := &FormSubmission{
		FormID:         form.ID,
		TaskID:         task.ID,
		SubmitterID:    submitterID,
		SubmitterName:  truncateRunes(name, 250),
		SubmitterEmail: truncateRunes(email, 250),
		IPHash:         ipHash,
		Created:        now,
	}
	if sub.SubmitterEmail == "" {
		sub.SubmitterEmail = truncateRunes(accountEmail, 250)
	}
	if _, err = s.Insert(sub); err != nil {
		return nil, err
	}

	events.DispatchOnCommit(s, &FormSubmittedEvent{
		FormID:         form.ID,
		FormTitle:      form.Title,
		SubmissionID:   sub.ID,
		Task:           task,
		Doer:           owner,
		SubmitterID:    sub.SubmitterID,
		SubmitterName:  sub.SubmitterName,
		SubmitterEmail: truncateRunes(email, 250),
	})

	if submitter != nil {
		if can, _, cerr := (&Project{ID: form.ProjectID}).CanRead(s, submitter); cerr == nil && can {
			result.TaskID = &task.ID
		}
	}
	return result, nil
}

func truncateRunes(in string, n int) string {
	if utf8.RuneCountInString(in) <= n {
		return in
	}
	return string([]rune(in)[:n])
}

// loadFormOwner loads the owner and makes sure they can still write to the project. If not, the form is disabled
// with a stored reason and the submission fails like a disabled form does.
func loadFormOwner(s *xorm.Session, form *Form) (*user.User, error) {
	disable := func(reason string) error {
		if _, err := s.ID(form.ID).Cols("enabled", "disabled_reason").Update(&Form{Enabled: false, DisabledReason: reason}); err != nil {
			return err
		}
		err := unavailable(form, FormUnavailableDisabled)
		err.persisted = true
		return err
	}
	owner, err := user.GetUserByID(s, form.OwnerID)
	if err != nil {
		if user.IsErrUserDoesNotExist(err) || user.IsErrUserStatusError(err) {
			return nil, disable(FormDisabledOwnerUnavailable)
		}
		return nil, err
	}
	can, err := (&Project{ID: form.ProjectID}).CanWrite(s, owner)
	if err != nil {
		if IsErrProjectDoesNotExist(err) || IsErrProjectIsArchived(err) {
			// Archived and trashed projects can come back: the form stays as it is.
			return nil, unavailable(form, FormUnavailableDisabled)
		}
		return nil, err
	}
	if !can {
		return nil, disable(FormDisabledOwnerNotWriter)
	}
	return owner, nil
}

// checkFormRateLimits counts the submissions of the window in the database, which is what makes the limits hold
// for several replicas. The answer says when the oldest counted submission leaves the window.
func checkFormRateLimits(s *xorm.Session, form *Form, ipHash string, now time.Time) error {
	cutoff := now.Add(-formRateWindow).Format(dbTimeFormat)
	check := func(cond builder.Cond, limit int64) error {
		if limit <= 0 {
			return nil
		}
		total, err := s.Where(builder.And(cond, builder.Gte{"created": cutoff})).Count(&FormSubmission{})
		if err != nil {
			return err
		}
		if total < limit {
			return nil
		}
		// The submission that frees a slot is the one total-limit places from the oldest.
		rows := []*FormSubmission{}
		if err = s.Where(builder.And(cond, builder.Gte{"created": cutoff})).OrderBy("created ASC, id ASC").Limit(1, int(total-limit)).Find(&rows); err != nil {
			return err
		}
		retry := int64(formRateWindow / time.Second)
		if len(rows) == 1 {
			retry = int64(rows[0].Created.UTC().Add(formRateWindow).Sub(now)/time.Second) + 1
		}
		return ErrFormRateLimited{RetryAfterSeconds: max(retry, 1)}
	}
	if ipHash != "" {
		if err := check(builder.Eq{"ip_hash": ipHash}, config.FormsPublicRateLimit.GetInt64()); err != nil {
			return err
		}
	}
	return check(builder.Eq{"form_id": form.ID}, config.FormsPublicFormRateLimit.GetInt64())
}

func validateSubmitter(st *FormSettings, name, email string) []FormFieldError {
	var errs []FormFieldError
	if st.CollectName {
		switch {
		case name == "" && st.NameRequired:
			errs = append(errs, FormFieldError{Field: "submitter.name", Code: "required", Message: "A name is required."})
		case utf8.RuneCountInString(name) > 250:
			errs = append(errs, FormFieldError{Field: "submitter.name", Code: "too_long", Message: "The name is too long."})
		}
	}
	if st.CollectEmail {
		switch {
		case email == "" && st.EmailRequired:
			errs = append(errs, FormFieldError{Field: "submitter.email", Code: "required", Message: "An e-mail address is required."})
		case email != "":
			if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 250 {
				errs = append(errs, FormFieldError{Field: "submitter.email", Code: "invalid", Message: "This is not an e-mail address."})
			}
		}
	}
	return errs
}

func isEmptyAnswer(v any) bool {
	if str, ok := v.(string); ok {
		return strings.TrimSpace(str) == ""
	}
	return isEmptyCustomFieldValue(v)
}

// collectFormAnswers validates the values against the visible fields and merges them with the defaults of the
// hidden ones. Problems are returned per field.
//
//nolint:gocyclo
func collectFormAnswers(s *xorm.Session, form *Form, req *FormSubmitRequest, name, email string, now time.Time, owner *user.User) (*formAnswers, []FormFieldError, error) {
	var errs []FormFieldError
	fail := func(field, code, msg string) {
		errs = append(errs, FormFieldError{Field: field, Code: code, Message: msg})
	}

	visible := map[string]*FormField{}
	for _, f := range form.Fields {
		if !f.Hidden {
			visible[f.Target] = f
		}
	}
	keys := make([]string, 0, len(req.Values))
	for k := range req.Values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, ok := visible[k]; !ok {
			fail(truncateRunes(k, 100), "unknown_field", "This form does not ask for this value.")
		}
	}
	if len(req.Files) > 0 && visible[FormTargetAttachments] == nil {
		fail(FormTargetAttachments, "unknown_field", "This form does not take files.")
	}

	vars := map[string]string{
		"submitter.name":  name,
		"submitter.email": email,
		"form.title":      form.Title,
		"date":            now.Format("2006-01-02"),
	}

	cfs := []*CustomField{}
	if err := s.Where("project_id = ?", form.ProjectID).Find(&cfs); err != nil {
		return nil, nil, err
	}
	cfByID := map[int64]*CustomField{}
	for _, cf := range cfs {
		cfByID[cf.ID] = cf
	}

	ans := &formAnswers{}
	for _, field := range form.Fields {
		var raw any
		present := false
		if field.Hidden {
			raw, present = field.Default, !isEmptyFormDefault(field.Default)
		} else if v, ok := req.Values[field.Target]; ok {
			raw, present = v, true
		} else if !isEmptyFormDefault(field.Default) {
			raw, present = field.Default, true
		}
		isDefault := field.Hidden
		if !field.Hidden && present {
			if _, given := req.Values[field.Target]; !given {
				isDefault = true
			}
		}

		if field.Target == FormTargetAttachments {
			errs = append(errs, validateFormFiles(field, req)...)
			continue
		}
		if !present || isEmptyAnswer(raw) {
			switch {
			case field.Target == FormTargetTitle && !isEmptyFormDefault(field.Default):
				raw, present = field.Default, true
				isDefault = true
			case field.Required && !field.Hidden:
				fail(field.Target, "required", "This field is required.")
				continue
			default:
				continue
			}
		}

		msgs, err := applyFormAnswer(s, ans, form, field, cfByID, raw, isDefault, vars)
		if err != nil {
			return nil, nil, err
		}
		if len(msgs) > 0 && !field.Hidden {
			errs = append(errs, msgs...)
		} else if len(msgs) > 0 {
			log.Warningf("Form %d: the default of the hidden field %s is no longer valid, ignoring it", form.ID, field.Target)
		}
	}
	if ans.title == "" && !hasFieldError(errs, FormTargetTitle) {
		fail(FormTargetTitle, "required", "This field is required.")
	}
	_ = owner
	return ans, errs, nil
}

func hasFieldError(errs []FormFieldError, field string) bool {
	return slices.ContainsFunc(errs, func(e FormFieldError) bool { return e.Field == field })
}

// applyFormAnswer validates one value and stores it in the answers. Defaults may contain placeholders, answers
// never do.
//
//nolint:gocyclo
func applyFormAnswer(s *xorm.Session, ans *formAnswers, form *Form, field *FormField, cfs map[int64]*CustomField, raw any, isDefault bool, vars map[string]string) ([]FormFieldError, error) {
	one := func(code, msg string) []FormFieldError {
		return []FormFieldError{{Field: field.Target, Code: code, Message: msg}}
	}
	text := func(limit int, multiline bool) (string, []FormFieldError) {
		str, ok := raw.(string)
		if !ok {
			return "", one("invalid", "Expected text.")
		}
		if isDefault {
			str = renderFormTemplate(str, vars, nil)
		}
		if utf8.RuneCountInString(str) > limit {
			if isDefault {
				return truncateRunes(str, limit), nil
			}
			return "", one("too_long", fmt.Sprintf("At most %d characters are allowed.", limit))
		}
		return cleanFormText(str, multiline), nil
	}

	switch field.Target {
	case FormTargetTitle:
		str, errs := text(formMaxTitleLen, false)
		if errs != nil {
			return errs, nil
		}
		ans.title = str
	case FormTargetDescription:
		str, ok := raw.(string)
		if !ok {
			return one("invalid", "Expected text."), nil
		}
		if utf8.RuneCountInString(str) > formMaxLongTextLen {
			return one("too_long", fmt.Sprintf("At most %d characters are allowed.", formMaxLongTextLen)), nil
		}
		if isDefault {
			str = renderFormTemplate(str, vars, func(v string) string {
				if formHTMLTag.MatchString(str) {
					return html.EscapeString(v)
				}
				return v
			})
		}
		ans.description, ans.hasDesc = formRichText(str), true
	case FormTargetDueDate:
		d, ok := parseFormDate(raw)
		if !ok {
			return one("invalid", "Expected a date."), nil
		}
		ans.due = d
	case FormTargetPriority:
		p, ok := parseFormPriority(raw)
		if !ok {
			return one("invalid", fmt.Sprintf("Expected a whole number between 0 and %d.", formMaxPriority)), nil
		}
		ans.priority = p
	case FormTargetLabels:
		ids, ok := toIntSlice(raw)
		if !ok || len(ids) > formMaxListLen {
			return one("invalid", "Expected a list of labels."), nil
		}
		ids = uniqueInts(ids)
		if !isDefault {
			for _, id := range ids {
				if !slices.Contains(field.Options, id) {
					return one("invalid", "This label is not one of the choices."), nil
				}
			}
		}
		ans.labels = ids
	default:
		id, _ := customFieldIDOfTarget(field.Target)
		cf := cfs[id]
		if cf == nil {
			return one("invalid", "The field does not exist any more."), nil
		}
		value := raw
		switch cf.Type { //nolint:exhaustive // the other types are checked by the custom field validators alone
		case CustomFieldTypeText, CustomFieldTypeURL, CustomFieldTypeEmail, CustomFieldTypeLongText:
			limit, multiline := formMaxTextLen, false
			if cf.Type == CustomFieldTypeLongText {
				limit, multiline = formMaxLongTextLen, true
			}
			str, errs := text(limit, multiline)
			if errs != nil {
				return errs, nil
			}
			value = str
		case CustomFieldTypeMultiSelect, CustomFieldTypeUser:
			if list, ok := raw.([]any); ok && len(list) > formMaxListLen {
				return one("too_many", "Too many values."), nil
			}
		case CustomFieldTypeCheckbox:
			if b, ok := raw.(bool); ok && !b && field.Required && !field.Hidden {
				return one("required", "This field is required."), nil
			}
		}
		if isEmptyCustomFieldValue(value) {
			return nil, nil
		}
		if err := formValidateCustomFieldValue(s, cf, form.ProjectID, value); err != nil {
			if IsErrCustomFieldInvalidValue(err) {
				return one("invalid", formCustomFieldMessage(err)), nil
			}
			return nil, err
		}
		ans.cfs = append(ans.cfs, formCFAnswer{id: id, value: value})
	}
	return nil, nil
}

var formDeniedExtensions = []string{
	".exe", ".dll", ".bat", ".cmd", ".com", ".scr", ".msi", ".msp", ".sh", ".bash", ".ps1", ".vbs", ".vbe", ".js", ".jse",
	".wsf", ".hta", ".lnk", ".reg", ".jar", ".apk", ".app", ".dmg", ".bin", ".elf", ".so", ".php", ".phtml", ".py", ".pl",
	".rb", ".html", ".htm", ".xhtml", ".svg",
}

var formDeniedMimes = []string{
	"application/x-executable", "application/x-msdownload", "application/x-dosexec", "application/vnd.microsoft.portable-executable",
	"application/x-sharedlib", "application/x-mach-binary", "application/x-elf", "application/x-sh", "application/x-shellscript",
	"application/x-msi", "application/java-archive", "application/x-php", "text/x-php", "text/html", "application/xhtml+xml",
	"image/svg+xml", "text/javascript", "application/javascript", "application/x-python", "text/x-python", "text/x-shellscript",
}

func validateFormFiles(field *FormField, req *FormSubmitRequest) []FormFieldError {
	one := func(code, msg string) []FormFieldError {
		return []FormFieldError{{Field: field.Target, Code: code, Message: msg}}
	}
	if len(req.Files) == 0 {
		if field.Required {
			return one("required", "This field is required.")
		}
		return nil
	}
	if !config.ServiceEnableTaskAttachments.GetBool() || (len(req.Files) > 0 && formAccessIsPublic(req) && !config.FormsPublicAttachments.GetBool()) {
		return one("attachments_disabled", "Files are not accepted.")
	}
	maxFiles, maxSize := FormAttachmentLimits()
	if len(req.Files) > maxFiles {
		return one("too_many", fmt.Sprintf("At most %d files are allowed.", maxFiles))
	}
	for _, f := range req.Files {
		if f.Size > maxSize {
			return one("file_too_large", fmt.Sprintf("A file may be at most %d bytes.", maxSize))
		}
		// Windows drops trailing dots and spaces from a name, so "run.bat." is saved as "run.bat".
		if slices.Contains(formDeniedExtensions, strings.ToLower(filepath.Ext(strings.TrimRight(f.Filename, ". ")))) {
			return one("file_type_not_allowed", "This type of file is not accepted.")
		}
		mime, err := mimetype.DetectReader(f.Reader)
		_, _ = f.Reader.Seek(0, 0)
		if err != nil {
			return one("invalid", "The file cannot be read.")
		}
		for _, denied := range formDeniedMimes {
			if mime.Is(denied) {
				return one("file_type_not_allowed", "This type of file is not accepted.")
			}
		}
	}
	return nil
}

// formAccessIsPublic is set on the request by submitForm's callers through the form; validateFormFiles has no
// form at hand, so the flag travels with the request.
func formAccessIsPublic(req *FormSubmitRequest) bool {
	return req.publicForm
}

func createFormTask(s *xorm.Session, form *Form, req *FormSubmitRequest, owner *user.User, ans *formAnswers, name, email string) (*Task, error) {
	desc := ans.description
	if name != "" || email != "" {
		who := html.EscapeString(name)
		if email != "" {
			if who != "" {
				who += " "
			}
			who += "&lt;" + html.EscapeString(email) + "&gt;"
		}
		desc += fmt.Sprintf("<p><strong>%s</strong> %s</p>", html.EscapeString(i18n.T(owner.Language, "forms.submitted_by")), who)
	}
	task := &Task{
		Title:       ans.title,
		Description: desc,
		ProjectID:   form.ProjectID,
		DueDate:     ans.due,
		Priority:    ans.priority,
	}
	can, err := task.CanCreate(s, owner)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, ErrGenericForbidden{}
	}
	if err = task.Create(s, owner); err != nil {
		return nil, err
	}

	for _, a := range ans.cfs {
		v := &CustomFieldValue{TaskID: task.ID, CustomFieldID: a.id, Value: a.value}
		if err = v.Update(s, owner); err != nil {
			return nil, err
		}
	}

	labels := []*Label{}
	for _, id := range ans.labels {
		ok, err := formLabelUsable(s, owner, id)
		if err != nil {
			return nil, err
		}
		if ok {
			labels = append(labels, &Label{ID: id})
		}
	}
	if len(labels) > 0 {
		if err = task.UpdateTaskLabels(s, owner, labels); err != nil {
			return nil, err
		}
	}

	project, err := GetProjectSimpleByID(s, form.ProjectID)
	if err != nil {
		return nil, err
	}
	for _, id := range form.Settings.DefaultAssigneeIDs {
		ok, err := userCanReadProject(s, id, form.ProjectID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if err = task.addNewAssigneeByID(s, id, project, owner); err != nil {
			return nil, err
		}
	}

	if form.Settings.InitialStatusID != 0 {
		d, err := loadWorkflowDef(s, form.ProjectID)
		if err != nil {
			return nil, err
		}
		if d != nil {
			if st := d.statuses[form.Settings.InitialStatusID]; st != nil && !st.Archived {
				if _, err = d.runStatusChange(s, owner, &statusChange{Task: task, ToStatusID: st.ID, SkipChecks: true}); err != nil {
					return nil, err
				}
			}
		}
	}

	for _, f := range req.Files {
		ta := &TaskAttachment{TaskID: task.ID}
		if err = ta.NewAttachment(s, f.Reader, f.Filename, f.Size, owner); err != nil {
			return nil, err
		}
		if ta.File != nil {
			req.uploadedFileIDs = append(req.uploadedFileIDs, ta.File.ID)
		}
	}
	return task, nil
}
