import type {ChartGroup, ChartPoint} from '@/helpers/charts'

// Why a click on a group cannot open the tasks behind it.
export type DrilldownBlock =
	| 'other' // the folded rest of a limit: there is no filter for "not one of the first N"
	| 'null_filter' // "no value" needs filter_include_nulls, which would also change the widget's own filter
	| 'include_nulls' // the widget itself uses filter_include_nulls
	| 'status_entered' // not a filter field
	| 'needs_username' // assignee / created_by filter by username, the report only knows the id
	| 'needs_options' // "no value" of a select field is "none of its options"
	| 'unknown_dimension'

export type DrilldownResult =
	| {ok: true, filter: string, includeNulls: boolean, parts: string[]}
	| {ok: false, reason: DrilldownBlock, userId?: number}

export type DrilldownInput = {
	baseFilter: string
	includeNulls?: boolean
	groupBy?: string
	thenBy?: string
	group?: ChartGroup | null
	child?: ChartGroup | null
	timeField?: string
	point?: ChartPoint | null
	// id -> username, filled by the panel for assignee / created_by / user fields.
	usernames?: Record<number, string>
	// cf_<id> -> option ids, for the "no value" group of select fields.
	cfOptionIds?: Record<string, number[]>
	// cf_<id> -> custom field type, from the report options.
	fieldTypes?: Record<string, string>
	// The range of a number widget is one point that has to be there before the tasks can be listed.
	requirePoint?: boolean
	// Only asks whether a filter can exist at all, before the lookups of usernames and options ran.
	assumeResolvable?: boolean
}

const categories = 'open,in_progress,done,cancelled'
const timeFilterFields: Record<string, string> = {
	created: 'created',
	done_at: 'done_at',
	due_date: 'due_date',
}

// Fields that no filter_include_nulls can change: the column is never null.
const nullSafeFields = new Set(['done', 'project', 'project_id', 'id', 'created'])

// True when the filter only uses columns that filter_include_nulls leaves alone.
export function isNullSafeFilter(filter: string): boolean {
	const fields = [...filter.matchAll(/([a-z_][a-z0-9_]*)\s*(?:=|!=|>=|<=|>|<|\blike\b|\bin\b|\bnot\s+in\b)/gi)]
	return fields.every(match => nullSafeFields.has(match[1]!.toLowerCase()))
}

type Clause =
	| {clause: string, nulls?: boolean}
	| {block: DrilldownBlock, userId?: number}

function quote(value: string): string {
	return JSON.stringify(value)
}

function isCustom(dimension: string): boolean {
	return /^cf_\d+$/.test(dimension)
}

// "No value" of a multi valued or optional field: a clause nothing satisfies, so with filter_include_nulls only tasks
// without any value remain. The ids start at 1, so 0 never exists; usernames are matched as text, so "0" is the
// sentinel there.
function emptyClause(field: string, sentinel: string): Clause {
	return {clause: `${field} = ${sentinel}`, nulls: true}
}

function dimensionClause(dimension: string, group: ChartGroup, input: DrilldownInput): Clause {
	const none = group.none === true
	const key = group.key
	switch (dimension) {
		case 'status':
			return none ? {clause: `status_category not in ${categories}`} : {clause: `status = ${Number(key)}`}
		case 'status_category':
			return none ? {clause: `status_category not in ${categories}`} : {clause: `status_category = ${String(key)}`}
		case 'assignee':
			return none ? emptyClause('assignees', '0') : usernameClause('assignees', Number(key), input)
		case 'created_by':
			return usernameClause('created_by', Number(key), input)
		case 'label':
			return none ? emptyClause('labels', '0') : {clause: `labels = ${Number(key)}`}
		case 'project':
			return {clause: `project = ${Number(key)}`}
		case 'priority':
			return none ? {clause: 'priority = 0'} : {clause: `priority = ${Number(key)}`}
		case 'done':
			return {clause: `done = ${key === true || key === 'true'}`}
	}
	if (!isCustom(dimension)) {
		return {block: 'unknown_dimension'}
	}
	const type = input.fieldTypes?.[dimension]
	if (type === 'checkbox') {
		return {clause: `${dimension} = ${key === true || key === 'true' || key === 1}`}
	}
	if (type === 'rating') {
		return none ? emptyClause(dimension, '-1') : {clause: `${dimension} = ${Number(key)}`}
	}
	if (type === 'user') {
		return none ? emptyClause(dimension, '0') : {clause: `${dimension} = ${Number(key)}`}
	}
	if (type === 'select' || type === 'multi_select') {
		if (!none) {
			return {clause: `${dimension} = ${Number(key)}`}
		}
		const options = input.cfOptionIds?.[dimension] ?? (input.assumeResolvable ? [0] : undefined)
		return options && options.length > 0
			? {clause: `${dimension} not in ${options.join(',')}`}
			: {block: 'needs_options'}
	}
	return {block: 'unknown_dimension'}
}

function usernameClause(field: string, userId: number, input: DrilldownInput): Clause {
	const name = input.usernames?.[userId] ?? (input.assumeResolvable ? 'x' : undefined)
	if (!name) {
		return {block: 'needs_username', userId}
	}
	return {clause: `${field} = ${quote(name)}`}
}

function timeClause(field: string, point: ChartPoint): Clause {
	const column = timeFilterFields[field]
	if (!column) {
		return {block: 'status_entered'}
	}
	if (!point.start || !point.end) {
		return {block: 'unknown_dimension'}
	}
	return {clause: `${column} >= ${quote(point.start)} && ${column} < ${quote(point.end)}`}
}

function wrap(filter: string): string {
	const trimmed = filter.trim()
	return /\|\||\bor\b/i.test(trimmed) ? `(${trimmed})` : trimmed
}

// The filter that lists exactly the tasks behind a group, a bucket or a number: the widget filter AND-ed with one
// condition per dimension and one for the bucket.
export function drilldownFilter(input: DrilldownInput): DrilldownResult {
	const clauses: Clause[] = []
	const parts: string[] = []

	if (input.groupBy && input.group) {
		if (input.group.other) {
			return {ok: false, reason: 'other'}
		}
		clauses.push(dimensionClause(input.groupBy, input.group, input))
		parts.push(input.group.label)
	}
	if (input.thenBy && input.child) {
		if (input.child.other) {
			return {ok: false, reason: 'other'}
		}
		clauses.push(dimensionClause(input.thenBy, input.child, input))
		parts.push(input.child.label)
	}
	if (input.requirePoint && !input.point) {
		return {ok: false, reason: 'unknown_dimension'}
	}
	if (input.timeField && input.point) {
		clauses.push(timeClause(input.timeField, input.point))
		parts.push(input.point.bucket)
	}

	const blocked = clauses.find((c): c is {block: DrilldownBlock, userId?: number} => 'block' in c)
	if (blocked) {
		return {ok: false, reason: blocked.block, userId: blocked.userId}
	}
	const found = clauses as {clause: string, nulls?: boolean}[]
	const needsNulls = found.some(c => c.nulls)
	if (input.includeNulls && found.length > 0) {
		return {ok: false, reason: 'include_nulls'}
	}
	if (needsNulls) {
		if (found.filter(c => c.nulls).length > 1) {
			// Two "no value" conditions would each be widened by the one flag.
			return {ok: false, reason: 'null_filter'}
		}
		if (!isNullSafeFilter(input.baseFilter) || found.some(c => !c.nulls && !isNullSafeFilter(c.clause))) {
			return {ok: false, reason: 'null_filter'}
		}
	}

	const filters = [wrap(input.baseFilter), ...found.map(c => c.clause)].filter(f => f !== '')
	return {ok: true, filter: filters.join(' && '), includeNulls: needsNulls || input.includeNulls === true, parts}
}

// The range a report counted, as the one "bucket" a number covers. The server resolved it, so relative dates, whole
// buckets and a range cut at the end of the period are the same ones the number was computed over.
export function reportRangePoint(time: {from?: string, to?: string} | null | undefined): ChartPoint | undefined {
	return time?.from && time.to ? {bucket: '', start: time.from, end: time.to, value: 0} : undefined
}

export type DrilldownWidget = {
	type: string
	config: Record<string, unknown>
}

type RequestLike = {
	filter?: string
	filter_include_nulls?: boolean
	group_by?: string
	then_by?: string
	time?: {field?: string}
}

// What a click on a chart means for the filter: the widget's own filter plus the clicked group, child and bucket.
export function drilldownInputFor(
	widget: DrilldownWidget,
	selection: {group?: ChartGroup | null, child?: ChartGroup | null, point?: ChartPoint | null, series?: {key: string} | null},
): DrilldownInput {
	const config = widget.config
	if (widget.type === 'created_vs_done' || widget.type === 'task_list') {
		const base = String((config as {filter?: string}).filter ?? '')
		if (widget.type === 'task_list') {
			return {baseFilter: base}
		}
		const field = selection.series?.key === 'done' ? 'done_at' : 'created'
		return {baseFilter: base, timeField: selection.point ? field : undefined, point: selection.point}
	}
	const request = (config.request ?? {}) as RequestLike
	const input: DrilldownInput = {
		baseFilter: request.filter ?? '',
		includeNulls: request.filter_include_nulls === true,
	}
	if (widget.type === 'bar' || widget.type === 'donut') {
		input.groupBy = request.group_by
		input.thenBy = request.then_by
		input.group = selection.group
		input.child = selection.child
	}
	if (widget.type === 'number' && request.time?.field) {
		// The number only counts the tasks of its time range.
		input.timeField = request.time.field
		input.point = selection.point
		input.requirePoint = true
	}
	if (widget.type === 'line') {
		input.groupBy = request.group_by
		input.group = selection.group
		input.timeField = request.time?.field
		input.point = selection.point
	}
	return input
}
