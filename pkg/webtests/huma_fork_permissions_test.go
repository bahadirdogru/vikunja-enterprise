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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/license"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"
	"code.vikunja.io/api/pkg/user"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The permission matrix of the fork's endpoints: every route of the six modules, called by every kind of caller
// against one project, and the answer compared with what docs/modules/*.md promise. The callers are an anonymous
// request, a stranger, members with read, write and admin access and the owner, link shares with read, write and admin
// permission, API tokens of an admin and of a reader with exactly the scope of the route, a token with the scope of
// another group, a token that holds only the read permissions of the route's group, the token of a bot that is an
// admin of the project, and a JWT of a disabled account.
//
// Every endpoint has at least one caller that must get through and one that must not; a caller that must not is
// compared with the exact status, so a 404 that hides a thing and a 403 that admits it exist are told apart.

const (
	roleAnon     = -2
	roleStranger = -1
	roleReader   = 0
	roleWriter   = 1
	roleAdmin    = 2
	roleOwner    = 3
)

type matrixCaller struct {
	name string
	// kind is jwt, share, token or anon.
	kind string
	// role is the project role of a jwt or token caller; for a share it is the permission of the share.
	role  int
	token string
	// denied is set for callers that are refused whatever the route (a disabled account).
	denied bool
	// wrongScope marks a token that holds a scope for another group of routes, readOnly one that holds only the read
	// permissions of the route's group.
	wrongScope bool
	readOnly   bool
}

type matrixEndpoint struct {
	name         string
	method, path string
	body         string
	// min is the project role the route needs: roleStranger for "any signed-in user".
	min int
	// share is the permission a link share needs, -1 when link shares never get through.
	share int
	// ok are the statuses of the first caller that gets through; later callers only must not be refused.
	ok []int
	// deny is the status of a refused signed-in caller (403 when zero); denyStranger the one of a stranger.
	deny, denyStranger int
	// only restricts who gets through to the named callers; everybody else is refused with the deny status.
	only []string
}

type matrixWorld struct {
	e       *echo.Echo
	project int64
	ids     map[string]string
	callers []*matrixCaller
}

func (w *matrixWorld) expand(s string) string {
	for k, v := range w.ids {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

var matrixPathParam = regexp.MustCompile(`\{[^}]+\}|:[^/]+`)

func matrixShape(path string) string {
	path = strings.TrimPrefix(path, "/api/v2")
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	return matrixPathParam.ReplaceAllString(path, ":")
}

func (w *matrixWorld) mustDo(t *testing.T, method, path, body, token string, want int) []byte {
	t.Helper()
	rec := humaRequest(t, w.e, method, path, body, token, "")
	require.Equal(t, want, rec.Code, "%s %s: %s", method, path, rec.Body.String())
	return rec.Body.Bytes()
}

func idOf(t *testing.T, raw []byte) string {
	t.Helper()
	var v struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(raw, &v), string(raw))
	require.NotZero(t, v.ID, string(raw))
	return strconv.FormatInt(v.ID, 10)
}

func newMatrixWorld(t *testing.T) *matrixWorld {
	t.Helper()
	e, err := setupTestEnv()
	require.NoError(t, err)
	w := &matrixWorld{e: e, ids: map[string]string{}}

	// The time tracking report answers 404 while the feature is off, and other tests leave every feature off.
	license.SetForTests([]license.Feature{license.FeatureTimeTracking})
	t.Cleanup(license.ResetForTests)

	jwt := func(id int64) string {
		s := db.NewSession()
		defer s.Close()
		u, err := user.GetUserByID(s, id)
		require.NoError(t, err)
		return humaTokenFor(t, u)
	}
	owner := jwt(1)

	w.project, _ = strconv.ParseInt(idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/projects", `{"title":"matrix"}`, owner, http.StatusCreated)), 10, 64)
	w.ids["P"] = strconv.FormatInt(w.project, 10)

	// user2 reads, user3 writes, user6 administers, user17 (disabled) and the bot 23 administer; user10 is a stranger.
	s := db.NewSession()
	for uid, perm := range map[int64]models.Permission{2: models.PermissionRead, 3: models.PermissionWrite, 6: models.PermissionAdmin, 17: models.PermissionAdmin, 23: models.PermissionAdmin} {
		_, err = s.Insert(&models.ProjectUser{UserID: uid, ProjectID: w.project, Permission: perm})
		require.NoError(t, err)
	}
	shares := map[int]*models.LinkSharing{}
	for perm, name := range map[int]string{0: "matrixread", 1: "matrixwrite", 2: "matrixadmin"} {
		share := &models.LinkSharing{Hash: name, ProjectID: w.project, Permission: models.Permission(perm), SharingType: models.SharingTypeWithoutPassword, SharedByID: 1}
		_, err = s.Insert(share)
		require.NoError(t, err)
		shares[perm] = share
	}
	require.NoError(t, s.Commit())
	s.Close()

	shareJWT := func(perm int) string {
		tok, err := auth.NewLinkShareJWTAuthtoken(shares[perm])
		require.NoError(t, err)
		return tok
	}
	w.callers = []*matrixCaller{
		{name: "anonymous", kind: "anon", role: roleAnon},
		{name: "stranger", kind: "jwt", role: roleStranger, token: jwt(10)},
		{name: "reader", kind: "jwt", role: roleReader, token: jwt(2)},
		{name: "writer", kind: "jwt", role: roleWriter, token: jwt(3)},
		{name: "admin", kind: "jwt", role: roleAdmin, token: jwt(6)},
		{name: "owner", kind: "jwt", role: roleOwner, token: owner},
		{name: "share-read", kind: "share", role: 0, token: shareJWT(0)},
		{name: "share-write", kind: "share", role: 1, token: shareJWT(1)},
		{name: "share-admin", kind: "share", role: 2, token: shareJWT(2)},
		{name: "disabled-account", kind: "jwt", role: roleAdmin, token: humaTokenFor(t, &user.User{ID: 17, Username: "user17", Issuer: "local"}), denied: true},
	}

	// The entities the routes work on, made by the owner.
	w.ids["T"] = idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/projects/"+w.ids["P"]+"/tasks", `{"title":"matrix task"}`, owner, http.StatusCreated))
	w.ids["F"] = idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/projects/"+w.ids["P"]+"/custom-fields", `{"title":"Note","type":"text"}`, owner, http.StatusCreated))
	w.ids["F2"] = idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/projects/"+w.ids["P"]+"/custom-fields", `{"title":"Other","type":"text"}`, owner, http.StatusCreated))
	w.ids["F3"] = idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/projects/"+w.ids["P"]+"/custom-fields", `{"title":"Doomed","type":"text"}`, owner, http.StatusCreated))
	w.mustDo(t, http.MethodPut, "/api/v2/tasks/"+w.ids["T"]+"/custom-field-values/"+w.ids["F"], `{"value":"hello"}`, owner, http.StatusOK)

	w.mustDo(t, http.MethodPut, "/api/v2/projects/"+w.ids["P"]+"/workflow",
		`{"restrict_transitions":false,"statuses":[{"id":-1,"title":"Todo","category":"open","is_initial":true},{"id":-2,"title":"Doing","category":"in_progress"},{"id":-3,"title":"Done","category":"done"}],"transitions":[]}`,
		owner, http.StatusOK)
	doc := w.mustDo(t, http.MethodGet, "/api/v2/projects/"+w.ids["P"]+"/workflow", "", owner, http.StatusOK)
	w.ids["WORKFLOW"] = string(doc)
	var wf struct {
		Statuses []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"statuses"`
	}
	require.NoError(t, json.Unmarshal(doc, &wf))
	for _, st := range wf.Statuses {
		w.ids["STATUS_"+strings.ToUpper(st.Title)] = strconv.FormatInt(st.ID, 10)
	}

	const rule = `{"title":"Urgent first","enabled":true,"trigger":{"type":"task.created"},"conditions":{"field":"title","op":"like","value":"urgent"},"actions":[{"type":"task.set_field","params":{"field":"priority","value":5}}]}`
	w.ids["RULE"] = rule
	w.ids["RUN"] = "999999"
	w.ids["SN"] = "OTHER"
	w.ids["SK"] = "KEY"
	w.ids["R"] = idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/projects/"+w.ids["P"]+"/automations", rule, owner, http.StatusCreated))
	w.ids["RM"] = idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/projects/"+w.ids["P"]+"/automations",
		`{"title":"Button","enabled":true,"trigger":{"type":"manual","options":{"label":"Go"}},"actions":[{"type":"task.set_field","params":{"field":"priority","value":2}}]}`, owner, http.StatusCreated))
	w.mustDo(t, http.MethodPut, "/api/v2/projects/"+w.ids["P"]+"/automation-secrets/KEY", `{"value":"s3cret"}`, owner, http.StatusOK)

	form := w.mustDo(t, http.MethodPost, "/api/v2/projects/"+w.ids["P"]+"/forms",
		`{"title":"Request","access":"public","enabled":true,"fields":[{"target":"title","label":"Title","required":true}],"settings":{}}`, owner, http.StatusCreated)
	w.ids["FORM"] = idOf(t, form)
	w.ids["FORMDOC"] = string(form)

	w.ids["D"] = idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/dashboards", `{"title":"Ops","project_id":`+w.ids["P"]+`,"widgets":[{"type":"text","title":"note","x":0,"y":0,"w":6,"h":4,"config":{"text":"hi"}}]}`, owner, http.StatusCreated))

	// Two approval requests: A is read, cancelled and listed, A2 is decided by its one approver (user6).
	for _, k := range []string{"A", "A2"} {
		w.ids[k] = idOf(t, w.mustDo(t, http.MethodPost, "/api/v2/tasks/"+w.ids["T"]+"/approvals",
			`{"title":"Check `+k+`","approver_ids":[6]}`, owner, http.StatusCreated))
	}
	return w
}

// scopeOf finds the (group, permission) pairs of the token table that authorize a route.
func scopeOf(method, path string) (group string, perm string, other []string) {
	shape := matrixShape(path)
	for g, perms := range models.GetAPITokenRoutes() {
		for p, rd := range perms {
			if rd.Method == method && strings.HasPrefix(rd.Path, "/api/v2/") && matrixShape(rd.Path) == shape {
				group, perm = g, p
			}
		}
	}
	if group != "" {
		for p, rd := range models.GetAPITokenRoutes()[group] {
			if p != perm && (strings.HasPrefix(p, "read") || rd.Method == http.MethodGet) {
				other = append(other, p)
			}
		}
	}
	return group, perm, other
}

func (w *matrixWorld) issueToken(t *testing.T, ownerID int64, perms models.APIPermissions) string {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	caller, err := user.GetUserByID(s, ownerID)
	require.NoError(t, err)
	tok := &models.APIToken{Title: "matrix", APIPermissions: perms, ExpiresAt: time.Now().AddDate(1, 0, 0)}
	if ownerID == 23 {
		caller, err = user.GetUserByID(s, 21)
		require.NoError(t, err)
		tok.OwnerID = 23
	}
	require.NoError(t, tok.Create(s, caller))
	require.NoError(t, s.Commit())
	return tok.Token
}

func matrixEndpoints() []matrixEndpoint {
	P := "/projects/{P}"
	return []matrixEndpoint{
		// custom fields
		{name: "custom-field list", method: "GET", path: P + "/custom-fields", min: roleReader, share: 0, ok: []int{200}},
		{name: "custom-field read", method: "GET", path: P + "/custom-fields/{F}", min: roleReader, share: 0, ok: []int{200}},
		{name: "custom-field create", method: "POST", path: P + "/custom-fields", body: `{"title":"New","type":"text"}`, min: roleAdmin, share: -1, ok: []int{201}},
		{name: "custom-field update", method: "PUT", path: P + "/custom-fields/{F2}", body: `{"title":"Renamed","type":"text"}`, min: roleAdmin, share: -1, ok: []int{200}},
		{name: "custom-field delete", method: "DELETE", path: P + "/custom-fields/{F3}", min: roleAdmin, share: -1, ok: []int{200, 204}},
		{name: "custom-field values list", method: "GET", path: "/tasks/{T}/custom-field-values", min: roleReader, share: 0, ok: []int{200}},
		{name: "custom-field value set", method: "PUT", path: "/tasks/{T}/custom-field-values/{F}", body: `{"value":"again"}`, min: roleWriter, share: 1, ok: []int{200}},
		{name: "custom-field value clear", method: "DELETE", path: "/tasks/{T}/custom-field-values/{F2}", min: roleWriter, share: 1, ok: []int{200, 204}},

		// workflow
		{name: "workflow read", method: "GET", path: P + "/workflow", min: roleReader, share: 0, ok: []int{200}},
		{name: "workflow replace", method: "PUT", path: P + "/workflow", body: "{WORKFLOW}", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "task status read", method: "GET", path: "/tasks/{T}/status", min: roleReader, share: 0, ok: []int{200}},
		{name: "task status change", method: "POST", path: "/tasks/{T}/status", body: `{"to_status_id":{STATUS_DOING}}`, min: roleWriter, share: 1, ok: []int{200}},
		{name: "task status history", method: "GET", path: "/tasks/{T}/status/history", min: roleReader, share: 0, ok: []int{200}},

		// approvals: link shares have no access at all
		{name: "approvals mine", method: "GET", path: "/approvals", min: roleStranger, share: -1, ok: []int{200}},
		{name: "approval read", method: "GET", path: "/approvals/{A}", min: roleReader, share: -1, ok: []int{200}},
		{name: "task approvals list", method: "GET", path: "/tasks/{T}/approvals", min: roleReader, share: -1, ok: []int{200}},
		{name: "approval request ad hoc", method: "POST", path: "/tasks/{T}/approvals", body: `{"title":"Again","approver_ids":[6]}`, min: roleWriter, share: -1, ok: []int{201}},
		// only an approver decides (the owner requested it), and a caller who cannot read the task is told it does not exist
		{name: "approval decide", method: "POST", path: "/approvals/{A2}/decision", body: `{"decision":"approved"}`, min: roleAdmin, share: -1, ok: []int{200},
			only: []string{"admin", "token-admin"}, denyStranger: 404},
		// the requester or a project admin cancels
		{name: "approval cancel", method: "POST", path: "/approvals/{A}/cancel", body: `{}`, min: roleAdmin, share: -1, ok: []int{200}, denyStranger: 404,
			only: []string{"admin", "owner", "token-admin", "token-bot"}},

		// automation: project admins only, never a link share
		{name: "automation catalog", method: "GET", path: "/automations/catalog", min: roleStranger, share: -3, ok: []int{200}},
		{name: "automation rules list", method: "GET", path: P + "/automations", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "automation rule read", method: "GET", path: P + "/automations/{R}", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "automation rule create", method: "POST", path: P + "/automations", body: "{RULE}", min: roleAdmin, share: -1, ok: []int{201}},
		{name: "automation rule update", method: "PUT", path: P + "/automations/{R}", body: "{RULE}", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "automation rule test", method: "POST", path: P + "/automations/{R}/test", body: `{"task_id":{T}}`, min: roleAdmin, share: -1, ok: []int{200}},
		// running a manual rule needs write on the task, the list of them write on the project (docs/modules/automation.md)
		{name: "automation manual list", method: "GET", path: P + "/manual-automations", min: roleWriter, share: -1, ok: []int{200}},
		{name: "automation rule run", method: "POST", path: P + "/automations/{RM}/run", body: `{"task_id":{T}}`, min: roleWriter, share: -1, ok: []int{200, 202}},
		{name: "automation webhook token", method: "POST", path: P + "/automations/{R}/webhook-token", min: roleAdmin, share: -1, ok: []int{400, 409}},
		{name: "automation runs list", method: "GET", path: P + "/automations/{R}/runs", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "automation run read", method: "GET", path: P + "/automations/{R}/runs/{RUN}", min: roleAdmin, share: -1, ok: []int{404}},
		{name: "automation run retry", method: "POST", path: P + "/automations/{R}/runs/{RUN}/retry", min: roleAdmin, share: -1, ok: []int{404}},
		{name: "automation secrets list", method: "GET", path: P + "/automation-secrets", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "automation secret set", method: "PUT", path: P + "/automation-secrets/{SN}", body: `{"value":"v"}`, min: roleAdmin, share: -1, ok: []int{200, 201}},
		{name: "automation secret delete", method: "DELETE", path: P + "/automation-secrets/{SK}", min: roleAdmin, share: -1, ok: []int{200, 204}},

		// dashboards and reports
		{name: "dashboards list", method: "GET", path: "/dashboards", min: roleStranger, share: -3, ok: []int{200}},
		{name: "dashboard read", method: "GET", path: "/dashboards/{D}", min: roleReader, share: 0, ok: []int{200}},
		{name: "dashboard create", method: "POST", path: "/dashboards", body: `{"title":"New","project_id":{P}}`, min: roleAdmin, share: -1, ok: []int{201}},
		{name: "dashboard replace", method: "PUT", path: "/dashboards/{D}", body: `{"title":"Ops 2"}`, min: roleAdmin, share: -1, ok: []int{200}},
		{name: "dashboard layout", method: "PUT", path: "/dashboards/{D}/layout", body: `{"items":[]}`, min: roleAdmin, share: -1, ok: []int{200}},
		{name: "report tasks", method: "POST", path: "/reports/tasks", body: `{"project_id":{P},"group_by":"done"}`, min: roleReader, share: 0, ok: []int{200}},
		{name: "report tasks export", method: "POST", path: "/reports/tasks/export", body: `{"project_id":{P},"group_by":"done"}`, min: roleReader, share: 0, ok: []int{200}},
		{name: "report tasks rows", method: "POST", path: "/reports/tasks/export/rows", body: `{"project_id":{P}}`, min: roleReader, share: 0, ok: []int{200}},
		{name: "report options", method: "GET", path: "/reports/tasks/options?project_id={P}", min: roleReader, share: 0, ok: []int{200}},
		{name: "report status time", method: "POST", path: "/reports/status-time", body: `{"project_id":{P}}`, min: roleReader, share: 0, ok: []int{200}},
		{name: "report workload", method: "POST", path: "/reports/workload", body: `{"project_id":{P}}`, min: roleReader, share: 0, ok: []int{200}},
		{name: "report time tracking", method: "POST", path: "/reports/time-tracking", body: `{"project_id":{P}}`, min: roleReader, share: -1, ok: []int{200}, denyStranger: 404},

		// forms: project admins, never a link share
		{name: "form options", method: "GET", path: P + "/forms/options", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "forms list", method: "GET", path: P + "/forms", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "form read", method: "GET", path: P + "/forms/{FORM}", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "form create", method: "POST", path: P + "/forms", body: `{"title":"Second","access":"public","enabled":true,"fields":[{"target":"title","label":"Title","required":true}],"settings":{}}`, min: roleAdmin, share: -1, ok: []int{201}},
		{name: "form update", method: "PUT", path: P + "/forms/{FORM}", body: "{FORMDOC}", min: roleAdmin, share: -1, ok: []int{200}},
		{name: "form token", method: "POST", path: P + "/forms/{FORM}/token", min: roleAdmin, share: -1, ok: []int{200, 201}},
		{name: "form logo remove", method: "DELETE", path: P + "/forms/{FORM}/logo", min: roleAdmin, share: -1, ok: []int{200, 204, 404}},
		{name: "form submissions", method: "GET", path: P + "/forms/{FORM}/submissions", min: roleAdmin, share: -1, ok: []int{200}},

		// destructive, last
		{name: "workflow delete", method: "DELETE", path: P + "/workflow", min: roleAdmin, share: -1, ok: []int{200, 204}},
		{name: "dashboard delete", method: "DELETE", path: "/dashboards/{D}", min: roleAdmin, share: -1, ok: []int{200, 204}},
		{name: "automation rule delete", method: "DELETE", path: P + "/automations/{R}", min: roleAdmin, share: -1, ok: []int{200, 204}},
		{name: "form delete", method: "DELETE", path: P + "/forms/{FORM}", min: roleAdmin, share: -1, ok: []int{200, 204}},
	}
}

// allowed says whether a caller is meant to get through.
func (ep *matrixEndpoint) allowed(c *matrixCaller) bool {
	if c.denied || c.kind == "anon" {
		return false
	}
	if len(ep.only) > 0 && !slicesContains(ep.only, c.name) {
		return false
	}
	switch c.kind {
	case "share":
		return ep.share >= 0 && c.role >= ep.share
	default:
		return c.role >= ep.min
	}
}

func slicesContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestHumaForkPermissionMatrix(t *testing.T) {
	w := newMatrixWorld(t)
	dump := os.Getenv("MATRIX_DUMP") != ""

	for _, ep := range matrixEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			path := w.expand("/api/v2" + ep.path)
			body := w.expand(ep.body)
			group, perm, readPerms := scopeOf(ep.method, ep.path)
			if group == "" {
				t.Fatalf("%s %s is in no API token scope", ep.method, ep.path)
			}

			callers := append([]*matrixCaller{}, w.callers...)
			// API tokens: an admin and a reader with exactly the scope of the route, an admin with another group's
			// scope, an admin that holds only the read permissions of the group, and the bot.
			callers = append(callers,
				&matrixCaller{name: "token-admin", kind: "token", role: roleAdmin, token: w.issueToken(t, 6, models.APIPermissions{group: {perm}})},
				&matrixCaller{name: "token-reader", kind: "token", role: roleReader, token: w.issueToken(t, 2, models.APIPermissions{group: {perm}})},
				&matrixCaller{name: "token-other-scope", kind: "token", role: roleAdmin, wrongScope: true, token: w.issueToken(t, 6, models.APIPermissions{"labels": {"read_all"}})},
				&matrixCaller{name: "token-bot", kind: "token", role: roleAdmin, token: w.issueToken(t, 23, models.APIPermissions{group: {perm}})},
			)
			if ep.method != http.MethodGet && len(readPerms) > 0 {
				callers = append(callers, &matrixCaller{name: "token-read-only", kind: "token", role: roleAdmin, readOnly: true, token: w.issueToken(t, 6, models.APIPermissions{group: readPerms})})
			}

			var allowedCallers, deniedCallers []*matrixCaller
			for _, c := range callers {
				if ep.share == -3 && c.kind == "share" {
					continue // what a share gets here is not promised
				}
				if ep.allowed(c) && !c.wrongScope && !c.readOnly {
					allowedCallers = append(allowedCallers, c)
				} else {
					deniedCallers = append(deniedCallers, c)
				}
			}
			require.NotEmpty(t, allowedCallers, "an endpoint without a caller that gets through tests nothing")

			// The refused callers first: an allowed caller of a destructive route changes what the others see.
			for _, c := range deniedCallers {
				rec := humaRequest(t, w.e, ep.method, path, body, c.token, "")
				if dump {
					fmt.Printf("MATRIX %-28s %-18s %d\n", ep.name, c.name, rec.Code)
				}
				switch {
				case c.kind == "anon" || c.denied:
					assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, rec.Code, "%s as %s: %s", ep.name, c.name, rec.Body.String())
				case c.wrongScope || c.readOnly:
					assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, rec.Code, "%s as %s: %s", ep.name, c.name, rec.Body.String())
				case c.role == roleStranger && ep.denyStranger != 0:
					assert.Equal(t, ep.denyStranger, rec.Code, "%s as %s: %s", ep.name, c.name, rec.Body.String())
				default:
					want := ep.deny
					if want == 0 {
						want = http.StatusForbidden
					}
					assert.Equal(t, want, rec.Code, "%s as %s: %s", ep.name, c.name, rec.Body.String())
				}
				assert.NotContains(t, []int{500, 502, 503}, rec.Code, "%s as %s: %s", ep.name, c.name, rec.Body.String())
			}

			for i, c := range allowedCallers {
				rec := humaRequest(t, w.e, ep.method, path, body, c.token, "")
				if dump {
					fmt.Printf("MATRIX %-28s %-18s %d\n", ep.name, c.name, rec.Code)
				}
				assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden, 500, 502, 503}, rec.Code, "%s as %s: %s", ep.name, c.name, rec.Body.String())
				if i == 0 {
					assert.Contains(t, ep.ok, rec.Code, "%s, first caller %s: %s", ep.name, c.name, rec.Body.String())
				}
			}
		})
	}
}

// docs/modules/automation.md: "Whoever cannot write in the project gets the same 403 whether the project, the rule or
// the task exist." Every route of the automation module asks for admin (or, for the manual run, write) access first, so
// the answer to a caller without it must not depend on what exists.
func TestHumaAutomationRefusalsDoNotTellWhatExists(t *testing.T) {
	w := newMatrixWorld(t)
	P := w.ids["P"]
	const missing = "99999999"

	probes := []struct{ name, method, path, body string }{
		{"run, project missing", http.MethodPost, "/projects/" + missing + "/automations/" + w.ids["RM"] + "/run", `{"task_id":` + w.ids["T"] + `}`},
		{"run, rule missing", http.MethodPost, "/projects/" + P + "/automations/" + missing + "/run", `{"task_id":` + w.ids["T"] + `}`},
		{"run, task missing", http.MethodPost, "/projects/" + P + "/automations/" + w.ids["RM"] + "/run", `{"task_id":` + missing + `}`},
		{"run, everything exists", http.MethodPost, "/projects/" + P + "/automations/" + w.ids["RM"] + "/run", `{"task_id":` + w.ids["T"] + `}`},
		{"manual list, project missing", http.MethodGet, "/projects/" + missing + "/manual-automations", ""},
		{"manual list", http.MethodGet, "/projects/" + P + "/manual-automations", ""},
		{"list, project missing", http.MethodGet, "/projects/" + missing + "/automations", ""},
		{"list", http.MethodGet, "/projects/" + P + "/automations", ""},
		{"read, rule missing", http.MethodGet, "/projects/" + P + "/automations/" + missing, ""},
		{"read", http.MethodGet, "/projects/" + P + "/automations/" + w.ids["R"], ""},
		{"update, rule missing", http.MethodPut, "/projects/" + P + "/automations/" + missing, w.ids["RULE"]},
		{"delete, rule missing", http.MethodDelete, "/projects/" + P + "/automations/" + missing, ""},
		{"test, rule missing", http.MethodPost, "/projects/" + P + "/automations/" + missing + "/test", `{"task_id":` + w.ids["T"] + `}`},
		{"runs, rule missing", http.MethodGet, "/projects/" + P + "/automations/" + missing + "/runs", ""},
		{"retry, run missing", http.MethodPost, "/projects/" + P + "/automations/" + w.ids["R"] + "/runs/" + missing + "/retry", ""},
		{"secrets, project missing", http.MethodGet, "/projects/" + missing + "/automation-secrets", ""},
		{"secret delete, secret missing", http.MethodDelete, "/projects/" + P + "/automation-secrets/NOPE", ""},
	}
	for _, name := range []string{"stranger", "reader"} {
		var c *matrixCaller
		for _, cc := range w.callers {
			if cc.name == name {
				c = cc
			}
		}
		require.NotNil(t, c)
		for _, p := range probes {
			// A reader may not write, so the manual run and the list are refused like everything else; only the
			// project's own writers and admins get past the first check.
			rec := humaRequest(t, w.e, p.method, "/api/v2"+p.path, p.body, c.token, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, "%s as %s: %s", p.name, name, rec.Body.String())
		}
	}
}
