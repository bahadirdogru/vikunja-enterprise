import {describe, expect, it} from 'vitest'

import {
	addStatus,
	createDefaultDraft,
	doneTargetStatus,
	draftFromWorkflow,
	draftToDocument,
	isDraftDirty,
	moveStatus,
	newTransition,
	outcomeOfProblem,
	readWorkflowProblem,
	removeStatus,
	reopenTargetStatus,
	setInitialStatus,
	toggleTransition,
	transitionAt,
	updateStatus,
	validateDraft,
	addTransition,
	type WorkflowResponse,
} from './workflow'

const titles = {todo: 'To do', inProgress: 'In progress', done: 'Done'}

function stored(): WorkflowResponse {
	return {
		id: 1,
		project_id: 9,
		restrict_transitions: true,
		statuses: [
			{id: 12, title: 'Done', color: '#16a34a', category: 'done', position: 3, is_initial: false, archived: false},
			{id: 10, title: 'To do', color: '#64748b', category: 'open', position: 1, is_initial: true, archived: false},
			{id: 11, title: 'Old', color: '', category: 'open', position: 2, is_initial: false, archived: true},
		],
		transitions: [{
			id: 5,
			from_status_id: 10,
			to_status_id: 12,
			title: 'Finish',
			min_permission: 'admin',
			require_comment: true,
			required_fields: ['due_date', 'cf_4'],
			team_ids: [2],
		}],
	}
}

describe('default draft', () => {
	it('is valid, has exactly one initial and a done status with temporary ids', () => {
		const draft = createDefaultDraft(titles)

		expect(draft.statuses.map(status => status.title)).toEqual(['To do', 'In progress', 'Done'])
		expect(draft.statuses.every(status => status.id < 0)).toBe(true)
		expect(draft.statuses.filter(status => status.is_initial)).toHaveLength(1)
		expect(validateDraft(draft).valid).toBe(true)
	})
})

describe('temporary ids', () => {
	it('gives each new status the next lower negative id', () => {
		let draft = createDefaultDraft(titles)
		draft = addStatus(draft, 'Review')
		draft = addStatus(draft, 'QA')

		expect(draft.statuses.map(status => status.id)).toEqual([-1, -2, -3, -4, -5])
	})

	it('starts below zero when every status is stored', () => {
		const draft = addStatus(draftFromWorkflow(stored()), 'New')

		expect(draft.statuses.at(-1)?.id).toBe(-1)
	})

	it('inserts new statuses after the last active one', () => {
		const draft = addStatus(draftFromWorkflow(stored()), 'New')

		expect(draft.statuses.map(status => status.archived)).toEqual([false, true, false, false])
	})
})

describe('document mapping', () => {
	it('orders stored statuses by position and keeps archived ones', () => {
		const draft = draftFromWorkflow(stored())

		expect(draft.statuses.map(status => status.id)).toEqual([10, 11, 12])
	})

	it('lets transitions refer to new statuses by their negative id', () => {
		let draft = draftFromWorkflow(stored())
		draft = addStatus(draft, 'Review')
		draft = addTransition(draft, newTransition(10, -1))

		const document = draftToDocument(draft)

		expect(document.statuses?.map(status => status.id)).toEqual([10, 11, 12, -1])
		expect(document.statuses?.map(status => status.position)).toEqual([1, 2, 3, 4])
		expect(document.transitions?.map(transition => [transition.from_status_id, transition.to_status_id]))
			.toEqual([[10, 12], [10, -1]])
	})

	it('omits the id of new transitions and keeps the id of stored ones', () => {
		const draft = addTransition(draftFromWorkflow(stored()), newTransition(10, 12 + 0))
		const document = draftToDocument({...draft, transitions: [draft.transitions[0]!, {...draft.transitions[1]!, to_status_id: 10, from_status_id: 12}]})

		expect(document.transitions?.[0]).toMatchObject({id: 5, title: 'Finish', required_fields: ['due_date', 'cf_4'], team_ids: [2]})
		expect(document.transitions?.[1]).not.toHaveProperty('id')
	})

	it('sends move_tasks_to only when given and drops transitions of missing statuses', () => {
		const draft = {...draftFromWorkflow(stored())}
		draft.statuses = draft.statuses.filter(status => status.id !== 12)

		expect(draftToDocument(draft).move_tasks_to).toBeUndefined()
		expect(draftToDocument(draft).transitions).toEqual([])
		expect(draftToDocument(draft, 10).move_tasks_to).toBe(10)
		expect(draftToDocument(draft, -2).move_tasks_to).toBe(-2)
	})

	it('detects changes and ignores the local transition keys', () => {
		const saved = draftFromWorkflow(stored())

		expect(isDraftDirty(draftFromWorkflow(stored()), saved)).toBe(false)
		expect(isDraftDirty(updateStatus(saved, 10, {title: 'Backlog'}), saved)).toBe(true)
		expect(isDraftDirty(saved, null)).toBe(true)
	})
})

describe('status editing', () => {
	it('moves the initial flag to the first remaining status when the initial one is removed', () => {
		const draft = removeStatus(createDefaultDraft(titles), -1)

		expect(draft.statuses.find(status => status.is_initial)?.id).toBe(-2)
	})

	it('removes the transitions of a removed status', () => {
		const draft = removeStatus(draftFromWorkflow(stored()), 12)

		expect(draft.transitions).toEqual([])
	})

	it('keeps exactly one initial status', () => {
		const draft = setInitialStatus(createDefaultDraft(titles), -3)

		expect(draft.statuses.filter(status => status.is_initial).map(status => status.id)).toEqual([-3])
	})

	it('moves a status among the active ones and ignores the edges', () => {
		const draft = createDefaultDraft(titles)

		expect(moveStatus(draft, -3, -1).statuses.map(status => status.id)).toEqual([-1, -3, -2])
		expect(moveStatus(draft, -1, -1)).toBe(draft)
		expect(moveStatus(draft, -3, 1)).toBe(draft)
	})
})

describe('validation', () => {
	it('flags empty and duplicate titles case-insensitively', () => {
		let draft = createDefaultDraft(titles)
		draft = updateStatus(draft, -1, {title: '  '})
		draft = updateStatus(draft, -3, {title: 'in progress'})

		const errors = validateDraft(draft)

		expect(errors.statuses[-1]).toEqual(['titleRequired'])
		expect(errors.statuses[-3]).toEqual(['titleDuplicate'])
		expect(errors.valid).toBe(false)
	})

	it('needs one initial and at least one done status', () => {
		let draft = createDefaultDraft(titles)
		draft = updateStatus(draft, -3, {category: 'cancelled'})
		expect(validateDraft(draft).global).toEqual(['noDone'])

		draft = updateStatus(draft, -1, {is_initial: false})
		expect(validateDraft(draft).global).toEqual(['noInitial', 'noDone'])
	})

	it('rejects self transitions and duplicate pairs', () => {
		let draft = createDefaultDraft(titles)
		const self = newTransition(-1, -1)
		const first = newTransition(-1, -2)
		const second = newTransition(-1, -2)
		draft = addTransition(addTransition(addTransition(draft, self), first), second)

		const errors = validateDraft(draft)

		expect(errors.transitions[self.key]).toEqual(['sameStatus'])
		expect(errors.transitions[first.key]).toBeUndefined()
		expect(errors.transitions[second.key]).toEqual(['duplicatePair'])
	})

	it('ignores archived statuses when checking titles', () => {
		const draft = updateStatus(draftFromWorkflow(stored()), 11, {title: 'To do'})

		expect(validateDraft(draft).valid).toBe(true)
	})
})

describe('matrix mapping', () => {
	it('adds a transition for an unchecked cell and removes it again', () => {
		const draft = createDefaultDraft(titles)

		const added = toggleTransition(draft, -1, -3)
		expect(added.key).not.toBeNull()
		expect(transitionAt(added.draft, -1, -3)?.key).toBe(added.key)
		expect(transitionAt(added.draft, -3, -1)).toBeUndefined()

		const removed = toggleTransition(added.draft, -1, -3)
		expect(removed.key).toBeNull()
		expect(removed.draft.transitions).toEqual([])
	})

	it('has no cell for a status and itself', () => {
		const draft = createDefaultDraft(titles)

		expect(toggleTransition(draft, -1, -1)).toEqual({draft, key: null})
	})

	it('maps every listed transition to exactly one cell', () => {
		const draft = draftFromWorkflow(stored())

		expect(transitionAt(draft, 10, 12)?.id).toBe(5)
		expect(draft.statuses.flatMap(from => draft.statuses.map(to => transitionAt(draft, from.id, to.id)))
			.filter(Boolean)).toHaveLength(draft.transitions.length)
	})
})

describe('done targets', () => {
	it('uses the first done status by position and the initial status to reopen', () => {
		const workflow = stored()
		workflow.statuses.push({
			id: 13, title: 'Shipped', color: '', category: 'done', position: 0.5, is_initial: false, archived: false,
		})

		expect(doneTargetStatus(workflow)?.id).toBe(13)
		expect(reopenTargetStatus(workflow)?.id).toBe(10)
	})
})

describe('API problems', () => {
	it('reads the unmet requirements of a 422', () => {
		const problem = readWorkflowProblem({
			status: 422,
			code: 21007,
			details: {missing_fields: ['due_date', 'cf_3'], comment_required: true, to_status_id: 12},
		})

		expect(problem).toEqual({
			kind: 'requirements',
			missingFields: ['due_date', 'cf_3'],
			commentRequired: true,
			toStatusId: 12,
		})
		expect(outcomeOfProblem(problem)).toEqual({
			kind: 'dialog',
			missingFields: ['due_date', 'cf_3'],
			commentRequired: true,
		})
	})

	it('maps not-allowed and forbidden to clear messages', () => {
		expect(outcomeOfProblem(readWorkflowProblem({code: 21005}))).toEqual({
			kind: 'message',
			messageKey: 'workflow.errors.notAllowed',
		})
		expect(outcomeOfProblem(readWorkflowProblem({code: 21006, details: {reason: 'team'}}))).toEqual({
			kind: 'message',
			messageKey: 'workflow.errors.forbiddenTeam',
		})
		expect(outcomeOfProblem(readWorkflowProblem({code: 21006, details: {reason: 'permission'}}))).toEqual({
			kind: 'message',
			messageKey: 'workflow.errors.forbiddenPermission',
		})
	})

	it('reads the statuses a removal still needs a target for', () => {
		expect(readWorkflowProblem({code: 21008, details: {status_ids: [4, 7]}}))
			.toEqual({kind: 'moveTasksRequired', statusIds: [4, 7]})
	})

	it('tolerates missing details and unknown errors', () => {
		expect(readWorkflowProblem({code: 21007})).toMatchObject({kind: 'requirements', missingFields: []})
		expect(readWorkflowProblem(new Error('boom'))).toEqual({kind: 'other'})
		expect(readWorkflowProblem(null)).toEqual({kind: 'other'})
		expect(outcomeOfProblem({kind: 'other'})).toEqual({kind: 'unhandled'})
		expect(readWorkflowProblem({code: 21001})).toEqual({kind: 'noWorkflow'})
	})

	it('uses the detail of a rejected definition', () => {
		expect(readWorkflowProblem({code: 21002, detail: 'exactly one status must be the initial status'}))
			.toEqual({kind: 'invalidDefinition', message: 'exactly one status must be the initial status'})
	})
})
