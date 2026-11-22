import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {defineComponent, h} from 'vue'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({taskBucketUpdate: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	getErrorText: () => 'generic error',
	translatedError: (key: string) => new Error(key),
}))

import {useStatusColumnMove} from './useStatusColumnMove'
import {kanbanKeys, type BoardData} from '@/client/queries/kanban'
import {error, success} from '@/message'
import en from '@/i18n/lang/en.json'

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

const workflow = {
	id: 1,
	project_id: 3,
	restrict_transitions: false,
	statuses: [
		{id: 10, title: 'To do', color: '', category: 'open', position: 1, is_initial: true, archived: false},
		{id: 12, title: 'Print ready', color: '', category: 'done', position: 2, is_initial: false, archived: false},
	],
	transitions: [],
} as never

const card = {id: 7, title: 'Cover', bucket_id: -10, position: 1}

function board(): BoardData {
	return {
		buckets: [
			{id: -10, title: 'To do', count: 1, limit: 0, position: 1, tasks: [card]},
			{id: -12, title: 'Print ready', count: 0, limit: 0, position: 2, tasks: []},
		],
		pages: {},
	} as never
}

function setup() {
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	queryClient.setQueryData(kanbanKeys.board(3, 5, {}), board())
	let result!: ReturnType<typeof useStatusColumnMove>
	mount(defineComponent({
		setup() {
			result = useStatusColumnMove(3, 5, workflow)
			return () => h('div')
		},
	}), {global: {plugins: [i18n, [VueQueryPlugin, {queryClient}]]}})
	return {move: result, queryClient}
}

const bucketsOf = (queryClient: QueryClient) => queryClient.getQueryData<BoardData>(kanbanKeys.board(3, 5, {}))!.buckets

enableAutoUnmount(afterEach)

describe('useStatusColumnMove', () => {
	beforeEach(() => {
		sdk.taskBucketUpdate.mockReset()
		vi.mocked(error).mockClear()
		vi.mocked(success).mockClear()
	})

	it('moves the card into the column when the status changed', async () => {
		sdk.taskBucketUpdate.mockResolvedValue({data: {task_id: 7, bucket_id: -12, bucket: {id: -12}}})
		const {move, queryClient} = setup()

		expect(await move.move(card as never, -12)).toBe(true)
		await flushPromises()

		expect(bucketsOf(queryClient).find(bucket => bucket.id === -12)!.tasks.map(task => task.id)).toEqual([7])
		expect(success).not.toHaveBeenCalled()
	})

	it('keeps the card where it was, says that an approval was requested and marks the card on a 202', async () => {
		sdk.taskBucketUpdate.mockResolvedValue({data: {
			task_id: 7,
			bucket_id: -12,
			bucket: {id: -10},
			approval: {id: 4, status: 'pending', task_id: 7},
		}})
		const {move, queryClient} = setup()

		expect(await move.move(card as never, -12)).toBe(false)
		await flushPromises()

		const buckets = bucketsOf(queryClient)
		expect(buckets.find(bucket => bucket.id === -10)!.tasks.map(task => task.id)).toEqual([7])
		expect(buckets.find(bucket => bucket.id === -12)!.tasks).toEqual([])
		expect(buckets.find(bucket => bucket.id === -10)!.tasks[0]!.approvals).toEqual({has_pending: true, pending_count: 1})
		expect(success).toHaveBeenCalledWith({message: 'Approval requested'})
		expect(error).not.toHaveBeenCalled()
		expect(move.dialog.value).toBeNull()
	})
})
