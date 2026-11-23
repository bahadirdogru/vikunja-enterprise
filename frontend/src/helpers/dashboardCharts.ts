import type {StatusTimeResponse, TimeTrackingResponse, WorkloadResponse} from '@/client/queries/reportWidgets'
import {NEUTRAL_COLOR, type ChartGroup} from '@/helpers/charts'

// Turns the answers of the 4c reports into the groups the bar chart draws. Pure, so the mapping is tested without a DOM.

export type StatusTimeMetric = 'average' | 'median' | 'total'
export type WorkloadMetric = 'count' | 'estimate'

export const STATUS_TIME_METRICS: StatusTimeMetric[] = ['average', 'median', 'total']
export const WORKLOAD_METRICS: WorkloadMetric[] = ['count', 'estimate']

export const WORKLOAD_BUCKET_COLORS: Record<string, string> = {
	overdue: 'var(--danger)',
	this_week: 'var(--warning)',
	next_week: 'var(--info)',
	later: 'var(--primary)',
	no_due_date: NEUTRAL_COLOR,
}

type StatusStat = StatusTimeResponse['statuses'][number]

function statusValue(stat: StatusStat, metric: StatusTimeMetric): number {
	switch (metric) {
		case 'median':
			return stat.median_seconds
		case 'total':
			return stat.total_seconds
		default:
			return stat.average_seconds
	}
}

// One bar per status, in the order of the workflow. With a split, every status has one child per assignee or label,
// drawn side by side (adding durations of different people into a stack would hide what the bars say).
export function statusTimeGroups(report: StatusTimeResponse, metric: StatusTimeMetric): ChartGroup[] {
	const split = report.group_by !== '' && report.groups.length > 0
	return report.statuses.map(stat => {
		const group: ChartGroup = {
			key: stat.status_id,
			label: stat.title,
			color: stat.color || undefined,
			value: statusValue(stat, metric),
		}
		if (split) {
			group.children = report.groups.flatMap(g => {
				const own = g.statuses.find(s => s.status_id === stat.status_id)
				return own ? [{key: g.key, label: g.label, color: g.color || undefined, none: g.none, value: statusValue(own, metric)}] : []
			})
		}
		return group
	})
}

export type WorkloadLabels = {
	unassigned: string
	buckets: Record<string, string>
}

// One bar per assignee, stacked by due bucket. Empty buckets are left out so the legend only lists what exists.
export function workloadGroups(report: WorkloadResponse, metric: WorkloadMetric, labels: WorkloadLabels): ChartGroup[] {
	return report.rows.map(row => {
		const values = metric === 'estimate' ? row.estimates : row.counts
		const children = report.buckets.flatMap((bucket, i): ChartGroup[] => {
			const value = values[i] ?? 0
			return value > 0 ? [{key: bucket, label: labels.buckets[bucket] ?? bucket, color: WORKLOAD_BUCKET_COLORS[bucket], value}] : []
		})
		return {
			key: row.none ? 'unassigned' : row.key,
			label: row.none ? labels.unassigned : row.label,
			color: row.none ? NEUTRAL_COLOR : undefined,
			value: metric === 'estimate' ? row.estimate_total : row.total,
			children,
		}
	})
}

export function timeTrackingGroups(report: TimeTrackingResponse): ChartGroup[] {
	return report.groups.map(g => ({
		key: g.key,
		label: g.label,
		color: g.color || undefined,
		value: g.seconds,
		none: g.none,
		other: g.other,
	}))
}
