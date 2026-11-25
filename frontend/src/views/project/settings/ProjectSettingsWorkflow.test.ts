import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {defineComponent, h, ref} from 'vue'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({workflowRead: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
const project = vi.hoisted(() => ({value: null as unknown, error: null as unknown}))
vi.mock('@/composables/useProject', async () => {
	const {ref} = await import('vue')
	return {useProject: () => ({project: ref(project.value), error: ref(project.error)})}
})
vi.mock('@/composables/useGoBack', () => ({useGoBack: () => vi.fn()}))

import ProjectSettingsWorkflow from './ProjectSettingsWorkflow.vue'
import en from '@/i18n/lang/en.json'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

const editorStub = defineComponent({
	props: ['projectId', 'workflow'],
	setup(props, {expose}) {
		expose({hasWorkflow: false, dirty: false, isActive: false})
		return () => h('div', {class: 'editor', 'data-workflow': JSON.stringify(props.workflow)})
	},
})

function mountPage() {
	return mount(ProjectSettingsWorkflow, {
		props: {projectId: 3},
		attachTo: document.body,
		global: {
			plugins: [i18n, [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]],
			stubs: {
				WorkflowEditor: editorStub,
				CreateEdit: defineComponent({setup: (_, {slots}) => () => h('div', [slots.default?.(), slots.footer?.()])}),
				ErrorMessage: defineComponent({setup: () => () => h('div', {class: 'error-message'})}),
				Icon: true,
			},
		},
	})
}

async function settle() {
	for (let i = 0; i < 4; i++) await flushPromises()
}

describe('ProjectSettingsWorkflow', () => {
	beforeEach(() => {
		sdk.workflowRead.mockReset()
		project.error = null
	})

	it.each([[0, 'read'], [1, 'write']])('tells a user with %i (%s) permission that only admins can edit, and loads nothing', async permission => {
		project.value = {id: 3, max_permission: permission}
		const wrapper = mountPage()
		await settle()
		expect(wrapper.text()).toContain('Only project admins can manage the workflow.')
		expect(wrapper.find('.editor').exists()).toBe(false)
		expect(sdk.workflowRead).not.toHaveBeenCalled()
	})

	it('hands the stored workflow of an admin to the editor', async () => {
		project.value = {id: 3, max_permission: 2}
		sdk.workflowRead.mockResolvedValue({data: {id: 1, project_id: 3, restrict_transitions: false, statuses: [], transitions: []}})
		const wrapper = mountPage()
		await settle()
		expect(sdk.workflowRead).toHaveBeenCalledWith(expect.objectContaining({path: {project: 3}}))
		expect(JSON.parse(wrapper.get('.editor').attributes('data-workflow')!)).toMatchObject({id: 1, project_id: 3})
	})

	it('opens the editor empty for a project without a workflow (error 21001 is not a failure)', async () => {
		project.value = {id: 3, max_permission: 2}
		sdk.workflowRead.mockRejectedValue(new ApiError({status: 404, code: 21001, detail: 'no workflow'}))
		const wrapper = mountPage()
		await settle()
		expect(wrapper.find('.error-message').exists()).toBe(false)
		expect(wrapper.get('.editor').attributes('data-workflow')).toBe('null')
	})

	it('shows the error when the project cannot be loaded', async () => {
		project.value = null
		project.error = new Error('nope')
		const wrapper = mountPage()
		await settle()
		expect(wrapper.find('.error-message').exists()).toBe(true)
		expect(wrapper.find('.editor').exists()).toBe(false)
	})

	it('shows the error when the workflow cannot be loaded', async () => {
		project.value = {id: 3, max_permission: 2}
		sdk.workflowRead.mockRejectedValue(new ApiError({status: 500, detail: 'boom'}))
		const wrapper = mountPage()
		await settle()
		expect(wrapper.find('.error-message').exists()).toBe(true)
		expect(wrapper.find('.editor').exists()).toBe(false)
	})
})
