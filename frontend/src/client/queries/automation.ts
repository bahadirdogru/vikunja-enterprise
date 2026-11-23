import {keepPreviousData, queryOptions, useMutation, type QueryClient} from '@tanstack/vue-query'

import {
	automationCatalog,
	automationManualList,
	automationRunManually,
	automationRunsList,
	automationRunsRead,
	automationRunsRetry,
	automationWebhookToken,
	automationsCreate,
	automationsDelete,
	automationsList,
	automationsTest,
	automationsUpdate,
} from '@/client/generated'
import type {AutomationRun, AutomationRunStep, DryRunResult} from '@/client/generated'
import {contextMutationOptions} from './contextMutation'
import {useSecretMutation} from './secretMutation'
import {fetchAllPages} from './fetchAllPages'
import {toPaginated, type Paginated} from './pagination'
import {i18n} from '@/i18n'
import {resolveAgainstApiRoot} from '@/helpers/apiUrl'
import {
	draftFromRule,
	normalizeCatalog,
	type CatalogResponse,
	type ConditionNode,
	type RuleDocument,
	type RuleDraft,
} from '@/helpers/automation'

export type AutomationRuleResponse = {
	id: number
	project_id: number
	title: string
	description: string
	enabled: boolean
	revision: number
	owner_id: number
	trigger: {type: string, options: Record<string, unknown>}
	conditions: ConditionNode | null
	actions: {type: string, params: Record<string, unknown>}[]
	disabled_reason: string
	consecutive_failures: number
	// The rule has an incoming webhook address. The address itself is never part of the rule: see WebhookReveal.
	has_webhook?: boolean
	last_run_at: string | null
	last_run_status: string
	created: string
	updated: string
}

export type AutomationRunStatus = 'running' | 'success' | 'failed' | 'skipped' | 'aborted'

export type AutomationStepResponse = {
	id: number
	position: number
	action_type: string
	status: string
	error: string
	output: string
	duration_ms: number
}

export type AutomationRunResponse = {
	id: number
	rule_id: number
	rule_revision: number
	event_id: number
	event_name: string
	task_id: number
	status: AutomationRunStatus | string
	skip_reason: string
	error: string
	depth: number
	started: string | null
	finished: string | null
	steps: AutomationStepResponse[]
}

export type DryRunResponse = {
	task_id: number
	conditions_met: boolean
	steps: {position: number, type: string, summary: string, warnings: string[], error: string, stops: boolean}[]
	notes: string[]
}

// The API sends Go zero times for "never".
function dateOrNull(value: string | undefined): string | null {
	return !value || value.startsWith('0001-') ? null : value
}

type RuleLike = {
	id?: number
	project_id?: number
	title?: string
	description?: string
	enabled?: boolean
	revision?: number
	owner_id?: number
	trigger?: {type?: string, options?: Record<string, unknown> | null}
	conditions?: unknown
	actions?: {type?: string, params?: Record<string, unknown> | null}[] | null
	disabled_reason?: string
	consecutive_failures?: number
	has_webhook?: boolean
	last_run_at?: string
	last_run_status?: string
	created?: string
	updated?: string
	// Only in the answer that created or regenerated the address.
	webhook_token?: string
	webhook_url?: string
}

// The address of an incoming webhook, with its secret token. The server returns it once; it lives in the component
// that shows it and nowhere else (no query cache entry, no store, no log).
export type WebhookReveal = {url: string, token: string}

// A saved rule; `webhook` is set when the save created the address of an incoming webhook.
export type SavedRuleResponse = AutomationRuleResponse & {webhook?: WebhookReveal}

export function webhookRevealOf(rule: Pick<RuleLike, 'webhook_token' | 'webhook_url'>): WebhookReveal | undefined {
	return rule.webhook_token && rule.webhook_url
		? {url: resolveAgainstApiRoot(rule.webhook_url), token: rule.webhook_token}
		: undefined
}

export function normalizeRule(rule: RuleLike): AutomationRuleResponse {
	const draft = draftFromRule({...rule, conditions: rule.conditions as ConditionNode | null | undefined})
	return {
		id: rule.id ?? 0,
		project_id: rule.project_id ?? 0,
		title: draft.title,
		description: draft.description,
		enabled: draft.enabled,
		revision: rule.revision ?? 1,
		owner_id: rule.owner_id ?? 0,
		trigger: draft.trigger,
		conditions: draft.conditions,
		actions: draft.actions,
		disabled_reason: rule.disabled_reason ?? '',
		consecutive_failures: rule.consecutive_failures ?? 0,
		has_webhook: rule.has_webhook === true,
		last_run_at: dateOrNull(rule.last_run_at),
		last_run_status: rule.last_run_status ?? '',
		created: rule.created ?? '',
		updated: rule.updated ?? '',
	}
}

function normalizeStep(step: AutomationRunStep): AutomationStepResponse {
	return {
		id: step.id ?? 0,
		position: step.position ?? 0,
		action_type: step.action_type ?? '',
		status: step.status ?? '',
		error: step.error ?? '',
		output: step.output ?? '',
		duration_ms: step.duration_ms ?? 0,
	}
}

export function normalizeRun(run: AutomationRun): AutomationRunResponse {
	return {
		id: run.id ?? 0,
		rule_id: run.rule_id ?? 0,
		rule_revision: run.rule_revision ?? 0,
		event_id: run.event_id ?? 0,
		event_name: run.event_name ?? '',
		task_id: run.task_id ?? 0,
		status: run.status ?? '',
		skip_reason: run.skip_reason ?? '',
		error: run.error ?? '',
		depth: run.depth ?? 0,
		started: dateOrNull(run.started),
		finished: dateOrNull(run.finished),
		steps: (run.steps ?? []).map(normalizeStep).sort((a, b) => a.position - b.position),
	}
}

function normalizeDryRun(result: DryRunResult): DryRunResponse {
	return {
		task_id: result.task_id ?? 0,
		conditions_met: result.conditions_met ?? false,
		steps: (result.steps ?? []).map(step => ({
			position: step.position ?? 0,
			type: step.type ?? '',
			summary: step.summary ?? '',
			warnings: step.warnings ?? [],
			error: step.error ?? '',
			stops: step.stops ?? false,
		})),
		notes: result.notes ?? [],
	}
}

export const automationKeys = {
	all: ['automation'] as const,
	catalog: (projectId: number) => ['automation', 'catalog', projectId] as const,
	rules: (projectId: number) => ['automation', 'rules', projectId] as const,
	runsScope: (projectId: number, ruleId: number) => ['automation', 'runs', projectId, ruleId] as const,
	runs: (projectId: number, ruleId: number, page: number) => ['automation', 'runs', projectId, ruleId, page] as const,
	run: (projectId: number, ruleId: number, runId: number) => ['automation', 'run', projectId, ruleId, runId] as const,
	manual: (projectId: number) => ['automation', 'manual', projectId] as const,
}

// The rules of a project and the list of its manual rules change together.
function invalidateRules(client: QueryClient, projectId: number) {
	return Promise.all([
		client.invalidateQueries({queryKey: automationKeys.rules(projectId), exact: true}),
		client.invalidateQueries({queryKey: automationKeys.manual(projectId), exact: true}),
	])
}

// An enabled rule with a manual trigger, as the button of a task shows it.
export type ManualAutomation = {id: number, title: string, label: string}

export function manualAutomationsQuery(projectId: number) {
	return queryOptions({
		queryKey: automationKeys.manual(projectId),
		queryFn: async ({signal}): Promise<ManualAutomation[]> => {
			const rules = (await automationManualList({path: {project: projectId}, signal})).data ?? []
			return rules.map(rule => ({
				id: rule.id ?? 0,
				title: rule.title ?? '',
				label: rule.label || rule.title || '',
			})).filter(rule => rule.id > 0)
		},
		enabled: projectId > 0,
		staleTime: 60 * 1000,
		// A project without the right (or without the feature) simply has no button; nothing to toast.
		retry: false,
		meta: {handlesError: true},
	})
}

export function automationCatalogQuery(projectId: number) {
	return queryOptions({
		queryKey: automationKeys.catalog(projectId),
		queryFn: async ({signal}): Promise<CatalogResponse> => normalizeCatalog(
			(await automationCatalog({query: {project_id: projectId}, signal})).data as unknown as Record<string, unknown>,
		),
		enabled: projectId > 0,
		staleTime: 60 * 1000,
	})
}

export function automationRulesQuery(projectId: number) {
	return queryOptions({
		queryKey: automationKeys.rules(projectId),
		queryFn: async ({signal}): Promise<AutomationRuleResponse[]> => {
			const rules = await fetchAllPages(async page => (await automationsList({
				path: {project: projectId},
				query: {page},
				signal,
			})).data)
			return rules.map(rule => normalizeRule(rule as RuleLike))
		},
		enabled: projectId > 0,
	})
}

export function automationRunsQuery(projectId: number, ruleId: number, page: number) {
	return queryOptions({
		queryKey: automationKeys.runs(projectId, ruleId, page),
		queryFn: async ({signal}): Promise<Paginated<AutomationRunResponse>> => {
			const data = (await automationRunsList({path: {project: projectId, rule: ruleId}, query: {page}, signal})).data
			const paginated = toPaginated(data, page)
			return {...paginated, items: (data.items ?? []).map(run => normalizeRun(run as AutomationRun))}
		},
		enabled: projectId > 0 && ruleId > 0,
		placeholderData: keepPreviousData,
	})
}

export function automationRunQuery(projectId: number, ruleId: number, runId: number) {
	return queryOptions({
		queryKey: automationKeys.run(projectId, ruleId, runId),
		queryFn: async ({signal}): Promise<AutomationRunResponse> =>
			normalizeRun((await automationRunsRead({path: {project: projectId, rule: ruleId, run: runId}, signal})).data),
		enabled: projectId > 0 && ruleId > 0 && runId > 0,
	})
}

// The document sent to the server; conditions may hold a script node the generated type does not know.
type RuleBody = Parameters<typeof automationsCreate>[0]['body']

function toBody(document: RuleDocument): RuleBody {
	return document as unknown as RuleBody
}

export type SaveRuleInput = {projectId: number, ruleId: number | null, document: RuleDocument}

export function saveRuleMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, ruleId, document}: SaveRuleInput): Promise<SavedRuleResponse> => {
			const data = ruleId
				? (await automationsUpdate({path: {project: projectId, rule: ruleId}, body: toBody(document)})).data
				: (await automationsCreate({path: {project: projectId}, body: toBody(document)})).data
			const webhook = webhookRevealOf(data)
			return {...normalizeRule(data), ...(webhook ? {webhook} : {})}
		},
		onSuccess: (result, {projectId}, client) => {
			// The address of a webhook is shown once and is not kept in the cache.
			const {webhook: _address, ...saved} = result
			client.setQueryData<AutomationRuleResponse[]>(automationKeys.rules(projectId), current => {
				if (!current) return current
				return current.some(rule => rule.id === saved.id)
					? current.map(rule => rule.id === saved.id ? saved : rule)
					: [...current, saved]
			})
		},
		onSettled: ({projectId}, client) => invalidateRules(client, projectId),
		successMessage: () => i18n.global.t('automation.saveSuccess'),
		// The editor maps validation errors onto the inputs itself.
		toastError: () => false,
	})
}

// A quick toggle: the PUT replaces every editable field, so the whole document is sent.
export function toggleRuleMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({rule, enabled}: {rule: AutomationRuleResponse, enabled: boolean}) => normalizeRule(
			(await automationsUpdate({
				path: {project: rule.project_id, rule: rule.id},
				body: toBody({
					title: rule.title,
					description: rule.description,
					enabled,
					trigger: rule.trigger,
					...(rule.conditions ? {conditions: rule.conditions} : {}),
					actions: rule.actions,
				}),
			})).data,
		),
		onSuccess: (saved, {rule}, client) => {
			client.setQueryData<AutomationRuleResponse[]>(automationKeys.rules(rule.project_id),
				current => current?.map(r => r.id === saved.id ? saved : r))
		},
		onSettled: ({rule}, client) => invalidateRules(client, rule.project_id),
	})
}

export function deleteRuleMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, ruleId}: {projectId: number, ruleId: number}) => {
			await automationsDelete({path: {project: projectId, rule: ruleId}})
		},
		onSuccess: (_data, {projectId, ruleId}, client) => {
			client.setQueryData<AutomationRuleResponse[]>(automationKeys.rules(projectId),
				current => current?.filter(rule => rule.id !== ruleId))
		},
		onSettled: ({projectId}, client) => invalidateRules(client, projectId),
		successMessage: () => i18n.global.t('automation.deleteSuccess'),
	})
}

// A dry run changes nothing, so it touches no cache; the caller shows the result.
export function testRuleMutationOptions() {
	return contextMutationOptions({
		// A rule without a task ignores task_id; the simulated body of an incoming webhook goes as payload.
		mutationFn: async ({projectId, ruleId, taskId, payload}: {
			projectId: number
			ruleId: number
			taskId?: number | null
			payload?: Record<string, unknown>
		}) => normalizeDryRun((await automationsTest({
			path: {project: projectId, rule: ruleId},
			body: {
				...(taskId ? {task_id: taskId} : {}),
				...(payload ? {payload} : {}),
			},
		})).data),
		toastError: () => false,
	})
}

export function retryRunMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, ruleId, runId}: {projectId: number, ruleId: number, runId: number}) =>
			(await automationRunsRetry({path: {project: projectId, rule: ruleId, run: runId}})).data,
		onSettled: ({projectId, ruleId}, client) => Promise.all([
			client.invalidateQueries({queryKey: automationKeys.runsScope(projectId, ruleId)}),
			client.invalidateQueries({queryKey: ['automation', 'run', projectId, ruleId]}),
		]),
		successMessage: () => i18n.global.t('automation.runs.retryQueued'),
	})
}

// The answer of a save can carry the address of an incoming webhook, which is a secret: read it from mutateAsync.
export function useSaveRuleMutation() {
	return useSecretMutation(saveRuleMutationOptions())
}

// A new address (and secret token) for an incoming webhook; the old one stops working. The answer is a secret.
export function rotateWebhookTokenMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, ruleId}: {projectId: number, ruleId: number}): Promise<WebhookReveal> => {
			const data = (await automationWebhookToken({path: {project: projectId, rule: ruleId}})).data
			return {url: data.url ? resolveAgainstApiRoot(data.url) : '', token: data.token ?? ''}
		},
		onSettled: ({projectId}, client) => invalidateRules(client, projectId),
		// The panel that asked shows the failure next to the button.
		toastError: () => false,
	})
}

export function useRotateWebhookTokenMutation() {
	return useSecretMutation(rotateWebhookTokenMutationOptions())
}

// Starts a manual rule for a task. The run is queued; the history of the rule shows how it went.
export type RunRuleInput = {projectId: number, ruleId: number, taskId: number, title: string}

export function runRuleManuallyMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, ruleId, taskId}: RunRuleInput) =>
			(await automationRunManually({path: {project: projectId, rule: ruleId}, body: {task_id: taskId}})).data,
		onSettled: ({projectId, ruleId}, client) => client.invalidateQueries({queryKey: automationKeys.runsScope(projectId, ruleId)}),
		// 22009: the rule was disabled or changed since the list was read, so its entry has to go.
		onFailure: (_cause, {projectId}, client) => {
			void client.invalidateQueries({queryKey: automationKeys.manual(projectId), exact: true})
		},
		successMessage: (_data, {title}) =>
			i18n.global.t('automation.manual.queued', {title}),
	})
}

export function useRunRuleManuallyMutation() {
	return useMutation(runRuleManuallyMutationOptions())
}

export function useToggleRuleMutation() {
	return useMutation(toggleRuleMutationOptions())
}

export function useDeleteRuleMutation() {
	return useMutation(deleteRuleMutationOptions())
}

export function useTestRuleMutation() {
	return useMutation(testRuleMutationOptions())
}

export function useRetryRunMutation() {
	return useMutation(retryRunMutationOptions())
}

export function draftOfRule(rule: AutomationRuleResponse): RuleDraft {
	return draftFromRule(rule)
}
