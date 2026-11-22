<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import User from '@/components/misc/User.vue'
import XButton from '@/components/input/Button.vue'
import {
	approvalProgress,
	canDecide,
	cancelReasonKey,
	isAdHoc,
	isOverdue,
	type ApprovalDecisionValue,
	type ApprovalResponse,
} from '@/helpers/approvals'
import {formatDateLong, formatDateSince} from '@/helpers/time/formatDate'

const props = defineProps<{
	request: ApprovalResponse
	userId: number
	// Show the approve and reject buttons.
	actions?: boolean
}>()

const emit = defineEmits<{
	decide: [request: ApprovalResponse, decision: ApprovalDecisionValue]
}>()

const {t} = useI18n({useScope: 'global'})

const what = computed(() => !isAdHoc(props.request) && props.request.to_status
	? t('approvals.banner.waitingFor', {status: props.request.to_status.title})
	: props.request.title)
const progress = computed(() => approvalProgress(props.request))
const overdue = computed(() => isOverdue(props.request))
const resolvedText = computed(() => props.request.status === 'cancelled'
	? t(cancelReasonKey(props.request.cancel_reason))
	: '')
</script>

<template>
	<li
		class="approval-row"
		:class="{'is-overdue': overdue}"
		data-cy="approval-row"
	>
		<div class="approval-row__main">
			<RouterLink
				v-if="request.task"
				:to="{name: 'task.detail', params: {id: request.task.id}}"
				class="approval-row__task"
			>
				<span class="approval-row__identifier">{{ request.task.identifier }}</span>
				{{ request.task.title }}
			</RouterLink>
			<span
				v-if="request.task"
				class="approval-row__project"
			>{{ request.task.project_title }}</span>
			<p class="approval-row__what">
				{{ what }}
				<span
					v-if="request.status !== 'pending'"
					class="outcome-badge"
					:class="`is-${request.status}`"
				>{{ $t(`approvals.outcome.${request.status}`) }}</span>
			</p>
			<p
				v-if="resolvedText"
				class="approval-row__reason"
			>
				{{ resolvedText }}
			</p>
		</div>
		<dl class="approval-row__meta">
			<div v-if="request.requested_by">
				<dt>{{ $t('approvals.requestedBy') }}</dt>
				<dd>
					<User
						:user="request.requested_by"
						:avatar-size="16"
						:is-inline="true"
					/>
				</dd>
			</div>
			<div>
				<dt>{{ $t('approvals.progressLabel') }}</dt>
				<dd>{{ $t('approvals.progress', {done: progress.approved, total: progress.total}) }}</dd>
			</div>
			<div v-if="request.due && request.status === 'pending'">
				<dt>{{ $t('approvals.due') }}</dt>
				<dd :class="{'has-text-danger': overdue}">
					<time
						v-tooltip="formatDateLong(request.due)"
						:datetime="request.due"
					>{{ formatDateSince(request.due) }}</time>
				</dd>
			</div>
			<div>
				<dt>{{ request.status === 'pending' ? $t('approvals.age') : $t('approvals.resolvedAt') }}</dt>
				<dd>
					<time
						v-tooltip="formatDateLong(request.status === 'pending' ? request.created : request.resolved)"
						:datetime="(request.status === 'pending' ? request.created : request.resolved) ?? undefined"
					>{{ formatDateSince(request.status === 'pending' ? request.created : request.resolved) }}</time>
				</dd>
			</div>
		</dl>
		<div
			v-if="actions && canDecide(request, userId)"
			class="approval-row__actions"
		>
			<XButton
				icon="check"
				@click="emit('decide', request, 'approved')"
			>
				{{ $t('approvals.approve') }}
			</XButton>
			<XButton
				variant="secondary"
				icon="times"
				@click="emit('decide', request, 'rejected')"
			>
				{{ $t('approvals.reject') }}
			</XButton>
		</div>
	</li>
</template>

<style lang="scss" scoped>
.approval-row {
	display: flex;
	flex-wrap: wrap;
	align-items: flex-start;
	gap: .5rem 1.5rem;
	padding: .75rem 1rem;
	background: var(--white);
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	min-inline-size: 0;

	&.is-overdue {
		border-color: var(--danger);
	}

	&__main {
		flex: 1 1 16rem;
		min-inline-size: 0;
	}

	&__task {
		font-weight: 600;
		overflow-wrap: anywhere;
	}

	&__identifier {
		color: var(--grey-500);
		font-weight: 400;
	}

	&__project {
		display: block;
		color: var(--grey-600);
		font-size: .8125rem;
	}

	&__what {
		margin: .25rem 0 0;
		overflow-wrap: anywhere;
	}

	&__reason {
		margin: .125rem 0 0;
		color: var(--grey-600);
		font-size: .8125rem;
	}

	&__meta {
		display: flex;
		flex-wrap: wrap;
		gap: .25rem 1.25rem;
		margin: 0;
		font-size: .8125rem;

		div {
			display: flex;
			flex-direction: column;
		}

		dt {
			color: var(--grey-500);
		}

		dd {
			margin: 0;
		}
	}

	&__actions {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
	}
}

.outcome-badge {
	margin-inline-start: .375rem;
	padding: .0625rem .5rem;
	border-radius: 999px;
	background: var(--grey-200);
	font-size: .75rem;
	font-weight: 600;

	&.is-approved {
		background: var(--success);
		color: var(--white);
	}

	&.is-rejected {
		background: var(--danger);
		color: var(--white);
	}
}
</style>
