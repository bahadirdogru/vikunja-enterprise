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

// Package outbound sends the HTTP requests of automation rules (the http.request action and http.fetch in
// scripts). There is one code path: the SSRF-protected client Vikunja uses for webhooks.
//
// The protection is the one of the webhooks: utils.NewSSRFSafeHTTPClient refuses to connect to addresses that are
// not globally routable (loopback, private, link-local, the cloud metadata address, ...) unless the administrator
// set outgoingrequests.allownonroutableips, and honours outgoingrequests.proxyurl. The check happens when the
// connection is made (a net.Dialer Control function looks at the address that is actually dialled), not on a
// name that was resolved earlier, so DNS rebinding does not help, and it runs again for every redirect because
// every redirect dials a new connection.
package outbound

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/utils"

	"code.dny.dev/ssrf"
)

// UserAgent is sent unless the caller sets its own.
const UserAgent = "Vikunja-Automation"

// MaxRedirects is how many redirects are followed.
const MaxRedirects = 3

// SignatureHeader carries the HMAC of the body.
const SignatureHeader = "X-Vikunja-Signature"

var clientFactory = utils.NewSSRFSafeHTTPClient

// SetClientFactory replaces the client constructor and returns a function that restores the previous one. It is
// meant for tests, which need a server on the loopback interface to be reachable; production code never calls it.
func SetClientFactory(f func() *http.Client) (restore func()) {
	previous := clientFactory
	clientFactory = f
	return func() { clientFactory = previous }
}

// ClientWithGuard builds a client factory that uses the given guard instead of the default one. Tests use it to
// allow exactly one test listener and keep every other address blocked, so the real dial-time check is still what
// stops a redirect to a private address.
func ClientWithGuard(guard *ssrf.Guardian) func() *http.Client {
	return func() *http.Client {
		client := utils.NewUnguardedHTTPClient()
		transport := client.Transport.(*http.Transport)
		dialer := &netDialer{guard: guard}
		transport.DialContext = dialer.dial
		transport.Proxy = nil
		return client
	}
}

// Request is an outgoing HTTP request.
type Request struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
	// Timeout is the wish of the caller; it is capped by automation.scripts.httptimeout.
	Timeout time.Duration
	// MaxResponse is the number of body bytes that are read.
	MaxResponse int
	// MaxRequest is the largest allowed request body.
	MaxRequest int
}

// Response is the answer.
type Response struct {
	Status int
	// Headers has lower-case names; repeated headers are joined with ", ".
	Headers   map[string]string
	Body      []byte
	Truncated bool
}

// BlockedError is returned when the guard refused the address.
type BlockedError struct{ Err error }

// CodeBlocked is the code of a refused address, see InvalidError.Code.
const CodeBlocked = "address_blocked"

func (e *BlockedError) Error() string {
	return "the address is not allowed (private, loopback, link-local or otherwise not publicly routable)"
}
func (e *BlockedError) Unwrap() error { return e.Err }

// InvalidError is a request that cannot be sent as it is (bad URL, scheme, header, too large). It is a failure of
// the rule, not a temporary one.
type InvalidError struct {
	Msg string
	// Code names the sentence for a client that translates it, Params are the values it mentions.
	Code   string
	Params map[string]string
}

func (e *InvalidError) Error() string { return e.Msg }

func invalid(code string, params map[string]string, format string, args ...any) error {
	return &InvalidError{Msg: fmt.Sprintf(format, args...), Code: code, Params: params}
}

// ValidateURL checks what can be checked without connecting: the scheme and the presence of a host.
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, invalid("url_invalid", nil, "the URL is not valid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, invalid("url_scheme", nil, "only http and https URLs are allowed")
	}
	if u.Hostname() == "" {
		return nil, invalid("url_no_host", nil, "the URL has no host")
	}
	if u.User != nil {
		return nil, invalid("url_credentials", nil, "credentials in the URL are not allowed, use a header")
	}
	if targetsOwnHooks(u) {
		return nil, invalid("url_own_webhook", nil, "the URL is an incoming webhook of this server: a rule cannot start rules through a request")
	}
	return u, nil
}

// hookPathSegment is where incoming automation webhooks live (see routes/api/v2/automation_hooks.go), possibly
// behind a path prefix of a reverse proxy.
const hookPathSegment = "/api/v2/automation/hooks/"

// MarkerHeader is set on every request the engine sends. This instance refuses API requests that carry it, which is
// what keeps a rule from calling the API of this server (the hook endpoint, the task API) and so from creating work
// that no run, origin or depth accounts for. Unlike the URL check it does not depend on how the server is addressed.
// It cannot see a proxy that strips the header, or a second hop through an external service that calls back without it.
const MarkerHeader = "X-Vikunja-Automation-Depth"

// markerValue is the value of the marker header.
const markerValue = "1"

// targetsOwnHooks is a cheap first line in front of MarkerHeader: it reports whether the URL looks like an incoming
// webhook of this instance, whatever the scheme, port, letter case, trailing dot of the host or path prefix. Without a
// public URL there is no host to compare, so the path alone decides.
func targetsOwnHooks(u *url.URL) bool {
	p := strings.ToLower(path.Clean("/" + u.Path))
	if !strings.Contains(p+"/", hookPathSegment) {
		return false
	}
	own, err := url.Parse(strings.TrimSpace(config.ServicePublicURL.GetString()))
	if err != nil || own.Hostname() == "" {
		return true
	}
	return normalizeHost(u.Hostname()) == normalizeHost(own.Hostname())
}

func normalizeHost(h string) string {
	return strings.TrimRight(strings.ToLower(h), ".")
}

// Sign returns the value of the signature header: sha256= followed by the hex HMAC-SHA256 of the body, keyed with
// the secret. It signs the body alone. The signature of the incoming webhook trigger (timestamp, hook token and
// body) is a different construction, so a rule cannot call a signed incoming hook of another instance directly.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Do sends the request.
func Do(ctx context.Context, r *Request) (*Response, error) {
	u, err := ValidateURL(r.URL)
	if err != nil {
		return nil, err
	}
	method := strings.ToUpper(r.Method)
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead:
	default:
		return nil, invalid("method_not_allowed", map[string]string{"method": r.Method}, "the method %q is not allowed", r.Method)
	}
	if r.MaxRequest > 0 && len(r.Body) > r.MaxRequest {
		return nil, invalid("body_too_large", map[string]string{"max": strconv.Itoa(r.MaxRequest)}, "the request body is larger than %d bytes", r.MaxRequest)
	}

	timeout := config.AutomationHTTPTimeout.GetDuration()
	if r.Timeout > 0 && (timeout <= 0 || r.Timeout < timeout) {
		timeout = r.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var body io.Reader
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}
	u.RawQuery = escapeQuery(u.RawQuery)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, invalid("request_invalid", nil, "the request is not valid")
	}
	req.Header.Set("User-Agent", UserAgent)
	userHeaders := map[string]bool{}
	for k, v := range r.Headers {
		if err := checkHeader(k, v); err != nil {
			return nil, err
		}
		req.Header.Set(k, v)
		userHeaders[http.CanonicalHeaderKey(k)] = true
	}
	// After the headers of the rule: it cannot remove or change the marker.
	req.Header.Set(MarkerHeader, markerValue)

	client := clientFactory()
	// The client is built for this request alone: do not leave its idle connections behind.
	defer client.CloseIdleConnections()
	client.Timeout = timeout
	origin := u.Host
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > MaxRedirects {
			return invalid("too_many_redirects", map[string]string{"max": strconv.Itoa(MaxRedirects)}, "more than %d redirects", MaxRedirects)
		}
		if next.URL.Scheme != "http" && next.URL.Scheme != "https" {
			return invalid("redirect_scheme", nil, "redirect to a URL that is not http or https")
		}
		if targetsOwnHooks(next.URL) {
			return invalid("redirect_own_webhook", nil, "redirect to an incoming webhook of this server")
		}
		if next.URL.Host != origin {
			// Whatever the rule put in the headers was meant for the first host only.
			for h := range userHeaders {
				next.Header.Del(h)
			}
		}
		next.Header.Set(MarkerHeader, markerValue)
		return nil
	}

	resp, err := client.Do(req) //nolint:gosec // the client is the SSRF-safe one
	if err != nil {
		return nil, classify(err)
	}
	defer resp.Body.Close()

	limit := int64(r.MaxResponse)
	if limit <= 0 {
		limit = 1 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, classify(err)
	}
	out := &Response{Status: resp.StatusCode, Headers: map[string]string{}}
	if int64(len(data)) > limit {
		data, out.Truncated = data[:limit], true
	}
	out.Body = data
	for k, v := range resp.Header {
		out.Headers[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return out, nil
}

func checkHeader(name, value string) error {
	for _, c := range name + value {
		if c == '\r' || c == '\n' || c == 0 {
			return invalid("header_line_break", map[string]string{"header": name}, "the header %q contains a line break", name)
		}
	}
	switch strings.ToLower(name) {
	case "host", "content-length", "transfer-encoding", "connection", "upgrade":
		return invalid("header_not_allowed", map[string]string{"header": name}, "the header %q cannot be set", name)
	}
	return nil
}

// classify turns a transport error into a BlockedError when the guard stopped it.
func classify(err error) error {
	if errors.Is(err, ssrf.ErrProhibitedIP) || errors.Is(err, ssrf.ErrProhibitedPort) || errors.Is(err, ssrf.ErrProhibitedNetwork) {
		return &BlockedError{Err: err}
	}
	var inv *InvalidError
	if errors.As(err, &inv) {
		return inv
	}
	return err
}

// escapeQuery percent-encodes the bytes of a query that cannot be sent as they are (spaces, control characters,
// non-ASCII), so a URL assembled from text still is a valid request line. Valid escapes are kept.
func escapeQuery(q string) string {
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', strings.IndexByte("-._~!$&'()*+,;=:@/?%[]", c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
