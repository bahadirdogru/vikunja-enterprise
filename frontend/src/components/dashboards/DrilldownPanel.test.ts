import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {defineComponent, h} from 'vue'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	projectTasksList: vi.fn(), tasksList: vi.fn(), projectViewTasksList: vi.fn(), reportsTasksOptions: vi.fn(), reportsTasksExportRows: vi.fn(),
	usersList: vi.fn(), customFieldsList: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => message)
const download = vi.hoisted(() => ({downloadBlob: vi.fn()}))
vi.mock('@/helpers/downloadBlob', () => download)

import DrilldownPanel from './DrilldownPanel.vue'
import en from '@/i18n/lang/en.json'

enableAutoUnmount(afterEach)

const range = {bucket: '2026-11', start: '2026-11-01T00:00:00+03:00', end: '2026-12-01T00:00:00+03:00', value: 5}
const group = (key: unknown, label: string, extra: object = {}) => ({key, label, color: '', value: 4, none: false, other: false, children: [], series: [], ...extra})

const SidePanelStub = defineComponent({
	props: ['title'],
	emits: ['close'],
	setup: (props, {slots, emit}) => () => h('aside', {class: 'panel'}, [h('h2', props.title), slots.default?.(), h('button', {class: 'close', onClick: () => emit('close')})]),
})

function mountPanel(props: Record<string, unknown>) {
	return mount(DrilldownPanel, {
		props: {type: 'bar', config: {request: {project_id: 3, filter: 'done = false', group_by: 'status'}}, title: 'By status', selection: {group: group(21, 'In progress')}, ...props} as never,
		attachTo: document.body,
		global: {
			plugins: [createI18n({legacy: false, locale: 'en', messages: {en}}), [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]],
			stubs: {SidePanel: SidePanelStub, TaskRow: {props: ['task'], template: '<li class="task">{{ task.title }}</li>'}},
		},
	})
}

const taskPage = (items: object[], total = items.length, pages = 1) => ({data: {items, total_pages: pages, total}})
async function settle() {
	for (let i = 0; i < 5; i++) await flushPromises()
}

describe('DrilldownPanel', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		message.error.mockReset()
		download.downloadBlob.mockReset()
		sdk.reportsTasksOptions.mockResolvedValue({data: {dimensions: []}})
		sdk.projectTasksList.mockResolvedValue(taskPage([{id: 1, title: 'Cover'}, {id: 2, title: 'Index'}], 2))
		URL.createObjectURL = vi.fn(() => 'blob:x')
	})

	it('lists the tasks of the clicked group with the widget filter, sorted, 25 per page, with the status expanded', async () => {
		const wrapper = mountPanel({})
		await settle()
		const call = sdk.projectTasksList.mock.calls[0]![0]
		expect(call.path).toEqual({project: 3})
		expect(call.query).toMatchObject({page: 1, per_page: 25, sort_by: ['done', 'due_date', 'id'], order_by: ['asc', 'asc', 'desc'], expand: ['status']})
		expect(call.query.filter).toContain('done = false')
		expect(call.query.filter).toContain('21')
		expect(wrapper.get('h2').text()).toBe('Tasks in By status')
		expect(wrapper.findAll('.task').map(t => t.text())).toEqual(['Cover', 'Index'])
		expect(wrapper.get('[data-test=drilldown-count]').text()).toBe('2 tasks')
		expect(wrapper.findAll('.tag').map(t => t.text())).toEqual(['In progress'])
	})

	it('carries the time range of a bar widget into the task filter and names it', async () => {
		const wrapper = mountPanel({
			config: {request: {project_id: 3, group_by: 'status', time: {field: 'created', bucket: 'month', from: '2026-11-01', to: '2026-11-30'}}},
			selection: {group: group(21, 'In progress'), point: range},
		})
		await settle()
		const filter = sdk.projectTasksList.mock.calls[0]![0].query.filter as string
		expect(filter).toContain('created')
		expect(filter).toContain(range.start)
		expect(filter).toContain(range.end)
		expect(filter).toContain('21')
		expect(wrapper.findAll('.tag').map(t => t.text())).toEqual(['In progress', '2026-11'])
		expect(wrapper.get('.drilldown__filter code').text()).toBe(filter)
	})

	it('sends no request and says why when the range the server resolved is missing', async () => {
		const wrapper = mountPanel({
			config: {request: {project_id: 3, group_by: 'status', time: {field: 'created', bucket: 'month'}}},
			selection: {group: group(21, 'In progress')},
		})
		await settle()
		expect(sdk.projectTasksList).not.toHaveBeenCalled()
		expect(wrapper.find('[role="alert"]').exists()).toBe(true)
		expect(wrapper.find('[data-test=drilldown-count]').exists()).toBe(false)
	})

	it('explains that the group "other" cannot be listed, without a request', async () => {
		const wrapper = mountPanel({selection: {group: group(null, 'Other', {other: true})}})
		await settle()
		expect(sdk.projectTasksList).not.toHaveBeenCalled()
		expect(wrapper.get('[role="alert"]').text()).toBe('The "Other" group cannot be listed: there is no filter for "not one of the first groups".')
	})

	it('shows an empty state, and an error state when the tasks cannot be read', async () => {
		sdk.projectTasksList.mockResolvedValueOnce(taskPage([], 0))
		const empty = mountPanel({})
		await settle()
		expect(empty.find('.task').exists()).toBe(false)
		expect(empty.get('[role="status"]').text()).not.toBe('')

		sdk.projectTasksList.mockRejectedValueOnce(new Error('down'))
		const failing = mountPanel({})
		await settle()
		expect(failing.find('[role="alert"]').exists()).toBe(true)
	})

	it('pages through the tasks and goes back to page 1 when the selection changes', async () => {
		sdk.projectTasksList.mockImplementation(async ({query}: {query: {page: number}}) => taskPage([{id: query.page, title: `T${query.page}`}], 60, 3))
		const wrapper = mountPanel({})
		await settle()
		expect(wrapper.text()).toContain('1 / 3')
		await wrapper.findAll('nav button')[1]!.trigger('click')
		await settle()
		expect(sdk.projectTasksList.mock.calls.at(-1)![0].query.page).toBe(2)
		expect(wrapper.text()).toContain('T2')
		await wrapper.setProps({selection: {group: group(22, 'Done')}})
		await settle()
		expect(sdk.projectTasksList.mock.calls.at(-1)![0].query.page).toBe(1)
	})

	it('exports the same tasks as CSV with the same filter and project', async () => {
		sdk.reportsTasksExportRows.mockResolvedValue({data: new Blob(['a'])})
		const wrapper = mountPanel({})
		await settle()
		await wrapper.get('[data-test=drilldown-export]').trigger('click')
		await settle()
		const body = sdk.reportsTasksExportRows.mock.calls[0]![0].body
		expect(body.project_id).toBe(3)
		expect(body.filter).toBe(sdk.projectTasksList.mock.calls[0]![0].query.filter)
		expect(download.downloadBlob).toHaveBeenCalledWith('blob:x', 'tasks.csv')
	})

	it('toasts a failed export and offers it again', async () => {
		sdk.reportsTasksExportRows.mockRejectedValue(new Error('down'))
		const wrapper = mountPanel({})
		await settle()
		await wrapper.get('[data-test=drilldown-export]').trigger('click')
		await settle()
		expect(message.error).toHaveBeenCalledTimes(1)
		expect(wrapper.get('[data-test=drilldown-export]').attributes('disabled')).toBeUndefined()
	})

	it('emits close', async () => {
		const wrapper = mountPanel({})
		await wrapper.get('.close').trigger('click')
		expect(wrapper.emitted('close')).toHaveLength(1)
	})
})
