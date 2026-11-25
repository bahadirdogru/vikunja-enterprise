import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	reportsTasks: vi.fn(), reportsTasksOptions: vi.fn(), reportsTasksExport: vi.fn(), reportsTasksExportRows: vi.fn(),
	reportsStatusTime: vi.fn(), reportsWorkload: vi.fn(), reportsTimeTracking: vi.fn(),
	reportsStatusTimeExport: vi.fn(), reportsWorkloadExport: vi.fn(), reportsTimeTrackingExport: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)

import {
	normalizeComparison, normalizeGroup, normalizeReport, normalizeReportOptions, reportKeys, reportOptionsQuery, REPORT_STALE_TIME, taskReportQuery,
} from './reports'
import {
	fetchReportCsv, fetchTasksCsv, fetchWidgetReportCsv, normalizeStatusTime, normalizeTimeTracking, normalizeWorkload, statusTimeBody,
	statusTimeQuery, timeTrackingQuery, workloadBody, workloadQuery,
} from './reportWidgets'

beforeEach(() => Object.values(sdk).forEach(mock => mock.mockReset()))

describe('normalizeReport', () => {
	it('fills the defaults of an empty answer', () => {
		expect(normalizeReport({} as never)).toEqual({
			groups: [], series: [], total: 0, value: 0, measure: 'count', field: '', unit: '', multi_valued: false, excluded: 0, truncated: false, comparison: null, time: null,
		})
	})

	it('keeps nested groups with their series, flags and keys', () => {
		const report = normalizeReport({
			groups: [{key: 3, label: 'To do', color: '#fff', value: 5, other: true, children: [{key: 'a', label: 'A', value: 2, none: true, series: [{bucket: '2026-11-01', value: 4}]}]}],
			total: 9, value: 5, measure: 'sum', field: 'percent_done', unit: 'tasks', multi_valued: true, excluded: 2, truncated: true,
		} as never)
		expect(report).toMatchObject({total: 9, value: 5, measure: 'sum', field: 'percent_done', unit: 'tasks', multi_valued: true, excluded: 2, truncated: true})
		expect(report.groups[0]).toMatchObject({key: 3, label: 'To do', color: '#fff', value: 5, other: true, none: false})
		expect(report.groups[0]!.children[0]).toMatchObject({key: 'a', none: true, series: [{bucket: '2026-11-01', value: 4}]})
	})

	it('keeps a null key as null (the "no value" group)', () => {
		expect(normalizeGroup({label: 'None', none: true} as never).key).toBeNull()
	})

	it('reads the time part only when it names a field, with a day bucket by default', () => {
		expect(normalizeReport({time: {bucket: 'week'}} as never).time).toBeNull()
		expect(normalizeReport({time: {field: 'created', from: '2026-01-01', to: '2026-02-01', timezone: 'Europe/Istanbul', buckets: 31}} as never).time)
			.toEqual({field: 'created', bucket: 'day', from: '2026-01-01', to: '2026-02-01', timezone: 'Europe/Istanbul', buckets: 31})
	})

	it('keeps the comparison with a null percentage when the previous period was empty', () => {
		expect(normalizeComparison({} as never)).toBeNull()
		expect(normalizeComparison({comparison: {value: 4, total: 9, delta: 4, from: 'a', to: 'b'}} as never)).toEqual({value: 4, total: 9, delta: 4, delta_percent: null, from: 'a', to: 'b'})
		expect(normalizeComparison({comparison: {delta_percent: -12.5}} as never)!.delta_percent).toBe(-12.5)
	})
})

describe('normalizeReportOptions', () => {
	it('fills the documented limits and keeps dimensions, measures and time fields', () => {
		const empty = normalizeReportOptions({} as never)
		expect(empty.limits).toEqual({max_dimensions: 2, max_groups: 200, max_buckets: 366, default_limit: 20})
		expect(empty).toMatchObject({project_id: 0, has_workflow: false, dimensions: [], measures: [], time_fields: [], buckets: []})

		const full = normalizeReportOptions({
			project_id: 3, has_workflow: true, buckets: ['day', 'week'],
			dimensions: [{name: 'cf_4', title_key: 'k', label: 'Budget', multi_valued: true, custom_field_id: 4, project_id: 3, field_type: 'select'}],
			measures: [{name: 'sum', title_key: 'm', label: 'Sum', unit: 'tasks', fields: [{name: 'percent_done'}]}],
			time_fields: [{name: 'created'}],
			limits: {max_dimensions: 3, max_groups: 10, max_buckets: 12, default_limit: 5},
		} as never)
		expect(full.dimensions[0]).toEqual({name: 'cf_4', title_key: 'k', label: 'Budget', multi_valued: true, custom_field_id: 4, project_id: 3, field_type: 'select'})
		expect(full.measures[0]!.fields[0]!.name).toBe('percent_done')
		expect(full.limits.max_buckets).toBe(12)
	})
})

describe('report queries', () => {
	it('asks for the options of a project, or of none for project 0', async () => {
		sdk.reportsTasksOptions.mockResolvedValue({data: {}})
		const client = new QueryClient()
		await client.fetchQuery(reportOptionsQuery(3))
		await client.fetchQuery(reportOptionsQuery(0))
		expect(sdk.reportsTasksOptions.mock.calls.map(c => c[0].query)).toEqual([{project_id: 3}, {}])
	})

	it('sends the normalised request, so equal widgets share one cache entry, and shows no retry', async () => {
		sdk.reportsTasks.mockResolvedValue({data: {total: 3}})
		const client = new QueryClient()
		const a = taskReportQuery({project_id: 3, group_by: 'status', measure: 'count', filter: '  ', limit: 0})
		const b = taskReportQuery({project_id: 3, group_by: 'status'})
		expect(a.queryKey).toEqual(b.queryKey)
		expect(a.queryKey).toEqual(reportKeys.run({project_id: 3, group_by: 'status'}))
		expect((await client.fetchQuery(a)).total).toBe(3)
		expect(sdk.reportsTasks.mock.calls[0]![0].body).toEqual({project_id: 3, group_by: 'status'})
		expect(a.retry).toBe(false)
		expect(a.staleTime).toBe(REPORT_STALE_TIME)
		expect(a.meta).toEqual({handlesError: true})
		expect(taskReportQuery({}, false).enabled).toBe(false)
	})
})

describe('widget reports', () => {
	it('drops empty values from the request body: "", 0, null, undefined (false and non-empty are kept)', () => {
		expect(workloadBody({filter: '', project_id: 0, estimate_field: null, x: undefined, done: false, buckets: ['a']})).toEqual({done: false, buckets: ['a']})
		expect(statusTimeBody({group_by: 'assignee', filter: 'done = false'})).toEqual({group_by: 'assignee', filter: 'done = false'})
	})

	it('normalises the status time report: statuses, groups and flags', () => {
		const r = normalizeStatusTime({
			statuses: [{status_id: 1, title: 'To do', tasks: 4, visits: 6, open_tasks: 1, total_seconds: 100, average_seconds: 25, median_seconds: 20, max_seconds: 60}],
			groups: [{key: 'ada', label: 'Ada', tasks: 2, statuses: [{status_id: 1}]}],
			tasks: 4, group_by: 'assignee', multi_valued: true, truncated: true,
		} as never)
		expect(r.statuses[0]).toEqual({status_id: 1, title: 'To do', color: '', category: '', tasks: 4, visits: 6, open_tasks: 1, total_seconds: 100, average_seconds: 25, median_seconds: 20, max_seconds: 60})
		expect(r.groups[0]).toMatchObject({key: 'ada', label: 'Ada', none: false, tasks: 2})
		expect(r.groups[0]!.statuses[0]!.status_id).toBe(1)
		expect(r).toMatchObject({tasks: 4, group_by: 'assignee', multi_valued: true, truncated: true})
	})

	it('normalises workload rows and totals, with an empty totals row when the server sends none', () => {
		const r = normalizeWorkload({
			buckets: ['overdue', 'later'],
			rows: [{key: 3, label: 'Ada', counts: [1, 2], total: 3, estimates: [60, 0], estimate_total: 60}],
			totals: {total: 3, counts: [1, 2]}, estimate_field: 'cf_2', truncated: true,
		} as never)
		expect(r.rows[0]).toEqual({key: 3, label: 'Ada', none: false, counts: [1, 2], total: 3, estimates: [60, 0], estimate_total: 60})
		expect(r.totals).toMatchObject({total: 3, counts: [1, 2], label: ''})
		expect(r).toMatchObject({buckets: ['overdue', 'later'], estimate_field: 'cf_2', truncated: true})
		expect(normalizeWorkload({} as never).totals).toEqual({key: null, label: '', none: false, counts: [], total: 0, estimates: [], estimate_total: 0})
	})

	it('normalises time tracking groups and the running count', () => {
		const r = normalizeTimeTracking({
			groups: [{key: 'x', label: 'X', color: '#abc', seconds: 90, entries: 2, other: true}], seconds: 90, entries: 2, running: 1, group_by: 'day', multi_valued: false, truncated: false,
		} as never)
		expect(r.groups[0]).toEqual({key: 'x', label: 'X', color: '#abc', seconds: 90, entries: 2, none: false, other: true})
		expect(r).toMatchObject({seconds: 90, entries: 2, running: 1, group_by: 'day'})
	})

	it.each([
		['status time', statusTimeQuery, 'reportsStatusTime'],
		['workload', workloadQuery, 'reportsWorkload'],
		['time tracking', timeTrackingQuery, 'reportsTimeTracking'],
	] as const)('%s: sends the compacted config, shares the key of an equal config and handles its own errors', async (_name, make, call) => {
		sdk[call].mockResolvedValue({data: {}})
		const options = make({project_id: 3, filter: ''})
		expect(options.queryKey).toEqual(make({project_id: 3}).queryKey)
		await new QueryClient().fetchQuery(options as never)
		expect(sdk[call].mock.calls[0]![0].body).toEqual({project_id: 3})
		expect(options.meta).toEqual({handlesError: true})
		expect(make({}, false).enabled).toBe(false)
	})
})

describe('csv downloads', () => {
	const blob = new Blob(['a,b'])

	it('downloads the report with the normalised request as a blob', async () => {
		sdk.reportsTasksExport.mockResolvedValue({data: blob})
		expect(await fetchReportCsv({project_id: 3, filter: ' ', group_by: 'status'})).toBe(blob)
		expect(sdk.reportsTasksExport.mock.calls[0]![0]).toMatchObject({body: {project_id: 3, group_by: 'status'}, parseAs: 'blob'})
	})

	it('exports the tasks of a drill-down with the trimmed filter and the null flag only when set', async () => {
		sdk.reportsTasksExportRows.mockResolvedValue({data: blob})
		await fetchTasksCsv({project_id: 3, filter: '  done = false ', filter_include_nulls: true})
		expect(sdk.reportsTasksExportRows.mock.calls[0]![0].body).toEqual({project_id: 3, filter: 'done = false', filter_include_nulls: true})
		await fetchTasksCsv({project_id: 0, filter: '   '})
		expect(sdk.reportsTasksExportRows.mock.calls[1]![0].body).toEqual({})
	})

	it.each([
		['status_time', 'reportsStatusTimeExport'],
		['workload', 'reportsWorkloadExport'],
		['time_tracking', 'reportsTimeTrackingExport'],
	] as const)('exports a %s widget through its own endpoint', async (type, call) => {
		sdk[call].mockResolvedValue({data: blob})
		expect(await fetchWidgetReportCsv(type, {project_id: 3, filter: ''})).toBe(blob)
		expect(sdk[call].mock.calls[0]![0]).toEqual({body: {project_id: 3}, parseAs: 'blob'})
	})

	it('refuses an answer that is not a file', async () => {
		sdk.reportsTasksExport.mockResolvedValue({data: {not: 'a blob'}})
		await expect(fetchReportCsv({})).rejects.toThrow('not a blob')
	})
})
