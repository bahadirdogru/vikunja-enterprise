<script setup lang="ts">
import {computed, ref, useId} from 'vue'
import {useI18n} from 'vue-i18n'

import Card from '@/components/misc/Card.vue'
import Modal from '@/components/misc/Modal.vue'
import XButton from '@/components/input/Button.vue'
import {useDecideApprovalMutation} from '@/client/queries/approvals'
import {isRequestContextAbort} from '@/client/requestContext'
import {
	decisionProblemKey,
	readApprovalProblem,
	type ApprovalDecisionValue,
	type ApprovalResponse,
} from '@/helpers/approvals'
import {getErrorText} from '@/message'

const props = defineProps<{
	request: ApprovalResponse
	decision: ApprovalDecisionValue
}>()

const emit = defineEmits<{
	close: []
	// The decision was recorded (or the request turned out to be settled already); lists should refresh.
	decided: []
}>()

const {t} = useI18n({useScope: 'global'})
const uid = useId()
const decide = useDecideApprovalMutation()

const comment = ref('')
// Error under the comment field (24005 and the empty client check).
const commentError = ref<string | null>(null)
const generalError = ref<string | null>(null)

const rejecting = computed(() => props.decision === 'rejected')
const title = computed(() => rejecting.value ? t('approvals.decision.rejectTitle') : t('approvals.decision.approveTitle'))

async function submit() {
	commentError.value = null
	generalError.value = null
	if (rejecting.value && comment.value.trim() === '') {
		commentError.value = t('approvals.errors.commentRequired')
		return
	}
	try {
		await decide.mutateAsync({
			requestId: props.request.id,
			taskId: props.request.task_id,
			decision: props.decision,
			comment: comment.value.trim(),
		})
		emit('decided')
		emit('close')
	} catch (cause) {
		if (isRequestContextAbort(cause)) {
			return
		}
		const problem = readApprovalProblem(cause)
		if (problem.kind === 'commentRequired') {
			commentError.value = t('approvals.errors.commentRequired')
			return
		}
		const key = decisionProblemKey(problem)
		generalError.value = key ? t(key) : getErrorText(cause)
		// Somebody else was faster: the lists must show what is true now.
		if (['notPending', 'alreadyDecided', 'notFound'].includes(problem.kind)) {
			emit('decided')
		}
	}
}
</script>

<template>
	<Modal
		:enabled="true"
		:overflow="true"
		:aria-label="title"
		@close="emit('close')"
	>
		<Card
			:title="title"
			:shadow="false"
			:show-close="true"
			class="decision-dialog has-text-start"
			@close="emit('close')"
		>
			<form
				novalidate
				@submit.prevent="submit"
			>
				<p class="decision-dialog__what">
					<strong>{{ request.title }}</strong>
					<span
						v-if="request.task"
						class="decision-dialog__task"
					>{{ request.task.identifier }} {{ request.task.title }}</span>
				</p>
				<div class="field">
					<label
						class="label"
						:for="`${uid}-comment`"
					>
						{{ rejecting ? $t('approvals.decision.commentRequired') : $t('approvals.decision.commentOptional') }}
					</label>
					<textarea
						:id="`${uid}-comment`"
						v-model="comment"
						class="textarea"
						rows="3"
						:aria-invalid="commentError ? 'true' : undefined"
						:aria-describedby="commentError ? `${uid}-comment-error` : undefined"
					/>
					<p
						v-if="commentError"
						:id="`${uid}-comment-error`"
						class="help is-danger"
						role="alert"
					>
						{{ commentError }}
					</p>
				</div>
				<p
					v-if="generalError"
					class="help is-danger"
					role="alert"
				>
					{{ generalError }}
				</p>
				<button
					type="submit"
					hidden
				/>
			</form>
			<template #footer>
				<XButton
					variant="secondary"
					@click="emit('close')"
				>
					{{ $t('misc.cancel') }}
				</XButton>
				<XButton
					class="mis-2"
					:class="{'is-danger': rejecting}"
					:loading="decide.isPending.value"
					@click="submit"
				>
					{{ rejecting ? $t('approvals.reject') : $t('approvals.approve') }}
				</XButton>
			</template>
		</Card>
	</Modal>
</template>

<style lang="scss" scoped>
.decision-dialog {
	&__what {
		display: flex;
		flex-direction: column;
		gap: .125rem;
		margin-block-end: .75rem;
	}

	&__task {
		color: var(--grey-600);
		font-size: .875rem;
	}
}
</style>
