<script setup lang="ts">
import {computed} from 'vue'

import type {TooltipState} from './useChart'

const props = defineProps<{
	tooltip: TooltipState | null
	width: number
	height: number
}>()

// Flip to the other side of the anchor near the edges so it never leaves the plot.
const style = computed(() => {
	if (!props.tooltip) {
		return {}
	}
	const {x, y} = props.tooltip
	const right = x > props.width * 0.6
	const below = y < props.height * 0.25
	return {
		insetInlineStart: `${x}px`,
		insetBlockStart: `${y}px`,
		transform: `translate(${right ? 'calc(-100% - 12px)' : '12px'}, ${below ? '12px' : 'calc(-100% - 8px)'})`,
	}
})
</script>

<template>
	<div
		v-if="tooltip"
		class="chart-tooltip"
		:style="style"
		aria-hidden="true"
	>
		<p class="chart-tooltip__title">
			{{ tooltip.title }}
		</p>
		<p
			v-for="(row, i) in tooltip.rows"
			:key="i"
			class="chart-tooltip__row"
		>
			<span
				v-if="row.color"
				class="chart-tooltip__swatch"
				:style="{background: row.color}"
			/>
			<span class="chart-tooltip__label">{{ row.label }}</span>
			<strong>{{ row.value }}</strong>
		</p>
		<p
			v-if="tooltip.note"
			class="chart-tooltip__note"
		>
			{{ tooltip.note }}
		</p>
	</div>
</template>

<style lang="scss" scoped>
.chart-tooltip {
	position: absolute;
	z-index: 5;
	max-inline-size: min(18rem, 90%);
	padding: .35rem .6rem;
	border: 1px solid var(--grey-200);
	border-radius: var(--input-radius);
	background: var(--scheme-main, #ffffff);
	box-shadow: var(--shadow-md);
	color: var(--text);
	font-size: .8rem;
	line-height: 1.35;
	pointer-events: none;
}

.chart-tooltip__title {
	margin: 0 0 .15rem;
	font-weight: 600;
	overflow-wrap: anywhere;
}

.chart-tooltip__row {
	display: flex;
	align-items: center;
	gap: .4rem;
	margin: 0;
}

.chart-tooltip__swatch {
	flex: none;
	inline-size: .6rem;
	block-size: .6rem;
	border-radius: 2px;
}

.chart-tooltip__label {
	flex: 1;
	min-inline-size: 0;
	overflow-wrap: anywhere;
}

.chart-tooltip__note {
	margin: .2rem 0 0;
	color: var(--text-light);
}
</style>
