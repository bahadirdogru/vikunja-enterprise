import {describe, expect, it} from 'vitest'

import fixture from './stackedReports.fixture.json'
import {readFileSync} from 'node:fs'
import {resolve as resolvePath} from 'node:path'

import {
	assignColors,
	groupLabel,
	GROUP_LABEL_KEYS,
	bandScale,
	donutArcs,
	formatAxisNumber,
	formatAxisValue,
	formatBucket,
	formatSeconds,
	groupColor,
	labelIndexes,
	linearScale,
	maxCharsFor,
	NEUTRAL_COLOR,
	niceTicks,
	paletteColor,
	PALETTE,
	safeColor,
	stackModel,
	stackSegments,
	truncateLabel,
	type ChartGroup,
} from './charts'

describe('niceTicks', () => {
	it('rounds to 1, 2, 5 steps that cover the data', () => {
		expect(niceTicks(0, 7, 5)).toMatchObject({min: 0, max: 8, step: 2, ticks: [0, 2, 4, 6, 8]})
		expect(niceTicks(0, 97, 5)).toMatchObject({min: 0, max: 100, step: 20})
		expect(niceTicks(0, 1234, 5).ticks.at(-1)).toBeGreaterThanOrEqual(1234)
	})

	it('handles a flat or empty range', () => {
		const flat = niceTicks(0, 0)
		expect(flat.max).toBeGreaterThan(flat.min)
		expect(niceTicks(5, 5).ticks.length).toBeGreaterThan(1)
		expect(niceTicks(Number.NaN, 3).ticks).toEqual([0, 1])
	})

	it('avoids floating point noise', () => {
		expect(niceTicks(0, 1, 5).ticks).toEqual([0, 0.2, 0.4, 0.6, 0.8, 1])
	})
})

describe('scales', () => {
	it('maps a domain onto a range', () => {
		const scale = linearScale([0, 10], [100, 0])
		expect(scale(0)).toBe(100)
		expect(scale(10)).toBe(0)
		expect(scale(5)).toBe(50)
		expect(linearScale([3, 3], [7, 9])(3)).toBe(7)
	})

	it('builds padded bands', () => {
		const bands = bandScale(4, [0, 100], 0.2)
		expect(bands).toHaveLength(4)
		expect(bands[0]!.size).toBeCloseTo(20)
		expect(bands[0]!.start).toBeCloseTo(2.5)
		expect(bands[3]!.center).toBeCloseTo(87.5)
		expect(bandScale(0, [0, 10])).toEqual([])
	})
})

describe('formatting', () => {
	it('formats numbers with the locale and compacts big axis values', () => {
		expect(formatAxisNumber(1234.5, 'en')).toBe('1,234.5')
		expect(formatAxisNumber(15000, 'en')).toBe('15K')
		expect(formatAxisNumber(1234, 'tr-TR')).toBe('1.234')
		expect(formatAxisValue(15000, 'tasks', 'en')).toBe('15K')
		expect(formatAxisValue(21600, 'seconds', 'en')).toBe('6 h')
		expect(formatAxisValue(90, 'seconds', 'en')).toBe('2 min')
	})

	it('formats durations', () => {
		expect(formatSeconds(30, 'en')).toBe('30 s')
		expect(formatSeconds(300, 'en')).toBe('5 min')
		expect(formatSeconds(5400, 'en')).toBe('1.5 h')
	})

	it.each([
		[0, '0 s'],
		[59, '59 s'],
		[60, '1 min'],
		[3599, '60 min'],
		[3600, '1 h'],
		[-5400, '-1.5 h'],
		[360000, '100 h'],
	])('formats %s seconds as a duration in English', (seconds, text) => {
		expect(formatSeconds(seconds, 'en')).toBe(text)
	})

	it.each([[Number.NaN], [Number.POSITIVE_INFINITY], [Number.NEGATIVE_INFINITY]])('shows no duration for %s', value => {
		expect(formatSeconds(value, 'en')).toBe('')
		expect(formatSeconds(value, 'tr-TR')).toBe('')
	})

	it('keeps the sign of a negative duration and the unit of its size', () => {
		expect(formatSeconds(-45, 'en')).toBe('-45 s')
		expect(formatSeconds(-300, 'en')).toBe('-5 min')
		expect(formatSeconds(-7200, 'tr-TR')).toBe('-2 sa')
		expect(formatSeconds(-0, 'en')).toBe('0 s')
	})

	it('writes durations in Turkish with Turkish units and decimal comma', () => {
		expect(formatSeconds(30, 'tr-TR')).toBe('30 sn')
		expect(formatSeconds(300, 'tr-TR')).toBe('5 dk')
		expect(formatSeconds(5400, 'tr-TR')).toBe('1,5 sa')
		expect(formatSeconds(3_600_000, 'tr')).toBe('1.000 sa')
	})

	it('truncates with an ellipsis and never to nothing', () => {
		expect(truncateLabel('Short', 10)).toBe('Short')
		expect(truncateLabel('A very long label', 8)).toBe('A very…')
		expect(truncateLabel('abcdef', 1)).toBe('…')
		expect(truncateLabel('abc', 0)).toBe('')
		expect(maxCharsFor(64)).toBe(10)
	})

	it('formats bucket dates as calendar days in the locale', () => {
		expect(formatBucket('2026-11-05', 'day', 'en')).toBe('Nov 5')
		expect(formatBucket('2026-11-01', 'month', 'en')).toBe('Nov 2026')
		expect(formatBucket('garbage', 'day', 'en')).toBe('garbage')
	})

	it('picks axis labels that fit', () => {
		expect(labelIndexes(30, 640, 64)).toEqual([0, 3, 6, 9, 12, 15, 18, 21, 24, 27])
		expect(labelIndexes(3, 640, 64)).toEqual([0, 1, 2])
		expect(labelIndexes(0, 640, 64)).toEqual([])
	})
})

describe('palette', () => {
	it('cycles through theme tokens and is stable', () => {
		expect(paletteColor(0)).toBe('var(--primary)')
		expect(paletteColor(PALETTE.length)).toBe(paletteColor(0))
		expect(paletteColor(-1)).toBe(PALETTE.at(-1))
	})

	it('only accepts colours that are certainly colours', () => {
		expect(safeColor('#22c55e')).toBe('#22c55e')
		expect(safeColor('hsl(10 20% 30%)')).toBe('hsl(10 20% 30%)')
		expect(safeColor('red; background:url(x)')).toBeUndefined()
		expect(safeColor('')).toBeUndefined()
	})

	it('uses the api colour, a neutral for other and none, else the palette', () => {
		const base = {key: 1, label: 'x', value: 1}
		expect(groupColor({...base, color: '#94a3b8'}, 3)).toBe('#94a3b8')
		expect(groupColor({...base, other: true, color: '#ff0000'}, 3)).toBe(NEUTRAL_COLOR)
		expect(groupColor({...base, none: true}, 3)).toBe(NEUTRAL_COLOR)
		expect(groupColor(base, 2)).toBe(paletteColor(2))
	})
})

describe('donutArcs', () => {
	it('splits the circle in proportion and skips zero values', () => {
		const arcs = donutArcs([1, 0, 3], 50, 50, 50, 30, 0)
		expect(arcs.map(a => a.index)).toEqual([0, 2])
		expect(arcs[0]!.fraction).toBeCloseTo(0.25)
		expect(arcs[1]!.endAngle).toBeCloseTo(Math.PI * 2, 3)
		expect(arcs[0]!.path.startsWith('M')).toBe(true)
	})

	it('draws a single value as a full ring', () => {
		const [arc] = donutArcs([5], 50, 50, 50, 30)
		expect(arc!.fraction).toBe(1)
		expect(arc!.path).toContain('Z')
		expect(arc!.path.match(/A/g)).toHaveLength(4)
	})

	it('has no arcs without a positive total', () => {
		expect(donutArcs([0, 0], 1, 1, 1, 0.5)).toEqual([])
	})

	it('marks the large arc flag for slices over half', () => {
		const [big] = donutArcs([3, 1], 50, 50, 50, 30, 0)
		expect(big!.path).toMatch(/A50,50 0 1 1/)
	})
})

describe('stacking', () => {
	it('offsets segments in key order and ignores missing ones', () => {
		const segments = stackSegments(new Map([['a', 2], ['b', 3]]), ['b', 'c', 'a'])
		expect(segments).toEqual([
			{key: 'b', value: 3, start: 0, end: 3},
			{key: 'a', value: 2, start: 3, end: 5},
		])
	})

	it('collects child keys ordered by total size', () => {
		const child = (key: number, label: string, value: number): ChartGroup => ({key, label, value})
		const groups: ChartGroup[] = [
			{key: 1, label: 'A', value: 5, children: [child(1, 'open', 2), child(2, 'done', 3)]},
			{key: 2, label: 'B', value: 4, children: [child(1, 'open', 4)]},
		]
		const model = stackModel(groups)
		expect(model.keys).toEqual(['1', '2'])
		expect(model.bars.map(b => b.total)).toEqual([5, 4])
		expect(model.labels.get('2')!.label).toBe('done')
	})
})

describe('colours and labels of nested groups (real reports)', () => {
	const reports = fixture as unknown as Record<string, {groups: (ChartGroup & {children?: ChartGroup[]})[]}>
	const colorsOf = (groups: ChartGroup[]) => {
		const colors = assignColors(groups)
		return Object.fromEntries(groups.map((g, i) => [g.label, colors[i]]))
	}

	it('gives an option the same colour in every chart whatever the order and sizes', () => {
		const model = stackModel(reports.statusByField!.groups)
		const stacked = colorsOf(model.keys.map(k => model.labels.get(k)!))
		const donut = colorsOf(reports.field!.groups)
		for (const name of ['Son', 'Taslak', 'Düzelti']) {
			expect(stacked[name]).toBe(donut[name])
		}
		const shuffled = colorsOf([...reports.field!.groups].reverse().map((g, i) => ({...g, value: i})))
		expect(shuffled['Son']).toBe(donut['Son'])
	})

	it('keeps the none group neutral and apart from the palette, keys unique per chart', () => {
		for (const report of Object.values(reports)) {
			const model = stackModel(report.groups)
			expect(new Set(model.keys).size).toBe(model.keys.length)
			const colors = assignColors(model.keys.map(k => model.labels.get(k)!))
			model.keys.forEach((key, i) => {
				if (key === '\u0000none') {
					expect(colors[i]).toBe(NEUTRAL_COLOR)
				}
			})
			const real = colors.filter(c => c !== NEUTRAL_COLOR)
			expect(new Set(real).size).toBe(real.length)
		}
	})

	it('uses the colours the API sends for labels and the same child values in every parent', () => {
		const model = stackModel(reports.statusByLabel!.groups)
		const colors = colorsOf(model.keys.map(k => model.labels.get(k)!))
		expect(colors['Acil']).toBe('#e11d48')
		expect(colors['Web']).toBe('#10b981')
		const first = model.bars[0]!
		expect([...first.values.keys()].every(k => model.keys.includes(k))).toBe(true)
		// A task with several labels counts in each, so the stack is at least the distinct tasks.
		expect(first.total).toBeGreaterThanOrEqual(first.group.value)
	})

	it('names the empty and folded groups in the user language', () => {
		const t = (key: string) => ({'charts.noValue': 'Değeri yok', 'charts.other': 'Diğer'}[key] ?? key)
		expect(groupLabel({key: null, label: 'None', value: 1, none: true}, t)).toBe('Değeri yok')
		expect(groupLabel({key: null, label: 'Other', value: 1, other: true}, t)).toBe('Diğer')
		expect(groupLabel({key: 1, label: 'Son', value: 1}, t)).toBe('Son')
	})
})

const load = (name: string) => JSON.parse(readFileSync(resolvePath(process.cwd(), 'src/i18n/lang', `${name}.json`), 'utf-8')) as Record<string, unknown>
const en = load('en')
const trMessages = load('tr-TR')

describe('names the server writes itself', () => {
	const tr = (key: string) => `tr:${key}`

	it('says the category, done, checked and priority groups in the language of the reader', () => {
		expect(groupLabel({key: 'open', label: 'Open', value: 1, label_code: 'status_category.open'}, tr)).toBe('tr:workflow.categories.open')
		expect(groupLabel({key: true, label: 'Done', value: 1, label_code: 'done.true'}, tr)).toBe('tr:workflow.categories.done')
		expect(groupLabel({key: false, label: 'Not checked', value: 1, label_code: 'checked.false'}, tr)).toBe('tr:charts.notChecked')
		expect(groupLabel({key: 5, label: 'DO NOW', value: 1, label_code: 'priority.5'}, tr)).toBe('tr:task.priority.doNow')
	})

	it('keeps the name of a group that belongs to the data, and of a code it does not know', () => {
		expect(groupLabel({key: 3, label: 'Proje Alfa', value: 1}, tr)).toBe('Proje Alfa')
		expect(groupLabel({key: 3, label: 'Something', value: 1, label_code: 'from_a_newer_server'}, tr)).toBe('Something')
	})

	it('has a text for every code in both languages', () => {
		for (const [name, messages] of [['en', en], ['tr-TR', trMessages]] as const) {
			const missing = Object.values(GROUP_LABEL_KEYS).filter(key => !key.split('.').reduce<unknown>((node, part) => (node as Record<string, unknown> | undefined)?.[part], messages))
			expect(missing, name).toEqual([])
		}
	})
})
