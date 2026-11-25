import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'

import ApprovalRow from './ApprovalRow.vue'
import {approvalFixture} from './testing'
import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'
import trTR from '@/i18n/lang/tr-TR.json'
import {normalizeApproval} from '@/helpers/approvals'

enableAutoUnmount(afterEach)

function mountRow(over: Record<string, unknown> = {}, props: Record<string, unknown> = {}, locale: 'en' | 'tr-TR' = 'en') {
	const i18n = createI18n({legacy: false, locale, fallbackLocale: 'en', messages: {en, 'tr-TR': trTR}})
	return mount(ApprovalRow, {
		props: {request: normalizeApproval(approvalFixture(over as never)), userId: 3, actions: true, ...props},
		attachTo: document.body,
		global: {
			plugins: [createPinia(), i18n],
			components: {XButton},
			stubs: {Icon: true, User: {props: ['user'], template: '<span class="user">{{ user.username }}</span>'}, RouterLink: {props: ['to'], template: '<a class="task-link" :data-to="JSON.stringify(to)"><slot /></a>'}},
			directives: {tooltip: {}},
		},
	})
}

const buttons = (wrapper: ReturnType<typeof mountRow>) => wrapper.findAll('button').map(b => b.text())

describe('ApprovalRow', () => {
	it('names the task, its project and what the approval is for', () => {
		const wrapper = mountRow()
		expect(wrapper.get('.task-link').text()).toContain('PUB-1')
		expect(wrapper.get('.task-link').text()).toContain('Cover')
		expect(JSON.parse(wrapper.get('.task-link').attributes('data-to')!)).toEqual({name: 'task.detail', params: {id: 7}})
		expect(wrapper.get('.approval-row__project').text()).toBe('Publishing')
		expect(wrapper.get('.approval-row__what').text()).toBe('Waiting for approval → Print ready')
		expect(wrapper.get('.user').text()).toBe('requester')
	})

	it('shows the progress of the approvers', () => {
		expect(mountRow().text()).toContain('1/2 approved')
	})

	it('offers approve and reject to an approver who has not decided, and emits the decision with the request', async () => {
		const wrapper = mountRow()
		expect(buttons(wrapper)).toEqual(['Approve', 'Reject'])
		await wrapper.findAll('button')[0]!.trigger('click')
		await wrapper.findAll('button')[1]!.trigger('click')
		const decisions = wrapper.emitted('decide')!
		expect(decisions.map(([request, decision]) => [(request as {id: number}).id, decision])).toEqual([[11, 'approved'], [11, 'rejected']])
	})

	it.each([
		['somebody who is not an approver', {userId: 99}, {}],
		['an approver who has decided already', {userId: 2}, {}],
		['a viewer of a list without actions', {actions: false}, {}],
		['a request that is no longer pending', {}, {status: 'approved'}],
	])('offers no buttons to %s', (_name, props, over) => {
		expect(buttons(mountRow(over, props))).toEqual([])
	})

	it('marks an overdue request and shows its due date only while pending', () => {
		const overdue = mountRow({due: '2020-01-01T00:00:00Z'})
		expect(overdue.classes()).toContain('is-overdue')
		expect(overdue.text()).toContain('Due')
		const resolved = mountRow({due: '2020-01-01T00:00:00Z', status: 'approved', resolved: '2026-11-22T12:00:00Z'})
		expect(resolved.classes()).not.toContain('is-overdue')
		expect(resolved.text()).not.toContain('Due')
		expect(resolved.get('.outcome-badge').text()).toBe('Approved')
	})

	it('shows the date of the decision for a resolved request instead of its age', () => {
		const wrapper = mountRow({status: 'rejected', resolved: '2026-11-22T12:00:00Z'})
		const times = wrapper.findAll('time')
		expect(times.map(t => t.attributes('datetime'))).toEqual(['2026-11-22T12:00:00Z'])
		expect(wrapper.get('.outcome-badge').classes()).toContain('is-rejected')
	})

	describe('a request that the system cancelled', () => {
		const cancelled = (reason: string) => ({status: 'cancelled', cancel_reason: reason, resolved: '2026-11-22T12:00:00Z'})

		it('explains a removed status', () => {
			const wrapper = mountRow(cancelled('status_removed'))
			expect(wrapper.get('.approval-row__reason').text()).toBe('A status of the request was removed or archived.')
			expect(wrapper.get('.outcome-badge').text()).toBe('Cancelled')
		})

		it('explains a removed status in Turkish', () => {
			const wrapper = mountRow(cancelled('status_removed'), {}, 'tr-TR')
			expect(wrapper.get('.approval-row__reason').text()).toBe('Talepteki bir durum kaldırıldı veya arşivlendi.')
		})

		it('falls back to the neutral text for a reason this version does not know', () => {
			const wrapper = mountRow(cancelled('invented_by_a_newer_server'))
			expect(wrapper.get('.approval-row__reason').text()).toBe('The request was cancelled.')
		})

		it('shows no reason for a pending request', () => {
			expect(mountRow().find('.approval-row__reason').exists()).toBe(false)
		})
	})
})
