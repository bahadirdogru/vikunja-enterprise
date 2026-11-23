import {flushPromises, shallowMount} from '@vue/test-utils'
import {beforeEach, describe, expect, it, vi} from 'vitest'

const state = vi.hoisted(() => ({
	mutateAsync: vi.fn(),
	ensureQueryData: vi.fn(),
	ensureTask: vi.fn(),
}))

vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))
vi.mock('@/components/workflow/TaskStatusDialog.vue', async () => {
	const {defineComponent, h} = await import('vue')
	return {
		default: defineComponent({
			name: 'TaskStatusDialog',
			props: ['taskId', 'projectId', 'toStatus', 'missingFields', 'commentRequired', 'busy', 'problem'],
			emits: ['close', 'submit'],
			render: () => h('div'),
		}),
	}
})
vi.mock('@/message', () => ({error: vi.fn(), getErrorText: () => 'failed', success: vi.fn()}))
vi.mock('@/client/queryClient', () => ({queryClient: {ensureQueryData: state.ensureQueryData}}))
vi.mock('@/client/queries/tasks', () => ({ensureTask: state.ensureTask}))
vi.mock('@/client/queries/workflow', () => ({
	workflowQuery: (projectId: number) => ({queryKey: ['workflow', projectId]}),
	useChangeTaskStatusMutation: () => ({mutateAsync: state.mutateAsync, isPending: {value: false}}),
}))

import ApprovalRequestHost from './ApprovalRequestHost.vue'
import {approvalRequestTarget} from '@/composables/approvalRequired'

const workflow = {statuses: [{id: 7, title: 'Done', color: '', category: 'done'}]}

describe('ApprovalRequestHost', () => {
	beforeEach(() => {
		state.mutateAsync.mockReset()
		state.ensureQueryData.mockReset().mockResolvedValue(workflow)
		state.ensureTask.mockReset().mockResolvedValue({project_id: 3})
		approvalRequestTarget.value = null
	})

	it('requests the transition the refused update pointed to', async () => {
		state.mutateAsync.mockResolvedValue({approval: {id: 1}})
		const wrapper = shallowMount(ApprovalRequestHost)

		approvalRequestTarget.value = {taskId: 5, projectId: 3, toStatusId: 7}
		await flushPromises()

		expect(state.mutateAsync).toHaveBeenCalledWith({taskId: 5, toStatusId: 7, comment: ''})
		expect(approvalRequestTarget.value).toBeNull()
		wrapper.unmount()
	})

	it('looks up the project of the task when the target does not know it', async () => {
		state.mutateAsync.mockResolvedValue({approval: {id: 1}})
		const wrapper = shallowMount(ApprovalRequestHost)

		approvalRequestTarget.value = {taskId: 5, projectId: 0, toStatusId: 7}
		await flushPromises()

		expect(state.ensureTask).toHaveBeenCalledWith(5)
		expect(state.ensureQueryData).toHaveBeenCalledWith({queryKey: ['workflow', 3]})
		expect(state.mutateAsync).toHaveBeenCalled()
		wrapper.unmount()
	})

	it('opens the transition dialog when the request needs more input', async () => {
		state.mutateAsync.mockRejectedValue({code: 21007, details: {missing_fields: ['due_date'], comment_required: false, to_status_id: 7, task_id: 5}})
		const wrapper = shallowMount(ApprovalRequestHost)

		approvalRequestTarget.value = {taskId: 5, projectId: 3, toStatusId: 7}
		await flushPromises()

		const dialog = wrapper.findComponent({name: 'TaskStatusDialog'})
		expect(dialog.exists()).toBe(true)
		expect(dialog.props()).toMatchObject({taskId: 5, projectId: 3, missingFields: ['due_date']})
		// The target stays while the dialog is open.
		expect(approvalRequestTarget.value).not.toBeNull()

		dialog.vm.$emit('close')
		await flushPromises()
		expect(approvalRequestTarget.value).toBeNull()
		wrapper.unmount()
	})
})
