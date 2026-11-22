import {computed, ref, toValue, type MaybeRefOrGetter} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQueryClient} from '@tanstack/vue-query'

import type {Task} from '@/client/generated'
import {kanbanKeys} from '@/client/queries/kanban'
import {useMoveTaskToStatusColumnMutation} from '@/client/queries/taskMutations'
import {useChangeTaskStatusMutation} from '@/client/queries/workflow'
import {isRequestContextAbort} from '@/client/requestContext'
import {
	outcomeOfProblem,
	readWorkflowProblem,
	statusIdOfBucket,
	type WorkflowResponse,
	type WorkflowStatusResponse,
} from '@/helpers/workflow'
import {error, getErrorText, success} from '@/message'

export type StatusColumnDialog = {
	taskId: number
	toStatus: WorkflowStatusResponse
	missingFields: string[]
	commentRequired: boolean
	problem: string | null
}

/**
 * Moving a card into another column of a status mode board performs the workflow transition.
 * A rejected move leaves the card where it was; what is missing is asked for in the status dialog
 * (the same one the task detail uses), anything else ends with a message.
 */
export function useStatusColumnMove(
	project: MaybeRefOrGetter<number>,
	view: MaybeRefOrGetter<number>,
	workflow: MaybeRefOrGetter<WorkflowResponse | null | undefined>,
) {
	const {t} = useI18n({useScope: 'global'})
	const client = useQueryClient()
	const moveMutation = useMoveTaskToStatusColumnMutation()
	const changeStatus = useChangeTaskStatusMutation()

	const dialog = ref<StatusColumnDialog | null>(null)
	const busy = computed(() => changeStatus.isPending.value)

	function statusOf(bucketId: number): WorkflowStatusResponse | undefined {
		const id = statusIdOfBucket(bucketId)
		return toValue(workflow)?.statuses.find(status => status.id === id)
	}

	// The optimistic move is rolled back by the mutation on failure, the board is re-read to be sure.
	function refreshBoard() {
		return client.invalidateQueries({queryKey: kanbanKeys.view(toValue(project), toValue(view)), refetchType: 'active'})
	}

	function report(cause: unknown, taskId: number, toStatus: WorkflowStatusResponse | undefined, stillOpen: boolean) {
		const outcome = outcomeOfProblem(readWorkflowProblem(cause))
		if (outcome.kind === 'dialog' && toStatus) {
			dialog.value = {
				taskId,
				toStatus,
				missingFields: outcome.missingFields,
				commentRequired: outcome.commentRequired,
				problem: stillOpen ? t('workflow.dialog.stillMissing') : null,
			}
			return
		}
		dialog.value = null
		error(outcome.kind === 'message' ? {message: t(outcome.messageKey)} : {message: getErrorText(cause)})
	}

	/** Resolves whether the task now is in the column (false: it stayed, a rejection or an approval request). */
	async function move(task: Pick<Task, 'id'> & Task, bucketId: number): Promise<boolean> {
		try {
			const result = await moveMutation.mutateAsync({project: toValue(project), view: toValue(view), bucket: bucketId, task})
			if (result.approval) {
				// 202: the card stays, a request was made.
				success({message: t('approvals.requested')})
				void refreshBoard()
				return false
			}
			return true
		} catch (cause) {
			if (!isRequestContextAbort(cause)) {
				report(cause, task.id!, statusOf(bucketId), false)
				void refreshBoard()
			}
			return false
		}
	}

	/**
	 * A task that was created in a column whose status it could not enter stays in the initial column.
	 * Missing fields are asked for right away, any other rejection is the error the caller shows.
	 * Resolves whether the rejection was handled here.
	 */
	function reportRejectedCreate(cause: unknown, bucketId: number): boolean {
		const problem = readWorkflowProblem(cause)
		const toStatus = statusOf(bucketId)
		if (problem.kind !== 'requirements' || problem.taskId === 0 || !toStatus) {
			return false
		}
		dialog.value = {
			taskId: problem.taskId,
			toStatus,
			missingFields: problem.missingFields,
			commentRequired: problem.commentRequired,
			problem: null,
		}
		void refreshBoard()
		return true
	}

	// The dialog saves the fields it asked for before this runs, so the change finds them set.
	async function submitDialog(comment: string) {
		const current = dialog.value
		if (!current) return
		try {
			await changeStatus.mutateAsync({taskId: current.taskId, toStatusId: current.toStatus.id, comment})
			dialog.value = null
		} catch (cause) {
			if (!isRequestContextAbort(cause)) {
				report(cause, current.taskId, current.toStatus, true)
			}
		}
		await refreshBoard()
	}

	function closeDialog() {
		dialog.value = null
	}

	return {dialog, busy, move, submitDialog, closeDialog, statusOf, refreshBoard, reportRejectedCreate}
}
