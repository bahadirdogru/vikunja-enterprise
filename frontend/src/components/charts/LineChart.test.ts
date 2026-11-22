import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'

import LineChart from './LineChart.vue'
import {mountChart} from './testing'

enableAutoUnmount(afterEach)

const points = (values: number[]) => values.map((value, i) => ({
	bucket: `2026-11-${String(i + 1).padStart(2, '0')}`,
	start: `2026-11-${String(i + 1).padStart(2, '0')}T00:00:00+03:00`,
	end: `2026-11-${String(i + 2).padStart(2, '0')}T00:00:00+03:00`,
	value,
}))

const series = [
	{key: 'created', label: 'Created', color: '#3b82f6', points: points([1, 4, 2, 6])},
	{key: 'done', label: 'Completed', color: '#22c55e', points: points([0, 2, 3, 3])},
]

describe('LineChart', () => {
	it('draws a path per series and a legend for several', () => {
		const wrapper = mountChart(LineChart, {series})
		expect(wrapper.findAll('path[stroke="#3b82f6"]')).toHaveLength(1)
		expect(wrapper.findAll('path[stroke="#22c55e"]')).toHaveLength(1)
		expect(wrapper.findAll('.chart-legend__item').map(i => i.text())).toEqual(['Created', 'Completed'])
	})

	it('has a data table with a column per series', () => {
		const wrapper = mountChart(LineChart, {series})
		expect(wrapper.findAll('table thead th').map(h => h.text())).toEqual(['Date', 'Created', 'Completed'])
		expect(wrapper.findAll('table tbody tr')).toHaveLength(4)
		expect(wrapper.findAll('table tbody tr')[1]!.text()).toBe('Nov 2' + '4 tasks' + '2 tasks')
	})

	it('moves with the arrow keys, announces the point and emits it on Enter', async () => {
		const wrapper = mountChart(LineChart, {series})
		const svg = wrapper.find('svg')
		await svg.trigger('focus')
		await svg.trigger('keydown', {key: 'ArrowRight'})
		await svg.trigger('keydown', {key: 'ArrowRight'})
		expect(wrapper.find('.line-chart__live').text()).toBe('Created, Nov 3: 2 tasks')
		expect(wrapper.find('.chart-tooltip').text()).toContain('Completed3 tasks')
		await svg.trigger('keydown', {key: 'ArrowDown'})
		await svg.trigger('keydown', {key: 'Enter'})
		const selection = wrapper.emitted('select')![0]![0] as {series: {key: string}, point: {bucket: string}}
		expect(selection.series.key).toBe('done')
		expect(selection.point.bucket).toBe('2026-11-03')
	})

	it('never goes past the ends', async () => {
		const wrapper = mountChart(LineChart, {series})
		const svg = wrapper.find('svg')
		await svg.trigger('focus')
		await svg.trigger('keydown', {key: 'ArrowLeft'})
		await svg.trigger('keydown', {key: 'End'})
		await svg.trigger('keydown', {key: 'ArrowRight'})
		await svg.trigger('keydown', {key: 'Enter'})
		expect((wrapper.emitted('select')![0]![0] as {point: {bucket: string}}).point.bucket).toBe('2026-11-04')
	})

	it('does not emit where drill-down is disabled', async () => {
		const wrapper = mountChart(LineChart, {series, disabledReason: () => 'not here'})
		const svg = wrapper.find('svg')
		await svg.trigger('focus')
		expect(wrapper.find('.chart-tooltip').text()).toContain('not here')
		await svg.trigger('keydown', {key: 'Enter'})
		expect(wrapper.emitted('select')).toBeUndefined()
	})

	it('shows empty and loading states', () => {
		expect(mountChart(LineChart, {series: []}).text()).toContain('No data for this filter')
		expect(mountChart(LineChart, {series: [], loading: true}).find('.is-loading').exists()).toBe(true)
	})

	it('fills the area under a single series', () => {
		const wrapper = mountChart(LineChart, {series: [series[0]]})
		expect(wrapper.findAll('path[opacity]')).toHaveLength(1)
		expect(mountChart(LineChart, {series}).findAll('path[opacity]')).toHaveLength(0)
	})
})
