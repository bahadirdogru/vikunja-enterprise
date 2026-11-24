import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	customFieldsList: vi.fn(),
	teamsList: vi.fn(),
	workflowReplace: vi.fn(),
	workflowDelete: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	getErrorText: (cause: {detail?: string}) => cause.detail ?? 'error',
	translatedError: (key: string) => new Error(key),
}))
vi.mock('vue-router', async importOriginal => ({
	...await importOriginal<typeof import('vue-router')>(),
	onBeforeRouteLeave: vi.fn(),
}))

import WorkflowEditor from './WorkflowEditor.vue'
import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'
import type {WorkflowResponse} from '@/helpers/workflow'

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

const stored: WorkflowResponse = {
	id: 1,
	project_id: 3,
	restrict_transitions: false,
	statuses: [
		{id: 10, title: 'To do', color: '#64748b', category: 'open', position: 1, is_initial: true, archived: false},
		{id: 11, title: 'Review', color: '#2563eb', category: 'in_progress', position: 2, is_initial: false, archived: false},
		{id: 12, title: 'Done', color: '#16a34a', category: 'done', position: 3, is_initial: false, archived: false},
	],
	transitions: [],
}

function mountEditor(workflow: WorkflowResponse | null) {
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(WorkflowEditor, {
		props: {projectId: 3, workflow},
		attachTo: document.body,
		global: {
			plugins: [i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {Icon: true, RouterLink: true},
			components: {XButton},
			directives: {cy: {}, tooltip: {}, focus: {}},
		},
	})
}

function titleInputs(wrapper: ReturnType<typeof mountEditor>) {
	return wrapper.findAll<HTMLInputElement>('input[aria-label^="Title of status"]')
}

function buttonByText(wrapper: ReturnType<typeof mountEditor>, text: string) {
	const button = wrapper.findAll('button').find(candidate => candidate.text() === text)
	if (!button) throw new Error(`No button "${text}"`)
	return button
}

enableAutoUnmount(afterEach)

describe('WorkflowEditor', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.customFieldsList.mockResolvedValue({data: {items: [{id: 4, project_id: 3, title: 'Budget', type: 'number'}], total_pages: 1}})
		sdk.teamsList.mockResolvedValue({data: {items: [{id: 2, name: 'Design'}], total_pages: 1}})
	})

	it('explains workflows and starts from the localized default template', async () => {
		const wrapper = mountEditor(null)
		await flushPromises()

		expect(wrapper.text()).toContain('This project has no workflow yet')
		expect(wrapper.text()).toContain('independent of the Kanban columns')

		await buttonByText(wrapper, 'Enable workflow').trigger('click')

		expect(titleInputs(wrapper).map(input => input.element.value)).toEqual(['To do', 'In progress', 'Done'])
		expect(wrapper.findAll('input[type="radio"]').map(radio => (radio.element as HTMLInputElement).checked))
			.toEqual([true, false, false])
		expect(sdk.workflowReplace).not.toHaveBeenCalled()
	})

	it('saves the default template with temporary status ids', async () => {
		sdk.workflowReplace.mockResolvedValue({data: {...stored, statuses: [], transitions: []}})
		const wrapper = mountEditor(null)
		await buttonByText(wrapper, 'Enable workflow').trigger('click')

		await (wrapper.vm as unknown as {save: () => Promise<void>}).save()

		expect(sdk.workflowReplace).toHaveBeenCalledTimes(1)
		const call = sdk.workflowReplace.mock.calls[0]![0]
		expect(call.path).toEqual({project: 3})
		expect(call.body.statuses.map((status: {id: number}) => status.id)).toEqual([-1, -2, -3])
		expect(call.body.statuses.map((status: {category: string}) => status.category)).toEqual(['open', 'in_progress', 'done'])
		expect(call.body.restrict_transitions).toBe(false)
	})

	it('blocks the save and shows inline messages for an invalid draft', async () => {
		const wrapper = mountEditor(stored)
		await flushPromises()
		const inputs = titleInputs(wrapper)
		await inputs[0]!.setValue('Review')

		await (wrapper.vm as unknown as {save: () => Promise<void>}).save()
		await flushPromises()

		expect(sdk.workflowReplace).not.toHaveBeenCalled()
		expect(wrapper.text()).toContain('Another status already has this title.')
	})

	it('needs a done status', async () => {
		const wrapper = mountEditor(stored)
		await flushPromises()
		await wrapper.findAll('select')[2]!.setValue('open')

		await (wrapper.vm as unknown as {save: () => Promise<void>}).save()
		await flushPromises()

		expect(sdk.workflowReplace).not.toHaveBeenCalled()
		expect(wrapper.text()).toContain('At least one status needs the category Done.')
	})

	it('shows a rejected definition inline', async () => {
		sdk.workflowReplace.mockRejectedValue({status: 400, code: 21002, detail: 'transitions must reference statuses of this workflow'})
		const wrapper = mountEditor(stored)
		await flushPromises()

		await (wrapper.vm as unknown as {save: () => Promise<void>}).save()
		await flushPromises()

		expect(wrapper.find('[role="alert"]').text()).toBe('transitions must reference statuses of this workflow')
	})

	it('asks where the tasks of a removed status go and retries with move_tasks_to', async () => {
		sdk.workflowReplace
			.mockRejectedValueOnce({status: 409, code: 21008, details: {status_ids: [11]}})
			.mockResolvedValueOnce({data: {...stored, statuses: stored.statuses.filter(status => status.id !== 11)}})
		const wrapper = mountEditor(stored)
		await flushPromises()

		await wrapper.findAll('button[aria-label^="Remove the status"]')[1]!.trigger('click')
		await (wrapper.vm as unknown as {save: () => Promise<void>}).save()
		await flushPromises()

		const dialog = document.body.querySelector('dialog')
		expect(dialog?.textContent).toContain('Tasks are still in Review')

		const target = dialog!.querySelector('select') as HTMLSelectElement
		target.value = '12'
		target.dispatchEvent(new Event('change'))
		const confirm = Array.from(dialog!.querySelectorAll('button')).find(button => button.textContent?.trim() === 'Move tasks and save')
		confirm!.click()
		await flushPromises()

		expect(sdk.workflowReplace).toHaveBeenCalledTimes(2)
		const retry = sdk.workflowReplace.mock.calls[1]![0].body
		expect(retry.move_tasks_to).toBe(12)
		expect(retry.statuses.map((status: {id: number}) => status.id)).toEqual([10, 12])
	})

	it('adds a transition from the matrix and opens its details', async () => {
		const wrapper = mountEditor(stored)
		await flushPromises()

		await buttonByText(wrapper, 'Matrix').trigger('click')
		const cell = wrapper.find('input[aria-label="To do to Done"]')
		await cell.setValue(true)

		expect((cell.element as HTMLInputElement).checked).toBe(true)
		expect(wrapper.text()).toContain('Required fields')
		expect(wrapper.text()).toContain('Budget')
	})
})
