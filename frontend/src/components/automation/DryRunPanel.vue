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
import {useTestRuleMutation, type DryRunResponse, type DryRunStepResponse} from '@/client/queries/automation'
import {tasksQuery} from '@/client/queries/tasks'
import {actionDef, catalogLabel, parseWebhookPayload} from '@/helpers/automation'
import {getErrorText} from '@/message'
import {automationCodeText, automationMessageText} from '@/helpers/automationMessages'

const props = defineProps<{
	ruleId: number | null
	// False while the draft differs from the saved rule.
	testable: boolean
	// Whether the rule works on a task. Without one the test needs none; an incoming webhook takes a simulated body.
	hasTask: boolean
	webhook: boolean
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
const payloadText = ref('')
const parsedPayload = computed(() => props.webhook ? parseWebhookPayload(payloadText.value) : {})
const payloadProblem = computed(() => parsedPayload.value.problem
	? t(`automation.test.payload.${parsedPayload.value.problem}`)
	: '')
const canRun = computed(() => props.testable
	&& props.ruleId !== null
	&& (!props.hasTask || taskId.value !== null)
	&& payloadProblem.value === '')

async function run() {
	if (!canRun.value || props.ruleId === null) return
	failure.value = ''
	result.value = null
	try {
		result.value = await test.mutateAsync({
			projectId: context.projectId,
			ruleId: props.ruleId,
			taskId: props.hasTask ? taskId.value : null,
			payload: parsedPayload.value.payload,
		})
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

// The sentences of the server in the language of the reader; the English text is what an unknown code falls back to.
function summaryHead(step: DryRunStepResponse): string {
	return step.summary_message ? automationMessageText(step.summary_message, splitSummary(step.summary).head) : splitSummary(step.summary).head
}

function warningText(step: DryRunStepResponse, index: number): string {
	return automationMessageText(step.warning_messages?.[index], step.warnings[index] ?? '')
}

function noteText(index: number): string {
	return automationMessageText(result.value?.note_messages?.[index], result.value?.notes[index] ?? '')
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
			<template v-if="hasTask">
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
			</template>
			<div
				v-if="webhook"
				class="dry-run__payload"
			>
				<label
					class="label"
					for="automation-test-payload"
				>{{ $t('automation.test.payload.label') }}</label>
				<textarea
					id="automation-test-payload"
					v-model="payloadText"
					class="textarea"
					rows="4"
					spellcheck="false"
					placeholder="{}"
					:aria-invalid="payloadProblem !== '' ? true : undefined"
					:class="{'is-danger': payloadProblem !== ''}"
				/>
				<p
					class="help"
					:class="{'is-danger': payloadProblem !== ''}"
				>
					{{ payloadProblem || $t('automation.test.payload.help') }}
				</p>
			</div>
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
					<span v-if="step.summary">: {{ summaryHead(step) }}</span>
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
						{{ $t('automation.test.wouldFail') }}: {{ automationCodeText(step.error_code, step.error_params, step.error) }}
					</p>
					<p
						v-for="(warning, index) in step.warnings"
						:key="warning"
						class="help dry-run__warning"
					>
						<Icon icon="exclamation-circle" /> {{ warningText(step, index) }}
					</p>
				</li>
			</ol>
			<p
				v-for="(note, index) in result.notes"
				:key="note"
				class="help"
			>
				{{ noteText(index) }}
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

	&__payload {
		flex: 1 1 100%;

		.textarea {
			font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
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
