<script setup lang="ts">
// The editor of a field's default value, typed by the target it fills.
import {computed} from 'vue'

import CustomFieldValueEditor from '@/components/custom-fields/CustomFieldValueEditor.vue'
import {
	MAX_PRIORITY,
	isEmptyDefault,
	targetAsCustomField,
	type FieldDraft,
	type TargetInfo,
} from '@/helpers/forms'
import {inputToIso, isoToInput} from '@/helpers/formsPublic'

export type LabelChoice = {id: number, title: string, color: string}

const props = defineProps<{
	field: FieldDraft
	info: TargetInfo
	projectId: number
	labels: LabelChoice[]
	// DOM id of the control; error text is attached by the parent.
	controlId: string
	labelledby: string
	describedby?: string
	invalid?: boolean
}>()

const emit = defineEmits<{
	'update:modelValue': [value: unknown]
}>()

const target = computed(() => props.info.target)
const isCustom = computed(() => target.value.startsWith('cf_'))
const customField = computed(() => targetAsCustomField(props.info, props.projectId))

const text = computed(() => typeof props.field.default === 'string' ? props.field.default : '')
const dateValue = computed(() => text.value ? isoToInput(text.value, true) : '')
const priority = computed(() => typeof props.field.default === 'number' ? String(props.field.default) : '')
const selectedLabels = computed(() => Array.isArray(props.field.default) ? props.field.default as number[] : [])

// A shown labels field can only default to labels it offers.
const choices = computed(() => props.field.hidden
	? props.labels
	: props.labels.filter(label => props.field.options.includes(label.id)))

function setText(value: string) {
	emit('update:modelValue', value === '' ? null : value)
}

function setDate(value: string) {
	emit('update:modelValue', value === '' ? null : inputToIso(value))
}

function setPriority(value: string) {
	emit('update:modelValue', value === '' ? null : Number(value))
}

function toggleLabel(id: number, checked: boolean) {
	const next = checked ? [...selectedLabels.value, id] : selectedLabels.value.filter(item => item !== id)
	emit('update:modelValue', next.length === 0 ? null : next)
}

const priorityKeys: Record<number, string> = {1: 'low', 2: 'medium', 3: 'high', 4: 'urgent', 5: 'doNow'}
</script>

<template>
	<div class="fdi">
		<input
			v-if="target === 'title'"
			:id="controlId"
			class="input"
			type="text"
			:value="text"
			maxlength="1000"
			:aria-describedby="describedby"
			:aria-invalid="invalid || undefined"
			@input="setText(($event.target as HTMLInputElement).value)"
		>
		<textarea
			v-else-if="target === 'description'"
			:id="controlId"
			class="textarea"
			rows="3"
			:value="text"
			:aria-describedby="describedby"
			:aria-invalid="invalid || undefined"
			@input="setText(($event.target as HTMLTextAreaElement).value)"
		/>
		<input
			v-else-if="target === 'due_date'"
			:id="controlId"
			class="input"
			type="date"
			:value="dateValue"
			:aria-describedby="describedby"
			:aria-invalid="invalid || undefined"
			@input="setDate(($event.target as HTMLInputElement).value)"
		>
		<select
			v-else-if="target === 'priority'"
			:id="controlId"
			class="fdi__select"
			:value="priority"
			:aria-describedby="describedby"
			:aria-invalid="invalid || undefined"
			@change="setPriority(($event.target as HTMLSelectElement).value)"
		>
			<option value="">
				{{ $t('forms.builder.noDefault') }}
			</option>
			<option
				v-for="value in MAX_PRIORITY"
				:key="value"
				:value="String(value)"
			>
				{{ $t(`task.priority.${priorityKeys[value]}`) }}
			</option>
		</select>
		<div
			v-else-if="target === 'labels'"
			:id="controlId"
			class="fdi__labels"
			role="group"
			:aria-labelledby="labelledby"
			:aria-describedby="describedby"
		>
			<p
				v-if="choices.length === 0"
				class="help"
			>
				{{ field.hidden ? $t('forms.builder.noLabels') : $t('forms.builder.pickOptionsFirst') }}
			</p>
			<label
				v-for="label in choices"
				:key="label.id"
				class="fdi__label"
			>
				<input
					type="checkbox"
					:checked="selectedLabels.includes(label.id)"
					@change="toggleLabel(label.id, ($event.target as HTMLInputElement).checked)"
				>
				<span
					class="fdi__chip"
					:style="label.color ? {background: `#${label.color.replace('#', '')}`} : {}"
				/>
				{{ label.title }}
			</label>
		</div>
		<CustomFieldValueEditor
			v-else-if="isCustom"
			:id="controlId"
			:field="customField"
			:model-value="isEmptyDefault(field.default) ? null : field.default"
			:project-id="projectId"
			:labelledby="labelledby"
			@update:modelValue="emit('update:modelValue', $event)"
		/>
	</div>
</template>

<style lang="scss" scoped>
.fdi {
	inline-size: 100%;
	min-inline-size: 0;

	&__select {
		background: var(--input-background, var(--white));
		color: var(--input-color, var(--text));
		border: 1px solid var(--input-border-color, var(--grey-300));
		border-radius: $radius;
		padding: .5rem .75rem;
		font: inherit;
		min-block-size: 2.5rem;
		inline-size: 100%;
	}

	&__labels {
		display: flex;
		flex-wrap: wrap;
		gap: .25rem 1rem;
	}

	&__label {
		display: inline-flex;
		align-items: center;
		gap: .4rem;
		min-block-size: 2rem;
	}

	&__chip {
		inline-size: .75rem;
		block-size: .75rem;
		border-radius: 50%;
		background: var(--grey-400);
	}
}

// The editor of custom fields is made for rows of plain text; here it needs a visible edge.
.fdi :deep(.custom-field-editor) {
	.input,
	.textarea {
		border-color: var(--grey-300);
		background: var(--scheme-main);
	}
}
</style>
