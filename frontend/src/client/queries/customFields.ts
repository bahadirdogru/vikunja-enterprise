import {queryOptions, useMutation, type QueryClient} from '@tanstack/vue-query'

import {
	customFieldsCreate,
	customFieldsDelete,
	customFieldsList,
	customFieldsUpdate,
	taskCustomFieldValuesList,
	taskCustomFieldValuesSet,
} from '@/client/generated'
import type {
	CustomField,
	CustomFieldOption,
	CustomFieldValue,
	CustomFieldWritable,
} from '@/client/generated'
import {contextMutationOptions} from './contextMutation'
import {fetchAllPages} from './fetchAllPages'
import {i18n} from '@/i18n'
import type {CustomFieldType} from '@/helpers/customFields'

export type CustomFieldOptionResponse = Omit<CustomFieldOption, 'id' | 'title' | 'color' | 'position'> &
	Required<Pick<CustomFieldOption, 'id' | 'title' | 'color' | 'position'>>

export type CustomFieldResponse = Omit<CustomField, 'id' | 'project_id' | 'title' | 'description' | 'type'
	| 'required' | 'position' | 'options'> &
	Required<Pick<CustomField, 'id' | 'project_id' | 'title' | 'description' | 'required' | 'position'>> & {
		type: CustomFieldType
		options: CustomFieldOptionResponse[]
	}

export type CustomFieldValueResponse = Omit<CustomFieldValue, 'custom_field_id' | 'task_id' | 'type'> &
	Required<Pick<CustomFieldValue, 'custom_field_id' | 'task_id' | 'type'>>

export function normalizeCustomField(field: CustomField): CustomFieldResponse {
	return {
		...field,
		id: field.id ?? 0,
		project_id: field.project_id ?? 0,
		title: field.title ?? '',
		description: field.description ?? '',
		type: field.type ?? 'text',
		required: field.required ?? false,
		position: field.position ?? 0,
		options: (field.options ?? []).map(option => ({
			...option,
			id: option.id ?? 0,
			title: option.title ?? '',
			color: option.color ?? '',
			position: option.position ?? 0,
		})),
	}
}

export function normalizeCustomFieldValue(value: CustomFieldValue): CustomFieldValueResponse {
	return {
		...value,
		custom_field_id: value.custom_field_id ?? 0,
		task_id: value.task_id ?? 0,
		type: value.type ?? '',
		value: value.value ?? null,
	}
}

export const customFieldKeys = {
	all: ['custom-fields'] as const,
	list: (projectId: number) => ['custom-fields', 'list', projectId] as const,
	allValues: ['custom-field-values'] as const,
	values: (taskId: number) => ['custom-field-values', taskId] as const,
}

export function customFieldsQuery(projectId: number) {
	return queryOptions({
		queryKey: customFieldKeys.list(projectId),
		queryFn: async ({signal}): Promise<CustomFieldResponse[]> => {
			const fields = await fetchAllPages(async page => (await customFieldsList({
				path: {project: projectId},
				query: {page},
				signal,
			})).data)
			return fields.map(normalizeCustomField)
		},
	})
}

export function customFieldValuesQuery(taskId: number) {
	return queryOptions({
		queryKey: customFieldKeys.values(taskId),
		queryFn: async ({signal}): Promise<CustomFieldValueResponse[]> => {
			const values = await fetchAllPages(async page => (await taskCustomFieldValuesList({
				path: {task: taskId},
				query: {page},
				signal,
			})).data)
			return values.map(normalizeCustomFieldValue)
		},
	})
}

// Deleting a field or an option removes stored values, so every cached task value list is stale.
function settleDefinitions(client: QueryClient, projectId: number) {
	return Promise.all([
		client.invalidateQueries({queryKey: customFieldKeys.list(projectId)}),
		client.invalidateQueries({queryKey: customFieldKeys.allValues}),
	])
}

export type CreateCustomFieldInput = {
	projectId: number
	field: CustomFieldWritable
}

export type UpdateCustomFieldInput = CreateCustomFieldInput & {
	fieldId: number
}

export type DeleteCustomFieldInput = {
	projectId: number
	fieldId: number
}

export function createCustomFieldMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, field}: CreateCustomFieldInput) =>
			normalizeCustomField((await customFieldsCreate({path: {project: projectId}, body: field})).data),
		onSuccess: (created, {projectId}, client) => {
			client.setQueryData<CustomFieldResponse[]>(customFieldKeys.list(projectId), current =>
				current ? [...current, created] : current,
			)
		},
		onSettled: ({projectId}, client) => settleDefinitions(client, projectId),
		successMessage: () => i18n.global.t('customFields.createSuccess'),
		// The form shows the API's validation message inline.
		toastError: () => false,
	})
}

export function updateCustomFieldMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, fieldId, field}: UpdateCustomFieldInput) =>
			normalizeCustomField((await customFieldsUpdate({
				path: {project: projectId, field: fieldId},
				body: field,
			})).data),
		onSuccess: (updated, {projectId}, client) => {
			client.setQueryData<CustomFieldResponse[]>(customFieldKeys.list(projectId), current =>
				current?.map(existing => existing.id === updated.id ? updated : existing),
			)
		},
		onSettled: ({projectId}, client) => settleDefinitions(client, projectId),
		successMessage: () => i18n.global.t('customFields.updateSuccess'),
		toastError: () => false,
	})
}

export function deleteCustomFieldMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, fieldId}: DeleteCustomFieldInput) => {
			await customFieldsDelete({path: {project: projectId, field: fieldId}})
		},
		optimistic: {
			queryKeys: ({projectId}) => [customFieldKeys.list(projectId)],
			update: ({projectId, fieldId}, client) => {
				client.setQueryData<CustomFieldResponse[]>(customFieldKeys.list(projectId), current =>
					current?.filter(existing => existing.id !== fieldId),
				)
			},
		},
		onSettled: ({projectId}, client) => settleDefinitions(client, projectId),
		successMessage: () => i18n.global.t('customFields.deleteSuccess'),
	})
}

export type SetCustomFieldValueInput = {
	taskId: number
	fieldId: number
	value: unknown
}

function withValue(
	current: CustomFieldValueResponse[] | undefined,
	{fieldId, taskId, value}: SetCustomFieldValueInput,
): CustomFieldValueResponse[] | undefined {
	return current?.map(existing => existing.custom_field_id === fieldId
		? {...existing, task_id: taskId, value}
		: existing,
	)
}

// PUT with null (or an empty array) clears the value, so one mutation covers set and clear.
export function setCustomFieldValueMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({taskId, fieldId, value}: SetCustomFieldValueInput) =>
			normalizeCustomFieldValue((await taskCustomFieldValuesSet({
				path: {task: taskId, field: fieldId},
				body: {value},
			})).data),
		optimistic: {
			queryKeys: ({taskId}) => [customFieldKeys.values(taskId)],
			update: (input, client) => {
				client.setQueryData<CustomFieldValueResponse[]>(
					customFieldKeys.values(input.taskId),
					current => withValue(current, input),
				)
			},
		},
		onSuccess: (saved, input, client) => {
			client.setQueryData<CustomFieldValueResponse[]>(
				customFieldKeys.values(input.taskId),
				current => withValue(current, {...input, value: saved.value}),
			)
		},
		onSettled: ({taskId}, client) => client.invalidateQueries({queryKey: customFieldKeys.values(taskId)}),
		successMessage: () => i18n.global.t('customFields.value.saved'),
	})
}

export function useCreateCustomFieldMutation() {
	return useMutation(createCustomFieldMutationOptions())
}

export function useUpdateCustomFieldMutation() {
	return useMutation(updateCustomFieldMutationOptions())
}

export function useDeleteCustomFieldMutation() {
	return useMutation(deleteCustomFieldMutationOptions())
}

export function useSetCustomFieldValueMutation() {
	return useMutation(setCustomFieldValueMutationOptions())
}
