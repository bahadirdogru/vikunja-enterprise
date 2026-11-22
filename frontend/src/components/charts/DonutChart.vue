<script setup lang="ts">
import {computed, ref, useId} from 'vue'

import {donutArcs, formatNumber, assignColors, groupKeyId, groupLabel, type ChartGroup, type ChartSelection} from '@/helpers/charts'
import ChartLegend, {type LegendItem} from './ChartLegend.vue'
import ChartState from './ChartState.vue'
import ChartTable from './ChartTable.vue'
import ChartTooltip from './ChartTooltip.vue'
import {useChartSize, useChartText, type TooltipState} from './useChart'

const props = withDefaults(defineProps<{
	groups: ChartGroup[]
	unit?: string
	loading?: boolean
	multiValued?: boolean
	selectable?: boolean
	showLegend?: boolean
	title?: string
	// The figure in the hole, usually the number of distinct tasks.
	total?: number
	totalLabel?: string
	disabledReason?: (selection: ChartSelection) => string | null
}>(), {
	unit: 'tasks',
	loading: false,
	multiValued: false,
	selectable: true,
	showLegend: true,
	title: undefined,
	total: undefined,
	totalLabel: undefined,
	disabledReason: undefined,
})

const emit = defineEmits<{
	select: [selection: ChartSelection]
}>()

const {t, locale, valueText, ariaLabel} = useChartText()
const uid = useId()
const root = ref<HTMLElement | null>(null)
const size = useChartSize(root, {width: 320, height: 300})
const tooltip = ref<TooltipState | null>(null)
const activeIndex = ref<number | null>(null)
const plot = ref<HTMLElement | null>(null)

const lbl = (group: ChartGroup) => groupLabel(group, t)
const colors = computed(() => assignColors(props.groups))
const sum = computed(() => props.groups.reduce((s, g) => s + Math.max(0, g.value), 0))
// Beside the legend when there is room, above it otherwise.
const sideBySide = computed(() => size.value.width >= 440 && size.value.width > size.value.height * 1.2)
const diameter = computed(() => {
	const {width, height} = size.value
	const legendHeight = props.showLegend && !sideBySide.value ? Math.min(height * 0.4, 28 + props.groups.length * 20) : 0
	const noteHeight = props.multiValued ? 18 : 0
	const available = sideBySide.value ? Math.min(height - noteHeight, width * 0.5) : Math.min(width, height - legendHeight - noteHeight)
	return Math.max(110, Math.floor(available) - 8)
})
const arcs = computed(() => {
	const r = diameter.value / 2
	return donutArcs(props.groups.map(g => g.value), r, r, r - 2, r * 0.62)
})

const slices = computed(() => arcs.value.map(arc => {
	const group = props.groups[arc.index]!
	const percent = formatNumber(arc.fraction * 100, locale.value, {maximumFractionDigits: 1})
	return {
		id: `${uid}-${arc.index}`,
		arc,
		group,
		color: colors.value[arc.index]!,
		percent,
		aria: `${ariaLabel(lbl(group), group.value, props.unit)} (${percent}%)`,
		reason: props.disabledReason?.({group}) ?? null,
	}
}))

const legendItems = computed<LegendItem[]>(() => props.groups.map((group, i) => ({
	key: groupKeyId(group),
	label: lbl(group),
	color: colors.value[i]!,
	value: valueText(group.value, props.unit),
	selectable: props.selectable && !props.disabledReason?.({group}),
})))

function showTooltip(index: number, event: PointerEvent | FocusEvent) {
	const slice = slices.value.find(s => s.arc.index === index)
	if (!slice) {
		return
	}
	const rect = plot.value?.getBoundingClientRect()
	const pos = 'clientX' in event && rect
		? {x: event.clientX - rect.left, y: event.clientY - rect.top}
		: slice.arc.anchor
	tooltip.value = {
		...pos,
		title: lbl(slice.group),
		rows: [{label: `${slice.percent}%`, value: valueText(slice.group.value, props.unit), color: slice.color}],
		note: slice.reason ?? undefined,
	}
	activeIndex.value = index
}

function hide() {
	tooltip.value = null
	activeIndex.value = null
}

function activate(index: number) {
	const group = props.groups[index]
	if (!group || !props.selectable || props.disabledReason?.({group})) {
		return
	}
	emit('select', {group})
}

function selectByKey(key: string) {
	const index = props.groups.findIndex(g => groupKeyId(g) === key)
	if (index >= 0) {
		activate(index)
	}
}

function hoverByKey(key: string | null) {
	activeIndex.value = key === null ? null : props.groups.findIndex(g => groupKeyId(g) === key)
}

const centerValue = computed(() => formatNumber(props.total ?? sum.value, locale.value))
const caption = computed(() => props.title ?? t('charts.donut'))
const tableRows = computed(() => props.groups.map(g => [lbl(g), valueText(g.value, props.unit)]))
</script>

<template>
	<div
		ref="root"
		class="chart donut-chart"
		:class="{'is-side': sideBySide, 'is-dimming': activeIndex !== null}"
	>
		<ChartState
			v-if="loading"
			kind="loading"
		/>
		<ChartState
			v-else-if="groups.length === 0 || sum === 0"
			kind="empty"
		/>
		<template v-else>
			<p
				v-if="multiValued"
				class="chart__note"
			>
				{{ t('charts.multiValued') }}
			</p>
			<div class="donut-chart__body">
				<div
					ref="plot"
					class="donut-chart__plot"
					:style="{inlineSize: `${diameter}px`, blockSize: `${diameter}px`}"
					@pointerleave="hide"
				>
					<svg
						:viewBox="`0 0 ${diameter} ${diameter}`"
						:width="diameter"
						:height="diameter"
						role="group"
						:aria-label="caption"
					>
						<g
							v-for="slice in slices"
							:key="slice.id"
							class="chart__mark"
							:class="{
								'is-selectable': selectable && !slice.reason,
								'is-disabled': selectable && !!slice.reason,
								'is-active': activeIndex === slice.arc.index,
							}"
							tabindex="0"
							:role="selectable ? 'button' : 'img'"
							:aria-label="slice.aria"
							:aria-disabled="selectable && slice.reason ? 'true' : undefined"
							@pointerenter="showTooltip(slice.arc.index, $event)"
							@pointermove="showTooltip(slice.arc.index, $event)"
							@focus="showTooltip(slice.arc.index, $event)"
							@blur="hide"
							@click="activate(slice.arc.index)"
							@keydown.enter.prevent="activate(slice.arc.index)"
							@keydown.space.prevent="activate(slice.arc.index)"
						>
							<path
								:d="slice.arc.path"
								:fill="slice.color"
								fill-rule="evenodd"
							/>
							<path
								class="chart__focus-ring"
								:d="slice.arc.path"
							/>
						</g>
						<g
							aria-hidden="true"
							class="donut-chart__center"
						>
							<text
								:x="diameter / 2"
								:y="diameter / 2 + (totalLabel ? 2 : 7)"
								text-anchor="middle"
								class="donut-chart__total"
							>{{ centerValue }}</text>
							<text
								v-if="totalLabel"
								:x="diameter / 2"
								:y="diameter / 2 + 20"
								text-anchor="middle"
								class="chart__text"
							>{{ totalLabel }}</text>
						</g>
					</svg>
					<ChartTooltip
						:tooltip="tooltip"
						:width="diameter"
						:height="diameter"
					/>
				</div>
				<ChartLegend
					v-if="showLegend"
					:items="legendItems"
					:label="t('charts.legend')"
					vertical
					@select="selectByKey"
					@hover="hoverByKey"
				/>
			</div>
			<ChartTable
				:caption="caption"
				:columns="[t('charts.group'), t('charts.value')]"
				:rows="tableRows"
			/>
		</template>
	</div>
</template>

<style lang="scss" scoped>
@import './chart';

.donut-chart__body {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: .5rem;
	flex: 1 1 auto;
	min-block-size: 0;
	min-inline-size: 0;
}

.is-side .donut-chart__body {
	flex-direction: row;
	justify-content: center;
	gap: 1.5rem;
}

.donut-chart__body > :deep(.chart-legend) {
	align-self: stretch;
	min-block-size: 0;
}

.is-side .donut-chart__body > :deep(.chart-legend) {
	align-self: center;
	flex: 1 1 0;
	max-block-size: 100%;
}

.donut-chart__plot {
	position: relative;
	flex: none;
}

.donut-chart__plot > svg {
	position: static;
}

.donut-chart__total {
	fill: var(--text-strong);
	font-size: 22px;
	font-weight: 700;
	font-variant-numeric: tabular-nums;
}
</style>
