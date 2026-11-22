import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'

import BarChart from './BarChart.vue'
import {mountChart, statusGroups} from './testing'

enableAutoUnmount(afterEach)

describe('BarChart', () => {
	it('draws one focusable, labelled bar per group', () => {
		const wrapper = mountChart(BarChart, {groups: statusGroups})
		const bars = wrapper.findAll('.chart__mark')
		expect(bars).toHaveLength(3)
		expect(bars[0]!.attributes('aria-label')).toBe('In progress: 12 tasks')
		expect(bars[0]!.attributes('tabindex')).toBe('0')
		expect(bars[0]!.find('rect[fill="#3b82f6"]').exists()).toBe(true)
	})

	it('sizes bars in proportion on a nice axis', () => {
		const wrapper = mountChart(BarChart, {groups: statusGroups})
		const heights = wrapper.findAll('.chart__mark').map(m => Number(m.findAll('rect')[1]!.attributes('height')))
		expect(heights[0]!).toBeGreaterThan(heights[1]!)
		expect(heights[1]!).toBeGreaterThan(heights[2]!)
		expect(heights[0]! / heights[1]!).toBeCloseTo(12 / 7, 1)
		expect(wrapper.text()).toContain('15')
	})

	it('offers a data table with every value', () => {
		const wrapper = mountChart(BarChart, {groups: statusGroups, title: 'Tasks by status'})
		const table = wrapper.find('table')
		expect(table.find('caption').text()).toBe('Tasks by status')
		expect(table.findAll('tbody tr').map(r => r.text())).toEqual([
			'In progress12 tasks', 'To do7 tasks', 'Done3 tasks',
		])
	})

	it('emits the group on click, Enter and Space', async () => {
		const wrapper = mountChart(BarChart, {groups: statusGroups})
		const bars = wrapper.findAll('.chart__mark')
		await bars[1]!.trigger('click')
		await bars[0]!.trigger('keydown', {key: 'Enter'})
		await bars[2]!.trigger('keydown', {key: ' '})
		const events = wrapper.emitted('select')!.map(e => (e[0] as {group: {label: string}}).group.label)
		expect(events).toEqual(['To do', 'In progress', 'Done'])
	})

	it('does not emit for an inert bar and says why in the tooltip', async () => {
		const wrapper = mountChart(BarChart, {
			groups: statusGroups,
			disabledReason: (s: {group?: {label: string}}) => s.group?.label === 'Done' ? 'Cannot be listed' : null,
		})
		const done = wrapper.findAll('.chart__mark')[2]!
		expect(done.attributes('aria-disabled')).toBe('true')
		await done.trigger('click')
		expect(wrapper.emitted('select')).toBeUndefined()
		await done.trigger('pointerenter')
		expect(wrapper.find('.chart-tooltip').text()).toContain('Cannot be listed')
	})

	it('shows a tooltip on focus', async () => {
		const wrapper = mountChart(BarChart, {groups: statusGroups})
		await wrapper.findAll('.chart__mark')[0]!.trigger('focus')
		expect(wrapper.find('.chart-tooltip').text()).toContain('12 tasks')
		await wrapper.findAll('.chart__mark')[0]!.trigger('blur')
		expect(wrapper.find('.chart-tooltip').exists()).toBe(false)
	})

	it('renders horizontally and truncates long labels with the full text in a title', () => {
		const long = 'An extraordinarily long group name that cannot fit'
		const wrapper = mountChart(BarChart, {groups: [{key: 1, label: long, value: 4}, {key: 2, label: 'x', value: 1}], orientation: 'horizontal'})
		const text = wrapper.findAll('.chart__mark')[0]!.find('text.chart__text')
		expect(text.text().endsWith('…') || text.text().includes(long)).toBe(true)
		expect(text.find('title').text()).toBe(long)
		expect(wrapper.findAll('.chart__mark')[0]!.attributes('aria-label')).toBe(`${long}: 4 tasks`)
	})

	it('stacks children and lists them in a legend', async () => {
		const groups = [
			{key: 1, label: 'Ada', value: 5, children: [{key: 1, label: 'Open', color: '#94a3b8', value: 2}, {key: 2, label: 'Done', color: '#22c55e', value: 3}]},
			{key: 2, label: 'Bob', value: 4, children: [{key: 1, label: 'Open', color: '#94a3b8', value: 4}]},
		]
		const wrapper = mountChart(BarChart, {groups})
		expect(wrapper.findAll('.chart-legend__item').map(l => l.text())).toEqual(['Open', 'Done'])
		const ada = wrapper.findAll('.chart__mark')[0]!
		expect(ada.findAll('rect[fill="#22c55e"]')).toHaveLength(1)
		expect(ada.attributes('aria-label')).toContain('Open: 2 tasks')
		await ada.findAll('rect[fill="#22c55e"]')[0]!.trigger('click')
		expect((wrapper.emitted('select')![0]![0] as {child: {label: string}}).child.label).toBe('Done')
		expect(wrapper.findAll('table tbody tr')).toHaveLength(3)
	})

	it('draws grouped bars when not stacked', () => {
		const groups = [{key: 1, label: 'Ada', value: 5, children: [{key: 1, label: 'Open', value: 2}, {key: 2, label: 'Done', value: 3}]}]
		const wrapper = mountChart(BarChart, {groups, stacked: false})
		const [, a, b] = wrapper.findAll('.chart__mark')[0]!.findAll('rect').map(r => r.attributes())
		expect(Number(a!.x)).not.toBe(Number(b!.x))
	})

	it('renders an other group neutrally', () => {
		const wrapper = mountChart(BarChart, {groups: [{key: 1, label: 'A', value: 3}, {key: null, label: 'Other', value: 2, other: true, color: '#ff0000'}]})
		expect(wrapper.findAll('.chart__mark')[1]!.find('rect[fill="var(--grey-400)"]').exists()).toBe(true)
	})

	it('notes that tasks can count in several groups', () => {
		expect(mountChart(BarChart, {groups: statusGroups, multiValued: true}).find('.chart__note').exists()).toBe(true)
		expect(mountChart(BarChart, {groups: statusGroups}).find('.chart__note').exists()).toBe(false)
	})

	it('shows the empty state and the loading skeleton', () => {
		expect(mountChart(BarChart, {groups: []}).text()).toContain('No data for this filter')
		const loading = mountChart(BarChart, {groups: [], loading: true})
		expect(loading.find('.chart-state.is-loading').exists()).toBe(true)
		expect(loading.text()).not.toContain('No data')
	})

	it('shows durations for seconds', () => {
		const wrapper = mountChart(BarChart, {groups: [{key: 1, label: 'A', value: 5400}], unit: 'seconds'})
		expect(wrapper.find('.chart__mark').attributes('aria-label')).toBe('A: 1.5 h')
	})
})
