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
	"strconv"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/files"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Duplicating a project through the real ProjectDuplicate flow: the form of the copy points at the copied workflow
// status, the copied custom fields and their copied options (a single option and a list of options), and it is
// disabled with the plain "duplicated" reason, not the one for a reference without a copy.
func TestForm_DuplicatingAProjectRemapsEverythingTheFormNames(t *testing.T) {
	formsReset(t)
	files.InitTestFileFixtures(t)

	source := mustSaveWorkflow(t, baseWorkflowDoc(1), cfUser1)
	doing := statusIDByTitle(source, "Doing")
	require.NotZero(t, doing)

	f := baseForm(FormAccessMembers,
		fld("title", "T", true),
		&FormField{Target: "cf_2", Label: "Stage", Default: 2},
		&FormField{Target: "cf_4", Label: "Tags", Default: []any{3, 4}},
		fld("cf_1", "Ref", false),
	)
	f.Settings.InitialStatusID = doing
	createForm(t, f)

	s := db.NewSession()
	defer s.Close()
	pd := &ProjectDuplicate{ProjectID: 1}
	can, err := pd.CanCreate(s, cfUser1)
	require.NoError(t, err)
	require.True(t, can)
	require.NoError(t, pd.Create(s, cfUser1))
	require.NoError(t, s.Commit())

	copies := []*Form{}
	s2 := db.NewSession()
	defer s2.Close()
	require.NoError(t, s2.Where("project_id = ?", pd.Project.ID).Find(&copies))
	require.Len(t, copies, 1)
	c := copies[0]

	assert.False(t, c.Enabled, "a copy waits to be checked")
	assert.Equal(t, FormDisabledDuplicated, c.DisabledReason, "everything it names was copied")
	assert.NotEqual(t, f.Token, c.Token)

	copiedWorkflow := readWorkflow(t, pd.Project.ID)
	assert.Equal(t, statusIDByTitle(copiedWorkflow, "Doing"), c.Settings.InitialStatusID)
	assert.NotEqual(t, doing, c.Settings.InitialStatusID)

	// The copied fields keep their order and type; the copied options of cf_2 and cf_4 are the new rows.
	copiedFields := []*CustomField{}
	require.NoError(t, s2.Where("project_id = ?", pd.Project.ID).OrderBy("position ASC, id ASC").Find(&copiedFields))
	byTitle := map[string]*CustomField{}
	for _, cf := range copiedFields {
		byTitle[cf.Title] = cf
	}
	require.NotEmpty(t, byTitle)
	optionIDs := func(cf *CustomField) []int64 {
		opts := []*CustomFieldOption{}
		require.NoError(t, s2.Where("custom_field_id = ?", cf.ID).OrderBy("position ASC, id ASC").Find(&opts))
		ids := make([]int64, 0, len(opts))
		for _, o := range opts {
			ids = append(ids, o.ID)
		}
		return ids
	}

	targets := map[string]*FormField{}
	for _, field := range c.Fields {
		targets[field.Label] = field
	}
	stage := targets["Stage"]
	require.NotNil(t, stage)
	srcStage, srcTags := &CustomField{ID: 2}, &CustomField{ID: 4}
	_, err = s2.Get(srcStage)
	require.NoError(t, err)
	_, err = s2.Get(srcTags)
	require.NoError(t, err)
	newStage, newTags := byTitle[srcStage.Title], byTitle[srcTags.Title]
	require.NotNil(t, newStage)
	require.NotNil(t, newTags)
	assert.Equal(t, "cf_"+strconv.FormatInt(newStage.ID, 10), stage.Target)
	def, ok := toInt(stage.Default)
	require.True(t, ok)
	assert.Equal(t, optionIDs(newStage)[1], def, "the default is the copy of option 2, the second option of the field")

	tags := targets["Tags"]
	require.NotNil(t, tags)
	assert.Equal(t, "cf_"+strconv.FormatInt(newTags.ID, 10), tags.Target)
	list, ok := toIntSlice(tags.Default)
	require.True(t, ok)
	assert.Equal(t, optionIDs(newTags), list, "both options of the list default are mapped, in order")
	assert.NotContains(t, list, int64(3))
	assert.NotContains(t, list, int64(4))

	// The source form is as it was.
	assert.Equal(t, doing, reloadForm(t, f.ID).Settings.InitialStatusID)
}
