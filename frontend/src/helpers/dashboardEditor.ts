import type {WidgetDisplay} from '@/helpers/dashboardDisplay'
import {
	configRequest,
	normalizeReportRequest,
	presetForRange,
	rangeForPreset,
	type CreatedVsDoneConfig,
	type RangePreset,
	type ReportRequestDraft,
	type StatusTimeConfig,
	type TaskListConfig,
	type TimeTrackingConfig,
	type WidgetType,
	type WorkloadConfig,
} from '@/helpers/dashboards'

// What the widget form edits. One flat shape for every type keeps switching between controls cheap; only the fields a
// type uses end up in its config.
export type EditorDraft = {
	type: WidgetType
	title: string
	// 0 = all projects the viewer can read.
	projectId: number
	filter: string
	groupBy: string
	thenBy: string
	measure: string
	field: string
	limit: number | null
	timeField: string
	bucket: string
	preset: RangePreset
	from: string
	to: string
	sortBy: string
	order: 'asc' | 'desc'
	listLimit: number
	text: string
	// number: also show the previous period of the same length.
	compare: boolean
	// workload: the numeric custom field that is added up per assignee.
	estimateField: string
	display: WidgetDisplay
}

export function emptyDraft(type: WidgetType, projectId = 0): EditorDraft {
	const range = rangeForPreset('last30')
	return {
		type,
		title: '',
		projectId,
		filter: '',
		groupBy: '',
		thenBy: '',
		measure: 'count',
		field: '',
		limit: null,
		timeField: type === 'line' ? 'created' : '',
		bucket: 'day',
		preset: 'last30',
		from: range.from,
		to: range.to,
		sortBy: 'due_date',
		order: 'asc',
		listLimit: 10,
		text: '',
		compare: false,
		estimateField: '',
		display: {},
		...typeDefaults(type),
	}
}

// Time in status starts with the whole history, tracked time with the people who tracked it.
function typeDefaults(type: WidgetType): Partial<EditorDraft> {
	switch (type) {
		case 'status_time':
			return {preset: 'all', from: '', to: ''}
		case 'time_tracking':
			return {groupBy: 'user'}
		case 'number':
			return {timeField: 'created'}
		default:
			return {}
	}
}

function rangeOf(draft: EditorDraft): {from: string, to: string} {
	if (draft.preset === 'all') {
		return {from: '', to: ''}
	}
	return draft.preset === 'custom' ? {from: draft.from, to: draft.to} : rangeForPreset(draft.preset)
}

function rangePatch(range: {from: string, to: string}): {from?: string, to?: string} {
	return {...(range.from ? {from: range.from} : {}), ...(range.to ? {to: range.to} : {})}
}

function projectPatch(draft: EditorDraft): {project_id?: number} {
	return draft.projectId > 0 ? {project_id: draft.projectId} : {}
}

function filterPatch(draft: EditorDraft): {filter?: string} {
	return draft.filter.trim() ? {filter: draft.filter.trim()} : {}
}

export function requestFromDraft(draft: EditorDraft): ReportRequestDraft {
	const range = rangeOf(draft)
	const usesGroup = draft.type === 'bar' || draft.type === 'donut' || draft.type === 'line'
	return normalizeReportRequest({
		project_id: draft.projectId,
		filter: draft.filter,
		group_by: usesGroup ? draft.groupBy : '',
		then_by: draft.type === 'bar' ? draft.thenBy : '',
		measure: draft.measure,
		field: draft.measure === 'count' ? '' : draft.field,
		time: draft.type === 'line' || (draft.type === 'number' && draft.compare)
			? {field: draft.timeField, bucket: draft.bucket, from: range.from, to: range.to}
			: undefined,
		limit: draft.type === 'bar' || draft.type === 'donut' ? draft.limit ?? undefined : undefined,
	})
}

export function configFromDraft(draft: EditorDraft): Record<string, unknown> {
	const range = rangeOf(draft)
	switch (draft.type) {
		case 'number':
			return draft.compare
				? {request: requestFromDraft(draft), compare: 'previous_period'}
				: {request: requestFromDraft(draft)}
		case 'bar':
		case 'donut':
		case 'line':
			return {request: requestFromDraft(draft)}
		case 'workload':
			return {...projectPatch(draft), ...filterPatch(draft), ...(draft.estimateField ? {estimate_field: draft.estimateField} : {})}
		case 'status_time':
			return {...projectPatch(draft), ...filterPatch(draft), ...(draft.groupBy ? {group_by: draft.groupBy} : {}), ...rangePatch(range)}
		case 'time_tracking':
			return {...projectPatch(draft), group_by: draft.groupBy || 'user', ...rangePatch(range)}
		case 'created_vs_done': {
			const config: CreatedVsDoneConfig = {bucket: draft.bucket, from: range.from, to: range.to}
			if (draft.projectId > 0) {
				config.project_id = draft.projectId
			}
			if (draft.filter.trim()) {
				config.filter = draft.filter.trim()
			}
			return {...config}
		}
		case 'task_list': {
			const config: TaskListConfig = {
				sort_by: [draft.sortBy],
				order_by: [draft.order],
				limit: Math.min(100, Math.max(1, draft.listLimit || 10)),
			}
			if (draft.projectId > 0) {
				config.project_id = draft.projectId
			}
			if (draft.filter.trim()) {
				config.filter = draft.filter.trim()
			}
			return {...config}
		}
		case 'text':
			return {text: draft.text}
	}
}

export function draftFromWidget(widget: {type: WidgetType, title: string, config: Record<string, unknown>}): EditorDraft {
	const draft = emptyDraft(widget.type)
	draft.title = widget.title
	const config = widget.config
	switch (widget.type) {
		case 'number':
		case 'bar':
		case 'donut':
		case 'line': {
			const request = configRequest({config})
			draft.compare = widget.type === 'number' && (config as {compare?: string}).compare === 'previous_period'
			draft.projectId = request.project_id ?? 0
			draft.filter = request.filter ?? ''
			draft.groupBy = request.group_by ?? ''
			draft.thenBy = request.then_by ?? ''
			draft.measure = request.measure ?? 'count'
			draft.field = request.field ?? ''
			draft.limit = request.limit ?? null
			if (request.time) {
				draft.timeField = request.time.field ?? 'created'
				draft.bucket = request.time.bucket ?? 'day'
				draft.from = request.time.from ?? ''
				draft.to = request.time.to ?? ''
				draft.preset = presetForRange(request.time.from, request.time.to)
				if (draft.preset !== 'custom') {
					const range = rangeForPreset(draft.preset)
					draft.from = range.from
					draft.to = range.to
				}
			}
			break
		}
		case 'created_vs_done': {
			const c = config as CreatedVsDoneConfig
			draft.projectId = c.project_id ?? 0
			draft.filter = c.filter ?? ''
			draft.bucket = c.bucket ?? 'day'
			draft.from = c.from ?? ''
			draft.to = c.to ?? ''
			draft.preset = presetForRange(c.from, c.to)
			if (draft.preset !== 'custom') {
				const range = rangeForPreset(draft.preset)
				draft.from = range.from
				draft.to = range.to
			}
			break
		}
		case 'task_list': {
			const c = config as TaskListConfig
			draft.projectId = c.project_id ?? 0
			draft.filter = c.filter ?? ''
			draft.sortBy = c.sort_by?.[0] ?? 'due_date'
			draft.order = c.order_by?.[0] === 'desc' ? 'desc' : 'asc'
			draft.listLimit = c.limit ?? 10
			break
		}
		case 'workload': {
			const c = config as WorkloadConfig
			draft.projectId = c.project_id ?? 0
			draft.filter = c.filter ?? ''
			draft.estimateField = c.estimate_field ?? ''
			break
		}
		case 'status_time': {
			const c = config as StatusTimeConfig
			draft.projectId = c.project_id ?? 0
			draft.filter = c.filter ?? ''
			draft.groupBy = c.group_by ?? ''
			applyRange(draft, c.from, c.to, true)
			break
		}
		case 'time_tracking': {
			const c = config as TimeTrackingConfig
			draft.projectId = c.project_id ?? 0
			draft.groupBy = c.group_by ?? 'user'
			applyRange(draft, c.from, c.to, false)
			break
		}
		case 'text':
			draft.text = String((config as {text?: string}).text ?? '')
			break
	}
	return draft
}

// Widgets that store only from and to: no dates at all is the whole history for time in status, the last 30 days for
// tracked time (the server default).
function applyRange(draft: EditorDraft, from: string | undefined, to: string | undefined, emptyIsAll: boolean) {
	if (!from && !to) {
		draft.preset = emptyIsAll ? 'all' : 'last30'
		const range = emptyIsAll ? {from: '', to: ''} : rangeForPreset('last30')
		draft.from = range.from
		draft.to = range.to
		return
	}
	draft.from = from ?? ''
	draft.to = to ?? ''
	draft.preset = presetForRange(from, to)
	if (draft.preset !== 'custom') {
		const range = rangeForPreset(draft.preset)
		draft.from = range.from
		draft.to = range.to
	}
}

export type DraftProblem = {control: string, key: string}

// What is missing before the widget can be requested at all; the server judges everything else.
export function draftProblems(draft: EditorDraft): DraftProblem[] {
	const problems: DraftProblem[] = []
	if ((draft.type === 'bar' || draft.type === 'donut') && !draft.groupBy) {
		problems.push({control: 'group_by', key: 'groupByRequired'})
	}
	if (draft.type === 'line' && !draft.timeField) {
		problems.push({control: 'time.field', key: 'timeFieldRequired'})
	}
	if (draft.measure !== 'count' && !draft.field && ['number', 'bar', 'donut', 'line'].includes(draft.type)) {
		problems.push({control: 'field', key: 'fieldRequired'})
	}
	if (draft.type === 'status_time' && draft.projectId <= 0) {
		problems.push({control: 'project_id', key: 'projectRequired'})
	}
	const usesRange = ['line', 'created_vs_done', 'status_time', 'time_tracking'].includes(draft.type) || (draft.type === 'number' && draft.compare)
	if (usesRange && draft.preset === 'custom') {
		if (!draft.from) {
			problems.push({control: 'time.from', key: 'dateRequired'})
		}
		if (!draft.to) {
			problems.push({control: 'time.to', key: 'dateRequired'})
		}
	}
	if (draft.type === 'task_list' && (draft.listLimit < 1 || draft.listLimit > 100)) {
		problems.push({control: 'limit', key: 'listLimitRange'})
	}
	return problems
}

export const TASK_LIST_SORT_FIELDS = ['due_date', 'priority', 'created', 'updated', 'title', 'id', 'done_at', 'percent_done'] as const

// A dimension needs a field to count in when the type asks for a measure other than count.
export function measureFields(options: {measures: {name: string, fields: {name: string}[]}[]}, measure: string) {
	return options.measures.find(m => m.name === measure)?.fields ?? []
}
