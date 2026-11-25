import {i18n} from '@/i18n'
import {notify} from '@kyvg/vue3-notification'

function errorRecord(value: unknown): Record<string, unknown> {
	return value !== null && typeof value === 'object' ? value as Record<string, unknown> : {}
}

// Some errors name their reason in code (reason_code) besides the English text: the reason is said in the language of
// the reader. reason_type and reason_limit are the values the text of the reason mentions.
function translatedReason(params: Record<string, unknown>): Record<string, unknown> {
	const key = `customFields.reasons.${String(params.reason_code)}`
	if (typeof params.reason_code !== 'string' || !i18n.global.te(key)) return params
	return {...params, reason: i18n.global.t(key, {type: params.reason_type, limit: params.reason_limit})}
}

// Errors that carry no code of their own and whose English text comes from the web server or the framework: a
// failed validation of the API (422), a refusal of the gateway, a request that never reached the server.
function genericErrorText(original: Record<string, unknown>, data: Record<string, unknown>): string {
	const {t, te} = i18n.global
	const status = typeof data.status === 'number' ? data.status : undefined
	if (status === 422) {
		const fields = Array.isArray(data.errors)
			? [...new Set(data.errors.map(e => String(errorRecord(e).location ?? '').replace(/^body\./, '')).filter(Boolean))]
			: []
		return fields.length > 0 ? `${t('error.validation')}\n${fields.join(', ')}` : t('error.validation')
	}
	if (status !== undefined && te(`error.http${status}`)) return t(`error.http${status}`)
	if (status !== undefined && status >= 500 && status < 600) return t('error.http5xx')
	if (status === undefined && (original.message === 'Network Error' || original.message === 'Failed to fetch' || original.message === 'Load failed')) {
		return t('error.network')
	}
	return ''
}

export function getErrorText(reason: unknown): string {
	const original = errorRecord(reason)
	// The dev rejection handler passes the event rather than its reason.
	const data = errorRecord(original.reason ?? reason)
	const code = data.code

	if (typeof code === 'number' && code) {
		const path = `error.${code}`
		let message = i18n.global.t(path, translatedReason(errorRecord(data.i18n_params)))
		if (typeof data.message === 'string' && [4016, 4017, 4018, 4019, 4024, 4031, 4033, 14007].includes(code)) {
			message += '\n' + data.message
		}
		if (path !== message) return message
	}

	const generic = genericErrorText(original, data)
	if (generic !== '') return generic

	// v2 problem responses carry detail instead of message.
	const fallback = data.message || data.detail || original.message || original.reason || reason
	let message = typeof fallback === 'string' ? fallback : ''
	const cause = errorRecord(original.cause)
	const causeMessage = cause.detail ?? cause.message
	if (typeof causeMessage === 'string') message += ' ' + causeMessage
	return message
}

export function translatedError(key: string): Error {
	return new Error(i18n.global.t(key))
}

export interface Action {
	title: string,
	callback: () => void,
}

export type ToastOptions = {
	takeFocus?: boolean,
}

export type ToastData = {
	actions: Action[],
	takeFocus: boolean,
}

// Long enough to reach the action; Notification.vue pauses it while the toast is hovered or focused
export const ACTION_TOAST_DURATION = 10_000

function toast(type: 'error' | 'success', title: string, e: unknown, actions: Action[], options: ToastOptions & {duration?: number} = {}) {
	const data: ToastData = {
		actions,
		takeFocus: options.takeFocus ?? false,
	}
	notify({
		type,
		title,
		text: getErrorText(e),
		// Duplicates merge on title and text, which would drop this toast's own actions.
		ignoreDuplicates: actions.length === 0,
		// Notification.vue runs the pausable timer instead
		duration: actions.length > 0 ? -1 : options.duration,
		data,
	})
}

const answeredErrors = new WeakSet<object>()

// An error a caller already explained to the user in its own way. Callers that let it propagate
// (an async event handler ends in the global handler) must not get a second, raw toast for it.
export function markErrorAnswered(e: unknown) {
	if (e !== null && typeof e === 'object') answeredErrors.add(e)
}

export function error(e: unknown, actions: Action[] = []) {
	if (e !== null && typeof e === 'object' && answeredErrors.has(e)) return
	// The 3s library default is too short to notice and read an error.
	toast('error', i18n.global.t('error.error'), e, actions, {duration: 10_000})
}

export function success(e: unknown, actions: Action[] = [], options: ToastOptions = {}) {
	toast('success', i18n.global.t('error.success'), e, actions, options)
}
