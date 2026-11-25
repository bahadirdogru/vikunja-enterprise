import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'

import RatingInput from './RatingInput.vue'
import OptionChips from './OptionChips.vue'
import en from '@/i18n/lang/en.json'

enableAutoUnmount(afterEach)

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})
const global = {plugins: [i18n], stubs: {Icon: true, ColorBubble: true}}

describe('RatingInput', () => {
	const mountRating = (modelValue: unknown, props: Record<string, unknown> = {}) =>
		mount(RatingInput, {props: {modelValue, max: 5, labelledby: 'lbl', ...props}, global})

	it('is a radio group named by the label of the field, with one named radio per star', () => {
		const wrapper = mountRating(3)
		const group = wrapper.get('[role="radiogroup"]')
		expect(group.attributes('aria-labelledby')).toBe('lbl')
		const stars = wrapper.findAll('[role="radio"]')
		expect(stars.map(s => s.attributes('aria-label'))).toEqual(['1 of 5', '2 of 5', '3 of 5', '4 of 5', '5 of 5'])
		expect(stars.map(s => s.attributes('aria-checked'))).toEqual(['false', 'false', 'true', 'false', 'false'])
		expect(stars.map(s => s.classes().includes('is-filled'))).toEqual([true, true, true, false, false])
	})

	it('renders as many stars as the maximum, however unusual', () => {
		expect(mountRating(null, {max: 1}).findAll('[role="radio"]')).toHaveLength(1)
		expect(mountRating(null, {max: 10}).findAll('[role="radio"]')).toHaveLength(10)
	})

	it.each([[null], ['3'], [undefined], [0]])('shows no star as chosen for the value %j', value => {
		const wrapper = mountRating(value)
		expect(wrapper.findAll('[aria-checked="true"]')).toHaveLength(0)
	})

	it('commits the star, clears when the current one is clicked again, and does nothing when disabled', async () => {
		const wrapper = mountRating(2)
		await wrapper.findAll('[role="radio"]')[3]!.trigger('click')
		await wrapper.findAll('[role="radio"]')[1]!.trigger('click')
		expect(wrapper.emitted('commit')).toEqual([[4], [null]])

		const disabled = mountRating(2, {disabled: true})
		await disabled.findAll('[role="radio"]')[3]!.trigger('click')
		expect(disabled.emitted('commit')).toBeUndefined()
		expect(disabled.findAll('[role="radio"]')[0]!.attributes('aria-disabled')).toBe('true')
	})
})

describe('OptionChips', () => {
	const options = [
		{id: 1, title: 'DE', color: '#22aa55', position: 1},
		{id: 2, title: 'FR', color: '', position: 2},
		{id: 3, title: 'TR', color: '#ffffff', position: 3},
	]
	const mountChips = (modelValue: unknown, props: Record<string, unknown> = {}) =>
		mount(OptionChips, {props: {options, modelValue, labelledby: 'lbl', ...props}, global})

	it('is a named group of toggle buttons that say whether they are pressed', () => {
		const wrapper = mountChips([1, 3])
		expect(wrapper.get('[role="group"]').attributes('aria-labelledby')).toBe('lbl')
		expect(wrapper.findAll('button').map(b => b.attributes('aria-pressed'))).toEqual(['true', 'false', 'true'])
		expect(wrapper.findAll('button').map(b => b.text())).toEqual(['DE', 'FR', 'TR'])
	})

	it('fills only a selected chip with its colour and picks a readable text colour', () => {
		const wrapper = mountChips([1, 2, 3])
		const [green, none, white] = wrapper.findAll('button')
		expect(green!.attributes('style')).toContain('background-color: #22aa55')
		// no colour on the option: the chip keeps its default look
		expect(none!.attributes('style') ?? '').not.toContain('background-color')
		expect(white!.attributes('style')).toContain('background-color: #ffffff')
		expect(white!.attributes('style')).toContain('color: #000000')
		// a dark option colour gets a light text
		expect(mountChips([4], {options: [{id: 4, title: 'Dark', color: '#101010', position: 1}]}).get('button').attributes('style')).toContain('color: #ffffff')
		const unselected = mountChips([])
		expect(unselected.findAll('button').every(b => !(b.attributes('style') ?? '').includes('background-color'))).toBe(true)
	})

	it('adds and removes ids in order, ignoring junk in the stored value', async () => {
		const wrapper = mountChips([1, 'x', null])
		await wrapper.findAll('button')[2]!.trigger('click')
		await wrapper.findAll('button')[0]!.trigger('click')
		expect(wrapper.emitted('commit')).toEqual([[[1, 3]], [[]]])
		expect(mountChips('not a list').findAll('[aria-pressed="true"]')).toHaveLength(0)
	})

	it('does nothing when disabled', async () => {
		const wrapper = mountChips([1], {disabled: true})
		await wrapper.findAll('button')[1]!.trigger('click')
		expect(wrapper.emitted('commit')).toBeUndefined()
	})
})
