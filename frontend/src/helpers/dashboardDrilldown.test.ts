import {describe, expect, it} from 'vitest'

import {drilldownFilter, isNullSafeFilter} from './dashboardDrilldown'
import type {ChartGroup} from './charts'

const g = (key: unknown, label = 'L', extra: Partial<ChartGroup> = {}): ChartGroup => ({key, label, value: 1, ...extra})
const none = (): ChartGroup => g(null, 'No value', {none: true})
const ok = (r: ReturnType<typeof drilldownFilter>) => {
	if (!r.ok) {
		throw new Error(`blocked: ${r.reason}`)
	}
	return r
}

describe('drilldownFilter', () => {
	it('uses the widget filter alone for a number', () => {
		expect(ok(drilldownFilter({baseFilter: 'done = false'})).filter).toBe('done = false')
		expect(ok(drilldownFilter({baseFilter: ''})).filter).toBe('')
	})

	it('ANDs a status id and wraps an or filter', () => {
		expect(ok(drilldownFilter({baseFilter: 'done = false', groupBy: 'status', group: g(7)})).filter)
			.toBe('done = false && status = 7')
		expect(ok(drilldownFilter({baseFilter: 'priority > 2 || done = true', groupBy: 'status', group: g(7)})).filter)
			.toBe('(priority > 2 || done = true) && status = 7')
	})

	it('expresses a task without a status through the categories', () => {
		const r = ok(drilldownFilter({baseFilter: '', groupBy: 'status', group: none()}))
		expect(r.filter).toBe('status_category not in open,in_progress,done,cancelled')
		expect(r.includeNulls).toBe(false)
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'status_category', group: g('in_progress')})).filter)
			.toBe('status_category = in_progress')
	})

	it('maps done, priority, project and label', () => {
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'done', group: g(true)})).filter).toBe('done = true')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'done', group: g(false)})).filter).toBe('done = false')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'priority', group: g(4)})).filter).toBe('priority = 4')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'priority', group: none()})).filter).toBe('priority = 0')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'project', group: g(3)})).filter).toBe('project = 3')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'label', group: g(9)})).filter).toBe('labels = 9')
	})

	it('needs the username for assignees and created_by', () => {
		expect(drilldownFilter({baseFilter: '', groupBy: 'assignee', group: g(5)})).toEqual({ok: false, reason: 'needs_username', userId: 5})
		const r = ok(drilldownFilter({baseFilter: '', groupBy: 'assignee', group: g(5), usernames: {5: 'ada'}}))
		expect(r.filter).toBe('assignees = "ada"')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'created_by', group: g(5), usernames: {5: 'ada'}})).filter)
			.toBe('created_by = "ada"')
	})

	it('maps custom fields by their type', () => {
		const fieldTypes = {cf_1: 'select', cf_2: 'checkbox', cf_3: 'rating', cf_4: 'user', cf_5: 'multi_select'}
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'cf_1', group: g(11), fieldTypes})).filter).toBe('cf_1 = 11')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'cf_5', group: g(11), fieldTypes})).filter).toBe('cf_5 = 11')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'cf_2', group: g(true), fieldTypes})).filter).toBe('cf_2 = true')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'cf_3', group: g(4), fieldTypes})).filter).toBe('cf_3 = 4')
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'cf_4', group: g(8), fieldTypes})).filter).toBe('cf_4 = 8')
	})

	it('expresses "no select value" as none of the options, when they are known', () => {
		const fieldTypes = {cf_1: 'select'}
		expect(drilldownFilter({baseFilter: '', groupBy: 'cf_1', group: none(), fieldTypes}))
			.toEqual({ok: false, reason: 'needs_options', userId: undefined})
		const r = ok(drilldownFilter({baseFilter: '', groupBy: 'cf_1', group: none(), fieldTypes, cfOptionIds: {cf_1: [11, 12]}}))
		expect(r.filter).toBe('cf_1 not in 11,12')
		expect(r.includeNulls).toBe(false)
	})

	it('uses filter_include_nulls for "no value" of multi valued dimensions, only when it cannot change the rest', () => {
		const r = ok(drilldownFilter({baseFilter: 'done = false', groupBy: 'assignee', group: none()}))
		expect(r.filter).toBe('done = false && assignees = 0')
		expect(r.includeNulls).toBe(true)
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'label', group: none()})).filter).toBe('labels = 0')

		expect(drilldownFilter({baseFilter: 'due_date < now', groupBy: 'assignee', group: none()}))
			.toMatchObject({ok: false, reason: 'null_filter'})
		expect(drilldownFilter({baseFilter: '', groupBy: 'assignee', thenBy: 'status', group: none(), child: g(3)}))
			.toMatchObject({ok: false, reason: 'null_filter'})
	})

	it('refuses the folded other group', () => {
		expect(drilldownFilter({baseFilter: '', groupBy: 'label', group: g(null, 'Other', {other: true})}))
			.toEqual({ok: false, reason: 'other'})
		expect(drilldownFilter({baseFilter: '', groupBy: 'label', thenBy: 'done', group: g(1), child: g(null, 'Other', {other: true})}))
			.toEqual({ok: false, reason: 'other'})
	})

	it('refuses when the widget itself includes nulls', () => {
		expect(drilldownFilter({baseFilter: '', includeNulls: true, groupBy: 'done', group: g(true)}))
			.toMatchObject({ok: false, reason: 'include_nulls'})
		expect(ok(drilldownFilter({baseFilter: 'done = false', includeNulls: true})).includeNulls).toBe(true)
	})

	it('adds the range of a time bucket on the time field', () => {
		const point = {bucket: '2026-11-02', start: '2026-11-02T00:00:00+03:00', end: '2026-11-03T00:00:00+03:00', value: 4}
		const r = ok(drilldownFilter({baseFilter: 'done = false', timeField: 'done_at', point}))
		expect(r.filter).toBe('done = false && done_at >= "2026-11-02T00:00:00+03:00" && done_at < "2026-11-03T00:00:00+03:00"')
		expect(drilldownFilter({baseFilter: '', timeField: 'status_entered', point}))
			.toMatchObject({ok: false, reason: 'status_entered'})
	})

	it('combines a group and a bucket', () => {
		const point = {bucket: '2026-11-02', start: 'a', end: 'b', value: 1}
		const r = ok(drilldownFilter({baseFilter: '', groupBy: 'status', group: g(2), timeField: 'created', point}))
		expect(r.filter).toBe('status = 2 && created >= "a" && created < "b"')
		expect(r.parts).toEqual(['L', '2026-11-02'])
	})

	it('allows "no value" with a created range because created is never null', () => {
		const point = {bucket: 'x', start: 'a', end: 'b', value: 1}
		expect(ok(drilldownFilter({baseFilter: '', groupBy: 'assignee', group: none(), timeField: 'created', point})).includeNulls).toBe(true)
		expect(drilldownFilter({baseFilter: '', groupBy: 'assignee', group: none(), timeField: 'done_at', point}))
			.toMatchObject({ok: false, reason: 'null_filter'})
	})
})

describe('isNullSafeFilter', () => {
	it('is true only for columns that are never null', () => {
		expect(isNullSafeFilter('')).toBe(true)
		expect(isNullSafeFilter('done = false && project in 1, 2')).toBe(true)
		expect(isNullSafeFilter('due_date < now')).toBe(false)
		expect(isNullSafeFilter('labels in 3')).toBe(false)
		expect(isNullSafeFilter('status = 1')).toBe(false)
	})
})
