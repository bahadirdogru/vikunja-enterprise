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
	"os"
	"path/filepath"
	"testing"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/i18n"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/user"
)

func TestMain(m *testing.M) {
	// The default in-memory SQLite uses shared-cache table locks, which fail concurrent workers with "table is
	// locked". A file in WAL mode behaves like a real installation, so use one unless a database is configured.
	tmpDir := ""
	if os.Getenv("VIKUNJA_TESTS_USE_CONFIG") != "1" {
		dir, err := os.MkdirTemp("", "vikunja-automation-*")
		if err != nil {
			panic(err)
		}
		tmpDir = dir
		os.Setenv("VIKUNJA_TESTS_USE_CONFIG", "1")
		os.Setenv("VIKUNJA_DATABASE_TYPE", "sqlite")
		os.Setenv("VIKUNJA_DATABASE_PATH", filepath.Join(dir, "vikunja.db"))
		if os.Getenv("VIKUNJA_SERVICE_PUBLICURL") == "" {
			os.Setenv("VIKUNJA_SERVICE_PUBLICURL", "http://localhost:3456/")
		}
	}

	log.InitLogger()
	config.InitDefaultConfig()
	i18n.Init()
	files.InitTests()
	user.InitTests()
	models.SetupTests()
	events.Fake()

	code := m.Run()
	if tmpDir != "" {
		_ = os.RemoveAll(tmpDir)
	}
	os.Exit(code)
}
