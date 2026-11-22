<script setup lang="ts">
import {computed} from 'vue'

import BaseButton from '@/components/base/BaseButton.vue'
import ColorBubble from '@/components/misc/ColorBubble.vue'
import {asNumberArray, readableTextColor} from '@/helpers/customFields'
import type {CustomFieldOptionResponse} from '@/client/queries/customFields'

const props = defineProps<{
	options: CustomFieldOptionResponse[]
	modelValue: unknown
	labelledby: string
	disabled?: boolean
}>()

const emit = defineEmits<{
	commit: [value: number[]],
}>()

const selected = computed(() => asNumberArray(props.modelValue))

// Selected chips are filled with the option color; unselected ones stay outlined.
function chipStyle(option: CustomFieldOptionResponse) {
	if (!selected.value.includes(option.id) || !option.color) {
		return undefined
	}
	return {
		backgroundColor: option.color,
		borderColor: option.color,
		color: readableTextColor(option.color),
	}
}

function toggle(id: number) {
	if (props.disabled) {
		return
	}
	emit('commit', selected.value.includes(id)
		? selected.value.filter(existing => existing !== id)
		: [...selected.value, id])
}
</script>

<template>
	<div
		class="option-chips"
		role="group"
		:aria-labelledby="labelledby"
	>
		<BaseButton
			v-for="option in options"
			:key="option.id"
			class="tag option-chips__chip"
			:class="{'is-selected': selected.includes(option.id)}"
			:aria-pressed="selected.includes(option.id)"
			:style="chipStyle(option)"
			:aria-disabled="disabled || undefined"
			@click="toggle(option.id)"
		>
			<ColorBubble
				v-if="option.color && !selected.includes(option.id)"
				:color="option.color"
				aria-hidden="true"
			/>
			{{ option.title }}
			<Icon
				v-if="selected.includes(option.id)"
				icon="check"
				class="option-chips__check"
			/>
		</BaseButton>
	</div>
</template>

<style lang="scss" scoped>
.option-chips {
	display: flex;
	flex-wrap: wrap;
	gap: .375rem;
	padding-block: .25rem;

	&__chip {
		gap: .375rem;
		border: 1px solid var(--grey-300);
		background: transparent;
		color: var(--grey-600);

		&.is-selected {
			border-color: var(--primary);
			background: var(--primary);
			color: var(--primary-invert);
			font-weight: 600;
		}

		&[aria-disabled="true"] {
			cursor: default;
		}
	}

	&__check {
		inline-size: .625rem;
	}
}
</style>
