import {afterEach, describe, expect, it} from 'vitest'
import {enableAutoUnmount} from '@vue/test-utils'

import CronInput from './CronInput.vue'
import RunStatusBadge from './RunStatusBadge.vue'
import {mountAutomation} from './testing'
import {CRON_PRESETS} from '@/helpers/automationCron'

enableAutoUnmount(afterEach)

function mountCron(modelValue: unknown, props: Record<string, unknown> = {}) {
	return mountAutomation(CronInput, {props: {id: 'cron', modelValue, label: 'Schedule', ...props}})
}

describe('CronInput', () => {
	it('shows the expression and the plain help while it is fine or empty', () => {
		const wrapper = mountCron('0 9 * * 1')
		expect((wrapper.get('input').element as HTMLInputElement).value).toBe('0 9 * * 1')
		expect(wrapper.get('input').attributes('aria-invalid')).toBeUndefined()
		expect(wrapper.get('#cron-help').classes()).not.toContain('is-danger')
		const empty = mountCron(undefined)
		expect((empty.get('input').element as HTMLInputElement).value).toBe('')
		expect(empty.get('input').attributes('aria-invalid')).toBeUndefined()
	})

	it('does not nag about an empty expression: that is the business of the required check', () => {
		expect(mountCron('   ').get('input').attributes('aria-invalid')).toBeUndefined()
	})

	it.each([
		['0 9 * *', 'cron'],
		['0 9 * * *  * *', 'cron'],
		['61 9 * * 1', 'cron'],
		['nonsense', 'cron'],
	])('flags the expression %j with a translated message tied to the input', (expression) => {
		const wrapper = mountCron(expression)
		const input = wrapper.get('input')
		expect(input.attributes('aria-invalid')).toBe('true')
		expect(input.classes()).toContain('is-danger')
		const help = wrapper.get('#cron-help')
		expect(help.classes()).toContain('is-danger')
		expect(help.text()).not.toBe('')
		expect(help.text()).not.toMatch(/^automation\./)
		expect(input.attributes('aria-describedby')).toBe('cron-help')
	})

	it('emits what is typed', async () => {
		const wrapper = mountCron('')
		await wrapper.get('input').setValue('*/5 * * * *')
		expect(wrapper.emitted('update:modelValue')).toEqual([['*/5 * * * *']])
	})

	it('has a button for each preset that fills in its expression', async () => {
		const wrapper = mountCron('')
		const buttons = wrapper.get('[role="group"]').findAll('button')
		expect(buttons).toHaveLength(CRON_PRESETS.length)
		for (const [index, preset] of CRON_PRESETS.entries()) {
			await buttons[index]!.trigger('click')
			expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([preset.value])
		}
		expect(buttons.every(b => b.text() !== '' && !b.text().startsWith('automation.'))).toBe(true)
	})

	it('shows only the help line of the field when the field reports the problem itself', () => {
		const wrapper = mountCron('0 9 * *', {invalid: true})
		expect(wrapper.get('input').attributes('aria-invalid')).toBe('true')
		expect(wrapper.get('#cron-help').classes()).not.toContain('is-danger')
	})
})

describe('RunStatusBadge', () => {
	it.each([
		['success', 'Success', 'is-success'],
		['failed', 'Failed', 'is-danger'],
		['running', 'Running', 'is-info'],
		['skipped', 'Skipped', 'is-neutral'],
		['aborted', 'Aborted', 'is-warning'],
	])('shows %s as "%s" in the colour of its meaning', (status, text, cls) => {
		const wrapper = mountAutomation(RunStatusBadge, {props: {status}})
		expect(wrapper.text()).toBe(text)
		expect(wrapper.classes()).toContain(cls)
	})

	it('shows an unknown status of a newer server as it is, in neutral colour', () => {
		const wrapper = mountAutomation(RunStatusBadge, {props: {status: 'paused'}})
		expect(wrapper.text()).toBe('paused')
		expect(wrapper.classes()).toContain('is-neutral')
	})
})
