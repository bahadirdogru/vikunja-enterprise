<script setup lang="ts">
import {computed, watch} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'
import {widgetErrorText} from '@/helpers/dashboards'

import {timeTrackingQuery} from '@/client/queries/reportWidgets'
import {formatSeconds} from '@/helpers/charts'
import {timeTrackingGroups} from '@/helpers/dashboardCharts'
import BarChart from '@/components/charts/BarChart.vue'
import ChartState from '@/components/charts/ChartState.vue'

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

const {t, te, locale} = useI18n({useScope: 'global'})

const query = useQuery(computed(() => timeTrackingQuery(props.config, props.enabled)))
watch(() => query.error.value, error => emit('error', error))

const report = computed(() => query.data.value)
const groups = computed(() => report.value ? timeTrackingGroups(report.value) : [])
const isDay = computed(() => report.value?.group_by === 'day')
const errorText = computed(() => {
	const error = query.error.value as {detail?: string, status?: number, code?: number} | null
	// An unlicensed feature answers 404 on every route of it.
	return error?.status === 404 ? t('dashboards.timeTracking.unavailable') : widgetErrorText(error, t, te)
})
const hasData = computed(() => (report.value?.seconds ?? 0) > 0)
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
			:message="t('dashboards.timeTracking.empty')"
		/>
		<template v-else>
			<p class="report-widget__note">
				{{ t('dashboards.timeTracking.total', {time: formatSeconds(report?.seconds ?? 0, locale), count: report?.entries ?? 0}, {plural: report?.entries ?? 0}) }}
				<template v-if="(report?.running ?? 0) > 0">
					· {{ t('dashboards.timeTracking.running', {count: report?.running ?? 0}, {plural: report?.running ?? 0}) }}
				</template>
			</p>
			<div class="report-widget__chart">
				<BarChart
					:groups="groups"
					:orientation="isDay ? 'vertical' : 'horizontal'"
					unit="seconds"
					:selectable="false"
					:multi-valued="report?.multi_valued"
					:show-legend="false"
					:title="title"
				/>
			</div>
			<p
				v-if="report?.truncated"
				class="report-widget__note"
			>
				{{ t('dashboards.timeTracking.truncated') }}
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
