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
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// deleteAutomationForProject removes the rules of a project with their events, runs and steps.
func deleteAutomationForProject(s *xorm.Session, projectID int64) error {
	runIDs := builder.Select("id").From("automation_runs").Where(builder.Eq{"project_id": projectID})
	if _, err := s.Where(builder.In("run_id", runIDs)).Delete(&AutomationRunStep{}); err != nil {
		return err
	}
	if _, err := s.Where("project_id = ?", projectID).Delete(&AutomationRun{}); err != nil {
		return err
	}
	if _, err := s.Where("project_id = ?", projectID).Delete(&AutomationEvent{}); err != nil {
		return err
	}
	if _, err := s.Where("project_id = ?", projectID).Delete(&AutomationRule{}); err != nil {
		return err
	}
	if err := deleteAutomationSecretsForProject(s, projectID); err != nil {
		return err
	}
	invalidateAutomationRules(s, projectID)
	return nil
}

// disableAutomationRules disables the enabled rules of the projects for which refersTo reports a reference to
// something that is gone, and stores the reason on them.
func disableAutomationRules(s *xorm.Session, projectIDs []int64, reason string, refersTo func(spec.Refs) bool) error {
	if len(projectIDs) == 0 {
		return nil
	}
	rules := []*AutomationRule{}
	if err := s.Where(builder.And(builder.In("project_id", projectIDs), builder.Eq{"enabled": true})).Find(&rules); err != nil {
		return err
	}
	for _, rule := range rules {
		if !refersTo(spec.Collect(rule.Doc())) {
			continue
		}
		if _, err := s.ID(rule.ID).Cols("enabled", "disabled_reason", "revision").Update(&AutomationRule{
			Enabled:        false,
			DisabledReason: reason,
			Revision:       rule.Revision + 1,
		}); err != nil {
			return err
		}
		invalidateAutomationRules(s, rule.ProjectID)
	}
	return nil
}

// disableAutomationRulesForCustomFields is called before custom fields are deleted.
func disableAutomationRulesForCustomFields(s *xorm.Session, fieldIDs []int64) error {
	if len(fieldIDs) == 0 {
		return nil
	}
	projectIDs := []int64{}
	if err := s.Table("custom_fields").Where(builder.In("id", fieldIDs)).Distinct("project_id").Find(&projectIDs); err != nil {
		return err
	}
	return disableAutomationRules(s, projectIDs, AutomationDisabledCustomFieldDeleted, func(r spec.Refs) bool {
		for _, id := range r.CustomFields {
			if slices.Contains(fieldIDs, id) {
				return true
			}
		}
		return false
	})
}

// disableAutomationRulesForStatuses is called when statuses of the workflow of a project are removed or archived.
func disableAutomationRulesForStatuses(s *xorm.Session, projectID int64, statusIDs []int64) error {
	if len(statusIDs) == 0 {
		return nil
	}
	return disableAutomationRules(s, []int64{projectID}, AutomationDisabledStatusDeleted, func(r spec.Refs) bool {
		for _, id := range r.Statuses {
			if slices.Contains(statusIDs, id) {
				return true
			}
		}
		return false
	})
}

// duplicateAutomationRulesForProject copies the rules of one project into its duplicate. The copies are disabled:
// they run as the user who duplicated the project and must be looked at before they act. Ids of custom fields,
// options and statuses are replaced with the ones of the copies; a rule that refers to something without a copy
// gets its own reason.
func duplicateAutomationRulesForProject(s *xorm.Session, doerID, sourceProjectID, targetProjectID int64, fieldMap, optionMap, statusMap map[int64]int64) error {
	rules := []*AutomationRule{}
	if err := s.Where("project_id = ?", sourceProjectID).OrderBy("id ASC").Find(&rules); err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}

	optionFields := map[int64]bool{}
	fields := []*CustomField{}
	if err := s.Where("project_id = ?", sourceProjectID).Find(&fields); err != nil {
		return err
	}
	for _, f := range fields {
		if f.Type.hasOptions() {
			optionFields[f.ID] = true
		}
	}

	for _, rule := range rules {
		doc := spec.CloneDoc(rule.Doc())
		reason := AutomationDisabledDuplicated
		spec.Walk(doc, spec.Handler{
			ID: func(kind string, id int64, _ string) int64 {
				var m map[int64]int64
				switch kind {
				case spec.KindCustomField:
					m = fieldMap
				case spec.KindStatus:
					m = statusMap
				default:
					return id
				}
				if to, ok := m[id]; ok {
					return to
				}
				reason = AutomationDisabledUnmappedReference
				return id
			},
			IsOptionField: func(id int64) bool { return optionFields[id] },
			Option: func(_, id int64) int64 {
				if to, ok := optionMap[id]; ok {
					return to
				}
				reason = AutomationDisabledUnmappedReference
				return id
			},
		})

		// Secrets are never copied to the duplicate; a rule that uses them cannot work until they are set again.
		if reason == AutomationDisabledDuplicated && spec.UsesSecrets(doc) {
			reason = AutomationDisabledSecretsMissing
		}

		clone := &AutomationRule{
			ProjectID:      targetProjectID,
			Title:          rule.Title,
			Description:    rule.Description,
			Enabled:        false,
			Revision:       1,
			OwnerID:        doerID,
			Trigger:        doc.Trigger,
			Conditions:     doc.Conditions,
			Actions:        doc.Actions,
			DisabledReason: reason,
		}
		if _, err := s.Insert(clone); err != nil {
			return err
		}
	}
	return nil
}

// cleanupAutomation deletes finished events and runs older than the retention. Returns what was deleted.
func cleanupAutomation(s *xorm.Session, olderThan time.Time) (events, runs int64, err error) {
	oldRuns := builder.Select("id").From("automation_runs").Where(builder.And(
		builder.Lt{"finished": olderThan.UTC().Format(dbTimeFormat)},
		builder.Neq{"status": AutomationRunRunning},
	))
	if _, err = s.Where(builder.In("run_id", oldRuns)).Delete(&AutomationRunStep{}); err != nil {
		return
	}
	runs, err = s.Where(builder.And(
		builder.Lt{"finished": olderThan.UTC().Format(dbTimeFormat)},
		builder.Neq{"status": AutomationRunRunning},
	)).Delete(&AutomationRun{})
	if err != nil {
		return
	}
	// The event of a run that still waits stays: the run continues from its payload.
	waitingEvents := builder.Select("event_id").From("automation_runs").Where(builder.Eq{"status": AutomationRunRunning})
	events, err = s.Where(builder.And(
		builder.Lt{"created": olderThan.UTC().Format(dbTimeFormat)},
		builder.In("status", AutomationEventDone, AutomationEventDead),
		builder.NotIn("id", waitingEvents),
	)).Delete(&AutomationEvent{})
	if err != nil {
		return
	}
	// The due markers of tasks that are gone; the others have to stay or the rule would fire again.
	if _, err = s.Where(builder.NotIn("task_id", builder.Select("id").From("tasks"))).Delete(&AutomationDueMarker{}); err != nil {
		return
	}
	// The origins of tasks that are gone or whose run was deleted: without the run there is no chain to continue.
	_, err = s.Where(builder.Or(
		builder.NotIn("task_id", builder.Select("id").From("tasks")),
		builder.NotIn("run_id", builder.Select("id").From("automation_runs")),
	)).Delete(&AutomationTaskOrigin{})
	return
}

// CleanupAutomation is the retention job: it deletes finished events, runs and steps older than the cutoff.
func CleanupAutomation(s *xorm.Session, olderThan time.Time) (events, runs int64, err error) {
	return cleanupAutomation(s, olderThan)
}

// automationStuckRunGrace is how long past its longest possible wait a run may stay running before it is ended.
const automationStuckRunGrace = 24 * time.Hour

// ReapStuckAutomationRuns fails the runs that have been running for longer than they can wait: a run is only kept
// alive by an event, and when that is gone (dead, deleted, a rule that was removed) nothing ends it. A run may wait
// automation.waitmax per wait action of its rule; with a rule that is gone or without waits it is one waitmax.
// Returns the number of runs ended. Nothing is reaped when waits are not limited.
func ReapStuckAutomationRuns(s *xorm.Session, now time.Time) (int64, error) {
	maxWait := config.AutomationWaitMax.GetDuration()
	if maxWait <= 0 {
		return 0, nil
	}
	oldest := now.Add(-(maxWait + automationStuckRunGrace))
	candidates := []*AutomationRun{}
	if err := s.Where(builder.And(
		builder.Eq{"status": AutomationRunRunning},
		builder.Lt{"started": oldest.UTC().Format(dbTimeFormat)},
	)).Limit(1000).Find(&candidates); err != nil {
		return 0, err
	}
	var reaped int64
	for _, run := range candidates {
		waits := int64(0)
		if rule, err := GetAutomationRule(s, run.RuleID); err == nil {
			for _, a := range rule.Actions {
				if a != nil && a.Type == spec.ActionWait {
					waits++
				}
			}
		}
		if !run.Started.Before(now.Add(-(time.Duration(max(waits, 1))*maxWait + automationStuckRunGrace))) {
			continue
		}
		n, err := s.Where(builder.Eq{"id": run.ID, "status": AutomationRunRunning}).Cols("status", "error", "finished").NoAutoTime().Update(&AutomationRun{
			Status:   AutomationRunFailed,
			Error:    "the run did not finish in time and was ended: it was waiting longer than automation.waitmax allows",
			Finished: now,
		})
		if err != nil {
			return reaped, err
		}
		reaped += n
		if n == 1 {
			// The statistics of the rule count it like any other failed run (the rule is not disabled by it).
			if _, err = s.ID(run.RuleID).Incr("consecutive_failures").Cols("last_run_at", "last_run_status").NoAutoTime().
				Update(&AutomationRule{LastRunAt: now, LastRunStatus: AutomationRunFailed}); err != nil {
				return reaped, err
			}
		}
	}
	return reaped, nil
}
