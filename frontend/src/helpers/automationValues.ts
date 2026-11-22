// Syntax of the relative values the automation engine accepts (mirrors pkg/automation/spec/dates.go).

const OFFSET = /^([+-]?)(\d{1,6})([mhdw])$/
const NOW = /^now(?:([+-])(\d{1,6})([mhdw]))?$/
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/
const PLAIN_DATE = /^\d{4}-\d{2}-\d{2}$/

export function isValidOffset(value: string): boolean {
	return OFFSET.test(value.trim())
}

// allowOffset is true for action parameters (a bare +3d and "none"), false for condition values.
export function isValidDateSpec(value: string, allowOffset: boolean): boolean {
	const s = value.trim()
	if (s === '') return false
	if (allowOffset && s === 'none') return true
	if (NOW.test(s)) return true
	if (allowOffset && (s[0] === '+' || s[0] === '-')) return OFFSET.test(s)
	if (RFC3339.test(s)) return !Number.isNaN(Date.parse(s))
	if (PLAIN_DATE.test(s)) return !Number.isNaN(Date.parse(s))
	return false
}

export type DateSuggestion = {value: string, labelKey: string}

export function dateSuggestions(allowOffset: boolean): DateSuggestion[] {
	return allowOffset
		? [
			{value: '+1d', labelKey: 'automation.date.tomorrow'},
			{value: '+3d', labelKey: 'automation.date.inThreeDays'},
			{value: '+1w', labelKey: 'automation.date.inOneWeek'},
			{value: 'none', labelKey: 'automation.date.none'},
		]
		: [
			{value: 'now', labelKey: 'automation.date.now'},
			{value: 'now+3d', labelKey: 'automation.date.inThreeDays'},
			{value: 'now-1w', labelKey: 'automation.date.oneWeekAgo'},
		]
}
