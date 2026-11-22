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
	"fmt"
	"net/http"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cfFilterEnv is a set of custom fields in project 1 with values on tasks 1 to 6 of that project:
//
//	num  number:   t1=10   t2=50   t3=100  t4=150  (t5, t6 unset)
//	date date:     t1=2024-01-10  t2=2024-02-10  t3=2024-03-10
//	chk  checkbox: t1=true t2=false
//	txt  text:     t1=alpha t2=beta t3=alphabet
//	user user:     t1={1} t2={2} t3={1,2}
//
// Fixture field 2 (select): t1=1 t2=2 t3=1. Fixture field 4 (multi_select): t1={3,4} t2={3} t3={4}.
type cfFilterEnv struct {
	num, date, chk, txt, user int64
}

func cfInsertValues(t *testing.T, rows ...*TaskCustomFieldValue) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	for _, r := range rows {
		_, err := s.Insert(r)
		require.NoError(t, err)
	}
	require.NoError(t, s.Commit())
}

func cfPtr[T any](v T) *T { return &v }

func setupCFFilterEnv(t *testing.T) *cfFilterEnv {
	t.Helper()
	db.LoadAndAssertFixtures(t)

	mk := func(title string, typ CustomFieldType) int64 {
		return createCustomField(t, &CustomField{ProjectID: 1, Title: title, Type: typ}).ID
	}
	env := &cfFilterEnv{
		num:  mk("Num", CustomFieldTypeNumber),
		date: mk("Date", CustomFieldTypeDate),
		chk:  mk("Chk", CustomFieldTypeCheckbox),
		txt:  mk("Txt", CustomFieldTypeText),
		user: mk("User", CustomFieldTypeUser),
	}

	day := func(d string) *time.Time {
		v, err := time.Parse("2006-01-02", d)
		require.NoError(t, err)
		return &v
	}
	var rows []*TaskCustomFieldValue
	for task, n := range map[int64]float64{1: 10, 2: 50, 3: 100, 4: 150} {
		rows = append(rows, &TaskCustomFieldValue{TaskID: task, CustomFieldID: env.num, ValueNumber: cfPtr(n)})
	}
	for task, d := range map[int64]string{1: "2024-01-10", 2: "2024-02-10", 3: "2024-03-10"} {
		rows = append(rows, &TaskCustomFieldValue{TaskID: task, CustomFieldID: env.date, ValueDate: day(d)})
	}
	rows = append(rows,
		&TaskCustomFieldValue{TaskID: 1, CustomFieldID: env.chk, ValueBool: cfPtr(true)},
		&TaskCustomFieldValue{TaskID: 2, CustomFieldID: env.chk, ValueBool: cfPtr(false)},
	)
	for task, s := range map[int64]string{1: "alpha", 2: "beta", 3: "alphabet"} {
		rows = append(rows, &TaskCustomFieldValue{TaskID: task, CustomFieldID: env.txt, ValueText: cfPtr(s)})
	}
	for task, users := range map[int64][]int64{1: {1}, 2: {2}, 3: {1, 2}} {
		for _, u := range users {
			rows = append(rows, &TaskCustomFieldValue{TaskID: task, CustomFieldID: env.user, UserID: cfPtr(u)})
		}
	}
	// Fixture field 2 already has task 1 = option 1 and field 4 task 1 = options 3 and 4.
	rows = append(rows,
		&TaskCustomFieldValue{TaskID: 2, CustomFieldID: 2, OptionID: cfPtr(int64(2))},
		&TaskCustomFieldValue{TaskID: 3, CustomFieldID: 2, OptionID: cfPtr(int64(1))},
		&TaskCustomFieldValue{TaskID: 2, CustomFieldID: 4, OptionID: cfPtr(int64(3))},
		&TaskCustomFieldValue{TaskID: 3, CustomFieldID: 4, OptionID: cfPtr(int64(4))},
	)
	cfInsertValues(t, rows...)
	return env
}

func cfTaskIDsFor(a web.Auth, projectID int64, collection TaskCollection) ([]int64, error) {
	s := db.NewSession()
	defer s.Close()
	collection.ProjectID = projectID
	res, _, _, err := collection.ReadAll(s, a, "", 1, 100)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, task := range res.([]*Task) {
		ids = append(ids, task.ID)
	}
	return ids, nil
}

// cfQuery runs the filter against tasks 1 to 6 of project 1 as user1.
func cfQuery(t *testing.T, filter string, sortBy ...string) []int64 {
	t.Helper()
	orderBy := make([]string, len(sortBy))
	by := make([]string, 0, len(sortBy))
	for i, s := range sortBy {
		by = append(by, s)
		orderBy[i] = "asc"
	}
	return cfQueryOrdered(t, filter, by, orderBy)
}

func cfQueryOrdered(t *testing.T, filter string, sortBy, orderBy []string) []int64 {
	t.Helper()
	if filter == "" {
		filter = "id <= 6"
	} else {
		filter = "(" + filter + ") && id <= 6"
	}
	ids, err := cfTaskIDsFor(cfUser1, 1, TaskCollection{Filter: filter, SortBy: sortBy, OrderBy: orderBy})
	require.NoError(t, err, filter)
	return ids
}

func TestCustomFieldFilter_Types(t *testing.T) {
	env := setupCFFilterEnv(t)
	f := func(format string, args ...any) string { return fmt.Sprintf(format, args...) }

	tests := []struct {
		name   string
		filter string
		want   []int64
	}{
		// number, currency, rating, progress: numeric comparators on value_number
		{"number =", f("cf_%d = 50", env.num), []int64{2}},
		{"number >", f("cf_%d > 50", env.num), []int64{3, 4}},
		{"number >=", f("cf_%d >= 50", env.num), []int64{2, 3, 4}},
		{"number <", f("cf_%d < 50", env.num), []int64{1}},
		{"number <=", f("cf_%d <= 50", env.num), []int64{1, 2}},
		{"number != includes tasks without value", f("cf_%d != 50", env.num), []int64{1, 3, 4, 5, 6}},
		{"number range", f("cf_%d > 10 && cf_%d < 150", env.num, env.num), []int64{2, 3}},
		{"number decimal", f("cf_%d > 99.5", env.num), []int64{3, 4}},
		// date: same formats as built-in date fields
		{"date >", f("cf_%d > 2024-01-31", env.date), []int64{2, 3}},
		{"date <=", f("cf_%d <= 2024-02-10", env.date), []int64{1, 2}},
		{"date day range", f("cf_%d > 2024-02-09 && cf_%d < 2024-02-11", env.date, env.date), []int64{2}},
		{"date != includes tasks without value", f("cf_%d != 2030-01-01", env.date), []int64{1, 2, 3, 4, 5, 6}},
		{"date math", f("cf_%d < now", env.date), []int64{1, 2, 3}},
		// checkbox: no value counts as false
		{"checkbox = true", f("cf_%d = true", env.chk), []int64{1}},
		{"checkbox = false", f("cf_%d = false", env.chk), []int64{2, 3, 4, 5, 6}},
		{"checkbox != true", f("cf_%d != true", env.chk), []int64{2, 3, 4, 5, 6}},
		{"checkbox != false", f("cf_%d != false", env.chk), []int64{1}},
		// text
		{"text =", f("cf_%d = alpha", env.txt), []int64{1}},
		{"text like", f("cf_%d like alph", env.txt), []int64{1, 3}},
		{"text !=", f("cf_%d != alpha", env.txt), []int64{2, 3, 4, 5, 6}},
		{"text fixture field", "cf_1 like REF", []int64{1, 2}},
		// select
		{"select =", "cf_2 = 1", []int64{1, 3}},
		{"select !=", "cf_2 != 1", []int64{2, 4, 5, 6}},
		{"select in", "cf_2 in 1,2", []int64{1, 2, 3}},
		{"select not in", "cf_2 not in 2", []int64{1, 3, 4, 5, 6}},
		// multi_select: matches if any selected option matches
		{"multi_select =", "cf_4 = 3", []int64{1, 2}},
		{"multi_select !=", "cf_4 != 3", []int64{3, 4, 5, 6}},
		{"multi_select in", "cf_4 in 3,4", []int64{1, 2, 3}},
		{"multi_select not in", "cf_4 not in 3,4", []int64{4, 5, 6}},
		{"multi_select in with a non-selected option", "cf_4 in 4", []int64{1, 3}},
		// user: ids or usernames
		{"user = id", f("cf_%d = 1", env.user), []int64{1, 3}},
		{"user = username", f("cf_%d = user2", env.user), []int64{2, 3}},
		{"user in mixed", f("cf_%d in 1,user2", env.user), []int64{1, 2, 3}},
		{"user !=", f("cf_%d != 1", env.user), []int64{2, 4, 5, 6}},
		{"user not in", f("cf_%d not in 1,2", env.user), []int64{4, 5, 6}},
		{"user unknown username matches nothing", f("cf_%d = nobody", env.user), []int64{}},
		// combinations with built-in fields
		{"with done", f("cf_%d >= 50 && done = true", env.num), []int64{2}},
		{"or with checkbox", f("cf_%d > 100 || cf_%d = true", env.num, env.chk), []int64{1, 4}},
		{"grouped", f("(cf_%d = 10 || cf_%d = 150) && cf_%d = alpha", env.num, env.num, env.txt), []int64{1}},
		{"several fields", f("cf_%d > 10 && cf_2 = 2", env.num), []int64{2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfQuery(t, tt.filter)
			assert.ElementsMatch(t, tt.want, got, tt.filter)
		})
	}

	t.Run("include nulls adds tasks without a value", func(t *testing.T) {
		ids, err := cfTaskIDsFor(cfUser1, 1, TaskCollection{
			Filter:             f("cf_%d > 100 && id <= 6", env.num),
			FilterIncludeNulls: true,
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, []int64{4, 5, 6}, ids)
	})

	t.Run("date honours the filter timezone", func(t *testing.T) {
		// 2024-02-10 in Berlin starts at 2024-02-09T23:00Z, so the stored UTC midnight of
		// 2024-02-09 is before it and the one of 2024-02-10 is after it.
		ids, err := cfTaskIDsFor(cfUser1, 1, TaskCollection{
			Filter:         f("cf_%d < 2024-02-10 && id <= 6", env.date),
			FilterTimezone: "Europe/Berlin",
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, []int64{1}, ids)

		ids, err = cfTaskIDsFor(cfUser1, 1, TaskCollection{
			Filter:         f("cf_%d >= 2024-02-10 && id <= 6", env.date),
			FilterTimezone: "Europe/Berlin",
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, []int64{2, 3}, ids)
	})

	t.Run("works on a subtask expansion", func(t *testing.T) {
		ids, err := cfTaskIDsFor(cfUser1, 1, TaskCollection{
			Filter: f("cf_%d = 50", env.num),
			Expand: []TaskCollectionExpandable{TaskCollectionExpandSubtasks},
		})
		require.NoError(t, err)
		assert.Equal(t, []int64{2}, ids)
	})
}

func TestCustomFieldFilter_Validation(t *testing.T) {
	env := setupCFFilterEnv(t)
	f := func(format string, args ...any) string { return fmt.Sprintf(format, args...) }

	tests := []struct {
		name   string
		filter string
		check  func(error) bool
	}{
		{"unknown field", "cf_999999 = 1", IsErrInvalidTaskField},
		{"malformed identifier", "cf_abc = 1", IsErrInvalidTaskField},
		{"zero id", "cf_0 = 1", IsErrInvalidTaskField},
		{"number with text", f("cf_%d = abc", env.num), IsErrInvalidTaskFilterValue},
		{"number like", f("cf_%d like 1", env.num), IsErrInvalidTaskFilterComparator},
		{"number in", f("cf_%d in 1,2", env.num), IsErrInvalidTaskFilterComparator},
		{"date with garbage", f("cf_%d > notadate", env.date), IsErrInvalidTaskFilterValue},
		{"date like", f("cf_%d like 2024", env.date), IsErrInvalidTaskFilterComparator},
		{"checkbox >", f("cf_%d > true", env.chk), IsErrInvalidTaskFilterComparator},
		{"checkbox non bool", f("cf_%d = maybe", env.chk), IsErrInvalidTaskFilterValue},
		{"text >", f("cf_%d > a", env.txt), IsErrInvalidTaskFilterComparator},
		{"text in", f("cf_%d in a,b", env.txt), IsErrInvalidTaskFilterComparator},
		{"select like", "cf_2 like 1", IsErrInvalidTaskFilterComparator},
		{"select with non number", "cf_2 = open", IsErrInvalidTaskFilterValue},
		{"select = list", "cf_2 = 1,2", IsErrInvalidTaskFilterValue},
		{"multi_select >", "cf_4 > 1", IsErrInvalidTaskFilterComparator},
		{"user like", f("cf_%d like a", env.user), IsErrInvalidTaskFilterComparator},
		{"nested group", f("(cf_%d = abc || done = true)", env.num), IsErrInvalidTaskFilterValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cfTaskIDsFor(cfUser1, 1, TaskCollection{Filter: tt.filter})
			require.Error(t, err, tt.filter)
			assert.True(t, tt.check(err), "unexpected error type %T: %v", err, err)
			httpErr, ok := err.(web.HTTPErrorProcessor)
			require.True(t, ok)
			assert.Equal(t, http.StatusBadRequest, httpErr.HTTPError().HTTPCode)
		})
	}
}

func TestCustomFieldFilter_Sort(t *testing.T) {
	env := setupCFFilterEnv(t)
	by := func(id int64) string { return fmt.Sprintf("cf_%d", id) }

	t.Run("number asc, tasks without value last", func(t *testing.T) {
		got := cfQueryOrdered(t, "", []string{by(env.num)}, []string{"asc"})
		assert.Equal(t, []int64{1, 2, 3, 4, 5, 6}, got)
	})
	t.Run("number desc, tasks without value still last", func(t *testing.T) {
		got := cfQueryOrdered(t, "", []string{by(env.num)}, []string{"desc"})
		assert.Equal(t, []int64{4, 3, 2, 1, 5, 6}, got)
	})
	t.Run("date", func(t *testing.T) {
		assert.Equal(t, []int64{1, 2, 3, 4, 5, 6}, cfQueryOrdered(t, "", []string{by(env.date)}, []string{"asc"}))
		assert.Equal(t, []int64{3, 2, 1, 4, 5, 6}, cfQueryOrdered(t, "", []string{by(env.date)}, []string{"desc"}))
	})
	t.Run("text", func(t *testing.T) {
		assert.Equal(t, []int64{1, 3, 2, 4, 5, 6}, cfQueryOrdered(t, "", []string{by(env.txt)}, []string{"asc"}))
		assert.Equal(t, []int64{2, 3, 1, 4, 5, 6}, cfQueryOrdered(t, "", []string{by(env.txt)}, []string{"desc"}))
	})
	t.Run("checkbox", func(t *testing.T) {
		assert.Equal(t, []int64{2, 1, 3, 4, 5, 6}, cfQueryOrdered(t, "", []string{by(env.chk)}, []string{"asc"}))
		assert.Equal(t, []int64{1, 2, 3, 4, 5, 6}, cfQueryOrdered(t, "", []string{by(env.chk)}, []string{"desc"}))
	})
	t.Run("select sorts by option position", func(t *testing.T) {
		// option 1 (Open) comes before option 2 (Closed)
		assert.Equal(t, []int64{1, 3, 2, 4, 5, 6}, cfQueryOrdered(t, "", []string{"cf_2"}, []string{"asc"}))
		assert.Equal(t, []int64{2, 1, 3, 4, 5, 6}, cfQueryOrdered(t, "", []string{"cf_2"}, []string{"desc"}))
	})
	t.Run("combined with a filter and a second sort key", func(t *testing.T) {
		got := cfQueryOrdered(t, fmt.Sprintf("cf_%d >= 50", env.num), []string{by(env.chk), by(env.num)}, []string{"asc", "desc"})
		assert.Equal(t, []int64{2, 4, 3}, got)
	})
	t.Run("distinct query with a bucket_id filter", func(t *testing.T) {
		ids, err := cfTaskIDsFor(cfUser1, 1, TaskCollection{
			Filter:             "bucket_id > 0 && id <= 6",
			FilterIncludeNulls: true,
			SortBy:             []string{by(env.num)},
			OrderBy:            []string{"desc"},
		})
		require.NoError(t, err)
		assert.Equal(t, []int64{4, 3, 2, 1, 5, 6}, ids)
	})

	rejected := []struct {
		name string
		sort string
	}{
		{"multi_select", "cf_4"},
		{"user", by(env.user)},
		{"unknown field", "cf_999999"},
	}
	for _, tt := range rejected {
		t.Run("rejects "+tt.name, func(t *testing.T) {
			_, err := cfTaskIDsFor(cfUser1, 1, TaskCollection{SortBy: []string{tt.sort}, OrderBy: []string{"asc"}})
			require.Error(t, err)
			assert.True(t, IsErrInvalidTaskField(err), "%T: %v", err, err)
		})
	}
}

func TestCustomFieldFilter_NoLeak(t *testing.T) {
	env := setupCFFilterEnv(t)

	// user2 has no access to project 1, so none of its fields is visible to them. An existing
	// but unreadable field must be indistinguishable from a field that does not exist.
	for _, filter := range []string{
		"cf_1 = REF-1",
		"cf_1 != REF-1",
		fmt.Sprintf("cf_%d > 0", env.num),
		"cf_2 in 1,2",
	} {
		_, err := cfTaskIDsFor(cfUser2, 0, TaskCollection{Filter: filter})
		require.Error(t, err, filter)
		assert.True(t, IsErrInvalidTaskField(err), "%T: %v", err, err)
	}
	_, unknownErr := cfTaskIDsFor(cfUser2, 0, TaskCollection{Filter: "cf_999999 = REF-1"})
	_, hiddenErr := cfTaskIDsFor(cfUser2, 0, TaskCollection{Filter: "cf_1 = REF-1"})
	assert.Equal(t, fmt.Sprintf("%T", unknownErr), fmt.Sprintf("%T", hiddenErr))

	// Same for sorting.
	_, err := cfTaskIDsFor(cfUser2, 0, TaskCollection{SortBy: []string{"cf_1"}, OrderBy: []string{"asc"}})
	require.Error(t, err)
	assert.True(t, IsErrInvalidTaskField(err))

	// A reader of project 1 can use the field, and never sees tasks of projects they cannot read.
	ids, err := cfTaskIDsFor(cfUser1, 0, TaskCollection{Filter: "cf_1 like REF"})
	require.NoError(t, err)
	assert.Contains(t, ids, int64(1))
	assert.NotContains(t, ids, int64(19))

	// A user's own fields never match tasks outside the field's project: task 20 lives in
	// project 11 and holds no value of project 1 fields.
	ids, err = cfTaskIDsFor(cfUser1, 0, TaskCollection{Filter: "cf_1 = REF-1"})
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, ids)

	// Tasks of an unreadable project never show up through a field of a readable one.
	ids, err = cfTaskIDsFor(cfUser1, 0, TaskCollection{Filter: "cf_5 != nothing"})
	require.NoError(t, err)
	s := db.NewSession()
	defer s.Close()
	for _, id := range ids {
		task := &Task{ID: id}
		require.NoError(t, task.ReadOne(s, cfUser1))
		can, _, err := (&Project{ID: task.ProjectID}).CanRead(s, cfUser1)
		require.NoError(t, err)
		assert.True(t, can, "task %d of project %d is not readable", id, task.ProjectID)
	}
}

func TestCustomFieldFilter_SavedFilter(t *testing.T) {
	env := setupCFFilterEnv(t)
	s := db.NewSession()
	defer s.Close()

	valid := &SavedFilter{
		Title:   "by custom field",
		Filters: &TaskCollection{Filter: fmt.Sprintf("cf_%d > 10 && done = false", env.num)},
	}
	require.NoError(t, valid.Create(s, cfUser1))

	// Syntactically valid cf_ filters are accepted when saving; type errors surface when evaluated.
	_, err := getTaskFiltersFromFilterString("cf_12 > 100 && cf_3 = 5 && done = false", "")
	require.NoError(t, err)

	cond, joinBuckets, err := parseFilterCond(s, cfUser1, valid.Filters.Filter, "", false)
	require.NoError(t, err)
	assert.False(t, joinBuckets)
	assert.NotNil(t, cond)

	_, _, err = parseFilterCond(s, cfUser2, valid.Filters.Filter, "", false)
	require.Error(t, err)
	assert.True(t, isErrInvalidFilter(err))
}

func TestCustomFieldValues_RemovedOnTaskMove(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	// A field of the target project and one value on it that must survive the move.
	s := db.NewSession()
	_, err := s.Insert(&CustomField{ProjectID: 7, Title: "Target field", Type: CustomFieldTypeText, Position: 65536, CreatedByID: 1})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()
	s = db.NewSession()
	target := &CustomField{}
	_, err = s.Where("project_id = 7").Get(target)
	require.NoError(t, err)
	s.Close()
	cfInsertValues(t, &TaskCustomFieldValue{TaskID: 1, CustomFieldID: target.ID, ValueText: cfPtr("keep")})

	// Task 1 holds values for the project 1 fields 1, 2 and 4 (fixtures).
	count := func(taskID int64, fieldIDs ...int64) int64 {
		s := db.NewSession()
		defer s.Close()
		n, err := s.Table("task_custom_field_values").Where("task_id = ?", taskID).In("custom_field_id", fieldIDs).Count()
		require.NoError(t, err)
		return n
	}
	require.EqualValues(t, 4, count(1, 1, 2, 4))

	s = db.NewSession()
	defer s.Close()
	moved := &Task{ID: 1, ProjectID: 7}
	require.NoError(t, moved.Update(s, cfUser1))
	require.NoError(t, s.Commit())

	assert.EqualValues(t, 0, count(1, 1, 2, 4), "values of fields of the old project are removed")
	assert.EqualValues(t, 1, count(1, target.ID), "values of fields of the new project stay")
	assert.EqualValues(t, 1, count(2, 1), "other tasks of the old project keep their values")

	t.Run("update without a project change keeps values", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		upd := &Task{ID: 2, Title: "renamed", ProjectID: 1}
		require.NoError(t, upd.Update(s, cfUser1))
		require.NoError(t, s.Commit())
		assert.EqualValues(t, 1, count(2, 1))
	})

	t.Run("bulk move", func(t *testing.T) {
		cfInsertValues(t, &TaskCustomFieldValue{TaskID: 3, CustomFieldID: 1, ValueText: cfPtr("bulk")})
		s := db.NewSession()
		defer s.Close()
		bt := &BulkTask{TaskIDs: []int64{3}, Fields: []string{"project_id"}, Values: &Task{ProjectID: 7}}
		require.NoError(t, bt.Update(s, cfUser1))
		require.NoError(t, s.Commit())
		assert.EqualValues(t, 0, count(3, 1))
	})
}
