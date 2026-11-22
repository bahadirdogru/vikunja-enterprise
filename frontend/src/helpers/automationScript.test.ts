import {describe, expect, it} from 'vitest'

import catalogFixture from './automationCatalog.fixture.json'
import {normalizeCatalog} from '@/helpers/automation'
import {allowsSecretPlaceholder, apiFor, filterApi, insertAt, snippetsFor} from '@/helpers/automationScript'

const script = normalizeCatalog(catalogFixture as unknown as Record<string, unknown>).script

describe('script helpers', () => {
	it('hides the API a condition script cannot use', () => {
		const names = apiFor(script, 'condition').map(entry => entry.name)
		expect(names).toContain('vikunja.tasks.get')
		expect(names).not.toContain('vikunja.tasks.update')
		expect(names).not.toContain('secrets.get')
		expect(apiFor(script, 'action').map(entry => entry.name)).toContain('secrets.get')
		expect(apiFor(null, 'action')).toEqual([])
	})

	it('filters by name, signature and description', () => {
		const all = apiFor(script, 'action')
		expect(filterApi(all, 'FIELDS.SET').map(e => e.name)).toEqual(['vikunja.fields.set'])
		expect(filterApi(all, '  ')).toHaveLength(all.length)
		expect(filterApi(all, 'zzzz-nothing')).toEqual([])
	})

	it('inserts at the selection', () => {
		expect(insertAt('abcd', 1, 3, 'X')).toEqual({value: 'aXd', caret: 2})
	})

	it('offers snippets per mode', () => {
		expect(snippetsFor('action').length).toBe(3)
		expect(snippetsFor('condition').length).toBeGreaterThan(0)
	})

	it('allows secret placeholders only where the backend does', () => {
		expect(allowsSecretPlaceholder('http.request', 'body')).toBe(true)
		expect(allowsSecretPlaceholder('http.request', 'headers')).toBe(true)
		expect(allowsSecretPlaceholder('http.request', 'url')).toBe(false)
		expect(allowsSecretPlaceholder('task.add_comment', 'text')).toBe(false)
	})
})
