// What a submitter sees: the public definition, the answers they type, client-side checks and the request body.
import type {FormPublicDefinition} from '@/client/generated'
import {isValidEmail, parseNumberInput, validateCustomFieldValue} from '@/helpers/customFields'
import {
	findTarget,
	isEmptyDefault,
	targetName,
	MAX_LONG_TEXT,
	MAX_PRIORITY,
	MAX_TITLE,
	type FormDraft,
	type FormOptionsResponse,
	type Translate,
} from '@/helpers/forms'
import {isValidHttpUrl} from '@/helpers/isValidHttpUrl'

export type PublicOption = {id: number, title: string, color: string}

export type PublicField = {
	target: string
	label: string
	help: string
	required: boolean
	kind: string
	default: unknown
	max_length: number
	config: {min?: number, max?: number, precision?: number, currency?: string, max_rating?: number} | null
	options: PublicOption[]
}

export type PublicDefinition = {
	title: string
	description: string
	fields: PublicField[]
	success: {message: string, redirect_url: string, submit_another: boolean}
	submitter: {collect_name: boolean, name_required: boolean, collect_email: boolean, email_required: boolean, stores_account: boolean}
	attachments: {max_files: number, max_file_size: number} | null
	// All optional: without them the page shows the form title only.
	branding: {name: string, accent_color: string, has_logo: boolean, hide_footer: boolean}
}

export function normalizeDefinition(data: FormPublicDefinition): PublicDefinition {
	return {
		title: data.title ?? '',
		description: data.description ?? '',
		fields: (data.fields ?? []).map(field => ({
			target: field.target ?? '',
			label: field.label ?? '',
			help: field.help ?? '',
			required: field.required ?? false,
			kind: field.kind ?? 'text',
			default: field.default ?? null,
			max_length: field.max_length ?? 0,
			config: field.config ?? null,
			options: (field.options ?? []).map(option => ({id: option.id ?? 0, title: option.title ?? '', color: option.color ?? ''})),
		})),
		success: {
			message: data.success?.message ?? '',
			redirect_url: data.success?.redirect_url ?? '',
			submit_another: data.success?.submit_another ?? false,
		},
		submitter: {
			collect_name: data.submitter?.collect_name ?? false,
			name_required: data.submitter?.name_required ?? false,
			collect_email: data.submitter?.collect_email ?? false,
			email_required: data.submitter?.email_required ?? false,
			stores_account: data.submitter?.stores_account ?? false,
		},
		attachments: data.attachments
			? {max_files: data.attachments.max_files ?? 0, max_file_size: data.attachments.max_file_size ?? 0}
			: null,
		branding: {
			name: data.branding?.name ?? '',
			accent_color: data.branding?.accent_color ?? '',
			has_logo: data.branding?.has_logo ?? false,
			hide_footer: data.branding?.hide_footer ?? false,
		},
	}
}

// ---- answers -----------------------------------------------------------------------------------------------------

// Number-like inputs are kept as text so an empty box means "no answer".
export type Answers = {
	values: Record<string, unknown>
	files: File[]
	name: string
	email: string
}

export const SIMPLE_KINDS = ['text', 'url', 'email'] as const
export const LONG_KINDS = ['long_text', 'rich_text'] as const
export const NUMBER_KINDS = ['number', 'currency'] as const

export function isTextKind(kind: string): boolean {
	return (SIMPLE_KINDS as readonly string[]).includes(kind)
}

export function isLongKind(kind: string): boolean {
	return (LONG_KINDS as readonly string[]).includes(kind)
}

export function textLimit(field: PublicField): number {
	if (field.max_length > 0) return field.max_length
	return isLongKind(field.kind) ? MAX_LONG_TEXT : 10000
}

// Priority is a fixed list of choices.
export const PRIORITY_VALUES = [1, 2, 3, 4, 5] as const

export function isDateOnly(field: PublicField): boolean {
	return field.target === 'due_date'
}

function pad(n: number): string {
	return String(n).padStart(2, '0')
}

// An ISO instant as the value of a native date or datetime-local input, in the viewer's time zone.
export function isoToInput(iso: string, dateOnly: boolean): string {
	if (/^\d{4}-\d{2}-\d{2}$/.test(iso)) return dateOnly ? iso : `${iso}T00:00`
	const d = new Date(iso)
	if (Number.isNaN(d.getTime())) return ''
	const day = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
	return dateOnly ? day : `${day}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// A date without a time is noon in the viewer's zone, so it names the same day for everyone within twelve hours.
export function inputToIso(value: string): string {
	const m = /^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?$/.exec(value)
	if (!m) return ''
	const hasTime = m[4] !== undefined
	const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), hasTime ? Number(m[4]) : 12, hasTime ? Number(m[5]) : 0)
	return Number.isNaN(d.getTime()) ? '' : d.toISOString()
}

export function initialValue(field: PublicField): unknown {
	const value = field.default
	switch (field.kind) {
		case 'checkbox':
			return value === true
		case 'multi_select':
		case 'labels':
			return Array.isArray(value) ? [...value] : []
		case 'select':
		case 'rating':
		case 'priority':
			return typeof value === 'number' ? value : null
		case 'progress':
			return typeof value === 'number' ? value : null
		case 'number':
		case 'currency':
			return typeof value === 'number' ? String(value) : ''
		case 'date':
			return typeof value === 'string' ? isoToInput(value, isDateOnly(field)) : ''
		case 'files':
			return null
		default:
			return typeof value === 'string' ? value : ''
	}
}

export function initialAnswers(definition: PublicDefinition): Answers {
	const values: Record<string, unknown> = {}
	for (const field of definition.fields) {
		values[field.target] = initialValue(field)
	}
	return {values, files: [], name: '', email: ''}
}

export function isAnswerEmpty(field: PublicField, value: unknown): boolean {
	switch (field.kind) {
		case 'checkbox':
			return value !== true
		case 'multi_select':
		case 'labels':
			return !Array.isArray(value) || value.length === 0
		case 'select':
		case 'rating':
		case 'priority':
		case 'progress':
			return value === null || value === undefined
		case 'files':
			return true
		default:
			return typeof value !== 'string' || value.trim() === ''
	}
}

function fieldValue(field: PublicField, value: unknown): unknown {
	switch (field.kind) {
		case 'number':
		case 'currency':
			return parseNumberInput(value as string)
		case 'date': {
			const iso = inputToIso(String(value))
			return iso === '' ? null : iso
		}
		case 'text':
		case 'long_text':
		case 'rich_text':
		case 'url':
		case 'email':
			return String(value).trim()
		default:
			return value
	}
}

// The body of a submission: only fields with an answer.
export function serializeValues(definition: PublicDefinition, answers: Answers): Record<string, unknown> {
	const out: Record<string, unknown> = {}
	for (const field of definition.fields) {
		if (field.kind === 'files') continue
		const value = answers.values[field.target]
		if (isAnswerEmpty(field, value)) {
			// An unchecked box is an answer.
			if (field.kind === 'checkbox' && field.target.startsWith('cf_')) out[field.target] = false
			continue
		}
		out[field.target] = fieldValue(field, value)
	}
	return out
}

export type SubmissionBody = {
	values: Record<string, unknown>
	submitter?: {name?: string, email?: string}
	honeypot: string
}

export function buildSubmission(definition: PublicDefinition, answers: Answers, honeypot: string): SubmissionBody {
	const submitter: {name?: string, email?: string} = {}
	if (definition.submitter.collect_name && answers.name.trim() !== '') submitter.name = answers.name.trim()
	if (definition.submitter.collect_email && answers.email.trim() !== '') submitter.email = answers.email.trim()
	return {
		values: serializeValues(definition, answers),
		...(Object.keys(submitter).length > 0 ? {submitter} : {}),
		honeypot,
	}
}

// ---- validation --------------------------------------------------------------------------------------------------

export type AnswerErrors = Record<string, string>

export const SUBMITTER_NAME = 'submitter.name'
export const SUBMITTER_EMAIL = 'submitter.email'

function fieldProblem(field: PublicField, value: unknown, t: Translate): string {
	switch (field.kind) {
		case 'text':
		case 'long_text':
		case 'rich_text':
		case 'url':
		case 'email': {
			const text = String(value).trim()
			if (text === '') return ''
			if ([...text].length > textLimit(field)) return t('forms.errors.tooLong', {max: textLimit(field)})
			if (field.kind === 'url' && !(isValidHttpUrl(text) && new URL(text).host !== '')) return t('customFields.errors.value.url')
			if (field.kind === 'email' && !isValidEmail(text)) return t('customFields.errors.value.email')
			return ''
		}
		case 'number':
		case 'currency': {
			const number = parseNumberInput(String(value))
			if (number === null) return ''
			const invalid = validateCustomFieldValue({type: field.kind, config: field.config}, Number.isNaN(number) ? 'x' : number)
			return invalid ? t(invalid.key, invalid.params ?? {}) : ''
		}
		case 'progress':
		case 'rating':
		case 'checkbox':
		case 'select':
		case 'multi_select': {
			const invalid = validateCustomFieldValue(
				{type: field.kind, config: field.config, options: field.options},
				value,
			)
			return invalid ? t(invalid.key, invalid.params ?? {}) : ''
		}
		case 'priority':
			return typeof value === 'number' && value >= 0 && value <= MAX_PRIORITY ? '' : t('forms.errors.priority', {max: MAX_PRIORITY})
		case 'labels':
			return Array.isArray(value) && value.every(id => field.options.some(option => option.id === id))
				? '' : t('customFields.errors.value.option')
		case 'date': {
			const iso = inputToIso(String(value))
			return iso === '' ? t('customFields.errors.value.date') : ''
		}
	}
	return ''
}

export function validateAnswers(definition: PublicDefinition, answers: Answers, t: Translate): AnswerErrors {
	const errors: AnswerErrors = {}
	for (const field of definition.fields) {
		if (field.kind === 'files') {
			const limits = definition.attachments
			if (field.required && answers.files.length === 0) errors[field.target] = t('forms.public.errors.required')
			else if (limits && answers.files.length > limits.max_files) errors[field.target] = t('forms.public.errors.tooManyFiles', {max: limits.max_files})
			else if (limits && limits.max_file_size > 0) {
				const big = answers.files.find(file => file.size > limits.max_file_size)
				if (big) errors[field.target] = t('forms.public.errors.fileTooLarge', {name: big.name, max: formatBytes(limits.max_file_size)})
			}
			continue
		}
		const value = answers.values[field.target]
		if (isAnswerEmpty(field, value)) {
			if (field.required) errors[field.target] = t('forms.public.errors.required')
			continue
		}
		const problem = fieldProblem(field, value, t)
		if (problem) errors[field.target] = problem
	}
	const submitter = definition.submitter
	if (submitter.collect_name) {
		const name = answers.name.trim()
		if (name === '' && submitter.name_required) errors[SUBMITTER_NAME] = t('forms.public.errors.required')
		else if ([...name].length > MAX_TITLE) errors[SUBMITTER_NAME] = t('forms.errors.tooLong', {max: MAX_TITLE})
	}
	if (submitter.collect_email) {
		const email = answers.email.trim()
		if (email === '' && submitter.email_required) errors[SUBMITTER_EMAIL] = t('forms.public.errors.required')
		else if (email !== '' && !isValidEmail(email)) errors[SUBMITTER_EMAIL] = t('customFields.errors.value.email')
	}
	return errors
}

export function formatBytes(bytes: number): string {
	if (bytes >= 1024 * 1024) return `${Math.round((bytes / (1024 * 1024)) * 10) / 10} MB`
	if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`
	return `${bytes} B`
}

// Server errors name the field by target; submitter details and attachments use their own keys.
export function serverErrorsFrom(problem: unknown): AnswerErrors | null {
	if (!problem || typeof problem !== 'object') return null
	const details = (problem as {details?: unknown}).details
	const list = details && typeof details === 'object' ? (details as {errors?: unknown}).errors : undefined
	if (!Array.isArray(list)) return null
	const out: AnswerErrors = {}
	for (const item of list) {
		if (!item || typeof item !== 'object') continue
		const {field, message} = item as {field?: unknown, message?: unknown}
		if (typeof field === 'string' && typeof message === 'string' && !(field in out)) out[field] = message
	}
	return Object.keys(out).length > 0 ? out : null
}

// ---- preview -----------------------------------------------------------------------------------------------------

export type PreviewLabel = {id: number, title: string, color: string}

// The definition the public page would get for a draft: shown fields only, no defaults with placeholders.
export function definitionFromDraft(
	draft: FormDraft,
	options: FormOptionsResponse,
	labels: PreviewLabel[],
	t: Translate,
): PublicDefinition {
	const fields: PublicField[] = []
	for (const field of draft.fields) {
		const info = findTarget(options, field.target)
		if (!info || field.hidden) continue
		const text = typeof field.default === 'string' ? field.default : ''
		const base: PublicField = {
			target: field.target,
			label: field.label.trim() || targetName(info, t),
			help: field.help.trim(),
			required: field.required,
			kind: info.kind,
			default: isEmptyDefault(field.default) || text.includes('{{') ? null : field.default,
			max_length: 0,
			config: info.config,
			options: info.options,
		}
		switch (field.target) {
			case 'title':
				base.kind = 'text'
				base.max_length = MAX_TITLE
				break
			case 'description':
				base.kind = 'rich_text'
				base.max_length = MAX_LONG_TEXT
				break
			case 'labels':
				base.options = field.options
					.map(id => labels.find(label => label.id === id))
					.filter((label): label is PreviewLabel => label !== undefined)
				break
		}
		fields.push(base)
	}
	const s = draft.settings
	const isPublic = draft.access === 'public'
	return {
		title: draft.title.trim(),
		description: draft.description,
		fields,
		success: {message: s.success_message, redirect_url: s.redirect_url, submit_another: s.submit_another},
		submitter: {
			collect_name: isPublic && s.name_mode !== 'off',
			name_required: isPublic && s.name_mode === 'required',
			collect_email: isPublic && s.email_mode !== 'off',
			email_required: isPublic && s.email_mode === 'required',
			stores_account: draft.access === 'members',
		},
		attachments: fields.some(field => field.kind === 'files') ? {max_files: 5, max_file_size: 10 * 1024 * 1024} : null,
		branding: {name: draft.brand_name.trim(), accent_color: draft.accent_color, has_logo: false, hide_footer: draft.hide_footer},
	}
}

// ---- what a failed call means for the page ----------------------------------------------------------------------

export type PublicProblem =
	| {kind: 'notFound'}
	| {kind: 'unavailable', reason: string, title: string}
	| {kind: 'login'}
	| {kind: 'forbidden'}
	| {kind: 'rate', retryAfter: number}
	| {kind: 'network'}
	| {kind: 'other', message: string}

export function classifyPublicProblem(cause: unknown): PublicProblem {
	const problem = (cause && typeof cause === 'object' ? cause : {}) as {
		status?: number
		code?: number
		detail?: string
		message?: string
		details?: {reason?: unknown, title?: unknown, retry_after?: unknown}
	}
	const status = problem.status
	if (typeof status !== 'number') return {kind: 'network'}
	if (status === 404) return {kind: 'notFound'}
	if (status === 410) {
		return {
			kind: 'unavailable',
			reason: typeof problem.details?.reason === 'string' ? problem.details.reason : 'disabled',
			title: typeof problem.details?.title === 'string' ? problem.details.title : '',
		}
	}
	if (status === 401) return {kind: 'login'}
	if (status === 403) return {kind: 'forbidden'}
	if (status === 429) {
		const seconds = Number(problem.details?.retry_after)
		return {kind: 'rate', retryAfter: Number.isFinite(seconds) && seconds > 0 ? seconds : 0}
	}
	return {kind: 'other', message: problem.detail || problem.message || ''}
}

export function redirectTarget(url: string): URL | null {
	try {
		const target = new URL(url)
		return (target.protocol === 'http:' || target.protocol === 'https:') && target.host !== '' ? target : null
	} catch {
		return null
	}
}

// ---- branding ------------------------------------------------------------------------------------------------------

// The CSS variables that turn the page's primary color into the accent color. Empty for no or an invalid accent.
// The text on the accent is chosen for contrast (--fp-button-text, applied to the buttons by the page).
// --primary and its derived variables are declared on :root as hsla(var(--primary-h) ...) and resolved there, so
// changing only the hue/saturation/lightness parts below the root has no effect on descendants: every variable that
// colors the page is emitted resolved, with the formulas of styles/custom-properties/colors.scss.
// Relative luminance of an hsl() color, h in degrees, s and l in 0..1.
function hslLuminance(h: number, s: number, l: number): number {
	const a = s * Math.min(l, 1 - l)
	const channel = (n: number) => {
		const k = (n + h / 30) % 12
		return l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))
	}
	const lin = (c: number) => c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
	return 0.2126 * lin(channel(0)) + 0.7152 * lin(channel(8)) + 0.0722 * lin(channel(4))
}

// The card of the public page: white, or the dark grey of the dark theme (--grey-50 there, hsl(221, 39%, 11%)).
const CARD_LIGHT_LUMINANCE = 1
const CARD_DARK_LUMINANCE = hslLuminance(221, 0.393, 0.11)

// The lightness nearest to the accent's at which text of the accent color is readable (4.5:1) on the card. Links are text,
// so a pale accent is darkened on the white card and a dark accent lightened on the dark one.
function readableLightness(h: number, s: number, l: number, cardLuminance: number): number {
	const direction = cardLuminance > 0.5 ? -1 : 1
	let lightness = l
	for (let i = 0; i < 100; i++) {
		const luminance = hslLuminance(h, s, lightness)
		const [hi, lo] = luminance > cardLuminance ? [luminance, cardLuminance] : [cardLuminance, luminance]
		if ((hi + 0.05) / (lo + 0.05) >= 4.5 || lightness <= 0 || lightness >= 1) break
		lightness = Math.min(1, Math.max(0, lightness + direction * 0.01))
	}
	return lightness
}

export function accentStyle(accent: string): Record<string, string> {
	const m = /^#([0-9a-f]{6})$/i.exec(accent)
	if (!m) return {}
	const n = parseInt(m[1] as string, 16)
	const r = (n >> 16) / 255
	const g = ((n >> 8) & 255) / 255
	const b = (n & 255) / 255
	const max = Math.max(r, g, b)
	const min = Math.min(r, g, b)
	const l = (max + min) / 2
	const d = max - min
	let h = 0
	let s = 0
	if (d !== 0) {
		s = d / (1 - Math.abs(2 * l - 1))
		if (max === r) h = ((g - b) / d) % 6
		else if (max === g) h = (b - r) / d + 2
		else h = (r - g) / d + 4
		h = (h * 60 + 360) % 360
	}
	const lin = (c: number) => c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
	const luminance = 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
	const hue = `${Math.round(h)}deg`
	const sat = `${Math.round(s * 100)}%`
	const light = Math.round(l * 100)
	const hsla = (lightness: number, alpha = 1) => `hsla(${hue}, ${sat}, ${lightness}%, ${alpha})`
	const link = (card: number) => Math.round(readableLightness(h, s, l, card) * 100)
	return {
		'--primary-h': hue,
		'--primary-s': sat,
		'--primary-l': `${light}%`,
		'--primary-a': '1',
		'--primary': hsla(light),
		'--primary-light': hsla(96),
		'--primary-dark': hsla(Math.max(light - 12, 0)),
		// The page picks one of these per theme (PublicFormPage.vue): a link has to be readable on the card, a button only
		// has to have readable text.
		'--fp-link': hsla(link(CARD_LIGHT_LUMINANCE)),
		'--fp-link-hover': hsla(link(CARD_LIGHT_LUMINANCE), 0.75),
		'--fp-link-dark': hsla(link(CARD_DARK_LUMINANCE)),
		'--fp-link-dark-hover': hsla(link(CARD_DARK_LUMINANCE), 0.75),
		// Whichever of black and white has the better contrast on the accent.
		'--fp-button-text': luminance > 0.179 ? '#000000' : '#ffffff',
	}
}
