import {describe, expect, it} from 'vitest'

import {
	controlForParam,
	defaultWidgets,
	normalizeReportRequest,
	presetForRange,
	rangeForPreset,
	RANGE_PRESETS,
	requestsForWidget,
	serverFieldError,
	summarizeFilter,
	widgetFilter,
	OFFERED_WIDGET_TYPES,
} from './dashboards'
import {layoutOf} from './dashboards'
import {collides} from './dashboardLayout'

describe('normalizeReportRequest', () => {
	it('drops empty and implied values in a stable order', () => {
		expect(normalizeReportRequest({
			limit: 0,
			measure: 'count',
			field: 'percent_done',
			filter: '  ',
			then_by: 'done',
			group_by: '',
			project_id: 0,
			time: {field: '', bucket: 'day'},
		})).toEqual({})
	})

	it('keeps what the server needs', () => {
		const out = normalizeReportRequest({
			limit: 10,
			time: {bucket: 'week', field: 'created', from: 'now-30d', to: 'now'},
			measure: 'avg',
			field: 'percent_done',
			group_by: 'assignee',
			then_by: 'status',
			filter: ' done = false ',
			project_id: 4,
		})
		expect(Object.keys(out)).toEqual(['project_id', 'filter', 'group_by', 'then_by', 'measure', 'field', 'time', 'limit'])
		expect(out.filter).toBe('done = false')
		expect(Object.keys(out.time!)).toEqual(['field', 'bucket', 'from', 'to'])
	})

	it('ignores a second dimension equal to the first and a field for count', () => {
		expect(normalizeReportRequest({group_by: 'done', then_by: 'done', field: 'x'})).toEqual({group_by: 'done'})
		expect(normalizeReportRequest({then_by: 'done'})).toEqual({})
	})
})

describe('relative ranges', () => {
	it('gives relative expressions the server resolves', () => {
		expect(rangeForPreset('last30')).toEqual({from: 'now-30d', to: 'now'})
		expect(rangeForPreset('thisMonth')).toEqual({from: 'now/M', to: 'now'})
		expect(rangeForPreset('thisYear')).toEqual({from: 'now/y', to: 'now'})
	})

	it('recognises a preset and treats the default as 90 days', () => {
		for (const preset of RANGE_PRESETS) {
			const {from, to} = rangeForPreset(preset)
			expect(presetForRange(from, to)).toBe(preset)
		}
		expect(presetForRange(undefined, undefined)).toBe('last90')
		expect(presetForRange('2026-01-01', 'now')).toBe('custom')
	})
})

describe('requestsForWidget', () => {
	it('uses the stored request for charts', () => {
		expect(requestsForWidget('bar', {request: {group_by: 'assignee', filter: 'done = false', measure: 'count'}}))
			.toEqual([{filter: 'done = false', group_by: 'assignee'}])
	})

	it('needs a time range for a line', () => {
		expect(requestsForWidget('line', {request: {group_by: 'done'}})).toEqual([])
		expect(requestsForWidget('line', {request: {time: {field: 'created'}}})).toHaveLength(1)
	})

	it('asks created and completed separately for created_vs_done', () => {
		const [created, done] = requestsForWidget('created_vs_done', {project_id: 3, filter: 'done = false', from: 'now-30d', to: 'now'})
		expect(created).toEqual({project_id: 3, filter: 'done = false', time: {field: 'created', bucket: 'day', from: 'now-30d', to: 'now'}})
		expect(done!.time!.field).toBe('done_at')
	})

	it('has no report for lists and text', () => {
		expect(requestsForWidget('task_list', {})).toEqual([])
		expect(requestsForWidget('text', {text: 'x'})).toEqual([])
	})
})

describe('widget accessors', () => {
	it('reads the filter from either config shape', () => {
		expect(widgetFilter('bar', {request: {filter: 'a'}})).toBe('a')
		expect(widgetFilter('task_list', {filter: 'b'})).toBe('b')
		expect(widgetFilter('text', {text: 'x'})).toBe('')
	})

	it('summarizes a long filter', () => {
		expect(summarizeFilter('done  =   false')).toBe('done = false')
		expect(summarizeFilter('x'.repeat(100), 10)).toBe('xxxxxxxxx…')
	})
})

describe('defaultWidgets', () => {
	const titles = {open: 'a', status: 'b', done: 'c', createdVsDone: 'd', assignee: 'e', overdue: 'f'}

	it('only uses types the UI offers and fits the grid without overlap', () => {
		const widgets = defaultWidgets({projectId: 5, hasWorkflow: true, titles})
		expect(widgets.every(w => (OFFERED_WIDGET_TYPES as readonly string[]).includes(w.type))).toBe(true)
		const items = layoutOf(widgets.map((w, i) => ({...w, id: i + 1})))
		items.forEach(a => {
			expect(a.x + a.w).toBeLessThanOrEqual(12)
			items.forEach(b => expect(collides(a, b)).toBe(false))
		})
	})

	it('groups by status only with a workflow', () => {
		const withFlow = defaultWidgets({projectId: 5, hasWorkflow: true, titles}).find(w => w.type === 'donut')!
		const without = defaultWidgets({projectId: 5, hasWorkflow: false, titles}).find(w => w.type === 'donut')!
		expect((withFlow.config.request as {group_by: string}).group_by).toBe('status')
		expect((without.config.request as {group_by: string}).group_by).toBe('done')
		expect(without.title).toBe('c')
	})

	it('scopes to the project', () => {
		for (const w of defaultWidgets({projectId: 5, hasWorkflow: false, titles})) {
			const scope = w.config.request ? (w.config.request as {project_id: number}).project_id : (w.config as {project_id: number}).project_id
			expect(scope).toBe(5)
		}
	})
})

describe('server errors', () => {
	it('maps a widget path to the control name', () => {
		expect(controlForParam('widgets[1].config.request.group_by')).toBe('group_by')
		expect(controlForParam('widgets[0].config.filter')).toBe('filter')
		expect(controlForParam('time.from')).toBe('time.from')
		expect(controlForParam(undefined)).toBe('')
	})

	it('only reads report and dashboard validation errors', () => {
		expect(serverFieldError({code: 23001, details: {param: 'group_by'}, detail: 'bad'})).toEqual({param: 'group_by', control: 'group_by', message: 'bad', code: 23001})
		expect(serverFieldError({code: 23008, details: {param: 'widgets[2].config.request.then_by'}, detail: 'x'})!.control).toBe('then_by')
		expect(serverFieldError({code: 23002, details: {}, detail: 'x'})!.control).toBe('time')
		expect(serverFieldError({code: 4019, detail: 'bad value'})).toEqual({param: 'filter', control: 'filter', message: 'bad value', code: 4019})
		expect(serverFieldError({code: 500})).toBeNull()
		expect(serverFieldError(null)).toBeNull()
	})
})
