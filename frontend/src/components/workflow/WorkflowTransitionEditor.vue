<script setup lang="ts">
import {computed, useId} from 'vue'
import {useI18n} from 'vue-i18n'

import FormCheckbox from '@/components/input/FormCheckbox.vue'
import FormInput from '@/components/input/FormInput.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import XButton from '@/components/input/Button.vue'
import type {CustomFieldResponse} from '@/client/queries/customFields'
import {
	WORKFLOW_BUILTIN_FIELDS,
	activeStatuses,
	customFieldName,
	type DraftErrors,
	type TransitionDraft,
	type WorkflowDraft,
} from '@/helpers/workflow'

const props = defineProps<{
	draft: WorkflowDraft
	transition: TransitionDraft
	customFields: CustomFieldResponse[]
	teams: {id?: number, name?: string}[]
	errors: DraftErrors
	showErrors: boolean
}>()

const emit = defineEmits<{
	update: [patch: Partial<Omit<TransitionDraft, 'key'>>]
	remove: []
}>()

const {t} = useI18n({useScope: 'global'})
const uid = useId()

const statusOptions = computed(() => activeStatuses(props.draft).map(status => ({
	value: status.id,
	label: status.title,
})))

const fieldOptions = computed(() => [
	...WORKFLOW_BUILTIN_FIELDS.map(name => ({value: name, label: t(`workflow.fields.${name}`)})),
	...props.customFields.map(field => ({value: customFieldName(field.id), label: field.title})),
])

const permissionOptions = computed(() => [
	{value: 'write', label: t('workflow.transitions.permission.write')},
	{value: 'admin', label: t('workflow.transitions.permission.admin')},
])

const errorCodes = computed(() => props.showErrors ? props.errors.transitions[props.transition.key] ?? [] : [])

function toggle(list: readonly (string | number)[], value: string | number, on: boolean) {
	const rest = list.filter(entry => entry !== value)
	return on ? [...rest, value] : rest
}
</script>

<template>
	<div class="transition-editor">
		<div class="transition-editor__pair">
			<FormSelect
				:model-value="transition.from_status_id"
				:options="statusOptions"
				:aria-label="$t('workflow.transitions.from')"
				@update:modelValue="value => emit('update', {from_status_id: Number(value)})"
			/>
			<Icon
				icon="arrow-right"
				class="transition-editor__arrow"
			/>
			<FormSelect
				:model-value="transition.to_status_id"
				:options="statusOptions"
				:aria-label="$t('workflow.transitions.to')"
				@update:modelValue="value => emit('update', {to_status_id: Number(value)})"
			/>
		</div>
		<p
			v-for="code in errorCodes"
			:key="code"
			class="help is-danger"
			role="alert"
		>
			{{ $t(`workflow.validation.${code}`) }}
		</p>

		<div class="transition-editor__grid">
			<div class="field">
				<label
					class="label"
					:for="`${uid}-title`"
				>{{ $t('workflow.transitions.buttonLabel') }}</label>
				<FormInput
					:id="`${uid}-title`"
					:model-value="transition.title"
					:placeholder="$t('workflow.transitions.buttonLabelPlaceholder')"
					@update:modelValue="value => emit('update', {title: String(value)})"
				/>
			</div>
			<div class="field">
				<label
					class="label"
					:for="`${uid}-permission`"
				>{{ $t('workflow.transitions.minPermission') }}</label>
				<FormSelect
					:id="`${uid}-permission`"
					:model-value="transition.min_permission"
					:options="permissionOptions"
					@update:modelValue="value => emit('update', {min_permission: value as TransitionDraft['min_permission']})"
				/>
			</div>
		</div>

		<FormCheckbox
			:model-value="transition.require_comment"
			@update:modelValue="value => emit('update', {require_comment: value})"
		>
			{{ $t('workflow.transitions.requireComment') }}
		</FormCheckbox>

		<fieldset class="transition-editor__fieldset">
			<legend class="label">
				{{ $t('workflow.transitions.requiredFields') }}
			</legend>
			<p class="help">
				{{ $t('workflow.transitions.requiredFieldsHint') }}
			</p>
			<div class="check-grid">
				<FormCheckbox
					v-for="option in fieldOptions"
					:key="option.value"
					:model-value="transition.required_fields.includes(option.value)"
					@update:modelValue="on => emit('update', {
						required_fields: toggle(transition.required_fields, option.value, on) as string[],
					})"
				>
					{{ option.label }}
				</FormCheckbox>
			</div>
		</fieldset>

		<fieldset class="transition-editor__fieldset">
			<legend class="label">
				{{ $t('workflow.transitions.teams') }}
			</legend>
			<p class="help">
				{{ $t('workflow.transitions.teamsHint') }}
			</p>
			<p
				v-if="teams.length === 0"
				class="help"
			>
				{{ $t('workflow.transitions.noTeams') }}
			</p>
			<div class="check-grid">
				<FormCheckbox
					v-for="team in teams"
					:key="team.id"
					:model-value="transition.team_ids.includes(team.id ?? 0)"
					@update:modelValue="on => emit('update', {
						team_ids: toggle(transition.team_ids, team.id ?? 0, on) as number[],
					})"
				>
					{{ team.name }}
				</FormCheckbox>
			</div>
		</fieldset>

		<div class="transition-editor__footer">
			<XButton
				variant="tertiary"
				icon="trash-alt"
				class="has-text-danger"
				@click="emit('remove')"
			>
				{{ $t('workflow.transitions.remove') }}
			</XButton>
		</div>
	</div>
</template>

<style lang="scss" scoped>
.transition-editor {
	display: flex;
	flex-direction: column;
	gap: .75rem;

	&__pair {
		display: grid;
		grid-template-columns: 1fr auto 1fr;
		align-items: center;
		gap: .5rem;
	}

	&__arrow {
		color: var(--grey-500);
	}

	&__grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
		gap: .75rem;

		.field {
			margin: 0;
		}
	}

	&__fieldset {
		margin: 0;
		padding: 0;
		border: 0;
		min-inline-size: 0;
	}

	&__footer {
		display: flex;
		justify-content: flex-end;
	}
}

.check-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr));
	gap: .125rem .75rem;
	max-block-size: 12rem;
	overflow-y: auto;
	padding-block-start: .5rem;

	:deep(label.checkbox:not(:last-child)) {
		margin-block-end: .25rem;
	}
}
</style>
