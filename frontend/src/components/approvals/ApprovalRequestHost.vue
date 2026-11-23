<script setup lang="ts">
import {ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'

import TaskStatusDialog from '@/components/workflow/TaskStatusDialog.vue'
import {queryClient} from '@/client/queryClient'
import {ensureTask} from '@/client/queries/tasks'
import {workflowQuery} from '@/client/queries/workflow'
import {approvalRequestTarget, type ApprovalRequestTarget} from '@/composables/approvalRequired'
import {useStatusTransition} from '@/composables/useStatusTransition'
import {error, getErrorText} from '@/message'

// Starts the approval-requiring transition a refused "mark as done" pointed to. It is the same flow
// as the status menu of the task: required fields and comments open the same dialog, and the
// mutation announces the request.

const {t} = useI18n({useScope: 'global'})

// The request being worked on. approvalRequestTarget is only the inbox: a target that arrives while this is set
// is dropped, so the dialog never pairs the status of one task with another task.
const active = ref<ApprovalRequestTarget | null>(null)
const {change, dialog, attempt, submitDialog} = useStatusTransition(() => active.value?.taskId ?? 0)

// The project of the task, which the target may not have carried.
const projectId = ref(0)
let starting = false

function finish() {
	dialog.value = null
	active.value = null
	approvalRequestTarget.value = null
}

function close() {
	finish()
}

async function start(target: ApprovalRequestTarget) {
	starting = true
	active.value = target
	try {
		projectId.value = target.projectId || (await ensureTask(target.taskId)).project_id
		const workflow = await queryClient.ensureQueryData(workflowQuery(projectId.value))
		const status = workflow?.statuses.find(entry => entry.id === target.toStatusId)
		if (!status) {
			error({message: t('workflow.errors.statusGone')})
			finish()
			return
		}
		await attempt(status)
		// A dialog stays open for the missing input; otherwise the request is made and we are done.
		if (!dialog.value) {
			finish()
		}
	} catch (cause) {
		error({message: getErrorText(cause)})
		finish()
	} finally {
		starting = false
	}
}

watch(approvalRequestTarget, target => {
	if (!target) {
		return
	}
	if (starting || active.value) {
		// Busy with another request: ignore this one.
		if (target !== active.value) {
			approvalRequestTarget.value = null
		}
		return
	}
	start(target)
})
</script>

<template>
	<TaskStatusDialog
		v-if="dialog && active"
		:task-id="active.taskId"
		:project-id="projectId"
		:to-status="dialog.toStatus"
		:missing-fields="dialog.missingFields"
		:comment-required="dialog.commentRequired"
		:problem="dialog.problem"
		:busy="change.isPending.value"
		@close="close"
		@submit="submitDialog"
	/>
</template>
