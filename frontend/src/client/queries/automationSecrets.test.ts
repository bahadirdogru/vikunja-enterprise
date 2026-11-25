import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({automationSecretsDelete: vi.fn(), automationSecretsList: vi.fn(), automationSecretsSet: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => message)
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import {automationSecretKeys, automationSecretsQuery, deleteSecretMutationOptions, setSecretMutationOptions, type AutomationSecretResponse} from './automationSecrets'
import {ApiError} from '@/client/problem'

const run = (client: QueryClient, options: unknown, vars: unknown) => client.getMutationCache().build(client, options as never).execute(vars as never)
const secret = (name: string): AutomationSecretResponse => ({id: 1, name, created: '', updated: '', unreadable: false})

beforeEach(() => {
	Object.values(sdk).forEach(mock => mock.mockReset())
	message.error.mockReset()
	message.success.mockReset()
})

describe('secret names', () => {
	it('lists names sorted, never a value, and flags a secret that cannot be decrypted any more', async () => {
		sdk.automationSecretsList.mockResolvedValue({data: {items: [{id: 2, name: 'ZED', value: 'leak', unreadable: true}, {id: 1, name: 'API_KEY', created: 'c', updated: 'u'}], total_pages: 1}})
		const secrets = await new QueryClient().fetchQuery(automationSecretsQuery(3))
		expect(secrets.map(s => s.name)).toEqual(['API_KEY', 'ZED'])
		expect(secrets[1]).toEqual({id: 2, name: 'ZED', created: '', updated: '', unreadable: true})
		expect(JSON.stringify(secrets)).not.toContain('leak')
		expect(secrets[0]!.unreadable).toBe(false)
		expect(sdk.automationSecretsList.mock.calls[0]![0].path).toEqual({project: 3})
		expect(automationSecretsQuery(0).enabled).toBe(false)
	})
})

describe('setting a secret', () => {
	it('sends name in the path and the value in the body, says created or replaced, and keeps the value out of the cache', async () => {
		sdk.automationSecretsSet.mockResolvedValue({})
		const client = new QueryClient()
		await run(client, setSecretMutationOptions(), {projectId: 3, name: 'TOKEN', value: 's3cret', replacing: false})
		expect(sdk.automationSecretsSet).toHaveBeenCalledWith({path: {project: 3, name: 'TOKEN'}, body: {value: 's3cret'}})
		expect(message.success).toHaveBeenLastCalledWith({message: 'automation.secrets.created'})
		await run(client, setSecretMutationOptions(), {projectId: 3, name: 'TOKEN', value: 'n3w', replacing: true})
		expect(message.success).toHaveBeenLastCalledWith({message: 'automation.secrets.replaced'})
		expect(JSON.stringify(client.getMutationCache().getAll().map(m => m.state.data))).not.toContain('s3cret')
	})

	it('marks the list stale and does not toast a failure (the form shows it)', async () => {
		const client = new QueryClient()
		client.setQueryData(automationSecretKeys.list(3), [secret('A')])
		sdk.automationSecretsSet.mockRejectedValue(new ApiError({status: 400, detail: 'x'}))
		await expect(run(client, setSecretMutationOptions(), {projectId: 3, name: 'A', value: 'v', replacing: true})).rejects.toBeTruthy()
		expect(message.error).not.toHaveBeenCalled()
		expect(client.getQueryState(automationSecretKeys.list(3))?.isInvalidated).toBe(true)
	})
})

describe('deleting a secret', () => {
	it('removes the name from the cached list and confirms', async () => {
		sdk.automationSecretsDelete.mockResolvedValue({})
		const client = new QueryClient()
		client.setQueryData(automationSecretKeys.list(3), [secret('A'), secret('B')])
		await run(client, deleteSecretMutationOptions(), {projectId: 3, name: 'A'})
		expect(sdk.automationSecretsDelete).toHaveBeenCalledWith({path: {project: 3, name: 'A'}})
		expect(client.getQueryData<AutomationSecretResponse[]>(automationSecretKeys.list(3))!.map(s => s.name)).toEqual(['B'])
		expect(message.success).toHaveBeenLastCalledWith({message: 'automation.secrets.deleted'})
	})

	it('toasts a failed delete and keeps the list', async () => {
		sdk.automationSecretsDelete.mockRejectedValue(new ApiError({status: 500, detail: 'x'}))
		const client = new QueryClient()
		client.setQueryData(automationSecretKeys.list(3), [secret('A')])
		await expect(run(client, deleteSecretMutationOptions(), {projectId: 3, name: 'A'})).rejects.toBeTruthy()
		expect(message.error).toHaveBeenCalledTimes(1)
		expect(client.getQueryData<AutomationSecretResponse[]>(automationSecretKeys.list(3))).toHaveLength(1)
	})
})
