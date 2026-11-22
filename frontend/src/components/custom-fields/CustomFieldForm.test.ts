import {describe, expect, it, vi} from 'vitest'
import {mount} from '@vue/test-utils'

vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

import CustomFieldForm from './CustomFieldForm.vue'
import type {CustomFieldDraft} from '@/helpers/customFields'
import type {CustomFieldResponse} from '@/client/queries/customFields'

function mountForm(props: Record<string, unknown> = {}) {
	return mount(CustomFieldForm, {
		props,
		global: {
			mocks: {$t: (key: string) => key},
			directives: {focus: {}},
			stubs: {Icon: true, RouterLink: true},
		},
	})
}

describe('CustomFieldForm', () => {
	it('blocks submit without a title and shows the error', async () => {
		const wrapper = mountForm()
		await wrapper.find('form').trigger('submit')

		expect(wrapper.emitted('submit')).toBeUndefined()
		expect(wrapper.find('[role="alert"]').text()).toBe('customFields.errors.titleRequired')
	})

	it('emits the draft of a valid field', async () => {
		const wrapper = mountForm()
		await wrapper.find('#custom-field-title').setValue('Budget')
		await wrapper.find('select').setValue('number')
		const [min, max] = wrapper.findAll('input[type="number"]')
		await min!.setValue('0')
		await max!.setValue('100')
		await wrapper.find('form').trigger('submit')

		const draft = (wrapper.emitted('submit')?.[0] ?? [])[0] as CustomFieldDraft
		expect(draft).toMatchObject({title: 'Budget', type: 'number', min: '0', max: '100'})
	})

	it('requires and edits options for a select field', async () => {
		const wrapper = mountForm()
		await wrapper.find('#custom-field-title').setValue('Stage')
		await wrapper.find('select').setValue('select')
		await wrapper.find('form').trigger('submit')
		expect(wrapper.emitted('submit')).toBeUndefined()
		expect(wrapper.text()).toContain('customFields.errors.optionsRequired')

		const addOption = wrapper.findAll('button').find(b => b.text() === 'customFields.form.addOption')
		await addOption!.trigger('click')
		await addOption!.trigger('click')
		const titles = wrapper.findAll('.options__title')
		await titles[0]!.setValue('First')
		await titles[1]!.setValue('Second')

		const moveDown = wrapper.findAll('button').filter(b => b.attributes('aria-label') === 'customFields.form.optionMoveDown')
		await moveDown[0]!.trigger('click')
		await wrapper.find('form').trigger('submit')

		const draft = (wrapper.emitted('submit')?.[0] ?? [])[0] as CustomFieldDraft
		expect(draft.options.map(option => option.title)).toEqual(['Second', 'First'])
	})

	it('locks the type when editing', () => {
		const field: CustomFieldResponse = {
			id: 5,
			project_id: 1,
			title: 'Rating',
			description: '',
			type: 'rating',
			required: true,
			position: 1,
			options: [],
			config: {max_rating: 4},
		}
		const wrapper = mountForm({field})

		expect(wrapper.find('select').attributes('disabled')).toBeDefined()
		expect(wrapper.text()).toContain('customFields.form.typeLocked')
		expect((wrapper.find('#custom-field-title').element as HTMLInputElement).value).toBe('Rating')
	})

	it('shows the API error', () => {
		const wrapper = mountForm({apiError: 'Boom'})
		expect(wrapper.text()).toContain('Boom')
	})
})
