import type {IconProp} from '@fortawesome/fontawesome-svg-core'

import type {LayoutItem, LayoutLimits} from '@/helpers/dashboardLayout'
import {GRID_COLUMNS} from '@/helpers/dashboardLayout'

// Widget types the backend can serve data for. status_time, workload and time_tracking are deferred (step 4c).
export const OFFERED_WIDGET_TYPES = ['number', 'bar', 'donut', 'line', 'created_vs_done', 'task_list', 'text'] as const
export type WidgetType = typeof OFFERED_WIDGET_TYPES[number]

export const WIDGET_ICONS: Record<WidgetType, IconProp> = {
	number: 'hashtag',
	bar: 'chart-column',
	donut: 'chart-pie',
	line: 'chart-line',
	created_vs_done: 'arrow-trend-up',
	task_list: 'list',
	text: 'align-left',
}

export const WIDGET_SIZES: Record<WidgetType, {w: number, h: number} & LayoutLimits> = {
	number: {w: 3, h: 2, minW: 2, minH: 2},
	bar: {w: 6, h: 4, minW: 3, minH: 3},
	donut: {w: 4, h: 4, minW: 3, minH: 3},
	line: {w: 6, h: 4, minW: 4, minH: 3},
	created_vs_done: {w: 6, h: 4, minW: 4, minH: 3},
	task_list: {w: 6, h: 4, minW: 3, minH: 3},
	text: {w: 4, h: 2, minW: 2, minH: 2},
}

export function isOfferedType(type: string): type is WidgetType {
	return (OFFERED_WIDGET_TYPES as readonly string[]).includes(type)
}

export function limitsFor(type: string): LayoutLimits {
	return isOfferedType(type) ? WIDGET_SIZES[type] : {minW: 2, minH: 2}
}

export type ReportTimeDraft = {
	field?: string
	bucket?: string
	from?: string
	to?: string
	timezone?: string
}

export type ReportRequestDraft = {
	project_id?: number
	filter?: string
	filter_include_nulls?: boolean
	group_by?: string
	then_by?: string
	measure?: string
	field?: string
	time?: ReportTimeDraft
	limit?: number
}

// Drops what is empty or implied, in a fixed key order, so equal requests share a cache key and the server cache.
export function normalizeReportRequest(draft: ReportRequestDraft): ReportRequestDraft {
	const out: ReportRequestDraft = {}
	if (draft.project_id) {
		out.project_id = draft.project_id
	}
	if (draft.filter && draft.filter.trim() !== '') {
		out.filter = draft.filter.trim()
	}
	if (draft.filter_include_nulls) {
		out.filter_include_nulls = true
	}
	if (draft.group_by) {
		out.group_by = draft.group_by
		if (draft.then_by && draft.then_by !== draft.group_by) {
			out.then_by = draft.then_by
		}
	}
	const measure = draft.measure && draft.measure !== 'count' ? draft.measure : ''
	if (measure) {
		out.measure = measure
		if (draft.field) {
			out.field = draft.field
		}
	}
	if (draft.time && draft.time.field) {
		const time: ReportTimeDraft = {field: draft.time.field}
		if (draft.time.bucket) {
			time.bucket = draft.time.bucket
		}
		if (draft.time.from) {
			time.from = draft.time.from
		}
		if (draft.time.to) {
			time.to = draft.time.to
		}
		if (draft.time.timezone) {
			time.timezone = draft.time.timezone
		}
		out.time = time
	}
	if (draft.limit && draft.limit > 0) {
		out.limit = draft.limit
	}
	return out
}

export type RangePreset = 'last7' | 'last30' | 'last90' | 'thisMonth' | 'thisYear' | 'custom'

export const RANGE_PRESETS: Exclude<RangePreset, 'custom'>[] = ['last7', 'last30', 'last90', 'thisMonth', 'thisYear']

// Relative expressions the server resolves at request time, so a saved widget always shows "the last 30 days".
const presetRanges: Record<Exclude<RangePreset, 'custom'>, {from: string, to: string}> = {
	last7: {from: 'now-7d', to: 'now'},
	last30: {from: 'now-30d', to: 'now'},
	last90: {from: 'now-90d', to: 'now'},
	thisMonth: {from: 'now/M', to: 'now'},
	thisYear: {from: 'now/y', to: 'now'},
}

export function rangeForPreset(preset: Exclude<RangePreset, 'custom'>): {from: string, to: string} {
	return {...presetRanges[preset]}
}

export function presetForRange(from: string | undefined, to: string | undefined): RangePreset {
	const f = from || 'now-90d'
	const t = to || 'now'
	for (const preset of RANGE_PRESETS) {
		if (presetRanges[preset].from === f && presetRanges[preset].to === t) {
			return preset
		}
	}
	return 'custom'
}

export type WidgetDraft = {
	id?: number
	type: WidgetType
	title: string
	config: Record<string, unknown>
	x: number
	y: number
	w: number
	h: number
}

export type TaskListConfig = {
	project_id?: number
	filter?: string
	sort_by?: string[]
	order_by?: string[]
	columns?: string[]
	limit?: number
}

export type CreatedVsDoneConfig = {
	project_id?: number
	filter?: string
	bucket?: string
	from?: string
	to?: string
}

export function configRequest(widget: Pick<WidgetDraft, 'config'>): ReportRequestDraft {
	const request = widget.config.request
	return request && typeof request === 'object' ? request as ReportRequestDraft : {}
}

// The report requests a widget needs. Charts have one, created_vs_done two (created and completed per bucket).
export function requestsForWidget(type: string, config: Record<string, unknown>): ReportRequestDraft[] {
	switch (type) {
		case 'number':
		case 'bar':
		case 'donut':
		case 'line': {
			const request = normalizeReportRequest(configRequest({config}))
			if (type === 'line' && !request.time) {
				return []
			}
			return [request]
		}
		case 'created_vs_done': {
			const c = config as CreatedVsDoneConfig
			const base = {project_id: c.project_id, filter: c.filter}
			const time = {bucket: c.bucket || 'day', from: c.from, to: c.to}
			return [
				normalizeReportRequest({...base, time: {...time, field: 'created'}}),
				normalizeReportRequest({...base, time: {...time, field: 'done_at'}}),
			]
		}
		default:
			return []
	}
}

export function hasReportRequest(type: string): boolean {
	return ['number', 'bar', 'donut', 'line', 'created_vs_done'].includes(type)
}

export function widgetFilter(type: string, config: Record<string, unknown>): string {
	if (type === 'created_vs_done' || type === 'task_list') {
		return String((config as {filter?: string}).filter ?? '')
	}
	return configRequest({config}).filter ?? ''
}

export function widgetProjectId(type: string, config: Record<string, unknown>): number {
	if (type === 'created_vs_done' || type === 'task_list') {
		return Number((config as {project_id?: number}).project_id ?? 0)
	}
	return Number(configRequest({config}).project_id ?? 0)
}

export function summarizeFilter(filter: string, max = 80): string {
	const text = filter.replace(/\s+/g, ' ').trim()
	return text.length > max ? text.slice(0, max - 1) + '…' : text
}

export type DefaultWidgetOptions = {
	projectId: number
	hasWorkflow: boolean
	// Titles are stored as typed, so they are written in the language of whoever creates the dashboard.
	titles: Record<'open' | 'status' | 'done' | 'createdVsDone' | 'assignee' | 'overdue', string>
}

export type NewWidget = Omit<WidgetDraft, 'id'>

// What a project dashboard starts with. Positions fill a 12 x 6 block without gaps.
export function defaultWidgets({projectId, hasWorkflow, titles}: DefaultWidgetOptions): NewWidget[] {
	const scope = projectId > 0 ? {project_id: projectId} : {}
	return [
		{
			type: 'number',
			title: titles.open,
			config: {request: {...scope, filter: 'done = false'}},
			x: 0, y: 0, w: 3, h: 2,
		},
		{
			type: 'donut',
			title: hasWorkflow ? titles.status : titles.done,
			config: {request: {...scope, group_by: hasWorkflow ? 'status' : 'done'}},
			x: 0, y: 2, w: 3, h: 4,
		},
		{
			type: 'created_vs_done',
			title: titles.createdVsDone,
			config: {...scope, bucket: 'day', from: 'now-30d', to: 'now'},
			x: 3, y: 0, w: 9, h: 3,
		},
		{
			type: 'bar',
			title: titles.assignee,
			config: {request: {...scope, filter: 'done = false', group_by: 'assignee'}},
			x: 3, y: 3, w: 5, h: 3,
		},
		{
			type: 'task_list',
			title: titles.overdue,
			config: {...scope, filter: 'done = false && due_date < now', sort_by: ['due_date'], order_by: ['asc'], limit: 10},
			x: 8, y: 3, w: 4, h: 3,
		},
	]
}

export function layoutOf(widgets: {id?: number, x?: number, y?: number, w?: number, h?: number}[]): LayoutItem[] {
	return widgets.map(w => ({id: w.id ?? 0, x: w.x ?? 0, y: w.y ?? 0, w: w.w ?? 4, h: w.h ?? 3}))
}

export function isValidGridPosition(item: LayoutItem): boolean {
	return item.x >= 0 && item.w >= 1 && item.x + item.w <= GRID_COLUMNS && item.y >= 0 && item.h >= 1
}

// "widgets[1].config.request.group_by" and "time.from" both point at the control of the same name.
export function controlForParam(param: string | undefined): string {
	if (!param) {
		return ''
	}
	return param
		.replace(/^widgets\[\d+\]\./, '')
		.replace(/^config\./, '')
		.replace(/^request\./, '')
}

export type ServerFieldError = {param: string, control: string, message: string, code: number}

// The filter syntax errors the task filter gives: unknown field, comparator, concatenator or value.
const FILTER_ERROR_CODES = [4016, 4017, 4018, 4019]
const PARAM_ERROR_CODES = [23001, 23002, 23003, 23004, 23008]

export function serverFieldError(error: unknown): ServerFieldError | null {
	if (typeof error !== 'object' || error === null) {
		return null
	}
	const e = error as {code?: number, details?: unknown, detail?: string, message?: string}
	const message = e.detail ?? e.message ?? ''
	if (FILTER_ERROR_CODES.includes(e.code ?? 0)) {
		return {param: 'filter', control: 'filter', message, code: e.code ?? 0}
	}
	if (!PARAM_ERROR_CODES.includes(e.code ?? 0)) {
		return null
	}
	const details = e.details as {param?: string} | undefined
	const param = typeof details?.param === 'string' ? details.param : ''
	let control = controlForParam(param)
	if (e.code === 23002) {
		control = control || 'time'
	}
	if (e.code === 23003) {
		control = control || 'limit'
	}
	return {param, control, message, code: e.code ?? 0}
}

export const DASHBOARD_ERROR_CODES = [23001, 23002, 23003, 23004, 23005, 23006, 23007, 23008, 23009] as const
