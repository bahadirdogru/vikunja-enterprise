import {afterEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, mount} from '@vue/test-utils'
import {defineComponent} from 'vue'

vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn()}))

import DashboardGrid from './DashboardGrid.vue'
import {i18n} from '@/components/charts/testing'
import type {LayoutItem} from '@/helpers/dashboardLayout'

enableAutoUnmount(afterEach)

// The card carries the data fetching; the grid only needs its header and handles.
const CardStub = defineComponent({
	props: ['id', 'title'],
	emits: ['headerKeydown', 'dragStart', 'resizeStart'],
	template: '<article><div class="head" tabindex="0" @keydown="$emit(\'headerKeydown\', $event)">{{ title }}</div></article>',
})

const widgets = [
	{id: 1, type: 'number', title: 'A', config: {}, x: 0, y: 0, w: 4, h: 2},
	{id: 2, type: 'bar', title: 'B', config: {}, x: 4, y: 0, w: 4, h: 3},
	{id: 3, type: 'text', title: 'C', config: {}, x: 0, y: 2, w: 4, h: 2},
]

function mountGrid(editing = true, canWrite = true) {
	return mount(DashboardGrid, {
		props: {widgets, editing, canWrite, displays: {}},
		global: {plugins: [i18n], stubs: {WidgetCard: CardStub}},
	})
}

const lastLayout = (wrapper: ReturnType<typeof mountGrid>) => (wrapper.emitted('layout')!.at(-1)![0] as LayoutItem[])

describe('DashboardGrid', () => {
	it('places widgets from their x, y, w and h', () => {
		const items = mountGrid().findAll('.dashboard-grid__item')
		expect(items).toHaveLength(3)
		expect(items[1]!.attributes('style')).toContain('--col: 5 / span 4')
		expect(items[1]!.attributes('style')).toContain('--row: 1 / span 3')
	})

	it('keeps reading order for the single column', () => {
		const order = mountGrid().findAll('.head').map(h => h.text())
		expect(order).toEqual(['A', 'B', 'C'])
	})

	it('moves a widget with the arrow keys and emits the whole layout', async () => {
		const wrapper = mountGrid()
		await wrapper.findAll('.head')[0]!.trigger('keydown', {key: 'ArrowRight'})
		const layout = lastLayout(wrapper)
		expect(layout.find(i => i.id === 1)!.x).toBe(1)
		layout.forEach(a => layout.forEach(b => {
			if (a.id !== b.id) {
				expect(a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y).toBe(false)
			}
		}))
		expect(wrapper.find('[role=status]').text()).toContain('A moved to column')
	})

	it('does not float a widget down through the one below it', async () => {
		const wrapper = mountGrid()
		await wrapper.findAll('.head')[0]!.trigger('keydown', {key: 'ArrowDown'})
		expect(wrapper.emitted('layout')).toBeUndefined()
	})

	it('resizes with Shift and the arrow keys', async () => {
		const wrapper = mountGrid()
		await wrapper.findAll('.head')[1]!.trigger('keydown', {key: 'ArrowRight', shiftKey: true})
		expect(lastLayout(wrapper).find(i => i.id === 2)).toMatchObject({w: 5, h: 3})
	})

	it('does not move past the edge and ignores other keys', async () => {
		const wrapper = mountGrid()
		await wrapper.findAll('.head')[0]!.trigger('keydown', {key: 'ArrowLeft'})
		await wrapper.findAll('.head')[0]!.trigger('keydown', {key: 'a'})
		expect(wrapper.emitted('layout')).toBeUndefined()
	})
})
