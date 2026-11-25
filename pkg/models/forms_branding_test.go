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
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/files"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func logoPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: 128})
		}
	}
	buf := &bytes.Buffer{}
	require.NoError(t, png.Encode(buf, img))
	return buf.Bytes()
}

func fileRows(t *testing.T, id int64) int64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("id = ?", id).Count(&files.File{})
	require.NoError(t, err)
	return n
}

func TestForm_Branding(t *testing.T) {
	t.Run("validation", func(t *testing.T) {
		formsReset(t)
		f := baseForm(FormAccessPublic)
		f.BrandName = strings.Repeat("x", 101)
		f.AccentColor = "blue"
		assert.ElementsMatch(t, []string{"brand_name", "accent_color"}, issuePaths(t, tryCreateForm(t, f, fU1)))

		f = baseForm(FormAccessPublic)
		f.BrandName = "bad\x00name"
		assert.Equal(t, []string{"brand_name"}, issuePaths(t, tryCreateForm(t, f, fU1)))

		f = baseForm(FormAccessPublic)
		f.AccentColor = "#12345"
		assert.Equal(t, []string{"accent_color"}, issuePaths(t, tryCreateForm(t, f, fU1)))
	})

	t.Run("a name and a color are trimmed, the color is normalized and both are stored", func(t *testing.T) {
		formsReset(t)
		f := baseForm(FormAccessPublic)
		f.BrandName = "  Damla Yayınevi "
		f.AccentColor = "1973FF"
		f.HideFooter = true
		createForm(t, f)

		stored := reloadForm(t, f.ID)
		assert.Equal(t, "Damla Yayınevi", stored.BrandName)
		assert.Equal(t, "#1973ff", stored.AccentColor)
		assert.True(t, stored.HideFooter)
		assert.False(t, stored.HasBrandLogo)

		// An update that sends none of them clears them, like every other field of a PUT.
		stored.BrandName, stored.AccentColor, stored.HideFooter = "", "", false
		s := db.NewSession()
		defer s.Close()
		require.NoError(t, stored.Update(s, fU1))
		require.NoError(t, s.Commit())
		cleared := reloadForm(t, f.ID)
		assert.Empty(t, cleared.BrandName)
		assert.Empty(t, cleared.AccentColor)
		assert.False(t, cleared.HideFooter)
	})

	t.Run("the public definition carries only what the page needs", func(t *testing.T) {
		formsReset(t)
		f := baseForm(FormAccessPublic)
		f.BrandName = "Damla"
		f.AccentColor = "#112233"
		f.HideFooter = true
		createForm(t, f)

		def, err := GetFormDefinition(f.Token, nil)
		require.NoError(t, err)
		top := marshalKeys(t, def)
		branding := top["branding"].(map[string]any)
		assert.ElementsMatch(t, []string{"name", "accent_color", "has_logo", "hide_footer"}, keysOf(branding))
		assert.Equal(t, "Damla", branding["name"])
		assert.Equal(t, "#112233", branding["accent_color"])
		assert.Equal(t, false, branding["has_logo"])
		assert.Equal(t, true, branding["hide_footer"])
	})
}

func TestFormLogo_Prepare(t *testing.T) {
	ctx := context.Background()

	t.Run("accepts a png, scales it down and keeps it a png", func(t *testing.T) {
		logo, err := PrepareFormLogo(ctx, bytes.NewReader(logoPNG(t, 1200, 600)))
		require.NoError(t, err)
		cfg, format, err := image.DecodeConfig(bytes.NewReader(logo.png))
		require.NoError(t, err)
		assert.Equal(t, "png", format)
		assert.Equal(t, 512, cfg.Width)
		assert.Equal(t, 256, cfg.Height)
	})

	t.Run("does not scale up", func(t *testing.T) {
		logo, err := PrepareFormLogo(ctx, bytes.NewReader(logoPNG(t, 40, 20)))
		require.NoError(t, err)
		cfg, _, err := image.DecodeConfig(bytes.NewReader(logo.png))
		require.NoError(t, err)
		assert.Equal(t, 40, cfg.Width)
	})

	t.Run("refuses what is not a usable raster image", func(t *testing.T) {
		for name, c := range map[string]struct {
			data   []byte
			reason string
		}{
			"text":   {[]byte("hello"), FormLogoNotAnImage},
			"svg":    {[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), FormLogoUnsupportedType},
			"html":   {[]byte(`<html><script>alert(1)</script></html>`), FormLogoNotAnImage},
			"gz":     {[]byte("\x1f\x8b\x08\x00\x00\x00\x00\x00"), FormLogoNotAnImage},
			"broken": {append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...), ""},
		} {
			_, err := PrepareFormLogo(ctx, bytes.NewReader(c.data))
			require.Error(t, err, name)
			if c.reason != "" {
				assert.Equal(t, ErrFormLogoInvalid{Reason: c.reason}, err, name)
			}
		}
	})

	t.Run("decodes every accepted format", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 30, 20))
		for x := 0; x < 30; x++ {
			for y := 0; y < 20; y++ {
				img.Set(x, y, color.NRGBA{R: 10, G: 120, B: 220, A: 255})
			}
		}
		encoded := map[string][]byte{"png": logoPNG(t, 30, 20)}
		jbuf := &bytes.Buffer{}
		require.NoError(t, jpeg.Encode(jbuf, img, nil))
		encoded["jpeg"] = jbuf.Bytes()
		gbuf := &bytes.Buffer{}
		require.NoError(t, gif.Encode(gbuf, img, nil))
		encoded["gif"] = gbuf.Bytes()
		// A 1x1 lossy WebP: x/image/webp only decodes, so the fixture is embedded instead of produced.
		webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
		require.NoError(t, err)
		encoded["webp"] = webp

		for name, data := range encoded {
			logo, err := PrepareFormLogo(ctx, bytes.NewReader(data))
			require.NoError(t, err, name)
			_, format, err := image.DecodeConfig(bytes.NewReader(logo.png))
			require.NoError(t, err, name)
			assert.Equal(t, "png", format, name)
		}
	})

	t.Run("refuses a small file with a huge canvas before decoding it", func(t *testing.T) {
		// A solid 5000x5000 PNG is a few dozen KiB but 25 megapixels.
		data := logoPNG(t, 5000, 5000)
		require.Less(t, len(data), FormLogoMaxBytes, "the fixture has to pass the byte limit")
		_, err := PrepareFormLogo(ctx, bytes.NewReader(data))
		assert.Equal(t, ErrFormLogoInvalid{Reason: FormLogoTooLarge}, err)

		_, err = PrepareFormLogo(ctx, bytes.NewReader(logoPNG(t, 4096, 100)))
		require.NoError(t, err, "the limit itself is fine")
	})

	t.Run("refuses a file over the limit", func(t *testing.T) {
		big := append(logoPNG(t, 10, 10), bytes.Repeat([]byte{0}, FormLogoMaxBytes)...)
		_, err := PrepareFormLogo(ctx, bytes.NewReader(big))
		assert.Equal(t, ErrFormLogoInvalid{Reason: FormLogoTooLarge}, err)
	})
}

func TestFormLogo_Lifecycle(t *testing.T) {
	formsReset(t)
	ctx := context.Background()
	f := createForm(t, baseForm(FormAccessPublic))

	t.Run("only project admins upload", func(t *testing.T) {
		_, err := UploadFormLogo(ctx, fU2, 1, f.ID, bytes.NewReader(logoPNG(t, 20, 20)))
		require.Error(t, err)
		_, err = DeleteFormLogo(ctx, fU2, 1, f.ID)
		require.Error(t, err)
		_, err = UploadFormLogo(ctx, fU1, 1, f.ID+100, bytes.NewReader(logoPNG(t, 20, 20)))
		assert.True(t, IsErrFormDoesNotExist(err))
	})

	t.Run("the logo is served by the token only", func(t *testing.T) {
		_, err := OpenFormLogo(f.Token)
		assert.True(t, IsErrFormDoesNotExist(err), "no logo yet")

		got, err := UploadFormLogo(ctx, fU1, 1, f.ID, bytes.NewReader(logoPNG(t, 20, 20)))
		require.NoError(t, err)
		assert.True(t, got.HasBrandLogo)
		firstFile := reloadForm(t, f.ID).BrandLogoFileID
		require.NotZero(t, firstFile)

		logo, err := OpenFormLogo(f.Token)
		require.NoError(t, err)
		data, err := io.ReadAll(logo.File.File)
		require.NoError(t, err)
		_ = logo.File.File.Close()
		_, format, err := image.DecodeConfig(bytes.NewReader(data))
		require.NoError(t, err)
		assert.Equal(t, "png", format)
		assert.NotEmpty(t, logo.ETag)

		_, err = OpenFormLogo("not-a-token")
		assert.True(t, IsErrFormDoesNotExist(err))

		// The public definition says there is a logo, without naming the file.
		def, err := GetFormDefinition(f.Token, nil)
		require.NoError(t, err)
		assert.True(t, def.Branding.HasLogo)

		// A replacement removes the old file.
		_, err = UploadFormLogo(ctx, fU1, 1, f.ID, bytes.NewReader(logoPNG(t, 30, 30)))
		require.NoError(t, err)
		secondFile := reloadForm(t, f.ID).BrandLogoFileID
		assert.NotEqual(t, firstFile, secondFile)
		assert.EqualValues(t, 0, fileRows(t, firstFile))
		assert.EqualValues(t, 1, fileRows(t, secondFile))
		logo2, err := OpenFormLogo(f.Token)
		require.NoError(t, err)
		_ = logo2.File.File.Close()
		assert.NotEqual(t, logo.ETag, logo2.ETag)

		// Saving the form by hand keeps the logo.
		stored := reloadForm(t, f.ID)
		s := db.NewSession()
		require.NoError(t, stored.Update(s, fU1))
		require.NoError(t, s.Commit())
		s.Close()
		assert.Equal(t, secondFile, reloadForm(t, f.ID).BrandLogoFileID)
	})

	t.Run("removing the logo and deleting the form delete the file", func(t *testing.T) {
		current := reloadForm(t, f.ID).BrandLogoFileID
		removed, err := DeleteFormLogo(ctx, fU1, 1, f.ID)
		require.NoError(t, err)
		assert.False(t, removed.HasBrandLogo)
		assert.EqualValues(t, 0, fileRows(t, current))
		_, err = OpenFormLogo(f.Token)
		assert.True(t, IsErrFormDoesNotExist(err))
		_, err = DeleteFormLogo(ctx, fU1, 1, f.ID)
		require.NoError(t, err, "removing a missing logo is fine")

		_, err = UploadFormLogo(ctx, fU1, 1, f.ID, bytes.NewReader(logoPNG(t, 20, 20)))
		require.NoError(t, err)
		logoFile := reloadForm(t, f.ID).BrandLogoFileID
		s := db.NewSession()
		require.NoError(t, (&Form{ID: f.ID, ProjectID: 1}).Delete(s, fU1))
		require.NoError(t, s.Commit())
		s.Close()
		assert.EqualValues(t, 0, fileRows(t, logoFile))
	})

	t.Run("a duplicated project copies the branding, not the logo file", func(t *testing.T) {
		g := baseForm(FormAccessPublic)
		g.BrandName = "Brand"
		g.AccentColor = "#abcdef"
		createForm(t, g)
		_, err := UploadFormLogo(ctx, fU1, 1, g.ID, bytes.NewReader(logoPNG(t, 20, 20)))
		require.NoError(t, err)

		s := db.NewSession()
		defer s.Close()
		require.NoError(t, duplicateFormsForProject(s, 1, 1, 2, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}))
		require.NoError(t, s.Commit())
		clone := &Form{}
		has, err := s.Where("project_id = 2 AND brand_name = ?", "Brand").Get(clone)
		require.NoError(t, err)
		require.True(t, has)
		assert.Equal(t, "#abcdef", clone.AccentColor)
		assert.Zero(t, clone.BrandLogoFileID)
	})
}

func TestFormLogo_Availability(t *testing.T) {
	formsReset(t)
	ctx := context.Background()
	f := createForm(t, baseForm(FormAccessPublic))
	_, err := UploadFormLogo(ctx, fU1, 1, f.ID, bytes.NewReader(logoPNG(t, 20, 20)))
	require.NoError(t, err)
	serves := func() bool {
		logo, err := OpenFormLogo(f.Token)
		if err == nil {
			_ = logo.File.File.Close()
			return true
		}
		assert.True(t, IsErrFormDoesNotExist(err), "got %T: %v", err, err)
		return false
	}
	setForm := func(cols string, v *Form) {
		s := db.NewSession()
		defer s.Close()
		_, err := s.ID(f.ID).Cols(cols).Update(v)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
	}
	require.True(t, serves())

	t.Run("not while the form is disabled", func(t *testing.T) {
		setForm("enabled", &Form{Enabled: false})
		defer setForm("enabled", &Form{Enabled: true})
		assert.False(t, serves())
	})

	t.Run("not while forms or public forms are disabled on the instance", func(t *testing.T) {
		config.FormsEnabled.Set(false)
		assert.False(t, serves())
		config.FormsEnabled.Set(true)

		config.FormsPublicEnabled.Set(false)
		defer config.FormsPublicEnabled.Set(true)
		assert.False(t, serves())
	})

	t.Run("a full form keeps serving it", func(t *testing.T) {
		setForm("max_submissions", &Form{MaxSubmissions: 1})
		defer setForm("max_submissions", &Form{MaxSubmissions: 0})
		s := db.NewSession()
		_, err := s.Exec("UPDATE forms SET submission_count = 5 WHERE id = ?", f.ID)
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		assert.True(t, serves())
	})

	t.Run("saving and removing the logo need the feature", func(t *testing.T) {
		config.FormsEnabled.Set(false)
		defer config.FormsEnabled.Set(true)
		_, err := UploadFormLogo(ctx, fU1, 1, f.ID, bytes.NewReader(logoPNG(t, 20, 20)))
		assert.True(t, IsErrFormsDisabled(err), "got %v", err)
		_, err = DeleteFormLogo(ctx, fU1, 1, f.ID)
		assert.True(t, IsErrFormsDisabled(err), "got %v", err)
	})
}

func TestFormLogo_ClosedNonPublicFormLooksLikeAnUnknownToken(t *testing.T) {
	formsReset(t)
	closed := time.Now().Add(-time.Minute)
	withLogo := func(access FormAccess, settings *FormSettings) *Form {
		f := baseForm(access)
		f.Settings = settings
		createForm(t, f)
		_, err := UploadFormLogo(context.Background(), fU1, 1, f.ID, bytes.NewReader(logoPNG(t, 20, 20)))
		require.NoError(t, err)
		return f
	}
	load := func(token string) error {
		s := db.NewReadSession()
		defer s.Close()
		_, err := LoadFormLogo(s, token)
		return err
	}
	unknown := load("does-not-exist")
	require.True(t, IsErrFormDoesNotExist(unknown))

	for _, access := range []FormAccess{FormAccessMembers, FormAccessUsers} {
		f := withLogo(access, &FormSettings{CloseDate: &closed})
		err := load(f.Token)
		assert.Equal(t, unknown, err, string(access))
		assert.Equal(t, unknown.(ErrFormDoesNotExist).HTTPError(), err.(ErrFormDoesNotExist).HTTPError())
	}
	t.Run("an open form keeps its logo, so does a closed public one", func(t *testing.T) {
		open := withLogo(FormAccessMembers, &FormSettings{})
		err := load(open.Token)
		require.NoError(t, err)
		pub := withLogo(FormAccessPublic, &FormSettings{CloseDate: &closed})
		err = load(pub.Token)
		require.NoError(t, err)
	})
}
