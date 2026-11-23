import {keepPreviousData, queryOptions} from '@tanstack/vue-query'

import {queryClient} from '@/client/queryClient'
import {reportsTasks, reportsTasksOptions} from '@/client/generated'
import type {
	ReportGroup,
	ReportOption,
	ReportOptions,
	ReportPoint,
	ReportRequestWritable,
	TaskReport,
} from '@/client/generated'
import type {ChartGroup, ChartPoint} from '@/helpers/charts'
import {normalizeReportRequest, type ReportRequestDraft} from '@/helpers/dashboards'

export type ReportGroupResponse = ChartGroup & {
	children: ReportGroupResponse[]
	series: ChartPoint[]
}

export type TaskReportResponse = {
	groups: ReportGroupResponse[]
	series: ChartPoint[]
	total: number
	value: number
	measure: string
	field: string
	unit: string
	multi_valued: boolean
	excluded: number
	truncated: boolean
	comparison: ReportComparisonResponse | null
	time: {
		field: string
		bucket: string
		from: string
		to: string
		timezone: string
		buckets: number
	} | null
}

export type ReportOptionResponse = {
	name: string
	title_key: string
	label: string
	multi_valued: boolean
	custom_field_id: number
	project_id: number
	field_type: string
}

export type ReportOptionsResponse = {
	project_id: number
	has_workflow: boolean
	dimensions: ReportOptionResponse[]
	measures: {name: string, title_key: string, label: string, unit: string, fields: ReportOptionResponse[]}[]
	time_fields: ReportOptionResponse[]
	buckets: string[]
	limits: {max_dimensions: number, max_groups: number, max_buckets: number, default_limit: number}
}

function normalizePoint(point: ReportPoint): ChartPoint {
	return {bucket: point.bucket ?? '', start: point.start, end: point.end, value: point.value ?? 0}
}

export function normalizeGroup(group: ReportGroup): ReportGroupResponse {
	return {
		key: group.key ?? null,
		label: group.label ?? '',
		color: group.color ?? '',
		value: group.value ?? 0,
		none: group.none ?? false,
		other: group.other ?? false,
		children: (group.children ?? []).map(normalizeGroup),
		series: (group.series ?? []).map(normalizePoint),
	}
}

export function normalizeReport(report: TaskReport): TaskReportResponse {
	return {
		groups: (report.groups ?? []).map(normalizeGroup),
		series: (report.series ?? []).map(normalizePoint),
		total: report.total ?? 0,
		value: report.value ?? 0,
		measure: report.measure ?? 'count',
		field: report.field ?? '',
		unit: report.unit ?? '',
		multi_valued: report.multi_valued ?? false,
		excluded: report.excluded ?? 0,
		truncated: report.truncated ?? false,
		comparison: normalizeComparison(report),
		time: report.time?.field
			? {
				field: report.time.field,
				bucket: report.time.bucket ?? 'day',
				from: report.time.from ?? '',
				to: report.time.to ?? '',
				timezone: report.time.timezone ?? '',
				buckets: report.time.buckets ?? 0,
			}
			: null,
	}
}

function normalizeOption(option: ReportOption): ReportOptionResponse {
	return {
		name: option.name ?? '',
		title_key: option.title_key ?? '',
		label: option.label ?? '',
		multi_valued: option.multi_valued ?? false,
		custom_field_id: option.custom_field_id ?? 0,
		project_id: option.project_id ?? 0,
		field_type: option.field_type ?? '',
	}
}

export function normalizeReportOptions(options: ReportOptions): ReportOptionsResponse {
	return {
		project_id: options.project_id ?? 0,
		has_workflow: options.has_workflow ?? false,
		dimensions: (options.dimensions ?? []).map(normalizeOption),
		measures: (options.measures ?? []).map(measure => ({
			name: measure.name ?? '',
			title_key: measure.title_key ?? '',
			label: measure.label ?? '',
			unit: measure.unit ?? '',
			fields: (measure.fields ?? []).map(normalizeOption),
		})),
		time_fields: (options.time_fields ?? []).map(normalizeOption),
		buckets: options.buckets ?? [],
		limits: {
			max_dimensions: options.limits?.max_dimensions ?? 2,
			max_groups: options.limits?.max_groups ?? 200,
			max_buckets: options.limits?.max_buckets ?? 366,
			default_limit: options.limits?.default_limit ?? 20,
		},
	}
}

export const reportKeys = {
	all: ['reports'] as const,
	options: (projectId: number) => ['reports', 'options', projectId] as const,
	run: (request: ReportRequestDraft) => ['reports', 'run', request] as const,
}

export function reportOptionsQuery(projectId: number) {
	return queryOptions({
		queryKey: reportKeys.options(projectId),
		queryFn: async ({signal}) => normalizeReportOptions(
			(await reportsTasksOptions({query: projectId > 0 ? {project_id: projectId} : {}, signal})).data,
		),
		staleTime: 60 * 1000,
	})
}

// The server caches a request for 30s per identity; the client keeps the same freshness and refetches on focus.
export const REPORT_STALE_TIME = 30 * 1000

export function taskReportQuery(request: ReportRequestDraft, enabled = true) {
	const body = normalizeReportRequest(request)
	// The enums of the generated request are plain strings in the draft the editor builds.
	const payload = body as ReportRequestWritable
	return queryOptions({
		queryKey: reportKeys.run(body),
		queryFn: async ({signal}) => normalizeReport((await reportsTasks({body: payload, signal})).data),
		enabled,
		staleTime: REPORT_STALE_TIME,
		refetchOnWindowFocus: true,
		retry: false,
		placeholderData: keepPreviousData,
		// Widgets and the preview render their own error state.
		meta: {handlesError: true},
	})
}

export function ensureReportOptions(projectId: number) {
	return queryClient.ensureQueryData(reportOptionsQuery(projectId))
}

export type ReportComparisonResponse = {
	value: number
	total: number
	delta: number
	delta_percent: number | null
	from: string
	to: string
}

// The previous period of a number widget with compare set.
export function normalizeComparison(report: TaskReport): ReportComparisonResponse | null {
	const c = report.comparison
	if (!c) {
		return null
	}
	return {
		value: c.value ?? 0,
		total: c.total ?? 0,
		delta: c.delta ?? 0,
		delta_percent: c.delta_percent ?? null,
		from: c.from ?? '',
		to: c.to ?? '',
	}
}
