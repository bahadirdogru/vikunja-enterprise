<script setup lang="ts" generic="T extends string">
defineProps<{
	options: {value: T, label: string}[]
	label: string
}>()

const model = defineModel<T>({required: true})
</script>

<template>
	<div
		class="metric-toggle"
		role="group"
		:aria-label="label"
	>
		<button
			v-for="option in options"
			:key="option.value"
			type="button"
			class="metric-toggle__button"
			:class="{'is-active': model === option.value}"
			:aria-pressed="model === option.value"
			@click="model = option.value"
		>
			{{ option.label }}
		</button>
	</div>
</template>

<style lang="scss" scoped>
.metric-toggle {
	display: inline-flex;
	border: 1px solid var(--grey-200);
	border-radius: var(--input-radius);
	overflow: hidden;
}

.metric-toggle__button {
	padding: .15rem .55rem;
	border: 0;
	background: var(--scheme-main);
	color: var(--text-light);
	font-size: .75rem;
	cursor: pointer;

	& + & {
		border-inline-start: 1px solid var(--grey-200);
	}

	&.is-active {
		background: var(--primary);
		color: var(--white);
	}

	&:focus-visible {
		outline: 2px solid var(--primary);
		outline-offset: -2px;
	}
}
</style>
