import type {CustomFieldResponse} from '@/client/queries/customFields'
import type {CustomFieldType} from '@/helpers/customFields'
import {maxRatingOf} from '@/helpers/customFields'

const SORT_KEY_PATTERN = /^cf_(\d+)$/

// Sorting by a field is addressed as `cf_<id>`, like in the filter syntax.
export function customFieldSortKey(fieldId: number): `cf_${number}` {
	return `cf_${fieldId}`
}

export function parseCustomFieldSortKey(key: string): number | null {
	const match = SORT_KEY_PATTERN.exec(key)
	return match ? Number(match[1]) : null
}

// The API cannot sort by fields that hold several values.
export function isSortableCustomFieldType(type: CustomFieldType | string | undefined): boolean {
	return type !== 'multi_select' && type !== 'user'
}

export function omitCustomSortKeys<T extends object>(sortBy: T): T {
	return Object.fromEntries(
		Object.entries(sortBy).filter(([key]) => parseCustomFieldSortKey(key) === null),
	) as T
}

export type CustomFieldSelection = Record<string, number[]>

export function selectionOf(selection: CustomFieldSelection, scope: number | string): number[] {
	const ids = selection[String(scope)]
	return Array.isArray(ids) ? ids : []
}

// Returns a new selection with the field switched on or off; other scopes stay untouched.
export function withFieldSelected(
	selection: CustomFieldSelection,
	scope: number | string,
	fieldId: number,
	selected: boolean,
): CustomFieldSelection {
	const current = selectionOf(selection, scope).filter(id => id !== fieldId)
	return {
		...selection,
		[String(scope)]: selected ? [...current, fieldId] : current,
	}
}

// Fields that were deleted since the selection was stored are dropped; the field order wins.
export function resolveSelectedFields(fields: CustomFieldResponse[], ids: number[]): CustomFieldResponse[] {
	return fields.filter(field => ids.includes(field.id))
}

export function withCustomFieldsExpand<T extends string>(expand: readonly T[], active: boolean): (T | 'custom_fields')[] {
	return active && !expand.includes('custom_fields' as T) ? [...expand, 'custom_fields'] : [...expand]
}

export type TaskCustomFieldValues = {custom_field_id?: number, value?: unknown}[] | null | undefined

export function valueOfCustomField(values: TaskCustomFieldValues, fieldId: number): unknown {
	return values?.find(entry => entry.custom_field_id === fieldId)?.value ?? null
}

export function hasCustomFieldValue(values: TaskCustomFieldValues, fieldId: number): boolean {
	return valueOfCustomField(values, fieldId) !== null
}

export function formatCustomFieldNumber(value: number, field: Pick<CustomFieldResponse, 'type' | 'config'>, locale?: string): string {
	const precision = field.config?.precision
	const fractionDigits = typeof precision === 'number' ? {maximumFractionDigits: precision} : {}
	const currency = field.type === 'currency' ? field.config?.currency?.toUpperCase() : undefined
	try {
		if (currency) {
			return new Intl.NumberFormat(locale, {
				style: 'currency',
				currency,
				...(typeof precision === 'number' ? {minimumFractionDigits: precision, maximumFractionDigits: precision} : {}),
			}).format(value)
		}
		return new Intl.NumberFormat(locale, fractionDigits).format(value)
	} catch {
		// An unknown locale or currency code must not break the table.
		return currency ? `${value} ${currency}` : String(value)
	}
}

export function ratingStars(field: Pick<CustomFieldResponse, 'config' | 'type'>, value: unknown): {max: number, filled: number} {
	const max = maxRatingOf(field)
	const filled = typeof value === 'number' ? Math.max(0, Math.min(max, Math.round(value))) : 0
	return {max, filled}
}
