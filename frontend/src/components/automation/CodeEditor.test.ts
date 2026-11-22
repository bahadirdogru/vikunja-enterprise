import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'

import CodeEditor from './CodeEditor.vue'
import {mountAutomation} from './testing'

enableAutoUnmount(afterEach)

const mountEditor = (props: Record<string, unknown> = {}) =>
	mountAutomation(CodeEditor, {props: {id: 'src', label: 'Script', modelValue: 'a\nb\nc', ...props}})

describe('CodeEditor', () => {
	it('shows a line number per line and marks the line of an error', () => {
		const wrapper = mountEditor({errorPosition: {line: 2, column: 5}})
		expect(wrapper.findAll('.code-editor__line').map(l => l.text())).toEqual(['1', '2', '3'])
		expect(wrapper.get('.code-editor__line.is-error').text()).toBe('2')
		expect(wrapper.text()).toContain('Syntax error at line 2, column 5.')
		expect(wrapper.get('textarea').attributes('aria-invalid')).toBe('true')
	})

	it('shows the convention of the kind of script and the limits', () => {
		const action = mountEditor()
		expect(action.text()).toContain('body of a function')
		expect(action.text()).toContain('automation.scripts.timeout')
		expect(mountEditor({mode: 'condition'}).text()).toContain('read-only API')
	})

	it('filters the API reference and inserts a name at the cursor', async () => {
		const wrapper = mountEditor({modelValue: 'x '})
		await wrapper.get('input[type=search]').setValue('labels.add')
		const entries = wrapper.findAll('.code-editor__api-entry')
		expect(entries).toHaveLength(1)
		await entries[0].trigger('click')
		expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['x vikunja.labels.add'])
	})

	it('inserts an example, replacing an empty editor', async () => {
		const wrapper = mountEditor({modelValue: ''})
		await wrapper.get('select').setValue('comment')
		expect((wrapper.emitted('update:modelValue')?.at(-1)?.[0] as string)).toContain('vikunja.tasks.comment')
	})

	it('is read-only without reference or toolbar', () => {
		const wrapper = mountEditor({readonly: true})
		expect(wrapper.get('textarea').attributes('readonly')).toBeDefined()
		expect(wrapper.find('.code-editor__reference').exists()).toBe(false)
		expect(wrapper.find('.code-editor__toolbar').exists()).toBe(false)
	})
})
