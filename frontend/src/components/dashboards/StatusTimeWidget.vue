<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'

import {statusTimeQuery} from '@/client/queries/reportWidgets'
import {statusTimeGroups, STATUS_TIME_METRICS, type StatusTimeMetric} from '@/helpers/dashboardCharts'
import BarChart from '@/components/charts/BarChart.vue'
import ChartState from '@/components/charts/ChartState.vue'
import MetricToggle from './MetricToggle.vue'

const props = withDefaults(defineProps<{
	config: Record<string, unknown>
	title?: string
	enabled?: boolean
}>(), {
	title: undefined,
	enabled: true,
})

const emit = defineEmits<{
	error: [error: unknown]
}>()

const {t} = useI18n({useScope: 'global'})
// A per-viewer choice like the other display options: the stored config only says what to count.
const metric = ref<StatusTimeMetric>('average')

const query = useQuery(computed(() => statusTimeQuery(props.config, props.enabled)))
watch(() => query.error.value, error => emit('error', error))

const report = computed(() => query.data.value)
const groups = computed(() => report.value ? statusTimeGroups(report.value, metric.value) : [])
const errorText = computed(() => (query.error.value as {detail?: string} | null)?.detail)
const metricOptions = computed(() => STATUS_TIME_METRICS.map(m => ({value: m, label: t(`dashboards.statusTime.metrics.${m}`)})))
const hasData = computed(() => (report.value?.statuses.length ?? 0) > 0)
</script>

<template>
	<div class="report-widget">
		<ChartState
			v-if="query.isError.value && !report"
			kind="error"
			:message="errorText"
		/>
		<ChartState
			v-else-if="query.isPending.value && enabled"
			kind="loading"
		/>
		<ChartState
			v-else-if="!hasData"
			kind="empty"
			:message="t('dashboards.statusTime.empty')"
		/>
		<template v-else>
			<div class="report-widget__toolbar">
				<MetricToggle
					v-model="metric"
					:options="metricOptions"
					:label="t('dashboards.statusTime.metric')"
				/>
				<span class="report-widget__note">
					{{ t('dashboards.statusTime.tasks', {count: report?.tasks ?? 0}, {plural: report?.tasks ?? 0}) }}
				</span>
			</div>
			<div class="report-widget__chart">
				<BarChart
					:groups="groups"
					orientation="horizontal"
					:stacked="false"
					unit="seconds"
					:selectable="false"
					:multi-valued="report?.multi_valued"
					:show-legend="report?.group_by !== ''"
					:title="title"
				/>
			</div>
			<p
				v-if="report?.truncated"
				class="report-widget__note"
			>
				{{ t('dashboards.statusTime.truncated') }}
			</p>
		</template>
	</div>
</template>

<style lang="scss" scoped>
.report-widget {
	display: flex;
	flex-direction: column;
	gap: .35rem;
	block-size: 100%;
	min-block-size: 0;
}

.report-widget__toolbar {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: space-between;
	gap: .5rem;
}

.report-widget__chart {
	flex: 1 1 auto;
	min-block-size: 0;
}

.report-widget__note {
	margin: 0;
	color: var(--text-light);
	font-size: .75rem;
}
</style>
