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

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"
)

// DryRunStep is what one action would do.
type DryRunStep struct {
	Position int64    `json:"position" doc:"The index of the action in the rule."`
	Type     string   `json:"type" doc:"The type of the action."`
	Summary  string   `json:"summary" doc:"What the action would do, in English. Empty when it would fail."`
	Warnings []string `json:"warnings" doc:"Problems that would not stop the action, e.g. a placeholder that renders empty, in English."`
	Error    string   `json:"error,omitempty" doc:"Why the action would fail right now, e.g. a workflow transition that is not allowed, in English."`
	Stops    bool     `json:"stops" doc:"Whether the run would end here (flow.stop_if)."`
	// The same sentences as codes with values, for a client that translates them. A client that does not know a
	// code shows the English text above.
	SummaryMessage  *models.AutomationMessage   `json:"summary_message,omitempty" doc:"The first line of summary as a code with values."`
	WarningMessages []*models.AutomationMessage `json:"warning_messages,omitempty" doc:"The warnings as codes with values, in the order of warnings."`
	ErrorCode       string                      `json:"error_code,omitempty" doc:"The code of error, see the error_code of a run."`
	ErrorParams     map[string]string           `json:"error_params,omitempty" doc:"The values the sentence of error_code mentions."`
}

// DryRunResult is the outcome of testing a rule against a task.
type DryRunResult struct {
	TaskID        int64                       `json:"task_id" doc:"The task the rule was tested with."`
	ConditionsMet bool                        `json:"conditions_met" doc:"Whether the conditions hold for the task as it is now. Conditions on event.* fields see no event in a test and are evaluated as empty."`
	Steps         []*DryRunStep               `json:"steps" doc:"The planned actions in order. Every action is planned against the current state of the task, so an action that depends on the result of an earlier one is judged as if the earlier one had not run."`
	Notes         []string                    `json:"notes" doc:"General remarks about the test, in English."`
	NoteMessages  []*models.AutomationMessage `json:"note_messages,omitempty" doc:"The notes as codes with values, in the order of notes."`
}

// DryRun evaluates the conditions of a rule against a task and plans every action without writing anything. The
// actions are planned as the owner of the rule. Planning only reads; the transaction is rolled back at the end as
// a second guard.
func DryRun(ctx context.Context, rule *models.AutomationRule, taskID int64) (*DryRunResult, error) {
	return DryRunWith(ctx, rule, taskID, nil)
}

// DryRunWith is DryRun for every trigger: taskID is ignored when the trigger has no task (incoming webhook, schedule
// per project) and required otherwise; hook is the simulated body of an incoming webhook.
func DryRunWith(_ context.Context, rule *models.AutomationRule, taskID int64, hook map[string]any) (*DryRunResult, error) {
	s := db.NewSession()
	defer s.Close()
	defer func() { _ = s.Rollback() }()

	hasTask := spec.TriggerHasTask(rule.Trigger)
	task := models.Task{}
	if hasTask {
		var err error
		task, err = models.GetTaskByIDSimple(s, taskID)
		if err != nil {
			return nil, err
		}
		if task.ProjectID != rule.ProjectID {
			return nil, models.ErrTaskDoesNotExist{ID: taskID}
		}
	}

	owner, err := user.GetUserByID(s, rule.OwnerID)
	if err != nil {
		return nil, err
	}

	def := spec.TriggerByType(rule.Trigger.Type)
	name := ""
	if def != nil {
		name = def.Event
	}
	payload := &Payload{
		Name:      name,
		TaskID:    task.ID,
		ProjectID: rule.ProjectID,
		Doer:      personOf(owner),
		Hook:      hook,
	}
	if hasTask {
		payload.Task = taskFields(&task)
	}
	env := &Env{S: s, Auth: owner, Rule: rule, Event: payload, Now: now()}
	if err = env.Reload(); err != nil {
		return nil, err
	}

	result := &DryRunResult{
		TaskID: task.ID,
		Steps:  []*DryRunStep{},
		Notes: []string{
			"The event is simulated: you are its doer and it carries no old or new values.",
			"Nothing was written.",
		},
		NoteMessages: []*models.AutomationMessage{say("note_simulated", nil), say("note_nothing_written", nil)},
	}
	result.ConditionsMet, err = Evaluate(rule.Conditions, env.Sub)
	if err != nil {
		var actionErr *ActionError
		if !errors.As(err, &actionErr) {
			return nil, err
		}
		// A script condition that fails would fail the run; say so instead of failing the test.
		result.ConditionsMet = false
		result.Notes = append(result.Notes, "The conditions fail: "+err.Error())
		code, params := errorMessage(err)
		result.NoteMessages = append(result.NoteMessages, say("note_conditions_fail", P{"code": code}.with(params)))
	} else if !result.ConditionsMet {
		result.Notes = append(result.Notes, "The conditions do not hold, so a real run would stop before the first action.")
		result.NoteMessages = append(result.NoteMessages, say("note_conditions_not_met", nil))
	}

	// After a create task action of a run without a task, the following actions work on a task that does not exist yet.
	virtualTask := false
	for i, act := range rule.Actions {
		step := &DryRunStep{Position: int64(i), Type: act.Type, Warnings: []string{}}
		result.Steps = append(result.Steps, step)
		impl := registry[act.Type]
		if impl == nil {
			step.Error = "unknown action"
			step.ErrorCode = "unknown_action"
			step.ErrorParams = map[string]string{"type": act.Type}
			continue
		}
		if virtualTask && spec.ActionNeedsTask(act) {
			step.Summary = "would act on the task created above (not simulated in a test)"
			step.SummaryMessage = say("on_created_task", nil)
			continue
		}
		if err = env.Reload(); err != nil {
			return nil, err
		}
		plan, err := impl.plan(env, params(act.Params))
		if err != nil {
			masked := env.maskErr(err)
			step.Error = env.maskOutput(err.Error())
			step.ErrorCode, step.ErrorParams = errorMessage(masked)
			if !isPermanent(err) {
				var actionErr *ActionError
				if !errors.As(err, &actionErr) {
					step.Error = env.maskOutput("could not be checked: " + err.Error())
					step.ErrorCode, step.ErrorParams = "could_not_check", boundParams(map[string]string{"message": env.maskOutput(err.Error())}, maxErrorParamsBytes)
				}
			}
			continue
		}
		step.Summary = plan.Summary
		if plan.Msg != nil {
			step.SummaryMessage = boundMessages(env.maskMessages([]*models.AutomationMessage{plan.Msg}))[0]
		}
		step.WarningMessages = boundMessages(env.maskMessages(plan.WarnMsgs))
		step.Stops = plan.Stop
		if act.Type == spec.ActionCreateTask && !hasTask {
			virtualTask = true
		}
		if plan.Warnings != nil {
			step.Warnings = plan.Warnings
		}
		if plan.preview != nil {
			// Only the dry run calls preview, which runs against a host that records instead of writing.
			summary, warnings, err := plan.preview()
			if err != nil {
				step.Summary = ""
				step.SummaryMessage = nil
				step.Error = env.maskOutput(err.Error())
				step.ErrorCode, step.ErrorParams = errorMessage(env.maskErr(err))
				continue
			}
			step.Summary = summary
			if len(plan.Lines) > 0 {
				step.SummaryMessage = boundMessages(env.maskMessages(plan.Lines[:1]))[0]
			}
			step.Warnings = append(step.Warnings, warnings...)
		}
	}
	result.NoteMessages = boundMessages(result.NoteMessages)
	return result, nil
}
