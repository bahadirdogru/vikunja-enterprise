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

package automation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/script"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

const maxOutputBytes = 16 * 1024

// stopOutput marks the step of a flow.stop_if that ended the run, so a resumed run knows it was over.
const stopOutput = "stop: "

// Skip reasons recorded on runs.
const (
	SkipMaxDepth        = "max_depth"
	SkipSelfTrigger     = "self_trigger"
	SkipRateLimit       = "rate_limit"
	SkipConditions      = "conditions_not_met"
	SkipTaskNotFound    = "task_not_found"
	SkipRuleDisabled    = "rule_disabled"
	SkipOwnerNotAdmin   = models.AutomationDisabledOwnerNotAdmin
	SkipOwnerUnavilable = models.AutomationDisabledOwnerUnavailable
	SkipTooManyActions  = "too_many_actions"
	SkipRuleChanged     = "rule_changed"
)

// now is the clock of the engine; tests replace it.
var now = time.Now

// dbTime formats a time for comparisons in conditions: xorm stores times as UTC text, and a time.Time handed to the
// driver would be compared as a differently formatted string.
func dbTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isPermanent reports whether an error will happen again when retried: rejected by the model (permissions,
// workflow, validation), caused by the rule or by the state of the task.
func isPermanent(err error) bool {
	var actionErr *ActionError
	var httpErr web.HTTPErrorProcessor
	switch {
	case errors.As(err, &actionErr), errors.Is(err, ErrTaskGone):
		return true
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return false
	case errors.As(err, &httpErr):
		return true
	}
	return false
}

// matchesTrigger reports whether the event is what the trigger of the rule waits for. It looks at the payload only.
//
//nolint:gocyclo
func matchesTrigger(rule *models.AutomationRule, p *Payload) bool {
	def := spec.TriggerByType(rule.Trigger.Type)
	if def == nil || def.Event != p.Name {
		return false
	}
	opts := rule.Trigger.Options
	switch rule.Trigger.Type {
	case spec.TriggerTaskUpdated:
		fields, _ := opts["fields"].([]any)
		if len(fields) == 0 {
			return true
		}
		for _, f := range fields {
			if name, ok := f.(string); ok && slices.Contains(p.Changed, name) {
				return true
			}
		}
		return false
	case spec.TriggerTaskCompleted, spec.TriggerTaskReopened:
		oldDone, hasOld := p.Old["done"].(bool)
		newDone, hasNew := p.New["done"].(bool)
		if !hasOld || !hasNew || oldDone == newDone {
			return false
		}
		return newDone == (rule.Trigger.Type == spec.TriggerTaskCompleted)
	case spec.TriggerTaskStatusChanged:
		if id, ok := spec.ToInt64(opts["from_status_id"]); ok {
			if old, has := spec.ToInt64(p.Old["status"]); !has || old != id {
				return false
			}
		}
		if id, ok := spec.ToInt64(opts["to_status_id"]); ok {
			if cur, has := spec.ToInt64(p.New["status"]); !has || cur != id {
				return false
			}
		}
		if category, ok := opts["to_category"].(string); ok {
			if cur, _ := p.New["status_category"].(string); cur != category {
				return false
			}
		}
		return true
	case spec.TriggerTaskCustomFieldChanged:
		if id, ok := spec.ToInt64(opts["field_id"]); ok {
			return p.FieldID == id
		}
		return true
	case spec.TriggerFormSubmitted:
		if id, ok := spec.ToInt64(opts["form_id"]); ok && id != 0 {
			cur, has := spec.ToInt64(p.New["form_id"])
			return has && cur == id
		}
		return true
	case spec.TriggerApprovalResolved:
		if outcome, ok := opts["outcome"].(string); ok && outcome != "" && outcome != spec.OutcomeAny {
			cur, _ := p.New["outcome"].(string)
			return cur == outcome
		}
		return true
	}
	return true
}

func loadRun(s *xorm.Session, eventID, ruleID int64) (*models.AutomationRun, error) {
	run := &models.AutomationRun{}
	has, err := s.Where("event_id = ? AND rule_id = ?", eventID, ruleID).Get(run)
	if err != nil || !has {
		return nil, err
	}
	return run, nil
}

// runRule executes one rule for one event, or resumes the run an earlier attempt left behind. It returns a non-nil
// error only for failures that are worth retrying.
func runRule(ctx context.Context, ev *models.AutomationEvent, payload *Payload, rule *models.AutomationRule) error {
	if !matchesTrigger(rule, payload) {
		return nil
	}

	s := db.NewAutocommitSession()
	defer s.Close()

	run, err := loadRun(s, ev.ID, rule.ID)
	if err != nil {
		return err
	}
	if run != nil && run.Status != models.AutomationRunRunning {
		log.Debugf("[automation] run %d of rule %d for event %d was handled already (%s)", run.ID, rule.ID, ev.ID, run.Status)
		return nil
	}
	fresh := run == nil
	if fresh {
		run = &models.AutomationRun{
			RuleID:       rule.ID,
			RuleRevision: rule.Revision,
			EventID:      ev.ID,
			ProjectID:    rule.ProjectID,
			TaskID:       ev.TaskID,
			Status:       models.AutomationRunRunning,
			Depth:        ev.Depth,
			Started:      now(),
		}
		// The unique index on (event_id, rule_id) is the idempotency guard: when the insert fails because
		// someone else created the run, the event was handled and there is nothing left to do.
		if _, err = s.Insert(run); err != nil {
			existing, getErr := loadRun(s, ev.ID, rule.ID)
			if getErr == nil && existing != nil {
				return nil
			}
			return err
		}
	}
	log.Debugf("[automation] run %d: rule %d, event %d (%s), resumed: %t", run.ID, rule.ID, ev.ID, ev.Name, !fresh)

	rc := &runContext{ev: ev, payload: payload, rule: rule, run: run}
	return rc.execute(ctx, s, fresh)
}

type runContext struct {
	ev      *models.AutomationEvent
	payload *Payload
	rule    *models.AutomationRule
	run     *models.AutomationRun
	owner   *user.User
}

// finish stores the final state of the run and updates the statistics of the rule.
func (rc *runContext) finish(s *xorm.Session, status, reason string, runErr error) error {
	rc.run.Status = status
	rc.run.SkipReason = reason
	rc.run.Error = ""
	rc.run.ErrorCode, rc.run.ErrorParams = "", nil
	if runErr != nil {
		rc.run.Error = truncate(runErr.Error(), 4000)
		rc.run.ErrorCode, rc.run.ErrorParams = errorMessage(runErr)
	}
	rc.run.Finished = now()
	if _, err := s.ID(rc.run.ID).Cols("status", "skip_reason", "error", "error_code", "error_params", "finished").Update(rc.run); err != nil {
		return err
	}
	countRun(status)

	rule := &models.AutomationRule{LastRunAt: rc.run.Finished, LastRunStatus: status}
	cols := []string{"last_run_at", "last_run_status"}
	switch status {
	case models.AutomationRunSuccess:
		rule.ConsecutiveFailures = 0
		cols = append(cols, "consecutive_failures")
	case models.AutomationRunFailed:
		if notBlamed(runErr) {
			break
		}
		if _, err := s.ID(rc.rule.ID).Incr("consecutive_failures").Cols(cols...).NoAutoTime().Update(rule); err != nil {
			return err
		}
		return rc.disableOnFailures(s)
	}
	_, err := s.ID(rc.rule.ID).Cols(cols...).NoAutoTime().Update(rule)
	return err
}

// notBlamed reports whether a failed run must not count towards disabling its rule: the retries ran out on a
// failure of the script machinery (no free script process, none could be started), or the failure is permanent
// but not the rule's doing (see ActionError.NoBlame).
func notBlamed(err error) bool {
	var actionErr *ActionError
	return errors.Is(err, script.ErrUnavailable) || (errors.As(err, &actionErr) && actionErr.NoBlame)
}

func (rc *runContext) disableOnFailures(s *xorm.Session) error {
	limit := int64(config.AutomationMaxConsecutiveFails.GetInt())
	if limit <= 0 {
		return nil
	}
	stored, err := models.GetAutomationRule(s, rc.rule.ID)
	if err != nil || !stored.Enabled || stored.ConsecutiveFailures < limit {
		return err
	}
	disabled, err := models.DisableAutomationRule(s, stored.ID, models.AutomationDisabledTooManyFailures)
	if disabled {
		log.Warningf("[automation] rule %d of project %d was disabled after %d failed runs in a row", stored.ID, stored.ProjectID, stored.ConsecutiveFailures)
	}
	return err
}

func (rc *runContext) abort(s *xorm.Session, reason string, disableReason string) error {
	if disableReason != "" {
		if _, err := models.DisableAutomationRule(s, rc.rule.ID, disableReason); err != nil {
			return err
		}
		log.Warningf("[automation] rule %d was disabled: %s", rc.rule.ID, disableReason)
	}
	return rc.finish(s, models.AutomationRunAborted, reason, nil)
}

// preChecks applies the limits that decide before anything runs. done is true when the run was finished.
func (rc *runContext) preChecks(s *xorm.Session) (done bool, err error) {
	if maxDepth := int64(config.AutomationMaxDepth.GetInt()); rc.ev.Depth > maxDepth {
		return true, rc.finish(s, models.AutomationRunSkipped, SkipMaxDepth, nil)
	}

	selfTriggered, err := rc.causedByOwnRun(s)
	if err != nil {
		return false, err
	}
	if selfTriggered {
		return true, rc.finish(s, models.AutomationRunSkipped, SkipSelfTrigger, nil)
	}

	// The time based triggers are limited by the cap per tick instead.
	timeBased := rc.ev.Name == spec.EventSchedule || rc.ev.Name == spec.EventOverdue || rc.ev.Name == spec.EventDueSoon
	if limit := int64(config.AutomationRateLimit.GetInt()); limit > 0 && !timeBased {
		recent, err := s.Where(builder.And(
			builder.Eq{"rule_id": rc.rule.ID},
			builder.Gt{"started": dbTime(now().Add(-time.Minute))},
			builder.Neq{"id": rc.run.ID},
			builder.Neq{"status": models.AutomationRunSkipped},
		)).Count(&models.AutomationRun{})
		if err != nil {
			return false, err
		}
		if recent >= limit {
			return true, rc.finish(s, models.AutomationRunSkipped, SkipRateLimit, nil)
		}
	}
	return false, nil
}

// causedByOwnRun follows the chain of runs that led to the event and reports whether one of them was a run of this
// rule. The task does not matter: a rule that creates tasks must not react to the tasks it created itself, or one
// event could fan out into a tree of runs.
func (rc *runContext) causedByOwnRun(s *xorm.Session) (bool, error) {
	originID := rc.ev.OriginRunID
	for i := 0; originID != 0 && i <= config.AutomationMaxDepth.GetInt()+2; i++ {
		origin := &models.AutomationRun{}
		has, err := s.ID(originID).Get(origin)
		if err != nil {
			return false, err
		}
		if !has {
			return false, nil
		}
		if origin.RuleID == rc.rule.ID {
			return true, nil
		}
		parent := &models.AutomationEvent{}
		has, err = s.ID(origin.EventID).Cols("id", "origin_run_id").Get(parent)
		if err != nil {
			return false, err
		}
		if !has {
			return false, nil
		}
		originID = parent.OriginRunID
	}
	return false, nil
}

// requireReadableSubject makes sure the owner of the rule may read the task the rule is about to see. The subject is
// loaded without a permission check, and an event can outlive the access of the owner to its task: the task may
// have been moved to a project the owner cannot see before the run starts, between two steps, or before a retry.
// For the rule such a task is gone.
func requireReadableSubject(s *xorm.Session, owner *user.User, sub *Subject) error {
	if sub == nil || sub.Task == nil {
		return nil
	}
	can, _, err := (&models.Task{ID: sub.Task.ID}).CanRead(s, owner)
	if err != nil {
		return err
	}
	if !can {
		return ErrTaskGone
	}
	return nil
}

// checkOwner loads the owner and makes sure they still administer the project.
func (rc *runContext) checkOwner(s *xorm.Session) (ok bool, err error) {
	owner, err := user.GetUserByID(s, rc.rule.OwnerID)
	if err != nil {
		if user.IsErrUserDoesNotExist(err) || user.IsErrUserStatusError(err) {
			return false, rc.abort(s, SkipOwnerUnavilable, models.AutomationDisabledOwnerUnavailable)
		}
		return false, err
	}
	admin, err := models.CanAdminProject(s, owner, rc.rule.ProjectID)
	if err != nil {
		if models.IsErrProjectDoesNotExist(err) {
			return false, rc.abort(s, SkipOwnerNotAdmin, models.AutomationDisabledOwnerNotAdmin)
		}
		return false, err
	}
	if !admin {
		return false, rc.abort(s, SkipOwnerNotAdmin, models.AutomationDisabledOwnerNotAdmin)
	}
	rc.owner = owner
	return true, nil
}

// checkConditions evaluates the conditions of the rule for a run that has not done anything yet. finished is set
// when the run was ended because of them (or because the task is gone).
func (rc *runContext) checkConditions(runCtx context.Context, s *xorm.Session) (finished bool, err error) {
	subject, err := LoadSubject(s, rc.payload.TaskID, rc.payload, now())
	if err == nil {
		err = requireReadableSubject(s, rc.owner, subject)
	}
	if err != nil {
		if errors.Is(err, ErrTaskGone) {
			return true, rc.finish(s, models.AutomationRunSkipped, SkipTaskNotFound, nil)
		}
		return false, err
	}
	// The environment lets script conditions read as the owner; they never get a host that can write.
	condEnv := &Env{Ctx: runCtx, S: s, Auth: rc.owner, Rule: rc.rule, Event: rc.payload, Now: now(), Sub: subject, RunID: rc.run.ID}
	subject.Script = condEnv.scriptCondition
	holds, err := Evaluate(rc.rule.Conditions, subject)
	if errors.Is(err, script.ErrUnavailable) {
		// The script machinery failed, not the condition: try again later.
		return false, err
	}
	if err != nil {
		return true, rc.finish(s, models.AutomationRunFailed, "", err)
	}
	if !holds {
		return true, rc.finish(s, models.AutomationRunSkipped, SkipConditions, nil)
	}
	return false, nil
}

func (rc *runContext) execute(ctx context.Context, s *xorm.Session, fresh bool) error {
	if fresh {
		if done, err := rc.preChecks(s); done || err != nil {
			return err
		}
	}
	if ok, err := rc.checkOwner(s); !ok || err != nil {
		return err
	}
	if maxActions := config.AutomationMaxActions.GetInt(); maxActions > 0 && len(rc.rule.Actions) > maxActions {
		return rc.finish(s, models.AutomationRunFailed, SkipTooManyActions, actionErr("too_many_actions", P{"count": len(rc.rule.Actions), "max": maxActions}, "the rule has %d actions, at most %d are allowed", len(rc.rule.Actions), maxActions))
	}

	existing := []*models.AutomationRunStep{}
	if err := s.Where("run_id = ?", rc.run.ID).Find(&existing); err != nil {
		return err
	}
	done := map[int64]*models.AutomationRunStep{}
	for _, st := range existing {
		if st.Status != models.AutomationStepSuccess {
			continue
		}
		// The rule may have been edited since the steps ran (for example to fix the step that failed). Resuming is
		// only safe when the recorded steps still are the actions at their positions.
		if st.Position >= int64(len(rc.rule.Actions)) || rc.rule.Actions[st.Position].Type != st.ActionType {
			return rc.finish(s, models.AutomationRunAborted, SkipRuleChanged, nil)
		}
		done[st.Position] = st
	}
	if rc.run.RuleRevision != rc.rule.Revision {
		rc.run.RuleRevision = rc.rule.Revision
		if _, err := s.ID(rc.run.ID).Cols("rule_revision").Update(rc.run); err != nil {
			return err
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, config.AutomationRunTimeout.GetDuration())
	defer cancel()

	// Conditions look at the state before the first action; a resumed run has changed the task already.
	if len(existing) == 0 {
		if finished, err := rc.checkConditions(runCtx, s); finished || err != nil {
			return err
		}
	}

	if rc.heldByWait(existing) {
		return nil
	}

	for position, act := range rc.rule.Actions {
		if st, ok := done[int64(position)]; ok {
			if strings.HasPrefix(st.Output, stopOutput) {
				return rc.finish(s, models.AutomationRunSuccess, "", nil)
			}
			continue
		}
		stop, suspend, err := rc.executeStep(runCtx, ctx, int64(position), act)
		if err != nil {
			if isPermanent(err) {
				log.Debugf("[automation] run %d failed at step %d: %v", rc.run.ID, position, err)
				return rc.finish(s, models.AutomationRunFailed, "", err)
			}
			if ctx.Err() != nil {
				// Shutting down: leave the run resumable.
				return ctx.Err()
			}
			log.Debugf("[automation] run %d: step %d failed temporarily: %v", rc.run.ID, position, err)
			code, params := errorMessage(err)
			if _, uerr := s.ID(rc.run.ID).Cols("error", "error_code", "error_params").Update(&models.AutomationRun{Error: truncate(err.Error(), 4000), ErrorCode: code, ErrorParams: params}); uerr != nil {
				log.Errorf("[automation] could not store the error of run %d: %v", rc.run.ID, uerr)
			}
			return err
		}
		if suspend {
			// The run stays running; the resume event written by the wait step continues it.
			return nil
		}
		if stop {
			break
		}
	}
	return rc.finish(s, models.AutomationRunSuccess, "", nil)
}

// heldByWait reports whether the run has to stay where it is because it waits. A run that waits is continued by its
// resume event, and only by it. Another delivery of the original event (a retry of a different rule's failure, say)
// must not continue past the wait, however late it comes: it would run next to the resume event. A resume event that
// is delivered again while a later wait of the run is pending must not run past that one either.
func (rc *runContext) heldByWait(steps []*models.AutomationRunStep) bool {
	until, waiting := pendingWait(steps)
	return waiting && (rc.ev.Name != spec.EventResume || until.After(now()))
}

// pendingWait reports whether the last successful step of the run is a wait, and until when.
func pendingWait(steps []*models.AutomationRunStep) (time.Time, bool) {
	var last *models.AutomationRunStep
	for _, st := range steps {
		if st.Status == models.AutomationStepSuccess && (last == nil || st.Position > last.Position) {
			last = st
		}
	}
	if last == nil || last.ActionType != spec.ActionWait {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, strings.TrimPrefix(last.Output, waitOutput))
	if err != nil {
		return time.Time{}, false
	}
	return until, true
}

// executeStep performs one action in its own transaction together with the step row, so a step recorded as
// successful has been applied exactly once.
func (rc *runContext) executeStep(runCtx, parent context.Context, position int64, act *spec.Action) (stop, suspend bool, err error) {
	impl := registry[act.Type]
	started := time.Now()
	if impl == nil {
		err = actionErr("unknown_action", P{"type": act.Type}, "unknown action %q", act.Type)
		rc.recordFailedStep(position, act.Type, err, started)
		return false, false, err
	}

	stepCtx, cancel := context.WithTimeout(runCtx, config.AutomationStepTimeout.GetDuration())
	defer cancel()

	s := db.NewSession()
	defer s.Close()
	db.SetSessionContext(stepCtx, s)
	// Write first: SQLite fails a transaction that reads and then tries to write after another writer committed in
	// between, which busy_timeout cannot fix. Taking the write lock before the first read makes concurrent steps wait
	// for each other instead. On other databases this is a cheap no-op update of a row only this worker touches.
	if _, err = s.ID(rc.run.ID).Cols("started").Update(&models.AutomationRun{Started: rc.run.Started}); err != nil {
		return false, false, err
	}
	// Another worker may hold the same run (the lock of its event expired, or the event was queued again by a manual
	// retry) and have committed this step while this one waited for the write above. Only now is the answer to
	// "did the step run" reliable.
	recorded := &models.AutomationRunStep{}
	has, err := s.Where("run_id = ? AND position = ? AND status = ?", rc.run.ID, position, models.AutomationStepSuccess).Get(recorded)
	if err != nil {
		return false, false, err
	}
	if has {
		// The other worker's outcome decides: a recorded wait must suspend this worker too, or it would run on
		// past the pause.
		return strings.HasPrefix(recorded.Output, stopOutput), recorded.ActionType == spec.ActionWait, s.Rollback()
	}
	markSession(s, origin{runID: rc.run.ID, depth: rc.run.Depth})
	defer unmarkSession(s)

	output, plan, err := rc.applyStep(stepCtx, s, impl, act)
	if err == nil {
		err = stepCtx.Err()
	}
	if err == nil {
		err = events.PendingHookError(s)
	}
	if err == nil {
		err = rc.storeStep(s, position, act.Type, models.AutomationStepSuccess, nil, output, plan.outputMessages(), started)
	}
	if err == nil {
		err = s.Commit()
	}
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		rc.recordFailedStep(position, act.Type, err, started)
		return false, false, err
	}
	if plan.CreatedTaskID != 0 {
		rc.run.TaskID = plan.CreatedTaskID
	}

	events.DispatchPending(parent, s)
	Wake()
	return plan.Stop, plan.Wait > 0, nil
}

func (rc *runContext) applyStep(ctx context.Context, s *xorm.Session, impl *action, act *spec.Action) (output string, plan *Plan, err error) {
	event := rc.payload
	if rc.run.TaskID != 0 && rc.run.TaskID != event.TaskID {
		// A create task action replaced the (missing) task of the run: the later actions work on the new task.
		withTask := *event
		withTask.TaskID = rc.run.TaskID
		event = &withTask
	}
	env := &Env{Ctx: ctx, S: s, Auth: rc.owner, Rule: rc.rule, Event: event, Now: now(), RunID: rc.run.ID}
	if err = env.Reload(); err != nil {
		return "", nil, err
	}
	if err = requireReadableSubject(s, rc.owner, env.Sub); err != nil {
		return "", nil, err
	}
	// Errors and output are checked against the secrets of the project when the step used any: a secret value
	// must not reach the run history.
	plan, err = impl.plan(env, params(act.Params))
	if err != nil {
		return "", nil, env.maskErr(err)
	}
	output = plan.Summary
	if plan.apply != nil {
		if output, err = plan.apply(); err != nil {
			return "", nil, env.maskErr(err)
		}
	}
	if plan.CreatedTaskID != 0 {
		// The new task is the task of the rest of the run.
		if _, err = s.ID(rc.run.ID).Cols("task_id").Update(&models.AutomationRun{TaskID: plan.CreatedTaskID}); err != nil {
			return "", nil, err
		}
	}
	if plan.rawOutput {
		// The output is data (an HTTP answer): the warnings come first, so they stay the leading lines.
		prefix := ""
		for _, w := range plan.Warnings {
			prefix += "warning: " + w + "\n"
		}
		output = prefix + output
	} else {
		for _, w := range plan.Warnings {
			output += "\nwarning: " + w
		}
	}
	if plan.Stop {
		output = stopOutput + output
	}
	// The sentences of the step lose the secrets too (the cut happens when they are stored).
	if plan.Lines != nil {
		plan.Lines = env.maskMessages(plan.Lines)
	}
	plan.WarnMsgs = env.maskMessages(plan.WarnMsgs)
	if plan.Msg != nil {
		plan.Msg = env.maskMessages([]*models.AutomationMessage{plan.Msg})[0]
	}
	return truncate(env.maskOutput(output), maxOutputBytes), plan, ctx.Err()
}

// storeStep inserts or updates the row of the step. Position is unique per run.
func (rc *runContext) storeStep(s *xorm.Session, position int64, typ, status string, stepErr error, output string, lines []*models.AutomationMessage, started time.Time) error {
	row := &models.AutomationRunStep{
		RunID:          rc.run.ID,
		Position:       position,
		ActionType:     typ,
		Status:         status,
		Output:         truncate(output, maxOutputBytes),
		OutputMessages: boundMessages(lines),
		DurationMs:     time.Since(started).Milliseconds(),
	}
	if stepErr != nil {
		row.Error = truncate(stepErr.Error(), 4000)
		row.ErrorCode, row.ErrorParams = errorMessage(stepErr)
	}
	existing := &models.AutomationRunStep{}
	has, err := s.Where("run_id = ? AND position = ?", rc.run.ID, position).Get(existing)
	if err != nil {
		return err
	}
	if has {
		_, err = s.ID(existing.ID).Cols("action_type", "status", "error", "error_code", "error_params", "output", "output_messages", "duration_ms").Update(row)
		return err
	}
	_, err = s.Insert(row)
	return err
}

func (rc *runContext) recordFailedStep(position int64, typ string, stepErr error, started time.Time) {
	s := db.NewAutocommitSession()
	defer s.Close()
	if err := rc.storeStep(s, position, typ, models.AutomationStepFailed, stepErr, "", nil, started); err != nil {
		log.Errorf("[automation] could not record the failed step %d of run %d: %v", position, rc.run.ID, err)
	}
}
