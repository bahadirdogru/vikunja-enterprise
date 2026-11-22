<script setup lang="ts">
import {truncateLabel} from '@/helpers/charts'

export type LegendItem = {
	key: string
	label: string
	color: string
	// Already formatted.
	value?: string
	selectable?: boolean
}

defineProps<{
	items: LegendItem[]
	label?: string
	vertical?: boolean
}>()

const emit = defineEmits<{
	select: [key: string]
	hover: [key: string | null]
}>()
</script>

<template>
	<ul
		class="chart-legend"
		:class="{'is-vertical': vertical}"
		:aria-label="label"
	>
		<li
			v-for="item in items"
			:key="item.key"
			class="chart-legend__item"
			@mouseenter="emit('hover', item.key)"
			@mouseleave="emit('hover', null)"
		>
			<component
				:is="item.selectable ? 'button' : 'span'"
				class="chart-legend__entry"
				:class="{'is-button': item.selectable}"
				:type="item.selectable ? 'button' : undefined"
				:title="item.label"
				@click="item.selectable && emit('select', item.key)"
				@focus="emit('hover', item.key)"
				@blur="emit('hover', null)"
			>
				<span
					class="chart-legend__swatch"
					:style="{background: item.color}"
				/>
				<span class="chart-legend__label">{{ truncateLabel(item.label, 28) }}</span>
				<span
					v-if="item.value"
					class="chart-legend__value"
				>{{ item.value }}</span>
			</component>
		</li>
	</ul>
</template>

<style lang="scss" scoped>
.chart-legend {
	display: flex;
	flex-wrap: wrap;
	gap: .15rem .9rem;
	margin: 0;
	padding: 0;
	list-style: none;
	font-size: .8rem;
	color: var(--text);

	&.is-vertical {
		flex-direction: column;
		flex-wrap: nowrap;
		gap: .15rem;
		min-inline-size: 0;
		overflow-y: auto;
	}
}

.chart-legend__entry {
	display: inline-flex;
	align-items: center;
	gap: .4rem;
	max-inline-size: 100%;
	padding: .1rem .25rem;
	border: 0;
	border-radius: var(--input-radius);
	background: none;
	color: inherit;
	font: inherit;

	&.is-button {
		cursor: pointer;

		&:hover {
			background: var(--grey-100);
		}
	}
}

.chart-legend__swatch {
	flex: none;
	inline-size: .65rem;
	block-size: .65rem;
	border-radius: 2px;
}

.chart-legend__label {
	min-inline-size: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.chart-legend__value {
	margin-inline-start: auto;
	padding-inline-start: .5rem;
	font-variant-numeric: tabular-nums;
	color: var(--text-light);
}
</style>
