import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({taskStatusHistoryList: vi.fn(), taskApprovalsList: vi.fn()}))
vi.mock('@/client/generated', () => sdk)

import TaskStatusHistory from './TaskStatusHistory.vue'
import en from '@/i18n/lang/en.json'
import trTR from '@/i18n/lang/tr-TR.json'
import {approvalFixture} from '@/components/approvals/testing'
import {focusedApprovalId} from '@/composables/useApprovalFocus'

enableAutoUnmount(afterEach)

async function settle() {
	for (let i = 0; i < 4; i++) await flushPromises()
}

const entry = (over: Record<string, unknown> = {}) => ({
	id: 1, from_status_id: 10, to_status_id: 11, from_title: 'To do', to_title: 'In review',
	doer: {id: 1, username: 'anna', name: 'Anna'}, comment: '', created: '2026-11-20T09:30:00Z',
	approval_request_id: null, ...over,
})

function mountHistory(locale: 'en' | 'tr-TR' = 'en') {
	const i18n = createI18n({legacy: false, locale, fallbackLocale: 'en', messages: {en, 'tr-TR': trTR}})
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(TaskStatusHistory, {
		props: {taskId: 7},
		attachTo: document.body,
		global: {
			plugins: [createPinia(), i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {Icon: true, User: {props: ['user'], template: '<span class="doer">{{ user.username }}</span>'}},
			directives: {tooltip: {}},
		},
	})
}

describe('TaskStatusHistory', () => {
	beforeEach(() => {
		sdk.taskStatusHistoryList.mockReset().mockResolvedValue({data: {items: [], total_pages: 1}})
		sdk.taskApprovalsList.mockReset().mockResolvedValue({data: {items: [], total_pages: 1}})
		focusedApprovalId.value = null
	})

	it('shows a loading line while the history is on its way, then the empty state', async () => {
		const wrapper = mountHistory()
		expect(wrapper.text()).toContain('Loading')
		await settle()
		expect(wrapper.text()).toBe('No status changes yet.')
		expect(wrapper.find('ol').exists()).toBe(false)
	})

	it('asks for the history of the task and lists a change as from -> to with its doer', async () => {
		sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [entry()], total_pages: 1}})
		const wrapper = mountHistory()
		await settle()

		expect(sdk.taskStatusHistoryList).toHaveBeenCalledWith(expect.objectContaining({path: {task: 7}, query: {page: 1}}))
		const line = wrapper.get('.status-history__line')
		expect(line.get('.status-history__from').text()).toBe('To do')
		expect(line.get('strong').text()).toBe('In review')
		expect(wrapper.get('.doer').text()).toBe('anna')
		expect(wrapper.get('time').attributes('datetime')).toBe('2026-11-20T09:30:00Z')
		expect(wrapper.find('.status-history__comment').exists()).toBe(false)
	})

	it('names a task that had no status yet', async () => {
		sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [entry({from_status_id: null, from_title: undefined})], total_pages: 1}})
		const wrapper = mountHistory()
		await settle()
		expect(wrapper.get('.status-history__from').classes()).toContain('is-none')
		expect(wrapper.get('.status-history__from').text()).toBe('No status')
	})

	it('shows the comment left with a change, as plain text', async () => {
		sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [entry({comment: '<b>Looks good</b>'})], total_pages: 1}})
		const wrapper = mountHistory()
		await settle()
		expect(wrapper.get('.status-history__comment').text()).toBe('<b>Looks good</b>')
		expect(wrapper.find('.status-history__comment b').exists()).toBe(false)
	})

	describe('a change the system made because a status was removed', () => {
		const removed = () => entry({id: 2, from_title: 'Legacy', to_title: 'To do', doer: null, comment: '', cause: 'status_removed'})

		it('explains the move with the title of the removed status, in English', async () => {
			sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [removed()], total_pages: 1}})
			const wrapper = mountHistory()
			await settle()
			expect(wrapper.get('.status-history__comment').text()).toBe('Moved because the status "Legacy" was removed from the workflow.')
			expect(wrapper.find('.doer').exists()).toBe(false)
		})

		it('explains the move in Turkish', async () => {
			sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [removed()], total_pages: 1}})
			const wrapper = mountHistory('tr-TR')
			await settle()
			const text = wrapper.get('.status-history__comment').text()
			expect(text).toBe('"Legacy" durumu iş akışından kaldırıldığı için taşındı.')
		})

		it('prefers the explanation over a comment text', async () => {
			sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [{...removed(), comment: 'stale comment'}], total_pages: 1}})
			const wrapper = mountHistory()
			await settle()
			expect(wrapper.text()).not.toContain('stale comment')
			expect(wrapper.text()).toContain('was removed from the workflow')
		})
	})

	describe('a change that an approval caused', () => {
		it('names the approvers who approved it', async () => {
			sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [entry({approval_request_id: 11})], total_pages: 1}})
			sdk.taskApprovalsList.mockResolvedValue({data: {items: [approvalFixture()], total_pages: 1}})
			const wrapper = mountHistory()
			await settle()
			// user 2 approved, user 3 has not decided
			expect(wrapper.get('.status-history__approval').text()).toBe('approved by anna')
		})

		it('falls back to a neutral line when the request is gone', async () => {
			sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [entry({approval_request_id: 99})], total_pages: 1}})
			sdk.taskApprovalsList.mockResolvedValue({data: {items: [approvalFixture()], total_pages: 1}})
			const wrapper = mountHistory()
			await settle()
			expect(wrapper.get('.status-history__approval').text()).toBe('via an approval request')
		})

		it('has no approval line for a change without a request', async () => {
			sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [entry()], total_pages: 1}})
			const wrapper = mountHistory()
			await settle()
			expect(wrapper.find('.status-history__approval').exists()).toBe(false)
		})

		it('points the approvals section of the task at the request when the line is clicked', async () => {
			sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [entry({approval_request_id: 11})], total_pages: 1}})
			sdk.taskApprovalsList.mockResolvedValue({data: {items: [approvalFixture()], total_pages: 1}})
			const wrapper = mountHistory()
			await settle()
			await wrapper.get('.status-history__approval-link').trigger('click')
			await settle()
			expect(focusedApprovalId.value).toBe(11)
		})
	})
})
