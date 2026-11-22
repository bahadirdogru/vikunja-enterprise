import {shallowMount, flushPromises} from '@vue/test-utils'
import {describe, expect, it, vi, beforeEach} from 'vitest'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'
import {createRouter, createMemoryHistory} from 'vue-router'
import {createPinia} from 'pinia'

const sdk = vi.hoisted(() => ({
	projectViewTasksList: vi.fn(),
	workflowRead: vi.fn(),
	customFieldsList: vi.fn(),
}))

vi.mock('@/client/generated', () => sdk)
vi.mock('@/stores/auth', () => ({useAuthStore: () => ({settings: {timezone: 'UTC'}})}))
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/composables/useCurrentProject', () => ({
	useCurrentProject: () => ({
		currentProject: {value: {id: 1, max_permission: 2, is_archived: false}},
		isPending: {value: false},
	}),
}))
vi.mock('@/composables/useProjects', () => ({useProjects: () => ({projects: {}})}))
vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

import ProjectTable from './ProjectTable.vue'

const workflow = {
	id: 1,
	project_id: 1,
	restrict_transitions: false,
	statuses: [{id: 5, title: 'Todo', color: '#888888', category: 'open', position: 1, is_initial: true, archived: false}],
	transitions: [],
}

const tasks = [
	{id: 1, title: 'With status', index: 1, project_id: 1, status: {id: 5, title: 'Todo', color: '#888888', category: 'open'}},
	{id: 2, title: 'Without status', index: 2, project_id: 1},
]

async function mountTable() {
	const router = createRouter({history: createMemoryHistory(), routes: [{path: '/', component: {render: () => null}}]})
	await router.push('/')
	const wrapper = shallowMount(ProjectTable, {
		props: {
			isLoadingProject: false,
			projectId: 1,
			viewId: 20,
			view: {id: 20, view_kind: 'table'},
		},
		global: {
			plugins: [
				router,
				createPinia(),
				[VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}],
			],
			mocks: {$t: (key: string) => key},
			directives: {tooltip: () => {}},
			stubs: {
				ProjectWrapper: {template: '<div><slot name="default"/></div>'},
				Card: {template: '<div><slot/></div>'},
				RouterLink: {template: '<a><slot/></a>'},
			},
		},
	})
	await flushPromises()
	return wrapper
}

const lastQuery = () => sdk.projectViewTasksList.mock.calls.at(-1)![0].query as {expand: string[], sort_by: string[], order_by: string[]}
const headers = (wrapper: Awaited<ReturnType<typeof mountTable>>) => wrapper.findAll('th').map(th => th.text())

describe('ProjectTable status column', () => {
	beforeEach(() => {
		localStorage.clear()
		for (const fn of Object.values(sdk)) fn.mockReset()
		sdk.projectViewTasksList.mockResolvedValue({data: {items: tasks, page: 1, total_pages: 1}})
		sdk.customFieldsList.mockResolvedValue({data: {items: [], page: 1, total_pages: 1}})
	})

	it('is shown by default in a project with a workflow and asks for the status', async () => {
		sdk.workflowRead.mockResolvedValue({data: workflow})
		const wrapper = await mountTable()

		expect(headers(wrapper)).toContain('workflow.task.label')
		expect(lastQuery().expand).toContain('status')
		const badges = wrapper.findAllComponents({name: 'WorkflowStatusBadge'})
		expect(badges).toHaveLength(1)
		expect(badges[0].props('status')).toEqual({title: 'Todo', color: '#888888', category: 'open'})
	})

	it('is not offered and not requested in a project without a workflow', async () => {
		sdk.workflowRead.mockRejectedValue(Object.assign(new Error('none'), {code: 21001}))
		const wrapper = await mountTable()

		expect(headers(wrapper)).not.toContain('workflow.task.label')
		expect(lastQuery().expand).not.toContain('status')
		expect(wrapper.findAllComponents({name: 'WorkflowStatusBadge'})).toHaveLength(0)
	})

	it('stays off once the user switched it off, and then is not requested', async () => {
		localStorage.setItem('tableViewColumns', JSON.stringify({index: true, done: true, title: true, status: false}))
		sdk.workflowRead.mockResolvedValue({data: workflow})
		const wrapper = await mountTable()

		expect(headers(wrapper)).not.toContain('workflow.task.label')
		expect(lastQuery().expand).not.toContain('status')
	})

	it('is still on for users whose stored columns predate it', async () => {
		localStorage.setItem('tableViewColumns', JSON.stringify({index: true, done: true, title: true}))
		sdk.workflowRead.mockResolvedValue({data: workflow})
		const wrapper = await mountTable()

		expect(headers(wrapper)).toContain('workflow.task.label')
	})

	it('sorts by the status position', async () => {
		sdk.workflowRead.mockResolvedValue({data: workflow})
		const wrapper = await mountTable()

		const sort = wrapper.findAllComponents({name: 'Sort'}).find(component => component.props('label') === 'workflow.task.label')
		expect(sort).toBeDefined()
		sort!.vm.$emit('click')
		await flushPromises()

		const query = lastQuery()
		expect(query.sort_by).toContain('status')
		expect(query.order_by[query.sort_by.indexOf('status')]).toBe('desc')
	})
})
