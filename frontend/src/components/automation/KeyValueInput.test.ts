import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'

import KeyValueInput from './KeyValueInput.vue'
import {mountAutomation} from './testing'

enableAutoUnmount(afterEach)

function mountKv(modelValue: unknown, props: Record<string, unknown> = {}) {
	return mountAutomation(KeyValueInput, {props: {modelValue, id: 'kv', label: 'Headers', ...props}})
}

const key = (w: ReturnType<typeof mountKv>, n: number) => w.get(`input[aria-label="Name of row ${n}"]`)
const value = (w: ReturnType<typeof mountKv>, n: number) => w.get(`input[aria-label="Value of row ${n}"]`)

describe('KeyValueInput', () => {
	it('is a named group with a row per entry', () => {
		const wrapper = mountKv({'X-Token': 'abc', Accept: 'json'})
		expect(wrapper.get('[role="group"]').attributes('aria-label')).toBe('Headers')
		expect((key(wrapper, 1).element as HTMLInputElement).value).toBe('X-Token')
		expect((value(wrapper, 2).element as HTMLInputElement).value).toBe('json')
	})

	it.each([[null], [undefined], ['text'], [['a']], [5]])('starts empty for the value %j', modelValue => {
		expect(mountKv(modelValue).findAll('.kv__row')).toHaveLength(0)
	})

	it('shows a non-string value as text and a null value as empty', () => {
		const wrapper = mountKv({n: 5, nothing: null})
		expect((value(wrapper, 1).element as HTMLInputElement).value).toBe('5')
		expect((value(wrapper, 2).element as HTMLInputElement).value).toBe('')
	})

	it('adds an empty row without emitting, and emits the object once a name is typed', async () => {
		const wrapper = mountKv({})
		await wrapper.findAll('button').find(b => b.text() === 'Add row')!.trigger('click')
		expect(wrapper.emitted('update:modelValue')).toBeUndefined()
		await value(wrapper, 1).setValue('v')
		expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{}])
		await key(wrapper, 1).setValue('  Name  ')
		expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{Name: 'v'}])
	})

	it('drops rows without a name from the result, and the last of two equal names wins', async () => {
		const wrapper = mountKv({})
		const add = wrapper.findAll('button').find(b => b.text() === 'Add row')!
		await add.trigger('click')
		await add.trigger('click')
		await key(wrapper, 1).setValue('A')
		await value(wrapper, 1).setValue('1')
		await key(wrapper, 2).setValue('A')
		await value(wrapper, 2).setValue('2')
		expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{A: '2'}])
	})

	it('removes the row that was asked for and emits the rest', async () => {
		const wrapper = mountKv({a: '1', b: '2', c: '3'})
		await wrapper.get('button[aria-label="Remove row 2"]').trigger('click')
		expect(wrapper.emitted('update:modelValue')).toEqual([[{a: '1', c: '3'}]])
		expect(wrapper.findAll('.kv__row')).toHaveLength(2)
	})

	it('re-seeds from the outside but keeps its rows when the new value is what it emitted itself', async () => {
		const wrapper = mountKv({a: '1'})
		await wrapper.setProps({modelValue: {z: '9'}})
		expect((key(wrapper, 1).element as HTMLInputElement).value).toBe('z')
		await wrapper.get('button[aria-label="Remove row 1"]').trigger('click')
		await wrapper.findAll('button').find(b => b.text() === 'Add row')!.trigger('click')
		await wrapper.setProps({modelValue: {}})
		// an empty row without a name stays, because {} equals what the rows already mean
		expect(wrapper.findAll('.kv__row')).toHaveLength(1)
	})

	describe('inserting a project secret into a value', () => {
		const select = (w: ReturnType<typeof mountKv>) => w.get('select')

		it('is offered only with secrets and at least one row', () => {
			expect(mountKv({a: '1'}).find('select').exists()).toBe(false)
			expect(mountKv({}, {secrets: ['TOKEN']}).find('select').exists()).toBe(false)
			expect(mountKv({a: '1'}, {secrets: []}).find('select').exists()).toBe(false)
			expect(select(mountKv({a: '1'}, {secrets: ['TOKEN', 'KEY']})).findAll('option').map(o => o.text())).toEqual(['Insert a secret…', 'TOKEN', 'KEY'])
		})

		it('appends the placeholder to the value that was edited last, else the last row, and resets the select', async () => {
			const wrapper = mountKv({a: 'Bearer ', b: 'x'}, {secrets: ['TOKEN']})
			await value(wrapper, 1).trigger('focus')
			await select(wrapper).setValue('TOKEN')
			expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{a: 'Bearer {{secrets.TOKEN}}', b: 'x'}])
			expect((select(wrapper).element as HTMLSelectElement).value).toBe('')

			const fresh = mountKv({a: '1', b: '2'}, {secrets: ['TOKEN']})
			await select(fresh).setValue('TOKEN')
			expect(fresh.emitted('update:modelValue')!.at(-1)).toEqual([{a: '1', b: '2{{secrets.TOKEN}}'}])
		})
	})
})
