<script setup lang="ts">
import {computed, reactive, ref, useId, watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'

import Card from '@/components/misc/Card.vue'
import CustomFieldValueEditor from '@/components/custom-fields/CustomFieldValueEditor.vue'
import Datepicker from '@/components/input/Datepicker.vue'
import EditAssignees from '@/components/tasks/partials/EditAssignees.vue'
import EditLabels from '@/components/tasks/partials/EditLabels.vue'
import Modal from '@/components/misc/Modal.vue'
import WorkflowStatusBadge from '@/components/workflow/WorkflowStatusBadge.vue'
import XButton from '@/components/input/Button.vue'
import {customFieldsQuery, customFieldValuesQuery} from '@/client/queries/customFields'
import {useUploadAttachmentsMutation} from '@/client/queries/attachments'
import {useUpdateTaskMutation} from '@/client/queries/taskMutations'
import {useSaveRequirementValuesMutation} from '@/client/queries/workflow'
import {isRequestContextAbort} from '@/client/requestContext'
import {useTask} from '@/composables/useTask'
import {customFieldIdOf, isCustomFieldName, type WorkflowStatusResponse} from '@/helpers/workflow'
import {parseDateOrNull} from '@/helpers/parseDateOrNull'
import {getErrorText} from '@/message'

const props = defineProps<{
	taskId: number
	projectId: number
	toStatus: WorkflowStatusResponse
	missingFields: string[]
	commentRequired: boolean
	busy: boolean
	// A message from the last attempt, e.g. fields that are still missing.
	problem: string | null
}>()

const emit = defineEmits<{
	close: []
	submit: [comment: string]
}>()

const {t} = useI18n({useScope: 'global'})
const uid = useId()

const taskResult = useTask(() => props.taskId)
const task = computed(() => taskResult.task.value)
const fieldsResult = useQuery(computed(() => customFieldsQuery(props.projectId)))
const valuesResult = useQuery(computed(() => customFieldValuesQuery(props.taskId)))
const updateTask = useUpdateTaskMutation()
const saveValues = useSaveRequirementValuesMutation()
const upload = useUploadAttachmentsMutation()

const customFields = computed(() => fieldsResult.data.value ?? [])
const storedValues = computed(() => new Map(
	(valuesResult.data.value ?? []).map(entry => [entry.custom_field_id, entry.value]),
))

const dates = reactive<{due_date: Date | null, start_date: Date | null, end_date: Date | null}>({
	due_date: null,
	start_date: null,
	end_date: null,
})
const description = ref('')
const assignees = ref<NonNullable<typeof task.value>['assignees']>([])
const labels = ref<NonNullable<typeof task.value>['labels']>([])
const customValues = ref<Record<number, unknown>>({})
const comment = ref('')
const localError = ref<string | null>(null)
// Once asked for, the comment box stays even when the server stops listing it as missing.
const showComment = ref(false)

watch(() => props.commentRequired, required => {
	if (required) showComment.value = true
}, {immediate: true})

let seededTask = 0
watch(task, loaded => {
	if (!loaded || seededTask === loaded.id) {
		return
	}
	seededTask = loaded.id
	dates.due_date = parseDateOrNull(loaded.due_date)
	dates.start_date = parseDateOrNull(loaded.start_date)
	dates.end_date = parseDateOrNull(loaded.end_date)
	description.value = htmlToText(loaded.description ?? '')
	assignees.value = loaded.assignees ?? []
	labels.value = loaded.labels ?? []
}, {immediate: true})

function htmlToText(html: string): string {
	if (html === '') {
		return ''
	}
	const element = document.createElement('div')
	element.innerHTML = html.replace(/<br\s*\/?>/gi, '\n').replace(/<\/p>\s*<p>/gi, '\n\n')
	return element.textContent ?? ''
}

function textToHtml(text: string): string {
	const escape = (value: string) => value
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
	return text.trim() === ''
		? ''
		: text.trim().split(/\n{2,}/).map(paragraph => `<p>${escape(paragraph).replace(/\n/g, '<br>')}</p>`).join('')
}

type Row =
	| {name: string, kind: 'builtin', label: string}
	| {name: string, kind: 'custom', label: string, fieldId: number}

const rows = computed<Row[]>(() => props.missingFields.map(name => {
	if (isCustomFieldName(name)) {
		const fieldId = customFieldIdOf(name)
		const field = customFields.value.find(entry => entry.id === fieldId)
		return {name, kind: 'custom' as const, fieldId, label: field?.title ?? name}
	}
	return {name, kind: 'builtin' as const, label: t(`workflow.fields.${name}`)}
}))

function customField(fieldId: number) {
	return customFields.value.find(field => field.id === fieldId)
}

function customValue(fieldId: number): unknown {
	return fieldId in customValues.value ? customValues.value[fieldId] : storedValues.value.get(fieldId) ?? null
}

const attachmentNames = computed(() => (task.value?.attachments ?? []).map(attachment => attachment.file?.name ?? ''))
const uploading = computed(() => upload.isPending.value)

function onFiles(event: Event) {
	const input = event.target as HTMLInputElement
	const files = Array.from(input.files ?? [])
	input.value = ''
	if (files.length > 0) {
		upload.mutate({taskId: props.taskId, files})
	}
}

function sameDate(a: Date | null, b: unknown): boolean {
	return (a?.getTime() ?? 0) === (parseDateOrNull(b as string)?.getTime() ?? 0)
}

const saving = ref(false)

// Fields go first so the status change finds them set.
async function submit() {
	localError.value = null
	if (showComment.value && comment.value.trim() === '') {
		localError.value = t('workflow.dialog.commentRequired')
		return
	}
	const current = task.value
	saving.value = true
	try {
		if (current) {
			const patch: Record<string, unknown> = {}
			for (const name of ['due_date', 'start_date', 'end_date'] as const) {
				if (props.missingFields.includes(name) && dates[name] && !sameDate(dates[name], current[name])) {
					patch[name] = dates[name]!.toISOString()
				}
			}
			if (props.missingFields.includes('description')) {
				const html = textToHtml(description.value)
				if (html !== '' && html !== current.description) {
					patch.description = html
				}
			}
			if (Object.keys(patch).length > 0) {
				await updateTask.mutateAsync({id: current.id, ...patch})
			}
		}
		const values = Object.entries(customValues.value).map(([fieldId, value]) => ({fieldId: Number(fieldId), value}))
		if (values.length > 0) {
			await saveValues.mutateAsync({taskId: props.taskId, values})
			customValues.value = {}
		}
	} catch (cause) {
		if (!isRequestContextAbort(cause)) {
			localError.value = getErrorText(cause)
		}
		return
	} finally {
		saving.value = false
	}
	emit('submit', comment.value.trim())
}

const title = computed(() => t('workflow.dialog.title', {status: props.toStatus.title}))
</script>

<template>
	<Modal
		:enabled="true"
		:overflow="true"
		:aria-label="title"
		@close="emit('close')"
	>
		<Card
			:title="title"
			:shadow="false"
			:show-close="true"
			class="status-dialog has-text-start"
			@close="emit('close')"
		>
			<form
				novalidate
				@submit.prevent="submit"
			>
				<p class="status-dialog__intro">
					<WorkflowStatusBadge :status="toStatus" />
				</p>
				<p
					v-if="rows.length > 0"
					class="status-dialog__text"
				>
					{{ $t('workflow.dialog.fieldsIntro') }}
				</p>

				<div
					v-for="row in rows"
					:key="row.name"
					class="field"
				>
					<span
						:id="`${uid}-${row.name}-label`"
						class="label"
					>{{ row.label }}</span>

					<template v-if="row.kind === 'custom'">
						<CustomFieldValueEditor
							v-if="customField(row.fieldId)"
							:field="customField(row.fieldId)!"
							:model-value="customValue(row.fieldId)"
							:project-id="projectId"
							:labelledby="`${uid}-${row.name}-label`"
							@update:modelValue="value => customValues = {...customValues, [row.fieldId]: value}"
						/>
					</template>
					<template v-else-if="row.name === 'due_date' || row.name === 'start_date' || row.name === 'end_date'">
						<Datepicker
							v-model="dates[row.name]"
							:aria-labelledby="`${uid}-${row.name}-label`"
							:choose-date-label="$t('workflow.dialog.chooseDate')"
							:empty-label="$t('misc.notSet')"
							:title="row.label"
						/>
					</template>
					<template v-else-if="row.name === 'description'">
						<textarea
							v-model="description"
							class="textarea"
							rows="3"
							:aria-labelledby="`${uid}-${row.name}-label`"
						/>
					</template>
					<template v-else-if="row.name === 'assignees'">
						<EditAssignees
							v-model="assignees"
							:project-id="projectId"
							:task-id="taskId"
						/>
					</template>
					<template v-else-if="row.name === 'labels'">
						<EditLabels
							v-model="labels"
							:task-id="taskId"
						/>
					</template>
					<template v-else-if="row.name === 'attachments'">
						<ul
							v-if="attachmentNames.length > 0"
							class="status-dialog__files"
						>
							<li
								v-for="(name, index) in attachmentNames"
								:key="index"
							>
								{{ name }}
							</li>
						</ul>
						<input
							type="file"
							multiple
							:aria-labelledby="`${uid}-${row.name}-label`"
							:disabled="uploading || undefined"
							@change="onFiles"
						>
					</template>
				</div>

				<div
					v-if="showComment"
					class="field"
				>
					<label
						class="label"
						:for="`${uid}-comment`"
					>{{ $t('workflow.dialog.comment') }}</label>
					<textarea
						:id="`${uid}-comment`"
						v-model="comment"
						class="textarea"
						rows="3"
						:placeholder="$t('workflow.dialog.commentPlaceholder')"
						:aria-invalid="localError !== null && comment.trim() === '' ? 'true' : undefined"
					/>
				</div>

				<p
					v-if="localError || problem"
					class="help is-danger"
					role="alert"
				>
					{{ localError ?? problem }}
				</p>
				<button
					type="submit"
					hidden
				/>
			</form>

			<template #footer>
				<XButton
					variant="secondary"
					@click="emit('close')"
				>
					{{ $t('misc.cancel') }}
				</XButton>
				<XButton
					class="mis-2"
					:loading="busy || saving"
					@click="submit"
				>
					{{ $t('workflow.dialog.confirm') }}
				</XButton>
			</template>
		</Card>
	</Modal>
</template>

<style lang="scss" scoped>
.status-dialog {
	&__intro {
		margin-block-end: .75rem;
	}

	&__text {
		margin-block-end: 1rem;
		color: var(--grey-600);
	}

	&__files {
		margin: 0 0 .5rem;
		padding-inline-start: 1.25rem;
		font-size: .875rem;
	}
}
</style>
