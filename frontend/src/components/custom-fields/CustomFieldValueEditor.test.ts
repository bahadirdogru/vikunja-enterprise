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
})
