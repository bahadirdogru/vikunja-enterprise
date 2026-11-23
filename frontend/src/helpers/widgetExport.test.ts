import {beforeEach, describe, expect, it, vi} from 'vitest'

const fetchTasksCsv = vi.fn(async (_params: unknown) => new Blob(['x']))
const fetchQuery = vi.fn()

vi.mock('@/client/queries/reportWidgets', () => ({
	fetchTasksCsv: (params: unknown) => fetchTasksCsv(params),
	fetchReportCsv: vi.fn(),
	fetchWidgetReportCsv: vi.fn(),
}))
vi.mock('@/client/queryClient', () => ({queryClient: {fetchQuery: (q: unknown) => fetchQuery(q)}}))
vi.mock('@/client/queries/reports', () => ({taskReportQuery: (request: unknown) => ({request})}))

import {canExportTasks, exportWidgetTasks, taskExportParams} from './widgetExport'

const numberConfig = (request: Record<string, unknown>) => ({request})

describe('exporting the tasks behind a widget', () => {
	beforeEach(() => {
		fetchTasksCsv.mockClear()
		fetchQuery.mockReset()
	})

	it('adds the open filter the workload report forces', () => {
		expect(taskExportParams('workload', {project_id: 3, filter: ''}).filter).toBe('done = false')
		expect(taskExportParams('workload', {project_id: 3, filter: 'priority = 3 || priority = 4'}).filter)
			.toBe('(priority = 3 || priority = 4) && done = false')
		expect(taskExportParams('status_time', {project_id: 3, filter: 'priority = 3'}).filter).toBe('priority = 3')
	})

	it('limits a number with a time range to that range', async () => {
		fetchQuery.mockResolvedValue({time: {from: '2026-01-01T00:00:00Z', to: '2026-03-11T00:00:00Z'}})
		const config = numberConfig({filter: 'done = false', time: {field: 'created', bucket: 'month', from: '2026-01-01', to: '2026-03-10'}})
		await exportWidgetTasks('number', config)
		expect(fetchTasksCsv).toHaveBeenCalledWith(expect.objectContaining({
			filter: 'done = false && created >= "2026-01-01T00:00:00Z" && created < "2026-03-11T00:00:00Z"',
		}))
	})

	it('exports a number without a time range by its filter alone', async () => {
		await exportWidgetTasks('number', numberConfig({filter: 'done = false'}))
		expect(fetchQuery).not.toHaveBeenCalled()
		expect(fetchTasksCsv).toHaveBeenCalledWith(expect.objectContaining({filter: 'done = false'}))
	})

	it('does not offer a list it cannot restrict to the range', () => {
		expect(canExportTasks('number', numberConfig({time: {field: 'status_entered', bucket: 'day', from: 'a', to: 'b'}}))).toBe(false)
		expect(canExportTasks('number', numberConfig({filter_include_nulls: true, time: {field: 'due_date', bucket: 'day', from: 'a', to: 'b'}}))).toBe(false)
		expect(canExportTasks('number', numberConfig({time: {field: 'created', bucket: 'day', from: 'a', to: 'b'}}))).toBe(true)
		expect(canExportTasks('number', numberConfig({filter: 'done = false'}))).toBe(true)
		expect(canExportTasks('time_tracking', {})).toBe(false)
	})
})
