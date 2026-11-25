import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {defineComponent, h} from 'vue'
import {enableAutoUnmount, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({dashboardsCreate: vi.fn(), reportsTasksOptions: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/i18n', async importOriginal => ({...await importOriginal<typeof import('@/i18n')>(), i18n: {global: {t: (key: string) => key}}}))

import {useCreateDashboard} from './useCreateDashboard'
import {queryClient as appClient} from '@/client/queryClient'
import en from '@/i18n/lang/en.json'

enableAutoUnmount(afterEach)

function setup() {
	let result!: ReturnType<typeof useCreateDashboard>
	mount(defineComponent({setup() {
		result = useCreateDashboard()
		return () => h('div')
	}}), {global: {plugins: [createI18n({legacy: false, locale: 'en', messages: {en}}), [VueQueryPlugin, {queryClient: new QueryClient()}]]}})
	return result
}

describe('useCreateDashboard', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.dashboardsCreate.mockResolvedValue({data: {id: 9}})
		appClient.clear()
	})

	it('starts a project dashboard with the status donut when the project has a workflow', async () => {
		sdk.reportsTasksOptions.mockResolvedValue({data: {has_workflow: true}})
		await setup().create({title: 'Ops', projectId: 3})
		expect(sdk.reportsTasksOptions.mock.calls[0]![0].query).toEqual({project_id: 3})
		const body = sdk.dashboardsCreate.mock.calls[0]![0].body
		expect(body).toMatchObject({title: 'Ops', project_id: 3})
		expect(body.widgets.map((w: {type: string}) => w.type)).toEqual(['number', 'donut', 'created_vs_done', 'bar', 'task_list'])
		expect(body.widgets[1].config.request.group_by).toBe('status')
		expect(body.widgets.every((w: {title: string}) => w.title !== '' && !w.title.startsWith('dashboards.'))).toBe(true)
	})

	it('starts with the done donut when the project has no workflow', async () => {
		sdk.reportsTasksOptions.mockResolvedValue({data: {has_workflow: false}})
		await setup().create({title: 'Ops', projectId: 3})
		expect(sdk.dashboardsCreate.mock.calls[0]![0].body.widgets[1].config.request.group_by).toBe('done')
	})

	it('still creates the dashboard when the options cannot be read', async () => {
		sdk.reportsTasksOptions.mockRejectedValue(new Error('down'))
		const saved = await setup().create({title: 'Ops', projectId: 3})
		expect(saved.id).toBe(9)
		expect(sdk.dashboardsCreate.mock.calls[0]![0].body.widgets[1].config.request.group_by).toBe('done')
	})

	it('makes a dashboard over all projects without a project filter', async () => {
		sdk.reportsTasksOptions.mockResolvedValue({data: {has_workflow: false}})
		await setup().create({title: 'All', projectId: 0})
		const body = sdk.dashboardsCreate.mock.calls[0]![0].body
		expect(body.project_id).toBe(0)
		expect(JSON.stringify(body.widgets)).not.toContain('project_id')
	})
})
