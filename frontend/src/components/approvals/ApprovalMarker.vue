<script setup lang="ts">
defineProps<{
	count?: number
}>()
</script>

<template>
	<span
		v-tooltip="$t('approvals.marker.tooltip')"
		class="approval-marker"
		data-cy="approval-marker"
	>
		<Icon
			icon="hourglass-half"
			class="approval-marker__icon"
		/>
		{{ $t('approvals.marker.label') }}
		<span
			v-if="count && count > 1"
			class="approval-marker__count"
		>{{ count }}</span>
	</span>
</template>

<style lang="scss" scoped>
.approval-marker {
	display: inline-flex;
	align-items: center;
	gap: .25rem;
	max-inline-size: 100%;
	padding: .0625rem .5rem;
	background: var(--warning-light, var(--grey-100));
	border: 1px solid var(--warning, var(--grey-300));
	border-radius: 999px;
	color: var(--grey-800);
	font-size: .75rem;
	font-weight: 600;
	line-height: 1.4;
	white-space: nowrap;

	&__icon {
		font-size: .6875rem;
	}
}
</style>
