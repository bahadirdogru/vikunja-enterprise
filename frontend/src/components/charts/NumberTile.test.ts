import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'

import NumberTile from './NumberTile.vue'
import {mountChart} from './testing'

enableAutoUnmount(afterEach)

describe('NumberTile', () => {
	it('shows the formatted value with an accessible label', () => {
		const wrapper = mountChart(NumberTile, {value: 1234, label: 'Open'})
		expect(wrapper.find('.number-tile__value').text()).toBe('1,234')
		expect(wrapper.find('button').attributes('aria-label')).toBe('Open: 1,234 tasks')
	})

	it('shows a delta against the previous period', () => {
		const up = mountChart(NumberTile, {value: 15, previous: 10})
		expect(up.find('.number-tile__delta').classes()).toContain('is-up')
		expect(up.find('.number-tile__delta').text()).toContain('+50%')
		const down = mountChart(NumberTile, {value: 5, previous: 10})
		expect(down.find('.number-tile__delta').classes()).toContain('is-down')
		expect(mountChart(NumberTile, {value: 5}).find('.number-tile__delta').exists()).toBe(false)
	})

	it('emits select and stays inert when disabled', async () => {
		const wrapper = mountChart(NumberTile, {value: 3})
		await wrapper.find('button').trigger('click')
		expect(wrapper.emitted('select')).toHaveLength(1)
		const disabled = mountChart(NumberTile, {value: 3, disabledReason: 'No'})
		await disabled.find('button').trigger('click')
		expect(disabled.emitted('select')).toBeUndefined()
	})

	it('formats durations', () => {
		expect(mountChart(NumberTile, {value: 7200, unit: 'seconds'}).find('.number-tile__value').text()).toBe('2 h')
	})

	it('shows a skeleton while loading', () => {
		expect(mountChart(NumberTile, {value: 0, loading: true}).find('.is-loading').exists()).toBe(true)
	})
})
