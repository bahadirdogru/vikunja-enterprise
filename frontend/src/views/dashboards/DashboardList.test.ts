import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({dashboardsList: vi.fn(), dashboardsUpdate: vi.fn(), dashboardsDelete: vi.fn(), dashboardsCreate: vi.fn(), reportsTasksOptions: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('vue-router', () => ({useRouter: () => ({push: vi.fn()})}))
vi.mock('@/composables/useProjects', () => ({
	useProjects: () => ({
		projects: {3: {id: 3, title: 'Plan', max_permission: 2}, 4: {id: 4, title: 'Read only', max_permission: 0}},
		projectsArray: [{id: 3, title: 'Plan', max_permission: 2, is_archived: false}, {id: 4, title: 'Read only', max_permission: 0, is_archived: false}],
	}),
}))

vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn()}))

import DashboardList from './DashboardList.vue'
import {i18n} from '@/components/charts/testing'

enableAutoUnmount(afterEach)

const item = (id: number, title: string, project_id: number) => ({id, title, project_id, widget_count: 2, widgets: null})

function mountList() {
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(DashboardList, {
		global: {
			plugins: [i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {RouterLink: {template: '<a><slot /></a>'}, Modal: true, Icon: true, Loading: true},
			directives: {tooltip: {}, cy: {}},
		},
	})
}

describe('DashboardList', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(m => m.mockReset())
		sdk.dashboardsList.mockResolvedValue({data: {items: [item(1, 'Mine', 0), item(2, 'Plan overview', 3), item(3, 'Shared', 4)], total_pages: 1}})
	})

	it('groups personal dashboards and dashboards by project', async () => {
		const wrapper = mountList()
		await flushPromises()
		expect(wrapper.findAll('.dashboard-list__group-title').map(t => t.text())).toEqual(['My dashboards', 'Plan', 'Read only'])
		expect(wrapper.findAll('[data-test=dashboard-item]')).toHaveLength(3)
		expect(wrapper.text()).toContain('2 widgets')
	})

	it('offers rename and delete only where the viewer may change the dashboard', async () => {
		const wrapper = mountList()
		await flushPromises()
		const items = wrapper.findAll('[data-test=dashboard-item]')
		expect(items[0]!.findAll('.dashboard-list__action')).toHaveLength(2)
		expect(items[1]!.findAll('.dashboard-list__action')).toHaveLength(2)
		expect(items[2]!.findAll('.dashboard-list__action')).toHaveLength(0)
	})

	it('renames through the document update without sending widgets', async () => {
		sdk.dashboardsUpdate.mockResolvedValue({data: item(1, 'New', 0)})
		const wrapper = mountList()
		await flushPromises()
		await wrapper.findAll('.dashboard-list__action')[0]!.trigger('click')
		const input = wrapper.find('.dashboard-list__rename input')
		await input.setValue('New')
		await wrapper.find('.dashboard-list__rename').trigger('submit')
		await flushPromises()
		const call = sdk.dashboardsUpdate.mock.calls[0]![0]
		expect(call.path).toEqual({dashboard: 1})
		expect(call.body.title).toBe('New')
		expect('widgets' in call.body).toBe(false)
	})

	it('requires a name to create', async () => {
		const wrapper = mountList()
		await flushPromises()
		await wrapper.find('[data-test=new-dashboard]').trigger('click')
		await wrapper.find('[data-test=create-submit]').trigger('click')
		expect(wrapper.text()).toContain('Give the dashboard a name.')
		expect(sdk.dashboardsCreate).not.toHaveBeenCalled()
	})

	it('shows the empty state', async () => {
		sdk.dashboardsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		const wrapper = mountList()
		await flushPromises()
		expect(wrapper.text()).toContain('No dashboards yet')
	})
})
