import {describe, expect, it, vi} from 'vitest'

import {readFileSync} from 'node:fs'
import {resolve as resolvePath} from 'node:path'
import optionsFixture from './reportOptions.fixture.json'
import {normalizeReportOptions} from '@/client/queries/reports'

type Messages = {[key: string]: string | Messages}

// Read from disk: the i18n plugin precompiles the messages of a JSON import.
const load = (name: string) => JSON.parse(readFileSync(resolvePath(process.cwd(), 'src/i18n/lang', `${name}.json`), 'utf-8')) as Messages
const messages: [string, Messages][] = [['en', load('en')], ['tr-TR', load('tr-TR')]]

function resolve(tree: Messages, key: string): string | undefined {
	let node: string | Messages | undefined = tree
	for (const part of key.split('.')) {
		if (typeof node !== 'object' || node === null) return undefined
		node = node[part]
	}
	return typeof node === 'string' ? node : undefined
}

const options = normalizeReportOptions(optionsFixture as never)
const titleKeys = [...new Set([
	...options.dimensions.map(o => o.title_key),
	...options.time_fields.map(o => o.title_key),
	...options.measures.flatMap(m => [m.title_key, ...m.fields.map(f => f.title_key)]),
])]

describe('report option translations', () => {
	it('reads a real options response', () => {
		expect(options.has_workflow).toBe(true)
		expect(titleKeys.length).toBeGreaterThan(15)
		expect(options.dimensions.some(d => d.name.startsWith('cf_'))).toBe(true)
	})

	it.each(messages)('has every title key the options endpoint emits in %s', (_name, tree) => {
		expect(titleKeys.filter(key => !resolve(tree, key))).toEqual([])
	})

	it.each(messages)('has the buckets, ranges, sort fields and error codes in %s', (_name, tree) => {
		const wanted = [
			...options.buckets.map(b => `dashboards.buckets.${b}`),
			...['last7', 'last30', 'last90', 'thisMonth', 'thisYear', 'custom'].map(r => `dashboards.ranges.${r}`),
			...['due_date', 'priority', 'created', 'updated', 'title', 'id', 'done_at', 'percent_done'].map(f => `dashboards.sort.${f}`),
			...[23001, 23002, 23003, 23004, 23005, 23006, 23007, 23008, 23009, 23010, 23011].map(c => `error.${c}`),
			...['number', 'bar', 'donut', 'line', 'created_vs_done', 'task_list', 'text'].flatMap(t => [`dashboards.types.${t}.name`, `dashboards.types.${t}.description`]),
			...['other', 'null_filter', 'include_nulls', 'status_entered', 'needs_username', 'needs_options', 'unknown_dimension'].map(r => `dashboards.drilldown.blocked.${r}`),
		]
		expect(wanted.filter(key => !resolve(tree, key))).toEqual([])
	})

	it('has the same dashboards, charts and reports keys in English and Turkish', () => {
		const flatten = (node: Messages, prefix = ''): string[] => Object.entries(node).flatMap(([k, v]) =>
			typeof v === 'string' ? [prefix + k] : flatten(v, `${prefix}${k}.`))
		for (const root of ['dashboards', 'charts', 'reports']) {
			const [en, tr] = messages.map(([, tree]) => flatten(tree[root] as Messages).sort())
			expect(tr).toEqual(en)
		}
	})
})

describe('message syntax', () => {
	const flatten = (node: Messages, prefix = ''): string[] => Object.entries(node).flatMap(([k, v]) =>
		typeof v === 'string' ? [prefix + k] : flatten(v, `${prefix}${k}.`))

	it.each(messages)('compiles and formats every charts, dashboards and reports message in %s', async (name, tree) => {
		const {createI18n} = await import('vue-i18n')
		const i18n = createI18n({legacy: false, locale: name, messages: {[name]: tree as never}})
		const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
		const bad: string[] = []
		for (const root of ['charts', 'dashboards', 'reports']) {
			for (const key of flatten(tree[root] as Messages, `${root}.`)) {
				const out = i18n.global.t(key, {count: 1, title: 'T', project: 'P', column: 1, row: 1, w: 1, h: 1}, {plural: 1})
				if (!out || out === key) {
					bad.push(key)
				}
			}
		}
		expect(bad).toEqual([])
		expect(warn).not.toHaveBeenCalled()
		warn.mockRestore()
	})
})
