import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'

const sdk = vi.hoisted(() => ({automationSecretsList: vi.fn(), automationSecretsSet: vi.fn(), automationSecretsDelete: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn(), getErrorText: () => 'error'}))

import SecretsSection from './SecretsSection.vue'
import {mountAutomation} from './testing'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

function mountSection() {
	return mountAutomation(SecretsSection, {props: {projectId: 3}})
}

async function settle() {
	for (let i = 0; i < 5; i++) await flushPromises()
}

const list = [
	{id: 1, name: 'CHAT_TOKEN', created: '2026-11-01T10:00:00Z', updated: '2026-11-10T10:00:00Z'},
	{id: 2, name: 'OLD_KEY', created: '2026-10-01T10:00:00Z', updated: '2026-10-01T10:00:00Z', unreadable: true},
]
const dialogButton = (text: string) => [...document.body.querySelectorAll<HTMLElement>('dialog xbutton')].find(b => b.textContent?.trim() === text)!

describe('SecretsSection', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.automationSecretsList.mockResolvedValue({data: {items: list, total_pages: 1}})
		sdk.automationSecretsSet.mockResolvedValue({})
		document.body.innerHTML = ''
		setActivePinia(createPinia())
	})

	it('explains that a value is never shown again and lists names with dates, never values', async () => {
		const wrapper = mountSection()
		await settle()
		expect(wrapper.get('h3').text()).toBe('Secrets')
		expect(wrapper.text()).toContain('can never be shown again')
		expect(wrapper.findAll('.secret__name').map(n => n.text())).toEqual(['CHAT_TOKEN', 'OLD_KEY'])
		expect(wrapper.findAll('.secret__dates')).toHaveLength(2)
		expect(wrapper.find('input[type="password"]').exists()).toBe(false)
	})

	it('warns about a secret that cannot be decrypted any more, and only about that one', async () => {
		const wrapper = mountSection()
		await settle()
		const rows = wrapper.findAll('.secret')
		expect(rows[0]!.find('.secret__warning').exists()).toBe(false)
		expect(rows[1]!.get('.secret__warning').text()).toContain('Set the value again')
		expect(rows[1]!.get('[role="img"]').attributes('aria-label')).toBe(rows[1]!.get('.secret__warning').text())
	})

	it('says there are no secrets yet, and shows an error when the list fails', async () => {
		sdk.automationSecretsList.mockResolvedValueOnce({data: {items: [], total_pages: 1}})
		const empty = mountSection()
		await settle()
		expect(empty.text()).toContain('This project has no secrets yet.')

		sdk.automationSecretsList.mockRejectedValue(new ApiError({status: 500, detail: 'x'}))
		const failing = mountSection()
		await settle()
		expect(failing.findAll('.secret')).toHaveLength(0)
		expect(failing.text()).not.toContain('This project has no secrets yet.')
	})

	it('opens the form for a new secret, hides its own button, refuses a name that exists and closes after saving', async () => {
		const wrapper = mountSection()
		await settle()
		await wrapper.findAll('button').find(b => b.text() === 'New secret')!.trigger('click')
		expect(wrapper.findAll('button').find(b => b.text() === 'New secret')).toBeUndefined()
		await wrapper.get('#automation-secret-name').setValue('CHAT_TOKEN')
		await wrapper.get('#automation-secret-value').setValue('v')
		await wrapper.get('form').trigger('submit')
		expect(sdk.automationSecretsSet).not.toHaveBeenCalled()
		expect(wrapper.text()).toContain('exists already')

		await wrapper.get('#automation-secret-name').setValue('NEW_KEY')
		await wrapper.get('#automation-secret-value').setValue('v')
		await wrapper.get('form').trigger('submit')
		await settle()
		expect(sdk.automationSecretsSet).toHaveBeenCalledWith({path: {project: 3, name: 'NEW_KEY'}, body: {value: 'v'}})
		expect(wrapper.find('form').exists()).toBe(false)
	})

	it('replaces the value of one secret through a form that has no name field', async () => {
		const wrapper = mountSection()
		await settle()
		await wrapper.get('button[aria-label="Replace the value of OLD_KEY"]').trigger('click')
		expect(wrapper.findAll('form')).toHaveLength(1)
		expect(wrapper.find('#automation-secret-2-name').exists()).toBe(false)
		await wrapper.get('#automation-secret-2-value').setValue('n')
		await wrapper.get('form').trigger('submit')
		await settle()
		expect(sdk.automationSecretsSet).toHaveBeenCalledWith({path: {project: 3, name: 'OLD_KEY'}, body: {value: 'n'}})
	})

	it('deletes only after a confirmation and closes it', async () => {
		sdk.automationSecretsDelete.mockResolvedValue({})
		const wrapper = mountSection()
		await settle()
		await wrapper.get('button[aria-label="Delete the secret CHAT_TOKEN"]').trigger('click')
		expect(document.body.textContent).toContain('Delete this secret?')
		expect(sdk.automationSecretsDelete).not.toHaveBeenCalled()
		dialogButton('Cancel').click()
		await settle()
		expect(sdk.automationSecretsDelete).not.toHaveBeenCalled()
		await wrapper.get('button[aria-label="Delete the secret CHAT_TOKEN"]').trigger('click')
		dialogButton('Do it!').click()
		await settle()
		expect(sdk.automationSecretsDelete).toHaveBeenCalledWith({path: {project: 3, name: 'CHAT_TOKEN'}})
		expect(document.body.textContent).not.toContain('Delete this secret?')
	})
})
