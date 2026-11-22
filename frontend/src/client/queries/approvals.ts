import {queryOptions, useMutation} from '@tanstack/vue-query'

import {
	approvalsCancel,
	approvalsDecide,
	approvalsList,
	taskApprovalsCreate,
	taskApprovalsList,
} from '@/client/generated'
import {contextMutationOptions} from './contextMutation'
import {commentKeys} from './comments'
import {fetchAllPages} from './fetchAllPages'
import {kanbanKeys} from './kanban'
import {approvalKeys} from './approvalKeys'
import {toPaginated, type Paginated} from './pagination'
import {invalidateTaskMembership} from './taskCache'
import {workflowKeys} from './workflow'
import {i18n} from '@/i18n'
import {
	normalizeApproval,
	type ApprovalDecisionValue,
	type ApprovalMode,
	type ApprovalResponse,
	type ApprovalStatus,
} from '@/helpers/approvals'

export {approvalKeys}

export const APPROVALS_PER_PAGE = 20

export type ApprovalListParams = {
	status?: ApprovalStatus
	role?: 'approver' | 'requester'
	projectId?: number
	page?: number
}

export type ApprovalPage = Paginated<ApprovalResponse>

async function fetchApprovalPage(params: ApprovalListParams, page: number, signal?: AbortSignal): Promise<ApprovalPage> {
	const {data} = await approvalsList({
		query: {
			...(params.status ? {status: params.status} : {}),
			...(params.role ? {role: params.role} : {}),
			...(params.projectId ? {project_id: params.projectId} : {}),
			page,
			per_page: APPROVALS_PER_PAGE,
		},
		signal,
	})
	const result = toPaginated(data, page)
	return {...result, items: result.items.map(normalizeApproval)}
}

export function approvalsListQuery(params: ApprovalListParams) {
	const page = params.page ?? 1
	return queryOptions({
		queryKey: approvalKeys.list({...params, page}),
		queryFn: ({signal}) => fetchApprovalPage(params, page, signal),
		staleTime: 15_000,
	})
}

// How many requests wait for the user. The list endpoint reports the total, so one item is enough.
export function approvalCountQuery() {
	return queryOptions({
		queryKey: approvalKeys.count,
		queryFn: async ({signal}): Promise<number> => {
			const {data} = await approvalsList({query: {status: 'pending', role: 'approver', page: 1, per_page: 1}, signal})
			return data.total ?? data.items?.length ?? 0
		},
		staleTime: 30_000,
		refetchOnWindowFocus: true,
	})
}

// Every request of a task, newest first: the banner needs the pending ones, the history section the others.
export function taskApprovalsQuery(taskId: number) {
	return queryOptions({
		queryKey: approvalKeys.task(taskId),
		queryFn: async ({signal}): Promise<ApprovalResponse[]> => {
			const items = await fetchAllPages(async page => (await taskApprovalsList({
				path: {task: taskId},
				query: {page},
				signal,
			})).data)
			return items.map(normalizeApproval)
		},
		enabled: taskId > 0,
		staleTime: 15_000,
	})
}

// A decision or cancellation can change the task, its status, its history and the board markers.
function invalidateAfterApproval(client: Parameters<typeof invalidateTaskMembership>[0], taskId: number) {
	return Promise.all([
		client.invalidateQueries({queryKey: approvalKeys.all}),
		client.invalidateQueries({queryKey: workflowKeys.taskStatus(taskId)}),
		client.invalidateQueries({queryKey: workflowKeys.history(taskId)}),
		client.invalidateQueries({queryKey: commentKeys.task(taskId)}),
		client.invalidateQueries({queryKey: kanbanKeys.all, refetchType: 'active'}),
		invalidateTaskMembership(client, taskId, 'active'),
	])
}

export type DecideInput = {
	requestId: number
	taskId: number
	decision: ApprovalDecisionValue
	comment: string
}

export function decideApprovalMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({requestId, decision, comment}: DecideInput) =>
			normalizeApproval((await approvalsDecide({
				path: {request: requestId},
				body: {decision, comment},
			})).data.request ?? {}),
		onSettled: ({taskId}, client) => invalidateAfterApproval(client, taskId),
		successMessage: (request, {decision}) => {
			if (request.status === 'cancelled') return i18n.global.t('approvals.decision.cancelledMeanwhile')
			return decision === 'approved'
				? i18n.global.t('approvals.decision.approvedSuccess')
				: i18n.global.t('approvals.decision.rejectedSuccess')
		},
		// The dialog maps the typed errors (24005 and the others) itself.
		toastError: () => false,
	})
}

export type CancelInput = {
	requestId: number
	taskId: number
	message?: string
}

export function cancelApprovalMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({requestId, message}: CancelInput) =>
			normalizeApproval((await approvalsCancel({
				path: {request: requestId},
				body: {message: message ?? ''},
			})).data.request ?? {}),
		onSettled: ({taskId}, client) => invalidateAfterApproval(client, taskId),
		successMessage: () => i18n.global.t('approvals.cancel.success'),
	})
}

export type CreateAdHocInput = {
	taskId: number
	title: string
	description: string
	approverIds: number[]
	mode: ApprovalMode
	due: string | null
}

export function createAdHocApprovalMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({taskId, title, description, approverIds, mode, due}: CreateAdHocInput) =>
			normalizeApproval((await taskApprovalsCreate({
				path: {task: taskId},
				body: {title, description, approver_ids: approverIds, mode, due},
			})).data),
		onSettled: ({taskId}, client) => Promise.all([
			client.invalidateQueries({queryKey: approvalKeys.all}),
			client.invalidateQueries({queryKey: kanbanKeys.all, refetchType: 'active'}),
			invalidateTaskMembership(client, taskId, 'active'),
		]),
		successMessage: () => i18n.global.t('approvals.requested'),
		// The dialog maps the typed errors itself.
		toastError: () => false,
	})
}

export function useDecideApprovalMutation() {
	return useMutation(decideApprovalMutationOptions())
}

export function useCancelApprovalMutation() {
	return useMutation(cancelApprovalMutationOptions())
}

export function useCreateAdHocApprovalMutation() {
	return useMutation(createAdHocApprovalMutationOptions())
}
