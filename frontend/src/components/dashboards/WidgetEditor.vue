<script setup lang="ts">
import {computed, reactive, ref, watch} from 'vue'
import {refDebounced} from '@vueuse/core'
import {useQuery} from '@tanstack/vue-query'
import {useI18n} from 'vue-i18n'

import {reportOptionsQuery, type ReportOptionResponse} from '@/client/queries/reports'
import {useProjects} from '@/composables/useProjects'
import {
	configFromDraft,
	draftFromWidget,
	draftProblems,
	emptyDraft,
	measureFields,
	TASK_LIST_SORT_FIELDS,
	type EditorDraft,
} from '@/helpers/dashboardEditor'
import type {WidgetDisplay} from '@/helpers/dashboardDisplay'
import {
	RANGE_PRESETS,
	serverFieldError,
	type ServerFieldError,
	type WidgetDraft,
	type WidgetType,
} from '@/helpers/dashboards'
import FilterEditor from '@/components/input/filter/FilterEditor.vue'
import FormField from '@/components/input/FormField.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import XButton from '@/components/input/Button.vue'
import Editor from '@/components/input/AsyncEditor'
import SidePanel from './SidePanel.vue'
import WidgetTypePicker from './WidgetTypePicker.vue'
import WidgetView from './WidgetView.vue'

export type EditorResult = {
	id?: number
	type: WidgetType
	title: string
	config: Record<string, unknown>
	display: WidgetDisplay
}

const props = defineProps<{
	// null starts a new widget at the gallery.
	widget: (WidgetDraft & {display?: WidgetDisplay}) | null
	// The project of a project dashboard; 0 on a personal one.
	dashboardProjectId: number
	saving?: boolean
	// The last failed save, to show its validation error on the control it names.
	saveError?: unknown
}>()

const emit = defineEmits<{
	save: [result: EditorResult]
	close: []
}>()

const {t, te} = useI18n({useScope: 'global'})
const projectStore = useProjects()

const isNew = computed(() => props.widget === null)
/* eslint-disable vue/no-setup-props-reactivity-loss -- the form is a draft seeded once, a background refetch must not replace it */
const draft = reactive<EditorDraft>(props.widget
	? {...draftFromWidget(props.widget), display: {...(props.widget.display ?? {})}}
	: emptyDraft('number', props.dashboardProjectId))
const step = ref<'type' | 'form'>(props.widget ? 'form' : 'type')
/* eslint-enable vue/no-setup-props-reactivity-loss */
const submitted = ref(false)
const previewErrors = reactive<Record<string, string>>({})
const serverErrors = reactive<Record<string, string>>({})

function pickType(type: WidgetType) {
	const next = emptyDraft(type, props.dashboardProjectId)
	Object.assign(draft, next)
	step.value = 'form'
}

const scopeProjectId = computed(() => props.dashboardProjectId > 0 ? props.dashboardProjectId : draft.projectId)
const options = useQuery(computed(() => reportOptionsQuery(scopeProjectId.value)))
const opts = computed(() => options.data.value)

function label(option: {name: string, title_key: string, label: string}): string {
	if (option.name.startsWith('cf_') || !te(option.title_key)) {
		return option.label || option.name
	}
	return t(option.title_key)
}

const dimensionOptions = computed(() => [
	{value: '', label: t('dashboards.editor.none')},
	...(opts.value?.dimensions ?? []).map((d: ReportOptionResponse) => ({value: d.name, label: label(d)})),
])
const thenByOptions = computed(() => dimensionOptions.value.filter(o => o.value === '' || o.value !== draft.groupBy))
const measureOptions = computed(() => (opts.value?.measures ?? []).map(m => ({value: m.name, label: label(m)})))
const fieldOptions = computed(() => [
	{value: '', label: t('dashboards.editor.choose')},
	...(opts.value ? measureFields(opts.value, draft.measure).map(f => ({value: f.name, label: label(f as ReportOptionResponse)})) : []),
])
const timeFieldOptions = computed(() => (opts.value?.time_fields ?? []).map(f => ({value: f.name, label: label(f)})))
const bucketOptions = computed(() => (opts.value?.buckets ?? ['day', 'week', 'month']).map(b => ({value: b, label: t(`dashboards.buckets.${b}`)})))
const presetOptions = computed(() => [
	...(draft.type === 'status_time' ? [{value: 'all', label: t('dashboards.ranges.all')}] : []),
	...RANGE_PRESETS.map(p => ({value: p, label: t(`dashboards.ranges.${p}`)})),
	{value: 'custom', label: t('dashboards.ranges.custom')},
])
const statusGroupOptions = computed(() => [
	{value: '', label: t('dashboards.editor.none')},
	{value: 'assignee', label: t('reports.dimension.assignee')},
	{value: 'label', label: t('reports.dimension.label')},
])
const trackedGroupOptions = computed(() => ['user', 'project', 'label', 'day'].map(g => ({value: g, label: t(`dashboards.timeTracking.groups.${g}`)})))
// An estimate is a number the people of the project fill in: only numeric custom fields are offered.
const estimateOptions = computed(() => [
	{value: '', label: t('dashboards.editor.none')},
	...(opts.value ? measureFields(opts.value, 'sum').filter(f => f.name.startsWith('cf_')).map(f => ({value: f.name, label: label(f as ReportOptionResponse)})) : []),
])
const projectOptions = computed(() => [
	{value: 0, label: t('dashboards.editor.allProjects')},
	...projectStore.projectsArray.filter(p => p.id > 0 && !p.is_archived).map(p => ({value: p.id, label: p.title})),
])
const sortOptions = computed(() => TASK_LIST_SORT_FIELDS.map(f => ({value: f, label: t(`dashboards.sort.${f}`)})))
const orientationOptions = computed(() => [
	{value: '', label: t('dashboards.editor.orientationAuto')},
	{value: 'vertical', label: t('dashboards.editor.vertical')},
	{value: 'horizontal', label: t('dashboards.editor.horizontal')},
])

// A measure and the field it works on go together; switching to count drops the field.
watch(() => draft.measure, measure => {
	if (measure === 'count') {
		draft.field = ''
	} else if (opts.value && !measureFields(opts.value, measure).some(f => f.name === draft.field)) {
		draft.field = ''
	}
})
watch(() => draft.groupBy, groupBy => {
	if (draft.thenBy === groupBy) {
		draft.thenBy = ''
	}
})
// Controls that the new scope cannot serve (a custom field of another project) are cleared.
watch(opts, value => {
	if (!value) {
		return
	}
	const names = new Set(value.dimensions.map(d => d.name))
	if (draft.groupBy && !names.has(draft.groupBy)) {
		draft.groupBy = ''
	}
	if (draft.thenBy && !names.has(draft.thenBy)) {
		draft.thenBy = ''
	}
	if (draft.timeField && !value.time_fields.some(f => f.name === draft.timeField)) {
		draft.timeField = draft.type === 'line' || draft.type === 'number' ? 'created' : ''
	}
	if (draft.estimateField && !value.measures.some(m => m.name === 'sum' && m.fields.some(f => f.name === draft.estimateField))) {
		draft.estimateField = ''
	}
	if (draft.field && !measureFields(value, draft.measure).some(f => f.name === draft.field)) {
		draft.field = ''
	}
})

const problems = computed(() => draftProblems(draft))
const complete = computed(() => problems.value.length === 0)
const config = computed(() => configFromDraft(draft))
// The preview asks the server about the unsaved config, so it waits for the typing to stop.
const previewConfig = refDebounced(config, 400)
const previewTitle = computed(() => draft.title || t(`dashboards.types.${draft.type}.name`))

const clientErrors = computed(() => submitted.value
	? Object.fromEntries(problems.value.map(p => [p.control, t(`dashboards.editor.problems.${p.key}`)]))
	: {})
function errorFor(control: string): string | null {
	return serverErrors[control] ?? clientErrors.value[control] ?? previewErrors[control] ?? null
}

// The limits (buckets, groups, field kind) have a translated explanation; the others name the exact parameter.
function messageFor(parsed: ServerFieldError): string {
	return [23002, 23003, 23004].includes(parsed.code) && te(`error.${parsed.code}`) ? t(`error.${parsed.code}`) : parsed.message
}

function onPreviewError(error: unknown) {
	for (const key of Object.keys(previewErrors)) {
		delete previewErrors[key]
	}
	const parsed = serverFieldError(error)
	if (parsed?.control) {
		previewErrors[parsed.control] = messageFor(parsed)
	}
}

watch(() => props.saveError, error => {
	for (const key of Object.keys(serverErrors)) {
		delete serverErrors[key]
	}
	const parsed = serverFieldError(error)
	if (parsed) {
		serverErrors[parsed.control || 'form'] = messageFor(parsed)
	}
})
// Typing again clears what the server said about the old value.
watch(() => ({...draft}), () => {
	for (const key of Object.keys(serverErrors)) {
		delete serverErrors[key]
	}
}, {deep: true})

const formError = computed(() => serverErrors.form ?? null)
const usesGroup = computed(() => ['bar', 'donut', 'line'].includes(draft.type))
const usesMeasure = computed(() => ['number', 'bar', 'donut', 'line'].includes(draft.type))
const usesTime = computed(() => ['line', 'created_vs_done', 'status_time', 'time_tracking'].includes(draft.type) || (draft.type === 'number' && draft.compare))
const usesTimeField = computed(() => draft.type === 'line' || (draft.type === 'number' && draft.compare))
const usesBucket = computed(() => ['line', 'created_vs_done'].includes(draft.type))
const usesFilter = computed(() => draft.type !== 'text' && draft.type !== 'time_tracking')
const orientation = computed({
	get: () => draft.display.orientation ?? '',
	set: (value: string) => {
		draft.display = {...draft.display, orientation: value === '' ? undefined : value as 'vertical' | 'horizontal'}
	},
})

const limitModel = computed({
	get: () => draft.limit ?? '',
	set: (value: string | number) => {
		draft.limit = value === '' || Number(value) <= 0 ? null : Number(value)
	},
})
const listLimitModel = computed({
	get: () => draft.listLimit,
	set: (value: string | number) => {
		draft.listLimit = Number(value) || 0
	},
})

function save() {
	submitted.value = true
	if (!complete.value || props.saving) {
		return
	}
	const display = Object.fromEntries(Object.entries(draft.display).filter(([, v]) => v !== undefined)) as WidgetDisplay
	emit('save', {id: props.widget?.id, type: draft.type, title: draft.title.trim(), config: config.value, display})
}

const projectLabel = computed(() => projectStore.projectsArray.find(p => p.id === props.dashboardProjectId)?.title ?? '')
</script>

<template>
	<SidePanel
		:title="isNew ? t('dashboards.editor.addTitle') : t('dashboards.editor.editTitle')"
		wide
		@close="emit('close')"
	>
		<template v-if="step === 'type'">
			<WidgetTypePicker
				:model-value="null"
				@update:modelValue="pickType"
			/>
		</template>
		<div
			v-else
			class="widget-editor"
		>
			<form
				class="widget-editor__form"
				novalidate
				data-test="widget-form"
				@submit.prevent="save"
			>
				<p
					v-if="isNew"
					class="widget-editor__type"
				>
					<span>{{ t(`dashboards.types.${draft.type}.name`) }}</span>
					<button
						type="button"
						class="widget-editor__change"
						@click="step = 'type'"
					>
						{{ t('dashboards.editor.changeType') }}
					</button>
				</p>
				<p
					v-if="formError"
					class="help is-danger"
					role="alert"
				>
					{{ formError }}
				</p>

				<FormField
					v-model="draft.title"
					:label="t('dashboards.editor.title')"
					:error="errorFor('title')"
					maxlength="250"
					:placeholder="t(`dashboards.types.${draft.type}.name`)"
				/>

				<template v-if="draft.type !== 'text'">
					<FormField
						v-if="dashboardProjectId > 0"
						:label="t('dashboards.editor.scope')"
					>
						<p class="widget-editor__static">
							{{ projectLabel || t('dashboards.editor.thisProject') }}
						</p>
					</FormField>
					<FormField
						v-else
						v-slot="{id}"
						:label="t('dashboards.editor.scope')"
					>
						<FormSelect
							:id="id"
							v-model="draft.projectId"
							:options="projectOptions"
							:error="errorFor('project_id')"
						/>
					</FormField>
				</template>

				<FilterEditor
					v-if="usesFilter"
					v-model="draft.filter"
					:project-id="scopeProjectId"
					:label="t('dashboards.editor.filter')"
				/>
				<p
					v-if="errorFor('filter')"
					class="help is-danger"
					role="alert"
				>
					{{ errorFor('filter') }}
				</p>

				<template v-if="usesGroup">
					<FormField
						v-slot="{id}"
						:label="draft.type === 'line' ? t('dashboards.editor.seriesBy') : t('dashboards.editor.groupBy')"
					>
						<FormSelect
							:id="id"
							v-model="draft.groupBy"
							:options="dimensionOptions"
							:error="errorFor('group_by')"
							data-control="group_by"
						/>
					</FormField>
					<FormField
						v-if="draft.type === 'bar'"
						v-slot="{id}"
						:label="t('dashboards.editor.thenBy')"
					>
						<FormSelect
							:id="id"
							v-model="draft.thenBy"
							:options="thenByOptions"
							:disabled="!draft.groupBy"
							:error="errorFor('then_by')"
							data-control="then_by"
						/>
					</FormField>
				</template>

				<template v-if="usesMeasure">
					<FormField
						v-slot="{id}"
						:label="t('dashboards.editor.measure')"
					>
						<FormSelect
							:id="id"
							v-model="draft.measure"
							:options="measureOptions"
							:error="errorFor('measure')"
							data-control="measure"
						/>
					</FormField>
					<FormField
						v-if="draft.measure !== 'count'"
						v-slot="{id}"
						:label="t('dashboards.editor.field')"
					>
						<FormSelect
							:id="id"
							v-model="draft.field"
							:options="fieldOptions"
							:error="errorFor('field')"
							data-control="field"
						/>
					</FormField>
				</template>

				<label
					v-if="draft.type === 'number'"
					class="checkbox"
				>
					<input
						v-model="draft.compare"
						type="checkbox"
						data-control="compare"
					>
					{{ t('dashboards.editor.compare') }}
				</label>
				<p
					v-if="draft.type === 'number' && draft.compare"
					class="help"
				>
					{{ t('dashboards.editor.compareHint') }}
				</p>

				<FormField
					v-if="draft.type === 'status_time'"
					v-slot="{id}"
					:label="t('dashboards.editor.splitBy')"
				>
					<FormSelect
						:id="id"
						v-model="draft.groupBy"
						:options="statusGroupOptions"
						:error="errorFor('group_by')"
						data-control="group_by"
					/>
				</FormField>
				<FormField
					v-if="draft.type === 'time_tracking'"
					v-slot="{id}"
					:label="t('dashboards.editor.addUpBy')"
				>
					<FormSelect
						:id="id"
						v-model="draft.groupBy"
						:options="trackedGroupOptions"
						:error="errorFor('group_by')"
						data-control="group_by"
					/>
				</FormField>
				<FormField
					v-if="draft.type === 'workload'"
					v-slot="{id}"
					:label="t('dashboards.editor.estimateField')"
				>
					<FormSelect
						:id="id"
						v-model="draft.estimateField"
						:options="estimateOptions"
						:error="errorFor('estimate_field')"
						data-control="estimate_field"
					/>
					<p class="help">
						{{ t('dashboards.editor.estimateHint') }}
					</p>
				</FormField>

				<template v-if="usesTime">
					<FormField
						v-if="usesTimeField"
						v-slot="{id}"
						:label="t('dashboards.editor.timeField')"
					>
						<FormSelect
							:id="id"
							v-model="draft.timeField"
							:options="timeFieldOptions"
							:error="errorFor('time.field')"
							data-control="time.field"
						/>
					</FormField>
					<FormField
						v-if="usesBucket"
						v-slot="{id}"
						:label="t('dashboards.editor.bucket')"
					>
						<FormSelect
							:id="id"
							v-model="draft.bucket"
							:options="bucketOptions"
							:error="errorFor('time.bucket') ?? errorFor('bucket')"
							data-control="time.bucket"
						/>
					</FormField>
					<FormField
						v-slot="{id}"
						:label="t('dashboards.editor.range')"
					>
						<FormSelect
							:id="id"
							v-model="draft.preset"
							:options="presetOptions"
							:error="errorFor('time') ?? null"
							data-control="range"
						/>
					</FormField>
					<div
						v-if="draft.preset === 'custom'"
						class="widget-editor__dates"
					>
						<FormField
							v-model="draft.from"
							type="date"
							:label="t('dashboards.editor.from')"
							:error="errorFor('time.from') ?? errorFor('from')"
						/>
						<FormField
							v-model="draft.to"
							type="date"
							:label="t('dashboards.editor.to')"
							:error="errorFor('time.to') ?? errorFor('to')"
						/>
					</div>
				</template>

				<FormField
					v-if="draft.type === 'bar' || draft.type === 'donut'"
					v-model="limitModel"
					type="number"
					min="1"
					:max="opts?.limits.max_groups"
					:label="t('dashboards.editor.limit')"
					:error="errorFor('limit')"
					:placeholder="String(opts?.limits.default_limit ?? 20)"
				/>

				<template v-if="draft.type === 'task_list'">
					<FormField
						v-slot="{id}"
						:label="t('dashboards.editor.sortBy')"
					>
						<FormSelect
							:id="id"
							v-model="draft.sortBy"
							:options="sortOptions"
							:error="errorFor('sort_by[0]') ?? errorFor('sort_by')"
						/>
					</FormField>
					<FormField
						v-slot="{id}"
						:label="t('dashboards.editor.order')"
					>
						<FormSelect
							:id="id"
							v-model="draft.order"
							:options="[{value: 'asc', label: t('dashboards.editor.ascending')}, {value: 'desc', label: t('dashboards.editor.descending')}]"
						/>
					</FormField>
					<FormField
						v-model="listLimitModel"
						type="number"
						min="1"
						max="100"
						:label="t('dashboards.editor.listLimit')"
						:error="errorFor('limit')"
					/>
				</template>

				<FormField
					v-if="draft.type === 'text'"
					v-slot="{id}"
					:label="t('dashboards.editor.text')"
				>
					<Editor
						:id="id"
						v-model="draft.text"
						:placeholder="t('dashboards.editor.textPlaceholder')"
					/>
					<p
						v-if="errorFor('text')"
						class="help is-danger"
						role="alert"
					>
						{{ errorFor('text') }}
					</p>
				</FormField>

				<fieldset
					v-if="['bar', 'donut', 'line', 'created_vs_done'].includes(draft.type)"
					class="widget-editor__display"
				>
					<legend>{{ t('dashboards.editor.display') }}</legend>
					<FormField
						v-if="draft.type === 'bar'"
						v-slot="{id}"
						:label="t('dashboards.editor.orientation')"
					>
						<FormSelect
							:id="id"
							v-model="orientation"
							:options="orientationOptions"
						/>
					</FormField>
					<label
						v-if="draft.type === 'bar' && draft.thenBy"
						class="checkbox"
					>
						<input
							type="checkbox"
							:checked="draft.display.stacked ?? true"
							@change="draft.display = {...draft.display, stacked: ($event.target as HTMLInputElement).checked}"
						>
						{{ t('dashboards.editor.stacked') }}
					</label>
					<label class="checkbox">
						<input
							type="checkbox"
							:checked="draft.display.legend ?? (draft.type === 'donut' || (draft.type === 'bar' && !!draft.thenBy) || draft.type === 'created_vs_done')"
							@change="draft.display = {...draft.display, legend: ($event.target as HTMLInputElement).checked}"
						>
						{{ t('dashboards.editor.showLegend') }}
					</label>
					<p class="help">
						{{ t('dashboards.editor.displayHint') }}
					</p>
				</fieldset>
			</form>

			<section
				class="widget-editor__preview"
				:aria-label="t('dashboards.editor.preview')"
			>
				<h3 class="widget-editor__preview-title">
					{{ t('dashboards.editor.preview') }}
				</h3>
				<div
					class="widget-editor__preview-card"
					data-test="widget-preview"
				>
					<p class="widget-editor__preview-heading">
						{{ previewTitle }}
					</p>
					<div class="widget-editor__preview-body">
						<WidgetView
							:type="draft.type"
							:config="previewConfig"
							:title="previewTitle"
							:display="draft.display"
							:can-drill="false"
							:enabled="complete"
							@error="onPreviewError"
						/>
					</div>
				</div>
				<p
					v-if="!complete"
					class="help"
				>
					{{ t('dashboards.editor.previewIdle') }}
				</p>
			</section>
		</div>

		<template
			v-if="step === 'form'"
			#footer
		>
			<XButton
				variant="tertiary"
				@click="emit('close')"
			>
				{{ t('misc.cancel') }}
			</XButton>
			<XButton
				:loading="saving"
				data-test="widget-save"
				@click="save"
			>
				{{ t('misc.save') }}
			</XButton>
		</template>
	</SidePanel>
</template>

<style lang="scss" scoped>
.widget-editor {
	display: grid;
	grid-template-columns: minmax(0, 1fr) minmax(0, 1.1fr);
	gap: 1.5rem;

	@media screen and (width <= 1000px) {
		grid-template-columns: minmax(0, 1fr);
	}
}

.widget-editor__form {
	display: flex;
	flex-direction: column;
	gap: .5rem;
	min-inline-size: 0;
}

.widget-editor__type {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin: 0 0 .25rem;
	font-weight: 600;
}

.widget-editor__change {
	padding: 0;
	border: 0;
	background: none;
	color: var(--link);
	font-size: .85rem;
	font-weight: 400;
	cursor: pointer;
}

.widget-editor__static {
	margin: 0;
	padding: .4rem 0;
}

.widget-editor__dates {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: .75rem;
}

.widget-editor__display {
	display: flex;
	flex-direction: column;
	gap: .35rem;
	margin: .5rem 0 0;
	padding: .75rem;
	border: 1px solid var(--grey-200);
	border-radius: var(--input-radius);

	legend {
		padding: 0 .35rem;
		font-weight: 600;
	}
}

.widget-editor__preview {
	min-inline-size: 0;

	@media screen and (width >= 1001px) {
		position: sticky;
		inset-block-start: 0;
		align-self: start;
	}
}

.widget-editor__preview-title {
	margin: 0 0 .5rem;
	font-size: .9rem;
	font-weight: 600;
	color: var(--text-light);
}

.widget-editor__preview-card {
	display: flex;
	flex-direction: column;
	block-size: 22rem;
	padding: .6rem .75rem .75rem;
	border: 1px solid var(--grey-200);
	border-radius: var(--input-radius);
	background: var(--scheme-main);
}

.widget-editor__preview-heading {
	margin: 0 0 .25rem;
	font-weight: 600;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.widget-editor__preview-body {
	flex: 1 1 auto;
	min-block-size: 0;
}
</style>
