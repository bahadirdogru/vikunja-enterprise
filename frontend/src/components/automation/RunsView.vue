<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
// Runs of one rule: paginated table, status filter and the steps of the selected run. Refreshes while something runs.
import {computed, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'

import RunStatusBadge from '@/components/automation/RunStatusBadge.vue'
import Loading from '@/components/misc/Loading.vue'
import {
	automationRunQuery,
	automationRunsQuery,
	useRetryRunMutation,
	type AutomationRuleResponse,
	type AutomationRunResponse,
} from '@/client/queries/automation'
import {actionDef, catalogLabel, humanize, type CatalogResponse} from '@/helpers/automation'
import {formatDateLong, formatDateSince} from '@/helpers/time/formatDate'
import {automationCodeText, automationOutput} from '@/helpers/automationMessages'

const props = defineProps<{
	projectId: number
	rule: AutomationRuleResponse
	catalog: CatalogResponse
}>()

const emit = defineEmits<{
	back: []
}>()

const REFRESH_MS = 2500

const {t, te} = useI18n({useScope: 'global'})

const page = ref(1)
const statusFilter = ref('')
const selectedId = ref<number | null>(null)

const runs = useQuery(computed(() => ({
	...automationRunsQuery(props.projectId, props.rule.id, page.value),
	refetchInterval: (query: {state: {data?: {items: AutomationRunResponse[]} }}) =>
		query.state.data?.items.some(run => run.status === 'running') ? REFRESH_MS : false,
})))

// The API has no status filter: it narrows the loaded page.
const visibleRuns = computed(() => (runs.data.value?.items ?? [])
	.filter(run => statusFilter.value === '' || run.status === statusFilter.value))

const detail = useQuery(computed(() => ({
	...automationRunQuery(props.projectId, props.rule.id, selectedId.value ?? 0),
	refetchInterval: (query: {state: {data?: AutomationRunResponse}}) =>
		query.state.data?.status === 'running' ? REFRESH_MS : false,
})))

watch(() => props.rule.id, () => {
	page.value = 1
	selectedId.value = null
})

const retry = useRetryRunMutation()

async function retryRun(run: AutomationRunResponse) {
	try {
		await retry.mutateAsync({projectId: props.projectId, ruleId: props.rule.id, runId: run.id})
	} catch {
		// The mutation already reported the failure.
	}
}

const statuses = ['running', 'success', 'failed', 'skipped', 'aborted']

function triggerLabel(eventName: string): string {
	const trigger = props.catalog.triggers.find(item => item.event === eventName)
	if (trigger) return catalogLabel(t, te, trigger.title_key, trigger.type)
	return eventName === '' ? '-' : eventName
}

function duration(run: AutomationRunResponse): string {
	if (!run.started || !run.finished) return '-'
	const ms = new Date(run.finished).getTime() - new Date(run.started).getTime()
	if (!Number.isFinite(ms) || ms < 0) return '-'
	return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`
}

function skipReason(run: AutomationRunResponse): string {
	if (run.skip_reason === '') return ''
	const key = `automation.skipReason.${run.skip_reason}`
	return te(key) ? t(key) : humanize(run.skip_reason)
}

// The reason a run or a step failed, said in the language of the reader when the server sent a code for it.
function failure(item: {error: string, error_code?: string, error_params?: {[key: string]: string}}): string {
	return automationCodeText(item.error_code, item.error_params, item.error)
}

function stepTitle(type: string): string {
	return catalogLabel(t, te, actionDef(props.catalog, type)?.title_key ?? '', type)
}

const selected = computed<AutomationRunResponse | null>(() => detail.data.value ?? null)

function toggleSelect(run: AutomationRunResponse) {
	selectedId.value = selectedId.value === run.id ? null : run.id
}

const totalPages = computed(() => runs.data.value?.total_pages ?? 1)
</script>

<template>
	<div class="runs">
		<header class="runs__head">
			<XButton
				type="button"
				variant="secondary"
				icon="angle-left"
				@click="emit('back')"
			>
				{{ $t('automation.backToList') }}
			</XButton>
			<h3>{{ $t('automation.runs.title', {title: rule.title}) }}</h3>
			<label class="runs__filter">
				<span>{{ $t('automation.runs.filter') }}</span>
				<div class="select">
					<select
						v-model="statusFilter"
						:aria-label="$t('automation.runs.filter')"
					>
						<option value="">{{ $t('automation.runs.allStatuses') }}</option>
						<option
							v-for="status in statuses"
							:key="status"
							:value="status"
						>{{ $t(`automation.runStatus.${status}`) }}</option>
					</select>
				</div>
			</label>
		</header>

		<Loading v-if="runs.isPending.value" />
		<p
			v-else-if="(runs.data.value?.items.length ?? 0) === 0"
			class="runs__empty"
		>
			{{ $t('automation.runs.empty') }}
		</p>
		<div
			v-else
			class="runs__scroll"
		>
			<table class="table runs__table">
				<thead>
					<tr>
						<th scope="col">
							{{ $t('automation.runs.status') }}
						</th>
						<th scope="col">
							{{ $t('automation.runs.trigger') }}
						</th>
						<th scope="col">
							{{ $t('automation.runs.task') }}
						</th>
						<th scope="col">
							{{ $t('automation.runs.started') }}
						</th>
						<th scope="col">
							{{ $t('automation.runs.duration') }}
						</th>
						<th scope="col">
							{{ $t('automation.runs.depth') }}
						</th>
						<th scope="col">
							{{ $t('automation.runs.reason') }}
						</th>
						<th scope="col">
							<span class="is-sr-only">{{ $t('misc.actions') }}</span>
						</th>
					</tr>
				</thead>
				<tbody>
					<tr
						v-for="run in visibleRuns"
						:key="run.id"
						:class="{'is-open': run.id === selectedId}"
					>
						<td><RunStatusBadge :status="run.status" /></td>
						<td>{{ triggerLabel(run.event_name) }}</td>
						<td>
							<RouterLink
								v-if="run.task_id"
								:to="{name: 'task.detail', params: {id: run.task_id}}"
							>
								#{{ run.task_id }}
							</RouterLink>
							<span v-else>-</span>
						</td>
						<td>
							<span v-tooltip="formatDateLong(run.started)">{{ formatDateSince(run.started) }}</span>
						</td>
						<td>{{ duration(run) }}</td>
						<td>{{ run.depth }}</td>
						<td>{{ skipReason(run) }}</td>
						<td class="runs__row-actions">
							<XButton
								type="button"
								:aria-expanded="run.id === selectedId"
								variant="secondary"
								@click="toggleSelect(run)"
							>
								{{ $t('automation.runs.steps') }}
							</XButton>
							<XButton
								v-if="run.status === 'failed'"
								type="button"
								:loading="retry.isPending.value"
								variant="secondary"
								icon="redo"
								@click="retryRun(run)"
							>
								{{ $t('automation.runs.retry') }}
							</XButton>
						</td>
					</tr>
				</tbody>
			</table>
		</div>

		<nav
			v-if="totalPages > 1"
			class="runs__pager"
			:aria-label="$t('automation.runs.pagination')"
		>
			<XButton
				type="button"
				variant="secondary"
				:disabled="page <= 1"
				@click="page--"
			>
				{{ $t('misc.previous') }}
			</XButton>
			<span>{{ page }} / {{ totalPages }}</span>
			<XButton
				type="button"
				variant="secondary"
				:disabled="page >= totalPages"
				@click="page++"
			>
				{{ $t('misc.next') }}
			</XButton>
		</nav>

		<section
			v-if="selectedId !== null"
			class="run-detail"
			:aria-label="$t('automation.runs.detail')"
		>
			<Loading v-if="detail.isPending.value" />
			<template v-else-if="selected">
				<h4>
					{{ $t('automation.runs.detailTitle', {id: selected.id}) }}
					<RunStatusBadge :status="selected.status" />
					<span
						v-if="selected.status === 'running'"
						class="help"
					>{{ $t('automation.runs.autoRefresh') }}</span>
				</h4>
				<p
					v-if="selected.error"
					class="help is-danger"
					role="alert"
				>
					{{ failure(selected) }}
				</p>
				<p
					v-if="skipReason(selected)"
					class="help"
				>
					{{ skipReason(selected) }}
				</p>
				<p
					v-if="selected.steps.length === 0"
					class="help"
				>
					{{ $t('automation.runs.noSteps') }}
				</p>
				<ol class="run-detail__steps">
					<li
						v-for="step in selected.steps"
						:key="step.id"
					>
						<div class="run-detail__step-head">
							<span class="run-detail__pos">{{ step.position + 1 }}</span>
							<strong>{{ stepTitle(step.action_type) }}</strong>
							<RunStatusBadge :status="step.status" />
							<span class="help">{{ step.duration_ms }} ms</span>
						</div>
						<p
							v-if="step.error"
							class="help is-danger"
						>
							{{ failure(step) }}
						</p>
						<template v-if="step.output">
							<p
								v-for="(sentence, sentenceIndex) in automationOutput(step.output, step.output_messages).sentences"
								:key="sentenceIndex"
								class="help"
							>
								{{ sentence }}
							</p>
							<pre
								v-if="automationOutput(step.output, step.output_messages).rest"
								class="run-detail__output"
								tabindex="0"
								:aria-label="$t('automation.runs.output')"
							>{{ automationOutput(step.output, step.output_messages).rest }}</pre>
						</template>
					</li>
				</ol>
			</template>
		</section>
	</div>
</template>

<style lang="scss" scoped>
.runs {
	display: flex;
	flex-direction: column;
	gap: .8rem;
	min-inline-size: 0;

	&__head {
		display: flex;
		flex-wrap: wrap;
		gap: .6rem 1rem;
		align-items: center;

		h3 {
			margin: 0;
			flex: 1 1 12rem;
			font-size: 1.05rem;
			overflow-wrap: anywhere;
		}
	}

	&__filter {
		display: flex;
		align-items: center;
		gap: .5rem;
		font-size: .85rem;
	}

	&__scroll {
		overflow-x: auto;
		max-inline-size: 100%;
	}

	&__table {
		inline-size: 100%;
		font-size: .85rem;

		td,
		th {
			vertical-align: middle;
			white-space: nowrap;
		}

		tr.is-open {
			background: var(--grey-100);
		}
	}

	&__row-actions {
		display: flex;
		gap: .3rem;
	}

	&__pager {
		display: flex;
		gap: .6rem;
		align-items: center;
		justify-content: center;
	}

	&__empty {
		text-align: center;
		color: var(--grey-600);
		padding-block: 1.5rem;
	}
}

.run-detail {
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	padding: .7rem .9rem;

	h4 {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		align-items: center;
		margin: 0 0 .5rem;
	}

	&__steps,
	&__steps > li {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: .6rem;
	}

	&__step-head {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		align-items: center;
	}

	&__pos {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		inline-size: 1.5rem;
		block-size: 1.5rem;
		border-radius: 50%;
		background: var(--grey-200);
		font-size: .75rem;
		font-weight: 700;
	}

	&__output {
		margin: .3rem 0 0;
		padding: .5rem .6rem;
		max-block-size: 10rem;
		overflow: auto;
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: .78rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		background: var(--grey-100);
		color: var(--text);
		border-radius: $radius;
	}
}
</style>
