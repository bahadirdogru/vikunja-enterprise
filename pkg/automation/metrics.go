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
	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/log"
	vmetrics "code.vikunja.io/api/pkg/metrics"
	"code.vikunja.io/api/pkg/models"

	"github.com/prometheus/client_golang/prometheus"
)

// runsTotal counts finished runs by status. nil until metrics are enabled.
var runsTotal *prometheus.CounterVec

func countRun(status string) {
	if runsTotal != nil {
		runsTotal.WithLabelValues(status).Inc()
	}
}

// initMetrics registers the automation metrics when metrics are enabled: the number of finished runs by status and
// the queue depth by event status.
func initMetrics() {
	if !config.MetricsEnabled.GetBool() || runsTotal != nil {
		return
	}
	registry := vmetrics.GetRegistry()
	runsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vikunja_automation_runs_total",
		Help: "The number of finished automation runs by status, since the process started",
	}, []string{"status"})
	if err := registry.Register(runsTotal); err != nil {
		log.Criticalf("Could not register automation run metrics: %s", err)
	}

	for _, status := range []string{models.AutomationEventPending, models.AutomationEventProcessing, models.AutomationEventDead} {
		status := status
		err := registry.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "vikunja_automation_events",
			Help:        "The number of automation events in the outbox by status",
			ConstLabels: prometheus.Labels{"status": status},
		}, func() float64 {
			s := db.NewAutocommitSession()
			defer s.Close()
			n, err := s.Where("status = ?", status).Count(&models.AutomationEvent{})
			if err != nil {
				log.Errorf("[automation] could not count events: %v", err)
			}
			return float64(n)
		}))
		if err != nil {
			log.Criticalf("Could not register automation queue metrics: %s", err)
		}
	}
}
