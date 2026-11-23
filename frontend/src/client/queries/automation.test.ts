import {beforeEach, describe, expect, it, vi} from 'vitest'
import {defineComponent, h} from 'vue'
import {mount} from '@vue/test-utils'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	automationManualList: vi.fn(),
	automationRunManually: vi.fn(),
	automationWebhookToken: vi.fn(),
	automationsCreate: vi.fn(),
	automationsUpdate: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/message', () => ({success: vi.fn(), error: vi.fn()}))

import {success} from '@/message'
import {
	automationKeys,
	manualAutomationsQuery,
	rotateWebhookTokenMutationOptions,
	runRuleManuallyMutationOptions,
	saveRuleMutationOptions,
	useRotateWebhookTokenMutation,
	useSaveRuleMutation,
	webhookRevealOf,
	type AutomationRuleResponse,
} from './automation'

const document = {
	title: 'Hook', description: '', enabled: true,
	trigger: {type: 'webhook.incoming'},
	actions: [{type: 'task.create', params: {title: 'x'}}],
}

const saved = {
	id: 5, project_id: 3, title: 'Hook', description: '', enabled: true, revision: 1, owner_id: 1,
	trigger: {type: 'webhook.incoming', options: {}}, actions: [{type: 'task.create', params: {title: 'x'}}],
	has_webhook: true, webhook_token: 'secret-token', webhook_url: 'https://vikunja.example/api/v2/automation/hooks/secret-token',
}

beforeEach(() => {
	Object.values(sdk).forEach(mock => mock.mockReset())
	vi.mocked(success).mockClear()
})

describe('manual automations', () => {
	it('lists the rules of a project with the button label, falling back to the title', async () => {
		sdk.automationManualList.mockResolvedValue({data: [{id: 1, title: 'Escalate', label: 'Escalate now'}, {id: 2, title: 'Archive'}, {title: 'broken'}]})
		const client = new QueryClient()
		expect(await client.fetchQuery(manualAutomationsQuery(3))).toEqual([
			{id: 1, title: 'Escalate', label: 'Escalate now'},
			{id: 2, title: 'Archive', label: 'Archive'},
		])
		expect(sdk.automationManualList).toHaveBeenCalledWith(expect.objectContaining({path: {project: 3}}))
	})

	it('treats an empty answer as no rules', async () => {
		sdk.automationManualList.mockResolvedValue({data: null})
		expect(await new QueryClient().fetchQuery(manualAutomationsQuery(3))).toEqual([])
	})

	it('runs a rule for a task and confirms with the title of the rule', async () => {
		sdk.automationRunManually.mockResolvedValue({data: {event_id: 9, status: 'queued'}})
		const client = new QueryClient()
		await client.getMutationCache().build(client, runRuleManuallyMutationOptions())
			.execute({projectId: 3, ruleId: 1, taskId: 42, title: 'Escalate'})
		expect(sdk.automationRunManually).toHaveBeenCalledWith({path: {project: 3, rule: 1}, body: {task_id: 42}})
		expect(success).toHaveBeenCalledWith({message: expect.stringContaining('Escalate')})
	})
})

describe('a manual run that is refused', () => {
	it('drops the stale list of manual rules, which a successful run leaves alone', async () => {
		const client = new QueryClient()
		client.setQueryData(automationKeys.manual(3), [{id: 1, title: 'Escalate', label: 'Escalate'}])
		sdk.automationRunManually.mockRejectedValue({status: 409, code: 22009})
		await expect(client.getMutationCache().build(client, runRuleManuallyMutationOptions())
			.execute({projectId: 3, ruleId: 1, taskId: 42, title: 'Escalate'})).rejects.toBeDefined()
		expect(client.getQueryState(automationKeys.manual(3))?.isInvalidated).toBe(true)

		client.setQueryData(automationKeys.manual(3), [{id: 1, title: 'Escalate', label: 'Escalate'}])
		sdk.automationRunManually.mockResolvedValue({data: {event_id: 9, status: 'queued'}})
		await client.getMutationCache().build(client, runRuleManuallyMutationOptions())
			.execute({projectId: 3, ruleId: 1, taskId: 42, title: 'Escalate'})
		expect(client.getQueryState(automationKeys.manual(3))?.isInvalidated).toBe(false)
	})
})

describe('the address of an incoming webhook', () => {
	it('resolves a relative address against the API of the client', async () => {
		expect(webhookRevealOf({webhook_token: 't', webhook_url: 'api/v2/automation/hooks/t'}))
			.toEqual({token: 't', url: `${window.location.origin}/api/v2/automation/hooks/t`})
		sdk.automationWebhookToken.mockResolvedValue({data: {token: 'n', url: 'api/v2/automation/hooks/n'}})
		const client = new QueryClient()
		expect(await client.getMutationCache().build(client, rotateWebhookTokenMutationOptions()).execute({projectId: 3, ruleId: 5}))
			.toEqual({token: 'n', url: `${window.location.origin}/api/v2/automation/hooks/n`})
	})

	it('reads it only when the server sent the token and the url', () => {
		expect(webhookRevealOf({webhook_token: 't', webhook_url: 'https://x/hooks/t'})).toEqual({token: 't', url: 'https://x/hooks/t'})
		expect(webhookRevealOf({webhook_token: 't'})).toBeUndefined()
		expect(webhookRevealOf({})).toBeUndefined()
	})

	it('hands the address of a save to the caller but keeps it out of the rule cache', async () => {
		sdk.automationsCreate.mockResolvedValue({data: saved})
		const client = new QueryClient()
		client.setQueryData<AutomationRuleResponse[]>(automationKeys.rules(3), [])
		const result = await client.getMutationCache().build(client, saveRuleMutationOptions())
			.execute({projectId: 3, ruleId: null, document})
		expect(result.webhook).toEqual({token: 'secret-token', url: saved.webhook_url})
		expect(result.has_webhook).toBe(true)
		const cached = client.getQueryData<AutomationRuleResponse[]>(automationKeys.rules(3))!
		expect(cached.map(rule => rule.id)).toEqual([5])
		expect(cached[0].has_webhook).toBe(true)
		expect(JSON.stringify(cached)).not.toContain('secret-token')
	})

	it('has no address for a save that did not create one', async () => {
		sdk.automationsUpdate.mockResolvedValue({data: {...saved, webhook_token: undefined, webhook_url: undefined}})
		const client = new QueryClient()
		const result = await client.getMutationCache().build(client, saveRuleMutationOptions())
			.execute({projectId: 3, ruleId: 5, document})
		expect(result.webhook).toBeUndefined()
	})

	it('creates a new address and returns it without caching it', async () => {
		sdk.automationWebhookToken.mockResolvedValue({data: {token: 'new-token', url: 'https://vikunja.example/api/v2/automation/hooks/new-token'}})
		const client = new QueryClient()
		const result = await client.getMutationCache().build(client, rotateWebhookTokenMutationOptions())
			.execute({projectId: 3, ruleId: 5})
		expect(sdk.automationWebhookToken).toHaveBeenCalledWith({path: {project: 3, rule: 5}})
		expect(result).toEqual({token: 'new-token', url: 'https://vikunja.example/api/v2/automation/hooks/new-token'})
		expect(JSON.stringify(client.getQueryCache().getAll().map(query => query.state.data))).not.toContain('new-token')
	})

	it.each([
		['saving a rule', () => useSaveRuleMutation(), (m: ReturnType<typeof useSaveRuleMutation>) => m.mutateAsync({projectId: 3, ruleId: null, document}), () => sdk.automationsCreate.mockResolvedValue({data: saved})],
		['creating a new address', () => useRotateWebhookTokenMutation(), (m: ReturnType<typeof useRotateWebhookTokenMutation>) => m.mutateAsync({projectId: 3, ruleId: 5}), () => sdk.automationWebhookToken.mockResolvedValue({data: {token: 'secret-token', url: 'u'}})],
	])('does not keep the address of %s in the mutation cache', async (_name, hook, run, arrange) => {
		arrange()
		const client = new QueryClient()
		let mutation!: ReturnType<typeof hook>
		const wrapper = mount(defineComponent({
			setup() {
				mutation = hook()
				return () => h('div')
			},
		}), {global: {plugins: [[VueQueryPlugin, {queryClient: client}]]}})
		await (run as (m: unknown) => Promise<unknown>)(mutation)
		await new Promise(resolve => setTimeout(resolve))
		expect(client.getMutationCache().getAll()).toEqual([])
		wrapper.unmount()
	})
})
