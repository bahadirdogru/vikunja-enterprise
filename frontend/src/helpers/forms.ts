// Form definitions: the draft the builder edits, the document the API takes, client-side checks that mirror the server
// and the mapping of server issues onto builder controls.
import type {Form, FormOptions, FormWritable} from '@/client/generated'
import type {CustomFieldConfig} from '@/client/generated'
import type {CustomFieldResponse} from '@/client/queries/customFields'
import {validateCustomFieldValue, type CustomFieldType} from '@/helpers/customFields'
import {isValidHttpUrl} from '@/helpers/isValidHttpUrl'

export const FORM_ERROR = {
	NOT_FOUND: 25001,
	INVALID: 25002,
	UNAVAILABLE: 25003,
	LOGIN_REQUIRED: 25004,
	NOT_ALLOWED: 25005,
	INVALID_ANSWERS: 25006,
	RATE_LIMITED: 25007,
	DISABLED: 25008,
	DATA_INVALID: 25010,
	DATA_TOO_LARGE: 25011,
} as const

export type Translate = (key: string, params?: Record<string, unknown>) => string

export type FormAccess = 'members' | 'users' | 'public'
export const FORM_ACCESS_MODES: FormAccess[] = ['members', 'users', 'public']

export const PLACEHOLDERS = ['submitter.name', 'submitter.email', 'form.title', 'date'] as const

export const MAX_FIELDS = 50
export const MAX_TITLE = 250
export const MAX_HELP = 1000
export const MAX_MESSAGE = 2000
export const MAX_URL = 2000
export const MAX_TITLE_DEFAULT = 1000
export const MAX_LONG_TEXT = 50000
export const MAX_PRIORITY = 5

export const BUILTIN_TARGETS = ['title', 'description', 'due_date', 'priority', 'labels', 'attachments'] as const

// ---- options (what a field can target) ---------------------------------------------------------------------------

export type TargetOption = {id: number, title: string, color: string}

export type TargetInfo = {
	target: string
	kind: string
	title: string
	custom_field_id: number
	config: CustomFieldConfig | null
	options: TargetOption[]
	public_visible: boolean
	public_hidden: boolean
}

export type FormOptionsResponse = {
	targets: TargetInfo[]
	public_enabled: boolean
	public_attachments: boolean
}

export function normalizeOptions(data: FormOptions): FormOptionsResponse {
	return {
		targets: (data.targets ?? []).map(info => ({
			target: info.target ?? '',
			kind: info.kind ?? '',
			title: info.title ?? '',
			custom_field_id: info.custom_field_id ?? 0,
			config: info.config ?? null,
			options: (info.options ?? []).map(option => ({
				id: option.id ?? 0,
				title: option.title ?? '',
				color: option.color ?? '',
			})),
			public_visible: info.public_visible ?? false,
			public_hidden: info.public_hidden ?? false,
		})),
		public_enabled: data.public_enabled ?? false,
		public_attachments: data.public_attachments ?? false,
	}
}

export function findTarget(options: FormOptionsResponse, target: string): TargetInfo | undefined {
	return options.targets.find(info => info.target === target)
}

export function isCustomTarget(target: string): boolean {
	return /^cf_[1-9][0-9]*$/.test(target)
}

// The name the builder shows for a target.
export function targetName(info: Pick<TargetInfo, 'target' | 'title'>, t: Translate): string {
	return isCustomTarget(info.target) ? info.title : t(`forms.target.${info.target}`)
}

// The editors of custom fields take a field definition; a target carries the same facts.
export function targetAsCustomField(info: TargetInfo, projectId: number): CustomFieldResponse {
	return {
		id: info.custom_field_id,
		project_id: projectId,
		title: info.title,
		description: '',
		type: info.kind as CustomFieldType,
		required: false,
		position: 0,
		config: info.config ?? undefined,
		options: info.options.map((option, index) => ({...option, position: index})),
	} as CustomFieldResponse
}

// ---- draft -------------------------------------------------------------------------------------------------------

export type SubmitterMode = 'off' | 'optional' | 'required'

export type FieldDraft = {
	// Stable v-for key and DOM id part; never sent.
	key: string
	target: string
	label: string
	help: string
	required: boolean
	hidden: boolean
	default: unknown
	options: number[]
}

export type SettingsDraft = {
	initial_status_id: number
	default_assignee_ids: number[]
	success_message: string
	redirect_url: string
	submit_another: boolean
	close_date: string | null
	max_submissions: number
	notify_user_ids: number[]
	name_mode: SubmitterMode
	email_mode: SubmitterMode
}

export type FormDraft = {
	title: string
	description: string
	access: FormAccess
	enabled: boolean
	// Branding of the public page. The logo is uploaded on its own, not as part of the draft.
	brand_name: string
	accent_color: string
	hide_footer: boolean
	fields: FieldDraft[]
	settings: SettingsDraft
}

export const MAX_BRAND_NAME = 100
export const MAX_LOGO_BYTES = 1024 * 1024
export const LOGO_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'] as const

const ACCENT_COLOR = /^#[0-9a-fA-F]{6}$/

// Empty means the default color.
export function isValidAccentColor(value: string): boolean {
	return value === '' || ACCENT_COLOR.test(value)
}

// The reason a file cannot be a logo, or null. The server checks again, by content.
export function logoProblem(file: {size: number, type: string}): 'type' | 'size' | null {
	if (!(LOGO_TYPES as readonly string[]).includes(file.type)) return 'type'
	return file.size > MAX_LOGO_BYTES ? 'size' : null
}

let keyCounter = 0
export function newFieldKey(): string {
	keyCounter += 1
	return `f${keyCounter}`
}

export function emptySettings(): SettingsDraft {
	return {
		initial_status_id: 0,
		default_assignee_ids: [],
		success_message: '',
		redirect_url: '',
		submit_another: false,
		close_date: null,
		max_submissions: 0,
		notify_user_ids: [],
		name_mode: 'off',
		email_mode: 'off',
	}
}

export function emptyDraft(): FormDraft {
	return {
		title: '',
		description: '',
		access: 'members',
		enabled: true,
		brand_name: '',
		accent_color: '',
		hide_footer: false,
		fields: [],
		settings: emptySettings(),
	}
}

export function newField(info: TargetInfo, t: Translate, access: FormAccess): FieldDraft {
	const preset = access === 'public' && !info.public_visible && info.public_hidden
	return {
		key: newFieldKey(),
		target: info.target,
		label: preset ? '' : targetName(info, t),
		help: '',
		required: info.target === 'title',
		hidden: preset,
		default: null,
		options: [],
	}
}

function modeOf(collect: boolean | undefined, required: boolean | undefined): SubmitterMode {
	if (!collect) return 'off'
	return required ? 'required' : 'optional'
}

function dateOrNull(value: string | undefined | null): string | null {
	return !value || value.startsWith('0001-') ? null : value
}

export function draftFromForm(form: Form): FormDraft {
	const settings = form.settings ?? {}
	return {
		title: form.title ?? '',
		description: form.description ?? '',
		access: (form.access ?? 'members') as FormAccess,
		enabled: form.enabled ?? false,
		brand_name: form.brand_name ?? '',
		accent_color: form.accent_color ?? '',
		hide_footer: form.hide_footer ?? false,
		fields: (form.fields ?? []).map(field => ({
			key: newFieldKey(),
			target: field.target ?? '',
			label: field.label ?? '',
			help: field.help ?? '',
			required: field.required ?? false,
			hidden: field.hidden ?? false,
			default: field.default ?? null,
			options: [...(field.options ?? [])],
		})),
		settings: {
			initial_status_id: settings.initial_status_id ?? 0,
			default_assignee_ids: [...(settings.default_assignee_ids ?? [])],
			success_message: settings.success_message ?? '',
			redirect_url: settings.redirect_url ?? '',
			submit_another: settings.submit_another ?? false,
			close_date: dateOrNull(settings.close_date),
			max_submissions: settings.max_submissions ?? 0,
			notify_user_ids: [...(settings.notify_user_ids ?? [])],
			name_mode: modeOf(settings.collect_name, settings.name_required),
			email_mode: modeOf(settings.collect_email, settings.email_required),
		},
	}
}

export function isEmptyDefault(value: unknown): boolean {
	if (value === null || value === undefined) return true
	if (typeof value === 'string') return value.trim() === ''
	if (Array.isArray(value)) return value.length === 0
	return false
}

export function documentFromDraft(draft: FormDraft): FormWritable {
	const isPublic = draft.access === 'public'
	const s = draft.settings
	const collectName = isPublic && s.name_mode !== 'off'
	const collectEmail = isPublic && s.email_mode !== 'off'
	return {
		title: draft.title.trim(),
		description: draft.description,
		access: draft.access,
		enabled: draft.enabled,
		brand_name: draft.brand_name.trim(),
		accent_color: draft.accent_color,
		hide_footer: draft.hide_footer,
		fields: draft.fields.map(field => ({
			target: field.target,
			label: field.label.trim(),
			help: field.help.trim(),
			required: field.required,
			hidden: field.hidden,
			default: isEmptyDefault(field.default) ? null : field.default,
			...(field.target === 'labels' && !field.hidden && field.options.length > 0 ? {options: [...field.options]} : {}),
		})),
		settings: {
			initial_status_id: s.initial_status_id,
			default_assignee_ids: [...s.default_assignee_ids],
			success_message: s.success_message,
			redirect_url: s.redirect_url.trim(),
			submit_another: s.submit_another,
			close_date: s.close_date,
			max_submissions: s.max_submissions,
			notify_user_ids: [...s.notify_user_ids],
			collect_name: collectName,
			name_required: collectName && s.name_mode === 'required',
			collect_email: collectEmail,
			email_required: collectEmail && s.email_mode === 'required',
		},
	}
}

// Field keys are random per load, so the signature leaves them out.
export function draftSignature(draft: FormDraft): string {
	return JSON.stringify({...draft, fields: draft.fields.map(field => ({...field, key: undefined}))})
}

export function cloneDraft(draft: FormDraft): FormDraft {
	return JSON.parse(JSON.stringify(draft)) as FormDraft
}

export function moveField(fields: FieldDraft[], index: number, delta: -1 | 1): FieldDraft[] {
	const target = index + delta
	if (index < 0 || index >= fields.length || target < 0 || target >= fields.length) return fields
	const copy = [...fields]
	const [moved] = copy.splice(index, 1)
	copy.splice(target, 0, moved as FieldDraft)
	return copy
}

// ---- rules about access ------------------------------------------------------------------------------------------

export type AddState = {allowed: boolean, presetOnly: boolean, reason: '' | 'used' | 'public' | 'limit'}

// Whether a target can be added now. On a public form a target that may not be asked can still be a hidden preset.
export function addState(info: TargetInfo, draft: FormDraft): AddState {
	if (draft.fields.some(field => field.target === info.target)) return {allowed: false, presetOnly: false, reason: 'used'}
	if (draft.fields.length >= MAX_FIELDS) return {allowed: false, presetOnly: false, reason: 'limit'}
	if (draft.access === 'public' && !info.public_visible) {
		return info.public_hidden
			? {allowed: true, presetOnly: true, reason: ''}
			: {allowed: false, presetOnly: false, reason: 'public'}
	}
	return {allowed: true, presetOnly: false, reason: ''}
}

// The fields that stop the form from being public, with a short reason for each.
export function publicBlockers(draft: FormDraft, options: FormOptionsResponse): {index: number, target: string, reason: 'asked' | 'hidden'}[] {
	const out: {index: number, target: string, reason: 'asked' | 'hidden'}[] = []
	draft.fields.forEach((field, index) => {
		const info = findTarget(options, field.target)
		if (!info) return
		if (!field.hidden && !info.public_visible) out.push({index, target: field.target, reason: 'asked'})
		if (field.hidden && !info.public_hidden) out.push({index, target: field.target, reason: 'hidden'})
	})
	return out
}

export function titleRuleOk(draft: FormDraft): boolean {
	const field = draft.fields.find(f => f.target === 'title')
	if (!field) return false
	return !((field.hidden || !field.required) && isEmptyDefault(field.default))
}

export function unknownPlaceholder(text: string): string {
	for (const match of text.matchAll(/\{\{\s*([A-Za-z0-9_.]+)\s*\}\}/g)) {
		const name = match[1] as string
		if (!(PLACEHOLDERS as readonly string[]).includes(name)) return name
	}
	return ''
}

// ---- client-side validation --------------------------------------------------------------------------------------

export type Issue = {path: string, message: string}

function charCount(text: string): number {
	return [...text].length
}

// The date a default carries must parse like the server parses it.
export function isFormDate(value: unknown): boolean {
	if (typeof value !== 'string') return false
	const text = value.trim()
	return /^\d{4}-\d{2}-\d{2}$/.test(text)
		|| (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/.test(text) && !Number.isNaN(Date.parse(text)))
}

function defaultProblem(field: FieldDraft, info: TargetInfo, t: Translate): string {
	const value = field.default
	if (isEmptyDefault(value)) return ''
	switch (info.target) {
		case 'title':
		case 'description': {
			if (typeof value !== 'string') return t('forms.errors.string')
			const limit = info.target === 'title' ? MAX_TITLE_DEFAULT : MAX_LONG_TEXT
			if (charCount(value) > limit) return t('forms.errors.tooLong', {max: limit})
			const unknown = unknownPlaceholder(value)
			return unknown ? t('forms.errors.placeholder', {name: unknown}) : ''
		}
		case 'due_date':
			return isFormDate(value) ? '' : t('forms.errors.date')
		case 'priority':
			return typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= MAX_PRIORITY
				? '' : t('forms.errors.priority', {max: MAX_PRIORITY})
		case 'labels':
			return Array.isArray(value) && value.every(id => typeof id === 'number') ? '' : t('forms.errors.labels')
		case 'attachments':
			return t('forms.errors.attachmentsDefault')
	}
	if (typeof value === 'string' && info.kind !== 'url' && info.kind !== 'email' && info.kind !== 'date') {
		const unknown = unknownPlaceholder(value)
		if (unknown) return t('forms.errors.placeholder', {name: unknown})
	}
	const invalid = validateCustomFieldValue(
		{type: info.kind, config: info.config, options: info.options.map(option => ({id: option.id}))},
		value,
	)
	return invalid ? t(invalid.key, invalid.params ?? {}) : ''
}

export function validateDraft(draft: FormDraft, options: FormOptionsResponse, t: Translate): Issue[] {
	const issues: Issue[] = []
	const add = (path: string, message: string) => issues.push({path, message})

	const title = draft.title.trim()
	if (title === '') add('title', t('forms.errors.titleRequired'))
	else if (charCount(title) > MAX_TITLE) add('title', t('forms.errors.tooLong', {max: MAX_TITLE}))
	if (draft.access === 'public' && !options.public_enabled) add('access', t('forms.errors.publicDisabled'))
	if (charCount(draft.brand_name.trim()) > MAX_BRAND_NAME) add('brand_name', t('forms.errors.tooLong', {max: MAX_BRAND_NAME}))
	if (!isValidAccentColor(draft.accent_color)) add('accent_color', t('forms.errors.accentColor'))
	if (draft.fields.length > MAX_FIELDS) {
		add('fields', t('forms.errors.tooManyFields', {max: MAX_FIELDS}))
		return issues
	}

	const seen = new Set<string>()
	draft.fields.forEach((field, index) => {
		const path = `fields[${index}]`
		const info = findTarget(options, field.target)
		if (!info) {
			add(`${path}.target`, t('forms.errors.targetGone'))
			return
		}
		if (seen.has(field.target)) {
			add(`${path}.target`, t('forms.errors.targetTwice'))
			return
		}
		seen.add(field.target)

		const label = field.label.trim()
		if (charCount(label) > MAX_TITLE) add(`${path}.label`, t('forms.errors.tooLong', {max: MAX_TITLE}))
		if (charCount(field.help.trim()) > MAX_HELP) add(`${path}.help`, t('forms.errors.tooLong', {max: MAX_HELP}))
		if (!field.hidden && label === '') add(`${path}.label`, t('forms.errors.labelRequired'))
		if (field.hidden && field.required && isEmptyDefault(field.default)) {
			add(`${path}.default`, t('forms.errors.hiddenNeedsDefault'))
		}
		if (field.target === 'title' && (field.hidden || !field.required) && isEmptyDefault(field.default)) {
			add(`${path}.default`, t('forms.errors.titleRule'))
		}

		if (draft.access === 'public') {
			if (!field.hidden && !info.public_visible) {
				add(`${path}.target`, t(field.target === 'attachments' ? 'forms.errors.attachmentsPublic' : 'forms.errors.notPublic'))
			} else if (field.hidden && !info.public_hidden) {
				add(`${path}.hidden`, t('forms.errors.hiddenNotAllowed'))
			}
		}
		if (field.target === 'attachments' && field.hidden) add(`${path}.hidden`, t('forms.errors.attachmentsHidden'))

		if (field.target === 'labels') {
			if (!field.hidden && field.options.length === 0) add(`${path}.options`, t('forms.errors.labelOptions'))
			if (Array.isArray(field.default) && !field.hidden) {
				if (field.default.some(id => !field.options.includes(id as number))) {
					add(`${path}.default`, t('forms.errors.labelDefaultInOptions'))
				}
			}
		}
		const problem = defaultProblem(field, info, t)
		if (problem) add(`${path}.default`, problem)
	})

	if (!draft.fields.some(field => field.target === 'title')) add('fields', t('forms.errors.titleRule'))

	const s = draft.settings
	if (s.success_message.length > MAX_MESSAGE) add('settings.success_message', t('forms.errors.tooLong', {max: MAX_MESSAGE}))
	const url = s.redirect_url.trim()
	if (url !== '' && (url.length > MAX_URL || !isValidHttpUrl(url) || new URL(url).host === '')) {
		add('settings.redirect_url', t('forms.errors.redirect'))
	}
	if (!Number.isInteger(s.max_submissions) || s.max_submissions < 0) add('settings.max_submissions', t('forms.errors.maxSubmissions'))
	return issues
}

export function issuesFromProblem(problem: unknown): Issue[] | null {
	if (!problem || typeof problem !== 'object') return null
	const details = (problem as {details?: unknown}).details
	const raw = details && typeof details === 'object' ? (details as {issues?: unknown}).issues : undefined
	if (!Array.isArray(raw)) return null
	return raw
		.filter((item): item is {path?: unknown, message?: unknown} => !!item && typeof item === 'object')
		.map(item => ({path: String(item.path ?? ''), message: String(item.message ?? '')}))
		.filter(item => item.message !== '')
}

// ---- issue path -> tab and control -------------------------------------------------------------------------------

export type BuilderTab = 'fields' | 'settings' | 'share'

export type Locator = {
	tab: BuilderTab | null
	// Index into draft.fields for issues inside one field.
	fieldIndex: number | null
	// The control within the field or the tab.
	control: string
}

const SETTINGS_CONTROLS: Record<string, string> = {
	initial_status_id: 'initial_status_id',
	default_assignee_ids: 'default_assignee_ids',
	success_message: 'success_message',
	redirect_url: 'redirect_url',
	submit_another: 'submit_another',
	close_date: 'close_date',
	max_submissions: 'max_submissions',
	notify_user_ids: 'notify_user_ids',
	collect_name: 'name_mode',
	name_required: 'name_mode',
	collect_email: 'email_mode',
	email_required: 'email_mode',
}

export function locateIssue(path: string): Locator {
	const field = /^fields\[(\d+)\](?:\.([a-z_]+))?/.exec(path)
	if (field) {
		const prop = field[2] ?? 'target'
		const control = ['label', 'help', 'required', 'hidden', 'default', 'options'].includes(prop) ? prop : 'head'
		return {tab: 'fields', fieldIndex: Number(field[1]), control}
	}
	if (path === 'fields' || path.startsWith('fields.')) return {tab: 'fields', fieldIndex: null, control: 'list'}
	if (path === 'title') return {tab: null, fieldIndex: null, control: 'title'}
	if (path === 'description') return {tab: null, fieldIndex: null, control: 'description'}
	if (path === 'access') return {tab: 'settings', fieldIndex: null, control: 'access'}
	if (path === 'brand_name' || path === 'accent_color' || path === 'hide_footer') {
		return {tab: 'settings', fieldIndex: null, control: path}
	}
	const settings = /^settings\.([a-z_]+)/.exec(path)
	if (settings) {
		return {tab: 'settings', fieldIndex: null, control: SETTINGS_CONTROLS[settings[1] as string] ?? 'access'}
	}
	return {tab: null, fieldIndex: null, control: 'title'}
}

// The DOM id of the control an issue belongs to; fields need the draft to turn an index into a key.
export function controlId(locator: Locator, draft: FormDraft): string {
	if (locator.fieldIndex !== null) {
		const field = draft.fields[locator.fieldIndex]
		return field ? `fb-f-${field.key}-${locator.control}` : 'fb-fields-list'
	}
	if (locator.tab === null) return `fb-${locator.control}`
	return locator.tab === 'fields' ? 'fb-fields-list' : `fb-s-${locator.control}`
}

// Issues grouped by their control id, each with the tab it lives in.
export function mapIssues(issues: Issue[], draft: FormDraft): {
	byId: Map<string, string[]>
	tabs: Map<string, BuilderTab | null>
	tabCounts: Record<BuilderTab, number>
} {
	const byId = new Map<string, string[]>()
	const tabs = new Map<string, BuilderTab | null>()
	const tabCounts: Record<BuilderTab, number> = {fields: 0, settings: 0, share: 0}
	for (const issue of issues) {
		const locator = locateIssue(issue.path)
		const id = controlId(locator, draft)
		byId.set(id, [...(byId.get(id) ?? []), issue.message])
		tabs.set(id, locator.tab)
		if (locator.tab) tabCounts[locator.tab] += 1
	}
	return {byId, tabs, tabCounts}
}

// ---- presets -----------------------------------------------------------------------------------------------------

export type Preset = {id: string, title: string, description: string, draft: FormDraft}

export function availablePresets(options: FormOptionsResponse, t: Translate): Preset[] {
	const has = (target: string) => options.targets.some(info => info.target === target)
	if (!has('title')) return []
	const make = (id: string, targets: string[], labels: Record<string, string>): Preset => {
		const draft = emptyDraft()
		draft.title = t(`forms.presets.${id}.formTitle`)
		draft.fields = targets.filter(has).map(target => {
			const info = findTarget(options, target) as TargetInfo
			const field = newField(info, t, 'members')
			field.label = labels[target] ?? field.label
			field.required = target === 'title'
			return field
		})
		draft.settings.success_message = t('forms.presets.thanks')
		draft.settings.submit_another = true
		return {id, title: t(`forms.presets.${id}.title`), description: t(`forms.presets.${id}.text`), draft}
	}
	return [
		make('simple', ['title', 'description'], {
			title: t('forms.presets.simple.title_label'),
			description: t('forms.presets.simple.description_label'),
		}),
		make('dueDate', ['title', 'description', 'due_date'], {
			title: t('forms.presets.simple.title_label'),
			description: t('forms.presets.simple.description_label'),
			due_date: t('forms.presets.dueDate.due_label'),
		}),
		make('bug', ['title', 'description', 'priority'], {
			title: t('forms.presets.bug.title_label'),
			description: t('forms.presets.bug.description_label'),
			priority: t('forms.presets.bug.priority_label'),
		}),
	]
}

// ---- helpers shared by the pages ---------------------------------------------------------------------------------

export function formLink(token: string): string {
	return `${window.location.origin}${import.meta.env.BASE_URL.replace(/\/$/, '')}/forms/${token}`
}

export function embedSnippet(link: string, title: string): string {
	const safeTitle = title.replace(/[<>"&]/g, '')
	return `<iframe src="${link}" title="${safeTitle}" width="100%" height="720" style="border:0" loading="lazy"></iframe>`
}
