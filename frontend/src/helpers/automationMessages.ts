import {i18n} from '@/i18n'
import {formatDateLong} from '@/helpers/time/formatDate'

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

function prepare(params: Params | null | undefined): Params {
	const out: Params = {...params}
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
	const values = prepare(params)

	if (/^\d+$/.test(code)) {
		return lookup(`error.${code}`, values) ?? fallback
	}
	// A sentence that wraps another one: "nothing to do: <what it would have done>".
	if (code === 'nothing_to_do' || code === 'task_created') {
		const inner = automationCodeText(values.inner, params, '')
		if (inner === '') return fallback
		return lookup(`automation.messages.${code}`, {...values, action: inner}) ?? fallback
	}
	// The conditions of a test failed with an error of their own.
	if (code === 'note_conditions_fail') {
		const error = automationCodeText(values.code, params, values.message ?? '')
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
