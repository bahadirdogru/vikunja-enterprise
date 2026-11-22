import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'
import {defineComponent, h} from 'vue'

const sdk = vi.hoisted(() => ({reportsTasksOptions: vi.fn(), reportsTasks: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/composables/useProjects', () => ({
	useProjects: () => ({
		projects: {3: {id: 3, title: 'Plan'}},
		projectsArray: [{id: 3, title: 'Plan', is_archived: false}, {id: 5, title: 'Other', is_archived: false}],
	}),
}))
vi.mock('@/components/input/AsyncEditor', () => ({default: {template: '<div class="editor-stub" />'}}))

vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn()}))

import WidgetEditor from './WidgetEditor.vue'
import optionsFixture from '@/helpers/reportOptions.fixture.json'
import {i18n} from '@/components/charts/testing'
import {configFromDraft, emptyDraft} from '@/helpers/dashboardEditor'

enableAutoUnmount(afterEach)

const ViewStub = defineComponent({
	props: ['type', 'config', 'enabled'],
	emits: ['error'],
	setup(props, {emit}) {
		return () => h('div', {class: 'view-stub', 'data-config': JSON.stringify(props.config), 'data-enabled': String(props.enabled), onClick: () => emit('error', {code: 23001, details: {param: 'group_by'}, detail: 'bad group'})})
	},
})

function mountEditor(props: {widget: Record<string, unknown> | null, saveError?: unknown}) {
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(WidgetEditor, {
		attachTo: document.body,
		props: {dashboardProjectId: 3, ...props} as never,
		global: {
			plugins: [i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {
				WidgetView: ViewStub,
				FilterEditor: {props: ['modelValue'], template: '<input class="filter-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)">'},
				Teleport: true,
				Icon: true,
			},
			directives: {tooltip: {}},
		},
	})
}

describe('WidgetEditor', () => {
	beforeEach(() => {
		vi.useFakeTimers({shouldAdvanceTime: true})
		sdk.reportsTasksOptions.mockReset().mockResolvedValue({data: optionsFixture})
	})
	afterEach(() => vi.useRealTimers())

	it('starts at the gallery with the offered types only', async () => {
		const wrapper = mountEditor({widget: null})
		expect(wrapper.findAll('[data-type]').map(b => b.attributes('data-type'))).toEqual([
			'number', 'bar', 'donut', 'line', 'created_vs_done', 'task_list', 'text',
		])
	})

	it('builds the controls from the options endpoint, labels from title keys and custom field titles', async () => {
		const wrapper = mountEditor({widget: null})
		await wrapper.find('[data-type=bar]').trigger('click')
		await flushPromises()
		const group = wrapper.find('[data-control=group_by]')
		const labels = group.findAll('option').map(o => o.text())
		expect(labels).toContain('Assignee')
		expect(labels).toContain('Aşama')
		expect(group.findAll('option').map(o => o.attributes('value'))).toContain('cf_1')
		expect(sdk.reportsTasksOptions).toHaveBeenCalledWith(expect.objectContaining({query: {project_id: 3}}))
	})

	it('previews the unsaved config only when it is complete, after a pause', async () => {
		const wrapper = mountEditor({widget: null})
		await wrapper.find('[data-type=bar]').trigger('click')
		await flushPromises()
		expect(wrapper.find('.view-stub').attributes('data-enabled')).toBe('false')
		await wrapper.find('[data-control=group_by]').setValue('assignee')
		await vi.advanceTimersByTimeAsync(500)
		const stub = wrapper.find('.view-stub')
		expect(stub.attributes('data-enabled')).toBe('true')
		expect(JSON.parse(stub.attributes('data-config')!)).toEqual({request: {project_id: 3, group_by: 'assignee'}})
	})

	it('shows what the server says about a parameter on its control', async () => {
		const wrapper = mountEditor({widget: null})
		await wrapper.find('[data-type=bar]').trigger('click')
		await flushPromises()
		await wrapper.find('.view-stub').trigger('click')
		const control = wrapper.find('[data-control=group_by]')
		expect(control.attributes('aria-invalid')).toBe('true')
		expect(wrapper.text()).toContain('bad group')
	})

	it('maps a failed save onto the control and clears it when the form changes', async () => {
		const wrapper = mountEditor({widget: null})
		await wrapper.find('[data-type=line]').trigger('click')
		await flushPromises()
		await wrapper.setProps({saveError: {code: 23008, details: {param: 'widgets[2].config.request.then_by'}, detail: 'nope then'}})
		expect(wrapper.text()).not.toContain('nope then')
		await wrapper.setProps({saveError: {code: 23008, details: {param: 'widgets[2].config.request.group_by'}, detail: 'nope group'}})
		expect(wrapper.text()).toContain('nope group')
		await wrapper.find('[data-control=group_by]').setValue('status')
		expect(wrapper.text()).not.toContain('nope group')
	})

	it('blocks saving an incomplete form with a message and saves the config otherwise', async () => {
		const wrapper = mountEditor({widget: null})
		await wrapper.find('[data-type=donut]').trigger('click')
		await flushPromises()
		await wrapper.find('[data-test=widget-save]').trigger('click')
		expect(wrapper.emitted('save')).toBeUndefined()
		expect(wrapper.text()).toContain('Choose what to group by.')
		await wrapper.find('[data-control=group_by]').setValue('priority')
		await wrapper.find('[data-test=widget-save]').trigger('click')
		expect(wrapper.emitted('save')![0]![0]).toMatchObject({type: 'donut', config: {request: {project_id: 3, group_by: 'priority'}}})
	})

	it('edits an existing widget without the gallery and keeps its id', async () => {
		const draft = {...emptyDraft('bar', 3), groupBy: 'done'}
		const wrapper = mountEditor({widget: {id: 9, type: 'bar', title: 'Mine', config: configFromDraft(draft), x: 0, y: 0, w: 6, h: 4}})
		await flushPromises()
		expect(wrapper.find('[data-type]').exists()).toBe(false)
		expect((wrapper.find('[data-control=group_by]').element as HTMLSelectElement).value).toBe('done')
		await wrapper.find('[data-test=widget-save]').trigger('click')
		expect(wrapper.emitted('save')![0]![0]).toMatchObject({id: 9, title: 'Mine'})
	})
})
