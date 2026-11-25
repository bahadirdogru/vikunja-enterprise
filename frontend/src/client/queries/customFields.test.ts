import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	customFieldsCreate: vi.fn(), customFieldsDelete: vi.fn(), customFieldsList: vi.fn(), customFieldsUpdate: vi.fn(),
	taskCustomFieldValuesList: vi.fn(), taskCustomFieldValuesSet: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => message)
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import {
	createCustomFieldMutationOptions, customFieldKeys, customFieldsQuery, customFieldValuesQuery, deleteCustomFieldMutationOptions,
	normalizeCustomField, normalizeCustomFieldValue, setCustomFieldValueMutationOptions, updateCustomFieldMutationOptions,
	type CustomFieldResponse, type CustomFieldValueResponse,
} from './customFields'
import {ApiError} from '@/client/problem'

const run = (client: QueryClient, options: unknown, vars: unknown) => client.getMutationCache().build(client, options as never).execute(vars as never)
const field = (id: number, title = `F${id}`): CustomFieldResponse => ({id, project_id: 3, title, description: '', type: 'text', required: false, position: id, options: []})
const value = (id: number, v: unknown): CustomFieldValueResponse => ({custom_field_id: id, task_id: 7, type: 'text', value: v})

beforeEach(() => {
	Object.values(sdk).forEach(mock => mock.mockReset())
	message.error.mockReset()
	message.success.mockReset()
})

describe('normalisation', () => {
	it('fills defaults of a field and its options and keeps unknown members (config)', () => {
		expect(normalizeCustomField({config: {min: 1}, options: [{id: 2}]} as never)).toEqual({
			id: 0, project_id: 0, title: '', description: '', type: 'text', required: false, position: 0, config: {min: 1},
			options: [{id: 2, title: '', color: '', position: 0}],
		})
	})

	it('keeps the type, the required flag and the option colours as sent', () => {
		const f = normalizeCustomField({id: 1, type: 'select', required: true, options: [{id: 5, title: 'A', color: '#fff', position: 2}]} as never)
		expect(f).toMatchObject({type: 'select', required: true, options: [{id: 5, title: 'A', color: '#fff', position: 2}]})
	})

	it('keeps every kind of stored value and turns a missing value into null', () => {
		expect(normalizeCustomFieldValue({custom_field_id: 1, task_id: 7, type: 'multi_select', value: [1, 2]} as never).value).toEqual([1, 2])
		expect(normalizeCustomFieldValue({custom_field_id: 1, value: false} as never)).toMatchObject({value: false, task_id: 0, type: ''})
		expect(normalizeCustomFieldValue({} as never)).toEqual({custom_field_id: 0, task_id: 0, type: '', value: null})
	})
})

describe('queries', () => {
	it('reads the fields of a project through all pages', async () => {
		sdk.customFieldsList
			.mockResolvedValueOnce({data: {items: [{id: 1}], total_pages: 2}})
			.mockResolvedValueOnce({data: {items: [{id: 2}], total_pages: 2}})
		const fields = await new QueryClient().fetchQuery(customFieldsQuery(3))
		expect(fields.map(f => f.id)).toEqual([1, 2])
		expect(sdk.customFieldsList.mock.calls.map(c => [c[0].path, c[0].query])).toEqual([[{project: 3}, {page: 1}], [{project: 3}, {page: 2}]])
	})

	it('reads the values of a task', async () => {
		sdk.taskCustomFieldValuesList.mockResolvedValue({data: {items: [{custom_field_id: 1, value: 'x'}], total_pages: 1}})
		const values = await new QueryClient().fetchQuery(customFieldValuesQuery(7))
		expect(values).toEqual([{custom_field_id: 1, task_id: 0, type: '', value: 'x'}])
		expect(sdk.taskCustomFieldValuesList.mock.calls[0]![0].path).toEqual({task: 7})
	})
})

describe('field definitions', () => {
	it('appends a created field to a cached list, never creating a list from nothing, and invalidates fields and every value list', async () => {
		sdk.customFieldsCreate.mockResolvedValue({data: {id: 9, title: 'New'}})
		const client = new QueryClient()
		const spy = vi.spyOn(client, 'invalidateQueries')
		client.setQueryData(customFieldKeys.list(3), [field(1)])
		await run(client, createCustomFieldMutationOptions(), {projectId: 3, field: {title: 'New', type: 'text'}})
		expect(sdk.customFieldsCreate.mock.calls[0]![0]).toEqual({path: {project: 3}, body: {title: 'New', type: 'text'}})
		expect(client.getQueryData<CustomFieldResponse[]>(customFieldKeys.list(3))!.map(f => f.id)).toEqual([1, 9])
		expect(spy).toHaveBeenCalledWith({queryKey: customFieldKeys.list(3)})
		expect(spy).toHaveBeenCalledWith({queryKey: customFieldKeys.allValues})

		await run(client, createCustomFieldMutationOptions(), {projectId: 4, field: {title: 'x', type: 'text'}})
		expect(client.getQueryData(customFieldKeys.list(4))).toBeUndefined()
	})

	it('does not toast a rejected definition: the form shows the message', async () => {
		sdk.customFieldsCreate.mockRejectedValue(new ApiError({status: 400, code: 20006, detail: 'limit'}))
		await expect(run(new QueryClient(), createCustomFieldMutationOptions(), {projectId: 3, field: {}})).rejects.toBeTruthy()
		expect(message.error).not.toHaveBeenCalled()
	})

	it('replaces only the updated field in the cached list', async () => {
		sdk.customFieldsUpdate.mockResolvedValue({data: {id: 2, title: 'Renamed'}})
		const client = new QueryClient()
		client.setQueryData(customFieldKeys.list(3), [field(1), field(2)])
		await run(client, updateCustomFieldMutationOptions(), {projectId: 3, fieldId: 2, field: {title: 'Renamed'}})
		expect(sdk.customFieldsUpdate.mock.calls[0]![0].path).toEqual({project: 3, field: 2})
		expect(client.getQueryData<CustomFieldResponse[]>(customFieldKeys.list(3))!.map(f => f.title)).toEqual(['F1', 'Renamed'])
	})

	it('removes a deleted field at once and puts the list back when the delete fails', async () => {
		const client = new QueryClient()
		client.setQueryData(customFieldKeys.list(3), [field(1), field(2)])
		let inRequest: number[] = []
		sdk.customFieldsDelete.mockImplementation(async () => {
			inRequest = client.getQueryData<CustomFieldResponse[]>(customFieldKeys.list(3))!.map(f => f.id)
			throw new ApiError({status: 500, detail: 'x'})
		})
		await expect(run(client, deleteCustomFieldMutationOptions(), {projectId: 3, fieldId: 1})).rejects.toBeTruthy()
		expect(inRequest).toEqual([2])
		expect(client.getQueryData<CustomFieldResponse[]>(customFieldKeys.list(3))!.map(f => f.id)).toEqual([1, 2])
		expect(message.error).toHaveBeenCalledTimes(1)
	})
})

describe('values', () => {
	it('shows the new value while the request is running, then the value the server stored', async () => {
		const client = new QueryClient()
		client.setQueryData(customFieldKeys.values(7), [value(1, 'old'), value(2, 'keep')])
		let inRequest: unknown
		sdk.taskCustomFieldValuesSet.mockImplementation(async () => {
			inRequest = client.getQueryData<CustomFieldValueResponse[]>(customFieldKeys.values(7))![0]!.value
			return {data: {custom_field_id: 1, task_id: 7, type: 'text', value: 'normalised'}}
		})
		await run(client, setCustomFieldValueMutationOptions(), {taskId: 7, fieldId: 1, value: 'typed'})
		expect(sdk.taskCustomFieldValuesSet.mock.calls[0]![0]).toEqual({path: {task: 7, field: 1}, body: {value: 'typed'}})
		expect(inRequest).toBe('typed')
		const after = client.getQueryData<CustomFieldValueResponse[]>(customFieldKeys.values(7))!
		expect(after.map(v => v.value)).toEqual(['normalised', 'keep'])
		expect(message.success).toHaveBeenCalledWith({message: 'customFields.value.saved'})
	})

	it('rolls the value back and toasts when the server refuses it', async () => {
		const client = new QueryClient()
		client.setQueryData(customFieldKeys.values(7), [value(1, 'old')])
		sdk.taskCustomFieldValuesSet.mockRejectedValue(new ApiError({status: 400, code: 20008, detail: 'long'}))
		await expect(run(client, setCustomFieldValueMutationOptions(), {taskId: 7, fieldId: 1, value: 'x'})).rejects.toBeTruthy()
		expect(client.getQueryData<CustomFieldValueResponse[]>(customFieldKeys.values(7))![0]!.value).toBe('old')
		expect(message.error).toHaveBeenCalledTimes(1)
	})

	it('sends null to clear a value', async () => {
		sdk.taskCustomFieldValuesSet.mockResolvedValue({data: {custom_field_id: 1, task_id: 7, type: 'text', value: null}})
		await run(new QueryClient(), setCustomFieldValueMutationOptions(), {taskId: 7, fieldId: 1, value: null})
		expect(sdk.taskCustomFieldValuesSet.mock.calls[0]![0].body).toEqual({value: null})
	})
})
