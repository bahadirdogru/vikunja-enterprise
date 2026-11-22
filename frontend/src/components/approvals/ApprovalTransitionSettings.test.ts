import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {createPinia} from 'pinia'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({projectsUsersSearch: vi.fn(), projectTeamsList: vi.fn()}))
vi.mock('@/client/generated', () => sdk)

import ApprovalTransitionSettings from './ApprovalTransitionSettings.vue'
import XButton from '@/components/input/Button.vue'
import {createDefaultDraft, newTransition, type TransitionDraft} from '@/helpers/workflow'
import en from '@/i18n/lang/en.json'

const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

const draft = createDefaultDraft({todo: 'To do', inProgress: 'In progress', done: 'Done'})
const customFields = [
	{id: 4, title: 'Editor', type: 'user'},
	{id: 5, title: 'Print run', type: 'number'},
] as never

function mountSettings(transition: TransitionDraft, props: Record<string, unknown> = {}) {
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(ApprovalTransitionSettings, {
		props: {draft, transition, projectId: 3, customFields, errorCodes: [], ...props},
		global: {
			plugins: [createPinia(), i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {Icon: true},
			components: {XButton},
		},
	})
}

function withApproval(patch: Partial<TransitionDraft['approval']> = {}): TransitionDraft {
	const transition = newTransition(-1, -3)
	return {...transition, requires_approval: true, approval: {...transition.approval, ...patch}}
}

enableAutoUnmount(afterEach)

describe('ApprovalTransitionSettings', () => {
	beforeEach(() => {
		sdk.projectsUsersSearch.mockResolvedValue({data: {items: [{id: 2, username: 'anna'}, {id: 3, username: 'bob', name: 'Bob B'}]}})
		sdk.projectTeamsList.mockResolvedValue({data: {items: [{id: 8, name: 'Designers'}], total_pages: 1}})
	})

	it('shows only the toggle while no approval is required, and turns it on through an update', async () => {
		const wrapper = mountSettings(newTransition(-1, -3))
		await flushPromises()
		expect(wrapper.find('.approval-settings__body').exists()).toBe(false)

		await wrapper.find('input[type="checkbox"]').setValue(true)
		expect(wrapper.emitted('update')![0]).toEqual([{requires_approval: true}])
	})

	it('explains both modes and switches between them', async () => {
		const wrapper = mountSettings(withApproval())
		await flushPromises()

		const text = wrapper.text()
		expect(text).toContain('Any one approver')
		expect(text).toContain('The first decision settles the request.')
		expect(text).toContain('Everybody has to approve. One rejection rejects the request.')
		await wrapper.findAll('input[type="radio"]')[1]!.setValue(true)
		expect(wrapper.emitted('update')![0]![0]).toMatchObject({approval: {mode: 'all'}})
	})

	it('adds a person, a team, the assignees and a user field as approver sources', async () => {
		const wrapper = mountSettings(withApproval())
		await flushPromises()
		const [type, id] = wrapper.findAll('select')
		const addButton = () => wrapper.findAll('button').find(button => button.text() === 'Add')!

		expect(addButton().attributes('disabled')).toBeDefined()
		await id!.setValue('3')
		await addButton().trigger('click')
		expect(wrapper.emitted('update')![0]![0]).toMatchObject({approval: {approvers: [{type: 'user', id: 3}]}})

		await type!.setValue('team')
		await wrapper.findAll('select')[1]!.setValue('8')
		await addButton().trigger('click')
		expect(wrapper.emitted('update')![1]![0]).toMatchObject({approval: {approvers: [{type: 'team', id: 8}]}})

		await type!.setValue('assignees')
		expect(wrapper.findAll('select')).toHaveLength(3 - 1) // no second picker
		await addButton().trigger('click')
		expect(wrapper.emitted('update')![2]![0]).toMatchObject({approval: {approvers: [{type: 'assignees', id: 0}]}})

		await type!.setValue('custom_field')
		const options = wrapper.findAll('select')[1]!.findAll('option').map(option => option.text())
		expect(options).toEqual(['Choose…', 'Editor']) // only fields of the type user
	})

	it('lists the chosen sources with their names and removes one', async () => {
		const wrapper = mountSettings(withApproval({
			approvers: [
				{key: 'a', type: 'user', id: 3},
				{key: 'b', type: 'team', id: 8},
				{key: 'c', type: 'assignees', id: 0},
				{key: 'd', type: 'custom_field', id: 4},
			],
		}))
		await flushPromises()

		const labels = wrapper.findAll('.approval-settings__source-label').map(label => label.text())
		expect(labels).toEqual(['Bob B', 'Team Designers', 'Assignees of the task', 'Field Editor'])
		await wrapper.find('button[aria-label="Remove Team Designers"]').trigger('click')
		expect(wrapper.emitted('update')![0]![0]).toMatchObject({
			approval: {approvers: [{key: 'a'}, {key: 'c'}, {key: 'd'}]},
		})
	})

	it('offers the other statuses as rejected status, the due duration and the self approval switch', async () => {
		const wrapper = mountSettings(withApproval())
		await flushPromises()

		const rejected = wrapper.findAll('select').at(-1)!
		expect(rejected.findAll('option').map(option => option.text())).toEqual(['Stay in the current status', 'To do', 'In progress'])
		await rejected.setValue('-2')
		expect(wrapper.emitted('update')![0]![0]).toMatchObject({approval: {rejected_status_id: -2}})

		await wrapper.find('input[placeholder="2d"]').setValue('3d')
		expect(wrapper.emitted('update')![1]![0]).toMatchObject({approval: {due_in: '3d'}})

		await wrapper.findAll('input[type="checkbox"]')[1]!.setValue(true)
		expect(wrapper.emitted('update')![2]![0]).toMatchObject({approval: {allow_self: true}})
	})

	it('shows the client validation errors and the server message at their controls', async () => {
		const wrapper = mountSettings(withApproval({due_in: 'soon'}), {
			errorCodes: ['approvalApproversRequired', 'approvalDueInvalid'],
			serverError: {field: 'rejectedStatus', message: 'the rejected status must not be archived'},
		})
		await flushPromises()

		const alerts = wrapper.findAll('[role="alert"]').map(alert => alert.text())
		expect(alerts).toEqual([
			'Add at least one approver.',
			'the rejected status must not be archived',
			'Use a duration like 12h, 2d or 1w.',
		])
		expect(wrapper.find('input[placeholder="2d"]').attributes('aria-invalid')).toBe('true')
	})
})
