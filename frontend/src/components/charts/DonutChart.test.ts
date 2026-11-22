import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'

import DonutChart from './DonutChart.vue'
import {mountChart, statusGroups} from './testing'

enableAutoUnmount(afterEach)

describe('DonutChart', () => {
	it('draws a labelled slice per group with the share', () => {
		const wrapper = mountChart(DonutChart, {groups: statusGroups, total: 22})
		const slices = wrapper.findAll('.chart__mark')
		expect(slices).toHaveLength(3)
		expect(slices[0]!.attributes('aria-label')).toBe('In progress: 12 tasks (54.5%)')
		expect(wrapper.find('.donut-chart__total').text()).toBe('22')
	})

	it('lists the groups with values in the legend and offers a data table', () => {
		const wrapper = mountChart(DonutChart, {groups: statusGroups})
		expect(wrapper.findAll('.chart-legend__item').map(i => i.text())).toEqual([
			'In progress12 tasks', 'To do7 tasks', 'Done3 tasks',
		])
		expect(wrapper.findAll('table tbody tr')).toHaveLength(3)
	})

	it('emits the group from a slice and from its legend entry', async () => {
		const wrapper = mountChart(DonutChart, {groups: statusGroups})
		await wrapper.findAll('.chart__mark')[1]!.trigger('click')
		await wrapper.findAll('.chart-legend__entry')[2]!.trigger('click')
		await wrapper.findAll('.chart__mark')[0]!.trigger('keydown', {key: 'Enter'})
		expect(wrapper.emitted('select')!.map(e => (e[0] as {group: {label: string}}).group.label))
			.toEqual(['To do', 'Done', 'In progress'])
	})

	it('keeps a disabled slice inert', async () => {
		const wrapper = mountChart(DonutChart, {groups: statusGroups, disabledReason: () => 'nope'})
		const slice = wrapper.findAll('.chart__mark')[0]!
		expect(slice.attributes('aria-disabled')).toBe('true')
		await slice.trigger('click')
		expect(wrapper.emitted('select')).toBeUndefined()
	})

	it('is empty when every value is zero', () => {
		expect(mountChart(DonutChart, {groups: [{key: 1, label: 'A', value: 0}]}).text()).toContain('No data for this filter')
		expect(mountChart(DonutChart, {groups: [], loading: true}).find('.is-loading').exists()).toBe(true)
	})

	it('uses a neutral colour for other', () => {
		const wrapper = mountChart(DonutChart, {groups: [{key: 1, label: 'A', value: 3}, {key: null, label: 'Other', value: 2, other: true, color: '#f00'}]})
		expect(wrapper.findAll('.chart__mark')[1]!.find('path').attributes('fill')).toBe('var(--grey-400)')
	})
})
