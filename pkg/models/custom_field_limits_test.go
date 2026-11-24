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
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func customFieldErrorCode(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	hp, ok := err.(web.HTTPErrorProcessor)
	require.True(t, ok, "got %T: %v", err, err)
	return hp.HTTPError().Code
}

func createCustomFieldAs(t *testing.T, f *CustomField) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	if err := f.Create(s, cfUser1); err != nil {
		_ = s.Rollback()
		return err
	}
	return s.Commit()
}

func updateCustomFieldAs(t *testing.T, f *CustomField) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	if err := f.Update(s, cfUser1); err != nil {
		_ = s.Rollback()
		return err
	}
	return s.Commit()
}

func optionsOf(n int) []*CustomFieldOption {
	opts := make([]*CustomFieldOption, 0, n)
	for i := 0; i < n; i++ {
		opts = append(opts, &CustomFieldOption{Title: "option " + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26))})
	}
	return opts
}

func TestCustomField_FieldsPerProjectLimit(t *testing.T) {
	t.Run("adding over the limit is refused, under it works", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		config.CustomFieldsMaxFieldsPerProject.Set(6)
		t.Cleanup(func() { config.CustomFieldsMaxFieldsPerProject.Set(100) })

		// Project 1 has four fields in the fixtures.
		require.NoError(t, createCustomFieldAs(t, &CustomField{ProjectID: 1, Title: "five", Type: CustomFieldTypeText}))
		require.NoError(t, createCustomFieldAs(t, &CustomField{ProjectID: 1, Title: "six", Type: CustomFieldTypeText}))
		err := createCustomFieldAs(t, &CustomField{ProjectID: 1, Title: "seven", Type: CustomFieldTypeText})
		assert.Equal(t, 20006, customFieldErrorCode(t, err))
		db.AssertCount(t, "custom_fields", builder.Eq{"project_id": 1}, 6)
	})

	t.Run("a project above the limit keeps working", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		config.CustomFieldsMaxFieldsPerProject.Set(2)
		t.Cleanup(func() { config.CustomFieldsMaxFieldsPerProject.Set(100) })

		err := createCustomFieldAs(t, &CustomField{ProjectID: 1, Title: "more", Type: CustomFieldTypeText})
		assert.Equal(t, 20006, customFieldErrorCode(t, err))

		existing := &CustomField{ID: 1, ProjectID: 1, Title: "renamed", Type: CustomFieldTypeText}
		stored := readCustomField(t, 1, 1)
		existing.Type = stored.Type
		existing.Options = stored.Options
		require.NoError(t, updateCustomFieldAs(t, existing), "editing a field of a project over the limit still works")

		// Deleting brings it back under, and adding works again.
		s := db.NewSession()
		require.NoError(t, (&CustomField{ID: 2, ProjectID: 1}).Delete(s, cfUser1))
		require.NoError(t, (&CustomField{ID: 3, ProjectID: 1}).Delete(s, cfUser1))
		require.NoError(t, (&CustomField{ID: 4, ProjectID: 1}).Delete(s, cfUser1))
		require.NoError(t, s.Commit())
		s.Close()
		require.NoError(t, createCustomFieldAs(t, &CustomField{ProjectID: 1, Title: "again", Type: CustomFieldTypeText}))
	})
}

func readCustomField(t *testing.T, projectID, id int64) *CustomField {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	f, err := getCustomFieldInProject(s, id, projectID)
	require.NoError(t, err)
	require.NoError(t, loadCustomFieldRelations(s, f))
	return f
}

func TestCustomField_OptionsPerFieldLimit(t *testing.T) {
	t.Run("creating and growing over the limit are refused", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		config.CustomFieldsMaxOptionsPerField.Set(5)
		t.Cleanup(func() { config.CustomFieldsMaxOptionsPerField.Set(200) })

		err := createCustomFieldAs(t, &CustomField{ProjectID: 1, Title: "big", Type: CustomFieldTypeSelect, Options: optionsOf(6)})
		assert.Equal(t, 20007, customFieldErrorCode(t, err))

		f := &CustomField{ProjectID: 1, Title: "ok", Type: CustomFieldTypeMultiSelect, Options: optionsOf(5)}
		require.NoError(t, createCustomFieldAs(t, f))

		f.Options = append(f.Options, &CustomFieldOption{Title: "one too many"})
		err = updateCustomFieldAs(t, f)
		assert.Equal(t, 20007, customFieldErrorCode(t, err))
		assert.Len(t, readCustomField(t, 1, f.ID).Options, 5, "nothing was written")
	})

	t.Run("a field above the limit can be edited and shrunk but not grown", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		f := &CustomField{ProjectID: 1, Title: "legacy", Type: CustomFieldTypeSelect, Options: optionsOf(8)}
		require.NoError(t, createCustomFieldAs(t, f))

		config.CustomFieldsMaxOptionsPerField.Set(5)
		t.Cleanup(func() { config.CustomFieldsMaxOptionsPerField.Set(200) })

		stored := readCustomField(t, 1, f.ID)
		stored.Title = "legacy renamed"
		require.NoError(t, updateCustomFieldAs(t, stored), "unchanged options of a field over the limit")

		stored = readCustomField(t, 1, f.ID)
		stored.Options = append(stored.Options, &CustomFieldOption{Title: "new"})
		assert.Equal(t, 20007, customFieldErrorCode(t, updateCustomFieldAs(t, stored)))

		stored = readCustomField(t, 1, f.ID)
		stored.Options = stored.Options[:7]
		require.NoError(t, updateCustomFieldAs(t, stored), "shrinking is allowed, also while still above the limit")
		assert.Len(t, readCustomField(t, 1, f.ID).Options, 7)
	})
}

func TestCustomFieldValue_TextLength(t *testing.T) {
	// The longest value MySQL's TEXT column stores, in bytes.
	const limit = 65535

	cases := []struct {
		name      string
		fieldType CustomFieldType
		value     func(n int) string
	}{
		{"text", CustomFieldTypeText, func(n int) string { return strings.Repeat("a", n) }},
		{"long_text", CustomFieldTypeLongText, func(n int) string { return strings.Repeat("a", n) }},
		{"url", CustomFieldTypeURL, func(n int) string { return "https://example.com/" + strings.Repeat("a", n-len("https://example.com/")) }},
		{"email", CustomFieldTypeEmail, func(n int) string { return strings.Repeat("a", n-len("@example.com")) + "@example.com" }},
	}
	for _, tc := range cases {
		t.Run(tc.name+" at the limit is stored, one byte more is refused", func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			f := createCustomField(t, &CustomField{ProjectID: 1, Title: "t-" + tc.name, Type: tc.fieldType})

			_, err := setCustomFieldValue(t, 1, f.ID, tc.value(limit), cfUser1)
			require.NoError(t, err)

			_, err = setCustomFieldValue(t, 1, f.ID, tc.value(limit+1), cfUser1)
			assert.Equal(t, 20008, customFieldErrorCode(t, err))
			assert.True(t, IsErrCustomFieldInvalidValue(err), "callers that check for an invalid value, such as forms, keep working")
		})
	}

	t.Run("multi-byte characters count as bytes", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		f := createCustomField(t, &CustomField{ProjectID: 1, Title: "multibyte", Type: CustomFieldTypeLongText})

		// 21845 three-byte characters are 65535 bytes, one more character is 65538 bytes.
		_, err := setCustomFieldValue(t, 1, f.ID, strings.Repeat("€", 21845), cfUser1)
		require.NoError(t, err)
		_, err = setCustomFieldValue(t, 1, f.ID, strings.Repeat("€", 21846), cfUser1)
		assert.Equal(t, 20008, customFieldErrorCode(t, err))
		assert.Less(t, len([]rune(strings.Repeat("€", 21846))), limit, "fewer characters than bytes")
	})
}
