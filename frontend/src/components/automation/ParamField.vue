<script setup lang="ts">
// The one generic renderer of a catalog parameter (trigger option or action param): label, input of the right kind
// and the validation messages of its path.
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import AutomationSelect from '@/components/automation/AutomationSelect.vue'
import CronInput from '@/components/automation/CronInput.vue'
import ParamInput from '@/components/automation/ParamInput.vue'
import {useAutomationContext} from '@/components/automation/context'
import {
	catalogLabel,
	customFieldIdOf,
	effectiveKind,
	pathToId,
	type CatalogParam,
} from '@/helpers/automation'
import {allowsSecretPlaceholder} from '@/helpers/automationScript'

const props = defineProps<{
	param: CatalogParam
	// The other params of the same action / trigger, for kind_by and for the custom field a value belongs to.
	siblings: CatalogParam[]
	values: Record<string, unknown>
	path: string
	dateContext?: 'action' | 'condition'
	// The action type the param belongs to (decides where {{secrets.NAME}} is accepted).
	actionType?: string
	// The type of the trigger the option belongs to; hints are looked up by owner (trigger or action type) and name.
	triggerType?: string
}>()

const emit = defineEmits<{
	update: [name: string, value: unknown]
}>()

const {t, te} = useI18n({useScope: 'global'})
const context = useAutomationContext()

const id = computed(() => pathToId(props.path))
const labelText = computed(() => catalogLabel(t, te, props.param.title_key, props.param.name))
const kind = computed(() => effectiveKind(props.param, props.values))
const messages = computed(() => context.errors.value.get(props.path) ?? [])

// kind "any" belongs to the custom field chosen in a sibling parameter.
const customField = computed(() => {
	if (kind.value !== 'any') return null
	const fieldParam = props.siblings.find(p => p.kind === 'custom_field')
	if (!fieldParam) return null
	const raw = props.values[fieldParam.name]
	const idValue = typeof raw === 'number' ? raw : typeof raw === 'string' ? customFieldIdOf(raw) : null
	return context.customFields.find(f => f.id === idValue) ?? null
})

const needsSibling = computed(() => {
	if (kind.value !== null) return null
	const by = props.param.kind_by
	const sibling = by ? props.siblings.find(p => p.name === by.param) : undefined
	return sibling ? catalogLabel(t, te, sibling.title_key, sibling.name) : null
})

const waitingForField = computed(() => kind.value === 'any' && !customField.value
	&& props.siblings.some(p => p.kind === 'custom_field'))

// The form.submitted trigger names a form by id; the project's forms are a better input than a number.
const formChoices = computed(() => context.forms && props.param.name === 'form_id' && kind.value === 'number'
	? context.forms.map(form => ({value: form.id, label: form.title}))
	: null)

// A schedule's cron expression gets presets and a check of its own.
const isCron = computed(() => props.triggerType === 'schedule' && props.param.name === 'cron' && kind.value === 'string')

// An explanation of the option or parameter, looked up by the type of its owner: automation.hint.<type>.<name>.
const hintKey = computed(() => `automation.hint.${(props.triggerType ?? props.actionType ?? '').replace(/\./g, '_')}.${props.param.name}`)

const isCondition = computed(() => kind.value === 'condition')
</script>

<template>
	<div
		class="param-field"
		:class="{'has-error': messages.length > 0}"
	>
		<component
			:is="isCondition ? 'span' : 'label'"
			:id="`${id}-label`"
			class="param-field__label"
			:for="isCondition ? undefined : id"
		>
			{{ labelText }}<span
				v-if="param.required"
				class="param-field__required"
				aria-hidden="true"
			> *</span>
			<span
				v-if="param.required"
				class="is-sr-only"
			>({{ $t('automation.param.required') }})</span>
		</component>

		<p
			v-if="needsSibling"
			:id="id"
			class="help"
			tabindex="-1"
		>
			{{ $t('automation.param.chooseFirst', {param: needsSibling}) }}
		</p>
		<p
			v-else-if="waitingForField"
			:id="id"
			class="help"
			tabindex="-1"
		>
			{{ $t('automation.param.chooseFieldFirst') }}
		</p>
		<AutomationSelect
			v-else-if="formChoices"
			:id="id"
			:model-value="values[param.name]"
			:options="formChoices"
			:label="labelText"
			:placeholder="$t('automation.param.choose')"
			:invalid="messages.length > 0"
			@update:modelValue="emit('update', param.name, $event)"
		/>
		<CronInput
			v-else-if="isCron"
			:id="id"
			:model-value="values[param.name]"
			:label="labelText"
			:invalid="messages.length > 0"
			@update:modelValue="emit('update', param.name, $event)"
		/>
		<ParamInput
			v-else-if="kind !== null"
			:id="id"
			:kind="kind"
			:multiple="param.multiple"
			:enum-values="param.enum"
			:model-value="values[param.name]"
			:label="labelText"
			:date-context="dateContext ?? 'action'"
			:required="param.required"
			:invalid="messages.length > 0"
			:custom-field="customField"
			:path="path"
			:allow-secrets="allowsSecretPlaceholder(actionType, param.name)"
			@update:modelValue="emit('update', param.name, $event)"
		/>

		<p
			v-if="$te(hintKey)"
			class="help"
		>
			{{ $t(hintKey) }}
		</p>
		<p
			v-for="message in messages"
			:key="message"
			class="help is-danger"
			role="alert"
		>
			{{ message }}
		</p>
	</div>
</template>

<style lang="scss" scoped>
.param-field {
	display: flex;
	flex-direction: column;
	gap: .3rem;
	min-inline-size: 0;

	&__label {
		font-weight: 600;
		font-size: .9rem;
	}

	&__required {
		color: var(--danger);
	}
}
</style>
