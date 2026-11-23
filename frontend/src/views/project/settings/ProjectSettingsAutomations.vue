<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {useTitle} from '@vueuse/core'
import {onBeforeRouteLeave} from 'vue-router'
import {useQuery} from '@tanstack/vue-query'

import RuleEditor from '@/components/automation/RuleEditor.vue'
import RuleList from '@/components/automation/RuleList.vue'
import RunsView from '@/components/automation/RunsView.vue'
import SecretsSection from '@/components/automation/SecretsSection.vue'
import XButton from '@/components/input/Button.vue'
import CreateEdit from '@/components/misc/CreateEdit.vue'
import ErrorMessage from '@/components/misc/Error.vue'
import Loading from '@/components/misc/Loading.vue'
import Message from '@/components/misc/Message.vue'
import Modal from '@/components/misc/Modal.vue'
import {automationCatalogQuery, automationRulesQuery, type AutomationRuleResponse} from '@/client/queries/automation'
import {automationSecretsQuery} from '@/client/queries/automationSecrets'
import {customFieldsQuery} from '@/client/queries/customFields'
import {formsQuery} from '@/client/queries/forms'
import {workflowQuery} from '@/client/queries/workflow'
import {PERMISSIONS} from '@/constants/permissions'
import {useGoBack} from '@/composables/useGoBack'
import {useProject} from '@/composables/useProject'
import {clone, draftFromRule, type RuleDraft} from '@/helpers/automation'
import {availableTemplates, type AvailableTemplate} from '@/helpers/automationTemplates'

const props = defineProps<{
	projectId: number
}>()

const {t} = useI18n({useScope: 'global'})
const goBack = useGoBack()
useTitle(() => t('automation.title'))

const {project, error: projectError} = useProject(() => props.projectId)
const isAdmin = computed(() => project.value?.max_permission === PERMISSIONS.ADMIN)

const enabled = computed(() => props.projectId > 0 && isAdmin.value)
const catalogResult = useQuery(computed(() => ({...automationCatalogQuery(props.projectId), enabled: enabled.value})))
const rulesResult = useQuery(computed(() => ({...automationRulesQuery(props.projectId), enabled: enabled.value})))
const fieldsResult = useQuery(computed(() => ({...customFieldsQuery(props.projectId), enabled: enabled.value})))
// A failing list leaves the rule editor with a plain text input for secret names.
const secretsResult = useQuery(computed(() => ({...automationSecretsQuery(props.projectId), enabled: enabled.value, retry: false})))
const secretNames = computed(() => secretsResult.data.value ? secretsResult.data.value.map(secret => secret.name) : null)
// Only the form.submitted trigger needs the forms; a failing list leaves its option a number input.
const formsResult = useQuery(computed(() => ({...formsQuery(props.projectId), enabled: enabled.value, retry: false})))
const forms = computed(() => formsResult.data.value ? formsResult.data.value.map(form => ({id: form.id, title: form.title})) : null)
const workflowResult = useQuery(computed(() => ({...workflowQuery(props.projectId), enabled: enabled.value})))

const loadError = computed(() => projectError.value ?? catalogResult.error.value ?? rulesResult.error.value
	?? fieldsResult.error.value ?? workflowResult.error.value)
const loaded = computed(() => catalogResult.data.value !== undefined
	&& rulesResult.data.value !== undefined
	&& fieldsResult.data.value !== undefined
	&& workflowResult.data.value !== undefined)

const customFields = computed(() => fieldsResult.data.value ?? [])
const statuses = computed(() => (workflowResult.data.value?.statuses ?? [])
	.filter(status => !status.archived)
	.map(status => ({id: status.id, title: status.title, category: status.category})))

const templates = computed<AvailableTemplate[]>(() => catalogResult.data.value
	? availableTemplates({
		catalog: catalogResult.data.value,
		statuses: statuses.value,
		customFields: customFields.value.map(field => ({id: field.id, title: field.title, type: field.type})),
	}, t)
	: [])

type Mode =
	| {kind: 'list'}
	| {kind: 'edit', key: number, rule: AutomationRuleResponse | null, preset: RuleDraft | null}
	| {kind: 'runs', rule: AutomationRuleResponse}

const mode = ref<Mode>({kind: 'list'})
const editor = ref<InstanceType<typeof RuleEditor> | null>(null)
let sessions = 0

function openEditor(rule: AutomationRuleResponse | null, preset: RuleDraft | null = null) {
	mode.value = {kind: 'edit', key: ++sessions, rule, preset}
}

function duplicate(rule: AutomationRuleResponse) {
	const draft = clone(draftFromRule(rule))
	draft.title = t('automation.copyOf', {title: rule.title})
	draft.enabled = false
	openEditor(null, draft)
}

function fromTemplate(template: AvailableTemplate) {
	openEditor(null, template.draft)
}

// ---- unsaved changes ------------------------------------------------------------------------------------------
// What would be lost by leaving: unsaved changes, or the one-time webhook address that was not copied.
const leaveReason = computed<'dirty' | 'address' | null>(() => {
	if (mode.value.kind !== 'edit') return null
	if (editor.value?.dirty === true) return 'dirty'
	return editor.value?.addressUncopied === true ? 'address' : null
})
const dirty = computed(() => leaveReason.value !== null)
const confirmLeave = ref(false)
let answerLeave: ((leave: boolean) => void) | null = null
let afterConfirm: (() => void) | null = null

function backToList() {
	if (dirty.value) {
		afterConfirm = () => { mode.value = {kind: 'list'} }
		confirmLeave.value = true
		return
	}
	mode.value = {kind: 'list'}
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

const editingTitle = computed(() => mode.value.kind === 'edit'
	? (mode.value.rule ? t('automation.editRule') : t('automation.newRule'))
	: t('automation.title'))
</script>

<template>
	<CreateEdit
		:title="mode.kind === 'edit' ? editingTitle : $t('automation.title')"
		:has-primary-action="false"
		:wide="true"
	>
		<ErrorMessage v-if="loadError" />
		<Message v-else-if="project && !isAdmin">
			{{ $t('automation.onlyAdminsCanEdit') }}
		</Message>
		<Loading v-else-if="!loaded" />
		<template v-else>
			<RuleList
				v-if="mode.kind === 'list'"
				:rules="rulesResult.data.value ?? []"
				:catalog="catalogResult.data.value!"
				:templates="templates"
				@create="openEditor(null)"
				@template="fromTemplate"
				@edit="openEditor($event)"
				@runs="mode = {kind: 'runs', rule: $event}"
				@duplicate="duplicate"
			/>
			<SecretsSection
				v-if="mode.kind === 'list'"
				:project-id="projectId"
			/>
			<RuleEditor
				v-else-if="mode.kind === 'edit'"
				ref="editor"
				:key="mode.key"
				:project-id="projectId"
				:catalog="catalogResult.data.value!"
				:custom-fields="customFields"
				:statuses="statuses"
				:secrets="secretNames"
				:forms="forms"
				:rule="mode.rule"
				:preset="mode.preset"
				@close="backToList"
			/>
			<RunsView
				v-else
				:project-id="projectId"
				:rule="mode.rule"
				:catalog="catalogResult.data.value!"
				@back="mode = {kind: 'list'}"
			/>
		</template>

		<template #footer>
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
				<span>{{ leaveReason === 'address' ? $t('automation.leave.addressTitle') : $t('automation.leave.title') }}</span>
			</template>
			<template #text>
				<p>{{ leaveReason === 'address' ? $t('automation.leave.addressText') : $t('automation.leave.text') }}</p>
			</template>
		</Modal>
	</CreateEdit>
</template>

<style lang="scss" scoped>
.spacer {
	flex: 1;
}
</style>
