<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
// "Test with a task": the API only dry-runs saved rules, so the panel is disabled for a new or changed draft.
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'
import {refDebounced} from '@vueuse/core'

import AutomationBadge from '@/components/automation/AutomationBadge.vue'
import AutomationSelect from '@/components/automation/AutomationSelect.vue'
import {useAutomationContext} from '@/components/automation/context'
import {isRequestContextAbort} from '@/client/requestContext'
import {useTestRuleMutation, type DryRunResponse} from '@/client/queries/automation'
import {tasksQuery} from '@/client/queries/tasks'
import {actionDef, catalogLabel} from '@/helpers/automation'
import {getErrorText} from '@/message'

const props = defineProps<{
	ruleId: number | null
	// False while the draft differs from the saved rule.
	testable: boolean
}>()

const {t, te} = useI18n({useScope: 'global'})
const context = useAutomationContext()

const search = ref('')
const debounced = refDebounced(search, 250)
const taskId = ref<number | null>(null)
const result = ref<DryRunResponse | null>(null)
const failure = ref('')

const tasks = useQuery(computed(() => tasksQuery({
	project: context.projectId,
	params: debounced.value ? {q: debounced.value} : {},
})))

const taskChoices = computed(() => (tasks.data.value?.items ?? []).map(task => ({
	value: task.id,
	label: `${task.identifier ? task.identifier + ' ' : ''}${task.title}`,
})))

const test = useTestRuleMutation()
const canRun = computed(() => props.testable && props.ruleId !== null && taskId.value !== null)

async function run() {
	if (!canRun.value || props.ruleId === null || taskId.value === null) return
	failure.value = ''
	result.value = null
	try {
		result.value = await test.mutateAsync({projectId: context.projectId, ruleId: props.ruleId, taskId: taskId.value})
	} catch (cause) {
		if (isRequestContextAbort(cause)) return
		failure.value = getErrorText(cause)
	}
}

// A script step reports several lines ("would call ..." for what it would change, then its log() output); the first
// line is the headline, the rest is shown as it is in a monospace box.
function splitSummary(summary: string): {head: string, rest: string} {
	const index = summary.indexOf('\n')
	return index === -1 ? {head: summary, rest: ''} : {head: summary.slice(0, index), rest: summary.slice(index + 1)}
}

function stepTitle(type: string): string {
	return catalogLabel(t, te, actionDef(context.catalog, type)?.title_key ?? '', type)
}
</script>

<template>
	<section
		class="dry-run"
		:aria-label="$t('automation.test.title')"
	>
		<p
			v-if="!testable"
			class="help"
		>
			{{ ruleId === null ? $t('automation.test.saveFirst') : $t('automation.test.unsaved') }}
		</p>
		<div class="dry-run__form">
			<input
				v-model="search"
				class="input"
				type="search"
				:aria-label="$t('automation.test.searchTask')"
				:placeholder="$t('automation.test.searchTask')"
			>
			<AutomationSelect
				v-model="taskId"
				:options="taskChoices"
				:label="$t('automation.test.task')"
				:placeholder="$t('automation.test.chooseTask')"
			/>
			<XButton
				type="button"
				:loading="test.isPending.value"
				:disabled="!canRun"
				icon="play"
				@click="run"
			>
				{{ $t('automation.test.run') }}
			</XButton>
		</div>

		<p
			v-if="failure"
			class="help is-danger"
			role="alert"
		>
			{{ failure }}
		</p>

		<div
			v-if="result"
			class="dry-run__result"
			role="status"
		>
			<p>
				<AutomationBadge :variant="result.conditions_met ? 'success' : 'warning'">
					{{ result.conditions_met ? $t('automation.test.conditionsMet') : $t('automation.test.conditionsNotMet') }}
				</AutomationBadge>
			</p>
			<p
				v-if="!result.conditions_met"
				class="help"
			>
				{{ $t('automation.test.nothingWouldRun') }}
			</p>
			<ol class="dry-run__steps">
				<li
					v-for="step in result.steps"
					:key="step.position"
					:class="{'has-error': step.error}"
				>
					<strong>{{ stepTitle(step.type) }}</strong>
					<span v-if="step.summary">: {{ splitSummary(step.summary).head }}</span>
					<AutomationBadge
						v-if="step.stops"
						variant="info"
						class="mis-2"
					>
						{{ $t('automation.test.stops') }}
					</AutomationBadge>
					<pre
						v-if="splitSummary(step.summary).rest"
						class="dry-run__output"
						:aria-label="$t('automation.test.scriptOutput')"
					>{{ splitSummary(step.summary).rest }}</pre>
					<p
						v-if="step.error"
						class="help is-danger"
					>
						{{ $t('automation.test.wouldFail') }}: {{ step.error }}
					</p>
					<p
						v-for="warning in step.warnings"
						:key="warning"
						class="help dry-run__warning"
					>
						<Icon icon="exclamation-circle" /> {{ warning }}
					</p>
				</li>
			</ol>
			<p
				v-for="note in result.notes"
				:key="note"
				class="help"
			>
				{{ note }}
			</p>
		</div>
	</section>
</template>

<style lang="scss" scoped>
.dry-run {
	display: flex;
	flex-direction: column;
	gap: .6rem;

	&__form {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		align-items: center;

		.input {
			max-inline-size: 14rem;
		}
	}

	&__result {
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		padding: .6rem .8rem;
	}

	&__steps {
		margin: .4rem 0 0;
		padding-inline-start: 1.4rem;

		li {
			margin-block: .35rem;
		}

		li.has-error strong {
			color: var(--danger);
		}
	}

	&__output {
		margin: .35rem 0 0;
		padding: .5rem .7rem;
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		background: var(--grey-100);
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: .8rem;
		line-height: 1.45;
		white-space: pre-wrap;
		word-break: break-word;
		max-block-size: 18rem;
		overflow: auto;
	}

	&__warning {
		color: var(--warning);
	}
}
</style>
