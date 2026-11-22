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

package events

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

type hookTestEvent struct{ name string }

func (e *hookTestEvent) Name() string { return e.name }

func TestTransactionalHook(t *testing.T) {
	t.Cleanup(ResetTransactionalHooks)

	t.Run("runs synchronously with the session of the key", func(t *testing.T) {
		ResetTransactionalHooks()
		s := &xorm.Session{}
		var gotSession *xorm.Session
		var got []string
		RegisterTransactionalHook(func(hs *xorm.Session, e Event) error {
			gotSession = hs
			got = append(got, e.Name())
			return nil
		})

		DispatchOnCommit(s, &hookTestEvent{name: "a"})
		// Visible right after the call, before any commit or DispatchPending.
		assert.Equal(t, []string{"a"}, got)
		assert.Same(t, s, gotSession)
		assert.NoError(t, PendingHookError(s))
		CleanupPending(s)
	})

	t.Run("ignores keys that are not sessions", func(t *testing.T) {
		ResetTransactionalHooks()
		calls := 0
		RegisterTransactionalHook(func(*xorm.Session, Event) error {
			calls++
			return nil
		})
		DispatchOnCommit("not a session", &hookTestEvent{name: "a"})
		assert.Equal(t, 0, calls)
		CleanupPending("not a session")
	})

	t.Run("keeps hook errors for the commit check and still queues the event", func(t *testing.T) {
		ResetTransactionalHooks()
		s := &xorm.Session{}
		boom := errors.New("boom")
		RegisterTransactionalHook(func(*xorm.Session, Event) error { return boom })

		DispatchOnCommit(s, &hookTestEvent{name: "a"})
		require.ErrorIs(t, PendingHookError(s), boom)

		CleanupPending(s)
		assert.NoError(t, PendingHookError(s))
	})

	t.Run("a panicking hook neither panics the caller nor fails the transaction", func(t *testing.T) {
		ResetTransactionalHooks()
		s := &xorm.Session{}
		called := false
		RegisterTransactionalHook(func(*xorm.Session, Event) error { panic("bug") })
		RegisterTransactionalHook(func(*xorm.Session, Event) error {
			called = true
			return nil
		})

		assert.NotPanics(t, func() { DispatchOnCommit(s, &hookTestEvent{name: "a"}) })
		assert.NoError(t, PendingHookError(s))
		assert.True(t, called, "later hooks still run")
		CleanupPending(s)
	})
}
