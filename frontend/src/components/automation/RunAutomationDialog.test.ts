import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({
	automationRunManually: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn(), getErrorText: () => 'error'}))

import RunAutomationDialog from './RunAutomationDialog.vue'
import {mountAutomation} from './testing'
import {error, success} from '@/message'

enableAutoUnmount(afterEach)

const rules = [{id: 1, title: 'Escalate', label: 'Escalate now'}, {id: 2, title: 'Archive', label: 'Archive'}]

function mountDialog() {
	return mountAutomation(RunAutomationDialog, {props: {taskId: 42, projectId: 3, rules}})
}

const buttons = () => [...document.body.querySelectorAll<HTMLElement>('dialog .run-automation__list button')]

describe('RunAutomationDialog', () => {
	beforeEach(() => {
		sdk.automationRunManually.mockReset()
		vi.mocked(success).mockClear()
		vi.mocked(error).mockClear()
	})

	it('offers one button per manual rule, labelled as the rule says', () => {
		mountDialog()
		expect(buttons().map(b => b.textContent?.trim())).toEqual(['Escalate now', 'Archive'])
	})

	it('runs the chosen rule for the task, confirms and closes', async () => {
		sdk.automationRunManually.mockResolvedValue({data: {event_id: 7, status: 'queued'}})
		const wrapper = mountDialog()
		buttons()[0].click()
		await flushPromises()
		expect(sdk.automationRunManually).toHaveBeenCalledWith({path: {project: 3, rule: 1}, body: {task_id: 42}})
		expect(success).toHaveBeenCalledWith({message: 'The automation “Escalate” was started.'})
		expect(wrapper.emitted('close')).toHaveLength(1)
	})

	it('shows a failure, stays open and lets another rule be tried', async () => {
		sdk.automationRunManually.mockRejectedValueOnce(new Error('rule cannot be run'))
		const wrapper = mountDialog()
		buttons()[0].click()
		await flushPromises()
		expect(error).toHaveBeenCalledTimes(1)
		expect(success).not.toHaveBeenCalled()
		expect(wrapper.emitted('close')).toBeUndefined()
		expect(buttons().every(b => !b.hasAttribute('disabled'))).toBe(true)
	})
})
