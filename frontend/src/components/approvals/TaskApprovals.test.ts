import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	taskApprovalsList: vi.fn(),
	approvalsDecide: vi.fn(),
	approvalsCancel: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	getErrorText: () => 'generic error',
}))
const auth = vi.hoisted(() => ({
	info: {id: 3} as {id: number},
	isLinkShareAuth: false,
	settings: {frontend_settings: {}, timezone: 'UTC', language: 'en'},
}))
vi.mock('@/stores/auth', () => ({useAuthStore: () => auth}))

import TaskApprovals from './TaskApprovals.vue'
import {approvalFixture} from './testing'
import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

function mountBanner(items: unknown[], props: Record<string, unknown> = {}) {
	sdk.taskApprovalsList.mockResolvedValue({data: {items, total_pages: 1}})
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(TaskApprovals, {
		props: {taskId: 7, isProjectAdmin: false, ...props},
		attachTo: document.body,
		global: {
			plugins: [createPinia(), i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {Icon: true, RouterLink: true},
			components: {XButton},
			directives: {tooltip: {}},
		},
	})
}

const buttons = (wrapper: ReturnType<typeof mountBanner>) => wrapper.findAll('button').map(button => button.text())

async function settle() {
	for (let i = 0; i < 4; i++) await flushPromises()
}

enableAutoUnmount(afterEach)

describe('TaskApprovals', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		auth.info = {id: 3}
	})

	it('renders nothing for a task without approvals', async () => {
		const wrapper = mountBanner([])
		await settle()
		expect(wrapper.find('.approval-banner').exists()).toBe(false)
	})

	it('shows the title, the requester, the mode, the progress and the state of every approver', async () => {
		const wrapper = mountBanner([approvalFixture()])
		await settle()

		const banner = wrapper.find('.approval-banner')
		expect(banner.find('h3').text()).toBe('Waiting for approval → Print ready')
		expect(banner.text()).toContain('requester')
		expect(banner.text()).toContain('All approvers')
		expect(banner.text()).toContain('1/2 approved')
		const approvers = banner.findAll('.approver')
		expect(approvers).toHaveLength(2)
		expect(approvers[0]!.text()).toContain('Approved')
		expect(approvers[0]!.text()).toContain('Fine')
		expect(approvers[1]!.text()).toContain('Pending')
	})

	it('uses the title of an ad-hoc request', async () => {
		const wrapper = mountBanner([approvalFixture({title: 'Legal check', transition_id: null, to_status_id: null, to_status: undefined})])
		await settle()
		expect(wrapper.find('.approval-banner h3').text()).toBe('Legal check')
	})

	it('offers approve and reject to an approver who has not decided, but no cancel', async () => {
		const wrapper = mountBanner([approvalFixture()])
		await settle()
		expect(buttons(wrapper)).toEqual(expect.arrayContaining(['Approve', 'Reject']))
		expect(buttons(wrapper)).not.toContain('Cancel request')
	})

	it('offers nothing to an approver who has decided already', async () => {
		auth.info = {id: 2}
		const wrapper = mountBanner([approvalFixture()])
		await settle()
		expect(wrapper.find('.approval-banner__actions').exists()).toBe(false)
	})

	it('offers cancel to the requester and to a project admin, not decisions', async () => {
		auth.info = {id: 1}
		const requester = mountBanner([approvalFixture()])
		await settle()
		expect(buttons(requester)).toContain('Cancel request')
		expect(buttons(requester)).not.toContain('Approve')

		auth.info = {id: 9}
		const admin = mountBanner([approvalFixture()], {isProjectAdmin: true})
		await settle()
		expect(buttons(admin)).toContain('Cancel request')
	})

	it('highlights an overdue request', async () => {
		const wrapper = mountBanner([approvalFixture({due: '2020-01-01T00:00:00Z'})])
		await settle()
		expect(wrapper.find('.approval-banner').classes()).toContain('is-overdue')
		expect(wrapper.find('.approval-banner').text()).toContain('overdue')
	})

	it('cancels after a confirmation and tells the task to refresh', async () => {
		auth.info = {id: 1}
		sdk.approvalsCancel.mockResolvedValue({data: {request: approvalFixture({status: 'cancelled'})}})
		const wrapper = mountBanner([approvalFixture()])
		await settle()

		await wrapper.findAll('button').find(button => button.text() === 'Cancel request')!.trigger('click')
		await settle()
		const dialog = document.body.querySelector('dialog')!
		expect(dialog.textContent).toContain('Cancel the request "To do → Print ready"?')
		expect(sdk.approvalsCancel).not.toHaveBeenCalled()

		Array.from(dialog.querySelectorAll('button')).filter(button => button.textContent?.trim() === 'Cancel request').pop()!.click()
		await settle()

		expect(sdk.approvalsCancel).toHaveBeenCalledWith(expect.objectContaining({path: {request: 11}}))
		expect(wrapper.emitted('changed')).toHaveLength(1)
	})

	it('lists resolved approvals in a collapsible section with the localized cancel reason', async () => {
		const resolved = approvalFixture({id: 12, status: 'cancelled', cancel_reason: 'task_moved', resolved: '2026-11-22T12:00:00Z'})
		const approved = approvalFixture({id: 13, status: 'approved', resolved: '2026-11-22T12:00:00Z'})
		const wrapper = mountBanner([resolved, approved])
		await settle()

		expect(wrapper.find('.approval-banner').exists()).toBe(false)
		const toggle = wrapper.find('.approval-history__toggle')
		expect(toggle.text()).toContain('Resolved approvals (2)')
		expect(wrapper.find('.approval-history__list').exists()).toBe(false)

		await toggle.trigger('click')
		const entries = wrapper.findAll('.approval-history__entry')
		expect(entries).toHaveLength(2)
		expect(entries[0]!.text()).toContain('Cancelled')
		expect(entries[0]!.text()).toContain('The task was moved to another project.')
		expect(entries[1]!.find('.outcome-badge').text()).toBe('Approved')
		expect(entries[1]!.text()).toContain('anna approved')
	})
})
