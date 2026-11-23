import {afterEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'
import {ref} from 'vue'

vi.mock('@/composables/useUserSearch', () => ({
	useProjectUserSearch: () => ({users: ref([]), isFetching: ref(false)}),
}))
vi.mock('@/composables/useLabels', () => ({useLabels: () => ({labels: ref([])})}))
vi.mock('@/composables/useProjects', () => ({useProjects: () => ({projectsArray: []})}))

import ParamField from './ParamField.vue'
import {catalog, makeContext, mountAutomation} from './testing'
import {triggerDef} from '@/helpers/automation'

enableAutoUnmount(afterEach)

const trigger = triggerDef(catalog, 'form.submitted')!
const param = trigger.options[0]!

function mountField(forms: {id: number, title: string}[] | null, value?: number) {
	return mountAutomation(ParamField, {
		props: {param, siblings: trigger.options, values: value === undefined ? {} : {form_id: value}, path: 'trigger.options.form_id'},
		context: makeContext({forms}),
	})
}

describe('ParamField: the form option of form.submitted', () => {
	it('is a select over the forms of the project when they are known', async () => {
		const wrapper = mountField([{id: 4, title: 'Cover request'}, {id: 9, title: 'Bug report'}], 9)
		expect(wrapper.find('input[type=number]').exists()).toBe(false)
		const select = wrapper.get('select')
		expect([...select.element.querySelectorAll('option')].map(o => o.textContent)).toEqual(['Choose…', 'Cover request', 'Bug report'])
		expect((select.element as HTMLSelectElement).value).toBe('9')
		await select.setValue('4')
		expect(wrapper.emitted('update')?.at(-1)).toEqual(['form_id', 4])
		await select.setValue('')
		expect(wrapper.emitted('update')?.at(-1)).toEqual(['form_id', null])
	})

	it('stays a number input when the forms are not known', () => {
		const wrapper = mountField(null)
		expect(wrapper.find('input[type=number]').exists()).toBe(true)
		expect(wrapper.find('select').exists()).toBe(false)
	})
})
