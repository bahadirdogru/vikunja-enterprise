import {describe, expect, it} from 'vitest'
import {readdirSync, readFileSync, statSync} from 'node:fs'
import {join, resolve as resolvePath} from 'node:path'

type Messages = {[key: string]: string | Messages}

const root = process.cwd()
const load = (name: string) => JSON.parse(readFileSync(resolvePath(root, 'src/i18n/lang', `${name}.json`), 'utf-8')) as Messages
const LOCALES: [string, Messages][] = [['en', load('en')], ['tr-TR', load('tr-TR')]]

function resolveKey(messages: Messages, key: string): string | Messages | undefined {
	let node: string | Messages | undefined = messages
	for (const part of key.split('.')) {
		if (typeof node !== 'object' || node === null) return undefined
		node = node[part]
	}
	return node
}

function sourceFiles(dir: string): string[] {
	return readdirSync(dir).flatMap(name => {
		const path = join(dir, name)
		if (statSync(path).isDirectory()) return name === 'generated' || name === 'i18n' ? [] : sourceFiles(path)
		return /\.(vue|ts)$/.test(name) && !/\.test\.ts$/.test(name) ? [path] : []
	})
}

// The namespaces the fork added to the language files. Upstream's own keys are not checked here.
const NAMESPACES = ['automation', 'forms', 'dashboards', 'workflow', 'approvals', 'customFields', 'charts', 'reports']
const KEY = new RegExp(`['"\`]((?:${NAMESPACES.join('|')})(?:\\.[A-Za-z0-9_<>-]+)+)['"\`]`, 'g')
const TEMPLATE = new RegExp(`\`((?:${NAMESPACES.join('|')})(?:\\.[A-Za-z0-9_<>-]+)*)\\.\\$\\{`, 'g')

const files = sourceFiles(join(root, 'src'))
const staticKeys = new Map<string, string>()
const dynamicPrefixes = new Set<string>()
for (const file of files) {
	const text = readFileSync(file, 'utf-8')
	for (const match of text.matchAll(KEY)) staticKeys.set(match[1], file.replace(`${root}/`, ''))
	for (const match of text.matchAll(TEMPLATE)) dynamicPrefixes.add(match[1])
}

// Names that look like keys but are not: route names and similar.
// dashboards.index, dashboards.view, approvals.index: route names. automation.scripts.enabled: the server
// config key that tells whether scripts are on. automation.notify: the notification type of the backend.
const NOT_KEYS = new Set<string>(['dashboards.index', 'dashboards.view', 'approvals.index', 'automation.scripts.enabled', 'automation.notify'])

// Families whose last segment is chosen at runtime. The values are the enums of the code or of the backend
// (pkg/models, pkg/automation/spec); a value missing here is a value no test looks at.
const FAMILIES: Record<string, string[]> = {
	'workflow.approval.mode': ['any', 'all', 'anyHint', 'allHint'],
	'workflow.approval.noOptions': ['user', 'team', 'custom_field'],
	'workflow.approval.pick': ['user', 'team', 'assignees', 'custom_field'],
	'approvals.outcome': ['pending', 'approved', 'rejected', 'cancelled'],
	'approvals.mode': ['any', 'all'],
	'approvals.cancelReason': ['cancelled_by_requester', 'task_deleted', 'task_moved', 'workflow_removed', 'status_removed', 'transition_removed', 'approval_disabled', 'status_changed', 'target_archived', 'requirements_not_met', 'requester_lost_access', 'requester_not_in_team', 'requester_deleted', 'requester_disabled', 'no_approvers', 'unknown'],
	'approvals.page.empty': ['waiting', 'requested', 'history'],
	'automation.validation.cron': ['every', 'timezone', 'fields', 'syntax', 'range', 'never'],
	'automation.test.payload': ['json', 'object', 'size'],
	'automation.disabledReason': ['owner_not_admin', 'owner_unavailable', 'too_many_failures', 'custom_field_deleted', 'status_deleted', 'duplicated', 'duplicated_unmapped_reference', 'duplicated_secrets_missing', 'unknown'],
	'automation.runStatus': ['running', 'success', 'failed', 'skipped', 'aborted'],
	'automation.skipReason': ['conditions_not_met', 'flow_stopped', 'max_depth', 'rate_limit', 'self_trigger', 'task_not_found', 'owner_not_admin', 'owner_unavailable', 'rule_changed', 'rule_disabled', 'too_many_actions'],
	'automation.secrets.nameError': ['required', 'invalid', 'exists'],
	'automation.fieldGroup': ['task', 'status', 'custom', 'event'],
	'automation.group': ['task', 'flow', 'script', 'http', 'notify', 'manual', 'schedule', 'webhook', 'wait'],
	'customFields.types': ['text', 'long_text', 'number', 'currency', 'date', 'checkbox', 'select', 'multi_select', 'user', 'url', 'email', 'rating', 'progress'],
	'dashboards.drilldown.blocked': ['other', 'null_filter', 'include_nulls', 'status_entered', 'needs_username', 'needs_options', 'unknown_dimension'],
	'dashboards.statusTime.metrics': ['average', 'median', 'total'],
	'dashboards.buckets': ['day', 'week', 'month'],
	'dashboards.editor.problems': ['groupByRequired', 'timeFieldRequired', 'fieldRequired', 'dateRequired', 'listLimitRange', 'projectRequired'],
	'dashboards.ranges': ['last7', 'last30', 'last90', 'thisMonth', 'thisYear', 'custom', 'all'],
	'dashboards.sort': ['due_date', 'priority', 'created', 'updated', 'title', 'id', 'done_at', 'percent_done'],
	'dashboards.timeTracking.groups': ['user', 'project', 'label', 'day'],
	'dashboards.workload.buckets': ['overdue', 'this_week', 'next_week', 'later', 'no_due_date'],
	'dashboards.workload.metrics': ['count', 'estimate'],
	'forms.access': ['members', 'users', 'public', 'membersHelp', 'usersHelp', 'publicHelp', 'membersShort', 'usersShort', 'publicShort'],
	'forms.branding.logoErrors': ['type', 'size'],
	'forms.disabledReason': ['owner_not_writer', 'owner_unavailable', 'custom_field_deleted', 'status_deleted', 'label_deleted', 'duplicated', 'duplicated_unmapped_reference', 'unknown'],
	'forms.settings.mode': ['off', 'optional', 'required'],
	'forms.tabs': ['fields', 'settings', 'share'],
	'forms.share.who': ['members', 'users', 'public'],
	'forms.public.unavailable': ['closed', 'full', 'disabled'],
	'forms.target': ['title', 'description', 'due_date', 'priority', 'labels', 'attachments'],
	'forms.presets': ['simple.title', 'simple.text', 'simple.formTitle', 'dueDate.title', 'dueDate.text', 'dueDate.formTitle', 'bug.title', 'bug.text', 'bug.formTitle'],
	'workflow.fields': ['assignees', 'due_date', 'start_date', 'end_date', 'description', 'attachments', 'labels'],
	'workflow.validation': ['approvalApproversRequired', 'approvalSourceIncomplete', 'approvalDueInvalid', 'approvalRejectedStatusSame', 'approvalRejectedStatusGone', 'noStatuses', 'noInitial', 'noDone', 'titleRequired', 'titleTooLong', 'titleDuplicate', 'statusRequired', 'sameStatus', 'duplicatePair'],
	'workflow.categories': ['open', 'in_progress', 'done', 'cancelled'],
	'workflow.transitions.tags': ['approval', 'comment', 'fields', 'teams', 'admin'],
}

describe('translations of the fork modules', () => {
	it('finds the source files and keys it is meant to check', () => {
		expect(files.length).toBeGreaterThan(500)
		expect(staticKeys.size).toBeGreaterThan(500)
		expect(dynamicPrefixes.size).toBeGreaterThan(30)
	})

	it.each(LOCALES)('has every key the fork code names literally in %s', (_name, messages) => {
		const missing = [...staticKeys]
			.filter(([key]) => !NOT_KEYS.has(key) && resolveKey(messages, key) === undefined)
			.map(([key, file]) => `${key} (${file})`)
		expect(missing).toEqual([])
	})

	it('knows every family of keys the code builds at runtime', () => {
		// Families with a runtime-only segment that are handled by a test of their own:
		// the catalog (automationI18n.test.ts), the report options (reportOptionsI18n.test.ts), the error
		// codes (forkErrorCodes.test.ts), the sentences of automation runs (pkg/automation/messages_test.go) and the widget
		// type names (dashboards.types.<type>).
		const elsewhere = new Set(['automation.enum', 'automation.hint', 'automation.triggerHint', 'automation.field', 'dashboards.types', 'error', 'automation.messages', 'customFields.reasons'])
		const unknown = [...dynamicPrefixes].filter(prefix => !(prefix in FAMILIES) && !elsewhere.has(prefix))
		expect(unknown).toEqual([])
	})

	describe.each(LOCALES)('families of runtime keys in %s', (_name, messages) => {
		it.each(Object.entries(FAMILIES))('%s has all values', (prefix, values) => {
			const missing = values.filter(value => typeof resolveKey(messages, `${prefix}.${value}`) !== 'string')
			expect(missing).toEqual([])
		})
	})

	it.each(LOCALES)('has a name and a description for every widget type in %s', (_name, messages) => {
		const types = Object.keys(resolveKey(messages, 'dashboards.types') as Messages)
		expect(types.length).toBeGreaterThanOrEqual(5)
		const missing = types.flatMap(type => ['name', 'description']
			.filter(part => typeof resolveKey(messages, `dashboards.types.${type}.${part}`) !== 'string')
			.map(part => `${type}.${part}`))
		expect(missing).toEqual([])
	})
})
