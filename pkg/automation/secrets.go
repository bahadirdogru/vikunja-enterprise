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
	"encoding/base64"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"code.vikunja.io/api/pkg/models"

	"xorm.io/xorm"
)

// minUnreadMask is the shortest secret that is masked when the run did not ask for it. Masking a one or two
// character value would damage every output; a secret the run did read is masked whatever its length.
const minUnreadMask = 4

const maskedMarker = "***"

// secretStore gives a step the secrets of its project. It loads them once, decrypted, and remembers which ones
// were read, so the masker knows what must not appear in the stored output.
type secretStore struct {
	s         *xorm.Session
	projectID int64
	loaded    bool
	values    map[string]*models.AutomationSecretValue
	accessed  map[string]bool
}

func (e *Env) store() *secretStore {
	if e.secrets == nil {
		e.secrets = &secretStore{s: e.S, projectID: e.Rule.ProjectID, accessed: map[string]bool{}}
	}
	return e.secrets
}

func (st *secretStore) load() error {
	if st.loaded {
		return nil
	}
	values, err := models.LoadAutomationSecrets(st.s, st.projectID)
	if err != nil {
		return err
	}
	st.values, st.loaded = values, true
	return nil
}

// get returns the value of a secret and marks it as read.
func (st *secretStore) get(name string) (string, error) {
	if err := st.load(); err != nil {
		return "", err
	}
	v, ok := st.values[name]
	if !ok {
		return "", wrapActionErr(models.ErrAutomationSecretDoesNotExist{Name: name})
	}
	if v.Err != nil {
		return "", wrapActionErr(v.Err)
	}
	st.accessed[name] = true
	return v.Value, nil
}

// exists reports whether a secret exists and can be read, without marking it as read.
func (st *secretStore) exists(name string) error {
	if err := st.load(); err != nil {
		return err
	}
	v, ok := st.values[name]
	if !ok {
		return wrapActionErr(models.ErrAutomationSecretDoesNotExist{Name: name})
	}
	if v.Err != nil {
		return wrapActionErr(v.Err)
	}
	return nil
}

// masker replaces secret values in text.
type masker struct {
	needles []string
}

// newMasker covers every secret the run read and, for robustness, every other readable secret of the project (when
// it is long enough to be masked without damaging the text). Besides the plain value it covers the forms a value
// takes when it is URL-encoded, base64-encoded or put in JSON.
func (st *secretStore) masker() *masker {
	set := map[string]bool{}
	add := func(v string) {
		if v != "" {
			set[v] = true
		}
	}
	for name, v := range st.values {
		if v.Err != nil || v.Value == "" {
			continue
		}
		if len(v.Value) < minUnreadMask && !st.accessed[name] {
			continue
		}
		add(v.Value)
		add(url.QueryEscape(v.Value))
		add(url.PathEscape(v.Value))
		add(base64.StdEncoding.EncodeToString([]byte(v.Value)))
		add(base64.RawStdEncoding.EncodeToString([]byte(v.Value)))
		add(base64.URLEncoding.EncodeToString([]byte(v.Value)))
		add(base64.RawURLEncoding.EncodeToString([]byte(v.Value)))
		if raw, err := json.Marshal(v.Value); err == nil && len(raw) > 2 {
			add(string(raw[1 : len(raw)-1]))
		}
	}
	m := &masker{}
	for n := range set {
		m.needles = append(m.needles, n)
	}
	// Longest first, so a value that contains another is replaced as a whole.
	sort.Slice(m.needles, func(i, j int) bool {
		if len(m.needles[i]) != len(m.needles[j]) {
			return len(m.needles[i]) > len(m.needles[j])
		}
		return m.needles[i] < m.needles[j]
	})
	return m
}

// mask returns the text with every secret value replaced.
func (m *masker) mask(text string) string {
	if m == nil {
		return text
	}
	for _, n := range m.needles {
		if strings.Contains(text, n) {
			text = strings.ReplaceAll(text, n, maskedMarker)
		}
	}
	return text
}

// masked wraps an error so its text has the secrets removed while the cause stays reachable for errors.As.
type maskedError struct {
	msg string
	err error
	// code and params are those of err with the secrets removed from the values.
	code   string
	params map[string]string
}

func (e *maskedError) Error() string { return e.msg }
func (e *maskedError) Unwrap() error { return e.err }

// maskOutput masks text with the secrets of the step, when it used any.
func (e *Env) maskOutput(text string) string {
	if e.secrets == nil || !e.secrets.loaded {
		return text
	}
	return e.secrets.masker().mask(text)
}

// maskErr masks the message of an error with the secrets of the step, when it used any.
func (e *Env) maskErr(err error) error {
	if err == nil || e.secrets == nil || !e.secrets.loaded {
		return err
	}
	msg := err.Error()
	code, params := errorMessageRaw(err)
	m := e.secrets.masker()
	changed := m.mask(msg) != msg
	var maskedParams map[string]string
	if params != nil {
		maskedParams = make(map[string]string, len(params))
		for k, v := range params {
			maskedParams[k] = m.mask(v)
			changed = changed || maskedParams[k] != v
		}
	}
	if changed {
		return &maskedError{msg: m.mask(msg), err: err, code: code, params: maskedParams}
	}
	return err
}
