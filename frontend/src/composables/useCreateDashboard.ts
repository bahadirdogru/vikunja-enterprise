import {useI18n} from 'vue-i18n'

import {useCreateDashboardMutation} from '@/client/queries/dashboards'
import {ensureReportOptions} from '@/client/queries/reports'
import {defaultWidgets} from '@/helpers/dashboards'

// Creates a dashboard with the default widgets; a project dashboard picks the status donut only when the project has
// a workflow, which the report options of its scope say.
export function useCreateDashboard() {
	const {t} = useI18n({useScope: 'global'})
	const mutation = useCreateDashboardMutation()

	async function create(input: {title: string, projectId: number}) {
		let hasWorkflow = false
		try {
			hasWorkflow = (await ensureReportOptions(input.projectId)).has_workflow
		} catch {
			// Without the options the dashboard still works, it just starts with the done donut.
		}
		return mutation.mutateAsync({
			title: input.title,
			project_id: input.projectId,
			widgets: defaultWidgets({
				projectId: input.projectId,
				hasWorkflow,
				titles: {
					open: t('dashboards.defaults.open'),
					status: t('dashboards.defaults.status'),
					done: t('dashboards.defaults.done'),
					createdVsDone: t('dashboards.defaults.createdVsDone'),
					assignee: t('dashboards.defaults.assignee'),
					overdue: t('dashboards.defaults.overdue'),
				},
			}),
		})
	}

	return {create, isPending: mutation.isPending}
}
