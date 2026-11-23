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
	"context"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
	"xorm.io/xorm/schemas"
)

// Fixture topology (pkg/db/fixtures): user1 owns project 1 (fields 1-4, tasks 1 and 2), has read
// access to project 9 (field 5, task 18), write access to project 10 (field 6, task 19) and
// admin access to project 11 (task 20). user2 has no access to project 1.
var (
	cfUser1 = &user.User{ID: 1}
	cfUser2 = &user.User{ID: 2}
)

func createCustomField(t *testing.T, f *CustomField) *CustomField {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	require.NoError(t, f.Create(s, cfUser1))
	require.NoError(t, s.Commit())
	return f
}

func setCustomFieldValue(t *testing.T, taskID, fieldID int64, value any, a web.Auth) (*CustomFieldValue, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()

	v := &CustomFieldValue{TaskID: taskID, CustomFieldID: fieldID, Value: value}
	can, err := v.CanUpdate(s, a)
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	if !can {
		_ = s.Rollback()
		return nil, ErrGenericForbidden{}
	}
	if err = v.Update(s, a); err != nil {
		_ = s.Rollback()
		return nil, err
	}
	require.NoError(t, s.Commit())
	return v, nil
}

func clearCustomFieldValue(t *testing.T, taskID, fieldID int64, a web.Auth) error {
	t.Helper()
	s := db.NewSession()
	defer s.Close()

	v := &CustomFieldValue{TaskID: taskID, CustomFieldID: fieldID}
	can, err := v.CanDelete(s, a)
	if err != nil {
		_ = s.Rollback()
		return err
	}
	if !can {
		_ = s.Rollback()
		return ErrGenericForbidden{}
	}
	if err = v.Delete(s, a); err != nil {
		_ = s.Rollback()
		return err
	}
	return s.Commit()
}

func readCustomFieldValues(t *testing.T, taskID int64) map[int64]any {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	result, _, _, err := (&CustomFieldValue{TaskID: taskID}).ReadAll(s, cfUser1, "", 0, 0)
	require.NoError(t, err)
	values := map[int64]any{}
	for _, e := range result.([]*CustomFieldValue) {
		values[e.CustomFieldID] = e.Value
	}
	return values
}

func TestCustomField_Definitions(t *testing.T) {
	t.Run("create with options", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		events.ClearDispatchedEvents()
		s := db.NewSession()
		defer s.Close()

		f := &CustomField{
			ProjectID: 1,
			Title:     "Priority",
			Type:      CustomFieldTypeSelect,
			Options: []*CustomFieldOption{
				{Title: "Low", Color: "#00ff00"},
				{Title: "High"},
			},
		}
		can, err := f.CanCreate(s, cfUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, f.Create(s, cfUser1))
		events.DispatchPending(context.Background(), s)
		require.NoError(t, s.Commit())

		assert.NotZero(t, f.ID)
		assert.Equal(t, int64(1), f.CreatedByID)
		assert.Greater(t, f.Position, 262144.0, "new fields go last")
		require.Len(t, f.Options, 2)
		assert.Less(t, f.Options[0].Position, f.Options[1].Position)
		db.AssertExists(t, "custom_field_options", map[string]interface{}{
			"custom_field_id": f.ID,
			"title":           "High",
		}, false)
		events.AssertDispatched(t, &CustomFieldCreatedEvent{})
	})

	t.Run("create validates", func(t *testing.T) {
		tests := []struct {
			name  string
			field *CustomField
		}{
			{
				name:  "empty title",
				field: &CustomField{ProjectID: 1, Title: "  ", Type: CustomFieldTypeText},
			},
			{
				name:  "unknown type",
				field: &CustomField{ProjectID: 1, Title: "x", Type: CustomFieldType("nope")},
			},
			{
				name: "options on a text field",
				field: &CustomField{
					ProjectID: 1,
					Title:     "x",
					Type:      CustomFieldTypeText,
					Options:   []*CustomFieldOption{{Title: "a"}},
				},
			},
			{
				name: "bad option color",
				field: &CustomField{
					ProjectID: 1,
					Title:     "x",
					Type:      CustomFieldTypeSelect,
					Options:   []*CustomFieldOption{{Title: "a", Color: "red"}},
				},
			},
			{
				name: "min above max",
				field: &CustomField{
					ProjectID: 1,
					Title:     "x",
					Type:      CustomFieldTypeNumber,
					Config:    &CustomFieldConfig{Min: Ptr(5.0), Max: Ptr(1.0)},
				},
			},
			{
				name: "max rating too high",
				field: &CustomField{
					ProjectID: 1,
					Title:     "x",
					Type:      CustomFieldTypeRating,
					Config:    &CustomFieldConfig{MaxRating: Ptr(11)},
				},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db.LoadAndAssertFixtures(t)
				s := db.NewSession()
				defer s.Close()
				err := tt.field.Create(s, cfUser1)
				require.Error(t, err)
				assert.True(t, IsErrCustomFieldInvalidDefinition(err), "got %v", err)
			})
		}
	})

	t.Run("update replaces options and drops values of removed ones", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		events.ClearDispatchedEvents()
		s := db.NewSession()
		defer s.Close()

		// Field 2 has options 1 (Open) and 2 (Closed); task 1 has option 1 selected.
		// Keep option 2 renamed, drop option 1, add a new one.
		f := &CustomField{
			ID:        2,
			ProjectID: 1,
			Title:     "Stage renamed",
			Type:      CustomFieldTypeSelect,
			Required:  false,
			Options: []*CustomFieldOption{
				{ID: 2, Title: "Done", Color: "#123456"},
				{Title: "Brand new"},
			},
		}
		can, err := f.CanUpdate(s, cfUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, f.Update(s, cfUser1))
		events.DispatchPending(context.Background(), s)
		require.NoError(t, s.Commit())

		assert.Equal(t, "Stage renamed", f.Title)
		assert.Equal(t, int64(1), f.CreatedByID, "creator is not editable")
		require.Len(t, f.Options, 2)
		assert.Equal(t, int64(2), f.Options[0].ID)
		assert.Equal(t, "Done", f.Options[0].Title)
		assert.NotZero(t, f.Options[1].ID)

		db.AssertMissing(t, "custom_field_options", map[string]interface{}{"id": 1})
		db.AssertExists(t, "custom_field_options", map[string]interface{}{"id": 2, "title": "Done", "color": "#123456"}, false)
		db.AssertMissing(t, "task_custom_field_values", map[string]interface{}{"custom_field_id": 2, "option_id": 1})
		// Values of other fields are untouched.
		db.AssertExists(t, "task_custom_field_values", map[string]interface{}{"id": 1}, false)
		events.AssertDispatched(t, &CustomFieldUpdatedEvent{})
	})

	t.Run("update keeps the position when omitted", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		f := &CustomField{ID: 3, ProjectID: 1, Title: "Budget 2", Type: CustomFieldTypeNumber}
		require.NoError(t, f.Update(s, cfUser1))
		require.NoError(t, s.Commit())

		assert.InDelta(t, 196608.0, f.Position, 0.001)
		assert.Nil(t, f.Config, "a missing config is cleared on PUT")
	})

	t.Run("update refuses a type change", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		err := (&CustomField{ID: 1, ProjectID: 1, Title: "x", Type: CustomFieldTypeNumber}).Update(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrCustomFieldTypeImmutable(err))
	})

	t.Run("update refuses an option of another field", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		f := &CustomField{
			ID:        2,
			ProjectID: 1,
			Title:     "Stage",
			Type:      CustomFieldTypeSelect,
			Options:   []*CustomFieldOption{{ID: 3, Title: "Red"}},
		}
		err := f.Update(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrCustomFieldOptionDoesNotExist(err))
	})

	t.Run("update of a field in another project does not exist", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		can, err := (&CustomField{ID: 1, ProjectID: 11, Title: "x", Type: CustomFieldTypeText}).CanUpdate(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrCustomFieldDoesNotExist(err))
		assert.False(t, can)
	})

	t.Run("delete removes options and values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		events.ClearDispatchedEvents()
		s := db.NewSession()
		defer s.Close()

		f := &CustomField{ID: 4, ProjectID: 1}
		can, err := f.CanDelete(s, cfUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, f.Delete(s, cfUser1))
		events.DispatchPending(context.Background(), s)
		require.NoError(t, s.Commit())

		db.AssertMissing(t, "custom_fields", map[string]interface{}{"id": 4})
		db.AssertCount(t, "custom_field_options", builder.Eq{"custom_field_id": 4}, 0)
		db.AssertCount(t, "task_custom_field_values", builder.Eq{"custom_field_id": 4}, 0)
		db.AssertExists(t, "custom_fields", map[string]interface{}{"id": 1}, false)
		db.AssertExists(t, "task_custom_field_values", map[string]interface{}{"id": 1}, false)
		events.AssertDispatched(t, &CustomFieldDeletedEvent{})
	})

	t.Run("read all", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		result, count, total, err := (&CustomField{ProjectID: 1}).ReadAll(s, cfUser1, "", 0, 0)
		require.NoError(t, err)
		fields := result.([]*CustomField)
		require.Len(t, fields, 4)
		assert.Equal(t, 4, count)
		assert.Equal(t, int64(4), total)
		assert.Equal(t, []int64{1, 2, 3, 4}, []int64{fields[0].ID, fields[1].ID, fields[2].ID, fields[3].ID})
		assert.Len(t, fields[1].Options, 2, "options are embedded")
		assert.Empty(t, fields[0].Options)
		require.NotNil(t, fields[2].Config)
		assert.InDelta(t, 1000.0, *fields[2].Config.Max, 0.001)

		result, _, total, err = (&CustomField{ProjectID: 1}).ReadAll(s, cfUser1, "Bud", 0, 0)
		require.NoError(t, err)
		assert.Len(t, result.([]*CustomField), 1)
		assert.Equal(t, int64(1), total)

		result, count, total, err = (&CustomField{ProjectID: 1}).ReadAll(s, cfUser1, "", 2, 3)
		require.NoError(t, err)
		assert.Len(t, result.([]*CustomField), 1)
		assert.Equal(t, 1, count)
		assert.Equal(t, int64(4), total)

		_, _, _, err = (&CustomField{ProjectID: 1}).ReadAll(s, cfUser2, "", 0, 0)
		require.Error(t, err)
		assert.True(t, IsErrGenericForbidden(err))
	})
}

func TestCustomField_Permissions(t *testing.T) {
	shareRead := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}
	shareWrite := &LinkSharing{ID: 2, ProjectID: 1, Permission: PermissionWrite}
	shareAdmin := &LinkSharing{ID: 3, ProjectID: 1, Permission: PermissionAdmin}

	tests := []struct {
		name       string
		projectID  int64
		auth       web.Auth
		wantRead   bool
		wantManage bool
	}{
		{"owner", 1, cfUser1, true, true},
		{"admin on a shared project", 11, cfUser1, true, true},
		{"write user cannot manage", 10, cfUser1, true, false},
		{"read user cannot manage", 9, cfUser1, true, false},
		{"stranger", 1, cfUser2, false, false},
		{"read link share", 1, shareRead, true, false},
		{"write link share cannot manage", 1, shareWrite, true, false},
		{"admin link share", 1, shareAdmin, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			s := db.NewSession()
			defer s.Close()

			f := &CustomField{ProjectID: tt.projectID, Title: "x", Type: CustomFieldTypeText}
			can, err := f.CanCreate(s, tt.auth)
			require.NoError(t, err)
			assert.Equal(t, tt.wantManage, can, "create")

			// Field 5 lives in project 9, field 6 in 10, field 1 in 1; 11 has none.
			fieldID := map[int64]int64{1: 1, 9: 5, 10: 6}[tt.projectID]
			if fieldID == 0 {
				return
			}
			existing := &CustomField{ID: fieldID, ProjectID: tt.projectID}
			can, _, err = existing.CanRead(s, tt.auth)
			if !tt.wantRead {
				// Permission denied is reported as "cannot", never as a 404.
				assert.False(t, can)
				return
			}
			require.NoError(t, err)
			assert.True(t, can, "read")

			can, err = (&CustomField{ID: fieldID, ProjectID: tt.projectID}).CanUpdate(s, tt.auth)
			require.NoError(t, err)
			assert.Equal(t, tt.wantManage, can, "update")

			can, err = (&CustomField{ID: fieldID, ProjectID: tt.projectID}).CanDelete(s, tt.auth)
			require.NoError(t, err)
			assert.Equal(t, tt.wantManage, can, "delete")
		})
	}
}

func TestCustomFieldValue_Permissions(t *testing.T) {
	shareRead := &LinkSharing{ID: 1, ProjectID: 1, Permission: PermissionRead}
	shareWrite := &LinkSharing{ID: 2, ProjectID: 1, Permission: PermissionWrite}

	tests := []struct {
		name      string
		taskID    int64
		fieldID   int64
		auth      web.Auth
		wantRead  bool
		wantWrite bool
	}{
		{"owner", 1, 1, cfUser1, true, true},
		{"read user", 18, 5, cfUser1, true, false},
		{"write user", 19, 6, cfUser1, true, true},
		{"stranger", 1, 1, cfUser2, false, false},
		{"read link share", 1, 1, shareRead, true, false},
		{"write link share", 1, 1, shareWrite, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			s := db.NewSession()
			defer s.Close()

			can, _, err := (&CustomFieldValue{TaskID: tt.taskID}).CanRead(s, tt.auth)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRead, can, "read")

			can, err = (&CustomFieldValue{TaskID: tt.taskID, CustomFieldID: tt.fieldID}).CanUpdate(s, tt.auth)
			require.NoError(t, err)
			assert.Equal(t, tt.wantWrite, can, "update")

			can, err = (&CustomFieldValue{TaskID: tt.taskID, CustomFieldID: tt.fieldID}).CanDelete(s, tt.auth)
			require.NoError(t, err)
			assert.Equal(t, tt.wantWrite, can, "delete")
		})
	}

	t.Run("read all is refused without read access", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		_, _, _, err := (&CustomFieldValue{TaskID: 1}).ReadAll(s, cfUser2, "", 0, 0)
		require.Error(t, err)
		assert.True(t, IsErrGenericForbidden(err))
	})

	t.Run("a field of another project does not exist for the task", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		// Field 5 belongs to project 9, task 1 to project 1.
		can, err := (&CustomFieldValue{TaskID: 1, CustomFieldID: 5}).CanUpdate(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrCustomFieldDoesNotExist(err))
		assert.False(t, can)
	})
}

func TestCustomFieldValue_ReadAll(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	values := readCustomFieldValues(t, 1)
	require.Len(t, values, 4, "one entry per field of the project, set or not")
	assert.Equal(t, "REF-1", values[1])
	assert.Equal(t, int64(1), values[2])
	assert.Nil(t, values[3], "unset fields are included with a null value")
	assert.Equal(t, []int64{3, 4}, values[4])

	// Task 3 has no values at all.
	values = readCustomFieldValues(t, 3)
	require.Len(t, values, 4)
	for id, v := range values {
		assert.Nil(t, v, "field %d", id)
	}
}

func TestCustomFieldValue_SetAndClear(t *testing.T) {
	type setCase struct {
		name    string
		fieldID func(t *testing.T) int64
		value   any
		want    any
	}

	date := time.Date(2026, 11, 22, 10, 30, 0, 0, time.UTC)

	// Extra fields are created in project 1 on demand.
	create := func(fieldType CustomFieldType, config *CustomFieldConfig, options ...string) func(t *testing.T) int64 {
		return func(t *testing.T) int64 {
			f := &CustomField{ProjectID: 1, Title: string(fieldType), Type: fieldType, Config: config}
			for _, o := range options {
				f.Options = append(f.Options, &CustomFieldOption{Title: o})
			}
			return createCustomField(t, f).ID
		}
	}
	existing := func(id int64) func(t *testing.T) int64 {
		return func(*testing.T) int64 { return id }
	}

	valid := []setCase{
		{"text", existing(1), "hello", "hello"},
		{"long_text", create(CustomFieldTypeLongText, nil), "line one\nline two", "line one\nline two"},
		{"number in range", existing(3), 12.5, 12.5},
		{"number bound min", existing(3), 0, 0.0},
		{"number bound max", existing(3), 1000, 1000.0},
		{"currency", create(CustomFieldTypeCurrency, &CustomFieldConfig{Currency: "TRY", Min: Ptr(0.0)}), 99.99, 99.99},
		{"date", create(CustomFieldTypeDate, nil), "2026-11-22T13:30:00+03:00", date},
		{"checkbox true", create(CustomFieldTypeCheckbox, nil), true, true},
		{"checkbox false", create(CustomFieldTypeCheckbox, nil), false, false},
		{"select", existing(2), 2, int64(2)},
		{"multi_select dedupes", existing(4), []any{4.0, 3.0, 4.0}, []int64{4, 3}},
		{"user", create(CustomFieldTypeUser, nil), []any{1.0}, []int64{1}},
		{"url", create(CustomFieldTypeURL, nil), "https://example.com/a?b=c", "https://example.com/a?b=c"},
		{"email", create(CustomFieldTypeEmail, nil), "jane@example.com", "jane@example.com"},
		{"rating default max", create(CustomFieldTypeRating, nil), 5, 5.0},
		{"rating custom max", create(CustomFieldTypeRating, &CustomFieldConfig{MaxRating: Ptr(10)}), 10, 10.0},
		{"progress", create(CustomFieldTypeProgress, nil), 40, 40.0},
		{"progress zero", create(CustomFieldTypeProgress, nil), 0, 0.0},
	}
	for _, tt := range valid {
		t.Run("set "+tt.name, func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			fieldID := tt.fieldID(t)

			v, err := setCustomFieldValue(t, 1, fieldID, tt.value, cfUser1)
			require.NoError(t, err)
			assert.Equal(t, tt.want, v.Value)

			// What was stored is what a later read returns.
			assert.Equal(t, tt.want, readCustomFieldValues(t, 1)[fieldID])

			require.NoError(t, clearCustomFieldValue(t, 1, fieldID, cfUser1))
			assert.Nil(t, readCustomFieldValues(t, 1)[fieldID])
			db.AssertCount(t, "task_custom_field_values", builder.And(
				builder.Eq{"task_id": 1},
				builder.Eq{"custom_field_id": fieldID},
			), 0)
		})
	}

	invalid := []setCase{
		{"text with a number", existing(1), 5, nil},
		{"number below min", existing(3), -0.1, nil},
		{"number above max", existing(3), 1000.5, nil},
		{"number as string", existing(3), "12", nil},
		{"currency below min", create(CustomFieldTypeCurrency, &CustomFieldConfig{Min: Ptr(0.0)}), -1, nil},
		{"date garbage", create(CustomFieldTypeDate, nil), "tomorrow", nil},
		{"date as number", create(CustomFieldTypeDate, nil), 5, nil},
		{"checkbox as string", create(CustomFieldTypeCheckbox, nil), "true", nil},
		{"select option of another field", existing(2), 3, nil},
		{"select as array", existing(2), []any{1.0}, nil},
		{"select non-integer", existing(2), 1.5, nil},
		{"multi_select option of another field", existing(4), []any{3.0, 1.0}, nil},
		{"multi_select as single id", existing(4), 3, nil},
		{"user without project access", create(CustomFieldTypeUser, nil), []any{2.0}, nil},
		{"user that does not exist", create(CustomFieldTypeUser, nil), []any{99999.0}, nil},
		{"user as single id", create(CustomFieldTypeUser, nil), 1, nil},
		{"url without scheme", create(CustomFieldTypeURL, nil), "example.com", nil},
		{"url with another scheme", create(CustomFieldTypeURL, nil), "javascript:alert(1)", nil},
		{"email without domain", create(CustomFieldTypeEmail, nil), "jane@", nil},
		{"email with display name", create(CustomFieldTypeEmail, nil), "Jane <jane@example.com>", nil},
		{"rating zero", create(CustomFieldTypeRating, nil), 0, nil},
		{"rating above default max", create(CustomFieldTypeRating, nil), 6, nil},
		{"rating above custom max", create(CustomFieldTypeRating, &CustomFieldConfig{MaxRating: Ptr(3)}), 4, nil},
		{"rating fractional", create(CustomFieldTypeRating, nil), 2.5, nil},
		{"progress above 100", create(CustomFieldTypeProgress, nil), 101, nil},
		{"progress negative", create(CustomFieldTypeProgress, nil), -1, nil},
	}
	for _, tt := range invalid {
		t.Run("reject "+tt.name, func(t *testing.T) {
			db.LoadAndAssertFixtures(t)
			fieldID := tt.fieldID(t)

			before := readCustomFieldValues(t, 1)[fieldID]
			_, err := setCustomFieldValue(t, 1, fieldID, tt.value, cfUser1)
			require.Error(t, err)
			assert.True(t, IsErrCustomFieldInvalidValue(err), "got %v", err)
			assert.Equal(t, before, readCustomFieldValues(t, 1)[fieldID], "a rejected value must not change anything")
		})
	}

	t.Run("setting replaces the previous value", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		_, err := setCustomFieldValue(t, 1, 4, []any{3.0}, cfUser1)
		require.NoError(t, err)
		assert.Equal(t, []int64{3}, readCustomFieldValues(t, 1)[4])
		db.AssertCount(t, "task_custom_field_values", builder.And(
			builder.Eq{"task_id": 1},
			builder.Eq{"custom_field_id": 4},
		), 1)
	})

	t.Run("null and empty arrays clear", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		_, err := setCustomFieldValue(t, 1, 1, nil, cfUser1)
		require.NoError(t, err)
		assert.Nil(t, readCustomFieldValues(t, 1)[1])

		_, err = setCustomFieldValue(t, 1, 4, []any{}, cfUser1)
		require.NoError(t, err)
		assert.Nil(t, readCustomFieldValues(t, 1)[4])
	})

	t.Run("clearing an unset value is fine", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		require.NoError(t, clearCustomFieldValue(t, 1, 3, cfUser1))
	})

	t.Run("forbidden for readers and strangers", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		_, err := setCustomFieldValue(t, 18, 5, "nope", cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrGenericForbidden(err))

		_, err = setCustomFieldValue(t, 1, 1, "nope", cfUser2)
		require.Error(t, err)
		assert.True(t, IsErrGenericForbidden(err))
		assert.Equal(t, "REF-1", readCustomFieldValues(t, 1)[1])
	})

	t.Run("write user and link share can set", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)

		_, err := setCustomFieldValue(t, 19, 6, "by a writer", cfUser1)
		require.NoError(t, err)

		_, err = setCustomFieldValue(t, 1, 1, "by a share", &LinkSharing{ID: 2, ProjectID: 1, Permission: PermissionWrite})
		require.NoError(t, err)
		assert.Equal(t, "by a share", readCustomFieldValues(t, 1)[1])
	})

	t.Run("dispatches an event with old and new value", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		events.ClearDispatchedEvents()

		s := db.NewSession()
		defer s.Close()
		v := &CustomFieldValue{TaskID: 1, CustomFieldID: 1, Value: "REF-9"}
		can, err := v.CanUpdate(s, cfUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, v.Update(s, cfUser1))
		events.DispatchPending(context.Background(), s)
		require.NoError(t, s.Commit())

		dispatched := events.GetDispatchedEvents((&TaskCustomFieldValueUpdatedEvent{}).Name())
		require.Len(t, dispatched, 1)
		e := dispatched[0].(*TaskCustomFieldValueUpdatedEvent)
		assert.Equal(t, int64(1), e.Task.ID)
		assert.Equal(t, int64(1), e.CustomField.ID)
		assert.Equal(t, "REF-1", e.OldValue)
		assert.Equal(t, "REF-9", e.NewValue)
		require.NotNil(t, e.Doer)
		assert.Equal(t, int64(1), e.Doer.ID)

		events.ClearDispatchedEvents()
		s2 := db.NewSession()
		defer s2.Close()
		same := &CustomFieldValue{TaskID: 1, CustomFieldID: 1, Value: "REF-9"}
		require.NoError(t, same.Update(s2, cfUser1))
		events.DispatchPending(context.Background(), s2)
		require.NoError(t, s2.Commit())
		assert.Zero(t, events.CountDispatchedEvents((&TaskCustomFieldValueUpdatedEvent{}).Name()), "no event when nothing changed")
	})
}

func TestCustomField_Lifecycle(t *testing.T) {
	t.Run("deleting a task removes its values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		task := &Task{ID: 1}
		require.NoError(t, task.Delete(s, cfUser1))
		// Soft delete keeps the values so a restore brings them back.
		db.AssertCount(t, "task_custom_field_values", builder.Eq{"task_id": 1}, 4)

		// The cleanup cron purges the trashed task.
		require.NoError(t, hardDeleteTask(s, &Task{ID: 1, ProjectID: 1}))
		require.NoError(t, s.Commit())

		db.AssertCount(t, "task_custom_field_values", builder.Eq{"task_id": 1}, 0)
		db.AssertExists(t, "task_custom_field_values", map[string]interface{}{"id": 5}, false)
	})

	t.Run("deleting a project removes its definitions", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()

		require.NoError(t, hardDeleteProject(s, &Project{ID: 1}, nil))
		require.NoError(t, s.Commit())

		db.AssertCount(t, "custom_fields", builder.Eq{"project_id": 1}, 0)
		db.AssertCount(t, "custom_field_options", builder.In("custom_field_id", 1, 2, 3, 4), 0)
		db.AssertCount(t, "task_custom_field_values", builder.In("custom_field_id", 1, 2, 3, 4), 0)
		db.AssertExists(t, "custom_fields", map[string]interface{}{"id": 5}, false)
	})

	t.Run("duplicating a task in the same project copies the values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		files.InitTestFileFixtures(t)
		s := db.NewSession()
		defer s.Close()

		td := &TaskDuplicate{TaskID: 1}
		require.NoError(t, td.Create(s, cfUser1))
		require.NoError(t, s.Commit())
		require.NotZero(t, td.Task.ID)
		assert.NotEqual(t, int64(1), td.Task.ID)

		values := readCustomFieldValues(t, td.Task.ID)
		assert.Equal(t, "REF-1", values[1])
		assert.Equal(t, int64(1), values[2])
		assert.ElementsMatch(t, []int64{3, 4}, values[4])
		// The original keeps its own.
		assert.Equal(t, "REF-1", readCustomFieldValues(t, 1)[1])
	})

	t.Run("duplicating a project copies definitions and values", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		files.InitTestFileFixtures(t)
		s := db.NewSession()
		defer s.Close()

		pd := &ProjectDuplicate{ProjectID: 1}
		can, err := pd.CanCreate(s, cfUser1)
		require.NoError(t, err)
		require.True(t, can)
		require.NoError(t, pd.Create(s, cfUser1))
		require.NoError(t, s.Commit())
		require.NotZero(t, pd.Project.ID)

		s2 := db.NewSession()
		defer s2.Close()
		result, _, _, err := (&CustomField{ProjectID: pd.Project.ID}).ReadAll(s2, cfUser1, "", 0, 0)
		require.NoError(t, err)
		fields := result.([]*CustomField)
		require.Len(t, fields, 4)
		assert.Equal(t, "Reference", fields[0].Title)
		assert.Equal(t, "Stage", fields[1].Title)
		require.Len(t, fields[1].Options, 2)
		// The copy has its own options, not the originals.
		assert.NotEqual(t, int64(1), fields[1].Options[0].ID)
		assert.NotEqual(t, int64(2), fields[1].Options[1].ID)
		assert.Equal(t, "Open", fields[1].Options[0].Title)
		assert.NotEqual(t, int64(1), fields[0].ID)

		// Values of the copied tasks reference the copied fields and options.
		copiedTaskIDs := []int64{}
		err = s2.Table("tasks").Where("project_id = ? AND title = ?", pd.Project.ID, "task #1").Cols("id").Find(&copiedTaskIDs)
		require.NoError(t, err)
		require.Len(t, copiedTaskIDs, 1)
		values := map[int64]any{}
		rows, err := getCustomFieldValueRows(s2, copiedTaskIDs[0], []int64{fields[0].ID, fields[1].ID, fields[3].ID})
		require.NoError(t, err)
		for _, f := range fields {
			values[f.ID] = rowsToValue(f.Type, rows[f.ID])
		}
		assert.Equal(t, "REF-1", values[fields[0].ID])
		assert.Equal(t, fields[1].Options[0].ID, values[fields[1].ID])
		assert.ElementsMatch(t, []int64{fields[3].Options[0].ID, fields[3].Options[1].ID}, values[fields[3].ID])

		// The source project is untouched.
		db.AssertExists(t, "custom_fields", map[string]interface{}{"id": 1, "project_id": 1}, false)
	})
}

// The permission check reads the task without a lock. A move that lands between the check and the write must not
// leave a value for a field of the project the task left.
func TestCustomFieldValue_TaskMovedBetweenCheckAndWrite(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	s := db.NewSession()
	defer s.Close()

	// Field 3 belongs to project 1, like task 1.
	v := &CustomFieldValue{TaskID: 1, CustomFieldID: 3, Value: 5.0}
	can, err := v.CanUpdate(s, cfUser1)
	require.NoError(t, err)
	require.True(t, can)

	require.NoError(t, (&Task{ID: 1, ProjectID: 7}).Update(s, cfUser1))

	err = v.Update(s, cfUser1)
	require.Error(t, err)
	assert.True(t, IsErrCustomFieldDoesNotExist(err))
	n, err := s.Where("task_id = ? AND custom_field_id = ?", 1, 3).Count(&TaskCustomFieldValue{})
	require.NoError(t, err)
	assert.EqualValues(t, 0, n)
}

// Two requests that set the same single-value field at the same time must leave one row: a second row breaks
// sort_by=cf_<id> (a scalar sub-select) and counts twice in reports.
func TestCustomFieldValue_ConcurrentSetKeepsOneRow(t *testing.T) {
	if db.Type() == schemas.SQLITE {
		t.Skip("SQLite has one writer at a time")
	}
	db.LoadAndAssertFixtures(t)

	s1 := db.NewSession()
	defer s1.Close()
	v1 := &CustomFieldValue{TaskID: 2, CustomFieldID: 3, Value: 1.0}
	can, err := v1.CanUpdate(s1, cfUser1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, v1.Update(s1, cfUser1))

	done := make(chan error, 1)
	go func() {
		s2 := db.NewSession()
		defer s2.Close()
		v2 := &CustomFieldValue{TaskID: 2, CustomFieldID: 3, Value: 2.0}
		if _, err := v2.CanUpdate(s2, cfUser1); err != nil {
			done <- err
			return
		}
		if err := v2.Update(s2, cfUser1); err != nil {
			done <- err
			return
		}
		done <- s2.Commit()
	}()

	// The second request is now past its checks and waits for the first one.
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, s1.Commit())
	require.NoError(t, <-done)

	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("task_id = ? AND custom_field_id = ?", 2, 3).Count(&TaskCustomFieldValue{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}
