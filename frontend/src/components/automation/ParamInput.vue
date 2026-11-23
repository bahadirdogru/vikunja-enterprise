<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
// Renders the input for one value of one catalog kind. It knows no action, trigger or field by name.
import {computed, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'

import AutomationSelect, {type SelectChoice} from '@/components/automation/AutomationSelect.vue'
import CodeEditor from '@/components/automation/CodeEditor.vue'
import ConditionBuilder from '@/components/automation/ConditionBuilder.vue'
import KeyValueInput from '@/components/automation/KeyValueInput.vue'
import SecretForm from '@/components/automation/SecretForm.vue'
import {placeholderLabel, useAutomationContext} from '@/components/automation/context'
import CustomFieldValueEditor from '@/components/custom-fields/CustomFieldValueEditor.vue'
import type {CustomFieldResponse} from '@/client/queries/customFields'
import {useLabels} from '@/composables/useLabels'
import {useProjects} from '@/composables/useProjects'
import {useProjectUserSearch} from '@/composables/useUserSearch'
import {useTeamSearch} from '@/composables/useTeams'
import {enumLabel, groupKind, type ConditionNode} from '@/helpers/automation'
import {secretPlaceholder} from '@/helpers/automationScript'
import {dateSuggestions, isValidDateSpec, isValidOffset} from '@/helpers/automationValues'
import {getDisplayName} from '@/helpers/user'

const props = withDefaults(defineProps<{
	kind: string
	modelValue: unknown
	id: string
	label: string
	multiple?: boolean
	// Keywords of user kinds, values of enum kinds.
	enumValues?: string[]
	// Overrides the choices of an enum (custom field options).
	choices?: SelectChoice[]
	// Dates in actions accept +3d and "none"; in conditions only now, now+3d and absolute dates.
	dateContext?: 'action' | 'condition'
	invalid?: boolean
	required?: boolean
	// For kind "any": the custom field the value belongs to.
	customField?: CustomFieldResponse | null
	// Path used for the ids of nested inputs (embedded conditions).
	path?: string
	// {{secrets.NAME}} may be used in this value (see allowsSecretPlaceholder).
	allowSecrets?: boolean
	// The name of the catalog option or param, which keys the label of an enum value.
	option?: string
}>(), {
	multiple: false,
	enumValues: () => [],
	choices: undefined,
	dateContext: 'action',
	invalid: false,
	required: false,
	customField: null,
	path: '',
	allowSecrets: false,
	option: undefined,
})

const emit = defineEmits<{
	'update:modelValue': [value: unknown]
}>()

const {t, te} = useI18n({useScope: 'global'})
const context = useAutomationContext()

const enumText = (value: string) => enumLabel(t, te, value, props.option)

// Members of the project. The first page is enough for the 10-20 person teams this is made for.
const memberSearch = useProjectUserSearch(() => context.projectId, '', () => props.kind === 'user' || props.kind === 'users')
const teamSearch = useTeamSearch({search: '', enabled: () => props.kind === 'team'})
const {labels} = useLabels()
const {projectsArray} = useProjects()

const emptyLabel = computed(() => t('automation.param.choose'))

const listItems = computed(() => Array.isArray(props.modelValue) ? props.modelValue as unknown[] : [])
const isList = computed(() => props.multiple && props.kind !== 'enum' && props.kind !== 'condition' && props.kind !== 'map')

function setList(items: unknown[]) {
	emit('update:modelValue', items)
}

function setItem(index: number, value: unknown) {
	const next = [...listItems.value]
	next[index] = value
	setList(next)
}

function addItem() {
	setList([...listItems.value, null])
}

function removeItem(index: number) {
	setList(listItems.value.filter((_, i) => i !== index))
}

const enumChoices = computed<SelectChoice[]>(() => props.choices
	?? props.enumValues.map(value => ({value, label: enumText(value)})))

const userChoices = computed<SelectChoice[]>(() => [
	...props.enumValues.map(value => ({value, label: enumText(value), group: t('automation.param.special')})),
	...memberSearch.users.value
		.filter(user => user.id !== undefined)
		.map(user => ({value: user.id as number, label: getDisplayName(user), group: t('automation.param.members')})),
])

const teamChoices = computed<SelectChoice[]>(() => teamSearch.teams.value
	.filter(team => team.id !== undefined)
	.map(team => ({value: team.id as number, label: team.name ?? `#${team.id}`})))

const labelChoices = computed<SelectChoice[]>(() => labels.value
	.filter(l => l.id !== undefined)
	.map(l => ({value: l.id as number, label: l.title ?? `#${l.id}`})))

const projectChoices = computed<SelectChoice[]>(() => projectsArray
	.filter(project => project.id > 0 && !project.is_archived)
	.map(project => ({value: project.id, label: project.title})))

const statusChoices = computed<SelectChoice[]>(() => context.statuses.map(status => ({
	value: status.id,
	label: `${status.title} (${t(`workflow.categories.${status.category}`)})`,
})))

const customFieldChoices = computed<SelectChoice[]>(() => context.customFields.map(field => ({value: field.id, label: field.title})))

const secretNames = computed(() => context.secrets ?? [])
// The empty entry of the select is "choose", the last one opens the inline form for a secret that does not exist yet.
const NEW_SECRET = '\u0000new-secret'
const secretChoices = computed<SelectChoice[]>(() => [
	...secretNames.value.map(name => ({value: name, label: name})),
	{value: NEW_SECRET, label: t('automation.secrets.newOption')},
])
const creatingSecret = ref(false)

function onSecretChoice(value: string | number | boolean | null) {
	if (value === NEW_SECRET) {
		creatingSecret.value = true
		return
	}
	creatingSecret.value = false
	emit('update:modelValue', value)
}

function onSecretCreated(name: string) {
	creatingSecret.value = false
	emit('update:modelValue', name)
}

// Scripts can be read but not changed when the instance turned them off.
const scriptsReadonly = computed(() => !context.catalog.supportsScript)
const scriptPosition = computed(() => context.lines?.value.get(props.path) ?? null)

const placeholders = computed<{value: string, label: string, group?: string}[]>(() => [
	...context.catalog.placeholders.flatMap(p => placeholderLabel(p, context.customFields)),
	...(props.allowSecrets && props.kind === 'template' ? secretNames.value.map(name => ({value: secretPlaceholder(name), label: secretPlaceholder(name), group: t('automation.secrets.title')})) : []),
])

// ---- text-like inputs ----------------------------------------------------------------------------------------
const textValue = computed(() => typeof props.modelValue === 'string' ? props.modelValue : props.modelValue == null ? '' : String(props.modelValue))
const textarea = ref<HTMLTextAreaElement | HTMLInputElement | null>(null)

function insertPlaceholder(value: string | number | boolean | null) {
	if (typeof value !== 'string') return
	const el = textarea.value
	const text = `{{${value}}}`
	const current = textValue.value
	const start = el?.selectionStart ?? current.length
	const end = el?.selectionEnd ?? current.length
	emit('update:modelValue', current.slice(0, start) + text + current.slice(end))
	requestAnimationFrame(() => {
		el?.focus()
		el?.setSelectionRange(start + text.length, start + text.length)
	})
}

function onNumber(event: Event) {
	const raw = (event.target as HTMLInputElement).value
	emit('update:modelValue', raw === '' ? null : Number(raw))
}

// Free values of kind "any": numbers and booleans keep their type.
function parseLoose(raw: string): unknown {
	const s = raw.trim()
	if (s === 'true') return true
	if (s === 'false') return false
	if (s !== '' && /^-?\d+(\.\d+)?$/.test(s)) return Number(s)
	return raw
}

const dateOk = computed(() => {
	const value = textValue.value
	return value === '' || isValidDateSpec(value, props.dateContext === 'action')
})
const durationOk = computed(() => textValue.value === '' || isValidOffset(textValue.value))
const suggestions = computed(() => dateSuggestions(props.dateContext === 'action'))
const datalistId = computed(() => `${props.id}-suggestions`)

// ---- unknown kinds: JSON ------------------------------------------------------------------------------------
// eslint-disable-next-line vue/no-setup-props-reactivity-loss -- seeded once, re-synced by the watcher below
const jsonText = ref(props.modelValue === undefined || props.modelValue === null ? '' : JSON.stringify(props.modelValue, null, 2))
const jsonError = ref(false)
watch(() => props.modelValue, value => {
	let parsed: unknown
	try {
		parsed = jsonText.value.trim() === '' ? undefined : JSON.parse(jsonText.value)
	} catch {
		return
	}
	if (JSON.stringify(parsed) !== JSON.stringify(value ?? undefined)) {
		jsonText.value = value === undefined || value === null ? '' : JSON.stringify(value, null, 2)
	}
})

function onJson(event: Event) {
	jsonText.value = (event.target as HTMLTextAreaElement).value
	if (jsonText.value.trim() === '') {
		jsonError.value = false
		emit('update:modelValue', null)
		return
	}
	try {
		emit('update:modelValue', JSON.parse(jsonText.value))
		jsonError.value = false
	} catch {
		jsonError.value = true
	}
}

const KNOWN = new Set([
	'string', 'text', 'template', 'number', 'bool', 'date', 'duration', 'user', 'users', 'label', 'status',
	'custom_field', 'project', 'team', 'enum', 'condition', 'code', 'secret', 'map', 'any',
])
const isKnown = computed(() => KNOWN.has(props.kind))

const conditionValue = computed<ConditionNode | null>(() => {
	const value = props.modelValue
	if (!value || typeof value !== 'object' || Array.isArray(value)) return null
	const node = value as ConditionNode
	// A bare comparison is shown as a group of one, so more can be added next to it.
	return groupKind(node) ? node : {all: [node]}
})

function toggleEnum(value: string, checked: boolean) {
	const current = listItems.value as string[]
	emit('update:modelValue', checked ? [...current, value] : current.filter(v => v !== value))
}
</script>

<template>
	<div class="param-input">
		<!-- lists of values of one kind -->
		<div
			v-if="isList"
			:id="id"
			class="param-input__list"
			role="group"
			:aria-label="label"
		>
			<div
				v-for="(item, index) in listItems"
				:key="index"
				class="param-input__list-item"
			>
				<ParamInput
					:id="`${id}-${index}`"
					:kind="kind"
					:model-value="item"
					:label="`${label} ${index + 1}`"
					:enum-values="enumValues"
					:choices="choices"
					:date-context="dateContext"
					:invalid="invalid"
					@update:modelValue="setItem(index, $event)"
				/>
				<XButton
					type="button"
					:aria-label="$t('automation.param.removeItem', {n: index + 1})"
					variant="secondary"
					icon="xmark"
					danger
					@click="removeItem(index)"
				/>
			</div>
			<XButton
				type="button"
				variant="secondary"
				icon="plus"
				@click="addItem"
			>
				{{ $t('automation.param.addItem') }}
			</XButton>
		</div>

		<!-- enum -->
		<div
			v-else-if="kind === 'enum' && multiple"
			:id="id"
			class="param-input__checks"
			role="group"
			:aria-label="label"
		>
			<label
				v-for="choice in enumChoices"
				:key="String(choice.value)"
				class="checkbox"
			>
				<input
					type="checkbox"
					:checked="listItems.includes(choice.value)"
					@change="toggleEnum(String(choice.value), ($event.target as HTMLInputElement).checked)"
				>
				{{ choice.label }}
			</label>
		</div>
		<AutomationSelect
			v-else-if="kind === 'enum'"
			:id="id"
			:model-value="modelValue"
			:options="enumChoices"
			:label="label"
			:placeholder="emptyLabel"
			:invalid="invalid"
			@update:modelValue="emit('update:modelValue', $event)"
		/>

		<!-- text -->
		<textarea
			v-else-if="kind === 'text' || kind === 'template'"
			:id="id"
			ref="textarea"
			class="textarea"
			rows="3"
			:value="textValue"
			:aria-label="label"
			:aria-invalid="invalid ? true : undefined"
			@input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
		/>
		<input
			v-else-if="kind === 'string'"
			:id="id"
			ref="textarea"
			class="input"
			type="text"
			:value="textValue"
			:aria-label="label"
			:aria-invalid="invalid ? true : undefined"
			@input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
		>

		<!-- number -->
		<input
			v-else-if="kind === 'number'"
			:id="id"
			class="input"
			type="number"
			step="any"
			:value="modelValue === null || modelValue === undefined ? '' : String(modelValue)"
			:aria-label="label"
			:aria-invalid="invalid ? true : undefined"
			@input="onNumber"
		>

		<!-- bool -->
		<AutomationSelect
			v-else-if="kind === 'bool'"
			:id="id"
			:model-value="modelValue"
			:options="[{value: true, label: $t('automation.param.yes')}, {value: false, label: $t('automation.param.no')}]"
			:label="label"
			:placeholder="required ? undefined : emptyLabel"
			:invalid="invalid"
			@update:modelValue="emit('update:modelValue', $event)"
		/>

		<!-- date -->
		<template v-else-if="kind === 'date'">
			<input
				:id="id"
				class="input"
				:class="{'is-danger': !dateOk || invalid}"
				type="text"
				autocomplete="off"
				:list="datalistId"
				:value="textValue"
				:aria-label="label"
				:aria-invalid="!dateOk || invalid ? true : undefined"
				:placeholder="dateContext === 'action' ? '+3d' : 'now+3d'"
				@input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
			>
			<datalist :id="datalistId">
				<option
					v-for="s in suggestions"
					:key="s.value"
					:value="s.value"
				>
					{{ $t(s.labelKey) }}
				</option>
			</datalist>
			<p
				class="help"
				:class="{'is-danger': !dateOk}"
			>
				{{ dateContext === 'action' ? $t('automation.date.helpAction') : $t('automation.date.helpCondition') }}
			</p>
		</template>

		<!-- duration -->
		<template v-else-if="kind === 'duration'">
			<input
				:id="id"
				class="input"
				:class="{'is-danger': !durationOk || invalid}"
				type="text"
				autocomplete="off"
				:value="textValue"
				:aria-label="label"
				:aria-invalid="!durationOk || invalid ? true : undefined"
				placeholder="3d"
				@input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
			>
			<p
				class="help"
				:class="{'is-danger': !durationOk}"
			>
				{{ $t('automation.duration.help') }}
			</p>
		</template>

		<!-- references -->
		<AutomationSelect
			v-else-if="kind === 'user' || kind === 'users'"
			:id="id"
			:model-value="modelValue"
			:options="userChoices"
			:label="label"
			:placeholder="emptyLabel"
			:invalid="invalid"
			@update:modelValue="emit('update:modelValue', $event)"
		/>
		<AutomationSelect
			v-else-if="kind === 'label'"
			:id="id"
			:model-value="modelValue"
			:options="labelChoices"
			:label="label"
			:placeholder="emptyLabel"
			:invalid="invalid"
			@update:modelValue="emit('update:modelValue', $event)"
		/>
		<AutomationSelect
			v-else-if="kind === 'team'"
			:id="id"
			:model-value="modelValue"
			:options="teamChoices"
			:label="label"
			:placeholder="emptyLabel"
			:invalid="invalid"
			@update:modelValue="emit('update:modelValue', $event)"
		/>
		<AutomationSelect
			v-else-if="kind === 'project'"
			:id="id"
			:model-value="modelValue"
			:options="projectChoices"
			:label="label"
			:placeholder="emptyLabel"
			:invalid="invalid"
			@update:modelValue="emit('update:modelValue', $event)"
		/>
		<template v-else-if="kind === 'status'">
			<AutomationSelect
				v-if="statusChoices.length > 0"
				:id="id"
				:model-value="modelValue"
				:options="statusChoices"
				:aria-label="label"
				:placeholder="emptyLabel"
				:invalid="invalid"
				@update:modelValue="emit('update:modelValue', $event)"
			/>
			<p
				v-else
				:id="id"
				class="help"
				tabindex="-1"
			>
				{{ $t('automation.param.noWorkflow') }}
			</p>
		</template>
		<AutomationSelect
			v-else-if="kind === 'custom_field'"
			:id="id"
			:model-value="modelValue"
			:options="customFieldChoices"
			:label="label"
			:placeholder="emptyLabel"
			:invalid="invalid"
			@update:modelValue="emit('update:modelValue', $event)"
		/>

		<!-- condition (flow.stop_if) -->
		<ConditionBuilder
			v-else-if="kind === 'condition'"
			:model-value="conditionValue"
			:path="path || id"
			@update:modelValue="emit('update:modelValue', $event)"
		/>

		<!-- scripting kinds -->
		<template v-else-if="kind === 'code'">
			<CodeEditor
				:id="id"
				:model-value="textValue"
				:label="label"
				mode="action"
				:invalid="invalid"
				:readonly="scriptsReadonly"
				:error-position="scriptPosition"
				@update:modelValue="emit('update:modelValue', $event)"
			/>
			<p
				v-if="scriptsReadonly"
				class="help is-danger"
				role="status"
			>
				{{ $t('automation.code.disabledNote') }}
			</p>
		</template>
		<template v-else-if="kind === 'secret'">
			<AutomationSelect
				v-if="context.secrets !== null"
				:id="id"
				:model-value="modelValue"
				:options="secretChoices"
				:label="label"
				:placeholder="emptyLabel"
				:invalid="invalid"
				@update:modelValue="onSecretChoice"
			/>
			<SecretForm
				v-if="context.secrets !== null && creatingSecret"
				:project-id="context.projectId"
				:existing="secretNames"
				:id-prefix="`${id}-new-secret`"
				@saved="onSecretCreated"
				@cancel="creatingSecret = false"
			/>
			<input
				v-else-if="context.secrets === null"
				
				:id="id"
				class="input"
				type="text"
				autocomplete="off"
				:value="textValue"
				:aria-label="label"
				:aria-invalid="invalid ? true : undefined"
				@input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
			>
		</template>
		<KeyValueInput
			v-else-if="kind === 'map'"
			:id="id"
			:model-value="modelValue"
			:label="label"
			:secrets="allowSecrets ? secretNames : []"
			@update:modelValue="emit('update:modelValue', $event)"
		/>

		<!-- value of a custom field / loose value -->
		<CustomFieldValueEditor
			v-else-if="kind === 'any' && customField"
			:field="customField"
			:model-value="modelValue"
			:project-id="context.projectId"
			:labelledby="`${id}-label`"
			@update:modelValue="emit('update:modelValue', $event)"
		/>
		<input
			v-else-if="kind === 'any'"
			:id="id"
			class="input"
			type="text"
			autocomplete="off"
			:value="textValue"
			:aria-label="label"
			:aria-invalid="invalid ? true : undefined"
			@input="emit('update:modelValue', parseLoose(($event.target as HTMLInputElement).value))"
		>

		<!-- anything the backend adds later -->
		<div
			v-else-if="!isKnown"
			class="param-input__unknown"
		>
			<textarea
				:id="id"
				class="textarea code-font"
				rows="3"
				:value="jsonText"
				:aria-label="label"
				:aria-invalid="jsonError || invalid ? true : undefined"
				@input="onJson"
			/>
			<p
				class="help"
				:class="{'is-danger': jsonError}"
			>
				{{ jsonError ? $t('automation.param.invalidJson') : $t('automation.param.unknownKind', {kind}) }}
			</p>
		</div>

		<AutomationSelect
			v-if="kind === 'template' && placeholders.length > 0"
			class="param-input__placeholders"
			:model-value="null"
			:options="placeholders.map(p => ({value: p.value, label: p.label, group: p.group}))"
			:aria-label="$t('automation.param.insertPlaceholder')"
			:placeholder="$t('automation.param.insertPlaceholder')"
			@update:modelValue="insertPlaceholder"
		/>
	</div>
</template>

<style lang="scss" scoped>
.param-input {
	display: flex;
	flex-direction: column;
	gap: .35rem;
	align-items: flex-start;
	min-inline-size: 0;
	inline-size: 100%;

	> .input,
	> .textarea,
	> .select {
		inline-size: 100%;
	}

	&__list,
	&__checks {
		display: flex;
		flex-direction: column;
		gap: .35rem;
		align-items: flex-start;
		inline-size: 100%;
	}

	&__checks {
		flex-flow: row wrap;
		gap: .35rem 1rem;
	}

	&__list-item {
		display: flex;
		gap: .4rem;
		align-items: center;
		inline-size: 100%;

		:deep(.param-input) {
			flex: 1 1 auto;
		}
	}

	&__unknown {
		inline-size: 100%;
	}

	.code-font {
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: .85rem;
	}

	.help {
		margin-block-start: 0;
	}

	&__placeholders {
		font-size: .85rem;
	}
}
</style>
