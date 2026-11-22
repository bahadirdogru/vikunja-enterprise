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
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"

	"code.vikunja.io/api/pkg/automation/outbound"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/utils"

	"code.dny.dev/ssrf"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// allowLoopback makes the outbound client reach the loopback listeners of httptest servers. It replaces the client
// factory (the production default is the SSRF-safe client of the webhooks) for the duration of the test only.
func allowLoopback(t *testing.T) {
	t.Helper()
	t.Cleanup(outbound.SetClientFactory(utils.NewUnguardedHTTPClient))
}

type recordedRequest struct {
	method, path, body string
	header             http.Header
}

// testServer records the requests it gets and answers with a fixed status and body.
type testServer struct {
	*httptest.Server
	url string

	mu       sync.Mutex
	requests []recordedRequest
	hits     atomic.Int32
	status   int
	body     string
	handler  http.HandlerFunc
}

func countingServer(t *testing.T, status int, body string) *testServer {
	t.Helper()
	ts := &testServer{status: status, body: body}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		ts.mu.Lock()
		ts.requests = append(ts.requests, recordedRequest{method: r.Method, path: r.URL.RequestURI(), body: string(raw), header: r.Header.Clone()})
		h := ts.handler
		ts.mu.Unlock()
		ts.hits.Add(1)
		if h != nil {
			h(w, r)
			return
		}
		w.WriteHeader(ts.status)
		_, _ = w.Write([]byte(ts.body))
	}))
	ts.url = ts.Server.URL
	t.Cleanup(ts.Close)
	return ts
}

func (ts *testServer) count() int32 { return ts.hits.Load() }

func (ts *testServer) reset() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.requests = nil
	ts.hits.Store(0)
}

func (ts *testServer) last(t *testing.T) recordedRequest {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	require.NotEmpty(t, ts.requests)
	return ts.requests[len(ts.requests)-1]
}

// setSecret stores a secret of a project as user1.
func setSecret(t *testing.T, projectID int64, name, value string) {
	t.Helper()
	mustCommit(t, func(s *xorm.Session) error {
		return (&models.AutomationSecret{ProjectID: projectID, Name: name, NewValue: value}).Update(s, user1)
	})
}

func countRows(t *testing.T, table string) int64 {
	t.Helper()
	s := newSession()
	defer s.Close()
	n, err := s.Table(table).Count()
	require.NoError(t, err)
	return n
}

// saveRule creates a rule as user1 through the model, like the API does.
func saveRule(t *testing.T, r *models.AutomationRule) error {
	t.Helper()
	return commit(t, func(s *xorm.Session) error { return r.Create(s, user1) })
}

func ruleIssues(t *testing.T, err error) []spec.Issue {
	t.Helper()
	require.Error(t, err)
	invalid, ok := err.(models.ErrAutomationRuleInvalid)
	require.True(t, ok, "expected an invalid rule error, got %T: %v", err, err)
	return invalid.Issues
}

// allowOnlyTestListeners lets the real dial-time guard accept the loopback listener of httptest servers
// (127.0.0.1) and nothing else.
func allowOnlyTestListeners(t *testing.T) {
	t.Helper()
	guard := ssrf.New(ssrf.WithAnyPort(), ssrf.WithAllowedV4Prefixes(netip.MustParsePrefix("127.0.0.1/32")))
	t.Cleanup(outbound.SetClientFactory(outbound.ClientWithGuard(guard)))
}
