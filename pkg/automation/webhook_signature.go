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
//	X-Vikunja-Signature: sha256=<64 hex digits: HMAC-SHA256(secret, timestamp + "." + token + "." + body)>
//
// The token is the one in the URL of the hook (base64url: letters, digits, "-" and "_", never "."), so the signed
// string is unambiguous and a call signed for one hook is not valid for another hook that uses the same secret.
// The outgoing signature of the http.request action covers the body only and differs from this one on purpose.
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
	// signedAt is the timestamp the sender signed; the cleanup works from it.
	signedAt time.Time
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

// WebhookSignature returns the two header values a sender sends for a body to the hook with the token at a time:
// the timestamp and the signature. It is the one definition of the scheme the tests and the documentation use.
func WebhookSignature(secret, token string, at time.Time, body []byte) (timestamp, signature string) {
	timestamp = strconv.FormatInt(at.Unix(), 10)
	return timestamp, signaturePrefix + hex.EncodeToString(webhookMAC(secret, timestamp, token, body))
}

func webhookMAC(secret, timestamp, token string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{'.'})
	mac.Write([]byte(token))
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
	want := webhookMAC(sec.Value, call.Timestamp, call.Token, call.Body)
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
		signedAt:  sent,
	}, nil
}

// webhookReplayed reports whether the verified signature was accepted before. It is asked before the rate limit, so
// a replay is the same 401 as a wrong signature and does not use up the budget of the real sender.
func webhookReplayed(s *xorm.Session, rule *models.AutomationRule, v *verifiedWebhook) (bool, error) {
	return models.AutomationWebhookSignatureSeen(s, rule.ID, v.signature)
}

// rememberWebhookSignature stores the signature in the session of the call. A repeat is the same error as a wrong
// signature. Two identical calls at once race on the unique index in the database: the loser's insert fails (or
// finds the row), and it is told apart from a database fault by looking the signature up in a new session.
func rememberWebhookSignature(s *xorm.Session, rule *models.AutomationRule, v *verifiedWebhook) error {
	replayed, err := models.RememberAutomationWebhookSignature(s, rule.ID, v.signature, v.signedAt)
	if err != nil {
		return insertFailedAsReplay(rule, v, err)
	}
	if replayed {
		log.Debugf("[automation] webhook of rule %d refused: the call was already accepted", rule.ID)
		return models.ErrAutomationHookSignatureInvalid{}
	}
	return nil
}

// insertFailedAsReplay decides what a failed insert of a signature was: when another call stored the same signature
// meanwhile it is a replay (the same answer as a wrong signature), otherwise it is the database error.
func insertFailedAsReplay(rule *models.AutomationRule, v *verifiedWebhook, insertErr error) error {
	check := db.NewReadSession()
	defer check.Close()
	seen, lookupErr := models.AutomationWebhookSignatureSeen(check, rule.ID, v.signature)
	if lookupErr == nil && seen {
		log.Debugf("[automation] webhook of rule %d refused: the call was accepted by a concurrent request", rule.ID)
		return models.ErrAutomationHookSignatureInvalid{}
	}
	return insertErr
}

// webhookCleanupMargin is kept on top of the tolerance when signatures are deleted: instances whose clocks differ by
// less than this (and a clock that is stepped back) cannot reopen a replay while another instance still accepts it.
const webhookCleanupMargin = webhookFutureSkew + time.Minute

// CleanupWebhookSignatures deletes the remembered signatures that cannot be replayed any more: the ones signed
// before the tolerance of this instance and a margin ago. The ticker runs it. Every instance should have the same
// tolerance: one with a shorter tolerance deletes rows another still needs.
func CleanupWebhookSignatures(at time.Time) (int64, error) {
	s := db.NewSession()
	defer s.Close()
	n, err := models.CleanupAutomationWebhookSeen(s, at.Add(-webhookTolerance()-webhookCleanupMargin))
	if err != nil {
		_ = s.Rollback()
		return 0, err
	}
	return n, s.Commit()
}
