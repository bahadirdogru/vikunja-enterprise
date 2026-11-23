<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'

import {workloadQuery} from '@/client/queries/reportWidgets'
import {workloadGroups, WORKLOAD_METRICS, type WorkloadMetric} from '@/helpers/dashboardCharts'
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
const metric = ref<WorkloadMetric>('count')

const query = useQuery(computed(() => workloadQuery(props.config, props.enabled)))
watch(() => query.error.value, error => emit('error', error))

const report = computed(() => query.data.value)
const hasEstimate = computed(() => (report.value?.estimate_field ?? '') !== '')
// Without an estimate field there is nothing to switch to.
const activeMetric = computed<WorkloadMetric>(() => hasEstimate.value ? metric.value : 'count')
const groups = computed(() => report.value
	? workloadGroups(report.value, activeMetric.value, {
		unassigned: t('dashboards.workload.unassigned'),
		buckets: Object.fromEntries(report.value.buckets.map(b => [b, t(`dashboards.workload.buckets.${b}`)])),
	})
	: [])
const errorText = computed(() => (query.error.value as {detail?: string} | null)?.detail)
const metricOptions = computed(() => WORKLOAD_METRICS.map(m => ({value: m, label: t(`dashboards.workload.metrics.${m}`)})))
const hasData = computed(() => (report.value?.totals.total ?? 0) > 0)
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
			:message="t('dashboards.workload.empty')"
		/>
		<template v-else>
			<div class="report-widget__toolbar">
				<MetricToggle
					v-if="hasEstimate"
					v-model="metric"
					:options="metricOptions"
					:label="t('dashboards.workload.metric')"
				/>
				<span class="report-widget__note">
					{{ t('dashboards.workload.open', {count: report?.totals.total ?? 0}, {plural: report?.totals.total ?? 0}) }}
				</span>
			</div>
			<div class="report-widget__chart">
				<BarChart
					:groups="groups"
					orientation="horizontal"
					stacked
					:unit="activeMetric === 'count' ? 'tasks' : ''"
					:selectable="false"
					multi-valued
					show-legend
					:title="title"
				/>
			</div>
			<p
				v-if="report?.truncated"
				class="report-widget__note"
			>
				{{ t('dashboards.workload.truncated') }}
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
