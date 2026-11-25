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

package webtests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func formsDecode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return out
}

func formsKeys(m map[string]any) []string {
	out := []string{}
	for k := range m {
		if k != "$schema" { // the schema link Huma adds to every body
			out = append(out, k)
		}
	}
	return out
}

func TestHumaForms_Admin(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)
	body := `{"title":"Cover request","access":"members","enabled":true,"fields":[{"target":"title","label":"Title","required":true},{"target":"cf_2","label":"Stage","required":true}],"settings":{"success_message":"Thanks"}}`

	t.Run("create, read, list, update, token, submissions, delete", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/forms", body, user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		created := formsDecode(t, rec)
		id := int64(created["id"].(float64))
		token := created["token"].(string)
		assert.GreaterOrEqual(t, len(token), 32)
		assert.EqualValues(t, 1, created["owner_id"])
		assert.EqualValues(t, 1, created["project_id"])

		rec = humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/projects/1/forms/%d", id), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotEmpty(t, rec.Header().Get("Etag"))
		assert.EqualValues(t, 2, formsDecode(t, rec)["max_permission"])

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/forms", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.EqualValues(t, 1, formsDecode(t, rec)["total"])

		rec = humaRequest(t, e, http.MethodPatch, fmt.Sprintf("/api/v2/projects/1/forms/%d", id), `{"title":"Renamed"}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "Renamed", formsDecode(t, rec)["title"])

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/forms/options", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"cf_2"`)
		assert.Contains(t, rec.Body.String(), `"public_visible"`)

		// A submission, then the list of submissions without the hash.
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions", `{"values":{"title":"x","cf_2":1}}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/projects/1/forms/%d/submissions", id), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), "ip_hash")
		assert.NotContains(t, rec.Body.String(), "IPHash")
		assert.EqualValues(t, 1, formsDecode(t, rec)["total"])

		rec = humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/projects/1/forms/%d/token", id), "", user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		newToken := formsDecode(t, rec)["token"].(string)
		assert.NotEqual(t, token, newToken)
		assert.Equal(t, http.StatusNotFound, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token, "", user1, "").Code)
		assert.Equal(t, http.StatusOK, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+newToken, "", user1, "").Code)

		rec = humaRequest(t, e, http.MethodDelete, fmt.Sprintf("/api/v2/projects/1/forms/%d", id), "", user1, "")
		assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		assert.Equal(t, http.StatusNotFound, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+newToken, "", user1, "").Code)
	})

	t.Run("validation errors are typed with a path", func(t *testing.T) {
		bad := `{"title":"x","access":"public","fields":[{"target":"title","label":"T","required":true},{"target":"cf_999","label":"x"}]}`
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/forms", bad, user1, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		out := formsDecode(t, rec)
		assert.EqualValues(t, models.ErrCodeFormInvalid, out["code"])
		assert.Contains(t, rec.Body.String(), "fields[1].target")
	})

	t.Run("only project admins", func(t *testing.T) {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/api/v2/projects/1/forms", ""},
			{http.MethodPost, "/api/v2/projects/1/forms", body},
			{http.MethodGet, "/api/v2/projects/1/forms/options", ""},
			{http.MethodGet, "/api/v2/projects/1/forms/1", ""},
			{http.MethodGet, "/api/v2/projects/1/forms/1/submissions", ""},
			{http.MethodPost, "/api/v2/projects/1/forms/1/token", ""},
			{http.MethodDelete, "/api/v2/projects/1/forms/1", ""},
		} {
			rec := humaRequest(t, e, c.method, c.path, c.body, user2, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", c.method, c.path, rec.Body.String())
			rec = humaRequest(t, e, c.method, c.path, c.body, "", "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s anonymous", c.method, c.path)
		}
		share, err := auth.NewLinkShareJWTAuthtoken(&models.LinkSharing{ID: 1, Hash: "test", ProjectID: 1, Permission: models.PermissionAdmin, SharingType: models.SharingTypeWithoutPassword, SharedByID: 1})
		require.NoError(t, err)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/1/forms", "", share, "")
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func newPublicForm(t *testing.T, e *echo.Echo, token, access, fields string, settings string) (id int64, formToken string) {
	t.Helper()
	body := fmt.Sprintf(`{"title":"Public form","access":%q,"enabled":true,"fields":%s,"settings":%s}`, access, fields, settings)
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/forms", body, token, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	out := formsDecode(t, rec)
	return int64(out["id"].(float64)), out["token"].(string)
}

func TestHumaForms_PublicRoutes(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)
	fields := `[{"target":"title","label":"Title","required":true},{"target":"cf_2","label":"Stage"},{"target":"cf_1","hidden":true,"default":"internal"}]`
	_, pub := newPublicForm(t, e, user1, "public", fields, `{"success_message":"Thanks"}`)
	_, usr := newPublicForm(t, e, user1, "users", fields, `{}`)
	_, mem := newPublicForm(t, e, user1, "members", fields, `{}`)

	t.Run("the public definition needs no auth, sets no cookie and is not cached", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+pub, "", "", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		assert.Empty(t, rec.Header().Values("Set-Cookie"))
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
		out := formsDecode(t, rec)
		assert.ElementsMatch(t, []string{"title", "description", "fields", "success", "submitter", "branding"}, formsKeys(out))
		assert.NotContains(t, rec.Body.String(), "internal")
		assert.NotContains(t, rec.Body.String(), "project_id")
		assert.Len(t, out["fields"], 2)
	})

	t.Run("an unknown token is 404", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/forms/nope", "", "", "")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.EqualValues(t, models.ErrCodeFormDoesNotExist, formsDecode(t, rec)["code"])
	})

	t.Run("a token that is no token is 404, not a database error", func(t *testing.T) {
		// PostgreSQL rejects a NUL byte and invalid UTF-8 in a text parameter; that must not surface as a 500.
		for _, token := range []string{"%00", "abc%00def", "%ff%fe", "caf%C3%A9"} {
			for _, c := range []struct{ method, suffix, body, contentType string }{
				{http.MethodGet, "", "", ""},
				{http.MethodGet, "/logo", "", ""},
				{http.MethodPost, "/submissions", `{"values":{"title":"x"}}`, ""},
				{http.MethodPost, "/submissions/multipart", "--b\r\nContent-Disposition: form-data; name=\"data\"\r\n\r\n{\"values\":{\"title\":\"x\"}}\r\n--b--\r\n", "multipart/form-data; boundary=b"},
			} {
				rec := humaRequest(t, e, c.method, "/api/v2/forms/"+token+c.suffix, c.body, "", c.contentType)
				assert.Equal(t, http.StatusNotFound, rec.Code, "%s %s%s: %s", c.method, token, c.suffix, rec.Body.String())
			}
		}
	})

	t.Run("forms for signed-in users answer a typed 401 without credentials", func(t *testing.T) {
		for _, token := range []string{usr, mem} {
			rec := humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token, "", "", "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.EqualValues(t, models.ErrCodeFormLoginRequired, formsDecode(t, rec)["code"])
			rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions", `{"values":{"title":"x"}}`, "", "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.EqualValues(t, models.ErrCodeFormLoginRequired, formsDecode(t, rec)["code"])
			assert.Empty(t, rec.Header().Values("Set-Cookie"))
		}
	})

	t.Run("signed-in users and members", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+usr, "", user2, "").Code)
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+mem, "", user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.EqualValues(t, models.ErrCodeFormNotAllowed, formsDecode(t, rec)["code"])
		assert.Equal(t, http.StatusOK, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+mem, "", user1, "").Code)
	})

	t.Run("link shares are not users", func(t *testing.T) {
		share, err := auth.NewLinkShareJWTAuthtoken(&models.LinkSharing{ID: 1, Hash: "test", ProjectID: 1, Permission: models.PermissionWrite, SharingType: models.SharingTypeWithoutPassword, SharedByID: 1})
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+usr, "", share, "").Code)
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+mem, "", share, "").Code)
		assert.Equal(t, http.StatusOK, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+pub, "", share, "").Code)
	})

	t.Run("a bad token on a public form is a 401 with code 11, not an anonymous request", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+pub, "", "garbage", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.EqualValues(t, 11, formsDecode(t, rec)["code"])
	})

	t.Run("an anonymous submission", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+pub+"/submissions", `{"values":{"title":"Hello","cf_2":1},"submitter":{},"honeypot":""}`, "", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Empty(t, rec.Header().Values("Set-Cookie"))
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		out := formsDecode(t, rec)
		assert.ElementsMatch(t, []string{"message", "redirect_url", "submit_another"}, formsKeys(out))
		assert.Equal(t, "Thanks", out["message"])
	})

	t.Run("a signed-in member gets the task id", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+mem+"/submissions", `{"values":{"title":"Hello"}}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotZero(t, formsDecode(t, rec)["task_id"])
	})

	t.Run("validation errors are 422 with one entry per field", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+pub+"/submissions", `{"values":{"cf_2":"no","cf_1":"hidden"}}`, "", "")
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		out := formsDecode(t, rec)
		assert.EqualValues(t, models.ErrCodeFormSubmissionInvalid, out["code"])
		details := out["details"].(map[string]any)
		errs := details["errors"].([]any)
		byField := map[string]string{}
		for _, e := range errs {
			m := e.(map[string]any)
			assert.Contains(t, formsKeys(m), "message")
			byField[m["field"].(string)] = m["code"].(string)
		}
		assert.Equal(t, map[string]string{"title": "required", "cf_2": "invalid", "cf_1": "unknown_field"}, byField)
	})

	t.Run("a honeypot looks like a success and creates nothing", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+pub+"/submissions", `{"values":{"title":"bot"},"honeypot":"x"}`, "", "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "Thanks", formsDecode(t, rec)["message"])
	})

	t.Run("form bodies and oversized bodies are refused", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+pub+"/submissions", "values=1", "", "application/x-www-form-urlencoded")
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+pub+"/submissions", "title=1", "", "text/plain")
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
		huge := `{"values":{"title":"` + strings.Repeat("a", 300*1024) + `"}}`
		rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+pub+"/submissions", huge, "", "")
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	})

	t.Run("a long value is a per-field error", func(t *testing.T) {
		long := `{"values":{"title":"` + strings.Repeat("a", 300) + `"}}`
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+pub+"/submissions", long, "", "")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Contains(t, rec.Body.String(), "too_long")
	})

	t.Run("a cross-origin request gets no CORS grant", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v2/forms/"+pub+"/submissions", nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
	})
}

func TestHumaForms_PublicLimitsAndAvailability(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	fields := `[{"target":"title","label":"Title","required":true}]`

	t.Run("closed, full and disabled answer 410 with a reason", func(t *testing.T) {
		_, full := newPublicForm(t, e, user1, "public", fields, `{"max_submissions":1}`)
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+full+"/submissions", `{"values":{"title":"a"}}`, "", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/api/v2/forms/" + full, ""},
			{http.MethodPost, "/api/v2/forms/" + full + "/submissions", `{"values":{"title":"b"}}`},
		} {
			rec = humaRequest(t, e, c.method, c.path, c.body, "", "")
			require.Equal(t, http.StatusGone, rec.Code, rec.Body.String())
			out := formsDecode(t, rec)
			assert.EqualValues(t, models.ErrCodeFormUnavailable, out["code"])
			assert.Equal(t, map[string]any{"reason": "full", "title": "Public form"}, out["details"])
		}

		_, closed := newPublicForm(t, e, user1, "public", fields, `{"close_date":"2020-01-01T00:00:00Z"}`)
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+closed, "", "", "")
		require.Equal(t, http.StatusGone, rec.Code)
		assert.Contains(t, rec.Body.String(), `"reason":"closed"`)

		id, off := newPublicForm(t, e, user1, "public", fields, `{}`)
		rec = humaRequest(t, e, http.MethodPatch, fmt.Sprintf("/api/v2/projects/1/forms/%d", id), `{"enabled":false}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+off, "", "", "")
		require.Equal(t, http.StatusGone, rec.Code)
		assert.Contains(t, rec.Body.String(), `"reason":"disabled"`)
	})

	t.Run("the per address limit answers 429 with Retry-After", func(t *testing.T) {
		config.FormsPublicRateLimit.Set(2)
		defer config.FormsPublicRateLimit.Set(10)
		e, err := setupTestEnv() // fresh fixtures: no earlier submissions from this address
		require.NoError(t, err)
		_, token := newPublicForm(t, e, user1, "public", fields, `{}`)
		for i := 0; i < 2; i++ {
			rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions", `{"values":{"title":"a"}}`, "", "")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		}
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions", `{"values":{"title":"a"}}`, "", "")
		require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
		assert.NotEmpty(t, rec.Header().Get("Retry-After"))
		assert.EqualValues(t, models.ErrCodeFormRateLimited, formsDecode(t, rec)["code"])
	})

	t.Run("the request floor of the router", func(t *testing.T) {
		config.FormsPublicRequestRateLimit.Set(3)
		defer config.FormsPublicRequestRateLimit.Set(60)
		e2, err := setupTestEnv()
		require.NoError(t, err)
		last := 0
		for i := 0; i < 6; i++ {
			last = humaRequest(t, e2, http.MethodGet, "/api/v2/forms/nope", "", "", "").Code
		}
		assert.Equal(t, http.StatusTooManyRequests, last)
	})
}

func TestHumaForms_PublicAttachments(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	fields := `[{"target":"title","label":"Title","required":true},{"target":"attachments","label":"Files"}]`

	multipartBody := func(data string, files map[string]string) (string, string) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		require.NoError(t, w.WriteField("data", data))
		for name, content := range files {
			part, err := w.CreateFormFile("files", name)
			require.NoError(t, err)
			_, _ = part.Write([]byte(content))
		}
		require.NoError(t, w.Close())
		return buf.String(), w.FormDataContentType()
	}

	// Saving is refused while the switch is off.
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/forms", `{"title":"x","access":"public","enabled":true,"fields":`+fields+`}`, user1, "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	config.FormsPublicAttachments.Set(true)
	defer config.FormsPublicAttachments.Set(false)
	_, token := newPublicForm(t, e, user1, "public", fields, `{}`)

	rec = humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token, "", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"attachments":{"max_files":5`)

	body, ct := multipartBody(`{"values":{"title":"with file"}}`, map[string]string{"notes.txt": "hello"})
	rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions/multipart", body, "", ct)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Header().Values("Set-Cookie"))

	// The data part of the submission: not JSON, and larger than the body limit, each with its own code.
	body, ct = multipartBody(`{"values": nope`, nil)
	rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions/multipart", body, "", ct)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Equal(t, models.ErrCodeFormDataInvalid, decodeProblem(t, rec.Body.Bytes()).Code)
	body, ct = multipartBody(`{"values":{"title":"`+strings.Repeat("x", config.FormsMaxBodySize.GetInt()+1)+`"}}`, nil)
	rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions/multipart", body, "", ct)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	assert.Equal(t, models.ErrCodeFormDataTooLarge, decodeProblem(t, rec.Body.Bytes()).Code)

	body, ct = multipartBody(`{"values":{"title":"with exe"}}`, map[string]string{"run.exe": "MZ"})
	rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions/multipart", body, "", ct)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "file_type_not_allowed")

	body, ct = multipartBody(`not json`, nil)
	rec = humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions/multipart", body, "", ct)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestHumaForms_PublicRoutesNeverFallBackToAnonymous(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	fields := `[{"target":"title","label":"Title","required":true}]`
	_, pub := newPublicForm(t, e, user1, "public", fields, `{}`)
	_, mem := newPublicForm(t, e, user1, "members", fields, `{}`)

	t.Run("credentials that are not a user session are refused, also on a public form", func(t *testing.T) {
		for _, header := range []string{
			"Bearer tk_2eef46f40ebab3304919ab2e7e39993f75f29d2e", // a valid API token: the routes are not in any scope
			"Bearer tk_loremipsumdolorsitamet",
			"Basic dXNlcjE6MTIzNDU2Nzg=",
			"Bearer",
			user1, // no scheme
		} {
			for _, token := range []string{pub, mem} {
				req := httptest.NewRequest(http.MethodPost, "/api/v2/forms/"+token+"/submissions", strings.NewReader(`{"values":{"title":"x"}}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", header)
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s: %s", header, rec.Body.String())
			}
		}
	})

	t.Run("only the three exact routes are public", func(t *testing.T) {
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, "/api/v2/forms/" + pub + "/"},
			{http.MethodGet, "/api/v2/forms/" + pub + "/submissions"},
			{http.MethodGet, "/api/v2/forms/" + pub + "/submissions/multipart"},
			{http.MethodGet, "/api/v2/forms"},
			{http.MethodGet, "/api/v2/forms/" + pub + "/../../projects/1/forms"},
			{http.MethodGet, "/api/v2/forms/" + pub + "/..%2F..%2Fprojects%2F1%2Fforms"},
			{http.MethodGet, "/api/v2/projects/1/forms"},
			{http.MethodGet, "/api/v2/projects/1/forms/1/submissions"},
			{http.MethodPut, "/api/v2/forms/" + pub},
			{http.MethodPatch, "/api/v2/forms/" + pub},
			{http.MethodDelete, "/api/v2/forms/" + pub},
		} {
			rec := humaRequest(t, e, c.method, c.path, "", "", "")
			assert.NotEqual(t, http.StatusOK, rec.Code, "%s %s: %s", c.method, c.path, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), pub, "%s %s", c.method, c.path)
		}
	})
}

func TestHumaForms_SignedInSubmitterCannotClaimAnotherIdentity(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	id, mem := newPublicForm(t, e, user1, "members", `[{"target":"title","label":"Title","required":true}]`, `{}`)

	rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+mem+"/submissions", `{"values":{"title":"Hello"},"submitter":{"name":"The Boss","email":"boss@example.com"}}`, user1, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/projects/1/forms/%d/submissions", id), "", user1, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"submitter_name":"user1"`)
	assert.NotContains(t, rec.Body.String(), "Boss")
	assert.NotContains(t, rec.Body.String(), "boss@example.com")
}

func TestHumaForms_MultipartDataIsLimitedLikeTheJSONBody(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	config.FormsPublicAttachments.Set(true)
	defer config.FormsPublicAttachments.Set(false)
	_, token := newPublicForm(t, e, user1, "public", `[{"target":"title","label":"Title","required":true},{"target":"attachments","label":"Files"}]`, `{}`)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	// 300 KiB of unknown keys: the JSON route refuses this body with 413 (forms.maxbodysize is 256 KiB).
	var data strings.Builder
	data.WriteString(`{"values":{"title":"x"`)
	for i := 0; data.Len() < 300*1024; i++ {
		fmt.Fprintf(&data, `,"k%d":0`, i)
	}
	data.WriteString(`}}`)
	require.NoError(t, w.WriteField("data", data.String()))
	require.NoError(t, w.Close())
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/forms/"+token+"/submissions/multipart", buf.String(), "", w.FormDataContentType())
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "%d bytes answered", rec.Body.Len())
}

func TestHumaForms_Branding(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)

	body := `{"title":"Branded","access":"members","enabled":true,"brand_name":"Damla Yayinevi","accent_color":"#1973FF","hide_footer":true,"fields":[{"target":"title","label":"Title","required":true}]}`
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/1/forms", body, user1, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := formsDecode(t, rec)
	id := int64(created["id"].(float64))
	token := created["token"].(string)
	assert.Equal(t, "Damla Yayinevi", created["brand_name"])
	assert.Equal(t, "#1973ff", created["accent_color"])
	assert.Equal(t, true, created["hide_footer"])
	assert.Equal(t, false, created["has_brand_logo"])
	logoPath := fmt.Sprintf("/api/v2/projects/1/forms/%d/logo", id)

	multipartLogo := func(content []byte) (string, string) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		part, err := w.CreateFormFile("logo", "logo.png")
		require.NoError(t, err)
		_, _ = part.Write(content)
		require.NoError(t, w.Close())
		return buf.String(), w.FormDataContentType()
	}
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewNRGBA(image.Rect(0, 0, 30, 30))))

	t.Run("the definition names the branding, never the file", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token, "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		branding := formsDecode(t, rec)["branding"].(map[string]any)
		assert.ElementsMatch(t, []string{"name", "accent_color", "has_logo", "hide_footer"}, formsKeys(branding))
		assert.Equal(t, false, branding["has_logo"])
	})

	t.Run("no logo is a 404 on the public route", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token+"/logo", "", "", "").Code)
	})

	t.Run("a bad color is refused with a path", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPatch, fmt.Sprintf("/api/v2/projects/1/forms/%d", id), `{"accent_color":"nope"}`, user1, "application/merge-patch+json")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "accent_color")
	})

	t.Run("only project admins upload and remove", func(t *testing.T) {
		data, ct := multipartLogo(pngBytes.Bytes())
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodPut, logoPath, data, user2, ct).Code)
		assert.Equal(t, http.StatusUnauthorized, humaRequest(t, e, http.MethodPut, logoPath, data, "", ct).Code)
		assert.Equal(t, http.StatusForbidden, humaRequest(t, e, http.MethodDelete, logoPath, "", user2, "").Code)
	})

	t.Run("a file that is no image is a typed 400", func(t *testing.T) {
		for _, content := range []string{"hello", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`} {
			data, ct := multipartLogo([]byte(content))
			rec := humaRequest(t, e, http.MethodPut, logoPath, data, user1, ct)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.EqualValues(t, models.ErrCodeFormLogoInvalid, formsDecode(t, rec)["code"])
		}
		data, ct := multipartLogo(append(pngBytes.Bytes(), bytes.Repeat([]byte{0}, models.FormLogoMaxBytes)...))
		rec := humaRequest(t, e, http.MethodPut, logoPath, data, user1, ct)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "too_large")
	})

	t.Run("upload, serve by token without credentials, revalidate, remove", func(t *testing.T) {
		data, ct := multipartLogo(pngBytes.Bytes())
		rec := humaRequest(t, e, http.MethodPut, logoPath, data, user1, ct)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, true, formsDecode(t, rec)["has_brand_logo"])

		// The form needs a login, the logo does not: an image request carries no credentials.
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token+"/logo", "", "", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		assert.Empty(t, rec.Header().Values("Set-Cookie"))
		_, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, "png", format)
		etag := rec.Header().Get("Etag")
		require.NotEmpty(t, etag)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/forms/"+token+"/logo", nil)
		req.Header.Set("If-None-Match", etag)
		cached := httptest.NewRecorder()
		e.ServeHTTP(cached, req)
		assert.Equal(t, http.StatusNotModified, cached.Code)
		assert.Equal(t, "private, no-cache", rec.Header().Get("Cache-Control"))
		assert.NotContains(t, etag, "form-logo-", "the ETag does not show the file id")
		for _, header := range []string{`W/` + etag, `"other", ` + etag, `*`} {
			req = httptest.NewRequest(http.MethodGet, "/api/v2/forms/"+token+"/logo", nil)
			req.Header.Set("If-None-Match", header)
			cached = httptest.NewRecorder()
			e.ServeHTTP(cached, req)
			assert.Equal(t, http.StatusNotModified, cached.Code, header)
		}

		assert.Equal(t, http.StatusNotFound, humaRequest(t, e, http.MethodGet, "/api/v2/forms/not-a-token/logo", "", "", "").Code)

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token, "", user1, "")
		assert.Equal(t, true, formsDecode(t, rec)["branding"].(map[string]any)["has_logo"])

		rec = humaRequest(t, e, http.MethodDelete, logoPath, "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, false, formsDecode(t, rec)["has_brand_logo"])
		assert.Equal(t, http.StatusNotFound, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token+"/logo", "", "", "").Code)
	})

	t.Run("nothing is served or saved while forms are disabled", func(t *testing.T) {
		data, ct := multipartLogo(pngBytes.Bytes())
		rec := humaRequest(t, e, http.MethodPut, logoPath, data, user1, ct)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, http.StatusOK, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token+"/logo", "", "", "").Code)

		config.FormsEnabled.Set(false)
		defer config.FormsEnabled.Set(true)
		assert.Equal(t, http.StatusNotFound, humaRequest(t, e, http.MethodGet, "/api/v2/forms/"+token+"/logo", "", "", "").Code)
		rec = humaRequest(t, e, http.MethodPut, logoPath, data, user1, ct)
		assert.EqualValues(t, models.ErrCodeFormsDisabled, formsDecode(t, rec)["code"], rec.Body.String())
		rec = humaRequest(t, e, http.MethodDelete, logoPath, "", user1, "")
		assert.EqualValues(t, models.ErrCodeFormsDisabled, formsDecode(t, rec)["code"], rec.Body.String())
	})
}

type countingBody struct {
	r io.Reader
	n int
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// The body of a submission is only read for a form the caller may submit: a token that does not exist, a form for
// members seen by an anonymous caller and a closed form are answered before anything is buffered.
func TestHumaForms_BodyIsReadAfterTheFormIsKnown(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)
	fields := `[{"target":"title","label":"Title","required":true},{"target":"attachments","label":"Files"}]`
	config.FormsPublicAttachments.Set(true)
	t.Cleanup(func() { config.FormsPublicAttachments.Set(false) })
	_, pub := newPublicForm(t, e, user1, "public", fields, `{}`)
	_, mem := newPublicForm(t, e, user1, "members", fields, `{}`)
	_, closed := newPublicForm(t, e, user1, "members", fields, `{"close_date":"2020-01-01T00:00:00Z"}`)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	require.NoError(t, w.WriteField("data", `{"values":{"title":"x"}}`))
	part, err := w.CreateFormFile("files", "big.txt")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("a"), 512<<10))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	type route struct {
		path, contentType string
		body              []byte
	}
	routes := []route{
		{"/submissions/multipart", w.FormDataContentType(), buf.Bytes()},
		{"/submissions", "application/json", []byte(`{"values":{"title":"x"}}`)},
	}
	var current route // the subtests run one after the other
	send := func(path, token string) (*httptest.ResponseRecorder, int) {
		body := &countingBody{r: bytes.NewReader(current.body)}
		req := httptest.NewRequest(http.MethodPost, path, body)
		req.Header.Set("Content-Type", current.contentType)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec, body.n
	}
	for _, rt := range routes {
		current = rt
		route := rt.path
		t.Run("an unknown token", func(t *testing.T) {
			rec, read := send("/api/v2/forms/does-not-exist"+route, "")
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
			assert.EqualValues(t, models.ErrCodeFormDoesNotExist, formsDecode(t, rec)["code"])
			assert.Zero(t, read, "the body was read")
		})
		t.Run("a members form without a login", func(t *testing.T) {
			rec, read := send("/api/v2/forms/"+mem+route, "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
			assert.Zero(t, read, "the body was read")
		})
		t.Run("a members form for somebody who is no member", func(t *testing.T) {
			rec, read := send("/api/v2/forms/"+mem+route, user2)
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			assert.Zero(t, read, "the body was read")
		})
		t.Run("a closed members form tells an anonymous caller nothing", func(t *testing.T) {
			rec, read := send("/api/v2/forms/"+closed+route, "")
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
			assert.EqualValues(t, models.ErrCodeFormDoesNotExist, formsDecode(t, rec)["code"])
			assert.NotContains(t, rec.Body.String(), "Public form")
			assert.Zero(t, read, "the body was read")
		})
		t.Run("a closed members form tells a member why", func(t *testing.T) {
			rec, read := send("/api/v2/forms/"+closed+route, user1)
			assert.Equal(t, http.StatusGone, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), `"reason":"closed"`)
			assert.Zero(t, read, "the body was read")
		})
		t.Run("a form that may be submitted still reads and takes the body", func(t *testing.T) {
			rec, read := send("/api/v2/forms/"+pub+route, "")
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Positive(t, read)
		})
	}
}
