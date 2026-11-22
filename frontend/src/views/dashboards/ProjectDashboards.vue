<script setup lang="ts">
import {computed, watch} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'
import {useRouter} from 'vue-router'

import {dashboardsQuery} from '@/client/queries/dashboards'
import {useCreateDashboard} from '@/composables/useCreateDashboard'
import {useProject} from '@/composables/useProject'
import {useTitle} from '@/composables/useTitle'
import {PERMISSIONS} from '@/constants/permissions'
import XButton from '@/components/input/Button.vue'
import Loading from '@/components/misc/Loading.vue'
import DashboardList from './DashboardList.vue'

const props = defineProps<{
	projectId: number
}>()

const {t} = useI18n({useScope: 'global'})
const router = useRouter()
const {project} = useProject(() => props.projectId)
const query = useQuery(computed(() => dashboardsQuery(props.projectId)))
const creator = useCreateDashboard()
useTitle(() => t('dashboards.projectTitle', {project: project.value.title}))

const items = computed(() => query.data.value ?? [])
const isAdmin = computed(() => project.value.max_permission === PERMISSIONS.ADMIN)

// One dashboard needs no list in between.
watch(items, list => {
	if (query.isSuccess.value && list.length === 1) {
		router.replace({name: 'dashboards.view', params: {id: list[0]!.id}})
	}
}, {immediate: true})

async function createDefault() {
	try {
		const created = await creator.create({title: t('dashboards.defaultTitle', {project: project.value.title}), projectId: props.projectId})
		await router.push({name: 'dashboards.view', params: {id: created.id}})
	} catch {
		// The mutation already toasted it.
	}
}
</script>

<template>
	<div class="project-dashboards">
		<Loading
			v-if="query.isPending.value || (items.length === 1)"
			variant="small"
		/>
		<div
			v-else-if="items.length === 0"
			class="project-dashboards__empty"
		>
			<h1>{{ t('dashboards.projectTitle', {project: project.title}) }}</h1>
			<p>{{ isAdmin ? t('dashboards.projectEmptyAdmin') : t('dashboards.projectEmpty') }}</p>
			<XButton
				v-if="isAdmin"
				icon="chart-simple"
				:loading="creator.isPending.value"
				data-test="create-default"
				@click="createDefault"
			>
				{{ t('dashboards.createDefault') }}
			</XButton>
		</div>
		<DashboardList
			v-else
			:project-id="projectId"
		/>
	</div>
</template>

<style lang="scss" scoped>
.project-dashboards__empty {
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: .75rem;

	h1 {
		margin: 0;
		font-size: 1.6rem;
		font-weight: 700;
	}

	p {
		margin: 0;
		max-inline-size: 36rem;
		color: var(--text-light);
	}
}
</style>
