import {afterEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'
import {ref} from 'vue'

vi.mock('@/composables/useUserSearch', () => ({
	useProjectUserSearch: () => ({
		users: ref([{id: 7, username: 'ada', name: 'Ada Lovelace'}, {id: 8, username: 'alan', name: ''}]),
		isFetching: ref(false),
	}),
}))
vi.mock('@/composables/useLabels', () => ({
	useLabels: () => ({labels: ref([{id: 5, title: 'Urgent'}, {id: 6, title: 'Later'}])}),
}))
vi.mock('@/composables/useProjects', () => ({
	useProjects: () => ({projectsArray: [{id: 1, title: 'Inbox', is_archived: false}, {id: 2, title: 'Old', is_archived: true}]}),
}))
vi.mock('@/components/custom-fields/CustomFieldValueEditor.vue', () => ({
	default: {
		props: ['field', 'modelValue', 'projectId', 'labelledby'],
		emits: ['update:modelValue'],
		template: '<div data-test="cf-editor" :data-type="field.type" :data-label="labelledby" />',
	},
}))

import ParamInput from './ParamInput.vue'
import {customFields, makeContext, mountAutomation} from './testing'

enableAutoUnmount(afterEach)

function mountInput(props: Record<string, unknown>, context = makeContext()) {
	return mountAutomation(ParamInput, {props: {id: 'p', label: 'Param', modelValue: undefined, ...props}, context})
}

describe('ParamInput', () => {
	it('renders text kinds and emits the typed value', async () => {
		const wrapper = mountInput({kind: 'string', modelValue: 'a'})
		const input = wrapper.get('input[type=text]')
		expect((input.element as HTMLInputElement).value).toBe('a')
		await input.setValue('abc')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['abc'])

		const area = mountInput({kind: 'text'})
		expect(area.find('textarea').exists()).toBe(true)
	})

	it('emits numbers and clears to null', async () => {
		const wrapper = mountInput({kind: 'number', modelValue: 2})
		const input = wrapper.get('input[type=number]')
		await input.setValue('3.5')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([3.5])
		await input.setValue('')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
	})

	it('offers yes and no for bool and keeps false', async () => {
		const wrapper = mountInput({kind: 'bool', modelValue: true, required: true})
		const select = wrapper.get('select')
		expect(select.findAll('option').map(o => o.text())).toEqual(['Yes', 'No'])
		await select.setValue('false')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
	})

	it('explains the date syntax per context and flags invalid values', async () => {
		const action = mountInput({kind: 'date', modelValue: '+3d', dateContext: 'action'})
		expect(action.text()).toContain('+3d')
		expect(action.find('input.is-danger').exists()).toBe(false)
		const bad = mountInput({kind: 'date', modelValue: 'tomorrow', dateContext: 'action'})
		expect(bad.find('input.is-danger').exists()).toBe(true)
		const condition = mountInput({kind: 'date', modelValue: '+3d', dateContext: 'condition'})
		expect(condition.find('input.is-danger').exists()).toBe(true)
		expect(condition.text()).toContain('now+3d')
		expect(condition.findAll('datalist option').map(o => o.attributes('value'))).toContain('now')
	})

	it('validates durations', () => {
		expect(mountInput({kind: 'duration', modelValue: '3d'}).find('input.is-danger').exists()).toBe(false)
		expect(mountInput({kind: 'duration', modelValue: '3 days'}).find('input.is-danger').exists()).toBe(true)
	})

	it('renders user choices with keywords and project members', async () => {
		const wrapper = mountInput({kind: 'user', enumValues: ['event_doer', 'task_creator'], modelValue: 'task_creator'})
		const options = wrapper.findAll('option').map(o => o.text())
		expect(options).toEqual(expect.arrayContaining(['The creator of the task', 'Ada Lovelace']))
		await wrapper.get('select').setValue('7')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([7])
		await wrapper.get('select').setValue('event_doer')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['event_doer'])
	})

	it('renders labels, projects (no archived), statuses and custom fields', async () => {
		expect(mountInput({kind: 'label'}).findAll('option').map(o => o.text())).toEqual(['Choose…', 'Urgent', 'Later'])
		expect(mountInput({kind: 'project'}).findAll('option').map(o => o.text())).toEqual(['Choose…', 'Inbox'])
		const status = mountInput({kind: 'status', modelValue: 21})
		expect(status.findAll('option').map(o => o.text())).toEqual(['Choose…', 'To do (Open)', 'Done (Done)'])
		expect((status.get('select').element as HTMLSelectElement).value).toBe('21')
		const field = mountInput({kind: 'custom_field'})
		expect(field.findAll('option').map(o => o.text())).toEqual(['Choose…', 'Print run', 'Stage'])
		await field.get('select').setValue('2')
		expect(field.emitted('update:modelValue')?.at(-1)).toEqual([2])
	})

	it('explains a status param when the project has no workflow', () => {
		const wrapper = mountInput({kind: 'status'}, makeContext({statuses: []}))
		expect(wrapper.find('select').exists()).toBe(false)
		expect(wrapper.text()).toContain('no workflow')
	})

	it('renders enums with localized labels and a humanized fallback', () => {
		const wrapper = mountInput({kind: 'enum', enumValues: ['in_progress', 'brand_new_value'], modelValue: 'in_progress'})
		expect(wrapper.findAll('option').map(o => o.text())).toEqual(['Choose…', 'In progress', 'Brand new value'])
	})

	it('renders a multiple enum as checkboxes', async () => {
		const wrapper = mountInput({kind: 'enum', multiple: true, enumValues: ['title', 'priority'], modelValue: ['title']})
		const boxes = wrapper.findAll('input[type=checkbox]')
		expect(boxes).toHaveLength(2)
		await boxes[1].setValue(true)
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['title', 'priority']])
		await boxes[0].setValue(false)
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
	})

	it('renders lists of a kind with add and remove', async () => {
		const wrapper = mountInput({kind: 'number', multiple: true, modelValue: [1, 2]})
		expect(wrapper.findAll('input[type=number]')).toHaveLength(2)
		await wrapper.findAll('button').find(b => b.text().includes('Add value'))!.trigger('click')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 2, null]])
		await wrapper.get('button[aria-label="Remove value 1"]').trigger('click')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[2]])
	})

	it('offers placeholders of the catalog for templates and inserts at the cursor', async () => {
		const wrapper = mountInput({kind: 'template', modelValue: 'Hi '})
		const options = wrapper.findAll('select option').map(o => o.attributes('value'))
		expect(options).toEqual(expect.arrayContaining(['task.title', 'project.title', 'task.cf.1', 'task.cf.2']))
		expect(options).not.toContain('task.cf.<id>')
		await wrapper.get('select').setValue('task.title')
		await flushPromises()
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['Hi {{task.title}}'])
	})

	it('edits scripts in a monospace textarea with tab support and the API reference', async () => {
		const wrapper = mountInput({kind: 'code', modelValue: 'a'})
		const area = wrapper.get('textarea')
		expect(area.classes()).toContain('code-editor__input')
		await area.trigger('keydown', {key: 'Tab'})
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['a\t'])
		expect(wrapper.text()).toContain('vikunja.tasks.get(id)')
	})

	it('uses a plain input for a secret without a secrets list and a select with one', () => {
		expect(mountInput({kind: 'secret', modelValue: 'TOKEN'}).find('input[type=text]').exists()).toBe(true)
		const wrapper = mountInput({kind: 'secret'}, makeContext({secrets: ['TOKEN', 'KEY']}))
		expect(wrapper.findAll('option').map(o => o.text())).toEqual(['Choose…', 'TOKEN', 'KEY', 'New secret…'])
	})

	it('edits a map as key/value rows', async () => {
		const wrapper = mountInput({kind: 'map', modelValue: {Accept: 'json'}})
		expect(wrapper.findAll('.kv__row')).toHaveLength(1)
		await wrapper.get('input[aria-label="Name of row 1"]').setValue('X-Id')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{'X-Id': 'json'}])
		await wrapper.findAll('button').find(b => b.text().includes('Add row'))!.trigger('click')
		expect(wrapper.findAll('.kv__row')).toHaveLength(2)
		await wrapper.get('button[aria-label="Remove row 1"]').trigger('click')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{}])
	})

	it('uses the custom field value editor for kind any with a field', () => {
		const wrapper = mountInput({kind: 'any', customField: customFields[1], modelValue: 11})
		const editor = wrapper.get('[data-test=cf-editor]')
		expect(editor.attributes('data-type')).toBe('select')
		expect(editor.attributes('data-label')).toBe('p-label')
	})

	it('falls back to a loose input for kind any without a field', async () => {
		const wrapper = mountInput({kind: 'any'})
		await wrapper.get('input').setValue('42')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([42])
		await wrapper.get('input').setValue('true')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([true])
		await wrapper.get('input').setValue('hello')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['hello'])
	})

	it('falls back to a JSON textarea with a note for an unknown kind', async () => {
		const wrapper = mountInput({kind: 'hologram', modelValue: {a: 1}})
		const area = wrapper.get('textarea')
		expect(JSON.parse((area.element as HTMLTextAreaElement).value)).toEqual({a: 1})
		expect(wrapper.text()).toContain('"hologram"')
		await area.setValue('{"b": 2}')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{b: 2}])
		await area.setValue('{oops')
		expect(wrapper.text()).toContain('not valid JSON')
		expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
	})
})
