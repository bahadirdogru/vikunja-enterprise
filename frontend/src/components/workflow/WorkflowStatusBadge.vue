<script setup lang="ts">
import {computed} from 'vue'

import {categoryIcon, statusBadgeStyle, type WorkflowCategory} from '@/helpers/workflow'

const props = defineProps<{
	status: {title: string, color: string, category: WorkflowCategory}
}>()

const style = computed(() => statusBadgeStyle(props.status.color))
const hasColor = computed(() => Object.keys(style.value).length > 0)
</script>

<template>
	<span
		class="workflow-status-badge"
		:class="[`is-${status.category}`, {'has-color': hasColor}]"
		:style="style"
	>
		<Icon
			:icon="categoryIcon(status.category)"
			class="workflow-status-badge__icon"
		/>
		<span class="workflow-status-badge__title">{{ status.title }}</span>
	</span>
</template>

<style lang="scss" scoped>
.workflow-status-badge {
	display: inline-flex;
	align-items: center;
	gap: .375rem;
	max-inline-size: 100%;
	padding: .125rem .625rem;
	border-radius: 999px;
	background: var(--grey-200);
	color: var(--text);
	font-size: .875rem;
	font-weight: 600;
	line-height: 1.5;

	&__icon {
		flex-shrink: 0;
		font-size: .75rem;
	}

	&__title {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	&.is-done:not(.has-color) {
		background: var(--success);
		color: var(--success-invert);
	}

	&.is-cancelled:not(.has-color) {
		background: var(--grey-300);
		color: var(--grey-700);
	}
}
</style>
