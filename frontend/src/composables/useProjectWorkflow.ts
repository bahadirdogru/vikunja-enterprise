import {computed, toValue, type MaybeRefOrGetter} from 'vue'
import {useQuery} from '@tanstack/vue-query'

import {workflowQuery} from '@/client/queries/workflow'

/**
 * The workflow of a project, if it has one. Pseudo projects (saved filters, favorites) have none;
 * the query is not even sent for them.
 */
export function useProjectWorkflow(projectId: MaybeRefOrGetter<number>) {
	const query = useQuery(computed(() => ({
		...workflowQuery(toValue(projectId)),
		enabled: toValue(projectId) > 0,
	})))
	const workflow = computed(() => toValue(projectId) > 0 ? query.data.value ?? null : null)
	const hasWorkflow = computed(() => workflow.value !== null)
	return {workflow, hasWorkflow, isLoading: query.isLoading}
}
