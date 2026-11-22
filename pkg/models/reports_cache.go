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
	"fmt"
	"sync"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"
)

// reportCacheSize bounds the cache. Entries are small (a report is at most a few hundred groups) and short lived.
const reportCacheSize = 256

type reportCacheEntry struct {
	report  *TaskReport
	expires time.Time
}

var reportCache = struct {
	sync.Mutex
	entries map[string]reportCacheEntry
}{entries: map[string]reportCacheEntry{}}

// ResetReportCache drops every cached report.
func ResetReportCache() {
	reportCache.Lock()
	defer reportCache.Unlock()
	reportCache.entries = map[string]reportCacheEntry{}
}

// reportCacheKey separates viewers: a report is the numbers of what one auth can read, so two auths never share an
// entry. The normalized request includes the time zone and the unresolved relative dates (now-90d).
func reportCacheKey(a web.Auth, plan *reportPlan) string {
	identity := fmt.Sprintf("%T:%d", a, a.GetID())
	switch a.(type) {
	case *user.User:
		identity = fmt.Sprintf("user:%d", a.GetID())
	case *LinkSharing:
		identity = fmt.Sprintf("share:%d", a.GetID())
	}
	req, _ := json.Marshal(plan.req)
	return identity + "|" + plan.loc.String() + "|" + string(req)
}

func reportCacheGet(key string) (*TaskReport, bool) {
	if config.ReportsCacheTTL.GetDuration() <= 0 {
		return nil, false
	}
	reportCache.Lock()
	defer reportCache.Unlock()
	e, ok := reportCache.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expires) {
		delete(reportCache.entries, key)
		return nil, false
	}
	return e.report, true
}

func reportCachePut(key string, rep *TaskReport) {
	ttl := config.ReportsCacheTTL.GetDuration()
	if ttl <= 0 {
		return
	}
	now := time.Now()
	reportCache.Lock()
	defer reportCache.Unlock()

	if len(reportCache.entries) >= reportCacheSize {
		oldestKey := ""
		var oldest time.Time
		for k, e := range reportCache.entries {
			if now.After(e.expires) {
				delete(reportCache.entries, k)
				continue
			}
			if oldestKey == "" || e.expires.Before(oldest) {
				oldestKey, oldest = k, e.expires
			}
		}
		if len(reportCache.entries) >= reportCacheSize && oldestKey != "" {
			delete(reportCache.entries, oldestKey)
		}
	}
	reportCache.entries[key] = reportCacheEntry{report: rep, expires: now.Add(ttl)}
}
