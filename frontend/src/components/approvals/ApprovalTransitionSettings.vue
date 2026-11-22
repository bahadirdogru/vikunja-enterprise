<script setup lang="ts">
import {computed, ref, useId} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'
import type {IconProp} from '@fortawesome/fontawesome-svg-core'

import FormCheckbox from '@/components/input/FormCheckbox.vue'
import FormInput from '@/components/input/FormInput.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import XButton from '@/components/input/Button.vue'
import {projectTeamSharesQuery} from '@/client/queries/projectShares'
import {projectUserSearchQuery} from '@/client/queries/userSearch'
import type {CustomFieldResponse} from '@/client/queries/customFields'
import {
	APPROVAL_MODES,
	approvalDraftToDocument,
	newApproverSource,
	type ApprovalDraft,
	type ApprovalMode,
	type ApprovalServerField,
	type ApproverSourceDraft,
	type ApproverSourceType,
} from '@/helpers/approvals'
import {activeStatuses, type TransitionDraft, type WorkflowDraft} from '@/helpers/workflow'
import {getDisplayName} from '@/helpers/user'

const props = defineProps<{
	draft: WorkflowDraft
	transition: TransitionDraft
	projectId: number
	customFields: CustomFieldResponse[]
	// Validation codes of this transition that start with "approval".
	errorCodes: string[]
	// What the server said about the approval of the last save attempt.
	serverError?: {field: ApprovalServerField | null, message: string} | null
}>()

const emit = defineEmits<{
	update: [patch: Partial<Omit<TransitionDraft, 'key'>>]
}>()

const {t} = useI18n({useScope: 'global'})
const uid = useId()

const approval = computed(() => props.transition.approval)

const membersResult = useQuery(computed(() => ({
	...projectUserSearchQuery(props.projectId, ''),
	enabled: props.projectId > 0 && props.transition.requires_approval,
})))
const teamsResult = useQuery(computed(() => ({
	...projectTeamSharesQuery(props.projectId),
	enabled: props.projectId > 0 && props.transition.requires_approval,
})))
const members = computed(() => membersResult.data.value ?? [])
const teams = computed(() => (teamsResult.data.value ?? []).flatMap(team => team.id ? [{id: team.id, name: team.name ?? ''}] : []))
const userFields = computed(() => props.customFields.filter(field => field.type === 'user'))

function patchApproval(change: Partial<ApprovalDraft>) {
	emit('update', {approval: {...approval.value, ...change}})
}

function setRequired(on: boolean) {
	emit('update', {requires_approval: on})
}

const modes = computed(() => APPROVAL_MODES.map(mode => ({
	mode,
	label: t(`workflow.approval.mode.${mode}`),
	hint: t(`workflow.approval.mode.${mode}Hint`),
})))

// ---- Approver sources ----

const sourceTypes = computed(() => [
	{value: 'user', label: t('workflow.approval.source.user')},
	{value: 'team', label: t('workflow.approval.source.team')},
	{value: 'assignees', label: t('workflow.approval.source.assignees')},
	{value: 'custom_field', label: t('workflow.approval.source.customField')},
])

const newType = ref<ApproverSourceType>('user')
const newId = ref(0)

const idOptions = computed(() => {
	switch (newType.value) {
		case 'user':
			return members.value.flatMap(user => user.id ? [{value: user.id, label: getDisplayName(user)}] : [])
		case 'team':
			return teams.value.map(team => ({value: team.id, label: team.name}))
		case 'custom_field':
			return userFields.value.map(field => ({value: field.id, label: field.title}))
		default:
			return []
	}
})
const needsId = computed(() => newType.value !== 'assignees')
const canAdd = computed(() => !needsId.value || newId.value > 0)

const idLabel = computed(() => t(`workflow.approval.pick.${newType.value}`))
const noOptions = computed(() => needsId.value && idOptions.value.length === 0)

function onTypeChange(value: string | number) {
	newType.value = value as ApproverSourceType
	newId.value = 0
}

function addSource() {
	if (!canAdd.value) {
		return
	}
	const source = newApproverSource(newType.value, newId.value)
	// The same source twice would only be collapsed by the server.
	const exists = approval.value.approvers.some(entry => entry.type === source.type && entry.id === source.id)
	if (!exists) {
		patchApproval({approvers: [...approval.value.approvers, source]})
	}
	newId.value = 0
}

function removeSource(key: string) {
	patchApproval({approvers: approval.value.approvers.filter(source => source.key !== key)})
}

function sourceLabel(source: ApproverSourceDraft): string {
	switch (source.type) {
		case 'assignees':
			return t('workflow.approval.sourceLabel.assignees')
		case 'team': {
			const team = teams.value.find(entry => entry.id === source.id)
			return t('workflow.approval.sourceLabel.team', {name: team?.name ?? `#${source.id}`})
		}
		case 'custom_field': {
			const field = props.customFields.find(entry => entry.id === source.id)
			return t('workflow.approval.sourceLabel.customField', {name: field?.title ?? `#${source.id}`})
		}
		default: {
			const user = members.value.find(entry => entry.id === source.id)
			return t('workflow.approval.sourceLabel.user', {name: user ? getDisplayName(user) : `#${source.id}`})
		}
	}
}

const sourceIcon = (type: ApproverSourceType): IconProp => ({
	user: 'user',
	team: 'users',
	assignees: 'user-check',
	custom_field: 'list-check',
}[type] as IconProp)

// ---- Rejected status ----

const rejectedOptions = computed(() => [
	{value: 0, label: t('workflow.approval.rejectedStay')},
	...activeStatuses(props.draft)
		.filter(status => status.id !== props.transition.to_status_id)
		.map(status => ({value: status.id, label: status.title})),
])

const messageFor = (field: ApprovalServerField) => props.serverError?.field === field ? props.serverError.message : null
const has = (code: string) => props.errorCodes.includes(code)

// Used by the tests: what the document of this transition will carry.
defineExpose({document: () => approvalDraftToDocument(approval.value)})
</script>

<template>
	<fieldset class="approval-settings">
		<legend class="label">
			{{ $t('workflow.approval.title') }}
		</legend>
		<FormCheckbox
			:model-value="transition.requires_approval"
			class="approval-settings__toggle"
			@update:modelValue="setRequired"
		>
			{{ $t('workflow.approval.requires') }}
		</FormCheckbox>
		<p class="help">
			{{ $t('workflow.approval.requiresHint') }}
		</p>

		<div
			v-if="transition.requires_approval"
			class="approval-settings__body"
		>
			<fieldset class="approval-settings__group">
				<legend class="label">
					{{ $t('workflow.approval.mode.title') }}
				</legend>
				<label
					v-for="entry in modes"
					:key="entry.mode"
					class="approval-settings__mode"
				>
					<input
						type="radio"
						:name="`${uid}-mode`"
						:value="entry.mode"
						:checked="approval.mode === entry.mode"
						@change="patchApproval({mode: entry.mode as ApprovalMode})"
					>
					<span>
						<strong>{{ entry.label }}</strong>
						<span class="help">{{ entry.hint }}</span>
					</span>
				</label>
				<p
					v-if="messageFor('mode')"
					class="help is-danger"
					role="alert"
				>
					{{ messageFor('mode') }}
				</p>
			</fieldset>

			<fieldset class="approval-settings__group">
				<legend class="label">
					{{ $t('workflow.approval.approvers') }}
				</legend>
				<p class="help">
					{{ $t('workflow.approval.approversHint') }}
				</p>
				<ul
					v-if="approval.approvers.length > 0"
					class="approval-settings__sources"
				>
					<li
						v-for="source in approval.approvers"
						:key="source.key"
						class="approval-settings__source"
					>
						<Icon
							:icon="sourceIcon(source.type)"
							class="approval-settings__source-icon"
						/>
						<span class="approval-settings__source-label">{{ sourceLabel(source) }}</span>
						<XButton
							variant="tertiary"
							icon="times"
							:aria-label="$t('workflow.approval.removeSource', {name: sourceLabel(source)})"
							@click="removeSource(source.key)"
						/>
					</li>
				</ul>
				<p
					v-else
					class="help"
				>
					{{ $t('workflow.approval.noApprovers') }}
				</p>
				<div class="approval-settings__add">
					<div class="field">
						<label
							class="label is-small"
							:for="`${uid}-type`"
						>{{ $t('workflow.approval.addKind') }}</label>
						<FormSelect
							:id="`${uid}-type`"
							:model-value="newType"
							:options="sourceTypes"
							@update:modelValue="onTypeChange"
						/>
					</div>
					<div
						v-if="needsId"
						class="field"
					>
						<label
							class="label is-small"
							:for="`${uid}-id`"
						>{{ idLabel }}</label>
						<FormSelect
							:id="`${uid}-id`"
							:model-value="newId"
							:options="[{value: 0, label: $t('workflow.approval.choose')}, ...idOptions]"
							:disabled="noOptions"
							@update:modelValue="value => newId = Number(value)"
						/>
					</div>
					<XButton
						variant="secondary"
						icon="plus"
						:disabled="!canAdd"
						@click="addSource"
					>
						{{ $t('workflow.approval.add') }}
					</XButton>
				</div>
				<p
					v-if="noOptions"
					class="help"
				>
					{{ $t(`workflow.approval.noOptions.${newType}`) }}
				</p>
				<p
					v-if="has('approvalApproversRequired')"
					class="help is-danger"
					role="alert"
				>
					{{ $t('workflow.validation.approvalApproversRequired') }}
				</p>
				<p
					v-if="has('approvalSourceIncomplete')"
					class="help is-danger"
					role="alert"
				>
					{{ $t('workflow.validation.approvalSourceIncomplete') }}
				</p>
				<p
					v-if="messageFor('approvers')"
					class="help is-danger"
					role="alert"
				>
					{{ messageFor('approvers') }}
				</p>
			</fieldset>

			<div class="approval-settings__grid">
				<div class="field">
					<label
						class="label"
						:for="`${uid}-rejected`"
					>{{ $t('workflow.approval.rejectedStatus') }}</label>
					<FormSelect
						:id="`${uid}-rejected`"
						:model-value="approval.rejected_status_id"
						:options="rejectedOptions"
						@update:modelValue="value => patchApproval({rejected_status_id: Number(value)})"
					/>
					<p class="help">
						{{ $t('workflow.approval.rejectedStatusHint') }}
					</p>
					<p
						v-if="has('approvalRejectedStatusSame') || has('approvalRejectedStatusGone')"
						class="help is-danger"
						role="alert"
					>
						{{ $t(has('approvalRejectedStatusSame')
							? 'workflow.validation.approvalRejectedStatusSame'
							: 'workflow.validation.approvalRejectedStatusGone') }}
					</p>
					<p
						v-if="messageFor('rejectedStatus')"
						class="help is-danger"
						role="alert"
					>
						{{ messageFor('rejectedStatus') }}
					</p>
				</div>
				<div class="field">
					<label
						class="label"
						:for="`${uid}-due`"
					>{{ $t('workflow.approval.dueIn') }}</label>
					<FormInput
						:id="`${uid}-due`"
						:model-value="approval.due_in"
						:placeholder="$t('workflow.approval.dueInPlaceholder')"
						:error="has('approvalDueInvalid') ? $t('workflow.validation.approvalDueInvalid') : null"
						@update:modelValue="value => patchApproval({due_in: String(value)})"
					/>
					<p class="help">
						{{ $t('workflow.approval.dueInHint') }}
					</p>
					<p
						v-if="messageFor('dueIn')"
						class="help is-danger"
						role="alert"
					>
						{{ messageFor('dueIn') }}
					</p>
				</div>
			</div>

			<FormCheckbox
				:model-value="approval.allow_self"
				@update:modelValue="value => patchApproval({allow_self: value})"
			>
				{{ $t('workflow.approval.allowSelf') }}
			</FormCheckbox>
		</div>
	</fieldset>
</template>

<style lang="scss" scoped>
.approval-settings {
	margin: 0;
	padding: .75rem;
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	min-inline-size: 0;

	&__body {
		display: flex;
		flex-direction: column;
		gap: .75rem;
		margin-block-start: .75rem;
	}

	&__group {
		margin: 0;
		padding: 0;
		border: 0;
		min-inline-size: 0;
	}

	&__mode {
		display: flex;
		align-items: flex-start;
		gap: .5rem;
		padding-block: .25rem;
		cursor: pointer;

		.help {
			display: block;
			margin: 0;
		}
	}

	&__sources {
		display: flex;
		flex-wrap: wrap;
		gap: .375rem;
		margin: .375rem 0;
		padding: 0;
		list-style: none;
	}

	&__source {
		display: inline-flex;
		align-items: center;
		gap: .375rem;
		max-inline-size: 100%;
		padding: .125rem .25rem .125rem .625rem;
		background: var(--grey-100);
		border-radius: 999px;
		font-size: .875rem;
	}

	&__source-icon {
		color: var(--grey-500);
	}

	&__source-label {
		overflow-wrap: anywhere;
	}

	&__add {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: .5rem;

		.field {
			margin: 0;
			min-inline-size: 10rem;
		}
	}

	&__grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
		gap: .75rem;

		.field {
			margin: 0;
		}
	}
}
</style>
