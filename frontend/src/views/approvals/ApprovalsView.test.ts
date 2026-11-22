import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {createMemoryHistory, createRouter} from 'vue-router'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({approvalsList: vi.fn(), approvalsDecide: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn(), getErrorText: () => 'generic error'}))
vi.mock('@/stores/auth', () => ({
	useAuthStore: () => ({info: {id: 3}, isLinkShareAuth: false, settings: {frontend_settings: {}, timezone: 'UTC', language: 'en'}}),
}))
vi.mock('@/composables/useProjects', () => ({
	useProjects: () => ({projectsArray: [{id: 3, title: 'Publishing'}, {id: 4, title: 'Marketing'}]}),
}))
vi.mock('@/composables/useTitle', () => ({useTitle: () => undefined}))

import ApprovalsView from './ApprovalsView.vue'
import {approvalFixture} from '@/components/approvals/testing'
import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

async function mountPage(path = '/approvals') {
	const router = createRouter({
		history: createMemoryHistory(),
		routes: [
			{path: '/approvals', name: 'approvals.index', component: ApprovalsView},
			{path: '/tasks/:id', name: 'task.detail', component: {template: '<div />'}},
		],
	})
	await router.push(path)
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	const wrapper = mount(ApprovalsView, {
		attachTo: document.body,
		global: {
			plugins: [createPinia(), i18n, router, [VueQueryPlugin, {queryClient}]],
			stubs: {Icon: true},
			components: {XButton},
			directives: {tooltip: {}},
		},
	})
	for (let i = 0; i < 4; i++) await flushPromises()
	return {wrapper, router}
}

// The sidebar count asks for one item; the list asks for a page.
const queries = () => sdk.approvalsList.mock.calls.map(call => call[0].query).filter(query => query.per_page !== 1)

enableAutoUnmount(afterEach)

describe('ApprovalsView', () => {
	beforeEach(() => {
		sdk.approvalsList.mockReset()
		sdk.approvalsDecide.mockReset()
		sdk.approvalsList.mockImplementation(async ({query}: {query: {per_page?: number}}) => ({
			data: {items: query.per_page === 1 ? [] : [approvalFixture()], total: 4, total_pages: 1},
		}))
	})

	it('starts on the requests that wait for me and shows what is asked, by whom, the progress and the task', async () => {
		const {wrapper} = await mountPage()

		expect(queries()).toContainEqual(expect.objectContaining({status: 'pending', role: 'approver', page: 1, per_page: 20}))
		const row = wrapper.find('[data-cy="approval-row"]')
		expect(row.text()).toContain('PUB-1 Cover')
		expect(row.text()).toContain('Publishing')
		expect(row.text()).toContain('Waiting for approval → Print ready')
		expect(row.text()).toContain('requester')
		expect(row.text()).toContain('1/2 approved')
		expect(row.findAll('button').map(button => button.text())).toEqual(['Approve', 'Reject'])
		// The badge of the tab shows how many requests wait, from the total of the count query.
		expect(wrapper.find('.approvals-page__count').text()).toBe('4')
	})

	it('shows the requests I made without decision buttons', async () => {
		const {wrapper, router} = await mountPage()
		await router.replace({name: 'approvals.index', query: {tab: 'requested'}})
		await flushPromises()

		expect(queries()).toContainEqual(expect.objectContaining({status: 'pending', role: 'requester'}))
		expect(wrapper.find('[data-cy="approval-row"]').exists()).toBe(true)
		expect(wrapper.find('[data-cy="approval-row"] button').exists()).toBe(false)
	})

	it('shows resolved requests of both roles in the history, by outcome', async () => {
		const {wrapper, router} = await mountPage('/approvals?tab=history')
		const query = queries().at(-1)!
		expect(query.status).toBe('approved')
		expect(query.role).toBeUndefined()

		await router.replace({name: 'approvals.index', query: {tab: 'history', outcome: 'rejected'}})
		await flushPromises()
		expect(queries().at(-1)!.status).toBe('rejected')
		expect(wrapper.find('#approvals-outcome').exists()).toBe(true)
	})

	it('filters by project', async () => {
		const {router} = await mountPage('/approvals?project=4')
		expect(queries()).toContainEqual(expect.objectContaining({project_id: 4, role: 'approver'}))
		expect(router.currentRoute.value.query.project).toBe('4')
	})

	it('explains an empty list in one sentence per tab', async () => {
		sdk.approvalsList.mockResolvedValue({data: {items: [], total: 0, total_pages: 0}})
		const {wrapper, router} = await mountPage()
		expect(wrapper.find('.empty-state').text()).toContain('Nothing waits for your decision.')

		await router.replace({name: 'approvals.index', query: {tab: 'requested'}})
		await flushPromises()
		expect(wrapper.find('.empty-state').text()).toContain('You have no open requests.')
	})

	it('opens the decision dialog from a row and decides', async () => {
		sdk.approvalsDecide.mockResolvedValue({data: {request: approvalFixture({status: 'approved'})}})
		const {wrapper} = await mountPage()

		await wrapper.find('[data-cy="approval-row"] button').trigger('click')
		await flushPromises()
		const dialog = document.body.querySelector('dialog')!
		expect(dialog.textContent).toContain('Approve request')
		Array.from(dialog.querySelectorAll('button')).find(button => button.textContent?.trim() === 'Approve')!.click()
		await flushPromises()

		expect(sdk.approvalsDecide).toHaveBeenCalledWith(expect.objectContaining({body: {decision: 'approved', comment: ''}}))
	})
})
