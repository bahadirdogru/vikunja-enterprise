import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'

import WorkflowStatusList from './WorkflowStatusList.vue'
import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'
import trTR from '@/i18n/lang/tr-TR.json'
import {draftFromWorkflow, type DraftErrors, type WorkflowDraft, type WorkflowResponse} from '@/helpers/workflow'

enableAutoUnmount(afterEach)

const stored: WorkflowResponse = {
	id: 1, project_id: 3, restrict_transitions: false, transitions: [],
	statuses: [
		{id: 10, title: 'To do', color: '#64748b', category: 'open', position: 1, is_initial: true, archived: false},
		{id: 11, title: 'Review', color: '#2563eb', category: 'in_progress', position: 2, is_initial: false, archived: false},
		{id: 12, title: 'Done', color: '#16a34a', category: 'done', position: 3, is_initial: false, archived: false},
		{id: 13, title: 'Legacy', color: '#999999', category: 'open', position: 4, is_initial: false, archived: true},
	],
}
const noErrors: DraftErrors = {global: [], statuses: {}, transitions: {}, valid: true}

function mountList(draft: WorkflowDraft = draftFromWorkflow(stored), errors: DraftErrors = noErrors, showErrors = false, locale: 'en' | 'tr-TR' = 'en') {
	return mount(WorkflowStatusList, {
		props: {modelValue: draft, errors, showErrors},
		attachTo: document.body,
		global: {
			plugins: [createI18n({legacy: false, locale, fallbackLocale: 'en', messages: {en, 'tr-TR': trTR}})],
			components: {XButton},
			stubs: {Icon: true, WorkflowStatusBadge: {props: ['status'], template: '<span class="badge">{{ status.title }}</span>'}},
			directives: {tooltip: {}},
		},
	})
}

const byLabel = (w: ReturnType<typeof mountList>, tag: string, label: string) => w.findAll(tag).find(e => e.attributes('aria-label') === label)!
const last = (wrapper: ReturnType<typeof mountList>) => wrapper.emitted('update:modelValue')!.at(-1)![0] as WorkflowDraft
const titles = (draft: WorkflowDraft) => draft.statuses.map(s => s.title)

describe('WorkflowStatusList', () => {
	it('has a row for each active status with labelled controls, and the archived ones apart', () => {
		const wrapper = mountList()
		expect(wrapper.findAll('input[aria-label^="Title of status"]').map(i => (i.element as HTMLInputElement).value)).toEqual(['To do', 'Review', 'Done'])
		expect(byLabel(wrapper, 'button', 'Move "Review" up')).toBeDefined()
		expect(byLabel(wrapper, 'button', 'Remove the status "Done"')).toBeDefined()
		expect(byLabel(wrapper, 'select', 'Category of the status "Review"')).toBeDefined()
		expect(wrapper.get('details summary').text()).toBe('1 archived status')
		expect(wrapper.get('details').text()).toContain('Legacy')
	})

	it('renames a status and changes its category without touching the others', async () => {
		const wrapper = mountList()
		await wrapper.get('input[aria-label="Title of status 2"]').setValue('In review')
		expect(titles(last(wrapper))).toEqual(['To do', 'In review', 'Done', 'Legacy'])
		await byLabel(wrapper, 'select', 'Category of the status "Review"').setValue('done')
		expect(last(wrapper).statuses.map(s => s.category)).toEqual(['open', 'done', 'done', 'open'])
	})

	it('makes exactly one status the initial one', async () => {
		const wrapper = mountList()
		const radios = wrapper.findAll('input[type="radio"]')
		expect(radios.map(r => (r.element as HTMLInputElement).checked)).toEqual([true, false, false])
		expect(new Set(radios.map(r => r.attributes('name'))).size).toBe(1)
		await radios[2]!.setValue(true)
		expect(last(wrapper).statuses.filter(s => s.is_initial).map(s => s.id)).toEqual([12])
	})

	it('moves a status up or down, and does nothing at either end', async () => {
		const wrapper = mountList()
		await byLabel(wrapper, 'button', 'Move "Done" up').trigger('click')
		expect(titles(last(wrapper)).slice(0, 3)).toEqual(['To do', 'Done', 'Review'])
		await byLabel(wrapper, 'button', 'Move "Done" down').trigger('click')
		expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
		await byLabel(wrapper, 'button', 'Move "To do" up').trigger('click')
		expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
	})

	it('adds a status with the default title, and removes one but never the last', async () => {
		const wrapper = mountList()
		await wrapper.findAll('button').find(b => b.text() === 'Add status')!.trigger('click')
		const added = last(wrapper).statuses.find(s => s.title === 'New status')!
		expect(added.id).toBeLessThan(0)
		expect(added.is_initial).toBe(false)

		await byLabel(wrapper, 'button', 'Remove the status "To do"').trigger('click')
		const removed = last(wrapper)
		expect(removed.statuses.find(s => s.title === 'To do')).toBeUndefined()
		// the initial flag moves on, so the draft stays saveable
		expect(removed.statuses.filter(s => s.is_initial)).toHaveLength(1)

		const single = draftFromWorkflow({...stored, statuses: [stored.statuses[0]!]})
		const alone = mountList(single)
		await byLabel(alone, 'button', 'Remove the status "To do"').trigger('click')
		expect(alone.emitted('update:modelValue')).toBeUndefined()
	})

	it('shows the translated validation message of a status only after a save attempt', async () => {
		const errors: DraftErrors = {global: [], statuses: {11: ['titleDuplicate']}, transitions: {}, valid: false}
		expect(mountList(draftFromWorkflow(stored), errors, false).text()).not.toContain('Another status already has this title.')
		const shown = mountList(draftFromWorkflow(stored), errors, true)
		expect(shown.text()).toContain('Another status already has this title.')
		const turkish = mountList(draftFromWorkflow(stored), errors, true, 'tr-TR')
		expect(turkish.text()).not.toContain('Another status')
		expect(turkish.text()).toContain('Başka bir durum zaten bu başlığı kullanıyor.')
	})
})
