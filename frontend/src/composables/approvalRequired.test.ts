import {beforeEach, describe, expect, it, vi} from 'vitest'

const message = vi.hoisted(() => ({
	error: vi.fn(),
	markErrorAnswered: vi.fn(),
}))
vi.mock('@/message', () => message)
vi.mock('@/i18n', () => ({
	i18n: {global: {t: (key: string, params?: Record<string, string>) => params ? `${key} ${JSON.stringify(params)}` : key}},
}))

import {approvalRequestTarget, handleApprovalRequired} from './approvalRequired'

const needsApproval = (details: Record<string, unknown>) => ({code: 24007, details: {task_id: 5, ...details}})

describe('handleApprovalRequired', () => {
	beforeEach(() => {
		message.error.mockReset()
		message.markErrorAnswered.mockReset()
		approvalRequestTarget.value = null
	})

	it('ignores every other error', () => {
		expect(handleApprovalRequired({code: 24002}, {id: 5})).toBe(false)
		expect(handleApprovalRequired(new Error('x'), {id: 5})).toBe(false)
		expect(message.error).not.toHaveBeenCalled()
	})

	it('names the status and offers to request the approval', () => {
		const cause = needsApproval({to_status_id: 7, to_status_title: 'Done', to_status_category: 'done'})
		expect(handleApprovalRequired(cause, {id: 5, project_id: 2})).toBe(true)

		expect(message.markErrorAnswered).toHaveBeenCalledWith(cause)
		const [shown, actions] = message.error.mock.calls[0]
		expect(shown.message).toContain('approvals.doneBlocked.needsDone')
		expect(shown.message).toContain('Done')
		expect(actions.map((action: {title: string}) => action.title)).toEqual(['approvals.doneBlocked.request'])

		expect(approvalRequestTarget.value).toBeNull()
		actions[0].callback()
		expect(approvalRequestTarget.value).toEqual({taskId: 5, projectId: 2, toStatusId: 7})
	})

	it('uses the reopen text when the target is not a final status', () => {
		handleApprovalRequired(needsApproval({to_status_id: 1, to_status_title: 'To do', to_status_category: 'open'}), {id: 5})
		expect(message.error.mock.calls[0][0].message).toContain('approvals.doneBlocked.needsReopen')
	})

	it('says so instead of offering a second request when one is already pending', () => {
		handleApprovalRequired(needsApproval({
			to_status_id: 7,
			to_status_title: 'Done',
			to_status_category: 'done',
			pending_approval_id: 12,
			approval_pending: true,
		}), {id: 5})
		const [shown, actions] = message.error.mock.calls[0]
		expect(shown.message).toContain('approvals.doneBlocked.pending')
		expect(actions.map((action: {title: string}) => action.title)).toEqual(['approvals.doneBlocked.open'])
		expect(approvalRequestTarget.value).toBeNull()
	})

	it('has no request action when the server did not name the status', () => {
		handleApprovalRequired({code: 24007}, {id: 5})
		const [shown, actions] = message.error.mock.calls[0]
		expect(shown.message).toBe('approvals.doneBlocked.needsGeneric')
		expect(actions).toEqual([])
	})
})
