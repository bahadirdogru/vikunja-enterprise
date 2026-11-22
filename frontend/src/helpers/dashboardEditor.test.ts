import {describe, expect, it} from 'vitest'

import {configFromDraft, draftFromWidget, draftProblems, emptyDraft, requestFromDraft} from './dashboardEditor'

describe('configFromDraft', () => {
	it('builds a bar request without empty parts', () => {
		const draft = {...emptyDraft('bar', 4), groupBy: 'assignee', filter: ' done = false '}
		expect(configFromDraft(draft)).toEqual({request: {project_id: 4, filter: 'done = false', group_by: 'assignee'}})
	})

	it('sends then_by only for bars and the limit only for bars and donuts', () => {
		const donut = {...emptyDraft('donut'), groupBy: 'status', thenBy: 'done', limit: 5}
		expect(requestFromDraft(donut)).toEqual({group_by: 'status', limit: 5})
		const line = {...emptyDraft('line'), groupBy: 'status', thenBy: 'done', limit: 5}
		expect(requestFromDraft(line)).toEqual({
			group_by: 'status',
			time: {field: 'created', bucket: 'day', from: 'now-30d', to: 'now'},
		})
	})

	it('uses a field only with a measure other than count', () => {
		const draft = {...emptyDraft('number'), measure: 'sum', field: 'cf_3'}
		expect(requestFromDraft(draft)).toEqual({measure: 'sum', field: 'cf_3'})
		expect(requestFromDraft({...draft, measure: 'count'})).toEqual({})
	})

	it('turns a preset into a relative range and keeps a custom one', () => {
		const preset = {...emptyDraft('line'), preset: 'last7' as const}
		expect(requestFromDraft(preset).time).toMatchObject({from: 'now-7d', to: 'now'})
		const custom = {...emptyDraft('line'), preset: 'custom' as const, from: '2026-01-01', to: '2026-02-01'}
		expect(requestFromDraft(custom).time).toMatchObject({from: '2026-01-01', to: '2026-02-01'})
	})

	it('builds created_vs_done, task_list and text configs', () => {
		expect(configFromDraft({...emptyDraft('created_vs_done', 2), preset: 'last90', bucket: 'week'}))
			.toEqual({project_id: 2, bucket: 'week', from: 'now-90d', to: 'now'})
		expect(configFromDraft({...emptyDraft('task_list'), filter: 'done = false', sortBy: 'priority', order: 'desc', listLimit: 500}))
			.toEqual({filter: 'done = false', sort_by: ['priority'], order_by: ['desc'], limit: 100})
		expect(configFromDraft({...emptyDraft('text'), text: '<p>x</p>'})).toEqual({text: '<p>x</p>'})
	})
})

describe('draftFromWidget', () => {
	it('round trips every type', () => {
		const drafts = [
			{...emptyDraft('number', 3), filter: 'done = false', measure: 'avg', field: 'percent_done'},
			{...emptyDraft('bar', 3), groupBy: 'assignee', thenBy: 'status', limit: 8},
			{...emptyDraft('donut'), groupBy: 'priority'},
			{...emptyDraft('line'), groupBy: 'done', preset: 'thisMonth' as const, from: 'now/M', to: 'now', bucket: 'week'},
			{...emptyDraft('created_vs_done', 5), preset: 'last90' as const, from: 'now-90d', to: 'now'},
			{...emptyDraft('task_list', 5), filter: 'done = false', sortBy: 'priority', order: 'desc' as const, listLimit: 7},
			{...emptyDraft('text'), text: '<p>hi</p>'},
		]
		for (const draft of drafts) {
			const config = configFromDraft(draft)
			const back = draftFromWidget({type: draft.type, title: draft.title, config})
			expect(configFromDraft(back)).toEqual(config)
		}
	})

	it('reads a relative range as a preset and anything else as custom', () => {
		const preset = draftFromWidget({type: 'created_vs_done', title: '', config: {from: 'now-7d', to: 'now'}})
		expect(preset.preset).toBe('last7')
		const custom = draftFromWidget({type: 'created_vs_done', title: '', config: {from: '2026-01-01', to: 'now'}})
		expect(custom.preset).toBe('custom')
		expect(custom.from).toBe('2026-01-01')
	})
})

describe('draftProblems', () => {
	it('names the controls that block a request', () => {
		expect(draftProblems(emptyDraft('bar'))).toEqual([{control: 'group_by', key: 'groupByRequired'}])
		expect(draftProblems({...emptyDraft('number'), measure: 'sum'})).toEqual([{control: 'field', key: 'fieldRequired'}])
		expect(draftProblems({...emptyDraft('line'), preset: 'custom', from: '', to: '2026-01-01'}))
			.toEqual([{control: 'time.from', key: 'dateRequired'}])
		expect(draftProblems({...emptyDraft('task_list'), listLimit: 0})).toHaveLength(1)
		expect(draftProblems({...emptyDraft('bar'), groupBy: 'done'})).toEqual([])
	})
})
