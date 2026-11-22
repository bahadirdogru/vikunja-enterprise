<script setup lang="ts">
import {computed, ref, useId} from 'vue'

import {
	areaPath,
	CHAR_WIDTH,
	formatAxisNumber,
	formatBucket,
	labelIndexes,
	linearScale,
	linePath,
	niceTicks,
	assignColors,
	type ChartSelection,
	type ChartSeries,
} from '@/helpers/charts'
import ChartLegend, {type LegendItem} from './ChartLegend.vue'
import ChartState from './ChartState.vue'
import ChartTable from './ChartTable.vue'
import ChartTooltip from './ChartTooltip.vue'
import {useChartSize, useChartText, type TooltipState} from './useChart'

const props = withDefaults(defineProps<{
	series: ChartSeries[]
	// day, week or month: only used to format the x axis.
	bucket?: string
	unit?: string
	loading?: boolean
	multiValued?: boolean
	selectable?: boolean
	showLegend?: boolean
	title?: string
	// Fill the area under a single series.
	area?: boolean
	disabledReason?: (selection: ChartSelection) => string | null
}>(), {
	bucket: 'day',
	unit: 'tasks',
	loading: false,
	multiValued: false,
	selectable: true,
	showLegend: undefined,
	title: undefined,
	area: true,
	disabledReason: undefined,
})

const emit = defineEmits<{
	select: [selection: ChartSelection]
}>()

const {t, locale, valueText, ariaLabel} = useChartText()
const uid = useId()
const plot = ref<HTMLElement | null>(null)
const size = useChartSize(plot)
const tooltip = ref<TooltipState | null>(null)
// The point the keyboard and pointer are on: bucket index and series index.
const cursor = ref<{bucket: number, series: number} | null>(null)
const focused = ref(false)

const buckets = computed(() => props.series[0]?.points ?? [])
const count = computed(() => buckets.value.length)
const hasData = computed(() => count.value > 0 && props.series.some(s => s.points.length > 0))

const ticks = computed(() => {
	const values = props.series.flatMap(s => s.points.map(p => p.value))
	return niceTicks(0, Math.max(0, ...values) || 1, 5)
})
const tickLabels = computed(() => ticks.value.ticks.map(v => formatAxisNumber(v, locale.value)))

const frame = computed(() => {
	const {width, height} = size.value
	const left = Math.max(...tickLabels.value.map(l => l.length)) * CHAR_WIDTH + 12
	return {left, right: width - 12, top: 10, bottom: height - 26}
})

const xOf = computed(() => {
	const f = frame.value
	return (i: number) => count.value <= 1 ? (f.left + f.right) / 2 : f.left + (i / (count.value - 1)) * (f.right - f.left)
})
const yScale = computed(() => linearScale([ticks.value.min, ticks.value.max], [frame.value.bottom, frame.value.top]))

const seriesColors = computed(() => assignColors(props.series.map(s => ({
	key: s.key,
	label: s.label,
	value: 0,
	color: s.color,
	none: s.group?.none,
	other: s.group?.other,
}))))

const lines = computed(() => props.series.map((s, si) => {
	const color = seriesColors.value[si]!
	const pts = s.points.map((p, i) => ({x: xOf.value(i), y: yScale.value(p.value)}))
	return {
		key: s.key,
		series: s,
		color,
		pts,
		d: linePath(pts),
		area: props.area && props.series.length === 1 ? areaPath(pts, frame.value.bottom) : '',
	}
}))

const xLabels = computed(() => labelIndexes(count.value, frame.value.right - frame.value.left, 64).map(i => ({
	i,
	x: xOf.value(i),
	text: formatBucket(buckets.value[i]!.bucket, props.bucket, locale.value),
})))

const showMarkers = computed(() => count.value <= 31)
const legendVisible = computed(() => props.showLegend ?? props.series.length > 1)
const legendItems = computed<LegendItem[]>(() => props.series.map((s, i) => ({
	key: s.key,
	label: s.label,
	color: seriesColors.value[i]!,
})))

function selectionAt(bucket: number, series: number): ChartSelection | null {
	const s = props.series[series]
	const point = s?.points[bucket]
	return s && point ? {series: s, point, group: s.group} : null
}

function reasonAt(bucket: number, series: number): string | null {
	const selection = selectionAt(bucket, series)
	return selection ? props.disabledReason?.(selection) ?? null : null
}

function tooltipFor(bucket: number, series: number): TooltipState | null {
	const point = buckets.value[bucket]
	if (!point) {
		return null
	}
	const rows = props.series.map((s, i) => ({
		label: s.label,
		value: valueText(s.points[bucket]?.value ?? 0, props.unit),
		color: seriesColors.value[i]!,
	}))
	const y = yScale.value(props.series[series]?.points[bucket]?.value ?? 0)
	return {
		x: xOf.value(bucket),
		y,
		title: formatBucket(point.bucket, props.bucket, locale.value),
		rows,
		note: reasonAt(bucket, series) ?? undefined,
	}
}

function move(bucket: number, series: number) {
	cursor.value = {bucket, series}
	tooltip.value = tooltipFor(bucket, series)
}

function nearest(event: PointerEvent) {
	const rect = plot.value?.getBoundingClientRect()
	if (!rect || count.value === 0) {
		return null
	}
	const x = event.clientX - rect.left
	const y = event.clientY - rect.top
	const f = frame.value
	const raw = count.value <= 1 ? 0 : Math.round(((x - f.left) / (f.right - f.left)) * (count.value - 1))
	const bucket = Math.min(count.value - 1, Math.max(0, raw))
	let series = 0
	let best = Infinity
	props.series.forEach((s, i) => {
		const distance = Math.abs(yScale.value(s.points[bucket]?.value ?? 0) - y)
		if (distance < best) {
			best = distance
			series = i
		}
	})
	return {bucket, series}
}

function onPointer(event: PointerEvent) {
	const target = nearest(event)
	if (target) {
		move(target.bucket, target.series)
	}
}

function leave() {
	if (!focused.value) {
		cursor.value = null
		tooltip.value = null
	}
}

function activate(bucket: number, series: number) {
	const selection = selectionAt(bucket, series)
	if (!selection || !props.selectable || props.disabledReason?.(selection)) {
		return
	}
	emit('select', selection)
}

function onClick(event: PointerEvent) {
	const target = nearest(event)
	if (target) {
		activate(target.bucket, target.series)
	}
}

// One tab stop for the whole line: arrows move along the buckets and between the series.
function onKey(event: KeyboardEvent) {
	const current = cursor.value ?? {bucket: 0, series: 0}
	const last = count.value - 1
	const seriesLast = props.series.length - 1
	const keys: Record<string, () => void> = {
		ArrowRight: () => move(Math.min(last, current.bucket + 1), current.series),
		ArrowLeft: () => move(Math.max(0, current.bucket - 1), current.series),
		ArrowUp: () => move(current.bucket, Math.max(0, current.series - 1)),
		ArrowDown: () => move(current.bucket, Math.min(seriesLast, current.series + 1)),
		Home: () => move(0, current.series),
		End: () => move(last, current.series),
		Enter: () => activate(current.bucket, current.series),
		' ': () => activate(current.bucket, current.series),
	}
	const handler = keys[event.key]
	if (handler) {
		event.preventDefault()
		handler()
	}
}

function onFocus() {
	focused.value = true
	move(cursor.value?.bucket ?? 0, cursor.value?.series ?? 0)
}

function onBlur() {
	focused.value = false
	cursor.value = null
	tooltip.value = null
}

const liveLabel = computed(() => {
	const c = cursor.value
	if (!c) {
		return props.title ?? t('charts.line')
	}
	const s = props.series[c.series]
	const p = s?.points[c.bucket]
	return s && p ? `${s.label}, ${formatBucket(p.bucket, props.bucket, locale.value)}: ${ariaLabel('', p.value, props.unit).slice(2)}` : ''
})

const caption = computed(() => props.title ?? t('charts.line'))
const tableColumns = computed(() => [t('charts.date'), ...props.series.map(s => s.label)])
const tableRows = computed(() => buckets.value.map((p, i) => [
	formatBucket(p.bucket, props.bucket, locale.value),
	...props.series.map(s => valueText(s.points[i]?.value ?? 0, props.unit)),
]))
</script>

<template>
	<div class="chart line-chart">
		<ChartState
			v-if="loading"
			kind="loading"
		/>
		<ChartState
			v-else-if="!hasData"
			kind="empty"
		/>
		<template v-else>
			<p
				v-if="multiValued"
				class="chart__note"
			>
				{{ t('charts.multiValued') }}
			</p>
			<div
				ref="plot"
				class="chart__plot"
				@pointerleave="leave"
			>
				<svg
					:viewBox="`0 0 ${size.width} ${size.height}`"
					:width="size.width"
					:height="size.height"
					role="application"
					tabindex="0"
					:aria-label="caption"
					:aria-description="t('charts.lineHelp')"
					@pointermove="onPointer"
					@pointerdown="onPointer"
					@click="onClick"
					@keydown="onKey"
					@focus="onFocus"
					@blur="onBlur"
				>
					<g aria-hidden="true">
						<template
							v-for="(tick, i) in ticks.ticks"
							:key="tick"
						>
							<line
								class="chart__grid"
								:x1="frame.left"
								:x2="frame.right"
								:y1="yScale(tick)"
								:y2="yScale(tick)"
							/>
							<text
								class="chart__text"
								text-anchor="end"
								:x="frame.left - 6"
								:y="yScale(tick) + 4"
							>{{ tickLabels[i] }}</text>
						</template>
						<text
							v-for="label in xLabels"
							:key="label.i"
							class="chart__text"
							text-anchor="middle"
							:x="label.x"
							:y="frame.bottom + 16"
						>{{ label.text }}</text>
					</g>
					<g aria-hidden="true">
						<template
							v-for="line in lines"
							:key="line.key"
						>
							<path
								v-if="line.area"
								:d="line.area"
								:fill="line.color"
								opacity=".12"
							/>
							<path
								:d="line.d"
								:stroke="line.color"
								stroke-width="2"
								fill="none"
								stroke-linejoin="round"
								stroke-linecap="round"
							/>
							<template v-if="showMarkers">
								<circle
									v-for="(p, i) in line.pts"
									:key="`${uid}-${i}`"
									:cx="p.x"
									:cy="p.y"
									r="2.5"
									:fill="line.color"
								/>
							</template>
						</template>
						<template v-if="cursor">
							<line
								class="chart__grid"
								:x1="xOf(cursor.bucket)"
								:x2="xOf(cursor.bucket)"
								:y1="frame.top"
								:y2="frame.bottom"
								stroke-dasharray="3 3"
							/>
							<circle
								v-for="line in lines"
								:key="`c-${line.key}`"
								:cx="line.pts[cursor.bucket]?.x"
								:cy="line.pts[cursor.bucket]?.y"
								r="5"
								:fill="line.color"
								stroke="var(--scheme-main)"
								stroke-width="2"
							/>
						</template>
					</g>
				</svg>
				<ChartTooltip
					:tooltip="tooltip"
					:width="size.width"
					:height="size.height"
				/>
				<span
					class="line-chart__live"
					aria-live="polite"
				>{{ focused ? liveLabel : '' }}</span>
			</div>
			<ChartLegend
				v-if="legendVisible"
				:items="legendItems"
				:label="t('charts.legend')"
			/>
			<ChartTable
				:caption="caption"
				:columns="tableColumns"
				:rows="tableRows"
			/>
		</template>
	</div>
</template>

<style lang="scss" scoped>
@import './chart';

.line-chart svg {
	outline: none;
	cursor: crosshair;

	&:focus-visible {
		outline: 2px solid var(--primary);
		outline-offset: 2px;
	}
}

.line-chart__live {
	position: absolute;
	inline-size: 1px;
	block-size: 1px;
	overflow: hidden;
	clip-path: inset(50%);
}
</style>
