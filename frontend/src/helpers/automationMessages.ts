import {i18n} from '@/i18n'
import {translatedReason} from '@/message'
import {formatDateLong} from '@/helpers/time/formatDate'
import {secondsToPeriod} from '@/helpers/time/period'

// What the server tells about an automation run (why a step failed, what it did, which warnings it has) comes as an
// English sentence and as a stable code with values. The code is looked up as automation.messages.<code>; a code of
// the models is a number and is looked up like any API error (error.<code>); a code this client does not know shows
// the English sentence of the server instead.

export interface MessageLike {
	code?: string | null
	params?: {[key: string]: string} | null
}

type Params = {[key: string]: string}

// Dates arrive as RFC 3339 text and are shown in the format of the reader.
const DATE_PARAMS = ['until', 'date']

const INTL_UNIT = {seconds: 'second', minutes: 'minute', hours: 'hour', days: 'day', weeks: 'week', months: 'month', years: 'year'} as const

// A number of seconds as people say it: "720 saat".
function formatSeconds(seconds: number): string {
	const {unit, amount} = secondsToPeriod(seconds)
	return new Intl.NumberFormat(i18n.global.locale.value, {style: 'unit', unit: INTL_UNIT[unit], unitDisplay: 'long'}).format(amount)
}

// Values the server sends in the form of its own: a ratio as 0.5, a limit in seconds.
function prepareValues(code: string, out: Params) {
	if (code === 'set_percent_done' && out.value !== undefined && !Number.isNaN(Number(out.value))) {
		out.value = new Intl.NumberFormat(i18n.global.locale.value, {style: 'percent', maximumFractionDigits: 1}).format(Number(out.value))
	}
	if (code === 'wait_too_long' && out.max !== undefined && !Number.isNaN(Number(out.max))) {
		out.max = formatSeconds(Number(out.max))
	}
}

function prepare(params: Params | null | undefined, code = ''): Params {
	const out: Params = {...params}
	prepareValues(code, out)
	for (const name of DATE_PARAMS) {
		if (typeof out[name] === 'string' && out[name] !== '') out[name] = formatDateLong(out[name])
	}
	return out
}

function lookup(key: string, params: Params): string | null {
	const {t, te} = i18n.global
	return te(key) ? String(t(key, params)) : null
}

/**
 * The text of a code with its values in the language of the reader, or `fallback` (the English text of the server)
 * when the code is empty or unknown.
 */
export function automationCodeText(code: string | null | undefined, params: Params | null | undefined, fallback = ''): string {
	if (!code) return fallback
	const values = prepare(params, code)

	if (/^\d+$/.test(code)) {
		return lookup(`error.${code}`, translatedReason(values) as Params) ?? fallback
	}
	// A sentence that wraps another one: "nothing to do: <what it would have done>".
	if (code === 'nothing_to_do') {
		// The sentence goes after a colon: it starts with a small letter, in the casing of the language (İ and ı in Turkish).
		const locale = i18n.global.locale.value
		const innerText = automationCodeText(values.inner, params, '')
		const inner = innerText === '' ? '' : innerText.charAt(0).toLocaleLowerCase(locale) + innerText.slice(1)
		if (inner === '') return fallback
		return lookup(`automation.messages.${code}`, {...values, action: inner}) ?? fallback
	}
	// The conditions of a test failed with an error of their own.
	if (code === 'note_conditions_fail') {
		const error = automationCodeText(values.code, params, values.message ?? '')
		// Without a sentence for the failure, the English note of the server says more than a note with nothing after it.
		if (error === '') return fallback
		return lookup(`automation.messages.${code}`, {...values, error}) ?? fallback
	}
	return lookup(`automation.messages.${code}`, values) ?? fallback
}

export function automationMessageText(message: MessageLike | null | undefined, fallback = ''): string {
	return automationCodeText(message?.code, message?.params, fallback)
}

/**
 * The output of a step: the leading lines the server wrote itself, said in the language of the reader, then the data
 * after them (a log, an HTTP answer) as it is. `messages` has one entry per leading line of `output`.
 */
export function automationOutput(output: string, messages: MessageLike[] | null | undefined): {sentences: string[], rest: string} {
	const lines = output.split('\n')
	if (!messages || messages.length === 0) return {sentences: [], rest: output}
	const sentences = messages.map((message, index) => automationMessageText(message, lines[index] ?? ''))
	return {sentences, rest: lines.slice(messages.length).join('\n')}
}
