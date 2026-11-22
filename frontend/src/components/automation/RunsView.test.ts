import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({
	automationRunsList: vi.fn(),
	automationRunsRead: vi.fn(),
	automationRunsRetry: vi.fn(),
}))
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

import RunsView from './RunsView.vue'
import {catalog, mountAutomation} from './testing'
import type {AutomationRuleResponse} from '@/client/queries/automation'

enableAutoUnmount(afterEach)

const rule: AutomationRuleResponse = {
	id: 9, project_id: 3, title: 'Existing', description: '', enabled: true, revision: 2, owner_id: 1,
	trigger: {type: 'task.created', options: {}}, conditions: null, actions: [],
	disabled_reason: '', consecutive_failures: 0, last_run_at: null, last_run_status: '', created: '', updated: '',
}

const run = (over: Record<string, unknown>) => ({
	id: 1, rule_id: 9, rule_revision: 2, event_id: 1, event_name: 'task.created', task_id: 4, status: 'success',
	skip_reason: '', error: '', depth: 0, started: '2026-11-22T10:00:00Z', finished: '2026-11-22T10:00:01.500Z', ...over,
})

function mountRuns() {
	return mountAutomation(RunsView, {props: {projectId: 3, rule, catalog}})
}

describe('RunsView', () => {
	beforeEach(() => {
		vi.useRealTimers()
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.automationRunsList.mockResolvedValue({data: {page: 1, per_page: 50, total: 3, total_pages: 1, items: [
			run({id: 3, status: 'failed', error: 'transition not allowed'}),
			run({id: 2, status: 'skipped', skip_reason: 'conditions_not_met', finished: '0001-01-01T00:00:00Z'}),
			run({id: 1}),
		]}})
	})

	it('lists the runs with status, trigger, task, duration and a localized skip reason', async () => {
		const wrapper = mountRuns()
		await flushPromises()
		const rows = wrapper.findAll('tbody tr')
		expect(rows).toHaveLength(3)
		expect(rows[0].text()).toContain('Failed')
		expect(rows[0].text()).toContain('A task is created')
		expect(rows[0].text()).toContain('#4')
		expect(rows[0].text()).toContain('1.5 s')
		expect(rows[1].text()).toContain('The conditions were not met')
		expect(sdk.automationRunsList.mock.calls[0][0]).toMatchObject({path: {project: 3, rule: 9}, query: {page: 1}})
	})

	it('filters by status on the loaded page', async () => {
		const wrapper = mountRuns()
		await flushPromises()
		await wrapper.get('select[aria-label="Status"]').setValue('failed')
		expect(wrapper.findAll('tbody tr')).toHaveLength(1)
		await wrapper.get('select[aria-label="Status"]').setValue('')
		expect(wrapper.findAll('tbody tr')).toHaveLength(3)
	})

	it('opens the steps of a run with output and error and retries failed runs', async () => {
		sdk.automationRunsRead.mockResolvedValue({data: run({id: 3, status: 'failed', error: 'step 2 failed', steps: [
			{id: 11, position: 0, action_type: 'task.add_comment', status: 'success', error: '', output: 'comment added\nline two', duration_ms: 12},
			{id: 12, position: 1, action_type: 'task.set_status', status: 'failed', error: 'the transition is not allowed', output: '', duration_ms: 3},
		]})})
		sdk.automationRunsRetry.mockResolvedValue({data: {run: run({id: 3, status: 'running'})}})
		const wrapper = mountRuns()
		await flushPromises()

		const stepsButton = wrapper.findAll('tbody tr')[0].findAll('button').find(b => b.text() === 'Steps')!
		await stepsButton.trigger('click')
		await flushPromises()
		const detail = wrapper.get('.run-detail')
		expect(sdk.automationRunsRead.mock.calls[0][0]).toMatchObject({path: {project: 3, rule: 9, run: 3}})
		expect(detail.text()).toContain('Add a comment')
		expect(detail.text()).toContain('Change the status')
		expect(detail.text()).toContain('the transition is not allowed')
		expect(detail.text()).toContain('step 2 failed')
		const output = detail.get('pre')
		expect(output.text()).toContain('comment added')
		expect(output.attributes('tabindex')).toBe('0')

		// only failed runs offer a retry
		const retryButtons = wrapper.findAll('tbody button').filter(b => b.text().includes('Retry'))
		expect(retryButtons).toHaveLength(1)
		await retryButtons[0].trigger('click')
		await flushPromises()
		expect(sdk.automationRunsRetry.mock.calls[0][0]).toEqual({path: {project: 3, rule: 9, run: 3}})
	})

	it('refreshes while a run is running', async () => {
		vi.useFakeTimers({shouldAdvanceTime: true})
		sdk.automationRunsList.mockResolvedValue({data: {page: 1, per_page: 50, total: 1, total_pages: 1, items: [run({id: 5, status: 'running', finished: '0001-01-01T00:00:00Z'})]}})
		const wrapper = mountRuns()
		await flushPromises()
		expect(sdk.automationRunsList).toHaveBeenCalledTimes(1)
		sdk.automationRunsList.mockResolvedValue({data: {page: 1, per_page: 50, total: 1, total_pages: 1, items: [run({id: 5, status: 'success'})]}})
		await vi.advanceTimersByTimeAsync(2600)
		await flushPromises()
		expect(sdk.automationRunsList.mock.calls.length).toBeGreaterThanOrEqual(2)
		expect(wrapper.findAll('tbody tr')[0].text()).toContain('Success')
		const calls = sdk.automationRunsList.mock.calls.length
		await vi.advanceTimersByTimeAsync(6000)
		expect(sdk.automationRunsList.mock.calls.length).toBe(calls)
		vi.useRealTimers()
	})

	it('shows an empty state', async () => {
		sdk.automationRunsList.mockResolvedValue({data: {page: 1, per_page: 50, total: 0, total_pages: 0, items: []}})
		const wrapper = mountRuns()
		await flushPromises()
		expect(wrapper.text()).toContain('has not run yet')
	})
})
