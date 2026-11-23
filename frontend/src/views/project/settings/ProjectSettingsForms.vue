<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {useTitle} from '@vueuse/core'
import {onBeforeRouteLeave} from 'vue-router'
import {useQuery} from '@tanstack/vue-query'

import FormBuilder from '@/components/forms/FormBuilder.vue'
import FormList from '@/components/forms/FormList.vue'
import FormSubmissions from '@/components/forms/FormSubmissions.vue'
import XButton from '@/components/input/Button.vue'
import CreateEdit from '@/components/misc/CreateEdit.vue'
import ErrorMessage from '@/components/misc/Error.vue'
import Loading from '@/components/misc/Loading.vue'
import Message from '@/components/misc/Message.vue'
import Modal from '@/components/misc/Modal.vue'
import {formOptionsQuery, formsQuery, type FormResponse} from '@/client/queries/forms'
import {labelsQuery} from '@/client/queries/labels'
import {workflowQuery} from '@/client/queries/workflow'
import {PERMISSIONS} from '@/constants/permissions'
import {useGoBack} from '@/composables/useGoBack'
import {useProject} from '@/composables/useProject'
import {cloneDraft, draftFromForm, type FormDraft, type Preset} from '@/helpers/forms'

const props = defineProps<{
	projectId: number
}>()

const {t} = useI18n({useScope: 'global'})
const goBack = useGoBack()
useTitle(() => t('forms.title'))

const {project, error: projectError} = useProject(() => props.projectId)
const isAdmin = computed(() => project.value?.max_permission === PERMISSIONS.ADMIN)
const enabled = computed(() => props.projectId > 0 && isAdmin.value)

const formsResult = useQuery(computed(() => ({...formsQuery(props.projectId), enabled: enabled.value})))
const optionsResult = useQuery(computed(() => ({...formOptionsQuery(props.projectId), enabled: enabled.value})))
const workflowResult = useQuery(computed(() => ({...workflowQuery(props.projectId), enabled: enabled.value})))
const labelsResult = useQuery(computed(() => ({...labelsQuery(), enabled: enabled.value})))

const loadError = computed(() => projectError.value ?? formsResult.error.value ?? optionsResult.error.value
	?? workflowResult.error.value ?? labelsResult.error.value)
const loaded = computed(() => formsResult.data.value !== undefined
	&& optionsResult.data.value !== undefined
	&& workflowResult.data.value !== undefined
	&& labelsResult.data.value !== undefined)

const statuses = computed(() => (workflowResult.data.value?.statuses ?? [])
	.filter(status => !status.archived)
	.map(status => ({id: status.id, title: status.title, category: status.category})))
const labels = computed(() => (labelsResult.data.value ?? []).map(label => ({
	id: label.id ?? 0,
	title: label.title ?? '',
	color: label.hex_color ?? '',
})))

type Mode =
	| {kind: 'list'}
	| {kind: 'edit', key: number, form: FormResponse | null, preset: FormDraft | null}
	| {kind: 'submissions', form: FormResponse}

const mode = ref<Mode>({kind: 'list'})
const editor = ref<InstanceType<typeof FormBuilder> | null>(null)
let sessions = 0

function openEditor(form: FormResponse | null, preset: FormDraft | null = null) {
	mode.value = {kind: 'edit', key: ++sessions, form, preset}
}

function duplicate(form: FormResponse) {
	const draft = cloneDraft(draftFromForm(form))
	draft.title = t('forms.copyOf', {title: form.title})
	// A copy gets its own link and stays closed until it has been checked.
	draft.enabled = false
	openEditor(null, draft)
}

function fromPreset(preset: Preset) {
	openEditor(null, preset.draft)
}

function showSubmissions(form: FormResponse) {
	mode.value = {kind: 'submissions', form}
}

// ---- unsaved changes --------------------------------------------------------------------------------------------
const dirty = computed(() => mode.value.kind === 'edit' && editor.value?.dirty === true)
const confirmLeave = ref(false)
let answerLeave: ((leave: boolean) => void) | null = null
let afterConfirm: (() => void) | null = null

function goTo(next: Mode) {
	if (dirty.value) {
		afterConfirm = () => { mode.value = next }
		confirmLeave.value = true
		return
	}
	mode.value = next
}

function backToList() {
	goTo({kind: 'list'})
}

function resolveLeave(leave: boolean) {
	confirmLeave.value = false
	if (leave) afterConfirm?.()
	answerLeave?.(leave)
	answerLeave = null
	afterConfirm = null
}

onBeforeRouteLeave(() => {
	if (!dirty.value) return true
	return new Promise<boolean>(resolve => {
		answerLeave = resolve
		confirmLeave.value = true
	})
})

function warnBeforeUnload(event: BeforeUnloadEvent) {
	if (dirty.value) event.preventDefault()
}

onMounted(() => window.addEventListener('beforeunload', warnBeforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', warnBeforeUnload))

const heading = computed(() => {
	if (mode.value.kind === 'edit') return mode.value.form ? t('forms.editForm') : t('forms.newForm')
	if (mode.value.kind === 'submissions') return t('forms.submissions.heading')
	return t('forms.title')
})

const saving = ref(false)

async function save() {
	if (!editor.value) return
	saving.value = true
	try {
		await editor.value.save()
	} finally {
		saving.value = false
	}
}
</script>

<template>
	<CreateEdit
		:title="heading"
		:has-primary-action="false"
		:wide="true"
	>
		<ErrorMessage v-if="loadError" />
		<Message v-else-if="project && !isAdmin">
			{{ $t('forms.onlyAdminsCanEdit') }}
		</Message>
		<Loading v-else-if="!loaded" />
		<template v-else>
			<FormList
				v-if="mode.kind === 'list'"
				:forms="formsResult.data.value ?? []"
				:options="optionsResult.data.value!"
				@create="openEditor(null)"
				@preset="fromPreset"
				@edit="openEditor($event)"
				@submissions="showSubmissions"
				@duplicate="duplicate"
			/>
			<FormBuilder
				v-else-if="mode.kind === 'edit'"
				ref="editor"
				:key="mode.key"
				:project-id="projectId"
				:form="mode.form"
				:preset="mode.preset"
				:options="optionsResult.data.value!"
				:statuses="statuses"
				:labels="labels"
				@submissions="showSubmissions"
			/>
			<FormSubmissions
				v-else
				:project-id="projectId"
				:form="mode.form"
				@back="mode = {kind: 'list'}"
			/>
		</template>

		<template #footer>
			<XButton
				v-if="mode.kind === 'edit' && loaded"
				type="button"
				icon="check"
				:loading="saving"
				@click="save"
			>
				{{ $t('forms.save') }}
			</XButton>
			<XButton
				v-if="mode.kind !== 'list' && loaded"
				type="button"
				variant="secondary"
				class="mis-2"
				@click="backToList"
			>
				{{ $t('forms.backToList') }}
			</XButton>
			<span class="spacer" />
			<XButton
				variant="secondary"
				@click="goBack()"
			>
				{{ $t('misc.close') }}
			</XButton>
		</template>

		<Modal
			:enabled="confirmLeave"
			@close="resolveLeave(false)"
			@submit="resolveLeave(true)"
		>
			<template #header>
				<span>{{ $t('forms.leave.title') }}</span>
			</template>
			<template #text>
				<p>{{ $t('forms.leave.text') }}</p>
			</template>
		</Modal>
	</CreateEdit>
</template>

<style lang="scss" scoped>
.spacer {
	flex: 1;
}
</style>
