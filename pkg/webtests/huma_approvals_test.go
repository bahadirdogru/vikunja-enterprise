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

package webtests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/auth"
	"code.vikunja.io/api/pkg/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

var approvalsUser6 = user.User{ID: 6, Username: "user6", Issuer: "local"}

type approvalDTO struct {
	ID           int64   `json:"id"`
	TaskID       int64   `json:"task_id"`
	ProjectID    int64   `json:"project_id"`
	Title        string  `json:"title"`
	Comment      string  `json:"comment"`
	Mode         string  `json:"mode"`
	Status       string  `json:"status"`
	CancelReason string  `json:"cancel_reason"`
	ToStatusID   *int64  `json:"to_status_id"`
	FromStatusID *int64  `json:"from_status_id"`
	Due          *string `json:"due"`
	Approvers    []struct {
		UserID   int64   `json:"user_id"`
		Decision *string `json:"decision"`
		Comment  string  `json:"comment"`
		User     struct {
			Username string `json:"username"`
		} `json:"user"`
	} `json:"approvers"`
	RequestedBy struct {
		ID int64 `json:"id"`
	} `json:"requested_by"`
	Task struct {
		ID           int64  `json:"id"`
		Title        string `json:"title"`
		Identifier   string `json:"identifier"`
		ProjectID    int64  `json:"project_id"`
		ProjectTitle string `json:"project_title"`
	} `json:"task"`
	ToStatus *struct {
		Title string `json:"title"`
	} `json:"to_status"`
}

type approvalListDTO struct {
	Items      []approvalDTO `json:"items"`
	Total      int64         `json:"total"`
	Page       int           `json:"page"`
	PerPage    int           `json:"per_page"`
	TotalPages int64         `json:"total_pages"`
}

type statusChangeDTO struct {
	TaskID int64 `json:"task_id"`
	Done   bool  `json:"done"`
	Status *struct {
		ID int64 `json:"id"`
	} `json:"status"`
	Approval *approvalDTO `json:"approval"`
}

func TestHumaApprovals(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	user1 := humaTokenFor(t, &testuser1)
	user2 := humaTokenFor(t, &testuser2)
	user6 := humaTokenFor(t, &approvalsUser6)
	// User10 has no access to project 11.
	stranger := humaTokenFor(t, &testuser10)

	// Share project 11 with user2 (write) before anything else.
	s := db.NewSession()
	_, err = s.Insert(&models.ProjectUser{UserID: 2, ProjectID: 11, Permission: models.PermissionWrite})
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	s.Close()

	newTask := func(t *testing.T, title string) int64 {
		t.Helper()
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/tasks", fmt.Sprintf(`{"title":%q}`, title), user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var created struct {
			ID int64 `json:"id"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		return created.ID
	}
	statusChange := func(t *testing.T, taskID int64, body string, token string) (*httpResult, *statusChangeDTO) {
		t.Helper()
		rec := humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/tasks/%d/status", taskID), body, token, "")
		res := &httpResult{code: rec.Code, body: rec.Body.Bytes()}
		if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
			return res, nil
		}
		dto := &statusChangeDTO{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), dto), rec.Body.String())
		return res, dto
	}
	readApproval := func(t *testing.T, id int64, token string) (int, approvalDTO, []byte) {
		t.Helper()
		rec := humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/approvals/%d", id), "", token, "")
		var dto approvalDTO
		if rec.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
		}
		return rec.Code, dto, rec.Body.Bytes()
	}

	t.Run("the workflow document accepts, returns and validates the approval configuration", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/projects/11/workflow", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var doc map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
		transitions := doc["transitions"].([]any)
		first := transitions[0].(map[string]any)
		assert.Equal(t, false, first["requires_approval"], "existing transitions need no approval")
		assert.Nil(t, first["approval"])

		// Fixture transition 3 is Todo to Dropped. Dropping needs the approval of user2 or user6, a rejection moves
		// the task to Doing. Todo to Done needs it of user6 and everybody must agree.
		dropping := transitions[2].(map[string]any)
		dropping["requires_approval"] = true
		dropping["approval"] = map[string]any{
			"mode":               "any",
			"approvers":          []any{map[string]any{"type": "user", "id": 2}, map[string]any{"type": "user", "id": 6}},
			"rejected_status_id": 2,
			"due_in":             "3d",
		}
		dropping["team_ids"] = []any{}
		doc["transitions"] = append(transitions, map[string]any{
			"from_status_id": 1, "to_status_id": 4, "requires_approval": true,
			"approval": map[string]any{"mode": "all", "approvers": []any{map[string]any{"type": "user", "id": 6}, map[string]any{"type": "assignees"}}},
		})
		body, err := json.Marshal(doc)
		require.NoError(t, err)

		rec = humaRequest(t, e, http.MethodPut, "/api/v2/projects/11/workflow", string(body), user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var saved struct {
			Transitions []struct {
				FromStatusID     int64 `json:"from_status_id"`
				ToStatusID       int64 `json:"to_status_id"`
				RequiresApproval bool  `json:"requires_approval"`
				Approval         *struct {
					Mode             string `json:"mode"`
					RejectedStatusID int64  `json:"rejected_status_id"`
					DueIn            string `json:"due_in"`
					AllowSelf        bool   `json:"allow_self"`
					Approvers        []struct {
						Type string `json:"type"`
						ID   int64  `json:"id"`
					} `json:"approvers"`
				} `json:"approval"`
			} `json:"transitions"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &saved))
		require.Len(t, saved.Transitions, 4)
		var found bool
		for _, tr := range saved.Transitions {
			if tr.FromStatusID == 1 && tr.ToStatusID == 5 {
				found = true
				assert.True(t, tr.RequiresApproval)
				require.NotNil(t, tr.Approval)
				assert.Equal(t, "any", tr.Approval.Mode)
				assert.EqualValues(t, 2, tr.Approval.RejectedStatusID)
				assert.Equal(t, "3d", tr.Approval.DueIn)
				require.Len(t, tr.Approval.Approvers, 2)
				assert.Equal(t, "user", tr.Approval.Approvers[0].Type)
				assert.EqualValues(t, 2, tr.Approval.Approvers[0].ID)
			}
		}
		assert.True(t, found)

		// Reading returns the same document.
		again := humaRequest(t, e, http.MethodGet, "/api/v2/projects/11/workflow", "", user1, "")
		require.Equal(t, http.StatusOK, again.Code)
		var reread map[string]any
		require.NoError(t, json.Unmarshal(again.Body.Bytes(), &reread))
		assert.Equal(t, reread["transitions"], func() any {
			var roundTrip map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &roundTrip))
			return roundTrip["transitions"]
		}())

		// Invalid configurations are 400 with the workflow error code.
		for name, mutate := range map[string]func(tr map[string]any){
			"no approvers":     func(tr map[string]any) { tr["approval"] = map[string]any{"approvers": []any{}} },
			"no configuration": func(tr map[string]any) { tr["approval"] = nil },
			"user without access": func(tr map[string]any) {
				tr["approval"] = map[string]any{"approvers": []any{map[string]any{"type": "user", "id": 10}}}
			},
			"bad due_in": func(tr map[string]any) {
				tr["approval"] = map[string]any{"approvers": []any{map[string]any{"type": "user", "id": 6}}, "due_in": "soon"}
			},
			"rejected status of another workflow": func(tr map[string]any) {
				tr["approval"] = map[string]any{"approvers": []any{map[string]any{"type": "user", "id": 6}}, "rejected_status_id": 9999}
			},
		} {
			var invalid map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &invalid))
			trs := invalid["transitions"].([]any)
			mutate(trs[2].(map[string]any))
			raw, err := json.Marshal(invalid)
			require.NoError(t, err)
			res := humaRequest(t, e, http.MethodPut, "/api/v2/projects/11/workflow", string(raw), user1, "")
			require.Equal(t, http.StatusBadRequest, res.Code, "%s: %s", name, res.Body.String())
			assert.Equal(t, models.ErrCodeWorkflowInvalidDefinition, decodeProblem(t, res.Body.Bytes()).Code, name)
		}
	})

	var requestID, taskID int64

	t.Run("a transition that needs approval answers 202 with the pending request", func(t *testing.T) {
		taskID = newTask(t, "needs a sign-off")
		res, dto := statusChange(t, taskID, `{"to_status_id":5,"comment":"drop it"}`, user1)
		require.Equal(t, http.StatusAccepted, res.code, string(res.body))
		require.NotNil(t, dto.Approval)
		require.NotNil(t, dto.Status)
		assert.EqualValues(t, 1, dto.Status.ID, "the task is still in Todo")
		assert.False(t, dto.Done)
		requestID = dto.Approval.ID

		approval := dto.Approval
		assert.Equal(t, "pending", approval.Status)
		assert.Equal(t, "any", approval.Mode)
		assert.Equal(t, taskID, approval.TaskID)
		assert.EqualValues(t, 11, approval.ProjectID)
		assert.Equal(t, "drop it", approval.Comment)
		assert.EqualValues(t, 1, approval.RequestedBy.ID)
		require.NotNil(t, approval.ToStatusID)
		assert.EqualValues(t, 5, *approval.ToStatusID)
		require.NotNil(t, approval.ToStatus)
		assert.Equal(t, "Dropped", approval.ToStatus.Title)
		require.Len(t, approval.Approvers, 2)
		assert.Equal(t, "user2", approval.Approvers[0].User.Username)
		assert.Nil(t, approval.Approvers[0].Decision)
		assert.NotNil(t, approval.Due, "due_in 3d set a due time")
		assert.Equal(t, "needs a sign-off", approval.Task.Title)
		assert.NotEmpty(t, approval.Task.ProjectTitle)

		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": taskID, "status_id": 1}, false)

		// Asking again returns the same request, still 202.
		res, dto = statusChange(t, taskID, `{"to_status_id":5}`, user1)
		require.Equal(t, http.StatusAccepted, res.code, string(res.body))
		assert.Equal(t, requestID, dto.Approval.ID)
		db.AssertCount(t, "approval_requests", builder.Eq{"task_id": taskID}, 1)

		// A change that needs no approval is still 200 and has no approval.
		other := newTask(t, "free move")
		res, dto = statusChange(t, other, `{"to_status_id":2}`, user1)
		require.Equal(t, http.StatusOK, res.code, string(res.body))
		assert.Nil(t, dto.Approval)
		assert.EqualValues(t, 2, dto.Status.ID)
	})

	t.Run("the other checks of the transition still come first", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/tasks/%d/status", taskID), `{"to_status_id":9999}`, user1, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		rec = humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/tasks/%d/status", taskID), `{"to_status_id":5}`, stranger, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("nobody to ask is a 422 with its own code", func(t *testing.T) {
		// The only approver of Todo to Done is user6 and the task has no assignee: asked by user6 nobody is left.
		id := newTask(t, "asked by the only approver")
		rec := humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/tasks/%d/status", id), `{"to_status_id":4}`, user6, "")
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		p := decodeProblem(t, rec.Body.Bytes())
		assert.Equal(t, models.ErrCodeApprovalNoApprovers, p.Code)
		assert.EqualValues(t, id, p.Details["task_id"])
		db.AssertCount(t, "approval_requests", builder.Eq{"task_id": id}, 0)
	})

	t.Run("the status of the task shows the pending approvals and who would be asked", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/tasks/%d/status", taskID), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body struct {
			PendingApprovals []approvalDTO `json:"pending_approvals"`
			Transitions      []struct {
				ToStatus struct {
					ID int64 `json:"id"`
				} `json:"to_status"`
				RequiresApproval bool   `json:"requires_approval"`
				ApprovalMode     string `json:"approval_mode"`
				Approvers        []struct {
					ID       int64  `json:"id"`
					Username string `json:"username"`
				} `json:"approvers"`
			} `json:"transitions"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.PendingApprovals, 1)
		assert.Equal(t, requestID, body.PendingApprovals[0].ID)
		for _, tr := range body.Transitions {
			switch tr.ToStatus.ID {
			case 5:
				assert.True(t, tr.RequiresApproval)
				assert.Equal(t, "any", tr.ApprovalMode)
				require.Len(t, tr.Approvers, 2)
				assert.Equal(t, "user2", tr.Approvers[0].Username)
			case 2:
				assert.False(t, tr.RequiresApproval)
				assert.Empty(t, tr.Approvers)
			}
		}
	})

	t.Run("reading a request", func(t *testing.T) {
		for _, token := range []string{user1, user2, user6} {
			code, dto, body := readApproval(t, requestID, token)
			require.Equal(t, http.StatusOK, code, string(body))
			assert.Equal(t, requestID, dto.ID)
		}
		code, _, _ := readApproval(t, requestID, stranger)
		assert.Equal(t, http.StatusForbidden, code)
		code, _, body := readApproval(t, 999999, user1)
		assert.Equal(t, http.StatusNotFound, code)
		assert.Equal(t, models.ErrCodeApprovalRequestDoesNotExist, decodeProblem(t, body).Code)
	})

	t.Run("my approvals lists what I have to decide or asked, with a task summary", func(t *testing.T) {
		list := func(query string, token string) approvalListDTO {
			rec := humaRequest(t, e, http.MethodGet, "/api/v2/approvals"+query, "", token, "")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var dto approvalListDTO
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
			return dto
		}
		asApprover := list("?role=approver&status=pending", user2)
		require.Len(t, asApprover.Items, 1)
		assert.Equal(t, requestID, asApprover.Items[0].ID)
		assert.Equal(t, "needs a sign-off", asApprover.Items[0].Task.Title)
		assert.NotEmpty(t, asApprover.Items[0].Task.Identifier)
		assert.EqualValues(t, 11, asApprover.Items[0].Task.ProjectID)
		assert.EqualValues(t, 1, asApprover.Total)

		assert.Empty(t, list("?role=requester", user2).Items)
		assert.Len(t, list("?role=requester", user1).Items, 1)
		assert.Len(t, list("?project_id=11", user1).Items, 1)
		assert.Empty(t, list("?project_id=1", user1).Items)
		assert.Empty(t, list("?status=approved", user1).Items)
		assert.Empty(t, list("", stranger).Items)

		paged := list("?per_page=1&page=1", user6)
		assert.Len(t, paged.Items, 1)
		assert.Equal(t, 1, paged.PerPage)

		for _, bad := range []string{"?status=bogus", "?role=bogus"} {
			rec := humaRequest(t, e, http.MethodGet, "/api/v2/approvals"+bad, "", user1, "")
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, bad+": "+rec.Body.String())
		}
	})

	t.Run("the approvals of a task", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/tasks/%d/approvals", taskID), "", user2, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var dto approvalListDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
		require.Len(t, dto.Items, 1)
		rec = humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/tasks/%d/approvals", taskID), "", stranger, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("the approvals expand marks tasks", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/tasks/%d?expand=approvals", taskID), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var task struct {
			Approvals *struct {
				HasPending   bool  `json:"has_pending"`
				PendingCount int64 `json:"pending_count"`
			} `json:"approvals"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &task))
		require.NotNil(t, task.Approvals)
		assert.True(t, task.Approvals.HasPending)
		assert.EqualValues(t, 1, task.Approvals.PendingCount)

		rec = humaRequest(t, e, http.MethodGet, "/api/v2/projects/11/tasks?expand=approvals", "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"has_pending":true`)
		rec = humaRequest(t, e, http.MethodGet, "/api/v2/tasks/20?expand=bogus", "", user1, "")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	})

	t.Run("deciding", func(t *testing.T) {
		decide := func(id int64, body, token string) *httpResult {
			rec := humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/approvals/%d/decision", id), body, token, "")
			return &httpResult{code: rec.Code, body: rec.Body.Bytes()}
		}

		res := decide(requestID, `{"decision":"rejected"}`, user2)
		require.Equal(t, http.StatusUnprocessableEntity, res.code, string(res.body))
		assert.Equal(t, models.ErrCodeApprovalCommentRequired, decodeProblem(t, res.body).Code)

		res = decide(requestID, `{"decision":"maybe"}`, user2)
		assert.Equal(t, http.StatusUnprocessableEntity, res.code, string(res.body))

		res = decide(requestID, `{"decision":"approved"}`, user1)
		require.Equal(t, http.StatusForbidden, res.code, string(res.body))
		assert.Equal(t, models.ErrCodeApprovalNotApprover, decodeProblem(t, res.body).Code)

		res = decide(requestID, `{"decision":"approved"}`, stranger)
		assert.Equal(t, http.StatusNotFound, res.code, "the request does not exist for somebody who cannot read the task: "+string(res.body))
		assert.Equal(t, models.ErrCodeApprovalRequestDoesNotExist, decodeProblem(t, res.body).Code)

		res = decide(requestID, `{"decision":"approved","comment":"fine"}`, user2)
		require.Equal(t, http.StatusOK, res.code, string(res.body))
		var decided struct {
			RequestID int64       `json:"request_id"`
			Decision  string      `json:"decision"`
			Comment   string      `json:"comment"`
			Request   approvalDTO `json:"request"`
		}
		require.NoError(t, json.Unmarshal(res.body, &decided))
		assert.Equal(t, requestID, decided.RequestID)
		assert.Equal(t, "approved", decided.Decision)
		assert.Equal(t, "approved", decided.Request.Status)
		require.NotNil(t, decided.Request.Approvers[0].Decision)
		assert.Equal(t, "approved", *decided.Request.Approvers[0].Decision)
		assert.Equal(t, "fine", decided.Request.Approvers[0].Comment)

		// The system performed the transition on behalf of the requester.
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": taskID, "status_id": 5}, false)
		db.AssertExists(t, "task_status_history", map[string]interface{}{"task_id": taskID, "to_status_id": 5, "doer_id": 1, "approval_request_id": requestID}, false)
		db.AssertExists(t, "tasks", map[string]interface{}{"id": taskID, "done": true}, false)

		res = decide(requestID, `{"decision":"approved"}`, user6)
		require.Equal(t, http.StatusConflict, res.code, string(res.body))
		assert.Equal(t, models.ErrCodeApprovalNotPending, decodeProblem(t, res.body).Code)

		// The status history exposes the approval.
		rec := humaRequest(t, e, http.MethodGet, fmt.Sprintf("/api/v2/tasks/%d/status/history", taskID), "", user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), fmt.Sprintf(`"approval_request_id":%d`, requestID))
	})

	t.Run("a rejection moves the task to the rejected status and comments", func(t *testing.T) {
		id := newTask(t, "to reject")
		_, dto := statusChange(t, id, `{"to_status_id":5}`, user1)
		require.NotNil(t, dto)
		rec := humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/approvals/%d/decision", dto.Approval.ID), `{"decision":"rejected","comment":"not like this"}`, user6, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"status":"rejected"`)
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": id, "status_id": 2}, false)
		db.AssertExists(t, "task_comments", map[string]interface{}{"task_id": id, "author_id": 6}, false)
	})

	t.Run("cancelling", func(t *testing.T) {
		id := newTask(t, "to cancel")
		_, dto := statusChange(t, id, `{"to_status_id":5}`, user1)
		require.NotNil(t, dto)
		path := fmt.Sprintf("/api/v2/approvals/%d/cancel", dto.Approval.ID)

		rec := humaRequest(t, e, http.MethodPost, path, `{}`, user2, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, "approvers cannot cancel: "+rec.Body.String())
		rec = humaRequest(t, e, http.MethodPost, path, `{}`, stranger, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		rec = humaRequest(t, e, http.MethodPost, path, `{"message":"wrong task"}`, user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var cancelled struct {
			Request approvalDTO `json:"request"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cancelled))
		assert.Equal(t, "cancelled", cancelled.Request.Status)
		assert.Equal(t, models.ApprovalCancelByRequester, cancelled.Request.CancelReason)
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": id, "status_id": 1}, false)

		rec = humaRequest(t, e, http.MethodPost, path, `{}`, user1, "")
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Equal(t, models.ErrCodeApprovalNotPending, decodeProblem(t, rec.Body.Bytes()).Code)

		// A project admin can cancel somebody else's request.
		_, dto = statusChange(t, id, `{"to_status_id":5}`, user2)
		require.NotNil(t, dto)
		rec = humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/approvals/%d/cancel", dto.Approval.ID), `{}`, user1, "")
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("a plain done update is a typed conflict and creates nothing", func(t *testing.T) {
		id := newTask(t, "checkbox")
		rec := humaRequest(t, e, http.MethodPut, fmt.Sprintf("/api/v2/tasks/%d", id), `{"title":"checkbox","done":true}`, user1, "")
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		p := decodeProblem(t, rec.Body.Bytes())
		assert.Equal(t, models.ErrCodeApprovalRequired, p.Code)
		assert.EqualValues(t, id, p.Details["task_id"])
		assert.EqualValues(t, 4, p.Details["to_status_id"])
		db.AssertExists(t, "tasks", map[string]interface{}{"id": id, "done": false}, false)
		db.AssertCount(t, "approval_requests", builder.Eq{"task_id": id}, 0)
	})

	t.Run("a kanban move into a column that needs approval answers 202 and the card stays", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/projects/11/views",
			`{"title":"By status","view_kind":"kanban","bucket_configuration_mode":"status"}`, user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var view struct {
			ID int64 `json:"id"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
		id := newTask(t, "dragged")

		path := fmt.Sprintf("/api/v2/projects/11/views/%d/buckets/-5/tasks", view.ID)
		rec = humaRequest(t, e, http.MethodPut, path, fmt.Sprintf(`{"task_id":%d}`, id), user1, "")
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		var moved struct {
			Approval *approvalDTO `json:"approval"`
			Bucket   struct {
				ID int64 `json:"id"`
			} `json:"bucket"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &moved))
		require.NotNil(t, moved.Approval)
		assert.Equal(t, "pending", moved.Approval.Status)
		assert.EqualValues(t, -1, moved.Bucket.ID, "the card is still in the Todo column")
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": id, "status_id": 1}, false)

		// A free column answers 200 without approval.
		rec = humaRequest(t, e, http.MethodPut, fmt.Sprintf("/api/v2/projects/11/views/%d/buckets/-2/tasks", view.ID), fmt.Sprintf(`{"task_id":%d}`, id), user1, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), `"approval"`)
	})

	t.Run("ad-hoc requests", func(t *testing.T) {
		id := newTask(t, "ad hoc")
		path := fmt.Sprintf("/api/v2/tasks/%d/approvals", id)

		rec := humaRequest(t, e, http.MethodPost, path, `{"title":"Legal check","description":"page 3","approver_ids":[2,6],"mode":"all"}`, user1, "")
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var created approvalDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		assert.Equal(t, "pending", created.Status)
		assert.Equal(t, "all", created.Mode)
		assert.Equal(t, "Legal check", created.Title)
		assert.Nil(t, created.ToStatusID)
		assert.Len(t, created.Approvers, 2)

		res := []struct{ body, why string }{
			{`{"approver_ids":[2]}`, "no title"},
			{`{"title":"x","approver_ids":[]}`, "no approvers"},
			{`{"title":"x","approver_ids":[10]}`, "approver without access"},
			{`{"title":"x","approver_ids":[1]}`, "the requester"},
		}
		for _, c := range res {
			rec = humaRequest(t, e, http.MethodPost, path, c.body, user1, "")
			require.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", c.why, rec.Body.String())
			assert.Equal(t, models.ErrCodeApprovalInvalid, decodeProblem(t, rec.Body.Bytes()).Code, c.why)
		}
		rec = humaRequest(t, e, http.MethodPost, path, `{"title":"x","approver_ids":[2]}`, stranger, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		// Both approvers have to approve; no status changes.
		for i, token := range []string{user2, user6} {
			rec = humaRequest(t, e, http.MethodPost, fmt.Sprintf("/api/v2/approvals/%d/decision", created.ID), `{"decision":"approved"}`, token, "")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			if i == 0 {
				assert.Contains(t, rec.Body.String(), `"status":"pending"`)
			} else {
				assert.Contains(t, rec.Body.String(), `"status":"approved"`)
			}
		}
		db.AssertExists(t, "task_statuses", map[string]interface{}{"task_id": id, "status_id": 1}, false)
	})

	t.Run("link shares have no access to approvals at all", func(t *testing.T) {
		share := &models.LinkSharing{ID: 3, Hash: "test3", ProjectID: 3, Permission: models.PermissionAdmin, SharingType: models.SharingTypeWithoutPassword, SharedByID: 1}
		token, err := auth.NewLinkShareJWTAuthtoken(share)
		require.NoError(t, err)
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/api/v2/approvals", ""},
			{http.MethodGet, fmt.Sprintf("/api/v2/approvals/%d", requestID), ""},
			{http.MethodGet, "/api/v2/tasks/32/approvals", ""},
			{http.MethodPost, "/api/v2/tasks/32/approvals", `{"title":"x","approver_ids":[1]}`},
			{http.MethodPost, fmt.Sprintf("/api/v2/approvals/%d/decision", requestID), `{"decision":"approved"}`},
			{http.MethodPost, fmt.Sprintf("/api/v2/approvals/%d/cancel", requestID), `{}`},
		} {
			rec := humaRequest(t, e, c.method, c.path, c.body, token, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", c.method, c.path, rec.Body.String())
		}
	})

	t.Run("without a token every route is 401", func(t *testing.T) {
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, "/api/v2/approvals"},
			{http.MethodGet, "/api/v2/approvals/1"},
			{http.MethodPost, "/api/v2/approvals/1/decision"},
			{http.MethodPost, "/api/v2/approvals/1/cancel"},
			{http.MethodGet, "/api/v2/tasks/20/approvals"},
			{http.MethodPost, "/api/v2/tasks/20/approvals"},
		} {
			rec := humaRequest(t, e, c.method, c.path, `{}`, "", "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code, c.method+" "+c.path)
		}
	})
}

type httpResult struct {
	code int
	body []byte
}
