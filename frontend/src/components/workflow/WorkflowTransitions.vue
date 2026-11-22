<script setup lang="ts">
import {computed, ref} from 'vue'

import BaseButton from '@/components/base/BaseButton.vue'
import FormCheckbox from '@/components/input/FormCheckbox.vue'
import XButton from '@/components/input/Button.vue'
import WorkflowStatusBadge from '@/components/workflow/WorkflowStatusBadge.vue'
import WorkflowTransitionEditor from '@/components/workflow/WorkflowTransitionEditor.vue'
import WorkflowTransitionMatrix from '@/components/workflow/WorkflowTransitionMatrix.vue'
import type {CustomFieldResponse} from '@/client/queries/customFields'
import {
	activeStatuses,
	addTransition,
	newTransition,
	removeTransition,
	updateTransition,
	type DraftErrors,
	type TransitionDraft,
	type WorkflowDraft,
} from '@/helpers/workflow'

const props = defineProps<{
	modelValue: WorkflowDraft
	customFields: CustomFieldResponse[]
	teams: {id?: number, name?: string}[]
	errors: DraftErrors
	showErrors: boolean
}>()

const emit = defineEmits<{
	'update:modelValue': [value: WorkflowDraft]
}>()

const mode = ref<'list' | 'matrix'>('list')
const openKey = ref<string | null>(null)

const statuses = computed(() => activeStatuses(props.modelValue))
const statusById = computed(() => new Map(statuses.value.map(status => [status.id, status])))
const openTransition = computed(() => props.modelValue.transitions.find(transition => transition.key === openKey.value))

function summary(transition: TransitionDraft): string[] {
	const parts: string[] = []
	if (transition.require_comment) parts.push('comment')
	if (transition.required_fields.length > 0) parts.push('fields')
	if (transition.team_ids.length > 0) parts.push('teams')
	if (transition.min_permission === 'admin') parts.push('admin')
	return parts
}

function add() {
	const [first, second] = statuses.value
	if (!first || !second) {
		return
	}
	// Prefer a pair that is not listed yet.
	const free = statuses.value.flatMap(from => statuses.value
		.filter(to => to.id !== from.id)
		.map(to => [from.id, to.id] as const))
		.find(([from, to]) => !props.modelValue.transitions
			.some(existing => existing.from_status_id === from && existing.to_status_id === to))
	const [from, to] = free ?? [first.id, second.id]
	const created = newTransition(from, to)
	emit('update:modelValue', addTransition(props.modelValue, created))
	openKey.value = created.key
}

function patch(key: string, change: Partial<Omit<TransitionDraft, 'key'>>) {
	emit('update:modelValue', updateTransition(props.modelValue, key, change))
}

function remove(key: string) {
	emit('update:modelValue', removeTransition(props.modelValue, key))
	if (openKey.value === key) {
		openKey.value = null
	}
}

function toggleOpen(key: string) {
	openKey.value = openKey.value === key ? null : key
}
</script>

<template>
	<section
		class="workflow-transitions"
		aria-labelledby="workflow-transitions-title"
	>
		<div class="section-head">
			<h3
				id="workflow-transitions-title"
				class="section-head__title"
			>
				{{ $t('workflow.transitions.title') }}
			</h3>
		</div>

		<div class="restrict">
			<FormCheckbox
				:model-value="modelValue.restrict_transitions"
				@update:modelValue="value => emit('update:modelValue', {...modelValue, restrict_transitions: value})"
			>
				{{ $t('workflow.transitions.restrict') }}
			</FormCheckbox>
			<p class="section-head__hint">
				{{ modelValue.restrict_transitions
					? $t('workflow.transitions.restrictOn')
					: $t('workflow.transitions.restrictOff') }}
			</p>
		</div>

		<div class="toolbar">
			<div
				class="view-switch"
				role="group"
				:aria-label="$t('workflow.transitions.view')"
			>
				<BaseButton
					class="view-switch__button"
					:class="{'is-active': mode === 'list'}"
					:aria-pressed="mode === 'list'"
					@click="mode = 'list'"
				>
					<Icon icon="list" />
					{{ $t('workflow.transitions.viewList') }}
				</BaseButton>
				<BaseButton
					class="view-switch__button"
					:class="{'is-active': mode === 'matrix'}"
					:aria-pressed="mode === 'matrix'"
					@click="mode = 'matrix'"
				>
					<Icon icon="table-cells" />
					{{ $t('workflow.transitions.viewMatrix') }}
				</BaseButton>
			</div>
			<XButton
				icon="plus"
				variant="secondary"
				@click="add"
			>
				{{ $t('workflow.transitions.add') }}
			</XButton>
		</div>

		<template v-if="mode === 'matrix'">
			<p class="section-head__hint">
				{{ $t('workflow.matrix.hint') }}
			</p>
			<WorkflowTransitionMatrix
				:model-value="modelValue"
				:open-key="openKey"
				@update:modelValue="value => emit('update:modelValue', value)"
				@open="key => openKey = key"
			/>
			<div
				v-if="openTransition"
				class="transition-card is-open"
			>
				<WorkflowTransitionEditor
					:draft="modelValue"
					:transition="openTransition"
					:custom-fields="customFields"
					:teams="teams"
					:errors="errors"
					:show-errors="showErrors"
					@update="change => patch(openTransition!.key, change)"
					@remove="remove(openTransition!.key)"
				/>
			</div>
		</template>

		<template v-else>
			<p
				v-if="modelValue.transitions.length === 0"
				class="empty"
			>
				{{ modelValue.restrict_transitions
					? $t('workflow.transitions.emptyRestricted')
					: $t('workflow.transitions.empty') }}
			</p>
			<ul class="transition-list">
				<li
					v-for="transition in modelValue.transitions"
					:key="transition.key"
					class="transition-card"
					:class="{'is-open': openKey === transition.key, 'has-error': showErrors && errors.transitions[transition.key]}"
				>
					<button
						type="button"
						class="transition-card__head"
						:aria-expanded="openKey === transition.key"
						@click="toggleOpen(transition.key)"
					>
						<WorkflowStatusBadge
							v-if="statusById.get(transition.from_status_id)"
							:status="statusById.get(transition.from_status_id)!"
						/>
						<Icon
							icon="arrow-right"
							class="transition-card__arrow"
						/>
						<WorkflowStatusBadge
							v-if="statusById.get(transition.to_status_id)"
							:status="statusById.get(transition.to_status_id)!"
						/>
						<span
							v-if="transition.title"
							class="transition-card__label"
						>“{{ transition.title }}”</span>
						<span class="transition-card__tags">
							<span
								v-for="tag in summary(transition)"
								:key="tag"
								class="tag"
							>{{ $t(`workflow.transitions.tags.${tag}`) }}</span>
						</span>
						<Icon
							:icon="openKey === transition.key ? 'chevron-up' : 'chevron-down'"
							class="transition-card__chevron"
						/>
					</button>
					<div
						v-if="openKey === transition.key"
						class="transition-card__body"
					>
						<WorkflowTransitionEditor
							:draft="modelValue"
							:transition="transition"
							:custom-fields="customFields"
							:teams="teams"
							:errors="errors"
							:show-errors="showErrors"
							@update="change => patch(transition.key, change)"
							@remove="remove(transition.key)"
						/>
					</div>
				</li>
			</ul>
		</template>
	</section>
</template>

<style lang="scss" scoped>
.section-head {
	margin-block-end: .5rem;

	&__title {
		margin: 0;
		font-size: 1.125rem;
	}

	&__hint {
		margin: .125rem 0 .5rem;
		color: var(--grey-600);
		font-size: .875rem;
	}
}

.restrict {
	margin-block-end: .75rem;
	padding: .75rem;
	border-radius: $radius;
	background: var(--grey-100);

	:deep(label.checkbox) {
		margin-block-end: .25rem;
		font-weight: 600;
	}

	.section-head__hint {
		margin: 0;
	}
}

.toolbar {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: space-between;
	gap: .5rem;
	margin-block-end: .75rem;
}

.view-switch {
	display: inline-flex;
	overflow: hidden;
	border: 1px solid var(--grey-300);
	border-radius: $radius;

	&__button {
		display: inline-flex;
		align-items: center;
		gap: .5rem;
		padding: .5rem 1rem;
		background: var(--scheme-main);
		color: var(--text);
		font-size: .875rem;
		font-weight: 600;
		text-transform: uppercase;

		&:hover,
		&:focus-visible {
			background: var(--grey-100);
		}

		&.is-active {
			background: var(--primary);
			color: var(--primary-invert);
		}
	}
}

.empty {
	color: var(--grey-600);
	font-size: .875rem;
}

.transition-list {
	display: flex;
	flex-direction: column;
	gap: .5rem;
	margin: 0;
	padding: 0;
	list-style: none;
}

.transition-card {
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	background: var(--scheme-main);

	&.is-open {
		border-color: var(--primary);
	}

	&.has-error {
		border-color: var(--danger);
	}

	&__head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .5rem;
		inline-size: 100%;
		padding: .5rem .75rem;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}

	&__arrow {
		color: var(--grey-500);
	}

	&__label {
		color: var(--grey-700);
		font-size: .875rem;
	}

	&__tags {
		display: inline-flex;
		flex-wrap: wrap;
		gap: .25rem;
		margin-inline-start: auto;
	}

	&__chevron {
		color: var(--grey-500);
	}

	&__body {
		padding: .75rem;
		border-block-start: 1px solid var(--grey-200);
	}
}

.transition-card.is-open:not(li) {
	padding: .75rem;
	margin-block-start: .75rem;
}
</style>
