import {describe, expect, it, vi} from 'vitest'
import {mount} from '@vue/test-utils'

vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string, params?: Record<string, unknown>) => params ? `${key} ${JSON.stringify(params)}` : key}),
}))

import CustomFieldValueEditor from './CustomFieldValueEditor.vue'
import type {CustomFieldResponse} from '@/client/queries/customFields'

function field(overrides: Partial<CustomFieldResponse> = {}): CustomFieldResponse {
	return {
		id: 1,
		project_id: 1,
		title: 'Budget',
		description: '',
		type: 'number',
		required: false,
		position: 1,
		options: [],
		...overrides,
	}
}

function mountEditor(f: CustomFieldResponse, modelValue: unknown = null, disabled = false) {
	return mount(CustomFieldValueEditor, {
		props: {
			field: f,
			modelValue,
			projectId: 1,
			labelledby: 'label-1',
			disabled,
		},
		global: {
			mocks: {$t: (key: string) => key},
			stubs: {
				Icon: true,
				RouterLink: true,
				Datepicker: true,
				UserValueInput: true,
			},
		},
	})
}

describe('CustomFieldValueEditor', () => {
	it('emits the parsed number when the input loses focus', async () => {
		const wrapper = mountEditor(field({config: {min: 0, max: 100, precision: 1}}))
		const input = wrapper.find('input[type="number"]')
		await input.setValue('12.34')
		await input.trigger('blur')

		expect(wrapper.emitted('update:modelValue')).toEqual([[12.3]])
	})

	it('does not emit and shows an error for a value above the maximum', async () => {
		const wrapper = mountEditor(field({config: {max: 10}}))
		const input = wrapper.find('input[type="number"]')
		await input.setValue('11')
		await input.trigger('blur')

		expect(wrapper.emitted('update:modelValue')).toBeUndefined()
		expect(wrapper.find('[role="alert"]').text()).toContain('customFields.errors.value.max')
		expect(input.attributes('aria-invalid')).toBe('true')
	})

	it('does not emit when the value did not change', async () => {
		const wrapper = mountEditor(field(), 5)
		await wrapper.find('input').trigger('blur')

		expect(wrapper.emitted('update:modelValue')).toBeUndefined()
	})

	it('clears the value when a text input is emptied', async () => {
		const wrapper = mountEditor(field({type: 'text'}), 'hello')
		const input = wrapper.find('input')
		await input.setValue('')
		await input.trigger('blur')

		expect(wrapper.emitted('update:modelValue')).toEqual([[null]])
	})

	it('rejects an invalid email and links a valid one', async () => {
		const wrapper = mountEditor(field({type: 'email'}))
		const input = wrapper.find('input')
		await input.setValue('nope')
		await input.trigger('blur')
		expect(wrapper.emitted('update:modelValue')).toBeUndefined()
		expect(wrapper.text()).toContain('customFields.errors.value.email')

		await input.setValue('me@example.com')
		expect(wrapper.find('a').attributes('href')).toBe('mailto:me@example.com')
		await input.trigger('blur')
		expect(wrapper.emitted('update:modelValue')).toEqual([['me@example.com']])
	})

	it('emits the option id of a select and null when cleared', async () => {
		const wrapper = mountEditor(field({
			type: 'select',
			options: [
				{id: 10, title: 'Open', color: '#ff0000', position: 1},
				{id: 11, title: 'Done', color: '', position: 2},
			],
		}), 10)
		const select = wrapper.find('select')
		await select.setValue('11')
		await select.setValue('')

		expect(wrapper.emitted('update:modelValue')).toEqual([[11], [null]])
	})

	it('toggles multi-select options', async () => {
		const wrapper = mountEditor(field({
			type: 'multi_select',
			options: [
				{id: 10, title: 'A', color: '', position: 1},
				{id: 11, title: 'B', color: '', position: 2},
			],
		}), [10])
		const chips = wrapper.findAll('button')
		await chips[1]!.trigger('click')
		await chips[0]!.trigger('click')

		expect(wrapper.emitted('update:modelValue')).toEqual([[[10, 11]], [null]])
	})

	it('sets and clears a rating by clicking stars', async () => {
		const wrapper = mountEditor(field({type: 'rating', config: {max_rating: 3}}), 2)
		const stars = wrapper.findAll('[role="radio"]')
		expect(stars).toHaveLength(3)

		await stars[2]!.trigger('click')
		await stars[1]!.trigger('click')

		expect(wrapper.emitted('update:modelValue')).toEqual([[3], [null]])
	})

	it('ignores input when read-only', async () => {
		const wrapper = mountEditor(field({type: 'rating'}), 1, true)
		await wrapper.findAll('[role="radio"]')[3]!.trigger('click')

		expect(wrapper.emitted('update:modelValue')).toBeUndefined()
	})

	describe('text values', () => {
		it('refuses a long text above 65535 bytes and says so on the textarea', async () => {
			const wrapper = mountEditor(field({type: 'long_text'}))
			const area = wrapper.get('textarea')
			// 21846 euro signs are 65538 bytes but only 21846 characters
			await area.setValue('\u20ac'.repeat(21846))
			await area.trigger('blur')

			expect(wrapper.emitted('update:modelValue')).toBeUndefined()
			expect(wrapper.get('[role="alert"]').text()).toContain('customFields.errors.value.tooLong {"max":65535}')
			expect(area.attributes('aria-invalid')).toBe('true')
		})

		it('accepts a long text of exactly 65535 bytes and trims it', async () => {
			const wrapper = mountEditor(field({type: 'long_text'}))
			const area = wrapper.get('textarea')
			await area.setValue('\u20ac'.repeat(21845))
			await area.trigger('blur')
			expect(wrapper.emitted('update:modelValue')).toEqual([['\u20ac'.repeat(21845)]])
		})

		it('commits a one-line text with Enter but keeps typing in a long text', async () => {
			const one = mountEditor(field({type: 'text'}))
			await one.get('input').setValue('  Ada  ')
			await one.get('input').trigger('keydown', {key: 'Enter'})
			expect(one.emitted('update:modelValue')).toEqual([['Ada']])

			const long = mountEditor(field({type: 'long_text'}))
			await long.get('textarea').setValue('line one')
			await long.get('textarea').trigger('keydown', {key: 'Enter'})
			expect(long.emitted('update:modelValue')).toBeUndefined()
		})

		it('rejects a url that is not http(s) and opens a valid one in a new tab without a referrer', async () => {
			const wrapper = mountEditor(field({type: 'url'}))
			const input = wrapper.get('input')
			await input.setValue('javascript:alert(1)')
			await input.trigger('blur')
			expect(wrapper.emitted('update:modelValue')).toBeUndefined()
			expect(wrapper.text()).toContain('customFields.errors.value.url')
			expect(wrapper.find('a').exists()).toBe(false)

			await input.setValue('https://example.com/a?b=1')
			const link = wrapper.get('a')
			expect(link.attributes('href')).toBe('https://example.com/a?b=1')
			expect(link.attributes('target')).toBe('_blank')
			expect(link.attributes('rel')).toBe('noopener noreferrer')
			await input.trigger('blur')
			expect(wrapper.emitted('update:modelValue')).toEqual([['https://example.com/a?b=1']])
		})

		it('does not commit when read-only', async () => {
			const wrapper = mountEditor(field({type: 'text'}), 'x', true)
			await wrapper.get('input').trigger('blur')
			expect(wrapper.emitted('update:modelValue')).toBeUndefined()
			expect(wrapper.get('input').attributes('readonly')).toBeDefined()
		})
	})

	describe('number-like values', () => {
		it('shows the currency code next to a currency input', () => {
			const wrapper = mountEditor(field({type: 'currency', config: {currency: 'TRY'}}), 12.5)
			expect(wrapper.get('.number-value__unit').text()).toBe('TRY')
			expect((wrapper.get('input').element as HTMLInputElement).value).toBe('12.5')
		})

		it('shows a percent sign and a bar for progress, and refuses more than 100', async () => {
			const wrapper = mountEditor(field({type: 'progress'}), 40)
			expect(wrapper.get('.number-value__unit').text()).toBe('%')
			expect(wrapper.get('input').attributes('max')).toBe('100')
			expect(wrapper.find('progress, .progress-bar, [role="progressbar"]').exists()).toBe(true)

			await wrapper.get('input').setValue('101')
			await wrapper.get('input').trigger('blur')
			expect(wrapper.emitted('update:modelValue')).toBeUndefined()
			expect(wrapper.get('[role="alert"]').text()).toContain('customFields.errors.value.progress')
		})

		it('clears a number when the box is emptied', async () => {
			const wrapper = mountEditor(field(), 5)
			await wrapper.get('input').setValue('')
			await wrapper.get('input').trigger('blur')
			expect(wrapper.emitted('update:modelValue')).toEqual([[null]])
		})

		it('rounds to the precision of the field before it validates against the bounds', async () => {
			const wrapper = mountEditor(field({config: {min: 0, max: 10, precision: 0}}))
			await wrapper.get('input').setValue('10.4')
			await wrapper.get('input').trigger('blur')
			expect(wrapper.emitted('update:modelValue')).toEqual([[10]])
		})
	})

	it('toggles a checkbox with true and false', async () => {
		const wrapper = mountEditor(field({type: 'checkbox'}), false)
		const box = wrapper.get('input[type="checkbox"]')
		await box.setValue(true)
		expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
	})
})
