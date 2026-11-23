import {cronProblem} from '@/helpers/automationCron'

// Pure helpers of the automation editor: the shapes of the catalog and of a rule document, the draft
// operations (conditions tree, actions), serialization, validation and the mapping of server issues onto inputs.
// Apart from the few server rules mirrored in validateSpecial, nothing in here knows a concrete trigger, field or
// action: everything comes from the catalog.

export type ParamKindBy = {param: string, kinds: Record<string, string>}

export type CatalogParam = {
	name: string
	kind: string
	required: boolean
	multiple: boolean
	enum: string[]
	title_key: string
	kind_by: ParamKindBy | null
}

export type CatalogTrigger = {type: string, title_key: string, event: string, options: CatalogParam[]}
export type CatalogAction = {type: string, title_key: string, params: CatalogParam[]}
export type CatalogField = {
	name: string
	pattern: string
	kind: string
	multiple: boolean
	enum: string[]
	operators: string[]
	title_key: string
	title: string
	custom_field_type: string
}
export type CatalogOperator = {type: string, title_key: string, no_value: boolean, list: boolean}

export type CatalogResponse = {
	triggers: CatalogTrigger[]
	fields: CatalogField[]
	operators: CatalogOperator[]
	actions: CatalogAction[]
	placeholders: string[]
	// The script reference of the backend (conventions, limits, API), or null when it sends none.
	script: ScriptDoc | null
	// True when scripts can be written: the catalog has a script reference and the instance has them enabled.
	supportsScript: boolean
}

export type ScriptApiEntry = {
	name: string
	signature: string
	description: string
	// Changes something (not available in a condition script).
	mutating: boolean
	// Available in a condition script.
	inCondition: boolean
}
export type ScriptLimit = {name: string, configKey: string, value: string, description: string}
export type ScriptDoc = {
	language: string
	actionConvention: string
	conditionConvention: string
	limits: ScriptLimit[]
	api: ScriptApiEntry[]
}

// The config key of the instance switch that turns scripts off; its value is a limit row of the reference.
export const SCRIPTS_ENABLED_KEY = 'automation.scripts.enabled'

type Json = Record<string, unknown>

function str(value: unknown): string {
	return typeof value === 'string' ? value : ''
}

function strList(value: unknown): string[] {
	return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []
}

export function normalizeParam(raw: Json): CatalogParam {
	const kindBy = raw.kind_by as Json | null | undefined
	return {
		name: str(raw.name),
		kind: str(raw.kind),
		required: raw.required === true,
		multiple: raw.multiple === true,
		enum: strList(raw.enum),
		title_key: str(raw.title_key),
		kind_by: kindBy && typeof kindBy === 'object'
			? {param: str(kindBy.param), kinds: (kindBy.kinds ?? {}) as Record<string, string>}
			: null,
	}
}

function params(value: unknown): CatalogParam[] {
	return Array.isArray(value) ? (value as Json[]).map(normalizeParam) : []
}

function normalizeScript(raw: Json): ScriptDoc | null {
	const doc = raw.script
	if (!doc || typeof doc !== 'object' || Array.isArray(doc)) return null
	const d = doc as Json
	const list = (value: unknown): Json[] => Array.isArray(value)
		? (value as unknown[]).filter((item): item is Json => !!item && typeof item === 'object')
		: []
	return {
		language: str(d.language) || 'javascript',
		actionConvention: str(d.action_convention),
		conditionConvention: str(d.condition_convention),
		limits: list(d.limits).map(l => ({
			name: str(l.name),
			configKey: str(l.config_key),
			value: str(l.value),
			description: str(l.description),
		})),
		api: list(d.api)
			.map(entry => ({
				name: str(entry.name),
				signature: str(entry.signature) || str(entry.name),
				description: str(entry.description),
				mutating: entry.mutating === true,
				inCondition: entry.in_condition !== false,
			}))
			.filter(entry => entry.name !== ''),
	}
}

// True for an action that runs a script (it has a `code` parameter).
export function isScriptAction(def: CatalogAction | undefined): boolean {
	return !!def && def.params.some(param => param.kind === 'code')
}

// The actions to offer: script actions only when scripts are available (an existing one stays in the list of its card).
export function offeredActions(catalog: CatalogResponse, keep = ''): CatalogAction[] {
	return catalog.supportsScript ? catalog.actions : catalog.actions.filter(a => a.type === keep || !isScriptAction(a))
}

// The generated type marks everything optional; this is the shape the editor relies on.
export function normalizeCatalog(raw: Json): CatalogResponse {
	const triggers = ((raw.triggers ?? []) as Json[]).map(t => ({
		type: str(t.type),
		title_key: str(t.title_key),
		event: str(t.event),
		options: params(t.options),
	}))
	const actions = ((raw.actions ?? []) as Json[]).map(a => ({
		type: str(a.type),
		title_key: str(a.title_key),
		params: params(a.params),
	}))
	const fields = ((raw.fields ?? []) as Json[]).map(f => ({
		name: str(f.name),
		pattern: str(f.pattern),
		kind: str(f.kind),
		multiple: f.multiple === true,
		enum: strList(f.enum),
		operators: strList(f.operators),
		title_key: str(f.title_key),
		title: str(f.title),
		custom_field_type: str(f.custom_field_type),
	}))
	const operators = ((raw.operators ?? []) as Json[]).map(o => ({
		type: str(o.type),
		title_key: str(o.title_key),
		no_value: o.no_value === true,
		list: o.list === true,
	}))
	const script = normalizeScript(raw)
	return {
		triggers,
		fields,
		operators,
		actions,
		placeholders: strList(raw.placeholders),
		script,
		// A backend that lists the switch with the value "false" has scripts turned off.
		supportsScript: script !== null
			&& script.limits.find(limit => limit.configKey === SCRIPTS_ENABLED_KEY)?.value !== 'false',
	}
}

// ---------------------------------------------------------------------------------------------------------------
// Rule document

export type ConditionNode = {
	all?: ConditionNode[]
	any?: ConditionNode[]
	not?: ConditionNode[]
	field?: string
	op?: string
	value?: unknown
	// Script condition (3b). Kept as is, the editor only renders a code box for it.
	script?: string
}

export type ActionDraft = {type: string, params: Record<string, unknown>}

export type RuleDraft = {
	title: string
	description: string
	enabled: boolean
	trigger: {type: string, options: Record<string, unknown>}
	conditions: ConditionNode | null
	actions: ActionDraft[]
}

export type RuleDocument = {
	title: string
	description: string
	enabled: boolean
	trigger: {type: string, options?: Record<string, unknown>}
	conditions?: ConditionNode
	actions: {type: string, params?: Record<string, unknown>}[]
}

export function clone<T>(value: T): T {
	return value === undefined ? value : JSON.parse(JSON.stringify(value)) as T
}

export function emptyDraft(): RuleDraft {
	return {title: '', description: '', enabled: true, trigger: {type: '', options: {}}, conditions: null, actions: []}
}

export function draftFromRule(rule: {
	title?: string
	description?: string
	enabled?: boolean
	trigger?: {type?: string, options?: Record<string, unknown> | null}
	conditions?: ConditionNode | null
	actions?: {type?: string, params?: Record<string, unknown> | null}[] | null
}): RuleDraft {
	return {
		title: rule.title ?? '',
		description: rule.description ?? '',
		enabled: rule.enabled ?? false,
		trigger: {type: rule.trigger?.type ?? '', options: clone(rule.trigger?.options ?? {})},
		conditions: rule.conditions && isNonEmptyNode(rule.conditions) ? asGroup(clone(rule.conditions)) : null,
		actions: (rule.actions ?? []).map(a => ({type: a.type ?? '', params: clone(a.params ?? {})})),
	}
}

// A bare comparison at the root becomes a group of one, so the builder can add siblings.
function asGroup(node: ConditionNode): ConditionNode {
	return groupKind(node) ? node : {all: [node]}
}

export function isEmptyValue(value: unknown): boolean {
	if (value === undefined || value === null) return true
	if (typeof value === 'string') return value.trim() === ''
	if (Array.isArray(value)) return value.length === 0
	return false
}

function pruneParams(values: Record<string, unknown>): Record<string, unknown> {
	const out: Record<string, unknown> = {}
	for (const [key, value] of Object.entries(values)) {
		if (isEmptyValue(value)) continue
		out[key] = value
	}
	return out
}

export function groupKind(node: ConditionNode): 'all' | 'any' | 'not' | null {
	if (Array.isArray(node.all)) return 'all'
	if (Array.isArray(node.any)) return 'any'
	if (Array.isArray(node.not)) return 'not'
	return null
}

export function isScriptNode(node: ConditionNode): boolean {
	return typeof node.script === 'string'
}

export function childrenOf(node: ConditionNode): ConditionNode[] {
	const kind = groupKind(node)
	return kind ? node[kind] ?? [] : []
}

function isNonEmptyNode(node: ConditionNode): boolean {
	const kind = groupKind(node)
	if (kind) return (node[kind] ?? []).length > 0
	return true
}

// Drops groups without children and comparisons the server would reject for being empty groups. Incomplete
// comparisons are kept: validation reports them.
export function pruneConditions(node: ConditionNode | null): ConditionNode | null {
	if (!node) return null
	const kind = groupKind(node)
	if (!kind) return clone(node)
	const children = (node[kind] ?? [])
		.map(child => pruneConditions(child))
		.filter((child): child is ConditionNode => child !== null)
	if (children.length === 0) return null
	return {[kind]: children}
}

export function draftToDocument(draft: RuleDraft): RuleDocument {
	const options = pruneParams(draft.trigger.options)
	const conditions = pruneConditions(draft.conditions)
	return {
		title: draft.title.trim(),
		description: draft.description,
		enabled: draft.enabled,
		trigger: {type: draft.trigger.type, ...(Object.keys(options).length > 0 ? {options} : {})},
		...(conditions ? {conditions: serializeCondition(conditions)} : {}),
		actions: draft.actions.map(action => {
			const p = serializeParams(action.params)
			return {type: action.type, ...(Object.keys(p).length > 0 ? {params: p} : {})}
		}),
	}
}

function serializeParams(values: Record<string, unknown>): Record<string, unknown> {
	const out = pruneParams(values)
	for (const [key, value] of Object.entries(out)) {
		// A condition parameter (flow.stop_if) is a condition tree itself.
		if (isConditionLike(value)) {
			const pruned = pruneConditions(value)
			if (pruned) out[key] = serializeCondition(pruned)
			else delete out[key]
		}
	}
	return out
}

function isConditionLike(value: unknown): value is ConditionNode {
	if (!value || typeof value !== 'object' || Array.isArray(value)) return false
	const node = value as ConditionNode
	return groupKind(node) !== null || typeof node.field === 'string' || typeof node.script === 'string'
}

function serializeCondition(node: ConditionNode): ConditionNode {
	const kind = groupKind(node)
	if (kind) return {[kind]: (node[kind] ?? []).map(serializeCondition)}
	if (isScriptNode(node)) return {script: node.script}
	const out: ConditionNode = {field: node.field, op: node.op}
	if (!isEmptyValue(node.value) || typeof node.value === 'boolean' || typeof node.value === 'number') {
		if (node.op !== 'is_empty' && node.op !== 'is_not_empty') out.value = node.value
	}
	return out
}

export function draftSignature(draft: RuleDraft): string {
	return JSON.stringify(draftToDocument(draft))
}

// ---------------------------------------------------------------------------------------------------------------
// Condition tree operations. A path is the list of child indexes from the root.

export type NodePath = number[]

export function getNode(root: ConditionNode | null, path: NodePath): ConditionNode | null {
	let node = root
	for (const index of path) {
		if (!node) return null
		node = childrenOf(node)[index] ?? null
	}
	return node
}

function mapAt(root: ConditionNode, path: NodePath, fn: (node: ConditionNode) => ConditionNode | null): ConditionNode | null {
	if (path.length === 0) return fn(root)
	const kind = groupKind(root)
	if (!kind) return root
	const [head, ...rest] = path
	const children = [...(root[kind] ?? [])]
	if (head < 0 || head >= children.length) return root
	const replaced = mapAt(children[head], rest, fn)
	if (replaced === null) children.splice(head, 1)
	else children[head] = replaced
	return {[kind]: children}
}

export function newComparison(field = '', op = ''): ConditionNode {
	return {field, op}
}

export function newGroup(kind: 'all' | 'any' | 'not' = 'all', children: ConditionNode[] = []): ConditionNode {
	return {[kind]: children}
}

// Adds a child to the group at path. A missing tree becomes an "all" group.
export function addChild(root: ConditionNode | null, path: NodePath, child: ConditionNode): ConditionNode {
	const base = root ?? newGroup('all')
	return mapAt(base, path, node => {
		const kind = groupKind(node)
		if (!kind) return node
		return {[kind]: [...(node[kind] ?? []), child]}
	}) ?? base
}

// Removes the node at path. Removing the root, or the last child of the root, gives null ("always").
export function removeNode(root: ConditionNode | null, path: NodePath): ConditionNode | null {
	if (!root) return null
	if (path.length === 0) return null
	const result = mapAt(root, path, () => null)
	if (!result) return null
	return childrenOf(result).length === 0 && groupKind(result) ? null : result
}

export function replaceNode(root: ConditionNode | null, path: NodePath, next: ConditionNode): ConditionNode {
	if (!root || path.length === 0) return next
	return mapAt(root, path, () => next) ?? next
}

export function setGroupKind(root: ConditionNode, path: NodePath, kind: 'all' | 'any' | 'not'): ConditionNode {
	return mapAt(root, path, node => {
		const current = groupKind(node)
		if (!current) return node
		return {[kind]: node[current] ?? []}
	}) ?? root
}

// Wraps the node at path in a new group of the given kind.
export function wrapNode(root: ConditionNode, path: NodePath, kind: 'all' | 'any' | 'not'): ConditionNode {
	return mapAt(root, path, node => newGroup(kind, [node])) ?? root
}

export function countNodes(node: ConditionNode | null): number {
	if (!node) return 0
	return 1 + childrenOf(node).reduce((sum, child) => sum + countNodes(child), 0)
}

export function hasScriptNode(node: ConditionNode | null): boolean {
	if (!node) return false
	return isScriptNode(node) || childrenOf(node).some(hasScriptNode)
}

// ---------------------------------------------------------------------------------------------------------------
// Action list operations

export function moveItem<T>(list: T[], index: number, delta: -1 | 1): T[] {
	const target = index + delta
	if (index < 0 || index >= list.length || target < 0 || target >= list.length) return list
	const copy = [...list]
	;[copy[index], copy[target]] = [copy[target], copy[index]]
	return copy
}

export function duplicateAction(actions: ActionDraft[], index: number): ActionDraft[] {
	if (index < 0 || index >= actions.length) return actions
	const copy = [...actions]
	copy.splice(index + 1, 0, clone(actions[index]))
	return copy
}

export function removeAction(actions: ActionDraft[], index: number): ActionDraft[] {
	return actions.filter((_, i) => i !== index)
}

// ---------------------------------------------------------------------------------------------------------------
// Catalog lookups

export function actionDef(catalog: CatalogResponse, type: string): CatalogAction | undefined {
	return catalog.actions.find(a => a.type === type)
}

export function triggerDef(catalog: CatalogResponse, type: string): CatalogTrigger | undefined {
	return catalog.triggers.find(t => t.type === type)
}

export function fieldDef(catalog: CatalogResponse, name: string): CatalogField | undefined {
	return catalog.fields.find(f => f.name === name && f.pattern === '')
}

export function customFieldIdOf(name: string): number | null {
	const match = /^cf_([1-9][0-9]*)$/.exec(name)
	return match ? Number(match[1]) : null
}

export function operatorDef(catalog: CatalogResponse, type: string): CatalogOperator | undefined {
	return catalog.operators.find(o => o.type === type)
}

// Operators of a field, in the order of the catalog operator list.
export function operatorsFor(catalog: CatalogResponse, field: CatalogField | undefined): CatalogOperator[] {
	if (!field) return []
	return catalog.operators.filter(o => field.operators.includes(o.type))
}

export type FieldGroup = 'task' | 'status' | 'custom' | 'event'

export function fieldGroupOf(field: CatalogField): FieldGroup {
	if (field.name.startsWith('event.')) return 'event'
	if (field.name.startsWith('cf_')) return 'custom'
	if (field.name === 'status' || field.name === 'status_category') return 'status'
	return 'task'
}

// The kind a parameter currently has, resolving kind_by from its siblings.
export function effectiveKind(param: CatalogParam, siblings: Record<string, unknown>): string | null {
	if (!param.kind_by) return param.kind
	const sibling = siblings[param.kind_by.param]
	if (typeof sibling !== 'string') return null
	return param.kind_by.kinds[sibling] ?? null
}

// Fresh params for an action type: nothing set, only enum params with one candidate keep their default.
export function defaultParams(def: CatalogAction | undefined): Record<string, unknown> {
	const out: Record<string, unknown> = {}
	if (!def) return out
	for (const param of def.params) {
		if (param.kind === 'bool' && param.required) out[param.name] = true
	}
	return out
}

// Keeps the values of params the new action type shares by name and kind.
export function changeActionType(action: ActionDraft, next: CatalogAction | undefined, previous: CatalogAction | undefined): ActionDraft {
	const params = defaultParams(next)
	if (next && previous) {
		for (const param of next.params) {
			const before = previous.params.find(p => p.name === param.name && p.kind === param.kind)
			if (before && action.params[param.name] !== undefined) params[param.name] = action.params[param.name]
		}
	}
	return {type: next?.type ?? action.type, params}
}

// ---------------------------------------------------------------------------------------------------------------
// Validation

export type ScriptPosition = {line: number, column: number}
export type Issue = {path: string, message: string, line?: number, column?: number}

export type Translate = (key: string, params?: Record<string, unknown>) => string

// A condition leaf is complete when it has a field, an operator and (when the operator takes one) a value.
function validateNode(node: ConditionNode, path: string, catalog: CatalogResponse, t: Translate, out: Issue[]) {
	const kind = groupKind(node)
	if (kind) {
		;(node[kind] ?? []).forEach((child, i) => validateNode(child, `${path}.${kind}[${i}]`, catalog, t, out))
		return
	}
	if (isScriptNode(node)) {
		if ((node.script ?? '').trim() === '') out.push({path: `${path}.script`, message: t('automation.validation.required')})
		return
	}
	if (!node.field) {
		out.push({path: `${path}.field`, message: t('automation.validation.required')})
		return
	}
	if (!node.op) {
		out.push({path: `${path}.op`, message: t('automation.validation.required')})
		return
	}
	const op = operatorDef(catalog, node.op)
	if (op && !op.no_value && isEmptyValue(node.value) && typeof node.value !== 'boolean' && typeof node.value !== 'number') {
		out.push({path: `${path}.value`, message: t('automation.validation.required')})
	}
}

function validateParams(
	defs: CatalogParam[],
	values: Record<string, unknown>,
	path: string,
	catalog: CatalogResponse,
	t: Translate,
	out: Issue[],
) {
	for (const param of defs) {
		const value = values[param.name]
		if (param.kind === 'condition') {
			if (value && isConditionLike(value)) validateNode(value, `${path}.${param.name}`, catalog, t, out)
			if (param.required && !pruneConditions(isConditionLike(value) ? value : null)) {
				out.push({path: `${path}.${param.name}`, message: t('automation.validation.required')})
			}
			continue
		}
		if (param.required && isEmptyValue(value) && typeof value !== 'boolean' && typeof value !== 'number') {
			out.push({path: `${path}.${param.name}`, message: t('automation.validation.required')})
		}
	}
}

// The seconds-free length of a duration like 12h or -2w in minutes, or null when it is no duration.
function offsetMinutes(value: unknown): number | null {
	if (typeof value !== 'string') return null
	const match = /^([+-]?)(\d{1,6})([mhdw])$/.exec(value.trim())
	if (!match) return null
	const unit = {m: 1, h: 60, d: 60 * 24, w: 60 * 24 * 7}[match[3] as 'm' | 'h' | 'd' | 'w']
	return (match[1] === '-' ? -1 : 1) * Number(match[2]) * unit
}

// The shortest wait and the shortest "due soon" offset the server accepts.
const MIN_MINUTES = 1

// What the generic checks cannot see and the server checks for a few types (spec.triggerRules / actionRules): a
// valid cron expression, durations of at least a minute and somebody to notify.
function validateSpecial(
	kind: 'trigger' | 'action',
	type: string,
	values: Record<string, unknown>,
	path: string,
	t: Translate,
	out: Issue[],
) {
	const has = (name: string) => !isEmptyValue(values[name])
	if (kind === 'trigger' && type === 'schedule' && typeof values.cron === 'string' && has('cron')) {
		const problem = cronProblem(values.cron)
		if (problem) out.push({path: `${path}.cron`, message: t(`automation.validation.cron.${problem}`)})
	}
	const duration = (name: string) => {
		if (!has(name)) return
		const minutes = offsetMinutes(values[name])
		if (minutes === null) out.push({path: `${path}.${name}`, message: t('automation.validation.duration')})
		else if (minutes < MIN_MINUTES) out.push({path: `${path}.${name}`, message: t('automation.validation.minDuration')})
	}
	if (kind === 'trigger' && type === 'task.due_soon') duration('offset')
	if (kind === 'action' && type === 'wait') duration('duration')
	if (kind === 'action' && type === 'notify' && !has('recipients') && !has('team_id')) {
		out.push({path: `${path}.recipients`, message: t('automation.validation.recipients')})
	}
}

// Client-side checks that mirror the structural rules of the server; the server remains the authority.
export function validateDraft(draft: RuleDraft, catalog: CatalogResponse, t: Translate): Issue[] {
	const out: Issue[] = []
	if (draft.title.trim() === '') out.push({path: 'title', message: t('automation.validation.required')})
	const trigger = triggerDef(catalog, draft.trigger.type)
	if (!draft.trigger.type) out.push({path: 'trigger.type', message: t('automation.validation.required')})
	else if (trigger) {
		validateParams(trigger.options, draft.trigger.options, 'trigger.options', catalog, t, out)
		validateSpecial('trigger', trigger.type, draft.trigger.options, 'trigger.options', t, out)
	}

	const conditions = pruneConditions(draft.conditions)
	if (conditions) validateNode(conditions, 'conditions', catalog, t, out)

	if (draft.actions.length === 0) out.push({path: 'actions', message: t('automation.validation.atLeastOneAction')})
	draft.actions.forEach((action, i) => {
		const path = `actions[${i}]`
		if (!action.type) {
			out.push({path: `${path}.type`, message: t('automation.validation.required')})
			return
		}
		const def = actionDef(catalog, action.type)
		if (def) {
			validateParams(def.params, action.params, `${path}.params`, catalog, t, out)
			validateSpecial('action', def.type, action.params, `${path}.params`, t, out)
		}
	})
	return out
}

// ---------------------------------------------------------------------------------------------------------------
// Error paths

// The paths for which the editor renders a message slot / an input with an id.
export function knownPaths(draft: RuleDraft, catalog: CatalogResponse): Set<string> {
	const paths = new Set<string>(['title', 'description', 'trigger', 'trigger.type', 'conditions', 'actions'])
	const trigger = triggerDef(catalog, draft.trigger.type)
	for (const option of trigger?.options ?? []) paths.add(`trigger.options.${option.name}`)

	const addNode = (node: ConditionNode | null, path: string) => {
		if (!node) return
		paths.add(path)
		const kind = groupKind(node)
		if (kind) {
			;(node[kind] ?? []).forEach((child, i) => addNode(child, `${path}.${kind}[${i}]`))
			return
		}
		if (isScriptNode(node)) {
			paths.add(`${path}.script`)
			return
		}
		paths.add(`${path}.field`)
		paths.add(`${path}.op`)
		paths.add(`${path}.value`)
	}
	addNode(pruneConditions(draft.conditions), 'conditions')
	paths.add('conditions')

	draft.actions.forEach((action, i) => {
		const path = `actions[${i}]`
		paths.add(path)
		paths.add(`${path}.type`)
		for (const param of actionDef(catalog, action.type)?.params ?? []) {
			paths.add(`${path}.params.${param.name}`)
			if (param.kind === 'condition') {
				const value = action.params[param.name]
				addNode(isConditionLike(value) ? pruneConditions(value) : null, `${path}.params.${param.name}`)
			}
		}
	})
	return paths
}

function parentPath(path: string): string {
	const bracket = path.lastIndexOf('[')
	const dot = path.lastIndexOf('.')
	const cut = Math.max(bracket, dot)
	return cut <= 0 ? '' : path.slice(0, cut)
}

// Server paths address the pruned document, which can differ from the draft after pruning empty groups. The
// serialized form is what the server saw, so indexes line up with knownPaths (which also uses the pruned tree).
export function mapIssues(issues: Issue[], known: Set<string>): {byPath: Map<string, string[]>, lines: Map<string, ScriptPosition>, unmapped: Issue[]} {
	const byPath = new Map<string, string[]>()
	const lines = new Map<string, ScriptPosition>()
	const unmapped: Issue[] = []
	for (const issue of issues) {
		let path = issue.path
		while (path !== '' && !known.has(path)) path = parentPath(path)
		if (path === '') {
			unmapped.push(issue)
			continue
		}
		byPath.set(path, [...(byPath.get(path) ?? []), issue.message])
		if (issue.line !== undefined && path === issue.path && !lines.has(path)) {
			lines.set(path, {line: issue.line, column: issue.column ?? 1})
		}
	}
	return {byPath, lines, unmapped}
}

// Id of the DOM element that carries the input for a path.
export function pathToId(path: string): string {
	return 'automation-' + path.replace(/\[(\d+)\]/g, '-$1').replace(/[^A-Za-z0-9_-]+/g, '-')
}

export function issuesFromProblem(problem: unknown): Issue[] | null {
	if (!problem || typeof problem !== 'object') return null
	const details = (problem as {details?: unknown}).details
	const raw = details && typeof details === 'object' ? (details as {issues?: unknown}).issues : undefined
	if (!Array.isArray(raw)) return null
	return raw
		.filter((item): item is {path?: unknown, message?: unknown, line?: unknown, column?: unknown} => !!item && typeof item === 'object')
		.map(item => {
			const issue: Issue = {path: str(item.path), message: str(item.message)}
			// A syntax error of a script carries the position in the source.
			if (typeof item.line === 'number' && item.line > 0) issue.line = item.line
			if (typeof item.column === 'number' && item.column > 0) issue.column = item.column
			return issue
		})
		.filter(item => item.message !== '')
}

// ---------------------------------------------------------------------------------------------------------------
// Keys and labels

const HUMANIZE_CACHE = new Map<string, string>()

// "task.set_custom_field" -> "Set custom field"; used when a translation is missing (newer backend).
export function humanize(name: string): string {
	const cached = HUMANIZE_CACHE.get(name)
	if (cached !== undefined) return cached
	const last = name.includes('.') && !name.startsWith('event.') ? name.slice(name.indexOf('.') + 1) : name
	const words = last.replace(/[._]+/g, ' ').replace(/<id>/g, '').trim()
	const result = words === '' ? name : words.charAt(0).toUpperCase() + words.slice(1)
	HUMANIZE_CACHE.set(name, result)
	return result
}

// Translates a catalog title key; falls back to a humanized form of the type name.
export function catalogLabel(t: Translate, te: (key: string) => boolean, titleKey: string, fallbackName: string): string {
	if (titleKey && te(titleKey)) return t(titleKey)
	return humanize(fallbackName)
}

export function enumLabel(t: Translate, te: (key: string) => boolean, value: string): string {
	const key = `automation.enum.${value}`
	return te(key) ? t(key) : humanize(value)
}

// Every translation key the catalog can emit, for the translation completeness test.
export function catalogTitleKeys(catalog: CatalogResponse): string[] {
	const keys = new Set<string>()
	const add = (key: string) => key && keys.add(key)
	catalog.triggers.forEach(trigger => {
		add(trigger.title_key)
		trigger.options.forEach(option => add(option.title_key))
	})
	catalog.actions.forEach(action => {
		add(action.title_key)
		action.params.forEach(param => add(param.title_key))
	})
	catalog.fields.forEach(field => add(field.title_key))
	catalog.operators.forEach(op => add(op.title_key))
	return [...keys]
}

// Every enum value the catalog lists (trigger options, params, fields), for the enum label test.
export function catalogEnumValues(catalog: CatalogResponse): string[] {
	const values = new Set<string>()
	const take = (list: string[]) => list.forEach(v => values.add(v))
	catalog.triggers.forEach(trigger => trigger.options.forEach(option => take(option.enum)))
	catalog.actions.forEach(action => action.params.forEach(param => take(param.enum)))
	catalog.fields.forEach(field => take(field.enum))
	return [...values]
}
