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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The interface turns the code of an API error into a sentence (error.<code> in the language files of the frontend).
// A code without a sentence shows the English message of the server, so every code the server can send has one in
// both files, and the Turkish one is not the English one.
// codeConstant matches every constant that names an error code, whatever its prefix (ErrCode..., ErrorCode...) and
// whatever its integer type.
var codeConstant = regexp.MustCompile(`(?m)^\s*(?:const )?\w*Code\w*\s+(?:u?int\d*\s+)?=\s*([0-9]+)\s*(?://.*)?$`)

// scanErrorCodes finds the error codes defined in the Go files below root, test files excluded, by their number: a Go
// literal with a leading zero is octal, so ErrorCodeGenericForbidden = 0001 is the number 1, which is what the JSON
// carries and what the interface looks up (error.1).
func scanErrorCodes(t *testing.T, root string) map[string]string {
	t.Helper()
	var paths []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			paths = append(paths, path)
		}
		return nil
	}))
	codes := map[string]string{}
	for _, path := range paths {
		src, err := os.ReadFile(path) //nolint:gosec // the packages of this repository
		require.NoError(t, err)
		for _, m := range codeConstant.FindAllStringSubmatch(string(src), -1) {
			number, err := strconv.ParseInt(m[1], 0, 64)
			require.NoError(t, err, "%s: %s", path, m[1])
			codes[strconv.FormatInt(number, 10)] = path
		}
	}
	return codes
}

// The scan finds the codes it is meant to guard: both prefixes, every integer type, octal literals, a package deeper
// than any glob of fixed depth would reach.
func TestTheScanOfErrorCodesFindsEveryKindOfConstant(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", "d", "e")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	write := func(path, src string) {
		require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	}
	write(filepath.Join(dir, "a.go"), "package x\n\n// ErrorCodeGenericForbidden is 1.\nconst ErrorCodeGenericForbidden = 0001\n")
	write(filepath.Join(dir, "a", "b.go"), "package x\n\nconst (\n\tErrCodeUsernameReserved = 1026 // comment\n\tErrorCodeBig int64 = 4242\n\tErrCodeSmall uint16 = 77\n)\n")
	write(filepath.Join(deep, "deep.go"), "package x\n\nconst ErrCodeDeep = 99001\n")
	write(filepath.Join(deep, "ignored_test.go"), "package x\n\nconst ErrCodeFromATest = 55555\n")
	write(filepath.Join(deep, "not_a_code.go"), "package x\n\nconst maxCodeLength = 1 << 20\nconst codeLength int = 12 * 3\n")

	found := scanErrorCodes(t, dir)
	assert.Contains(t, found, "1", "ErrorCodeGenericForbidden = 0001 is the number 1")
	for _, code := range []string{"1026", "4242", "77", "99001"} {
		assert.Contains(t, found, code)
	}
	assert.NotContains(t, found, "55555")
	assert.NotContains(t, found, "1048576")
	assert.Len(t, found, 5)

	// What the first version of the guard (ErrCode only, a fixed depth) saw of the same tree.
	old := regexp.MustCompile(`(?m)^\s*(?:const )?ErrCode\w+\s+(?:int\s+)?=\s*([0-9]+)`)
	src, err := os.ReadFile(filepath.Join(dir, "a.go"))
	require.NoError(t, err)
	assert.Empty(t, old.FindAllString(string(src), -1), "the old pattern did not see ErrorCodeGenericForbidden")
}

// The interface turns the code of an API error into a sentence (error.<code> in the language files of the frontend).
// A code without a sentence shows the English message of the server, so every code the server can send has one in
// both files, and the Turkish one is not the English one.
func TestEveryErrorCodeOfTheServerHasATextInBothLanguages(t *testing.T) {
	codes := scanErrorCodes(t, "..")
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
