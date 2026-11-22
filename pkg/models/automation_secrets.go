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
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

const (
	// MaxAutomationSecretSize is the largest value of a secret in bytes.
	MaxAutomationSecretSize = 8 * 1024

	secretVersionPrefix = "v1:"
	// secretKeyInfo is the fixed HKDF info string. Changing it makes every stored value unreadable.
	secretKeyInfo = "vikunja automation secrets v1"
)

// AutomationSecret is a named value of a project that rules may use (signing key, token, list of ids) without it
// being shown anywhere. Values are encrypted at rest and never returned by the API.
type AutomationSecret struct {
	ID        int64  `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this secret."`
	ProjectID int64  `xorm:"bigint not null unique(automation_secrets_project_name)" json:"project_id" param:"project" readOnly:"true" doc:"The project the secret belongs to. Taken from the URL."`
	Name      string `xorm:"varchar(64) not null unique(automation_secrets_project_name)" json:"name" param:"name" doc:"The name scripts and templates use: letters, digits and underscores, not starting with a digit, at most 64 characters."`
	// Value is the encrypted value: a version prefix, then base64 of nonce and AES-256-GCM ciphertext.
	Value       string    `xorm:"longtext not null" json:"-"`
	CreatedByID int64     `xorm:"bigint not null" json:"created_by_id" readOnly:"true" doc:"The user who first stored the secret."`
	Created     time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"When the secret was created."`
	Updated     time.Time `xorm:"updated not null" json:"updated" readOnly:"true" doc:"When the value was last set."`

	// Unreadable is true when the stored value cannot be decrypted, because service.secret changed. Set the secret
	// again to fix it.
	Unreadable bool `xorm:"-" json:"unreadable" readOnly:"true" doc:"True when the stored value cannot be decrypted any more because service.secret changed. Set the secret again."`
	// NewValue is the plain value to store; it is never returned.
	NewValue string `xorm:"-" json:"-"`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name.
func (*AutomationSecret) TableName() string {
	return "automation_secrets"
}

func secretKey() ([]byte, error) {
	secret := config.ServiceSecret.GetString()
	if secret == "" {
		return nil, errors.New("service.secret is not set")
	}
	return hkdf.Key(sha256.New, []byte(secret), nil, secretKeyInfo, 32)
}

func secretAAD(projectID int64, name string) []byte {
	return []byte(fmt.Sprintf("automation-secret:%d:%s", projectID, name))
}

// EncryptAutomationSecret encrypts a value with AES-256-GCM. The key is derived from service.secret with
// HKDF-SHA256, every value gets a random nonce, and the project and name are authenticated, so a stored value
// cannot be moved to another secret. The result starts with a version prefix so the scheme can change later.
func EncryptAutomationSecret(projectID int64, name, plain string) (string, error) {
	key, err := secretKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), secretAAD(projectID, name))
	return secretVersionPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// DecryptAutomationSecret reverses EncryptAutomationSecret. It fails when service.secret is not the one the value
// was encrypted with.
func DecryptAutomationSecret(projectID int64, name, stored string) (string, error) {
	encoded, ok := strings.CutPrefix(stored, secretVersionPrefix)
	if !ok {
		return "", errors.New("unknown secret format")
	}
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	key, err := secretKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("the stored value is too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], secretAAD(projectID, name))
	if err != nil {
		return "", errors.New("the stored value cannot be decrypted")
	}
	return string(plain), nil
}

// AutomationSecretValue is a secret of a project as the engine uses it.
type AutomationSecretValue struct {
	Name  string
	Value string
	// Err is set when the value cannot be decrypted.
	Err error
}

// LoadAutomationSecrets returns every secret of a project, decrypted. Secrets that cannot be decrypted are
// included with Err set. It is for the engine only; the API never calls it.
func LoadAutomationSecrets(s *xorm.Session, projectID int64) (map[string]*AutomationSecretValue, error) {
	rows := []*AutomationSecret{}
	if err := s.Where("project_id = ?", projectID).Find(&rows); err != nil {
		return nil, err
	}
	out := make(map[string]*AutomationSecretValue, len(rows))
	for _, row := range rows {
		v := &AutomationSecretValue{Name: row.Name}
		plain, err := DecryptAutomationSecret(projectID, row.Name, row.Value)
		if err != nil {
			v.Err = ErrAutomationSecretUnreadable{Name: row.Name}
		} else {
			v.Value = plain
		}
		out[row.Name] = v
	}
	return out, nil
}

// AutomationSecretNames returns the names of the secrets of a project.
func AutomationSecretNames(s *xorm.Session, projectID int64) (map[string]bool, error) {
	names := []string{}
	if err := s.Table("automation_secrets").Where("project_id = ?", projectID).Cols("name").Find(&names); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out, nil
}

func getAutomationSecretByName(s *xorm.Session, projectID int64, name string) (*AutomationSecret, error) {
	rows := []*AutomationSecret{}
	// Databases differ in how they compare names (MySQL ignores case by default); the comparison that counts is
	// the exact one below.
	if err := s.Where(builder.And(builder.Eq{"project_id": projectID}, builder.Expr("LOWER(name) = ?", strings.ToLower(name)))).Find(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Name == name {
			return row, nil
		}
	}
	return nil, nil
}

func checkSecretName(name string) error {
	if !spec.SecretNamePattern.MatchString(name) {
		return ErrAutomationSecretInvalid{Reason: "the name must be letters, digits and underscores, not start with a digit and be at most 64 characters long"}
	}
	return nil
}

func (sec *AutomationSecret) fill() {
	_, err := DecryptAutomationSecret(sec.ProjectID, sec.Name, sec.Value)
	sec.Unreadable = err != nil
}

// CanRead checks that the user administers the project.
func (sec *AutomationSecret) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	can, max, err := canAdministerAutomation(s, a, sec.ProjectID)
	if err != nil || !can {
		return false, 0, err
	}
	if err = checkSecretName(sec.Name); err != nil {
		return false, 0, err
	}
	stored, err := getAutomationSecretByName(s, sec.ProjectID, sec.Name)
	if err != nil {
		return false, 0, err
	}
	if stored == nil {
		return false, 0, ErrAutomationSecretDoesNotExist{Name: sec.Name}
	}
	*sec = *stored
	return true, max, nil
}

// CanCreate is never true: secrets are set with PUT, which creates or replaces.
func (sec *AutomationSecret) CanCreate(_ *xorm.Session, _ web.Auth) (bool, error) {
	return false, nil
}

// CanUpdate checks that the user administers the project.
func (sec *AutomationSecret) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	can, _, err := canAdministerAutomation(s, a, sec.ProjectID)
	return can, err
}

// CanDelete checks that the user administers the project.
func (sec *AutomationSecret) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	can, _, err := canAdministerAutomation(s, a, sec.ProjectID)
	return can, err
}

// Create is not supported, see CanCreate.
func (sec *AutomationSecret) Create(_ *xorm.Session, _ web.Auth) error {
	return ErrGenericForbidden{}
}

// ReadOne fills the secret's metadata (never its value).
func (sec *AutomationSecret) ReadOne(_ *xorm.Session, _ web.Auth) error {
	sec.fill()
	return nil
}

// ReadAll returns the secrets of the project: names and timestamps, never values.
func (sec *AutomationSecret) ReadAll(s *xorm.Session, a web.Auth, _ string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	can, _, err := canAdministerAutomation(s, a, sec.ProjectID)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}
	cond := builder.Eq{"project_id": sec.ProjectID}
	total, err := s.Where(cond).Count(&AutomationSecret{})
	if err != nil {
		return nil, 0, 0, err
	}
	rows := []*AutomationSecret{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where(cond).OrderBy("name ASC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&rows); err != nil {
		return nil, 0, 0, err
	}
	for _, row := range rows {
		row.fill()
	}
	return rows, len(rows), total, nil
}

// Update creates the secret or replaces its value.
func (sec *AutomationSecret) Update(s *xorm.Session, a web.Auth) error {
	ownerID, err := ownerOf(a)
	if err != nil {
		return err
	}
	if err = checkSecretName(sec.Name); err != nil {
		return err
	}
	if sec.NewValue == "" {
		return ErrAutomationSecretInvalid{Reason: "the value must not be empty"}
	}
	if len(sec.NewValue) > MaxAutomationSecretSize {
		return ErrAutomationSecretInvalid{Reason: fmt.Sprintf("the value must be at most %d bytes", MaxAutomationSecretSize)}
	}
	encrypted, err := EncryptAutomationSecret(sec.ProjectID, sec.Name, sec.NewValue)
	if err != nil {
		return err
	}
	sec.NewValue = ""

	existing, err := getAutomationSecretByName(s, sec.ProjectID, sec.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		if _, err = s.ID(existing.ID).Cols("value").Update(&AutomationSecret{Value: encrypted}); err != nil {
			return err
		}
	} else {
		sec.ID = 0
		sec.Value = encrypted
		sec.CreatedByID = ownerID
		if _, err = s.Insert(sec); err != nil {
			return err
		}
		existing = sec
	}
	stored := &AutomationSecret{}
	if _, err = s.ID(existing.ID).Get(stored); err != nil {
		return err
	}
	*sec = *stored
	sec.fill()
	return nil
}

// Delete removes the secret. Rules that use it keep running and fail at the step that needs it.
func (sec *AutomationSecret) Delete(s *xorm.Session, _ web.Auth) error {
	if err := checkSecretName(sec.Name); err != nil {
		return err
	}
	existing, err := getAutomationSecretByName(s, sec.ProjectID, sec.Name)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrAutomationSecretDoesNotExist{Name: sec.Name}
	}
	_, err = s.ID(existing.ID).Delete(&AutomationSecret{})
	return err
}

func deleteAutomationSecretsForProject(s *xorm.Session, projectID int64) error {
	_, err := s.Where("project_id = ?", projectID).Delete(&AutomationSecret{})
	return err
}
