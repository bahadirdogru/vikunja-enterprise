import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({approvalsCancel: vi.fn(), approvalsDecide: vi.fn(), approvalsList: vi.fn(), taskApprovalsCreate: vi.fn(), taskApprovalsList: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => message)
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import {
	APPROVALS_PER_PAGE, approvalCountQuery, approvalKeys, approvalsListQuery, approvalWaitingQuery, cancelApprovalMutationOptions,
	createAdHocApprovalMutationOptions, decideApprovalMutationOptions, taskApprovalsQuery,
} from './approvals'
import {workflowKeys} from './workflow'
import {ApiError} from '@/client/problem'

const run = (client: QueryClient, options: unknown, vars: unknown) => client.getMutationCache().build(client, options as never).execute(vars as never)
const request = (id: number, approvers: {user_id: number, decision?: string}[], over: Record<string, unknown> = {}) => ({
	id, task_id: 7, status: 'pending', mode: 'any', allow_self: false, requested_by: {id: 1}, approvers: approvers.map(a => ({...a, decision: a.decision ?? null})), ...over,
})

beforeEach(() => {
	Object.values(sdk).forEach(mock => mock.mockReset())
	message.error.mockReset()
	message.success.mockReset()
})

describe('approval lists', () => {
	it('asks for one page with the filters that are set and a fixed page size', async () => {
		sdk.approvalsList.mockResolvedValue({data: {items: [request(1, [])], total_pages: 3, page: 2}})
		const client = new QueryClient()
		const result = await client.fetchQuery(approvalsListQuery({status: 'pending', role: 'approver', projectId: 3, page: 2}))
		expect(sdk.approvalsList.mock.calls[0]![0].query).toEqual({status: 'pending', role: 'approver', project_id: 3, page: 2, per_page: APPROVALS_PER_PAGE})
		expect(result.items[0]!.id).toBe(1)
		await client.fetchQuery(approvalsListQuery({}))
		expect(sdk.approvalsList.mock.calls[1]![0].query).toEqual({page: 1, per_page: APPROVALS_PER_PAGE})
	})

	it('keeps pages apart in the cache', () => {
		expect(approvalsListQuery({status: 'pending', page: 1}).queryKey).not.toEqual(approvalsListQuery({status: 'pending', page: 2}).queryKey)
		expect(approvalsListQuery({status: 'pending'}).queryKey).toEqual(approvalsListQuery({status: 'pending', page: 1}).queryKey)
	})

	it('lists only requests the user still has to decide, across all pages', async () => {
		sdk.approvalsList
			.mockResolvedValueOnce({data: {items: [request(1, [{user_id: 5}]), request(2, [{user_id: 5, decision: 'approved'}])], total_pages: 2}})
			.mockResolvedValueOnce({data: {items: [request(3, [{user_id: 6}]), request(4, [{user_id: 5}], {requested_by: {id: 5}})], total_pages: 2}})
		const waiting = await new QueryClient().fetchQuery(approvalWaitingQuery(5, 3))
		// 2 decided already, 3 is for somebody else, 4 is the user's own request
		expect(waiting.map(r => r.id)).toEqual([1])
		expect(sdk.approvalsList.mock.calls[0]![0].query).toEqual({status: 'pending', role: 'approver', project_id: 3, page: 1})
	})

	it('does not run without a user and counts the waiting requests for the badge', async () => {
		expect(approvalWaitingQuery(0).enabled).toBe(false)
		sdk.approvalsList.mockResolvedValue({data: {items: [request(1, [{user_id: 5}]), request(2, [{user_id: 5}])], total_pages: 1}})
		const client = new QueryClient()
		await client.fetchQuery(approvalWaitingQuery(5))
		expect(approvalCountQuery(5).select!(await client.fetchQuery(approvalWaitingQuery(5)))).toBe(2)
		expect(sdk.approvalsList.mock.calls[0]![0].query).not.toHaveProperty('project_id')
	})

	it('reads every request of a task', async () => {
		sdk.taskApprovalsList.mockResolvedValue({data: {items: [request(1, [])], total_pages: 1}})
		const items = await new QueryClient().fetchQuery(taskApprovalsQuery(7))
		expect(items).toHaveLength(1)
		expect(sdk.taskApprovalsList.mock.calls[0]![0].path).toEqual({task: 7})
		expect(taskApprovalsQuery(0).enabled).toBe(false)
	})
})

describe('deciding', () => {
	it('sends the decision and the comment and confirms in the words of the decision', async () => {
		sdk.approvalsDecide.mockResolvedValue({data: {request: request(1, [])}})
		await run(new QueryClient(), decideApprovalMutationOptions(), {requestId: 1, taskId: 7, decision: 'approved', comment: ''})
		expect(sdk.approvalsDecide.mock.calls[0]![0]).toEqual({path: {request: 1}, body: {decision: 'approved', comment: ''}})
		expect(message.success).toHaveBeenLastCalledWith({message: 'approvals.decision.approvedSuccess'})
		await run(new QueryClient(), decideApprovalMutationOptions(), {requestId: 1, taskId: 7, decision: 'rejected', comment: 'no'})
		expect(message.success).toHaveBeenLastCalledWith({message: 'approvals.decision.rejectedSuccess'})
	})

	it('says so when the request was cancelled in the meantime', async () => {
		sdk.approvalsDecide.mockResolvedValue({data: {request: request(1, [], {status: 'cancelled'})}})
		await run(new QueryClient(), decideApprovalMutationOptions(), {requestId: 1, taskId: 7, decision: 'approved', comment: ''})
		expect(message.success).toHaveBeenLastCalledWith({message: 'approvals.decision.cancelledMeanwhile'})
	})

	it('does not toast 24005 (a rejection needs a comment): the dialog shows it', async () => {
		sdk.approvalsDecide.mockRejectedValue(new ApiError({status: 422, code: 24005, detail: 'x'}))
		await expect(run(new QueryClient(), decideApprovalMutationOptions(), {requestId: 1, taskId: 7, decision: 'rejected', comment: ''})).rejects.toBeTruthy()
		expect(message.error).not.toHaveBeenCalled()
	})

	it('marks approvals, the status and history of the task, comments and boards stale after a decision', async () => {
		sdk.approvalsDecide.mockResolvedValue({data: {request: request(1, [])}})
		const client = new QueryClient()
		client.setQueryData(approvalKeys.task(7), [])
		client.setQueryData(approvalKeys.task(8), [])
		client.setQueryData(workflowKeys.taskStatus(7), {})
		client.setQueryData(workflowKeys.history(7), [])
		client.setQueryData(workflowKeys.taskStatus(8), {})
		await run(client, decideApprovalMutationOptions(), {requestId: 1, taskId: 7, decision: 'approved', comment: ''})
		expect(client.getQueryState(approvalKeys.task(7))?.isInvalidated).toBe(true)
		expect(client.getQueryState(workflowKeys.taskStatus(7))?.isInvalidated).toBe(true)
		expect(client.getQueryState(workflowKeys.history(7))?.isInvalidated).toBe(true)
		// the status of another task is untouched
		expect(client.getQueryState(workflowKeys.taskStatus(8))?.isInvalidated).toBe(false)
	})
})

describe('cancelling and ad-hoc requests', () => {
	it('cancels with an optional message and toasts failures', async () => {
		sdk.approvalsCancel.mockResolvedValue({data: {request: request(1, [], {status: 'cancelled'})}})
		await run(new QueryClient(), cancelApprovalMutationOptions(), {requestId: 1, taskId: 7})
		expect(sdk.approvalsCancel.mock.calls[0]![0]).toEqual({path: {request: 1}, body: {message: ''}})
		expect(message.success).toHaveBeenLastCalledWith({message: 'approvals.cancel.success'})
		sdk.approvalsCancel.mockRejectedValue(new ApiError({status: 500, detail: 'x'}))
		await expect(run(new QueryClient(), cancelApprovalMutationOptions(), {requestId: 1, taskId: 7, message: 'm'})).rejects.toBeTruthy()
		expect(message.error).toHaveBeenCalledTimes(1)
	})

	it('creates an ad-hoc request with snake-case approver ids and does not toast its typed errors', async () => {
		sdk.taskApprovalsCreate.mockResolvedValue({data: request(9, [])})
		const saved = await run(new QueryClient(), createAdHocApprovalMutationOptions(), {taskId: 7, title: 'T', description: 'd', approverIds: [2, 3], mode: 'all', due: '2026-12-01T00:00:00Z'})
		expect(sdk.taskApprovalsCreate.mock.calls[0]![0]).toEqual({path: {task: 7}, body: {title: 'T', description: 'd', approver_ids: [2, 3], mode: 'all', due: '2026-12-01T00:00:00Z'}})
		expect((saved as {id: number}).id).toBe(9)
		expect(message.success).toHaveBeenLastCalledWith({message: 'approvals.requested'})

		sdk.taskApprovalsCreate.mockRejectedValue(new ApiError({status: 422, code: 24006, detail: 'x'}))
		await expect(run(new QueryClient(), createAdHocApprovalMutationOptions(), {taskId: 7, title: 'T', description: '', approverIds: [], mode: 'any', due: null})).rejects.toBeTruthy()
		expect(message.error).not.toHaveBeenCalled()
	})
})
