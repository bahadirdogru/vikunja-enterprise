import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'

import WorkflowStatusBadge from './WorkflowStatusBadge.vue'
import ApprovalMarker from '@/components/approvals/ApprovalMarker.vue'
import en from '@/i18n/lang/en.json'
import trTR from '@/i18n/lang/tr-TR.json'

enableAutoUnmount(afterEach)

const global = (locale: 'en' | 'tr-TR' = 'en') => ({
	plugins: [createI18n({legacy: false, locale, fallbackLocale: 'en', messages: {en, 'tr-TR': trTR}})],
	stubs: {Icon: {props: ['icon'], template: '<i class="icon" :data-icon="JSON.stringify(icon)" />'}},
	directives: {tooltip: {}},
})

describe('WorkflowStatusBadge', () => {
	const mountBadge = (status: Record<string, string>) => mount(WorkflowStatusBadge, {props: {status} as never, global: global()})

	it('shows the title as text and the category as class and icon', () => {
		const badge = mountBadge({title: '<b>Review</b>', color: '', category: 'in_progress'})
		expect(badge.get('.workflow-status-badge__title').text()).toBe('<b>Review</b>')
		expect(badge.find('b').exists()).toBe(false)
		expect(badge.classes()).toContain('is-in_progress')
		expect(badge.get('.icon').attributes('data-icon')).toBe('"circle-half-stroke"')
	})

	it.each([
		['open', '["far","circle"]'],
		['in_progress', '"circle-half-stroke"'],
		['done', '"circle-check"'],
		['cancelled', '"circle-xmark"'],
	])('uses the icon of the %s category', (category, icon) => {
		expect(mountBadge({title: 'x', color: '', category}).get('.icon').attributes('data-icon')).toBe(icon)
	})

	it('colours the badge with readable text only for a six-digit hex colour', () => {
		const dark = mountBadge({title: 'x', color: '#101010', category: 'open'})
		expect(dark.classes()).toContain('has-color')
		expect(dark.attributes('style')).toContain('background-color: #101010')
		expect(dark.attributes('style')).toContain('color: #ffffff')
		const light = mountBadge({title: 'x', color: '#FFFFAA', category: 'open'})
		expect(light.attributes('style')).toContain('color: #000000')
		for (const color of ['', 'red', '#fff', '#12345g', 'url(javascript:1)', '#1234567']) {
			const badge = mountBadge({title: 'x', color, category: 'open'})
			expect(badge.classes()).not.toContain('has-color')
			expect(badge.attributes('style') ?? '').toBe('')
		}
	})
})

describe('ApprovalMarker', () => {
	it('says a request waits, in both languages', () => {
		expect(mount(ApprovalMarker, {global: global()}).text()).toBe('Waiting for approval')
		expect(mount(ApprovalMarker, {global: global('tr-TR')}).text()).toBe('Onay bekliyor')
	})

	it('shows a count only for more than one request', () => {
		expect(mount(ApprovalMarker, {global: global()}).find('.approval-marker__count').exists()).toBe(false)
		expect(mount(ApprovalMarker, {props: {count: 1}, global: global()}).find('.approval-marker__count').exists()).toBe(false)
		expect(mount(ApprovalMarker, {props: {count: 0}, global: global()}).find('.approval-marker__count').exists()).toBe(false)
		expect(mount(ApprovalMarker, {props: {count: 3}, global: global()}).get('.approval-marker__count').text()).toBe('3')
	})
})
