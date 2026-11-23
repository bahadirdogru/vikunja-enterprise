import {keepPreviousData, queryOptions} from '@tanstack/vue-query'

import {
	reportsStatusTime,
	reportsStatusTimeExport,
	reportsTasksExport,
	reportsTasksExportRows,
	reportsTimeTracking,
	reportsTimeTrackingExport,
	reportsWorkload,
	reportsWorkloadExport,
} from '@/client/generated'
import type {
	StatusTimeGroup,
	StatusTimeReport,
	StatusTimeRequestWritable,
	StatusTimeStat,
	TaskExportRequestWritable,
	TimeTrackingReport,
	TimeTrackingRequestWritable,
	WorkloadReport,
	WorkloadRequestWritable,
	WorkloadRow,
} from '@/client/generated'
import {expectBlob} from '@/client/queries/blobResponse'
import {REPORT_STALE_TIME} from '@/client/queries/reports'
import {normalizeReportRequest, type ReportRequestDraft} from '@/helpers/dashboards'

export type StatusTimeStatResponse = {
	status_id: number
	title: string
	color: string
	category: string
	tasks: number
	visits: number
	open_tasks: number
	total_seconds: number
	average_seconds: number
	median_seconds: number
	max_seconds: number
}

export type StatusTimeGroupResponse = {
	key: unknown
	label: string
	color: string
	none: boolean
	tasks: number
	statuses: StatusTimeStatResponse[]
}

export type StatusTimeResponse = {
	statuses: StatusTimeStatResponse[]
	groups: StatusTimeGroupResponse[]
	tasks: number
	group_by: string
	multi_valued: boolean
	truncated: boolean
}

export type WorkloadRowResponse = {
	key: unknown
	label: string
	none: boolean
	counts: number[]
	total: number
	estimates: number[]
	estimate_total: number
}

export type WorkloadResponse = {
	buckets: string[]
	rows: WorkloadRowResponse[]
	totals: WorkloadRowResponse
	estimate_field: string
	truncated: boolean
}

export type TimeTrackingGroupResponse = {
	key: unknown
	label: string
	color: string
	seconds: number
	entries: number
	none: boolean
	other: boolean
}

export type TimeTrackingResponse = {
	groups: TimeTrackingGroupResponse[]
	seconds: number
	entries: number
	running: number
	group_by: string
	multi_valued: boolean
	truncated: boolean
}

function normalizeStat(stat: StatusTimeStat): StatusTimeStatResponse {
	return {
		status_id: stat.status_id ?? 0,
		title: stat.title ?? '',
		color: stat.color ?? '',
		category: stat.category ?? '',
		tasks: stat.tasks ?? 0,
		visits: stat.visits ?? 0,
		open_tasks: stat.open_tasks ?? 0,
		total_seconds: stat.total_seconds ?? 0,
		average_seconds: stat.average_seconds ?? 0,
		median_seconds: stat.median_seconds ?? 0,
		max_seconds: stat.max_seconds ?? 0,
	}
}

function normalizeStatusGroup(group: StatusTimeGroup): StatusTimeGroupResponse {
	return {
		key: group.key ?? null,
		label: group.label ?? '',
		color: group.color ?? '',
		none: group.none ?? false,
		tasks: group.tasks ?? 0,
		statuses: (group.statuses ?? []).map(normalizeStat),
	}
}

export function normalizeStatusTime(report: StatusTimeReport): StatusTimeResponse {
	return {
		statuses: (report.statuses ?? []).map(normalizeStat),
		groups: (report.groups ?? []).map(normalizeStatusGroup),
		tasks: report.tasks ?? 0,
		group_by: report.group_by ?? '',
		multi_valued: report.multi_valued ?? false,
		truncated: report.truncated ?? false,
	}
}

function normalizeRow(row: WorkloadRow): WorkloadRowResponse {
	return {
		key: row.key ?? null,
		label: row.label ?? '',
		none: row.none ?? false,
		counts: row.counts ?? [],
		total: row.total ?? 0,
		estimates: row.estimates ?? [],
		estimate_total: row.estimate_total ?? 0,
	}
}

export function normalizeWorkload(report: WorkloadReport): WorkloadResponse {
	return {
		buckets: report.buckets ?? [],
		rows: (report.rows ?? []).map(normalizeRow),
		totals: normalizeRow(report.totals ?? {}),
		estimate_field: report.estimate_field ?? '',
		truncated: report.truncated ?? false,
	}
}

export function normalizeTimeTracking(report: TimeTrackingReport): TimeTrackingResponse {
	return {
		groups: (report.groups ?? []).map(g => ({
			key: g.key ?? null,
			label: g.label ?? '',
			color: g.color ?? '',
			seconds: g.seconds ?? 0,
			entries: g.entries ?? 0,
			none: g.none ?? false,
			other: g.other ?? false,
		})),
		seconds: report.seconds ?? 0,
		entries: report.entries ?? 0,
		running: report.running ?? 0,
		group_by: report.group_by ?? '',
		multi_valued: report.multi_valued ?? false,
		truncated: report.truncated ?? false,
	}
}

// What the three widget configs share with their requests: empty values are dropped so equal widgets share a cache key.
function compact<T extends Record<string, unknown>>(config: T): T {
	return Object.fromEntries(
		Object.entries(config).filter(([, v]) => v !== '' && v !== undefined && v !== null && v !== 0),
	) as T
}

export const widgetReportKeys = {
	statusTime: (body: object) => ['reports', 'status-time', body] as const,
	workload: (body: object) => ['reports', 'workload', body] as const,
	timeTracking: (body: object) => ['reports', 'time-tracking', body] as const,
}

const widgetQueryDefaults = {
	staleTime: REPORT_STALE_TIME,
	refetchOnWindowFocus: true,
	retry: false,
	placeholderData: keepPreviousData,
	// Widgets and the preview render their own error state.
	meta: {handlesError: true},
}

export function statusTimeBody(config: Record<string, unknown>): StatusTimeRequestWritable {
	return compact(config) as StatusTimeRequestWritable
}

export function workloadBody(config: Record<string, unknown>): WorkloadRequestWritable {
	return compact(config) as WorkloadRequestWritable
}

export function timeTrackingBody(config: Record<string, unknown>): TimeTrackingRequestWritable {
	return compact(config) as TimeTrackingRequestWritable
}

export function statusTimeQuery(config: Record<string, unknown>, enabled = true) {
	const body = statusTimeBody(config)
	return queryOptions({
		queryKey: widgetReportKeys.statusTime(body),
		queryFn: async ({signal}) => normalizeStatusTime((await reportsStatusTime({body, signal})).data),
		enabled,
		...widgetQueryDefaults,
	})
}

export function workloadQuery(config: Record<string, unknown>, enabled = true) {
	const body = workloadBody(config)
	return queryOptions({
		queryKey: widgetReportKeys.workload(body),
		queryFn: async ({signal}) => normalizeWorkload((await reportsWorkload({body, signal})).data),
		enabled,
		...widgetQueryDefaults,
	})
}

export function timeTrackingQuery(config: Record<string, unknown>, enabled = true) {
	const body = timeTrackingBody(config)
	return queryOptions({
		queryKey: widgetReportKeys.timeTracking(body),
		queryFn: async ({signal}) => normalizeTimeTracking((await reportsTimeTracking({body, signal})).data),
		enabled,
		...widgetQueryDefaults,
	})
}

export type TaskExportDraft = {
	project_id?: number
	filter?: string
	filter_include_nulls?: boolean
}

// The files come from the server: it applies the same permissions as the lists and escapes the cells.
export async function fetchReportCsv(request: ReportRequestDraft): Promise<Blob> {
	const body = normalizeReportRequest(request) as Parameters<typeof reportsTasksExport>[0]['body']
	const {data} = await reportsTasksExport({body, parseAs: 'blob'})
	return expectBlob(data, 'Report export')
}

export async function fetchTasksCsv(draft: TaskExportDraft): Promise<Blob> {
	const body: TaskExportRequestWritable = {}
	if (draft.project_id && draft.project_id !== 0) {
		body.project_id = draft.project_id
	}
	if (draft.filter?.trim()) {
		body.filter = draft.filter.trim()
	}
	if (draft.filter_include_nulls) {
		body.filter_include_nulls = true
	}
	const {data} = await reportsTasksExportRows({body, parseAs: 'blob'})
	return expectBlob(data, 'Task export')
}

export async function fetchWidgetReportCsv(type: 'status_time' | 'workload' | 'time_tracking', config: Record<string, unknown>): Promise<Blob> {
	switch (type) {
		case 'status_time': {
			const {data} = await reportsStatusTimeExport({body: statusTimeBody(config), parseAs: 'blob'})
			return expectBlob(data, 'Status time export')
		}
		case 'workload': {
			const {data} = await reportsWorkloadExport({body: workloadBody(config), parseAs: 'blob'})
			return expectBlob(data, 'Workload export')
		}
		case 'time_tracking': {
			const {data} = await reportsTimeTrackingExport({body: timeTrackingBody(config), parseAs: 'blob'})
			return expectBlob(data, 'Time tracking export')
		}
	}
}
