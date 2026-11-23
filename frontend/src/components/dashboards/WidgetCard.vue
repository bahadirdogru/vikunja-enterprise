<script setup lang="ts">
import {computed, ref, toRef} from 'vue'
import {useIsFetching} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'

import {reportKeys} from '@/client/queries/reports'
import {taskKeys} from '@/client/queries/tasks'
import {useProjects} from '@/composables/useProjects'
import {useWidgetExport} from '@/composables/useWidgetExport'
import type {WidgetDisplay} from '@/helpers/dashboardDisplay'
import {summarizeFilter, widgetFilter, widgetProjectId} from '@/helpers/dashboards'
import Dropdown from '@/components/misc/Dropdown.vue'
import DropdownItem from '@/components/misc/DropdownItem.vue'
import BaseButton from '@/components/base/BaseButton.vue'
import {useQueryClient} from '@tanstack/vue-query'
import WidgetView, {type DrillRequest} from './WidgetView.vue'

const props = defineProps<{
	id: number
	type: string
	title: string
	config: Record<string, unknown>
	display: WidgetDisplay
	editing: boolean
	canWrite: boolean
	// Layout editing is off on a phone, where the grid is one column.
	canArrange: boolean
	moving?: boolean
	resizing?: boolean
}>()

const emit = defineEmits<{
	edit: []
	duplicate: []
	remove: []
	display: [display: WidgetDisplay]
	drill: [request: DrillRequest]
	headerKeydown: [event: KeyboardEvent]
	dragStart: [event: PointerEvent]
	resizeStart: [event: PointerEvent]
}>()

const {t} = useI18n({useScope: 'global'})
const projectStore = useProjects()
const queryClient = useQueryClient()
const fetching = useIsFetching({predicate: query => query.queryKey[0] === reportKeys.all[0] || query.queryKey[0] === taskKeys.all[0]})
const refreshing = ref(false)

const widgetExport = useWidgetExport(toRef(props, 'type'), toRef(props, 'config'))

const subtitle = computed(() => {
	const parts: string[] = []
	const project = widgetProjectId(props.type, props.config)
	if (project > 0 && projectStore.projects[project]) {
		parts.push(projectStore.projects[project]!.title)
	}
	const filter = summarizeFilter(widgetFilter(props.type, props.config))
	if (filter) {
		parts.push(filter)
	}
	return parts.join(' · ')
})

const isChart = computed(() => ['number', 'bar', 'donut', 'line', 'created_vs_done'].includes(props.type))
const heading = computed(() => props.title || t(`dashboards.types.${props.type}.name`))
const hasLegendOptions = computed(() => ['bar', 'donut', 'line', 'created_vs_done'].includes(props.type))

async function refresh() {
	refreshing.value = true
	try {
		await queryClient.invalidateQueries({queryKey: props.type === 'task_list' ? taskKeys.lists : reportKeys.all})
	} finally {
		refreshing.value = false
	}
}

function setDisplay(patch: WidgetDisplay) {
	emit('display', {...props.display, ...patch})
}
</script>

<template>
	<article
		class="widget-card"
		:class="{'is-editing': editing, 'is-moving': moving, 'is-resizing': resizing}"
		:data-widget-id="id"
		:aria-label="heading"
	>
		<header
			class="widget-card__header"
			:class="{'is-draggable': editing && canArrange}"
			:tabindex="editing && canArrange ? 0 : undefined"
			:aria-roledescription="editing && canArrange ? t('dashboards.grid.draggable') : undefined"
			:aria-label="editing && canArrange ? t('dashboards.grid.moveLabel', {title: heading}) : undefined"
			@pointerdown="editing && canArrange && emit('dragStart', $event)"
			@keydown="editing && canArrange && emit('headerKeydown', $event)"
		>
			<div class="widget-card__titles">
				<h3 class="widget-card__title">
					{{ heading }}
				</h3>
				<p
					v-if="subtitle"
					class="widget-card__subtitle"
					:title="subtitle"
				>
					{{ subtitle }}
				</p>
			</div>
			<div
				class="widget-card__actions"
				@pointerdown.stop
				@keydown.stop
			>
				<BaseButton
					v-if="type !== 'text'"
					class="widget-card__action"
					:class="{'is-spinning': refreshing || fetching > 0}"
					:aria-label="t('dashboards.widget.refresh')"
					:title="t('dashboards.widget.refresh')"
					@click="refresh"
				>
					<Icon icon="arrows-rotate" />
				</BaseButton>
				<Dropdown
					v-if="canWrite || (isChart && hasLegendOptions) || widgetExport.canExportData.value"
					:trigger-label="t('dashboards.widget.menu', {title: heading})"
				>
					<template v-if="isChart && type === 'bar'">
						<DropdownItem
							icon="chart-column"
							@click="setDisplay({orientation: (display.orientation ?? 'vertical') === 'vertical' ? 'horizontal' : 'vertical'})"
						>
							{{ t('dashboards.widget.toggleOrientation') }}
						</DropdownItem>
						<DropdownItem
							icon="grip"
							@click="setDisplay({stacked: !(display.stacked ?? true)})"
						>
							{{ t('dashboards.widget.toggleStacked') }}
						</DropdownItem>
					</template>
					<DropdownItem
						v-if="isChart && hasLegendOptions"
						icon="list"
						@click="setDisplay({legend: !(display.legend ?? true)})"
					>
						{{ t('dashboards.widget.toggleLegend') }}
					</DropdownItem>
					<DropdownItem
						v-if="widgetExport.canExportData.value"
						icon="download"
						data-test="widget-export"
						@click="widgetExport.exportData()"
					>
						{{ type === 'task_list' ? t('dashboards.widget.exportTasks') : t('dashboards.widget.exportCsv') }}
					</DropdownItem>
					<DropdownItem
						v-if="widgetExport.canExportTasks.value"
						icon="download"
						data-test="widget-export-tasks"
						@click="widgetExport.exportTasks()"
					>
						{{ t('dashboards.widget.exportTasks') }}
					</DropdownItem>
					<template v-if="canWrite">
						<DropdownItem
							icon="pen"
							data-test="widget-edit"
							@click="emit('edit')"
						>
							{{ t('dashboards.widget.edit') }}
						</DropdownItem>
						<DropdownItem
							icon="copy"
							data-test="widget-duplicate"
							@click="emit('duplicate')"
						>
							{{ t('dashboards.widget.duplicate') }}
						</DropdownItem>
						<DropdownItem
							icon="trash-alt"
							icon-class="has-text-danger"
							data-test="widget-remove"
							@click="emit('remove')"
						>
							{{ t('dashboards.widget.remove') }}
						</DropdownItem>
					</template>
				</Dropdown>
			</div>
		</header>
		<div class="widget-card__body">
			<WidgetView
				:type="type"
				:config="config"
				:title="heading"
				:display="display"
				:can-drill="!editing"
				@drill="emit('drill', $event)"
			/>
		</div>
		<span
			v-if="editing && canArrange"
			class="widget-card__resize"
			role="presentation"
			@pointerdown.prevent="emit('resizeStart', $event)"
		/>
	</article>
</template>

<style lang="scss" scoped>
.widget-card {
	position: relative;
	display: flex;
	flex-direction: column;
	min-inline-size: 0;
	min-block-size: 0;
	border: 1px solid var(--grey-200);
	border-radius: var(--input-radius);
	background: var(--scheme-main);
	box-shadow: var(--shadow-xs);

	&.is-editing {
		border-style: dashed;
	}

	&.is-moving {
		z-index: 10;
		box-shadow: var(--shadow-lg);
		opacity: .92;
		cursor: grabbing;
	}

	&.is-resizing {
		border-color: var(--primary);
	}
}

.widget-card__header {
	display: flex;
	align-items: flex-start;
	justify-content: space-between;
	gap: .5rem;
	padding: .6rem .75rem .25rem;

	&.is-draggable {
		cursor: grab;
		touch-action: none;
		user-select: none;
		border-radius: var(--input-radius) var(--input-radius) 0 0;

		&:hover {
			background: var(--grey-100);
		}

		&:focus-visible {
			outline: 2px solid var(--primary);
			outline-offset: -2px;
		}
	}
}

.widget-card__titles {
	min-inline-size: 0;
}

.widget-card__title {
	margin: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	color: var(--text-strong);
	font-size: .95rem;
	font-weight: 600;
}

.widget-card__subtitle {
	margin: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	color: var(--text-light);
	font-size: .75rem;
}

.widget-card__actions {
	display: flex;
	flex: none;
	align-items: center;
	gap: .25rem;
	color: var(--text-light);
}

.widget-card__action {
	padding: .25rem;
	border-radius: var(--input-radius);
	color: inherit;

	&:hover {
		background: var(--grey-100);
		color: var(--text);
	}

	&.is-spinning :deep(svg) {
		@media (prefers-reduced-motion: no-preference) {
			animation: widget-spin 1s linear infinite;
		}
	}
}

.widget-card__body {
	flex: 1 1 auto;
	min-block-size: 0;
	padding: .25rem .75rem .75rem;
	overflow: hidden;
}

.widget-card__resize {
	position: absolute;
	inset-block-end: 0;
	inset-inline-end: 0;
	inline-size: 1.1rem;
	block-size: 1.1rem;
	cursor: nwse-resize;
	touch-action: none;
	background:
		linear-gradient(135deg, transparent 50%, var(--grey-400) 50%, var(--grey-400) 58%, transparent 58%, transparent 70%, var(--grey-400) 70%, var(--grey-400) 78%, transparent 78%);
	border-end-end-radius: var(--input-radius);
}

@keyframes widget-spin {
	to {
		transform: rotate(360deg);
	}
}
</style>
