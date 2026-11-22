<script setup lang="ts">
import {computed} from 'vue'

import FormSelect from '@/components/input/FormSelect.vue'
import ColorBubble from '@/components/misc/ColorBubble.vue'
import type {CustomFieldOptionResponse} from '@/client/queries/customFields'

const props = defineProps<{
	options: CustomFieldOptionResponse[]
	modelValue: unknown
	labelledby: string
	disabled?: boolean
	required?: boolean
	error?: string | null
}>()

const emit = defineEmits<{
	commit: [value: number | null],
}>()

const selected = computed(() => typeof props.modelValue === 'number' ? props.modelValue : null)
const selectedOption = computed(() => props.options.find(option => option.id === selected.value))

function change(value: string | number) {
	emit('commit', value === '' ? null : Number(value))
}
</script>

<template>
	<div
		class="option-select"
		:class="{'is-unset': selected === null}"
	>
		<ColorBubble
			v-if="selectedOption?.color"
			:color="selectedOption.color"
			aria-hidden="true"
		/>
		<FormSelect
			:model-value="selected === null ? '' : selected"
			:disabled="disabled"
			flat
			:aria-labelledby="labelledby"
			:aria-required="required || undefined"
			:error="error"
			@update:modelValue="change"
		>
			<option value="">
				{{ $t('misc.notSet') }}
			</option>
			<option
				v-for="option in options"
				:key="option.id"
				:value="option.id"
			>
				{{ option.title }}
			</option>
		</FormSelect>
	</div>
</template>

<style lang="scss" scoped>
.option-select {
	display: flex;
	align-items: center;
	gap: .5rem;

	&.is-unset :deep(select) {
		color: var(--grey-600);
		font-style: italic;
	}
}
</style>
