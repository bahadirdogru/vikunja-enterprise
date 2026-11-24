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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"code.vikunja.io/api/pkg/automation/script"
	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// The reasons the system stores on a rule it disabled.
const (
	AutomationDisabledOwnerNotAdmin      = "owner_not_admin"
	AutomationDisabledOwnerUnavailable   = "owner_unavailable"
	AutomationDisabledTooManyFailures    = "too_many_failures"
	AutomationDisabledCustomFieldDeleted = "custom_field_deleted"
	AutomationDisabledStatusDeleted      = "status_deleted"
	AutomationDisabledDuplicated         = "duplicated"
	AutomationDisabledUnmappedReference  = "duplicated_unmapped_reference"
	// AutomationDisabledSecretsMissing is the reason of a duplicated rule that uses secrets: secrets are not
	// copied to the duplicate.
	AutomationDisabledSecretsMissing = "duplicated_secrets_missing"
)

// The statuses of an automation event in the outbox.
const (
	AutomationEventPending    = "pending"
	AutomationEventProcessing = "processing"
	AutomationEventDone       = "done"
	AutomationEventDead       = "dead"
)

// The statuses of an automation run.
const (
	AutomationRunRunning = "running"
	AutomationRunSuccess = "success"
	AutomationRunFailed  = "failed"
	AutomationRunSkipped = "skipped"
	AutomationRunAborted = "aborted"
)

// The statuses of a run step.
const (
	AutomationStepSuccess = "success"
	AutomationStepFailed  = "failed"
	AutomationStepSkipped = "skipped"
)

// AutomationRule is a when / if / then rule of a project. It runs as its owner.
type AutomationRule struct {
	ID          int64  `xorm:"bigint autoincr not null unique pk" json:"id" param:"rule" readOnly:"true" doc:"The unique, numeric id of this rule."`
	ProjectID   int64  `xorm:"bigint not null INDEX" json:"project_id" param:"project" readOnly:"true" doc:"The project this rule belongs to. Taken from the URL."`
	Title       string `xorm:"varchar(250) not null" json:"title" minLength:"1" maxLength:"250" doc:"The title of the rule."`
	Description string `xorm:"longtext null" json:"description" doc:"What the rule is for."`
	Enabled     bool   `xorm:"bool default false" json:"enabled" doc:"Whether the rule reacts to events. A rule the system disabled has a disabled_reason; enabling it clears the reason."`
	Revision    int64  `xorm:"bigint not null default 1" json:"revision" readOnly:"true" doc:"Increases with every change of the rule. Runs record the revision they ran with."`
	OwnerID     int64  `xorm:"bigint not null" json:"owner_id" readOnly:"true" doc:"The user the rule runs as. Always the user who last saved the rule: actions can never do more than this user could do by hand. The rule is disabled when the owner stops being admin of the project."`

	Trigger    spec.Trigger    `xorm:"json not null" json:"trigger" doc:"When the rule runs."`
	Conditions *spec.Condition `xorm:"json null" json:"conditions" doc:"Optional conditions that have to hold, evaluated against the task as it is when the rule runs."`
	Actions    []*spec.Action  `xorm:"json not null" json:"actions" doc:"What the rule does, in order. A failing action stops the run."`

	DisabledReason      string    `xorm:"varchar(100) null" json:"disabled_reason" readOnly:"true" doc:"Set when the system disabled the rule: owner_not_admin, owner_unavailable, too_many_failures, custom_field_deleted, status_deleted, duplicated or duplicated_unmapped_reference."`
	ConsecutiveFailures int64     `xorm:"bigint not null default 0" json:"consecutive_failures" readOnly:"true" doc:"How many runs failed in a row. The rule is disabled when this reaches automation.maxconsecutivefailures."`
	LastRunAt           time.Time `xorm:"DATETIME null" json:"last_run_at" readOnly:"true" doc:"When the rule last ran."`
	LastRunStatus       string    `xorm:"varchar(20) null" json:"last_run_status" readOnly:"true" doc:"The status of the last run."`
	// ArmedAt is when the rule was created, last enabled or last got another trigger: at that moment the tasks that
	// already are overdue (or inside the due soon window) get a due marker, so enabling a rule does not replay the
	// past and editing its actions does not move the line.
	ArmedAt      time.Time `xorm:"DATETIME null" json:"-"`
	LastFiredAt  time.Time `xorm:"DATETIME null" json:"last_fired_at" readOnly:"true" doc:"Schedule triggers: when the schedule last fired. Zero for other triggers and before the first firing."`
	LastFireNote string    `xorm:"varchar(250) null" json:"last_fire_note" readOnly:"true" doc:"Schedule triggers: what the last firing did, e.g. that it was capped at automation.ticker.maxtasks tasks."`

	// WebhookTokenHash is the SHA-256 of the secret in the URL of an incoming webhook; the token itself is never stored.
	WebhookTokenHash string `xorm:"varchar(64) null INDEX" json:"-"`
	// HasWebhook is set on reads: the rule has a webhook URL.
	HasWebhook bool `xorm:"-" json:"has_webhook" readOnly:"true" doc:"Whether the rule (trigger webhook.incoming) has a webhook URL."`
	// WebhookToken is only set in the answer of the request that created or regenerated the token.
	WebhookToken string `xorm:"-" json:"webhook_token,omitempty" readOnly:"true" doc:"The secret of the webhook URL. Only returned once, by the request that created or regenerated it: the server stores a hash only."`
	WebhookURL   string `xorm:"-" json:"webhook_url,omitempty" readOnly:"true" doc:"The full URL of the incoming webhook. Only returned together with webhook_token."`

	Created time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this rule was created. You cannot change this value."`
	Updated time.Time `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this rule was last updated. You cannot change this value."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`

	// requestCtx bounds the compile check of the scripts when the rule is saved.
	requestCtx context.Context
	// checkedSources are the script sources of the stored rule: they passed the check when they were saved, and
	// are not checked again, so a rule can be disabled or edited while no script process is available.
	checkedSources map[string]bool
}

// SetRequestContext makes the compile check of a save end with the request.
func (r *AutomationRule) SetRequestContext(ctx context.Context) {
	r.requestCtx = ctx
}

// TableName returns the table name of automation rules.
func (*AutomationRule) TableName() string {
	return "automation_rules"
}

// AutomationEvent is a row of the outbox. It is written in the transaction of the change that caused it.
type AutomationEvent struct {
	ID          int64     `xorm:"bigint autoincr not null unique pk" json:"id"`
	ProjectID   int64     `xorm:"bigint not null INDEX" json:"project_id"`
	TaskID      int64     `xorm:"bigint null" json:"task_id"`
	Name        string    `xorm:"varchar(100) not null" json:"name"`
	Payload     string    `xorm:"longtext not null" json:"payload"`
	Depth       int64     `xorm:"bigint not null default 0" json:"depth"`
	OriginRunID int64     `xorm:"bigint null" json:"origin_run_id"`
	Status      string    `xorm:"varchar(20) not null index(automation_events_poll)" json:"status"`
	Attempts    int64     `xorm:"bigint not null default 0" json:"attempts"`
	RunAfter    time.Time `xorm:"DATETIME not null index(automation_events_poll)" json:"run_after"`
	LockedUntil time.Time `xorm:"DATETIME null" json:"locked_until"`
	LastError   string    `xorm:"text null" json:"last_error"`
	// RuleID is set for events that are meant for one rule only (manual, schedule, due, webhook, resume).
	// The index also serves the rate limit count of incoming webhooks (rule, name, created).
	RuleID  int64     `xorm:"bigint null index(automation_events_rule_created)" json:"rule_id"`
	Created time.Time `xorm:"created not null index(automation_events_rule_created)" json:"created"`
}

// TableName returns the table name of automation events.
func (*AutomationEvent) TableName() string {
	return "automation_events"
}

// AutomationRunStep is one executed action of a run.
type AutomationRunStep struct {
	ID         int64     `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this step."`
	RunID      int64     `xorm:"bigint not null INDEX unique(automation_run_steps_position)" json:"run_id" readOnly:"true" doc:"The run the step belongs to."`
	Position   int64     `xorm:"bigint not null unique(automation_run_steps_position)" json:"position" readOnly:"true" doc:"The index of the action in the rule, starting at 0."`
	ActionType string    `xorm:"varchar(100) not null" json:"action_type" readOnly:"true" doc:"The type of the action."`
	Status     string    `xorm:"varchar(20) not null" json:"status" readOnly:"true" enum:"success,failed,skipped" doc:"success, failed or skipped."`
	Error      string    `xorm:"text null" json:"error" readOnly:"true" doc:"Why the step failed."`
	Output     string    `xorm:"text null" json:"output" readOnly:"true" doc:"What the step did, truncated to 16 KiB."`
	DurationMs int64     `xorm:"bigint not null default 0" json:"duration_ms" readOnly:"true" doc:"How long the step took."`
	Created    time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"When the step was recorded."`
}

// TableName returns the table name of automation run steps.
func (*AutomationRunStep) TableName() string {
	return "automation_run_steps"
}

// getAutomationRuleInProject loads a rule and makes sure it belongs to the project.
func getAutomationRuleInProject(s *xorm.Session, ruleID, projectID int64) (*AutomationRule, error) {
	rule := &AutomationRule{}
	has, err := s.Where("id = ? AND project_id = ?", ruleID, projectID).Get(rule)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrAutomationRuleDoesNotExist{RuleID: ruleID}
	}
	return rule, nil
}

// GetAutomationRule loads a rule by id.
func GetAutomationRule(s *xorm.Session, ruleID int64) (*AutomationRule, error) {
	rule := &AutomationRule{}
	has, err := s.ID(ruleID).Get(rule)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrAutomationRuleDoesNotExist{RuleID: ruleID}
	}
	return rule, nil
}

// Doc returns the rule document in the form the spec package works with.
func (r *AutomationRule) Doc() *spec.Doc {
	return &spec.Doc{Trigger: r.Trigger, Conditions: r.Conditions, Actions: r.Actions}
}

// Validate checks the rule document against the catalog and the project: every id has to exist in the project
// and the auth has to be allowed to use it. All problems are returned together.
func (r *AutomationRule) Validate(s *xorm.Session, a web.Auth) error {
	if r.Title == "" {
		return ErrAutomationRuleInvalid{AutomationRuleInvalidDetails{Issues: []spec.Issue{{Path: "title", Message: "is required"}}}}
	}
	if r.Actions == nil {
		r.Actions = []*spec.Action{}
	}
	for _, act := range r.Actions {
		if act != nil && act.Params == nil {
			act.Params = map[string]any{}
		}
	}
	issues := spec.Validate(r.Doc(), config.AutomationMaxActions.GetInt())
	if len(issues) > 0 {
		return ErrAutomationRuleInvalid{AutomationRuleInvalidDetails{Issues: issues}}
	}
	ctx := r.requestCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if issues, err := validateAutomationScripts(ctx, s, r.ProjectID, r.Doc(), r.checkedSources); err != nil {
		return err
	} else if len(issues) > 0 {
		return ErrAutomationRuleInvalid{AutomationRuleInvalidDetails{Issues: issues}}
	}
	if issues, err := validateAutomationLimits(s, r); err != nil {
		return err
	} else if len(issues) > 0 {
		return ErrAutomationRuleInvalid{AutomationRuleInvalidDetails{Issues: issues}}
	}
	issues, err := validateAutomationReferences(s, a, r.ProjectID, r.Doc())
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return ErrAutomationRuleInvalid{AutomationRuleInvalidDetails{Issues: issues}}
	}
	return nil
}

// validateAutomationScripts checks the scripts (kill switch, size, syntax) and that the secrets the rule names
// exist. A syntax error is reported with the path of the script and the line and column of the error.
func validateAutomationScripts(ctx context.Context, s *xorm.Session, projectID int64, doc *spec.Doc, checked map[string]bool) ([]spec.Issue, error) {
	scripts := spec.Scripts(doc)
	issues := []spec.Issue{}
	if len(scripts) > 0 {
		if !script.Enabled() {
			return nil, ErrAutomationScriptsDisabled{}
		}
		maxSource := script.MaxSource()
		for _, sc := range scripts {
			if maxSource > 0 && len(sc.Source) > maxSource {
				issues = append(issues, spec.Issue{Path: sc.Path, Message: fmt.Sprintf("the script is %d bytes long, at most %d are allowed", len(sc.Source), maxSource)})
				continue
			}
			if checked[sc.Source] {
				continue
			}
			synErr, err := script.Check(ctx, projectID, sc.Source)
			if errors.Is(err, script.ErrUnavailable) {
				// Not the script's fault (every script process busy, none can be started): the save is refused. It is
				// never compiled in the server instead, where a hostile source could take seconds.
				issues = append(issues, spec.Issue{Path: sc.Path, Message: "the script could not be checked: " + err.Error()})
				continue
			}
			if err != nil {
				return nil, err
			}
			if synErr != nil {
				issues = append(issues, spec.Issue{Path: sc.Path, Message: "syntax error: " + synErr.Message, Line: synErr.Line, Column: synErr.Column})
			}
		}
	}
	refs := spec.SecretRefs(doc)
	if len(refs) > 0 {
		names, err := AutomationSecretNames(s, projectID)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if !names[ref.Name] {
				issues = append(issues, spec.Issue{Path: ref.Path, Message: fmt.Sprintf("the secret %q does not exist in this project", ref.Name)})
			}
		}
	}
	return issues, nil
}

type automationRef struct {
	id   int64
	path string
}

func validateAutomationReferences(s *xorm.Session, a web.Auth, projectID int64, doc *spec.Doc) ([]spec.Issue, error) {
	refs := map[string][]automationRef{}
	spec.Walk(spec.CloneDoc(doc), spec.Handler{ID: func(kind string, id int64, path string) int64 {
		refs[kind] = append(refs[kind], automationRef{id, path})
		return id
	}})
	issues := []spec.Issue{}
	missing := func(r automationRef, what string) {
		issues = append(issues, spec.Issue{Path: r.path, Message: what + " does not exist in this project"})
	}

	if len(refs[spec.KindCustomField]) > 0 {
		ids := make([]int64, 0)
		for _, r := range refs[spec.KindCustomField] {
			ids = append(ids, r.id)
		}
		found := []int64{}
		if err := s.Table("custom_fields").Where(builder.And(builder.Eq{"project_id": projectID}, builder.In("id", ids))).Cols("id").Find(&found); err != nil {
			return nil, err
		}
		exists := map[int64]bool{}
		for _, id := range found {
			exists[id] = true
		}
		for _, r := range refs[spec.KindCustomField] {
			if !exists[r.id] {
				missing(r, "the custom field")
			}
		}
	}

	if len(refs[spec.KindStatus]) > 0 {
		def, err := loadWorkflowDef(s, projectID)
		if err != nil {
			return nil, err
		}
		for _, r := range refs[spec.KindStatus] {
			var st *WorkflowStatus
			if def != nil {
				st = def.statuses[r.id]
			}
			switch {
			case st == nil:
				missing(r, "the status")
			case st.Archived && len(r.path) > 7 && r.path[:7] == "actions":
				issues = append(issues, spec.Issue{Path: r.path, Message: "the status is archived"})
			}
		}
	}

	for _, r := range refs[spec.KindLabel] {
		label := &Label{ID: r.id}
		has, _, err := label.hasAccessToLabel(s, a)
		if err != nil && !IsErrLabelDoesNotExist(err) {
			return nil, err
		}
		if err != nil || !has {
			issues = append(issues, spec.Issue{Path: r.path, Message: "the label does not exist or you have no access to it"})
		}
	}

	for _, r := range refs[spec.KindProject] {
		// Only the target of task.move needs to be writable; conditions just compare ids.
		if len(r.path) < 7 || r.path[:7] != "actions" {
			continue
		}
		can, err := (&Project{ID: r.id}).CanWrite(s, a)
		if err != nil && !IsErrProjectDoesNotExist(err) {
			return nil, err
		}
		if err != nil || !can {
			issues = append(issues, spec.Issue{Path: r.path, Message: "the project does not exist or you have no write access to it"})
		}
	}

	teamIssues, err := validateAutomationTeams(s, a, doc)
	if err != nil {
		return nil, err
	}
	issues = append(issues, teamIssues...)
	return issues, nil
}

// validateAutomationTeams checks that the team a notify action addresses is one the owner can see: the run reads its
// members as the owner. Teams are not remapped when a rule is copied, so they are not part of the reference walk.
func validateAutomationTeams(s *xorm.Session, a web.Auth, doc *spec.Doc) ([]spec.Issue, error) {
	issues := []spec.Issue{}
	for i, act := range doc.Actions {
		if act == nil || act.Type != spec.ActionNotify {
			continue
		}
		teamID, ok := spec.ToInt64(act.Params["team_id"])
		if !ok || teamID <= 0 {
			continue
		}
		can, _, err := (&Team{ID: teamID}).CanRead(s, a)
		if err != nil {
			return nil, err
		}
		if !can {
			issues = append(issues, spec.Issue{
				Path:    fmt.Sprintf("actions[%d].params.team_id", i),
				Message: "the team does not exist or you are not a member of it",
			})
		}
	}
	return issues, nil
}

// ReadAll returns the rules of a project. Needs admin access: rules can reveal other users' data.
// @Summary List the automation rules of a project
func (r *AutomationRule) ReadAll(s *xorm.Session, a web.Auth, search string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	// DoReadAll skips the Can* checks.
	if r.ProjectID < 1 {
		return nil, 0, 0, ErrProjectDoesNotExist{ID: r.ProjectID}
	}
	can, _, err := canAdministerAutomation(s, a, r.ProjectID)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	var cond builder.Cond = builder.Eq{"project_id": r.ProjectID}
	if search != "" {
		cond = cond.And(db.ILIKE("title", search))
	}
	total, err := s.Where(cond).Count(&AutomationRule{})
	if err != nil {
		return nil, 0, 0, err
	}
	rules := []*AutomationRule{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where(cond).OrderBy("id ASC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&rules); err != nil {
		return nil, 0, 0, err
	}
	for _, rule := range rules {
		rule.fillComputed()
	}
	return rules, len(rules), total, nil
}

// ReadOne returns one rule; CanRead already loaded it.
// @Summary Get an automation rule
func (r *AutomationRule) ReadOne(_ *xorm.Session, _ web.Auth) error {
	r.fillComputed()
	return nil
}

func ownerOf(a web.Auth) (int64, error) {
	u, is := a.(*user.User)
	if !is || u.ID <= 0 {
		return 0, ErrGenericForbidden{}
	}
	return u.ID, nil
}

// Create validates and stores a rule. The creator becomes its owner.
// @Summary Create an automation rule
func (r *AutomationRule) Create(s *xorm.Session, a web.Auth) error {
	ownerID, err := ownerOf(a)
	if err != nil {
		return err
	}
	if err = r.Validate(s, a); err != nil {
		return err
	}
	r.ID = 0
	r.OwnerID = ownerID
	r.Revision = 1
	r.ConsecutiveFailures = 0
	r.DisabledReason = ""
	r.LastRunAt = time.Time{}
	r.LastRunStatus = ""
	r.ArmedAt = time.Now()
	r.LastFiredAt = time.Time{}
	r.LastFireNote = ""
	r.WebhookTokenHash = ""
	if err = r.ensureWebhookToken(); err != nil {
		return err
	}
	if _, err = s.Insert(r); err != nil {
		return err
	}
	if r.Enabled {
		if err = seedAutomationDueMarkers(s, r, r.ArmedAt); err != nil {
			return err
		}
	}
	r.fillComputed()
	invalidateAutomationRules(s, r.ProjectID)
	return nil
}

// Update replaces the editable fields of a rule. The user who saves becomes the owner.
// @Summary Update an automation rule
func (r *AutomationRule) Update(s *xorm.Session, a web.Auth) error {
	ownerID, err := ownerOf(a)
	if err != nil {
		return err
	}
	existing, err := getAutomationRuleInProject(s, r.ID, r.ProjectID)
	if err != nil {
		return err
	}
	r.checkedSources = map[string]bool{}
	for _, sc := range spec.Scripts(existing.Doc()) {
		r.checkedSources[sc.Source] = true
	}
	if err = r.Validate(s, a); err != nil {
		return err
	}
	r.OwnerID = ownerID
	r.Revision = existing.Revision + 1
	r.WebhookTokenHash = existing.WebhookTokenHash
	// Only a token generated by this very save is shown.
	r.WebhookToken, r.WebhookURL = "", ""
	if err = r.ensureWebhookToken(); err != nil {
		return err
	}
	// Saving by hand clears what the system stored; enabling starts with a clean failure count.
	r.DisabledReason = ""
	r.ConsecutiveFailures = 0
	if !r.Enabled && !existing.Enabled {
		r.ConsecutiveFailures = existing.ConsecutiveFailures
	}
	r.ArmedAt = existing.ArmedAt
	rearmed := (r.Enabled && !existing.Enabled) || !sameTrigger(r.Trigger, existing.Trigger)
	if rearmed {
		r.ArmedAt = time.Now()
	}
	if _, err = s.ID(r.ID).
		Cols("title", "description", "enabled", "revision", "owner_id", "trigger", "conditions", "actions", "disabled_reason", "consecutive_failures", "webhook_token_hash", "armed_at").
		Update(r); err != nil {
		return err
	}
	if rearmed && r.Enabled {
		if err = seedAutomationDueMarkers(s, r, r.ArmedAt); err != nil {
			return err
		}
	}
	newToken, newURL := r.WebhookToken, r.WebhookURL
	stored, err := getAutomationRuleInProject(s, r.ID, r.ProjectID)
	if err != nil {
		return err
	}
	*r = *stored
	r.WebhookToken, r.WebhookURL = newToken, newURL
	r.fillComputed()
	invalidateAutomationRules(s, r.ProjectID)
	return nil
}

// Delete removes a rule with its runs.
// @Summary Delete an automation rule
func (r *AutomationRule) Delete(s *xorm.Session, _ web.Auth) error {
	runIDs := builder.Select("id").From("automation_runs").Where(builder.Eq{"rule_id": r.ID})
	if _, err := s.Where(builder.In("run_id", runIDs)).Delete(&AutomationRunStep{}); err != nil {
		return err
	}
	if _, err := s.Where("rule_id = ?", r.ID).Delete(&AutomationRun{}); err != nil {
		return err
	}
	if _, err := s.Where("rule_id = ?", r.ID).Delete(&AutomationDueMarker{}); err != nil {
		return err
	}
	if _, err := s.ID(r.ID).Delete(&AutomationRule{}); err != nil {
		return err
	}
	invalidateAutomationRules(s, r.ProjectID)
	return nil
}

// validateAutomationLimits checks what depends on the configuration or the database: the longest wait and the secret
// of an incoming webhook.
func validateAutomationLimits(s *xorm.Session, r *AutomationRule) ([]spec.Issue, error) {
	issues := []spec.Issue{}
	if maxWait := config.AutomationWaitMax.GetDuration(); maxWait > 0 {
		for i, act := range r.Actions {
			if act == nil || act.Type != spec.ActionWait {
				continue
			}
			text, _ := act.Params["duration"].(string)
			if d, err := spec.ParseOffset(text); err == nil && d > maxWait {
				issues = append(issues, spec.Issue{Path: fmt.Sprintf("actions[%d].params.duration", i), Message: fmt.Sprintf("is longer than the %s this server allows (automation.waitmax)", maxWait)})
			}
		}
	}
	if name, ok := r.Trigger.Options["secret"].(string); ok && name != "" && r.Trigger.Type == spec.TriggerWebhookIncoming {
		names, err := AutomationSecretNames(s, r.ProjectID)
		if err != nil {
			return nil, err
		}
		if !names[name] {
			issues = append(issues, spec.Issue{Path: "trigger.options.secret", Message: fmt.Sprintf("the secret %q does not exist in this project", name)})
		}
	}
	return issues, nil
}

// sameTrigger compares two triggers by what is stored of them.
func sameTrigger(a, b spec.Trigger) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}
