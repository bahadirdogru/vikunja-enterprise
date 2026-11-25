import {afterEach, describe, expect, it} from 'vitest'
import {defineComponent, h, ref} from 'vue'
import {enableAutoUnmount, mount} from '@vue/test-utils'

import ChartLegend from './ChartLegend.vue'
import ChartState from './ChartState.vue'
import ChartTable from './ChartTable.vue'
import ChartTooltip from './ChartTooltip.vue'
import {useChartSize, useChartText} from './useChart'
import {i18n, mountChart} from './testing'

enableAutoUnmount(afterEach)

describe('ChartTable (the accessible form of every chart)', () => {
	const props = {caption: 'Tasks by status', columns: ['Status', 'Tasks'], rows: [['To do', '7 tasks'], ['<b>Done</b>', '3 tasks']]}

	it('is a labelled table with a caption, column headers and the first cell of a row as its row header', () => {
		const wrapper = mountChart(ChartTable, props)
		expect(wrapper.get('table').attributes('aria-label')).toBe('Chart data')
		expect(wrapper.get('caption').text()).toBe('Tasks by status')
		expect(wrapper.findAll('thead th').map(th => [th.text(), th.attributes('scope')])).toEqual([['Status', 'col'], ['Tasks', 'col']])
		expect(wrapper.findAll('tbody tr').map(tr => [tr.get('th').attributes('scope'), tr.get('th').text(), tr.get('td').text()])).toEqual([
			['row', 'To do', '7 tasks'], ['row', '<b>Done</b>', '3 tasks'],
		])
	})

	it('renders labels as text, never as markup', () => {
		expect(mountChart(ChartTable, props).find('tbody b').exists()).toBe(false)
	})

	it('handles a chart without rows and rows with several value columns', () => {
		expect(mountChart(ChartTable, {...props, rows: []}).findAll('tbody tr')).toHaveLength(0)
		const wide = mountChart(ChartTable, {caption: 'c', columns: ['Bucket', 'Created', 'Done'], rows: [['Nov 5', '3', '2']]})
		expect(wide.findAll('tbody td').map(td => td.text())).toEqual(['3', '2'])
	})
})

describe('ChartLegend', () => {
	const items = [
		{key: 'a', label: 'To do', color: '#94a3b8', value: '7', selectable: true},
		{key: 'b', label: 'A very very long label that must be cut somewhere', color: '#3b82f6'},
	]

	it('is a labelled list with a swatch, label and value per item', () => {
		const wrapper = mountChart(ChartLegend, {items, label: 'Legend'})
		expect(wrapper.get('ul').attributes('aria-label')).toBe('Legend')
		const rows = wrapper.findAll('li')
		expect(rows).toHaveLength(2)
		expect(rows[0]!.get('.chart-legend__swatch').attributes('style')).toContain('#94a3b8')
		expect(rows[0]!.get('.chart-legend__value').text()).toBe('7')
		expect(rows[1]!.find('.chart-legend__value').exists()).toBe(false)
	})

	it('cuts a long label for the eye but keeps the whole text as a title', () => {
		const second = mountChart(ChartLegend, {items}).findAll('li')[1]!
		expect(second.get('.chart-legend__label').text().endsWith('…')).toBe(true)
		expect(Array.from(second.get('.chart-legend__label').text()).length).toBeLessThanOrEqual(28)
		expect(second.get('.chart-legend__entry').attributes('title')).toBe(items[1]!.label)
	})

	it('makes only selectable items buttons, and selecting emits the key', async () => {
		const wrapper = mountChart(ChartLegend, {items})
		expect(wrapper.findAll('button')).toHaveLength(1)
		await wrapper.get('button').trigger('click')
		await wrapper.findAll('.chart-legend__entry')[1]!.trigger('click')
		expect(wrapper.emitted('select')).toEqual([['a']])
	})

	it('reports hover and keyboard focus of an item, and the end of both', async () => {
		const wrapper = mountChart(ChartLegend, {items})
		const li = wrapper.findAll('li')[0]!
		await li.trigger('mouseenter')
		await li.trigger('mouseleave')
		await wrapper.get('button').trigger('focus')
		await wrapper.get('button').trigger('blur')
		expect(wrapper.emitted('hover')).toEqual([['a'], [null], ['a'], [null]])
	})
})

describe('ChartState', () => {
	it('announces an error as an alert and everything else as a status', () => {
		expect(mountChart(ChartState, {kind: 'error'}).get('[role="alert"]').text()).toBe('The chart could not be loaded.')
		expect(mountChart(ChartState, {kind: 'empty'}).get('[role="status"]').text()).toBe('No data for this filter')
		expect(mountChart(ChartState, {kind: 'loading'}).get('[role="status"]').text()).toContain('Loading')
	})

	it('shows its own message instead of the default and a skeleton while loading', () => {
		expect(mountChart(ChartState, {kind: 'error', message: 'Too many values.'}).text()).toBe('Too many values.')
		const loading = mountChart(ChartState, {kind: 'loading'})
		expect(loading.findAll('.chart-state__bar')).toHaveLength(6)
		expect(loading.get('.chart-state__skeleton').attributes('aria-hidden')).toBe('true')
	})
})

describe('ChartTooltip', () => {
	const tooltip = {x: 10, y: 80, title: 'Nov 5', rows: [{label: 'Created', value: '3', color: '#3b82f6'}, {label: 'Done', value: '2'}], note: 'Several groups'}

	it('is hidden from assistive technology (the table is the accessible form) and shows title, rows and note', () => {
		const wrapper = mountChart(ChartTooltip, {tooltip, width: 400, height: 200})
		expect(wrapper.get('.chart-tooltip').attributes('aria-hidden')).toBe('true')
		expect(wrapper.get('.chart-tooltip__title').text()).toBe('Nov 5')
		expect(wrapper.findAll('.chart-tooltip__row').map(r => r.text())).toEqual(['Created3', 'Done2'])
		expect(wrapper.findAll('.chart-tooltip__swatch')).toHaveLength(1)
		expect(wrapper.get('.chart-tooltip__note').text()).toBe('Several groups')
	})

	it('renders nothing without a tooltip', () => {
		expect(mountChart(ChartTooltip, {tooltip: null, width: 400, height: 200}).find('.chart-tooltip').exists()).toBe(false)
	})

	it.each([
		[10, 150, '12px', 'calc(-100% - 8px)'],
		[300, 150, 'calc(-100% - 12px)', 'calc(-100% - 8px)'],
		[10, 10, '12px', '12px'],
		[300, 10, 'calc(-100% - 12px)', '12px'],
	])('puts a tooltip at x=%i y=%i on the side that stays inside the plot', (x, y, horizontal, vertical) => {
		const wrapper = mountChart(ChartTooltip, {tooltip: {...tooltip, x, y}, width: 400, height: 200})
		expect(wrapper.get('.chart-tooltip').attributes('style')).toContain(`translate(${horizontal}, ${vertical})`)
		expect(wrapper.get('.chart-tooltip').attributes('style')).toContain(`${x}px`)
	})
})

describe('useChartText', () => {
	function text(locale: string) {
		let result!: ReturnType<typeof useChartText>
		i18n.global.locale.value = locale as 'en'
		mount(defineComponent({setup() {
			result = useChartText()
			return () => h('div')
		}}), {global: {plugins: [i18n]}})
		return result
	}

	it('says tasks in English with a plural', () => {
		const t = text('en')
		expect(t.valueText(1, 'tasks')).toBe('1 task')
		expect(t.valueText(12, 'tasks')).toBe('12 tasks')
		expect(t.valueText(0, 'tasks')).toBe('no tasks')
		expect(t.valueText(1234.5)).toBe('1,234.5')
		expect(t.valueText(5400, 'seconds')).toBe('1.5 h')
		expect(t.ariaLabel('To do', 7, 'tasks')).toBe('To do: 7 tasks')
	})
})

describe('useChartSize', () => {
	it('keeps the fallback size until the element has a size', () => {
		let size!: ReturnType<typeof useChartSize>
		const el = ref<HTMLElement | null>(null)
		mount(defineComponent({setup() {
			size = useChartSize(el, {width: 300, height: 100})
			return () => h('div')
		}}))
		expect(size.value).toEqual({width: 300, height: 100})
		const element = document.createElement('div')
		element.getBoundingClientRect = () => ({width: 410.4, height: 199.6}) as DOMRect
		el.value = element
		return Promise.resolve().then(() => {
			expect(size.value).toEqual({width: 410, height: 200})
		})
	})
})
