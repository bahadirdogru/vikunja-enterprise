import type {WidgetDisplay} from '@/helpers/dashboardDisplay'
import {
	configRequest,
	normalizeReportRequest,
	presetForRange,
	rangeForPreset,
	type CreatedVsDoneConfig,
	type RangePreset,
	type ReportRequestDraft,
	type TaskListConfig,
	type WidgetType,
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
		display: {},
	}
}

function rangeOf(draft: EditorDraft): {from: string, to: string} {
	return draft.preset === 'custom' ? {from: draft.from, to: draft.to} : rangeForPreset(draft.preset)
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
		time: draft.type === 'line'
			? {field: draft.timeField, bucket: draft.bucket, from: range.from, to: range.to}
			: undefined,
		limit: draft.type === 'bar' || draft.type === 'donut' ? draft.limit ?? undefined : undefined,
	})
}

export function configFromDraft(draft: EditorDraft): Record<string, unknown> {
	const range = rangeOf(draft)
	switch (draft.type) {
		case 'number':
		case 'bar':
		case 'donut':
		case 'line':
			return {request: requestFromDraft(draft)}
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
		case 'text':
			draft.text = String((config as {text?: string}).text ?? '')
			break
	}
	return draft
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
	if (['line', 'created_vs_done'].includes(draft.type) && draft.preset === 'custom') {
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
