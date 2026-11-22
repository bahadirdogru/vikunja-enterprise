<script setup lang="ts">
defineProps<{
	variant?: 'success' | 'danger' | 'warning' | 'info' | 'neutral'
}>()
</script>

<template>
	<span
		class="automation-badge"
		:class="`is-${variant ?? 'neutral'}`"
	><slot /></span>
</template>

<style lang="scss" scoped>
.automation-badge {
	display: inline-flex;
	align-items: center;
	gap: .3rem;
	padding: .1rem .6rem;
	border-radius: 999px;
	font-size: .8rem;
	font-weight: 600;
	line-height: 1.5;
	white-space: nowrap;
	background: var(--grey-200);
	color: var(--text);

	&.is-success {
		background: var(--success);
		color: var(--success-invert);
	}

	&.is-danger {
		background: var(--danger);
		color: var(--danger-invert);
	}

	&.is-warning {
		background: var(--warning);
		color: var(--warning-invert);
	}

	&.is-info {
		background: var(--info);
		color: var(--info-invert);
	}
}
</style>
