import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	workflowRead: vi.fn(), workflowReplace: vi.fn(), workflowDelete: vi.fn(), taskStatusRead: vi.fn(), taskStatusChange: vi.fn(),
	taskStatusHistoryList: vi.fn(), teamsList: vi.fn(), taskCustomFieldValuesSet: vi.fn(), taskCustomFieldValuesList: vi.fn(),
	tasksRead: vi.fn(), patchTasksRead: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => message)
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import {
	changeTaskStatusMutationOptions, deleteWorkflowMutationOptions, normalizeTaskStatus, normalizeWorkflow, normalizeWorkflowStatus,
	replaceWorkflowMutationOptions, saveRequirementValuesMutationOptions, taskStatusHistoryQuery, taskStatusQuery, workflowKeys, workflowQuery,
} from './workflow'
import {customFieldKeys} from './customFields'
import {ApiError} from '@/client/problem'

const run = (client: QueryClient, options: unknown, vars: unknown) => client.getMutationCache().build(client, options as never).execute(vars as never)

beforeEach(() => {
	Object.values(sdk).forEach(mock => mock.mockReset())
	message.error.mockReset()
	message.success.mockReset()
})

describe('normalisation', () => {
	it('fills the defaults of an empty status and of a workflow', () => {
		expect(normalizeWorkflowStatus({} as never)).toEqual({id: 0, title: '', color: '', category: 'open', position: 0, is_initial: false, archived: false})
		expect(normalizeWorkflow({} as never)).toEqual({id: 0, project_id: 0, restrict_transitions: false, statuses: [], transitions: []})
	})

	it('keeps every setting of a transition and reads its approval only when it needs one', () => {
		const wf = normalizeWorkflow({
			id: 1,
			transitions: [
				{id: 1, from_status_id: 10, to_status_id: 11, title: 'Ship', min_permission: 'admin', require_comment: true, required_fields: ['description', 'cf_2'], team_ids: [5], requires_approval: true, approval: {mode: 'all', approver_type: 'user', approver_ids: [3]}},
				{id: 2, from_status_id: 11, to_status_id: 12, requires_approval: false, approval: {mode: 'all'}},
			],
		} as never)
		expect(wf.transitions[0]).toMatchObject({title: 'Ship', min_permission: 'admin', require_comment: true, required_fields: ['description', 'cf_2'], team_ids: [5], requires_approval: true})
		expect(wf.transitions[0]!.approval).not.toBeNull()
		expect(wf.transitions[1]).toMatchObject({min_permission: 'write', required_fields: [], team_ids: [], requires_approval: false, approval: null})
	})

	it('keeps the available transitions, the pending approvals and the entry time of a task status', () => {
		const status = normalizeTaskStatus({
			task_id: 7, workflow_id: 1, status: {id: 10, title: 'To do'}, entered: '2026-11-20T09:00:00Z',
			transitions: [{to_status: {id: 11, title: 'Review'}, require_comment: true, missing_fields: ['description'], requires_approval: true, approval_mode: 'any', approvers: [{id: 2, username: 'anna'}]}],
			pending_approvals: [{id: 4, task_id: 7, status: 'pending', approvers: []}],
		} as never)
		expect(status.entered).toBe('2026-11-20T09:00:00Z')
		expect(status.transitions[0]).toMatchObject({title: '', require_comment: true, missing_fields: ['description'], requires_approval: true, approval_mode: 'any'})
		expect(status.transitions[0]!.to_status.title).toBe('Review')
		expect(status.transitions[0]!.approvers).toEqual([{id: 2, username: 'anna'}])
		expect(status.pending_approvals.map(a => a.id)).toEqual([4])
		expect(normalizeTaskStatus({} as never)).toMatchObject({status: null, entered: null, transitions: [], pending_approvals: []})
	})
})

describe('queries', () => {
	it('reads the workflow of a project and treats error 21001 as "no workflow"', async () => {
		sdk.workflowRead.mockRejectedValueOnce(new ApiError({status: 404, code: 21001, detail: 'none'}))
		const client = new QueryClient()
		expect(await client.fetchQuery(workflowQuery(3))).toBeNull()
		expect(sdk.workflowRead.mock.calls[0]![0].path).toEqual({project: 3})
	})

	it.each([404, 500])('passes any other failure (%i) on', async status => {
		sdk.workflowRead.mockRejectedValue(new ApiError({status, code: 4004, detail: 'x'}))
		await expect(new QueryClient().fetchQuery(workflowQuery(3))).rejects.toBeTruthy()
	})

	it('reads the status of a task, with 21001 meaning no status, and does not run for task 0', async () => {
		sdk.taskStatusRead.mockRejectedValue(new ApiError({status: 404, code: 21001, detail: 'none'}))
		expect(await new QueryClient().fetchQuery(taskStatusQuery(7))).toBeNull()
		expect(taskStatusQuery(0).enabled).toBe(false)
		expect(workflowQuery(0).enabled).toBe(false)
	})

	it('reads every page of the history', async () => {
		sdk.taskStatusHistoryList
			.mockResolvedValueOnce({data: {items: [{id: 1, cause: 'status_removed'}], total_pages: 2}})
			.mockResolvedValueOnce({data: {items: [{id: 2}], total_pages: 2}})
		const entries = await new QueryClient().fetchQuery(taskStatusHistoryQuery(7))
		expect(entries.map(e => [e.id, e.cause])).toEqual([[1, 'status_removed'], [2, '']])
	})
})

describe('saving the workflow', () => {
	it('puts the saved workflow into the cache and marks task statuses and histories stale', async () => {
		sdk.workflowReplace.mockResolvedValue({data: {id: 1, project_id: 3, statuses: [{id: 10, title: 'To do'}]}})
		const client = new QueryClient()
		const spy = vi.spyOn(client, 'invalidateQueries')
		client.setQueryData(workflowKeys.taskStatus(7), {task_id: 7})
		const saved = await run(client, replaceWorkflowMutationOptions(), {projectId: 3, document: {statuses: []}})
		expect(sdk.workflowReplace.mock.calls[0]![0]).toEqual({path: {project: 3}, body: {statuses: []}})
		expect((saved as {statuses: unknown[]}).statuses).toHaveLength(1)
		expect(client.getQueryData(workflowKeys.project(3))).toMatchObject({id: 1})
		expect(spy).toHaveBeenCalledWith({queryKey: workflowKeys.taskStatuses})
		expect(spy).toHaveBeenCalledWith({queryKey: workflowKeys.histories})
		expect(client.getQueryState(workflowKeys.taskStatus(7))?.isInvalidated).toBe(true)
	})

	it('does not toast a refused save, the editor maps 21002 and 21008/21011 itself', async () => {
		sdk.workflowReplace.mockRejectedValue(new ApiError({status: 422, code: 21011, detail: 'too many'}))
		await expect(run(new QueryClient(), replaceWorkflowMutationOptions(), {projectId: 3, document: {}})).rejects.toBeTruthy()
		expect(message.error).not.toHaveBeenCalled()
	})

	it('caches "no workflow" after disabling it', async () => {
		sdk.workflowDelete.mockResolvedValue({})
		const client = new QueryClient()
		client.setQueryData(workflowKeys.project(3), {id: 1})
		await run(client, deleteWorkflowMutationOptions(), {projectId: 3})
		expect(sdk.workflowDelete).toHaveBeenCalledWith({path: {project: 3}})
		expect(client.getQueryData(workflowKeys.project(3))).toBeNull()
	})
})

describe('changing the status of a task', () => {
	it('sends status and comment, and does not toast a rejection (the caller opens a dialog)', async () => {
		sdk.taskStatusChange.mockRejectedValue(new ApiError({status: 422, code: 21007, detail: 'x'}))
		await expect(run(new QueryClient(), changeTaskStatusMutationOptions(), {taskId: 7, toStatusId: 11})).rejects.toBeTruthy()
		expect(sdk.taskStatusChange.mock.calls[0]![0]).toEqual({path: {task: 7}, body: {to_status_id: 11, comment: ''}})
		expect(message.error).not.toHaveBeenCalled()
	})

	it('says "changed" with the new status, or "requested" for an approval (202)', async () => {
		sdk.taskStatusChange.mockResolvedValueOnce({data: {status: {title: 'Review'}}})
		await run(new QueryClient(), changeTaskStatusMutationOptions(), {taskId: 7, toStatusId: 11, comment: 'c'})
		expect(message.success).toHaveBeenLastCalledWith({message: 'workflow.task.changed'})
		sdk.taskStatusChange.mockResolvedValueOnce({data: {approval: {id: 4}}})
		await run(new QueryClient(), changeTaskStatusMutationOptions(), {taskId: 7, toStatusId: 12})
		expect(message.success).toHaveBeenLastCalledWith({message: 'approvals.requested'})
	})

	it('marks the status, the history and the approvals of the task stale', async () => {
		sdk.taskStatusChange.mockResolvedValue({data: {}})
		const client = new QueryClient()
		client.setQueryData(workflowKeys.taskStatus(7), {})
		client.setQueryData(workflowKeys.history(7), [])
		client.setQueryData(workflowKeys.history(8), [])
		await run(client, changeTaskStatusMutationOptions(), {taskId: 7, toStatusId: 11})
		expect(client.getQueryState(workflowKeys.taskStatus(7))?.isInvalidated).toBe(true)
		expect(client.getQueryState(workflowKeys.history(7))?.isInvalidated).toBe(true)
		expect(client.getQueryState(workflowKeys.history(8))?.isInvalidated).toBe(false)
	})
})

describe('saving the values a transition requires', () => {
	it('sets every value in order, without a toast per field, and marks the values stale', async () => {
		sdk.taskCustomFieldValuesSet.mockResolvedValue({data: {}})
		const client = new QueryClient()
		client.setQueryData(customFieldKeys.values(7), [])
		await run(client, saveRequirementValuesMutationOptions(), {taskId: 7, values: [{fieldId: 1, value: 'a'}, {fieldId: 2, value: null}]})
		expect(sdk.taskCustomFieldValuesSet.mock.calls.map(c => [c[0].path.field, c[0].body.value])).toEqual([[1, 'a'], [2, null]])
		expect(message.success).not.toHaveBeenCalled()
		expect(client.getQueryState(customFieldKeys.values(7))?.isInvalidated).toBe(true)
	})

	it('stops at the first refused value', async () => {
		sdk.taskCustomFieldValuesSet.mockRejectedValueOnce(new ApiError({status: 400, detail: 'x'}))
		await expect(run(new QueryClient(), saveRequirementValuesMutationOptions(), {taskId: 7, values: [{fieldId: 1, value: 'a'}, {fieldId: 2, value: 'b'}]})).rejects.toBeTruthy()
		expect(sdk.taskCustomFieldValuesSet).toHaveBeenCalledTimes(1)
	})
})
