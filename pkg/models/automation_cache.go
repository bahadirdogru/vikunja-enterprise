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
	"sync"
	"time"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/events"

	"xorm.io/xorm"
)

type automationCacheEntry struct {
	events  map[string]bool
	expires time.Time
}

var (
	automationCacheMu sync.Mutex
	automationCache   = map[int64]automationCacheEntry{}
)

// AutomationSubscribedEvents returns the names of the internal events some enabled rule of the project listens to.
// The set is cached in memory for automation.cachettl. Changes of rules made through this process invalidate it at
// once (when the changing transaction commits); changes made by other replicas are noticed after at most the TTL.
func AutomationSubscribedEvents(s *xorm.Session, projectID int64) (map[string]bool, error) {
	ttl := config.AutomationCacheTTL.GetDuration()
	if ttl > 0 {
		automationCacheMu.Lock()
		entry, has := automationCache[projectID]
		automationCacheMu.Unlock()
		if has && time.Now().Before(entry.expires) {
			return entry.events, nil
		}
	}

	rules := []*AutomationRule{}
	if err := s.Where("project_id = ? AND enabled = ?", projectID, true).Cols("id", "trigger").Find(&rules); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, rule := range rules {
		if def := spec.TriggerByType(rule.Trigger.Type); def != nil {
			names[def.Event] = true
		}
	}

	if ttl > 0 {
		automationCacheMu.Lock()
		automationCache[projectID] = automationCacheEntry{events: names, expires: time.Now().Add(ttl)}
		automationCacheMu.Unlock()
	}
	return names, nil
}

// ResetAutomationCache forgets all cached subscriptions.
func ResetAutomationCache() {
	automationCacheMu.Lock()
	automationCache = map[int64]automationCacheEntry{}
	automationCacheMu.Unlock()
}

func forgetAutomationProject(projectID int64) {
	automationCacheMu.Lock()
	delete(automationCache, projectID)
	automationCacheMu.Unlock()
}

// invalidateAutomationRules drops the cached subscriptions of the project now and again after the transaction
// of s committed, so a concurrent reader cannot re-cache the state from before the commit.
func invalidateAutomationRules(s *xorm.Session, projectID int64) {
	forgetAutomationProject(projectID)
	events.OnCommit(s, func() error {
		forgetAutomationProject(projectID)
		return nil
	})
}
