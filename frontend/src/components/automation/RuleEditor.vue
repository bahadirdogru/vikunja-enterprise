<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
// The rule editor: three stacked blocks When / If / Then generated from the catalog.
import {computed, nextTick, provide, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'

import ActionCard from '@/components/automation/ActionCard.vue'
import ConditionBuilder from '@/components/automation/ConditionBuilder.vue'
import DryRunPanel from '@/components/automation/DryRunPanel.vue'
import ParamField from '@/components/automation/ParamField.vue'
import TypePicker from '@/components/automation/TypePicker.vue'
import {automationContextKey, type AutomationContext, type AutomationStatus} from '@/components/automation/context'
import Message from '@/components/misc/Message.vue'
import {useSaveRuleMutation, type AutomationRuleResponse} from '@/client/queries/automation'
import type {CustomFieldResponse} from '@/client/queries/customFields'
import {isRequestContextAbort} from '@/client/requestContext'
import {
	actionDef,
	clone,
	defaultParams,
	draftFromRule,
	draftSignature,
	draftToDocument,
	duplicateAction,
	emptyDraft,
	issuesFromProblem,
	knownPaths,
	mapIssues,
	moveItem,
	offeredActions,
	pathToId,
	pruneConditions,
	removeAction,
	triggerDef,
	validateDraft,
	type ActionDraft,
	type CatalogResponse,
	type ConditionNode,
	type Issue,
	type RuleDraft,
} from '@/helpers/automation'
import {summarizeRule} from '@/helpers/automationSummary'
import {getErrorText} from '@/message'

const props = defineProps<{
	projectId: number
	catalog: CatalogResponse
	customFields: CustomFieldResponse[]
	statuses: AutomationStatus[]
	secrets?: string[] | null
	// The rule to edit; null starts a new one (optionally from a template draft).
	rule: AutomationRuleResponse | null
	preset?: RuleDraft | null
}>()

const emit = defineEmits<{
	saved: [rule: AutomationRuleResponse]
	close: []
	dirty: [value: boolean]
}>()

const {t, te} = useI18n({useScope: 'global'})

// The editor is mounted once per edit session (the view keys it), so the props are a one-time snapshot.
/* eslint-disable vue/no-setup-props-reactivity-loss */
const initial: RuleDraft = props.rule ? draftFromRule(props.rule) : props.preset ? clone(props.preset) : emptyDraft()
const draft = ref<RuleDraft>(clone(initial))
const ruleId = ref<number | null>(props.rule?.id ?? null)
const savedSignature = ref(draftSignature(initial))
const disabledReason = ref(props.rule?.disabled_reason ?? '')
/* eslint-enable vue/no-setup-props-reactivity-loss */

const dirty = computed(() => draftSignature(draft.value) !== savedSignature.value)
watch(dirty, value => emit('dirty', value), {immediate: true})

// ---- validation -------------------------------------------------------------------------------------------------
const showClientErrors = ref(false)
const serverIssues = ref<Issue[]>([])
const topError = ref('')

const clientIssues = computed(() => showClientErrors.value ? validateDraft(draft.value, props.catalog, t) : [])
const mapped = computed(() => mapIssues(
	[...clientIssues.value, ...serverIssues.value],
	knownPaths(draft.value, props.catalog),
))
const errors = computed(() => mapped.value.byPath)
const lines = computed(() => mapped.value.lines)

// Server messages describe the saved document: they go stale with the next edit.
watch(() => draftSignature(draft.value), () => {
	serverIssues.value = []
	topError.value = ''
})

const context: AutomationContext = {
	get projectId() { return props.projectId },
	get catalog() { return props.catalog },
	get customFields() { return props.customFields },
	get statuses() { return props.statuses },
	get secrets() { return props.secrets ?? null },
	errors,
	lines,
}
provide(automationContextKey, context)

// ---- editing ----------------------------------------------------------------------------------------------------
const trigger = computed(() => triggerDef(props.catalog, draft.value.trigger.type))

function setTriggerType(type: string) {
	draft.value.trigger = {type, options: {}}
}

function setTriggerOption(name: string, value: unknown) {
	draft.value.trigger.options = {...draft.value.trigger.options, [name]: value}
}

function setConditions(value: ConditionNode | null) {
	draft.value.conditions = value
}

const addKey = ref(0)

function addAction(type: string) {
	const def = actionDef(props.catalog, type)
	draft.value.actions.push({type, params: defaultParams(def)})
	addKey.value++
}

function updateAction(index: number, action: ActionDraft) {
	draft.value.actions[index] = action
}

const summary = computed(() => summarizeRule(draft.value, props.catalog, t, te))

// ---- saving -----------------------------------------------------------------------------------------------------
const saveMutation = useSaveRuleMutation()

// Scrolls to and focuses the topmost input that has a message.
async function focusFirstError() {
	await nextTick()
	const elements = [...errors.value.keys()]
		.map(path => document.getElementById(pathToId(path)))
		.filter((el): el is HTMLElement => el !== null)
		.sort((a, b) => a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1)
	const first = elements[0]
	if (!first) return
	first.scrollIntoView?.({block: 'center'})
	first.focus?.({preventScroll: true})
}

async function save() {
	showClientErrors.value = true
	topError.value = ''
	// Empty groups are dropped on save; do it in the draft too so error paths match what is shown.
	draft.value.conditions = pruneConditions(draft.value.conditions)
	const found = validateDraft(draft.value, props.catalog, t)
	if (found.length > 0) {
		await focusFirstError()
		return
	}
	const projectId = props.projectId
	try {
		const result = await saveMutation.mutateAsync({
			projectId,
			ruleId: ruleId.value,
			document: draftToDocument(draft.value),
		})
		if (props.projectId !== projectId) return
		ruleId.value = result.id
		disabledReason.value = result.disabled_reason
		const next = draftFromRule(result)
		draft.value = clone(next)
		savedSignature.value = draftSignature(next)
		showClientErrors.value = false
		serverIssues.value = []
		emit('saved', result)
	} catch (cause) {
		if (isRequestContextAbort(cause)) return
		const issues = issuesFromProblem(cause)
		if (issues && issues.length > 0) {
			serverIssues.value = issues
			await nextTick()
			if (mapped.value.unmapped.length > 0 && mapped.value.byPath.size === 0) return
			await focusFirstError()
			return
		}
		topError.value = getErrorText(cause)
	}
}

defineExpose({save, dirty, ruleId})

const testable = computed(() => ruleId.value !== null && !dirty.value)
const titleErrors = computed(() => errors.value.get('title') ?? [])
const triggerErrors = computed(() => [...(errors.value.get('trigger') ?? []), ...(errors.value.get('trigger.type') ?? [])])
const actionsErrors = computed(() => errors.value.get('actions') ?? [])
</script>

<template>
	<form
		class="rule-editor"
		novalidate
		@submit.prevent="save()"
	>
		<Message
			v-if="disabledReason"
			variant="warning"
		>
			{{ $te(`automation.disabledReason.${disabledReason}`) ? $t(`automation.disabledReason.${disabledReason}`) : $t('automation.disabledReason.unknown', {reason: disabledReason}) }}
		</Message>
		<Message
			v-if="topError || mapped.unmapped.length > 0"
			variant="danger"
		>
			<p v-if="topError">
				{{ topError }}
			</p>
			<ul v-if="mapped.unmapped.length > 0">
				<li
					v-for="issue in mapped.unmapped"
					:key="issue.path + issue.message"
				>
					<code v-if="issue.path">{{ issue.path }}</code> {{ issue.message }}
				</li>
			</ul>
		</Message>

		<section class="rule-editor__meta">
			<div class="field">
				<label
					class="label"
					:for="pathToId('title')"
				>{{ $t('automation.rule.title') }}</label>
				<input
					:id="pathToId('title')"
					v-model="draft.title"
					class="input"
					:class="{'is-danger': titleErrors.length > 0}"
					type="text"
					maxlength="250"
					:placeholder="$t('automation.rule.titlePlaceholder')"
					:aria-invalid="titleErrors.length > 0 ? true : undefined"
				>
				<p
					v-for="message in titleErrors"
					:key="message"
					class="help is-danger"
					role="alert"
				>
					{{ message }}
				</p>
			</div>
			<div class="field">
				<label
					class="label"
					:for="pathToId('description')"
				>{{ $t('automation.rule.description') }}</label>
				<textarea
					:id="pathToId('description')"
					v-model="draft.description"
					class="textarea"
					rows="2"
				/>
			</div>
			<label class="checkbox rule-editor__enabled">
				<input
					v-model="draft.enabled"
					type="checkbox"
				>
				{{ $t('automation.rule.enabled') }}
			</label>
		</section>

		<p
			class="rule-editor__summary"
			aria-live="polite"
		>
			{{ summary }}
		</p>

		<!-- When -->
		<section
			id="automation-trigger"
			class="rule-block"
			tabindex="-1"
			aria-labelledby="automation-when"
		>
			<h3
				id="automation-when"
				class="rule-block__title"
			>
				<span class="rule-block__badge">{{ $t('automation.when.badge') }}</span>
				{{ $t('automation.when.title') }}
			</h3>
			<div class="rule-block__body">
				<TypePicker
					:id="pathToId('trigger.type')"
					:model-value="draft.trigger.type"
					:items="catalog.triggers"
					:label="$t('automation.when.trigger')"
					:invalid="triggerErrors.length > 0"
					@update:modelValue="setTriggerType"
				/>
				<p
					v-for="message in triggerErrors"
					:key="message"
					class="help is-danger"
					role="alert"
				>
					{{ message }}
				</p>
				<template v-if="trigger && trigger.options.length > 0">
					<ParamField
						v-for="option in trigger.options"
						:key="option.name"
						:param="option"
						:siblings="trigger.options"
						:values="draft.trigger.options"
						:path="`trigger.options.${option.name}`"
						@update="setTriggerOption"
					/>
				</template>
			</div>
		</section>

		<!-- If -->
		<section
			class="rule-block"
			aria-labelledby="automation-if"
		>
			<h3
				id="automation-if"
				class="rule-block__title"
			>
				<span class="rule-block__badge">{{ $t('automation.if.badge') }}</span>
				{{ $t('automation.if.title') }}
			</h3>
			<div class="rule-block__body">
				<ConditionBuilder
					:model-value="draft.conditions"
					path="conditions"
					@update:modelValue="setConditions"
				/>
			</div>
		</section>

		<!-- Then -->
		<section
			id="automation-actions"
			class="rule-block"
			tabindex="-1"
			aria-labelledby="automation-then"
		>
			<h3
				id="automation-then"
				class="rule-block__title"
			>
				<span class="rule-block__badge">{{ $t('automation.then.badge') }}</span>
				{{ $t('automation.then.title') }}
			</h3>
			<div class="rule-block__body">
				<p
					v-for="message in actionsErrors"
					:key="message"
					class="help is-danger"
					role="alert"
				>
					{{ message }}
				</p>
				<ol class="rule-editor__actions">
					<li
						v-for="(action, index) in draft.actions"
						:key="index"
					>
						<ActionCard
							:action="action"
							:index="index"
							:count="draft.actions.length"
							@update:action="updateAction(index, $event)"
							@move="delta => draft.actions = moveItem(draft.actions, index, delta)"
							@duplicate="draft.actions = duplicateAction(draft.actions, index)"
							@remove="draft.actions = removeAction(draft.actions, index)"
						/>
					</li>
				</ol>
				<div class="rule-editor__add-action">
					<TypePicker
						id="automation-add-action"
						:key="addKey"
						model-value=""
						:items="offeredActions(catalog)"
						:label="$t('automation.then.add')"
						:placeholder="$t('automation.then.add')"
						@update:modelValue="addAction"
					/>
				</div>
			</div>
		</section>

		<!-- Test -->
		<section
			class="rule-block"
			aria-labelledby="automation-test"
		>
			<h3
				id="automation-test"
				class="rule-block__title"
			>
				<span class="rule-block__badge"><Icon icon="play" /></span>
				{{ $t('automation.test.title') }}
			</h3>
			<div class="rule-block__body">
				<DryRunPanel
					:rule-id="ruleId"
					:testable="testable"
				/>
			</div>
		</section>

		<footer class="rule-editor__footer">
			<span
				v-if="dirty"
				class="rule-editor__unsaved"
				role="status"
			>{{ $t('automation.unsaved') }}</span>
			<span class="rule-editor__spacer" />
			<XButton
				type="button"
				variant="secondary"
				@click="emit('close')"
			>
				{{ $t('automation.backToList') }}
			</XButton>
			<XButton
				type="submit"
				:loading="saveMutation.isPending.value"
				icon="check"
			>
				{{ $t('automation.save') }}
			</XButton>
		</footer>
	</form>
</template>

<style lang="scss" scoped>
.rule-editor {
	display: flex;
	flex-direction: column;
	gap: 1rem;
	min-inline-size: 0;

	&__meta {
		display: flex;
		flex-direction: column;
		gap: .5rem;
	}

	&__enabled {
		display: flex;
		gap: .5rem;
		align-items: center;
	}

	&__summary {
		margin: 0;
		padding: .5rem .75rem;
		border-radius: $radius;
		background: var(--grey-100);
		color: var(--grey-700);
		font-size: .9rem;
	}

	&__actions,
	&__actions > li {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: .6rem;
	}

	&__footer {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		align-items: center;
		padding-block-start: .6rem;
		border-block-start: 1px solid var(--grey-200);
	}

	&__spacer {
		flex: 1;
	}

	&__unsaved {
		color: var(--grey-600);
		font-size: .875rem;
	}
}

.rule-block {
	border: 1px solid var(--grey-200);
	border-radius: $radius;

	&__title {
		display: flex;
		align-items: center;
		gap: .6rem;
		margin: 0;
		padding: .6rem .8rem;
		font-size: 1rem;
		font-weight: 700;
		border-block-end: 1px solid var(--grey-200);
		background: var(--grey-100);
		border-start-start-radius: $radius;
		border-start-end-radius: $radius;
	}

	&__badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-inline-size: 3.2rem;
		padding: .1rem .5rem;
		border-radius: 999px;
		background: var(--primary);
		color: var(--white);
		font-size: .75rem;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: .03em;
	}

	&__body {
		padding: .8rem;
		display: flex;
		flex-direction: column;
		gap: .8rem;
		min-inline-size: 0;
	}
}
</style>
