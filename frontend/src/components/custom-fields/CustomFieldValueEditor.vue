<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'

import Datepicker from '@/components/input/Datepicker.vue'
import FormCheckbox from '@/components/input/FormCheckbox.vue'
import NumberValueInput from '@/components/custom-fields/NumberValueInput.vue'
import OptionChips from '@/components/custom-fields/OptionChips.vue'
import OptionSelect from '@/components/custom-fields/OptionSelect.vue'
import RatingInput from '@/components/custom-fields/RatingInput.vue'
import TextValueInput from '@/components/custom-fields/TextValueInput.vue'
import UserValueInput from '@/components/custom-fields/UserValueInput.vue'
import type {CustomFieldResponse} from '@/client/queries/customFields'
import {
	maxRatingOf,
	validateCustomFieldValue,
} from '@/helpers/customFields'

const props = defineProps<{
	field: CustomFieldResponse
	modelValue: unknown
	projectId: number
	// DOM id of the element that names this editor.
	labelledby: string
	disabled?: boolean
}>()

const emit = defineEmits<{
	'update:modelValue': [value: unknown],
}>()

const {t} = useI18n({useScope: 'global'})

const error = ref<string | null>(null)

watch(() => props.modelValue, () => {
	error.value = null
})

// Text and empty lists clear the value; a checkbox keeps an explicit false.
function toStored(value: unknown): unknown {
	if (value === '' || value === undefined) return null
	if (Array.isArray(value) && value.length === 0) return null
	return value
}

function isSame(a: unknown, b: unknown): boolean {
	return JSON.stringify(a ?? null) === JSON.stringify(b ?? null)
}

function commit(value: unknown) {
	if (props.disabled) {
		return
	}
	const stored = toStored(value)
	if (isSame(stored, props.modelValue)) {
		error.value = null
		return
	}
	const invalid = validateCustomFieldValue(props.field, stored)
	if (invalid) {
		error.value = t(invalid.key, invalid.params ?? {})
		return
	}
	error.value = null
	emit('update:modelValue', stored)
}

const dateModel = computed(() => typeof props.modelValue === 'string' ? props.modelValue : null)
const pendingDate = ref<Date | null>(null)

function setPendingDate(value: Date | null) {
	pendingDate.value = value
	// Clearing has no "close on change" for an already empty picker, so commit it right away.
	if (value === null) {
		commit(null)
	}
}

function commitDate() {
	if (pendingDate.value) {
		commit(pendingDate.value.toISOString())
	}
}
</script>

<template>
	<div class="custom-field-editor">
		<TextValueInput
			v-if="field.type === 'text' || field.type === 'long_text' || field.type === 'url' || field.type === 'email'"
			:type="field.type"
			:model-value="modelValue"
			:labelledby="labelledby"
			:disabled="disabled"
			:required="field.required"
			:error="error"
			@commit="commit"
		/>
		<NumberValueInput
			v-else-if="field.type === 'number' || field.type === 'currency' || field.type === 'progress'"
			:field="field"
			:model-value="modelValue"
			:labelledby="labelledby"
			:disabled="disabled"
			:required="field.required"
			:error="error"
			@commit="commit"
		/>
		<Datepicker
			v-else-if="field.type === 'date'"
			:model-value="dateModel"
			:aria-labelledby="labelledby"
			:choose-date-label="$t('customFields.value.chooseDate')"
			:empty-label="$t('misc.notSet')"
			:title="field.title"
			:disabled="disabled"
			@update:modelValue="setPendingDate"
			@closeOnChange="commitDate"
		/>
		<FormCheckbox
			v-else-if="field.type === 'checkbox'"
			:model-value="modelValue === true"
			:disabled="disabled"
			@update:modelValue="commit"
		>
			<span class="is-sr-only">{{ field.title }}</span>
		</FormCheckbox>
		<OptionSelect
			v-else-if="field.type === 'select'"
			:options="field.options"
			:model-value="modelValue"
			:labelledby="labelledby"
			:disabled="disabled"
			:required="field.required"
			:error="error"
			@commit="commit"
		/>
		<OptionChips
			v-else-if="field.type === 'multi_select'"
			:options="field.options"
			:model-value="modelValue"
			:labelledby="labelledby"
			:disabled="disabled"
			@commit="commit"
		/>
		<UserValueInput
			v-else-if="field.type === 'user'"
			:project-id="projectId"
			:model-value="modelValue"
			:labelledby="labelledby"
			:disabled="disabled"
			:error="error"
			@commit="commit"
		/>
		<RatingInput
			v-else-if="field.type === 'rating'"
			:model-value="modelValue"
			:max="maxRatingOf(field)"
			:labelledby="labelledby"
			:disabled="disabled"
			@commit="commit"
		/>
		<p
			v-if="error && (field.type === 'checkbox' || field.type === 'date' || field.type === 'multi_select' || field.type === 'rating')"
			class="help is-danger"
			role="alert"
		>
			{{ error }}
		</p>
		<p
			v-if="field.description"
			class="help custom-field-editor__description"
		>
			{{ field.description }}
		</p>
	</div>
</template>

<style lang="scss" scoped>
.custom-field-editor {
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: .25rem .5rem;
	inline-size: 100%;

	> :first-child {
		flex: 1 1 auto;
	}

	// Same quiet look as the built-in rows: plain text until hovered or focused.
	:deep(.input),
	:deep(.textarea) {
		border-color: transparent;
		background: transparent;
		box-shadow: none;
		padding-inline: .5rem;
		min-block-size: 2rem;
		transition: background-color $transition, border-color $transition;

		&::placeholder {
			color: var(--grey-600);
			font-style: italic;
			opacity: 1;
		}

		&:not([readonly]):hover {
			border-color: var(--grey-300);
			background: var(--scheme-main);
		}

		&:not([readonly]):focus {
			border-color: var(--primary);
			background: var(--scheme-main);
		}
	}

	:deep(.input:not(.input-wrapper)) {
		block-size: 2rem;
	}

	:deep(.multiselect:focus-within .input-wrapper) {
		border-color: var(--primary);
	}

	:deep(label.checkbox) {
		margin: 0;
		min-block-size: 2rem;
	}

	&__description {
		flex-basis: 100%;
		margin: 0;
		order: 1;
	}

}
</style>
