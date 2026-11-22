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
	"fmt"
	"sync"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/require"
)

// The test actions are not part of the catalog: rules using them are inserted without validation.
// The effects of the test actions are labels, so they are part of the step transaction like the
// effects of real actions. attempts counts executions in memory.
var (
	attempts   = map[string]int{}
	attemptsMu sync.Mutex
)

func resetCounters() {
	attemptsMu.Lock()
	defer attemptsMu.Unlock()
	attempts = map[string]int{}
}

func bump(key string) int {
	attemptsMu.Lock()
	defer attemptsMu.Unlock()
	attempts[key]++
	return attempts[key]
}

func attemptsOf(key string) int {
	attemptsMu.Lock()
	defer attemptsMu.Unlock()
	return attempts[key]
}

// counter is how often the effect with the key was committed.
func counter(key string) int {
	s := db.NewSession()
	defer s.Close()
	n, err := s.Where("title = ?", key).Count(&models.Label{})
	if err != nil {
		panic(err)
	}
	return int(n)
}

func applyEffect(env *Env, key string) error {
	return (&models.Label{Title: key}).Create(env.S, env.Auth)
}

func init() {
	// test.count leaves one comment with the key.
	register("test.count", func(env *Env, p params) (*Plan, error) {
		key := p.str("key")
		return &Plan{Summary: "count " + key, apply: func() (string, error) {
			return "counted " + key, applyEffect(env, key)
		}}, nil
	})
	// test.flaky fails with a temporary error the first n times it is applied.
	register("test.flaky", func(env *Env, p params) (*Plan, error) {
		key := p.str("key")
		return &Plan{Summary: "flaky " + key, apply: func() (string, error) {
			attempt := bump(key + ".attempts")
			if attempt <= int(p.int("fails")) {
				return "", fmt.Errorf("temporary failure %d", attempt)
			}
			return "worked", applyEffect(env, key)
		}}, nil
	})
	// test.reject fails for good.
	register("test.reject", func(env *Env, p params) (*Plan, error) {
		return &Plan{Summary: "reject", apply: func() (string, error) {
			bump("reject")
			return "", actionErrorf("rejected on purpose")
		}}, nil
	})
	// test.slow blocks until the step runs out of time.
	register("test.slow", func(env *Env, p params) (*Plan, error) {
		return &Plan{Summary: "slow", apply: func() (string, error) {
			select {
			case <-env.Ctx.Done():
				return "", env.Ctx.Err()
			case <-time.After(10 * time.Second):
				return "finished", nil
			}
		}}, nil
	})
}

func rawAct(typ string, kv ...any) *spec.Action {
	params := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		params[kv[i].(string)] = kv[i+1]
	}
	return &spec.Action{Type: typ, Params: params}
}

// insertRawRule stores a rule without validating it, so it can use the test actions.
func insertRawRule(t *testing.T, projectID int64, owner *user.User, trigger string, actions ...*spec.Action) *models.AutomationRule {
	t.Helper()
	return insertRawRuleWith(t, projectID, owner, trigger, nil, nil, actions...)
}

func insertRawRuleWith(t *testing.T, projectID int64, owner *user.User, trigger string, options map[string]any, cond *spec.Condition, actions ...*spec.Action) *models.AutomationRule {
	t.Helper()
	r := &models.AutomationRule{
		ProjectID:  projectID,
		Title:      "raw",
		Enabled:    true,
		Revision:   1,
		OwnerID:    owner.ID,
		Trigger:    spec.Trigger{Type: trigger, Options: options},
		Conditions: cond,
		Actions:    actions,
	}
	s := db.NewSession()
	defer s.Close()
	_, err := s.Insert(r)
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	models.ResetAutomationCache()
	return r
}
