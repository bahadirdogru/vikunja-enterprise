import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({approvalsDecide: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn(), getErrorText: () => 'generic error'}))

import ApprovalDecisionDialog from './ApprovalDecisionDialog.vue'
import {approvalFixture} from './testing'
import XButton from '@/components/input/Button.vue'
import {normalizeApproval} from '@/helpers/approvals'
import en from '@/i18n/lang/en.json'

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

function mountDialog(decision: 'approved' | 'rejected') {
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(ApprovalDecisionDialog, {
		props: {request: normalizeApproval(approvalFixture()), decision},
		attachTo: document.body,
		global: {
			plugins: [createPinia(), i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {Icon: true},
			components: {XButton},
			directives: {tooltip: {}},
		},
	})
}

const dialog = () => document.body.querySelector('dialog')!
const submitButton = () => Array.from(dialog().querySelectorAll('button')).find(b => ['Approve', 'Reject'].includes(b.textContent?.trim() ?? ''))!

function type(text: string) {
	const area = dialog().querySelector('textarea')!
	area.value = text
	area.dispatchEvent(new Event('input'))
}

enableAutoUnmount(afterEach)

describe('ApprovalDecisionDialog', () => {
	beforeEach(() => {
		sdk.approvalsDecide.mockReset()
		sdk.approvalsDecide.mockResolvedValue({data: {request: approvalFixture({status: 'approved'})}})
	})

	it('approves without a comment', async () => {
		const wrapper = mountDialog('approved')
		await flushPromises()
		submitButton().click()
		await flushPromises()

		expect(sdk.approvalsDecide).toHaveBeenCalledWith(expect.objectContaining({
			path: {request: 11},
			body: {decision: 'approved', comment: ''},
		}))
		expect(wrapper.emitted('decided')).toHaveLength(1)
		expect(wrapper.emitted('close')).toHaveLength(1)
	})

	it('needs a comment to reject and says so at the field without asking the server', async () => {
		const wrapper = mountDialog('rejected')
		await flushPromises()
		submitButton().click()
		await flushPromises()

		expect(sdk.approvalsDecide).not.toHaveBeenCalled()
		const textarea = dialog().querySelector('textarea')!
		expect(textarea.getAttribute('aria-invalid')).toBe('true')
		expect(dialog().textContent).toContain('A comment is required to reject.')

		type('Wrong cover')
		submitButton().click()
		await flushPromises()
		expect(sdk.approvalsDecide).toHaveBeenCalledWith(expect.objectContaining({body: {decision: 'rejected', comment: 'Wrong cover'}}))
		expect(wrapper.emitted('close')).toHaveLength(1)
	})

	it('maps the server error 24005 to the comment field', async () => {
		sdk.approvalsDecide.mockRejectedValue({status: 422, code: 24005})
		const wrapper = mountDialog('rejected')
		await flushPromises()
		type('x')
		submitButton().click()
		await flushPromises()

		expect(dialog().querySelector('textarea')!.getAttribute('aria-invalid')).toBe('true')
		expect(dialog().textContent).toContain('A comment is required to reject.')
		expect(wrapper.emitted('close')).toBeUndefined()
	})

	it('explains that somebody else was faster and refreshes the lists', async () => {
		sdk.approvalsDecide.mockRejectedValue({status: 409, code: 24002})
		const wrapper = mountDialog('approved')
		await flushPromises()
		submitButton().click()
		await flushPromises()

		expect(dialog().textContent).toContain('This request has already been resolved.')
		expect(wrapper.emitted('decided')).toHaveLength(1)
		expect(wrapper.emitted('close')).toBeUndefined()
	})
})
