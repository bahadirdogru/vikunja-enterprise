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

package script

import "context"

// Host is everything a script can reach beyond the interpreter. The runtime counts and limits the calls, converts
// the arguments to plain Go values and hands them to the host; it never gives a script anything else. The engine
// has several implementations: one that performs changes, one that only records them (test runs) and a read-only
// one (conditions). The runner picks the implementation, the script cannot.
//
// Ids are validated to be positive integers before the host is called. Maps are plain map[string]any / []any /
// string / float64 / bool / nil trees.
type Host interface {
	TaskGet(ctx context.Context, id int64) (map[string]any, error)
	TaskFind(ctx context.Context, filter string, projectID int64, limit int) ([]map[string]any, error)
	TaskUpdate(ctx context.Context, id int64, patch map[string]any) (map[string]any, error)
	TaskCreate(ctx context.Context, projectID int64, data map[string]any) (map[string]any, error)
	TaskComment(ctx context.Context, id int64, text string) error
	FieldGet(ctx context.Context, taskID, fieldID int64) (any, error)
	FieldSet(ctx context.Context, taskID, fieldID int64, value any) error
	StatusSet(ctx context.Context, taskID, statusID int64, comment string) error
	AssigneeAdd(ctx context.Context, taskID, userID int64) error
	AssigneeRemove(ctx context.Context, taskID, userID int64) error
	LabelAdd(ctx context.Context, taskID, labelID int64) error
	LabelRemove(ctx context.Context, taskID, labelID int64) error
	Fetch(ctx context.Context, req *FetchRequest) (*FetchResponse, error)
	SecretGet(ctx context.Context, name string) (string, error)
}

// FetchRequest is an outgoing HTTP request of a script.
type FetchRequest struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    string
	// TimeoutMs is what the script asked for; the host caps it.
	TimeoutMs int64
}

// FetchResponse is the answer to a FetchRequest.
type FetchResponse struct {
	Status  int
	Headers map[string]string
	Body    string
	// Truncated is set when the body was cut off at the size limit.
	Truncated bool
}
