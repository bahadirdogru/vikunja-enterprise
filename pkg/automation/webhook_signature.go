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
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"

	"xorm.io/xorm"
)

// The signature of an incoming webhook call covers the timestamp and the body:
//
//	X-Vikunja-Timestamp: <Unix time in seconds, digits only>
//	X-Vikunja-Signature: sha256=<64 hex digits: HMAC-SHA256(secret, timestamp + "." + body)>
//
// A call is accepted when the signature is right and the timestamp is at most automation.webhook.tolerance old and
// at most webhookFutureSkew ahead of this server's clock. An accepted signature is remembered (database, so every
// instance knows it) until its timestamp is out of that window: sending the same call again is refused.
const (
	signaturePrefix = "sha256="
	// signatureLen is the exact length of a valid signature header; anything else is refused before it is decoded.
	signatureLen = len(signaturePrefix) + sha256.Size*2
	// timestampMaxLen bounds the timestamp header: 11 digits are good until the year 5138.
	timestampMaxLen = 11
	// webhookFutureSkew is how far a sender's clock may run ahead of this server's.
	webhookFutureSkew = time.Minute
)

// verifiedWebhook is a call whose signature and timestamp were right.
type verifiedWebhook struct {
	// signature is the hex of the MAC, lower case: what is remembered.
	signature string
	// expires is when a call with this timestamp would be refused as stale anyway.
	expires time.Time
}

// webhookTolerance is the window of the timestamp into the past.
func webhookTolerance() time.Duration {
	if d := config.AutomationWebhookTolerance.GetDuration(); d > 0 {
		return d
	}
	return 5 * time.Minute
}

// parseWebhookTimestamp accepts only ASCII digits, so no sign, space, fraction or exponent form can make two
// spellings of one instant.
func parseWebhookTimestamp(v string) (time.Time, bool) {
	if v == "" || len(v) > timestampMaxLen {
		return time.Time{}, false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return time.Time{}, false
		}
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// parseWebhookSignature returns the MAC bytes of a header of the exact form sha256=<64 hex digits>.
func parseWebhookSignature(v string) ([]byte, bool) {
	if len(v) != signatureLen || !strings.HasPrefix(v, signaturePrefix) {
		return nil, false
	}
	b, err := hex.DecodeString(v[len(signaturePrefix):])
	if err != nil {
		return nil, false
	}
	return b, true
}

// WebhookSignature returns the two header values a sender sends for a body at a time: the timestamp and the
// signature. It is the one definition of the scheme the tests and the documentation use.
func WebhookSignature(secret string, at time.Time, body []byte) (timestamp, signature string) {
	timestamp = strconv.FormatInt(at.Unix(), 10)
	return timestamp, signaturePrefix + hex.EncodeToString(webhookMAC(secret, timestamp, body))
}

func webhookMAC(secret, timestamp string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return mac.Sum(nil)
}

// checkWebhookSignature verifies a call of a rule that names a secret. Every failure is the same
// ErrAutomationHookSignatureInvalid, so a caller learns nothing about which part was wrong; the reason is logged.
func checkWebhookSignature(s *xorm.Session, rule *models.AutomationRule, secretName string, call *WebhookCall, at time.Time) (*verifiedWebhook, error) {
	refuse := func(reason string) (*verifiedWebhook, error) {
		log.Debugf("[automation] webhook of rule %d refused: %s", rule.ID, reason)
		return nil, models.ErrAutomationHookSignatureInvalid{}
	}
	secrets, err := models.LoadAutomationSecrets(s, rule.ProjectID)
	if err != nil {
		return nil, err
	}
	sec := secrets[secretName]
	if sec == nil || sec.Err != nil {
		// The rule cannot be verified; refuse instead of accepting unsigned calls.
		return refuse("the secret is missing or unreadable")
	}
	got, ok := parseWebhookSignature(call.Signature)
	if !ok {
		return refuse("the signature header is malformed")
	}
	sent, ok := parseWebhookTimestamp(call.Timestamp)
	if !ok {
		return refuse("the timestamp header is missing or malformed")
	}
	// The MAC is checked first: whether the timestamp is stale is not worth a different cost or answer.
	want := webhookMAC(sec.Value, call.Timestamp, call.Body)
	macOK := hmac.Equal(got, want)
	tol := webhookTolerance()
	skew := min(webhookFutureSkew, tol)
	stale := at.Sub(sent) > tol
	ahead := sent.Sub(at) > skew
	switch {
	case !macOK:
		return refuse("the signature does not match")
	case stale:
		return refuse("the timestamp is older than the tolerance")
	case ahead:
		return refuse("the timestamp is too far ahead")
	}
	return &verifiedWebhook{
		signature: hex.EncodeToString(want),
		expires:   sent.Add(tol),
	}, nil
}

// rememberWebhookSignature stores the signature in the session of the call. A repeat is the same error as a wrong
// signature. Two identical calls at once race on the unique index in the database: the loser's insert fails (or
// finds the row), and it is told apart from a database fault by looking the signature up in a new session.
func rememberWebhookSignature(s *xorm.Session, rule *models.AutomationRule, v *verifiedWebhook) error {
	replayed, err := models.RememberAutomationWebhookSignature(s, rule.ID, v.signature, v.expires)
	if err != nil {
		check := db.NewReadSession()
		seen, lookupErr := models.AutomationWebhookSignatureSeen(check, rule.ID, v.signature)
		check.Close()
		if lookupErr == nil && seen {
			replayed = true
		} else {
			return err
		}
	}
	if replayed {
		log.Debugf("[automation] webhook of rule %d refused: the call was already accepted", rule.ID)
		return models.ErrAutomationHookSignatureInvalid{}
	}
	return nil
}

// CleanupWebhookSignatures deletes the remembered signatures that cannot be replayed any more. The ticker runs it.
func CleanupWebhookSignatures(at time.Time) (int64, error) {
	s := db.NewSession()
	defer s.Close()
	n, err := models.CleanupAutomationWebhookSeen(s, at)
	if err != nil {
		_ = s.Rollback()
		return 0, err
	}
	return n, s.Commit()
}
