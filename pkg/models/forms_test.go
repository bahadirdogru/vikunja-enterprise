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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/notifications"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// Fixture topology: user1 owns project 1 (custom fields 1 text, 2 select with options 1 and 2, 3 number 0..1000,
// 4 multi_select with options 3 and 4) and administers project 11 (workflow with statuses 1-5). User 2 has no access
// to project 1. Labels 1 and 2 belong to user1, label 3 to user2.
var (
	fU1 = &user.User{ID: 1}
	fU2 = &user.User{ID: 2}
	fU3 = &user.User{ID: 3}
)

func formsReset(t *testing.T) {
	t.Helper()
	db.LoadAndAssertFixtures(t)
	ResetAutomationCache()
	config.FormsEnabled.Set(true)
	config.FormsPublicEnabled.Set(true)
	config.FormsPublicAttachments.Set(false)
	config.FormsPublicRateLimit.Set(10)
	config.FormsPublicFormRateLimit.Set(300)
	config.FormsMaxAttachments.Set(5)
	config.FormsMaxAttachmentSize.Set("10MB")
	config.ServiceEnableTaskAttachments.Set(true)
	formClock = time.Now
	t.Cleanup(func() { formClock = time.Now })
	events.ClearDispatchedEvents()
}

func fld(target, label string, required bool) *FormField {
	return &FormField{Target: target, Label: label, Required: required}
}

func hiddenFld(target string, def any) *FormField {
	return &FormField{Target: target, Hidden: true, Default: def}
}

func baseForm(access FormAccess, fields ...*FormField) *Form {
	if len(fields) == 0 {
		fields = []*FormField{fld("title", "Title", true)}
	}
	return &Form{ProjectID: 1, Title: "Request", Access: access, Enabled: true, Fields: fields, Settings: &FormSettings{}}
}

func tryCreateForm(t *testing.T, f *Form, a web.Auth) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	if err := f.Create(s, a); err != nil {
		_ = s.Rollback()
		return err
	}
	return s.Commit()
}

func createForm(t *testing.T, f *Form) *Form {
	t.Helper()
	require.NoError(t, tryCreateForm(t, f, fU1))
	return f
}

func issuePaths(t *testing.T, err error) []string {
	t.Helper()
	var inv ErrFormInvalid
	require.ErrorAs(t, err, &inv, "expected ErrFormInvalid, got %v", err)
	paths := []string{}
	for _, i := range inv.Issues {
		paths = append(paths, i.Path)
	}
	return paths
}

func submit(t *testing.T, f *Form, values map[string]any, mutate ...func(*FormSubmitRequest)) (*FormSubmitResult, error) {
	t.Helper()
	req := &FormSubmitRequest{Token: f.Token, IP: "203.0.113.7", Values: values}
	for _, m := range mutate {
		m(req)
	}
	return SubmitForm(context.Background(), req)
}

func asAuth(a web.Auth) func(*FormSubmitRequest) {
	return func(r *FormSubmitRequest) { r.Auth = a }
}

func fromIP(ip string) func(*FormSubmitRequest) {
	return func(r *FormSubmitRequest) { r.IP = ip }
}

func lastTask(t *testing.T) *Task {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	task := &Task{}
	has, err := s.Where("project_id = 1").OrderBy("id DESC").Get(task)
	require.NoError(t, err)
	require.True(t, has)
	return task
}

func fieldErrors(t *testing.T, err error) map[string]string {
	t.Helper()
	var inv ErrFormSubmissionInvalid
	require.ErrorAs(t, err, &inv, "expected ErrFormSubmissionInvalid, got %v", err)
	out := map[string]string{}
	for _, e := range inv.Errors {
		out[e.Field] = e.Code
	}
	return out
}

func countRows(t *testing.T, bean any) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Count(bean)
	require.NoError(t, err)
	return n
}

func reloadForm(t *testing.T, id int64) *Form {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	f := &Form{}
	has, err := s.ID(id).Get(f)
	require.NoError(t, err)
	require.True(t, has)
	normalizeForm(f)
	return f
}

func TestForm_CreateValidation(t *testing.T) {
	formsReset(t)
	cases := []struct {
		name   string
		form   func() *Form
		expect string
	}{
		{"unknown target", func() *Form { return baseForm(FormAccessMembers, fld("title", "T", true), fld("nonsense", "x", false)) }, "fields[1].target"},
		{"custom field of another project", func() *Form { return baseForm(FormAccessMembers, fld("title", "T", true), fld("cf_5", "x", false)) }, "fields[1].target"},
		{"malformed custom field target", func() *Form { return baseForm(FormAccessMembers, fld("title", "T", true), fld("cf_x", "x", false)) }, "fields[1].target"},
		{"duplicate target", func() *Form { return baseForm(FormAccessMembers, fld("title", "T", true), fld("title", "T2", false)) }, "fields[1].target"},
		{"title neither asked nor defaulted", func() *Form { return baseForm(FormAccessMembers, fld("description", "D", false)) }, "fields"},
		{"hidden title without default", func() *Form { return baseForm(FormAccessMembers, hiddenFld("title", nil)) }, "fields[0].default"},
		{"optional title without default", func() *Form { return baseForm(FormAccessMembers, fld("title", "T", false)) }, "fields[0].default"},
		{"required hidden without default", func() *Form {
			f := hiddenFld("cf_1", nil)
			f.Required = true
			return baseForm(FormAccessMembers, fld("title", "T", true), f)
		}, "fields[1].default"},
		{"shown field without label", func() *Form { return baseForm(FormAccessMembers, fld("title", "", true)) }, "fields[0].label"},
		{"bad text default", func() *Form {
			return baseForm(FormAccessMembers, fld("title", "T", true), &FormField{Target: "cf_1", Label: "x", Default: 5})
		}, "fields[1].default"},
		{"number default above max", func() *Form {
			return baseForm(FormAccessMembers, fld("title", "T", true), &FormField{Target: "cf_3", Label: "x", Default: 5000})
		}, "fields[1].default"},
		{"option of another field as default", func() *Form {
			return baseForm(FormAccessMembers, fld("title", "T", true), &FormField{Target: "cf_2", Label: "x", Default: 3})
		}, "fields[1].default"},
		{"bad priority default", func() *Form {
			return baseForm(FormAccessMembers, fld("title", "T", true), &FormField{Target: "priority", Label: "x", Default: 9})
		}, "fields[1].default"},
		{"bad due date default", func() *Form {
			return baseForm(FormAccessMembers, fld("title", "T", true), &FormField{Target: "due_date", Label: "x", Default: "tomorrow"})
		}, "fields[1].default"},
		{"unknown placeholder", func() *Form {
			return baseForm(FormAccessMembers, &FormField{Target: "title", Label: "T", Required: true, Default: "{{evil}}"})
		}, "fields[0].default"},
		{"label of another user as hidden default", func() *Form {
			return baseForm(FormAccessMembers, fld("title", "T", true), hiddenFld("labels", []any{3}))
		}, "fields[1].default[0]"},
		{"missing label as option", func() *Form {
			f := fld("labels", "L", false)
			f.Options = []int64{9999}
			return baseForm(FormAccessMembers, fld("title", "T", true), f)
		}, "fields[1].options[0]"},
		{"shown labels without options", func() *Form { return baseForm(FormAccessMembers, fld("title", "T", true), fld("labels", "L", false)) }, "fields[1].options"},
		{"options on another target", func() *Form {
			f := fld("description", "D", false)
			f.Options = []int64{1}
			return baseForm(FormAccessMembers, fld("title", "T", true), f)
		}, "fields[1].options"},
		{"hidden attachments", func() *Form {
			f := fld("attachments", "A", false)
			f.Hidden = true
			return baseForm(FormAccessMembers, fld("title", "T", true), f)
		}, "fields[1].hidden"},
		{"initial status of another workflow", func() *Form {
			f := baseForm(FormAccessMembers)
			f.Settings.InitialStatusID = 1
			return f
		}, "settings.initial_status_id"},
		{"assignee without access", func() *Form {
			f := baseForm(FormAccessMembers)
			f.Settings.DefaultAssigneeIDs = []int64{2}
			return f
		}, "settings.default_assignee_ids[0]"},
		{"notify user without access", func() *Form {
			f := baseForm(FormAccessMembers)
			f.Settings.NotifyUserIDs = []int64{2}
			return f
		}, "settings.notify_user_ids[0]"},
		{"redirect not http", func() *Form {
			f := baseForm(FormAccessMembers)
			f.Settings.RedirectURL = "javascript:alert(1)"
			return f
		}, "settings.redirect_url"},
		{"negative maximum", func() *Form {
			f := baseForm(FormAccessMembers)
			f.Settings.MaxSubmissions = -1
			return f
		}, "settings.max_submissions"},
		{"name required without collecting", func() *Form {
			f := baseForm(FormAccessMembers)
			f.Settings.NameRequired = true
			return f
		}, "settings.name_required"},
		{"unknown access", func() *Form { f := baseForm("everyone"); return f }, "access"},
		{"empty title", func() *Form { f := baseForm(FormAccessMembers); f.Title = " "; return f }, "title"},
		{"public form asking for labels", func() *Form {
			f := fld("labels", "L", false)
			f.Options = []int64{1}
			return baseForm(FormAccessPublic, fld("title", "T", true), f)
		}, "fields[1].target"},
		{"public form asking for attachments", func() *Form {
			return baseForm(FormAccessPublic, fld("title", "T", true), fld("attachments", "A", false))
		}, "fields[1].target"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := tryCreateForm(t, c.form(), fU1)
			require.Error(t, err)
			assert.Contains(t, issuePaths(t, err), c.expect)
		})
	}

	t.Run("a user custom field cannot be asked on a public form but can be hidden", func(t *testing.T) {
		cf := &CustomField{ProjectID: 1, Title: "Owner", Type: CustomFieldTypeUser}
		s := db.NewSession()
		require.NoError(t, cf.Create(s, fU1))
		require.NoError(t, s.Commit())
		s.Close()
		target := "cf_" + fitoa(cf.ID)

		err := tryCreateForm(t, baseForm(FormAccessPublic, fld("title", "T", true), fld(target, "Who", false)), fU1)
		require.Error(t, err)
		assert.Contains(t, issuePaths(t, err), "fields[1].target")

		hidden := hiddenFld(target, []any{1})
		require.NoError(t, tryCreateForm(t, baseForm(FormAccessPublic, fld("title", "T", true), hidden), fU1))
		require.NoError(t, tryCreateForm(t, baseForm(FormAccessMembers, fld("title", "T", true), fld(target, "Who", false)), fU1))
	})

	t.Run("a valid form with every kind of default is accepted", func(t *testing.T) {
		f := baseForm(FormAccessMembers,
			&FormField{Target: "title", Label: "T", Required: true, Default: "Request from {{submitter.name}} on {{date}} for {{form.title}} {{submitter.email}}"},
			&FormField{Target: "description", Label: "D", Default: "<p>hi</p>"},
			&FormField{Target: "due_date", Label: "Due", Default: "2030-01-02"},
			&FormField{Target: "priority", Label: "P", Default: 3},
			hiddenFld("labels", []any{1, 2}),
			&FormField{Target: "cf_2", Label: "Stage", Default: 1},
			&FormField{Target: "cf_4", Label: "Colors", Default: []any{3, 4}},
			&FormField{Target: "cf_3", Label: "Budget", Default: 10.5},
		)
		f.Settings.DefaultAssigneeIDs = []int64{1}
		f.Settings.RedirectURL = "https://example.com/thanks"
		require.NoError(t, tryCreateForm(t, f, fU1))
		assert.GreaterOrEqual(t, len(f.Token), 32)
		assert.EqualValues(t, 1, f.OwnerID)
	})

	t.Run("public forms can be disabled for the instance", func(t *testing.T) {
		config.FormsPublicEnabled.Set(false)
		defer config.FormsPublicEnabled.Set(true)
		err := tryCreateForm(t, baseForm(FormAccessPublic), fU1)
		require.Error(t, err)
		assert.Contains(t, issuePaths(t, err), "access")
		require.NoError(t, tryCreateForm(t, baseForm(FormAccessUsers), fU1))
	})

	t.Run("the feature kill switch blocks saving", func(t *testing.T) {
		config.FormsEnabled.Set(false)
		defer config.FormsEnabled.Set(true)
		err := tryCreateForm(t, baseForm(FormAccessUsers), fU1)
		require.Error(t, err)
		assert.True(t, IsErrFormsDisabled(err))
	})

	t.Run("an initial status of the workflow is accepted and an archived one is not", func(t *testing.T) {
		f := baseForm(FormAccessMembers)
		f.ProjectID = 11
		f.Settings.InitialStatusID = 2
		require.NoError(t, tryCreateForm(t, f, fU1))
	})
}

func fitoa(i int64) string {
	return strconv.FormatInt(i, 10)
}

func TestForm_TokenIsUniqueAndRegenerable(t *testing.T) {
	formsReset(t)
	a := createForm(t, baseForm(FormAccessPublic))
	b := createForm(t, baseForm(FormAccessPublic))
	assert.NotEqual(t, a.Token, b.Token)
	assert.Regexp(t, `^[A-Za-z0-9_-]{32,}$`, a.Token)

	old := a.Token
	s := db.NewSession()
	tok := &FormToken{ID: a.ID, ProjectID: 1}
	can, err := tok.CanCreate(s, fU1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, tok.Create(s, fU1))
	require.NoError(t, s.Commit())
	s.Close()
	assert.NotEqual(t, old, tok.Token)

	_, err = submit(t, &Form{Token: old}, map[string]any{"title": "x"})
	assert.True(t, IsErrFormDoesNotExist(err), "the old link stops working: %v", err)
	_, err = GetFormDefinition(old, nil)
	assert.True(t, IsErrFormDoesNotExist(err))
	_, err = GetFormDefinition(tok.Token, nil)
	require.NoError(t, err)
	// The form keeps its settings across a regeneration.
	assert.Equal(t, tok.Token, reloadForm(t, a.ID).Token)
}

func TestForm_AdminPermissions(t *testing.T) {
	formsReset(t)
	form := createForm(t, baseForm(FormAccessMembers))

	s := db.NewSession()
	defer s.Close()
	for _, p := range []*ProjectUser{
		{UserID: 2, ProjectID: 1, Permission: PermissionRead},
		{UserID: 3, ProjectID: 1, Permission: PermissionWrite},
		{UserID: 4, ProjectID: 1, Permission: PermissionAdmin},
	} {
		_, err := s.Insert(p)
		require.NoError(t, err)
	}
	require.NoError(t, s.Commit())

	roles := []struct {
		name  string
		auth  web.Auth
		admin bool
	}{
		{"owner", fU1, true},
		{"admin", &user.User{ID: 4}, true},
		{"write", fU3, false},
		{"read", fU2, false},
		{"stranger", &user.User{ID: 5}, false},
		{"link share", &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionAdmin}, false},
	}
	for _, r := range roles {
		t.Run(r.name, func(t *testing.T) {
			s := db.NewSession()
			defer s.Close()
			f := &Form{ID: form.ID, ProjectID: 1}
			can, _, err := f.CanRead(s, r.auth)
			require.NoError(t, err)
			assert.Equal(t, r.admin, can, "read")
			can, err = (&Form{ProjectID: 1}).CanCreate(s, r.auth)
			require.NoError(t, err)
			assert.Equal(t, r.admin, can, "create")
			can, err = (&Form{ID: form.ID, ProjectID: 1}).CanUpdate(s, r.auth)
			require.NoError(t, err)
			assert.Equal(t, r.admin, can, "update")
			can, err = (&Form{ID: form.ID, ProjectID: 1}).CanDelete(s, r.auth)
			require.NoError(t, err)
			assert.Equal(t, r.admin, can, "delete")
			can, err = (&FormToken{ID: form.ID, ProjectID: 1}).CanCreate(s, r.auth)
			require.NoError(t, err)
			assert.Equal(t, r.admin, can, "token")
			can, _, err = (&FormSubmission{FormID: form.ID, ProjectID: 1}).CanRead(s, r.auth)
			require.NoError(t, err)
			assert.Equal(t, r.admin, can, "submissions")
			can, _, err = (&FormOptions{ProjectID: 1}).CanRead(s, r.auth)
			require.NoError(t, err)
			assert.Equal(t, r.admin, can, "options")
			_, _, _, err = (&Form{ProjectID: 1}).ReadAll(s, r.auth, "", 1, 10)
			assert.Equal(t, !r.admin, err != nil, "list")
		})
	}

	t.Run("a form of another project is not found through this one", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		_, err := (&Form{ID: form.ID, ProjectID: 11}).CanUpdate(s, fU1)
		require.Error(t, err)
		assert.True(t, IsErrFormDoesNotExist(err))
	})
}

func TestForm_UpdateKeepsTokenAndClearsReason(t *testing.T) {
	formsReset(t)
	form := createForm(t, baseForm(FormAccessMembers))
	token := form.Token
	s := db.NewSession()
	_, err := s.ID(form.ID).Cols("enabled", "disabled_reason").Update(&Form{Enabled: false, DisabledReason: FormDisabledOwnerNotWriter})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	upd := &Form{ID: form.ID, ProjectID: 1, Title: "Renamed", Access: FormAccessUsers, Enabled: true, Fields: form.Fields, Settings: &FormSettings{MaxSubmissions: 3}, Token: "ignored", OwnerID: 99}
	s = db.NewSession()
	require.NoError(t, upd.Update(s, fU1))
	require.NoError(t, s.Commit())
	s.Close()
	assert.Equal(t, token, upd.Token)
	assert.Empty(t, upd.DisabledReason)
	assert.EqualValues(t, 1, upd.OwnerID)
	stored := reloadForm(t, form.ID)
	assert.Equal(t, "Renamed", stored.Title)
	assert.EqualValues(t, 3, stored.MaxSubmissions)
	assert.Equal(t, token, stored.Token)
}

func TestFormOptions(t *testing.T) {
	formsReset(t)
	s := db.NewSession()
	defer s.Close()
	o := &FormOptions{ProjectID: 1}
	require.NoError(t, o.ReadOne(s, fU1))
	byTarget := map[string]*FormTargetInfo{}
	for _, tg := range o.Targets {
		byTarget[tg.Target] = tg
	}
	for _, name := range []string{"title", "description", "due_date", "priority", "labels", "attachments", "cf_1", "cf_2", "cf_3", "cf_4"} {
		require.Contains(t, byTarget, name)
	}
	assert.False(t, byTarget["labels"].PublicVisible)
	assert.True(t, byTarget["labels"].PublicHidden)
	assert.False(t, byTarget["attachments"].PublicVisible, "off by default on public forms")
	assert.True(t, byTarget["title"].PublicVisible)
	assert.Len(t, byTarget["cf_2"].Options, 2)
	require.NotNil(t, byTarget["cf_3"].Config)
	assert.Equal(t, "number", byTarget["cf_3"].Kind)
	config.FormsPublicAttachments.Set(true)
	defer config.FormsPublicAttachments.Set(false)
	require.NoError(t, o.ReadOne(s, fU1))
	for _, tg := range o.Targets {
		if tg.Target == "attachments" {
			assert.True(t, tg.PublicVisible)
		}
	}
}

func marshalKeys(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func keysOf(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFormDefinition_DoesNotLeak(t *testing.T) {
	formsReset(t)
	f := baseForm(FormAccessPublic,
		fld("title", "What do you need?", true),
		&FormField{Target: "cf_2", Label: "Stage", Help: "pick one", Required: true},
		&FormField{Target: "cf_3", Label: "Budget"},
		hiddenFld("labels", []any{1}),
		hiddenFld("cf_1", "secret internal reference"),
	)
	f.Description = `<p>Hello</p><script>alert(1)</script><img src=x onerror=alert(1)>`
	f.Settings = &FormSettings{
		SuccessMessage: "Thanks", RedirectURL: "https://example.com/done", SubmitAnother: true,
		MaxSubmissions: 5, NotifyUserIDs: []int64{1}, DefaultAssigneeIDs: []int64{1}, CollectEmail: true,
	}
	createForm(t, f)

	def, err := GetFormDefinition(f.Token, nil)
	require.NoError(t, err)
	top := marshalKeys(t, def)
	assert.ElementsMatch(t, []string{"title", "description", "fields", "success", "submitter", "branding"}, keysOf(top))
	assert.ElementsMatch(t, []string{"message", "redirect_url", "submit_another"}, keysOf(top["success"].(map[string]any)))
	assert.ElementsMatch(t, []string{"collect_name", "name_required", "collect_email", "email_required"}, keysOf(top["submitter"].(map[string]any)))

	fields := top["fields"].([]any)
	require.Len(t, fields, 3, "hidden fields are not part of it")
	title := fields[0].(map[string]any)
	assert.ElementsMatch(t, []string{"target", "label", "help", "required", "kind", "max_length"}, keysOf(title))
	stage := fields[1].(map[string]any)
	assert.ElementsMatch(t, []string{"target", "label", "help", "required", "kind", "options"}, keysOf(stage))
	for _, o := range stage["options"].([]any) {
		assert.ElementsMatch(t, []string{"id", "title", "color"}, keysOf(o.(map[string]any)))
	}
	budget := fields[2].(map[string]any)
	assert.ElementsMatch(t, []string{"target", "label", "help", "required", "kind", "config"}, keysOf(budget))

	raw, err := json.Marshal(def)
	require.NoError(t, err)
	body := string(raw)
	for _, forbidden := range []string{"secret internal reference", "project", "owner", "submission_count", "token", "notify", "assignee", "<script", "onerror", "cf_1", `"labels"`} {
		assert.NotContains(t, body, forbidden)
	}
	assert.Equal(t, "<p>Hello</p>", def.Description)
}

func TestFormDefinition_ReasonsAndAccess(t *testing.T) {
	formsReset(t)
	t.Run("unknown token is not found", func(t *testing.T) {
		_, err := GetFormDefinition("does-not-exist", nil)
		assert.True(t, IsErrFormDoesNotExist(err))
	})
	t.Run("disabled, closed and full say why and give only the title", func(t *testing.T) {
		f := createForm(t, baseForm(FormAccessPublic))
		check := func(reason string) {
			_, err := GetFormDefinition(f.Token, nil)
			var un ErrFormUnavailable
			require.ErrorAs(t, err, &un)
			assert.Equal(t, reason, un.Reason)
			assert.Equal(t, "Request", un.Title)
			assert.Equal(t, 410, un.HTTPError().HTTPCode)
		}
		s := db.NewSession()
		_, err := s.ID(f.ID).Cols("enabled").Update(&Form{Enabled: false})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		check(FormUnavailableDisabled)

		s = db.NewSession()
		_, err = s.ID(f.ID).Cols("enabled").Update(&Form{Enabled: true})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		closed := time.Now().Add(-time.Minute)
		upd := reloadForm(t, f.ID)
		upd.Settings.CloseDate = &closed
		s = db.NewSession()
		_, err = s.ID(f.ID).Cols("settings").Update(upd)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		check(FormUnavailableClosed)

		upd.Settings.CloseDate = nil
		upd.MaxSubmissions = 1
		upd.SubmissionCount = 1
		s = db.NewSession()
		_, err = s.ID(f.ID).Cols("settings", "max_submissions", "submission_count").Update(upd)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		check(FormUnavailableFull)
	})
	t.Run("the instance kill switches", func(t *testing.T) {
		pub := createForm(t, baseForm(FormAccessPublic))
		usr := createForm(t, baseForm(FormAccessUsers))
		config.FormsPublicEnabled.Set(false)
		_, err := GetFormDefinition(pub.Token, nil)
		assert.True(t, IsErrFormUnavailable(err))
		_, err = submit(t, pub, map[string]any{"title": "x"})
		assert.True(t, IsErrFormUnavailable(err))
		_, err = GetFormDefinition(usr.Token, fU2)
		require.NoError(t, err, "forms for signed-in users stay")
		config.FormsPublicEnabled.Set(true)
		config.FormsEnabled.Set(false)
		_, err = GetFormDefinition(usr.Token, fU2)
		assert.True(t, IsErrFormUnavailable(err))
		config.FormsEnabled.Set(true)
	})
}

func TestForm_AccessModes(t *testing.T) {
	formsReset(t)
	pub := createForm(t, baseForm(FormAccessPublic))
	usr := createForm(t, baseForm(FormAccessUsers))
	mem := createForm(t, baseForm(FormAccessMembers))
	s := db.NewSession()
	_, err := s.Insert(&ProjectUser{UserID: 3, ProjectID: 1, Permission: PermissionRead})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	share := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionWrite}

	type outcome func(t *testing.T, err error)
	ok := func(t *testing.T, err error) { assert.NoError(t, err) }
	login := func(t *testing.T, err error) { assert.True(t, IsErrFormLoginRequired(err), "%v", err) }
	forbidden := func(t *testing.T, err error) { assert.True(t, IsErrFormNotAllowed(err), "%v", err) }

	cases := []struct {
		name string
		form *Form
		auth web.Auth
		want outcome
	}{
		{"public anonymous", pub, nil, ok},
		{"public user", pub, fU2, ok},
		{"public link share", pub, share, ok},
		{"users anonymous", usr, nil, login},
		{"users user", usr, fU2, ok},
		{"users link share", usr, share, forbidden},
		{"members anonymous", mem, nil, login},
		{"members stranger", mem, fU2, forbidden},
		{"members member", mem, fU3, ok},
		{"members owner", mem, fU1, ok},
		{"members link share", mem, share, forbidden},
	}
	for _, c := range cases {
		t.Run(c.name+" definition", func(t *testing.T) {
			_, err := GetFormDefinition(c.form.Token, c.auth)
			c.want(t, err)
		})
		t.Run(c.name+" submission", func(t *testing.T) {
			before := countRows(t, &Task{})
			_, err := submit(t, c.form, map[string]any{"title": "x"}, asAuth(c.auth))
			c.want(t, err)
			if err != nil {
				assert.Equal(t, before, countRows(t, &Task{}), "nothing is created")
			}
		})
	}

	t.Run("link shares never submit as a user", func(t *testing.T) {
		_, err := submit(t, pub, map[string]any{"title": "from a share"}, asAuth(share))
		require.NoError(t, err)
		s := db.NewSession()
		defer s.Close()
		sub := &FormSubmission{}
		_, err = s.Where("form_id = ?", pub.ID).OrderBy("id DESC").Get(sub)
		require.NoError(t, err)
		assert.Nil(t, sub.SubmitterID)
	})
}

func TestFormSubmit_HappyPathAllTargets(t *testing.T) {
	formsReset(t)
	var ids = map[CustomFieldType]int64{}
	s := db.NewSession()
	for _, typ := range []CustomFieldType{CustomFieldTypeLongText, CustomFieldTypeCurrency, CustomFieldTypeDate, CustomFieldTypeCheckbox, CustomFieldTypeUser,
		CustomFieldTypeURL, CustomFieldTypeEmail, CustomFieldTypeRating, CustomFieldTypeProgress} {
		cf := &CustomField{ProjectID: 1, Title: string(typ), Type: typ}
		require.NoError(t, cf.Create(s, fU1))
		ids[typ] = cf.ID
	}
	require.NoError(t, s.Commit())
	s.Close()
	target := func(typ CustomFieldType) string { return "cf_" + fitoa(ids[typ]) }

	labels := fld("labels", "Labels", false)
	labels.Options = []int64{1, 2}
	f := baseForm(FormAccessMembers,
		fld("title", "Title", true),
		fld("description", "Description", false),
		fld("due_date", "Due", false),
		fld("priority", "Priority", false),
		labels,
		fld("cf_1", "Text", false),
		fld("cf_2", "Select", false),
		fld("cf_3", "Number", false),
		fld("cf_4", "Multi", false),
		fld(target(CustomFieldTypeLongText), "Long", false),
		fld(target(CustomFieldTypeCurrency), "Money", false),
		fld(target(CustomFieldTypeDate), "Date", false),
		fld(target(CustomFieldTypeCheckbox), "Check", false),
		fld(target(CustomFieldTypeUser), "User", false),
		fld(target(CustomFieldTypeURL), "URL", false),
		fld(target(CustomFieldTypeEmail), "Mail", false),
		fld(target(CustomFieldTypeRating), "Rating", false),
		fld(target(CustomFieldTypeProgress), "Progress", false),
	)
	createForm(t, f)

	res, err := submit(t, f, map[string]any{
		"title": "Laptop", "description": "Needs <b>more</b> RAM", "due_date": "2030-05-06", "priority": 4,
		"labels": []any{1, 2}, "cf_1": "REF-1", "cf_2": 1, "cf_3": 12.5, "cf_4": []any{3, 4},
		target(CustomFieldTypeLongText): "line one\nline two", target(CustomFieldTypeCurrency): 99.9,
		target(CustomFieldTypeDate): "2030-01-01T10:00:00Z", target(CustomFieldTypeCheckbox): true,
		target(CustomFieldTypeUser): []any{1}, target(CustomFieldTypeURL): "https://example.com/a",
		target(CustomFieldTypeEmail): "a@example.com", target(CustomFieldTypeRating): 4, target(CustomFieldTypeProgress): 60,
	}, asAuth(fU1))
	require.NoError(t, err)
	require.NotNil(t, res.TaskID, "a signed-in submitter who can read the project gets the task id")

	task := lastTask(t)
	assert.Equal(t, *res.TaskID, task.ID)
	assert.Equal(t, "Laptop", task.Title)
	assert.EqualValues(t, 4, task.Priority)
	assert.Equal(t, "2030-05-06", task.DueDate.UTC().Format("2006-01-02"))
	assert.Contains(t, task.Description, "Needs <b>more</b> RAM")
	assert.EqualValues(t, 1, task.CreatedByID)

	s = db.NewSession()
	defer s.Close()
	values, err := GetCustomFieldValuesOfTask(s, task)
	require.NoError(t, err)
	got := map[int64]any{}
	for _, v := range values {
		got[v.CustomFieldID] = v.Value
	}
	assert.Equal(t, "REF-1", got[1])
	assert.EqualValues(t, 1, got[2])
	assert.InDelta(t, 12.5, got[3], 0)
	assert.ElementsMatch(t, []int64{3, 4}, got[4])
	assert.Equal(t, "line one\nline two", got[ids[CustomFieldTypeLongText]])
	assert.InDelta(t, 99.9, got[ids[CustomFieldTypeCurrency]], 0)
	assert.Equal(t, true, got[ids[CustomFieldTypeCheckbox]])
	assert.Equal(t, []int64{1}, got[ids[CustomFieldTypeUser]])
	assert.Equal(t, "https://example.com/a", got[ids[CustomFieldTypeURL]])
	assert.Equal(t, "a@example.com", got[ids[CustomFieldTypeEmail]])
	assert.EqualValues(t, 4, got[ids[CustomFieldTypeRating]])
	assert.EqualValues(t, 60, got[ids[CustomFieldTypeProgress]])
	assert.NotNil(t, got[ids[CustomFieldTypeDate]])

	labelRows := []*LabelTask{}
	require.NoError(t, s.Where("task_id = ?", task.ID).Find(&labelRows))
	assert.Len(t, labelRows, 2)

	assert.EqualValues(t, 1, reloadForm(t, f.ID).SubmissionCount)
	assert.EqualValues(t, 1, countRows(t, &FormSubmission{}))
	assert.Equal(t, 1, events.CountDispatchedEvents("form.submitted"))
	assert.GreaterOrEqual(t, events.CountDispatchedEvents("task.created"), 1)
}

func TestFormSubmit_PublicNeverLeaksTheTask(t *testing.T) {
	formsReset(t)
	f := baseForm(FormAccessPublic)
	f.Settings = &FormSettings{SuccessMessage: "Thanks", RedirectURL: "https://example.com/x", SubmitAnother: true}
	createForm(t, f)
	res, err := submit(t, f, map[string]any{"title": "hi"})
	require.NoError(t, err)
	assert.Nil(t, res.TaskID)
	assert.Equal(t, "Thanks", res.Message)
	assert.Equal(t, "https://example.com/x", res.RedirectURL)
	assert.True(t, res.SubmitAnother)
	assert.ElementsMatch(t, []string{"message", "redirect_url", "submit_another"}, keysOf(marshalKeys(t, res)))

	// Signed-in submitters who cannot read the project get no id either.
	usr := createForm(t, baseForm(FormAccessUsers))
	res, err = submit(t, usr, map[string]any{"title": "hi"}, asAuth(fU2))
	require.NoError(t, err)
	assert.Nil(t, res.TaskID)
}

func TestFormSubmit_Validation(t *testing.T) {
	formsReset(t)
	f := createForm(t, baseForm(FormAccessPublic,
		fld("title", "Title", true),
		fld("cf_1", "Text", false),
		&FormField{Target: "cf_2", Label: "Stage", Required: true},
		fld("cf_3", "Budget", false),
		hiddenFld("cf_4", []any{3}),
		fld("priority", "P", false),
		fld("due_date", "Due", false),
	))
	tasksBefore := countRows(t, &Task{})

	cases := []struct {
		name   string
		values map[string]any
		expect map[string]string
	}{
		{"required fields", map[string]any{}, map[string]string{"title": "required", "cf_2": "required"}},
		{"blank title", map[string]any{"title": "   ", "cf_2": 1}, map[string]string{"title": "required"}},
		{"wrong type", map[string]any{"title": "x", "cf_2": "one"}, map[string]string{"cf_2": "invalid"}},
		{"option of another field", map[string]any{"title": "x", "cf_2": 3}, map[string]string{"cf_2": "invalid"}},
		{"number above max", map[string]any{"title": "x", "cf_2": 1, "cf_3": 5000}, map[string]string{"cf_3": "invalid"}},
		{"unknown key", map[string]any{"title": "x", "cf_2": 1, "bogus": 1}, map[string]string{"bogus": "unknown_field"}},
		{"a key of another project", map[string]any{"title": "x", "cf_2": 1, "cf_5": "x"}, map[string]string{"cf_5": "unknown_field"}},
		{"hidden target supplied", map[string]any{"title": "x", "cf_2": 1, "cf_4": []any{4}}, map[string]string{"cf_4": "unknown_field"}},
		{"title too long", map[string]any{"title": strings.Repeat("a", 251), "cf_2": 1}, map[string]string{"title": "too_long"}},
		{"text too long", map[string]any{"title": "x", "cf_2": 1, "cf_1": strings.Repeat("a", 10001)}, map[string]string{"cf_1": "too_long"}},
		{"bad priority", map[string]any{"title": "x", "cf_2": 1, "priority": 12}, map[string]string{"priority": "invalid"}},
		{"bad date", map[string]any{"title": "x", "cf_2": 1, "due_date": "soon"}, map[string]string{"due_date": "invalid"}},
		{"several at once", map[string]any{"title": "", "cf_2": "no", "nope": 1}, map[string]string{"title": "required", "cf_2": "invalid", "nope": "unknown_field"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := submit(t, f, c.values)
			require.Error(t, err)
			assert.Equal(t, c.expect, fieldErrors(t, err))
			var inv ErrFormSubmissionInvalid
			require.ErrorAs(t, err, &inv)
			assert.Equal(t, 422, inv.HTTPError().HTTPCode)
		})
	}
	assert.Equal(t, tasksBefore, countRows(t, &Task{}), "nothing was created")
	assert.EqualValues(t, 0, reloadForm(t, f.ID).SubmissionCount, "a rejected submission does not count")

	t.Run("hidden defaults apply and an optional empty answer is skipped", func(t *testing.T) {
		_, err := submit(t, f, map[string]any{"title": "ok", "cf_2": 2, "cf_1": ""})
		require.NoError(t, err)
		task := lastTask(t)
		s := db.NewSession()
		defer s.Close()
		values, err := GetCustomFieldValuesOfTask(s, task)
		require.NoError(t, err)
		got := map[int64]any{}
		for _, v := range values {
			got[v.CustomFieldID] = v.Value
		}
		assert.ElementsMatch(t, []int64{3}, got[4], "the hidden default")
		assert.Nil(t, got[1])
	})
}

func TestFormSubmit_TitleTemplateAndSubmitterBlock(t *testing.T) {
	formsReset(t)
	f := baseForm(FormAccessPublic, &FormField{Target: "title", Label: "T", Hidden: true, Required: true, Default: "Request from {{submitter.name}} <{{submitter.email}}> via {{form.title}} on {{date}}"})
	f.Settings = &FormSettings{CollectName: true, NameRequired: true, CollectEmail: true}
	createForm(t, f)
	formClock = func() time.Time { return time.Date(2031, 2, 3, 10, 0, 0, 0, time.UTC) }

	_, err := submit(t, f, nil)
	assert.Equal(t, map[string]string{"submitter.name": "required"}, fieldErrors(t, err))
	_, err = submit(t, f, nil, func(r *FormSubmitRequest) { r.SubmitterName = "Ada"; r.SubmitterEmail = "not an address" })
	assert.Equal(t, map[string]string{"submitter.email": "invalid"}, fieldErrors(t, err))

	_, err = submit(t, f, nil, func(r *FormSubmitRequest) {
		r.SubmitterName = `Ada <script>alert(1)</script>`
		r.SubmitterEmail = "ada@example.com"
	})
	require.NoError(t, err)
	task := lastTask(t)
	assert.Equal(t, "Request from Ada <script>alert(1)</script> <ada@example.com> via Request on 2031-02-03", task.Title, "the title is plain text")
	assert.NotContains(t, task.Description, "<script")
	assert.Contains(t, task.Description, "Submitted by")
	assert.Contains(t, task.Description, "&lt;ada@example.com&gt;")
	assert.Contains(t, task.Description, "&lt;script&gt;")

	s := db.NewSession()
	defer s.Close()
	sub := &FormSubmission{}
	_, err = s.Where("task_id = ?", task.ID).Get(sub)
	require.NoError(t, err)
	assert.Equal(t, "ada@example.com", sub.SubmitterEmail)
	assert.Contains(t, sub.SubmitterName, "Ada")
	assert.Nil(t, sub.SubmitterID)
}

func TestFormSubmit_UntrustedTextIsNeutralised(t *testing.T) {
	formsReset(t)
	f := createForm(t, baseForm(FormAccessPublic,
		fld("title", "T", true), fld("description", "D", false), fld("cf_1", "Text", false)))

	payload := `<p onclick="x()">hi</p><script>alert(1)</script><a href="javascript:alert(1)">bad</a><a href="https://ok.example/">ok</a>` +
		`<img src="x" onerror="alert(1)"><iframe src="https://evil"></iframe><style>*{}</style><svg onload=alert(1)>`
	_, err := submit(t, f, map[string]any{"title": "x", "description": payload, "cf_1": "javascript:alert(1)<script>x</script>\x00"})
	require.NoError(t, err)
	task := lastTask(t)
	for _, bad := range []string{"<script", "onclick", "onerror", "javascript:", "<img", "<iframe", "<style", "<svg", "onload"} {
		assert.NotContains(t, strings.ToLower(task.Description), bad)
	}
	assert.Contains(t, task.Description, "https://ok.example/")
	assert.Contains(t, task.Description, "<p>hi</p>")

	s := db.NewSession()
	values, err := GetCustomFieldValuesOfTask(s, task)
	s.Close()
	require.NoError(t, err)
	for _, v := range values {
		if v.CustomFieldID == 1 {
			assert.Equal(t, "javascript:alert(1)<script>x</script>", v.Value, "plain text stays plain text, control characters are gone")
		}
	}

	t.Run("plain text is escaped, not sanitized away", func(t *testing.T) {
		_, err := submit(t, f, map[string]any{"title": "x", "description": "1 < 2 & 3 > 2\n\nsecond"})
		require.NoError(t, err)
		d := lastTask(t).Description
		assert.Equal(t, "<p>1 &lt; 2 &amp; 3 &gt; 2</p><p>second</p>", d)
	})

	t.Run("a url field rejects script urls", func(t *testing.T) {
		cf := &CustomField{ProjectID: 1, Title: "U", Type: CustomFieldTypeURL}
		s := db.NewSession()
		require.NoError(t, cf.Create(s, fU1))
		require.NoError(t, s.Commit())
		s.Close()
		target := "cf_" + fitoa(cf.ID)
		g := createForm(t, baseForm(FormAccessPublic, fld("title", "T", true), fld(target, "U", false)))
		_, err := submit(t, g, map[string]any{"title": "x", target: "javascript:alert(1)"})
		assert.Equal(t, map[string]string{target: "invalid"}, fieldErrors(t, err))
	})

	t.Run("the description of the form is sanitized when saved", func(t *testing.T) {
		g := baseForm(FormAccessPublic)
		g.Description = `<p>x</p><script>1</script><a href="javascript:1">y</a>`
		createForm(t, g)
		assert.Equal(t, "<p>x</p>y", g.Description)
	})
}

func TestFormSubmit_AsOwnerWithStatusAndAssignees(t *testing.T) {
	formsReset(t)
	f := baseForm(FormAccessPublic)
	f.ProjectID = 11
	f.Settings = &FormSettings{InitialStatusID: 2, DefaultAssigneeIDs: []int64{1}}
	createForm(t, f)
	_, err := submit(t, f, map[string]any{"title": "from outside"})
	require.NoError(t, err)

	s := db.NewSession()
	defer s.Close()
	task := &Task{}
	_, err = s.Where("project_id = 11").OrderBy("id DESC").Get(task)
	require.NoError(t, err)
	assert.Equal(t, "from outside", task.Title)
	assert.EqualValues(t, 1, task.CreatedByID, "created as the form owner")
	status, err := GetTaskWorkflowStatus(s, task.ID)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.EqualValues(t, 2, status.ID, "the initial status override")
	assignees, err := getRawTaskAssigneesForTasks(s, []int64{task.ID})
	require.NoError(t, err)
	require.Len(t, assignees, 1)
	assert.EqualValues(t, 1, assignees[0].ID)
	s.Close()

	t.Run("without an override the workflow decides", func(t *testing.T) {
		g := baseForm(FormAccessPublic)
		g.ProjectID = 11
		createForm(t, g)
		_, err := submit(t, g, map[string]any{"title": "default status"})
		require.NoError(t, err)
		s := db.NewSession()
		defer s.Close()
		task := &Task{}
		_, err = s.Where("project_id = 11").OrderBy("id DESC").Get(task)
		require.NoError(t, err)
		status, err := GetTaskWorkflowStatus(s, task.ID)
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.True(t, status.IsInitial)
	})
}

func TestFormSubmit_OwnerLosesAccess(t *testing.T) {
	formsReset(t)
	f := createForm(t, baseForm(FormAccessPublic))
	s := db.NewSession()
	_, err := s.ID(f.ID).Cols("owner_id").Update(&Form{OwnerID: 2})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	before := countRows(t, &Task{})
	_, err = submit(t, f, map[string]any{"title": "x"})
	var un ErrFormUnavailable
	require.ErrorAs(t, err, &un)
	assert.Equal(t, FormUnavailableDisabled, un.Reason)
	assert.Equal(t, before, countRows(t, &Task{}))
	stored := reloadForm(t, f.ID)
	assert.False(t, stored.Enabled)
	assert.Equal(t, FormDisabledOwnerNotWriter, stored.DisabledReason)

	t.Run("a deleted owner", func(t *testing.T) {
		g := createForm(t, baseForm(FormAccessPublic))
		s := db.NewSession()
		_, err := s.ID(g.ID).Cols("owner_id").Update(&Form{OwnerID: 99999})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		_, err = submit(t, g, map[string]any{"title": "x"})
		require.ErrorAs(t, err, &un)
		assert.Equal(t, FormDisabledOwnerUnavailable, reloadForm(t, g.ID).DisabledReason)
	})
}

func TestFormSubmit_Honeypot(t *testing.T) {
	formsReset(t)
	f := baseForm(FormAccessPublic)
	f.Settings = &FormSettings{SuccessMessage: "Thanks"}
	createForm(t, f)
	before := countRows(t, &Task{})
	res, err := submit(t, f, map[string]any{"title": "bot"}, func(r *FormSubmitRequest) { r.Honeypot = "http://spam" })
	require.NoError(t, err)
	assert.Equal(t, "Thanks", res.Message)
	assert.Equal(t, before, countRows(t, &Task{}))
	assert.EqualValues(t, 0, countRows(t, &FormSubmission{}))
	assert.EqualValues(t, 0, reloadForm(t, f.ID).SubmissionCount)
	assert.Equal(t, 0, events.CountDispatchedEvents("form.submitted"))
	// Invalid values do not give a bot away either.
	_, err = submit(t, f, map[string]any{"bogus": 1}, func(r *FormSubmitRequest) { r.Honeypot = "x" })
	require.NoError(t, err)
}

func TestFormSubmit_ClosedFullDisabled(t *testing.T) {
	formsReset(t)
	f := baseForm(FormAccessPublic)
	f.Settings = &FormSettings{MaxSubmissions: 2}
	createForm(t, f)
	for i := 0; i < 2; i++ {
		_, err := submit(t, f, map[string]any{"title": "x"}, fromIP("198.51.100."+fitoa(int64(i))))
		require.NoError(t, err)
	}
	_, err := submit(t, f, map[string]any{"title": "x"}, fromIP("198.51.100.9"))
	var un ErrFormUnavailable
	require.ErrorAs(t, err, &un)
	assert.Equal(t, FormUnavailableFull, un.Reason)
	assert.EqualValues(t, 2, reloadForm(t, f.ID).SubmissionCount)

	g := baseForm(FormAccessPublic)
	closes := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	g.Settings = &FormSettings{CloseDate: &closes}
	createForm(t, g)
	formClock = func() time.Time { return closes.Add(-time.Hour) }
	_, err = submit(t, g, map[string]any{"title": "x"})
	require.NoError(t, err)
	formClock = func() time.Time { return closes }
	_, err = submit(t, g, map[string]any{"title": "x"})
	require.ErrorAs(t, err, &un)
	assert.Equal(t, FormUnavailableClosed, un.Reason)

	h := createForm(t, baseForm(FormAccessPublic))
	s := db.NewSession()
	_, err = s.ID(h.ID).Cols("enabled").Update(&Form{Enabled: false})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	_, err = submit(t, h, map[string]any{"title": "x"})
	require.ErrorAs(t, err, &un)
	assert.Equal(t, FormUnavailableDisabled, un.Reason)
}

func TestFormSubmit_RateLimits(t *testing.T) {
	formsReset(t)
	config.FormsPublicRateLimit.Set(3)
	config.FormsPublicFormRateLimit.Set(5)
	defer config.FormsPublicRateLimit.Set(10)
	defer config.FormsPublicFormRateLimit.Set(300)

	now := time.Date(2031, 1, 1, 12, 0, 0, 0, time.UTC)
	formClock = func() time.Time { return now }
	f := createForm(t, baseForm(FormAccessPublic))

	for i := 0; i < 3; i++ {
		_, err := submit(t, f, map[string]any{"title": "x"}, fromIP("192.0.2.1"))
		require.NoError(t, err)
		now = now.Add(time.Minute)
	}
	_, err := submit(t, f, map[string]any{"title": "x"}, fromIP("192.0.2.1"))
	var rl ErrFormRateLimited
	require.ErrorAs(t, err, &rl)
	assert.Equal(t, 429, rl.HTTPError().HTTPCode)
	// The oldest submission was 3 minutes ago, it frees a slot 57 minutes from now.
	assert.InDelta(t, 57*60, rl.RetryAfterSeconds, 2)
	assert.EqualValues(t, 3, reloadForm(t, f.ID).SubmissionCount)

	t.Run("another address is not limited", func(t *testing.T) {
		_, err := submit(t, f, map[string]any{"title": "x"}, fromIP("192.0.2.2"))
		require.NoError(t, err)
	})

	t.Run("the window expires", func(t *testing.T) {
		now = now.Add(58 * time.Minute)
		_, err := submit(t, f, map[string]any{"title": "x"}, fromIP("192.0.2.1"))
		require.NoError(t, err)
	})

	t.Run("the form limit applies to everybody", func(t *testing.T) {
		g := createForm(t, baseForm(FormAccessPublic))
		for i := 0; i < 5; i++ {
			_, err := submit(t, g, map[string]any{"title": "x"}, fromIP("192.0.2."+fitoa(int64(100+i))))
			require.NoError(t, err)
		}
		_, err := submit(t, g, map[string]any{"title": "x"}, fromIP("192.0.2.200"))
		require.ErrorAs(t, err, &rl)
		assert.Positive(t, rl.RetryAfterSeconds)
		now = now.Add(time.Hour + time.Minute)
		_, err = submit(t, g, map[string]any{"title": "x"}, fromIP("192.0.2.200"))
		require.NoError(t, err)
	})

	t.Run("forms for signed-in users are not limited per address", func(t *testing.T) {
		u := createForm(t, baseForm(FormAccessUsers))
		for i := 0; i < 5; i++ {
			_, err := submit(t, u, map[string]any{"title": "x"}, asAuth(fU2), fromIP("192.0.2.1"))
			require.NoError(t, err)
		}
	})

	t.Run("only a salted hash of the address is stored", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		rows := []*FormSubmission{}
		require.NoError(t, s.Find(&rows))
		require.NotEmpty(t, rows)
		for _, r := range rows {
			assert.Len(t, r.IPHash, 64)
			assert.NotContains(t, r.IPHash, "192.0.2")
			assert.NotEqual(t, "192.0.2.1", r.IPHash)
		}
		first := formIPHash("192.0.2.1")
		assert.Equal(t, first, formIPHash("192.0.2.1"), "the hash is stable")
		assert.NotEqual(t, formIPHash("192.0.2.1"), formIPHash("192.0.2.2"))
	})
}

func TestFormSubmit_SubmissionsListHidesTheHash(t *testing.T) {
	formsReset(t)
	f := createForm(t, baseForm(FormAccessPublic))
	_, err := submit(t, f, map[string]any{"title": "x"})
	require.NoError(t, err)
	s := db.NewSession()
	defer s.Close()
	result, _, total, err := (&FormSubmission{FormID: f.ID, ProjectID: 1}).ReadAll(s, fU1, "", 1, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	rows := result.([]*FormSubmission)
	require.Len(t, rows, 1)
	assert.NotEmpty(t, rows[0].IPHash, "stored")
	keys := marshalKeys(t, rows[0])
	assert.ElementsMatch(t, []string{"id", "form_id", "task_id", "submitter_id", "submitter_name", "submitter_email", "created"}, keysOf(keys))
	_, _, _, err = (&FormSubmission{FormID: f.ID, ProjectID: 1}).ReadAll(s, fU2, "", 1, 10)
	assert.Error(t, err)
}

func upload(name string, content []byte) *AttachmentToUpload {
	return &AttachmentToUpload{Reader: bytes.NewReader(content), Filename: name, Size: uint64(len(content))}
}

func TestFormSubmit_Attachments(t *testing.T) {
	formsReset(t)
	asked := func(access FormAccess) *Form {
		return createForm(t, baseForm(access, fld("title", "T", true), fld("attachments", "Files", false)))
	}
	withFiles := func(files ...*AttachmentToUpload) func(*FormSubmitRequest) {
		return func(r *FormSubmitRequest) { r.Files = files }
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

	t.Run("off by default on public forms", func(t *testing.T) {
		err := tryCreateForm(t, baseForm(FormAccessPublic, fld("title", "T", true), fld("attachments", "Files", false)), fU1)
		require.Error(t, err)
		// A form that was saved while the switch was on cannot take files once it is off.
		config.FormsPublicAttachments.Set(true)
		f := asked(FormAccessPublic)
		config.FormsPublicAttachments.Set(false)
		_, err = submit(t, f, map[string]any{"title": "x"}, withFiles(upload("a.png", png)))
		assert.Equal(t, map[string]string{"attachments": "attachments_disabled"}, fieldErrors(t, err))
	})

	t.Run("a form without the field takes no files", func(t *testing.T) {
		f := createForm(t, baseForm(FormAccessUsers))
		_, err := submit(t, f, map[string]any{"title": "x"}, asAuth(fU2), withFiles(upload("a.png", png)))
		assert.Equal(t, map[string]string{"attachments": "unknown_field"}, fieldErrors(t, err))
	})

	t.Run("files are attached to the task as the owner", func(t *testing.T) {
		config.FormsPublicAttachments.Set(true)
		defer config.FormsPublicAttachments.Set(false)
		f := asked(FormAccessPublic)
		def, err := GetFormDefinition(f.Token, nil)
		require.NoError(t, err)
		require.NotNil(t, def.Attachments)
		assert.Equal(t, 5, def.Attachments.MaxFiles)
		assert.EqualValues(t, 10*1024*1024, def.Attachments.MaxFileSize)

		_, err = submit(t, f, map[string]any{"title": "with a file"}, withFiles(upload("a.png", png), upload("notes.txt", []byte("hello"))))
		require.NoError(t, err)
		task := lastTask(t)
		s := db.NewSession()
		defer s.Close()
		rows := []*TaskAttachment{}
		require.NoError(t, s.Where("task_id = ?", task.ID).Find(&rows))
		require.Len(t, rows, 2)
		assert.EqualValues(t, 1, rows[0].CreatedByID)
	})

	t.Run("limits and the denylist", func(t *testing.T) {
		config.FormsPublicAttachments.Set(true)
		defer config.FormsPublicAttachments.Set(false)
		config.FormsMaxAttachments.Set(2)
		config.FormsMaxAttachmentSize.Set("1KB")
		defer config.FormsMaxAttachments.Set(5)
		defer config.FormsMaxAttachmentSize.Set("10MB")
		f := asked(FormAccessPublic)
		tasks := countRows(t, &Task{})
		attachments := countRows(t, &TaskAttachment{})

		bad := func(code string, files ...*AttachmentToUpload) {
			_, err := submit(t, f, map[string]any{"title": "x"}, withFiles(files...))
			assert.Equal(t, map[string]string{"attachments": code}, fieldErrors(t, err))
		}
		bad("too_many", upload("1.txt", []byte("a")), upload("2.txt", []byte("a")), upload("3.txt", []byte("a")))
		bad("file_too_large", upload("big.txt", bytes.Repeat([]byte("a"), 2048)))
		bad("file_type_not_allowed", upload("run.exe", []byte("MZ")))
		bad("file_type_not_allowed", upload("page.html", []byte("<html>")))
		bad("file_type_not_allowed", upload("pic.svg", []byte("<svg/>")))
		bad("file_type_not_allowed", upload("harmless.txt", []byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00>\x00")))
		bad("file_type_not_allowed", upload("harmless.txt", []byte("<!DOCTYPE html><html><body>x</body></html>")))
		assert.Equal(t, tasks, countRows(t, &Task{}), "a rejected file creates nothing")
		assert.Equal(t, attachments, countRows(t, &TaskAttachment{}))
	})

	t.Run("required attachments", func(t *testing.T) {
		f := createForm(t, baseForm(FormAccessUsers, fld("title", "T", true), fld("attachments", "Files", true)))
		_, err := submit(t, f, map[string]any{"title": "x"}, asAuth(fU2))
		assert.Equal(t, map[string]string{"attachments": "required"}, fieldErrors(t, err))
	})
}

func TestFormSubmit_NotificationAndEvent(t *testing.T) {
	formsReset(t)
	f := baseForm(FormAccessPublic)
	f.Settings = &FormSettings{NotifyUserIDs: []int64{1}, CollectName: true}
	createForm(t, f)
	_, err := submit(t, f, map[string]any{"title": "Ping"}, func(r *FormSubmitRequest) { r.SubmitterName = "Ada" })
	require.NoError(t, err)

	evs := events.GetDispatchedEvents("form.submitted")
	require.Len(t, evs, 1)
	ev := evs[0].(*FormSubmittedEvent)
	assert.Equal(t, f.ID, ev.FormID)
	assert.Equal(t, "Request", ev.FormTitle)
	assert.Equal(t, "Ping", ev.Task.Title)
	assert.Positive(t, ev.Task.ID)
	assert.EqualValues(t, 1, ev.Doer.ID)

	events.TestListener(t, ev, &SendFormSubmittedNotification{})
	s := db.NewSession()
	defer s.Close()
	rows := []*notifications.DatabaseNotification{}
	require.NoError(t, s.Where("notifiable_id = ? AND name = ?", 1, "form.submitted").Find(&rows))
	require.Len(t, rows, 1)
	assert.EqualValues(t, 1, rows[0].ProjectID)
	payload := marshalKeys(t, rows[0].Notification)
	assert.EqualValues(t, f.ID, payload["form_id"])
	assert.Equal(t, "Ada", payload["submitter_name"])
	assert.Equal(t, "Request", payload["form_title"])
	assert.NotNil(t, payload["task"])

	t.Run("a notified user who lost access gets nothing", func(t *testing.T) {
		g := baseForm(FormAccessPublic)
		g.Settings = &FormSettings{}
		createForm(t, g)
		upd := reloadForm(t, g.ID)
		upd.Settings.NotifyUserIDs = []int64{2}
		s := db.NewSession()
		_, err := s.ID(g.ID).Cols("settings").Update(upd)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		events.TestListener(t, &FormSubmittedEvent{FormID: g.ID, Task: ev.Task}, &SendFormSubmittedNotification{})
		s = db.NewSession()
		defer s.Close()
		n, err := s.Where("notifiable_id = ? AND name = ?", 2, "form.submitted").Count(&notifications.DatabaseNotification{})
		require.NoError(t, err)
		assert.EqualValues(t, 0, n)
	})
}

func TestForm_Lifecycle(t *testing.T) {
	formsReset(t)

	t.Run("deleting a form keeps its tasks and removes its submissions", func(t *testing.T) {
		f := createForm(t, baseForm(FormAccessPublic))
		_, err := submit(t, f, map[string]any{"title": "kept"})
		require.NoError(t, err)
		task := lastTask(t)
		s := db.NewSession()
		require.NoError(t, (&Form{ID: f.ID, ProjectID: 1}).Delete(s, fU1))
		require.NoError(t, s.Commit())
		s.Close()
		assert.EqualValues(t, 0, countRows(t, &FormSubmission{}))
		s = db.NewSession()
		defer s.Close()
		has, err := s.ID(task.ID).Exist(&Task{})
		require.NoError(t, err)
		assert.True(t, has)
	})

	t.Run("deleting a custom field disables the form that asks for it", func(t *testing.T) {
		f := createForm(t, baseForm(FormAccessMembers, fld("title", "T", true), fld("cf_1", "Ref", false)))
		g := createForm(t, baseForm(FormAccessMembers, fld("title", "T", true)))
		s := db.NewSession()
		require.NoError(t, (&CustomField{ID: 1, ProjectID: 1}).Delete(s, fU1))
		require.NoError(t, s.Commit())
		s.Close()
		assert.Equal(t, FormDisabledCustomFieldDeleted, reloadForm(t, f.ID).DisabledReason)
		assert.False(t, reloadForm(t, f.ID).Enabled)
		assert.True(t, reloadForm(t, g.ID).Enabled)
	})

	t.Run("deleting a label disables the form that uses it", func(t *testing.T) {
		formsReset(t)
		f := createForm(t, baseForm(FormAccessMembers, fld("title", "T", true), hiddenFld("labels", []any{2})))
		g := createForm(t, baseForm(FormAccessMembers))
		s := db.NewSession()
		require.NoError(t, (&Label{ID: 2}).Delete(s, fU1))
		require.NoError(t, s.Commit())
		s.Close()
		assert.Equal(t, FormDisabledLabelDeleted, reloadForm(t, f.ID).DisabledReason)
		assert.True(t, reloadForm(t, g.ID).Enabled)
	})

	t.Run("removing the status of a form disables it", func(t *testing.T) {
		formsReset(t)
		f := baseForm(FormAccessMembers)
		f.ProjectID = 11
		f.Settings.InitialStatusID = 2
		createForm(t, f)
		s := db.NewSession()
		require.NoError(t, disableFormsForStatuses(s, 11, []int64{2}))
		require.NoError(t, s.Commit())
		s.Close()
		assert.Equal(t, FormDisabledStatusDeleted, reloadForm(t, f.ID).DisabledReason)
	})

	t.Run("deleting the owner disables the forms", func(t *testing.T) {
		formsReset(t)
		f := createForm(t, baseForm(FormAccessMembers))
		s := db.NewSession()
		require.NoError(t, disableFormsOfOwner(s, 1))
		require.NoError(t, s.Commit())
		s.Close()
		stored := reloadForm(t, f.ID)
		assert.False(t, stored.Enabled)
		assert.Equal(t, FormDisabledOwnerUnavailable, stored.DisabledReason)
	})

	t.Run("purging a project deletes its forms and submissions", func(t *testing.T) {
		formsReset(t)
		f := createForm(t, baseForm(FormAccessPublic))
		_, err := submit(t, f, map[string]any{"title": "x"})
		require.NoError(t, err)
		s := db.NewSession()
		require.NoError(t, deleteFormsForProject(s, 1))
		require.NoError(t, s.Commit())
		s.Close()
		assert.EqualValues(t, 0, countRows(t, &Form{}))
		assert.EqualValues(t, 0, countRows(t, &FormSubmission{}))
	})

	t.Run("duplicating a project copies the forms disabled with a new token and remapped ids", func(t *testing.T) {
		formsReset(t)
		f := baseForm(FormAccessMembers, fld("title", "T", true), &FormField{Target: "cf_2", Label: "Stage", Default: 2}, fld("cf_1", "Ref", false))
		f.Settings.CloseDate = nil
		createForm(t, f)
		s := db.NewSession()
		fieldMap, optionMap, err := duplicateCustomFieldsForProject(s, 1, 10)
		require.NoError(t, err)
		require.NoError(t, duplicateFormsForProject(s, 1, 1, 10, fieldMap, optionMap, map[int64]int64{}))
		require.NoError(t, s.Commit())
		s.Close()

		copies := []*Form{}
		s = db.NewSession()
		defer s.Close()
		require.NoError(t, s.Where("project_id = 10").Find(&copies))
		require.Len(t, copies, 1)
		c := copies[0]
		assert.False(t, c.Enabled)
		assert.Equal(t, FormDisabledDuplicated, c.DisabledReason)
		assert.NotEqual(t, f.Token, c.Token)
		assert.Equal(t, "cf_"+fitoa(fieldMap[2]), c.Fields[1].Target)
		def, ok := toInt(c.Fields[1].Default)
		require.True(t, ok)
		assert.Equal(t, optionMap[2], def)
		assert.EqualValues(t, 0, c.SubmissionCount)

		// A reference without a copy says so.
		s.Close()
		s2 := db.NewSession()
		defer s2.Close()
		require.NoError(t, duplicateFormsForProject(s2, 1, 1, 9, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}))
		unmapped := []*Form{}
		require.NoError(t, s2.Where("project_id = 9").Find(&unmapped))
		require.Len(t, unmapped, 1)
		assert.Equal(t, FormDisabledUnmappedReference, unmapped[0].DisabledReason)
	})
}

var _ io.Reader = (*bytes.Reader)(nil)
var _ = (*xorm.Session)(nil)

func TestFormSubmit_SubmitterDetailsOnlyWhenAsked(t *testing.T) {
	formsReset(t)
	submissionOf := func(t *testing.T, task *Task) *FormSubmission {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		sub := &FormSubmission{}
		has, err := s.Where("task_id = ?", task.ID).Get(sub)
		require.NoError(t, err)
		require.True(t, has)
		return sub
	}
	claimed := func(r *FormSubmitRequest) {
		r.SubmitterName = "The Boss"
		r.SubmitterEmail = "not an address <script>"
	}

	t.Run("a public form that does not ask takes neither a name nor an address", func(t *testing.T) {
		f := createForm(t, baseForm(FormAccessPublic, fld("title", "T", true), fld("description", "D", false)))
		_, err := submit(t, f, map[string]any{"title": "x", "description": "hello"}, claimed)
		require.NoError(t, err)
		task := lastTask(t)
		assert.Equal(t, "<p>hello</p>", task.Description)
		sub := submissionOf(t, task)
		assert.Empty(t, sub.SubmitterName)
		assert.Empty(t, sub.SubmitterEmail)
	})

	t.Run("a signed-in submitter cannot claim to be somebody else", func(t *testing.T) {
		s := db.NewSession()
		u1, err := user.GetUserByID(s, 1)
		s.Close()
		require.NoError(t, err)
		f := createForm(t, baseForm(FormAccessMembers))
		_, err = submit(t, f, map[string]any{"title": "x"}, asAuth(u1), claimed)
		require.NoError(t, err)
		task := lastTask(t)
		assert.NotContains(t, task.Description, "The Boss")
		assert.NotContains(t, task.Description, "not an address")
		sub := submissionOf(t, task)
		assert.Equal(t, "user1", sub.SubmitterName)
		assert.Equal(t, u1.Email, sub.SubmitterEmail)
		require.NotNil(t, sub.SubmitterID)
		assert.EqualValues(t, 1, *sub.SubmitterID)
	})

	t.Run("only the detail the form asks for is taken", func(t *testing.T) {
		f := baseForm(FormAccessPublic)
		f.Settings = &FormSettings{CollectName: true}
		createForm(t, f)
		_, err := submit(t, f, map[string]any{"title": "x"}, claimed)
		require.NoError(t, err)
		sub := submissionOf(t, lastTask(t))
		assert.Equal(t, "The Boss", sub.SubmitterName)
		assert.Empty(t, sub.SubmitterEmail)
	})
}

func TestFormSubmit_AttachmentDenylistIgnoresTrailingDotsAndSpaces(t *testing.T) {
	formsReset(t)
	f := createForm(t, baseForm(FormAccessUsers, fld("title", "T", true), fld("attachments", "Files", false)))
	// Windows drops trailing dots and spaces when the file is saved, so "run.bat." lands as "run.bat".
	for _, name := range []string{"run.bat.", "run.bat ", "run.BAT. .", "page.html.."} {
		_, err := submit(t, f, map[string]any{"title": "x"}, asAuth(fU2), func(r *FormSubmitRequest) {
			r.Files = []*AttachmentToUpload{upload(name, []byte("echo hello"))}
		})
		require.Error(t, err, name)
		assert.Equal(t, map[string]string{"attachments": "file_type_not_allowed"}, fieldErrors(t, err), name)
	}
}

// failingUpload can be sniffed and fails when it is stored: reads fail after the second rewind.
type failingUpload struct {
	*bytes.Reader
	rewinds int
}

func (f *failingUpload) Seek(offset int64, whence int) (int64, error) {
	f.rewinds++
	return f.Reader.Seek(offset, whence)
}

func (f *failingUpload) Read(p []byte) (int, error) {
	if f.rewinds >= 2 {
		return 0, errors.New("the storage is gone")
	}
	return f.Reader.Read(p)
}

func TestFormSubmit_FailureAfterTheTaskLeavesNothing(t *testing.T) {
	formsReset(t)
	f := createForm(t, baseForm(FormAccessUsers, fld("title", "T", true), fld("attachments", "Files", false)))
	s := db.NewSession()
	var maxFileID int64
	_, err := s.SQL("SELECT COALESCE(MAX(id), 0) FROM files").Get(&maxFileID)
	s.Close()
	require.NoError(t, err)
	// The fixtures reset the rows but not the storage: blobs earlier tests left under the next ids go first.
	for id := maxFileID + 1; id <= maxFileID+3; id++ {
		_ = files.DeleteBlob(id)
	}
	before := []int64{countRows(t, &Task{}), countRows(t, &TaskAttachment{}), countRows(t, &FormSubmission{}), countRows(t, &files.File{}), countRows(t, &TaskCustomFieldValue{})}
	events.ClearDispatchedEvents()

	// The first file is stored, the second one breaks after the task, its values and the first attachment exist.
	_, err = submit(t, f, map[string]any{"title": "half"}, asAuth(fU2), func(r *FormSubmitRequest) {
		r.Files = []*AttachmentToUpload{
			upload("ok.txt", []byte("hello")),
			{Reader: &failingUpload{Reader: bytes.NewReader([]byte("world"))}, Filename: "broken.txt", Size: 5},
		}
	})
	require.Error(t, err)
	var invalid ErrFormSubmissionInvalid
	require.NotErrorAs(t, err, &invalid, "the file passed validation and failed in storage")

	after := []int64{countRows(t, &Task{}), countRows(t, &TaskAttachment{}), countRows(t, &FormSubmission{}), countRows(t, &files.File{}), countRows(t, &TaskCustomFieldValue{})}
	assert.Equal(t, before, after, "tasks, attachments, submissions, files, values")
	assert.Zero(t, reloadForm(t, f.ID).SubmissionCount)
	assert.Empty(t, events.GetDispatchedEvents("form.submitted"))
	assert.Empty(t, events.GetDispatchedEvents("task.created"))
	for id := maxFileID + 1; id <= maxFileID+3; id++ {
		err := (&files.File{ID: id}).LoadFileByID()
		assert.True(t, files.IsErrFileDoesNotExist(err), "the blob %d of the rolled back file is still stored: %v", id, err)
	}
}
