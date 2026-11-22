<script setup lang="ts">
import {computed} from 'vue'

import WorkflowStatusBadge from '@/components/workflow/WorkflowStatusBadge.vue'
import {
	activeStatuses,
	toggleTransition,
	transitionAt,
	type WorkflowDraft,
} from '@/helpers/workflow'

const props = defineProps<{
	modelValue: WorkflowDraft
	openKey: string | null
}>()

const emit = defineEmits<{
	'update:modelValue': [value: WorkflowDraft]
	'open': [key: string | null]
}>()

const statuses = computed(() => activeStatuses(props.modelValue))

function cell(from: number, to: number) {
	return transitionAt(props.modelValue, from, to)
}

// A checked cell opens its details; the details panel has the remove button.
function activate(from: number, to: number) {
	const existing = cell(from, to)
	if (existing) {
		emit('open', props.openKey === existing.key ? null : existing.key)
		return
	}
	const result = toggleTransition(props.modelValue, from, to)
	emit('update:modelValue', result.draft)
	emit('open', result.key)
}

function onCheckboxChange(event: Event, from: number, to: number) {
	if (cell(from, to)) {
		// The checked state comes from the draft, so undo the browser's toggle.
		(event.target as HTMLInputElement).checked = true
	}
	activate(from, to)
}
</script>

<template>
	<div class="matrix-wrapper">
		<table class="matrix">
			<caption class="is-sr-only">
				{{ $t('workflow.matrix.caption') }}
			</caption>
			<thead>
				<tr>
					<th scope="col">
						<span class="matrix__corner">{{ $t('workflow.matrix.corner') }}</span>
					</th>
					<th
						v-for="to in statuses"
						:key="to.id"
						scope="col"
					>
						<WorkflowStatusBadge :status="to" />
					</th>
				</tr>
			</thead>
			<tbody>
				<tr
					v-for="from in statuses"
					:key="from.id"
				>
					<th scope="row">
						<WorkflowStatusBadge :status="from" />
					</th>
					<td
						v-for="to in statuses"
						:key="to.id"
						:class="{
							'is-self': from.id === to.id,
							'is-open': cell(from.id, to.id)?.key === openKey && openKey !== null,
						}"
					>
						<span
							v-if="from.id === to.id"
							class="matrix__self"
							aria-hidden="true"
						>–</span>
						<input
							v-else
							type="checkbox"
							class="matrix__check"
							:checked="cell(from.id, to.id) !== undefined"
							:aria-label="$t('workflow.matrix.cell', {from: from.title, to: to.title})"
							@change="event => onCheckboxChange(event, from.id, to.id)"
						>
						<Icon
							v-if="cell(from.id, to.id)?.requires_approval"
							v-tooltip="$t('workflow.approval.marker')"
							icon="user-check"
							class="matrix__approval"
							role="img"
							:aria-label="$t('workflow.approval.marker')"
						/>
					</td>
				</tr>
			</tbody>
		</table>
	</div>
</template>

<style lang="scss" scoped>
.matrix-wrapper {
	overflow-x: auto;
}

.matrix {
	border-collapse: separate;
	border-spacing: 0;
	font-size: .875rem;

	th,
	td {
		padding: .375rem .5rem;
		border-block-end: 1px solid var(--grey-200);
		text-align: center;
		vertical-align: middle;
	}

	thead th {
		font-weight: 500;
	}

	tbody th {
		text-align: start;
		white-space: nowrap;
	}

	td.is-self {
		background: var(--grey-100);
	}

	td.is-open {
		background: var(--primary-light, var(--grey-100));
		outline: 2px solid var(--primary);
		outline-offset: -2px;
	}

	&__corner {
		color: var(--grey-500);
		font-size: .75rem;
	}

	&__self {
		color: var(--grey-400);
	}

	&__approval {
		margin-inline-start: .25rem;
		color: var(--primary);
		font-size: .75rem;
	}

	&__check {
		inline-size: 1.125rem;
		block-size: 1.125rem;
		cursor: pointer;
	}
}
</style>
