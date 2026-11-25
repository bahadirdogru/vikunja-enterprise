import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	dashboardsCreate: vi.fn(), dashboardsDelete: vi.fn(), dashboardsLayout: vi.fn(), dashboardsList: vi.fn(), dashboardsRead: vi.fn(), dashboardsUpdate: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => message)
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import {
	createDashboardMutationOptions, dashboardKeys, dashboardQuery, dashboardsQuery, deleteDashboardMutationOptions, normalizeDashboard,
	saveLayoutMutationOptions, updateDashboardMutationOptions, widgetBody, widgetFromResponse, type DashboardResponse,
} from './dashboards'
import {ApiError} from '@/client/problem'

const run = (client: QueryClient, options: Parameters<QueryClient['getMutationCache']['build']>[1], vars: unknown) =>
	client.getMutationCache().build(client, options as never).execute(vars as never)

const stored = (over: Partial<DashboardResponse> = {}): DashboardResponse => ({
	id: 4, title: 'Ops', description: '', project_id: 3, position: 1, owner: null, widget_count: 2, max_permission: 2, updated: '',
	widgets: [
		{id: 10, type: 'number', title: 'Open', config: {}, x: 0, y: 0, w: 4, h: 3},
		{id: 11, type: 'text', title: '', config: {text: 'hi'}, x: 4, y: 0, w: 4, h: 3},
	],
	...over,
})

beforeEach(() => {
	Object.values(sdk).forEach(mock => mock.mockReset())
	message.error.mockReset()
	message.success.mockReset()
})

describe('normalizeDashboard', () => {
	it('fills defaults for an empty answer', () => {
		expect(normalizeDashboard({} as never)).toEqual({
			id: 0, title: '', description: '', project_id: 0, position: 0, owner: null, widgets: [], widget_count: 0, max_permission: 0, updated: '',
		})
	})

	it('keeps owner, widgets with their defaults, and the permission of a read body', () => {
		const d = normalizeDashboard({
			id: 4, title: 'Ops', owner: {id: 2, username: 'ada'}, max_permission: 2, widget_count: 9,
			widgets: [{id: 1}, {id: 2, type: 'bar', title: 'T', config: {a: 1}, x: 1, y: 2, w: 3, h: 4}],
		} as never)
		expect(d.owner).toEqual({id: 2, username: 'ada', name: ''})
		expect(d.max_permission).toBe(2)
		expect(d.widget_count).toBe(9)
		expect(d.widgets[0]).toEqual({id: 1, type: 'text', title: '', config: {}, x: 0, y: 0, w: 4, h: 3})
		expect(d.widgets[1]).toEqual({id: 2, type: 'bar', title: 'T', config: {a: 1}, x: 1, y: 2, w: 3, h: 4})
	})

	it('counts the widgets when the server sends no count', () => {
		expect(normalizeDashboard({widgets: [{id: 1}, {id: 2}]} as never).widget_count).toBe(2)
	})
})

describe('widgetBody and widgetFromResponse', () => {
	it('sends the id only for a stored widget', () => {
		const base = {type: 'text', title: 'T', config: {}, x: 0, y: 1, w: 2, h: 3}
		expect(widgetBody(base)).toEqual(base)
		expect(widgetBody({...base, id: 7})).toEqual({...base, id: 7})
	})

	it('hides a widget of a type this version does not offer', () => {
		expect(widgetFromResponse({id: 1, type: 'hologram', title: '', config: {}, x: 0, y: 0, w: 1, h: 1})).toBeNull()
		expect(widgetFromResponse({id: 1, type: 'number', title: '', config: {}, x: 0, y: 0, w: 1, h: 1})).toMatchObject({type: 'number'})
	})
})

describe('queries', () => {
	it('lists every page and filters by project only for a project', async () => {
		sdk.dashboardsList
			.mockResolvedValueOnce({data: {items: [{id: 1}], total_pages: 2}})
			.mockResolvedValueOnce({data: {items: [{id: 2}], total_pages: 2}})
		const client = new QueryClient()
		const all = await client.fetchQuery(dashboardsQuery())
		expect(all.map(d => d.id)).toEqual([1, 2])
		expect(sdk.dashboardsList.mock.calls.map(call => call[0].query)).toEqual([{page: 1}, {page: 2}])

		sdk.dashboardsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		await client.fetchQuery(dashboardsQuery(3))
		expect(sdk.dashboardsList.mock.calls.at(-1)![0].query).toEqual({page: 1, project_id: 3})
	})

	it('reads one dashboard and does not run for id 0', async () => {
		sdk.dashboardsRead.mockResolvedValue({data: {id: 4, title: 'Ops', max_permission: 2}})
		const client = new QueryClient()
		expect((await client.fetchQuery(dashboardQuery(4))).title).toBe('Ops')
		expect(sdk.dashboardsRead.mock.calls[0]![0].path).toEqual({dashboard: 4})
		expect(dashboardQuery(0).enabled).toBe(false)
	})
})

describe('create', () => {
	it('sends defaults, caches the dashboard as writable and invalidates the lists', async () => {
		sdk.dashboardsCreate.mockResolvedValue({data: {id: 9, title: 'New'}})
		const client = new QueryClient()
		const spy = vi.spyOn(client, 'invalidateQueries')
		const saved = await run(client, createDashboardMutationOptions(), {title: 'New'}) as DashboardResponse
		expect(sdk.dashboardsCreate.mock.calls[0]![0]).toEqual({body: {title: 'New', description: '', project_id: 0}})
		expect(saved.id).toBe(9)
		expect(client.getQueryData<DashboardResponse>(dashboardKeys.detail(9))?.max_permission).toBe(2)
		expect(spy).toHaveBeenCalledWith({queryKey: dashboardKeys.lists})
	})

	it('sends the widgets when given', async () => {
		sdk.dashboardsCreate.mockResolvedValue({data: {id: 9}})
		await run(new QueryClient(), createDashboardMutationOptions(), {title: 'N', project_id: 3, widgets: [{type: 'text', title: '', config: {}, x: 0, y: 0, w: 1, h: 1}]})
		expect(sdk.dashboardsCreate.mock.calls[0]![0].body.widgets).toHaveLength(1)
		expect(sdk.dashboardsCreate.mock.calls[0]![0].body.project_id).toBe(3)
	})
})

describe('update', () => {
	it('keeps the permission of the cached dashboard and leaves the widgets alone when none are sent', async () => {
		sdk.dashboardsUpdate.mockResolvedValue({data: {id: 4, title: 'Renamed'}})
		const client = new QueryClient()
		client.setQueryData(dashboardKeys.detail(4), stored())
		await run(client, updateDashboardMutationOptions(), {id: 4, title: 'Renamed', description: 'd', position: 2})
		const call = sdk.dashboardsUpdate.mock.calls[0]![0]
		expect(call.path).toEqual({dashboard: 4})
		expect(call.body).toEqual({title: 'Renamed', description: 'd', position: 2})
		expect(client.getQueryData<DashboardResponse>(dashboardKeys.detail(4))).toMatchObject({title: 'Renamed', max_permission: 2})
	})

	it.each([23001, 23002, 23003, 23004, 23008])('does not toast the validation error %i: the widget form shows it', async code => {
		sdk.dashboardsUpdate.mockRejectedValue(new ApiError({status: 400, code, detail: 'x'}))
		await expect(run(new QueryClient(), updateDashboardMutationOptions(), {id: 4, title: 'T', description: '', position: 0})).rejects.toBeTruthy()
		expect(message.error).not.toHaveBeenCalled()
	})

	it('toasts any other failure', async () => {
		sdk.dashboardsUpdate.mockRejectedValue(new ApiError({status: 500, detail: 'boom'}))
		await expect(run(new QueryClient(), updateDashboardMutationOptions(), {id: 4, title: 'T', description: '', position: 0})).rejects.toBeTruthy()
		expect(message.error).toHaveBeenCalledTimes(1)
	})
})

describe('delete and layout', () => {
	it('removes the detail of a deleted dashboard from the cache and confirms', async () => {
		sdk.dashboardsDelete.mockResolvedValue({})
		const client = new QueryClient()
		client.setQueryData(dashboardKeys.detail(4), stored())
		client.setQueryData(dashboardKeys.detail(5), stored({id: 5}))
		await run(client, deleteDashboardMutationOptions(), 4)
		expect(sdk.dashboardsDelete).toHaveBeenCalledWith({path: {dashboard: 4}})
		expect(client.getQueryData(dashboardKeys.detail(4))).toBeUndefined()
		expect(client.getQueryData(dashboardKeys.detail(5))).toBeDefined()
		expect(message.success).toHaveBeenCalledWith({message: 'dashboards.deleted'})
	})

	it('sends only id and geometry, and writes the saved geometry into the cached widgets', async () => {
		sdk.dashboardsLayout.mockResolvedValue({})
		const client = new QueryClient()
		client.setQueryData(dashboardKeys.detail(4), stored())
		await run(client, saveLayoutMutationOptions(), {id: 4, items: [{id: 11, x: 0, y: 3, w: 6, h: 2, title: 'ignored'}]})
		expect(sdk.dashboardsLayout.mock.calls[0]![0]).toEqual({path: {dashboard: 4}, body: {items: [{id: 11, x: 0, y: 3, w: 6, h: 2}]}})
		const widgets = client.getQueryData<DashboardResponse>(dashboardKeys.detail(4))!.widgets
		expect(widgets[1]).toMatchObject({id: 11, x: 0, y: 3, w: 6, h: 2, config: {text: 'hi'}})
		expect(widgets[0]).toMatchObject({id: 10, x: 0, y: 0, w: 4, h: 3})
	})

	it('does not toast a failed layout save (the grid shows a retry) and creates no cache entry', async () => {
		sdk.dashboardsLayout.mockRejectedValue(new ApiError({status: 500, detail: 'x'}))
		const client = new QueryClient()
		await expect(run(client, saveLayoutMutationOptions(), {id: 4, items: []})).rejects.toBeTruthy()
		expect(message.error).not.toHaveBeenCalled()
		expect(client.getQueryData(dashboardKeys.detail(4))).toBeUndefined()
	})
})
