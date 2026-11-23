import {ref} from 'vue'

import {i18n} from '@/i18n'
import {readApprovalProblem} from '@/helpers/approvals'
import {error, markErrorAnswered} from '@/message'

export type ApprovalRequestTarget = {
	taskId: number
	// 0 when the caller did not know it; the host looks it up.
	projectId: number
	toStatusId: number
}

// What the host component picks up: the status change the user asked to request.
export const approvalRequestTarget = ref<ApprovalRequestTarget | null>(null)

/**
 * A plain update (the done checkbox) was refused because the workflow transition needs an approval
 * (409, 24007). The server changed nothing; this tells the user why and offers the next step: a
 * request for the approval, or the task with the request that is already waiting.
 *
 * Returns whether the failure was this one, so callers can leave the toast of other errors alone.
 */
export function handleApprovalRequired(cause: unknown, task: {id?: number, project_id?: number}): boolean {
	const problem = readApprovalProblem(cause)
	if (problem.kind !== 'doneRequiresApproval') {
		return false
	}
	markErrorAnswered(cause)
	const t = i18n.global.t
	const taskId = problem.taskId || task.id || 0
	const reopening = problem.toStatusCategory !== '' && problem.toStatusCategory !== 'done' && problem.toStatusCategory !== 'cancelled'
	const status = problem.toStatusTitle

	if (problem.pendingApprovalId !== 0) {
		error({message: status === ''
			? t('approvals.doneBlocked.pendingGeneric')
			: t('approvals.doneBlocked.pending', {status})}, [{
			title: t('approvals.doneBlocked.open'),
			callback: () => {
				import('@/router').then(({default: router}) => router.push({name: 'task.detail', params: {id: taskId}}))
			},
		}])
		return true
	}

	let message = t('approvals.doneBlocked.needsGeneric')
	if (status !== '') {
		message = t(reopening ? 'approvals.doneBlocked.needsReopen' : 'approvals.doneBlocked.needsDone', {status})
	}
	error({message}, problem.toStatusId === 0 || !problem.requestable ? [] : [{
		title: t('approvals.doneBlocked.request'),
		callback: () => {
			approvalRequestTarget.value = {
				taskId,
				projectId: task.project_id ?? 0,
				toStatusId: problem.toStatusId,
			}
		},
	}])
	return true
}
