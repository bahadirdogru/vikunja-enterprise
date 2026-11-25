import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({taskApprovalsCreate: vi.fn(), projectsUsersSearch: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
// the requester is user 1
const auth = vi.hoisted(() => ({info: {id: 1, username: 'requester'}, isLinkShareAuth: false, settings: {frontend_settings: {}, timezone: 'UTC', language: 'en'}}))
vi.mock('@/stores/auth', () => ({useAuthStore: () => auth}))
vi.mock('@/message', async importOriginal => ({...await importOriginal<typeof import('@/message')>(), error: vi.fn(), success: vi.fn()}))

import AdHocApprovalDialog from './AdHocApprovalDialog.vue'
import {approvalFixture} from './testing'
import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

async function mountDialog() {
	const pinia = createPinia()
	sdk.projectsUsersSearch.mockResolvedValue({data: {items: [{id: 1, username: 'requester'}, {id: 2, username: 'anna'}, {id: 3, username: 'bob', name: 'Bob B'}]}})
	const wrapper = mount(AdHocApprovalDialog, {
		props: {taskId: 7, projectId: 3},
		attachTo: document.body,
		global: {
			plugins: [pinia, i18n, [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]],
			stubs: {Icon: true, Datepicker: true, Teleport: true},
			components: {XButton},
			directives: {tooltip: {}, focus: {}, cy: {}},
		},
	})
	await flushPromises()
	await flushPromises()
	return wrapper
}

const dialog = () => document.body.querySelector('dialog')!
const submit = async () => {
	const button = [...dialog().querySelectorAll('button')].find(b => b.textContent?.trim() === 'Send request')!
	button.click()
	await flushPromises()
	await flushPromises()
}
const typeTitle = async (text: string) => {
	const input = dialog().querySelector<HTMLInputElement>('input[type="text"], input:not([type])')!
	input.value = text
	input.dispatchEvent(new Event('input'))
	await flushPromises()
}
const tick = async (name: string) => {
	const box = [...dialog().querySelectorAll('label')].find(l => l.textContent?.trim() === name)!.querySelector('input')!
	box.click()
	await flushPromises()
}
const alerts = () => [...dialog().querySelectorAll('[role="alert"]')].map(a => a.textContent?.trim())

describe('AdHocApprovalDialog', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		document.body.innerHTML = ''
	})

	it('offers the project members as approvers, but not the requester', async () => {
		await mountDialog()
		const names = [...dialog().querySelectorAll('.adhoc-dialog__members label')].map(l => l.textContent?.trim())
		expect(names).toEqual(['anna', 'Bob B'])
		expect(sdk.projectsUsersSearch).toHaveBeenCalledWith(expect.objectContaining({path: {project: 3}, query: {q: ''}}))
	})

	it('asks for a title and for approvers before it sends anything', async () => {
		await mountDialog()
		await submit()
		expect(alerts()).toHaveLength(2)
		expect(sdk.taskApprovalsCreate).not.toHaveBeenCalled()
	})

	it('creates the request with the chosen approvers and mode, then reports it and closes', async () => {
		sdk.taskApprovalsCreate.mockResolvedValue({data: approvalFixture()})
		const wrapper = await mountDialog()
		await typeTitle('  Sign off the cover ')
		await tick('anna')
		await tick('Bob B')
		const all = [...dialog().querySelectorAll<HTMLInputElement>('input[type="radio"]')].find(r => r.value === 'all')!
		all.click()
		await flushPromises()
		await submit()

		expect(sdk.taskApprovalsCreate).toHaveBeenCalledTimes(1)
		expect(sdk.taskApprovalsCreate.mock.calls[0]![0]).toEqual({
			path: {task: 7},
			body: {title: 'Sign off the cover', description: '', approver_ids: [2, 3], mode: 'all', due: null},
		})
		expect(wrapper.emitted('created')).toHaveLength(1)
		expect(wrapper.emitted('close')).toHaveLength(1)
	})

	it('removes an approver again when the box is unticked', async () => {
		sdk.taskApprovalsCreate.mockResolvedValue({data: approvalFixture()})
		await mountDialog()
		await typeTitle('Sign off')
		await tick('anna')
		await tick('Bob B')
		await tick('anna')
		await submit()
		expect(sdk.taskApprovalsCreate.mock.calls[0]![0].body.approver_ids).toEqual([3])
	})

	it.each([
		[24006, 'Choose at least one person.'],
	])('answers error %i with its own text', async (code, text) => {
		sdk.taskApprovalsCreate.mockRejectedValue(new ApiError({status: 422, code, detail: 'English from the server'}))
		const wrapper = await mountDialog()
		await typeTitle('Sign off')
		await tick('anna')
		await submit()
		expect(alerts()).toContain(text)
		expect(wrapper.emitted('close')).toBeUndefined()
	})

	it('shows the translated text of error 24009, never the English reason of the server, and the generic text for anything else', async () => {
		sdk.taskApprovalsCreate.mockRejectedValueOnce(new ApiError({status: 400, code: 24009, detail: 'every approver must be an active user who can read the task'}))
		const wrapper = await mountDialog()
		await typeTitle('Sign off')
		await tick('anna')
		await submit()
		expect(alerts()).toEqual(['The approval request is not valid.'])

		sdk.taskApprovalsCreate.mockRejectedValueOnce(new ApiError({status: 500, detail: 'boom'}))
		await submit()
		expect(alerts()).toEqual(['Something went wrong on the server. Try again later.\nboom'])
		expect(wrapper.emitted('close')).toBeUndefined()
	})

	it('refuses a title of more than 250 characters (counted as characters) and more than 50 approvers before sending', async () => {
		const wrapper = await mountDialog()
		await typeTitle('😀'.repeat(251))
		await tick('anna')
		await submit()
		expect(alerts()).toEqual(['The title may have at most 250 characters.'])
		expect(sdk.taskApprovalsCreate).not.toHaveBeenCalled()
		expect(wrapper.emitted('created')).toBeUndefined()

		sdk.taskApprovalsCreate.mockResolvedValue({data: approvalFixture()})
		await typeTitle('😀'.repeat(250))
		await submit()
		expect(sdk.taskApprovalsCreate).toHaveBeenCalledTimes(1)
	})

	it('closes without creating anything when cancelled', async () => {
		const wrapper = await mountDialog()
		const cancel = [...dialog().querySelectorAll('button')].find(b => b.textContent?.trim() === 'Cancel')!
		cancel.click()
		expect(wrapper.emitted('close')).toHaveLength(1)
		expect(sdk.taskApprovalsCreate).not.toHaveBeenCalled()
	})
})
