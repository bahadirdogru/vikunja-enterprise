import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({customFieldsList: vi.fn(), teamsList: vi.fn(), workflowReplace: vi.fn(), workflowDelete: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
// The real getErrorText and the real language files: the text of the error is under test.
vi.mock('@/message', async importOriginal => ({...await importOriginal<typeof import('@/message')>(), error: vi.fn(), success: vi.fn()}))
vi.mock('vue-router', async importOriginal => ({...await importOriginal<typeof import('vue-router')>(), onBeforeRouteLeave: vi.fn()}))

import WorkflowEditor from './WorkflowEditor.vue'
import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'
import {ApiError} from '@/client/problem'
import {i18n as appI18n, setLanguage} from '@/i18n'
import type {WorkflowResponse} from '@/helpers/workflow'

enableAutoUnmount(afterEach)

const stored: WorkflowResponse = {
	id: 1, project_id: 3, restrict_transitions: false, transitions: [],
	statuses: [
		{id: 10, title: 'To do', color: '#64748b', category: 'open', position: 1, is_initial: true, archived: false},
		{id: 12, title: 'Done', color: '#16a34a', category: 'done', position: 3, is_initial: false, archived: false},
	],
}

function mountEditor() {
	return mount(WorkflowEditor, {
		props: {projectId: 3, workflow: stored},
		attachTo: document.body,
		global: {
			plugins: [createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}}), [VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]],
			stubs: {Icon: true, RouterLink: true},
			components: {XButton},
			directives: {cy: {}, tooltip: {}, focus: {}},
		},
	})
}

const over = () => new ApiError({status: 422, code: 21011, detail: 'This change would move or change 120 tasks.', i18n_params: {tasks: '120', limit: '100'}})

describe('saving a workflow that would change too many tasks (21011)', () => {
	beforeEach(async () => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.customFieldsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		sdk.teamsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		await setLanguage('en')
	})

	it('shows the translated text with both numbers, inline, and keeps the draft', async () => {
		sdk.workflowReplace.mockRejectedValue(over())
		const wrapper = mountEditor()
		await flushPromises()
		await (wrapper.vm as unknown as {save: () => Promise<void>}).save()
		await flushPromises()
		const alert = wrapper.get('[role="alert"]').text()
		expect(alert).toBe('This change would move or change 120 tasks. At most 100 tasks can be changed by one save of the workflow: move some of them to other statuses first.')
		expect(wrapper.find('dialog').exists()).toBe(false)
	})

	it('shows the Turkish text with both numbers when the language is Turkish', async () => {
		await setLanguage('tr-TR')
		expect(appI18n.global.locale.value).toBe('tr-TR')
		sdk.workflowReplace.mockRejectedValue(over())
		const wrapper = mountEditor()
		await flushPromises()
		await (wrapper.vm as unknown as {save: () => Promise<void>}).save()
		await flushPromises()
		const alert = wrapper.get('[role="alert"]').text()
		expect(alert).toContain('120')
		expect(alert).toContain('100')
		expect(alert).not.toContain('This change would')
	})
})
