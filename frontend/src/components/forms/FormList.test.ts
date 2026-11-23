import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({formsUpdate: vi.fn(), formsDelete: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => ({
	...message,
	getErrorText: (cause: {detail?: string}) => cause.detail ?? 'error',
	translatedError: (key: string) => new Error(key),
}))

import FormList from './FormList.vue'
import {mountForms, options, storedForm} from './testing'

enableAutoUnmount(afterEach)

function mountList(forms = [storedForm()]) {
	return mountForms(FormList, {props: {forms, options}}).wrapper
}

describe('FormList', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		message.success.mockReset()
	})

	it('explains forms and offers presets built from the project when empty', async () => {
		const wrapper = mountList([])
		expect(wrapper.text()).toContain('No forms yet')
		expect(wrapper.text()).toContain('every answer becomes a task')
		await wrapper.findAll('button').find(b => b.text().includes('New form'))!.trigger('click')
		expect(wrapper.emitted('create')).toBeTruthy()
		const presets = wrapper.findAll('.preset')
		expect(presets.map(p => p.find('strong').text())).toEqual(['Simple request', 'Request with a due date', 'Bug report'])
		await presets[2]!.trigger('click')
		const preset = wrapper.emitted('preset')![0]![0] as {id: string, draft: {fields: {target: string}[]}}
		expect(preset.id).toBe('bug')
		expect(preset.draft.fields.map(f => f.target)).toEqual(['title', 'description', 'priority'])
	})

	it('shows title, access, submissions and a warning for a form the system switched off', () => {
		const wrapper = mountList([
			storedForm({access: 'public', submission_count: 1}),
			storedForm({id: 6, title: 'Broken', enabled: false, disabled_reason: 'custom_field_deleted', submission_count: 0}),
		])
		const rows = wrapper.findAll('.fi')
		expect(rows[0]!.text()).toContain('Cover request')
		expect(rows[0]!.text()).toContain('Public')
		expect(rows[0]!.text()).toContain('1 submission')
		expect(rows[1]!.text()).toContain('Members')
		expect(rows[1]!.text()).toContain('0 submissions')
		const warning = rows[1]!.get('[role=img]')
		expect(warning.attributes('aria-label')).toContain('custom field this form asks for was deleted')
		expect(rows[0]!.find('[role=img]').exists()).toBe(false)
	})

	it('toggles immediately with the whole document', async () => {
		sdk.formsUpdate.mockResolvedValue({data: storedForm({enabled: false})})
		const wrapper = mountList()
		await wrapper.get('input[aria-label=\'Accept submissions for the form "Cover request"\']').setValue(false)
		await flushPromises()
		const call = sdk.formsUpdate.mock.calls[0]![0]
		expect(call.path).toEqual({project: 3, form: 5})
		expect(call.body).toMatchObject({enabled: false, title: 'Cover request', access: 'members'})
		expect(call.body.fields).toHaveLength(2)
	})

	it('copies the link and opens it in a new tab', async () => {
		const writeText = vi.fn().mockResolvedValue(undefined)
		Object.defineProperty(navigator, 'clipboard', {value: {writeText}, configurable: true})
		const wrapper = mountList()
		await wrapper.get('button[aria-label=\'Copy the link of "Cover request"\']').trigger('click')
		await flushPromises()
		expect(writeText).toHaveBeenCalledWith(expect.stringContaining('/forms/tok-1'))
		const open = wrapper.get('a[aria-label=\'Open "Cover request" in a new tab\']')
		expect(open.attributes('href')).toContain('/forms/tok-1')
		expect(open.attributes('target')).toBe('_blank')
	})

	it('emits edit, submissions and duplicate', async () => {
		const wrapper = mountList()
		await wrapper.get('button[aria-label=\'Edit "Cover request"\']').trigger('click')
		await wrapper.get('button[aria-label=\'Show the submissions of "Cover request"\']').trigger('click')
		await wrapper.get('button[aria-label=\'Duplicate "Cover request"\']').trigger('click')
		expect(wrapper.emitted('edit')).toHaveLength(1)
		expect(wrapper.emitted('submissions')).toHaveLength(1)
		expect(wrapper.emitted('duplicate')).toHaveLength(1)
	})

	it('confirms before deleting and says the tasks stay', async () => {
		sdk.formsDelete.mockResolvedValue({data: {}})
		const wrapper = mountList()
		await wrapper.get('button[aria-label=\'Delete "Cover request"\']').trigger('click')
		expect(document.body.textContent).toContain('The tasks it created are kept.')
		expect(sdk.formsDelete).not.toHaveBeenCalled()
		const confirm = [...document.body.querySelectorAll('button')].find(b => b.textContent?.includes('Do it!'))!
		confirm.click()
		await flushPromises()
		expect(sdk.formsDelete.mock.calls[0]![0].path).toEqual({project: 3, form: 5})
	})
})
