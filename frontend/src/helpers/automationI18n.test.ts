import {describe, expect, it} from 'vitest'

import {readFileSync} from 'node:fs'
import {resolve as resolvePath} from 'node:path'
import catalogFixture from './automationCatalog.fixture.json'
import {catalogEnumEntries, catalogTitleKeys, normalizeCatalog} from '@/helpers/automation'

type Messages = {[key: string]: string | Messages}

// Read from disk: the i18n plugin precompiles the messages of a JSON import.
const load = (name: string) => JSON.parse(readFileSync(resolvePath(process.cwd(), 'src/i18n/lang', `${name}.json`), 'utf-8')) as Messages
const en = load('en')
const trTR = load('tr-TR')

function resolve(messages: Messages, key: string): string | undefined {
	let node: string | Messages | undefined = messages
	// Keys are dotted paths; a segment itself never contains a dot ("cf_<id>" does not).
	for (const part of key.split('.')) {
		if (typeof node !== 'object' || node === null) return undefined
		node = node[part]
	}
	return typeof node === 'string' ? node : undefined
}

const catalog = normalizeCatalog(catalogFixture as unknown as Record<string, unknown>)

describe('automation translations', () => {
	const keys = catalogTitleKeys(catalog)

	it('reads the catalog fixture', () => {
		expect(catalog.triggers.length).toBeGreaterThan(5)
		expect(catalog.actions.length).toBeGreaterThan(5)
		expect(keys.length).toBeGreaterThan(80)
	})

	it.each([['en', en], ['tr-TR', trTR]])('has every catalog title key in %s', (_name, messages) => {
		const missing = keys.filter(key => !resolve(messages as Messages, key))
		expect(missing).toEqual([])
	})

	it.each([['en', en], ['tr-TR', trTR]])('has a label for every enum value in %s', (_name, messages) => {
		const missing = catalogEnumEntries(catalog)
			.filter(({option, value}) => !resolve(messages as Messages, `automation.enum.${option}.${value}`)
				&& !resolve(messages as Messages, `automation.enum.${value}`))
			.map(({option, value}) => `${option}.${value}`)
		expect(missing).toEqual([])
	})

	it('has the same automation keys in English and Turkish', () => {
		const flatten = (node: Messages, prefix = ''): string[] => Object.entries(node).flatMap(([k, v]) =>
			typeof v === 'string' ? [prefix + k] : flatten(v, `${prefix}${k}.`))
		const english = flatten((en as unknown as {automation: Messages}).automation).sort()
		const turkish = flatten((trTR as unknown as {automation: Messages}).automation).sort()
		expect(turkish).toEqual(english)
	})

	it('translates the run statuses, skip reasons and disable reasons the API documents', () => {
		const wanted = [
			...['running', 'success', 'failed', 'skipped', 'aborted'].map(s => `automation.runStatus.${s}`),
			...['max_depth', 'rate_limit', 'self_trigger', 'conditions_not_met', 'flow_stopped', 'owner_not_admin'].map(s => `automation.skipReason.${s}`),
			...['owner_not_admin', 'owner_unavailable', 'too_many_failures', 'custom_field_deleted', 'status_deleted', 'duplicated', 'duplicated_unmapped_reference'].map(s => `automation.disabledReason.${s}`),
		]
		for (const messages of [en, trTR]) {
			expect(wanted.filter(key => !resolve(messages as Messages, key))).toEqual([])
		}
	})
})
