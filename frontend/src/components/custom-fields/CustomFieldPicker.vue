<script setup lang="ts">
import FancyCheckbox from '@/components/input/FancyCheckbox.vue'
import type {CustomFieldResponse} from '@/client/queries/customFields'

defineProps<{
	fields: CustomFieldResponse[]
	isSelected: (fieldId: number) => boolean
}>()

const emit = defineEmits<{
	toggle: [fieldId: number, selected: boolean],
}>()
</script>

<template>
	<div class="custom-field-picker">
		<FancyCheckbox
			v-for="field in fields"
			:key="field.id"
			:model-value="isSelected(field.id)"
			@update:modelValue="selected => emit('toggle', field.id, selected)"
		>
			{{ field.title }}
		</FancyCheckbox>
	</div>
</template>

<style lang="scss" scoped>
.custom-field-picker {
	display: flex;
	flex-direction: column;
}
</style>
