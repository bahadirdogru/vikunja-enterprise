<script setup lang="ts">
import {computed, ref, useId} from 'vue'

import {
	bandScale,
	formatAxisNumber,
	assignColors,
	groupLabel,
	groupKeyId,
	linearScale,
	maxCharsFor,
	niceTicks,
	stackModel,
	stackSegments,
	truncateLabel,
	CHAR_WIDTH,
	type ChartGroup,
	type ChartSelection,
} from '@/helpers/charts'
import ChartLegend, {type LegendItem} from './ChartLegend.vue'
import ChartState from './ChartState.vue'
import ChartTable from './ChartTable.vue'
import ChartTooltip from './ChartTooltip.vue'
import {useChartSize, useChartText, type TooltipState} from './useChart'

const props = withDefaults(defineProps<{
	groups: ChartGroup[]
	orientation?: 'vertical' | 'horizontal'
	// Groups with children are stacked, or drawn side by side when false.
	stacked?: boolean
	unit?: string
	loading?: boolean
	multiValued?: boolean
	selectable?: boolean
	showLegend?: boolean
	title?: string
	// A reason makes a bar inert and is shown in its tooltip.
	disabledReason?: (selection: ChartSelection) => string | null
}>(), {
	orientation: 'vertical',
	stacked: true,
	unit: 'tasks',
	loading: false,
	multiValued: false,
	selectable: true,
	showLegend: undefined,
	title: undefined,
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
const activeKey = ref<string | null>(null)

const model = computed(() => stackModel(props.groups))
const nested = computed(() => model.value.keys.length > 0)
const horizontal = computed(() => props.orientation === 'horizontal')
const legendVisible = computed(() => props.showLegend ?? nested.value)

const lbl = (group: ChartGroup) => groupLabel(group, t)
const colorById = computed(() => {
	const children = model.value.keys.map(k => model.value.labels.get(k)!)
	const colors = assignColors(children)
	return new Map(model.value.keys.map((key, i) => [key, colors[i]!]))
})
const groupColors = computed(() => assignColors(props.groups))

function childColor(id: string): string {
	return colorById.value.get(id) ?? 'var(--grey-400)'
}

function childLabel(id: string): string {
	const child = model.value.labels.get(id)
	return child ? lbl(child) : id
}

const maxValue = computed(() => {
	if (!nested.value) {
		return Math.max(0, ...props.groups.map(g => g.value))
	}
	if (props.stacked) {
		return Math.max(0, ...model.value.bars.map(b => b.total))
	}
	return Math.max(0, ...model.value.bars.flatMap(b => [...b.values.values()]))
})

const ticks = computed(() => niceTicks(0, maxValue.value || 1, horizontal.value ? 4 : 5))
const tickLabels = computed(() => ticks.value.ticks.map(v => formatAxisNumber(v, locale.value)))

const tickMarks = computed(() => {
	const f = frame.value
	const scale = horizontal.value
		? linearScale([ticks.value.min, ticks.value.max], [f.left, f.right])
		: linearScale([ticks.value.min, ticks.value.max], [f.bottom, f.top])
	return ticks.value.ticks.map((value, i) => ({value, pos: scale(value), label: tickLabels.value[i] ?? ''}))
})

const frame = computed(() => {
	const {width, height} = size.value
	const longest = Math.max(1, ...props.groups.map(g => Array.from(lbl(g)).length))
	if (horizontal.value) {
		const labelWidth = Math.min(longest * CHAR_WIDTH + 10, width * 0.4)
		return {left: labelWidth, right: width - 40, top: 4, bottom: height - 22, labelWidth}
	}
	const tickWidth = Math.max(...tickLabels.value.map(l => l.length)) * CHAR_WIDTH + 10
	return {left: tickWidth, right: width - 6, top: 16, bottom: height - 24, labelWidth: 0}
})

type Mark = {
	id: string
	group: ChartGroup
	label: string
	color: string
	rects: {x: number, y: number, width: number, height: number, color: string, child?: ChartGroup}[]
	box: {x: number, y: number, width: number, height: number}
	valueLabel: {x: number, y: number, text: string, anchor: string} | null
	categoryLabel: {x: number, y: number, text: string, anchor: string}
	aria: string
	reason: string | null
}

const marks = computed<Mark[]>(() => {
	const f = frame.value
	const count = props.groups.length
	const bands = bandScale(count, horizontal.value ? [f.top, f.bottom] : [f.left, f.right], 0.3)
	const along = linearScale(
		[ticks.value.min, ticks.value.max],
		horizontal.value ? [f.left, f.right] : [f.bottom, f.top],
	)
	const step = count > 0 ? (horizontal.value ? (f.bottom - f.top) : (f.right - f.left)) / count : 0
	const labelChars = horizontal.value ? maxCharsFor(f.labelWidth - 10) : Math.max(2, maxCharsFor(step))

	function rect(band: {start: number, size: number}, from: number, to: number, color: string, child?: ChartGroup) {
		const a = along(from)
		const b = along(to)
		let lo = Math.min(a, b)
		let length = Math.abs(b - a)
		// A tiny value stays visible.
		if (to > from && length < 2) {
			length = 2
			lo = horizontal.value ? a : a - 2
		}
		return horizontal.value
			? {x: lo, y: band.start, width: length, height: band.size, color, child}
			: {x: band.start, y: lo, width: band.size, height: length, color, child}
	}

	return props.groups.map((group, i) => {
		const band = bands[i]!
		const bar = model.value.bars[i]!
		const rects: Mark['rects'] = []
		let total = group.value
		if (!nested.value) {
			rects.push(rect(band, 0, group.value, groupColors.value[i]!))
		} else if (props.stacked) {
			total = bar.total
			for (const seg of stackSegments(bar.values, model.value.keys)) {
				rects.push(rect(band, seg.start, seg.end, childColor(seg.key), childOf(group, seg.key)))
			}
		} else {
			const sub = bandScale(model.value.keys.length, [band.start, band.start + band.size], 0.08)
			model.value.keys.forEach((key, k) => {
				const value = bar.values.get(key) ?? 0
				if (value > 0) {
					rects.push(rect(sub[k]!, 0, value, childColor(key), childOf(group, key)))
				}
			})
			total = Math.max(0, ...bar.values.values())
		}
		const top = along(total)
		const valueLabel = !nested.value || props.stacked
			? horizontal.value
				? {x: top + 4, y: band.center, text: formatAxisNumber(total, locale.value), anchor: 'start'}
				: {x: band.center, y: top - 4, text: formatAxisNumber(total, locale.value), anchor: 'middle'}
			: null
		const box = horizontal.value
			? {x: f.left, y: band.start, width: f.right - f.left + 40, height: band.size}
			: {x: band.start, y: f.top, width: band.size, height: f.bottom - f.top}
		const selection: ChartSelection = {group}
		return {
			id: `${uid}-${i}`,
			group,
			label: lbl(group),
			color: groupColors.value[i]!,
			rects,
			box,
			valueLabel,
			categoryLabel: horizontal.value
				? {x: f.left - 6, y: band.center + 4, text: truncateLabel(lbl(group), labelChars), anchor: 'end'}
				: {x: band.center, y: f.bottom + 16, text: truncateLabel(lbl(group), labelChars), anchor: 'middle'},
			aria: nested.value
				? `${ariaLabel(lbl(group), total, props.unit)} (${[...bar.values.entries()].map(([key, v]) => ariaLabel(childLabel(key), v, props.unit)).join(', ')})`
				: ariaLabel(lbl(group), group.value, props.unit),
			reason: props.disabledReason?.(selection) ?? null,
		}
	})
})

function childOf(group: ChartGroup, key: string): ChartGroup | undefined {
	return group.children?.find(c => groupKeyId(c) === key)
}

const legendItems = computed<LegendItem[]>(() => model.value.keys.map(key => ({
	key,
	label: childLabel(key),
	color: childColor(key),
})))

function point(event: PointerEvent | FocusEvent, fallback: {x: number, y: number}) {
	const rect = plot.value?.getBoundingClientRect()
	if ('clientX' in event && rect) {
		return {x: event.clientX - rect.left, y: event.clientY - rect.top}
	}
	return fallback
}

function showTooltip(mark: Mark, event: PointerEvent | FocusEvent) {
	const horizontalBar = horizontal.value
	const fallback = {
		x: mark.box.x + (horizontalBar ? Math.min(mark.box.width, 60) : mark.box.width / 2),
		y: horizontalBar ? mark.box.y : (mark.rects[0]?.y ?? mark.box.y),
	}
	const rows = nested.value
		? [...model.value.bars[props.groups.indexOf(mark.group)]!.values.entries()].map(([key, v]) => ({
			label: childLabel(key),
			value: valueText(v, props.unit),
			color: childColor(key),
		}))
		: [{label: mark.label, value: valueText(mark.group.value, props.unit), color: mark.color}]
	if (nested.value) {
		const sum = model.value.bars[props.groups.indexOf(mark.group)]!.total
		rows.push({label: t('charts.total'), value: valueText(sum, props.unit), color: ''})
		// The height of a stack counts a task once per child it is in.
		if (sum !== mark.group.value) {
			rows.push({label: t('charts.distinct'), value: valueText(mark.group.value, props.unit), color: ''})
		}
	}
	tooltip.value = {...point(event, fallback), title: mark.label, rows, note: mark.reason ?? undefined}
	activeKey.value = mark.id
}

function hideTooltip() {
	tooltip.value = null
	activeKey.value = null
}

function activate(mark: Mark, child?: ChartGroup) {
	if (!props.selectable || mark.reason) {
		return
	}
	emit('select', child ? {group: mark.group, child} : {group: mark.group})
}

const tableRows = computed(() => nested.value
	? props.groups.flatMap(group => (group.children ?? []).map(child => [
		lbl(group), lbl(child), valueText(child.value, props.unit),
	]))
	: props.groups.map(group => [lbl(group), valueText(group.value, props.unit)]))
const tableColumns = computed(() => nested.value
	? [t('charts.group'), t('charts.subgroup'), t('charts.value')]
	: [t('charts.group'), t('charts.value')])
const caption = computed(() => props.title ?? t('charts.bar'))
</script>

<template>
	<div
		class="chart bar-chart"
		:class="{'is-dimming': activeKey !== null}"
	>
		<ChartState
			v-if="loading"
			kind="loading"
		/>
		<ChartState
			v-else-if="groups.length === 0"
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
				@pointerleave="hideTooltip"
			>
				<svg
					:viewBox="`0 0 ${size.width} ${size.height}`"
					:width="size.width"
					:height="size.height"
					role="group"
					:aria-label="caption"
				>
					<g aria-hidden="true">
						<template
							v-for="tick in tickMarks"
							:key="tick.value"
						>
							<template v-if="horizontal">
								<line
									class="chart__grid"
									:x1="tick.pos"
									:x2="tick.pos"
									:y1="frame.top"
									:y2="frame.bottom"
								/>
								<text
									class="chart__text"
									text-anchor="middle"
									:x="tick.pos"
									:y="frame.bottom + 14"
								>{{ tick.label }}</text>
							</template>
							<template v-else>
								<line
									class="chart__grid"
									:x1="frame.left"
									:x2="frame.right"
									:y1="tick.pos"
									:y2="tick.pos"
								/>
								<text
									class="chart__text"
									text-anchor="end"
									:x="frame.left - 6"
									:y="tick.pos + 4"
								>{{ tick.label }}</text>
							</template>
						</template>
					</g>
					<g
						v-for="mark in marks"
						:key="mark.id"
						class="chart__mark"
						:class="{
							'is-selectable': selectable && !mark.reason,
							'is-disabled': selectable && !!mark.reason,
							'is-active': activeKey === mark.id,
						}"
						tabindex="0"
						:role="selectable ? 'button' : 'img'"
						:aria-label="mark.aria"
						:aria-disabled="selectable && mark.reason ? 'true' : undefined"
						@pointerenter="showTooltip(mark, $event)"
						@pointermove="showTooltip(mark, $event)"
						@focus="showTooltip(mark, $event)"
						@blur="hideTooltip"
						@click="activate(mark)"
						@keydown.enter.prevent="activate(mark)"
						@keydown.space.prevent="activate(mark)"
					>
						<rect
							:x="mark.box.x"
							:y="mark.box.y"
							:width="mark.box.width"
							:height="mark.box.height"
							fill="transparent"
						/>
						<rect
							v-for="(r, i) in mark.rects"
							:key="i"
							:x="r.x"
							:y="r.y"
							:width="r.width"
							:height="r.height"
							:fill="r.color"
							rx="2"
							@click.stop="activate(mark, r.child)"
						/>
						<rect
							class="chart__focus-ring"
							:x="mark.box.x - 1"
							:y="mark.box.y - 1"
							:width="mark.box.width + 2"
							:height="mark.box.height + 2"
							rx="3"
						/>
						<text
							v-if="mark.valueLabel"
							class="chart__value"
							:x="mark.valueLabel.x"
							:y="mark.valueLabel.y + (horizontal ? 4 : 0)"
							:text-anchor="mark.valueLabel.anchor"
							aria-hidden="true"
						>{{ mark.valueLabel.text }}</text>
						<text
							class="chart__text"
							:x="mark.categoryLabel.x"
							:y="mark.categoryLabel.y"
							:text-anchor="mark.categoryLabel.anchor"
							aria-hidden="true"
						>
							<title>{{ mark.label }}</title>
							{{ mark.categoryLabel.text }}
						</text>
					</g>
				</svg>
				<ChartTooltip
					:tooltip="tooltip"
					:width="size.width"
					:height="size.height"
				/>
			</div>
			<ChartLegend
				v-if="legendVisible && legendItems.length > 0"
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
</style>
