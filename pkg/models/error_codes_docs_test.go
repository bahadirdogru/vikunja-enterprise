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
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"code.vikunja.io/api/pkg/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The error codes of the fork's modules (20001 - 25011) as the API answers them. API.md lists the ones a client needs
// with their HTTP status; this test holds the code, the status and the range of every error to that table, and fails
// when a code is added to the source without being listed here.
func TestForkErrorCodesMatchTheDocumentation(t *testing.T) {
	errs := map[int]web.HTTPErrorProcessor{
		20001: ErrCustomFieldDoesNotExist{}, 20002: ErrCustomFieldInvalidDefinition{}, 20003: ErrCustomFieldTypeImmutable{},
		20004: ErrCustomFieldOptionDoesNotExist{}, 20005: ErrCustomFieldInvalidValue{}, 20006: ErrCustomFieldLimitReached{},
		20007: ErrCustomFieldOptionLimitReached{}, 20008: ErrCustomFieldValueTooLong{},

		21001: ErrWorkflowDoesNotExist{}, 21002: ErrWorkflowInvalidDefinition{}, 21003: ErrWorkflowStatusDoesNotExist{},
		21004: ErrWorkflowStatusArchived{}, 21005: ErrWorkflowTransitionNotAllowed{}, 21006: ErrWorkflowTransitionForbidden{},
		21007: ErrWorkflowRequirementsNotMet{}, 21008: ErrWorkflowMoveTasksRequired{}, 21009: ErrWorkflowRequiredForStatusView{},
		21010: ErrStatusViewBucketsManaged{}, 21011: ErrWorkflowStatusMoveTooLarge{},

		22001: ErrAutomationRuleDoesNotExist{}, 22002: ErrAutomationRuleInvalid{}, 22003: ErrAutomationRunDoesNotExist{},
		22004: ErrAutomationRunNotRetryable{}, 22005: ErrAutomationScriptsDisabled{}, 22006: ErrAutomationSecretInvalid{},
		22007: ErrAutomationSecretDoesNotExist{}, 22008: ErrAutomationSecretUnreadable{}, 22009: ErrAutomationRuleNotRunnable{},
		22010: ErrAutomationHookNotFound{}, 22011: ErrAutomationHookSignatureInvalid{}, 22012: ErrAutomationHookRateLimited{},
		22013: ErrAutomationHookPayloadInvalid{}, 22014: ErrAutomationRuleLimitReached{}, 22015: ErrAutomationRunEventGone{}, 22016: ErrAutomationTestNeedsTask{},

		23001: ErrReportInvalidParameter{}, 23002: ErrReportTooManyBuckets{}, 23003: ErrReportTooManyGroups{},
		23004: ErrReportFieldKindNotSupported{}, 23005: ErrReportNotAvailableForShare{}, 23006: ErrDashboardDoesNotExist{},
		23007: ErrDashboardWidgetDoesNotExist{}, 23008: ErrDashboardInvalid{}, 23009: ErrReportTimeout{},
		23010: ErrReportTooManyRows{}, 23011: ErrReportTooLarge{},

		24001: ErrApprovalRequestDoesNotExist{}, 24002: ErrApprovalNotPending{}, 24003: ErrApprovalNotApprover{},
		24004: ErrApprovalAlreadyDecided{}, 24005: ErrApprovalCommentRequired{}, 24006: ErrApprovalNoApprovers{},
		24007: ErrApprovalRequired{}, 24008: ErrApprovalSelfApproval{}, 24009: ErrApprovalInvalid{}, 24010: ErrApprovalLinkShare{},

		25001: ErrFormDoesNotExist{}, 25002: ErrFormInvalid{}, 25003: ErrFormUnavailable{}, 25004: ErrFormLoginRequired{},
		25005: ErrFormNotAllowed{}, 25006: ErrFormSubmissionInvalid{}, 25007: ErrFormRateLimited{}, 25008: ErrFormsDisabled{},
		25009: ErrFormLogoInvalid{}, 25010: ErrFormDataInvalid{}, 25011: ErrFormDataTooLarge{},
	}

	t.Run("every error answers with the code it is listed under, in the range of its module", func(t *testing.T) {
		for code, err := range errs {
			h := err.HTTPError()
			assert.Equal(t, code, h.Code, "%T", err)
			assert.NotEmpty(t, h.Message, "%T needs a message", err)
			assert.GreaterOrEqual(t, h.HTTPCode, 400, "%T", err)
			assert.Less(t, h.HTTPCode, 600, "%T", err)
		}
	})

	t.Run("every code of the source is in the table", func(t *testing.T) {
		defined := regexp.MustCompile(`(?m)^const ErrCode\w+ = (2[0-5]\d{3})$`)
		files, err := filepath.Glob("error_*.go")
		require.NoError(t, err)
		found := 0
		for _, f := range files {
			src, err := os.ReadFile(f)
			require.NoError(t, err)
			for _, m := range defined.FindAllStringSubmatch(string(src), -1) {
				code, err := strconv.Atoi(m[1])
				require.NoError(t, err)
				found++
				assert.Contains(t, errs, code, "%s defines %d, which the table above does not list", f, code)
			}
		}
		assert.Equal(t, len(errs), found, "the table and the source define the same number of codes")
	})

	t.Run("the HTTP status is the one API.md documents", func(t *testing.T) {
		api, err := os.ReadFile(filepath.Join("..", "..", "API.md"))
		require.NoError(t, err)
		row := regexp.MustCompile(`(?m)^\| (2[0-5]\d{3})(?: / (2[0-5]\d{3}))? \| (\d{3}) \|`)
		rows := row.FindAllStringSubmatch(string(api), -1)
		require.GreaterOrEqual(t, len(rows), 30, "the documented codes were found")
		for _, m := range rows {
			status, err := strconv.Atoi(m[3])
			require.NoError(t, err)
			for _, c := range m[1:3] {
				if c == "" {
					continue
				}
				code, err := strconv.Atoi(c)
				require.NoError(t, err)
				e, ok := errs[code]
				require.True(t, ok, "API.md documents %d, which no error has", code)
				assert.Equal(t, status, e.HTTPError().HTTPCode, "API.md documents %d as %d", code, status)
			}
		}
	})
}
