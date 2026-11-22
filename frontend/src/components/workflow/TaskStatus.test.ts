import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	workflowRead: vi.fn(),
	taskStatusRead: vi.fn(),
	taskStatusChange: vi.fn(),
	taskStatusHistoryList: vi.fn(),
	customFieldsList: vi.fn(),
	taskCustomFieldValuesList: vi.fn(),
	taskCustomFieldValuesSet: vi.fn(),
	tasksRead: vi.fn(),
	patchTasksRead: vi.fn(),
	teamsList: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	getErrorText: () => 'generic error',
	translatedError: (key: string) => new Error(key),
}))

import TaskStatus from './TaskStatus.vue'
import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'
import {error} from '@/message'
import {workflowKeys} from '@/client/queries/workflow'

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

const statuses = [
	{id: 10, title: 'To do', color: '#64748b', category: 'open', position: 1, is_initial: true, archived: false},
	{id: 11, title: 'In review', color: '#2563eb', category: 'in_progress', position: 2, is_initial: false, archived: false},
	{id: 12, title: 'Done', color: '#16a34a', category: 'done', position: 3, is_initial: false, archived: false},
]

const workflow = {id: 1, project_id: 3, restrict_transitions: false, statuses, transitions: [], created: '', updated: ''}

function taskStatus(transitions: unknown[]) {
	return {task_id: 7, workflow_id: 1, status: statuses[0], entered: null, transitions}
}

const plain = {to_status: statuses[1], title: '', require_comment: false, missing_fields: []}
const demanding = {to_status: statuses[2], title: 'Ship it', require_comment: true, missing_fields: ['description']}

function mountStatus(transitions: unknown[], props: Record<string, unknown> = {}) {
	sdk.workflowRead.mockResolvedValue({data: workflow})
	sdk.taskStatusRead.mockResolvedValue({data: taskStatus(transitions)})
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	const wrapper = mount(TaskStatus, {
		props: {taskId: 7, projectId: 3, done: false, canWrite: true, ...props},
		attachTo: document.body,
		global: {
			plugins: [createPinia(), i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {Icon: true, RouterLink: true},
			components: {XButton},
			directives: {cy: {}, tooltip: {}, focus: {}},
		},
	})
	return {wrapper, queryClient}
}

async function openMenu(wrapper: ReturnType<typeof mountStatus>['wrapper']) {
	await wrapper.find('button[aria-haspopup="menu"]').trigger('click')
	await flushPromises()
}

function menuItems(wrapper: ReturnType<typeof mountStatus>['wrapper']) {
	return wrapper.findAll('[role="menuitem"]')
}

enableAutoUnmount(afterEach)

describe('TaskStatus', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		vi.mocked(error).mockClear()
		sdk.customFieldsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		sdk.taskCustomFieldValuesList.mockResolvedValue({data: {items: [], total_pages: 1}})
		sdk.taskStatusHistoryList.mockResolvedValue({data: {items: [], total_pages: 1}})
		sdk.teamsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		sdk.tasksRead.mockResolvedValue({data: {id: 7, project_id: 3, title: 'Task', description: '', assignees: [], labels: [], attachments: []}})
		sdk.patchTasksRead.mockResolvedValue({data: {id: 7, project_id: 3, title: 'Task', description: '<p>text</p>'}})
		sdk.taskStatusChange.mockResolvedValue({data: {status: statuses[1], done: false}})
	})

	it('shows the current status as a badge and lists the transitions with their requirements', async () => {
		const {wrapper} = mountStatus([plain, demanding])
		await flushPromises()

		expect(wrapper.find('.workflow-status-badge').text()).toBe('To do')

		await openMenu(wrapper)
		const items = menuItems(wrapper)
		expect(items).toHaveLength(2)
		expect(items[0]!.text()).toContain('In review')
		expect(items[0]!.text()).not.toContain('Needs')
		expect(items[1]!.text()).toContain('Ship it')
		expect(items[1]!.text()).toContain('Needs: Description, a comment')
	})

	it('changes the status directly when nothing is required', async () => {
		const {wrapper} = mountStatus([plain])
		await flushPromises()
		await openMenu(wrapper)

		await menuItems(wrapper)[0]!.trigger('click')
		await flushPromises()

		expect(sdk.taskStatusChange).toHaveBeenCalledWith(expect.objectContaining({
			path: {task: 7},
			body: {to_status_id: 11, comment: ''},
		}))
		expect(wrapper.emitted('changed')).toHaveLength(1)
		expect(document.body.querySelector('dialog')).toBeNull()
	})

	it('collects the missing fields and the comment in a dialog before changing the status', async () => {
		const {wrapper} = mountStatus([plain, demanding])
		await flushPromises()
		await openMenu(wrapper)
		await menuItems(wrapper)[1]!.trigger('click')
		await flushPromises()

		const dialog = document.body.querySelector('dialog')!
		expect(dialog.textContent).toContain('Change status to "Done"')
		expect(sdk.taskStatusChange).not.toHaveBeenCalled()

		const confirm = () => Array.from(dialog.querySelectorAll('button')).find(button => button.textContent?.trim() === 'Change status')!

		// The comment is required, so the first attempt stays in the dialog.
		confirm().click()
		await flushPromises()
		expect(dialog.textContent).toContain('A comment is required for this change.')
		expect(sdk.taskStatusChange).not.toHaveBeenCalled()

		const [description, comment] = Array.from(dialog.querySelectorAll('textarea'))
		description!.value = 'Final text'
		description!.dispatchEvent(new Event('input'))
		comment!.value = 'Looks good'
		comment!.dispatchEvent(new Event('input'))
		confirm().click()
		await flushPromises()

		expect(sdk.patchTasksRead).toHaveBeenCalledTimes(1)
		expect(sdk.taskStatusChange).toHaveBeenCalledWith(expect.objectContaining({
			body: {to_status_id: 12, comment: 'Looks good'},
		}))
		expect(document.body.querySelector('dialog')).toBeNull()
	})

	it('reopens the dialog with the server\'s list when requirements are still unmet', async () => {
		sdk.taskStatusChange.mockRejectedValue({
			status: 422,
			code: 21007,
			details: {missing_fields: ['due_date'], comment_required: false, to_status_id: 12},
		})
		const {wrapper} = mountStatus([plain])
		await flushPromises()
		await openMenu(wrapper)
		await menuItems(wrapper)[0]!.trigger('click')
		await flushPromises()

		const dialog = document.body.querySelector('dialog')!
		expect(dialog.textContent).toContain('Change status to "In review"')
		expect(dialog.textContent).toContain('Due date')
		expect(error).not.toHaveBeenCalled()
	})

	it('shows a clear message for a transition the workflow does not allow', async () => {
		sdk.taskStatusChange.mockRejectedValue({status: 400, code: 21005})
		const {wrapper} = mountStatus([plain])
		await flushPromises()
		await openMenu(wrapper)
		await menuItems(wrapper)[0]!.trigger('click')
		await flushPromises()

		expect(error).toHaveBeenCalledWith({message: 'This status change is not allowed by the workflow.'})
		expect(document.body.querySelector('dialog')).toBeNull()
	})

	it('names the missing team for a forbidden change', async () => {
		sdk.taskStatusChange.mockRejectedValue({status: 403, code: 21006, details: {reason: 'team'}})
		const {wrapper} = mountStatus([plain])
		await flushPromises()
		await openMenu(wrapper)
		await menuItems(wrapper)[0]!.trigger('click')
		await flushPromises()

		expect(error).toHaveBeenCalledWith({
			message: 'Only members of the allowed teams can make this status change.',
		})
	})

	it('renders nothing and asks nothing more when the project has no workflow', async () => {
		sdk.workflowRead.mockRejectedValue({status: 404, code: 21001})
		const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
		const wrapper = mount(TaskStatus, {
			props: {taskId: 7, projectId: 3, done: false, canWrite: true},
			global: {
				plugins: [createPinia(), i18n, [VueQueryPlugin, {queryClient}]],
				stubs: {Icon: true},
				directives: {tooltip: {}},
			},
		})
		await flushPromises()

		expect(wrapper.find('.property-status').exists()).toBe(false)
		expect(sdk.taskStatusRead).not.toHaveBeenCalled()
		expect(sdk.workflowRead).toHaveBeenCalledTimes(1)
		expect(queryClient.getQueryData(workflowKeys.project(3))).toBeNull()
		expect(await (wrapper.vm as unknown as {toggleDone: () => Promise<boolean | null>}).toggleDone()).toBeNull()
	})

	it('moves a task to the first done status through the done button', async () => {
		const {wrapper} = mountStatus([plain, {...demanding, require_comment: false, missing_fields: []}])
		await flushPromises()

		const result = await (wrapper.vm as unknown as {toggleDone: () => Promise<boolean | null>}).toggleDone()

		expect(result).toBe(true)
		expect(sdk.taskStatusChange).toHaveBeenCalledWith(expect.objectContaining({body: {to_status_id: 12, comment: ''}}))
	})

	it('opens the dialog instead of a raw error when the done button hits unmet requirements', async () => {
		const {wrapper} = mountStatus([plain, demanding])
		await flushPromises()

		const result = await (wrapper.vm as unknown as {toggleDone: () => Promise<boolean | null>}).toggleDone()

		expect(result).toBe(false)
		expect(document.body.querySelector('dialog')?.textContent).toContain('Change status to "Done"')
		expect(error).not.toHaveBeenCalled()
	})

	it('is read-only without write access', async () => {
		const {wrapper} = mountStatus([plain], {canWrite: false})
		await flushPromises()

		expect(wrapper.find('.workflow-status-badge').exists()).toBe(true)
		expect(wrapper.find('button[aria-haspopup="menu"]').exists()).toBe(false)
	})
})
