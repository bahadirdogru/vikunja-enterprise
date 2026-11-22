<script setup lang="ts">
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'

import BaseButton from '@/components/base/BaseButton.vue'
import Dropdown from '@/components/misc/Dropdown.vue'
import TaskPropertyRow from '@/components/tasks/partials/TaskPropertyRow.vue'
import TaskStatusDialog from '@/components/workflow/TaskStatusDialog.vue'
import TaskStatusHistory from '@/components/workflow/TaskStatusHistory.vue'
import WorkflowStatusBadge from '@/components/workflow/WorkflowStatusBadge.vue'
import {customFieldsQuery} from '@/client/queries/customFields'
import {
	taskStatusQuery,
	useChangeTaskStatusMutation,
	workflowQuery,
} from '@/client/queries/workflow'
import {isRequestContextAbort} from '@/client/requestContext'
import {
	customFieldIdOf,
	doneTargetStatus,
	isCustomFieldName,
	needsInput,
	outcomeOfProblem,
	readWorkflowProblem,
	reopenTargetStatus,
	type AvailableTransitionResponse,
	type WorkflowStatusResponse,
} from '@/helpers/workflow'
import {describeApprovers} from '@/helpers/approvals'
import {error, getErrorText} from '@/message'

const props = defineProps<{
	taskId: number
	projectId: number
	done: boolean
	canWrite: boolean
}>()

const emit = defineEmits<{
	// The status change or a field saved by the dialog may have changed the task.
	changed: []
}>()

const {t} = useI18n({useScope: 'global'})

const workflowResult = useQuery(computed(() => workflowQuery(props.projectId)))
const hasWorkflow = computed(() => (workflowResult.data.value ?? null) !== null)
const statusResult = useQuery(computed(() => ({
	...taskStatusQuery(props.taskId),
	enabled: props.taskId > 0 && hasWorkflow.value,
})))
const fieldsResult = useQuery(computed(() => ({
	...customFieldsQuery(props.projectId),
	enabled: props.projectId > 0 && hasWorkflow.value,
})))
const change = useChangeTaskStatusMutation()

const taskStatus = computed(() => statusResult.data.value ?? null)
const currentStatus = computed(() => taskStatus.value?.status ?? null)
const transitions = computed(() => taskStatus.value?.transitions ?? [])
const visible = computed(() => hasWorkflow.value && taskStatus.value !== null)
const canChange = computed(() => props.canWrite && transitions.value.length > 0)

const showHistory = ref(false)

function fieldLabel(name: string): string {
	if (isCustomFieldName(name)) {
		const field = fieldsResult.data.value?.find(entry => entry.id === customFieldIdOf(name))
		return field?.title ?? name
	}
	return t(`workflow.fields.${name}`)
}

function requirementText(transition: AvailableTransitionResponse): string {
	const parts = transition.missing_fields.map(fieldLabel)
	if (transition.require_comment) {
		parts.push(t('workflow.menu.comment'))
	}
	return t('workflow.menu.needs', {items: parts.join(', ')})
}

function transitionLabel(transition: AvailableTransitionResponse): string {
	if (transition.requires_approval) {
		return t('approvals.menu.request', {status: transition.title || transition.to_status.title})
	}
	return transition.title || transition.to_status.title
}

function approverText(transition: AvailableTransitionResponse): string {
	const names = describeApprovers(transition.approvers)
	return names === '' ? t('approvals.menu.askNobody') : t('approvals.menu.willAsk', {names})
}

type DialogState = {
	toStatus: WorkflowStatusResponse
	missingFields: string[]
	commentRequired: boolean
	problem: string | null
}

const dialog = ref<DialogState | null>(null)

async function attempt(toStatus: WorkflowStatusResponse, comment = ''): Promise<boolean> {
	const taskId = props.taskId
	let succeeded = false
	try {
		const result = await change.mutateAsync({taskId, toStatusId: toStatus.id, comment})
		// 202: a request was made, the task is still in its status. The mutation announced it.
		succeeded = !result.approval
		if (props.taskId === taskId) {
			dialog.value = null
		}
	} catch (cause) {
		if (isRequestContextAbort(cause) || props.taskId !== taskId) {
			return false
		}
		const outcome = outcomeOfProblem(readWorkflowProblem(cause))
		if (outcome.kind === 'dialog') {
			dialog.value = {
				toStatus,
				missingFields: outcome.missingFields,
				commentRequired: outcome.commentRequired,
				problem: dialog.value ? t('workflow.dialog.stillMissing') : null,
			}
		} else {
			dialog.value = null
			error({message: outcome.kind === 'message' ? t(outcome.messageKey) : getErrorText(cause)})
		}
	}
	emit('changed')
	return succeeded
}

function choose(transition: AvailableTransitionResponse, close: () => void) {
	close()
	if (needsInput(transition)) {
		dialog.value = {
			toStatus: transition.to_status,
			missingFields: transition.missing_fields,
			commentRequired: transition.require_comment,
			problem: null,
		}
		return
	}
	attempt(transition.to_status)
}

async function submitDialog(comment: string) {
	if (dialog.value) {
		await attempt(dialog.value.toStatus, comment)
	}
}

// The done button of the task goes through the workflow so its rules and dialog apply.
// Resolves null when the normal update should run instead (no workflow, or nothing to move to),
// otherwise whether the task changed.
async function toggleDone(): Promise<boolean | null> {
	const workflow = workflowResult.data.value
	const current = currentStatus.value
	if (!workflow || !current) {
		return null
	}
	const target = props.done ? reopenTargetStatus(workflow) : doneTargetStatus(workflow)
	if (!target || target.id === current.id) {
		return null
	}
	const available = transitions.value.find(transition => transition.to_status.id === target.id)
	if (available && needsInput(available)) {
		dialog.value = {
			toStatus: target,
			missingFields: available.missing_fields,
			commentRequired: available.require_comment,
			problem: null,
		}
		return false
	}
	return attempt(target)
}

defineExpose({toggleDone, visible})
</script>

<template>
	<TaskPropertyRow
		v-if="visible"
		name="status"
		icon="list-check"
		:label="$t('workflow.task.label')"
		:is-set="true"
		:can-write="true"
	>
		<Dropdown v-if="canChange">
			<template #trigger="{toggleOpen, open}">
				<BaseButton
					class="status-trigger"
					:aria-expanded="open"
					aria-haspopup="menu"
					:aria-label="$t('workflow.task.change', {status: currentStatus?.title ?? $t('workflow.task.none')})"
					@click="toggleOpen"
				>
					<WorkflowStatusBadge
						v-if="currentStatus"
						:status="currentStatus"
					/>
					<span
						v-else
						class="status-none"
					>{{ $t('workflow.task.none') }}</span>
					<Icon
						icon="chevron-down"
						class="status-trigger__chevron"
					/>
				</BaseButton>
			</template>
			<template #default="{close}">
				<p class="menu-heading">
					{{ $t('workflow.menu.heading') }}
				</p>
				<BaseButton
					v-for="transition in transitions"
					:key="transition.to_status.id"
					class="menu-item"
					:class="{'has-requirements': needsInput(transition), 'needs-approval': transition.requires_approval}"
					:title="transition.requires_approval ? approverText(transition) : undefined"
					role="menuitem"
					@click="choose(transition, close)"
				>
					<span class="menu-item__main">
						<Icon
							v-if="transition.requires_approval"
							icon="user-check"
							class="menu-item__approval-icon"
						/>
						<span class="menu-item__label">{{ transitionLabel(transition) }}</span>
						<WorkflowStatusBadge
							v-if="transition.title || transition.requires_approval"
							:status="transition.to_status"
						/>
					</span>
					<span
						v-if="transition.requires_approval"
						class="menu-item__approvers"
					>
						{{ approverText(transition) }}
					</span>
					<span
						v-if="needsInput(transition)"
						class="menu-item__needs"
					>
						<Icon icon="exclamation-circle" />
						{{ requirementText(transition) }}
					</span>
				</BaseButton>
			</template>
		</Dropdown>
		<template v-else>
			<WorkflowStatusBadge
				v-if="currentStatus"
				:status="currentStatus"
			/>
			<span
				v-else
				class="status-none"
			>{{ $t('workflow.task.none') }}</span>
		</template>
		<BaseButton
			v-tooltip="$t('workflow.history.title')"
			class="history-toggle"
			:aria-expanded="showHistory"
			:aria-label="$t('workflow.history.title')"
			@click="showHistory = !showHistory"
		>
			<Icon icon="history" />
		</BaseButton>

		<template #editor>
			<TaskStatusHistory
				v-if="showHistory"
				:task-id="taskId"
			/>
		</template>
	</TaskPropertyRow>

	<TaskStatusDialog
		v-if="dialog"
		:task-id="taskId"
		:project-id="projectId"
		:to-status="dialog.toStatus"
		:missing-fields="dialog.missingFields"
		:comment-required="dialog.commentRequired"
		:problem="dialog.problem"
		:busy="change.isPending.value"
		@close="dialog = null"
		@submit="submitDialog"
	/>
</template>

<style lang="scss" scoped>
.status-trigger {
	display: inline-flex;
	align-items: center;
	gap: .375rem;
	max-inline-size: 100%;
	padding: .125rem .25rem;
	border-radius: 999px;

	&:hover,
	&:focus-visible {
		background: var(--grey-100);
	}

	&__chevron {
		font-size: .75rem;
		color: var(--grey-500);
	}
}

.status-none {
	color: var(--grey-500);
	font-style: italic;
}

.history-toggle {
	padding: .375rem;
	border-radius: $radius;
	color: var(--grey-500);

	&:hover,
	&:focus-visible,
	&[aria-expanded='true'] {
		color: var(--primary);
	}
}

.menu-heading {
	margin: 0;
	padding: .25rem 1rem .375rem;
	color: var(--grey-500);
	font-size: .75rem;
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: .03em;
}

.menu-item {
	// The menu's hover bridge is positioned, so unpositioned items would sit under it.
	position: relative;
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: .125rem;
	inline-size: 100%;
	min-inline-size: 14rem;
	max-inline-size: 22rem;
	padding: .375rem 1rem;
	color: var(--text);
	font-size: .875rem;
	text-align: start;

	&:hover,
	&:focus-visible {
		background: var(--grey-100);
	}

	&__main {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .5rem;
	}

	&__label {
		font-weight: 600;
	}

	&__approval-icon {
		color: var(--primary);
	}

	&__approvers {
		color: var(--grey-600);
		font-size: .75rem;
		white-space: normal;
	}

	&__needs {
		display: flex;
		align-items: baseline;
		gap: .375rem;
		color: var(--warning-dark, var(--grey-700));
		font-size: .75rem;
		white-space: normal;
	}
}
</style>
