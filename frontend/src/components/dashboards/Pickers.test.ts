import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'

import MetricToggle from './MetricToggle.vue'
import WidgetTypePicker from './WidgetTypePicker.vue'
import en from '@/i18n/lang/en.json'
import trTR from '@/i18n/lang/tr-TR.json'
import {OFFERED_WIDGET_TYPES} from '@/helpers/dashboards'

enableAutoUnmount(afterEach)

const global = (locale: 'en' | 'tr-TR' = 'en') => ({
	plugins: [createI18n({legacy: false, locale, fallbackLocale: 'en', messages: {en, 'tr-TR': trTR}})],
	stubs: {Icon: true},
})

describe('MetricToggle', () => {
	const options = [{value: 'count', label: 'Tasks'}, {value: 'estimate', label: 'Estimate'}]
	const mountToggle = (modelValue: string) => mount(MetricToggle, {
		props: {options, label: 'Show', modelValue, 'onUpdate:modelValue': (v: string) => wrapper.setProps({modelValue: v})} as never,
	})
	let wrapper: ReturnType<typeof mountToggle>

	it('is a named group of toggle buttons, one of them pressed', () => {
		wrapper = mountToggle('count')
		expect(wrapper.get('[role="group"]').attributes('aria-label')).toBe('Show')
		expect(wrapper.findAll('button').map(b => [b.text(), b.attributes('aria-pressed')])).toEqual([['Tasks', 'true'], ['Estimate', 'false']])
		expect(wrapper.findAll('button').map(b => b.attributes('type'))).toEqual(['button', 'button'])
	})

	it('changes the model when another option is chosen and keeps it when the chosen one is clicked again', async () => {
		wrapper = mountToggle('count')
		await wrapper.findAll('button')[1]!.trigger('click')
		expect(wrapper.emitted('update:modelValue')).toEqual([['estimate']])
		expect(wrapper.findAll('button').map(b => b.attributes('aria-pressed'))).toEqual(['false', 'true'])
		await wrapper.findAll('button')[1]!.trigger('click')
		expect(wrapper.findAll('button')[1]!.attributes('aria-pressed')).toBe('true')
	})
})

describe('WidgetTypePicker', () => {
	it('offers every widget type with its name and description in the language', () => {
		const english = mount(WidgetTypePicker, {global: global()})
		const turkish = mount(WidgetTypePicker, {global: global('tr-TR')})
		expect(english.findAll('button').map(b => b.attributes('data-type'))).toEqual([...OFFERED_WIDGET_TYPES])
		for (const wrapper of [english, turkish]) {
			for (const button of wrapper.findAll('button')) {
				expect(button.get('strong').text()).not.toMatch(/^dashboards\./)
				expect(button.findAll('span').at(-1)!.text()).not.toMatch(/^dashboards\./)
				expect(button.get('strong').text()).not.toBe('')
			}
		}
		expect(english.findAll('strong').map(s => s.text())).not.toEqual(turkish.findAll('strong').map(s => s.text()))
	})

	it('marks the chosen type as pressed and emits the type that is clicked', async () => {
		const wrapper = mount(WidgetTypePicker, {props: {modelValue: OFFERED_WIDGET_TYPES[1]}, global: global()})
		const pressed = wrapper.findAll('button').filter(b => b.attributes('aria-pressed') === 'true')
		expect(pressed.map(b => b.attributes('data-type'))).toEqual([OFFERED_WIDGET_TYPES[1]])
		expect(pressed[0]!.classes()).toContain('is-selected')
		await wrapper.findAll('button')[0]!.trigger('click')
		expect(wrapper.emitted('update:modelValue')).toEqual([[OFFERED_WIDGET_TYPES[0]]])
		expect(mount(WidgetTypePicker, {global: global()}).findAll('[aria-pressed="true"]')).toHaveLength(0)
	})
})
