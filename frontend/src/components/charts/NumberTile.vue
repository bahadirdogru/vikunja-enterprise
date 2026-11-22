<script setup lang="ts">
import {computed} from 'vue'

import {formatNumber, formatSeconds} from '@/helpers/charts'
import ChartState from './ChartState.vue'
import {useChartText} from './useChart'

const props = withDefaults(defineProps<{
	value: number
	unit?: string
	label?: string
	// The value of the period before, shows a delta when set.
	previous?: number | null
	loading?: boolean
	selectable?: boolean
	disabledReason?: string | null
}>(), {
	unit: 'tasks',
	label: undefined,
	previous: null,
	loading: false,
	selectable: true,
	disabledReason: null,
})

const emit = defineEmits<{
	select: []
}>()

const {t, locale, valueText} = useChartText()

const text = computed(() => props.unit === 'seconds' ? formatSeconds(props.value, locale.value) : formatNumber(props.value, locale.value))
const delta = computed(() => {
	if (props.previous === null || props.previous === undefined) {
		return null
	}
	const diff = props.value - props.previous
	const percent = props.previous === 0 ? null : (diff / Math.abs(props.previous)) * 100
	return {
		diff,
		direction: diff > 0 ? 'up' : diff < 0 ? 'down' : 'flat',
		text: percent === null
			? formatNumber(diff, locale.value, {signDisplay: 'exceptZero'})
			: formatNumber(percent, locale.value, {maximumFractionDigits: 0, signDisplay: 'exceptZero'}) + '%',
	}
})
const aria = computed(() => `${props.label ? props.label + ': ' : ''}${valueText(props.value, props.unit)}`)
const interactive = computed(() => props.selectable && !props.disabledReason)

function activate() {
	if (interactive.value) {
		emit('select')
	}
}
</script>

<template>
	<div class="number-tile">
		<ChartState
			v-if="loading"
			kind="loading"
		/>
		<component
			:is="selectable ? 'button' : 'div'"
			v-else
			class="number-tile__body"
			:class="{'is-selectable': interactive}"
			:type="selectable ? 'button' : undefined"
			:aria-label="aria"
			:aria-disabled="selectable && disabledReason ? 'true' : undefined"
			:title="disabledReason ?? undefined"
			@click="activate"
		>
			<span
				class="number-tile__value"
				aria-hidden="true"
			>{{ text }}</span>
			<span
				v-if="unit === 'tasks'"
				class="number-tile__unit"
				aria-hidden="true"
			>{{ t('charts.tasksUnit', Math.round(value)) }}</span>
			<span
				v-if="delta"
				class="number-tile__delta"
				:class="`is-${delta.direction}`"
			>
				<span aria-hidden="true">{{ delta.direction === 'up' ? '▲' : delta.direction === 'down' ? '▼' : '–' }}</span>
				{{ delta.text }}
				<span class="visually-hidden">{{ t('charts.deltaPrevious') }}</span>
			</span>
		</component>
	</div>
</template>

<style lang="scss" scoped>
.number-tile {
	container-type: size;
	display: flex;
	block-size: 100%;
	min-block-size: 4rem;
	align-items: center;
	justify-content: center;
}

.number-tile__body {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: .1rem;
	padding: 0;
	border: 0;
	border-radius: var(--input-radius);
	background: none;
	color: var(--text-strong);
	font: inherit;

	&.is-selectable {
		cursor: pointer;
		padding: .25rem 1rem;

		&:hover {
			background: var(--grey-100);
		}
	}
}

.number-tile__value {
	font-size: clamp(1.8rem, min(34cqh, 18cqw), 4.5rem);
	font-weight: 700;
	line-height: 1.05;
	font-variant-numeric: tabular-nums;
}

.number-tile__unit {
	color: var(--text-light);
	font-size: .85rem;
}

.number-tile__delta {
	font-size: .8rem;
	font-weight: 600;

	&.is-up {
		color: var(--success);
	}

	&.is-down {
		color: var(--danger);
	}

	&.is-flat {
		color: var(--text-light);
	}
}

.visually-hidden {
	position: absolute;
	inline-size: 1px;
	block-size: 1px;
	overflow: hidden;
	clip-path: inset(50%);
}
</style>
