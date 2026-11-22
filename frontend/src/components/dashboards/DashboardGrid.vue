<script setup lang="ts">
import {computed, onBeforeUnmount, ref} from 'vue'
import {useI18n} from 'vue-i18n'

import type {DashboardWidgetResponse} from '@/client/queries/dashboards'
import {
	GRID_COLUMNS,
	GRID_GAP,
	ROW_HEIGHT,
	layoutMove,
	layoutMoveBy,
	layoutResize,
	layoutResizeBy,
	readingOrder,
	sameLayout,
	type LayoutItem,
} from '@/helpers/dashboardLayout'
import type {WidgetDisplay} from '@/helpers/dashboardDisplay'
import {layoutOf, limitsFor} from '@/helpers/dashboards'
import WidgetCard from './WidgetCard.vue'
import type {DrillRequest} from './WidgetView.vue'

const props = defineProps<{
	widgets: DashboardWidgetResponse[]
	editing: boolean
	canWrite: boolean
	displays: Record<number, WidgetDisplay>
}>()

const emit = defineEmits<{
	layout: [items: LayoutItem[]]
	edit: [id: number]
	duplicate: [id: number]
	remove: [id: number]
	display: [id: number, display: WidgetDisplay]
	drill: [id: number, request: DrillRequest]
}>()

const {t} = useI18n({useScope: 'global'})
const grid = ref<HTMLElement | null>(null)
const preview = ref<LayoutItem[] | null>(null)
const active = ref<{id: number, mode: 'move' | 'resize', dx: number, dy: number} | null>(null)
const announcement = ref('')

const base = computed(() => layoutOf(props.widgets))
const items = computed(() => preview.value ?? base.value)
const byId = computed(() => new Map(props.widgets.map(w => [w.id, w])))
const ordered = computed(() => readingOrder(items.value).map(item => ({item, widget: byId.value.get(item.id)!})).filter(e => e.widget))
// Arranging makes no sense in one column, which the grid collapses to on small screens.
const canArrange = computed(() => props.editing && props.canWrite)

type Drag = {
	id: number
	mode: 'move' | 'resize'
	startX: number
	startY: number
	origin: LayoutItem
	layout: LayoutItem[]
	colStep: number
	rowStep: number
}
let drag: Drag | null = null

function metrics() {
	const rect = grid.value?.getBoundingClientRect()
	const width = rect && rect.width > 0 ? rect.width : 1200
	return {
		colStep: (width + GRID_GAP) / GRID_COLUMNS,
		rowStep: ROW_HEIGHT + GRID_GAP,
	}
}

function start(mode: 'move' | 'resize', id: number, event: PointerEvent) {
	if (event.button !== 0 || !canArrange.value || isSingleColumn()) {
		return
	}
	const origin = base.value.find(item => item.id === id)
	if (!origin) {
		return
	}
	const {colStep, rowStep} = metrics()
	drag = {id, mode, startX: event.clientX, startY: event.clientY, origin, layout: base.value, colStep, rowStep}
	active.value = {id, mode, dx: 0, dy: 0}
	window.addEventListener('pointermove', onMove)
	window.addEventListener('pointerup', onUp)
	window.addEventListener('pointercancel', cancel)
	window.addEventListener('keydown', onEscape)
}

function isSingleColumn(): boolean {
	return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
		&& window.matchMedia('(max-width: 768px)').matches
}

function onMove(event: PointerEvent) {
	if (!drag) {
		return
	}
	const dx = event.clientX - drag.startX
	const dy = event.clientY - drag.startY
	const cols = Math.round(dx / drag.colStep)
	const rows = Math.round(dy / drag.rowStep)
	const limits = limitsFor(byId.value.get(drag.id)?.type ?? '')
	preview.value = drag.mode === 'move'
		? layoutMove(drag.layout, drag.id, drag.origin.x + cols, drag.origin.y + rows, limits)
		: layoutResize(drag.layout, drag.id, drag.origin.w + cols, drag.origin.h + rows, limits)
	active.value = {id: drag.id, mode: drag.mode, dx, dy}
}

function finish(commit: boolean) {
	window.removeEventListener('pointermove', onMove)
	window.removeEventListener('pointerup', onUp)
	window.removeEventListener('pointercancel', cancel)
	window.removeEventListener('keydown', onEscape)
	const result = preview.value
	const before = drag?.layout ?? base.value
	drag = null
	active.value = null
	if (commit && result && !sameLayout(result, before)) {
		emit('layout', result)
	}
	preview.value = null
}

function onUp() {
	finish(true)
}

function cancel() {
	finish(false)
}

function onEscape(event: KeyboardEvent) {
	if (event.key === 'Escape') {
		finish(false)
	}
}

onBeforeUnmount(() => finish(false))

// The dragged card follows the pointer from the cell it started in, whatever cell the layout put it in meanwhile.
function transformFor(id: number): string | undefined {
	const a = active.value
	if (!a || a.id !== id || a.mode !== 'move' || !drag) {
		return undefined
	}
	const now = items.value.find(item => item.id === id)
	if (!now) {
		return undefined
	}
	const x = a.dx - (now.x - drag.origin.x) * drag.colStep
	const y = a.dy - (now.y - drag.origin.y) * drag.rowStep
	return `translate(${x}px, ${y}px)`
}

function onHeaderKeydown(id: number, event: KeyboardEvent) {
	if (event.target !== event.currentTarget) {
		return
	}
	const delta: Record<string, [number, number]> = {
		ArrowLeft: [-1, 0],
		ArrowRight: [1, 0],
		ArrowUp: [0, -1],
		ArrowDown: [0, 1],
	}
	const d = delta[event.key]
	if (!d || event.altKey || event.ctrlKey || event.metaKey) {
		return
	}
	event.preventDefault()
	const widget = byId.value.get(id)
	const limits = limitsFor(widget?.type ?? '')
	const next = event.shiftKey
		? layoutResizeBy(base.value, id, d[0], d[1], limits)
		: layoutMoveBy(base.value, id, d[0], d[1], limits)
	const moved = next.find(item => item.id === id)
	if (!moved || sameLayout(next, base.value)) {
		return
	}
	announcement.value = event.shiftKey
		? t('dashboards.grid.resized', {title: widget?.title || t(`dashboards.types.${widget?.type}.name`), w: moved.w, h: moved.h})
		: t('dashboards.grid.moved', {title: widget?.title || t(`dashboards.types.${widget?.type}.name`), column: moved.x + 1, row: moved.y + 1})
	emit('layout', next)
}

function itemStyle(item: LayoutItem) {
	return {
		'--col': `${item.x + 1} / span ${item.w}`,
		'--row': `${item.y + 1} / span ${item.h}`,
		'--span': item.h,
		'--span-mobile': Math.max(item.h, 3),
		transform: transformFor(item.id),
	}
}
</script>

<template>
	<div>
		<div
			ref="grid"
			class="dashboard-grid"
			:class="{'is-editing': editing}"
			data-test="dashboard-grid"
			:style="{'--row-height': `${ROW_HEIGHT}px`, '--gap': `${GRID_GAP}px`}"
		>
			<div
				v-for="{item, widget} in ordered"
				:key="widget.id"
				class="dashboard-grid__item"
				:class="{'is-active': active?.id === widget.id}"
				:style="itemStyle(item)"
			>
				<WidgetCard
					:id="widget.id"
					:type="widget.type"
					:title="widget.title"
					:config="widget.config"
					:display="displays[widget.id] ?? {}"
					:editing="editing"
					:can-write="canWrite"
					:can-arrange="canArrange"
					:moving="active?.id === widget.id && active.mode === 'move'"
					:resizing="active?.id === widget.id && active.mode === 'resize'"
					@edit="emit('edit', widget.id)"
					@duplicate="emit('duplicate', widget.id)"
					@remove="emit('remove', widget.id)"
					@display="emit('display', widget.id, $event)"
					@drill="emit('drill', widget.id, $event)"
					@headerKeydown="onHeaderKeydown(widget.id, $event)"
					@dragStart="start('move', widget.id, $event)"
					@resizeStart="start('resize', widget.id, $event)"
				/>
			</div>
		</div>
		<p
			class="visually-hidden"
			role="status"
			aria-live="polite"
		>
			{{ announcement }}
		</p>
	</div>
</template>

<style lang="scss" scoped>
.dashboard-grid {
	display: grid;
	grid-template-columns: repeat(12, minmax(0, 1fr));
	grid-auto-rows: var(--row-height);
	gap: var(--gap);
	position: relative;

	&.is-editing {
		background-image:
			repeating-linear-gradient(
				to right,
				transparent 0,
				transparent calc((100% + var(--gap)) / 12 - 1px),
				var(--grey-100) calc((100% + var(--gap)) / 12 - 1px),
				var(--grey-100) calc((100% + var(--gap)) / 12)
			);
		background-size: 100% 100%;
	}
}

.dashboard-grid__item {
	grid-column: var(--col);
	grid-row: var(--row);
	min-inline-size: 0;
	min-block-size: 0;
	display: flex;
	flex-direction: column;

	&.is-active {
		z-index: 10;
	}

	> :deep(.widget-card) {
		flex: 1 1 auto;
	}

	@media (prefers-reduced-motion: no-preference) {
		&:not(.is-active) {
			transition: none;
		}
	}
}

@media screen and (width <= 768px) {
	.dashboard-grid {
		grid-template-columns: minmax(0, 1fr);
		background-image: none !important;
	}

	.dashboard-grid__item {
		grid-column: 1;
		grid-row: auto / span var(--span-mobile);
		transform: none !important;
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
