<script setup lang="ts">
import {computed, onBeforeUnmount, reactive, ref, watch} from 'vue'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'
import {onBeforeRouteLeave} from 'vue-router'

import {
	dashboardQuery,
	useSaveLayoutMutation,
	useUpdateDashboardMutation,
	type DashboardWidgetResponse,
} from '@/client/queries/dashboards'
import {useProjects} from '@/composables/useProjects'
import {useTitle} from '@/composables/useTitle'
import {loadDisplay, saveDisplay, type WidgetDisplay} from '@/helpers/dashboardDisplay'
import {layoutOf, WIDGET_SIZES, type WidgetDraft, type WidgetType} from '@/helpers/dashboards'
import {placeNew, sameLayout, type LayoutItem} from '@/helpers/dashboardLayout'
import type {ChartSelection} from '@/helpers/charts'
import DashboardGrid from '@/components/dashboards/DashboardGrid.vue'
import DrilldownPanel from '@/components/dashboards/DrilldownPanel.vue'
import WidgetEditor, {type EditorResult} from '@/components/dashboards/WidgetEditor.vue'
import type {DrillRequest} from '@/components/dashboards/WidgetView.vue'
import Modal from '@/components/misc/Modal.vue'
import XButton from '@/components/input/Button.vue'
import Loading from '@/components/misc/Loading.vue'

const props = defineProps<{
	dashboardId: number
}>()

const LAYOUT_DELAY = 700

const {t} = useI18n({useScope: 'global'})
const projectStore = useProjects()
const query = useQuery(computed(() => ({...dashboardQuery(props.dashboardId), meta: {handlesError: true}})))
const updateDashboard = useUpdateDashboardMutation()
const saveLayout = useSaveLayoutMutation()

const dashboard = computed(() => query.data.value)
const canWrite = computed(() => (dashboard.value?.max_permission ?? 0) >= 2)
const project = computed(() => dashboard.value && dashboard.value.project_id > 0 ? projectStore.projects[dashboard.value.project_id] : null)
useTitle(() => dashboard.value?.title ?? t('dashboards.title'))

const widgets = ref<DashboardWidgetResponse[]>([])
const displays = reactive<Record<number, WidgetDisplay>>({})
const editing = ref(false)

type SaveState = 'idle' | 'unsaved' | 'saving' | 'saved' | 'failed'
const saveState = ref<SaveState>('idle')
const history = ref<LayoutItem[][]>([])
let timer: ReturnType<typeof setTimeout> | null = null
let saved: LayoutItem[] = []

// The working copy follows the server unless the user has changes that were not saved yet.
watch(dashboard, value => {
	if (!value || saveState.value === 'unsaved' || saveState.value === 'saving' || saveState.value === 'failed') {
		return
	}
	widgets.value = value.widgets.map(w => ({...w}))
	saved = layoutOf(value.widgets)
	for (const widget of value.widgets) {
		displays[widget.id] = loadDisplay(value.id, widget.id)
	}
}, {immediate: true})

watch(() => props.dashboardId, () => {
	editing.value = false
	history.value = []
	saveState.value = 'idle'
})

function applyLayout(items: LayoutItem[]) {
	const byId = new Map(items.map(item => [item.id, item]))
	widgets.value = widgets.value.map(w => ({...w, ...(byId.get(w.id) ?? {})}))
}

function onLayout(items: LayoutItem[]) {
	history.value = [...history.value.slice(-19), layoutOf(widgets.value)]
	applyLayout(items)
	scheduleLayoutSave()
}

function undo() {
	const previous = history.value.at(-1)
	if (!previous) {
		return
	}
	history.value = history.value.slice(0, -1)
	applyLayout(previous)
	scheduleLayoutSave()
}

function scheduleLayoutSave() {
	saveState.value = 'unsaved'
	if (timer) {
		clearTimeout(timer)
	}
	timer = setTimeout(flushLayout, LAYOUT_DELAY)
}

async function flushLayout() {
	if (timer) {
		clearTimeout(timer)
		timer = null
	}
	const items = layoutOf(widgets.value)
	if (sameLayout(items, saved)) {
		saveState.value = 'idle'
		return
	}
	saveState.value = 'saving'
	try {
		await saveLayout.mutateAsync({id: props.dashboardId, items})
		saved = items
		// A move made while the request ran is saved by its own timer.
		saveState.value = timer ? 'unsaved' : 'saved'
	} catch {
		saveState.value = 'failed'
	}
}

onBeforeRouteLeave(async () => {
	if (saveState.value === 'unsaved') {
		await flushLayout()
	}
})
onBeforeUnmount(() => {
	if (timer) {
		clearTimeout(timer)
	}
})

function onDisplay(id: number, display: WidgetDisplay) {
	displays[id] = display
	saveDisplay(props.dashboardId, id, display)
}

// Widgets are added, changed and removed through the document, which carries the current layout with it.
const editor = ref<{widget: (WidgetDraft & {display?: WidgetDisplay}) | null} | null>(null)
const editorError = ref<unknown>(null)
const removing = ref<number | null>(null)

function widgetPayload(list: DashboardWidgetResponse[]) {
	return list.map(w => ({id: w.id, type: w.type, title: w.title, config: w.config, x: w.x, y: w.y, w: w.w, h: w.h}))
}

async function saveDocument(list: {id?: number, type: string, title: string, config: Record<string, unknown>, x: number, y: number, w: number, h: number}[]) {
	const current = dashboard.value
	if (!current) {
		throw new Error('no dashboard')
	}
	if (timer) {
		clearTimeout(timer)
		timer = null
	}
	saveState.value = 'saving'
	try {
		const result = await updateDashboard.mutateAsync({
			id: current.id,
			title: current.title,
			description: current.description,
			position: current.position,
			widgets: list,
		})
		widgets.value = result.widgets.map(w => ({...w}))
		saved = layoutOf(result.widgets)
		saveState.value = 'idle'
		history.value = []
		return result
	} catch (error) {
		saveState.value = layoutOf(widgets.value).length && !sameLayout(layoutOf(widgets.value), saved) ? 'unsaved' : 'idle'
		throw error
	}
}

function openAdd() {
	editorError.value = null
	editor.value = {widget: null}
}

function openEdit(id: number) {
	const widget = widgets.value.find(w => w.id === id)
	if (!widget || !(widget.type in WIDGET_SIZES)) {
		return
	}
	editorError.value = null
	editor.value = {widget: {...widget, type: widget.type as WidgetType, display: displays[id] ?? {}}}
}

async function onEditorSave(result: EditorResult) {
	editorError.value = null
	const known = new Set(widgets.value.map(w => w.id))
	const list = widgetPayload(widgets.value)
	if (result.id) {
		const index = list.findIndex(w => w.id === result.id)
		if (index >= 0) {
			list[index] = {...list[index]!, type: result.type, title: result.title, config: result.config}
		}
	} else {
		const size = WIDGET_SIZES[result.type]
		const spot = placeNew(layoutOf(widgets.value), size.w, size.h)
		list.push({id: 0, type: result.type, title: result.title, config: result.config, ...spot})
	}
	try {
		const saved = await saveDocument(list)
		const id = result.id ?? saved.widgets.find(w => !known.has(w.id))?.id
		if (id) {
			displays[id] = result.display
			saveDisplay(props.dashboardId, id, result.display)
		}
		editor.value = null
	} catch (error) {
		editorError.value = error
	}
}

async function duplicate(id: number) {
	const source = widgets.value.find(w => w.id === id)
	if (!source) {
		return
	}
	const spot = placeNew(layoutOf(widgets.value), source.w, source.h)
	try {
		await saveDocument([
			...widgetPayload(widgets.value),
			{id: 0, type: source.type, title: source.title ? `${source.title} ${t('dashboards.widget.copySuffix')}` : '', config: JSON.parse(JSON.stringify(source.config)), ...spot},
		])
	} catch {
		// The mutation already toasted it.
	}
}

async function confirmRemove() {
	const id = removing.value
	removing.value = null
	if (id === null) {
		return
	}
	try {
		await saveDocument(widgetPayload(widgets.value.filter(w => w.id !== id)))
	} catch {
		// The mutation already toasted it.
	}
}

const drill = ref<{widgetId: number, selection: ChartSelection | null} | null>(null)
const drillWidget = computed(() => widgets.value.find(w => w.id === drill.value?.widgetId))

function onDrill(id: number, request: DrillRequest) {
	drill.value = {widgetId: id, selection: request.selection}
}

const saveLabel = computed(() => ({
	idle: '',
	unsaved: t('dashboards.save.unsaved'),
	saving: t('dashboards.save.saving'),
	saved: t('dashboards.save.saved'),
	failed: t('dashboards.save.failed'),
}[saveState.value]))
</script>

<template>
	<div class="dashboard-view">
		<Loading
			v-if="query.isPending.value"
			variant="small"
		/>
		<div
			v-else-if="query.isError.value || !dashboard"
			class="notification is-danger"
			role="alert"
		>
			{{ t('dashboards.loadError') }}
		</div>
		<template v-else>
			<header class="dashboard-view__header">
				<div class="dashboard-view__titles">
					<h1 class="dashboard-view__title">
						{{ dashboard.title }}
					</h1>
					<p class="dashboard-view__meta">
						<RouterLink
							v-if="project"
							:to="{name: 'project.index', params: {projectId: project.id}}"
						>
							{{ project.title }}
						</RouterLink>
						<span v-else>{{ t('dashboards.personal') }}</span>
						<span v-if="dashboard.description"> · {{ dashboard.description }}</span>
					</p>
				</div>
				<div class="dashboard-view__actions">
					<span
						v-if="saveLabel"
						class="dashboard-view__state"
						:class="`is-${saveState}`"
						role="status"
						data-test="save-state"
					>
						{{ saveLabel }}
						<button
							v-if="saveState === 'failed'"
							type="button"
							class="dashboard-view__retry"
							@click="flushLayout"
						>
							{{ t('dashboards.save.retry') }}
						</button>
					</span>
					<RouterLink
						:to="{name: 'dashboards.index'}"
						class="button is-small"
					>
						{{ t('dashboards.allDashboards') }}
					</RouterLink>
					<template v-if="canWrite">
						<XButton
							v-if="editing"
							variant="secondary"
							icon="undo"
							:aria-disabled="history.length === 0"
							@click="undo"
						>
							{{ t('dashboards.undo') }}
						</XButton>
						<XButton
							v-if="editing"
							icon="plus"
							data-test="add-widget"
							@click="openAdd"
						>
							{{ t('dashboards.addWidget') }}
						</XButton>
						<XButton
							:variant="editing ? 'primary' : 'secondary'"
							:icon="editing ? 'check' : 'pen'"
							data-test="toggle-edit"
							:aria-pressed="editing"
							@click="editing = !editing"
						>
							{{ editing ? t('dashboards.doneEditing') : t('dashboards.edit') }}
						</XButton>
					</template>
				</div>
			</header>

			<p
				v-if="editing"
				class="dashboard-view__hint"
			>
				{{ t('dashboards.editHint') }}
			</p>

			<div
				v-if="widgets.length === 0"
				class="dashboard-view__empty"
			>
				<p>{{ t('dashboards.noWidgets') }}</p>
				<XButton
					v-if="canWrite"
					icon="plus"
					@click="editing = true; openAdd()"
				>
					{{ t('dashboards.addWidget') }}
				</XButton>
			</div>
			<DashboardGrid
				v-else
				:widgets="widgets"
				:editing="editing"
				:can-write="canWrite"
				:displays="displays"
				@layout="onLayout"
				@edit="openEdit"
				@duplicate="duplicate"
				@remove="removing = $event"
				@display="onDisplay"
				@drill="onDrill"
			/>
		</template>

		<WidgetEditor
			v-if="editor && dashboard"
			:widget="editor.widget"
			:dashboard-project-id="dashboard.project_id"
			:saving="updateDashboard.isPending.value"
			:save-error="editorError"
			@save="onEditorSave"
			@close="editor = null"
		/>
		<DrilldownPanel
			v-if="drill && drillWidget"
			:type="drillWidget.type"
			:config="drillWidget.config"
			:title="drillWidget.title || t(`dashboards.types.${drillWidget.type}.name`)"
			:selection="drill.selection"
			@close="drill = null"
		/>
		<Modal
			:enabled="removing !== null"
			@close="removing = null"
			@submit="confirmRemove"
		>
			<template #header>
				<span>{{ t('dashboards.widget.removeTitle') }}</span>
			</template>
			<template #text>
				<p>{{ t('dashboards.widget.removeText') }}</p>
			</template>
		</Modal>
	</div>
</template>

<style lang="scss" scoped>
.dashboard-view__header {
	display: flex;
	flex-wrap: wrap;
	align-items: flex-end;
	justify-content: space-between;
	gap: .75rem 1rem;
	margin-block-end: 1rem;
}

.dashboard-view__title {
	margin: 0;
	font-size: 1.6rem;
	font-weight: 700;
	color: var(--text-strong);
	overflow-wrap: anywhere;
}

.dashboard-view__meta {
	margin: 0;
	color: var(--text-light);
	font-size: .9rem;
}

.dashboard-view__actions {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: .5rem;
}

.dashboard-view__state {
	font-size: .85rem;
	color: var(--text-light);

	&.is-failed {
		color: var(--danger);
		font-weight: 600;
	}

	&.is-saved {
		color: var(--success);
	}
}

.dashboard-view__retry {
	margin-inline-start: .5rem;
	padding: 0;
	border: 0;
	background: none;
	color: var(--link);
	text-decoration: underline;
	cursor: pointer;
}

.dashboard-view__hint {
	margin: 0 0 .75rem;
	color: var(--text-light);
	font-size: .85rem;
}

.dashboard-view__empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 1rem;
	padding: 3rem 1rem;
	border: 1px dashed var(--grey-300);
	border-radius: var(--input-radius);
	color: var(--text-light);
}
</style>
