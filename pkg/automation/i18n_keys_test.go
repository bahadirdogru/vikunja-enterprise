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
	"os"
	"path/filepath"
	"testing"

	"code.vikunja.io/api/pkg/automation/spec"
	"code.vikunja.io/api/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor looks every title key, enum value, skip reason and disabled reason that the server can send up in
// frontend/src/i18n/lang/{en,tr-TR}.json. `mage check:translations` cannot see these references (the server names
// the keys at run time and the frontend builds the lookups from them), which is why it reports them as dead keys, so
// this test is the check that the files hold what the server can send.
func flattenI18n(t *testing.T, file string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "i18n", "lang", file))
	require.NoError(t, err)
	var tree map[string]any
	require.NoError(t, json.Unmarshal(data, &tree))
	flat := map[string]bool{}
	var walk func(prefix string, node map[string]any)
	walk = func(prefix string, node map[string]any) {
		for k, v := range node {
			switch vv := v.(type) {
			case string:
				flat[prefix+k] = vv != ""
			case map[string]any:
				walk(prefix+k+".", vv)
			}
		}
	}
	walk("", tree)
	return flat
}

func TestEveryKeyTheServerNamesIsTranslated(t *testing.T) {
	catalog := spec.GetCatalog()

	titleKeys := map[string]bool{}
	// enumKeys holds the candidates for every enum value: the key by option wins, the flat key serves the rest.
	type enumUse struct{ option, value string }
	enums := map[enumUse]bool{}
	for _, tr := range catalog.Triggers {
		titleKeys[tr.TitleKey] = true
		for _, o := range tr.Options {
			titleKeys[o.TitleKey] = true
			for _, v := range o.Enum {
				enums[enumUse{o.Name, v}] = true
			}
		}
	}
	for _, ac := range catalog.Actions {
		titleKeys[ac.TitleKey] = true
		for _, p := range ac.Params {
			titleKeys[p.TitleKey] = true
			for _, v := range p.Enum {
				enums[enumUse{p.Name, v}] = true
			}
		}
	}
	for _, f := range catalog.Fields {
		titleKeys[f.TitleKey] = true
		for _, v := range f.Enum {
			enums[enumUse{"", v}] = true
		}
	}
	for _, op := range catalog.Operators {
		titleKeys[op.TitleKey] = true
	}
	require.NotEmpty(t, titleKeys)

	skipReasons := []string{SkipMaxDepth, SkipSelfTrigger, SkipRateLimit, SkipConditions, SkipTaskNotFound,
		SkipRuleDisabled, SkipOwnerNotAdmin, SkipOwnerUnavilable, SkipTooManyActions, SkipRuleChanged}
	disabledReasons := []string{models.AutomationDisabledOwnerNotAdmin, models.AutomationDisabledOwnerUnavailable,
		models.AutomationDisabledTooManyFailures, models.AutomationDisabledCustomFieldDeleted,
		models.AutomationDisabledStatusDeleted, models.AutomationDisabledDuplicated,
		models.AutomationDisabledUnmappedReference, models.AutomationDisabledSecretsMissing}

	for _, file := range []string{"en.json", "tr-TR.json"} {
		t.Run(file, func(t *testing.T) {
			keys := flattenI18n(t, file)

			for key := range titleKeys {
				assert.True(t, keys[key], "%s has no text for the catalog key %s", file, key)
			}
			for use := range enums {
				flat := "automation.enum." + use.value
				byOption := "automation.enum." + use.option + "." + use.value
				assert.True(t, keys[flat] || keys[byOption], "%s has no label for the value %q of %q (looked for %s and %s)",
					file, use.value, use.option, byOption, flat)
			}
			for _, r := range skipReasons {
				assert.True(t, keys["automation.skipReason."+r], "%s has no text for the skip reason %s", file, r)
			}
			for _, r := range disabledReasons {
				assert.True(t, keys["automation.disabledReason."+r], "%s has no text for the disabled reason %s", file, r)
			}
		})
	}

	// The outcome of an approval has its own word for "any": the flat label of that value is "Any one approver".
	for _, file := range []string{"en.json", "tr-TR.json"} {
		assert.True(t, flattenI18n(t, file)["automation.enum.outcome."+spec.OutcomeAny], file)
	}
}
