// Pure geometry and formatting for the SVG charts. No Vue, no DOM.

export type ChartPoint = {
	bucket: string
	start?: string
	end?: string
	value: number
}

export type ChartGroup = {
	key: unknown
	label: string
	color?: string
	value: number
	none?: boolean
	other?: boolean
	// Set when the server wrote the name itself (Open, Done, Low...): it is said in the language of the reader.
	label_code?: string
	children?: ChartGroup[]
	series?: ChartPoint[]
}

export type ChartSeries = {
	key: string
	label: string
	color?: string
	points: ChartPoint[]
	// The group the series came from, so a click can drill down into it.
	group?: ChartGroup
}

export type ChartSelection = {
	group?: ChartGroup
	child?: ChartGroup
	point?: ChartPoint
	series?: ChartSeries
}

export const NEUTRAL_COLOR = 'var(--grey-400)'

// Theme tokens so the chart follows the colour scheme, plus fixed mid-lightness hues that read on both. Neighbours are
// far apart in hue; the blue-ish info token comes late.
export const PALETTE = [
	'var(--primary)',
	'var(--warning)',
	'var(--success)',
	'hsl(272 45% 60%)',
	'var(--danger)',
	'hsl(176 50% 40%)',
	'hsl(330 60% 62%)',
	'var(--info)',
	'hsl(28 70% 50%)',
	'hsl(215 15% 55%)',
]

export function paletteColor(index: number): string {
	return PALETTE[((index % PALETTE.length) + PALETTE.length) % PALETTE.length]!
}

const colorPattern = /^(#[0-9a-f]{3,8}|(rgb|hsl)a?\([^)]*\)|var\(--[a-z0-9-]+\))$/i

// The API colour is data from another user; only accept what is certainly a colour.
export function safeColor(color: string | undefined | null): string | undefined {
	if (!color) {
		return undefined
	}
	const trimmed = color.trim()
	return colorPattern.test(trimmed) ? trimmed : undefined
}

export function groupColor(group: ChartGroup, index: number): string {
	if (group.other) {
		return NEUTRAL_COLOR
	}
	return safeColor(group.color) ?? (group.none ? NEUTRAL_COLOR : paletteColor(index))
}

function hashString(text: string): number {
	let hash = 2166136261
	for (let i = 0; i < text.length; i++) {
		hash ^= text.charCodeAt(i)
		hash = Math.imul(hash, 16777619)
	}
	return hash >>> 0
}

// Colours for the groups of one chart, parallel to the input. The API colour wins, "other" and "none" are neutral. The
// rest take a palette slot from a hash of what they are (key and label), not of where they are in the list, so the same
// option has the same colour in every widget whatever the sizes; clashes inside one chart move on to the next free slot.
export function assignColors(groups: ChartGroup[]): string[] {
	const colors: string[] = groups.map(group => {
		if (group.other) {
			return NEUTRAL_COLOR
		}
		return safeColor(group.color) ?? (group.none ? NEUTRAL_COLOR : '')
	})
	const used = new Set<number>()
	const pending = groups
		.map((group, index) => ({index, id: `${groupKeyId(group)}|${group.label}`}))
		.filter(entry => colors[entry.index] === '')
		.sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
	for (const {index, id} of pending) {
		let slot = hashString(id) % PALETTE.length
		for (let tries = 0; tries < PALETTE.length && used.has(slot); tries++) {
			slot = (slot + 1) % PALETTE.length
		}
		used.add(slot)
		colors[index] = paletteColor(slot)
	}
	return colors
}

// The names the server writes itself, by their label_code, and the translation that says each of them.
export const GROUP_LABEL_KEYS: Record<string, string> = {
	'status_category.open': 'workflow.categories.open',
	'status_category.in_progress': 'workflow.categories.in_progress',
	'status_category.done': 'workflow.categories.done',
	'status_category.cancelled': 'workflow.categories.cancelled',
	'done.true': 'workflow.categories.done',
	'done.false': 'workflow.categories.open',
	'checked.true': 'charts.checked',
	'checked.false': 'charts.notChecked',
	'priority.1': 'task.priority.low',
	'priority.2': 'task.priority.medium',
	'priority.3': 'task.priority.high',
	'priority.4': 'task.priority.urgent',
	'priority.5': 'task.priority.doNow',
}

// The API names the empty and the folded group, and the groups of a few dimensions, in English.
export function groupLabel(group: ChartGroup, t: (key: string) => string): string {
	if (group.other) {
		return t('charts.other')
	}
	if (group.none) {
		return t('charts.noValue')
	}
	const key = group.label_code ? GROUP_LABEL_KEYS[group.label_code] : undefined
	return key ? t(key) : group.label
}

export function seriesColor(series: ChartSeries, index: number): string {
	return safeColor(series.color) ?? paletteColor(index)
}

export function niceNumber(range: number, round: boolean): number {
	if (range <= 0) {
		return 1
	}
	const exponent = Math.floor(Math.log10(range))
	const fraction = range / Math.pow(10, exponent)
	let nice: number
	if (round) {
		nice = fraction < 1.5 ? 1 : fraction < 3 ? 2 : fraction < 7 ? 5 : 10
	} else {
		nice = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10
	}
	return nice * Math.pow(10, exponent)
}

export type NiceTicks = {
	ticks: number[]
	min: number
	max: number
	step: number
}

// "Nice" axis ticks covering [min, max]. Counts start at zero for bars; pass min for lines.
export function niceTicks(min: number, max: number, target = 5): NiceTicks {
	if (!Number.isFinite(min) || !Number.isFinite(max)) {
		return {ticks: [0, 1], min: 0, max: 1, step: 1}
	}
	if (min === max) {
		max = min + (min === 0 ? 1 : Math.abs(min) * 0.1)
	}
	const range = niceNumber(max - min, false)
	const step = niceNumber(range / Math.max(1, target - 1), true)
	const niceMin = Math.floor(min / step) * step
	const niceMax = Math.ceil(max / step) * step
	const ticks: number[] = []
	const decimals = Math.max(0, -Math.floor(Math.log10(step)) + 1)
	for (let v = niceMin; v <= niceMax + step / 2; v += step) {
		ticks.push(Number(v.toFixed(decimals)))
	}
	return {ticks, min: niceMin, max: niceMax, step}
}

export function linearScale(domain: [number, number], range: [number, number]): (value: number) => number {
	const [d0, d1] = domain
	const [r0, r1] = range
	const span = d1 - d0
	if (span === 0) {
		return () => r0
	}
	return value => r0 + ((value - d0) / span) * (r1 - r0)
}

export type Band = {
	start: number
	size: number
	center: number
}

// Equal bands with padding between them (a fraction of the band, 0..1).
export function bandScale(count: number, range: [number, number], padding = 0.25): Band[] {
	if (count <= 0) {
		return []
	}
	const [r0, r1] = range
	const step = (r1 - r0) / count
	const size = step * (1 - padding)
	return Array.from({length: count}, (_, i) => {
		const start = r0 + step * i + (step - size) / 2
		return {start, size, center: start + size / 2}
	})
}

export function formatNumber(value: number, locale?: string, options: Intl.NumberFormatOptions = {}): string {
	if (!Number.isFinite(value)) {
		return ''
	}
	try {
		return new Intl.NumberFormat(locale, {maximumFractionDigits: Number.isInteger(value) ? 0 : 2, ...options}).format(value)
	} catch {
		return String(value)
	}
}

// Axis labels: compact for big numbers so they never overflow the margin.
export function formatAxisNumber(value: number, locale?: string): string {
	if (Math.abs(value) >= 10000) {
		return formatNumber(value, locale, {notation: 'compact', maximumFractionDigits: 1})
	}
	return formatNumber(value, locale, {maximumFractionDigits: 2})
}

// Abbreviations of hour, minute and second per language; English is the default.
const DURATION_UNITS: Record<string, {h: string, min: string, s: string}> = {
	tr: {h: 'sa', min: 'dk', s: 'sn'},
}

// A value that is not a number (NaN, Infinity) has no duration to show: empty, like formatNumber.
export function formatSeconds(seconds: number, locale?: string): string {
	if (!Number.isFinite(seconds)) {
		return ''
	}
	// -0 would print as "-0 s"
	seconds = seconds === 0 ? 0 : seconds
	const abs = Math.abs(seconds)
	const units = DURATION_UNITS[(locale ?? '').split('-')[0]!.toLowerCase()] ?? {h: 'h', min: 'min', s: 's'}
	if (abs >= 3600) {
		return `${formatNumber(seconds / 3600, locale, {maximumFractionDigits: 1})} ${units.h}`
	}
	if (abs >= 60) {
		return `${formatNumber(seconds / 60, locale, {maximumFractionDigits: 0})} ${units.min}`
	}
	return `${formatNumber(seconds, locale)} ${units.s}`
}

// Axis ticks and value labels: durations read as hours and minutes, not as a bare number of seconds.
export function formatAxisValue(value: number, unit: string | undefined, locale?: string): string {
	return unit === 'seconds' ? formatSeconds(value, locale) : formatAxisNumber(value, locale)
}

export function formatValue(value: number, unit: string | undefined, locale?: string): string {
	return unit === 'seconds' ? formatSeconds(value, locale) : formatNumber(value, locale)
}

export function truncateLabel(label: string, maxChars: number): string {
	const chars = Array.from(label)
	if (maxChars < 1) {
		return ''
	}
	if (chars.length <= maxChars) {
		return label
	}
	return chars.slice(0, maxChars - 1).join('').trimEnd() + '…'
}

// Average glyph width of the chart font at 12px; only used to decide how much label fits.
export const CHAR_WIDTH = 6.4

export function maxCharsFor(width: number, charWidth = CHAR_WIDTH): number {
	return Math.max(0, Math.floor(width / charWidth))
}

export type Arc = {
	index: number
	startAngle: number
	endAngle: number
	fraction: number
	path: string
	// The centre of the arc's outer edge, for tooltip anchoring.
	anchor: {x: number, y: number}
}

function polar(cx: number, cy: number, r: number, angle: number) {
	// 0 rad is 12 o'clock, clockwise.
	return {x: cx + r * Math.sin(angle), y: cy - r * Math.cos(angle)}
}

function arcPath(cx: number, cy: number, outer: number, inner: number, start: number, end: number): string {
	const full = end - start >= Math.PI * 2 - 1e-6
	const large = end - start > Math.PI ? 1 : 0
	if (full) {
		// A full circle cannot be one arc; two halves.
		const mid = start + Math.PI
		const o0 = polar(cx, cy, outer, start)
		const o1 = polar(cx, cy, outer, mid)
		const i0 = polar(cx, cy, inner, start)
		const i1 = polar(cx, cy, inner, mid)
		return [
			`M${o0.x},${o0.y}`,
			`A${outer},${outer} 0 1 1 ${o1.x},${o1.y}`,
			`A${outer},${outer} 0 1 1 ${o0.x},${o0.y}`,
			`M${i0.x},${i0.y}`,
			`A${inner},${inner} 0 1 0 ${i1.x},${i1.y}`,
			`A${inner},${inner} 0 1 0 ${i0.x},${i0.y}`,
			'Z',
		].join(' ')
	}
	const o0 = polar(cx, cy, outer, start)
	const o1 = polar(cx, cy, outer, end)
	const i1 = polar(cx, cy, inner, end)
	const i0 = polar(cx, cy, inner, start)
	return [
		`M${o0.x},${o0.y}`,
		`A${outer},${outer} 0 ${large} 1 ${o1.x},${o1.y}`,
		`L${i1.x},${i1.y}`,
		`A${inner},${inner} 0 ${large} 0 ${i0.x},${i0.y}`,
		'Z',
	].join(' ')
}

// Donut slices for non-negative values. Zero values get no arc (index kept so callers can zip).
export function donutArcs(values: number[], cx: number, cy: number, outer: number, inner: number, gap = 0.012): Arc[] {
	const total = values.reduce((sum, v) => sum + Math.max(0, v), 0)
	if (total <= 0) {
		return []
	}
	const arcs: Arc[] = []
	const positive = values.filter(v => v > 0).length
	const useGap = positive > 1 ? gap : 0
	let angle = 0
	values.forEach((raw, index) => {
		const value = Math.max(0, raw)
		if (value === 0) {
			return
		}
		const fraction = value / total
		const span = fraction * Math.PI * 2
		const start = angle + useGap / 2
		const end = Math.max(start + 0.0001, angle + span - useGap / 2)
		angle += span
		arcs.push({
			index,
			startAngle: start,
			endAngle: end,
			fraction,
			path: arcPath(cx, cy, outer, inner, start, end),
			anchor: polar(cx, cy, (outer + inner) / 2, (start + end) / 2),
		})
	})
	return arcs
}

export type Segment = {
	key: string
	value: number
	start: number
	end: number
}

// Stacked offsets of one bar: segments in the order of `keys`, missing keys count as 0.
export function stackSegments(values: Map<string, number>, keys: string[]): Segment[] {
	let offset = 0
	return keys.flatMap(key => {
		const value = values.get(key) ?? 0
		if (value <= 0) {
			return []
		}
		const segment = {key, value, start: offset, end: offset + value}
		offset += value
		return [segment]
	})
}

export function groupKeyId(group: ChartGroup): string {
	if (group.other) {
		return '\u0000other'
	}
	if (group.none) {
		return '\u0000none'
	}
	return String(group.key)
}

export type StackedModel = {
	keys: string[]
	labels: Map<string, ChartGroup>
	bars: {group: ChartGroup, values: Map<string, number>, total: number}[]
}

// Builds the child keys of nested groups (bar chart with then_by), in order of total size.
export function stackModel(groups: ChartGroup[]): StackedModel {
	const totals = new Map<string, number>()
	const labels = new Map<string, ChartGroup>()
	const bars = groups.map(group => {
		const values = new Map<string, number>()
		for (const child of group.children ?? []) {
			const id = groupKeyId(child)
			values.set(id, (values.get(id) ?? 0) + child.value)
			totals.set(id, (totals.get(id) ?? 0) + child.value)
			if (!labels.has(id)) {
				labels.set(id, child)
			}
		}
		let total = 0
		for (const v of values.values()) {
			total += v
		}
		return {group, values, total}
	})
	const keys = [...totals.entries()].sort((a, b) => b[1] - a[1]).map(([key]) => key)
	return {keys, labels, bars}
}

// Thin out x axis labels so they never collide: returns the indexes to label.
export function labelIndexes(count: number, width: number, labelWidth: number): number[] {
	if (count <= 0) {
		return []
	}
	const fit = Math.max(1, Math.floor(width / Math.max(1, labelWidth)))
	const every = Math.max(1, Math.ceil(count / fit))
	const indexes: number[] = []
	for (let i = 0; i < count; i += every) {
		indexes.push(i)
	}
	return indexes
}

export function linePath(points: {x: number, y: number}[]): string {
	return points.map((p, i) => `${i === 0 ? 'M' : 'L'}${round(p.x)},${round(p.y)}`).join(' ')
}

export function areaPath(points: {x: number, y: number}[], baseline: number): string {
	if (points.length === 0) {
		return ''
	}
	const first = points[0]!
	const last = points[points.length - 1]!
	return `${linePath(points)} L${round(last.x)},${round(baseline)} L${round(first.x)},${round(baseline)} Z`
}

function round(n: number): number {
	return Math.round(n * 100) / 100
}

const bucketFormats: Record<string, Intl.DateTimeFormatOptions> = {
	day: {day: 'numeric', month: 'short'},
	week: {day: 'numeric', month: 'short'},
	month: {month: 'short', year: 'numeric'},
}

// Bucket labels are plain dates ("2026-11-01"); parse them as local calendar days so no zone shifts them.
export function formatBucket(bucket: string, unit: string, locale?: string): string {
	const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(bucket)
	if (!match) {
		return bucket
	}
	const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
	try {
		return new Intl.DateTimeFormat(locale, bucketFormats[unit] ?? bucketFormats.day).format(date)
	} catch {
		return bucket
	}
}
