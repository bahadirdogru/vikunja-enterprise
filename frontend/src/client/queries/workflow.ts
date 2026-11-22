import {queryOptions, useMutation} from '@tanstack/vue-query'

import {
	taskStatusChange,
	taskStatusHistoryList,
	taskCustomFieldValuesSet,
	taskStatusRead,
	teamsList,
	workflowDelete,
	workflowRead,
	workflowReplace,
} from '@/client/generated'
import type {
	AvailableTransition,
	TaskStatusHistory,
	TaskWorkflowStatus,
	User,
	Workflow,
	WorkflowStatus,
	WorkflowTransition,
	WorkflowWritable,
} from '@/client/generated'
import {contextMutationOptions} from './contextMutation'
import {customFieldKeys} from './customFields'
import {fetchAllPages} from './fetchAllPages'
import {invalidateTaskMembership} from './taskCache'
import {i18n} from '@/i18n'
import {approvalKeys} from './approvalKeys'
import {normalizeApproval, normalizeTransitionApproval, type ApprovalResponse} from '@/helpers/approvals'
import {
	WORKFLOW_ERROR_CODE,
	type AvailableTransitionResponse,
	type WorkflowCategory,
	type WorkflowResponse,
	type WorkflowStatusResponse,
	type WorkflowTransitionResponse,
} from '@/helpers/workflow'

export type TaskStatusResponse = {
	task_id: number
	workflow_id: number
	status: WorkflowStatusResponse | null
	entered: string | null
	transitions: AvailableTransitionResponse[]
	pending_approvals: ApprovalResponse[]
}

export type StatusHistoryResponse = {
	id: number
	from_status_id: number | null
	to_status_id: number
	from_title: string
	to_title: string
	doer: User | null
	comment: string
	created: string
	// Set when the change was made by an approved request.
	approval_request_id: number | null
}

export function normalizeWorkflowStatus(status: WorkflowStatus): WorkflowStatusResponse {
	return {
		id: status.id ?? 0,
		title: status.title ?? '',
		color: status.color ?? '',
		category: (status.category ?? 'open') as WorkflowCategory,
		position: status.position ?? 0,
		is_initial: status.is_initial ?? false,
		archived: status.archived ?? false,
	}
}

function normalizeWorkflowTransition(transition: WorkflowTransition): WorkflowTransitionResponse {
	return {
		id: transition.id ?? 0,
		from_status_id: transition.from_status_id ?? 0,
		to_status_id: transition.to_status_id ?? 0,
		title: transition.title ?? '',
		min_permission: transition.min_permission ?? 'write',
		require_comment: transition.require_comment ?? false,
		required_fields: transition.required_fields ?? [],
		team_ids: transition.team_ids ?? [],
		requires_approval: transition.requires_approval ?? false,
		approval: transition.requires_approval ? normalizeTransitionApproval(transition.approval) : null,
	}
}

export function normalizeWorkflow(workflow: Workflow): WorkflowResponse {
	return {
		id: workflow.id ?? 0,
		project_id: workflow.project_id ?? 0,
		restrict_transitions: workflow.restrict_transitions ?? false,
		statuses: (workflow.statuses ?? []).map(normalizeWorkflowStatus),
		transitions: (workflow.transitions ?? []).map(normalizeWorkflowTransition),
	}
}

function normalizeAvailableTransition(transition: AvailableTransition): AvailableTransitionResponse {
	return {
		to_status: normalizeWorkflowStatus(transition.to_status ?? {}),
		title: transition.title ?? '',
		require_comment: transition.require_comment ?? false,
		missing_fields: transition.missing_fields ?? [],
		requires_approval: transition.requires_approval ?? false,
		approval_mode: transition.approval_mode ?? '',
		approvers: transition.approvers ?? [],
	}
}

export function normalizeTaskStatus(status: TaskWorkflowStatus): TaskStatusResponse {
	return {
		task_id: status.task_id ?? 0,
		workflow_id: status.workflow_id ?? 0,
		status: status.status ? normalizeWorkflowStatus(status.status) : null,
		entered: status.entered ?? null,
		transitions: (status.transitions ?? []).map(normalizeAvailableTransition),
		pending_approvals: (status.pending_approvals ?? []).map(normalizeApproval),
	}
}

function normalizeHistory(entry: TaskStatusHistory): StatusHistoryResponse {
	return {
		id: entry.id ?? 0,
		from_status_id: entry.from_status_id ?? null,
		to_status_id: entry.to_status_id ?? 0,
		from_title: entry.from_title ?? '',
		to_title: entry.to_title ?? '',
		doer: entry.doer ?? null,
		comment: entry.comment ?? '',
		created: entry.created ?? '',
		approval_request_id: entry.approval_request_id ?? null,
	}
}

function isNoWorkflowError(cause: unknown): boolean {
	return typeof cause === 'object'
		&& cause !== null
		&& (cause as {code?: unknown}).code === WORKFLOW_ERROR_CODE.noWorkflow
}

export const workflowKeys = {
	all: ['workflow'] as const,
	project: (projectId: number) => ['workflow', 'project', projectId] as const,
	taskStatuses: ['workflow', 'task-status'] as const,
	taskStatus: (taskId: number) => ['workflow', 'task-status', taskId] as const,
	histories: ['workflow', 'history'] as const,
	history: (taskId: number) => ['workflow', 'history', taskId] as const,
	teams: ['workflow', 'teams'] as const,
}

// A project without a workflow answers 404 code 21001; that is a normal state, not a failure.
export function workflowQuery(projectId: number) {
	return queryOptions({
		queryKey: workflowKeys.project(projectId),
		queryFn: async ({signal}): Promise<WorkflowResponse | null> => {
			try {
				return normalizeWorkflow((await workflowRead({path: {project: projectId}, signal})).data)
			} catch (cause) {
				if (isNoWorkflowError(cause)) return null
				throw cause
			}
		},
		enabled: projectId > 0,
	})
}

export function taskStatusQuery(taskId: number) {
	return queryOptions({
		queryKey: workflowKeys.taskStatus(taskId),
		queryFn: async ({signal}): Promise<TaskStatusResponse | null> => {
			try {
				return normalizeTaskStatus((await taskStatusRead({path: {task: taskId}, signal})).data)
			} catch (cause) {
				if (isNoWorkflowError(cause)) return null
				throw cause
			}
		},
		enabled: taskId > 0,
	})
}

export function taskStatusHistoryQuery(taskId: number) {
	return queryOptions({
		queryKey: workflowKeys.history(taskId),
		queryFn: async ({signal}): Promise<StatusHistoryResponse[]> => {
			const entries = await fetchAllPages(async page => (await taskStatusHistoryList({
				path: {task: taskId},
				query: {page},
				signal,
			})).data)
			return entries.map(normalizeHistory)
		},
		enabled: taskId > 0,
	})
}

export function workflowTeamsQuery() {
	return queryOptions({
		queryKey: workflowKeys.teams,
		queryFn: async ({signal}) => fetchAllPages(async page => (await teamsList({query: {page}, signal})).data),
	})
}

export type ReplaceWorkflowInput = {
	projectId: number
	document: WorkflowWritable
}

export function replaceWorkflowMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, document}: ReplaceWorkflowInput) =>
			normalizeWorkflow((await workflowReplace({path: {project: projectId}, body: document})).data),
		onSuccess: (saved, {projectId}, client) => {
			client.setQueryData<WorkflowResponse | null>(workflowKeys.project(projectId), saved)
		},
		// Removed statuses move their tasks, so every cached task status is stale.
		onSettled: ({projectId}, client) => Promise.all([
			client.invalidateQueries({queryKey: workflowKeys.project(projectId), exact: true}),
			client.invalidateQueries({queryKey: workflowKeys.taskStatuses}),
			client.invalidateQueries({queryKey: workflowKeys.histories}),
		]),
		successMessage: () => i18n.global.t('workflow.saveSuccess'),
		// The editor maps validation and move-tasks errors itself.
		toastError: () => false,
	})
}

export function deleteWorkflowMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId}: {projectId: number}) => {
			await workflowDelete({path: {project: projectId}})
		},
		onSuccess: (_data, {projectId}, client) => {
			client.setQueryData<WorkflowResponse | null>(workflowKeys.project(projectId), null)
		},
		onSettled: (_input, client) => client.invalidateQueries({queryKey: workflowKeys.all}),
		successMessage: () => i18n.global.t('workflow.disableSuccess'),
	})
}

export type ChangeTaskStatusInput = {
	taskId: number
	toStatusId: number
	comment?: string
}

export function changeTaskStatusMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({taskId, toStatusId, comment}: ChangeTaskStatusInput) =>
			(await taskStatusChange({
				path: {task: taskId},
				body: {to_status_id: toStatusId, comment: comment ?? ''},
			})).data,
		onSettled: ({taskId}, client) => Promise.all([
			client.invalidateQueries({queryKey: workflowKeys.taskStatus(taskId)}),
			client.invalidateQueries({queryKey: workflowKeys.history(taskId)}),
			// A change that needed approval answers 202 with a new pending request.
			client.invalidateQueries({queryKey: approvalKeys.all}),
			// The done flag may have changed.
			invalidateTaskMembership(client, taskId, 'active'),
		]),
		// 202: the status did not change, a request was made.
		successMessage: data => data.approval
			? i18n.global.t('approvals.requested')
			: i18n.global.t('workflow.task.changed', {status: data.status?.title ?? ''}),
		// Callers turn 400/403/422 into a dialog or a message.
		toastError: () => false,
	})
}

export type SaveRequirementValuesInput = {
	taskId: number
	values: {fieldId: number, value: unknown}[]
}

// The requirements dialog saves its custom field values in one go, without a toast per field.
export function saveRequirementValuesMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({taskId, values}: SaveRequirementValuesInput) => {
			for (const {fieldId, value} of values) {
				await taskCustomFieldValuesSet({path: {task: taskId, field: fieldId}, body: {value}})
			}
		},
		onSettled: ({taskId}, client) => client.invalidateQueries({queryKey: customFieldKeys.values(taskId)}),
	})
}

export function useSaveRequirementValuesMutation() {
	return useMutation(saveRequirementValuesMutationOptions())
}

export function useReplaceWorkflowMutation() {
	return useMutation(replaceWorkflowMutationOptions())
}

export function useDeleteWorkflowMutation() {
	return useMutation(deleteWorkflowMutationOptions())
}

export function useChangeTaskStatusMutation() {
	return useMutation(changeTaskStatusMutationOptions())
}
