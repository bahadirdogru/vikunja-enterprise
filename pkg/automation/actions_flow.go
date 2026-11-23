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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/notifications"
	"code.vikunja.io/api/pkg/user"

	"xorm.io/builder"
)

// maxNotifyRecipients bounds the people one notify step reaches (a team can be large).
const maxNotifyRecipients = 100

// waitOutput starts the output of a wait step; the rest is the time the run continues at (RFC 3339).
const waitOutput = models.AutomationWaitOutput

// planCreateStandaloneTask creates a task in the project of the rule. When the run has no task, the new task is the
// task of the actions that follow.
func planCreateStandaloneTask(env *Env, p params) (*Plan, error) {
	plan := &Plan{}
	unknownBefore := len(env.Unknown)
	title := strings.TrimSpace(env.render(p.str("title")))
	description := env.render(p.str("description"))
	renderedWarning(env, plan, unknownBefore)
	if title == "" {
		return nil, actionErrorf("the title would be empty")
	}

	created := &models.Task{Title: title, Description: description, ProjectID: env.Rule.ProjectID}
	if p.has("priority") {
		f, ok := spec.ToFloat64(p["priority"])
		if !ok || f < 0 || f > 5 || f != float64(int64(f)) {
			return nil, actionErrorf("the priority must be a whole number from 0 to 5")
		}
		created.Priority = int64(f)
	}
	if p.has("due_offset") {
		offset, err := spec.ParseOffset(p.str("due_offset"))
		if err != nil {
			return nil, actionErrorf("%s", err.Error())
		}
		created.DueDate = env.Now.Add(offset)
	}
	if p.has("assignee") {
		id, err := resolveUserParam(env, p["assignee"])
		if err != nil {
			return nil, err
		}
		assignee, err := user.GetUserByID(env.S, id)
		if err != nil {
			return nil, err
		}
		created.Assignees = []*user.User{assignee}
	}
	can, err := created.CanCreate(env.S, env.Auth)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, models.ErrGenericForbidden{}
	}

	plan.Summary = fmt.Sprintf("create the task %q", title)
	plan.apply = func() (string, error) {
		if err := created.Create(env.S, env.Auth); err != nil {
			return "", err
		}
		if err := recordTaskOrigin(env.S, created.ID); err != nil {
			return "", err
		}
		if env.Sub == nil || env.Sub.Task == nil {
			plan.CreatedTaskID = created.ID
		}
		return fmt.Sprintf("%s (task %d)", plan.Summary, created.ID), nil
	}
	return plan, nil
}

// planWait pauses the run: the actions after it are continued by a resume event that is due later. Nothing sleeps.
func planWait(env *Env, p params) (*Plan, error) {
	d, err := spec.ParseOffset(p.str("duration"))
	if err != nil {
		return nil, actionErrorf("%s", err.Error())
	}
	if d < spec.MinWait {
		return nil, actionErrorf("the wait must be at least 1m")
	}
	if maxWait := config.AutomationWaitMax.GetDuration(); maxWait > 0 && d > maxWait {
		return nil, actionErrorf("the wait is longer than the %s this server allows", maxWait)
	}
	resumeAt := env.Now.Add(d)
	plan := &Plan{
		Summary: fmt.Sprintf("would wait %s (until %s), then continue with the next actions", p.str("duration"), resumeAt.UTC().Format(time.RFC3339)),
		Wait:    d,
	}
	plan.apply = func() (string, error) {
		if env.RunID == 0 || env.Event == nil {
			return "", actionErrorf("a wait needs a run")
		}
		// Every waiting run has a resume event that is not done yet: they are what a project may not pile up.
		limit := int64(config.AutomationMaxPendingWaits.GetInt())
		waiting, err := env.S.Where(builder.And(
			builder.Eq{"project_id": env.Rule.ProjectID, "name": spec.EventResume},
			builder.In("status", models.AutomationEventPending, models.AutomationEventProcessing),
		)).Count(&models.AutomationEvent{})
		if err != nil {
			return "", err
		}
		if limit > 0 && waiting >= limit {
			return "", actionErrorf("%d runs of this project are waiting already, which is the most this server allows (automation.maxpendingwaits)", waiting)
		}
		payload := *env.Event
		payload.RuleID = env.Rule.ID
		payload.RunID = env.RunID
		raw, err := json.Marshal(&payload)
		if err != nil {
			return "", err
		}
		taskID := env.Event.TaskID
		row := &models.AutomationEvent{
			ProjectID: env.Rule.ProjectID,
			TaskID:    taskID,
			Name:      spec.EventResume,
			Payload:   string(raw),
			Status:    models.AutomationEventPending,
			RunAfter:  resumeAt,
			RuleID:    env.Rule.ID,
		}
		if _, err := env.S.Insert(row); err != nil {
			return "", err
		}
		return waitOutput + resumeAt.UTC().Format(time.RFC3339), nil
	}
	return plan, nil
}

// planNotify sends an in-app notification to the recipients that can read the project.
func planNotify(env *Env, p params) (*Plan, error) {
	plan := &Plan{}
	unknownBefore := len(env.Unknown)
	message := strings.TrimSpace(env.render(p.str("message")))
	renderedWarning(env, plan, unknownBefore)
	if message == "" {
		return nil, actionErrorf("the message would be empty")
	}
	if len(message) > 1000 {
		message = message[:1000]
	}

	ids := []int64{}
	add := func(id int64) {
		if id > 0 && !containsInt(ids, id) {
			ids = append(ids, id)
		}
	}
	list, _ := p["recipients"].([]any)
	for _, item := range list {
		if s, ok := item.(string); ok && s == spec.UserAssignees {
			assignees, _ := env.Sub.Fields["assignees"].([]any)
			for _, a := range assignees {
				if id, ok := a.(float64); ok {
					add(int64(id))
				}
			}
			continue
		}
		id, err := resolveUserParam(env, item)
		if err != nil {
			return nil, err
		}
		add(id)
	}
	if p.has("team_id") {
		teamID := p.int("team_id")
		team := &models.Team{ID: teamID}
		can, _, err := team.CanRead(env.S, env.Auth)
		if err != nil {
			return nil, err
		}
		if !can {
			return nil, actionErrorf("the team does not exist or the owner of the rule cannot see it")
		}
		members := []int64{}
		if err := env.S.Table("team_members").Where("team_id = ?", teamID).OrderBy("id ASC").Cols("user_id").Find(&members); err != nil {
			return nil, err
		}
		for _, id := range members {
			add(id)
		}
	}

	project, err := models.GetProjectSimpleByID(env.S, env.Rule.ProjectID)
	if err != nil {
		return nil, err
	}
	var task *models.Task
	if env.Sub != nil && env.Sub.Task != nil {
		task = env.Sub.Task
	}

	if len(ids) > maxNotifyRecipients {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d recipients, only the first %d are notified", len(ids), maxNotifyRecipients))
		ids = ids[:maxNotifyRecipients]
	}
	targets := []*user.User{}
	skipped := 0
	for _, id := range ids {
		target, err := user.GetUserWithEmail(env.S, &user.User{ID: id})
		if err != nil {
			if user.IsErrUserDoesNotExist(err) || user.IsErrUserStatusError(err) {
				skipped++
				continue
			}
			return nil, err
		}
		canRead, _, err := (&models.Project{ID: project.ID}).CanRead(env.S, target)
		if err != nil {
			return nil, err
		}
		if !canRead {
			skipped++
			continue
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, actionErrorf("nobody to notify: the recipients are empty, unknown or cannot read the project")
	}

	plan.Summary = fmt.Sprintf("notify %d user(s): %q", len(targets), message)
	if skipped > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d recipient(s) skipped: unknown or without access to the project", skipped))
	}
	plan.apply = func() (string, error) {
		// The notification is persisted in the transaction of the step. Its mail is queued by the AfterInsert of the
		// row, which xorm runs only when the transaction commits: a step that fails at the 50th recipient rolls
		// back and sends nothing, so the retry does not send twice.
		for _, target := range targets {
			n := &models.AutomationNotifyNotification{
				Message:   message,
				RuleID:    env.Rule.ID,
				RuleTitle: env.Rule.Title,
				Task:      task,
				Project:   project,
				Target:    target,
			}
			if err := notifications.Notify(target, n, env.S); err != nil {
				return "", err
			}
		}
		return plan.Summary, nil
	}
	return plan, nil
}
