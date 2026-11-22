<script setup lang="ts">
import {computed, nextTick, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'

import Card from '@/components/misc/Card.vue'
import Modal from '@/components/misc/Modal.vue'
import User from '@/components/misc/User.vue'
import XButton from '@/components/input/Button.vue'
import BaseButton from '@/components/base/BaseButton.vue'
import ApprovalDecisionDialog from '@/components/approvals/ApprovalDecisionDialog.vue'
import {taskApprovalsQuery, useCancelApprovalMutation} from '@/client/queries/approvals'
import {
	approvalProgress,
	canCancel,
	canDecide,
	cancelReasonKey,
	isAdHoc,
	isOverdue,
	pendingApprovals,
	resolvedApprovals,
	type ApprovalDecisionValue,
	type ApprovalResponse,
} from '@/helpers/approvals'
import {formatDateLong, formatDateSince} from '@/helpers/time/formatDate'
import {useAuthStore} from '@/stores/auth'
import {focusedApprovalId} from '@/composables/useApprovalFocus'

const props = defineProps<{
	taskId: number
	isProjectAdmin: boolean
}>()

const emit = defineEmits<{
	// A decision or cancellation may have changed the task.
	changed: []
}>()

const {t} = useI18n({useScope: 'global'})
const authStore = useAuthStore()
const userId = computed(() => authStore.info?.id ?? 0)

const approvalsResult = useQuery(computed(() => taskApprovalsQuery(props.taskId)))
const all = computed(() => approvalsResult.data.value ?? [])
const pending = computed(() => pendingApprovals(all.value))
const resolved = computed(() => resolvedApprovals(all.value))
const cancel = useCancelApprovalMutation()

const showResolved = ref(false)
watch(focusedApprovalId, async id => {
	if (id === null) return
	showResolved.value = true
	await nextTick()
	document.getElementById(`approval-${id}`)?.scrollIntoView({block: 'center'})
})
const decisionFor = ref<{request: ApprovalResponse, decision: ApprovalDecisionValue} | null>(null)
const cancelFor = ref<ApprovalResponse | null>(null)

function heading(request: ApprovalResponse): string {
	if (!isAdHoc(request) && request.to_status) {
		return t('approvals.banner.waitingFor', {status: request.to_status.title})
	}
	return request.title
}

function resolvedHeading(request: ApprovalResponse): string {
	return isAdHoc(request) || !request.to_status ? request.title : `${request.title}`
}

function progressText(request: ApprovalResponse): string {
	const progress = approvalProgress(request)
	return t('approvals.progress', {done: progress.approved, total: progress.total})
}

function decisionLabel(decision: string | null): string {
	return decision === 'approved'
		? t('approvals.state.approved')
		: decision === 'rejected'
			? t('approvals.state.rejected')
			: t('approvals.state.pending')
}

function outcomeLabel(request: ApprovalResponse): string {
	return t(`approvals.outcome.${request.status}`)
}

function cancelText(request: ApprovalResponse): string {
	const base = t(cancelReasonKey(request.cancel_reason))
	return request.cancel_message && request.cancel_reason === 'requirements_not_met'
		? `${base} (${request.cancel_message})`
		: base
}

function decide(request: ApprovalResponse, decision: ApprovalDecisionValue) {
	decisionFor.value = {request, decision}
}

async function confirmCancel() {
	const request = cancelFor.value
	if (!request) return
	try {
		await cancel.mutateAsync({requestId: request.id, taskId: request.task_id})
		cancelFor.value = null
		emit('changed')
	} catch {
		// The mutation already told the user; keep the dialog so they can retry or leave.
	}
}

function onDecided() {
	emit('changed')
}

defineExpose({pending})
</script>

<template>
	<section
		v-if="pending.length > 0 || resolved.length > 0"
		class="task-approvals"
		:aria-label="$t('approvals.title')"
	>
		<article
			v-for="request in pending"
			:key="request.id"
			class="approval-banner"
			:class="{'is-overdue': isOverdue(request)}"
			data-cy="approval-banner"
		>
			<header class="approval-banner__head">
				<Icon
					icon="hourglass-half"
					class="approval-banner__icon"
				/>
				<h3 class="approval-banner__title">
					{{ heading(request) }}
				</h3>
				<span class="approval-banner__progress">{{ progressText(request) }}</span>
			</header>
			<p
				v-if="request.description"
				class="approval-banner__text"
			>
				{{ request.description }}
			</p>
			<dl class="approval-banner__meta">
				<div v-if="request.requested_by">
					<dt>{{ $t('approvals.requestedBy') }}</dt>
					<dd>
						<User
							:user="request.requested_by"
							:avatar-size="18"
							:is-inline="true"
						/>
					</dd>
				</div>
				<div>
					<dt>{{ $t('approvals.modeLabel') }}</dt>
					<dd>{{ $t(`approvals.mode.${request.mode}`) }}</dd>
				</div>
				<div v-if="request.due">
					<dt>{{ $t('approvals.due') }}</dt>
					<dd :class="{'has-text-danger': isOverdue(request)}">
						<time
							v-tooltip="formatDateLong(request.due)"
							:datetime="request.due"
						>{{ formatDateSince(request.due) }}</time>
						<strong v-if="isOverdue(request)"> · {{ $t('approvals.overdue') }}</strong>
					</dd>
				</div>
			</dl>
			<ul class="approval-banner__approvers">
				<li
					v-for="approver in request.approvers"
					:key="approver.id"
					class="approver"
					:class="`is-${approver.decision ?? 'pending'}`"
				>
					<User
						v-if="approver.user"
						:user="approver.user"
						:avatar-size="20"
						:is-inline="true"
					/>
					<span class="approver__state">
						<Icon :icon="approver.decision === 'approved' ? 'circle-check' : approver.decision === 'rejected' ? 'circle-xmark' : 'hourglass-half'" />
						{{ decisionLabel(approver.decision) }}
					</span>
					<time
						v-if="approver.decided"
						v-tooltip="formatDateLong(approver.decided)"
						class="approver__time"
						:datetime="approver.decided"
					>{{ formatDateSince(approver.decided) }}</time>
					<p
						v-if="approver.comment"
						class="approver__comment"
					>
						{{ approver.comment }}
					</p>
				</li>
			</ul>
			<footer
				v-if="canDecide(request, userId) || canCancel(request, userId, isProjectAdmin)"
				class="approval-banner__actions"
			>
				<template v-if="canDecide(request, userId)">
					<XButton
						icon="check"
						@click="decide(request, 'approved')"
					>
						{{ $t('approvals.approve') }}
					</XButton>
					<XButton
						variant="secondary"
						icon="times"
						@click="decide(request, 'rejected')"
					>
						{{ $t('approvals.reject') }}
					</XButton>
				</template>
				<XButton
					v-if="canCancel(request, userId, isProjectAdmin)"
					variant="tertiary"
					class="has-text-danger"
					@click="cancelFor = request"
				>
					{{ $t('approvals.cancel.action') }}
				</XButton>
			</footer>
		</article>

		<div
			v-if="resolved.length > 0"
			class="approval-history"
		>
			<BaseButton
				class="approval-history__toggle"
				:aria-expanded="showResolved"
				@click="showResolved = !showResolved"
			>
				<Icon :icon="showResolved ? 'chevron-down' : 'chevron-right'" />
				{{ $t('approvals.history.title', {count: resolved.length}) }}
			</BaseButton>
			<ul
				v-if="showResolved"
				class="approval-history__list"
			>
				<li
					v-for="request in resolved"
					:id="`approval-${request.id}`"
					:key="request.id"
					class="approval-history__entry"
				>
					<div class="approval-history__line">
						<span
							class="outcome-badge"
							:class="`is-${request.status}`"
						>{{ outcomeLabel(request) }}</span>
						<strong>{{ resolvedHeading(request) }}</strong>
						<time
							v-if="request.resolved"
							v-tooltip="formatDateLong(request.resolved)"
							:datetime="request.resolved"
						>{{ formatDateSince(request.resolved) }}</time>
					</div>
					<p
						v-if="request.status === 'cancelled'"
						class="approval-history__reason"
					>
						{{ cancelText(request) }}
					</p>
					<ul class="approval-history__decisions">
						<li
							v-for="approver in request.approvers.filter(entry => entry.decision)"
							:key="approver.id"
						>
							<User
								v-if="approver.user"
								:user="approver.user"
								:avatar-size="16"
								:is-inline="true"
							/>
							{{ decisionLabel(approver.decision).toLowerCase() }}<template v-if="approver.comment">
								: “{{ approver.comment }}”
							</template>
						</li>
					</ul>
				</li>
			</ul>
		</div>

		<ApprovalDecisionDialog
			v-if="decisionFor"
			:request="decisionFor.request"
			:decision="decisionFor.decision"
			@close="decisionFor = null"
			@decided="onDecided"
		/>

		<Modal
			v-if="cancelFor"
			:enabled="true"
			:aria-label="$t('approvals.cancel.title')"
			@close="cancelFor = null"
		>
			<Card
				:title="$t('approvals.cancel.title')"
				:shadow="false"
				:show-close="true"
				class="has-text-start"
				@close="cancelFor = null"
			>
				<p>{{ $t('approvals.cancel.text', {title: cancelFor.title}) }}</p>
				<template #footer>
					<XButton
						variant="secondary"
						@click="cancelFor = null"
					>
						{{ $t('approvals.cancel.keep') }}
					</XButton>
					<XButton
						class="mis-2 is-danger"
						:loading="cancel.isPending.value"
						@click="confirmCancel"
					>
						{{ $t('approvals.cancel.confirm') }}
					</XButton>
				</template>
			</Card>
		</Modal>
	</section>
</template>

<style lang="scss" scoped>
.task-approvals {
	display: flex;
	flex-direction: column;
	gap: .75rem;
	margin-block-end: 1rem;
	min-inline-size: 0;
}

.approval-banner {
	padding: .75rem 1rem;
	background: var(--warning-light, var(--grey-100));
	border: 1px solid var(--warning, var(--grey-300));
	border-radius: $radius;
	min-inline-size: 0;

	&.is-overdue {
		border-color: var(--danger);
	}

	&__head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .5rem;
	}

	&__icon {
		color: var(--warning-dark, var(--grey-700));
	}

	&__title {
		flex: 1 1 10rem;
		margin: 0;
		font-size: 1rem;
		overflow-wrap: anywhere;
	}

	&__progress {
		padding: .0625rem .5rem;
		background: var(--white);
		border-radius: 999px;
		font-size: .75rem;
		font-weight: 600;
	}

	&__text {
		margin: .5rem 0 0;
		overflow-wrap: anywhere;
	}

	&__meta {
		display: flex;
		flex-wrap: wrap;
		gap: .25rem 1.25rem;
		margin: .5rem 0;
		font-size: .875rem;

		div {
			display: flex;
			align-items: center;
			gap: .375rem;
		}

		dt {
			color: var(--grey-600);
		}

		dd {
			margin: 0;
		}
	}

	&__approvers {
		display: flex;
		flex-direction: column;
		gap: .375rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	&__actions {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		margin-block-start: .75rem;
	}
}

.approver {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: .25rem .75rem;
	font-size: .875rem;

	&__state {
		display: inline-flex;
		align-items: center;
		gap: .25rem;
		font-weight: 600;
	}

	&.is-approved &__state {
		color: var(--success);
	}

	&.is-rejected &__state {
		color: var(--danger);
	}

	&__time {
		color: var(--grey-500);
		font-size: .75rem;
	}

	&__comment {
		flex-basis: 100%;
		margin: 0;
		padding-inline-start: .75rem;
		border-inline-start: 2px solid var(--grey-300);
		overflow-wrap: anywhere;
	}
}

.approval-history {
	&__toggle {
		display: inline-flex;
		align-items: center;
		gap: .375rem;
		color: var(--grey-700);
		font-size: .875rem;
		font-weight: 600;
	}

	&__list {
		display: flex;
		flex-direction: column;
		gap: .5rem;
		margin: .5rem 0 0;
		padding: 0;
		list-style: none;
	}

	&__entry {
		padding: .5rem .75rem;
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		font-size: .875rem;
	}

	&__line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .5rem;

		time {
			color: var(--grey-500);
			font-size: .75rem;
		}
	}

	&__reason {
		margin: .25rem 0 0;
		color: var(--grey-600);
	}

	&__decisions {
		margin: .25rem 0 0;
		padding: 0;
		list-style: none;
		overflow-wrap: anywhere;
	}
}

.outcome-badge {
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
