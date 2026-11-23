import {snakeCase} from 'change-case'

import type {CustomFieldType} from '@/helpers/customFields'
import {AVAILABLE_FILTER_FIELDS} from '@/helpers/filters'

/**
 * Custom fields in the filter query.
 *
 * The API addresses a custom field as `cf_<id>` and a select option by its id. The filter input
 * shows the field name and the option title instead:
 *
 *   display:  "Budget Q1" > 100 && Stage in Review, "Won, closed"
 *   API:      cf_12 > 100 && cf_4 in 31, 33
 *
 * Names that are plain words (letters, digits, underscore) are written as they are; everything
 * else is double quoted, with backslash escapes for `"` and `\`. A bare word that is also a
 * built-in filter field (`done`, `priority`, ...) always means the built-in field, so a custom
 * field with such a name has to be quoted. Quoted names always mean custom fields.
 *
 * Fields that share a name (case-insensitive) cannot be told apart by it. Their display names
 * get the project title appended, `Budget (Marketing)`, and when that is still not unique the
 * field id, `Budget (Marketing) #12`. The plain name of such a field is not resolved, so the
 * API reports it as an unknown field instead of silently picking one.
 */

export interface CustomFieldFilterOption {
	id: number
	title: string
}

export interface CustomFieldFilterRef {
	id: number
	title: string
	type: CustomFieldType
	projectId: number
	// The unique name that is shown and typed in the filter.
	name: string
	options: CustomFieldFilterOption[]
}

export interface CustomFieldFilterSource {
	id: number
	project_id: number
	title: string
	type: CustomFieldType
	position: number
	options: CustomFieldFilterOption[]
}

const BUILT_IN_NAMES = new Set([
	...AVAILABLE_FILTER_FIELDS,
	...AVAILABLE_FILTER_FIELDS.map(f => snakeCase(f)),
	'id',
	'title',
	'description',
	'project_id',
	'created_by_id',
	'repeat_after',
	'hex_color',
	'uid',
	'position',
	'bucket_id',
	'index',
].map(name => name.toLowerCase()))

const BARE_NAME = /^[\p{L}\p{N}_]+$/u
const RAW_IDENTIFIER = /^cf_\d+$/i

export function isBuiltInFieldName(name: string): boolean {
	return BUILT_IN_NAMES.has(name.toLowerCase())
}

/**
 * Gives every field a unique display name. The order is deterministic: project id, position, id.
 */
export function buildCustomFieldRefs(
	fields: readonly CustomFieldFilterSource[],
	projectTitle: (projectId: number) => string | undefined,
): CustomFieldFilterRef[] {
	const sorted = [...fields].sort((a, b) =>
		a.project_id - b.project_id || a.position - b.position || a.id - b.id)

	const count = (names: string[]) => {
		const counts = new Map<string, number>()
		names.forEach(name => counts.set(name.toLowerCase(), (counts.get(name.toLowerCase()) ?? 0) + 1))
		return counts
	}

	const titles = count(sorted.map(field => field.title.trim()))
	const withProject = sorted.map(field => {
		const title = field.title.trim()
		if ((titles.get(title.toLowerCase()) ?? 0) < 2) {
			return title
		}
		const project = projectTitle(field.project_id)
		return project ? `${title} (${project})` : `${title} (#${field.project_id})`
	})
	const qualified = count(withProject)

	return sorted.map((field, index) => ({
		id: field.id,
		title: field.title,
		type: field.type,
		projectId: field.project_id,
		name: (qualified.get(withProject[index].toLowerCase()) ?? 0) < 2
			? withProject[index]
			: `${withProject[index]} #${field.id}`,
		options: field.options.map(option => ({id: option.id, title: option.title})),
	}))
}

function escapeQuoted(value: string): string {
	return value.replaceAll('\\', '\\\\').replaceAll('"', '\\"')
}

function unquote(value: string): string {
	const trimmed = value.trim()
	const quote = trimmed[0]
	if (trimmed.length >= 2 && (quote === '"' || quote === '\'') && trimmed.endsWith(quote)) {
		return trimmed.slice(1, -1).replace(/\\(.)/g, '$1')
	}
	return trimmed
}

/** The way a field name is written in the filter input. */
export function formatCustomFieldName(name: string): string {
	if (BARE_NAME.test(name) && !isBuiltInFieldName(name) && !RAW_IDENTIFIER.test(name)) {
		return name
	}
	return `"${escapeQuoted(name)}"`
}

/** The way a select option title is written as a value in the filter input. */
export function formatCustomFieldValue(title: string, inList: boolean): string {
	if (/["'\\&|()]/.test(title) || (inList && title.includes(',')) || title !== title.trim()) {
		return `"${escapeQuoted(title)}"`
	}
	return title
}

const COMPARATORS = '!=|>=|<=|\\?!=|\\?=|=|>|<'
const WORD_OPERATORS = 'not\\s+in|in|like'
const EXPRESSION_HEAD = new RegExp(
	'(?:"((?:\\\\.|[^"\\\\])*)"|\'((?:\\\\.|[^\'\\\\])*)\'|([\\p{L}\\p{N}_]+))'
	+ `(?:\\s*(${COMPARATORS})|\\s+(${WORD_OPERATORS})\\b)`,
	'uy',
)

export interface FilterExpression {
	// The name as written, including quotes.
	nameStart: number
	nameEnd: number
	// The name without quotes and escapes.
	name: string
	quoted: boolean
	operator: string
	// The value, trimmed. Empty when nothing was typed after the operator yet.
	valueStart: number
	valueEnd: number
}

export interface FilterScan {
	expressions: FilterExpression[]
	// Where the last, possibly unfinished, segment of the filter starts.
	lastStart: number
}

function isMultiValue(operator: string): boolean {
	return /^(not\s+in|in|\?=|\?!=)$/.test(operator)
}

// The end of a quoted run that starts at i, or -1 if it is never closed (then it is an apostrophe).
function quotedRunEnd(text: string, i: number): number {
	const quote = text[i]
	for (let j = i + 1; j < text.length; j++) {
		if (text[j] === '\\') {
			j++
			continue
		}
		if (text[j] === quote) {
			return j + 1
		}
	}
	return -1
}

function isJoiner(text: string, i: number): boolean {
	return text.startsWith('&&', i) || text.startsWith('||', i) || text[i] === '(' || text[i] === ')'
}

/**
 * Splits a filter into its `name operator value` expressions. Only the start of an expression
 * is looked at (the beginning, or after `&&`, `||` or a parenthesis), so text inside values is
 * never taken for a field.
 */
export function scanFilterExpressions(text: string): FilterScan {
	const expressions: FilterExpression[] = []
	let i = 0
	let lastStart = 0

	const skipJoiners = () => {
		while (i < text.length && (/\s/.test(text[i]) || isJoiner(text, i))) {
			i += text.startsWith('&&', i) || text.startsWith('||', i) ? 2 : 1
		}
	}

	while (i < text.length) {
		skipJoiners()
		if (i >= text.length) {
			lastStart = text.length
			break
		}
		lastStart = i

		EXPRESSION_HEAD.lastIndex = i
		const head = EXPRESSION_HEAD.exec(text)
		if (head) {
			const quoted = head[1] !== undefined || head[2] !== undefined
			const rawName = head[1] ?? head[2] ?? head[3]
			const operator = (head[4] ?? head[5]).replace(/\s+/g, ' ')
			const nameLength = quoted ? rawName.length + 2 : rawName.length
			const nameStart = i
			const afterName = nameStart + nameLength

			let valueStart = i + head[0].length
			while (valueStart < text.length && /\s/.test(text[valueStart])) {
				valueStart++
			}
			let j = valueStart
			while (j < text.length && !isJoiner(text, j)) {
				if (text[j] === '"' || text[j] === '\'') {
					const end = quotedRunEnd(text, j)
					if (end > 0) {
						j = end
						continue
					}
				}
				j++
			}
			let valueEnd = j
			while (valueEnd > valueStart && /\s/.test(text[valueEnd - 1])) {
				valueEnd--
			}

			expressions.push({
				nameStart,
				nameEnd: afterName,
				name: quoted ? rawName.replace(/\\(.)/g, '$1') : rawName,
				quoted,
				operator,
				valueStart,
				valueEnd,
			})
			i = j
			continue
		}

		// Not an expression (free text, a typo): skip to the next joiner.
		while (i < text.length && !isJoiner(text, i)) {
			if (text[i] === '"' || text[i] === '\'') {
				const end = quotedRunEnd(text, i)
				if (end > 0) {
					i = end
					continue
				}
			}
			i++
		}
	}

	return {expressions, lastStart}
}

export interface ResolvedExpression {
	expression: FilterExpression
	field: CustomFieldFilterRef
}

class Lookup {
	private byName = new Map<string, CustomFieldFilterRef>()
	private byId = new Map<number, CustomFieldFilterRef>()

	constructor(refs: readonly CustomFieldFilterRef[]) {
		refs.forEach(ref => {
			this.byName.set(ref.name.toLowerCase(), ref)
			this.byId.set(ref.id, ref)
		})
	}

	resolve(expression: FilterExpression): CustomFieldFilterRef | undefined {
		if (!expression.quoted && isBuiltInFieldName(expression.name)) {
			return undefined
		}
		return this.byName.get(expression.name.toLowerCase())
	}

	resolveId(expression: FilterExpression): CustomFieldFilterRef | undefined {
		if (expression.quoted || !RAW_IDENTIFIER.test(expression.name)) {
			return undefined
		}
		return this.byId.get(Number(expression.name.slice(3)))
	}
}

/** The expressions of the text that name a known custom field. */
export function findCustomFieldExpressions(text: string, refs: readonly CustomFieldFilterRef[]): ResolvedExpression[] {
	if (refs.length === 0) {
		return []
	}
	const lookup = new Lookup(refs)
	return scanFilterExpressions(text).expressions.flatMap(expression => {
		const field = lookup.resolve(expression) ?? lookup.resolveId(expression)
		return field ? [{expression, field}] : []
	})
}

// Splits a value at the commas that are not inside quotes.
function splitValueItems(value: string): string[] {
	const items: string[] = []
	let current = ''
	for (let i = 0; i < value.length; i++) {
		const c = value[i]
		if (c === '"' || c === '\'') {
			const end = quotedRunEnd(value, i)
			if (end > 0) {
				current += value.slice(i, end)
				i = end - 1
				continue
			}
		}
		if (c === ',') {
			items.push(current)
			current = ''
			continue
		}
		current += c
	}
	items.push(current)
	return items.map(item => item.trim()).filter(item => item !== '')
}

function hasOptions(field: CustomFieldFilterRef): boolean {
	return field.type === 'select' || field.type === 'multi_select'
}

function convertValue(
	field: CustomFieldFilterRef,
	operator: string,
	value: string,
	convertItem: (item: string) => string,
): string {
	if (!hasOptions(field) || value === '') {
		return value
	}
	if (isMultiValue(operator)) {
		return splitValueItems(value).map(convertItem).join(', ')
	}
	return convertItem(value.trim())
}

interface Replacement {
	start: number
	end: number
	text: string
}

function applyReplacements(text: string, replacements: Replacement[]): string {
	let result = text
	;[...replacements].sort((a, b) => b.start - a.start).forEach(({start, end, text: replacement}) => {
		result = result.slice(0, start) + replacement + result.slice(end)
	})
	return result
}

/** Display form to API form: names become `cf_<id>`, option titles become ids. */
export function customFieldFilterToApi(text: string, refs: readonly CustomFieldFilterRef[]): string {
	const replacements: Replacement[] = []
	findCustomFieldExpressions(text, refs).forEach(({expression, field}) => {
		replacements.push({start: expression.nameStart, end: expression.nameEnd, text: `cf_${field.id}`})

		if (!hasOptions(field) || expression.valueEnd <= expression.valueStart) {
			return
		}
		const converted = convertValue(field, expression.operator,
			text.slice(expression.valueStart, expression.valueEnd),
			item => {
				const title = unquote(item).toLowerCase()
				const option = field.options.find(o => o.title.trim().toLowerCase() === title)
				return option ? String(option.id) : item
			})
		replacements.push({start: expression.valueStart, end: expression.valueEnd, text: converted})
	})
	return applyReplacements(text, replacements)
}

/** API form to display form: `cf_<id>` becomes the field name, option ids become titles. */
export function customFieldFilterFromApi(text: string, refs: readonly CustomFieldFilterRef[]): string {
	const replacements: Replacement[] = []
	findCustomFieldExpressions(text, refs).forEach(({expression, field}) => {
		if (expression.quoted || !RAW_IDENTIFIER.test(expression.name)) {
			return
		}
		replacements.push({start: expression.nameStart, end: expression.nameEnd, text: formatCustomFieldName(field.name)})

		if (!hasOptions(field) || expression.valueEnd <= expression.valueStart) {
			return
		}
		const inList = isMultiValue(expression.operator)
		const converted = convertValue(field, expression.operator,
			text.slice(expression.valueStart, expression.valueEnd),
			item => {
				const option = /^\d+$/.test(item) ? field.options.find(o => o.id === Number(item)) : undefined
				return option ? formatCustomFieldValue(option.title, inList) : item
			})
		replacements.push({start: expression.valueStart, end: expression.valueEnd, text: converted})
	})
	return applyReplacements(text, replacements)
}

export interface CustomFieldAutocompleteContext {
	kind: 'name' | 'value'
	field?: CustomFieldFilterRef
	operator?: string
	// What the user typed so far (without quote).
	search: string
	// The part of the text that a suggestion replaces.
	start: number
	end: number
	quoteChar: string
}

/**
 * What the cursor (the end of textUpToCursor) is in: a field name at the start of an expression,
 * or the value of a custom field expression. Undefined for everything else, which stays with
 * the built-in autocomplete.
 */
export function detectCustomFieldAutocomplete(
	textUpToCursor: string,
	refs: readonly CustomFieldFilterRef[],
): CustomFieldAutocompleteContext | undefined {
	if (refs.length === 0) {
		return undefined
	}
	const scan = scanFilterExpressions(textUpToCursor)
	const last = scan.expressions[scan.expressions.length - 1]

	if (last && last.nameStart === scan.lastStart) {
		const field = new Lookup(refs).resolve(last)
		if (!field || !(hasOptions(field) || field.type === 'user')) {
			return undefined
		}
		let start = last.valueStart
		let rawValue = textUpToCursor.slice(last.valueStart)
		if (isMultiValue(last.operator)) {
			// Only the item after the last comma outside of quotes.
			const items = rawValue.split(/,(?=(?:[^"]*"[^"]*")*[^"]*$)/)
			const lastItem = items[items.length - 1]
			start += rawValue.length - lastItem.length
			rawValue = lastItem
		}
		const leading = rawValue.length - rawValue.trimStart().length
		start += leading
		rawValue = rawValue.trimStart()
		let quoteChar = ''
		if (rawValue[0] === '"' || rawValue[0] === '\'') {
			quoteChar = rawValue[0]
			rawValue = rawValue.slice(1)
			start += 1
		}
		return {
			kind: 'value',
			field,
			operator: last.operator,
			search: rawValue,
			start,
			end: textUpToCursor.length,
			quoteChar,
		}
	}

	const tail = textUpToCursor.slice(scan.lastStart)
	const named = /^(["']?)([\p{L}\p{N}_ ()-]*)$/u.exec(tail)
	if (!named || tail.trim() === '') {
		return undefined
	}
	const [, quoteChar, typed] = named
	// Spaces are only part of a name inside quotes.
	if (!quoteChar && /[ ()-]/.test(typed)) {
		return undefined
	}
	return {
		kind: 'name',
		search: typed,
		start: scan.lastStart,
		end: textUpToCursor.length,
		quoteChar,
	}
}

export interface CustomFieldSuggestion {
	id: number | string
	title: string
	// What is written into the filter.
	insertText: string
}

export function suggestCustomFieldNames(refs: readonly CustomFieldFilterRef[], search: string): CustomFieldSuggestion[] {
	const query = search.trim().toLowerCase()
	return refs
		.filter(ref => query !== '' && ref.name.toLowerCase().includes(query))
		.slice(0, 20)
		.map(ref => ({id: ref.id, title: ref.name, insertText: formatCustomFieldName(ref.name)}))
}

export function suggestCustomFieldOptions(
	field: CustomFieldFilterRef,
	operator: string,
	search: string,
	quoteChar: string,
): CustomFieldSuggestion[] {
	const query = search.trim().toLowerCase()
	return field.options
		.filter(option => option.title.toLowerCase().includes(query))
		.map(option => ({
			id: option.id,
			title: option.title,
			insertText: quoteChar ? option.title : formatCustomFieldValue(option.title, isMultiValue(operator)),
		}))
}
