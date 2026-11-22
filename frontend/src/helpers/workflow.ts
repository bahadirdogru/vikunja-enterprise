import type {IconProp} from '@fortawesome/fontawesome-svg-core'

import type {User, WorkflowWritable} from '@/client/generated'
import {
	approvalDraftFromResponse,
	approvalDraftToDocument,
	emptyApprovalDraft,
	validateApprovalDraft,
	type ApprovalDraft,
	type TransitionApprovalResponse,
} from '@/helpers/approvals'
import {readableTextColor} from '@/helpers/customFields'

export const WORKFLOW_CATEGORIES = ['open', 'in_progress', 'done', 'cancelled'] as const
export type WorkflowCategory = typeof WORKFLOW_CATEGORIES[number]

export const WORKFLOW_BUILTIN_FIELDS = [
	'assignees',
	'due_date',
	'start_date',
	'end_date',
	'description',
	'attachments',
	'labels',
] as const
export type WorkflowBuiltinField = typeof WORKFLOW_BUILTIN_FIELDS[number]

export type TransitionPermission = 'write' | 'admin'

export const WORKFLOW_ERROR_CODE = {
	noWorkflow: 21001,
	invalidDefinition: 21002,
	statusNotFound: 21003,
	statusArchived: 21004,
	notAllowed: 21005,
	forbidden: 21006,
	requirementsNotMet: 21007,
	moveTasksRequired: 21008,
	workflowRequiredForStatusView: 21009,
	statusViewBucketsManaged: 21010,
} as const

export const MAX_TITLE_LENGTH = 250

export type WorkflowStatusResponse = {
	id: number
	title: string
	color: string
	category: WorkflowCategory
	position: number
	is_initial: boolean
	archived: boolean
}

export type WorkflowTransitionResponse = {
	id: number
	from_status_id: number
	to_status_id: number
	title: string
	min_permission: TransitionPermission
	require_comment: boolean
	required_fields: string[]
	team_ids: number[]
	requires_approval: boolean
	approval: TransitionApprovalResponse | null
}

export type WorkflowResponse = {
	id: number
	project_id: number
	restrict_transitions: boolean
	statuses: WorkflowStatusResponse[]
	transitions: WorkflowTransitionResponse[]
}

export type AvailableTransitionResponse = {
	to_status: WorkflowStatusResponse
	title: string
	require_comment: boolean
	missing_fields: string[]
	// The user has to ask for an approval instead of changing the status; approvers is who would be asked.
	requires_approval: boolean
	approval_mode: string
	approvers: User[]
}

export type StatusDraft = {
	id: number
	title: string
	color: string
	category: WorkflowCategory
	is_initial: boolean
	archived: boolean
}

export type TransitionDraft = {
	// Local identity: a transition has no real id until it is saved.
	key: string
	id: number
	from_status_id: number
	to_status_id: number
	title: string
	min_permission: TransitionPermission
	require_comment: boolean
	required_fields: string[]
	team_ids: number[]
	requires_approval: boolean
	approval: ApprovalDraft
}

export type WorkflowDraft = {
	restrict_transitions: boolean
	statuses: StatusDraft[]
	transitions: TransitionDraft[]
}

export type DefaultStatusTitles = {
	todo: string
	inProgress: string
	done: string
}

export const DEFAULT_STATUS_COLORS = {
	open: '#64748b',
	in_progress: '#2563eb',
	done: '#16a34a',
	cancelled: '#dc2626',
} as const satisfies Record<WorkflowCategory, string>

let transitionKeyCounter = 0

function nextTransitionKey(): string {
	transitionKeyCounter += 1
	return `t${transitionKeyCounter}`
}

// New statuses carry negative ids so transitions and move_tasks_to can refer to them before they exist.
export function nextTemporaryStatusId(draft: WorkflowDraft): number {
	return Math.min(0, ...draft.statuses.map(status => status.id)) - 1
}

export function createDefaultDraft(titles: DefaultStatusTitles): WorkflowDraft {
	return {
		restrict_transitions: false,
		statuses: [
			{
				id: -1,
				title: titles.todo,
				color: DEFAULT_STATUS_COLORS.open,
				category: 'open',
				is_initial: true,
				archived: false,
			},
			{
				id: -2,
				title: titles.inProgress,
				color: DEFAULT_STATUS_COLORS.in_progress,
				category: 'in_progress',
				is_initial: false,
				archived: false,
			},
			{
				id: -3,
				title: titles.done,
				color: DEFAULT_STATUS_COLORS.done,
				category: 'done',
				is_initial: false,
				archived: false,
			},
		],
		transitions: [],
	}
}

export function draftFromWorkflow(workflow: WorkflowResponse): WorkflowDraft {
	return {
		restrict_transitions: workflow.restrict_transitions,
		statuses: [...workflow.statuses]
			.sort((a, b) => a.position - b.position || a.id - b.id)
			.map(status => ({
				id: status.id,
				title: status.title,
				color: status.color,
				category: status.category,
				is_initial: status.is_initial,
				archived: status.archived,
			})),
		transitions: workflow.transitions.map(transition => ({
			key: nextTransitionKey(),
			id: transition.id,
			from_status_id: transition.from_status_id,
			to_status_id: transition.to_status_id,
			title: transition.title,
			min_permission: transition.min_permission,
			require_comment: transition.require_comment,
			required_fields: [...transition.required_fields],
			team_ids: [...transition.team_ids],
			requires_approval: transition.requires_approval,
			approval: approvalDraftFromResponse(transition.approval),
		})),
	}
}

export function draftToDocument(draft: WorkflowDraft, moveTasksTo = 0): WorkflowWritable {
	const knownIds = new Set(draft.statuses.map(status => status.id))
	return {
		restrict_transitions: draft.restrict_transitions,
		statuses: draft.statuses.map((status, index) => ({
			id: status.id,
			title: status.title.trim(),
			color: status.color,
			category: status.category,
			is_initial: status.is_initial,
			position: index + 1,
		})),
		transitions: draft.transitions
			.filter(transition => knownIds.has(transition.from_status_id) && knownIds.has(transition.to_status_id))
			.map(transition => ({
				...(transition.id > 0 ? {id: transition.id} : {}),
				from_status_id: transition.from_status_id,
				to_status_id: transition.to_status_id,
				title: transition.title.trim(),
				min_permission: transition.min_permission,
				require_comment: transition.require_comment,
				required_fields: transition.required_fields,
				team_ids: transition.team_ids,
				...(transition.requires_approval
					? {requires_approval: true, approval: approvalDraftToDocument(transition.approval)}
					: {}),
			})),
		...(moveTasksTo !== 0 ? {move_tasks_to: moveTasksTo} : {}),
	}
}

// Identity keys are local, so they stay out of the comparison.
export function serializeDraft(draft: WorkflowDraft): string {
	return JSON.stringify({
		restrict_transitions: draft.restrict_transitions,
		statuses: draft.statuses,
		transitions: draft.transitions.map(({key: _key, ...transition}) => ({
			...transition,
			// A switched off approval keeps nothing the server would store.
			approval: transition.requires_approval
				? {...transition.approval, approvers: transition.approval.approvers.map(({key: _source, ...source}) => source)}
				: null,
		})),
	})
}

export function isDraftDirty(draft: WorkflowDraft, saved: WorkflowDraft | null): boolean {
	return saved === null || serializeDraft(draft) !== serializeDraft(saved)
}

export function activeStatuses(draft: WorkflowDraft): StatusDraft[] {
	return draft.statuses.filter(status => !status.archived)
}

export function addStatus(draft: WorkflowDraft, title: string): WorkflowDraft {
	const status: StatusDraft = {
		id: nextTemporaryStatusId(draft),
		title,
		color: DEFAULT_STATUS_COLORS.in_progress,
		category: 'in_progress',
		is_initial: false,
		archived: false,
	}
	const statuses = [...draft.statuses]
	const lastActive = statuses.map(existing => existing.archived).lastIndexOf(false)
	statuses.splice(lastActive + 1, 0, status)
	return {...draft, statuses}
}

export function updateStatus(draft: WorkflowDraft, id: number, patch: Partial<StatusDraft>): WorkflowDraft {
	return {
		...draft,
		statuses: draft.statuses.map(status => status.id === id ? {...status, ...patch} : status),
	}
}

export function setInitialStatus(draft: WorkflowDraft, id: number): WorkflowDraft {
	return {
		...draft,
		statuses: draft.statuses.map(status => ({...status, is_initial: status.id === id})),
	}
}

export function moveStatus(draft: WorkflowDraft, id: number, delta: -1 | 1): WorkflowDraft {
	const active = activeStatuses(draft)
	const from = active.findIndex(status => status.id === id)
	const to = from + delta
	if (from === -1 || to < 0 || to >= active.length) {
		return draft
	}
	const reordered = [...active]
	const [moved] = reordered.splice(from, 1)
	reordered.splice(to, 0, moved as StatusDraft)
	return {...draft, statuses: [...reordered, ...draft.statuses.filter(status => status.archived)]}
}

// The initial flag moves to the first remaining status so the draft stays saveable.
export function removeStatus(draft: WorkflowDraft, id: number): WorkflowDraft {
	const removed = draft.statuses.find(status => status.id === id)
	const statuses = draft.statuses.filter(status => status.id !== id)
	const next: WorkflowDraft = {
		...draft,
		statuses,
		transitions: draft.transitions
			.filter(transition => transition.from_status_id !== id && transition.to_status_id !== id)
			.map(transition => transition.approval.rejected_status_id === id
				? {...transition, approval: {...transition.approval, rejected_status_id: 0}}
				: transition),
	}
	if (removed?.is_initial) {
		const first = statuses.find(status => !status.archived)
		if (first) {
			return setInitialStatus(next, first.id)
		}
	}
	return next
}

export function newTransition(from: number, to: number): TransitionDraft {
	return {
		key: nextTransitionKey(),
		id: 0,
		from_status_id: from,
		to_status_id: to,
		title: '',
		min_permission: 'write',
		require_comment: false,
		required_fields: [],
		team_ids: [],
		requires_approval: false,
		approval: emptyApprovalDraft(),
	}
}

export function addTransition(draft: WorkflowDraft, transition: TransitionDraft): WorkflowDraft {
	return {...draft, transitions: [...draft.transitions, transition]}
}

export function updateTransition(
	draft: WorkflowDraft,
	key: string,
	patch: Partial<Omit<TransitionDraft, 'key'>>,
): WorkflowDraft {
	return {
		...draft,
		transitions: draft.transitions.map(transition => transition.key === key ? {...transition, ...patch} : transition),
	}
}

export function removeTransition(draft: WorkflowDraft, key: string): WorkflowDraft {
	return {...draft, transitions: draft.transitions.filter(transition => transition.key !== key)}
}

export function transitionAt(draft: WorkflowDraft, from: number, to: number): TransitionDraft | undefined {
	return draft.transitions.find(transition => transition.from_status_id === from && transition.to_status_id === to)
}

// Returns the draft and the key of the transition that exists afterwards, if any.
export function toggleTransition(
	draft: WorkflowDraft,
	from: number,
	to: number,
): {draft: WorkflowDraft, key: string | null} {
	if (from === to) {
		return {draft, key: null}
	}
	const existing = transitionAt(draft, from, to)
	if (existing) {
		return {draft: removeTransition(draft, existing.key), key: null}
	}
	const created = newTransition(from, to)
	return {draft: addTransition(draft, created), key: created.key}
}

export type DraftErrors = {
	global: string[]
	statuses: Record<number, string[]>
	transitions: Record<string, string[]>
	valid: boolean
}

// Mirrors the server rules so most mistakes show up before the request.
export function validateDraft(draft: WorkflowDraft): DraftErrors {
	const errors: DraftErrors = {global: [], statuses: {}, transitions: {}, valid: true}
	const active = activeStatuses(draft)
	const addStatusError = (id: number, code: string) => {
		errors.statuses[id] = [...(errors.statuses[id] ?? []), code]
	}
	const addTransitionError = (key: string, code: string) => {
		errors.transitions[key] = [...(errors.transitions[key] ?? []), code]
	}

	if (active.length === 0) {
		errors.global.push('noStatuses')
	}
	if (active.filter(status => status.is_initial).length !== 1) {
		errors.global.push('noInitial')
	}
	if (!active.some(status => status.category === 'done')) {
		errors.global.push('noDone')
	}

	const seenTitles = new Map<string, number>()
	for (const status of active) {
		const title = status.title.trim()
		if (title === '') {
			addStatusError(status.id, 'titleRequired')
			continue
		}
		if ([...title].length > MAX_TITLE_LENGTH) {
			addStatusError(status.id, 'titleTooLong')
		}
		const normalized = title.toLocaleLowerCase()
		if (seenTitles.has(normalized)) {
			addStatusError(status.id, 'titleDuplicate')
		} else {
			seenTitles.set(normalized, status.id)
		}
	}

	const activeIds = new Set(active.map(status => status.id))
	const seenPairs = new Set<string>()
	for (const transition of draft.transitions) {
		if (!activeIds.has(transition.from_status_id) || !activeIds.has(transition.to_status_id)) {
			addTransitionError(transition.key, 'statusRequired')
			continue
		}
		if (transition.from_status_id === transition.to_status_id) {
			addTransitionError(transition.key, 'sameStatus')
		}
		const pair = `${transition.from_status_id}>${transition.to_status_id}`
		if (seenPairs.has(pair)) {
			addTransitionError(transition.key, 'duplicatePair')
		}
		seenPairs.add(pair)
		if ([...transition.title.trim()].length > MAX_TITLE_LENGTH) {
			addTransitionError(transition.key, 'titleTooLong')
		}
		if (transition.requires_approval) {
			for (const code of validateApprovalDraft(transition.approval, transition.to_status_id)) {
				addTransitionError(transition.key, `approval${code.charAt(0).toUpperCase()}${code.slice(1)}`)
			}
			const rejected = transition.approval.rejected_status_id
			if (rejected !== 0 && !activeIds.has(rejected)) {
				addTransitionError(transition.key, 'approvalRejectedStatusGone')
			}
		}
	}

	errors.valid = errors.global.length === 0
		&& Object.keys(errors.statuses).length === 0
		&& Object.keys(errors.transitions).length === 0
	return errors
}

export function categoryIcon(category: WorkflowCategory): IconProp {
	switch (category) {
		case 'in_progress':
			return 'circle-half-stroke'
		case 'done':
			return 'circle-check'
		case 'cancelled':
			return 'circle-xmark'
		default:
			return ['far', 'circle']
	}
}

export function statusBadgeStyle(color: string): Record<string, string> {
	if (!/^#[0-9a-f]{6}$/i.test(color)) {
		return {}
	}
	return {
		backgroundColor: color,
		color: readableTextColor(color),
	}
}

export function isCustomFieldName(field: string): boolean {
	return /^cf_\d+$/.test(field)
}

export function customFieldIdOf(field: string): number {
	return isCustomFieldName(field) ? Number(field.slice(3)) : 0
}

export function customFieldName(id: number): string {
	return `cf_${id}`
}

// The status a done task is moved to: the first one of the category done.
export function doneTargetStatus(workflow: WorkflowResponse): WorkflowStatusResponse | undefined {
	return [...workflow.statuses]
		.sort((a, b) => a.position - b.position || a.id - b.id)
		.find(status => !status.archived && status.category === 'done')
}

export function reopenTargetStatus(workflow: WorkflowResponse): WorkflowStatusResponse | undefined {
	return workflow.statuses.find(status => !status.archived && status.is_initial)
}

export type WorkflowProblem =
	| {kind: 'noWorkflow'}
	| {kind: 'invalidDefinition', message: string}
	| {kind: 'moveTasksRequired', statusIds: number[]}
	| {kind: 'statusGone'}
	| {kind: 'notAllowed'}
	| {kind: 'forbidden', reason: string}
	| {kind: 'statusViewNeedsWorkflow'}
	| {kind: 'requirements', missingFields: string[], commentRequired: boolean, toStatusId: number, taskId: number}
	| {kind: 'other'}

function numberList(value: unknown): number[] {
	return Array.isArray(value) ? value.filter((entry): entry is number => typeof entry === 'number') : []
}

function stringList(value: unknown): string[] {
	return Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === 'string') : []
}

export function readWorkflowProblem(cause: unknown): WorkflowProblem {
	const problem = (typeof cause === 'object' && cause !== null ? cause : {}) as {
		code?: unknown
		detail?: unknown
		message?: unknown
		details?: unknown
	}
	const details = (typeof problem.details === 'object' && problem.details !== null ? problem.details : {}) as
		Record<string, unknown>

	switch (problem.code) {
		case WORKFLOW_ERROR_CODE.noWorkflow:
			return {kind: 'noWorkflow'}
		case WORKFLOW_ERROR_CODE.invalidDefinition:
			return {
				kind: 'invalidDefinition',
				message: typeof problem.detail === 'string' ? problem.detail : String(problem.message ?? ''),
			}
		case WORKFLOW_ERROR_CODE.moveTasksRequired:
			return {kind: 'moveTasksRequired', statusIds: numberList(details.status_ids)}
		case WORKFLOW_ERROR_CODE.statusNotFound:
		case WORKFLOW_ERROR_CODE.statusArchived:
			return {kind: 'statusGone'}
		case WORKFLOW_ERROR_CODE.notAllowed:
			return {kind: 'notAllowed'}
		case WORKFLOW_ERROR_CODE.forbidden:
			return {kind: 'forbidden', reason: typeof details.reason === 'string' ? details.reason : ''}
		case WORKFLOW_ERROR_CODE.workflowRequiredForStatusView:
			return {kind: 'statusViewNeedsWorkflow'}
		case WORKFLOW_ERROR_CODE.requirementsNotMet:
			return {
				kind: 'requirements',
				missingFields: stringList(details.missing_fields),
				commentRequired: details.comment_required === true,
				toStatusId: typeof details.to_status_id === 'number' ? details.to_status_id : 0,
				taskId: typeof details.task_id === 'number' ? details.task_id : 0,
			}
		default:
			return {kind: 'other'}
	}
}

export type ProblemOutcome =
	| {kind: 'dialog', missingFields: string[], commentRequired: boolean}
	| {kind: 'message', messageKey: string}
	| {kind: 'unhandled'}

// A rejected status change either needs more input (the dialog) or ends with a clear message.
export function outcomeOfProblem(problem: WorkflowProblem): ProblemOutcome {
	switch (problem.kind) {
		case 'requirements':
			return {kind: 'dialog', missingFields: problem.missingFields, commentRequired: problem.commentRequired}
		case 'notAllowed':
			return {kind: 'message', messageKey: 'workflow.errors.notAllowed'}
		case 'forbidden':
			return {
				kind: 'message',
				messageKey: problem.reason === 'team'
					? 'workflow.errors.forbiddenTeam'
					: 'workflow.errors.forbiddenPermission',
			}
		case 'statusGone':
			return {kind: 'message', messageKey: 'workflow.errors.statusGone'}
		default:
			return {kind: 'unhandled'}
	}
}

export function needsInput(transition: Pick<AvailableTransitionResponse, 'missing_fields' | 'require_comment'>): boolean {
	return transition.missing_fields.length > 0 || transition.require_comment
}

// Columns of a kanban view in status mode have the negated id of their status as bucket id,
// which can never clash with the (positive) ids of real buckets.
export function statusBucketId(statusId: number): number {
	return -statusId
}

export function isStatusBucketId(bucketId: number): boolean {
	return bucketId < 0
}

export function statusIdOfBucket(bucketId: number): number {
	return -bucketId
}

export type StatusBadgeProps = {title: string, color: string, category: WorkflowCategory}

// The status that tasks and columns embed (expand=status) in the shape of the badge.
export function statusBadgeOf(
	status: {title?: string, color?: string, category?: string} | null | undefined,
): StatusBadgeProps | null {
	if (!status?.title) {
		return null
	}
	const category = (WORKFLOW_CATEGORIES as readonly string[]).includes(status.category ?? '')
		? status.category as WorkflowCategory
		: 'open'
	return {title: status.title, color: status.color ?? '', category}
}
