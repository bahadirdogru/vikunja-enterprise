<script setup lang="ts">
import {computed, ref, watch} from 'vue'

import FormInput from '@/components/input/FormInput.vue'
import ProgressBar from '@/components/misc/ProgressBar.vue'
import {
	numberStep,
	parseNumberInput,
	roundToPrecision,
	type FieldLike,
} from '@/helpers/customFields'

const props = defineProps<{
	field: FieldLike
	modelValue: unknown
	labelledby: string
	disabled?: boolean
	required?: boolean
	error?: string | null
}>()

const emit = defineEmits<{
	commit: [value: number | null],
}>()

const text = ref('')

watch(
	() => props.modelValue,
	value => {
		text.value = typeof value === 'number' ? String(value) : ''
	},
	{immediate: true},
)

const bounds = computed(() => props.field.type === 'progress'
	? {min: 0, max: 100}
	: {min: props.field.config?.min ?? undefined, max: props.field.config?.max ?? undefined})

const progressValue = computed(() => {
	const parsed = parseNumberInput(text.value)
	return parsed === null || Number.isNaN(parsed) ? 0 : Math.min(100, Math.max(0, parsed))
})

function commit() {
	if (props.disabled) {
		return
	}
	const parsed = parseNumberInput(text.value)
	// NaN goes up as is so the editor reports it as invalid.
	emit('commit', parsed === null || Number.isNaN(parsed)
		? parsed
		: roundToPrecision(parsed, props.field.config?.precision ?? undefined))
}
</script>

<template>
	<div class="number-value">
		<div
			class="number-value__row"
			:class="{'has-unit': field.type !== 'number'}"
		>
			<FormInput
				v-model="text"
				type="number"
				inputmode="decimal"
				:step="numberStep(field)"
				:min="bounds.min"
				:max="bounds.max"
				:readonly="disabled"
				:aria-labelledby="labelledby"
				:placeholder="$t('misc.notSet')"
				:aria-required="required || undefined"
				:error="error"
				@blur="commit"
				@keydown.enter.prevent="commit"
			/>
			<span
				v-if="field.type === 'currency' && field.config?.currency"
				class="number-value__unit"
			>
				{{ field.config.currency }}
			</span>
			<span
				v-else-if="field.type === 'progress'"
				class="number-value__unit"
				aria-hidden="true"
			>
				%
			</span>
		</div>
		<ProgressBar
			v-if="field.type === 'progress' && text.trim() !== ''"
			:value="progressValue"
			is-small
			is-primary
			aria-hidden="true"
		/>
	</div>
</template>

<style lang="scss" scoped>
.number-value {
	display: flex;
	flex-direction: column;
	gap: .125rem;
	inline-size: 100%;

	&__row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .25rem;

		> :first-child {
			flex: 1 1 0;
			min-inline-size: 6rem;
		}

		// FormInput renders its error as a sibling; keep it under the row.
		:deep(.help) {
			order: 3;
			flex-basis: 100%;
			margin: 0;
		}

		// The unit sits right next to a narrower input.
		&.has-unit > :first-child {
			flex: 0 1 8rem;
		}
	}

	&__unit {
		flex: none;
		order: 2;
		color: var(--grey-600);
		font-weight: 500;
	}
}
</style>
