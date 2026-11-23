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
	"encoding/json"
	"sync"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/cron"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// One event per claim: the lock of a claimed event covers one run timeout. Claiming a batch and working through it
// in sequence could keep the later events locked past locked_until, and another worker would take them again.
const claimBatchSize = 1

// retryBase is the wait before the second attempt; it doubles with every further attempt. Tests shorten it.
var retryBase = 10 * time.Second

func backoff(attempt int64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 20 {
		attempt = 20
	}
	return retryBase << (attempt - 1)
}

var wakeCh = make(chan struct{}, 64)

// Wake tells the workers of this process that new events were committed. Events written by other replicas are
// found by polling.
func Wake() {
	select {
	case wakeCh <- struct{}{}:
	default:
	}
}

// lockDuration is how long a claimed event belongs to its worker before another one may take it over.
func lockDuration() time.Duration {
	return config.AutomationRunTimeout.GetDuration() + 30*time.Second
}

// recoverStuck gives events back whose worker disappeared: they stay in processing past locked_until. Events that
// used up their attempts become dead instead.
func recoverStuck(s *xorm.Session) error {
	stuck := builder.And(
		builder.Eq{"status": models.AutomationEventProcessing},
		builder.Lt{"locked_until": dbTime(now())},
	)
	if _, err := s.Where(builder.And(stuck, builder.Gte{"attempts": config.AutomationMaxAttempts.GetInt()})).
		Cols("status", "locked_until").
		Update(&models.AutomationEvent{Status: models.AutomationEventDead}); err != nil {
		return err
	}
	_, err := s.Where(stuck).Cols("status", "locked_until").
		Update(&models.AutomationEvent{Status: models.AutomationEventPending})
	return err
}

// claim takes up to limit due events. Each one is taken with a conditional update, so two workers - in this process
// or in another replica - never get the same row, on every supported database.
func claim(s *xorm.Session, limit int) ([]*models.AutomationEvent, error) {
	candidates := []*models.AutomationEvent{}
	err := s.Where(builder.And(
		builder.Eq{"status": models.AutomationEventPending},
		builder.Lte{"run_after": dbTime(now())},
	)).OrderBy("id ASC").Limit(limit).Find(&candidates)
	if err != nil {
		return nil, err
	}

	claimed := make([]*models.AutomationEvent, 0, len(candidates))
	for _, ev := range candidates {
		affected, err := s.Where("id = ? AND status = ?", ev.ID, models.AutomationEventPending).
			Incr("attempts").
			Cols("status", "locked_until").
			Update(&models.AutomationEvent{
				Status:      models.AutomationEventProcessing,
				LockedUntil: now().Add(lockDuration()),
			})
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			continue
		}
		ev.Status = models.AutomationEventProcessing
		ev.Attempts++
		claimed = append(claimed, ev)
	}
	return claimed, nil
}

// ProcessPending claims and processes events until none is due. It returns the number of events it processed.
// The workers call it in a loop; tests call it to drain the queue synchronously. Several calls may run at once.
func ProcessPending(ctx context.Context) (processed int, err error) {
	if !config.AutomationEnabled.GetBool() {
		return 0, nil
	}
	for ctx.Err() == nil {
		s := db.NewAutocommitSession()
		if err = recoverStuck(s); err != nil {
			s.Close()
			return processed, err
		}
		events, err := claim(s, claimBatchSize)
		s.Close()
		if err != nil {
			return processed, err
		}
		if len(events) == 0 {
			return processed, nil
		}
		for _, ev := range events {
			processEvent(ctx, ev)
			processed++
		}
	}
	return processed, ctx.Err()
}

func processEvent(ctx context.Context, ev *models.AutomationEvent) {
	log.Debugf("[automation] claimed event %d (%s, attempt %d)", ev.ID, ev.Name, ev.Attempts)

	retryErr := handleEvent(ctx, ev)
	if ctx.Err() != nil && retryErr == nil {
		// Interrupted by shutdown: let the lock expire, another process resumes it.
		return
	}

	s := db.NewAutocommitSession()
	defer s.Close()

	update := &models.AutomationEvent{Status: models.AutomationEventDone}
	switch {
	case retryErr == nil:
	case ev.Attempts >= int64(config.AutomationMaxAttempts.GetInt()):
		update.Status = models.AutomationEventDead
		update.LastError = truncate(retryErr.Error(), 4000)
		failRunningRuns(s, ev, retryErr)
	default:
		update.Status = models.AutomationEventPending
		update.RunAfter = now().Add(backoff(ev.Attempts))
		update.LastError = truncate(retryErr.Error(), 4000)
	}
	cols := []string{"status", "locked_until", "last_error"}
	if update.Status == models.AutomationEventPending {
		cols = append(cols, "run_after")
	}
	if _, err := s.Where("id = ? AND status = ?", ev.ID, models.AutomationEventProcessing).Cols(cols...).Update(update); err != nil {
		log.Errorf("[automation] could not update event %d: %v", ev.ID, err)
	}
}

// failRunningRuns ends the runs of an event that gave up: they stay resumable only as long as retries are left.
func failRunningRuns(s *xorm.Session, ev *models.AutomationEvent, cause error) {
	runs := []*models.AutomationRun{}
	if err := s.Where("event_id = ? AND status = ?", ev.ID, models.AutomationRunRunning).Find(&runs); err != nil {
		log.Errorf("[automation] could not load the runs of event %d: %v", ev.ID, err)
		return
	}
	for _, run := range runs {
		rule, err := models.GetAutomationRule(s, run.RuleID)
		if err != nil {
			rule = &models.AutomationRule{ID: run.RuleID}
		}
		rc := &runContext{ev: ev, rule: rule, run: run}
		if err := rc.finish(s, models.AutomationRunFailed, "", cause); err != nil {
			log.Errorf("[automation] could not fail run %d: %v", run.ID, err)
		}
	}
}

// handleEvent runs every enabled rule that listens to the event. A returned error means that at least one run
// stopped for a reason worth retrying.
func handleEvent(ctx context.Context, ev *models.AutomationEvent) error {
	payload := &Payload{}
	if err := json.Unmarshal([]byte(ev.Payload), payload); err != nil {
		// A payload we wrote ourselves and cannot read is a bug; retrying cannot fix it.
		log.Errorf("[automation] event %d has an unreadable payload: %v", ev.ID, err)
		return nil
	}

	s := db.NewAutocommitSession()
	rules := []*models.AutomationRule{}
	err := s.Where("project_id = ? AND enabled = ?", ev.ProjectID, true).OrderBy("id ASC").Find(&rules)
	if err == nil {
		abortOrphanedRuns(s, ev, rules)
	}
	s.Close()
	if err != nil {
		return err
	}

	var firstErr error
	for _, rule := range rules {
		def := spec.TriggerByType(rule.Trigger.Type)
		if def == nil || def.Event != ev.Name {
			continue
		}
		renewLock(ev)
		if err := runRule(ctx, ev, payload, rule); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// renewLock gives the worker of a claimed event the lock duration again. The lock covers one run timeout, and every
// rule of the event gets a run timeout of its own: without renewing, an event with several slow rules would be
// taken over by another worker while this one is still at work.
func renewLock(ev *models.AutomationEvent) {
	s := db.NewAutocommitSession()
	defer s.Close()
	_, err := s.Where("id = ? AND status = ?", ev.ID, models.AutomationEventProcessing).
		Cols("locked_until").
		Update(&models.AutomationEvent{LockedUntil: now().Add(lockDuration())})
	if err != nil {
		log.Errorf("[automation] could not renew the lock of event %d: %v", ev.ID, err)
	}
}

// abortOrphanedRuns ends runs of this event whose rule was disabled or deleted while the run waited for a retry.
func abortOrphanedRuns(s *xorm.Session, ev *models.AutomationEvent, enabled []*models.AutomationRule) {
	ids := map[int64]bool{}
	for _, r := range enabled {
		ids[r.ID] = true
	}
	runs := []*models.AutomationRun{}
	if err := s.Where("event_id = ? AND status = ?", ev.ID, models.AutomationRunRunning).Find(&runs); err != nil {
		return
	}
	for _, run := range runs {
		if ids[run.RuleID] {
			continue
		}
		run.Status = models.AutomationRunAborted
		run.SkipReason = SkipRuleDisabled
		run.Finished = now()
		if _, err := s.ID(run.ID).Cols("status", "skip_reason", "finished").Update(run); err != nil {
			log.Errorf("[automation] could not abort run %d: %v", run.ID, err)
		}
	}
}

var (
	workersMu     sync.Mutex
	workersCancel context.CancelFunc
	workersDone   sync.WaitGroup
)

// Start launches the workers of this process. Calling it again replaces the running workers.
func Start(parent context.Context) {
	Stop()
	workersMu.Lock()
	defer workersMu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	workersCancel = cancel
	n := config.AutomationWorkers.GetInt()
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		workersDone.Add(1)
		go func() {
			defer workersDone.Done()
			workerLoop(ctx)
		}()
	}
	log.Infof("[automation] started %d workers", n)
}

// Stop stops the workers and waits for the runs in progress.
func Stop() {
	workersMu.Lock()
	cancel := workersCancel
	workersCancel = nil
	workersMu.Unlock()
	if cancel != nil {
		cancel()
	}
	workersDone.Wait()
}

func workerLoop(ctx context.Context) {
	interval := config.AutomationPollInterval.GetDuration()
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := ProcessPending(ctx); err != nil && ctx.Err() == nil {
			log.Errorf("[automation] could not process events: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-wakeCh:
		case <-ticker.C:
		}
	}
}

// Cleanup deletes finished events, runs and steps older than automation.retention days.
func Cleanup(olderThan time.Time) (events, runs int64, err error) {
	s := db.NewSession()
	defer s.Close()
	events, runs, err = models.CleanupAutomation(s, olderThan)
	if err != nil {
		_ = s.Rollback()
		return 0, 0, err
	}
	return events, runs, s.Commit()
}

// RegisterCleanupCron schedules the retention job.
func RegisterCleanupCron() {
	err := cron.Schedule("17 3 * * *", func() {
		days := config.AutomationRetention.GetInt()
		if days <= 0 {
			return
		}
		events, runs, err := Cleanup(now().AddDate(0, 0, -days))
		if err != nil {
			log.Errorf("[automation] cleanup failed: %v", err)
			return
		}
		log.Debugf("[automation] cleanup deleted %d events and %d runs", events, runs)
	})
	if err != nil {
		log.Fatalf("Could not register the automation cleanup cron: %s", err)
	}
}

// Init starts the workers and the retention job. Call it once after the database is ready.
func Init() {
	if !config.AutomationEnabled.GetBool() {
		log.Infof("[automation] disabled by configuration")
		return
	}
	initMetrics()
	RegisterCleanupCron()
	Start(context.Background())
}
