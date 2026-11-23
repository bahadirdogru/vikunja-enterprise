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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"  // The decoders DecodeConfig and imageutils need for every accepted logo type.
	_ "image/jpeg" // See above.
	"image/png"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/modules/imageutils"
	"code.vikunja.io/api/pkg/web"

	"github.com/disintegration/imaging"
	"github.com/gabriel-vasile/mimetype"
	_ "golang.org/x/image/webp" // See above.
	"xorm.io/xorm"
)

const (
	formMaxBrandNameLen = 100
	// FormLogoMaxBytes is the largest logo file that is accepted.
	FormLogoMaxBytes = 1 << 20
	// formLogoMaxSide is the longest side of the stored logo; larger images are scaled down.
	formLogoMaxSide = 512
	// formLogoMaxInputSide is the largest width and height an uploaded logo may have. A file of a few KiB can
	// declare far more pixels than any logo needs, so this is checked on the header before anything is decoded.
	formLogoMaxInputSide = 4096
)

var formAccentColor = regexp.MustCompile(`^#?([0-9a-fA-F]{6})$`)

// formLogoMimes are the image types a logo may be uploaded as. SVG is not one of them: it can carry scripts and the
// logo is served from the API origin.
var formLogoMimes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// validateBranding checks and normalizes the branding fields of a form: the brand name is trimmed plain text, the
// accent color becomes #rrggbb.
func (f *Form) validateBranding(issues *formIssues) {
	f.BrandName = strings.TrimSpace(f.BrandName)
	switch {
	case utf8.RuneCountInString(f.BrandName) > formMaxBrandNameLen:
		issues.add("brand_name", "may be at most %d characters long", formMaxBrandNameLen)
	case strings.IndexFunc(f.BrandName, unicode.IsControl) >= 0:
		issues.add("brand_name", "must not contain control characters")
	}

	f.AccentColor = strings.TrimSpace(f.AccentColor)
	if f.AccentColor == "" {
		return
	}
	m := formAccentColor.FindStringSubmatch(f.AccentColor)
	if m == nil {
		issues.add("accent_color", "must be a hex color like #1973ff")
		return
	}
	f.AccentColor = "#" + strings.ToLower(m[1])
}

// EncodedFormLogo is a logo that was decoded, scaled down and re-encoded as PNG, ready to be stored.
type EncodedFormLogo struct {
	png []byte
}

// PrepareFormLogo reads, checks and re-encodes an uploaded logo. Re-encoding drops everything but the pixels
// (metadata, embedded payloads), keeps transparency and bounds the size. It may queue for decode capacity, so call it
// outside of a transaction.
func PrepareFormLogo(ctx context.Context, src io.Reader) (*EncodedFormLogo, error) {
	raw, err := io.ReadAll(io.LimitReader(src, FormLogoMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > FormLogoMaxBytes {
		return nil, ErrFormLogoInvalid{Reason: FormLogoTooLarge}
	}
	mime := mimetype.Detect(raw)
	if !strings.HasPrefix(mime.String(), "image/") {
		return nil, ErrFormLogoInvalid{Reason: FormLogoNotAnImage}
	}
	supported := false
	for _, allowed := range formLogoMimes {
		if mime.Is(allowed) {
			supported = true
			break
		}
	}
	if !supported {
		return nil, ErrFormLogoInvalid{Reason: FormLogoUnsupportedType}
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrFormLogoInvalid{Reason: FormLogoUnsupportedType}
	}
	if cfg.Width > formLogoMaxInputSide || cfg.Height > formLogoMaxInputSide {
		return nil, ErrFormLogoInvalid{Reason: FormLogoTooLarge}
	}

	buf := &bytes.Buffer{}
	err = imageutils.Decode(ctx, imageutils.BytesOpener(raw), func(img image.Image, _ string) error {
		return png.Encode(buf, imaging.Fit(img, formLogoMaxSide, formLogoMaxSide, imaging.Lanczos))
	})
	if err != nil {
		if errors.Is(err, imageutils.ErrUndecodable) {
			return nil, ErrFormLogoInvalid{Reason: FormLogoUnsupportedType}
		}
		return nil, err
	}
	return &EncodedFormLogo{png: buf.Bytes()}, nil
}

// SetBrandLogo stores the logo and replaces the previous one. The old blob goes away when the session commits.
func (f *Form) SetBrandLogo(s *xorm.Session, a web.Auth, logo *EncodedFormLogo) error {
	file, err := files.CreateWithMimeAndSession(s, bytes.NewReader(logo.png), "logo.png", uint64(len(logo.png)), a, "image/png", true)
	if err != nil {
		return err
	}
	old := f.BrandLogoFileID
	if _, err = s.ID(f.ID).Cols("brand_logo_file_id").Update(&Form{BrandLogoFileID: file.ID}); err != nil {
		return err
	}
	f.BrandLogoFileID = file.ID
	f.HasBrandLogo = true
	return deleteFormLogoFile(s, old)
}

// RemoveBrandLogo removes the logo of the form. It is not an error when there is none.
func (f *Form) RemoveBrandLogo(s *xorm.Session) error {
	old := f.BrandLogoFileID
	if old == 0 {
		return nil
	}
	if _, err := s.ID(f.ID).Cols("brand_logo_file_id").Update(&Form{BrandLogoFileID: 0}); err != nil {
		return err
	}
	f.BrandLogoFileID = 0
	f.HasBrandLogo = false
	return deleteFormLogoFile(s, old)
}

func deleteFormLogoFile(s *xorm.Session, fileID int64) error {
	if fileID == 0 {
		return nil
	}
	err := (&files.File{ID: fileID}).DeleteOnCommit(s)
	if files.IsErrFileDoesNotExist(err) {
		return nil
	}
	return err
}

// FormLogo is the logo of a form for the public download.
type FormLogo struct {
	File *files.File
	// ETag changes whenever the logo is replaced.
	ETag string
}

// LoadFormLogo opens the logo of the form with the token. Anyone who knows the token may load it, whatever the access
// mode of the form is: a browser cannot send credentials with an image request, and a logo is not project data. A
// missing token, a form without a logo and a missing file are all the same error.
func LoadFormLogo(s *xorm.Session, token string) (*FormLogo, error) {
	f, err := GetFormByToken(s, token)
	if err != nil {
		return nil, err
	}
	normalizeForm(f)
	// Like the definition, a form that is switched off, by the owner or by the instance, serves nothing. A form that
	// is closed, full or members-only keeps its logo.
	if formUnavailableReason(f) == FormUnavailableDisabled {
		return nil, ErrFormDoesNotExist{}
	}
	if f.BrandLogoFileID == 0 {
		return nil, ErrFormDoesNotExist{}
	}
	file := &files.File{ID: f.BrandLogoFileID}
	if err = file.LoadFileMetaByID(s); err != nil {
		return nil, ErrFormDoesNotExist{}
	}
	if err = file.LoadFileByID(); err != nil {
		return nil, ErrFormDoesNotExist{}
	}
	// The ETag changes with every replacement without exposing the internal file id.
	sum := sha256.Sum256([]byte("form-logo:" + strconv.FormatInt(file.ID, 10) + ":" + strconv.FormatUint(file.Size, 10) + ":" + file.Created.UTC().Format(time.RFC3339Nano)))
	return &FormLogo{File: file, ETag: `"` + hex.EncodeToString(sum[:12]) + `"`}, nil
}

// UploadFormLogo checks the image and stores it as the logo of the form. Needs admin access to the project. The
// decoding may queue for capacity, so it runs before the transaction starts, and the permission is checked again
// inside it.
func UploadFormLogo(ctx context.Context, a web.Auth, projectID, formID int64, src io.Reader) (*Form, error) {
	if err := (&Form{}).checkFeature(); err != nil {
		return nil, err
	}
	if err := checkFormAdmin(a, projectID, formID); err != nil {
		return nil, err
	}
	logo, err := PrepareFormLogo(ctx, src)
	if err != nil {
		return nil, err
	}

	s := db.NewSession()
	defer s.Close()
	defer events.CleanupPending(s)

	f, err := lockedAdminForm(s, a, projectID, formID)
	if err == nil {
		err = f.SetBrandLogo(s, a, logo)
	}
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	events.DispatchPending(ctx, s)
	return f, nil
}

// DeleteFormLogo removes the logo of the form. Needs admin access to the project.
func DeleteFormLogo(ctx context.Context, a web.Auth, projectID, formID int64) (*Form, error) {
	if err := (&Form{}).checkFeature(); err != nil {
		return nil, err
	}
	s := db.NewSession()
	defer s.Close()
	defer events.CleanupPending(s)

	f, err := lockedAdminForm(s, a, projectID, formID)
	if err == nil {
		err = f.RemoveBrandLogo(s)
	}
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	events.DispatchPending(ctx, s)
	return f, nil
}

// lockedAdminForm is adminForm for a write of the logo: it takes the lock of the form row first, the way submitForm
// does, and reads the form after that, so two concurrent replacements cannot both see the same old logo and orphan
// a file.
func lockedAdminForm(s *xorm.Session, a web.Auth, projectID, formID int64) (*Form, error) {
	if _, err := adminForm(s, a, projectID, formID); err != nil {
		return nil, err
	}
	if _, err := s.Exec("UPDATE forms SET id = id WHERE id = ?", formID); err != nil {
		return nil, err
	}
	return adminForm(s, a, projectID, formID)
}

func checkFormAdmin(a web.Auth, projectID, formID int64) error {
	s := db.NewReadSession()
	defer s.Close()
	_, err := adminForm(s, a, projectID, formID)
	return err
}

// adminForm loads the form when the caller administers its project.
func adminForm(s *xorm.Session, a web.Auth, projectID, formID int64) (*Form, error) {
	can, _, err := canAdministerForms(s, a, projectID)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, ErrGenericForbidden{}
	}
	f, err := getFormInProject(s, formID, projectID)
	if err != nil {
		return nil, err
	}
	normalizeForm(f)
	return f, nil
}

// OpenFormLogo opens the logo of the form with the token for the public download.
func OpenFormLogo(token string) (*FormLogo, error) {
	s := db.NewReadSession()
	defer s.Close()
	return LoadFormLogo(s, token)
}
