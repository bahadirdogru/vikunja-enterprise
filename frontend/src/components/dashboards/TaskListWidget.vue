<script setup lang="ts">
import {computed} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'

import {tasksQuery, type TaskFilterParams} from '@/client/queries/tasks'
import {REPORT_STALE_TIME} from '@/client/queries/reports'
import type {TaskListConfig} from '@/helpers/dashboards'
import ChartState from '@/components/charts/ChartState.vue'
import TaskRow from './TaskRow.vue'

const props = defineProps<{
	config: TaskListConfig
}>()

const emit = defineEmits<{
	showAll: []
}>()

const {t} = useI18n({useScope: 'global'})

const limit = computed(() => Math.min(100, Math.max(1, props.config.limit ?? 10)))
const params = computed<TaskFilterParams>(() => ({
	filter: props.config.filter ?? '',
	sort_by: props.config.sort_by?.length ? props.config.sort_by : ['due_date', 'id'],
	order_by: props.config.order_by?.length ? props.config.order_by : ['asc', 'desc'],
	per_page: limit.value,
	expand: ['status'],
}))

const query = useQuery(computed(() => ({
	...tasksQuery({project: (props.config.project_id ?? 0) > 0 ? props.config.project_id : null, params: params.value}),
	staleTime: REPORT_STALE_TIME,
	refetchOnWindowFocus: true,
	meta: {handlesError: true},
})))

const tasks = computed(() => query.data.value?.items ?? [])
const total = computed(() => query.data.value?.total ?? tasks.value.length)
</script>

<template>
	<div class="task-list-widget">
		<ChartState
			v-if="query.isPending.value"
			kind="loading"
		/>
		<ChartState
			v-else-if="query.isError.value"
			kind="error"
		/>
		<ChartState
			v-else-if="tasks.length === 0"
			kind="empty"
			:message="t('dashboards.taskList.empty')"
		/>
		<template v-else>
			<ul class="task-list-widget__list">
				<TaskRow
					v-for="task in tasks"
					:key="task.id"
					:task="task"
				/>
			</ul>
			<button
				v-if="total > tasks.length"
				type="button"
				class="task-list-widget__all"
				@click="emit('showAll')"
			>
				{{ t('dashboards.taskList.showAll', {count: total}) }}
			</button>
		</template>
	</div>
</template>

<style lang="scss" scoped>
.task-list-widget {
	display: flex;
	flex-direction: column;
	block-size: 100%;
	min-block-size: 0;
}

.task-list-widget__list {
	flex: 1 1 auto;
	margin: 0;
	padding: 0;
	overflow-y: auto;
	list-style: none;
}

.task-list-widget__all {
	align-self: flex-start;
	margin-block-start: .4rem;
	padding: 0;
	border: 0;
	background: none;
	color: var(--link);
	font-size: .85rem;
	cursor: pointer;

	&:hover {
		text-decoration: underline;
	}
}
</style>
