import type {ApprovalRequest} from '@/client/generated'

// A pending request of two approvers (user 2 has approved, user 3 has not) made by user 1.
export function approvalFixture(overrides: Partial<ApprovalRequest> = {}): ApprovalRequest {
	return {
		id: 11,
		task_id: 7,
		project_id: 3,
		transition_id: 5,
		from_status_id: 10,
		to_status_id: 12,
		title: 'To do → Print ready',
		description: '',
		comment: '',
		mode: 'all',
		status: 'pending',
		allow_self: false,
		due: null,
		resolved: null,
		cancel_reason: '',
		cancel_message: '',
		created: '2026-11-22T10:00:00Z',
		approvers: [
			{id: 1, request_id: 11, user_id: 2, decision: 'approved', comment: 'Fine', decided: '2026-11-22T11:00:00Z', user: {id: 2, username: 'anna'}},
			{id: 2, request_id: 11, user_id: 3, decision: undefined, comment: '', decided: null, user: {id: 3, username: 'bob'}},
		],
		requested_by: {id: 1, username: 'requester'},
		resolved_by: undefined,
		task: {id: 7, title: 'Cover', identifier: 'PUB-1', project_id: 3, project_title: 'Publishing'},
		to_status: {id: 12, title: 'Print ready', color: '#16a34a', category: 'done'},
		...overrides,
	} as ApprovalRequest
}
