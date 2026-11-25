import {describe, expect, it} from 'vitest'
import {readFileSync} from 'node:fs'
import {resolve as resolvePath} from 'node:path'

// A normaliser that forgets a field of the API type hides it from every component (this happened with
// `cause` of the status history). For each normaliser of a fork module this reads the generated type and the
// function text, and requires every field of the type to be named in the function or spread into the result.
// Fields that are deliberately not passed on are listed with the reason.

const root = process.cwd()
const read = (path: string) => readFileSync(resolvePath(root, 'src', path), 'utf-8')
const generated = read('client/generated/types.gen.ts')

function typeKeys(name: string): string[] {
	const match = generated.match(new RegExp(`export type ${name} = \\{\\n([\\s\\S]*?)\\n\\};\\n`))
	if (!match) throw new Error(`type ${name} not found in the generated client`)
	return [...match[1]!.matchAll(/^ {4}(?:readonly )?([A-Za-z0-9_$]+)\??:/gm)].map(m => m[1]!)
}

function functionText(file: string, name: string): string {
	const text = read(file)
	const start = text.search(new RegExp(`function ${name}\\(`))
	if (start < 0) throw new Error(`function ${name} not found in ${file}`)
	return text.slice(start, text.indexOf('\n}\n', start))
}

const SCHEMA = '$schema'

type Check = {file: string, fn: string, type: string, dropped?: Record<string, string>}

const CHECKS: Check[] = [
	{file: 'client/queries/workflow.ts', fn: 'normalizeHistory', type: 'TaskStatusHistory', dropped: {task_id: 'the history is requested per task'}},
	{file: 'client/queries/workflow.ts', fn: 'normalizeWorkflowStatus', type: 'WorkflowStatus', dropped: {workflow_id: 'implied by the workflow'}},
	{file: 'client/queries/workflow.ts', fn: 'normalizeWorkflowTransition', type: 'WorkflowTransition', dropped: {workflow_id: 'implied by the workflow'}},
	{file: 'client/queries/workflow.ts', fn: 'normalizeWorkflow', type: 'Workflow', dropped: {created: 'not shown', updated: 'not shown', move_tasks_to: 'request-only'}},
	{file: 'client/queries/workflow.ts', fn: 'normalizeAvailableTransition', type: 'AvailableTransition'},
	{file: 'client/queries/workflow.ts', fn: 'normalizeTaskStatus', type: 'TaskWorkflowStatus'},
	{file: 'client/queries/customFields.ts', fn: 'normalizeCustomField', type: 'CustomField', dropped: {config: 'spread from the field', created: 'not shown', created_by: 'not shown', updated: 'not shown'}},
	{file: 'client/queries/customFields.ts', fn: 'normalizeCustomFieldValue', type: 'CustomFieldValue'},
	{file: 'client/queries/forms.ts', fn: 'normalizeForm', type: 'Form', dropped: {}},
	{file: 'client/queries/forms.ts', fn: 'normalizeSubmission', type: 'FormSubmission', dropped: {form_id: 'implied by the list'}},
	{file: 'client/queries/dashboards.ts', fn: 'normalizeWidget', type: 'DashboardWidget', dropped: {created: 'not shown', updated: 'not shown', dashboard_id: 'implied by the dashboard'}},
	{file: 'client/queries/dashboards.ts', fn: 'normalizeDashboard', type: 'Dashboard', dropped: {created: 'not shown'}},
	{file: 'client/queries/automation.ts', fn: 'normalizeStep', type: 'AutomationRunStep', dropped: {created: 'not shown', run_id: 'implied by the run'}},
	{file: 'client/queries/automation.ts', fn: 'normalizeRun', type: 'AutomationRun', dropped: {project_id: 'implied by the list'}},
	{file: 'client/queries/automation.ts', fn: 'normalizeDryRun', type: 'DryRunResult'},
	{file: 'helpers/approvals.ts', fn: 'normalizeApprover', type: 'ApprovalApprover', dropped: {request_id: 'implied by the request'}},
	{file: 'helpers/approvals.ts', fn: 'normalizeApproval', type: 'ApprovalRequest', dropped: {updated: 'not shown'}},
	{file: 'helpers/approvals.ts', fn: 'normalizeTransitionApproval', type: 'TransitionApproval'},
	{file: 'client/queries/reports.ts', fn: 'normalizeGroup', type: 'ReportGroup'},
	{file: 'client/queries/reports.ts', fn: 'normalizeReport', type: 'TaskReport'},
	{file: 'client/queries/reportWidgets.ts', fn: 'normalizeStat', type: 'StatusTimeStat'},
	{file: 'client/queries/reportWidgets.ts', fn: 'normalizeStatusGroup', type: 'StatusTimeGroup'},
	{file: 'client/queries/reportWidgets.ts', fn: 'normalizeStatusTime', type: 'StatusTimeReport', dropped: {from: 'the range is the widget config', to: 'the range is the widget config', timezone: 'not shown'}},
	{file: 'client/queries/reportWidgets.ts', fn: 'normalizeRow', type: 'WorkloadRow'},
	{file: 'client/queries/reportWidgets.ts', fn: 'normalizeWorkload', type: 'WorkloadReport', dropped: {multi_valued: 'a table, not a chart with a note', timezone: 'not shown'}},
	{file: 'client/queries/reportWidgets.ts', fn: 'normalizeTimeTracking', type: 'TimeTrackingReport', dropped: {from: 'the range is the widget config', to: 'the range is the widget config', timezone: 'not shown'}},
	{file: 'helpers/formsPublic.ts', fn: 'normalizeDefinition', type: 'FormPublicDefinition'},
	{file: 'helpers/forms.ts', fn: 'normalizeOptions', type: 'FormOptions'},
]

describe('normalisers of the fork modules', () => {
	it.each(CHECKS.map(check => [check.fn, check] as const))('%s keeps every field of its API type', (_name, check) => {
		const body = functionText(check.file, check.fn)
		const spreads = /\.\.\.[a-z]+[,\n]/.test(body)
		const dropped = check.dropped ?? {}
		const lost = typeKeys(check.type).filter(key => key !== SCHEMA
			&& !(key in dropped)
			&& !spreads
			&& !new RegExp(`\\b${key.replace(/\$/g, '\\$')}\\b`).test(body))
		expect(lost).toEqual([])
	})

	it.each(CHECKS.filter(c => c.dropped && Object.keys(c.dropped).length > 0).map(c => [c.fn, c] as const))('%s: every field it is allowed to drop is a field of the type', (_name, check) => {
		const keys = typeKeys(check.type)
		expect(Object.keys(check.dropped!).filter(key => !keys.includes(key))).toEqual([])
	})
})
