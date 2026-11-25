import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({automationSecretsSet: vi.fn(), automationSecretsList: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn(), getErrorText: (cause: {detail?: string}) => cause.detail ?? 'error'}))
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import SecretForm from './SecretForm.vue'
import {mountAutomation} from './testing'
import {isValidSecretName} from '@/client/queries/automationSecrets'
import {ApiError} from '@/client/problem'
import {success} from '@/message'

enableAutoUnmount(afterEach)

function mountForm(props: Record<string, unknown> = {}) {
	return mountAutomation(SecretForm, {props: {projectId: 3, ...props}})
}

const nameInput = (wrapper: ReturnType<typeof mountForm>) => wrapper.get('#automation-secret-name')
const valueInput = (wrapper: ReturnType<typeof mountForm>) => wrapper.get('#automation-secret-value')

describe('isValidSecretName', () => {
	it.each(['A', '_', 'CHAT_TOKEN', 'a1', '_1', 'a'.repeat(64)])('accepts %s', name => {
		expect(isValidSecretName(name)).toBe(true)
	})

	it.each(['', '1A', 'A-B', 'A B', ' A', 'A ', 'A\n', 'a'.repeat(65), 'TOKENı', 'İSTANBUL', 'A.B', 'secrets.get'])('rejects %j', name => {
		expect(isValidSecretName(name)).toBe(false)
	})
})

describe('SecretForm', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.automationSecretsSet.mockResolvedValue({})
		sdk.automationSecretsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		vi.mocked(success).mockClear()
	})

	it('keeps the value in a password field that browsers do not fill or suggest', () => {
		const wrapper = mountForm()
		expect(valueInput(wrapper).attributes('type')).toBe('password')
		expect(valueInput(wrapper).attributes('autocomplete')).toBe('new-password')
		expect(wrapper.get('form').attributes('autocomplete')).toBe('off')
	})

	it('says nothing about the name until the form was submitted, then asks for it', async () => {
		const wrapper = mountForm()
		expect(wrapper.find('[role="alert"]').exists()).toBe(false)
		await wrapper.get('form').trigger('submit')
		expect(wrapper.findAll('[role="alert"]')).toHaveLength(2)
		expect(nameInput(wrapper).attributes('aria-invalid')).toBe('true')
		expect(sdk.automationSecretsSet).not.toHaveBeenCalled()
	})

	it.each([
		['1TOKEN', 'invalid'],
		['MY-TOKEN', 'invalid'],
		['TAKEN', 'exists'],
	])('shows the %s name error as "%s" and does not send', async (name, kind) => {
		const wrapper = mountForm({existing: ['TAKEN']})
		await nameInput(wrapper).setValue(name)
		await valueInput(wrapper).setValue('s3cret')
		await wrapper.get('form').trigger('submit')
		expect(wrapper.text()).toContain(kind === 'invalid' ? 'letters, digits' : 'exists already')
		expect(sdk.automationSecretsSet).not.toHaveBeenCalled()
		// nothing was sent, but the secret is still not kept around visibly
		expect(wrapper.text()).not.toContain('s3cret')
	})

	it('trims the name, sends name and value to the server, clears the value and reports the name', async () => {
		const wrapper = mountForm()
		await nameInput(wrapper).setValue('  CHAT_TOKEN ')
		await valueInput(wrapper).setValue('s3cret')
		await wrapper.get('form').trigger('submit')
		await flushPromises()

		expect(sdk.automationSecretsSet).toHaveBeenCalledWith({path: {project: 3, name: 'CHAT_TOKEN'}, body: {value: 's3cret'}})
		expect(wrapper.emitted('saved')).toEqual([['CHAT_TOKEN']])
		expect((valueInput(wrapper).element as HTMLInputElement).value).toBe('')
		expect((nameInput(wrapper).element as HTMLInputElement).value).toBe('')
		expect(success).toHaveBeenCalledWith({message: 'automation.secrets.created'})
	})

	it('clears the value and shows the server message when the save fails', async () => {
		sdk.automationSecretsSet.mockRejectedValue(new ApiError({status: 400, detail: 'The secret store is full.'}))
		const wrapper = mountForm()
		await nameInput(wrapper).setValue('CHAT_TOKEN')
		await valueInput(wrapper).setValue('s3cret')
		await wrapper.get('form').trigger('submit')
		await flushPromises()

		expect(wrapper.findAll('[role="alert"]').map(a => a.text())).toEqual(['The secret store is full.'])
		expect(valueInput(wrapper).attributes('aria-invalid')).toBeUndefined()
		expect((valueInput(wrapper).element as HTMLInputElement).value).toBe('')
		expect(wrapper.emitted('saved')).toBeUndefined()
	})

	it('replaces the value of an existing secret without a name field', async () => {
		const wrapper = mountForm({name: 'API_KEY'})
		expect(wrapper.find('#automation-secret-name').exists()).toBe(false)
		expect(wrapper.get('label[for="automation-secret-value"]').text()).toContain('API_KEY')
		await valueInput(wrapper).setValue('n3w')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(sdk.automationSecretsSet).toHaveBeenCalledWith({path: {project: 3, name: 'API_KEY'}, body: {value: 'n3w'}})
		expect(success).toHaveBeenCalledWith({message: 'automation.secrets.replaced'})
	})

	it('refuses an empty value', async () => {
		const wrapper = mountForm({name: 'API_KEY'})
		await wrapper.get('form').trigger('submit')
		expect(wrapper.get('[role="alert"]').text()).toContain('required')
		expect(sdk.automationSecretsSet).not.toHaveBeenCalled()
	})

	it('cancels with Escape and wipes what was typed', async () => {
		const wrapper = mountForm()
		await valueInput(wrapper).setValue('s3cret')
		await wrapper.get('form').trigger('keydown', {key: 'Escape'})
		expect(wrapper.emitted('cancel')).toHaveLength(1)
		expect((valueInput(wrapper).element as HTMLInputElement).value).toBe('')
	})
})
