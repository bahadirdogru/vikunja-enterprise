import {describe, expect, it} from 'vitest'

import {CRON_PRESETS, cronProblem} from './automationCron'
import shared from './automationCron.cases.json'

describe('cronProblem', () => {
	it.each([
		'* * * * *',
		'0 9 * * 1',
		'*/15 8-18 * * mon-fri',
		'0 0 1,15 * *',
		'5/15 * * * *',
		'0 0 1 jan,JUL *',
		'30 4 29 2 *',
		'  0   9 * * *  ',
		'@hourly', '@daily', '@weekly', '@monthly', '@yearly', '@annually', '@midnight',
		// a weekday restriction lets the day of the month match either way
		'0 0 31 2 1',
		// robfig reads numbers with Atoi, ignores what follows a star hyphen and counts */N (N > 1) as a restriction
		'+5 * * * *', '*/+2 * * * *', '*-5 * * * *', '0 0 30 2 */2',
	])('accepts %s', expression => {
		expect(cronProblem(expression)).toBeNull()
	})

	it.each([
		['', 'empty'],
		['   ', 'empty'],
		['@every 5m', 'every'],
		['@EVERY 1h', 'every'],
		['TZ=Europe/Berlin 0 9 * * *', 'timezone'],
		['CRON_TZ=UTC 0 9 * * *', 'timezone'],
		['@sometimes', 'syntax'],
		['* * * *', 'fields'],
		['* * * * * *', 'fields'],
		['a * * * *', 'syntax'],
		['1-2-3 * * * *', 'syntax'],
		['*/x * * * *', 'syntax'],
		['1//2 * * * *', 'syntax'],
		['60 * * * *', 'range'],
		['* 24 * * *', 'range'],
		['* * 0 * *', 'range'],
		['* * 32 * *', 'range'],
		['* * * 13 *', 'range'],
		['* * * * 7', 'range'],
		['*/0 * * * *', 'range'],
		['10-5 * * * *', 'range'],
		['0 0 30 2 *', 'never'],
		['0 0 31 4,6,9,11 *', 'never'],
	])('rejects %s as %s', (expression, problem) => {
		expect(cronProblem(expression)).toBe(problem)
	})

	// The same table is run against spec.ParseCron in pkg/automation/spec, so the client never rejects what the server
	// accepts and rejects what the server would.
	it.each(shared.cases)('agrees with the server on "$expr" (valid: $valid)', ({expr, valid}) => {
		expect(cronProblem(expr) === null).toBe(valid)
	})

	it('offers presets the server accepts', () => {
		expect(CRON_PRESETS.map(p => p.value)).toEqual(['@hourly', '@daily', '@weekly', '@monthly'])
		for (const preset of CRON_PRESETS) expect(cronProblem(preset.value)).toBeNull()
	})
})
