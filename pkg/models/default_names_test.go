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
	"context"
	"strings"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm/contexts"
)

func kanbanBucketTitlesOfNewProjectOf(t *testing.T, u *user.User) []string {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	project := &Project{Title: "default names"}
	require.NoError(t, project.Create(s, u))
	require.NoError(t, s.Commit())

	s = db.NewSession()
	defer s.Close()
	var buckets []*Bucket
	require.NoError(t, s.Join("INNER", "project_views", "project_views.id = buckets.project_view_id").
		Where("project_views.project_id = ?", project.ID).OrderBy("buckets.position").Find(&buckets))
	titles := make([]string, 0, len(buckets))
	for _, b := range buckets {
		titles = append(titles, b.Title)
	}
	return titles
}

func TestNewKanbanColumnsAreNamedInTheLanguageOfTheCreator(t *testing.T) {
	t.Run("a Turkish user gets Turkish columns", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		_, err := s.ID(1).Cols("language").Update(&user.User{Language: "tr-TR"})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		// The authentication of a request has no language; it is read from the account.
		assert.Equal(t, []string{"Yapılacaklar", "Yapılıyor", "Tamamlandı"}, kanbanBucketTitlesOfNewProjectOf(t, &user.User{ID: 1, Username: "user1"}))
	})

	t.Run("a user without a language gets the English columns", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		assert.Equal(t, []string{"To-Do", "Doing", "Done"}, kanbanBucketTitlesOfNewProjectOf(t, &user.User{ID: 1, Username: "user1"}))
	})

	t.Run("a language without the names falls back to English", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		assert.Equal(t, "To-Do", defaultKanbanBucketTitle(s, &user.User{ID: 1, Language: "xx-XX"}, "kanban.default_buckets.todo"))
	})

	t.Run("every key has a name in both languages", func(t *testing.T) {
		for key, english := range defaultKanbanBucketKeys {
			assert.NotEqual(t, key, defaultKanbanBucketTitleIn("en", key))
			assert.Equal(t, english, defaultKanbanBucketTitleIn("en", key))
			assert.NotEqual(t, english, defaultKanbanBucketTitleIn("tr-TR", key), "tr-TR has no name for %s", key)
		}
	})
}

type userQueryCountKey struct{}

// userQueryCountHook counts the statements that read the users table of a session whose context carries a counter.
type userQueryCountHook struct{}

func (userQueryCountHook) BeforeProcess(c *contexts.ContextHook) (context.Context, error) {
	if n, ok := c.Ctx.Value(userQueryCountKey{}).(*int); ok && strings.Contains(c.SQL, "users") && strings.HasPrefix(strings.TrimSpace(c.SQL), "SELECT") {
		*n++
	}
	return c.Ctx, nil
}

func (userQueryCountHook) AfterProcess(c *contexts.ContextHook) error { return c.Err }

func TestTheLanguageOfTheCreatorIsLookedUpOncePerView(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	queries := 0
	s := db.NewSession().Context(context.WithValue(context.Background(), userQueryCountKey{}, &queries))
	defer s.Close()
	s.Engine().AddHook(userQueryCountHook{})
	view := &ProjectView{ProjectID: 1, Title: "Board", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeManual}
	require.NoError(t, view.Create(s, &user.User{ID: 1, Username: "user1"}))
	assert.LessOrEqual(t, queries, 1, "the three default columns need one lookup of the language, not three")
	assert.GreaterOrEqual(t, queries, 1, "the counter did not see the lookup: the test proves nothing")
}
