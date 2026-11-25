import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({formsToken: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => ({
	...message,
	getErrorText: (cause: {detail?: string}) => cause.detail ?? 'error',
	translatedError: (key: string) => new Error(key),
}))

import FormShare from './FormShare.vue'
import {mountForms, storedForm} from './testing'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

function mountShare(props: Record<string, unknown> = {}) {
	return mountForms(FormShare, {props: {form: storedForm({title: 'Cover "request" <b>'}), dirty: false, projectId: 3, access: 'public', ...props}})
}

const link = () => `${window.location.origin}${import.meta.env.BASE_URL.replace(/\/$/, '')}/forms/tok-1`
const dialogButton = (text: string) => [...document.body.querySelectorAll<HTMLElement>('dialog button')].find(b => b.textContent?.trim() === text)!

describe('FormShare', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		message.success.mockReset()
		message.error.mockReset()
		Object.defineProperty(navigator, 'clipboard', {value: {writeText: vi.fn().mockResolvedValue(undefined)}, configurable: true})
	})

	it('asks to save a form that has no link yet', () => {
		const {wrapper} = mountShare({form: null})
		expect(wrapper.text()).toContain('Save the form to get its link.')
		expect(wrapper.find('#fb-s-link').exists()).toBe(false)
	})

	it('shows the public link read-only and says who can use it', () => {
		const {wrapper} = mountShare()
		const input = wrapper.get('#fb-s-link')
		expect((input.element as HTMLInputElement).value).toBe(link())
		expect(input.attributes('readonly')).toBeDefined()
		expect(wrapper.text()).toContain('Anyone who has the link can use it, no account needed.')
	})

	it.each([
		['members', 'Only people who can see this project can use it; they sign in first.'],
		['users', 'Anyone with an account on this server can use it; they sign in first.'],
	])('names the audience of a %s form', (access, text) => {
		expect(mountShare({access}).wrapper.text()).toContain(text)
	})

	it('warns about unsaved changes and about a form that is switched off', () => {
		const {wrapper} = mountShare({dirty: true, form: storedForm({enabled: false})})
		expect(wrapper.text()).toContain('You have unsaved changes. The link shows the last saved version.')
		expect(wrapper.text()).toContain('The form is closed. The link shows that it takes no submissions.')
	})

	it('copies the link and the embed code', async () => {
		const {wrapper} = mountShare()
		await wrapper.findAll('button').find(b => b.text() === 'Copy link')!.trigger('click')
		await flushPromises()
		expect(navigator.clipboard.writeText).toHaveBeenLastCalledWith(link())
		expect(message.success).toHaveBeenCalledTimes(1)

		await wrapper.findAll('button').find(b => b.text().includes('Copy embed'))!.trigger('click')
		await flushPromises()
		const snippet = vi.mocked(navigator.clipboard.writeText).mock.calls.at(-1)![0]
		expect(snippet).toBe(`<iframe src="${link()}" title="Cover request b" width="100%" height="720" style="border:0" loading="lazy"></iframe>`)
		expect((wrapper.get('#fb-s-embed').element as HTMLTextAreaElement).value).toBe(snippet)
	})

	it('shows an error and no success toast when the clipboard is refused', async () => {
		vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('denied'))
		const {wrapper} = mountShare()
		await wrapper.findAll('button').find(b => b.text() === 'Copy link')!.trigger('click')
		await flushPromises()
		expect(message.success).not.toHaveBeenCalled()
		expect(message.error).toHaveBeenCalledTimes(1)
	})

	it('opens the link in a new tab without giving the page access to this one', () => {
		const {wrapper} = mountShare()
		const open = wrapper.findAll('a').find(a => a.attributes('href') === link())!
		expect(open.attributes('target')).toBe('_blank')
		expect(open.attributes('rel')).toContain('noopener')
	})

	it('regenerates the token only after a confirmation and reports the new token', async () => {
		sdk.formsToken.mockResolvedValue({data: {token: 'tok-2'}})
		const {wrapper} = mountShare()
		await wrapper.findAll('button').find(b => b.text() === 'Create a new link')!.trigger('click')
		expect(document.body.textContent).toContain('Create a new link?')
		expect(sdk.formsToken).not.toHaveBeenCalled()

		dialogButton('Do it!').click()
		await flushPromises()
		expect(sdk.formsToken).toHaveBeenCalledWith({path: {project: 3, form: 5}})
		expect(wrapper.emitted('tokenChanged')).toEqual([['tok-2']])
	})

	it('keeps the confirmation open when regenerating fails and reports no new token', async () => {
		sdk.formsToken.mockRejectedValue(new ApiError({status: 500, detail: 'broken'}))
		const {wrapper} = mountShare()
		await wrapper.findAll('button').find(b => b.text() === 'Create a new link')!.trigger('click')
		dialogButton('Do it!').click()
		await flushPromises()
		expect(wrapper.emitted('tokenChanged')).toBeUndefined()
		expect(document.body.textContent).toContain('Create a new link?')
	})

	it('leads to the submissions with their count', async () => {
		const {wrapper} = mountShare({form: storedForm({submission_count: 7})})
		const button = wrapper.findAll('button').find(b => b.text().includes('7'))!
		await button.trigger('click')
		expect(wrapper.emitted('submissions')).toHaveLength(1)
	})
})
