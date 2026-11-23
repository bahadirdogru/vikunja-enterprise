// Client-side sanity check of the cron expression of the schedule trigger. It mirrors what the server accepts
// (spec.ParseCron in pkg/automation/spec/validate.go, built on robfig/cron): five fields or one of the descriptors
// @hourly, @daily, @midnight, @weekly, @monthly, @yearly, @annually. @every and TZ= / CRON_TZ= prefixes are refused. The
// server stays the authority; this only saves a round trip for the obvious mistakes.

export type CronProblem = 'empty' | 'every' | 'timezone' | 'fields' | 'syntax' | 'range' | 'never'

const DESCRIPTORS = ['@yearly', '@annually', '@monthly', '@weekly', '@daily', '@midnight', '@hourly']

type FieldSpec = {min: number, max: number, names?: Record<string, number>}

const MONTHS: Record<string, number> = {
	jan: 1, feb: 2, mar: 3, apr: 4, may: 5, jun: 6, jul: 7, aug: 8, sep: 9, oct: 10, nov: 11, dec: 12,
}
const WEEKDAYS: Record<string, number> = {sun: 0, mon: 1, tue: 2, wed: 3, thu: 4, fri: 5, sat: 6}

const FIELDS: FieldSpec[] = [
	{min: 0, max: 59},
	{min: 0, max: 23},
	{min: 1, max: 31},
	{min: 1, max: 12, names: MONTHS},
	{min: 0, max: 6, names: WEEKDAYS},
]

// Days a month can have at most (February counts 29: leap years exist).
const MAX_DAYS = [0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]

type Parsed = {values: Set<number>, star: boolean}

// robfig reads numbers with strconv.Atoi, which allows a leading plus sign.
function parseNumber(raw: string, spec: FieldSpec): number | null {
	const lower = raw.toLowerCase()
	if (spec.names && lower in spec.names) return spec.names[lower]
	return /^\+?\d+$/.test(raw) ? Number(raw) : null
}

// One comma separated part: *, ?, a, a-b, optionally followed by /step.
function parsePart(part: string, spec: FieldSpec): Parsed | CronProblem {
	const [rangeText, stepText, ...rest] = part.split('/')
	if (rest.length > 0 || rangeText === '') return 'syntax'
	let step = 1
	if (stepText !== undefined) {
		if (!/^\+?\d+$/.test(stepText)) return 'syntax'
		step = Number(stepText)
		if (step === 0) return 'range'
	}
	const bounds = rangeText.split('-')
	let low: number
	let high: number
	let star = false
	if (bounds[0] === '*' || bounds[0] === '?') {
		// robfig ignores whatever follows a hyphen after a star ("*-5" is a star).
		low = spec.min
		high = spec.max
		// A step above one makes it a restriction: it no longer counts as a star for the day rule.
		star = step === 1
	} else {
		if (bounds.length > 2) return 'syntax'
		const first = parseNumber(bounds[0], spec)
		if (first === null) return 'syntax'
		low = first
		if (bounds.length === 2) {
			const second = parseNumber(bounds[1], spec)
			if (second === null) return 'syntax'
			high = second
		} else {
			// "5/15" runs from 5 to the end of the range; a single value is just that value.
			high = stepText !== undefined ? spec.max : low
		}
	}
	if (low < spec.min || high > spec.max || low > high) return 'range'
	const values = new Set<number>()
	for (let v = low; v <= high; v += step) values.add(v)
	return {values, star}
}

function parseField(text: string, spec: FieldSpec): Parsed | CronProblem {
	const values = new Set<number>()
	let star = false
	for (const part of text.split(',')) {
		// robfig skips empty parts ("1,,2").
		if (part === '') continue
		const parsed = parsePart(part, spec)
		if (typeof parsed === 'string') return parsed
		parsed.values.forEach(v => values.add(v))
		star = star || parsed.star
	}
	return {values, star}
}

// Returns what is wrong with the expression, or null when the server would accept it.
export function cronProblem(expression: string): CronProblem | null {
	const expr = expression.trim()
	if (expr === '') return 'empty'
	const lower = expr.toLowerCase()
	if (lower.startsWith('@every')) return 'every'
	if (lower.startsWith('tz=') || lower.startsWith('cron_tz=')) return 'timezone'
	if (expr.startsWith('@')) return DESCRIPTORS.includes(expr) ? null : 'syntax'

	const parts = expr.split(/\s+/)
	if (parts.length !== 5) return 'fields'
	const parsed: Parsed[] = []
	for (let i = 0; i < 5; i++) {
		const field = parseField(parts[i], FIELDS[i])
		if (typeof field === 'string') return field
		parsed.push(field)
	}
	const [, , dom, month, dow] = parsed
	// With a restricted day of the month and a star weekday both have to match (when both are restricted, either
	// does). So the day has to exist in one of the months: 30 February never happens. An empty field (only commas)
	// is left to the server.
	if (
		!dom.star && dow.star
		&& dom.values.size > 0 && month.values.size > 0
		&& ![...month.values].some(m => [...dom.values].some(d => d <= MAX_DAYS[m]))
	) return 'never'
	return null
}

export type CronPreset = {value: string, labelKey: string}

export const CRON_PRESETS: CronPreset[] = [
	{value: '@hourly', labelKey: 'automation.cron.presets.hourly'},
	{value: '@daily', labelKey: 'automation.cron.presets.daily'},
	{value: '@weekly', labelKey: 'automation.cron.presets.weekly'},
	{value: '@monthly', labelKey: 'automation.cron.presets.monthly'},
]
