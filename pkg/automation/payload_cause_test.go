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
	"testing"

	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribe_StatusChangeCarriesItsCause(t *testing.T) {
	task := &models.Task{ID: 5, ProjectID: 1}
	from := &models.WorkflowStatus{ID: 1, Category: models.WorkflowCategoryOpen}
	to := &models.WorkflowStatus{ID: 2, Category: models.WorkflowCategoryInProgress}

	p, ok := describe(&models.TaskStatusChangedEvent{Task: task, FromStatus: from, ToStatus: to, Cause: "status_removed"})
	require.True(t, ok)
	assert.Equal(t, "status_removed", p.New["cause"])
	assert.EqualValues(t, 2, p.New["status"])

	p, ok = describe(&models.TaskStatusChangedEvent{Task: task, FromStatus: from, ToStatus: to})
	require.True(t, ok)
	_, has := p.New["cause"]
	assert.False(t, has, "a change somebody made has no cause")
}
