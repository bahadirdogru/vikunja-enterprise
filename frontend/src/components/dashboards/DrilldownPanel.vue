<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'

import {customFieldsQuery} from '@/client/queries/customFields'
import {reportOptionsQuery} from '@/client/queries/reports'
import {tasksQuery, type TaskFilterParams} from '@/client/queries/tasks'
import {userSearchQuery} from '@/client/queries/userSearch'
import {drilldownFilter, drilldownInputFor} from '@/helpers/dashboardDrilldown'
import {widgetProjectId} from '@/helpers/dashboards'
import type {ChartSelection} from '@/helpers/charts'
import ChartState from '@/components/charts/ChartState.vue'
import SidePanel from './SidePanel.vue'
import TaskRow from './TaskRow.vue'

const props = defineProps<{
	type: string
	config: Record<string, unknown>
	title: string
	selection: ChartSelection | null
}>()

const emit = defineEmits<{
	close: []
}>()

const {t} = useI18n({useScope: 'global'})
const page = ref(1)
const usernames = ref<Record<number, string>>({})
const cfOptionIds = ref<Record<string, number[]>>({})
const projectId = computed(() => widgetProjectId(props.type, props.config))

const baseInput = computed(() => drilldownInputFor({type: props.type, config: props.config}, props.selection ?? {}))
const result = computed(() => drilldownFilter({
	...baseInput.value,
	usernames: usernames.value,
	cfOptionIds: cfOptionIds.value,
	fieldTypes: fieldTypes.value,
}))

const dimensionNames = computed(() => [baseInput.value.groupBy, baseInput.value.thenBy].filter((d): d is string => !!d))
const needsOptions = computed(() => !result.value.ok && result.value.reason === 'needs_options')
const optionsQuery = useQuery(computed(() => ({
	...reportOptionsQuery(projectId.value),
	enabled: dimensionNames.value.some(d => d.startsWith('cf_')),
})))
const fieldTypes = computed(() => Object.fromEntries(
	(optionsQuery.data.value?.dimensions ?? []).filter(d => d.field_type).map(d => [d.name, d.field_type]),
))

// Assignees and creators are filtered by username but reported by id: look the person up by the name the chart shows.
const unresolvedUser = computed(() => !result.value.ok && result.value.reason === 'needs_username' ? result.value : null)
const userDimensions = ['assignee', 'created_by']
const userLabel = computed(() => {
	const {groupBy, group, child} = baseInput.value
	return (groupBy && userDimensions.includes(groupBy) ? group?.label : child?.label) ?? ''
})
const userQuery = useQuery(computed(() => ({
	...userSearchQuery(userLabel.value),
	enabled: unresolvedUser.value !== null,
})))
watch(() => [userQuery.data.value, unresolvedUser.value] as const, ([users, unresolved]) => {
	if (!unresolved || !users) {
		return
	}
	const match = users.find(u => u.id === unresolved.userId)
	if (match?.username) {
		usernames.value = {...usernames.value, [unresolved.userId!]: match.username}
	}
}, {immediate: true})
const userLookupFailed = computed(() => unresolvedUser.value !== null && (userQuery.isError.value || (userQuery.isSuccess.value && !userQuery.data.value?.some(u => u.id === unresolvedUser.value?.userId))))

const noneField = computed(() => baseInput.value.groupBy?.startsWith('cf_') ? baseInput.value.groupBy : baseInput.value.thenBy)
const fieldProject = computed(() => optionsQuery.data.value?.dimensions.find(d => d.name === noneField.value)?.project_id ?? projectId.value)
const fieldsQuery = useQuery(computed(() => ({
	...customFieldsQuery(fieldProject.value),
	enabled: needsOptions.value && fieldProject.value > 0,
})))
watch(() => [fieldsQuery.data.value, noneField.value] as const, ([fields, name]) => {
	if (!fields || !name) {
		return
	}
	const id = Number(name.slice(3))
	const field = fields.find(f => f.id === id)
	if (field) {
		cfOptionIds.value = {...cfOptionIds.value, [name]: field.options.map(o => o.id)}
	}
}, {immediate: true})

const ready = computed(() => result.value.ok)
const filter = computed(() => result.value.ok ? result.value.filter : '')
const includeNulls = computed(() => result.value.ok && result.value.includeNulls)
const params = computed<TaskFilterParams>(() => ({
	filter: filter.value,
	filter_include_nulls: includeNulls.value,
	sort_by: ['done', 'due_date', 'id'],
	order_by: ['asc', 'asc', 'desc'],
	per_page: 25,
	expand: ['status'],
}))
const tasks = useQuery(computed(() => ({
	...tasksQuery({project: projectId.value > 0 ? projectId.value : null, params: params.value}, page.value),
	enabled: ready.value,
	meta: {handlesError: true},
})))

watch(filter, () => {
	page.value = 1
})

const items = computed(() => tasks.data.value?.items ?? [])
const total = computed(() => tasks.data.value?.total ?? 0)
const totalPages = computed(() => tasks.data.value?.total_pages ?? 1)
const conditions = computed(() => result.value.ok ? result.value.parts : [])
const blocked = computed(() => {
	if (result.value.ok || unresolvedUser.value || (needsOptions.value && fieldsQuery.isFetching.value)) {
		return null
	}
	return t(`dashboards.drilldown.blocked.${result.value.reason}`)
})
</script>

<template>
	<SidePanel
		:title="t('dashboards.drilldown.title', {title})"
		@close="emit('close')"
	>
		<p
			v-if="conditions.length"
			class="drilldown__conditions"
		>
			<span
				v-for="condition in conditions"
				:key="condition"
				class="tag"
			>{{ condition }}</span>
		</p>
		<p
			v-if="ready && filter"
			class="drilldown__filter"
		>
			<code>{{ filter }}</code>
		</p>
		<ChartState
			v-if="blocked || userLookupFailed"
			kind="error"
			:message="blocked ?? t('dashboards.drilldown.blocked.needs_username')"
		/>
		<ChartState
			v-else-if="!ready || tasks.isPending.value"
			kind="loading"
		/>
		<ChartState
			v-else-if="tasks.isError.value"
			kind="error"
		/>
		<ChartState
			v-else-if="items.length === 0"
			kind="empty"
			:message="t('dashboards.drilldown.empty')"
		/>
		<template v-else>
			<p
				class="drilldown__count"
				data-test="drilldown-count"
			>
				{{ t('charts.tasks', {count: total}, {plural: total}) }}
			</p>
			<ul class="drilldown__list">
				<TaskRow
					v-for="task in items"
					:key="task.id"
					:task="task"
				/>
			</ul>
			<nav
				v-if="totalPages > 1"
				class="drilldown__pages"
				:aria-label="t('dashboards.drilldown.pages')"
			>
				<button
					type="button"
					class="button is-small"
					:aria-disabled="page <= 1"
					@click="page > 1 && page--"
				>
					{{ t('dashboards.drilldown.previous') }}
				</button>
				<span>{{ page }} / {{ totalPages }}</span>
				<button
					type="button"
					class="button is-small"
					:aria-disabled="page >= totalPages"
					@click="page < totalPages && page++"
				>
					{{ t('dashboards.drilldown.next') }}
				</button>
			</nav>
		</template>
	</SidePanel>
</template>

<style lang="scss" scoped>
.drilldown__conditions {
	display: flex;
	flex-wrap: wrap;
	gap: .35rem;
	margin: 0 0 .5rem;
}

.drilldown__filter {
	margin: 0 0 .75rem;
	overflow-x: auto;
	font-size: .8rem;
	color: var(--text-light);
}

.drilldown__count {
	margin: 0 0 .25rem;
	font-weight: 600;
}

.drilldown__list {
	margin: 0;
	padding: 0;
	list-style: none;
}

.drilldown__pages {
	display: flex;
	align-items: center;
	justify-content: center;
	gap: 1rem;
	margin-block-start: .75rem;
}
</style>
