import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'

const sdk = vi.hoisted(() => ({formSubmissionsList: vi.fn()}))
vi.mock('@/client/generated', () => sdk)

import FormSubmissions from './FormSubmissions.vue'
import {mountForms, storedForm} from './testing'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

const row = (over: Record<string, unknown> = {}) => ({
	id: 1, task_id: 41, submitter_id: null, submitter_name: '', submitter_email: '', created: '2026-11-20T09:30:00Z', ...over,
})

function mountList() {
	return mountForms(FormSubmissions, {
		props: {projectId: 3, form: storedForm({title: 'Cover request'})},
		global: {stubs: {RouterLink: {props: ['to'], template: '<a class="task-link" :data-to="JSON.stringify(to)"><slot /></a>'}}},
	})
}

const page = (items: unknown[], totalPages = 1) => ({data: {items, total_pages: totalPages, page: 1}})
async function settle() {
	for (let i = 0; i < 4; i++) await flushPromises()
}

describe('FormSubmissions', () => {
	beforeEach(() => {
		sdk.formSubmissionsList.mockReset()
		setActivePinia(createPinia())
	})

	it('asks for the first page of the form of this project', async () => {
		sdk.formSubmissionsList.mockResolvedValue(page([]))
		mountList()
		await settle()
		expect(sdk.formSubmissionsList).toHaveBeenCalledWith(expect.objectContaining({path: {project: 3, form: 5}, query: {page: 1}}))
	})

	it('says so when nobody has answered yet', async () => {
		sdk.formSubmissionsList.mockResolvedValue(page([]))
		const {wrapper} = mountList()
		await settle()
		expect(wrapper.find('table').exists()).toBe(false)
		expect(wrapper.text()).toContain('Nobody has submitted this form yet.')
	})

	it('lists who answered and links to the task each answer created', async () => {
		sdk.formSubmissionsList.mockResolvedValue(page([
			row({id: 1, task_id: 41, submitter_name: 'Ada', submitter_email: 'ada@example.com'}),
			row({id: 2, task_id: 42, submitter_id: 9, submitter_name: 'Ben'}),
			row({id: 3, task_id: 43}),
		]))
		const {wrapper} = mountList()
		await settle()
		const rows = wrapper.findAll('tbody tr')
		expect(rows).toHaveLength(3)
		expect(rows[0]!.findAll('td')[1]!.text()).toBe('Ada · ada@example.com')
		expect(rows[1]!.findAll('td')[1]!.text()).toContain('Ben')
		expect(rows[1]!.findAll('td')[1]!.text()).toContain('signed in')
		expect(rows[2]!.findAll('td')[1]!.text()).toContain('Anonymous')
		expect(rows[2]!.get('a.task-link').text()).toBe('#43')
		expect(JSON.parse(rows[2]!.get('a.task-link').attributes('data-to')!)).toEqual({name: 'task.detail', params: {id: 43}})
		expect(rows[0]!.get('time').attributes('datetime')).toBe('2026-11-20T09:30:00Z')
		expect(wrapper.get('caption').text()).toContain('Cover request')
	})

	it('renders a name that looks like markup as text', async () => {
		sdk.formSubmissionsList.mockResolvedValue(page([row({submitter_name: '<img src=x onerror=alert(1)>'})]))
		const {wrapper} = mountList()
		await settle()
		expect(wrapper.find('tbody img').exists()).toBe(false)
		expect(wrapper.get('tbody').text()).toContain('<img src=x onerror=alert(1)>')
	})

	it('pages through the submissions and stops at both ends', async () => {
		sdk.formSubmissionsList.mockImplementation(async ({query}: {query: {page: number}}) => page([row({id: query.page, task_id: 100 + query.page})], 3))
		const {wrapper} = mountList()
		await settle()
		const button = (text: string) => wrapper.findAll('button').find(b => b.text() === text)!
		expect(wrapper.text()).toContain('1 / 3')
		expect(button('Previous').attributes('disabled')).toBeDefined()

		await button('Next').trigger('click')
		await settle()
		expect(sdk.formSubmissionsList).toHaveBeenLastCalledWith(expect.objectContaining({query: {page: 2}}))
		expect(wrapper.text()).toContain('#102')
		await button('Next').trigger('click')
		await settle()
		expect(button('Next').attributes('disabled')).toBeDefined()
		expect(wrapper.text()).toContain('3 / 3')
	})

	it('shows no pager for a single page', async () => {
		sdk.formSubmissionsList.mockResolvedValue(page([row()]))
		const {wrapper} = mountList()
		await settle()
		expect(wrapper.text()).not.toContain(' / ')
	})

	it('shows the error component when the list cannot be loaded', async () => {
		sdk.formSubmissionsList.mockRejectedValue(new ApiError({status: 500, detail: 'boom'}))
		const {wrapper} = mountList()
		await settle()
		expect(wrapper.find('table').exists()).toBe(false)
		expect(wrapper.text()).not.toContain('Nobody has submitted this form yet.')
	})

	it('goes back to the share tab', async () => {
		sdk.formSubmissionsList.mockResolvedValue(page([]))
		const {wrapper} = mountList()
		await settle()
		await wrapper.findAll('button').find(b => b.text().includes('Back'))!.trigger('click')
		expect(wrapper.emitted('back')).toHaveLength(1)
	})
})
