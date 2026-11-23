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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/outbound"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func httpAction(params map[string]any) *spec.Action {
	return act(spec.ActionHTTPRequest, params)
}

func runHTTP(t *testing.T, params map[string]any) (*models.AutomationRun, []*models.AutomationRunStep) {
	t.Helper()
	_, run, steps := runAction(t, projectOwned, user1, user1, httpAction(params))
	return run, steps
}

func TestHTTPRequest_SendsTheTemplatedRequestWithASignature(t *testing.T) {
	reset(t)
	allowLoopback(t)
	srv := countingServer(t, 200, `{"ok":true}`)
	setSecret(t, projectOwned, "CHAT_TOKEN", "tok-123")
	setSecret(t, projectOwned, "SIGN", "signing-key")

	run, steps := runHTTP(t, map[string]any{
		"url":     srv.url + "/hook/{{project.title}}?t={{task.title}}",
		"method":  "POST",
		"headers": map[string]any{"Authorization": "Bearer {{secrets.CHAT_TOKEN}}", "X-Doer": "{{event.doer.username}}"},
		"body":    `{"text": "created {{task.title}}", "who": "{{event.doer.name}}"}`,
		"secret":  "SIGN",
	})
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	require.EqualValues(t, 1, srv.count())

	req := srv.last(t)
	assert.Equal(t, "POST", req.method)
	assert.Equal(t, "/hook/Test1?t=Original%20title", req.path)
	assert.JSONEq(t, `{"text": "created Original title", "who": "user1"}`, req.body)
	assert.Equal(t, "Bearer tok-123", req.header.Get("Authorization"))
	assert.Equal(t, "user1", req.header.Get("X-Doer"))
	assert.Equal(t, "application/json", req.header.Get("Content-Type"))
	assert.Equal(t, "Vikunja-Automation", req.header.Get("User-Agent"))

	mac := hmac.New(sha256.New, []byte("signing-key"))
	mac.Write([]byte(req.body))
	assert.Equal(t, "sha256="+hex.EncodeToString(mac.Sum(nil)), req.header.Get("X-Vikunja-Signature"), "HMAC-SHA256 of the exact body")

	require.Len(t, steps, 1)
	assert.Contains(t, steps[0].Output, "POST "+srv.url)
	assert.Contains(t, steps[0].Output, "200 OK")
	assert.Contains(t, steps[0].Output, `{"ok":true}`)
	assert.NotContains(t, steps[0].Output, "tok-123")
	assert.NotContains(t, steps[0].Output, "signing-key")

	t.Run("method defaults to POST and a plain body is text", func(t *testing.T) {
		srv.reset()
		run, _ := runHTTP(t, map[string]any{"url": srv.url + "/x", "body": "plain {{task.title}}"})
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		req := srv.last(t)
		assert.Equal(t, "POST", req.method)
		assert.Equal(t, "text/plain; charset=utf-8", req.header.Get("Content-Type"))
		assert.Empty(t, req.header.Get("X-Vikunja-Signature"), "no secret, no signature")
	})

	t.Run("GET without a body", func(t *testing.T) {
		srv.reset()
		run, _ := runHTTP(t, map[string]any{"url": srv.url + "/x", "method": "GET"})
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Equal(t, "GET", srv.last(t).method)
		assert.Empty(t, srv.last(t).body)
	})

	t.Run("a header of the rule wins over the defaults", func(t *testing.T) {
		srv.reset()
		run, _ := runHTTP(t, map[string]any{"url": srv.url + "/x", "body": "{}", "headers": map[string]any{"Content-Type": "application/x-custom", "User-Agent": "mine"}})
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Equal(t, "application/x-custom", srv.last(t).header.Get("Content-Type"))
		assert.Equal(t, "mine", srv.last(t).header.Get("User-Agent"))
	})

	t.Run("a task title with a line break cannot inject headers", func(t *testing.T) {
		reset(t)
		allowLoopback(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, httpAction(map[string]any{
			"url": srv.url + "/x", "headers": map[string]any{"X-Title": "{{task.title}}"}})))
		newTask(t, projectOwned, "line\r\nInjected: yes", user1)
		srv.reset()
		drain(t)
		run := allRuns(t, r.ID)[0]
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, run.Error, "line break")
		assert.EqualValues(t, 0, srv.count())
	})
}

func TestHTTPRequest_RetryClassification(t *testing.T) {
	reset(t)
	allowLoopback(t)

	t.Run("4xx fails the run for good", func(t *testing.T) {
		reset(t)
		srv := countingServer(t, 404, "no such hook")
		run, steps := runHTTP(t, map[string]any{"url": srv.url + "/x"})
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.Contains(t, steps[0].Error, "404")
		assert.Contains(t, steps[0].Error, "no such hook")
		assert.Equal(t, models.AutomationEventDone, allEvents(t)[len(allEvents(t))-1].Status)
		assert.EqualValues(t, 1, srv.count())
	})

	t.Run("5xx is retried with backoff and the step is not recorded as done", func(t *testing.T) {
		reset(t)
		retryBase = time.Hour
		advance := fakeClock(t)
		srv := countingServer(t, 503, "try later")
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, httpAction(map[string]any{"url": srv.url + "/x"})))
		newTask(t, projectOwned, "t", user1)
		drain(t)
		runs := allRuns(t, r.ID)
		require.Len(t, runs, 1)
		assert.Equal(t, models.AutomationRunRunning, runs[0].Status, "a temporary failure leaves the run resumable")
		assert.Contains(t, runs[0].Error, "503")
		evs := allEvents(t)
		assert.Equal(t, models.AutomationEventPending, evs[len(evs)-1].Status)
		assert.EqualValues(t, 1, srv.count())

		// The service recovers; the retry sends the request again and the run completes.
		srv.mu.Lock()
		srv.status, srv.body = 200, "fine"
		srv.mu.Unlock()
		advance(time.Hour)
		drain(t)
		assert.Equal(t, models.AutomationRunSuccess, allRuns(t, r.ID)[0].Status)
		assert.EqualValues(t, 2, srv.count())
	})

	t.Run("a network error is temporary", func(t *testing.T) {
		reset(t)
		retryBase = time.Hour
		fakeClock(t)
		srv := countingServer(t, 200, "")
		url := srv.url
		srv.Close()
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, httpAction(map[string]any{"url": url + "/x"})))
		newTask(t, projectOwned, "t", user1)
		drain(t)
		assert.Equal(t, models.AutomationRunRunning, allRuns(t, r.ID)[0].Status)
	})

	t.Run("attempts run out", func(t *testing.T) {
		reset(t)
		retryBase = time.Hour
		advance := fakeClock(t)
		config.AutomationMaxAttempts.Set(2)
		srv := countingServer(t, 500, "boom")
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, httpAction(map[string]any{"url": srv.url + "/x"})))
		newTask(t, projectOwned, "t", user1)
		drain(t)
		advance(time.Hour)
		drain(t)
		assert.Equal(t, models.AutomationRunFailed, allRuns(t, r.ID)[0].Status)
		assert.EqualValues(t, 2, srv.count())
	})
}

func TestHTTPRequest_SSRFProtectionOfTheProductionClient(t *testing.T) {
	reset(t)
	srv := countingServer(t, 200, "reached")
	_, port, _ := strings.Cut(strings.TrimPrefix(srv.url, "http://"), ":")

	for name, target := range map[string]string{
		"127.0.0.1":  srv.url + "/",
		"localhost":  "http://localhost:" + port + "/",
		"ipv6":       "http://[::1]:" + port + "/",
		"10.x":       "http://10.0.0.7/",
		"metadata":   "http://169.254.169.254/latest/meta-data/",
		"link local": "http://169.254.1.1/",
	} {
		t.Run(name, func(t *testing.T) {
			reset(t)
			run, steps := runHTTP(t, map[string]any{"url": target})
			assert.Equal(t, models.AutomationRunFailed, run.Status, "permanent: the rule asks for something that is not allowed")
			require.Len(t, steps, 1)
			assert.Contains(t, steps[0].Error, "not allowed")
			assert.Equal(t, models.AutomationEventDone, allEvents(t)[len(allEvents(t))-1].Status)
		})
	}
	assert.EqualValues(t, 0, srv.count(), "the guard stopped every request before it reached the listener")

	t.Run("the dry run does not send anything either", func(t *testing.T) {
		reset(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, httpAction(map[string]any{"url": srv.url + "/x"})))
		tk := newTask(t, projectOwned, "t", user1)
		res := dryRun(t, r, tk.ID)
		assert.Contains(t, res.Steps[0].Summary, "send POST "+srv.url)
		assert.EqualValues(t, 0, srv.count())
	})

	t.Run("a script cannot reach private addresses either", func(t *testing.T) {
		reset(t)
		_, run, steps := runScript(t, `
			var out = {};
			["`+srv.url+`/", "http://localhost:`+port+`/", "http://[::1]:`+port+`/", "http://169.254.169.254/", "http://10.0.0.1/"].forEach(function (u) {
				try { http.fetch(u, {timeout: 2000}); out[u] = "reached" } catch (e) { out[u] = e.message }
			});
			return out`)
		config.AutomationScriptsMaxHTTPRequests.Set(5)
		if run.Status != models.AutomationRunSuccess {
			t.Fatalf("run failed: %s", run.Error)
		}
		out := steps[0].Output
		assert.NotContains(t, out, "reached")
		assert.Equal(t, 5, strings.Count(out, "the address is not allowed"))
		assert.EqualValues(t, 0, srv.count())
	})

	t.Run("the administrator may allow private addresses", func(t *testing.T) {
		reset(t)
		config.OutgoingRequestsAllowNonRoutableIPs.Set(true)
		defer config.OutgoingRequestsAllowNonRoutableIPs.Set(false)
		run, _ := runHTTP(t, map[string]any{"url": srv.url + "/x"})
		assert.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.EqualValues(t, 1, srv.count())
	})
}

func TestHTTPFetch_InScripts(t *testing.T) {
	reset(t)
	allowLoopback(t)
	srv := countingServer(t, 200, `{"id": 7, "items": [1, 2, 3]}`)
	srv.mu.Lock()
	srv.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Reply", "yes")
		if r.URL.Path == "/big" {
			fmt.Fprint(w, strings.Repeat("a", 5000))
			return
		}
		w.WriteHeader(201)
		fmt.Fprint(w, `{"id": 7, "items": [1, 2, 3]}`)
	}
	srv.mu.Unlock()

	_, run, steps := runScript(t, `
		var r = http.fetch("`+srv.url+`/api", {method: "PUT", headers: {"X-Mine": "1"}, body: JSON.stringify({task: task.title})});
		var data = r.json();
		return {status: r.status, ok: r.ok, reply: r.headers["x-reply"], id: data.id, n: data.items.length, truncated: r.truncated}`)
	require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
	assert.Contains(t, steps[0].Output, `result: {"status":201,"ok":true,"reply":"yes","id":7,"n":3,"truncated":false}`)
	req := srv.last(t)
	assert.Equal(t, "PUT", req.method)
	assert.JSONEq(t, `{"task":"Original title"}`, req.body)
	assert.Equal(t, "1", req.header.Get("X-Mine"))
	assert.Equal(t, "Vikunja-Automation", req.header.Get("User-Agent"))
	assert.Empty(t, req.header.Get("Authorization"), "no credentials of Vikunja")

	t.Run("response size cap", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsMaxResponseSize.Set(1000)
		_, run, steps := runScript(t, `var r = http.fetch("`+srv.url+`/big"); return [r.body.length, r.truncated]`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, steps[0].Output, "result: [1000,true]")
	})

	t.Run("request size cap", func(t *testing.T) {
		reset(t)
		config.AutomationScriptsMaxResponseSize.Set(100)
		_, run, steps := runScript(t, `try { http.fetch("`+srv.url+`/", {method: "POST", body: "x".repeat(500)}) } catch (e) { return e.message }`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, steps[0].Output, "longer than 100 bytes")
	})

	t.Run("an error status does not throw", func(t *testing.T) {
		reset(t)
		srv2 := countingServer(t, 418, "teapot")
		_, run, steps := runScript(t, `var r = http.fetch("`+srv2.url+`/"); return [r.status, r.ok, r.body]`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, steps[0].Output, `[418,false,"teapot"]`)
	})
}

func TestHTTPRequest_RedirectsToPrivateAddressesAreStopped(t *testing.T) {
	reset(t)
	// The test listener is allowed by an injected guard; everything else stays blocked by the real dial-time check.
	srv := countingServer(t, 200, "x")
	redirector := countingServer(t, 200, "")
	redirector.mu.Lock()
	redirector.handler = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}
	redirector.mu.Unlock()
	allowOnlyTestListeners(t)

	run, steps := runHTTP(t, map[string]any{"url": redirector.url + "/go", "method": "GET"})
	assert.Equal(t, models.AutomationRunFailed, run.Status)
	assert.Contains(t, steps[0].Error, "not allowed")
	assert.EqualValues(t, 1, redirector.count())
	_ = srv

	_, run2, steps2 := runScript(t, `try { http.fetch("`+redirector.url+`/go") } catch (e) { return e.message }`)
	require.Equal(t, models.AutomationRunSuccess, run2.Status, run2.Error)
	assert.Contains(t, steps2[0].Output, "not allowed")
}

func TestSecrets_AreMaskedInOutputAndErrors(t *testing.T) {
	const value = "s3cr3t-value-XYZ"
	resetWithSecrets := func(t *testing.T) {
		t.Helper()
		reset(t)
		allowLoopback(t)
		setSecret(t, projectOwned, "KEY", value)
		setSecret(t, projectOwned, "OTHER", "another-hidden-one")
	}

	t.Run("log of a secret", func(t *testing.T) {
		resetWithSecrets(t)
		_, run, steps := runScript(t, `log(secrets.get("KEY")); log("prefix " + secrets.get("KEY") + " suffix"); return secrets.get("KEY")`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.NotContains(t, steps[0].Output, value)
		assert.Contains(t, steps[0].Output, "***")
		assert.Contains(t, steps[0].Output, "prefix *** suffix")
		assert.Contains(t, steps[0].Output, `result: "***"`)
	})

	t.Run("a thrown secret", func(t *testing.T) {
		resetWithSecrets(t)
		_, run, steps := runScript(t, `throw new Error("failed with " + secrets.get("KEY"))`)
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.NotContains(t, steps[0].Error, value)
		assert.NotContains(t, run.Error, value)
		assert.Contains(t, steps[0].Error, "failed with ***")
	})

	t.Run("encodings of the secret", func(t *testing.T) {
		resetWithSecrets(t)
		var run *models.AutomationRun
		var steps []*models.AutomationRunStep
		_, run, steps = runScript(t, `
			var v = secrets.get("KEY");
			log(JSON.stringify({v: v}));
			log(encodeURIComponent(v + " &"));
			`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.NotContains(t, steps[0].Output, value)
		assert.NotContains(t, steps[0].Output, "s3cr3t-value-XYZ")
		b64 := base64.StdEncoding.EncodeToString([]byte(value))
		srv := countingServer(t, 200, "echo: "+value+" b64 "+b64+" other: another-hidden-one")
		_, run, steps = runScript(t, `var r = http.fetch("`+srv.url+`/"); secrets.get("KEY"); return r.body`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.NotContains(t, steps[0].Output, value)
		assert.NotContains(t, steps[0].Output, b64)
		assert.NotContains(t, steps[0].Output, "another-hidden-one", "every secret of the project is masked, not only the ones read")
	})

	t.Run("an answer that echoes the secret", func(t *testing.T) {
		resetWithSecrets(t)
		srv := countingServer(t, 200, "you sent "+value)
		run, steps := runHTTP(t, map[string]any{"url": srv.url + "/x", "headers": map[string]any{"X-Key": "{{secrets.KEY}}"}})
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.NotContains(t, steps[0].Output, value)
		assert.Contains(t, steps[0].Output, "you sent ***")

		srv = countingServer(t, 400, "bad key "+value)
		run, steps = runHTTP(t, map[string]any{"url": srv.url + "/x", "headers": map[string]any{"X-Key": "{{secrets.KEY}}"}})
		assert.Equal(t, models.AutomationRunFailed, run.Status)
		assert.NotContains(t, steps[0].Error, value)
		assert.NotContains(t, run.Error, value)
		assert.Contains(t, steps[0].Error, "bad key ***")
	})

	t.Run("a temporarily failing request does not store the secret in the run either", func(t *testing.T) {
		resetWithSecrets(t)
		retryBase = time.Hour
		fakeClock(t)
		srv := countingServer(t, 502, "gateway saw "+value)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, httpAction(map[string]any{"url": srv.url + "/x", "body": "{{secrets.KEY}}"})))
		newTask(t, projectOwned, "t", user1)
		drain(t)
		run := allRuns(t, r.ID)[0]
		assert.Equal(t, models.AutomationRunRunning, run.Status)
		assert.NotContains(t, run.Error, value)
		for _, ev := range allEvents(t) {
			assert.NotContains(t, ev.LastError, value)
		}
		for _, st := range runSteps(t, run.ID) {
			assert.NotContains(t, st.Error, value)
			assert.NotContains(t, st.Output, value)
		}
	})

	t.Run("the dry run shows a placeholder and masks anyway", func(t *testing.T) {
		resetWithSecrets(t)
		r := addRule(t, projectOwned, user1, rule(spec.TriggerTaskCreated, nil, nil, scriptAction(`log(secrets.get("KEY")); return secrets.get("OTHER")`)))
		tk := newTask(t, projectOwned, "t", user1)
		res := dryRun(t, r, tk.ID)
		require.Empty(t, res.Steps[0].Error)
		assert.Contains(t, res.Steps[0].Summary, "[secret KEY]")
		assert.NotContains(t, res.Steps[0].Summary, value)
	})

	t.Run("secrets that do not exist or cannot be read", func(t *testing.T) {
		resetWithSecrets(t)
		_, run, steps := runScript(t, `try { secrets.get("MISSING_ONE") } catch (e) { return e.message }`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, steps[0].Output, `the secret \"MISSING_ONE\" does not exist`)

		_, run, _ = runScript(t, `secrets.get("not a name")`)
		assert.Equal(t, models.AutomationRunFailed, run.Status)

		// A value that was stored with another service.secret is unreadable.
		s := newSession()
		_, err := s.Table("automation_secrets").Where("name = ?", "OTHER").Cols("value").Update(&models.AutomationSecret{Value: "v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		_, run, steps = runScript(t, `try { return secrets.get("OTHER") } catch (e) { return e.message }`)
		require.Equal(t, models.AutomationRunSuccess, run.Status, run.Error)
		assert.Contains(t, steps[0].Output, "cannot be decrypted")
		assert.Contains(t, steps[0].Output, "service.secret changed")
	})
}

func TestMasker(t *testing.T) {
	st := &secretStore{
		loaded:   true,
		accessed: map[string]bool{"SHORT": true},
		values: map[string]*models.AutomationSecretValue{
			"LONG":   {Name: "LONG", Value: "longer-secret-value"},
			"INSIDE": {Name: "INSIDE", Value: "secret-value"},
			"SHORT":  {Name: "SHORT", Value: "ab1"},
			"UNUSED": {Name: "UNUSED", Value: "xy"},
			"BAD":    {Name: "BAD", Err: fmt.Errorf("unreadable")},
		},
	}
	m := st.masker()
	assert.Equal(t, "a *** b", m.mask("a longer-secret-value b"), "the longest value is replaced as a whole")
	assert.Equal(t, "***/***", m.mask("secret-value/secret-value"))
	assert.Equal(t, "x ***", m.mask("x ab1"), "a short secret that was read is masked")
	assert.Equal(t, "xy stays", m.mask("xy stays"), "a short secret that was not read would damage the text")
	assert.Equal(t, "nothing here", m.mask("nothing here"))
	assert.Empty(t, (*masker)(nil).mask(""))
	var nilMasker *masker
	assert.Equal(t, "unchanged", nilMasker.mask("unchanged"))

	env := &Env{secrets: st}
	err := env.maskErr(&ActionError{Msg: "failed: longer-secret-value"})
	assert.Equal(t, "failed: ***", err.Error())
	var actionErr *ActionError
	require.ErrorAs(t, err, &actionErr, "the type survives masking, so the retry classification does")
	assert.True(t, isPermanent(err))
	require.NoError(t, env.maskErr(nil))
	same := fmt.Errorf("nothing secret")
	assert.Same(t, same, env.maskErr(same))
	assert.Equal(t, "plain", (&Env{}).maskOutput("plain"), "a step that used no secrets is not touched")
	_ = outbound.UserAgent
}
