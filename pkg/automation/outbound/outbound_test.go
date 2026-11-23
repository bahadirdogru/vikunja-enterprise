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

package outbound

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/utils"

	"code.dny.dev/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	config.InitDefaultConfig()
	m.Run()
}

// allowLoopbackListener lets the production guard accept exactly the address of a test listener (127.0.0.1) and
// nothing else: 127.0.0.2, 10.x, 169.254.x and ::1 stay blocked, and the check still runs when the connection is
// made, for every redirect.
func allowLoopbackListener(t *testing.T) {
	t.Helper()
	guard := ssrf.New(ssrf.WithAnyPort(), ssrf.WithAllowedV4Prefixes(netip.MustParsePrefix("127.0.0.1/32")))
	t.Cleanup(SetClientFactory(ClientWithGuard(guard)))
}

func TestDo_DefaultGuardBlocksNonRoutableAddresses(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	defer srv.Close()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)

	for name, url := range map[string]string{
		"ipv4 loopback":     srv.URL,
		"localhost":         "http://localhost:" + port + "/",
		"ipv6 loopback":     "http://[::1]:" + port + "/",
		"10.x":              "http://10.0.0.5:" + port + "/",
		"172.16.x":          "http://172.16.1.1/",
		"192.168.x":         "http://192.168.1.1/",
		"metadata":          "http://169.254.169.254/latest/meta-data/",
		"link local v6":     "http://[fe80::1]/",
		"unspecified":       "http://0.0.0.0:" + port + "/",
		"carrier grade nat": "http://100.64.0.1/",
		"ipv4 mapped v6":    "http://[::ffff:127.0.0.1]:" + port + "/",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Do(context.Background(), &Request{URL: url, Method: "GET", Timeout: 3 * time.Second})
			require.Error(t, err)
			var blocked *BlockedError
			assert.ErrorAs(t, err, &blocked, "%v", err)
		})
	}
	assert.EqualValues(t, 0, hits.Load(), "nothing reached the listener")

	t.Run("a decimal address is not an address and fails", func(t *testing.T) {
		_, err := Do(context.Background(), &Request{URL: "http://2130706433:" + port + "/", Timeout: 3 * time.Second})
		require.Error(t, err)
	})

	t.Run("the administrator can allow private addresses", func(t *testing.T) {
		config.OutgoingRequestsAllowNonRoutableIPs.Set(true)
		defer config.OutgoingRequestsAllowNonRoutableIPs.Set(false)
		resp, err := Do(context.Background(), &Request{URL: srv.URL, Method: "GET"})
		require.NoError(t, err)
		assert.Equal(t, 200, resp.Status)
	})
}

func TestDo_OnlyHTTPAndHTTPS(t *testing.T) {
	for _, url := range []string{"ftp://example.com/x", "file:///etc/passwd", "gopher://example.com", "javascript:alert(1)", "http://", "//example.com", "example.com/x", "http://user:pass@example.com/"} {
		_, err := Do(context.Background(), &Request{URL: url})
		var inv *InvalidError
		require.ErrorAs(t, err, &inv, url)
	}
	_, err := Do(context.Background(), &Request{URL: "https://example.com/", Method: "CONNECT"})
	var inv *InvalidError
	assert.ErrorAs(t, err, &inv)
}

func TestDo_RequestAndResponse(t *testing.T) {
	allowLoopbackListener(t)
	var got struct {
		method, ua, ct, custom, body, sig, auth string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, _ := r.Body.Read(buf)
		got.method, got.ua, got.ct, got.custom, got.body = r.Method, r.UserAgent(), r.Header.Get("Content-Type"), r.Header.Get("X-Custom"), string(buf[:n])
		got.sig, got.auth = r.Header.Get(SignatureHeader), r.Header.Get("Authorization")
		w.Header().Add("X-Multi", "a")
		w.Header().Add("X-Multi", "b")
		w.WriteHeader(201)
		fmt.Fprint(w, "created")
	}))
	defer srv.Close()

	resp, err := Do(context.Background(), &Request{
		URL: srv.URL + "/path?q=1", Method: "post", Body: []byte(`{"a":1}`),
		Headers: map[string]string{"X-Custom": "v", "Content-Type": "application/json"},
	})
	require.NoError(t, err)
	assert.Equal(t, 201, resp.Status)
	assert.Equal(t, "created", string(resp.Body))
	assert.Equal(t, "a, b", resp.Headers["x-multi"])
	assert.Equal(t, "POST", got.method)
	assert.Equal(t, "Vikunja-Automation", got.ua)
	assert.Equal(t, "application/json", got.ct)
	assert.Equal(t, "v", got.custom)
	assert.Equal(t, `{"a":1}`, got.body)
	assert.Empty(t, got.auth, "no credentials of Vikunja are ever sent")

	t.Run("a caller can set its own user agent", func(t *testing.T) {
		_, err := Do(context.Background(), &Request{URL: srv.URL, Method: "GET", Headers: map[string]string{"User-Agent": "mine"}})
		require.NoError(t, err)
		assert.Equal(t, "mine", got.ua)
	})

	t.Run("headers with line breaks or forbidden names are refused", func(t *testing.T) {
		for _, h := range []map[string]string{{"X": "a\r\nInjected: 1"}, {"X\nY": "a"}, {"Host": "evil"}, {"Content-Length": "1"}, {"Transfer-Encoding": "chunked"}} {
			_, err := Do(context.Background(), &Request{URL: srv.URL, Headers: h})
			var inv *InvalidError
			assert.ErrorAs(t, err, &inv, "%v", h)
		}
	})
}

func TestDo_ResponseAndRequestSizeCaps(t *testing.T) {
	allowLoopbackListener(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 5000))
	}))
	defer srv.Close()

	resp, err := Do(context.Background(), &Request{URL: srv.URL, MaxResponse: 1000})
	require.NoError(t, err)
	assert.Len(t, resp.Body, 1000)
	assert.True(t, resp.Truncated)

	resp, err = Do(context.Background(), &Request{URL: srv.URL, MaxResponse: 5000})
	require.NoError(t, err)
	assert.Len(t, resp.Body, 5000)
	assert.False(t, resp.Truncated, "a body of exactly the limit is complete")

	_, err = Do(context.Background(), &Request{URL: srv.URL, Method: "POST", Body: []byte(strings.Repeat("y", 101)), MaxRequest: 100})
	var inv *InvalidError
	assert.ErrorAs(t, err, &inv)
}

func TestDo_Redirects(t *testing.T) {
	allowLoopbackListener(t)
	var hops atomic.Int32
	var leaked atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("X-Secret-Token"))
		fmt.Fprint(w, "arrived")
	})
	mux.HandleFunc("/chain/", func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/chain/%d", &n); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if n == 0 {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/chain/%d", n-1), http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("up to three redirects are followed", func(t *testing.T) {
		resp, err := Do(context.Background(), &Request{URL: srv.URL + "/chain/2"})
		require.NoError(t, err)
		assert.Equal(t, "arrived", string(resp.Body))
	})
	t.Run("more are refused", func(t *testing.T) {
		_, err := Do(context.Background(), &Request{URL: srv.URL + "/chain/5"})
		var inv *InvalidError
		require.ErrorAs(t, err, &inv)
		assert.Contains(t, err.Error(), "redirects")
	})
	t.Run("a redirect to another scheme is refused", func(t *testing.T) {
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "ftp://example.com/x", http.StatusFound)
		}))
		defer other.Close()
		_, err := Do(context.Background(), &Request{URL: other.URL})
		require.Error(t, err)
	})
	t.Run("a redirect to a private address is stopped when the connection is made", func(t *testing.T) {
		for name, target := range map[string]string{
			"other loopback address": "http://127.0.0.2:1/",
			"private":                "http://10.1.2.3/",
			"metadata":               "http://169.254.169.254/latest/meta-data/",
			"ipv6 loopback":          "http://[::1]:1/",
		} {
			t.Run(name, func(t *testing.T) {
				redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, target, http.StatusFound)
				}))
				defer redirector.Close()
				_, err := Do(context.Background(), &Request{URL: redirector.URL})
				var blocked *BlockedError
				require.ErrorAs(t, err, &blocked, "%v", err)
			})
		}
	})
	t.Run("headers meant for the first host are not sent to another host", func(t *testing.T) {
		// 127.0.0.1 and localhost are different hosts for the redirect check; only the first is allowed, so use a
		// second allowed listener address that differs by port.
		target := httptest.NewServer(mux)
		defer target.Close()
		redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/ok", http.StatusFound)
		}))
		defer redirector.Close()
		leaked.Store("")
		_, err := Do(context.Background(), &Request{URL: redirector.URL, Headers: map[string]string{"X-Secret-Token": "abc"}})
		require.NoError(t, err)
		assert.Empty(t, leaked.Load(), "the header does not follow a redirect to another host")
	})
}

func TestDo_TimeoutIsCapped(t *testing.T) {
	allowLoopbackListener(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	config.AutomationHTTPTimeout.Set(150 * time.Millisecond)
	defer config.AutomationHTTPTimeout.Set(10 * time.Second)
	started := time.Now()
	_, err := Do(context.Background(), &Request{URL: srv.URL, Timeout: time.Hour})
	require.Error(t, err)
	assert.Less(t, time.Since(started), 3*time.Second)

	t.Run("the caller's context ends the request too", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, err := Do(ctx, &Request{URL: srv.URL})
		require.Error(t, err)
		require.ErrorIs(t, err, context.DeadlineExceeded, "%v", err)
		assert.Less(t, time.Since(started), 3*time.Second)
	})
}

func TestSign(t *testing.T) {
	body := []byte(`{"text":"hello"}`)
	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	assert.Equal(t, want, Sign("topsecret", body))
	assert.NotEqual(t, want, Sign("other", body))
	assert.NotEqual(t, want, Sign("topsecret", append(body, ' ')))
	// The known answer (HMAC-SHA256, key "key"), so a change of the algorithm is noticed.
	assert.Equal(t, "sha256=f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8", Sign("key", []byte("The quick brown fox jumps over the lazy dog")))
}

func TestUnguardedFactoryIsNotTheDefault(t *testing.T) {
	// The default must be the SSRF-safe client of the webhooks; tests replace it only through SetClientFactory.
	restore := SetClientFactory(utils.NewUnguardedHTTPClient)
	restore()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	_, err := Do(context.Background(), &Request{URL: srv.URL})
	var blocked *BlockedError
	assert.ErrorAs(t, err, &blocked)
}
