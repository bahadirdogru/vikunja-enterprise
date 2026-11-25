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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The interface turns the code of an API error into a sentence (error.<code> in the language files of the frontend).
// A code without a sentence shows the English message of the server, so every code the server can send has one in
// both files, and the Turkish one is not the English one.
func TestEveryErrorCodeOfTheServerHasATextInBothLanguages(t *testing.T) {
	definition := regexp.MustCompile(`(?m)^\s*(?:const )?ErrCode\w+\s+(?:int\s+)?=\s*([0-9]+)`)
	codes := map[string]string{}
	// The packages of pkg/ are at most four levels deep.
	for _, pattern := range []string{"../*.go", "../*/*.go", "../*/*/*.go", "../*/*/*/*.go"} {
		files, err := filepath.Glob(pattern)
		require.NoError(t, err)
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			for _, m := range definition.FindAllStringSubmatch(string(src), -1) {
				codes[m[1]] = path
			}
		}
	}
	require.Greater(t, len(codes), 200, "the scan found suspiciously few codes: did the definition change?")

	load := func(file string) map[string]string {
		raw, err := os.ReadFile("../../frontend/src/i18n/lang/" + file)
		require.NoError(t, err)
		var lang struct {
			Error map[string]json.RawMessage `json:"error"`
		}
		require.NoError(t, json.Unmarshal(raw, &lang))
		out := map[string]string{}
		for k, v := range lang.Error {
			var s string
			if json.Unmarshal(v, &s) == nil {
				out[k] = s
			}
		}
		return out
	}
	en, tr := load("en.json"), load("tr-TR.json")

	keys := make([]string, 0, len(codes))
	for c := range codes {
		keys = append(keys, c)
	}
	sort.Strings(keys)
	for _, code := range keys {
		assert.NotEmpty(t, en[code], "en.json has no text for the error code %s (defined in %s)", code, codes[code])
		assert.NotEmpty(t, tr[code], "tr-TR.json has no text for the error code %s (defined in %s)", code, codes[code])
		assert.NotEqual(t, en[code], tr[code], "tr-TR.json has the English text for the error code %s", code)
	}
}
