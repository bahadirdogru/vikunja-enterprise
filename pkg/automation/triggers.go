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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/outbound"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// recordTaskOrigin remembers that the run whose actions the session executes created the task, see
// models.AutomationTaskOrigin. Outside a run (a dry run, a test) there is nothing to remember.
func recordTaskOrigin(s *xorm.Session, taskID int64) error {
	o, has := sessionOrigins.Load(s)
	if !has || taskID == 0 {
		return nil
	}
	from := o.(origin)
	_, err := s.Insert(&models.AutomationTaskOrigin{TaskID: taskID, RunID: from.runID, Depth: from.depth + 1})
	return err
}

// taskOriginOf returns the run that created the task, nil when no run did.
func taskOriginOf(s *xorm.Session, taskID int64) (*models.AutomationTaskOrigin, error) {
	if taskID == 0 {
		return nil, nil
	}
	o := &models.AutomationTaskOrigin{}
	has, err := s.Where("task_id = ?", taskID).Get(o)
	if err != nil || !has {
		return nil, err
	}
	return o, nil
}

// insertTargetedEvent writes an outbox row for one rule. The caller commits. from is the run that created the task,
// when one did: the event then continues the chain of that run, so the loop guards see it.
func insertTargetedEvent(s *xorm.Session, rule *models.AutomationRule, name string, taskID int64, payload *Payload, from *models.AutomationTaskOrigin) (*models.AutomationEvent, error) {
	payload.Name = name
	payload.RuleID = rule.ID
	payload.ProjectID = rule.ProjectID
	payload.TaskID = taskID
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	row := &models.AutomationEvent{
		ProjectID: rule.ProjectID,
		TaskID:    taskID,
		Name:      name,
		Payload:   string(raw),
		Status:    models.AutomationEventPending,
		RunAfter:  now(),
		RuleID:    rule.ID,
	}
	if from != nil {
		row.OriginRunID = from.RunID
		row.Depth = from.Depth
	}
	if _, err = s.Insert(row); err != nil {
		return nil, err
	}
	return row, nil
}

// RunManually queues a run of a rule with the manual trigger for a task. The person needs write access to the task;
// the rule still runs as its owner. The run goes through the outbox like every other, so it shows in the run history.
func RunManually(a web.Auth, projectID, ruleID, taskID int64) (*models.AutomationEvent, error) {
	doer, err := manualDoer(a)
	if err != nil {
		return nil, err
	}
	if !config.AutomationEnabled.GetBool() {
		return nil, models.ErrAutomationRuleNotRunnable{RuleID: ruleID, Reason: "automation is turned off on this server"}
	}

	s := db.NewSession()
	defer s.Close()

	// Access to the project comes before anything is said about the rule or the task: whoever cannot write in the
	// project gets the same answer whether the project, the rule or the task exist, are enabled or have a manual
	// trigger.
	canWrite, err := (&models.Project{ID: projectID}).CanWrite(s, a)
	if err != nil && !models.IsErrProjectDoesNotExist(err) {
		return nil, err
	}
	if err != nil || !canWrite {
		return nil, models.ErrGenericForbidden{}
	}

	rule, err := models.GetAutomationRule(s, ruleID)
	if err != nil {
		return nil, err
	}
	if rule.ProjectID != projectID {
		return nil, models.ErrAutomationRuleDoesNotExist{RuleID: ruleID}
	}
	if rule.Trigger.Type != spec.TriggerManual {
		return nil, models.ErrAutomationRuleNotRunnable{RuleID: ruleID, Reason: "the rule has no manual trigger"}
	}
	if !rule.Enabled {
		return nil, models.ErrAutomationRuleNotRunnable{RuleID: ruleID, Reason: "the rule is disabled"}
	}

	task, err := models.GetTaskByIDSimple(s, taskID)
	if err != nil {
		return nil, err
	}
	if task.ProjectID != projectID {
		return nil, models.ErrTaskDoesNotExist{ID: taskID}
	}
	can, err := (&models.Task{ID: task.ID}).CanWrite(s, a)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}

	row, err := insertTargetedEvent(s, rule, spec.EventManual, task.ID, &Payload{Doer: personOf(doer), Task: taskFields(&task)}, nil)
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	Wake()
	return row, nil
}

func manualDoer(a web.Auth) (*user.User, error) {
	u, is := a.(*user.User)
	if !is || u.ID <= 0 {
		return nil, models.ErrGenericForbidden{}
	}
	s := db.NewReadSession()
	defer s.Close()
	full, err := user.GetUserByID(s, u.ID)
	if err != nil {
		return nil, err
	}
	return full, nil
}

// WebhookCall is one call of the public endpoint of an incoming webhook.
type WebhookCall struct {
	Token string
	// Body is the raw request body, exactly as received: the signature is computed over it.
	Body []byte
	// Signature is the X-Vikunja-Signature header.
	Signature string
}

const webhookRateWindow = time.Minute

// ReceiveWebhook checks a call of an incoming webhook and queues the run. Anything that is not a webhook of an
// enabled rule is "not found", so tokens cannot be told from absent ones.
func ReceiveWebhook(call *WebhookCall) (*models.AutomationEvent, error) {
	if !config.AutomationEnabled.GetBool() {
		return nil, models.ErrAutomationHookNotFound{}
	}
	s := db.NewSession()
	defer s.Close()

	rule, err := models.GetAutomationRuleByWebhookToken(s, call.Token)
	if err != nil {
		return nil, err
	}
	if rule == nil || !rule.Enabled || rule.Trigger.Type != spec.TriggerWebhookIncoming {
		return nil, models.ErrAutomationHookNotFound{}
	}

	// The signature first: a call that is not from the sender costs a hash and nothing else, and it neither is
	// counted against the sender's budget nor learns whether the budget is used up.
	if name, _ := rule.Trigger.Options["secret"].(string); name != "" {
		if err := checkWebhookSignature(s, rule, name, call); err != nil {
			return nil, err
		}
	}

	if limit := int64(config.AutomationWebhookRateLimit.GetInt()); limit > 0 {
		recent, err := s.Where(builder.And(
			builder.Eq{"rule_id": rule.ID, "name": spec.EventWebhook},
			builder.Gt{"created": dbTime(now().Add(-webhookRateWindow))},
		)).Count(&models.AutomationEvent{})
		if err != nil {
			return nil, err
		}
		if recent >= limit {
			return nil, models.ErrAutomationHookRateLimited{RetryAfterSeconds: int64(webhookRateWindow / time.Second)}
		}
	}

	hook := map[string]any{}
	if len(strings.TrimSpace(string(call.Body))) > 0 {
		if err := json.Unmarshal(call.Body, &hook); err != nil || hook == nil {
			return nil, models.ErrAutomationHookPayloadInvalid{}
		}
	}

	row, err := insertTargetedEvent(s, rule, spec.EventWebhook, 0, &Payload{Hook: hook}, nil)
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	Wake()
	return row, nil
}

func checkWebhookSignature(s *xorm.Session, rule *models.AutomationRule, secretName string, call *WebhookCall) error {
	secrets, err := models.LoadAutomationSecrets(s, rule.ProjectID)
	if err != nil {
		return err
	}
	sec := secrets[secretName]
	if sec == nil || sec.Err != nil {
		// The rule cannot be verified; refuse instead of accepting unsigned calls.
		return models.ErrAutomationHookSignatureInvalid{}
	}
	got, ok := strings.CutPrefix(strings.TrimSpace(call.Signature), "sha256=")
	if !ok {
		return models.ErrAutomationHookSignatureInvalid{}
	}
	gotBytes, err := hex.DecodeString(got)
	if err != nil {
		return models.ErrAutomationHookSignatureInvalid{}
	}
	mac := hmac.New(sha256.New, []byte(sec.Value))
	mac.Write(call.Body)
	if !hmac.Equal(gotBytes, mac.Sum(nil)) {
		return models.ErrAutomationHookSignatureInvalid{}
	}
	return nil
}

// SignatureHeader is the header incoming webhooks are signed in; the same one http.request sends.
const SignatureHeader = outbound.SignatureHeader

// ---- the ticker: schedule, overdue and due soon ----

// Tick fires the schedule, overdue and due soon triggers that are due at the current time. Every replica runs it;
// the claims in the database make a firing happen once. It returns the number of events it queued.
func Tick(ctx context.Context) (queued int, err error) {
	if !config.AutomationEnabled.GetBool() {
		return 0, nil
	}
	s := db.NewReadSession()
	candidates := []*models.AutomationRule{}
	err = s.Where("enabled = ?", true).Cols("id", "trigger").Find(&candidates)
	s.Close()
	if err != nil {
		return 0, err
	}
	for _, c := range candidates {
		if ctx.Err() != nil {
			return queued, ctx.Err()
		}
		switch c.Trigger.Type {
		case spec.TriggerSchedule, spec.TriggerTaskOverdue, spec.TriggerTaskDueSoon:
		default:
			continue
		}
		n, err := tickRule(ctx, c.ID)
		queued += n
		if err != nil {
			log.Errorf("[automation] ticker: rule %d: %v", c.ID, err)
		}
	}
	if queued > 0 {
		Wake()
	}
	return queued, nil
}

func tickRule(_ context.Context, ruleID int64) (int, error) {
	s := db.NewReadSession()
	rule, err := models.GetAutomationRule(s, ruleID)
	s.Close()
	if err != nil {
		if models.IsErrAutomationRuleDoesNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !rule.Enabled {
		return 0, nil
	}
	switch rule.Trigger.Type {
	case spec.TriggerSchedule:
		return tickSchedule(rule)
	case spec.TriggerTaskOverdue, spec.TriggerTaskDueSoon:
		return tickDue(rule)
	}
	return 0, nil
}

// ownerMayFire makes sure the owner of a rule that is due still exists and administers the project, like the run
// does. A rule whose owner lost access is disabled instead of firing; the check only happens when something is due,
// so an idle tick stays read only.
func ownerMayFire(rule *models.AutomationRule) (bool, error) {
	s := db.NewSession()
	defer s.Close()
	reason := ""
	owner, err := user.GetUserByID(s, rule.OwnerID)
	switch {
	case err != nil && (user.IsErrUserDoesNotExist(err) || user.IsErrUserStatusError(err)):
		reason = models.AutomationDisabledOwnerUnavailable
	case err != nil:
		_ = s.Rollback()
		return false, err
	default:
		admin, err := models.CanAdminProject(s, owner, rule.ProjectID)
		if err != nil && !models.IsErrProjectDoesNotExist(err) {
			_ = s.Rollback()
			return false, err
		}
		if err != nil || !admin {
			reason = models.AutomationDisabledOwnerNotAdmin
		}
	}
	if reason == "" {
		return true, nil
	}
	if _, err = models.DisableAutomationRule(s, rule.ID, reason); err != nil {
		_ = s.Rollback()
		return false, err
	}
	log.Warningf("[automation] rule %d of project %d was disabled by the ticker: %s", rule.ID, rule.ProjectID, reason)
	return false, s.Commit()
}

// maxOccurrenceSteps bounds how many occurrences latestOccurrence walks through in one go.
const maxOccurrenceSteps = 10000

// latestOccurrence returns the last cron occurrence in (base, at], zero when there is none. A server that was down
// for a while fires once for everything it missed, not once per missed occurrence, however long it was away.
func latestOccurrence(sched interface{ Next(time.Time) time.Time }, base, at time.Time) time.Time {
	last, capped := lastOccurrenceWithin(sched, base, at, maxOccurrenceSteps)
	if !capped {
		return last
	}
	// A dense schedule and a long gap: walking from the start would take as long as the gap is. The latest
	// occurrence is near the end, so look at windows that end at "at" and grow until one holds an occurrence.
	for window := time.Minute; ; window *= 8 {
		start := at.Add(-window)
		if !start.After(base) {
			// The window reaches back to the start: nothing is left to skip. What the first walk found is a valid
			// occurrence; the next tick continues from it.
			return last
		}
		if found, _ := lastOccurrenceWithin(sched, start, at, maxOccurrenceSteps); !found.IsZero() {
			return found
		}
	}
}

// lastOccurrenceWithin walks the occurrences after base up to at and returns the last one. capped is set when the
// walk stopped at maxSteps while occurrences were left.
func lastOccurrenceWithin(sched interface{ Next(time.Time) time.Time }, base, at time.Time, maxSteps int) (last time.Time, capped bool) {
	next := sched.Next(base)
	for i := 0; !next.IsZero() && !next.After(at); i++ {
		if i == maxSteps {
			return last, true
		}
		last = next
		next = sched.Next(next)
	}
	return last, false
}

func tickSchedule(rule *models.AutomationRule) (int, error) {
	expr, _ := rule.Trigger.Options["cron"].(string)
	sched, err := spec.ParseCron(expr)
	if err != nil {
		return 0, fmt.Errorf("invalid cron expression: %w", err)
	}
	loc := config.GetTimeZone()
	at := now()
	// Nothing fires for the time before the rule was last saved: enabling a rule does not replay the past.
	base := rule.LastFiredAt
	if rule.Updated.After(base) {
		base = rule.Updated
	}
	due := latestOccurrence(sched, base.In(loc), at.In(loc))
	if due.IsZero() {
		return 0, nil
	}
	// When the clocks go back, a wall clock time happens twice. The schedule fires for the first one only: an
	// occurrence that reads the same on the wall clock as the one fired last is the repeat.
	if !rule.LastFiredAt.IsZero() && !due.Equal(rule.LastFiredAt) {
		const wall = "2006-01-02 15:04"
		if due.Format(wall) == rule.LastFiredAt.In(loc).Format(wall) {
			return 0, nil
		}
	}
	due = due.UTC()
	if ok, err := ownerMayFire(rule); err != nil || !ok {
		return 0, err
	}

	s := db.NewSession()
	defer s.Close()
	// The claim comes first, so of several replicas only one scans the tasks: the occurrence is stored as the last
	// fired time, only when it is newer than what is stored. Of several replicas, exactly one changes the row. The
	// claim, the scan and the events share one transaction: a crash in between claims nothing.
	affected, err := s.Where(builder.And(
		builder.Eq{"id": rule.ID, "enabled": true},
		builder.Or(builder.IsNull{"last_fired_at"}, builder.Lt{"last_fired_at": dbTime(due)}),
	)).Cols("last_fired_at").NoAutoTime().Update(&models.AutomationRule{LastFiredAt: due})
	if err != nil {
		_ = s.Rollback()
		return 0, err
	}
	if affected != 1 {
		_ = s.Rollback()
		return 0, nil
	}
	targets, note, err := scheduleTargets(s, rule, at)
	if err != nil {
		_ = s.Rollback()
		return 0, err
	}
	if _, err = s.ID(rule.ID).Cols("last_fire_note").NoAutoTime().Update(&models.AutomationRule{LastFireNote: truncate(note, 250)}); err != nil {
		_ = s.Rollback()
		return 0, err
	}
	owner, _ := user.GetUserByID(s, rule.OwnerID)
	for _, taskID := range targets {
		from, err := taskOriginOf(s, taskID)
		if err != nil {
			_ = s.Rollback()
			return 0, err
		}
		if _, err = insertTargetedEvent(s, rule, spec.EventSchedule, taskID, &Payload{Doer: personOf(owner), New: map[string]any{"scheduled_for": due.Format(time.RFC3339)}}, from); err != nil {
			_ = s.Rollback()
			return 0, err
		}
	}
	if err = s.Commit(); err != nil {
		return 0, err
	}
	if strings.Contains(note, "capped") {
		log.Warningf("[automation] schedule of rule %d: %s", rule.ID, note)
	}
	return len(targets), nil
}

// scheduleTargets returns the tasks a schedule firing starts a run for: the tasks of the project that match the
// conditions, up to the cap per tick. For the project scope it is a single entry without a task. note says what
// happened (capped, or the number of tasks).
func scheduleTargets(s *xorm.Session, rule *models.AutomationRule, at time.Time) (targets []int64, note string, err error) {
	if !spec.TriggerHasTask(rule.Trigger) {
		return []int64{0}, "project: one run", nil
	}
	maxTasks := config.AutomationTickMaxTasks.GetInt()
	maxScan := config.AutomationTickMaxScan.GetInt()

	scanned := 0
	capped := false
	const pageSize = 200
	var lastID int64
scan:
	for scanned < maxScan {
		ids := []int64{}
		if err = s.Table("tasks").Where("project_id = ? AND id > ?", rule.ProjectID, lastID).OrderBy("id ASC").Limit(pageSize).Cols("id").Find(&ids); err != nil {
			return nil, "", err
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			lastID = id
			if scanned >= maxScan {
				break scan
			}
			scanned++
			ok, err := matchesPrefilter(s, rule, id, at)
			if err != nil {
				return nil, "", err
			}
			if !ok {
				continue
			}
			if maxTasks > 0 && len(targets) >= maxTasks {
				capped = true
				break scan
			}
			targets = append(targets, id)
		}
	}
	note = fmt.Sprintf("%d task(s) matched", len(targets))
	if capped {
		note = fmt.Sprintf("capped: started %d task(s), more match (automation.ticker.maxtasks); the rest is skipped for this firing", len(targets))
	} else if scanned >= maxScan {
		note = fmt.Sprintf("%d task(s) matched, stopped scanning after %d tasks (automation.ticker.maxscan)", len(targets), scanned)
	}
	return targets, note, nil
}

// matchesPrefilter evaluates the conditions of the rule for a task before a run is queued, so tasks that do not
// match cost nothing but this check. Script conditions are not evaluated here (they match); the run evaluates them.
func matchesPrefilter(s *xorm.Session, rule *models.AutomationRule, taskID int64, at time.Time) (bool, error) {
	if rule.Conditions == nil {
		return true, nil
	}
	sub, err := LoadSubject(s, taskID, &Payload{ProjectID: rule.ProjectID, TaskID: taskID}, at)
	if err != nil {
		if errors.Is(err, ErrTaskGone) {
			return false, nil
		}
		return false, err
	}
	sub.Script = func(string) (bool, error) { return true, nil }
	ok, err := Evaluate(rule.Conditions, sub)
	if err != nil {
		// A condition that cannot be evaluated is left to the run, which records the failure.
		return true, nil //nolint:nilerr // intended: the run records the failure
	}
	return ok, nil
}

// tickDue fires an overdue or due soon rule for the tasks that crossed their line since the last tick. A marker per
// (rule, task) remembers the due date it fired for, so a task fires once per due date.
func tickDue(rule *models.AutomationRule) (int, error) {
	at := now()
	s := db.NewReadSession()
	defer s.Close()

	cond := builder.And(
		builder.Eq{"project_id": rule.ProjectID, "done": false},
		builder.NotNull{"due_date"},
	)
	// What the rule finds when it is armed carries a marker (models.seedAutomationDueMarkers), so everything without
	// one fires: a task that became overdue or entered the window since, one created there, or one whose due date
	// was edited into it.
	switch rule.Trigger.Type {
	case spec.TriggerTaskOverdue:
		cond = builder.And(cond, builder.Lt{"due_date": dbTime(at)})
	case spec.TriggerTaskDueSoon:
		offsetText, _ := rule.Trigger.Options["offset"].(string)
		offset, err := spec.ParseOffset(offsetText)
		if err != nil {
			return 0, err
		}
		cond = builder.And(cond, builder.Gt{"due_date": dbTime(at)}, builder.Lte{"due_date": dbTime(at.Add(offset))})
	}
	tasks := []*models.Task{}
	q := s.Where(cond).And("NOT EXISTS (SELECT 1 FROM automation_due_markers m WHERE m.rule_id = ? AND m.task_id = tasks.id AND m.due_date = tasks.due_date)", rule.ID).
		OrderBy("due_date ASC, id ASC").Cols("id", "due_date")
	if limit := config.AutomationTickMaxScan.GetInt(); limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&tasks); err != nil {
		return 0, err
	}

	if len(tasks) == 0 {
		return 0, nil
	}
	if ok, err := ownerMayFire(rule); err != nil || !ok {
		return 0, err
	}

	maxTasks := config.AutomationTickMaxTasks.GetInt()
	fired := 0
	for _, task := range tasks {
		if maxTasks > 0 && fired >= maxTasks {
			break
		}
		ok, err := matchesPrefilter(s, rule, task.ID, at)
		if err != nil {
			return fired, err
		}
		claimed, err := claimDue(rule, task, at, ok)
		if err != nil {
			return fired, err
		}
		if claimed && ok {
			fired++
		}
	}
	return fired, nil
}

// claimDue stores the marker of (rule, task) for the due date and, when matched is set, queues the run in the same
// transaction. It returns false when another replica (or an earlier tick) claimed the due date.
func claimDue(rule *models.AutomationRule, task *models.Task, at time.Time, matched bool) (bool, error) {
	s := db.NewSession()
	defer s.Close()

	marker := &models.AutomationDueMarker{}
	has, err := s.Where("rule_id = ? AND task_id = ?", rule.ID, task.ID).Get(marker)
	if err != nil {
		_ = s.Rollback()
		return false, err
	}
	if !has {
		marker = &models.AutomationDueMarker{RuleID: rule.ID, TaskID: task.ID, DueDate: task.DueDate, Fired: at}
		if _, err = s.Insert(marker); err != nil {
			_ = s.Rollback()
			// The unique index on (rule, task) means someone else inserted the marker first. The drivers report
			// that differently, so look: when the marker is there the claim is lost, otherwise the error is real.
			check := db.NewReadSession()
			defer check.Close()
			exists, getErr := check.Where("rule_id = ? AND task_id = ?", rule.ID, task.ID).Exist(&models.AutomationDueMarker{})
			if getErr == nil && exists {
				return false, nil
			}
			log.Errorf("[automation] ticker: could not claim task %d for rule %d: %v", task.ID, rule.ID, err)
			return false, err
		}
	} else {
		affected, err := s.Where(builder.And(builder.Eq{"id": marker.ID}, builder.Neq{"due_date": dbTime(task.DueDate)})).
			Cols("due_date", "fired").Update(&models.AutomationDueMarker{DueDate: task.DueDate, Fired: at})
		if err != nil {
			_ = s.Rollback()
			return false, err
		}
		if affected != 1 {
			_ = s.Rollback()
			return false, nil
		}
	}
	if matched {
		owner, _ := user.GetUserByID(s, rule.OwnerID)
		name := spec.EventOverdue
		if rule.Trigger.Type == spec.TriggerTaskDueSoon {
			name = spec.EventDueSoon
		}
		payload := &Payload{Doer: personOf(owner), New: map[string]any{"due_date": task.DueDate.UTC().Format(time.RFC3339)}}
		from, err := taskOriginOf(s, task.ID)
		if err != nil {
			_ = s.Rollback()
			return false, err
		}
		if _, err = insertTargetedEvent(s, rule, name, task.ID, payload, from); err != nil {
			_ = s.Rollback()
			return false, err
		}
	}
	return true, s.Commit()
}

// tickLoop runs Tick every automation.ticker.interval.
func tickLoop(ctx context.Context) {
	interval := config.AutomationTickInterval.GetDuration()
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := Tick(ctx); err != nil && ctx.Err() == nil {
				log.Errorf("[automation] ticker failed: %v", err)
			}
		}
	}
}
