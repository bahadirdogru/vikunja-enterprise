import {computed, type MaybeRefOrGetter, toValue} from 'vue'
import {useStorage} from '@vueuse/core'
import {useQuery} from '@tanstack/vue-query'

import {customFieldsQuery} from '@/client/queries/customFields'
import {
	type CustomFieldSelection,
	resolveSelectedFields,
	selectionOf,
	withFieldSelected,
} from '@/helpers/customFieldDisplay'

/**
 * Persists which custom fields of a project are shown (as table columns or kanban card fields).
 * The selection lives in localStorage under `storageKey`, keyed by `scope` (a project or view id),
 * so every project or view keeps its own choice.
 */
export function useCustomFieldSelection(
	storageKey: string,
	scope: MaybeRefOrGetter<number>,
	projectId: MaybeRefOrGetter<number>,
) {
	const stored = useStorage<CustomFieldSelection>(storageKey, {})

	const fieldsQuery = useQuery(computed(() => ({
		...customFieldsQuery(toValue(projectId)),
		// Saved filters have negative ids and no fields of their own.
		enabled: toValue(projectId) > 0,
	})))
	const fields = computed(() => toValue(projectId) > 0 ? fieldsQuery.data.value ?? [] : [])

	const selectedIds = computed(() => selectionOf(stored.value, toValue(scope)))
	const selectedFields = computed(() => resolveSelectedFields(fields.value, selectedIds.value))

	function isSelected(fieldId: number) {
		return selectedIds.value.includes(fieldId)
	}

	function setSelected(fieldId: number, selected: boolean) {
		stored.value = withFieldSelected(stored.value, toValue(scope), fieldId, selected)
	}

	return {fields, selectedIds, selectedFields, isSelected, setSelected}
}
