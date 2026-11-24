import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'
import {ref} from 'vue'

const sdk = vi.hoisted(() => ({
	automationsCreate: vi.fn(),
	automationsUpdate: vi.fn(),
	projectTasksList: vi.fn(),
	tasksList: vi.fn(),
	automationsTest: vi.fn(),
	automationWebhookToken: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
	getErrorText: (cause: {detail?: string}) => cause.detail ?? 'error',
	translatedError: (key: string) => new Error(key),
}))
vi.mock('@/composables/useUserSearch', () => ({
	useProjectUserSearch: () => ({users: ref([{id: 7, username: 'ada', name: 'Ada'}]), isFetching: ref(false)}),
}))
vi.mock('@/composables/useLabels', () => ({useLabels: () => ({labels: ref([{id: 5, title: 'Urgent'}])})}))
vi.mock('@/composables/useProjects', () => ({useProjects: () => ({projectsArray: [{id: 1, title: 'Inbox', is_archived: false}]})}))
vi.mock('@/components/custom-fields/CustomFieldValueEditor.vue', () => ({
	default: {
		props: ['field', 'modelValue'],
		emits: ['update:modelValue'],
		template: '<button type="button" data-test="cf-editor" :data-type="field.type" @click="$emit(\'update:modelValue\', 99)">edit</button>',
	},
}))

import RuleEditor from './RuleEditor.vue'
import {ApiError} from '@/client/problem'
import {catalog, customFields, mountAutomation} from './testing'
import type {AutomationRuleResponse} from '@/client/queries/automation'

enableAutoUnmount(afterEach)

const statuses = [{id: 20, title: 'To do', category: 'open'}, {id: 21, title: 'Done', category: 'done'}]

function mountEditor(rule: AutomationRuleResponse | null = null, extra: Record<string, unknown> = {}) {
	return mountAutomation(RuleEditor, {
		props: {projectId: 3, catalog, customFields, statuses, secrets: null, rule, ...extra},
	})
}

const storedRule: AutomationRuleResponse = {
	id: 9, project_id: 3, title: 'Existing', description: '', enabled: true, revision: 2, owner_id: 1,
	trigger: {type: 'task.created', options: {}},
	conditions: {all: [{field: 'priority', op: '>=', value: 3}]},
	actions: [{type: 'task.add_comment', params: {text: 'hi'}}],
	disabled_reason: '', consecutive_failures: 0, last_run_at: null, last_run_status: '', created: '', updated: '',
}

function issueProblem(issues: {path: string, message: string}[]) {
	return new ApiError({status: 400, detail: 'The rule is invalid', code: 22002, details: {issues}})
}

async function pick(wrapper: ReturnType<typeof mountEditor>, selector: string, value: string) {
	await wrapper.get(selector).setValue(value)
}

describe('RuleEditor', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.projectTasksList.mockResolvedValue({data: {items: [{id: 1, title: 'First task', project_id: 3}], total_pages: 1}})
	})

	it('generates the trigger and action pickers from the catalog', () => {
		const wrapper = mountEditor()
		const triggerOptions = wrapper.get('#automation-trigger-type').findAll('option').map(o => o.attributes('value')).filter(Boolean)
		// grouped by the first segment of the type, so the order differs from the catalog's
		expect([...triggerOptions].sort()).toEqual(catalog.triggers.map(t => t.type).sort())
		const addOptions = wrapper.get('#automation-add-action').findAll('option').map(o => o.attributes('value')).filter(Boolean)
		expect([...addOptions].sort()).toEqual(catalog.actions.map(a => a.type).sort())
		// long lists get a search field
		expect(wrapper.findAll('input[type=search]').length).toBeGreaterThanOrEqual(2)
	})

	it('renders trigger options and action params from their definitions', async () => {
		const wrapper = mountEditor()
		await pick(wrapper, '#automation-trigger-type', 'task.status_changed')
		expect(wrapper.find('#automation-trigger-options-to_category').exists()).toBe(true)
		expect(wrapper.find('#automation-trigger-options-from_status_id').exists()).toBe(true)

		await pick(wrapper, '#automation-add-action', 'task.set_status')
		expect(wrapper.find('#automation-actions-0-params-status_id').exists()).toBe(true)
		expect(wrapper.find('#automation-actions-0-params-comment').exists()).toBe(true)
		expect(wrapper.text()).toContain('Status *')
	})

	it('adapts the value of set_field to the chosen field and of set_custom_field to the custom field type', async () => {
		const wrapper = mountEditor()
		await pick(wrapper, '#automation-add-action', 'task.set_field')
		expect(wrapper.text()).toContain('Choose "Field" first.')
		await pick(wrapper, '#automation-actions-0-params-field', 'priority')
		expect(wrapper.get('#automation-actions-0-params-value').attributes('type')).toBe('number')
		await pick(wrapper, '#automation-actions-0-params-field', 'due_date')
		expect(wrapper.text()).toContain('+3d')

		await pick(wrapper, '#automation-add-action', 'task.set_custom_field')
		expect(wrapper.text()).toContain('Choose the custom field first.')
		await pick(wrapper, '#automation-actions-1-params-field_id', '2')
		expect(wrapper.get('[data-test=cf-editor]').attributes('data-type')).toBe('select')
		await wrapper.get('[data-test=cf-editor]').trigger('click')
	})

	it('moves, duplicates and removes actions', async () => {
		const wrapper = mountEditor({...storedRule, actions: [
			{type: 'task.add_comment', params: {text: 'one'}},
			{type: 'task.set_done', params: {done: true}},
		]})
		const titles = () => wrapper.findAll('.action-card select[aria-label="Action type"]').map(s => (s.element as HTMLSelectElement).value)
		expect(titles()).toEqual(['task.add_comment', 'task.set_done'])
		await wrapper.get('button[aria-label="Move action 2 up"]').trigger('click')
		expect(titles()).toEqual(['task.set_done', 'task.add_comment'])
		await wrapper.get('button[aria-label="Duplicate action 1"]').trigger('click')
		expect(titles()).toEqual(['task.set_done', 'task.set_done', 'task.add_comment'])
		await wrapper.get('button[aria-label="Remove action 3"]').trigger('click')
		expect(titles()).toEqual(['task.set_done', 'task.set_done'])
	})

	it('shows client validation on the exact inputs and does not call the API', async () => {
		const wrapper = mountEditor()
		await pick(wrapper, '#automation-trigger-type', 'task.created')
		await pick(wrapper, '#automation-add-action', 'task.set_status')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(sdk.automationsCreate).not.toHaveBeenCalled()
		expect(wrapper.get('#automation-title').attributes('aria-invalid')).toBe('true')
		expect(wrapper.get('#automation-actions-0-params-status_id').attributes('aria-invalid')).toBe('true')
		expect(wrapper.text()).toContain('This is required.')
	})

	it('creates a rule and stays in the editor with the saved version', async () => {
		sdk.automationsCreate.mockResolvedValue({data: {...storedRule, id: 31, title: 'New rule'}})
		const wrapper = mountEditor()
		await wrapper.get('#automation-title').setValue('New rule')
		await pick(wrapper, '#automation-trigger-type', 'task.created')
		await pick(wrapper, '#automation-add-action', 'task.add_comment')
		await wrapper.get('#automation-actions-0-params-text').setValue('hello')
		expect(wrapper.text()).toContain('Unsaved changes')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(sdk.automationsCreate).toHaveBeenCalledTimes(1)
		expect(sdk.automationsCreate.mock.calls[0][0].body).toEqual({
			title: 'New rule', description: '', enabled: true,
			trigger: {type: 'task.created'},
			actions: [{type: 'task.add_comment', params: {text: 'hello'}}],
		})
		expect(sdk.automationsCreate.mock.calls[0][0].path).toEqual({project: 3})
		expect(wrapper.emitted('saved')).toBeTruthy()
		expect(wrapper.text()).not.toContain('Unsaved changes')
		// the second save updates instead of creating
		sdk.automationsUpdate.mockResolvedValue({data: {...storedRule, id: 31, title: 'Renamed'}})
		await wrapper.get('#automation-title').setValue('Renamed')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(sdk.automationsUpdate).toHaveBeenCalledTimes(1)
		expect(sdk.automationsUpdate.mock.calls[0][0].path).toEqual({project: 3, rule: 31})
	})

	it('maps server validation errors onto the exact inputs and lists the unmapped ones on top', async () => {
		sdk.automationsUpdate.mockRejectedValue(issueProblem([
			{path: 'actions[1].params.status_id', message: 'the status does not exist'},
			{path: 'conditions.all[0].value', message: 'must be a number'},
			{path: 'something.odd', message: 'unexpected problem'},
		]))
		const wrapper = mountEditor({...storedRule, actions: [
			{type: 'task.add_comment', params: {text: 'a'}},
			{type: 'task.set_status', params: {status_id: 999}},
		]})
		const scroll = vi.fn()
		window.HTMLElement.prototype.scrollIntoView = scroll
		await wrapper.get('form').trigger('submit')
		await flushPromises()

		const status = wrapper.get('#automation-actions-1-params-status_id')
		expect(status.attributes('aria-invalid')).toBe('true')
		const field = status.element.closest('.param-field')!
		expect(field.textContent).toContain('the status does not exist')
		expect(wrapper.get('#automation-conditions-all-0-value').attributes('aria-invalid')).toBe('true')
		expect(wrapper.text()).toContain('must be a number')
		// unmapped: shown at the top with its path
		const top = wrapper.get('.message.is-danger, .message.danger')
		expect(top.text()).toContain('unexpected problem')
		// the first mapped input is scrolled into view and focused
		expect(scroll).toHaveBeenCalled()
		expect(document.activeElement?.id).toBe('automation-conditions-all-0-value')

		// the messages go away with the next edit
		await wrapper.get('#automation-title').setValue('Other title')
		expect(wrapper.text()).not.toContain('the status does not exist')
	})

	it('shows a failure without issues at the top', async () => {
		sdk.automationsUpdate.mockRejectedValue(new ApiError({status: 500, detail: 'database is down'}))
		const wrapper = mountEditor(storedRule)
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(wrapper.text()).toContain('database is down')
	})

	it('explains the disabled reason of a rule the system turned off', () => {
		const wrapper = mountEditor({...storedRule, enabled: false, disabled_reason: 'too_many_failures'})
		expect(wrapper.text()).toContain('failed too many times')
	})

	it('requires a saved, unchanged rule for the dry run and runs it', async () => {
		const unsaved = mountEditor()
		expect(unsaved.text()).toContain('Save the rule first')
		expect(unsaved.get('button.is-primary[type=button]').attributes('disabled')).toBeDefined()

		sdk.automationsTest.mockResolvedValue({data: {
			task_id: 1, conditions_met: true,
			steps: [{position: 0, type: 'task.add_comment', summary: 'Would add a comment', warnings: ['unknown placeholder {{x}}'], error: '', stops: false}],
			notes: [],
		}})
		const wrapper = mountEditor(storedRule)
		await flushPromises()
		expect(wrapper.text()).not.toContain('Save the rule first')
		await wrapper.get('.dry-run select').setValue('1')
		await wrapper.get('button.is-primary[type=button]').trigger('click')
		await flushPromises()
		expect(sdk.automationsTest.mock.calls[0][0]).toEqual({path: {project: 3, rule: 9}, body: {task_id: 1}})
		expect(wrapper.text()).toContain('The conditions are met')
		expect(wrapper.text()).toContain('Would add a comment')
		expect(wrapper.text()).toContain('unknown placeholder')

		// a changed draft cannot be tested
		await wrapper.get('#automation-title').setValue('Edited')
		expect(wrapper.text()).toContain('unsaved changes')
	})

	describe('dry run of a rule without a task', () => {
		const dryRunAnswer = {data: {task_id: 0, conditions_met: true, steps: [], notes: []}}
		const webhookRule: AutomationRuleResponse = {
			...storedRule, id: 31, trigger: {type: 'webhook.incoming', options: {}}, conditions: null,
			actions: [{type: 'task.create', params: {title: 'x'}}], has_webhook: true,
		}
		const testButton = (wrapper: ReturnType<typeof mountEditor>) => wrapper.get('.dry-run button.is-primary')

		beforeEach(() => {
			sdk.automationsTest.mockReset()
			sdk.automationsTest.mockResolvedValue(dryRunAnswer)
		})

		it('needs no task for an incoming webhook and sends the simulated body as payload', async () => {
			const wrapper = mountEditor(webhookRule)
			await flushPromises()
			expect(wrapper.find('.dry-run select').exists()).toBe(false)
			expect(testButton(wrapper).attributes('disabled')).toBeUndefined()
			await wrapper.get('#automation-test-payload').setValue('{"ticket": 7}')
			await testButton(wrapper).trigger('click')
			await flushPromises()
			expect(sdk.automationsTest.mock.calls[0][0]).toEqual({path: {project: 3, rule: 31}, body: {payload: {ticket: 7}}})
		})

		it('sends no payload when the textarea is empty', async () => {
			const wrapper = mountEditor(webhookRule)
			await flushPromises()
			await testButton(wrapper).trigger('click')
			await flushPromises()
			expect(sdk.automationsTest.mock.calls[0][0].body).toEqual({})
		})

		it.each([
			['{"a":', 'not valid JSON'],
			['[1, 2]', 'has to be a JSON object'],
			['"text"', 'has to be a JSON object'],
			[JSON.stringify({text: 'x'.repeat(64 * 1024)}), 'larger than 64 KiB'],
		])('does not test with the body %s', async (text, message) => {
			const wrapper = mountEditor(webhookRule)
			await flushPromises()
			await wrapper.get('#automation-test-payload').setValue(text)
			expect(wrapper.get('.dry-run').text()).toContain(message)
			expect(testButton(wrapper).attributes('disabled')).toBeDefined()
			await testButton(wrapper).trigger('click')
			expect(sdk.automationsTest).not.toHaveBeenCalled()
		})

		it('needs no task for a schedule per project and offers no payload', async () => {
			const schedule = {...webhookRule, trigger: {type: 'schedule', options: {cron: '@daily', scope: 'project'}}, has_webhook: false}
			const wrapper = mountEditor(schedule)
			await flushPromises()
			expect(wrapper.find('.dry-run select').exists()).toBe(false)
			expect(wrapper.find('#automation-test-payload').exists()).toBe(false)
			await testButton(wrapper).trigger('click')
			await flushPromises()
			expect(sdk.automationsTest.mock.calls[0][0].body).toEqual({})
		})

		it('still needs a task for a schedule per task', async () => {
			const schedule = {...webhookRule, trigger: {type: 'schedule', options: {cron: '@daily', scope: 'tasks'}}, has_webhook: false}
			const wrapper = mountEditor(schedule)
			await flushPromises()
			expect(wrapper.find('.dry-run select').exists()).toBe(true)
			expect(testButton(wrapper).attributes('disabled')).toBeDefined()
		})
	})

	describe('triggers and actions of the time, manual and webhook family', () => {
		it('shows presets, the time zone note and the problem of a cron expression', async () => {
			const wrapper = mountEditor()
			await pick(wrapper, '#automation-trigger-type', 'schedule')
			expect(wrapper.text()).toContain('time zone of the server')
			expect(wrapper.text()).toContain('does not replay the past')
			const buttons = wrapper.findAll('.cron-input__presets button').map(b => b.text())
			expect(buttons).toEqual(['Every hour', 'Every day', 'Every week', 'Every month'])
			await wrapper.findAll('.cron-input__presets button')[1].trigger('click')
			expect((wrapper.get('#automation-trigger-options-cron').element as HTMLInputElement).value).toBe('@daily')
			await wrapper.get('#automation-trigger-options-cron').setValue('61 * * * *')
			expect(wrapper.text()).toContain('out of range')
			expect(wrapper.get('#automation-trigger-options-cron').attributes('aria-invalid')).toBe('true')
		})

		it('shows the problem of a cron expression once, also after a failed save', async () => {
			const wrapper = mountEditor()
			await wrapper.get('#automation-title').setValue('Timed')
			await pick(wrapper, '#automation-trigger-type', 'schedule')
			await wrapper.get('#automation-trigger-options-cron').setValue('61 * * * *')
			const occurrences = () => wrapper.text().split('A value is out of range').length - 1
			expect(occurrences()).toBe(1)
			await pick(wrapper, '#automation-add-action', 'task.create')
			await wrapper.get('#automation-actions-0-params-title').setValue('x')
			await wrapper.get('form').trigger('submit')
			await flushPromises()
			expect(sdk.automationsCreate).not.toHaveBeenCalled()
			expect(occurrences()).toBe(1)
			expect(wrapper.get('#automation-trigger-options-cron').attributes('aria-invalid')).toBe('true')
		})

		it('does not save a rule with a bad cron expression, a too short wait or nobody to notify', async () => {
			const wrapper = mountEditor()
			await wrapper.get('#automation-title').setValue('Timed')
			await pick(wrapper, '#automation-trigger-type', 'schedule')
			await wrapper.get('#automation-trigger-options-cron').setValue('* * *')
			await pick(wrapper, '#automation-add-action', 'wait')
			await wrapper.get('#automation-actions-0-params-duration').setValue('30s')
			await pick(wrapper, '#automation-add-action', 'notify')
			await wrapper.get('#automation-actions-1-params-message').setValue('hi')
			await wrapper.get('form').trigger('submit')
			await flushPromises()
			expect(sdk.automationsCreate).not.toHaveBeenCalled()
			expect(wrapper.text()).toContain('Use five fields')
			expect(wrapper.text()).toContain('Use a number and a unit')
			expect(wrapper.text()).toContain('Name at least one recipient or a team.')
		})

		it('explains the arming of the overdue and due soon triggers and asks for the offset', async () => {
			const wrapper = mountEditor()
			await pick(wrapper, '#automation-trigger-type', 'task.overdue')
			expect(wrapper.text()).toContain('already overdue when the rule is created or enabled do not fire')
			await pick(wrapper, '#automation-trigger-type', 'task.due_soon')
			expect(wrapper.text()).toContain('already inside the window')
			expect(wrapper.find('#automation-trigger-options-offset').exists()).toBe(true)
		})

		it('has no options for the manual trigger apart from the button text', async () => {
			const wrapper = mountEditor()
			await pick(wrapper, '#automation-trigger-type', 'manual')
			expect(wrapper.find('#automation-trigger-options-label').exists()).toBe(true)
			expect(wrapper.text()).toContain('Run automation')
		})

		it('renders the parameters of create task, wait and notify from the catalog', async () => {
			const wrapper = mountEditor()
			await pick(wrapper, '#automation-add-action', 'task.create')
			for (const name of ['title', 'description', 'priority', 'assignee', 'due_offset']) {
				expect(wrapper.find(`#automation-actions-0-params-${name}`).exists()).toBe(true)
			}
			await pick(wrapper, '#automation-add-action', 'wait')
			expect(wrapper.find('#automation-actions-1-params-duration').exists()).toBe(true)
			expect(wrapper.text()).toContain('maximum set by the administrator')
			await pick(wrapper, '#automation-add-action', 'notify')
			expect(wrapper.find('#automation-actions-2-params-recipients').exists()).toBe(true)
			expect(wrapper.find('#automation-actions-2-params-team_id').exists()).toBe(true)
			expect(wrapper.find('#automation-actions-2-params-message').exists()).toBe(true)
		})
	})

	describe('incoming webhook address', () => {
		const writeText = vi.fn().mockResolvedValue(undefined)
		const hook = {webhook_token: 'tok-1', webhook_url: 'https://vikunja.example/api/v2/automation/hooks/tok-1'}

		beforeEach(() => {
			writeText.mockClear()
			Object.defineProperty(navigator, 'clipboard', {value: {writeText}, configurable: true})
		})

		async function saveNewWebhookRule(response: Record<string, unknown>) {
			sdk.automationsCreate.mockResolvedValue({data: {
				...storedRule, id: 31, title: 'Hook', trigger: {type: 'webhook.incoming', options: {}},
				conditions: null, actions: [{type: 'task.create', params: {title: 'x'}}], has_webhook: true, ...response,
			}})
			const wrapper = mountEditor()
			await wrapper.get('#automation-title').setValue('Hook')
			await pick(wrapper, '#automation-trigger-type', 'webhook.incoming')
			expect(wrapper.text()).toContain('created when you save the rule')
			await pick(wrapper, '#automation-add-action', 'task.create')
			await wrapper.get('#automation-actions-0-params-title').setValue('x')
			await wrapper.get('form').trigger('submit')
			await flushPromises()
			return wrapper
		}

		it('shows the address once after the save, with a warning and a copy button', async () => {
			const wrapper = await saveNewWebhookRule(hook)
			expect(sdk.automationsCreate).toHaveBeenCalledTimes(1)
			expect(wrapper.get('#automation-webhook-url').element).toHaveProperty('value', hook.webhook_url)
			expect(wrapper.text()).toContain('cannot be shown again')
			expect(wrapper.text()).toContain('protected against replays')
			expect(wrapper.text()).toContain('X-Vikunja-Signature')
			expect(wrapper.text()).toContain('X-Vikunja-Timestamp')
			const copy = wrapper.findAll('button').find(b => b.text() === 'Copy')!
			await copy.trigger('click')
			await flushPromises()
			expect(writeText).toHaveBeenCalledWith(hook.webhook_url)
			// the token is not part of what the editor tells its parent
			expect(JSON.stringify(wrapper.emitted('saved'))).not.toContain('tok-1')
		})

		async function rotateAgain(wrapper: ReturnType<typeof mountEditor>) {
			await wrapper.findAll('button').find(b => b.text() === 'Create another new address')!.trigger('click')
			await flushPromises()
			;(document.body.querySelector('dialog .actions > :last-child') as HTMLElement).click()
			await flushPromises()
		}

		it('turns a relative address into one on the API the client talks to', async () => {
			const relative = {webhook_token: 'tok-1', webhook_url: 'api/v2/automation/hooks/tok-1'}
			const wrapper = await saveNewWebhookRule(relative)
			const shown = `${window.location.origin}/api/v2/automation/hooks/tok-1`
			expect(wrapper.get('#automation-webhook-url').element).toHaveProperty('value', shown)
			await wrapper.findAll('button').find(b => b.text() === 'Copy')!.trigger('click')
			await flushPromises()
			expect(writeText).toHaveBeenCalledWith(shown)
		})

		it.each([
			['the request fails', () => Object.assign(new Error('offline'), {detail: 'network down'}), true],
			['the request context changed', () => new DOMException('Client request context changed', 'AbortError'), false],
		])('drops the old address and says it has to be generated again when %s', async (_name, cause, showsError) => {
			const wrapper = await saveNewWebhookRule(hook)
			expect(wrapper.find('#automation-webhook-url').exists()).toBe(true)
			sdk.automationWebhookToken.mockRejectedValue(cause())
			await rotateAgain(wrapper)
			expect(sdk.automationWebhookToken).toHaveBeenCalledTimes(1)
			expect(wrapper.find('#automation-webhook-url').exists()).toBe(false)
			expect(wrapper.text()).not.toContain(hook.webhook_url)
			expect(wrapper.text()).toContain('may have changed already')
			expect(wrapper.text().includes('network down')).toBe(showsError)
			expect(wrapper.vm.addressUncopied).toBe(false)
		})

		it('knows when the one-time address was shown and not copied yet', async () => {
			const wrapper = await saveNewWebhookRule(hook)
			expect(wrapper.vm.addressUncopied).toBe(true)
			await wrapper.findAll('button').find(b => b.text() === 'Copy')!.trigger('click')
			await flushPromises()
			expect(wrapper.vm.addressUncopied).toBe(false)
			sdk.automationWebhookToken.mockResolvedValue({data: {token: 'tok-2', url: 'https://vikunja.example/api/v2/automation/hooks/tok-2'}})
			await rotateAgain(wrapper)
			expect(wrapper.vm.addressUncopied).toBe(true)
		})

		it('does not show an address the server did not send and offers a new one instead', async () => {
			sdk.automationWebhookToken.mockResolvedValue({data: {token: 'tok-2', url: 'https://vikunja.example/api/v2/automation/hooks/tok-2'}})
			const wrapper = await saveNewWebhookRule({})
			expect(wrapper.find('#automation-webhook-url').exists()).toBe(false)
			expect(wrapper.text()).toContain('cannot be shown again')
			await wrapper.findAll('button').find(b => b.text() === 'Create a new address')!.trigger('click')
			await flushPromises()
			// the confirmation dialog is teleported to the body
			expect(document.body.textContent).toContain('The current address stops working at once.')
			;(document.body.querySelector('dialog .actions > :last-child') as HTMLElement).click()
			await flushPromises()
			expect(sdk.automationWebhookToken).toHaveBeenCalledTimes(1)
			expect(sdk.automationWebhookToken.mock.calls[0][0].path).toEqual({project: 3, rule: 31})
			expect(wrapper.get('#automation-webhook-url').element).toHaveProperty('value', 'https://vikunja.example/api/v2/automation/hooks/tok-2')
		})
	})
})
