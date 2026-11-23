import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({formsCreate: vi.fn(), formsUpdate: vi.fn(), formsToken: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	getErrorText: (cause: {detail?: string}) => cause.detail ?? 'error',
	translatedError: (key: string) => new Error(key),
}))
vi.mock('@/components/input/AsyncEditor', () => ({
	default: {
		props: ['modelValue'],
		emits: ['update:modelValue'],
		template: '<textarea data-test="editor" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
	},
}))
vi.mock('@/components/custom-fields/UserValueInput.vue', () => ({
	default: {props: ['modelValue'], template: '<div data-test="user-input" />'},
}))
vi.mock('@/components/custom-fields/CustomFieldValueEditor.vue', () => ({
	default: {
		props: ['field', 'modelValue'],
		emits: ['update:modelValue'],
		template: '<button type="button" data-test="cf-editor" :data-type="field.type" @click="$emit(\'update:modelValue\', 150)">edit</button>',
	},
}))

import FormBuilder from './FormBuilder.vue'
import {labels, mountForms, options, statuses, storedForm} from './testing'
import {ApiError} from '@/client/problem'
import type {FormResponse} from '@/client/queries/forms'

enableAutoUnmount(afterEach)

function mountBuilder(form: FormResponse | null = null, extra: Record<string, unknown> = {}) {
	return mountForms(FormBuilder, {props: {projectId: 3, form, options, statuses, labels, ...extra}}).wrapper
}

async function openAddMenu(wrapper: ReturnType<typeof mountBuilder>) {
	await wrapper.get('#fb-add-field').trigger('click')
}

function addItem(wrapper: ReturnType<typeof mountBuilder>, name: string) {
	return wrapper.findAll('.fb__add-item').find(button => button.text().startsWith(name))!
}

describe('FormBuilder', () => {
	beforeEach(() => Object.values(sdk).forEach(mock => mock.mockReset()))

	it('starts a new form with the title field and a live preview of it', () => {
		const wrapper = mountBuilder()
		expect(wrapper.findAll('li.fe').length).toBe(1)
		expect(wrapper.get('.fb__preview').text()).toContain('Task title')
		expect(wrapper.find('.fb__preview form').exists()).toBe(true)
	})

	it('builds the add menu from the options of the project, each target at most once', async () => {
		const wrapper = mountBuilder(storedForm())
		await openAddMenu(wrapper)
		const names = wrapper.findAll('.fb__add-item').map(button => button.text().replace(/\s+/g, ' '))
		// Built-in targets first, then the custom fields, exactly what the options endpoint listed.
		expect(names.slice(0, 6).map(name => name.split(' ')[0])).toEqual(['Task', 'Description', 'Due', 'Priority', 'Labels', 'Attachments'])
		for (const info of options.targets.filter(target => target.target.startsWith('cf_'))) {
			expect(names).toContain(info.title)
		}
		expect(wrapper.findAll('[role=group] > p').map(p => p.text())).toEqual(['Task fields', 'Custom fields'])
		// Used targets are listed but disabled.
		expect(addItem(wrapper, 'Task title').attributes('disabled')).toBeDefined()
		expect(addItem(wrapper, 'Description').attributes('disabled')).toBeDefined()
		expect(addItem(wrapper, 'Due date').attributes('disabled')).toBeUndefined()

		await addItem(wrapper, 'Stage').trigger('click')
		expect(wrapper.findAll('li.fe').length).toBe(3)
		expect(wrapper.find('#fb-add-list').exists()).toBe(false)
		await openAddMenu(wrapper)
		expect(addItem(wrapper, 'Stage').attributes('disabled')).toBeDefined()
		// The new field shows in the preview as a group of radios.
		expect(wrapper.get('.fb__preview').text()).toContain('Stage')
		expect(wrapper.findAll('.fb__preview input[type=radio]').length).toBe(3)
	})

	it('limits a public form: targets that cannot be asked are presets, others are disabled', async () => {
		const wrapper = mountBuilder(storedForm({access: 'public'}))
		await openAddMenu(wrapper)
		const labelsItem = addItem(wrapper, 'Labels')
		expect(labelsItem.attributes('disabled')).toBeUndefined()
		expect(wrapper.text()).toContain('can only be set by you')
		await labelsItem.trigger('click')
		const field = wrapper.findAll('li.fe')[2]!
		expect(field.text()).toContain('Not shown')
		// The user field of the project cannot be a preset-less public field either way: it stays a normal add.
		await openAddMenu(wrapper)
		expect(addItem(wrapper, 'Reviewer').attributes('disabled')).toBeUndefined()
	})

	it('warns and names the fields that block a public form', async () => {
		const wrapper = mountBuilder(storedForm({
			fields: [
				{target: 'title', label: 'Title', help: '', required: true, hidden: false, default: null},
				{target: 'labels', label: 'Labels', help: '', required: false, hidden: false, default: null, options: [1]},
			] as never,
		}))
		await wrapper.get('#fb-tab-settings').trigger('click')
		expect(wrapper.text()).not.toContain('Anyone with the link can create tasks')
		await wrapper.get('input[name=fb-access][value=public]').setValue(true)
		expect(wrapper.get('.fb__public-warning').text()).toContain('Anyone with the link can create tasks in this project.')
		expect(wrapper.text()).toContain('These fields cannot be on a public form:')
		expect(wrapper.text()).toContain('Labels: cannot be asked on a public form')
	})

	it('explains the hidden field and keeps the title rule visible', async () => {
		const wrapper = mountBuilder(storedForm())
		await wrapper.get('li.fe .fe__toggle').trigger('click')
		const titleField = wrapper.get('li.fe')
		await titleField.get('input[id$=-required]').setValue(false)
		expect(wrapper.get('.fb__rule').text()).toContain('The task needs a title.')
		await titleField.get('input[id$=-hidden]').setValue(true)
		expect(titleField.text()).toContain('Not shown to the submitter. Every task gets the value below.')
		expect(titleField.text()).toContain('{{submitter.name}}')
		expect(titleField.text()).toContain('{{date}}')
	})

	it('updates the preview while typing', async () => {
		const wrapper = mountBuilder(storedForm())
		await wrapper.get('#fb-title').setValue('Printing desk')
		expect(wrapper.get('.fb__preview-title').text()).toBe('Printing desk')
		await wrapper.get('li.fe .fe__toggle').trigger('click')
		await wrapper.get('li.fe input[id$=-label]').setValue('Your wish')
		expect(wrapper.get('.fb__preview').text()).toContain('Your wish')
		await wrapper.get('li.fe input[id$=-help]').setValue('Be brief')
		expect(wrapper.get('.fb__preview').text()).toContain('Be brief')
		// Hidden fields leave the preview.
		await wrapper.findAll('li.fe')[1]!.get('.fe__toggle').trigger('click')
		await wrapper.findAll('li.fe')[1]!.get('input[id$=-hidden]').setValue(true)
		expect(wrapper.get('.fb__preview').text()).not.toContain('Details')
	})

	it('previews without sending anything', async () => {
		const wrapper = mountBuilder(storedForm())
		await wrapper.get('.fb__preview input[type=text]').setValue('x')
		await wrapper.get('.fb__preview form').trigger('submit')
		expect(wrapper.get('.fb__preview').text()).toContain('This is a preview. Nothing was sent.')
		expect(sdk.formsCreate).not.toHaveBeenCalled()
		expect(sdk.formsUpdate).not.toHaveBeenCalled()
	})

	it('moves and removes fields with buttons', async () => {
		const wrapper = mountBuilder(storedForm())
		const names = () => wrapper.findAll('li.fe .fe__name strong').map(el => el.text())
		await wrapper.findAll('li.fe')[0]!.get('button[aria-label^="Move"][aria-label$="down"]').trigger('click')
		expect(names()).toEqual(['Details', 'What do you need?'])
		await wrapper.findAll('li.fe')[0]!.get('button[aria-label^="Remove"]').trigger('click')
		expect(names()).toEqual(['What do you need?'])
		expect(wrapper.find('.fb__rule').exists()).toBe(false)
	})

	it('creates a new form with the document and stays in the builder', async () => {
		sdk.formsCreate.mockResolvedValue({data: storedForm({id: 9, token: 'new-token', title: 'Printing desk'})})
		const wrapper = mountBuilder()
		await wrapper.get('#fb-title').setValue('Printing desk')
		expect((wrapper.vm as unknown as {dirty: boolean}).dirty).toBe(true)
		await (wrapper.vm as unknown as {save: () => Promise<boolean>}).save()
		await flushPromises()
		expect(sdk.formsCreate).toHaveBeenCalledTimes(1)
		const call = sdk.formsCreate.mock.calls[0]![0]
		expect(call.path).toEqual({project: 3})
		expect(call.body).toMatchObject({title: 'Printing desk', access: 'members', enabled: true, fields: [{target: 'title', required: true}]})
		expect((wrapper.vm as unknown as {dirty: boolean}).dirty).toBe(false)
		expect(wrapper.emitted('saved')).toHaveLength(1)
		// The link is known now.
		await wrapper.get('#fb-tab-share').trigger('click')
		expect((wrapper.get('#fb-s-link').element as HTMLInputElement).value).toContain('/forms/new-token')
		// A second save updates.
		sdk.formsUpdate.mockResolvedValue({data: storedForm({id: 9})})
		await wrapper.get('#fb-title').setValue('Printing desk 2')
		await (wrapper.vm as unknown as {save: () => Promise<boolean>}).save()
		await flushPromises()
		expect(sdk.formsUpdate.mock.calls[0]![0].path).toEqual({project: 3, form: 9})
	})

	it('shows client errors at their controls and does not call the API', async () => {
		const wrapper = mountBuilder()
		await wrapper.get('#fb-title').setValue('')
		await (wrapper.vm as unknown as {save: () => Promise<boolean>}).save()
		await flushPromises()
		expect(sdk.formsCreate).not.toHaveBeenCalled()
		expect(wrapper.get('#fb-title-error').text()).toBe('The form needs a title.')
		expect(document.activeElement?.id).toBe('fb-title')
	})

	it('maps a server issue onto the control and the tab it belongs to', async () => {
		sdk.formsUpdate.mockRejectedValue(new ApiError({
			status: 400, detail: 'The form is not valid', code: 25002,
			details: {issues: [{path: 'settings.redirect_url', message: 'must be an http or https address'}]},
		}))
		const wrapper = mountBuilder(storedForm())
		expect(wrapper.get('#fb-panel-settings').isVisible()).toBe(false)
		await (wrapper.vm as unknown as {save: () => Promise<boolean>}).save()
		await flushPromises()
		expect(wrapper.get('#fb-tab-settings').attributes('aria-selected')).toBe('true')
		expect(wrapper.get('#fb-s-redirect_url-help').element.parentElement!.textContent).toContain('must be an http or https address')
		expect(document.activeElement?.id).toBe('fb-s-redirect_url')
		expect(wrapper.get('#fb-tab-settings .fb__tab-badge').text()).toBe('1')
	})

	it('opens the field and focuses the default when the server rejects it', async () => {
		sdk.formsUpdate.mockRejectedValue(new ApiError({
			status: 400, detail: 'The form is not valid', code: 25002,
			details: {issues: [{path: 'fields[1].default', message: 'must be a string'}]},
		}))
		const wrapper = mountBuilder(storedForm())
		// The first tab is current, and the second field starts closed.
		expect(wrapper.findAll('li.fe')[1]!.find('.fe__body').exists()).toBe(false)
		await wrapper.get('#fb-tab-share').trigger('click')
		await (wrapper.vm as unknown as {save: () => Promise<boolean>}).save()
		await flushPromises()
		expect(wrapper.get('#fb-tab-fields').attributes('aria-selected')).toBe('true')
		const field = wrapper.findAll('li.fe')[1]!
		expect(field.find('.fe__body').exists()).toBe(true)
		expect(field.text()).toContain('must be a string')
		expect(document.activeElement?.id).toMatch(/^fb-f-f\d+-default$/)
		expect(field.find('.fe__badge--error').exists()).toBe(true)
	})

	it('shows a problem without issues as a message', async () => {
		sdk.formsUpdate.mockRejectedValue(new ApiError({status: 403, detail: 'Forms are switched off on this server.', code: 25008}))
		const wrapper = mountBuilder(storedForm())
		await (wrapper.vm as unknown as {save: () => Promise<boolean>}).save()
		await flushPromises()
		expect(wrapper.text()).toContain('Forms are switched off on this server.')
	})

	it('hands the default of a custom field to the editor of its type', async () => {
		const wrapper = mountBuilder(storedForm({
			fields: [
				{target: 'title', label: 'Title', help: '', required: true, hidden: false, default: null},
				{target: 'cf_1', label: 'Print run', help: '', required: false, hidden: true, default: null},
			] as never,
		}))
		await wrapper.findAll('li.fe')[1]!.get('.fe__toggle').trigger('click')
		const editor = wrapper.get('[data-test=cf-editor]')
		expect(editor.attributes('data-type')).toBe('number')
		await editor.trigger('click')
		sdk.formsUpdate.mockResolvedValue({data: storedForm()})
		await (wrapper.vm as unknown as {save: () => Promise<boolean>}).save()
		await flushPromises()
		expect(sdk.formsUpdate.mock.calls[0]![0].body.fields[1]).toMatchObject({target: 'cf_1', hidden: true, default: 150})
	})

	it('offers the labels to choose from and defaults among them', async () => {
		const wrapper = mountBuilder(storedForm({
			fields: [
				{target: 'title', label: 'Title', help: '', required: true, hidden: false, default: null},
				{target: 'labels', label: 'Tags', help: '', required: false, hidden: false, default: null},
			] as never,
		}))
		const field = wrapper.findAll('li.fe')[1]!
		await field.get('.fe__toggle').trigger('click')
		const offered = field.findAll('.fe__options input')
		expect(offered.length).toBe(labels.length)
		await offered[0]!.setValue(true)
		// The default picker only shows what is offered.
		expect(field.findAll('.fdi__labels input').length).toBe(1)
	})

	it('only asks for the status when the project has a workflow', async () => {
		const withFlow = mountBuilder(storedForm())
		expect(withFlow.find('#fb-s-initial_status_id').exists()).toBe(true)
		expect(withFlow.findAll('#fb-s-initial_status_id option').map(o => o.text())).toEqual(['The default status of the workflow', 'To do', 'In progress'])
		const without = mountBuilder(storedForm(), {statuses: []})
		expect(without.find('#fb-s-initial_status_id').exists()).toBe(false)
	})

	it('copes with tabs on the keyboard', async () => {
		const wrapper = mountBuilder(storedForm())
		await wrapper.get('[role=tablist]').trigger('keydown', {key: 'ArrowRight'})
		expect(wrapper.get('#fb-tab-settings').attributes('aria-selected')).toBe('true')
		await wrapper.get('[role=tablist]').trigger('keydown', {key: 'End'})
		expect(wrapper.get('#fb-tab-share').attributes('aria-selected')).toBe('true')
	})

	it('confirms before the link is replaced', async () => {
		sdk.formsToken.mockResolvedValue({data: {token: 'fresh'}})
		const wrapper = mountBuilder(storedForm())
		await wrapper.get('#fb-tab-share').trigger('click')
		expect((wrapper.get('#fb-s-embed').element as HTMLTextAreaElement).value).toContain('<iframe')
		expect((wrapper.get('#fb-s-link').element as HTMLInputElement).value).toContain('/forms/tok-1')
		await wrapper.findAll('#fb-panel-share button').find(b => b.text().includes('Create a new link'))!.trigger('click')
		expect(document.body.textContent).toContain('The old link stops working at once.')
		expect(sdk.formsToken).not.toHaveBeenCalled()
		const confirm = [...document.body.querySelectorAll('button')].find(b => b.textContent?.includes('Do it!'))!
		confirm.click()
		await flushPromises()
		expect(sdk.formsToken.mock.calls[0]![0].path).toEqual({project: 3, form: 5})
		// The tab shows the new link at once.
		expect((wrapper.get('#fb-s-link').element as HTMLInputElement).value).toContain('/forms/fresh')
	})

	it('says so when the form has no link yet or has unsaved changes', async () => {
		const fresh = mountBuilder()
		await fresh.get('#fb-tab-share').trigger('click')
		expect(fresh.get('#fb-panel-share').text()).toContain('Save the form to get its link.')
		const saved = mountBuilder(storedForm())
		await saved.get('#fb-title').setValue('Changed')
		await saved.get('#fb-tab-share').trigger('click')
		expect(saved.get('#fb-panel-share').text()).toContain('You have unsaved changes.')
	})
})
