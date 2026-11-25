import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({customFieldsList: vi.fn(), taskCustomFieldValuesList: vi.fn(), taskCustomFieldValuesSet: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn(), getErrorText: () => 'error'}))
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import TaskCustomFields from './TaskCustomFields.vue'
import en from '@/i18n/lang/en.json'

enableAutoUnmount(afterEach)

const fields = [
	{id: 1, project_id: 3, title: 'Print run', description: '', type: 'number', required: true, position: 1, options: []},
	{id: 2, project_id: 3, title: 'Customer', description: '', type: 'text', required: false, position: 2, options: []},
]

function mountFields(props: Record<string, unknown> = {}) {
	return mount(TaskCustomFields, {
		props: {taskId: 7, projectId: 3, canWrite: true, ...props},
		attachTo: document.body,
		global: {
			plugins: [createPinia(), createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}}), [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]],
			stubs: {Icon: true, Datepicker: true, UserValueInput: true},
			directives: {tooltip: {}},
		},
	})
}

async function settle() {
	for (let i = 0; i < 5; i++) await flushPromises()
}

describe('TaskCustomFields', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.customFieldsList.mockResolvedValue({data: {items: fields, total_pages: 1}})
		sdk.taskCustomFieldValuesList.mockResolvedValue({data: {items: [{custom_field_id: 2, task_id: 7, type: 'text', value: 'ACME'}], total_pages: 1}})
		sdk.taskCustomFieldValuesSet.mockResolvedValue({data: {custom_field_id: 1, task_id: 7, type: 'number', value: 100}})
	})

	it('shows a row for every field of the project with its stored value, and marks an empty required field', async () => {
		const wrapper = mountFields()
		await settle()
		expect(sdk.customFieldsList.mock.calls[0]![0].path).toEqual({project: 3})
		expect(sdk.taskCustomFieldValuesList.mock.calls[0]![0].path).toEqual({task: 7})
		expect(wrapper.findAll('.property').map(p => p.get('.label-text').text())).toEqual(['Print run*', 'Customer'])
		expect(wrapper.get('.property-custom-field-1 .required-mark').attributes('aria-label')).toBe('Required')
		expect(wrapper.find('.property-custom-field-2 .required-mark').exists()).toBe(false)
		expect((wrapper.get('.property-custom-field-2 input').element as HTMLInputElement).value).toBe('ACME')
	})

	it('labels each editor by its row label for assistive technology', async () => {
		const wrapper = mountFields()
		await settle()
		expect(wrapper.get('.property-custom-field-2 input').attributes('aria-labelledby')).toBe('task-property-custom-field-2-label')
		expect(wrapper.get('#task-property-custom-field-2-label').text()).toContain('Customer')
	})

	it('saves a typed value for this task and field', async () => {
		const wrapper = mountFields()
		await settle()
		const input = wrapper.get('.property-custom-field-1 input')
		await input.setValue('100')
		await input.trigger('blur')
		await settle()
		expect(sdk.taskCustomFieldValuesSet.mock.calls[0]![0]).toEqual({path: {task: 7, field: 1}, body: {value: 100}})
	})

	it('clears a set value with null through the remove button', async () => {
		const wrapper = mountFields()
		await settle()
		await wrapper.get('.property-custom-field-2 button.remove').trigger('click')
		await settle()
		expect(sdk.taskCustomFieldValuesSet.mock.calls[0]![0]).toEqual({path: {task: 7, field: 2}, body: {value: null}})
	})

	it('shows only the fields that have a value to a reader without write access, and no controls', async () => {
		const wrapper = mountFields({canWrite: false})
		await settle()
		expect(wrapper.findAll('.property')).toHaveLength(1)
		expect(wrapper.get('input').attributes('readonly')).toBeDefined()
		expect(wrapper.find('button.remove').exists()).toBe(false)
		await wrapper.get('input').trigger('blur')
		expect(sdk.taskCustomFieldValuesSet).not.toHaveBeenCalled()
	})

	it('renders nothing and asks for nothing without a project or a task', async () => {
		const wrapper = mountFields({projectId: 0})
		await settle()
		expect(wrapper.findAll('.property')).toHaveLength(0)
		expect(sdk.customFieldsList).not.toHaveBeenCalled()
		const noTask = mountFields({taskId: 0})
		await settle()
		expect(noTask.findAll('.property')).toHaveLength(0)
	})
})
