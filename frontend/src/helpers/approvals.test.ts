import {describe, expect, it} from 'vitest'

import {
	APPROVAL_CANCEL_REASONS,
	approvalDraftFromResponse,
	approvalDraftToDocument,
	approvalFieldOfServerMessage,
	approvalProgress,
	canCancel,
	canDecide,
	cancelReasonKey,
	emptyApprovalDraft,
	isDoneRequiresApproval,
	isOverdue,
	isValidDueIn,
	newApproverSource,
	normalizeApproval,
	readApprovalProblem,
	validateApprovalDraft,
	type ApprovalResponse,
} from './approvals'
import en from '@/i18n/lang/en.json'
import trTR from '@/i18n/lang/tr-TR.json'

function request(overrides: Partial<ApprovalResponse> = {}): ApprovalResponse {
	return {
		...normalizeApproval({
			id: 1,
			task_id: 7,
			title: 'Todo → Print',
			mode: 'all',
			status: 'pending',
			requested_by: {id: 1, username: 'req'},
			approvers: [
				{id: 1, user_id: 2, decision: 'approved', user: {id: 2, username: 'a'}},
				{id: 2, user_id: 3, user: {id: 3, username: 'b'}},
			],
		}),
		...overrides,
	}
}

describe('approval state', () => {
	it('lets an approver who has not decided decide, once', () => {
		expect(canDecide(request(), 3)).toBe(true)
		expect(canDecide(request(), 2)).toBe(false) // decided already
		expect(canDecide(request(), 9)).toBe(false) // not an approver
		expect(canDecide(request(), 0)).toBe(false)
	})

	it('does not let anybody decide a resolved request', () => {
		expect(canDecide(request({status: 'approved'}), 3)).toBe(false)
		expect(canDecide(request({status: 'cancelled'}), 3)).toBe(false)
	})

	it('lets the requester decide only when the workflow allows it', () => {
		const own = request({approvers: [{id: 3, user_id: 1, decision: null, comment: '', decided: null, user: null}]})
		expect(canDecide(own, 1)).toBe(false)
		expect(canDecide({...own, allow_self: true}, 1)).toBe(true)
	})

	it('lets the requester and project admins cancel a pending request', () => {
		expect(canCancel(request(), 1, false)).toBe(true)
		expect(canCancel(request(), 5, true)).toBe(true)
		expect(canCancel(request(), 5, false)).toBe(false)
		expect(canCancel(request({status: 'rejected'}), 1, true)).toBe(false)
	})

	it('counts the progress', () => {
		expect(approvalProgress(request())).toEqual({approved: 1, rejected: 0, pending: 1, total: 2})
	})

	it('knows when a pending request is overdue', () => {
		const now = new Date('2026-11-23T12:00:00Z')
		expect(isOverdue(request({due: '2026-11-22T12:00:00Z'}), now)).toBe(true)
		expect(isOverdue(request({due: '2026-11-24T12:00:00Z'}), now)).toBe(false)
		expect(isOverdue(request({due: null}), now)).toBe(false)
		expect(isOverdue(request({due: '2026-11-22T12:00:00Z', status: 'approved'}), now)).toBe(false)
	})
})

describe('cancel reasons', () => {
	it('has a translation for every machine readable reason in both languages', () => {
		for (const messages of [en, trTR]) {
			for (const reason of APPROVAL_CANCEL_REASONS) {
				const key = cancelReasonKey(reason).replace('approvals.cancelReason.', '')
				expect((messages.approvals.cancelReason as Record<string, string>)[key]).toBeTruthy()
			}
		}
	})

	it('falls back for a reason the client does not know', () => {
		expect(cancelReasonKey('something_new')).toBe('approvals.cancelReason.unknown')
		expect(cancelReasonKey('task_moved')).toBe('approvals.cancelReason.task_moved')
	})
})

describe('problems', () => {
	it('reads the typed approval errors', () => {
		expect(readApprovalProblem({code: 24005}).kind).toBe('commentRequired')
		expect(readApprovalProblem({code: 24002}).kind).toBe('notPending')
		expect(readApprovalProblem({code: 24004}).kind).toBe('alreadyDecided')
		expect(readApprovalProblem({code: 24007, details: {to_status_id: 4}})).toEqual({
			kind: 'doneRequiresApproval',
			taskId: 0,
			fromStatusTitle: '',
			toStatusId: 4,
			toStatusTitle: '',
			toStatusCategory: '',
			pendingApprovalId: 0,
			requestable: true,
		})
		expect(readApprovalProblem({code: 24007, details: {to_status_id: 4, requestable: false}})).toMatchObject({requestable: false})
		expect(readApprovalProblem({
			code: 24007,
			details: {
				task_id: 9,
				from_status_title: 'To do',
				to_status_id: 4,
				to_status_title: 'Done',
				to_status_category: 'done',
				pending_approval_id: 12,
				approval_pending: true,
			},
		})).toEqual({
			kind: 'doneRequiresApproval',
			taskId: 9,
			fromStatusTitle: 'To do',
			toStatusId: 4,
			toStatusTitle: 'Done',
			toStatusCategory: 'done',
			pendingApprovalId: 12,
			requestable: true,
		})
		expect(readApprovalProblem({code: 1})).toEqual({kind: 'other'})
		expect(isDoneRequiresApproval({code: 24007})).toBe(true)
	})
})

describe('transition approval draft', () => {
	it('round-trips between the response and the document', () => {
		const draft = approvalDraftFromResponse({
			mode: 'all',
			approvers: [{type: 'user', id: 2}, {type: 'assignees', id: 0}, {type: 'team', id: 4}],
			rejected_status_id: 3,
			allow_self: true,
			due_in: '2d',
		})
		expect(draft.approvers.map(source => source.key)).toHaveLength(3)
		expect(approvalDraftToDocument(draft)).toEqual({
			mode: 'all',
			approvers: [{type: 'user', id: 2}, {type: 'assignees'}, {type: 'team', id: 4}],
			rejected_status_id: 3,
			allow_self: true,
			due_in: '2d',
		})
	})

	it('leaves out an empty rejected status and duration and removes duplicate sources', () => {
		const draft = {
			...emptyApprovalDraft(),
			approvers: [newApproverSource('user', 2), newApproverSource('user', 2), newApproverSource('assignees', 9)],
		}
		expect(approvalDraftToDocument(draft)).toEqual({
			mode: 'any',
			approvers: [{type: 'user', id: 2}, {type: 'assignees'}],
			allow_self: false,
		})
	})

	it('validates like the server', () => {
		expect(validateApprovalDraft(emptyApprovalDraft(), 5)).toEqual(['approversRequired'])
		const draft = {...emptyApprovalDraft(), approvers: [newApproverSource('team', 0)], due_in: '3 days', rejected_status_id: 5}
		expect(validateApprovalDraft(draft, 5)).toEqual(['sourceIncomplete', 'dueInvalid', 'rejectedStatusSame'])
		expect(validateApprovalDraft({...draft, approvers: [newApproverSource('assignees')], due_in: '12h', rejected_status_id: 2}, 5)).toEqual([])
	})

	it('accepts the duration formats of the server', () => {
		for (const ok of ['12h', '2d', '1w', '30m', '+3d']) expect(isValidDueIn(ok)).toBe(true)
		for (const bad of ['', '0d', '-2d', '2 d', 'd', '1.5d', '2y']) expect(isValidDueIn(bad)).toBe(false)
	})

	it('maps a server message to the control it is about', () => {
		expect(approvalFieldOfServerMessage('approval due_in must be a positive duration like 12h, 3d or 2w')).toBe('dueIn')
		expect(approvalFieldOfServerMessage('the rejected status must differ from the target status')).toBe('rejectedStatus')
		expect(approvalFieldOfServerMessage('approver user 5 does not exist or cannot read the project')).toBe('approvers')
		expect(approvalFieldOfServerMessage('a status needs a title')).toBeNull()
	})
})
