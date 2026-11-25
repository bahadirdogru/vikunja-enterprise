import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {defineComponent, h} from 'vue'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({taskStatusChange: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	getErrorText: (cause: {detail?: string}) => cause.detail ?? 'generic error',
	translatedError: (key: string) => new Error(key),
}))
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import {useStatusTransition} from './useStatusTransition'
import en from '@/i18n/lang/en.json'
import {ApiError} from '@/client/problem'
import {error, success} from '@/message'

enableAutoUnmount(afterEach)

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})
const target = {id: 12, title: 'Done', color: '', category: 'done' as const, position: 3, is_initial: false, archived: false}

function setup(initialTask = 7) {
	let current = initialTask
	const changed = vi.fn()
	let result!: ReturnType<typeof useStatusTransition>
	mount(defineComponent({
		setup() {
			result = useStatusTransition(() => current, changed)
			return () => h('div')
		},
	}), {global: {plugins: [i18n, [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]]}})
	return {flow: result, changed, switchTask: (id: number) => { current = id }}
}

const problem = (code: number, details?: object) => new ApiError({status: 422, code, detail: 'raw english', ...(details ? {details} : {})})

describe('useStatusTransition', () => {
	beforeEach(() => {
		sdk.taskStatusChange.mockReset()
		vi.mocked(error).mockClear()
		vi.mocked(success).mockClear()
	})

	it('sends the status and the comment of the task and reports that it moved', async () => {
		sdk.taskStatusChange.mockResolvedValue({data: {status: target}})
		const {flow, changed} = setup()
		await expect(flow.attempt(target, 'ship it')).resolves.toBe(true)
		expect(sdk.taskStatusChange).toHaveBeenCalledWith({path: {task: 7}, body: {to_status_id: 12, comment: 'ship it'}})
		expect(flow.dialog.value).toBeNull()
		expect(changed).toHaveBeenCalledTimes(1)
		expect(success).toHaveBeenCalledWith({message: 'workflow.task.changed'})
	})

	it('treats an answer with an approval request as "stayed": the task did not move', async () => {
		sdk.taskStatusChange.mockResolvedValue({data: {approval: {id: 4}}})
		const {flow, changed} = setup()
		await expect(flow.attempt(target)).resolves.toBe(false)
		expect(sdk.taskStatusChange.mock.calls[0]![0].body.comment).toBe('')
		expect(success).toHaveBeenCalledWith({message: 'approvals.requested'})
		expect(changed).toHaveBeenCalledTimes(1)
	})

	it('opens the dialog for exactly the missing fields and the comment the server asked for', async () => {
		sdk.taskStatusChange.mockRejectedValue(problem(21007, {missing_fields: ['description', 'cf_3'], comment_required: true, to_status_id: 12}))
		const {flow} = setup()
		await expect(flow.attempt(target)).resolves.toBe(false)
		expect(flow.dialog.value).toEqual({toStatus: target, missingFields: ['description', 'cf_3'], commentRequired: true, problem: null})
		expect(error).not.toHaveBeenCalled()
	})

	it('submits the dialog with its comment, and says the input is still missing when the server asks again', async () => {
		sdk.taskStatusChange.mockRejectedValue(problem(21007, {missing_fields: ['description'], comment_required: false}))
		const {flow} = setup()
		await flow.attempt(target)
		await flow.submitDialog('because')
		expect(sdk.taskStatusChange).toHaveBeenCalledTimes(2)
		expect(sdk.taskStatusChange.mock.calls[1]![0].body).toEqual({to_status_id: 12, comment: 'because'})
		expect(flow.dialog.value?.problem).toBe('Some required fields are still missing.')

		sdk.taskStatusChange.mockResolvedValue({data: {status: target}})
		await flow.submitDialog('because')
		expect(flow.dialog.value).toBeNull()
	})

	it('does nothing when the dialog is submitted while none is open', async () => {
		const {flow} = setup()
		await flow.submitDialog('x')
		expect(sdk.taskStatusChange).not.toHaveBeenCalled()
	})

	it.each([
		[21005, undefined, 'This status change is not allowed by the workflow.'],
		[21006, {reason: 'team'}, 'Only members of the allowed teams can make this status change.'],
		[21006, {reason: 'permission'}, 'You do not have the permission this status change needs.'],
		[21006, undefined, 'You do not have the permission this status change needs.'],
		[21003, undefined, 'This status no longer exists. Reload the task and try again.'],
		[21004, undefined, 'This status no longer exists. Reload the task and try again.'],
	])('answers error %i %j with the text "%s", never the raw server text', async (code, details, key) => {
		sdk.taskStatusChange.mockRejectedValue(problem(code, details))
		const {flow, changed} = setup()
		await expect(flow.attempt(target)).resolves.toBe(false)
		expect(error).toHaveBeenCalledWith({message: key})
		expect(flow.dialog.value).toBeNull()
		expect(changed).toHaveBeenCalledTimes(1)
	})

	it('shows the server text of an error it does not know and closes an open dialog', async () => {
		sdk.taskStatusChange.mockRejectedValueOnce(problem(21007, {missing_fields: ['description']}))
		const {flow} = setup()
		await flow.attempt(target)
		expect(flow.dialog.value).not.toBeNull()
		sdk.taskStatusChange.mockRejectedValueOnce(new ApiError({status: 500, detail: 'database is down'}))
		await flow.submitDialog('x')
		expect(error).toHaveBeenCalledWith({message: 'database is down'})
		expect(flow.dialog.value).toBeNull()
	})

	it('ignores the answer for a task the user has navigated away from', async () => {
		let reject!: (cause: unknown) => void
		sdk.taskStatusChange.mockReturnValue(new Promise((_resolve, rej) => { reject = rej }))
		const {flow, changed, switchTask} = setup(7)
		const pending = flow.attempt(target)
		switchTask(8)
		reject(problem(21007, {missing_fields: ['description']}))
		await expect(pending).resolves.toBe(false)
		expect(flow.dialog.value).toBeNull()
		expect(error).not.toHaveBeenCalled()
		expect(changed).not.toHaveBeenCalled()
		await flushPromises()
	})
})
