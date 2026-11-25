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
	"slices"
	"strconv"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// deleteFormsForProject removes the forms of a project that is purged with their submissions. The tasks go with
// the project.
func deleteFormsForProject(s *xorm.Session, projectID int64) error {
	ids := builder.Select("id").From("forms").Where(builder.Eq{"project_id": projectID})
	if _, err := s.Where(builder.In("form_id", ids)).Delete(&FormSubmission{}); err != nil {
		return err
	}
	forms := []*Form{}
	if err := s.Where("project_id = ? AND brand_logo_file_id IS NOT NULL AND brand_logo_file_id > 0", projectID).Cols("id", "brand_logo_file_id").Find(&forms); err != nil {
		return err
	}
	for _, form := range forms {
		if err := deleteFormLogoFile(s, form.BrandLogoFileID); err != nil {
			return err
		}
	}
	_, err := s.Where("project_id = ?", projectID).Delete(&Form{})
	return err
}

// disableForm stores why a form was switched off.
func disableForm(s *xorm.Session, f *Form, reason string) error {
	_, err := s.ID(f.ID).Cols("enabled", "disabled_reason").Update(&Form{Enabled: false, DisabledReason: reason})
	return err
}

// disableFormsWhere disables the enabled forms of the given projects (all projects when nil) for which refersTo
// reports a reference to something that is gone.
func disableFormsWhere(s *xorm.Session, projectIDs []int64, reason string, refersTo func(*Form) bool) error {
	cond := builder.Eq{"enabled": true}
	var c builder.Cond = cond
	if projectIDs != nil {
		if len(projectIDs) == 0 {
			return nil
		}
		c = builder.And(cond, builder.In("project_id", projectIDs))
	}
	forms := []*Form{}
	if err := s.Where(c).Find(&forms); err != nil {
		return err
	}
	for _, f := range forms {
		normalizeForm(f)
		if refersTo(f) {
			if err := disableForm(s, f, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// disableFormsForCustomFields is called before custom fields are deleted.
func disableFormsForCustomFields(s *xorm.Session, fieldIDs []int64) error {
	if len(fieldIDs) == 0 {
		return nil
	}
	projectIDs := []int64{}
	if err := s.Table("custom_fields").Where(builder.In("id", fieldIDs)).Distinct("project_id").Find(&projectIDs); err != nil {
		return err
	}
	targets := make([]string, 0, len(fieldIDs))
	for _, id := range fieldIDs {
		targets = append(targets, "cf_"+strconv.FormatInt(id, 10))
	}
	return disableFormsWhere(s, projectIDs, FormDisabledCustomFieldDeleted, func(f *Form) bool {
		return slices.ContainsFunc(f.Fields, func(field *FormField) bool { return slices.Contains(targets, field.Target) })
	})
}

// disableFormsForStatuses is called when statuses of the workflow of a project are removed or archived.
func disableFormsForStatuses(s *xorm.Session, projectID int64, statusIDs []int64) error {
	if len(statusIDs) == 0 {
		return nil
	}
	return disableFormsWhere(s, []int64{projectID}, FormDisabledStatusDeleted, func(f *Form) bool {
		return slices.Contains(statusIDs, f.Settings.InitialStatusID)
	})
}

// disableFormsForLabel is called before a label is deleted: labels are not bound to a project, so every enabled
// form is looked at.
func disableFormsForLabel(s *xorm.Session, labelID int64) error {
	return disableFormsWhere(s, nil, FormDisabledLabelDeleted, func(f *Form) bool {
		for _, field := range f.Fields {
			if field.Target != FormTargetLabels {
				continue
			}
			if slices.Contains(field.Options, labelID) {
				return true
			}
			if ids, ok := toIntSlice(field.Default); ok && slices.Contains(ids, labelID) {
				return true
			}
		}
		return false
	})
}

// disableFormsOfOwner is part of deleting a user: the tasks of their forms would have no author to be created as.
func disableFormsOfOwner(s *xorm.Session, userID int64) error {
	forms := []*Form{}
	if err := s.Where(builder.And(builder.Eq{"owner_id": userID}, builder.Eq{"enabled": true})).Find(&forms); err != nil {
		return err
	}
	for _, f := range forms {
		if err := disableForm(s, f, FormDisabledOwnerUnavailable); err != nil {
			return err
		}
	}
	return nil
}

// duplicateFormsForProject copies the forms of one project into its duplicate. The copies are disabled and get a new
// token: they run as the user who duplicated the project and must be looked at before they take submissions.
// Ids of custom fields, options and statuses are replaced with the ones of the copies; a form that refers to
// something without a copy gets its own reason.
func duplicateFormsForProject(s *xorm.Session, doerID, sourceProjectID, targetProjectID int64, fieldMap, optionMap, statusMap map[int64]int64) error {
	forms := []*Form{}
	if err := s.Where("project_id = ?", sourceProjectID).OrderBy("id ASC").Find(&forms); err != nil {
		return err
	}
	if len(forms) == 0 {
		return nil
	}
	cfs := []*CustomField{}
	if err := s.Where("project_id = ?", sourceProjectID).Find(&cfs); err != nil {
		return err
	}
	types := map[int64]CustomFieldType{}
	for _, cf := range cfs {
		types[cf.ID] = cf.Type
	}

	for _, f := range forms {
		normalizeForm(f)
		reason := FormDisabledDuplicated
		unmapped := func() { reason = FormDisabledUnmappedReference }

		fields := make([]*FormField, 0, len(f.Fields))
		for _, field := range f.Fields {
			c := *field
			if id, ok := customFieldIDOfTarget(field.Target); ok {
				to, mapped := fieldMap[id]
				if !mapped {
					unmapped()
				} else {
					c.Target = "cf_" + strconv.FormatInt(to, 10)
					if types[id].hasOptions() && !isEmptyFormDefault(c.Default) {
						c.Default = remapOptionDefault(c.Default, optionMap, unmapped)
					}
				}
			}
			fields = append(fields, &c)
		}
		settings := *f.Settings
		if settings.InitialStatusID != 0 {
			if to, ok := statusMap[settings.InitialStatusID]; ok {
				settings.InitialStatusID = to
			} else {
				unmapped()
			}
		}
		token, err := newFormToken()
		if err != nil {
			return err
		}
		clone := &Form{
			ProjectID:      targetProjectID,
			Title:          f.Title,
			Description:    f.Description,
			Token:          token,
			Access:         f.Access,
			Enabled:        false,
			OwnerID:        doerID,
			Fields:         fields,
			Settings:       &settings,
			DisabledReason: reason,
			MaxSubmissions: f.MaxSubmissions,
			// The logo file is not copied; the copy has to get its own upload.
			BrandName:   f.BrandName,
			AccentColor: f.AccentColor,
			HideFooter:  f.HideFooter,
		}
		if _, err = s.Insert(clone); err != nil {
			return err
		}
	}
	return nil
}

func remapOptionDefault(def any, optionMap map[int64]int64, unmapped func()) any {
	one := func(id int64) int64 {
		if to, ok := optionMap[id]; ok {
			return to
		}
		unmapped()
		return id
	}
	if ids, ok := toIntSlice(def); ok {
		out := make([]any, 0, len(ids))
		for _, id := range ids {
			out = append(out, one(id))
		}
		return out
	}
	if id, ok := toInt(def); ok {
		return one(id)
	}
	return def
}

// removeUserFromFormSubmissions is part of deleting a user: the submissions they made stay (the tasks they created
// belong to the project), but nothing that identifies the account stays on them: the id, the name and the e-mail
// address. Plain SQL on purpose, a nil submitter id has to become NULL on every database.
func removeUserFromFormSubmissions(s *xorm.Session, userID int64) error {
	_, err := s.Exec("UPDATE form_submissions SET submitter_id = NULL, submitter_name = '', submitter_email = '' WHERE submitter_id = ?", userID)
	return err
}
