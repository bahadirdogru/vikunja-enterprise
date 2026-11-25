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
	"code.vikunja.io/api/pkg/i18n"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

// The names the server gives to things it creates for a person (the columns of a new Kanban view) are stored once and
// shown as they are, so they are written in the language of the person who caused them. Names that already exist are
// the property of whoever owns them and are never rewritten.

// defaultKanbanBucketKeys are the keys of pkg/i18n/lang/*.json and the English names the columns have without a
// translation (a language file without the key, or the translations not loaded in a test).
var defaultKanbanBucketKeys = map[string]string{
	"kanban.default_buckets.todo":  "To-Do",
	"kanban.default_buckets.doing": "Doing",
	"kanban.default_buckets.done":  "Done",
}

// languageOf is the interface language of the person behind an authentication, empty when it has none (a link
// share) or it cannot be read.
func languageOf(s *xorm.Session, a web.Auth) string {
	u, ok := a.(*user.User)
	if !ok || u == nil {
		return ""
	}
	if u.Language != "" {
		return u.Language
	}
	full, err := user.GetUserByID(s, u.ID)
	if err != nil {
		return ""
	}
	return full.Language
}

// defaultKanbanBucketTitleIn is the name of a default column in the language of the person who creates the view.
func defaultKanbanBucketTitleIn(lang, key string) string {
	text := i18n.T(lang, key)
	if text == "" || text == key {
		return defaultKanbanBucketKeys[key]
	}
	return text
}
