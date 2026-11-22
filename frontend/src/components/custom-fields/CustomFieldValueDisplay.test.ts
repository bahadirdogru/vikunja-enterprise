import {describe, expect, it, vi} from 'vitest'
import {mount} from '@vue/test-utils'

vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({
		locale: {value: 'en'},
		t: (key: string, params?: Record<string, unknown>) => params ? `${key} ${JSON.stringify(params)}` : key,
	}),
}))
vi.mock('@/composables/useUserSearch', () => ({
	useProjectUserSearch: () => ({users: {value: [{id: 5, username: 'ada', name: 'Ada Lovelace'}]}}),
}))
vi.mock('@/helpers/time/formatDate', () => ({
	dateIsValid: (d: unknown) => !Number.isNaN(new Date(d as string).getTime()),
	formatDateLong: () => 'long date',
	formatDisplayDate: () => 'display date',
}))

import CustomFieldValueDisplay from './CustomFieldValueDisplay.vue'
import type {CustomFieldResponse} from '@/client/queries/customFields'

function field(type: CustomFieldResponse['type'], overrides: Partial<CustomFieldResponse> = {}): CustomFieldResponse {
	return {id: 1, project_id: 1, title: 'F', description: '', type, required: false, position: 1, options: [], ...overrides}
}

function render(f: CustomFieldResponse, value: unknown) {
	return mount(CustomFieldValueDisplay, {
		props: {field: f, value, projectId: 1},
		global: {
			mocks: {$t: (key: string) => key},
			stubs: {Icon: true, User: {props: ['user'], template: '<span class="user-stub">{{ user.name || user.username }}</span>'}},
		},
	})
}

describe('CustomFieldValueDisplay', () => {
	it('renders nothing for an unset value', () => {
		expect(render(field('text'), null).html()).toBe('<!--v-if-->')
		expect(render(field('multi_select'), []).html()).toBe('<!--v-if-->')
	})

	it('truncates text via a title attribute', () => {
		const wrapper = render(field('text'), 'a very long text')
		const el = wrapper.find('.cf-value__text')
		expect(el.text()).toBe('a very long text')
		expect(el.attributes('title')).toBe('a very long text')
	})

	it('formats numbers and currency', () => {
		expect(render(field('number', {config: {precision: 1}}), 1234.56).text()).toBe('1,234.6')
		const money = render(field('currency', {config: {currency: 'USD', precision: 2}}), 10).text()
		expect(money).toContain('10.00')
	})

	it('renders dates with the app formatter', () => {
		const wrapper = render(field('date'), '2026-01-02T00:00:00Z')
		expect(wrapper.find('time').text()).toBe('display date')
		expect(wrapper.find('time').attributes('title')).toBe('long date')
	})

	it('shows a check icon for true and a dash for false', () => {
		expect(render(field('checkbox'), true).find('icon-stub').exists()).toBe(true)
		const off = render(field('checkbox'), false)
		expect(off.find('icon-stub').exists()).toBe(false)
		expect(off.text()).toContain('customFields.display.no')
	})

	it('renders select and multi_select options as colored chips', () => {
		const options = [
			{id: 1, title: 'Red', color: '#ff0000', position: 1},
			{id: 2, title: 'Plain', color: '', position: 2},
		]
		const single = render(field('select', {options}), 1)
		expect(single.findAll('.cf-value__chip')).toHaveLength(1)
		expect(single.find('.cf-value__chip').attributes('style')).toContain('#ff0000')

		const multi = render(field('multi_select', {options}), [1, 2])
		expect(multi.findAll('.cf-value__chip').map(c => c.text())).toEqual(['Red', 'Plain'])
		expect(multi.findAll('.cf-value__chip')[1].classes()).toContain('cf-value__chip--plain')
	})

	it('shows users by name and falls back to the id', () => {
		const wrapper = render(field('user'), [5, 9])
		expect(wrapper.findAll('.user-stub').map(u => u.text())).toEqual(['Ada Lovelace', '#9'])
	})

	it('links urls only when they are http(s) and emails as mailto', () => {
		const url = render(field('url'), 'https://example.com')
		expect(url.find('a').attributes('href')).toBe('https://example.com')
		expect(url.find('a').attributes('rel')).toContain('noopener')
		expect(render(field('url'), 'javascript:alert(1)').find('a').exists()).toBe(false)
		expect(render(field('email'), 'a@b.co').find('a').attributes('href')).toBe('mailto:a@b.co')
	})

	it('renders rating stars up to the maximum', () => {
		const wrapper = render(field('rating', {config: {max_rating: 4}}), 3)
		expect(wrapper.findAll('icon-stub')).toHaveLength(4)
		expect(wrapper.findAll('.is-filled')).toHaveLength(3)
	})

	it('renders progress as a bar with a percentage', () => {
		const wrapper = render(field('progress'), 40)
		expect(wrapper.find('progress').attributes('value')).toBe('40')
		expect(wrapper.text()).toContain('40%')
	})
})
