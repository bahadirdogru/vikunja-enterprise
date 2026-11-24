import {taskReportQuery, type TaskReportResponse} from '@/client/queries/reports'
import type {ChartPoint} from '@/helpers/charts'
import {fetchReportCsv, fetchTasksCsv, fetchWidgetReportCsv} from '@/client/queries/reportWidgets'
import {queryClient} from '@/client/queryClient'
import {toCsv} from '@/helpers/csv'
import {drilldownFilter, reportRangePoint} from '@/helpers/dashboardDrilldown'
import {
	configRequest,
	hasReportRequest,
	requestsForWidget,
	widgetFilter,
	widgetProjectId,
	type ReportRequestDraft,
} from '@/helpers/dashboards'

export type WidgetExportFile = {
	blob: Blob
	filename: string
}

const CSV_TYPE = 'text/csv;charset=utf-8'

// created_vs_done is two reports; the other charts have one and are exported by the server.
export function createdVsDoneCsv(created: TaskReportResponse, done: TaskReportResponse, labels: {bucket: string, created: string, done: string}): string {
	const doneByBucket = new Map(done.series.map(p => [p.bucket, p.value]))
	return toCsv(
		[labels.bucket, labels.created, labels.done],
		created.series.map(p => [p.bucket, p.value, doneByBucket.get(p.bucket) ?? 0]),
	)
}

export function canExportData(type: string): boolean {
	return type !== 'text'
}

// "The tasks behind this widget": every widget that has a filter (all but the note) can list them.
export function canExportTasks(type: string, config: Record<string, unknown> = {}): boolean {
	if (type === 'text' || type === 'task_list' || type === 'time_tracking') {
		return false
	}
	if (hasRange(type, config)) {
		// A time range that no task filter can express (status_entered), or one the include-nulls flag would widen.
		return drilldownFilter(numberRangeInput(type, config, {bucket: '', start: 'x', end: 'y', value: 0})).ok
	}
	return true
}

// A number, bar, donut or line widget counts the tasks of its time range only, so the tasks behind it are those of
// the range too.
function hasRange(type: string, config: Record<string, unknown>): boolean {
	return RANGE_TYPES.includes(type) && !!(configRequest({config}) as ReportRequestDraft).time?.field
}

const RANGE_TYPES = ['number', 'bar', 'donut', 'line']

function numberRangeInput(type: string, config: Record<string, unknown>, point: ChartPoint | undefined) {
	const request = configRequest({config}) as ReportRequestDraft
	return {
		baseFilter: widgetFilter(type, config),
		includeNulls: request.filter_include_nulls === true,
		timeField: request.time?.field,
		point,
		requirePoint: true,
	}
}

export function taskExportParams(type: string, config: Record<string, unknown>): {project_id?: number, filter?: string, filter_include_nulls?: boolean} {
	const request = hasReportRequest(type) && type !== 'created_vs_done' ? configRequest({config}) : {}
	let filter = widgetFilter(type, config)
	if (type === 'workload') {
		// The workload report only counts open tasks, whatever the filter says.
		filter = filter.trim() === '' ? 'done = false' : `(${filter}) && done = false`
	}
	return {
		project_id: widgetProjectId(type, config) || undefined,
		filter,
		filter_include_nulls: (request as ReportRequestDraft).filter_include_nulls,
	}
}

export async function exportWidgetTasks(type: string, config: Record<string, unknown>): Promise<WidgetExportFile> {
	const params = taskExportParams(type, config)
	if (hasRange(type, config)) {
		// The widget counts the tasks of its time range, the one the server resolved.
		const [request] = requestsForWidget(type, config)
		const report = await queryClient.fetchQuery(taskReportQuery(request!))
		const result = drilldownFilter(numberRangeInput(type, config, reportRangePoint(report.time)))
		if (!result.ok) {
			throw new Error(`The tasks of this widget cannot be listed: ${result.reason}`)
		}
		params.filter = result.filter
		params.filter_include_nulls = result.includeNulls
	}
	if (type === 'created_vs_done') {
		params.filter = await createdVsDoneFilter(config, params.filter ?? '')
	}
	return {blob: await fetchTasksCsv(params), filename: 'tasks.csv'}
}

// The tasks the two series count: created inside the range of the first, or done inside the range of the second.
async function createdVsDoneFilter(config: Record<string, unknown>, base: string): Promise<string> {
	const reports = await Promise.all(requestsForWidget('created_vs_done', config).map(request => queryClient.fetchQuery(taskReportQuery(request))))
	const ranges = ([['created', reports[0]], ['done_at', reports[1]]] as const).map(([column, report]) => {
		const point = reportRangePoint(report?.time)
		if (!point) {
			throw new Error('The tasks of this widget cannot be listed: unknown_dimension')
		}
		return `(${column} >= ${JSON.stringify(point.start)} && ${column} < ${JSON.stringify(point.end)})`
	})
	const range = `(${ranges.join(' || ')})`
	return base.trim() === '' ? range : `${/\|\||\bor\b/i.test(base) ? `(${base.trim()})` : base.trim()} && ${range}`
}

// The numbers the widget shows. The server writes the file for everything it computes, so the permissions of the
// widget and of the export are the same code.
export async function exportWidgetData(
	type: string,
	config: Record<string, unknown>,
	labels: {bucket: string, created: string, done: string},
): Promise<WidgetExportFile> {
	switch (type) {
		case 'task_list':
			return exportWidgetTasks(type, config)
		case 'status_time':
		case 'workload':
		case 'time_tracking':
			return {blob: await fetchWidgetReportCsv(type, config), filename: `${type.replace('_', '-')}.csv`}
		case 'created_vs_done': {
			const [created, done] = await Promise.all(requestsForWidget(type, config).map(request => queryClient.fetchQuery(taskReportQuery(request))))
			return {blob: new Blob([createdVsDoneCsv(created!, done!, labels)], {type: CSV_TYPE}), filename: 'created-vs-completed.csv'}
		}
		default: {
			const [request] = requestsForWidget(type, config)
			return {blob: await fetchReportCsv(request ?? {}), filename: 'report.csv'}
		}
	}
}

