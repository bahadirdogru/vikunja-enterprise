<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {onBeforeRouteLeave} from 'vue-router'
import {useQuery} from '@tanstack/vue-query'

import Message from '@/components/misc/Message.vue'
import Modal from '@/components/misc/Modal.vue'
import XButton from '@/components/input/Button.vue'
import WorkflowMoveTasksDialog from '@/components/workflow/WorkflowMoveTasksDialog.vue'
import WorkflowStatusList from '@/components/workflow/WorkflowStatusList.vue'
import WorkflowTransitions from '@/components/workflow/WorkflowTransitions.vue'
import {customFieldsQuery} from '@/client/queries/customFields'
import {
	useDeleteWorkflowMutation,
	useReplaceWorkflowMutation,
	workflowTeamsQuery,
} from '@/client/queries/workflow'
import {isRequestContextAbort} from '@/client/requestContext'
import {
	activeStatuses,
	createDefaultDraft,
	draftFromWorkflow,
	draftToDocument,
	isDraftDirty,
	readWorkflowProblem,
	validateDraft,
	type WorkflowDraft,
	type WorkflowResponse,
} from '@/helpers/workflow'
import {getErrorText} from '@/message'

const props = defineProps<{
	projectId: number
	workflow: WorkflowResponse | null
}>()

const {t} = useI18n({useScope: 'global'})

const customFieldsResult = useQuery(computed(() => customFieldsQuery(props.projectId)))
const teamsResult = useQuery(workflowTeamsQuery())
const customFields = computed(() => customFieldsResult.data.value ?? [])
const teams = computed(() => teamsResult.data.value ?? [])

const replace = useReplaceWorkflowMutation()
const disable = useDeleteWorkflowMutation()

function initialDraft(): WorkflowDraft | null {
	return props.workflow ? draftFromWorkflow(props.workflow) : null
}

// The editor owns its draft: a refetch must not overwrite what the admin is typing.
const saved = ref<WorkflowDraft | null>(initialDraft())
const draft = ref<WorkflowDraft | null>(initialDraft())
const showErrors = ref(false)
const apiError = ref<string | null>(null)
const isSaving = computed(() => replace.isPending.value)

const errors = computed(() => draft.value ? validateDraft(draft.value) : null)
const dirty = computed(() => draft.value !== null && isDraftDirty(draft.value, saved.value))
const hasWorkflow = computed(() => saved.value !== null)

function enable() {
	draft.value = createDefaultDraft({
		todo: t('workflow.defaults.todo'),
		inProgress: t('workflow.defaults.inProgress'),
		done: t('workflow.defaults.done'),
	})
	showErrors.value = false
	apiError.value = null
}

function update(value: WorkflowDraft) {
	draft.value = value
	apiError.value = null
}

function reseed(workflow: WorkflowResponse) {
	saved.value = draftFromWorkflow(workflow)
	draft.value = draftFromWorkflow(workflow)
	showErrors.value = false
}

const moveDialog = ref<{removedTitles: string[]} | null>(null)

const moveTargets = computed(() => draft.value
	? activeStatuses(draft.value).map(status => ({id: status.id, title: status.title}))
	: [])
const moveDefault = computed(() => draft.value?.statuses.find(status => !status.archived && status.is_initial)?.id
	?? moveTargets.value[0]?.id
	?? 0)

async function save(moveTasksTo = 0) {
	if (!draft.value || !errors.value) {
		return
	}
	showErrors.value = true
	if (!errors.value.valid) {
		return
	}
	const projectId = props.projectId
	apiError.value = null
	try {
		const result = await replace.mutateAsync({
			projectId,
			document: draftToDocument(draft.value, moveTasksTo),
		})
		if (props.projectId === projectId) {
			moveDialog.value = null
			reseed(result)
		}
	} catch (cause) {
		if (isRequestContextAbort(cause) || props.projectId !== projectId) {
			return
		}
		const problem = readWorkflowProblem(cause)
		if (problem.kind === 'moveTasksRequired') {
			const titles = problem.statusIds.map(id => saved.value?.statuses.find(status => status.id === id)?.title ?? String(id))
			moveDialog.value = {removedTitles: titles}
			return
		}
		moveDialog.value = null
		apiError.value = problem.kind === 'invalidDefinition' && problem.message
			? problem.message
			: getErrorText(cause)
	}
}

const confirmDisable = ref(false)

async function disableWorkflow() {
	const projectId = props.projectId
	try {
		await disable.mutateAsync({projectId})
	} catch {
		return
	}
	if (props.projectId === projectId) {
		confirmDisable.value = false
		saved.value = null
		draft.value = null
		showErrors.value = false
		apiError.value = null
	}
}

defineExpose({
	save: () => save(),
	requestDisable: () => { confirmDisable.value = true },
	hasWorkflow,
	dirty,
	isSaving,
	isActive: computed(() => draft.value !== null),
})

let answerLeave: ((leave: boolean) => void) | null = null
const confirmLeave = ref(false)

onBeforeRouteLeave(() => {
	if (!dirty.value) {
		return true
	}
	return new Promise<boolean>(resolve => {
		answerLeave = resolve
		confirmLeave.value = true
	})
})

function resolveLeave(leave: boolean) {
	confirmLeave.value = false
	answerLeave?.(leave)
	answerLeave = null
}

function warnBeforeUnload(event: BeforeUnloadEvent) {
	if (dirty.value) {
		event.preventDefault()
	}
}

onMounted(() => window.addEventListener('beforeunload', warnBeforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', warnBeforeUnload))
</script>

<template>
	<div class="workflow-editor">
		<div
			v-if="draft === null"
			class="empty-state"
		>
			<Icon
				icon="sitemap"
				class="empty-state__icon"
			/>
			<h3 class="empty-state__title">
				{{ $t('workflow.empty.title') }}
			</h3>
			<p class="empty-state__text">
				{{ $t('workflow.empty.text') }}
			</p>
			<p class="empty-state__text">
				{{ $t('workflow.empty.kanban') }}
			</p>
			<XButton
				icon="plus"
				@click="enable"
			>
				{{ $t('workflow.empty.enable') }}
			</XButton>
		</div>

		<form
			v-else
			class="editor-form"
			novalidate
			@submit.prevent="save()"
		>
			<Message
				v-if="!hasWorkflow"
				variant="info"
			>
				{{ $t('workflow.empty.draftHint') }}
			</Message>

			<WorkflowStatusList
				:model-value="draft"
				:errors="errors!"
				:show-errors="showErrors"
				@update:modelValue="update"
			/>

			<ul
				v-if="showErrors && errors!.global.length > 0"
				class="global-errors"
				role="alert"
			>
				<li
					v-for="code in errors!.global"
					:key="code"
					class="help is-danger"
				>
					{{ $t(`workflow.validation.${code}`) }}
				</li>
			</ul>

			<WorkflowTransitions
				:model-value="draft"
				:custom-fields="customFields"
				:teams="teams"
				:errors="errors!"
				:show-errors="showErrors"
				@update:modelValue="update"
			/>

			<p
				v-if="apiError"
				class="help is-danger"
				role="alert"
			>
				{{ apiError }}
			</p>
		</form>

		<Modal
			:enabled="confirmDisable"
			@close="confirmDisable = false"
			@submit="disableWorkflow"
		>
			<template #header>
				<span>{{ $t('workflow.disable.title') }}</span>
			</template>
			<template #text>
				<p>{{ $t('workflow.disable.text') }}</p>
			</template>
		</Modal>

		<Modal
			:enabled="confirmLeave"
			@close="resolveLeave(false)"
			@submit="resolveLeave(true)"
		>
			<template #header>
				<span>{{ $t('workflow.leave.title') }}</span>
			</template>
			<template #text>
				<p>{{ $t('workflow.leave.text') }}</p>
			</template>
		</Modal>

		<WorkflowMoveTasksDialog
			:enabled="moveDialog !== null"
			:removed-titles="moveDialog?.removedTitles ?? []"
			:targets="moveTargets"
			:default-target="moveDefault"
			:busy="isSaving"
			@close="moveDialog = null"
			@confirm="target => save(target)"
		/>
	</div>
</template>

<style lang="scss" scoped>
.empty-state {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: .75rem;
	padding-block: 2rem;
	text-align: center;

	&__icon {
		font-size: 2rem;
		color: var(--grey-400);
	}

	&__title {
		margin: 0;
		font-size: 1.125rem;
	}

	&__text {
		max-inline-size: 38rem;
		margin: 0;
		color: var(--grey-600);
	}
}

.editor-form {
	display: flex;
	flex-direction: column;
	gap: 1.5rem;
}

.global-errors {
	margin: 0;
	padding: 0;
	list-style: none;
}
</style>
