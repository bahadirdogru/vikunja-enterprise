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
	"encoding/json"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// AutomationRun is one execution of a rule for an event. There is at most one per (event, rule).
type AutomationRun struct {
	ID           int64             `xorm:"bigint autoincr not null unique pk" json:"id" param:"run" readOnly:"true" doc:"The unique, numeric id of this run."`
	RuleID       int64             `xorm:"bigint not null INDEX unique(automation_runs_event_rule)" json:"rule_id" param:"rule" readOnly:"true" doc:"The rule that ran. Taken from the URL."`
	RuleRevision int64             `xorm:"bigint not null" json:"rule_revision" readOnly:"true" doc:"The revision of the rule the run executed."`
	EventID      int64             `xorm:"bigint not null unique(automation_runs_event_rule)" json:"event_id" readOnly:"true" doc:"The outbox event that caused the run."`
	ProjectID    int64             `xorm:"bigint not null INDEX" json:"project_id" param:"project" readOnly:"true" doc:"The project of the rule. Taken from the URL."`
	TaskID       int64             `xorm:"bigint null" json:"task_id" readOnly:"true" doc:"The task the event was about, 0 when none."`
	Status       string            `xorm:"varchar(20) not null" json:"status" readOnly:"true" enum:"running,success,failed,skipped,aborted" doc:"running while the run is executing or waits for a retry, then success, failed, skipped (a limit or the conditions stopped it) or aborted (the owner may no longer run the rule)."`
	SkipReason   string            `xorm:"varchar(100) null" json:"skip_reason" readOnly:"true" doc:"Why the run was skipped or aborted, e.g. max_depth, rate_limit, self_trigger, conditions_not_met, flow_stopped, owner_not_admin."`
	Error        string            `xorm:"text null" json:"error" readOnly:"true" doc:"Why the run failed, in English. Show error_code translated when the client knows it and this text when it does not."`
	ErrorCode    string            `xorm:"varchar(100) null" json:"error_code,omitempty" readOnly:"true" doc:"A stable code for why the run failed, e.g. title_empty, script_thrown or a numeric API error code like 4002. Empty for a run that stored its error before codes existed; use error then."`
	ErrorParams  map[string]string `xorm:"json null" json:"error_params,omitempty" readOnly:"true" doc:"The values the sentence of error_code mentions, by name."`
	Depth        int64             `xorm:"bigint not null default 0" json:"depth" readOnly:"true" doc:"How many rules triggered each other before this run; 0 for a change made by a person."`
	Started      time.Time         `xorm:"DATETIME not null" json:"started" readOnly:"true" doc:"When the run started."`
	Finished     time.Time         `xorm:"DATETIME null" json:"finished" readOnly:"true" doc:"When the run finished; zero while running."`

	EventName string               `xorm:"-" json:"event_name,omitempty" readOnly:"true" doc:"The internal event that caused the run."`
	Steps     []*AutomationRunStep `xorm:"-" json:"steps,omitempty" readOnly:"true" doc:"The executed actions. Only included when reading a single run."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name of automation runs.
func (*AutomationRun) TableName() string {
	return "automation_runs"
}

func getAutomationRunOfRule(s *xorm.Session, runID, ruleID, projectID int64) (*AutomationRun, error) {
	run := &AutomationRun{}
	has, err := s.Where("id = ? AND rule_id = ? AND project_id = ?", runID, ruleID, projectID).Get(run)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrAutomationRunDoesNotExist{RunID: runID}
	}
	return run, nil
}

func addAutomationEventNames(s *xorm.Session, runs []*AutomationRun) error {
	ids := make([]int64, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.EventID)
	}
	if len(ids) == 0 {
		return nil
	}
	events := []*AutomationEvent{}
	if err := s.Where(builder.In("id", ids)).Cols("id", "name").Find(&events); err != nil {
		return err
	}
	names := make(map[int64]string, len(events))
	for _, e := range events {
		names[e.ID] = e.Name
	}
	for _, run := range runs {
		run.EventName = names[run.EventID]
	}
	return nil
}

// ReadAll returns the runs of a rule, newest first. Needs admin access to the project.
// @Summary List the runs of an automation rule
func (r *AutomationRun) ReadAll(s *xorm.Session, a web.Auth, _ string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	// DoReadAll skips the Can* checks.
	can, _, err := canAdministerAutomation(s, a, r.ProjectID)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}
	if _, err = getAutomationRuleInProject(s, r.RuleID, r.ProjectID); err != nil {
		return nil, 0, 0, err
	}

	cond := builder.Eq{"rule_id": r.RuleID}
	total, err := s.Where(cond).Count(&AutomationRun{})
	if err != nil {
		return nil, 0, 0, err
	}
	runs := []*AutomationRun{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where(cond).OrderBy("id DESC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&runs); err != nil {
		return nil, 0, 0, err
	}
	if err = addAutomationEventNames(s, runs); err != nil {
		return nil, 0, 0, err
	}
	return runs, len(runs), total, nil
}

// ReadOne adds the steps; CanRead already loaded the run.
// @Summary Get an automation run
func (r *AutomationRun) ReadOne(s *xorm.Session, _ web.Auth) error {
	r.Steps = []*AutomationRunStep{}
	if err := s.Where("run_id = ?", r.ID).OrderBy("position ASC").Find(&r.Steps); err != nil {
		return err
	}
	return addAutomationEventNames(s, []*AutomationRun{r})
}

// AutomationRunRetry puts a failed run back in the queue. The steps that succeeded are not executed again, and no other
// run of its event is touched.
type AutomationRunRetry struct {
	ProjectID int64 `json:"-" param:"project"`
	RuleID    int64 `json:"-" param:"rule"`
	RunID     int64 `json:"-" param:"run"`

	Run *AutomationRun `xorm:"-" json:"run" readOnly:"true" doc:"The run, now waiting to be executed again."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// Create queues an event for this run alone, so a worker picks it up again.
// @Summary Retry a failed automation run
func (rr *AutomationRunRetry) Create(s *xorm.Session, _ web.Auth) error {
	run, err := getAutomationRunOfRule(s, rr.RunID, rr.RuleID, rr.ProjectID)
	if err != nil {
		return err
	}
	if run.Status != AutomationRunFailed {
		return ErrAutomationRunNotRetryable{RunID: run.ID, Status: run.Status}
	}

	// The run is continued by an event of its own, the one a wait writes: it names the run and its rule, so only this
	// run is executed again. Putting the original event back in the queue would run every rule of the event once
	// more, including the ones that were created, or enabled, after it happened. A run whose last successful step is a
	// wait is held back for the end of the wait; the original event is held back by the wait guard anyway.
	resumeAt, _, err := pendingAutomationWait(s, run.ID)
	if err != nil {
		return err
	}
	// The retention job deletes events before it deletes the runs that still reference them; without the event the
	// payload of the run is gone and its actions would render the {{event.*}} placeholders empty.
	hasEvent, err := s.ID(run.EventID).Exist(&AutomationEvent{})
	if err != nil {
		return err
	}
	if !hasEvent {
		return ErrAutomationRunEventGone{RunID: run.ID}
	}

	// Back to running: the worker resumes a running run at its first step that did not succeed. The transition is
	// conditional, so two requests at once queue one event: the one that finds the run running already is refused.
	affected, err := s.Where("id = ? AND status = ?", run.ID, AutomationRunFailed).
		Cols("status", "error", "error_code", "error_params", "skip_reason", "finished").
		Update(&AutomationRun{Status: AutomationRunRunning})
	if err != nil {
		return err
	}
	if affected == 0 {
		current := &AutomationRun{}
		if _, err = s.ID(run.ID).Cols("status").Get(current); err != nil {
			return err
		}
		return ErrAutomationRunNotRetryable{RunID: run.ID, Status: current.Status}
	}
	run.Status = AutomationRunRunning
	run.Error = ""
	run.ErrorCode = ""
	run.ErrorParams = nil
	run.SkipReason = ""
	run.Finished = time.Time{}
	if err = insertAutomationResumeEvent(s, run, resumeAt); err != nil {
		return err
	}
	rr.Run = run
	return nil
}

// AutomationWaitOutput starts the output of a wait step; the rest is the time the run continues at (RFC 3339).
const AutomationWaitOutput = "wait: "

// pendingAutomationWait reports whether the last successful step of the run is a wait, and until when.
func pendingAutomationWait(s *xorm.Session, runID int64) (until time.Time, waiting bool, err error) {
	steps := []*AutomationRunStep{}
	if err = s.Where("run_id = ? AND status = ?", runID, AutomationStepSuccess).OrderBy("position DESC").Limit(1).Find(&steps); err != nil {
		return time.Time{}, false, err
	}
	if len(steps) == 0 || steps[0].ActionType != spec.ActionWait {
		return time.Time{}, false, nil
	}
	until, err = time.Parse(time.RFC3339, strings.TrimPrefix(steps[0].Output, AutomationWaitOutput))
	if err != nil {
		return time.Time{}, false, nil //nolint:nilerr // an output that is no time is no pending wait
	}
	return until, true, nil
}

// insertAutomationResumeEvent queues the continuation of a run after its wait, due at the end of the wait or now.
func insertAutomationResumeEvent(s *xorm.Session, run *AutomationRun, until time.Time) error {
	payload := map[string]any{}
	original := &AutomationEvent{}
	has, err := s.ID(run.EventID).Get(original)
	if err != nil {
		return err
	}
	if has {
		_ = json.Unmarshal([]byte(original.Payload), &payload)
	}
	taskID := run.TaskID
	if has {
		taskID = original.TaskID
	} else {
		payload["name"] = spec.EventResume
		payload["task_id"] = taskID
		payload["project_id"] = run.ProjectID
	}
	payload["rule_id"] = run.RuleID
	payload["run_id"] = run.ID
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	runAfter := time.Now()
	if until.After(runAfter) {
		runAfter = until
	}
	_, err = s.Insert(&AutomationEvent{
		ProjectID: run.ProjectID,
		TaskID:    taskID,
		Name:      spec.EventResume,
		Payload:   string(raw),
		Status:    AutomationEventPending,
		RunAfter:  runAfter,
		RuleID:    run.RuleID,
	})
	return err
}
