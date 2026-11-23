import {taskReportQuery, type TaskReportResponse} from '@/client/queries/reports'
import {fetchReportCsv, fetchTasksCsv, fetchWidgetReportCsv} from '@/client/queries/reportWidgets'
import {queryClient} from '@/client/queryClient'
import {toCsv} from '@/helpers/csv'
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
export function canExportTasks(type: string): boolean {
	return type !== 'text' && type !== 'task_list' && type !== 'time_tracking'
}

export function taskExportParams(type: string, config: Record<string, unknown>): {project_id?: number, filter?: string, filter_include_nulls?: boolean} {
	const request = hasReportRequest(type) && type !== 'created_vs_done' ? configRequest({config}) : {}
	return {
		project_id: widgetProjectId(type, config) || undefined,
		filter: widgetFilter(type, config),
		filter_include_nulls: (request as ReportRequestDraft).filter_include_nulls,
	}
}

export async function exportWidgetTasks(type: string, config: Record<string, unknown>): Promise<WidgetExportFile> {
	return {blob: await fetchTasksCsv(taskExportParams(type, config)), filename: 'tasks.csv'}
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

