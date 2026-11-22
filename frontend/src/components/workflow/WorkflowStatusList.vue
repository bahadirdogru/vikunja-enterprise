<script setup lang="ts">
import {computed, useId} from 'vue'

import BaseButton from '@/components/base/BaseButton.vue'
import FormInput from '@/components/input/FormInput.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import XButton from '@/components/input/Button.vue'
import WorkflowStatusBadge from '@/components/workflow/WorkflowStatusBadge.vue'
import {
	WORKFLOW_CATEGORIES,
	activeStatuses,
	addStatus,
	categoryIcon,
	moveStatus,
	removeStatus,
	setInitialStatus,
	updateStatus,
	type DraftErrors,
	type StatusDraft,
	type WorkflowDraft,
} from '@/helpers/workflow'
import {useI18n} from 'vue-i18n'

const props = defineProps<{
	modelValue: WorkflowDraft
	errors: DraftErrors
	showErrors: boolean
}>()

const emit = defineEmits<{
	'update:modelValue': [value: WorkflowDraft]
}>()

const {t} = useI18n({useScope: 'global'})
const groupName = useId()

const active = computed(() => activeStatuses(props.modelValue))
const archived = computed(() => props.modelValue.statuses.filter(status => status.archived))
const categoryOptions = computed(() => WORKFLOW_CATEGORIES.map(category => ({
	value: category,
	label: t(`workflow.categories.${category}`),
})))

function patch(status: StatusDraft, change: Partial<StatusDraft>) {
	emit('update:modelValue', updateStatus(props.modelValue, status.id, change))
}

function errorText(status: StatusDraft): string | null {
	const codes = props.showErrors ? props.errors.statuses[status.id] : undefined
	return codes?.length ? t(`workflow.validation.${codes[0]}`) : null
}

function add() {
	emit('update:modelValue', addStatus(props.modelValue, t('workflow.statuses.newTitle')))
}
</script>

<template>
	<section
		class="workflow-statuses"
		aria-labelledby="workflow-statuses-title"
	>
		<div class="section-head">
			<div>
				<h3
					id="workflow-statuses-title"
					class="section-head__title"
				>
					{{ $t('workflow.statuses.title') }}
				</h3>
				<p class="section-head__hint">
					{{ $t('workflow.statuses.hint') }}
				</p>
			</div>
			<XButton
				icon="plus"
				variant="secondary"
				@click="add"
			>
				{{ $t('workflow.statuses.add') }}
			</XButton>
		</div>

		<ul class="status-list">
			<li
				v-for="(status, index) in active"
				:key="status.id"
				class="status-row"
				:data-test="`workflow-status-${index}`"
			>
				<input
					:value="status.color || '#64748b'"
					type="color"
					class="status-row__color"
					:aria-label="$t('workflow.statuses.color', {title: status.title})"
					@input="event => patch(status, {color: (event.target as HTMLInputElement).value})"
				>
				<FormInput
					:model-value="status.title"
					class="status-row__title"
					:aria-label="$t('workflow.statuses.titleLabel', {n: index + 1})"
					:placeholder="$t('workflow.statuses.titlePlaceholder')"
					:error="errorText(status)"
					@update:modelValue="value => patch(status, {title: String(value)})"
				/>
				<div class="status-row__category">
					<Icon
						:icon="categoryIcon(status.category)"
						:class="`category-icon is-${status.category}`"
					/>
					<FormSelect
						:model-value="status.category"
						:options="categoryOptions"
						:aria-label="$t('workflow.statuses.categoryLabel', {title: status.title})"
						@update:modelValue="value => patch(status, {category: value as StatusDraft['category']})"
					/>
				</div>
				<label class="status-row__initial">
					<input
						type="radio"
						:name="groupName"
						:checked="status.is_initial"
						@change="emit('update:modelValue', setInitialStatus(modelValue, status.id))"
					>
					{{ $t('workflow.statuses.initial') }}
				</label>
				<div class="status-row__actions">
					<BaseButton
						class="icon-button"
						:aria-disabled="index === 0 || undefined"
						:aria-label="$t('workflow.statuses.moveUp', {title: status.title})"
						@click="index > 0 && emit('update:modelValue', moveStatus(modelValue, status.id, -1))"
					>
						<Icon icon="arrow-up" />
					</BaseButton>
					<BaseButton
						class="icon-button"
						:aria-disabled="index === active.length - 1 || undefined"
						:aria-label="$t('workflow.statuses.moveDown', {title: status.title})"
						@click="index < active.length - 1 && emit('update:modelValue', moveStatus(modelValue, status.id, 1))"
					>
						<Icon icon="arrow-down" />
					</BaseButton>
					<BaseButton
						class="icon-button is-danger"
						:aria-disabled="active.length <= 1 || undefined"
						:aria-label="$t('workflow.statuses.remove', {title: status.title})"
						@click="active.length > 1 && emit('update:modelValue', removeStatus(modelValue, status.id))"
					>
						<Icon icon="trash-alt" />
					</BaseButton>
				</div>
			</li>
		</ul>

		<p class="section-head__hint">
			{{ $t('workflow.statuses.removeHint') }}
		</p>

		<details
			v-if="archived.length > 0"
			class="archived"
		>
			<summary>{{ $t('workflow.statuses.archived', {count: archived.length}, archived.length) }}</summary>
			<p class="section-head__hint">
				{{ $t('workflow.statuses.archivedHint') }}
			</p>
			<ul class="archived__list">
				<li
					v-for="status in archived"
					:key="status.id"
				>
					<WorkflowStatusBadge :status="status" />
				</li>
			</ul>
		</details>
	</section>
</template>

<style lang="scss" scoped>
.section-head {
	display: flex;
	flex-wrap: wrap;
	align-items: flex-start;
	justify-content: space-between;
	gap: .75rem;
	margin-block-end: .75rem;

	&__title {
		margin: 0;
		font-size: 1.125rem;
	}

	&__hint {
		margin: .125rem 0 0;
		color: var(--grey-600);
		font-size: .875rem;
	}
}

.status-list {
	display: flex;
	flex-direction: column;
	gap: .5rem;
	margin: 0 0 .5rem;
	padding: 0;
	list-style: none;
}

.status-row {
	display: grid;
	grid-template-columns: auto minmax(8rem, 1fr) minmax(9rem, 12rem) auto auto;
	align-items: start;
	gap: .5rem;
	padding: .5rem;
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	background: var(--scheme-main);

	@media screen and (max-width: $tablet) {
		grid-template-columns: auto 1fr;

		&__category,
		&__initial,
		&__actions {
			grid-column: 1 / -1;
		}
	}

	&__color {
		inline-size: 2.25rem;
		block-size: 2.25rem;
		padding: 0;
		border: 1px solid var(--grey-300);
		border-radius: $radius;
		background: none;
		cursor: pointer;
	}

	&__category {
		display: flex;
		align-items: center;
		gap: .5rem;
	}

	&__initial {
		display: flex;
		align-items: center;
		gap: .375rem;
		min-block-size: 2.25rem;
		white-space: nowrap;
		cursor: pointer;
	}

	&__actions {
		display: flex;
		align-items: center;
		min-block-size: 2.25rem;
	}
}

.category-icon {
	flex-shrink: 0;
	color: var(--grey-500);

	&.is-in_progress {
		color: var(--primary);
	}

	&.is-done {
		color: var(--success);
	}

	&.is-cancelled {
		color: var(--danger);
	}
}

.icon-button {
	padding: .5rem;
	color: var(--grey-500);
	border-radius: $radius;

	&:hover,
	&:focus-visible {
		color: var(--text);
		background: var(--grey-100);
	}

	&.is-danger:hover,
	&.is-danger:focus-visible {
		color: var(--danger);
	}

	&[aria-disabled='true'] {
		opacity: .35;
		cursor: not-allowed;
	}
}

.archived {
	margin-block-start: .75rem;

	summary {
		cursor: pointer;
		color: var(--grey-600);
		font-size: .875rem;
	}

	&__list {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		margin: .5rem 0 0;
		padding: 0;
		list-style: none;
	}
}
</style>
