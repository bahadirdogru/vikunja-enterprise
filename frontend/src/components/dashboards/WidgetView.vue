<script setup lang="ts">
import {computed, watch} from 'vue'
import {useQueries, useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'
import {widgetErrorText} from '@/helpers/dashboards'

import {reportOptionsQuery, taskReportQuery, type TaskReportResponse} from '@/client/queries/reports'
import {drilldownFilter, drilldownInputFor, reportRangePoint, type DrilldownBlock} from '@/helpers/dashboardDrilldown'
import {defaultOrientation, type WidgetDisplay} from '@/helpers/dashboardDisplay'
import {
	configRequest,
	requestsForWidget,
	widgetProjectId,
	type TaskListConfig,
} from '@/helpers/dashboards'
import {groupLabel, type ChartSelection, type ChartSeries} from '@/helpers/charts'
import BarChart from '@/components/charts/BarChart.vue'
import ChartState from '@/components/charts/ChartState.vue'
import DonutChart from '@/components/charts/DonutChart.vue'
import LineChart from '@/components/charts/LineChart.vue'
import NumberTile from '@/components/charts/NumberTile.vue'
import TaskListWidget from './TaskListWidget.vue'
import TextWidget from './TextWidget.vue'
import StatusTimeWidget from './StatusTimeWidget.vue'
import TimeTrackingWidget from './TimeTrackingWidget.vue'
import WorkloadWidget from './WorkloadWidget.vue'

export type DrillRequest = {
	selection: ChartSelection | null
}

const props = withDefaults(defineProps<{
	type: string
	config: Record<string, unknown>
	title?: string
	display?: WidgetDisplay
	canDrill?: boolean
	// The editor passes false until the unsaved config can be requested at all.
	enabled?: boolean
}>(), {
	title: undefined,
	display: () => ({}),
	canDrill: true,
	enabled: true,
})

const emit = defineEmits<{
	drill: [request: DrillRequest]
	// The editor maps validation errors of the unsaved config onto its controls.
	error: [error: unknown]
}>()

const {t, te} = useI18n({useScope: 'global'})

const requests = computed(() => requestsForWidget(props.type, props.config))
const queries = useQueries({
	queries: computed(() => requests.value.map(request => taskReportQuery(request, props.enabled))),
})
const reports = computed<(TaskReportResponse | undefined)[]>(() => queries.value.map(q => q.data))
const firstError = computed(() => queries.value.find(q => q.isError)?.error ?? null)
watch(firstError, error => emit('error', error))
const isPending = computed(() => queries.value.some(q => q.isPending))
const report = computed(() => reports.value[0])

const request = computed(() => configRequest({config: props.config}))
// Custom field types are needed to decide whether a "no value" group can be listed.
const optionsQuery = useQuery(computed(() => ({
	...reportOptionsQuery(widgetProjectId(props.type, props.config)),
	enabled: (request.value.group_by ?? '').startsWith('cf_') || (request.value.then_by ?? '').startsWith('cf_'),
})))
const fieldTypes = computed(() => Object.fromEntries(
	(optionsQuery.data.value?.dimensions ?? []).filter(d => d.field_type).map(d => [d.name, d.field_type]),
))

// Only the reasons the user cannot resolve by waiting block a click; usernames and field options are fetched when
// the panel opens.
const hardBlocks: DrilldownBlock[] = ['other', 'null_filter', 'include_nulls', 'status_entered', 'unknown_dimension']

function reasonFor(selection: ChartSelection): string | null {
	if (!props.canDrill) {
		return null
	}
	const result = drilldownFilter({
		...drilldownInputFor({type: props.type, config: props.config}, selection),
		fieldTypes: fieldTypes.value,
		assumeResolvable: true,
	})
	return !result.ok && hardBlocks.includes(result.reason) ? t(`dashboards.drilldown.blocked.${result.reason}`) : null
}

function select(selection: ChartSelection) {
	if (props.canDrill) {
		emit('drill', {selection})
	}
}

// A number with a time range lists the tasks of that range, so its click carries the range the server resolved.
const numberSelection = computed<ChartSelection>(() => {
	const point = reportRangePoint(report.value?.time)
	return point ? {point} : {}
})

const orientation = computed(() => props.display.orientation ?? defaultOrientation(report.value?.groups ?? []))
const stacked = computed(() => props.display.stacked ?? true)
const legend = computed(() => props.display.legend)

const lineSeries = computed<ChartSeries[]>(() => {
	const r = report.value
	if (!r) {
		return []
	}
	if (props.type === 'created_vs_done') {
		const [created, done] = reports.value
		return [
			{key: 'created', label: t('dashboards.createdSeries'), color: 'var(--info)', points: created?.series ?? []},
			{key: 'done', label: t('dashboards.doneSeries'), color: 'var(--success)', points: done?.series ?? []},
		]
	}
	if (r.groups.length > 0) {
		return r.groups.map((group, i) => ({
			key: `${i}:${String(group.key)}`,
			label: groupLabel(group, t),
			color: group.color,
			points: group.series,
			group,
		}))
	}
	return [{key: 'all', label: t('dashboards.allTasks'), color: 'var(--info)', points: r.series}]
})

const bucket = computed(() => report.value?.time?.bucket ?? 'day')
const hasChartError = computed(() => firstError.value !== null && !isPending.value && !report.value)
const errorText = computed(() => widgetErrorText(firstError.value, t, te))
const loading = computed(() => isPending.value && props.enabled)
</script>

<template>
	<div class="widget-view">
		<TaskListWidget
			v-if="type === 'task_list'"
			:config="config as TaskListConfig"
			@showAll="select({})"
		/>
		<TextWidget
			v-else-if="type === 'text'"
			:text="String(config.text ?? '')"
		/>
		<WorkloadWidget
			v-else-if="type === 'workload'"
			:config="config"
			:title="title"
			:enabled="enabled"
			@error="emit('error', $event)"
		/>
		<StatusTimeWidget
			v-else-if="type === 'status_time'"
			:config="config"
			:title="title"
			:enabled="enabled"
			@error="emit('error', $event)"
		/>
		<TimeTrackingWidget
			v-else-if="type === 'time_tracking'"
			:config="config"
			:title="title"
			:enabled="enabled"
			@error="emit('error', $event)"
		/>
		<ChartState
			v-else-if="hasChartError"
			kind="error"
			:message="errorText"
		/>
		<ChartState
			v-else-if="!enabled || (requests.length === 0 && type === 'line')"
			kind="empty"
			:message="t('dashboards.editor.previewIdle')"
		/>
		<NumberTile
			v-else-if="type === 'number'"
			:value="report?.value ?? 0"
			:unit="report?.unit ?? 'tasks'"
			:label="title"
			:previous="report?.comparison?.value ?? null"
			:loading="loading"
			:selectable="canDrill"
			:disabled-reason="reasonFor(numberSelection)"
			@select="select(numberSelection)"
		/>
		<BarChart
			v-else-if="type === 'bar'"
			:groups="report?.groups ?? []"
			:orientation="orientation"
			:stacked="stacked"
			:show-legend="legend"
			:unit="report?.unit ?? 'tasks'"
			:multi-valued="report?.multi_valued"
			:loading="loading"
			:selectable="canDrill"
			:title="title"
			:disabled-reason="reasonFor"
			@select="select"
		/>
		<DonutChart
			v-else-if="type === 'donut'"
			:groups="report?.groups ?? []"
			:unit="report?.unit ?? 'tasks'"
			:total="report?.unit === 'tasks' || !report?.unit ? report?.total : undefined"
			:total-label="t('charts.tasksUnit', report?.total ?? 0)"
			:show-legend="legend ?? true"
			:multi-valued="report?.multi_valued"
			:loading="loading"
			:selectable="canDrill"
			:title="title"
			:disabled-reason="reasonFor"
			@select="select"
		/>
		<LineChart
			v-else-if="type === 'line' || type === 'created_vs_done'"
			:series="lineSeries"
			:bucket="bucket"
			:unit="report?.unit ?? 'tasks'"
			:multi-valued="report?.multi_valued"
			:show-legend="legend"
			:loading="loading"
			:selectable="canDrill"
			:title="title"
			:disabled-reason="reasonFor"
			@select="select"
		/>
		<ChartState
			v-else
			kind="empty"
			:message="t('dashboards.unsupportedType')"
		/>
	</div>
</template>

<style lang="scss" scoped>
.widget-view {
	block-size: 100%;
	min-block-size: 0;
	min-inline-size: 0;
}
</style>
