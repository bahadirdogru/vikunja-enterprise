import {describe, expect, it} from 'vitest'

import catalogFixture from './automationCatalog.fixture.json'
import {
	actionDef,
	addChild,
	changeActionType,
	childrenOf,
	clone,
	defaultParams,
	draftFromRule,
	draftSignature,
	draftToDocument,
	duplicateAction,
	effectiveKind,
	emptyDraft,
	fieldGroupOf,
	getNode,
	humanize,
	issuesFromProblem,
	knownPaths,
	mapIssues,
	offeredActions,
	moveItem,
	newComparison,
	newGroup,
	normalizeCatalog,
	operatorsFor,
	pathToId,
	pruneConditions,
	removeAction,
	removeNode,
	replaceNode,
	setGroupKind,
	validateDraft,
	wrapNode,
	fieldDef,
	type ConditionNode,
	type RuleDraft,
} from './automation'
import {summarizeRule} from './automationSummary'
import {availableTemplates, RULE_TEMPLATES} from './automationTemplates'
import {isValidDateSpec, isValidOffset} from './automationValues'

const catalog = normalizeCatalog(catalogFixture as unknown as Record<string, unknown>)
const t = (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key
const te = () => false

function draftWith(partial: Partial<RuleDraft>): RuleDraft {
	return {...emptyDraft(), title: 'Rule', trigger: {type: 'task.created', options: {}}, ...partial}
}

describe('catalog normalization', () => {
	it('keeps params, kind_by and enums', () => {
		const setField = actionDef(catalog, 'task.set_field')!
		const value = setField.params.find(p => p.name === 'value')!
		expect(value.kind_by?.param).toBe('field')
		expect(effectiveKind(value, {field: 'due_date'})).toBe('date')
		expect(effectiveKind(value, {})).toBeNull()
		expect(setField.params[0].enum).toContain('title')
	})

	it('survives missing arrays and unknown kinds', () => {
		const result = normalizeCatalog({triggers: null, actions: [{type: 'x.y', params: [{name: 'p', kind: 'brand_new'}]}]})
		expect(result.triggers).toEqual([])
		expect(result.actions[0].params[0]).toMatchObject({kind: 'brand_new', required: false, enum: []})
		expect(result.supportsScript).toBe(false)
		expect(result.script).toBeNull()
	})

	it('groups fields', () => {
		expect(fieldGroupOf(fieldDef(catalog, 'title')!)).toBe('task')
		expect(fieldGroupOf(fieldDef(catalog, 'status')!)).toBe('status')
		expect(fieldGroupOf(fieldDef(catalog, 'cf_1')!)).toBe('custom')
		expect(fieldGroupOf(fieldDef(catalog, 'event.doer.name')!)).toBe('event')
	})

	it('limits operators to the field', () => {
		const ops = operatorsFor(catalog, fieldDef(catalog, 'done')!).map(o => o.type)
		expect(ops).toEqual(['=', '!='])
		expect(operatorsFor(catalog, undefined)).toEqual([])
	})
})

describe('condition tree', () => {
	const leaf = (field: string): ConditionNode => ({field, op: '=', value: 1})

	it('adds the first condition as an all group and removes back to null', () => {
		let tree: ConditionNode | null = addChild(null, [], newComparison())
		expect(tree).toEqual({all: [{field: '', op: ''}]})
		tree = removeNode(tree, [0])
		expect(tree).toBeNull()
	})

	it('nests groups and edits by path', () => {
		let tree: ConditionNode | null = {all: [leaf('priority')]}
		tree = addChild(tree, [], newGroup('any'))
		tree = addChild(tree, [1], leaf('done'))
		tree = addChild(tree, [1], leaf('title'))
		expect(getNode(tree, [1, 1])).toEqual(leaf('title'))
		tree = replaceNode(tree, [1, 0], {field: 'done', op: '!=', value: true})
		expect(getNode(tree, [1, 0])).toEqual({field: 'done', op: '!=', value: true})
		tree = removeNode(tree, [1, 0])
		expect(childrenOf(getNode(tree, [1])!)).toHaveLength(1)
	})

	it('changes group kind and wraps nodes', () => {
		let tree: ConditionNode = {all: [leaf('priority'), leaf('done')]}
		tree = setGroupKind(tree, [], 'not')
		expect(tree).toEqual({not: [leaf('priority'), leaf('done')]})
		tree = wrapNode(tree, [0], 'any')
		expect(tree).toEqual({not: [{any: [leaf('priority')]}, leaf('done')]})
	})

	it('prunes empty groups', () => {
		expect(pruneConditions({all: []})).toBeNull()
		expect(pruneConditions({all: [{any: []}, leaf('a')]})).toEqual({all: [leaf('a')]})
	})

	it('removing the last child of a nested group removes the group only', () => {
		const tree: ConditionNode = {all: [leaf('a'), {any: [leaf('b')]}]}
		expect(removeNode(tree, [1, 0])).toEqual({all: [leaf('a'), {any: []}]})
		// ...and pruning on save drops it.
		expect(pruneConditions(removeNode(tree, [1, 0]))).toEqual({all: [leaf('a')]})
	})

	it('preserves a script node through drafts and serialization', () => {
		const rule = {
			title: 'x',
			trigger: {type: 'task.created'},
			conditions: {all: [{script: 'return task.priority > 2'}, leaf('done')]},
			actions: [{type: 'task.set_done', params: {done: true}}],
		}
		const draft = draftFromRule(rule)
		expect(draft.conditions).toEqual(rule.conditions)
		expect(draftToDocument(draft).conditions).toEqual(rule.conditions)
	})

	it('wraps a bare root comparison into a group on load', () => {
		const draft = draftFromRule({trigger: {type: 'task.created'}, conditions: {field: 'done', op: '=', value: true}, actions: []})
		expect(draft.conditions).toEqual({all: [{field: 'done', op: '=', value: true}]})
	})
})

describe('rule documents', () => {
	it('serializes without empty options, params and values', () => {
		const draft = draftWith({
			trigger: {type: 'task.updated', options: {fields: []}},
			conditions: {all: [{field: 'priority', op: 'is_empty', value: 3}, {field: 'done', op: '=', value: false}]},
			actions: [
				{type: 'task.add_comment', params: {text: 'hi', unknown_future: {a: 1}}},
				{type: 'task.set_field', params: {field: 'title', value: ''}},
			],
		})
		expect(draftToDocument(draft)).toEqual({
			title: 'Rule',
			description: '',
			enabled: true,
			trigger: {type: 'task.updated'},
			conditions: {all: [{field: 'priority', op: 'is_empty'}, {field: 'done', op: '=', value: false}]},
			actions: [
				{type: 'task.add_comment', params: {text: 'hi', unknown_future: {a: 1}}},
				{type: 'task.set_field', params: {field: 'title'}},
			],
		})
	})

	it('serializes a flow.stop_if condition param as a tree', () => {
		const draft = draftWith({actions: [{type: 'flow.stop_if', params: {condition: {all: [{field: 'done', op: '=', value: true}, {any: []}]}}}]})
		expect(draftToDocument(draft).actions[0].params).toEqual({condition: {all: [{field: 'done', op: '=', value: true}]}})
	})

	it('round-trips a server rule without changing its signature', () => {
		const rule = {
			title: 'R', description: 'd', enabled: true,
			trigger: {type: 'task.status_changed', options: {to_category: 'done'}},
			conditions: {all: [{field: 'priority', op: '>=', value: 3}]},
			actions: [{type: 'task.add_label', params: {label_id: 4}}],
		}
		const draft = draftFromRule(rule)
		expect(draftToDocument(draft)).toEqual(rule)
		expect(draftSignature(draftFromRule(clone(rule)))).toBe(draftSignature(draft))
	})

	it('lists operations: move, duplicate, remove', () => {
		const actions = [{type: 'a', params: {}}, {type: 'b', params: {}}, {type: 'c', params: {}}]
		expect(moveItem(actions, 0, -1)).toBe(actions)
		expect(moveItem(actions, 0, 1).map(a => a.type)).toEqual(['b', 'a', 'c'])
		expect(duplicateAction(actions, 1).map(a => a.type)).toEqual(['a', 'b', 'b', 'c'])
		expect(removeAction(actions, 1).map(a => a.type)).toEqual(['a', 'c'])
		const dup = duplicateAction([{type: 'x', params: {n: {a: 1}}}], 0)
		expect(dup[0].params.n).not.toBe(dup[1].params.n)
	})

	it('keeps shared params when the action type changes', () => {
		const comment = actionDef(catalog, 'task.add_comment')!
		const subtask = actionDef(catalog, 'task.create_subtask')!
		const next = changeActionType({type: comment.type, params: {text: 'x'}}, subtask, comment)
		expect(next).toEqual({type: 'task.create_subtask', params: {}})
		const same = changeActionType({type: 'task.create_subtask', params: {title: 't', description: 'd'}}, actionDef(catalog, 'task.create_related'), subtask)
		expect(same.params).toEqual({title: 't', description: 'd'})
		expect(defaultParams(actionDef(catalog, 'task.set_done'))).toEqual({done: true})
	})
})

describe('validation and error paths', () => {
	it('requires title, trigger, actions and required params', () => {
		const issues = validateDraft(emptyDraft(), catalog, t)
		expect(issues.map(i => i.path)).toEqual(['title', 'trigger.type', 'actions'])

		const draft = draftWith({
			actions: [{type: 'task.set_status', params: {}}, {type: 'task.set_done', params: {done: false}}],
			conditions: {all: [{field: 'priority', op: '', value: undefined}, {field: 'title', op: '=', value: ''}, {field: 'done', op: 'is_empty'}]},
		})
		expect(validateDraft(draft, catalog, t).map(i => i.path)).toEqual([
			'conditions.all[0].op',
			'conditions.all[1].value',
			'actions[0].params.status_id',
		])
	})

	it('accepts false and 0 as values', () => {
		const draft = draftWith({
			conditions: {all: [{field: 'done', op: '=', value: false}, {field: 'priority', op: '=', value: 0}]},
			actions: [{type: 'task.set_done', params: {done: false}}],
		})
		expect(validateDraft(draft, catalog, t)).toEqual([])
	})

	it('validates the condition inside flow.stop_if', () => {
		const draft = draftWith({actions: [{type: 'flow.stop_if', params: {condition: {all: [{field: 'done', op: '='}]}}}]})
		expect(validateDraft(draft, catalog, t).map(i => i.path)).toEqual(['actions[0].params.condition.all[0].value'])
		const none = draftWith({actions: [{type: 'flow.stop_if', params: {}}]})
		expect(validateDraft(none, catalog, t).map(i => i.path)).toEqual(['actions[0].params.condition'])
	})

	it('maps server issues onto known inputs and the nearest ancestor', () => {
		const draft = draftWith({
			conditions: {all: [{field: 'priority', op: '>=', value: 3}]},
			actions: [{type: 'task.add_comment', params: {text: 'a'}}, {type: 'task.set_status', params: {status_id: 9}}],
		})
		const known = knownPaths(draft, catalog)
		const {byPath, unmapped} = mapIssues([
			{path: 'actions[1].params.status_id', message: 'status does not exist'},
			{path: 'conditions.all[0].value', message: 'bad value'},
			{path: 'conditions.all[0].value[2]', message: 'nested'},
			{path: 'actions[5].params.x', message: 'gone'},
			{path: 'trigger.options.nothing', message: 'unknown option'},
			{path: '', message: 'general'},
		], known)
		expect(byPath.get('actions[1].params.status_id')).toEqual(['status does not exist'])
		expect(byPath.get('conditions.all[0].value')).toEqual(['bad value', 'nested'])
		expect(byPath.get('trigger')).toEqual(['unknown option'])
		expect(byPath.get('actions')).toEqual(['gone'])
		expect(unmapped).toEqual([{path: '', message: 'general'}])
	})

	it('knows the paths of embedded conditions and trigger options', () => {
		const draft = draftWith({
			trigger: {type: 'task.status_changed', options: {}},
			actions: [{type: 'flow.stop_if', params: {condition: {any: [{field: 'done', op: '=', value: true}]}}}],
		})
		const known = knownPaths(draft, catalog)
		expect(known.has('trigger.options.to_status_id')).toBe(true)
		expect(known.has('actions[0].params.condition.any[0].value')).toBe(true)
	})

	it('reads the issues of a problem response', () => {
		expect(issuesFromProblem({details: {issues: [{path: 'a', message: 'b'}, {path: 'c'}]}})).toEqual([{path: 'a', message: 'b'}])
		expect(issuesFromProblem({details: {}})).toBeNull()
		expect(issuesFromProblem(null)).toBeNull()
	})

	it('builds DOM ids from paths', () => {
		expect(pathToId('actions[1].params.status_id')).toBe('automation-actions-1-params-status_id')
		expect(pathToId('conditions.all[0].value')).toBe('automation-conditions-all-0-value')
	})
})

describe('summary and humanize', () => {
	it('summarizes from catalog titles', () => {
		const draft = draftWith({
			trigger: {type: 'task.created', options: {}},
			conditions: {all: [{field: 'done', op: '=', value: true}, {any: [{field: 'priority', op: '>', value: 1}]}]},
			actions: [{type: 'task.set_field', params: {field: 'priority', value: 3}}, {type: 'task.add_comment', params: {text: 'x'}}],
		})
		const text = summarizeRule(draft, catalog, t, () => true)
		expect(text).toContain('automation.summary.line')
		expect(text).toContain('automation.trigger.task_created')
		expect(text).toContain('automation.action.set_field (automation.enum.priority), automation.action.add_comment')
		expect(text).toContain('"count":2')
	})

	it('falls back to humanized names for unknown types', () => {
		const text = summarizeRule(draftWith({trigger: {type: 'task.brand_new', options: {}}, actions: [{type: 'x.do_thing', params: {}}]}), catalog, t, te)
		expect(text).toContain('Brand new')
		expect(text).toContain('Do thing')
		expect(humanize('task.set_custom_field')).toBe('Set custom field')
		expect(humanize('event.old.value')).toBe('Event old value')
	})
})

describe('date and duration syntax', () => {
	it('mirrors the server for dates', () => {
		for (const ok of ['+3d', '-2h', 'none', 'now', 'now+3d', '2026-12-31', '2026-12-31T10:00:00Z']) expect(isValidDateSpec(ok, true), ok).toBe(true)
		for (const bad of ['', 'tomorrow', '3d', '+3x', '2026-13-45']) expect(isValidDateSpec(bad, true), bad).toBe(false)
		expect(isValidDateSpec('+3d', false)).toBe(false)
		expect(isValidDateSpec('none', false)).toBe(false)
		expect(isValidDateSpec('now-12h', false)).toBe(true)
	})

	it('validates offsets', () => {
		expect(isValidOffset('3d')).toBe(true)
		expect(isValidOffset('+12h')).toBe(true)
		expect(isValidOffset('3 days')).toBe(false)
	})
})

describe('template presets', () => {
	const ctx = {
		catalog,
		statuses: [{id: 3, title: 'Done', category: 'done'}],
		customFields: [{id: 1, title: 'Print run', type: 'number'}, {id: 2, title: 'Checked', type: 'checkbox'}],
	}

	it('are all valid against the catalog', () => {
		const templates = availableTemplates(ctx, t)
		expect(templates.map(tpl => tpl.id)).toEqual(RULE_TEMPLATES.map(tpl => tpl.id))
		for (const template of templates) {
			expect(validateDraft(template.draft, catalog, t), template.id).toEqual([])
			expect(template.draft.title).not.toBe('')
		}
	})

	it('only offers templates the project and the catalog can serve', () => {
		const ids = availableTemplates({...ctx, statuses: [], customFields: []}, t).map(tpl => tpl.id)
		expect(ids).not.toContain('field-on-done')
		expect(ids).toContain('assign-creator')

		const smaller = {...catalog, actions: catalog.actions.filter(a => a.type !== 'task.assign'), triggers: catalog.triggers.filter(x => x.type !== 'task.completed')}
		const offered = availableTemplates({...ctx, catalog: smaller}, t).map(tpl => tpl.id)
		expect(offered).not.toContain('assign-creator')
		expect(offered).not.toContain('comment-on-completed')
		expect(availableTemplates({...ctx, catalog: normalizeCatalog({})}, t)).toEqual([])
	})

	it('uses a checkbox field for the status template when there is one', () => {
		const tpl = availableTemplates(ctx, t).find(x => x.id === 'field-on-done')!
		expect(tpl.draft.actions[0].params).toEqual({field_id: 2, value: true})
		expect(tpl.draft.trigger.options).toEqual({to_status_id: 3})
	})
})

describe('script reference from the catalog', () => {
	it('reads the script object: conventions, limits and api', () => {
		const catalog = normalizeCatalog({
			triggers: [], fields: [], operators: [], actions: [],
			script: {
				language: 'javascript',
				action_convention: 'Use return.',
				condition_convention: 'Return a boolean.',
				limits: [{name: 'Script timeout', config_key: 'automation.scripts.timeout', value: '5s', description: 'Stops it.'}],
				api: [
					{name: 'vikunja.tasks.get', signature: 'vikunja.tasks.get(id) -> task', description: 'Loads a task.', mutating: false, in_condition: true},
					{name: 'vikunja.tasks.update', signature: '', description: 'Changes a task.', mutating: true, in_condition: false},
				],
			},
		})
		expect(catalog.script?.actionConvention).toBe('Use return.')
		expect(catalog.script?.conditionConvention).toBe('Return a boolean.')
		expect(catalog.script?.limits).toEqual([{name: 'Script timeout', configKey: 'automation.scripts.timeout', value: '5s', description: 'Stops it.'}])
		expect(catalog.script?.api).toEqual([
			{name: 'vikunja.tasks.get', signature: 'vikunja.tasks.get(id) -> task', description: 'Loads a task.', mutating: false, inCondition: true},
			{name: 'vikunja.tasks.update', signature: 'vikunja.tasks.update', description: 'Changes a task.', mutating: true, inCondition: false},
		])
		expect(catalog.supportsScript).toBe(true)
	})

	it('offers no scripts without a script object or when the instance switch is off', () => {
		const none = normalizeCatalog({triggers: [], fields: [], operators: [], actions: []})
		expect(none.script).toBeNull()
		expect(none.supportsScript).toBe(false)
		const off = normalizeCatalog({
			triggers: [], fields: [], operators: [],
			actions: [{type: 'script.run', params: [{name: 'source', kind: 'code'}]}, {type: 'task.assign', params: []}],
			script: {limits: [{name: 'Scripts enabled', config_key: 'automation.scripts.enabled', value: 'false', description: ''}], api: []},
		})
		expect(off.supportsScript).toBe(false)
		expect(offeredActions(off).map(a => a.type)).toEqual(['task.assign'])
		expect(offeredActions(off, 'script.run').map(a => a.type)).toEqual(['script.run', 'task.assign'])
	})

	it('maps the line and column of a syntax error onto its path', () => {
		const issues = issuesFromProblem({details: {issues: [{path: 'actions[0].params.source', message: 'syntax error: Unexpected token ;', line: 2, column: 9}]}})
		expect(issues).toEqual([{path: 'actions[0].params.source', message: 'syntax error: Unexpected token ;', line: 2, column: 9}])
		const mapped = mapIssues(issues!, new Set(['actions[0].params.source']))
		expect(mapped.lines.get('actions[0].params.source')).toEqual({line: 2, column: 9})
	})
})
