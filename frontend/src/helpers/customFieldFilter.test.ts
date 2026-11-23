import {describe, expect, it} from 'vitest'

import {
	buildCustomFieldRefs,
	customFieldFilterFromApi,
	customFieldFilterToApi,
	detectCustomFieldAutocomplete,
	findCustomFieldExpressions,
	formatCustomFieldName,
	scanFilterExpressions,
	suggestCustomFieldNames,
	suggestCustomFieldOptions,
	type CustomFieldFilterSource,
} from './customFieldFilter'
import {transformFilterStringForApi, transformFilterStringFromApi} from './filters'

const fields: CustomFieldFilterSource[] = [
	{id: 12, project_id: 1, title: 'Budget', type: 'currency', position: 1, options: []},
	{id: 3, project_id: 1, title: 'Cost center', type: 'text', position: 2, options: []},
	{id: 4, project_id: 1, title: 'Stage', type: 'select', position: 3, options: [
		{id: 31, title: 'Review'},
		{id: 32, title: 'Won, closed'},
		{id: 33, title: 'Lost'},
	]},
	{id: 5, project_id: 1, title: 'Go live', type: 'date', position: 4, options: []},
	{id: 6, project_id: 1, title: 'Owner', type: 'user', position: 5, options: []},
	{id: 7, project_id: 1, title: 'Priority', type: 'number', position: 6, options: []},
]

const refs = buildCustomFieldRefs(fields, id => `Project ${id}`)

describe('buildCustomFieldRefs', () => {
	it('keeps unique titles as they are', () => {
		expect(refs.map(ref => ref.name)).toEqual(['Budget', 'Cost center', 'Stage', 'Go live', 'Owner', 'Priority'])
	})

	it('appends the project title to colliding names, then the id', () => {
		const colliding = buildCustomFieldRefs([
			{id: 1, project_id: 1, title: 'Budget', type: 'number', position: 1, options: []},
			{id: 2, project_id: 2, title: 'budget', type: 'number', position: 1, options: []},
			{id: 3, project_id: 2, title: 'Budget', type: 'number', position: 2, options: []},
			{id: 4, project_id: 3, title: 'Unique', type: 'number', position: 1, options: []},
		], id => ({1: 'Marketing', 2: 'Sales', 3: 'Ops'}[id]))
		expect(colliding.map(ref => ref.name)).toEqual([
			'Budget (Marketing)',
			'budget (Sales) #2',
			'Budget (Sales) #3',
			'Unique',
		])
	})

	it('is independent of the input order', () => {
		const shuffled = [...fields].reverse()
		expect(buildCustomFieldRefs(shuffled, id => `Project ${id}`)).toEqual(refs)
	})
})

describe('formatCustomFieldName', () => {
	it('quotes names with spaces, built-in names and quotes', () => {
		expect(formatCustomFieldName('Budget')).toBe('Budget')
		expect(formatCustomFieldName('Cost center')).toBe('"Cost center"')
		expect(formatCustomFieldName('done')).toBe('"done"')
		expect(formatCustomFieldName('Due date')).toBe('"Due date"')
		expect(formatCustomFieldName('Say "hi"')).toBe('"Say \\"hi\\""')
		expect(formatCustomFieldName('Größe')).toBe('Größe')
	})
})

describe('scanFilterExpressions', () => {
	it('finds the expressions at the start of each part', () => {
		const text = '(Budget > 100 || "Cost center" like a b) && done = false'
		const found = scanFilterExpressions(text).expressions
		expect(found.map(e => [e.name, e.operator, text.slice(e.valueStart, e.valueEnd)])).toEqual([
			['Budget', '>', '100'],
			['Cost center', 'like', 'a b'],
			['done', '=', 'false'],
		])
	})

	it('does not read fields out of values', () => {
		const found = scanFilterExpressions('title = "Budget > 5 && x" && Budget < 3').expressions
		expect(found.map(e => e.name)).toEqual(['title', 'Budget'])
	})

	it('reads word operators only with a space', () => {
		expect(scanFilterExpressions('Stage in Review').expressions[0].operator).toBe('in')
		expect(scanFilterExpressions('Stagein Review').expressions).toHaveLength(0)
		expect(scanFilterExpressions('Stage not  in Review').expressions[0].operator).toBe('not in')
	})
})

describe('API round trip', () => {
	const roundTrip = (display: string, api: string) => {
		expect(customFieldFilterToApi(display, refs)).toBe(api)
		expect(customFieldFilterFromApi(api, refs)).toBe(display)
	}

	it('translates names and numbers', () => {
		roundTrip('Budget > 100', 'cf_12 > 100')
		roundTrip('"Cost center" like abc && Budget <= 5', 'cf_3 like abc && cf_12 <= 5')
		roundTrip('(Budget >= 1 || Budget != 5) && done = false', '(cf_12 >= 1 || cf_12 != 5) && done = false')
	})

	it('translates select option titles and lists', () => {
		roundTrip('Stage = Review', 'cf_4 = 31')
		roundTrip('Stage in Review, Lost', 'cf_4 in 31, 33')
		roundTrip('Stage not in "Won, closed"', 'cf_4 not in 32')
		roundTrip('Stage in Review, "Won, closed"', 'cf_4 in 31, 32')
	})

	it('matches names and option titles case-insensitively', () => {
		expect(customFieldFilterToApi('budget > 1 && STAGE = review', refs)).toBe('cf_12 > 1 && cf_4 = 31')
	})

	it('leaves date math and user values alone', () => {
		roundTrip('"Go live" < now/w+1w', 'cf_5 < now/w+1w')
		roundTrip('"Go live" > 2026-01-01', 'cf_5 > 2026-01-01')
		roundTrip('Owner in alice, bob', 'cf_6 in alice, bob')
	})

	it('keeps unknown names, option titles and raw identifiers', () => {
		expect(customFieldFilterToApi('Nope > 1', refs)).toBe('Nope > 1')
		expect(customFieldFilterToApi('Stage = Unknown', refs)).toBe('cf_4 = Unknown')
		expect(customFieldFilterToApi('cf_12 > 5', refs)).toBe('cf_12 > 5')
		expect(customFieldFilterFromApi('cf_99 > 5', refs)).toBe('cf_99 > 5')
		expect(customFieldFilterFromApi('cf_4 = 999', refs)).toBe('Stage = 999')
	})

	it('treats a bare built-in name as the built-in field and a quoted one as custom', () => {
		expect(customFieldFilterToApi('priority > 2', refs)).toBe('priority > 2')
		expect(customFieldFilterToApi('"Priority" > 2', refs)).toBe('cf_7 > 2')
		expect(customFieldFilterFromApi('cf_7 > 2 && priority > 1', refs)).toBe('"Priority" > 2 && priority > 1')
	})

	it('does not resolve the plain name of colliding fields', () => {
		const colliding = buildCustomFieldRefs([
			{id: 1, project_id: 1, title: 'Budget', type: 'number', position: 1, options: []},
			{id: 2, project_id: 2, title: 'Budget', type: 'number', position: 1, options: []},
		], id => ({1: 'Marketing', 2: 'Sales'}[id]))
		expect(customFieldFilterToApi('Budget > 1', colliding)).toBe('Budget > 1')
		expect(customFieldFilterToApi('"Budget (Sales)" > 1', colliding)).toBe('cf_2 > 1')
		expect(customFieldFilterFromApi('cf_1 > 1 || cf_2 > 1', colliding))
			.toBe('"Budget (Marketing)" > 1 || "Budget (Sales)" > 1')
	})

	it('finds nothing without fields', () => {
		expect(findCustomFieldExpressions('Budget > 1', [])).toEqual([])
		expect(customFieldFilterToApi('Budget > 1', [])).toBe('Budget > 1')
	})

	it('works together with the built-in transforms', () => {
		const toApi = transformFilterStringForApi(
			customFieldFilterToApi('"Cost center" = x && dueDate < now && Stage = Review', refs),
			() => null, () => null,
		)
		expect(toApi).toBe('cf_3 = x && due_date < now && cf_4 = 31')
		const back = customFieldFilterFromApi(transformFilterStringFromApi(toApi, () => null, () => null), refs)
		expect(back).toBe('"Cost center" = x && dueDate < now && Stage = Review')
	})
})

describe('detectCustomFieldAutocomplete', () => {
	it('suggests names at the start of an expression', () => {
		const context = detectCustomFieldAutocomplete('done = false && Bud', refs)
		expect(context).toMatchObject({kind: 'name', search: 'Bud', start: 16, end: 19, quoteChar: ''})
		expect(suggestCustomFieldNames(refs, 'bud').map(s => s.insertText)).toEqual(['Budget'])
		expect(suggestCustomFieldNames(refs, 'cost').map(s => s.insertText)).toEqual(['"Cost center"'])
	})

	it('suggests names inside an opened quote', () => {
		const context = detectCustomFieldAutocomplete('"Cost c', refs)
		expect(context).toMatchObject({kind: 'name', search: 'Cost c', start: 0, quoteChar: '"'})
	})

	it('suggests nothing for an empty start or after a finished expression', () => {
		expect(detectCustomFieldAutocomplete('', refs)).toBeUndefined()
		expect(detectCustomFieldAutocomplete('done = false && ', refs)).toBeUndefined()
		expect(detectCustomFieldAutocomplete('done = fal', refs)).toBeUndefined()
		expect(detectCustomFieldAutocomplete('Bud', [])).toBeUndefined()
	})

	it('suggests option titles as values of select fields', () => {
		const text = 'Stage = Re'
		const context = detectCustomFieldAutocomplete(text, refs)!
		expect(context).toMatchObject({kind: 'value', search: 'Re', start: 8, end: 10})
		expect(context.field?.id).toBe(4)
		expect(suggestCustomFieldOptions(context.field!, context.operator!, context.search, context.quoteChar)
			.map(s => s.insertText)).toEqual(['Review'])
	})

	it('only completes the item after the last comma', () => {
		const text = 'Stage in Review, Wo'
		const context = detectCustomFieldAutocomplete(text, refs)!
		expect(context.search).toBe('Wo')
		expect(text.slice(context.start)).toBe('Wo')
		expect(suggestCustomFieldOptions(context.field!, context.operator!, 'Wo', '')[0].insertText).toBe('"Won, closed"')
	})

	it('knows user fields, so the assignee autocomplete can take over', () => {
		const context = detectCustomFieldAutocomplete('Owner = al', refs)!
		expect(context.field?.type).toBe('user')
		expect(context.search).toBe('al')
	})

	it('leaves values of other fields to the built-in autocomplete', () => {
		expect(detectCustomFieldAutocomplete('Budget > 1', refs)).toBeUndefined()
		expect(detectCustomFieldAutocomplete('labels = foo', refs)).toBeUndefined()
	})
})
