// Project secrets of the automations. The API never returns a value: the list holds names and timestamps only, and a
// value passed to the mutations is not kept anywhere (no cache entry, no log).
import {queryOptions, useMutation} from '@tanstack/vue-query'

import {automationSecretsDelete, automationSecretsList, automationSecretsSet} from '@/client/generated'
import {contextMutationOptions} from './contextMutation'
import {fetchAllPages} from './fetchAllPages'
import {i18n} from '@/i18n'

export type AutomationSecretResponse = {
	id: number
	name: string
	created: string
	updated: string
	// The stored value cannot be decrypted any more (service.secret changed): it has to be set again.
	unreadable: boolean
}

export const automationSecretKeys = {
	list: (projectId: number) => ['automation', 'secrets', projectId] as const,
}

export function automationSecretsQuery(projectId: number) {
	return queryOptions({
		queryKey: automationSecretKeys.list(projectId),
		queryFn: async ({signal}): Promise<AutomationSecretResponse[]> => {
			const items = await fetchAllPages(async page => (await automationSecretsList({
				path: {project: projectId},
				query: {page},
				signal,
			})).data)
			return items
				.map(item => ({
					id: item.id ?? 0,
					name: item.name ?? '',
					created: item.created ?? '',
					updated: item.updated ?? '',
					unreadable: item.unreadable === true,
				}))
				.sort((a, b) => a.name.localeCompare(b.name))
		},
		enabled: projectId > 0,
	})
}

// Same rule as the server: letters, digits and underscores, not starting with a digit, at most 64 characters.
export const SECRET_NAME_PATTERN = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/

export function isValidSecretName(name: string): boolean {
	return SECRET_NAME_PATTERN.test(name)
}

export type SetSecretInput = {projectId: number, name: string, value: string, replacing: boolean}

export function setSecretMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, name, value}: SetSecretInput) => {
			await automationSecretsSet({path: {project: projectId, name}, body: {value}})
			return name
		},
		onSettled: ({projectId}, client) => client.invalidateQueries({queryKey: automationSecretKeys.list(projectId), exact: true}),
		successMessage: (_name, {replacing}) => i18n.global.t(replacing ? 'automation.secrets.replaced' : 'automation.secrets.created'),
		// The form shows the server's message under the field.
		toastError: () => false,
	})
}

export function deleteSecretMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, name}: {projectId: number, name: string}) => {
			await automationSecretsDelete({path: {project: projectId, name}})
		},
		onSuccess: (_data, {projectId, name}, client) => {
			client.setQueryData<AutomationSecretResponse[]>(automationSecretKeys.list(projectId),
				current => current?.filter(secret => secret.name !== name))
		},
		onSettled: ({projectId}, client) => client.invalidateQueries({queryKey: automationSecretKeys.list(projectId), exact: true}),
		successMessage: () => i18n.global.t('automation.secrets.deleted'),
	})
}

export function useSetSecretMutation() {
	return useMutation(setSecretMutationOptions())
}

export function useDeleteSecretMutation() {
	return useMutation(deleteSecretMutationOptions())
}
