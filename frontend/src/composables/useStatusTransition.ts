import {ref} from 'vue'
import {useI18n} from 'vue-i18n'

import {useChangeTaskStatusMutation} from '@/client/queries/workflow'
import {isRequestContextAbort} from '@/client/requestContext'
import {outcomeOfProblem, readWorkflowProblem, type WorkflowStatusResponse} from '@/helpers/workflow'
import {error, getErrorText} from '@/message'

export type StatusDialogState = {
	toStatus: WorkflowStatusResponse
	missingFields: string[]
	commentRequired: boolean
	problem: string | null
}

/**
 * The one flow of asking for a workflow transition of a task: send it, and when it needs more
 * input (required fields, a comment) open the dialog for exactly that. A transition that needs an
 * approval answers with a request, which the mutation announces.
 */
export function useStatusTransition(taskId: () => number, onChanged: () => void = () => undefined) {
	const {t} = useI18n({useScope: 'global'})
	const change = useChangeTaskStatusMutation()
	const dialog = ref<StatusDialogState | null>(null)

	// Resolves true when the task moved, false when it stayed (a request was made, a dialog is open, or it failed).
	async function attempt(toStatus: WorkflowStatusResponse, comment = ''): Promise<boolean> {
		const id = taskId()
		let succeeded = false
		try {
			const result = await change.mutateAsync({taskId: id, toStatusId: toStatus.id, comment})
			// 202: a request was made, the task is still in its status.
			succeeded = !result.approval
			if (taskId() === id) {
				dialog.value = null
			}
		} catch (cause) {
			if (isRequestContextAbort(cause) || taskId() !== id) {
				return false
			}
			const outcome = outcomeOfProblem(readWorkflowProblem(cause))
			if (outcome.kind === 'dialog') {
				dialog.value = {
					toStatus,
					missingFields: outcome.missingFields,
					commentRequired: outcome.commentRequired,
					problem: dialog.value ? t('workflow.dialog.stillMissing') : null,
				}
			} else {
				dialog.value = null
				error({message: outcome.kind === 'message' ? t(outcome.messageKey) : getErrorText(cause)})
			}
		}
		onChanged()
		return succeeded
	}

	async function submitDialog(comment: string) {
		if (dialog.value) {
			await attempt(dialog.value.toStatus, comment)
		}
	}

	return {change, dialog, attempt, submitDialog}
}
