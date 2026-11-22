import {describe, expect, it} from 'vitest'

import type {CustomFieldResponse} from '@/client/queries/customFields'
import {
	customFieldSortKey,
	formatCustomFieldNumber,
	hasCustomFieldValue,
	isSortableCustomFieldType,
	omitCustomSortKeys,
	parseCustomFieldSortKey,
	resolveSelectedFields,
	selectionOf,
	valueOfCustomField,
	withCustomFieldsExpand,
	withFieldSelected,
} from './customFieldDisplay'

function field(id: number, overrides: Partial<CustomFieldResponse> = {}): CustomFieldResponse {
	return {id, project_id: 1, title: `F${id}`, description: '', type: 'text', required: false, position: id, options: [], ...overrides}
}

describe('custom field sort keys', () => {
	it('round-trips cf_<id> and rejects other keys', () => {
		expect(customFieldSortKey(12)).toBe('cf_12')
		expect(parseCustomFieldSortKey('cf_12')).toBe(12)
		expect(parseCustomFieldSortKey('due_date')).toBeNull()
		expect(parseCustomFieldSortKey('cf_x')).toBeNull()
	})

	it('only multi value types are not sortable', () => {
		expect(isSortableCustomFieldType('number')).toBe(true)
		expect(isSortableCustomFieldType('select')).toBe(true)
		expect(isSortableCustomFieldType('multi_select')).toBe(false)
		expect(isSortableCustomFieldType('user')).toBe(false)
	})

	it('drops custom field keys from a sort state', () => {
		expect(omitCustomSortKeys({id: 'desc', cf_3: 'asc'})).toEqual({id: 'desc'})
	})
})

describe('selection', () => {
	it('keeps one selection per scope', () => {
		let selection = withFieldSelected({}, 1, 10, true)
		selection = withFieldSelected(selection, 1, 11, true)
		selection = withFieldSelected(selection, 2, 10, true)
		expect(selectionOf(selection, 1)).toEqual([10, 11])
		expect(selectionOf(selection, 2)).toEqual([10])
		expect(selectionOf(selection, 3)).toEqual([])
	})

	it('removes a field and does not duplicate on repeated selects', () => {
		let selection = withFieldSelected({}, 1, 10, true)
		selection = withFieldSelected(selection, 1, 10, true)
		expect(selectionOf(selection, 1)).toEqual([10])
		selection = withFieldSelected(selection, 1, 10, false)
		expect(selectionOf(selection, 1)).toEqual([])
	})

	it('ignores malformed stored data', () => {
		expect(selectionOf({1: 'x'} as never, 1)).toEqual([])
	})

	it('drops deleted fields and follows the field order', () => {
		const fields = [field(1), field(2), field(3)]
		expect(resolveSelectedFields(fields, [3, 99, 1]).map(f => f.id)).toEqual([1, 3])
	})
})

describe('expand', () => {
	it('adds custom_fields only when active', () => {
		expect(withCustomFieldsExpand(['comment_count'], false)).toEqual(['comment_count'])
		expect(withCustomFieldsExpand(['comment_count'], true)).toEqual(['comment_count', 'custom_fields'])
		expect(withCustomFieldsExpand(['custom_fields'], true)).toEqual(['custom_fields'])
	})
})

describe('task values', () => {
	const values = [{custom_field_id: 1, value: 'a'}, {custom_field_id: 2, value: false}]

	it('finds values, treating false as set', () => {
		expect(valueOfCustomField(values, 1)).toBe('a')
		expect(hasCustomFieldValue(values, 2)).toBe(true)
		expect(hasCustomFieldValue(values, 3)).toBe(false)
		expect(hasCustomFieldValue(undefined, 1)).toBe(false)
	})
})

describe('formatCustomFieldNumber', () => {
	it('formats currency with its code', () => {
		const text = formatCustomFieldNumber(1234.5, field(1, {type: 'currency', config: {currency: 'EUR', precision: 2}}), 'en')
		expect(text).toContain('1,234.50')
		expect(text).toMatch(/€|EUR/)
	})

	it('limits fraction digits to the precision', () => {
		expect(formatCustomFieldNumber(1.23456, field(1, {type: 'number', config: {precision: 2}}), 'en')).toBe('1.23')
	})

	it('falls back for an invalid currency code', () => {
		expect(formatCustomFieldNumber(5, field(1, {type: 'currency', config: {currency: '12'}}), 'en')).toBe('5 12')
	})
})
