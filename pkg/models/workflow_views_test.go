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
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
	"xorm.io/xorm/contexts"
)

// Fixture workflow of project 11 (see workflow_test.go): statuses 1 Todo (initial, open), 2 Doing (in_progress),
// 3 Review (in_progress), 4 Done (done), 5 Dropped (cancelled), 6 Legacy (archived). Transitions: 2 to 3,
// 3 to 4 (admin, comment) and 1 to 5. Project 1 has no workflow.
const (
	stTodo    int64 = 1
	stDoing   int64 = 2
	stReview  int64 = 3
	stDone    int64 = 4
	stDropped int64 = 5
	stLegacy  int64 = 6
)

// wfViewEnv is project 11 with tasks in several statuses:
//
//	t20 (fixture) Todo, a Todo, b Doing, c Done, d Review, e Dropped
type wfViewEnv struct {
	a, b, c, d, e int64
}

func wfCreateTask(t *testing.T, title string, to int64) int64 {
	t.Helper()
	task := createWorkflowTask(t, 11, &Task{Title: title, ProjectID: 11})
	if to != stTodo {
		_, err := changeTaskStatusAs(t, task.ID, to, "done", cfUser1)
		require.NoError(t, err)
	}
	return task.ID
}

func setupWFViewEnv(t *testing.T) *wfViewEnv {
	t.Helper()
	db.LoadAndAssertFixtures(t)
	return &wfViewEnv{
		a: wfCreateTask(t, "alpha", stTodo),
		b: wfCreateTask(t, "beta", stDoing),
		c: wfCreateTask(t, "gamma", stDone),
		d: wfCreateTask(t, "delta", stDoing),
		e: wfCreateTask(t, "epsilon", stDropped),
	}
}

func wfSetStatus(t *testing.T, taskID, status int64) {
	t.Helper()
	_, err := changeTaskStatusAs(t, taskID, status, "x", cfUser1)
	require.NoError(t, err)
}

func wfIDs(t *testing.T, a web.Auth, projectID int64, tc TaskCollection) []int64 {
	t.Helper()
	if len(tc.SortBy) == 0 {
		tc.SortBy = []string{"id"}
		tc.OrderBy = []string{"asc"}
	}
	ids, err := cfTaskIDsFor(a, projectID, tc)
	require.NoError(t, err, tc.Filter)
	return ids
}

func TestWorkflowFilter_Parsing(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	valid := []string{
		"status = 1", "status != 1", "status in 1,2", "status not in 1, 2",
		"status_category = open", "status_category != done", "status_category in open,in_progress",
		"status_category not in done,cancelled", "status = 1 && done = false", "(status = 1 || status_category = done) && priority > 2",
	}
	for _, f := range valid {
		_, err := getTaskFiltersFromFilterString(f, "")
		assert.NoError(t, err, f)
	}

	t.Run("invalid values", func(t *testing.T) {
		for _, f := range []string{"status = abc", "status = 1,2", "status in 1,x", "status_category = bogus", "status_category in open,bogus", "status = 1.5"} {
			_, err := getTaskFiltersFromFilterString(f, "")
			require.Error(t, err, f)
			assert.True(t, IsErrInvalidTaskFilterValue(err), "%s: %T", f, err)
		}
	})
	t.Run("invalid comparators", func(t *testing.T) {
		for _, f := range []string{"status > 1", "status >= 1", "status < 1", "status <= 1", "status like 1", "status_category like open", "status_category > open"} {
			_, err := getTaskFiltersFromFilterString(f, "")
			require.Error(t, err, f)
			_, is := err.(ErrInvalidTaskFilterComparator)
			assert.True(t, is, "%s: %T", f, err)
		}
	})
	t.Run("errors are 400 through a collection", func(t *testing.T) {
		_, err := cfTaskIDsFor(cfUser1, 11, TaskCollection{Filter: "status = abc"})
		require.Error(t, err)
		assert.True(t, isErrInvalidFilter(err) || IsErrInvalidTaskFilterValue(err))
		he, ok := err.(web.HTTPErrorProcessor)
		require.True(t, ok)
		assert.Equal(t, 400, he.HTTPError().HTTPCode)
	})
	t.Run("saved filters accept them", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		sf := &SavedFilter{Title: "by status", Filters: &TaskCollection{Filter: "status_category in open,in_progress && done = false"}}
		require.NoError(t, sf.Create(s, cfUser1))
		cond, _, err := parseFilterCond(s, cfUser1, sf.Filters.Filter, "", false)
		require.NoError(t, err)
		assert.NotNil(t, cond)
		require.NoError(t, s.Commit())

		// and evaluates them: task 20 is open, project 11 tasks of the env are spread over the statuses
		env := setupWFViewEnv(t)
		s2 := db.NewSession()
		defer s2.Close()
		sf2 := &SavedFilter{Title: "open ones", Filters: &TaskCollection{Filter: "status_category = open"}}
		require.NoError(t, sf2.Create(s2, cfUser1))
		require.NoError(t, s2.Commit())
		res, _, _, err := (&TaskCollection{ProjectID: getProjectIDFromSavedFilterID(sf2.ID), SortBy: []string{"id"}, OrderBy: []string{"asc"}}).ReadAll(s2, cfUser1, "", 1, 100)
		require.NoError(t, err)
		ids := []int64{}
		for _, task := range res.([]*Task) {
			ids = append(ids, task.ID)
		}
		assert.Equal(t, []int64{20, env.a}, ids)
	})
}

func TestWorkflowFilter_Semantics(t *testing.T) {
	env := setupWFViewEnv(t)
	q := func(filter string) []int64 { return wfIDs(t, cfUser1, 11, TaskCollection{Filter: filter}) }

	// statuses: t20 todo, a todo, b doing, c done, d doing, e dropped
	t.Run("status equals", func(t *testing.T) {
		assert.Equal(t, []int64{20, env.a}, q("status = 1"))
		assert.Equal(t, []int64{env.b, env.d}, q("status = 2"))
		assert.Equal(t, []int64{env.c}, q("status = 4"))
		assert.Empty(t, q("status = 3"))
		assert.Empty(t, q("status = 999"))
	})
	t.Run("status not equals", func(t *testing.T) {
		assert.Equal(t, []int64{env.b, env.c, env.d, env.e}, q("status != 1"))
	})
	t.Run("status in and not in", func(t *testing.T) {
		assert.Equal(t, []int64{env.b, env.c, env.d}, q("status in 2,4"))
		assert.Equal(t, []int64{20, env.a, env.e}, q("status not in 2,4"))
		assert.Equal(t, []int64{20, env.a, env.b, env.c, env.d, env.e}, q("status not in 3"))
	})
	t.Run("category equals, not equals, in, not in", func(t *testing.T) {
		assert.Equal(t, []int64{20, env.a}, q("status_category = open"))
		assert.Equal(t, []int64{env.b, env.d}, q("status_category = in_progress"))
		assert.Equal(t, []int64{env.c}, q("status_category = done"))
		assert.Equal(t, []int64{env.e}, q("status_category = cancelled"))
		assert.Equal(t, []int64{20, env.a, env.b, env.d}, q("status_category != done && status_category != cancelled"))
		assert.Equal(t, []int64{env.c, env.e}, q("status_category in done,cancelled"))
		assert.Equal(t, []int64{20, env.a, env.b, env.d}, q("status_category not in done,cancelled"))
	})
	t.Run("combined with built-in filters", func(t *testing.T) {
		setTaskColumns(t, env.b, &Task{Priority: 4}, "priority")
		setTaskColumns(t, env.d, &Task{Priority: 1}, "priority")
		assert.Equal(t, []int64{env.b}, q("status = 2 && priority >= 3"))
		assert.Equal(t, []int64{20, env.a, env.b}, q("status = 1 || (status = 2 && priority >= 3)"))
		assert.Equal(t, []int64{env.c, env.e}, q("status_category in done,cancelled && done = true"))
		assert.Equal(t, []int64{env.d}, q("status_category = in_progress && title like delta"))
	})
	t.Run("combined with custom field filters", func(t *testing.T) {
		field := createCustomField(t, &CustomField{ProjectID: 11, Title: "Cost", Type: CustomFieldTypeNumber})
		cfInsertValues(t,
			&TaskCustomFieldValue{TaskID: env.a, CustomFieldID: field.ID, ValueNumber: cfPtr(5.0)},
			&TaskCustomFieldValue{TaskID: env.b, CustomFieldID: field.ID, ValueNumber: cfPtr(50.0)},
			&TaskCustomFieldValue{TaskID: env.c, CustomFieldID: field.ID, ValueNumber: cfPtr(500.0)},
		)
		assert.Equal(t, []int64{env.b}, q(fmt.Sprintf("status = 2 && cf_%d > 10", field.ID)))
		assert.Equal(t, []int64{env.b, env.c}, q(fmt.Sprintf("status_category != open && cf_%d > 10", field.ID)))
		assert.Equal(t, []int64{env.a, env.b, env.d}, q(fmt.Sprintf("status = 2 || cf_%d = 5", field.ID)))
	})
	t.Run("search and page size", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		res, _, total, err := (&TaskCollection{ProjectID: 11, Filter: "status_category = in_progress", SortBy: []string{"id"}, OrderBy: []string{"asc"}}).ReadAll(s, cfUser1, "", 1, 1)
		require.NoError(t, err)
		assert.Len(t, res.([]*Task), 1)
		assert.EqualValues(t, 2, total)
	})
}

func TestWorkflowFilter_TasksWithoutStatus(t *testing.T) {
	env := setupWFViewEnv(t)
	// All projects of user 1: project 1 has no workflow and tasks 1 to 12 and more without a status row.
	all := func(filter string) []int64 {
		ids, err := cfTaskIDsFor(cfUser1, 0, TaskCollection{Filter: filter, SortBy: []string{"id"}, OrderBy: []string{"asc"}})
		require.NoError(t, err, filter)
		return ids
	}

	t.Run("positive comparators never match them", func(t *testing.T) {
		for _, f := range []string{"status = 1", "status in 1,2,3,4,5", "status_category = open", "status_category in open,done"} {
			got := all(f)
			assert.NotContains(t, got, int64(1), f)
			assert.NotContains(t, got, int64(2), f)
		}
	})
	t.Run("negative comparators always match them", func(t *testing.T) {
		for _, f := range []string{"status != 1", "status not in 1,2", "status_category != open", "status_category not in open,done"} {
			got := all(f)
			assert.Contains(t, got, int64(1), f)
			assert.Contains(t, got, int64(2), f)
		}
		got := all("status != 1")
		assert.NotContains(t, got, int64(20))
		assert.NotContains(t, got, env.a)
		assert.Contains(t, got, env.b)
	})
	t.Run("in a project without a workflow", func(t *testing.T) {
		assert.Empty(t, wfIDs(t, cfUser1, 1, TaskCollection{Filter: "status = 1"}))
		assert.NotEmpty(t, wfIDs(t, cfUser1, 1, TaskCollection{Filter: "status != 1"}))
	})
	t.Run("include nulls adds them to positive filters", func(t *testing.T) {
		ids, err := cfTaskIDsFor(cfUser1, 0, TaskCollection{Filter: "status = 4", FilterIncludeNulls: true, SortBy: []string{"id"}, OrderBy: []string{"asc"}})
		require.NoError(t, err)
		assert.Contains(t, ids, env.c)
		assert.Contains(t, ids, int64(1))
		assert.NotContains(t, ids, env.a)
	})
	t.Run("removing the workflow leaves no matches", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		wf := &Workflow{ProjectID: 11}
		require.NoError(t, wf.Delete(s, cfUser1))
		require.NoError(t, s.Commit())
		assert.Empty(t, wfIDs(t, cfUser1, 11, TaskCollection{Filter: "status = 1"}))
		assert.Len(t, wfIDs(t, cfUser1, 11, TaskCollection{Filter: "status != 1"}), 6)
	})
}

func TestWorkflowSort(t *testing.T) {
	env := setupWFViewEnv(t)
	// positions: todo 65536, doing 131072, review, done 262144, dropped 327680
	t.Run("ascending by status position, ties by the next sort", func(t *testing.T) {
		ids := wfIDs(t, cfUser1, 11, TaskCollection{SortBy: []string{"status", "id"}, OrderBy: []string{"asc", "desc"}})
		assert.Equal(t, []int64{env.a, 20, env.d, env.b, env.c, env.e}, ids)
	})
	t.Run("descending", func(t *testing.T) {
		ids := wfIDs(t, cfUser1, 11, TaskCollection{SortBy: []string{"status", "id"}, OrderBy: []string{"desc", "asc"}})
		assert.Equal(t, []int64{env.e, env.c, env.b, env.d, 20, env.a}, ids)
	})
	t.Run("tasks without a status are last in both directions", func(t *testing.T) {
		withStatus := map[int64]bool{20: true, env.a: true, env.b: true, env.c: true, env.d: true, env.e: true}
		for _, order := range []string{"asc", "desc"} {
			ids, err := cfTaskIDsFor(cfUser1, 0, TaskCollection{SortBy: []string{"status", "id"}, OrderBy: []string{order, "asc"}})
			require.NoError(t, err)
			lastWith, firstWithout := -1, -1
			for i, id := range ids {
				if withStatus[id] {
					lastWith = i
				} else if firstWithout < 0 {
					firstWithout = i
				}
			}
			require.Greater(t, firstWithout, 0, order)
			assert.Equal(t, 5, lastWith, order)
			assert.Less(t, lastWith, firstWithout, order)
		}
	})
	t.Run("sort and filter together", func(t *testing.T) {
		ids := wfIDs(t, cfUser1, 11, TaskCollection{Filter: "status_category != open", SortBy: []string{"status"}, OrderBy: []string{"desc"}})
		assert.Equal(t, []int64{env.e, env.c}, ids[:2])
	})
	t.Run("archived status keeps its position", func(t *testing.T) {
		s := db.NewSession()
		_, err := s.Where("task_id = ?", env.d).Cols("status_id").Update(&TaskStatus{StatusID: stReview})
		require.NoError(t, err)
		require.NoError(t, s.Commit())
		s.Close()
		ids := wfIDs(t, cfUser1, 11, TaskCollection{SortBy: []string{"status", "id"}, OrderBy: []string{"asc", "asc"}})
		assert.Equal(t, []int64{20, env.a, env.b, env.d, env.c, env.e}, ids)
	})
}

func TestWorkflowExpandStatus(t *testing.T) {
	env := setupWFViewEnv(t)

	t.Run("collection", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		tc := &TaskCollection{ProjectID: 0, Expand: []TaskCollectionExpandable{TaskCollectionExpandStatus}, SortBy: []string{"id"}, OrderBy: []string{"asc"}}
		res, _, _, err := tc.ReadAll(s, cfUser1, "", 1, 200)
		require.NoError(t, err)
		by := map[int64]*Task{}
		for _, task := range res.([]*Task) {
			by[task.ID] = task
		}
		require.Contains(t, by, env.c)
		require.NotNil(t, by[env.c].Status)
		assert.Equal(t, &TaskStatusSummary{ID: stDone, Title: "Done", Color: "#00aa00", Category: WorkflowCategoryDone}, by[env.c].Status)
		assert.Equal(t, stTodo, by[20].Status.ID)
		assert.Equal(t, WorkflowCategoryCancelled, by[env.e].Status.Category)
		assert.Nil(t, by[1].Status, "tasks of projects without a workflow have none")
	})
	t.Run("not attached without the expand", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		res, _, _, err := (&TaskCollection{ProjectID: 11}).ReadAll(s, cfUser1, "", 1, 50)
		require.NoError(t, err)
		for _, task := range res.([]*Task) {
			assert.Nil(t, task.Status)
		}
	})
	t.Run("single task", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		task := &Task{ID: env.b, Expand: []TaskCollectionExpandable{TaskCollectionExpandStatus}}
		require.NoError(t, task.ReadOne(s, cfUser1))
		require.NotNil(t, task.Status)
		assert.Equal(t, stDoing, task.Status.ID)
	})
	t.Run("fixed number of queries", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		count := func(ids []int64) int {
			taskMap := map[int64]*Task{}
			for _, id := range ids {
				taskMap[id] = &Task{ID: id}
			}
			before := dbReadCount(s)
			require.NoError(t, addStatusToTasks(s, ids, taskMap))
			return dbReadCount(s) - before
		}
		small := count([]int64{20})
		large := count([]int64{20, env.a, env.b, env.c, env.d, env.e})
		assert.Equal(t, small, large)
		assert.LessOrEqual(t, large, 2)
	})
	t.Run("validates as an expand option and scope", func(t *testing.T) {
		assert.NoError(t, TaskCollectionExpandStatus.Validate())
		group, perm, needs := requiredScopeForExpand("status")
		assert.True(t, needs)
		assert.Equal(t, "tasks", group)
		assert.Equal(t, "status", perm)
	})
}

// wfQueryCounter counts the SELECT statements of the test engine once wfCountHook is installed.
var (
	wfQueryCounter atomic.Int64
	wfHookOnce     sync.Once
)

type wfCountHook struct{}

func (wfCountHook) BeforeProcess(c *contexts.ContextHook) (context.Context, error) {
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(c.SQL)), "SELECT") {
		wfQueryCounter.Add(1)
	}
	return c.Ctx, nil
}

func (wfCountHook) AfterProcess(*contexts.ContextHook) error { return nil }

// dbReadCount returns the number of SELECT statements the engine of s executed since the hook was installed.
func dbReadCount(s *xorm.Session) int {
	wfHookOnce.Do(func() { s.Engine().AddHook(wfCountHook{}) })
	return int(wfQueryCounter.Load())
}

func wfGrantWrite(t *testing.T, userID int64) {
	t.Helper()
	insertRow(t, &ProjectUser{UserID: userID, ProjectID: 11, Permission: PermissionWrite})
}

func wfStatusView(t *testing.T, projectID int64) *ProjectView {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	v := &ProjectView{ProjectID: projectID, Title: "By status", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus}
	require.NoError(t, v.Create(s, cfUser1))
	require.NoError(t, s.Commit())
	return v
}

func wfColumns(t *testing.T, view *ProjectView, a web.Auth, page, perPage int, filter string) []*Bucket {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	tc := &TaskCollection{ProjectID: view.ProjectID, ProjectViewID: view.ID, Filter: filter}
	res, _, _, err := tc.ReadAll(s, a, "", page, perPage)
	require.NoError(t, err)
	buckets, ok := res.([]*Bucket)
	require.True(t, ok, "got %T", res)
	return buckets
}

func columnTaskIDs(b *Bucket) []int64 {
	ids := []int64{}
	for _, task := range b.Tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func wfMove(t *testing.T, view *ProjectView, taskID, statusID int64, a web.Auth) (*TaskBucket, error) {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	tb := &TaskBucket{BucketID: statusBucketID(statusID), TaskID: taskID, ProjectViewID: view.ID, ProjectID: view.ProjectID}
	can, err := tb.CanUpdate(s, a)
	if err != nil {
		_ = s.Rollback()
		return nil, err
	}
	if !can {
		_ = s.Rollback()
		return nil, ErrGenericForbidden{}
	}
	if err = tb.Update(s, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, err
	}
	require.NoError(t, s.Commit())
	return tb, nil
}

func wfPosition(t *testing.T, viewID, taskID int64) float64 {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	tp := &TaskPosition{}
	has, err := s.Where("task_id = ? AND project_view_id = ?", taskID, viewID).Get(tp)
	require.NoError(t, err)
	require.True(t, has, "task %d has no position in view %d", taskID, viewID)
	return tp.Position
}

func TestStatusKanban_Columns(t *testing.T) {
	env := setupWFViewEnv(t)
	view := wfStatusView(t, 11)

	t.Run("mode round trips", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		stored, err := GetProjectViewByIDAndProject(s, view.ID, 11)
		require.NoError(t, err)
		assert.Equal(t, BucketConfigurationModeStatus, stored.BucketConfigurationMode)
		b, err := stored.BucketConfigurationMode.MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, `"status"`, string(b))
		var m BucketConfigurationModeKind
		require.NoError(t, m.UnmarshalJSON([]byte(`"status"`)))
		assert.Equal(t, BucketConfigurationModeStatus, m)
	})
	t.Run("no buckets are stored", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		n, err := s.Where("project_view_id = ?", view.ID).Count(&Bucket{})
		require.NoError(t, err)
		assert.EqualValues(t, 0, n)
	})
	t.Run("columns are the non-archived statuses in position order", func(t *testing.T) {
		cols := wfColumns(t, view, cfUser1, 1, 50, "")
		titles := []string{}
		ids := []int64{}
		for _, c := range cols {
			titles = append(titles, c.Title)
			ids = append(ids, c.ID)
			assert.Equal(t, view.ID, c.ProjectViewID)
			require.NotNil(t, c.Status)
			assert.Equal(t, -c.ID, c.Status.ID)
		}
		assert.Equal(t, []string{"Todo", "Doing", "Review", "Done", "Dropped"}, titles)
		assert.Equal(t, []int64{-1, -2, -3, -4, -5}, ids)
	})
	t.Run("tasks are in the column of their status", func(t *testing.T) {
		cols := wfColumns(t, view, cfUser1, 1, 50, "")
		assert.ElementsMatch(t, []int64{20, env.a}, columnTaskIDs(cols[0]))
		assert.ElementsMatch(t, []int64{env.b, env.d}, columnTaskIDs(cols[1]))
		assert.Empty(t, cols[2].Tasks)
		assert.ElementsMatch(t, []int64{env.c}, columnTaskIDs(cols[3]))
		assert.ElementsMatch(t, []int64{env.e}, columnTaskIDs(cols[4]))
		assert.EqualValues(t, 2, cols[0].Count)
		assert.EqualValues(t, 0, cols[2].Count)
		for _, task := range cols[1].Tasks {
			assert.Equal(t, int64(-2), task.BucketID)
		}
	})
	t.Run("the bucket list endpoint returns the same columns", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		res, n, _, err := (&Bucket{ProjectID: 11, ProjectViewID: view.ID}).ReadAll(s, cfUser1, "", 1, 50)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, int64(-1), res.([]*Bucket)[0].ID)
	})
	t.Run("a view filter and the request filter narrow the tasks of every column", func(t *testing.T) {
		cols := wfColumns(t, view, cfUser1, 1, 50, "title like alpha || title like beta")
		assert.ElementsMatch(t, []int64{env.a}, columnTaskIDs(cols[0]))
		assert.ElementsMatch(t, []int64{env.b}, columnTaskIDs(cols[1]))
		assert.EqualValues(t, 1, cols[0].Count)
	})
	t.Run("a flat bucket_id filter loads one column", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		tc := &TaskCollection{ProjectID: 11, ProjectViewID: view.ID, Filter: "bucket_id = -2", SortBy: []string{"position"}, OrderBy: []string{"asc"}}
		res, _, total, err := tc.ReadAll(s, cfUser1, "", 1, 50)
		require.NoError(t, err)
		tasks, ok := res.([]*Task)
		require.True(t, ok, "got %T", res)
		got := []int64{}
		for _, task := range tasks {
			got = append(got, task.ID)
		}
		assert.ElementsMatch(t, []int64{env.b, env.d}, got)
		assert.EqualValues(t, 2, total)
	})
	t.Run("a manual view of the same project is unaffected", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		res, _, _, err := (&TaskCollection{ProjectID: 11, ProjectViewID: 44}).ReadAll(s, cfUser1, "", 1, 50)
		require.NoError(t, err)
		buckets, ok := res.([]*Bucket)
		require.True(t, ok)
		for _, b := range buckets {
			assert.Positive(t, b.ID)
			assert.Nil(t, b.Status)
		}
	})
	t.Run("archived statuses get no column and the status of a task stays readable", func(t *testing.T) {
		cols := wfColumns(t, view, cfUser1, 1, 50, "")
		for _, c := range cols {
			assert.NotEqual(t, int64(-stLegacy), c.ID)
		}
	})
	t.Run("a new status shows up", func(t *testing.T) {
		wf := readWorkflow(t, 11)
		wf.Statuses = append(wf.Statuses, &WorkflowStatus{ID: -10, Title: "QA", Category: WorkflowCategoryInProgress, Position: 1000000})
		mustSaveWorkflow(t, wf, cfUser1)
		cols := wfColumns(t, view, cfUser1, 1, 50, "")
		assert.Equal(t, "QA", cols[len(cols)-1].Title)
	})
}

func TestStatusKanban_Pagination(t *testing.T) {
	db.LoadAndAssertFixtures(t)
	view := wfStatusView(t, 11)
	created := []int64{20}
	for i := range 5 {
		created = append(created, wfCreateTask(t, fmt.Sprintf("page %d", i), stTodo))
	}
	cols := wfColumns(t, view, cfUser1, 1, 2, "")
	require.Len(t, cols[0].Tasks, 2)
	assert.EqualValues(t, 6, cols[0].Count)

	cols2 := wfColumns(t, view, cfUser1, 2, 2, "")
	require.Len(t, cols2[0].Tasks, 2)
	cols3 := wfColumns(t, view, cfUser1, 3, 2, "")
	require.Len(t, cols3[0].Tasks, 2)
	seen := append(append(columnTaskIDs(cols[0]), columnTaskIDs(cols2[0])...), columnTaskIDs(cols3[0])...)
	slices.Sort(seen)
	assert.Equal(t, 6, len(seen))
	assert.Equal(t, created, seen)
	cols4 := wfColumns(t, view, cfUser1, 4, 2, "")
	assert.Empty(t, cols4[0].Tasks)
	assert.EqualValues(t, 6, cols4[0].Count)
}

func TestStatusKanban_Move(t *testing.T) {
	t.Run("success changes the status through the transition function", func(t *testing.T) {
		env := setupWFViewEnv(t)
		view := wfStatusView(t, 11)
		before := len(taskHistoryOf(t, env.a))

		tb, err := wfMove(t, view, env.a, stDoing, cfUser1)
		require.NoError(t, err)
		assert.Equal(t, stDoing, taskStatusOf(t, env.a).StatusID)
		assert.Len(t, taskHistoryOf(t, env.a), before+1)
		hist := taskHistoryOf(t, env.a)
		last := hist[len(hist)-1]
		assert.Equal(t, stDoing, last.ToStatusID)
		assert.EqualValues(t, 1, last.DoerID)
		assert.Empty(t, last.Comment)
		require.NotNil(t, tb.Bucket)
		assert.Equal(t, int64(-2), tb.Bucket.ID)
		assert.EqualValues(t, 3, tb.Bucket.Count)
		assert.Equal(t, env.a, tb.Task.ID)

		cols := wfColumns(t, view, cfUser1, 1, 50, "")
		assert.ElementsMatch(t, []int64{20}, columnTaskIDs(cols[0]))
		assert.ElementsMatch(t, []int64{env.a, env.b, env.d}, columnTaskIDs(cols[1]))
	})
	t.Run("entering and leaving a done column syncs the done flag", func(t *testing.T) {
		env := setupWFViewEnv(t)
		view := wfStatusView(t, 11)
		_, err := wfMove(t, view, env.a, stDone, cfUser1)
		// Doing is not required, no transition rule from Todo to Done
		require.NoError(t, err)
		assert.True(t, taskDone(t, env.a))
		_, err = wfMove(t, view, env.a, stTodo, cfUser1)
		require.NoError(t, err)
		assert.False(t, taskDone(t, env.a))
	})
	t.Run("the same column is a no-op", func(t *testing.T) {
		env := setupWFViewEnv(t)
		view := wfStatusView(t, 11)
		before := len(taskHistoryOf(t, env.b))
		_, err := wfMove(t, view, env.b, stDoing, cfUser1)
		require.NoError(t, err)
		assert.Len(t, taskHistoryOf(t, env.b), before)
	})
	t.Run("rejections leave status, history, done flag and position untouched", func(t *testing.T) {
		env := setupWFViewEnv(t)
		view := wfStatusView(t, 11)
		wfGrantWrite(t, 2)
		cols := wfColumns(t, view, cfUser1, 1, 50, "") // gives the tasks their positions in the view

		wf := readWorkflow(t, 11)
		for _, tr := range wf.Transitions {
			if tr.FromStatusID == stDoing && tr.ToStatusID == stReview {
				tr.RequiredFields = []string{WorkflowFieldDueDate}
			}
		}
		// admin only transition todo to doing, and a restricted pair
		wf.Transitions = append(wf.Transitions, &WorkflowTransition{FromStatusID: stTodo, ToStatusID: stDoing, MinPermission: WorkflowTransitionPermissionAdmin})
		mustSaveWorkflow(t, wf, cfUser1)
		_ = cols

		expectUntouched := func(t *testing.T, taskID, status int64, histLen int, pos float64, done bool) {
			t.Helper()
			assert.Equal(t, status, taskStatusOf(t, taskID).StatusID)
			assert.Len(t, taskHistoryOf(t, taskID), histLen)
			assert.Equal(t, done, taskDone(t, taskID))
			assert.InDelta(t, pos, wfPosition(t, view.ID, taskID), 0.0001)
		}

		t.Run("requirements not met (21007)", func(t *testing.T) {
			hist, pos := len(taskHistoryOf(t, env.b)), wfPosition(t, view.ID, env.b)
			_, err := wfMove(t, view, env.b, stReview, cfUser1)
			req := requireRequirements(t, err)
			assert.Equal(t, env.b, req.TaskID)
			assert.Equal(t, stReview, req.ToStatusID)
			assert.Equal(t, []string{WorkflowFieldDueDate}, req.MissingFields)
			assert.Equal(t, ErrCodeWorkflowRequirementsNotMet, req.HTTPError().Code)
			expectUntouched(t, env.b, stDoing, hist, pos, false)
		})
		t.Run("forbidden (21006)", func(t *testing.T) {
			hist, pos := len(taskHistoryOf(t, env.a)), wfPosition(t, view.ID, env.a)
			_, err := wfMove(t, view, env.a, stDoing, &user.User{ID: 2})
			require.Error(t, err)
			assert.True(t, IsErrWorkflowTransitionForbidden(err), "%T %v", err, err)
			assert.Equal(t, ErrCodeWorkflowTransitionForbidden, err.(ErrWorkflowTransitionForbidden).HTTPError().Code)
			expectUntouched(t, env.a, stTodo, hist, pos, false)
		})
		t.Run("not allowed (21005)", func(t *testing.T) {
			wf := readWorkflow(t, 11)
			wf.RestrictTransitions = true
			mustSaveWorkflow(t, wf, cfUser1)
			hist, pos := len(taskHistoryOf(t, env.a)), wfPosition(t, view.ID, env.a)
			_, err := wfMove(t, view, env.a, stDone, cfUser1)
			require.Error(t, err)
			assert.True(t, IsErrWorkflowTransitionNotAllowed(err), "%T %v", err, err)
			expectUntouched(t, env.a, stTodo, hist, pos, false)
		})
		t.Run("unknown and archived statuses", func(t *testing.T) {
			_, err := wfMove(t, view, env.a, 999, cfUser1)
			assert.True(t, IsErrWorkflowStatusDoesNotExist(err), "%T", err)
			_, err = wfMove(t, view, env.a, stLegacy, cfUser1)
			assert.True(t, IsErrWorkflowStatusArchived(err), "%T", err)
			assert.Equal(t, stTodo, taskStatusOf(t, env.a).StatusID)
		})
	})
	t.Run("a real bucket id is not a column", func(t *testing.T) {
		env := setupWFViewEnv(t)
		view := wfStatusView(t, 11)
		s := db.NewSession()
		defer s.Close()
		tb := &TaskBucket{BucketID: 11, TaskID: env.a, ProjectViewID: view.ID, ProjectID: 11}
		_, err := tb.CanUpdate(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrBucketDoesNotExist(err), "%T", err)
	})
	t.Run("read only access cannot move", func(t *testing.T) {
		env := setupWFViewEnv(t)
		view := wfStatusView(t, 11)
		insertRow(t, &ProjectUser{UserID: 3, ProjectID: 11, Permission: PermissionRead})
		_, err := wfMove(t, view, env.a, stDoing, &user.User{ID: 3})
		require.Error(t, err)
		assert.Equal(t, stTodo, taskStatusOf(t, env.a).StatusID)
	})
	t.Run("a task of another project cannot be moved through the view", func(t *testing.T) {
		setupWFViewEnv(t)
		view := wfStatusView(t, 11)
		_, err := wfMove(t, view, 1, stDoing, cfUser1)
		require.Error(t, err)
		assert.Nil(t, taskStatusOfOrNil(t, 1))
	})
}

func taskStatusOfOrNil(t *testing.T, taskID int64) *TaskStatus {
	t.Helper()
	s := db.NewSession()
	defer s.Close()
	row, err := getTaskStatusRow(s, taskID)
	require.NoError(t, err)
	return row
}

func TestStatusKanban_ReorderWithinColumn(t *testing.T) {
	env := setupWFViewEnv(t)
	view := wfStatusView(t, 11)
	wfColumns(t, view, cfUser1, 1, 50, "")

	posB, posD := wfPosition(t, view.ID, env.b), wfPosition(t, view.ID, env.d)
	hist := len(taskHistoryOf(t, env.d))

	// The client sends the same column again followed by the new position.
	_, err := wfMove(t, view, env.d, stDoing, cfUser1)
	require.NoError(t, err)
	s := db.NewSession()
	defer s.Close()
	require.NoError(t, (&TaskPosition{TaskID: env.d, ProjectViewID: view.ID, Position: posB - 10}).Update(s, cfUser1))
	require.NoError(t, s.Commit())

	assert.Equal(t, stDoing, taskStatusOf(t, env.d).StatusID)
	assert.Len(t, taskHistoryOf(t, env.d), hist, "no status change")
	assert.InDelta(t, posB-10, wfPosition(t, view.ID, env.d), 0.0001)
	assert.NotEqual(t, posD, wfPosition(t, view.ID, env.d))

	cols := wfColumns(t, view, cfUser1, 1, 50, "")
	assert.Equal(t, []int64{env.d, env.b}, columnTaskIDs(cols[1]), "the column is ordered by the per-view position")
}

func TestStatusKanban_PositionsAreKeptPerView(t *testing.T) {
	env := setupWFViewEnv(t)
	view := wfStatusView(t, 11)
	wfColumns(t, view, cfUser1, 1, 50, "")
	other := wfPosition(t, 41, env.a)
	_, err := wfMove(t, view, env.a, stDoing, cfUser1)
	require.NoError(t, err)
	assert.InDelta(t, other, wfPosition(t, 41, env.a), 0.0001)
	// the moved task keeps its row in the status view
	assert.NotZero(t, wfPosition(t, view.ID, env.a))
}

func TestStatusKanban_CreateInColumn(t *testing.T) {
	create := func(t *testing.T, task *Task, a web.Auth) (*Task, error) {
		t.Helper()
		s := db.NewSession()
		defer s.Close()
		task.ProjectID = 11
		if err := task.Create(s, a); err != nil {
			_ = s.Rollback()
			events.CleanupPending(s)
			return nil, err
		}
		require.NoError(t, s.Commit())
		return task, nil
	}

	t.Run("initial column", func(t *testing.T) {
		setupWFViewEnv(t)
		task, err := create(t, &Task{Title: "new", BucketID: statusBucketID(stTodo)}, cfUser1)
		require.NoError(t, err)
		require.NoError(t, task.StatusColumnError())
		assert.Equal(t, stTodo, taskStatusOf(t, task.ID).StatusID)
		assert.Len(t, taskHistoryOf(t, task.ID), 1)
		assert.Equal(t, int64(-1), task.BucketID)
	})
	t.Run("another column with an allowed transition", func(t *testing.T) {
		setupWFViewEnv(t)
		task, err := create(t, &Task{Title: "new doing", BucketID: statusBucketID(stDoing)}, cfUser1)
		require.NoError(t, err)
		require.NoError(t, task.StatusColumnError())
		assert.Equal(t, stDoing, taskStatusOf(t, task.ID).StatusID)
		assert.Len(t, taskHistoryOf(t, task.ID), 2)
		assert.Equal(t, int64(-2), task.BucketID)
	})
	t.Run("a done column marks the task done", func(t *testing.T) {
		setupWFViewEnv(t)
		task, err := create(t, &Task{Title: "new done", BucketID: statusBucketID(stDone)}, cfUser1)
		require.NoError(t, err)
		require.NoError(t, task.StatusColumnError())
		assert.True(t, taskDone(t, task.ID))
	})
	t.Run("a rejected transition keeps the task in the initial status", func(t *testing.T) {
		setupWFViewEnv(t)
		wf := readWorkflow(t, 11)
		wf.Transitions = append(wf.Transitions, &WorkflowTransition{FromStatusID: stTodo, ToStatusID: stDoing, MinPermission: WorkflowTransitionPermissionWrite, RequiredFields: []string{WorkflowFieldDueDate}})
		mustSaveWorkflow(t, wf, cfUser1)

		task, err := create(t, &Task{Title: "stays todo", BucketID: statusBucketID(stDoing)}, cfUser1)
		require.NoError(t, err, "creating itself succeeds")
		require.Error(t, task.StatusColumnError())
		req := requireRequirements(t, task.StatusColumnError())
		assert.Equal(t, task.ID, req.TaskID)
		assert.Equal(t, []string{WorkflowFieldDueDate}, req.MissingFields)
		assert.NotZero(t, task.ID)
		assert.Equal(t, stTodo, taskStatusOf(t, task.ID).StatusID)
		assert.Len(t, taskHistoryOf(t, task.ID), 1)
		assert.Equal(t, int64(-1), task.BucketID)

		// With the due date in the payload the same creation works.
		task2, err := create(t, &Task{Title: "has due", BucketID: statusBucketID(stDoing), DueDate: time.Now().Add(time.Hour)}, cfUser1)
		require.NoError(t, err)
		require.NoError(t, task2.StatusColumnError())
		assert.Equal(t, stDoing, taskStatusOf(t, task2.ID).StatusID)
	})
	t.Run("a column that does not exist creates nothing", func(t *testing.T) {
		setupWFViewEnv(t)
		s := db.NewSession()
		before, err := s.Where("project_id = 11").Count(&Task{})
		require.NoError(t, err)
		s.Close()

		_, err = create(t, &Task{Title: "bad", BucketID: -999}, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowStatusDoesNotExist(err), "%T", err)
		_, err = create(t, &Task{Title: "archived", BucketID: statusBucketID(stLegacy)}, cfUser1)
		assert.True(t, IsErrWorkflowStatusArchived(err), "%T", err)

		s = db.NewSession()
		defer s.Close()
		after, err := s.Where("project_id = 11").Count(&Task{})
		require.NoError(t, err)
		assert.Equal(t, before, after)
	})
	t.Run("a negative bucket in a project without workflow is not found", func(t *testing.T) {
		db.LoadAndAssertFixtures(t)
		s := db.NewSession()
		defer s.Close()
		err := (&Task{Title: "x", ProjectID: 1, BucketID: -1}).Create(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrBucketDoesNotExist(err), "%T", err)
	})
	t.Run("task lands in the column and has a position in the status view", func(t *testing.T) {
		setupWFViewEnv(t)
		view := wfStatusView(t, 11)
		task, err := create(t, &Task{Title: "positioned", BucketID: statusBucketID(stDoing)}, cfUser1)
		require.NoError(t, err)
		cols := wfColumns(t, view, cfUser1, 1, 50, "")
		assert.Contains(t, columnTaskIDs(cols[1]), task.ID)
	})
}

func TestStatusKanban_ModeNeedsAWorkflow(t *testing.T) {
	db.LoadAndAssertFixtures(t)

	t.Run("create on a project without a workflow", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		v := &ProjectView{ProjectID: 1, Title: "x", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus}
		err := v.Create(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowRequiredForStatusView(err), "%T", err)
		he := err.(ErrWorkflowRequiredForStatusView).HTTPError()
		assert.Equal(t, 400, he.HTTPCode)
		assert.Equal(t, ErrCodeWorkflowRequiredForStatusView, he.Code)
		_ = s.Rollback()
	})
	t.Run("update to status on a project without a workflow", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		// view 4 is the kanban view of project 1
		v := &ProjectView{ID: 4, ProjectID: 1, Title: "Kanban", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus}
		err := v.Update(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowRequiredForStatusView(err), "%T", err)
		_ = s.Rollback()
		s2 := db.NewSession()
		defer s2.Close()
		stored, err := GetProjectViewByIDAndProject(s2, 4, 1)
		require.NoError(t, err)
		assert.Equal(t, BucketConfigurationModeManual, stored.BucketConfigurationMode)
	})
	t.Run("other view kinds ignore the mode", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		v := &ProjectView{ProjectID: 1, Title: "list", ViewKind: ProjectViewKindList, BucketConfigurationMode: BucketConfigurationModeStatus}
		require.NoError(t, v.Create(s, cfUser1))
		assert.Equal(t, BucketConfigurationModeNone, v.BucketConfigurationMode)
		require.NoError(t, s.Commit())
	})
	t.Run("update of an existing manual view with a workflow", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		v := &ProjectView{ID: 44, ProjectID: 11, Title: "Kanban", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus}
		require.NoError(t, v.Update(s, cfUser1))
		require.NoError(t, s.Commit())

		cols := wfColumns(t, &ProjectView{ID: 44, ProjectID: 11}, cfUser1, 1, 50, "")
		assert.Len(t, cols, 5)
		assert.Equal(t, int64(-1), cols[0].ID)
	})
}

func TestStatusKanban_SwitchBackToManual(t *testing.T) {
	env := setupWFViewEnv(t)
	view := wfStatusView(t, 11)

	// A manual view of the same project, converted to status and back, keeps its buckets and sees new tasks.
	s := db.NewSession()
	manual := &ProjectView{ID: 44, ProjectID: 11, Title: "Kanban", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus}
	require.NoError(t, manual.Update(s, cfUser1))
	require.NoError(t, s.Commit())
	s.Close()
	late := wfCreateTask(t, "created while in status mode", stTodo)

	s = db.NewSession()
	back := &ProjectView{ID: 44, ProjectID: 11, Title: "Kanban", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeManual}
	require.NoError(t, back.Update(s, cfUser1))
	require.NoError(t, s.Commit())
	s.Close()

	cols := wfColumns(t, &ProjectView{ID: 44, ProjectID: 11}, cfUser1, 1, 100, "")
	seen := []int64{}
	for _, c := range cols {
		assert.Positive(t, c.ID)
		seen = append(seen, columnTaskIDs(c)...)
	}
	assert.Contains(t, seen, late)
	assert.Contains(t, seen, env.a)
	_ = view
}

func TestStatusKanban_WorkflowDeletedLater(t *testing.T) {
	env := setupWFViewEnv(t)
	view := wfStatusView(t, 11)

	s := db.NewSession()
	require.NoError(t, (&Workflow{ProjectID: 11}).Delete(s, cfUser1))
	require.NoError(t, s.Commit())
	s.Close()

	t.Run("the board has no columns and no error", func(t *testing.T) {
		cols := wfColumns(t, view, cfUser1, 1, 50, "")
		assert.NotNil(t, cols)
		assert.Empty(t, cols)
	})
	t.Run("the bucket list is empty", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		res, n, _, err := (&Bucket{ProjectID: 11, ProjectViewID: view.ID}).ReadAll(s, cfUser1, "", 1, 50)
		require.NoError(t, err)
		assert.Zero(t, n)
		assert.Empty(t, res.([]*Bucket))
	})
	t.Run("moves fail with the workflow error", func(t *testing.T) {
		_, err := wfMove(t, view, env.a, stDoing, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrWorkflowDoesNotExist(err), "%T", err)
	})
	t.Run("creating a task in a column fails", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		err := (&Task{Title: "x", ProjectID: 11, BucketID: -1}).Create(s, cfUser1)
		require.Error(t, err)
		assert.True(t, IsErrBucketDoesNotExist(err), "%T", err)
	})
	t.Run("the view can still be renamed and switched", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		v := &ProjectView{ID: view.ID, ProjectID: 11, Title: "Renamed", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus}
		require.NoError(t, v.Update(s, cfUser1))
		v2 := &ProjectView{ID: view.ID, ProjectID: 11, Title: "Renamed", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeManual}
		require.NoError(t, v2.Update(s, cfUser1))
		require.NoError(t, s.Commit())
	})
}

func TestStatusKanban_BucketManagementIsRejected(t *testing.T) {
	setupWFViewEnv(t)
	view := wfStatusView(t, 11)
	assertManaged := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		managed, ok := err.(ErrStatusViewBucketsManaged)
		require.True(t, ok, "%T: %v", err, err)
		he := managed.HTTPError()
		assert.Equal(t, 400, he.HTTPCode)
		assert.Equal(t, ErrCodeStatusViewBucketsManaged, he.Code)
	}

	t.Run("create", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		_, err := (&Bucket{Title: "x", ProjectID: 11, ProjectViewID: view.ID}).CanCreate(s, cfUser1)
		assertManaged(t, err)
	})
	t.Run("rename and limit", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		_, err := (&Bucket{ID: -2, Title: "x", Limit: 3, ProjectID: 11, ProjectViewID: view.ID}).CanUpdate(s, cfUser1)
		assertManaged(t, err)
	})
	t.Run("delete", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		_, err := (&Bucket{ID: -2, ProjectID: 11, ProjectViewID: view.ID}).CanDelete(s, cfUser1)
		assertManaged(t, err)
	})
	t.Run("default and done bucket", func(t *testing.T) {
		for _, v := range []*ProjectView{
			{ID: view.ID, ProjectID: 11, Title: "x", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus, DoneBucketID: 27},
			{ID: view.ID, ProjectID: 11, Title: "x", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus, DefaultBucketID: 27},
		} {
			s := db.NewSession()
			assertManaged(t, v.Update(s, cfUser1))
			_ = s.Rollback()
			s.Close()
		}
	})
	t.Run("an update that only renames passes", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		v := &ProjectView{ID: view.ID, ProjectID: 11, Title: "Renamed", ViewKind: ProjectViewKindKanban, BucketConfigurationMode: BucketConfigurationModeStatus}
		require.NoError(t, v.Update(s, cfUser1))
		require.NoError(t, s.Commit())
	})
	t.Run("manual views keep their management", func(t *testing.T) {
		s := db.NewSession()
		defer s.Close()
		can, err := (&Bucket{Title: "x", ProjectID: 11, ProjectViewID: 44}).CanCreate(s, cfUser1)
		require.NoError(t, err)
		assert.True(t, can)
	})
}

// A token that may only read tasks must not read their workflow status through the expand, which has
// its own permission, tasks status (GET /tasks/:task/status).
func TestCanDoAPIRoute_ExpandStatusScope(t *testing.T) {
	resetAPITokenRoutes(t)
	for _, r := range []echo.RouteInfo{
		{Method: "GET", Path: "/api/v2/tasks"},
		{Method: "GET", Path: "/api/v2/tasks/:task"},
		{Method: "GET", Path: "/api/v2/projects/:project/tasks"},
		{Method: "GET", Path: "/api/v2/projects/:project/views/:view/tasks"},
		{Method: "GET", Path: "/api/v2/projects/:project/views/:view/buckets/tasks"},
		{Method: "GET", Path: "/api/v2/tasks/:task/status"},
		{Method: "GET", Path: "/api/v1/tasks"},
	} {
		CollectRoutesForAPITokenUsage(r, true)
	}

	e := echo.New()
	allowed := func(url string, token *APIToken) bool {
		c := e.NewContext(httptest.NewRequest("GET", url, nil), httptest.NewRecorder())
		return CanDoAPIRoute(c, token)
	}
	base := APIPermissions{
		"tasks":                []string{"read_all", "read_one"},
		"projects":             []string{"views_buckets_tasks"},
		"projects_views_tasks": []string{"read_all"},
	}
	tasksOnly := &APIToken{APIPermissions: base}
	withStatus := &APIToken{APIPermissions: APIPermissions{
		"tasks":                []string{"read_all", "read_one", "status"},
		"projects":             []string{"views_buckets_tasks"},
		"projects_views_tasks": []string{"read_all"},
	}}

	require.NoError(t, PermissionsAreValid(withStatus.APIPermissions), "the status permission exists")
	for _, path := range []string{
		"/api/v2/tasks",
		"/api/v2/tasks/:task",
		"/api/v2/projects/:project/tasks",
		"/api/v2/projects/:project/views/:view/tasks",
		"/api/v2/projects/:project/views/:view/buckets/tasks",
	} {
		assert.False(t, allowed(path+"?expand=status", tasksOnly), "%s must need the status permission", path)
		assert.True(t, allowed(path+"?expand=status", withStatus), "%s may expand the status with it", path)
	}
	assert.True(t, allowed("/api/v2/tasks", tasksOnly), "no expand stays allowed")
}
