import type {
	ApprovalRequest,
	ApprovalApprover,
	ApproverSource,
	TransitionApproval,
	User,
} from '@/client/generated'

export const APPROVAL_MODES = ['any', 'all'] as const
export type ApprovalMode = typeof APPROVAL_MODES[number]

export type ApprovalStatus = 'pending' | 'approved' | 'rejected' | 'cancelled'
export type ApprovalDecisionValue = 'approved' | 'rejected'

export const APPROVER_SOURCE_TYPES = ['user', 'team', 'assignees', 'custom_field'] as const
export type ApproverSourceType = typeof APPROVER_SOURCE_TYPES[number]

export const APPROVAL_ERROR_CODE = {
	notFound: 24001,
	notPending: 24002,
	notApprover: 24003,
	alreadyDecided: 24004,
	commentRequired: 24005,
	noApprovers: 24006,
	doneRequiresApproval: 24007,
	selfDecision: 24008,
	invalidRequest: 24009,
	linkShare: 24010,
} as const

export const APPROVAL_CANCEL_REASONS = [
	'cancelled_by_requester',
	'task_deleted',
	'task_moved',
	'workflow_removed',
	'status_removed',
	'transition_removed',
	'approval_disabled',
	'status_changed',
	'target_archived',
	'requirements_not_met',
	'requester_lost_access',
	'requester_not_in_team',
	'requester_deleted',
	'no_approvers',
] as const

export type ApproverSourceResponse = {
	type: ApproverSourceType
	id: number
}

export type TransitionApprovalResponse = {
	mode: ApprovalMode
	approvers: ApproverSourceResponse[]
	rejected_status_id: number
	allow_self: boolean
	due_in: string
}

export type ApproverResponse = {
	id: number
	user_id: number
	decision: ApprovalDecisionValue | null
	comment: string
	decided: string | null
	user: User | null
}

export type StatusSummaryResponse = {
	id: number
	title: string
	color: string
	category: string
}

export type ApprovalTaskResponse = {
	id: number
	title: string
	identifier: string
	project_id: number
	project_title: string
}

export type ApprovalResponse = {
	id: number
	task_id: number
	project_id: number
	transition_id: number | null
	from_status_id: number | null
	to_status_id: number | null
	rejected_status_id: number | null
	title: string
	description: string
	comment: string
	mode: ApprovalMode
	status: ApprovalStatus
	allow_self: boolean
	due: string | null
	resolved: string | null
	cancel_reason: string
	cancel_message: string
	created: string
	approvers: ApproverResponse[]
	requested_by: User | null
	resolved_by: User | null
	task: ApprovalTaskResponse | null
	from_status: StatusSummaryResponse | null
	to_status: StatusSummaryResponse | null
	rejected_status: StatusSummaryResponse | null
}

function normalizeMode(mode: unknown): ApprovalMode {
	return mode === 'all' ? 'all' : 'any'
}

function normalizeStatus(status: unknown): ApprovalStatus {
	return status === 'approved' || status === 'rejected' || status === 'cancelled' ? status : 'pending'
}

function normalizeSummary(summary: {id?: number, title?: string, color?: string, category?: string} | null | undefined) {
	return summary
		? {id: summary.id ?? 0, title: summary.title ?? '', color: summary.color ?? '', category: summary.category ?? 'open'}
		: null
}

function normalizeApprover(approver: ApprovalApprover): ApproverResponse {
	return {
		id: approver.id ?? 0,
		user_id: approver.user_id ?? approver.user?.id ?? 0,
		decision: approver.decision ?? null,
		comment: approver.comment ?? '',
		decided: approver.decided ?? null,
		user: approver.user ?? null,
	}
}

export function normalizeApproval(request: ApprovalRequest): ApprovalResponse {
	return {
		id: request.id ?? 0,
		task_id: request.task_id ?? 0,
		project_id: request.project_id ?? 0,
		transition_id: request.transition_id ?? null,
		from_status_id: request.from_status_id ?? null,
		to_status_id: request.to_status_id ?? null,
		rejected_status_id: request.rejected_status_id ?? null,
		title: request.title ?? '',
		description: request.description ?? '',
		comment: request.comment ?? '',
		mode: normalizeMode(request.mode),
		status: normalizeStatus(request.status),
		allow_self: request.allow_self ?? false,
		due: request.due ?? null,
		resolved: request.resolved ?? null,
		cancel_reason: request.cancel_reason ?? '',
		cancel_message: request.cancel_message ?? '',
		created: request.created ?? '',
		approvers: (request.approvers ?? []).map(normalizeApprover),
		requested_by: request.requested_by ?? null,
		resolved_by: request.resolved_by ?? null,
		task: request.task
			? {
				id: request.task.id ?? 0,
				title: request.task.title ?? '',
				identifier: request.task.identifier ?? '',
				project_id: request.task.project_id ?? 0,
				project_title: request.task.project_title ?? '',
			}
			: null,
		from_status: normalizeSummary(request.from_status),
		to_status: normalizeSummary(request.to_status),
		rejected_status: normalizeSummary(request.rejected_status),
	}
}

export function normalizeTransitionApproval(approval: TransitionApproval | null | undefined): TransitionApprovalResponse | null {
	if (!approval) {
		return null
	}
	return {
		mode: normalizeMode(approval.mode),
		approvers: (approval.approvers ?? []).map(source => ({
			type: (APPROVER_SOURCE_TYPES as readonly string[]).includes(source.type ?? '')
				? source.type as ApproverSourceType
				: 'user',
			id: source.id ?? 0,
		})),
		rejected_status_id: approval.rejected_status_id ?? 0,
		allow_self: approval.allow_self ?? false,
		due_in: approval.due_in ?? '',
	}
}

// ---- State derivation ----

export function isPending(request: Pick<ApprovalResponse, 'status'>): boolean {
	return request.status === 'pending'
}

export function myApproverEntry(request: ApprovalResponse, userId: number): ApproverResponse | undefined {
	return request.approvers.find(approver => approver.user_id === userId)
}

// The requester only decides on their own request when the workflow allows it.
export function canDecide(request: ApprovalResponse, userId: number): boolean {
	if (!isPending(request) || userId <= 0) {
		return false
	}
	const entry = myApproverEntry(request, userId)
	if (!entry || entry.decision !== null) {
		return false
	}
	return request.allow_self || request.requested_by?.id !== userId
}

export function canCancel(request: ApprovalResponse, userId: number, isProjectAdmin: boolean): boolean {
	return isPending(request) && (isProjectAdmin || (userId > 0 && request.requested_by?.id === userId))
}

export type ApprovalProgress = {
	approved: number
	rejected: number
	pending: number
	total: number
}

export function approvalProgress(request: Pick<ApprovalResponse, 'approvers'>): ApprovalProgress {
	const approved = request.approvers.filter(approver => approver.decision === 'approved').length
	const rejected = request.approvers.filter(approver => approver.decision === 'rejected').length
	const total = request.approvers.length
	return {approved, rejected, pending: total - approved - rejected, total}
}

export function isOverdue(request: Pick<ApprovalResponse, 'status' | 'due'>, now: Date = new Date()): boolean {
	if (!isPending(request) || !request.due) {
		return false
	}
	const due = new Date(request.due)
	return !Number.isNaN(due.getTime()) && due.getTime() < now.getTime()
}

export function isAdHoc(request: Pick<ApprovalResponse, 'transition_id' | 'to_status_id'>): boolean {
	return request.transition_id === null && request.to_status_id === null
}

// Resolves to a translation key; reasons the client does not know yet fall back to a generic text.
export function cancelReasonKey(reason: string): string {
	return (APPROVAL_CANCEL_REASONS as readonly string[]).includes(reason)
		? `approvals.cancelReason.${reason}`
		: 'approvals.cancelReason.unknown'
}

export function pendingApprovals(requests: readonly ApprovalResponse[]): ApprovalResponse[] {
	return requests.filter(isPending)
}

export function resolvedApprovals(requests: readonly ApprovalResponse[]): ApprovalResponse[] {
	return requests.filter(request => !isPending(request))
}

// The pending request that asks for this target status of a task, if any.
export function pendingForStatus(requests: readonly ApprovalResponse[], toStatusId: number): ApprovalResponse | undefined {
	return requests.find(request => isPending(request) && request.to_status_id === toStatusId)
}

// ---- Problems of the approval endpoints ----

export type ApprovalProblem =
	| {kind: 'commentRequired'}
	| {kind: 'notPending'}
	| {kind: 'alreadyDecided'}
	| {kind: 'notApprover'}
	| {kind: 'selfDecision'}
	| {kind: 'noApprovers'}
	| {kind: 'doneRequiresApproval', toStatusId: number}
	| {kind: 'notFound'}
	| {kind: 'invalidRequest', message: string}
	| {kind: 'linkShare'}
	| {kind: 'other'}

export function readApprovalProblem(cause: unknown): ApprovalProblem {
	const problem = (typeof cause === 'object' && cause !== null ? cause : {}) as {
		code?: unknown
		detail?: unknown
		message?: unknown
		details?: unknown
	}
	const details = (typeof problem.details === 'object' && problem.details !== null ? problem.details : {}) as
		Record<string, unknown>
	switch (problem.code) {
		case APPROVAL_ERROR_CODE.commentRequired:
			return {kind: 'commentRequired'}
		case APPROVAL_ERROR_CODE.notPending:
			return {kind: 'notPending'}
		case APPROVAL_ERROR_CODE.alreadyDecided:
			return {kind: 'alreadyDecided'}
		case APPROVAL_ERROR_CODE.notApprover:
			return {kind: 'notApprover'}
		case APPROVAL_ERROR_CODE.selfDecision:
			return {kind: 'selfDecision'}
		case APPROVAL_ERROR_CODE.noApprovers:
			return {kind: 'noApprovers'}
		case APPROVAL_ERROR_CODE.doneRequiresApproval:
			return {
				kind: 'doneRequiresApproval',
				toStatusId: typeof details.to_status_id === 'number' ? details.to_status_id : 0,
			}
		case APPROVAL_ERROR_CODE.notFound:
			return {kind: 'notFound'}
		case APPROVAL_ERROR_CODE.invalidRequest:
			return {
				kind: 'invalidRequest',
				message: typeof problem.detail === 'string' ? problem.detail : String(problem.message ?? ''),
			}
		case APPROVAL_ERROR_CODE.linkShare:
			return {kind: 'linkShare'}
		default:
			return {kind: 'other'}
	}
}

export function isDoneRequiresApproval(cause: unknown): boolean {
	return readApprovalProblem(cause).kind === 'doneRequiresApproval'
}

// The message key of problems of a decision that need no field of their own.
export function decisionProblemKey(problem: ApprovalProblem): string | null {
	switch (problem.kind) {
		case 'notPending':
			return 'approvals.errors.notPending'
		case 'alreadyDecided':
			return 'approvals.errors.alreadyDecided'
		case 'notApprover':
			return 'approvals.errors.notApprover'
		case 'selfDecision':
			return 'approvals.errors.selfDecision'
		case 'notFound':
			return 'approvals.errors.notFound'
		default:
			return null
	}
}

// ---- Transition approval draft ----

export type ApproverSourceDraft = {
	// Local identity, a source has no id of its own.
	key: string
	type: ApproverSourceType
	id: number
}

export type ApprovalDraft = {
	mode: ApprovalMode
	approvers: ApproverSourceDraft[]
	rejected_status_id: number
	allow_self: boolean
	due_in: string
}

let sourceKeyCounter = 0

export function nextSourceKey(): string {
	sourceKeyCounter += 1
	return `s${sourceKeyCounter}`
}

export function emptyApprovalDraft(): ApprovalDraft {
	return {mode: 'any', approvers: [], rejected_status_id: 0, allow_self: false, due_in: ''}
}

export function newApproverSource(type: ApproverSourceType, id = 0): ApproverSourceDraft {
	return {key: nextSourceKey(), type, id: type === 'assignees' ? 0 : id}
}

export function approvalDraftFromResponse(approval: TransitionApprovalResponse | null): ApprovalDraft {
	if (!approval) {
		return emptyApprovalDraft()
	}
	return {
		mode: approval.mode,
		approvers: approval.approvers.map(source => ({key: nextSourceKey(), type: source.type, id: source.id})),
		rejected_status_id: approval.rejected_status_id,
		allow_self: approval.allow_self,
		due_in: approval.due_in,
	}
}

export function approvalDraftToDocument(draft: ApprovalDraft): TransitionApproval {
	const seen = new Set<string>()
	const approvers: ApproverSource[] = []
	for (const source of draft.approvers) {
		const id = source.type === 'assignees' ? 0 : source.id
		const identity = `${source.type}:${id}`
		if (seen.has(identity)) {
			continue
		}
		seen.add(identity)
		approvers.push(source.type === 'assignees' ? {type: 'assignees'} : {type: source.type, id})
	}
	const due = draft.due_in.trim()
	return {
		mode: draft.mode,
		approvers,
		...(draft.rejected_status_id !== 0 ? {rejected_status_id: draft.rejected_status_id} : {}),
		allow_self: draft.allow_self,
		...(due !== '' ? {due_in: due} : {}),
	}
}

// Same pattern as the server: a positive count and one of minutes, hours, days, weeks.
export function isValidDueIn(value: string): boolean {
	const match = /^\+?(\d{1,6})([mhdw])$/.exec(value.trim())
	return match !== null && Number(match[1]) > 0
}

export type ApprovalDraftError = 'approversRequired' | 'sourceIncomplete' | 'dueInvalid' | 'rejectedStatusSame'

// Mirrors what the server checks without asking it: at least one complete source, a sane duration,
// a rejected status that differs from the target.
export function validateApprovalDraft(draft: ApprovalDraft, toStatusId: number): ApprovalDraftError[] {
	const errors: ApprovalDraftError[] = []
	if (draft.approvers.length === 0) {
		errors.push('approversRequired')
	} else if (draft.approvers.some(source => source.type !== 'assignees' && source.id <= 0)) {
		errors.push('sourceIncomplete')
	}
	if (draft.due_in.trim() !== '' && !isValidDueIn(draft.due_in)) {
		errors.push('dueInvalid')
	}
	if (draft.rejected_status_id !== 0 && draft.rejected_status_id === toStatusId) {
		errors.push('rejectedStatusSame')
	}
	return errors
}

export type ApprovalServerField = 'approvers' | 'mode' | 'dueIn' | 'rejectedStatus'

// The server's definition errors name no transition, only the rule; this maps the rule to the control it is about.
export function approvalFieldOfServerMessage(message: string): ApprovalServerField | null {
	const text = message.toLowerCase()
	if (text.includes('due_in')) return 'dueIn'
	if (text.includes('rejected status')) return 'rejectedStatus'
	if (text.includes('approval mode')) return 'mode'
	if (text.includes('approver') || text.includes('approval configuration')) return 'approvers'
	return null
}

export function describeApprovers(users: readonly Pick<User, 'name' | 'username'>[]): string {
	return users.map(user => user.name || user.username || '').filter(Boolean).join(', ')
}
