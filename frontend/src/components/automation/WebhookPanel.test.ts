import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({automationWebhookToken: vi.fn(), automationList: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn(), getErrorText: (cause: {detail?: string}) => cause?.detail ?? 'error'}))
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import WebhookPanel from './WebhookPanel.vue'
import {mountAutomation} from './testing'
import {ApiError} from '@/client/problem'
import {error, success} from '@/message'

enableAutoUnmount(afterEach)

const reveal = {url: 'https://vikunja.example/api/v2/hooks/abc123', token: 'abc123'}

function mountPanel(props: Record<string, unknown> = {}) {
	return mountAutomation(WebhookPanel, {props: {projectId: 3, ruleId: 31, saved: true, dirty: false, reveal: null, ...props}})
}

const rotateButton = (wrapper: ReturnType<typeof mountPanel>) => wrapper.findAll('button').find(b => b.text().includes('new address'))!
const confirm = async () => {
	// Modal renders its buttons through the globally registered XButton, which these tests do not register: they stay plain elements with the click listener.
	const buttons = [...document.body.querySelectorAll<HTMLElement>('dialog xbutton')]
	buttons.find(b => b.textContent?.trim() === 'Do it!')!.click()
	await flushPromises()
}

describe('WebhookPanel', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		vi.mocked(success).mockClear()
		vi.mocked(error).mockClear()
		Object.defineProperty(navigator, 'clipboard', {value: {writeText: vi.fn().mockResolvedValue(undefined)}, configurable: true})
	})

	it('explains that the address is created on save for a new or unsaved rule', () => {
		expect(mountPanel({ruleId: null, saved: false}).text()).toContain('created when you save the rule')
		const unsaved = mountPanel({saved: false})
		expect(unsaved.text()).toContain('created when you save the rule')
		expect(unsaved.find('#automation-webhook-url').exists()).toBe(false)
		expect(unsaved.findAll('button')).toHaveLength(0)
	})

	it('says the address cannot be shown again when this session does not hold it', () => {
		const wrapper = mountPanel()
		expect(wrapper.find('#automation-webhook-url').exists()).toBe(false)
		expect(wrapper.text()).toContain('stored only as a hash')
		expect(wrapper.text()).not.toContain('abc123')
	})

	it('shows a fresh address read-only with the one-time warning, and copies it', async () => {
		const wrapper = mountPanel({reveal})
		const input = wrapper.get('#automation-webhook-url')
		expect((input.element as HTMLInputElement).value).toBe(reveal.url)
		expect(input.attributes('readonly')).toBeDefined()
		expect(wrapper.text()).toContain('cannot be shown again')

		await wrapper.findAll('button').find(b => b.text() === 'Copy')!.trigger('click')
		await flushPromises()
		expect(navigator.clipboard.writeText).toHaveBeenCalledWith(reveal.url)
		expect(wrapper.emitted('copied')).toHaveLength(1)
		expect(success).toHaveBeenCalledWith({message: 'The address was copied.'})
	})

	it('tells the user to copy by hand when the clipboard is refused', async () => {
		vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('denied'))
		const wrapper = mountPanel({reveal})
		await wrapper.findAll('button').find(b => b.text() === 'Copy')!.trigger('click')
		await flushPromises()
		expect(wrapper.emitted('copied')).toBeUndefined()
		expect(error).toHaveBeenCalledTimes(1)
		expect(vi.mocked(error).mock.calls[0]![0]).toMatchObject({message: expect.stringContaining('copy it by hand')})
	})

	it('does not offer to rotate with unsaved changes', () => {
		const wrapper = mountPanel({dirty: true})
		expect(rotateButton(wrapper).attributes('disabled')).toBeDefined()
		expect(wrapper.text()).toContain('Save your changes first.')
	})

	it('asks for a confirmation before it creates a new address, and reveals it once', async () => {
		sdk.automationWebhookToken.mockResolvedValue({data: {url: '/hooks/new1', token: 'new1'}})
		const wrapper = mountPanel({reveal})
		await rotateButton(wrapper).trigger('click')
		expect(document.body.textContent).toContain('Create a new webhook address?')
		expect(sdk.automationWebhookToken).not.toHaveBeenCalled()

		await confirm()
		expect(sdk.automationWebhookToken).toHaveBeenCalledWith({path: {project: 3, rule: 31}})
		const revealed = wrapper.emitted('revealed')!
		expect(revealed).toHaveLength(1)
		expect(revealed[0]![0]).toMatchObject({token: 'new1'})
		expect((revealed[0]![0] as {url: string}).url).toMatch(/\/hooks\/new1$/)
	})

	it('drops the address on screen when rotating fails, and says it may have changed', async () => {
		sdk.automationWebhookToken.mockRejectedValue(new ApiError({status: 500, detail: 'Something broke.'}))
		const wrapper = mountPanel({reveal})
		await rotateButton(wrapper).trigger('click')
		await confirm()

		expect(wrapper.emitted('revealed')).toEqual([[null]])
		const alerts = wrapper.findAll('[role="alert"]').map(a => a.text())
		expect(alerts.some(text => text.includes('may have changed already'))).toBe(true)
		expect(alerts).toContain('Something broke.')
		// no raw toast: the panel answers in its own way
		expect(error).not.toHaveBeenCalled()
	})

	it('does nothing when the confirmation is cancelled', async () => {
		const wrapper = mountPanel({reveal})
		await rotateButton(wrapper).trigger('click')
		const cancel = [...document.body.querySelectorAll<HTMLElement>('dialog xbutton')].find(b => b.textContent?.includes('Cancel'))!
		cancel.click()
		await flushPromises()
		expect(sdk.automationWebhookToken).not.toHaveBeenCalled()
		expect(wrapper.emitted('revealed')).toBeUndefined()
	})
})
