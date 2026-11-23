import {afterEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {defineComponent, h, ref} from 'vue'
import {createMemoryHistory, createRouter, RouterView} from 'vue-router'
import {QueryClient, VueQueryPlugin, queryOptions} from '@tanstack/vue-query'

vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))
vi.mock('@/composables/useProject', () => ({
	useProject: () => ({project: ref({id: 3, max_permission: 2}), error: ref(null)}),
}))
vi.mock('@/composables/useGoBack', () => ({useGoBack: () => vi.fn()}))

// The queries only have to answer; what the view does with the editor is under test.
vi.mock('@/client/queries/automation', () => ({
	automationCatalogQuery: () => queryOptions({queryKey: ['t', 'catalog'], queryFn: async () => ({triggers: [], actions: [], fields: []})}),
	automationRulesQuery: () => queryOptions({queryKey: ['t', 'rules'], queryFn: async () => []}),
}))
vi.mock('@/client/queries/automationSecrets', () => ({
	automationSecretsQuery: () => queryOptions({queryKey: ['t', 'secrets'], queryFn: async () => []}),
}))
vi.mock('@/client/queries/customFields', () => ({
	customFieldsQuery: () => queryOptions({queryKey: ['t', 'fields'], queryFn: async () => []}),
}))
vi.mock('@/client/queries/forms', () => ({
	formsQuery: () => queryOptions({queryKey: ['t', 'forms'], queryFn: async () => []}),
}))
vi.mock('@/client/queries/workflow', () => ({
	workflowQuery: () => queryOptions({queryKey: ['t', 'workflow'], queryFn: async () => ({statuses: []})}),
}))
vi.mock('@/helpers/automationTemplates', () => ({availableTemplates: () => []}))

import ProjectSettingsAutomations from './ProjectSettingsAutomations.vue'

enableAutoUnmount(afterEach)

const state = {dirty: ref(false), addressUncopied: ref(false)}

const editorStub = defineComponent({
	emits: ['close'],
	setup(_, {expose}) {
		expose({dirty: state.dirty, addressUncopied: state.addressUncopied})
		return () => h('div', {class: 'editor'})
	},
})
const listStub = defineComponent({
	emits: ['create'],
	setup(_, {emit}) {
		return () => h('button', {class: 'create', onClick: () => emit('create')})
	},
})
const modalStub = defineComponent({
	props: {enabled: Boolean},
	emits: ['close', 'submit'],
	setup(props, {emit, slots}) {
		return () => props.enabled
			? h('div', {class: 'confirm'}, [
				h('h2', slots.header?.()),
				h('div', {class: 'text'}, slots.text?.()),
				h('button', {class: 'leave', onClick: () => emit('submit')}),
				h('button', {class: 'stay', onClick: () => emit('close')}),
			])
			: null
	},
})
const createEditStub = defineComponent({
	setup(_, {slots}) {
		return () => h('div', [slots.default?.(), slots.footer?.()])
	},
})

async function mountView() {
	state.dirty.value = false
	state.addressUncopied.value = false
	const router = createRouter({
		history: createMemoryHistory(),
		routes: [
			{path: '/', component: ProjectSettingsAutomations, props: {projectId: 3}},
			{path: '/elsewhere', component: {render: () => null}},
		],
	})
	await router.push('/')
	// onBeforeRouteLeave only runs for a view the router renders.
	const wrapper = mount(defineComponent(() => () => h(RouterView)), {
		attachTo: document.body,
		global: {
			plugins: [router, [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]],
			mocks: {$t: (key: string) => key},
			stubs: {
				RuleEditor: editorStub,
				RuleList: listStub,
				SecretsSection: true,
				CreateEdit: createEditStub,
				Modal: modalStub,
				XButton: true,
			},
		},
	})
	await flushPromises()
	await wrapper.get('.create').trigger('click')
	await flushPromises()
	expect(wrapper.find('.editor').exists()).toBe(true)
	return {wrapper, router}
}

describe('ProjectSettingsAutomations leaving the editor', () => {
	it('asks before closing the editor while the one-time webhook address was not copied', async () => {
		const {wrapper} = await mountView()
		state.addressUncopied.value = true
		wrapper.getComponent(editorStub).vm.$emit('close')
		await flushPromises()
		expect(wrapper.get('.confirm h2').text()).toBe('automation.leave.addressTitle')
		expect(wrapper.get('.confirm .text').text()).toBe('automation.leave.addressText')
		expect(wrapper.find('.editor').exists()).toBe(true)

		await wrapper.get('.stay').trigger('click')
		expect(wrapper.find('.confirm').exists()).toBe(false)
		expect(wrapper.find('.editor').exists()).toBe(true)

		wrapper.getComponent(editorStub).vm.$emit('close')
		await flushPromises()
		await wrapper.get('.leave').trigger('click')
		await flushPromises()
		expect(wrapper.find('.editor').exists()).toBe(false)
	})

	it('closes without asking once the address was copied', async () => {
		const {wrapper} = await mountView()
		state.addressUncopied.value = false
		wrapper.getComponent(editorStub).vm.$emit('close')
		await flushPromises()
		expect(wrapper.find('.confirm').exists()).toBe(false)
		expect(wrapper.find('.editor').exists()).toBe(false)
	})

	it('keeps the message about unsaved changes for a dirty draft', async () => {
		const {wrapper} = await mountView()
		state.dirty.value = true
		state.addressUncopied.value = true
		wrapper.getComponent(editorStub).vm.$emit('close')
		await flushPromises()
		expect(wrapper.get('.confirm .text').text()).toBe('automation.leave.text')
	})

	it('asks before the route is left with an address that was not copied', async () => {
		const {wrapper, router} = await mountView()
		state.addressUncopied.value = true
		const navigation = router.push('/elsewhere')
		await flushPromises()
		expect(wrapper.get('.confirm .text').text()).toBe('automation.leave.addressText')
		expect(router.currentRoute.value.path).toBe('/')

		await wrapper.get('.stay').trigger('click')
		await navigation
		expect(router.currentRoute.value.path).toBe('/')

		const again = router.push('/elsewhere')
		await flushPromises()
		await wrapper.get('.leave').trigger('click')
		await again
		expect(router.currentRoute.value.path).toBe('/elsewhere')
	})
})
