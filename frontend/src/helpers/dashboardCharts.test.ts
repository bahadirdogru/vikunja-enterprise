import {describe, expect, it} from 'vitest'

import type {StatusTimeResponse, TimeTrackingResponse, WorkloadResponse} from '@/client/queries/reportWidgets'
import {statusTimeGroups, timeTrackingGroups, workloadGroups} from './dashboardCharts'

const stat = (id: number, title: string, average: number, median: number, total: number) => ({
	status_id: id, title, color: '', category: '', tasks: 2, visits: 2, open_tasks: 0,
	total_seconds: total, average_seconds: average, median_seconds: median, max_seconds: total,
})

describe('statusTimeGroups', () => {
	const report: StatusTimeResponse = {
		statuses: [stat(1, 'Todo', 100, 80, 200), stat(2, 'Doing', 50, 40, 100)],
		groups: [
			{key: 7, label: 'Ada', color: '', none: false, tasks: 1, statuses: [stat(1, 'Todo', 120, 120, 120)]},
			{key: null, label: 'None', color: '', none: true, tasks: 1, statuses: [stat(1, 'Todo', 80, 80, 80), stat(2, 'Doing', 50, 50, 50)]},
		],
		tasks: 2, group_by: 'assignee', multi_valued: true, truncated: false,
	}

	it('uses the chosen metric and keeps the workflow order', () => {
		expect(statusTimeGroups({...report, groups: [], group_by: ''}, 'average').map(g => [g.label, g.value])).toEqual([['Todo', 100], ['Doing', 50]])
		expect(statusTimeGroups({...report, groups: [], group_by: ''}, 'median').map(g => g.value)).toEqual([80, 40])
		expect(statusTimeGroups({...report, groups: [], group_by: ''}, 'total').map(g => g.value)).toEqual([200, 100])
	})

	it('splits a status by group and skips groups that never were in it', () => {
		const groups = statusTimeGroups(report, 'average')
		expect(groups[0]!.children!.map(c => [c.label, c.value])).toEqual([['Ada', 120], ['None', 80]])
		expect(groups[1]!.children!.map(c => c.label)).toEqual(['None'])
		expect(groups[0]!.children![1]!.none).toBe(true)
	})
})

describe('workloadGroups', () => {
	const row = (label: string, counts: number[], estimates: number[], none = false) => ({
		key: none ? null : 1, label, none, counts, total: counts.reduce((a, b) => a + b, 0), estimates, estimate_total: estimates.reduce((a, b) => a + b, 0),
	})
	const report: WorkloadResponse = {
		buckets: ['overdue', 'this_week', 'next_week', 'later', 'no_due_date'],
		rows: [row('Ada', [1, 0, 2, 0, 0], [5, 0, 3, 0, 0]), row('Unassigned', [0, 0, 0, 0, 4], [0, 0, 0, 0, 0], true)],
		totals: row('Total', [1, 0, 2, 0, 4], [5, 0, 3, 0, 0]), estimate_field: 'cf_1', truncated: false,
	}
	const labels = {unassigned: 'Atanmamış', buckets: {overdue: 'Gecikmiş', next_week: 'Gelecek hafta', no_due_date: 'Tarihsiz'}}

	it('stacks counts by due bucket and names the unassigned row', () => {
		const groups = workloadGroups(report, 'count', labels)
		expect(groups.map(g => [g.label, g.value])).toEqual([['Ada', 3], ['Atanmamış', 4]])
		expect(groups[0]!.children!.map(c => [c.label, c.value])).toEqual([['Gecikmiş', 1], ['Gelecek hafta', 2]])
		expect(groups[0]!.children![0]!.color).toBe('var(--danger)')
		expect(groups[1]!.none).toBeUndefined()
	})

	it('can show the estimate instead and leaves out empty buckets', () => {
		const groups = workloadGroups(report, 'estimate', labels)
		expect(groups.map(g => g.value)).toEqual([8, 0])
		expect(groups[1]!.children).toEqual([])
	})
})

describe('timeTrackingGroups', () => {
	it('maps seconds and the folded group', () => {
		const report: TimeTrackingResponse = {
			groups: [
				{key: 1, label: 'Ada', color: '#ff0000', seconds: 3600, entries: 2, none: false, other: false},
				{key: null, label: 'Other', color: '', seconds: 60, entries: 1, none: false, other: true},
			],
			seconds: 3660, entries: 3, running: 0, group_by: 'user', multi_valued: false, truncated: true,
		}
		const groups = timeTrackingGroups(report)
		expect(groups.map(g => g.value)).toEqual([3600, 60])
		expect(groups[0]!.color).toBe('#ff0000')
		expect(groups[1]!.other).toBe(true)
	})
})
