import {afterEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'
import {ref} from 'vue'

vi.mock('@/composables/useUserSearch', () => ({
	useProjectUserSearch: () => ({users: ref([{id: 7, username: 'ada', name: 'Ada'}]), isFetching: ref(false)}),
}))
vi.mock('@/composables/useLabels', () => ({useLabels: () => ({labels: ref([])})}))
vi.mock('@/composables/useProjects', () => ({useProjects: () => ({projectsArray: []})}))

import ConditionBuilder from './ConditionBuilder.vue'
import {catalog, makeContext, mountAutomation} from './testing'
import type {ConditionNode} from '@/helpers/automation'

enableAutoUnmount(afterEach)

function mountBuilder(modelValue: ConditionNode | null, context = makeContext()) {
	const wrapper = mountAutomation(ConditionBuilder, {
		props: {
			modelValue,
			path: 'conditions',
			'onUpdate:modelValue': (value: ConditionNode | null) => wrapper.setProps({modelValue: value}),
		},
		context,
	})
	return wrapper
}

const button = (wrapper: ReturnType<typeof mountBuilder>, text: string) => {
	const found = wrapper.findAll('button').find(b => b.text().includes(text))
	if (!found) throw new Error(`No button "${text}"`)
	return found
}

describe('ConditionBuilder', () => {
	it('reads "Always" without conditions and adds the first comparison', async () => {
		const wrapper = mountBuilder(null)
		expect(wrapper.text()).toContain('Always')
		await button(wrapper, 'Add condition').trigger('click')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: '', op: ''}]})
		expect(wrapper.text()).not.toContain('Always')
	})

	it('builds a comparison from the catalog: fields grouped, operators limited, value by kind', async () => {
		const wrapper = mountBuilder({all: [{field: '', op: ''}]})
		const field = wrapper.get('select[aria-label="Field"]')
		const groups = field.findAll('optgroup').map(g => g.attributes('label'))
		expect(groups).toEqual(['Task', 'Status', 'Custom fields', 'The change that started the rule'])
		expect(field.findAll('option').map(o => o.text())).toContain('Print run')
		expect(field.findAll('option').map(o => o.text())).not.toContain('Custom field')

		await field.setValue('done')
		const op = wrapper.get('select[aria-label="Operator"]')
		expect(op.findAll('option').map(o => o.text()).filter(Boolean)).toEqual(['Choose', 'is', 'is not'])
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'done', op: '='}]})
		// bool field: a yes/no select
		const value = wrapper.get('[id$="-value"]')
		expect(value.element.tagName).toBe('SELECT')
		await value.setValue('false')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'done', op: '=', value: false}]})

		await field.setValue('priority')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'priority', op: '='}]})
		expect(wrapper.get('[id$="-value"]').attributes('type')).toBe('number')
	})

	it('converts the value between scalar and list operators and drops it for is_empty', async () => {
		const wrapper = mountBuilder({all: [{field: 'priority', op: '=', value: 3}]})
		await wrapper.get('select[aria-label="Operator"]').setValue('in')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'priority', op: 'in', value: [3]}]})
		await wrapper.get('select[aria-label="Operator"]').setValue('=')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'priority', op: '=', value: 3}]})
		await wrapper.get('select[aria-label="Operator"]').setValue('is_empty')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'priority', op: 'is_empty'}]})
		expect(wrapper.find('[id$="-value"]').exists()).toBe(false)
	})

	it('uses the options of a custom field for its values', async () => {
		const wrapper = mountBuilder({all: [{field: 'cf_2', op: '='}]})
		const value = wrapper.get('[id$="-value"]')
		expect(value.findAll('option').map(o => o.text())).toEqual(['Choose…', 'Editor', 'Design'])
		await value.setValue('12')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'cf_2', op: '=', value: 12}]})
	})

	it('nests groups, changes their type and removes them', async () => {
		const wrapper = mountBuilder({all: [{field: 'done', op: '=', value: true}]})
		await button(wrapper, 'Add group').trigger('click')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'done', op: '=', value: true}, {all: []}]})
		const nested = wrapper.findAll('.cond-node.is-group')[1]
		await nested.findAll('button').find(b => b.text().includes('Add condition'))!.trigger('click')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'done', op: '=', value: true}, {all: [{field: '', op: ''}]}]})

		await wrapper.findAll('select[aria-label="Group type"]')[1].setValue('not')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'done', op: '=', value: true}, {not: [{field: '', op: ''}]}]})

		const second = wrapper.findAll('.cond-node.is-group')[1]
		await second.findAll('button').find(b => b.text().includes('Remove group'))!.trigger('click')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'done', op: '=', value: true}]})

		await button(wrapper, 'Remove all conditions').trigger('click')
		expect(wrapper.props('modelValue')).toBeNull()
	})

	it('hides status fields when the project has no workflow', () => {
		const wrapper = mountBuilder({all: [{field: '', op: ''}]}, makeContext({statuses: []}))
		const options = wrapper.get('select[aria-label="Field"]').findAll('option').map(o => o.attributes('value'))
		expect(options).not.toContain('status')
		expect(options).not.toContain('status_category')
		expect(options).not.toContain('event.old.status')
		expect(options).toContain('title')
	})

	it('preserves a script node and renders it as a labelled code box', async () => {
		const tree: ConditionNode = {all: [{script: 'return task.priority > 2'}, {field: 'done', op: '=', value: true}]}
		const wrapper = mountBuilder(tree)
		const area = wrapper.get('textarea')
		expect((area.element as HTMLTextAreaElement).value).toBe('return task.priority > 2')
		expect(wrapper.text()).toContain('Script condition')
		// editing a sibling leaves the script alone
		await wrapper.get('select[aria-label="Operator"]').setValue('!=')
		expect((wrapper.props('modelValue') as ConditionNode).all![0]).toEqual({script: 'return task.priority > 2'})
		await area.setValue('return true')
		expect((wrapper.props('modelValue') as ConditionNode).all![0]).toEqual({script: 'return true'})
	})

	it('offers adding a script only when the catalog advertises it', () => {
		const without = mountBuilder({all: []}, makeContext({catalog: {...catalog, script: null, supportsScript: false}}))
		expect(without.text()).not.toContain('Add script condition')
		const wrapper = mountBuilder({all: []})
		expect(wrapper.text()).toContain('Add script condition')
	})

	it('shows the messages of its paths', () => {
		const errors = new Map([['conditions.all[0].value', ['bad value']], ['conditions', ['group problem']]])
		const wrapper = mountBuilder({all: [{field: 'priority', op: '=', value: 'x'}]}, makeContext({errors}))
		expect(wrapper.text()).toContain('bad value')
		expect(wrapper.text()).toContain('group problem')
		expect(wrapper.get('[id$="-value"]').attributes('aria-invalid')).toBe('true')
	})

	it('lets a rule ask why a status changed: the cause field offers only "status removed"', async () => {
		const wrapper = mountBuilder({all: [{field: 'event.new.cause', op: '='}]})
		const field = wrapper.get('select[aria-label="Field"]')
		expect((field.element as HTMLSelectElement).value).toBe('event.new.cause')
		expect(field.findAll('option').map(o => o.text())).toContain('Status change: cause')
		const value = wrapper.get('[id$="-value"]')
		expect(value.element.tagName).toBe('SELECT')
		expect(value.findAll('option').map(o => o.text()).slice(1)).toEqual(['The status was removed from the workflow'])
		await value.setValue('status_removed')
		expect(wrapper.props('modelValue')).toEqual({all: [{field: 'event.new.cause', op: '=', value: 'status_removed'}]})
	})
})
