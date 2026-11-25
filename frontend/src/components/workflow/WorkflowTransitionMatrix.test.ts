import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'

import WorkflowTransitionMatrix from './WorkflowTransitionMatrix.vue'
import en from '@/i18n/lang/en.json'
import {addTransition, draftFromWorkflow, newTransition, removeStatus, type WorkflowDraft, type WorkflowResponse} from '@/helpers/workflow'

enableAutoUnmount(afterEach)

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

const stored: WorkflowResponse = {
	id: 1, project_id: 3, restrict_transitions: true,
	statuses: [
		{id: 10, title: 'To do', color: '#64748b', category: 'open', position: 1, is_initial: true, archived: false},
		{id: 11, title: 'Review', color: '#2563eb', category: 'in_progress', position: 2, is_initial: false, archived: false},
		{id: 12, title: 'Done', color: '#16a34a', category: 'done', position: 3, is_initial: false, archived: false},
	],
	transitions: [],
}

function mountMatrix(draft: WorkflowDraft = draftFromWorkflow(stored), openKey: string | null = null) {
	return mount(WorkflowTransitionMatrix, {
		props: {modelValue: draft, openKey},
		attachTo: document.body,
		global: {plugins: [i18n], stubs: {Icon: true, WorkflowStatusBadge: {props: ['status'], template: '<span class="badge">{{ status.title }}</span>'}}, directives: {tooltip: {}}},
	})
}

const box = (wrapper: ReturnType<typeof mountMatrix>, from: string, to: string) => wrapper.get<HTMLInputElement>(`input[aria-label="${from} to ${to}"]`)

describe('WorkflowTransitionMatrix', () => {
	it('has a row and a column for every status and no box on the diagonal', () => {
		const wrapper = mountMatrix()
		expect(wrapper.findAll('thead th').map(th => th.text())).toEqual(['From \\ To', 'To do', 'Review', 'Done'])
		expect(wrapper.findAll('tbody th').map(th => th.text())).toEqual(['To do', 'Review', 'Done'])
		expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(6)
		expect(wrapper.find('input[aria-label="To do to To do"]').exists()).toBe(false)
		expect(wrapper.get('caption').text()).toContain('Rows are the status a task leaves')
	})

	it('shows only the statuses that were not removed', () => {
		let draft = draftFromWorkflow(stored)
		draft = removeStatus(draft, 11)
		const wrapper = mountMatrix(draft)
		expect(wrapper.findAll('tbody th').map(th => th.text())).toEqual(['To do', 'Done'])
		expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(2)
	})

	it('ticks the cells of the transitions of the draft, in the right direction', () => {
		const draft = addTransition(draftFromWorkflow(stored), newTransition(10, 11))
		const wrapper = mountMatrix(draft)
		expect(box(wrapper, 'To do', 'Review').element.checked).toBe(true)
		expect(box(wrapper, 'Review', 'To do').element.checked).toBe(false)
	})

	it('adds a transition and opens its details when an empty cell is ticked', async () => {
		const wrapper = mountMatrix()
		await box(wrapper, 'Review', 'Done').setValue(true)
		const updated = wrapper.emitted('update:modelValue')!
		expect(updated).toHaveLength(1)
		const draft = updated[0]![0] as WorkflowDraft
		expect(draft.transitions.map(t => [t.from_status_id, t.to_status_id])).toEqual([[11, 12]])
		expect(wrapper.emitted('open')).toEqual([[draft.transitions[0]!.key]])
	})

	it('opens the details of a ticked cell instead of removing it, and closes them on a second activation', async () => {
		const draft = addTransition(draftFromWorkflow(stored), newTransition(10, 12))
		const key = draft.transitions[0]!.key
		const wrapper = mountMatrix(draft)
		await box(wrapper, 'To do', 'Done').setValue(false)
		expect(wrapper.emitted('update:modelValue')).toBeUndefined()
		expect(wrapper.emitted('open')).toEqual([[key]])
		// the checked state comes from the draft, the browser's toggle is undone
		expect(box(wrapper, 'To do', 'Done').element.checked).toBe(true)

		const opened = mountMatrix(draft, key)
		await box(opened, 'To do', 'Done').setValue(false)
		expect(opened.emitted('open')).toEqual([[null]])
		expect(opened.find('td.is-open').exists()).toBe(true)
	})

	it('marks only the transition that needs an approval, in its own cell', () => {
		const withApproval = addTransition(draftFromWorkflow(stored), {...newTransition(11, 12), requires_approval: true})
		const draft = addTransition(withApproval, newTransition(10, 11))
		const wrapper = mountMatrix(draft)
		const marks = wrapper.findAll('icon-stub[icon="user-check"]')
		expect(marks).toHaveLength(1)
		expect(marks[0]!.element.closest('td')!.querySelector('input')!.getAttribute('aria-label')).toBe('Review to Done')
		expect(marks[0]!.attributes('aria-label')).toBe('Needs approval')
		expect(mountMatrix(addTransition(draftFromWorkflow(stored), newTransition(10, 11))).find('icon-stub[icon="user-check"]').exists()).toBe(false)
	})
})
