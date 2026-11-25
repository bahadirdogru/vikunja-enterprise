import {describe, expect, it} from 'vitest'
import {readFileSync} from 'node:fs'
import {resolve as resolvePath} from 'node:path'

import {ApiError} from '@/client/problem'
import {getErrorText} from '@/message'

type Messages = {[key: string]: string | Messages}

// Read from disk: the i18n plugin precompiles the messages of a JSON import.
const load = (name: string) => JSON.parse(readFileSync(resolvePath(process.cwd(), 'src/i18n/lang', `${name}.json`), 'utf-8')) as Messages
const en = (load('en').error as Messages)
const tr = (load('tr-TR').error as Messages)

// Every code the fork's backend defines in pkg/models/error_*.go (custom fields 20xxx, workflow 21xxx,
// automation 22xxx, reports 23xxx, approvals 24xxx, forms 25xxx), as a literal list so that a code
// added there without a translation here fails this test once it is added below.
const range = (from: number, to: number) => Array.from({length: to - from + 1}, (_, i) => from + i)
const FORK_CODES = [
	...range(20001, 20008), ...range(21001, 21011), ...range(22001, 22015),
	...range(23001, 23011), ...range(24001, 24010), ...range(25001, 25009),
]
// These two carry the reason of the failure as free English text from the server; a fixed translation would
// hide what is wrong, so the interface shows the server's detail.
const SERVER_REASON = [20002, 20005]
const TRANSLATED = FORK_CODES.filter(code => !SERVER_REASON.includes(code))

const placeholders = (text: string) => [...text.matchAll(/\{([A-Za-z0-9_]+)\}/g)].map(m => m[1]).sort()

describe('error codes of the fork modules', () => {
	it.each([['en', en], ['tr-TR', tr]])('has a text for every code in %s', (_name, messages) => {
		const missing = TRANSLATED.filter(code => typeof messages[String(code)] !== 'string' || messages[String(code)] === '')
		expect(missing).toEqual([])
	})

	it('keeps the placeholders of the English text in the Turkish one', () => {
		const differing = TRANSLATED.filter(code => placeholders(en[String(code)] as string).join() !== placeholders(tr[String(code)] as string).join())
		expect(differing).toEqual([])
	})

	it('has no fork code in the 20xxx-25xxx range that the list above does not know', () => {
		const known = new Set(FORK_CODES.map(String))
		const unknown = Object.keys(en).filter(key => /^2[0-5]\d{3}$/.test(key) && !known.has(key))
		expect(unknown).toEqual([])
	})

	it.each(TRANSLATED)('shows the translation of %i, not the English detail of the server', code => {
		const english = en[String(code)] as string
		// The server sends the numbers a text mentions as i18n_params.
		const i18n_params = Object.fromEntries(placeholders(english).map(name => [name, '7']))
		const text = getErrorText(new ApiError({status: 400, code, detail: 'RAW SERVER TEXT', i18n_params}))
		expect(text).not.toContain('RAW SERVER TEXT')
		expect(text).not.toBe(`error.${code}`)
		expect(text).toBe(english.replace(/\{\w+\}/g, '7'))
	})

	it.each(SERVER_REASON)('shows the server reason of %i, which has no fixed translation', code => {
		expect(getErrorText(new ApiError({status: 400, code, detail: 'The value is not valid: too long'}))).toBe('The value is not valid: too long')
	})

	it('shows the retry error of a run whose event was deleted (22015) in plain words', () => {
		const text = getErrorText(new ApiError({status: 409, code: 22015, detail: 'x'}))
		expect(text).toBe('This run cannot be retried: the event it was made for has been deleted.')
		expect(tr['22015']).toMatch(/yeniden/i)
	})
})
