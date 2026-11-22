<script setup lang="ts">
import {computed, nextTick, ref} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'
import {useRouter} from 'vue-router'

import {
	dashboardsQuery,
	useDeleteDashboardMutation,
	useUpdateDashboardMutation,
	type DashboardResponse,
} from '@/client/queries/dashboards'
import {useCreateDashboard} from '@/composables/useCreateDashboard'
import {useProjects} from '@/composables/useProjects'
import {useTitle} from '@/composables/useTitle'
import {PERMISSIONS} from '@/constants/permissions'
import XButton from '@/components/input/Button.vue'
import FormField from '@/components/input/FormField.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import Loading from '@/components/misc/Loading.vue'
import Modal from '@/components/misc/Modal.vue'

const props = withDefaults(defineProps<{
	// Only the dashboards of this project; 0 lists every dashboard the user can read.
	projectId?: number
}>(), {
	projectId: 0,
})

const {t} = useI18n({useScope: 'global'})
const router = useRouter()
const projectStore = useProjects()
const query = useQuery(computed(() => dashboardsQuery(props.projectId)))
const updateDashboard = useUpdateDashboardMutation()
const deleteDashboard = useDeleteDashboardMutation()
const creator = useCreateDashboard()
useTitle(() => t('dashboards.title'))

const dashboards = computed(() => query.data.value ?? [])

function projectTitle(id: number): string {
	return projectStore.projects[id]?.title ?? ''
}

// Personal dashboards are always the viewer's own; project dashboards are changed by project admins.
function canWrite(dashboard: DashboardResponse): boolean {
	return dashboard.project_id === 0 || projectStore.projects[dashboard.project_id]?.max_permission === PERMISSIONS.ADMIN
}

const groups = computed(() => {
	const personal = dashboards.value.filter(d => d.project_id === 0)
	const byProject = new Map<number, DashboardResponse[]>()
	for (const dashboard of dashboards.value.filter(d => d.project_id > 0)) {
		byProject.set(dashboard.project_id, [...(byProject.get(dashboard.project_id) ?? []), dashboard])
	}
	return [
		...(personal.length ? [{key: 'personal', title: t('dashboards.personal'), items: personal}] : []),
		...[...byProject.entries()]
			.map(([id, items]) => ({key: `project-${id}`, title: projectTitle(id) || t('dashboards.unknownProject'), items}))
			.sort((a, b) => a.title.localeCompare(b.title)),
	]
})

const creating = ref(false)
const newTitle = ref('')
// eslint-disable-next-line vue/no-setup-props-reactivity-loss -- the initial scope only
const newScope = ref<number>(props.projectId)
const createError = ref('')
const titleInput = ref<InstanceType<typeof FormField> | null>(null)

const scopeOptions = computed(() => [
	{value: 0, label: t('dashboards.personal')},
	...projectStore.projectsArray
		.filter(p => p.id > 0 && !p.is_archived && p.max_permission === PERMISSIONS.ADMIN)
		.map(p => ({value: p.id, label: p.title})),
])

async function openCreate() {
	creating.value = true
	createError.value = ''
	await nextTick()
	titleInput.value?.focus()
}

async function create() {
	const title = newTitle.value.trim()
	if (!title) {
		createError.value = t('dashboards.titleRequired')
		return
	}
	createError.value = ''
	try {
		const created = await creator.create({title, projectId: Number(newScope.value)})
		creating.value = false
		newTitle.value = ''
		await router.push({name: 'dashboards.view', params: {id: created.id}})
	} catch {
		// The mutation already toasted it.
	}
}

const renaming = ref<{id: number, title: string} | null>(null)
const root = ref<HTMLElement | null>(null)

async function startRename(dashboard: DashboardResponse) {
	renaming.value = {id: dashboard.id, title: dashboard.title}
	await nextTick()
	root.value?.querySelector<HTMLInputElement>('.dashboard-list__rename input')?.focus()
}

async function rename(dashboard: DashboardResponse) {
	const title = renaming.value?.title.trim()
	renaming.value = null
	if (!title || title === dashboard.title) {
		return
	}
	try {
		await updateDashboard.mutateAsync({
			id: dashboard.id,
			title,
			description: dashboard.description,
			position: dashboard.position,
		})
	} catch {
		// The mutation already toasted it.
	}
}

const removing = ref<DashboardResponse | null>(null)

async function confirmDelete() {
	const target = removing.value
	removing.value = null
	if (!target) {
		return
	}
	try {
		await deleteDashboard.mutateAsync(target.id)
	} catch {
		// The mutation already toasted it.
	}
}
</script>

<template>
	<div
		ref="root"
		class="dashboard-list"
	>
		<header class="dashboard-list__header">
			<h1>{{ projectId > 0 ? t('dashboards.projectTitle', {project: projectTitle(projectId)}) : t('dashboards.title') }}</h1>
			<XButton
				icon="plus"
				data-test="new-dashboard"
				@click="openCreate"
			>
				{{ t('dashboards.new') }}
			</XButton>
		</header>

		<form
			v-if="creating"
			class="dashboard-list__create"
			data-test="create-form"
			@submit.prevent="create"
		>
			<FormField
				ref="titleInput"
				v-model="newTitle"
				:label="t('dashboards.nameLabel')"
				:error="createError"
				maxlength="250"
			/>
			<FormField
				v-if="projectId === 0"
				v-slot="{id}"
				:label="t('dashboards.scopeLabel')"
			>
				<FormSelect
					:id="id"
					v-model="newScope"
					:options="scopeOptions"
				/>
			</FormField>
			<p class="help">
				{{ t('dashboards.createHint') }}
			</p>
			<div class="dashboard-list__create-actions">
				<XButton
					variant="tertiary"
					@click="creating = false"
				>
					{{ t('misc.cancel') }}
				</XButton>
				<XButton
					type="submit"
					:loading="creator.isPending.value"
					data-test="create-submit"
					@click="create"
				>
					{{ t('misc.create') }}
				</XButton>
			</div>
		</form>

		<Loading
			v-if="query.isPending.value"
			variant="small"
		/>
		<div
			v-else-if="query.isError.value"
			class="notification is-danger"
			role="alert"
		>
			{{ t('dashboards.loadError') }}
		</div>
		<p
			v-else-if="groups.length === 0"
			class="dashboard-list__empty"
		>
			{{ t('dashboards.empty') }}
		</p>
		<section
			v-for="group in groups"
			v-else
			:key="group.key"
			class="dashboard-list__group"
			:aria-labelledby="`group-${group.key}`"
		>
			<h2
				:id="`group-${group.key}`"
				class="dashboard-list__group-title"
			>
				{{ group.title }}
			</h2>
			<ul class="dashboard-list__items">
				<li
					v-for="dashboard in group.items"
					:key="dashboard.id"
					class="dashboard-list__item"
					data-test="dashboard-item"
				>
					<form
						v-if="renaming?.id === dashboard.id"
						class="dashboard-list__rename"
						@submit.prevent="rename(dashboard)"
					>
						<input
							v-model="renaming.title"
							class="input"
							maxlength="250"
							:aria-label="t('dashboards.nameLabel')"
							@keydown.esc="renaming = null"
							@blur="rename(dashboard)"
						>
					</form>
					<RouterLink
						v-else
						:to="{name: 'dashboards.view', params: {id: dashboard.id}}"
						class="dashboard-list__link"
					>
						<strong>{{ dashboard.title }}</strong>
						<span>{{ t('dashboards.widgetCount', {count: dashboard.widget_count}, {plural: dashboard.widget_count}) }}</span>
					</RouterLink>
					<span
						v-if="canWrite(dashboard)"
						class="dashboard-list__actions"
					>
						<button
							type="button"
							class="dashboard-list__action"
							:aria-label="t('dashboards.rename', {title: dashboard.title})"
							@click="startRename(dashboard)"
						>
							<Icon icon="pen" />
						</button>
						<button
							type="button"
							class="dashboard-list__action has-text-danger"
							:aria-label="t('dashboards.delete', {title: dashboard.title})"
							@click="removing = dashboard"
						>
							<Icon icon="trash-alt" />
						</button>
					</span>
				</li>
			</ul>
		</section>

		<Modal
			:enabled="removing !== null"
			@close="removing = null"
			@submit="confirmDelete"
		>
			<template #header>
				<span>{{ t('dashboards.deleteTitle') }}</span>
			</template>
			<template #text>
				<p>{{ t('dashboards.deleteText', {title: removing?.title ?? ''}) }}</p>
			</template>
		</Modal>
	</div>
</template>

<style lang="scss" scoped>
.dashboard-list__header {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: space-between;
	gap: .75rem;
	margin-block-end: 1rem;

	h1 {
		margin: 0;
		font-size: 1.6rem;
		font-weight: 700;
	}
}

.dashboard-list__create {
	max-inline-size: 32rem;
	margin-block-end: 1.5rem;
	padding: 1rem;
	border: 1px solid var(--grey-200);
	border-radius: var(--input-radius);
}

.dashboard-list__create-actions {
	display: flex;
	justify-content: flex-end;
	gap: .5rem;
	margin-block-start: .75rem;
}

.dashboard-list__empty {
	color: var(--text-light);
}

.dashboard-list__group {
	margin-block-end: 1.5rem;
}

.dashboard-list__group-title {
	margin: 0 0 .5rem;
	font-size: 1rem;
	font-weight: 600;
	color: var(--text-light);
}

.dashboard-list__items {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(16rem, 1fr));
	gap: .75rem;
	margin: 0;
	padding: 0;
	list-style: none;
}

.dashboard-list__item {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: .5rem;
	padding: .75rem 1rem;
	border: 1px solid var(--grey-200);
	border-radius: var(--input-radius);
	background: var(--scheme-main);
	box-shadow: var(--shadow-xs);
}

.dashboard-list__link {
	display: flex;
	flex-direction: column;
	flex: 1;
	min-inline-size: 0;
	color: var(--text);

	strong {
		overflow-wrap: anywhere;
		color: var(--text-strong);
	}

	span {
		color: var(--text-light);
		font-size: .8rem;
	}

	&:hover strong {
		color: var(--primary);
	}
}

.dashboard-list__rename {
	flex: 1;
}

.dashboard-list__actions {
	display: flex;
	gap: .25rem;
}

.dashboard-list__action {
	padding: .35rem;
	border: 0;
	border-radius: var(--input-radius);
	background: none;
	color: var(--text-light);
	cursor: pointer;

	&:hover {
		background: var(--grey-100);
	}
}
</style>
