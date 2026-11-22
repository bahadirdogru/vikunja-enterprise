import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({automationsUpdate: vi.fn(), automationsDelete: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	getErrorText: (cause: {detail?: string}) => cause.detail ?? 'error',
	translatedError: (key: string) => new Error(key),
}))

vi.mock('@/helpers/time/formatDate', () => ({
	formatDateSince: () => 'moments ago',
	formatDateLong: () => 'a long date',
}))

import RuleList from './RuleList.vue'
import {catalog, mountAutomation} from './testing'
import {availableTemplates} from '@/helpers/automationTemplates'
import type {AutomationRuleResponse} from '@/client/queries/automation'

enableAutoUnmount(afterEach)

const rule = (over: Partial<AutomationRuleResponse> = {}): AutomationRuleResponse => ({
	id: 1, project_id: 3, title: 'Comment on done', description: '', enabled: true, revision: 1, owner_id: 1,
	trigger: {type: 'task.completed', options: {}}, conditions: null,
	actions: [{type: 'task.add_comment', params: {text: 'x'}}],
	disabled_reason: '', consecutive_failures: 0, last_run_at: null, last_run_status: '', created: '', updated: '', ...over,
})

const templates = availableTemplates({catalog, statuses: [], customFields: []}, (key: string) => key)

function mountList(rules: AutomationRuleResponse[]) {
	return mountAutomation(RuleList, {props: {rules, catalog, templates}})
}

describe('RuleList', () => {
	beforeEach(() => Object.values(sdk).forEach(mock => mock.mockReset()))

	it('explains automations and offers templates when empty', async () => {
		const wrapper = mountList([])
		expect(wrapper.text()).toContain('No automations yet')
		expect(wrapper.text()).toContain('three parts')
		await wrapper.findAll('button').find(b => b.text().includes('New rule'))!.trigger('click')
		expect(wrapper.emitted('create')).toBeTruthy()
		const cards = wrapper.findAll('.template-card')
		expect(cards.length).toBe(templates.length)
		await cards[0].trigger('click')
		expect(wrapper.emitted('template')?.[0][0]).toMatchObject({id: templates[0].id})
	})

	it('shows the sentence, the last run, a warning for a rule the system disabled and toggles immediately', async () => {
		sdk.automationsUpdate.mockResolvedValue({data: rule({enabled: false})})
		const wrapper = mountList([
			rule({last_run_at: new Date().toISOString(), last_run_status: 'failed'}),
			rule({id: 2, title: 'Broken', enabled: false, disabled_reason: 'too_many_failures'}),
		])
		expect(wrapper.text()).toContain('A task is completed')
		expect(wrapper.text()).toContain('Add a comment')
		expect(wrapper.text()).toContain('Failed')
		expect(wrapper.text()).toContain('Has not run yet')
		const warning = wrapper.get('[role=img]')
		expect(warning.attributes('aria-label')).toContain('failed too many times')

		await wrapper.get('input[aria-label=\'Enable the rule "Comment on done"\']').setValue(false)
		await flushPromises()
		expect(sdk.automationsUpdate).toHaveBeenCalledTimes(1)
		const call = sdk.automationsUpdate.mock.calls[0][0]
		expect(call.path).toEqual({project: 3, rule: 1})
		expect(call.body).toMatchObject({enabled: false, title: 'Comment on done', trigger: {type: 'task.completed'}})
	})

	it('emits edit, runs and duplicate and confirms before deleting', async () => {
		sdk.automationsDelete.mockResolvedValue({data: {}})
		const wrapper = mountList([rule()])
		await wrapper.get('button[aria-label=\'Edit the rule "Comment on done"\']').trigger('click')
		await wrapper.get('button[aria-label=\'Show the runs of "Comment on done"\']').trigger('click')
		await wrapper.get('button[aria-label=\'Duplicate the rule "Comment on done"\']').trigger('click')
		expect(wrapper.emitted('edit')).toHaveLength(1)
		expect(wrapper.emitted('runs')).toHaveLength(1)
		expect(wrapper.emitted('duplicate')).toHaveLength(1)

		await wrapper.get('button[aria-label=\'Delete the rule "Comment on done"\']').trigger('click')
		expect(sdk.automationsDelete).not.toHaveBeenCalled()
		expect(document.body.textContent).toContain('Delete this rule?')
	})
})
