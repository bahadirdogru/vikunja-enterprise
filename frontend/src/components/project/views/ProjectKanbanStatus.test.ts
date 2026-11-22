import {shallowMount, flushPromises} from '@vue/test-utils'
import {describe, expect, it, vi, beforeEach} from 'vitest'
import {createPinia} from 'pinia'
import draggable from 'zhyswan-vuedraggable'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	projectViewBucketsTasksList: vi.fn(),
	projectViewTasksList: vi.fn(),
	taskBucketUpdate: vi.fn(),
	tasksPositionUpdate: vi.fn(),
	taskStatusChange: vi.fn(),
	workflowRead: vi.fn(),
	tasksCreate: vi.fn(),
	bucketsUpdate: vi.fn(),
}))

const messages = vi.hoisted(() => ({error: vi.fn(), success: vi.fn(), getErrorText: vi.fn(() => 'generic error')}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => messages)

const project = vi.hoisted(() => ({
	id: 1,
	title: 'Test',
	max_permission: 2,
	views: [{id: 10, view_kind: 'kanban', bucket_configuration_mode: 'status'}],
}))
vi.mock('@/composables/useCurrentProject', () => ({
	useCurrentProject: () => ({currentProject: {value: project}, isPending: {value: false}}),
}))
vi.mock('@/composables/useTaskDragToProject', () => ({
	useTaskDragToProject: () => ({handleTaskDropToProject: async () => ({moved: false, targetProjectId: null})}),
}))
vi.mock('@/stores/auth', () => ({
	useAuthStore: () => ({
		settings: {
			frontend_settings: {
				always_show_bucket_task_count: false,
				quick_add_magic_mode: 'vikunja',
				quick_add_default_reminders: [],
			},
		},
	}),
}))
vi.mock('@/client/queries/savedFilters', () => ({
	savedFilterQuery: (id: number) => ({queryKey: ['saved-filters', 'detail', id], queryFn: async () => null, enabled: false}),
}))
vi.mock('@vueuse/router', () => ({useRouteQuery: () => ({value: undefined})}))
vi.mock('vue-router', () => ({
	useRouter: () => ({push: vi.fn(), currentRoute: {value: {fullPath: '/', params: {}}}}),
}))
vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

import ProjectKanban from './ProjectKanban.vue'
import TaskStatusDialog from '@/components/workflow/TaskStatusDialog.vue'
import {projectKeys} from '@/client/queries/projects'

const statuses = [
	{id: 1, title: 'Todo', color: '#888888', category: 'open', position: 1, is_initial: true, archived: false},
	{id: 2, title: 'Doing', color: '#0088ff', category: 'in_progress', position: 2, is_initial: false, archived: false},
	{id: 3, title: 'Review', color: '#ffaa00', category: 'in_progress', position: 3, is_initial: false, archived: false},
]
const workflow = {id: 1, project_id: 1, restrict_transitions: false, statuses, transitions: [], created: '', updated: ''}

function column(status: typeof statuses[number], tasks: {id: number, title: string, position: number}[]) {
	return {
		id: -status.id,
		title: status.title,
		position: status.position,
		limit: 0,
		count: tasks.length,
		tasks,
		status: {id: status.id, title: status.title, color: status.color, category: status.category},
	}
}

const board = () => [
	column(statuses[0], [{id: 11, title: 'Task 11', position: 100}]),
	column(statuses[1], [{id: 12, title: 'Task 12', position: 200}]),
	column(statuses[2], []),
]

const DRAGGABLE_STUB = {
	props: ['modelValue'],
	template: '<ul><template v-for="(element, index) in modelValue"><slot name="item" :element="element" :index="index" /></template><slot name="footer" /></ul>',
}

let queryClient: QueryClient

async function mountBoard(mode: 'status' | 'manual' = 'status') {
	queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	queryClient.setQueryData(projectKeys.detail(project.id), project)
	const wrapper = shallowMount(ProjectKanban, {
		props: {
			isLoadingProject: false,
			projectId: 1,
			viewId: 10,
			view: {id: 10, view_kind: 'kanban', bucket_configuration_mode: mode},
		},
		global: {
			plugins: [createPinia(), [VueQueryPlugin, {queryClient}]],
			mocks: {$t: (key: string) => key},
			directives: {focus: () => {}, tooltip: () => {}, cy: () => {}},
			stubs: {
				ProjectWrapper: {template: '<div><slot name="default"/></div>'},
				BucketDraggable: DRAGGABLE_STUB,
				TaskDraggable: DRAGGABLE_STUB,
			},
		},
	})
	await flushPromises()
	return wrapper
}

function cachedBoard() {
	return queryClient.getQueriesData<{buckets: {id: number, tasks: {id: number}[], count: number}[]}>({queryKey: ['kanban']})[0][1]!
}

// What sortable does when a card is dropped on another column: both lists change, then the drag ends.
async function dropOnto(wrapper: Awaited<ReturnType<typeof mountBoard>>, taskId: number, from: number, to: number) {
	const lists = wrapper.findAllComponents(draggable).slice(1)
	const item = document.createElement('li')
	item.dataset.taskId = String(taskId)
	const source = document.createElement('ul')
	source.dataset.bucketIndex = String(from)
	const target = document.createElement('ul')
	target.dataset.bucketIndex = String(to)

	lists[from].vm.$emit('start', {item, from: source, to: source})
	const buckets = cachedBoard().buckets
	const moved = buckets[from].tasks.find(task => task.id === taskId)!
	lists[from].vm.$emit('update:modelValue', buckets[from].tasks.filter(task => task.id !== taskId))
	lists[to].vm.$emit('update:modelValue', [moved, ...buckets[to].tasks])
	await flushPromises()
	lists[to].vm.$emit('end', {item, from: source, to: target, originalEvent: new MouseEvent('mouseup'), newIndex: 0})
	await flushPromises()
}

const rejection = (code: number, details: Record<string, unknown> = {}) => Object.assign(new Error('rejected'), {code, details})

describe('ProjectKanban in status mode', () => {
	beforeEach(() => {
		for (const fn of Object.values({...sdk, ...messages})) fn.mockClear()
		sdk.projectViewBucketsTasksList.mockImplementation(async () => ({data: {items: board()}}))
		sdk.projectViewTasksList.mockResolvedValue({data: {items: []}})
		sdk.workflowRead.mockResolvedValue({data: workflow})
		sdk.tasksPositionUpdate.mockResolvedValue({data: {position: 150}})
		sdk.taskBucketUpdate.mockResolvedValue({data: {bucket_id: -2, task_id: 11, task: {id: 11, title: 'Task 11'}}})
		sdk.taskStatusChange.mockResolvedValue({data: {status: statuses[2], done: false}})
	})

	it('shows a status badge per column and no bucket management', async () => {
		const wrapper = await mountBoard()

		expect(wrapper.findAll('.bucket')).toHaveLength(3)
		expect(wrapper.findComponent({name: 'Message'}).exists()).toBe(false)
		expect(wrapper.findAll('h2.status-title')).toHaveLength(3)
		expect(wrapper.findAllComponents({name: 'WorkflowStatusBadge'})).toHaveLength(3)
		expect(wrapper.find('.new-bucket').exists()).toBe(false)
		expect(wrapper.findAllComponents({name: 'Dropdown'})).toHaveLength(0)
		expect(wrapper.find('[contenteditable]').exists()).toBe(false)
		// cards can still be moved and created
		expect(wrapper.findAll('.bucket-footer').length).toBeGreaterThan(0)
	})

	it('keeps the editable titles and the add column of a manual board', async () => {
		sdk.projectViewBucketsTasksList.mockResolvedValue({data: {items: [{id: 1, title: 'First', position: 1, limit: 0, count: 0, tasks: []}]}})
		const wrapper = await mountBoard('manual')

		expect(wrapper.find('.new-bucket').exists()).toBe(true)
		expect(wrapper.findAll('h2.status-title')).toHaveLength(0)
	})

	it('moves the card into the dropped column and then saves its position', async () => {
		const wrapper = await mountBoard()

		await dropOnto(wrapper, 11, 0, 1)

		expect(sdk.taskBucketUpdate).toHaveBeenCalledWith(expect.objectContaining({
			path: {project: 1, view: 10, bucket: -2},
			body: {task_id: 11},
		}))
		expect(sdk.tasksPositionUpdate).toHaveBeenCalledTimes(1)
		expect(messages.error).not.toHaveBeenCalled()
		expect(wrapper.findComponent(TaskStatusDialog).exists()).toBe(false)
	})

	it('opens the status dialog when the move needs more input and puts the card back', async () => {
		sdk.taskBucketUpdate.mockRejectedValue(rejection(21007, {
			task_id: 11, from_status_id: 1, to_status_id: 3, missing_fields: ['due_date'], comment_required: true,
		}))
		const wrapper = await mountBoard()

		await dropOnto(wrapper, 11, 0, 2)

		const dialog = wrapper.findComponent(TaskStatusDialog)
		expect(dialog.exists()).toBe(true)
		expect(dialog.props()).toMatchObject({
			taskId: 11,
			projectId: 1,
			missingFields: ['due_date'],
			commentRequired: true,
			toStatus: {id: 3, title: 'Review'},
		})
		expect(sdk.tasksPositionUpdate).not.toHaveBeenCalled()
		expect(messages.error).not.toHaveBeenCalled()
		expect(cachedBoard().buckets[0].tasks.map(task => task.id)).toEqual([11])
		expect(cachedBoard().buckets[2].tasks).toEqual([])
	})

	it('completes the move from the dialog with the comment and reloads the board', async () => {
		sdk.taskBucketUpdate.mockRejectedValue(rejection(21007, {
			task_id: 11, to_status_id: 3, missing_fields: [], comment_required: true,
		}))
		const wrapper = await mountBoard()
		await dropOnto(wrapper, 11, 0, 2)
		sdk.projectViewBucketsTasksList.mockClear()

		wrapper.findComponent(TaskStatusDialog).vm.$emit('submit', 'looks good')
		await flushPromises()

		expect(sdk.taskStatusChange).toHaveBeenCalledWith(expect.objectContaining({
			path: {task: 11},
			body: {to_status_id: 3, comment: 'looks good'},
		}))
		expect(wrapper.findComponent(TaskStatusDialog).exists()).toBe(false)
		expect(sdk.projectViewBucketsTasksList).toHaveBeenCalled()
	})

	it('keeps the dialog open with a hint when fields are still missing', async () => {
		sdk.taskBucketUpdate.mockRejectedValue(rejection(21007, {task_id: 11, to_status_id: 3, missing_fields: ['due_date']}))
		const wrapper = await mountBoard()
		await dropOnto(wrapper, 11, 0, 2)
		sdk.taskStatusChange.mockRejectedValue(rejection(21007, {task_id: 11, to_status_id: 3, missing_fields: ['description']}))

		wrapper.findComponent(TaskStatusDialog).vm.$emit('submit', '')
		await flushPromises()

		expect(wrapper.findComponent(TaskStatusDialog).props()).toMatchObject({
			missingFields: ['description'],
			problem: 'workflow.dialog.stillMissing',
		})
	})

	it.each([
		['not allowed (21005)', rejection(21005), 'workflow.errors.notAllowed'],
		['forbidden by permission (21006)', rejection(21006, {reason: 'permission'}), 'workflow.errors.forbiddenPermission'],
		['forbidden by team (21006)', rejection(21006, {reason: 'team'}), 'workflow.errors.forbiddenTeam'],
		['a status that is gone (21003)', rejection(21003), 'workflow.errors.statusGone'],
	])('shows a message and snaps the card back when the move is %s', async (_name, cause, messageKey) => {
		sdk.taskBucketUpdate.mockRejectedValue(cause)
		const wrapper = await mountBoard()

		await dropOnto(wrapper, 11, 0, 1)

		expect(messages.error).toHaveBeenCalledTimes(1)
		expect(messages.error).toHaveBeenCalledWith({message: messageKey})
		expect(wrapper.findComponent(TaskStatusDialog).exists()).toBe(false)
		expect(sdk.tasksPositionUpdate).not.toHaveBeenCalled()
		expect(cachedBoard().buckets[0].tasks.map(task => task.id)).toEqual([11])
		expect(cachedBoard().buckets[1].tasks.map(task => task.id)).toEqual([12])
	})

	it('reports an unrelated failure with its own text', async () => {
		sdk.taskBucketUpdate.mockRejectedValue(rejection(500))
		const wrapper = await mountBoard()

		await dropOnto(wrapper, 11, 0, 1)

		expect(messages.error).toHaveBeenCalledWith({message: 'generic error'})
	})

	it('opens the dialog for a task created in a column whose status needs input', async () => {
		sdk.tasksCreate.mockRejectedValue(rejection(21007, {
			task_id: 99, to_status_id: 3, missing_fields: ['due_date'], comment_required: false,
		}))
		const wrapper = await mountBoard()

		;(wrapper.vm as unknown as {toggleShowNewTaskInput: (id: number) => void}).toggleShowNewTaskInput(-3)
		await flushPromises()
		await wrapper.find('.bucket-footer input').setValue('New card')
		await wrapper.find('.bucket-footer input').trigger('keyup.enter')
		await flushPromises()

		expect(sdk.tasksCreate).toHaveBeenCalledWith(expect.objectContaining({
			body: expect.objectContaining({bucket_id: -3}),
		}))
		expect(wrapper.findComponent(TaskStatusDialog).props()).toMatchObject({taskId: 99, missingFields: ['due_date']})
		expect(messages.error).not.toHaveBeenCalled()
	})

	it('explains an empty board whose workflow is gone', async () => {
		sdk.workflowRead.mockRejectedValue(rejection(21001))
		sdk.projectViewBucketsTasksList.mockResolvedValue({data: {items: []}})
		const wrapper = await mountBoard()

		// shallowMount stubs the message, so its text is not rendered
		expect(wrapper.findComponent({name: 'Message'}).exists()).toBe(true)
		expect(wrapper.findComponent({name: 'Message'}).props('variant')).toBe('warning')
		expect(wrapper.findAll('.bucket')).toHaveLength(0)
	})
})
