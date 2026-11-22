import {describe, expect, it} from 'vitest'

import {
	asNumberArray,
	createCustomFieldDraft,
	customFieldToDraft,
	draftToWritable,
	hasDraftErrors,
	isCustomFieldValueEmpty,
	moveItem,
	numberStep,
	parseNumberInput,
	readableTextColor,
	roundToPrecision,
	validateCustomFieldDraft,
	validateCustomFieldValue,
} from './customFields'

describe('parseNumberInput', () => {
	it('returns null for empty input', () => {
		expect(parseNumberInput('')).toBeNull()
		expect(parseNumberInput('  ')).toBeNull()
		expect(parseNumberInput(null)).toBeNull()
	})

	it('parses plain, decimal and comma-decimal numbers', () => {
		expect(parseNumberInput('42')).toBe(42)
		expect(parseNumberInput('-3.5')).toBe(-3.5)
		expect(parseNumberInput('12,75')).toBe(12.75)
		expect(parseNumberInput(7)).toBe(7)
	})

	it('returns NaN for text that is not a number', () => {
		expect(parseNumberInput('abc')).toBeNaN()
		expect(parseNumberInput('1.2.3')).toBeNaN()
	})
})

describe('roundToPrecision', () => {
	it('rounds to the given decimal places', () => {
		expect(roundToPrecision(1.005, 2)).toBe(1.01)
		expect(roundToPrecision(2.5, 0)).toBe(3)
	})

	it('keeps the value without precision', () => {
		expect(roundToPrecision(1.23456, undefined)).toBe(1.23456)
	})
})

describe('numberStep', () => {
	it('derives the step from the precision', () => {
		expect(numberStep({type: 'number', config: {precision: 2}})).toBe('0.01')
		expect(numberStep({type: 'number'})).toBe('any')
		expect(numberStep({type: 'progress'})).toBe('1')
	})
})

describe('isCustomFieldValueEmpty', () => {
	it('treats null, blank text, empty lists and an unchecked box as empty', () => {
		expect(isCustomFieldValueEmpty({type: 'text'}, null)).toBe(true)
		expect(isCustomFieldValueEmpty({type: 'text'}, '  ')).toBe(true)
		expect(isCustomFieldValueEmpty({type: 'multi_select'}, [])).toBe(true)
		expect(isCustomFieldValueEmpty({type: 'checkbox'}, false)).toBe(true)
	})

	it('treats zero and checked values as set', () => {
		expect(isCustomFieldValueEmpty({type: 'number'}, 0)).toBe(false)
		expect(isCustomFieldValueEmpty({type: 'checkbox'}, true)).toBe(false)
		expect(isCustomFieldValueEmpty({type: 'select'}, 4)).toBe(false)
	})
})

describe('validateCustomFieldValue', () => {
	it('accepts null for every type', () => {
		expect(validateCustomFieldValue({type: 'rating'}, null)).toBeNull()
	})

	it('checks number min and max', () => {
		const field = {type: 'number', config: {min: 1, max: 10}}
		expect(validateCustomFieldValue(field, 5)).toBeNull()
		expect(validateCustomFieldValue(field, 0)?.key).toBe('customFields.errors.value.min')
		expect(validateCustomFieldValue(field, 11)?.key).toBe('customFields.errors.value.max')
		expect(validateCustomFieldValue(field, Number.NaN)?.key).toBe('customFields.errors.value.number')
	})

	it('checks urls and emails', () => {
		expect(validateCustomFieldValue({type: 'url'}, 'https://example.com/a')).toBeNull()
		expect(validateCustomFieldValue({type: 'url'}, 'javascript:alert(1)')?.key).toBe('customFields.errors.value.url')
		expect(validateCustomFieldValue({type: 'url'}, 'example.com')?.key).toBe('customFields.errors.value.url')
		expect(validateCustomFieldValue({type: 'email'}, 'a@b.co')).toBeNull()
		expect(validateCustomFieldValue({type: 'email'}, 'a@b')?.key).toBe('customFields.errors.value.email')
	})

	it('checks rating against the configured maximum', () => {
		const field = {type: 'rating', config: {max_rating: 3}}
		expect(validateCustomFieldValue(field, 3)).toBeNull()
		expect(validateCustomFieldValue(field, 4)?.params).toEqual({max: 3})
		expect(validateCustomFieldValue({type: 'rating'}, 5)).toBeNull()
		expect(validateCustomFieldValue({type: 'rating'}, 2.5)).not.toBeNull()
	})

	it('keeps progress within 0 to 100', () => {
		expect(validateCustomFieldValue({type: 'progress'}, 0)).toBeNull()
		expect(validateCustomFieldValue({type: 'progress'}, 100)).toBeNull()
		expect(validateCustomFieldValue({type: 'progress'}, 101)).not.toBeNull()
	})

	it('requires option ids of the field', () => {
		const field = {type: 'select', options: [{id: 1}, {id: 2}]}
		expect(validateCustomFieldValue(field, 2)).toBeNull()
		expect(validateCustomFieldValue(field, 3)).not.toBeNull()
		expect(validateCustomFieldValue({...field, type: 'multi_select'}, [1, 2])).toBeNull()
		expect(validateCustomFieldValue({...field, type: 'multi_select'}, [1, 9])).not.toBeNull()
	})
})

describe('asNumberArray', () => {
	it('drops everything but numbers', () => {
		expect(asNumberArray([1, 'x', 2, null])).toEqual([1, 2])
		expect(asNumberArray(null)).toEqual([])
	})
})

describe('custom field drafts', () => {
	it('requires a title', () => {
		const errors = validateCustomFieldDraft(createCustomFieldDraft())
		expect(errors.title?.key).toBe('customFields.errors.titleRequired')
		expect(hasDraftErrors(errors)).toBe(true)
	})

	it('accepts a valid text field', () => {
		expect(hasDraftErrors(validateCustomFieldDraft({...createCustomFieldDraft(), title: 'Budget'}))).toBe(false)
	})

	it('validates number config', () => {
		const draft = {...createCustomFieldDraft(), title: 'N', type: 'number' as const}
		expect(validateCustomFieldDraft({...draft, min: '10', max: '2'}).range).toBeDefined()
		expect(validateCustomFieldDraft({...draft, min: 'x'}).min).toBeDefined()
		expect(validateCustomFieldDraft({...draft, precision: '11'}).precision).toBeDefined()
		expect(validateCustomFieldDraft({...draft, precision: '1.5'}).precision).toBeDefined()
		expect(hasDraftErrors(validateCustomFieldDraft({...draft, min: '0', max: '9', precision: '2'}))).toBe(false)
	})

	it('validates the currency code and rating maximum', () => {
		const currency = {...createCustomFieldDraft(), title: 'C', type: 'currency' as const}
		expect(validateCustomFieldDraft({...currency, currency: 'TR'}).currency).toBeDefined()
		expect(validateCustomFieldDraft({...currency, currency: 'try'}).currency).toBeUndefined()

		const rating = {...createCustomFieldDraft(), title: 'R', type: 'rating' as const}
		expect(validateCustomFieldDraft({...rating, maxRating: 11}).maxRating).toBeDefined()
		expect(validateCustomFieldDraft({...rating, maxRating: 0}).maxRating).toBeDefined()
		expect(validateCustomFieldDraft({...rating, maxRating: 10}).maxRating).toBeUndefined()
	})

	it('validates select options', () => {
		const select = {...createCustomFieldDraft(), title: 'S', type: 'select' as const}
		expect(validateCustomFieldDraft(select).options?.key).toBe('customFields.errors.optionsRequired')
		expect(validateCustomFieldDraft({
			...select,
			options: [{key: 'a', title: ' ', color: ''}],
		}).options?.key).toBe('customFields.errors.optionTitle')
		expect(validateCustomFieldDraft({
			...select,
			options: [{key: 'a', title: 'A', color: 'red'}],
		}).options?.key).toBe('customFields.errors.optionColor')
		expect(hasDraftErrors(validateCustomFieldDraft({
			...select,
			options: [{key: 'a', title: 'A', color: '#ff8800'}],
		}))).toBe(false)
	})

	it('builds the API body with config only for the matching type', () => {
		const body = draftToWritable({
			...createCustomFieldDraft(),
			title: ' Price ',
			type: 'currency',
			min: '0',
			precision: '2',
			currency: 'try',
			maxRating: 7,
		})
		expect(body).toEqual({
			title: 'Price',
			description: '',
			type: 'currency',
			required: false,
			config: {min: 0, precision: 2, currency: 'TRY'},
			options: undefined,
		})
	})

	it('omits config when nothing is set and keeps the position on update', () => {
		const body = draftToWritable({...createCustomFieldDraft(), title: 'T'}, 131072)
		expect(body.config).toBeUndefined()
		expect(body.position).toBe(131072)
	})

	it('keeps option ids and orders positions by list order', () => {
		const body = draftToWritable({
			...createCustomFieldDraft(),
			title: 'S',
			type: 'select',
			options: [
				{key: 'a', id: 9, title: 'First', color: '#ff0000'},
				{key: 'b', title: 'Second', color: ''},
			],
		})
		expect(body.options).toEqual([
			{id: 9, title: 'First', color: '#ff0000', position: 65536},
			{title: 'Second', color: '', position: 131072},
		])
	})

	it('round-trips a stored field into a draft sorted by option position', () => {
		const draft = customFieldToDraft({
			title: 'Stage',
			type: 'select',
			required: true,
			config: {max_rating: 4},
			options: [
				{id: 2, title: 'B', color: '', position: 2},
				{id: 1, title: 'A', color: '#00ff00', position: 1},
			],
		})
		expect(draft.options.map(option => option.title)).toEqual(['A', 'B'])
		expect(draft.required).toBe(true)
		expect(draft.maxRating).toBe(4)
	})
})

describe('moveItem', () => {
	it('moves an item and ignores out of range moves', () => {
		expect(moveItem(['a', 'b', 'c'], 1, -1)).toEqual(['b', 'a', 'c'])
		expect(moveItem(['a', 'b', 'c'], 1, 1)).toEqual(['a', 'c', 'b'])
		const list = ['a', 'b']
		expect(moveItem(list, 0, -1)).toBe(list)
		expect(moveItem(list, 1, 1)).toBe(list)
	})
})

describe('readableTextColor', () => {
	it('picks a contrasting text color', () => {
		expect(readableTextColor('#ffff00')).toBe('#000000')
		expect(readableTextColor('#0000ff')).toBe('#ffffff')
		expect(readableTextColor('')).toBe('inherit')
	})
})
