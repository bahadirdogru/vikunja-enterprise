<script setup lang="ts">
import {computed} from 'vue'
import {useQuery} from '@tanstack/vue-query'

import User from '@/components/misc/User.vue'
import BaseButton from '@/components/base/BaseButton.vue'
import {taskApprovalsQuery} from '@/client/queries/approvals'
import {taskStatusHistoryQuery} from '@/client/queries/workflow'
import {focusApproval} from '@/composables/useApprovalFocus'
import {describeApprovers} from '@/helpers/approvals'
import {formatDateLong, formatDateSince} from '@/helpers/time/formatDate'

const props = defineProps<{
	taskId: number
}>()

const historyResult = useQuery(computed(() => taskStatusHistoryQuery(props.taskId)))
const entries = computed(() => historyResult.data.value ?? [])
const approvalsResult = useQuery(computed(() => taskApprovalsQuery(props.taskId)))

// Who approved the request that caused this change, empty when the entry has none.
function approvedBy(requestId: number | null): string | null {
	if (requestId === null) {
		return null
	}
	const request = approvalsResult.data.value?.find(entry => entry.id === requestId)
	const names = describeApprovers(request?.approvers.filter(a => a.decision === 'approved').flatMap(a => a.user ? [a.user] : []) ?? [])
	return names === '' ? '' : names
}
</script>

<template>
	<div
		class="status-history"
		role="region"
		:aria-label="$t('workflow.history.title')"
	>
		<p
			v-if="historyResult.isPending.value"
			class="status-history__empty"
		>
			{{ $t('misc.loading') }}
		</p>
		<p
			v-else-if="entries.length === 0"
			class="status-history__empty"
		>
			{{ $t('workflow.history.empty') }}
		</p>
		<ol
			v-else
			class="status-history__list"
		>
			<li
				v-for="entry in entries"
				:key="entry.id"
				class="status-history__entry"
			>
				<div class="status-history__line">
					<span
						v-if="entry.from_title"
						class="status-history__from"
					>{{ entry.from_title }}</span>
					<span
						v-else
						class="status-history__from is-none"
					>{{ $t('workflow.history.none') }}</span>
					<Icon
						icon="arrow-right"
						class="status-history__arrow"
					/>
					<strong>{{ entry.to_title }}</strong>
				</div>
				<div class="status-history__meta">
					<User
						v-if="entry.doer"
						:user="entry.doer"
						:avatar-size="18"
						:is-inline="true"
					/>
					<time
						v-tooltip="formatDateLong(entry.created)"
						:datetime="entry.created"
					>{{ formatDateSince(entry.created) }}</time>
				</div>
				<p
					v-if="approvedBy(entry.approval_request_id) !== null"
					class="status-history__approval"
				>
					<Icon icon="user-check" />
					<BaseButton
						class="status-history__approval-link"
						@click="focusApproval(entry.approval_request_id!)"
					>
						{{ approvedBy(entry.approval_request_id)
							? $t('approvals.history.approvedBy', {names: approvedBy(entry.approval_request_id)})
							: $t('approvals.history.viaApproval') }}
					</BaseButton>
				</p>
				<p
					v-if="entry.cause === 'status_removed'"
					class="status-history__comment"
				>
					{{ $t('workflow.history.statusRemoved', {title: entry.from_title}) }}
				</p>
				<p
					v-else-if="entry.comment"
					class="status-history__comment"
				>
					{{ entry.comment }}
				</p>
			</li>
		</ol>
	</div>
</template>

<style lang="scss" scoped>
.status-history {
	grid-column: 1 / -1;
	margin-block-start: .5rem;
	font-size: .875rem;

	&__empty {
		margin: 0;
		color: var(--grey-600);
	}

	&__list {
		display: flex;
		flex-direction: column;
		gap: .625rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	&__entry {
		padding-inline-start: .75rem;
		border-inline-start: 2px solid var(--grey-300);
	}

	&__line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .375rem;
	}

	&__from {
		color: var(--grey-600);

		&.is-none {
			font-style: italic;
		}
	}

	&__arrow {
		font-size: .75rem;
		color: var(--grey-500);
	}

	&__meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .5rem;
		color: var(--grey-600);
		font-size: .8125rem;
	}

	&__comment {
		margin: .25rem 0 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
}

.status-history__approval {
	display: flex;
	align-items: center;
	gap: .375rem;
	margin: .125rem 0 0;
	color: var(--grey-600);
	font-size: .75rem;
}

.status-history__approval-link {
	color: var(--primary);
	text-decoration: underline;
}
</style>
