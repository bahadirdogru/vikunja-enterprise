import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {defineComponent, h} from 'vue'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({customFieldsList: vi.fn(), customFieldsCreate: vi.fn(), customFieldsDelete: vi.fn(), customFieldsUpdate: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
const project = vi.hoisted(() => ({value: null as unknown, error: null as unknown}))
vi.mock('@/composables/useProject', async () => {
	const {ref} = await import('vue')
	return {useProject: () => ({project: ref(project.value), error: ref(project.error)})}
})
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => ({...message, getErrorText: (cause: {detail?: string}) => cause.detail ?? 'error', translatedError: (key: string) => new Error(key)}))
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import ProjectSettingsCustomFields from './ProjectSettingsCustomFields.vue'
import XButton from '@/components/input/Button.vue'
import Modal from '@/components/misc/Modal.vue'
import en from '@/i18n/lang/en.json'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

const stored = [
	{id: 1, project_id: 3, title: 'Print run', description: '', type: 'number', required: true, position: 1, options: []},
	{id: 2, project_id: 3, title: 'Stage', description: '', type: 'select', required: false, position: 2, options: [{id: 11, title: 'Editor', color: '', position: 1}]},
]

function mountPage() {
	return mount(ProjectSettingsCustomFields, {
		props: {projectId: 3},
		attachTo: document.body,
		global: {
			plugins: [createPinia(), i18n, [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]],
			stubs: {
				CreateEdit: defineComponent({setup: (_, {slots}) => () => h('div', [slots.default?.(), slots.footer?.()])}),
				ErrorMessage: defineComponent({setup: () => () => h('div', {class: 'error-message'})}),
				CustomFieldForm: defineComponent({
					props: ['field', 'saving', 'apiError'],
					emits: ['submit', 'cancel'],
					setup: (props, {emit}) => () => h('div', {class: 'form-stub', 'data-field': props.field?.title ?? '', 'data-error': props.apiError ?? ''}, [
						h('button', {class: 'submit', onClick: () => emit('submit', {title: 'Budget', description: '', type: 'text', required: false, options: [], min: '', max: '', precision: '', currency: '', maxRating: 5})}),
						h('button', {class: 'cancel', onClick: () => emit('cancel')}),
					]),
				}),
				Icon: true,
			},
			// Modal is registered globally by the app
			components: {XButton, Modal},
			directives: {tooltip: {}, cy: {}},
		},
	})
}

async function settle() {
	for (let i = 0; i < 4; i++) await flushPromises()
}

const button = (wrapper: ReturnType<typeof mountPage>, label: string) => wrapper.findAll('button').find(b => b.attributes('aria-label') === label)!
const dialogButton = (text: string) => [...document.body.querySelectorAll<HTMLElement>('dialog button')].find(b => b.textContent?.trim() === text)!

describe('ProjectSettingsCustomFields', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.customFieldsList.mockResolvedValue({data: {items: stored, total_pages: 1}})
		project.value = {id: 3, max_permission: 2}
		project.error = null
		document.body.innerHTML = ''
	})

	it.each([0, 1])('tells a user with permission %i that only admins manage custom fields', async permission => {
		project.value = {id: 3, max_permission: permission}
		const wrapper = mountPage()
		await settle()
		expect(wrapper.text()).toContain('Only project admins can manage custom fields.')
		expect(wrapper.find('table').exists()).toBe(false)
		expect(wrapper.find('.form-stub').exists()).toBe(false)
	})

	it('shows an empty state with an add button when the project has no fields', async () => {
		sdk.customFieldsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		const wrapper = mountPage()
		await settle()
		expect(wrapper.find('table').exists()).toBe(false)
		await wrapper.findAll('button').find(b => b.text().includes('Add'))!.trigger('click')
		expect(wrapper.get('.form-stub').attributes('data-field')).toBe('')
	})

	it('lists the fields with their type and a mark for required ones', async () => {
		const wrapper = mountPage()
		await settle()
		const rows = wrapper.findAll('tbody tr')
		expect(rows).toHaveLength(2)
		expect(rows[0]!.text()).toContain('Print run')
		expect(rows[0]!.text()).toContain('Number')
		expect(rows[0]!.find('.tag.is-danger').exists()).toBe(true)
		expect(rows[1]!.text()).toContain('Select')
		expect(rows[1]!.find('.tag').exists()).toBe(false)
	})

	it('creates a field from the form and returns to the list', async () => {
		sdk.customFieldsCreate.mockResolvedValue({data: {...stored[0], id: 9, title: 'Budget', type: 'text'}})
		const wrapper = mountPage()
		await settle()
		await wrapper.findAll('button').find(b => b.text().includes('Add'))!.trigger('click')
		await wrapper.get('.submit').trigger('click')
		await settle()
		expect(sdk.customFieldsCreate).toHaveBeenCalledTimes(1)
		expect(sdk.customFieldsCreate.mock.calls[0]![0]).toMatchObject({path: {project: 3}, body: {title: 'Budget', type: 'text'}})
		expect(wrapper.find('.form-stub').exists()).toBe(false)
	})

	it('keeps the form open and shows the server message when saving fails', async () => {
		sdk.customFieldsCreate.mockRejectedValue(new ApiError({status: 400, code: 20006, detail: 'limit reached'}))
		const wrapper = mountPage()
		await settle()
		await wrapper.findAll('button').find(b => b.text().includes('Add'))!.trigger('click')
		await wrapper.get('.submit').trigger('click')
		await settle()
		expect(wrapper.get('.form-stub').attributes('data-error')).toBe('limit reached')
	})

	it('edits an existing field with its position kept', async () => {
		sdk.customFieldsUpdate.mockResolvedValue({data: stored[1]})
		const wrapper = mountPage()
		await settle()
		await button(wrapper, 'Edit the field "Stage"').trigger('click')
		expect(wrapper.get('.form-stub').attributes('data-field')).toBe('Stage')
		await wrapper.get('.submit').trigger('click')
		await settle()
		expect(sdk.customFieldsUpdate.mock.calls[0]![0]).toMatchObject({path: {project: 3, field: 2}, body: {position: 2}})
	})

	it('deletes only after a confirmation that names the field', async () => {
		sdk.customFieldsDelete.mockResolvedValue({})
		const wrapper = mountPage()
		await settle()
		await button(wrapper, 'Delete the field "Print run"').trigger('click')
		expect(document.body.textContent).toContain('Print run')
		expect(sdk.customFieldsDelete).not.toHaveBeenCalled()
		dialogButton('Do it!').click()
		await settle()
		expect(sdk.customFieldsDelete).toHaveBeenCalledWith(expect.objectContaining({path: {project: 3, field: 1}}))
	})

	it('shows the error component when the fields cannot be loaded', async () => {
		sdk.customFieldsList.mockRejectedValue(new ApiError({status: 500, detail: 'boom'}))
		const wrapper = mountPage()
		await settle()
		expect(wrapper.find('.error-message').exists()).toBe(true)
	})
})
