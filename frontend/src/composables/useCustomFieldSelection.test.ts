import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {defineComponent, h, ref} from 'vue'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({customFieldsList: vi.fn(), workflowRead: vi.fn()}))
vi.mock('@/client/generated', () => sdk)

import {useCustomFieldSelection} from './useCustomFieldSelection'
import {useProjectWorkflow} from './useProjectWorkflow'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

const fields = [
	{id: 1, title: 'Print run', type: 'number'}, {id: 2, title: 'Stage', type: 'select'}, {id: 3, title: 'Link', type: 'url'},
]

function setupSelection(scope = 5, projectId = 3, key = 'cf-test') {
	const scopeRef = ref(scope)
	const projectRef = ref(projectId)
	let result!: ReturnType<typeof useCustomFieldSelection>
	const wrapper = mount(defineComponent({setup() {
		result = useCustomFieldSelection(key, scopeRef, projectRef)
		return () => h('div')
	}}), {global: {plugins: [[VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]]}})
	return {sel: result, scopeRef, projectRef, wrapper}
}

describe('useCustomFieldSelection', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.customFieldsList.mockResolvedValue({data: {items: fields, total_pages: 1}})
		localStorage.clear()
	})

	it('selects nothing at first and keeps a choice per view in the local storage', async () => {
		const {sel, scopeRef} = setupSelection()
		await flushPromises()
		expect(sel.selectedFields.value).toEqual([])
		sel.setSelected(2, true)
		sel.setSelected(1, true)
		expect(sel.isSelected(2)).toBe(true)
		// shown in the order of the field definitions, not of the clicks
		expect(sel.selectedFields.value.map(f => f.id)).toEqual([1, 2])
		await flushPromises()
		expect(JSON.parse(localStorage.getItem('cf-test')!)).toEqual({5: [2, 1]})
		scopeRef.value = 6
		expect(sel.selectedIds.value).toEqual([])
		scopeRef.value = 5
		expect(sel.selectedIds.value).toEqual([2, 1])
	})

	it('unselects, and forgets a field that no longer exists', async () => {
		localStorage.setItem('cf-test', JSON.stringify({5: [1, 99]}))
		const {sel} = setupSelection()
		await flushPromises()
		expect(sel.selectedFields.value.map(f => f.id)).toEqual([1])
		sel.setSelected(1, false)
		expect(sel.isSelected(1)).toBe(false)
	})

	it('does not ask for fields of a saved filter or favourites (negative or zero project)', async () => {
		const {sel} = setupSelection(5, -2)
		await flushPromises()
		expect(sdk.customFieldsList).not.toHaveBeenCalled()
		expect(sel.fields.value).toEqual([])
	})

	it('keeps separate storage keys apart', async () => {
		const a = setupSelection(5, 3, 'cf-a').sel
		const b = setupSelection(5, 3, 'cf-b').sel
		a.setSelected(1, true)
		expect(b.selectedIds.value).toEqual([])
	})
})

describe('useProjectWorkflow', () => {
	beforeEach(() => Object.values(sdk).forEach(mock => mock.mockReset()))

	function setupWorkflow(projectId: number) {
		const idRef = ref(projectId)
		let result!: ReturnType<typeof useProjectWorkflow>
		mount(defineComponent({setup() {
			result = useProjectWorkflow(idRef)
			return () => h('div')
		}}), {global: {plugins: [[VueQueryPlugin, {queryClient: new QueryClient({defaultOptions: {queries: {retry: false}}})}]]}})
		return {wf: result, idRef}
	}

	it('has the workflow of a project with one', async () => {
		sdk.workflowRead.mockResolvedValue({data: {id: 1, project_id: 3, statuses: [{id: 10, title: 'To do'}]}})
		const {wf} = setupWorkflow(3)
		expect(wf.isLoading.value).toBe(true)
		await flushPromises()
		expect(wf.hasWorkflow.value).toBe(true)
		expect(wf.workflow.value!.statuses[0]!.title).toBe('To do')
		expect(wf.isLoading.value).toBe(false)
	})

	it('has none for a project without (error 21001) and sends nothing for pseudo projects', async () => {
		sdk.workflowRead.mockRejectedValue(new ApiError({status: 404, code: 21001, detail: 'none'}))
		const none = setupWorkflow(3).wf
		await flushPromises()
		expect(none.hasWorkflow.value).toBe(false)
		expect(none.workflow.value).toBeNull()

		sdk.workflowRead.mockClear()
		const pseudo = setupWorkflow(-1).wf
		await flushPromises()
		expect(sdk.workflowRead).not.toHaveBeenCalled()
		expect(pseudo.hasWorkflow.value).toBe(false)
	})
})
