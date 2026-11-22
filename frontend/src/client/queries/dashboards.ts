import {queryOptions, useMutation} from '@tanstack/vue-query'

import {
	dashboardsCreate,
	dashboardsDelete,
	dashboardsLayout,
	dashboardsList,
	dashboardsRead,
	dashboardsUpdate,
} from '@/client/generated'
import type {Dashboard, DashboardReadBody, DashboardWidget, DashboardWidgetWritable} from '@/client/generated'
import {contextMutationOptions} from './contextMutation'
import {fetchAllPages} from './fetchAllPages'
import {i18n} from '@/i18n'
import type {LayoutItem} from '@/helpers/dashboardLayout'
import {isOfferedType, type WidgetDraft, type WidgetType} from '@/helpers/dashboards'

export type DashboardWidgetResponse = {
	id: number
	type: string
	title: string
	config: Record<string, unknown>
	x: number
	y: number
	w: number
	h: number
}

export type DashboardResponse = {
	id: number
	title: string
	description: string
	project_id: number
	position: number
	owner: {id: number, username: string, name: string} | null
	widgets: DashboardWidgetResponse[]
	widget_count: number
	// 2 where the viewer may change it.
	max_permission: number
	updated: string
}

function normalizeWidget(widget: DashboardWidget): DashboardWidgetResponse {
	return {
		id: widget.id ?? 0,
		type: widget.type ?? 'text',
		title: widget.title ?? '',
		config: widget.config ?? {},
		x: widget.x ?? 0,
		y: widget.y ?? 0,
		w: widget.w ?? 4,
		h: widget.h ?? 3,
	}
}

export function normalizeDashboard(dashboard: Dashboard | DashboardReadBody): DashboardResponse {
	const owner = dashboard.owner
	return {
		id: dashboard.id ?? 0,
		title: dashboard.title ?? '',
		description: dashboard.description ?? '',
		project_id: dashboard.project_id ?? 0,
		position: dashboard.position ?? 0,
		owner: owner ? {id: owner.id ?? 0, username: owner.username ?? '', name: owner.name ?? ''} : null,
		widgets: (dashboard.widgets ?? []).map(normalizeWidget),
		widget_count: dashboard.widget_count ?? dashboard.widgets?.length ?? 0,
		max_permission: 'max_permission' in dashboard ? dashboard.max_permission ?? 0 : 0,
		updated: dashboard.updated ?? '',
	}
}

export const dashboardKeys = {
	all: ['dashboards'] as const,
	lists: ['dashboards', 'list'] as const,
	list: (projectId: number) => ['dashboards', 'list', projectId] as const,
	detail: (id: number) => ['dashboards', 'detail', id] as const,
}

export function dashboardsQuery(projectId = 0) {
	return queryOptions({
		queryKey: dashboardKeys.list(projectId),
		queryFn: async ({signal}): Promise<DashboardResponse[]> => {
			const items = await fetchAllPages(async page => (await dashboardsList({
				query: {page, ...(projectId > 0 ? {project_id: projectId} : {})},
				signal,
			})).data)
			return items.map(normalizeDashboard)
		},
	})
}

export function dashboardQuery(id: number) {
	return queryOptions({
		queryKey: dashboardKeys.detail(id),
		queryFn: async ({signal}) => normalizeDashboard((await dashboardsRead({path: {dashboard: id}, signal})).data),
		enabled: id > 0,
	})
}

// The id is accepted on write (it keeps the widget) although the generated type marks it read-only.
type WidgetBody = DashboardWidgetWritable & {id?: number}

export function widgetBody(widget: Omit<WidgetDraft, 'type'> & {type: string}): WidgetBody {
	return {
		...(widget.id ? {id: widget.id} : {}),
		type: widget.type as WidgetBody['type'],
		title: widget.title,
		config: widget.config,
		x: widget.x,
		y: widget.y,
		w: widget.w,
		h: widget.h,
	}
}

export function widgetFromResponse(widget: DashboardWidgetResponse): WidgetDraft | null {
	if (!isOfferedType(widget.type)) {
		return null
	}
	return {...widget, type: widget.type as WidgetType}
}

export type CreateDashboardInput = {
	title: string
	description?: string
	project_id?: number
	widgets?: (Omit<WidgetDraft, 'type'> & {type: string})[]
}

export function createDashboardMutationOptions() {
	return contextMutationOptions({
		mutationFn: async (input: CreateDashboardInput) => normalizeDashboard((await dashboardsCreate({
			body: {
				title: input.title,
				description: input.description ?? '',
				project_id: input.project_id ?? 0,
				...(input.widgets ? {widgets: input.widgets.map(widgetBody)} : {}),
			},
		})).data),
		onSuccess: (saved, _input, client) => {
			client.setQueryData(dashboardKeys.detail(saved.id), {...saved, max_permission: 2})
		},
		onSettled: (_input, client) => client.invalidateQueries({queryKey: dashboardKeys.lists}),
		// The default dashboard can fail on one widget; the caller maps that.
		toastError: () => true,
	})
}

export type UpdateDashboardInput = {
	id: number
	title: string
	description: string
	position: number
	// Whole replacement list; leave undefined to keep the stored widgets.
	widgets?: (Omit<WidgetDraft, 'type'> & {type: string})[]
}

export function updateDashboardMutationOptions() {
	return contextMutationOptions({
		mutationFn: async (input: UpdateDashboardInput) => normalizeDashboard((await dashboardsUpdate({
			path: {dashboard: input.id},
			body: {
				title: input.title,
				description: input.description,
				position: input.position,
				...(input.widgets ? {widgets: input.widgets.map(widgetBody)} : {}),
			},
		})).data),
		onSuccess: (saved, input, client) => {
			client.setQueryData<DashboardResponse>(dashboardKeys.detail(input.id), current => ({
				...saved,
				max_permission: current?.max_permission ?? saved.max_permission,
			}))
		},
		onSettled: (_input, client) => client.invalidateQueries({queryKey: dashboardKeys.lists}),
		// Widget forms map validation errors onto their controls.
		toastError: (_input, cause) => !(typeof cause === 'object' && cause !== null && [23001, 23002, 23003, 23004, 23008].includes((cause as {code?: number}).code ?? 0)),
	})
}

export function deleteDashboardMutationOptions() {
	return contextMutationOptions({
		mutationFn: async (id: number) => {
			await dashboardsDelete({path: {dashboard: id}})
			return id
		},
		onSuccess: (id, _input, client) => {
			client.removeQueries({queryKey: dashboardKeys.detail(id), exact: true})
		},
		onSettled: (_input, client) => client.invalidateQueries({queryKey: dashboardKeys.lists}),
		successMessage: () => i18n.global.t('dashboards.deleted'),
	})
}

export type LayoutInput = {id: number, items: LayoutItem[]}

export function saveLayoutMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({id, items}: LayoutInput) => {
			await dashboardsLayout({
				path: {dashboard: id},
				body: {items: items.map(({id: widgetId, x, y, w, h}) => ({id: widgetId, x, y, w, h}))},
			})
			return items
		},
		// The view keeps the working layout; the cache follows so a reload from cache shows the saved one.
		onSuccess: (items, {id}, client) => {
			client.setQueryData<DashboardResponse>(dashboardKeys.detail(id), current => {
				if (!current) {
					return current
				}
				const byId = new Map(items.map(item => [item.id, item]))
				return {
					...current,
					widgets: current.widgets.map(widget => ({...widget, ...(byId.get(widget.id) ?? {})})),
				}
			})
		},
		// The grid shows its own failed state with a retry.
		toastError: () => false,
	})
}

export function useCreateDashboardMutation() {
	return useMutation(createDashboardMutationOptions())
}

export function useUpdateDashboardMutation() {
	return useMutation(updateDashboardMutationOptions())
}

export function useDeleteDashboardMutation() {
	return useMutation(deleteDashboardMutationOptions())
}

export function useSaveLayoutMutation() {
	return useMutation(saveLayoutMutationOptions())
}
