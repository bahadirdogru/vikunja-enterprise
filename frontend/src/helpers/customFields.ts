import type {
	CustomFieldConfig,
	CustomFieldOptionWritable,
	CustomFieldWritable,
} from '@/client/generated'
import type {IconProp} from '@fortawesome/fontawesome-svg-core'
import {isValidHttpUrl} from '@/helpers/isValidHttpUrl'

export const CUSTOM_FIELD_TYPES = [
	'text',
	'long_text',
	'number',
	'currency',
	'date',
	'checkbox',
	'select',
	'multi_select',
	'user',
	'url',
	'email',
	'rating',
	'progress',
] as const

export type CustomFieldType = typeof CUSTOM_FIELD_TYPES[number]

export const DEFAULT_MAX_RATING = 5
export const MAX_MAX_RATING = 10
export const MAX_PRECISION = 10

const TYPE_ICONS: Record<CustomFieldType, IconProp> = {
	text: 'font',
	long_text: 'align-left',
	number: 'hashtag',
	currency: 'dollar-sign',
	date: 'calendar',
	checkbox: ['far', 'check-square'],
	select: 'list',
	multi_select: 'list-check',
	user: 'user',
	url: 'link',
	email: 'envelope',
	rating: 'star',
	progress: 'percent',
}

export function customFieldTypeIcon(type: string | undefined): IconProp {
	return TYPE_ICONS[type as CustomFieldType] ?? 'font'
}

export function hasOptions(type: string | undefined): boolean {
	return type === 'select' || type === 'multi_select'
}

export function hasNumericRange(type: string | undefined): boolean {
	return type === 'number' || type === 'currency'
}

// The pieces of a field definition the value helpers need; a CustomFieldResponse satisfies it.
export type FieldLike = {
	type?: string
	config?: CustomFieldConfig | null
	options?: {id?: number}[] | null
}

export type ValidationError = {key: string, params?: Record<string, string | number>}

// Returns null for an empty input and NaN for text that is not a number.
export function parseNumberInput(raw: string | number | null | undefined): number | null {
	if (raw === null || raw === undefined) {
		return null
	}
	const text = String(raw).trim().replace(',', '.')
	if (text === '') {
		return null
	}
	return /^[-+]?(\d+\.?\d*|\.\d+)(e[-+]?\d+)?$/i.test(text) ? Number(text) : NaN
}

export function roundToPrecision(value: number, precision: number | undefined): number {
	if (precision === undefined || precision < 0) {
		return value
	}
	const factor = 10 ** precision
	return Math.round((value + Number.EPSILON) * factor) / factor
}

// Browsers expect a dot step of 1/10^precision; 'any' keeps the input free when no precision is set.
export function numberStep(field: FieldLike): string {
	if (field.type === 'progress' || field.type === 'rating') {
		return '1'
	}
	const precision = field.config?.precision
	return precision === undefined || precision === null ? 'any' : String(10 ** -precision)
}

export function maxRatingOf(field: FieldLike): number {
	return field.config?.max_rating ?? DEFAULT_MAX_RATING
}

export function isValidEmail(value: string): boolean {
	return /^[^\s@<>()[\]\\,;:"]+@[^\s@<>()[\]\\,;:"]+\.[^\s@<>()[\]\\,;:"]+$/.test(value)
}

export function isCustomFieldValueEmpty(field: FieldLike, value: unknown): boolean {
	if (value === null || value === undefined) {
		return true
	}
	if (Array.isArray(value)) {
		return value.length === 0
	}
	if (typeof value === 'string') {
		return value.trim() === ''
	}
	return field.type === 'checkbox' && value === false
}

// The most a text value may hold, in bytes: value_text is a 64 KB column on MySQL. Characters count with all
// of their UTF-8 bytes, like on the server.
export const MAX_TEXT_VALUE_BYTES = 65535

function textTooLong(value: unknown): ValidationError | null {
	return typeof value === 'string' && new TextEncoder().encode(value).length > MAX_TEXT_VALUE_BYTES
		? {key: 'customFields.errors.value.tooLong', params: {max: MAX_TEXT_VALUE_BYTES}}
		: null
}

export function validateCustomFieldValue(field: FieldLike, value: unknown): ValidationError | null {
	if (value === null || value === undefined) {
		return null
	}
	if (field.type === 'text' || field.type === 'long_text' || field.type === 'url' || field.type === 'email') {
		const tooLong = textTooLong(value)
		if (tooLong) {
			return tooLong
		}
	}

	switch (field.type) {
		case 'text':
		case 'long_text':
			return typeof value === 'string' ? null : {key: 'customFields.errors.value.string'}
		case 'url':
			if (value === '') return null
			return typeof value === 'string' && isValidHttpUrl(value) && new URL(value).host !== ''
				? null
				: {key: 'customFields.errors.value.url'}
		case 'email':
			if (value === '') return null
			return typeof value === 'string' && isValidEmail(value)
				? null
				: {key: 'customFields.errors.value.email'}
		case 'number':
		case 'currency': {
			if (typeof value !== 'number' || !Number.isFinite(value)) {
				return {key: 'customFields.errors.value.number'}
			}
			const {min, max} = field.config ?? {}
			if (min !== undefined && min !== null && value < min) {
				return {key: 'customFields.errors.value.min', params: {min}}
			}
			if (max !== undefined && max !== null && value > max) {
				return {key: 'customFields.errors.value.max', params: {max}}
			}
			return null
		}
		case 'progress':
			return typeof value === 'number' && value >= 0 && value <= 100
				? null
				: {key: 'customFields.errors.value.progress'}
		case 'rating': {
			const max = maxRatingOf(field)
			return typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= max
				? null
				: {key: 'customFields.errors.value.rating', params: {max}}
		}
		case 'checkbox':
			return typeof value === 'boolean' ? null : {key: 'customFields.errors.value.boolean'}
		case 'date':
			return typeof value === 'string' && !Number.isNaN(Date.parse(value))
				? null
				: {key: 'customFields.errors.value.date'}
		case 'select':
			return typeof value === 'number' && (field.options ?? []).some(option => option.id === value)
				? null
				: {key: 'customFields.errors.value.option'}
		case 'multi_select':
			return Array.isArray(value) && value.every(id => (field.options ?? []).some(option => option.id === id))
				? null
				: {key: 'customFields.errors.value.option'}
		case 'user':
			return Array.isArray(value) && value.every(id => typeof id === 'number')
				? null
				: {key: 'customFields.errors.value.user'}
	}
	return null
}

export function asNumberArray(value: unknown): number[] {
	return Array.isArray(value) ? value.filter((v): v is number => typeof v === 'number') : []
}

// ---- Definition drafts (settings form) ----

export type CustomFieldOptionDraft = {
	// Stable v-for key; options created in the form have no server id yet.
	key: string
	id?: number
	title: string
	color: string
}

// Number inputs are kept as text so an empty box means "unset".
export type CustomFieldDraft = {
	title: string
	description: string
	type: CustomFieldType
	required: boolean
	min: string
	max: string
	precision: string
	currency: string
	maxRating: number
	options: CustomFieldOptionDraft[]
}

export type CustomFieldDraftErrors = Partial<Record<
	'title' | 'min' | 'max' | 'range' | 'precision' | 'currency' | 'maxRating' | 'options',
	ValidationError
>>

const OPTION_POSITION_STEP = 65536
const COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/

let optionKeyCounter = 0
export function newOptionKey(): string {
	optionKeyCounter += 1
	return `option-${optionKeyCounter}`
}

export function createCustomFieldDraft(): CustomFieldDraft {
	return {
		title: '',
		description: '',
		type: 'text',
		required: false,
		min: '',
		max: '',
		precision: '',
		currency: '',
		maxRating: DEFAULT_MAX_RATING,
		options: [],
	}
}

type DraftSource = {
	title?: string
	description?: string
	type?: string
	required?: boolean
	config?: CustomFieldConfig | null
	options?: {id?: number, title?: string, color?: string, position?: number}[] | null
}

function numberToText(value: number | undefined | null): string {
	return value === undefined || value === null ? '' : String(value)
}

export function customFieldToDraft(field: DraftSource): CustomFieldDraft {
	return {
		title: field.title ?? '',
		description: field.description ?? '',
		type: (field.type as CustomFieldType | undefined) ?? 'text',
		required: field.required ?? false,
		min: numberToText(field.config?.min),
		max: numberToText(field.config?.max),
		precision: numberToText(field.config?.precision),
		currency: field.config?.currency ?? '',
		maxRating: field.config?.max_rating ?? DEFAULT_MAX_RATING,
		options: [...(field.options ?? [])]
			.sort((a, b) => (a.position ?? 0) - (b.position ?? 0))
			.map(option => ({
				key: newOptionKey(),
				id: option.id,
				title: option.title ?? '',
				color: option.color ?? '',
			})),
	}
}

export function validateCustomFieldDraft(draft: CustomFieldDraft): CustomFieldDraftErrors {
	const errors: CustomFieldDraftErrors = {}

	const title = draft.title.trim()
	if (title === '') {
		errors.title = {key: 'customFields.errors.titleRequired'}
	} else if ([...title].length > 250) {
		errors.title = {key: 'customFields.errors.titleTooLong'}
	}

	if (hasNumericRange(draft.type)) {
		const min = parseNumberInput(draft.min)
		const max = parseNumberInput(draft.max)
		if (Number.isNaN(min)) {
			errors.min = {key: 'customFields.errors.notANumber'}
		}
		if (Number.isNaN(max)) {
			errors.max = {key: 'customFields.errors.notANumber'}
		}
		if (min !== null && max !== null && min > max) {
			errors.range = {key: 'customFields.errors.minGreaterThanMax'}
		}

		const precision = parseNumberInput(draft.precision)
		if (precision !== null && (!Number.isInteger(precision) || precision < 0 || precision > MAX_PRECISION)) {
			errors.precision = {key: 'customFields.errors.precision', params: {max: MAX_PRECISION}}
		}
	}

	if (draft.type === 'currency' && draft.currency.trim() !== '' && !/^[A-Za-z]{3}$/.test(draft.currency.trim())) {
		errors.currency = {key: 'customFields.errors.currency'}
	}

	if (draft.type === 'rating'
		&& (!Number.isInteger(draft.maxRating) || draft.maxRating < 1 || draft.maxRating > MAX_MAX_RATING)) {
		errors.maxRating = {key: 'customFields.errors.maxRating', params: {max: MAX_MAX_RATING}}
	}

	if (hasOptions(draft.type)) {
		if (draft.options.length === 0) {
			errors.options = {key: 'customFields.errors.optionsRequired'}
		} else if (draft.options.some(option => option.title.trim() === '' || [...option.title.trim()].length > 250)) {
			errors.options = {key: 'customFields.errors.optionTitle'}
		} else if (draft.options.some(option => option.color !== '' && !COLOR_PATTERN.test(option.color))) {
			errors.options = {key: 'customFields.errors.optionColor'}
		}
	}

	return errors
}

export function hasDraftErrors(errors: CustomFieldDraftErrors): boolean {
	return Object.keys(errors).length > 0
}

function configFromDraft(draft: CustomFieldDraft): CustomFieldConfig | undefined {
	const config: CustomFieldConfig = {}

	if (hasNumericRange(draft.type)) {
		const min = parseNumberInput(draft.min)
		const max = parseNumberInput(draft.max)
		const precision = parseNumberInput(draft.precision)
		if (min !== null) config.min = min
		if (max !== null) config.max = max
		if (precision !== null) config.precision = precision
	}
	if (draft.type === 'currency' && draft.currency.trim() !== '') {
		config.currency = draft.currency.trim().toUpperCase()
	}
	if (draft.type === 'rating') {
		config.max_rating = draft.maxRating
	}

	return Object.keys(config).length > 0 ? config : undefined
}

// Positions follow the list order; the API keeps ids, so renamed options keep their values.
export function draftToWritable(draft: CustomFieldDraft, position?: number): CustomFieldWritable {
	const options: CustomFieldOptionWritable[] | undefined = hasOptions(draft.type)
		? draft.options.map((option, index) => ({
			...(option.id === undefined ? {} : {id: option.id}),
			title: option.title.trim(),
			color: option.color,
			position: (index + 1) * OPTION_POSITION_STEP,
		}))
		: undefined

	return {
		title: draft.title.trim(),
		description: draft.description,
		type: draft.type,
		required: draft.required,
		config: configFromDraft(draft),
		options,
		...(position === undefined ? {} : {position}),
	}
}

export function moveItem<T>(items: T[], index: number, delta: -1 | 1): T[] {
	const target = index + delta
	if (index < 0 || index >= items.length || target < 0 || target >= items.length) {
		return items
	}
	const copy = [...items]
	const [moved] = copy.splice(index, 1)
	copy.splice(target, 0, moved as T)
	return copy
}

// Black or white, whichever reads better on the given #rrggbb background.
export function readableTextColor(hex: string): string {
	const match = /^#([0-9a-f]{6})$/i.exec(hex)
	if (!match) {
		return 'inherit'
	}
	const value = parseInt(match[1] as string, 16)
	const channel = (shift: number) => {
		const c = ((value >> shift) & 255) / 255
		return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
	}
	const luminance = 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0)
	return luminance > 0.179 ? '#000000' : '#ffffff'
}
