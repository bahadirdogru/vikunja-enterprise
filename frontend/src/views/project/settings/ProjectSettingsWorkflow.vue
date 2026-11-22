<script setup lang="ts">
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {useTitle} from '@vueuse/core'
import {useQuery} from '@tanstack/vue-query'

import CreateEdit from '@/components/misc/CreateEdit.vue'
import ErrorMessage from '@/components/misc/Error.vue'
import Loading from '@/components/misc/Loading.vue'
import Message from '@/components/misc/Message.vue'
import WorkflowEditor from '@/components/workflow/WorkflowEditor.vue'
import XButton from '@/components/input/Button.vue'
import {workflowQuery} from '@/client/queries/workflow'
import {PERMISSIONS} from '@/constants/permissions'
import {useGoBack} from '@/composables/useGoBack'
import {useProject} from '@/composables/useProject'

const props = defineProps<{
	projectId: number
}>()

const {t} = useI18n({useScope: 'global'})
const goBack = useGoBack()
const editor = ref<InstanceType<typeof WorkflowEditor> | null>(null)
useTitle(() => t('workflow.title'))

// useProject snapshots its project, which is enough for the permission check.
const {project, error: projectError} = useProject(() => props.projectId)
const isAdmin = computed(() => project.value?.max_permission === PERMISSIONS.ADMIN)

const workflowResult = useQuery(computed(() => ({
	...workflowQuery(props.projectId),
	enabled: props.projectId > 0 && isAdmin.value,
})))
const loadError = computed(() => projectError.value ?? workflowResult.error.value)
// null is a loaded project without a workflow, undefined is still loading.
const loaded = computed(() => workflowResult.data.value !== undefined)
</script>

<template>
	<CreateEdit
		:title="$t('workflow.title')"
		:has-primary-action="false"
		:wide="true"
	>
		<ErrorMessage v-if="loadError" />
		<Message v-else-if="project && !isAdmin">
			{{ $t('workflow.onlyAdminsCanEdit') }}
		</Message>
		<Loading v-else-if="!loaded" />
		<WorkflowEditor
			v-else
			ref="editor"
			:key="projectId"
			:project-id="projectId"
			:workflow="workflowResult.data.value ?? null"
		/>
		<template #footer>
			<XButton
				v-if="editor?.hasWorkflow"
				variant="tertiary"
				class="has-text-danger"
				icon="power-off"
				@click="editor?.requestDisable()"
			>
				{{ $t('workflow.disable.action') }}
			</XButton>
			<span
				v-if="editor?.dirty"
				class="unsaved"
				role="status"
			>{{ $t('workflow.unsaved') }}</span>
			<span class="spacer" />
			<XButton
				variant="secondary"
				@click="goBack()"
			>
				{{ $t('misc.cancel') }}
			</XButton>
			<XButton
				v-if="editor?.isActive"
				class="mis-2"
				:loading="editor?.isSaving"
				icon="check"
				@click="editor?.save()"
			>
				{{ $t('workflow.save') }}
			</XButton>
		</template>
	</CreateEdit>
</template>

<style lang="scss" scoped>
.spacer {
	flex: 1;
}

.unsaved {
	color: var(--grey-600);
	font-size: .875rem;
}
</style>
