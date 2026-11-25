<script setup lang="ts">
import {computed, ref, useId} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'

import Card from '@/components/misc/Card.vue'
import Modal from '@/components/misc/Modal.vue'
import Datepicker from '@/components/input/Datepicker.vue'
import FormCheckbox from '@/components/input/FormCheckbox.vue'
import FormInput from '@/components/input/FormInput.vue'
import XButton from '@/components/input/Button.vue'
import {useCreateAdHocApprovalMutation} from '@/client/queries/approvals'
import {projectUserSearchQuery} from '@/client/queries/userSearch'
import {isRequestContextAbort} from '@/client/requestContext'
import {APPROVAL_MODES, readApprovalProblem, type ApprovalMode} from '@/helpers/approvals'
import {getDisplayName} from '@/helpers/user'
import {getErrorText} from '@/message'
import {useAuthStore} from '@/stores/auth'

const props = defineProps<{
	taskId: number
	projectId: number
}>()

const emit = defineEmits<{
	close: []
	created: []
}>()

const {t} = useI18n({useScope: 'global'})
const uid = useId()
const authStore = useAuthStore()
const create = useCreateAdHocApprovalMutation()

const membersResult = useQuery(computed(() => projectUserSearchQuery(props.projectId, '')))
// The requester cannot be one of the approvers.
const members = computed(() => (membersResult.data.value ?? [])
	.filter(user => user.id !== undefined && user.id !== authStore.info?.id))

const title = ref('')
const description = ref('')
const selected = ref<number[]>([])
const mode = ref<ApprovalMode>('any')
const due = ref<Date | null>(null)
// The limits of the server (pkg/models/approvals): a title of 250 characters, 1 to 50 approvers.
const MAX_TITLE = 250
const MAX_APPROVERS = 50
const errors = ref<{title?: string, approvers?: string, general?: string}>({})

function toggle(id: number, on: boolean) {
	selected.value = on ? [...selected.value, id] : selected.value.filter(entry => entry !== id)
}

async function submit() {
	errors.value = {}
	if (title.value.trim() === '') {
		errors.value.title = t('approvals.adHoc.titleRequired')
	} else if (Array.from(title.value.trim()).length > MAX_TITLE) {
		errors.value.title = t('approvals.adHoc.titleTooLong', {max: MAX_TITLE})
	}
	if (selected.value.length === 0) {
		errors.value.approvers = t('approvals.adHoc.approversRequired')
	} else if (selected.value.length > MAX_APPROVERS) {
		errors.value.approvers = t('approvals.adHoc.approversTooMany', {max: MAX_APPROVERS})
	}
	if (Object.keys(errors.value).length > 0) {
		return
	}
	try {
		await create.mutateAsync({
			taskId: props.taskId,
			title: title.value.trim(),
			description: description.value.trim(),
			approverIds: selected.value,
			mode: mode.value,
			due: due.value ? due.value.toISOString() : null,
		})
		emit('created')
		emit('close')
	} catch (cause) {
		if (isRequestContextAbort(cause)) {
			return
		}
		const problem = readApprovalProblem(cause)
		// The server's reason for an invalid request is English text, so the translated message of the code is
		// shown; the limits it names are checked above before anything is sent.
		errors.value.general = problem.kind === 'noApprovers'
			? t('approvals.adHoc.approversRequired')
			: getErrorText(cause)
	}
}
</script>

<template>
	<Modal
		:enabled="true"
		:overflow="true"
		:aria-label="$t('approvals.adHoc.title')"
		@close="emit('close')"
	>
		<Card
			:title="$t('approvals.adHoc.title')"
			:shadow="false"
			:show-close="true"
			class="adhoc-dialog has-text-start"
			@close="emit('close')"
		>
			<form
				novalidate
				@submit.prevent="submit"
			>
				<p class="help adhoc-dialog__intro">
					{{ $t('approvals.adHoc.intro') }}
				</p>
				<div class="field">
					<label
						class="label"
						:for="`${uid}-title`"
					>{{ $t('approvals.adHoc.what') }}</label>
					<FormInput
						:id="`${uid}-title`"
						v-model="title"
						:placeholder="$t('approvals.adHoc.whatPlaceholder')"
						:aria-invalid="errors.title ? 'true' : undefined"
					/>
					<p
						v-if="errors.title"
						class="help is-danger"
						role="alert"
					>
						{{ errors.title }}
					</p>
				</div>
				<div class="field">
					<label
						class="label"
						:for="`${uid}-description`"
					>{{ $t('approvals.adHoc.details') }}</label>
					<textarea
						:id="`${uid}-description`"
						v-model="description"
						class="textarea"
						rows="3"
					/>
				</div>
				<fieldset class="adhoc-dialog__group">
					<legend class="label">
						{{ $t('approvals.adHoc.approvers') }}
					</legend>
					<p
						v-if="members.length === 0"
						class="help"
					>
						{{ $t('approvals.adHoc.noMembers') }}
					</p>
					<div class="adhoc-dialog__members">
						<FormCheckbox
							v-for="user in members"
							:key="user.id"
							:model-value="selected.includes(user.id!)"
							@update:modelValue="on => toggle(user.id!, on)"
						>
							{{ getDisplayName(user) }}
						</FormCheckbox>
					</div>
					<p
						v-if="errors.approvers"
						class="help is-danger"
						role="alert"
					>
						{{ errors.approvers }}
					</p>
				</fieldset>
				<fieldset class="adhoc-dialog__group">
					<legend class="label">
						{{ $t('workflow.approval.mode.title') }}
					</legend>
					<label
						v-for="entry in APPROVAL_MODES"
						:key="entry"
						class="adhoc-dialog__mode"
					>
						<input
							type="radio"
							:name="`${uid}-mode`"
							:value="entry"
							:checked="mode === entry"
							@change="mode = entry"
						>
						<span>
							<strong>{{ $t(`workflow.approval.mode.${entry}`) }}</strong>
							<span class="help">{{ $t(`workflow.approval.mode.${entry}Hint`) }}</span>
						</span>
					</label>
				</fieldset>
				<div class="field">
					<span
						:id="`${uid}-due`"
						class="label"
					>{{ $t('approvals.adHoc.due') }}</span>
					<Datepicker
						v-model="due"
						:aria-labelledby="`${uid}-due`"
						:choose-date-label="$t('workflow.dialog.chooseDate')"
						:empty-label="$t('misc.notSet')"
						:title="$t('approvals.adHoc.due')"
						:min-date="new Date()"
					/>
				</div>
				<p
					v-if="errors.general"
					class="help is-danger"
					role="alert"
				>
					{{ errors.general }}
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
					:loading="create.isPending.value"
					@click="submit"
				>
					{{ $t('approvals.adHoc.submit') }}
				</XButton>
			</template>
		</Card>
	</Modal>
</template>

<style lang="scss" scoped>
.adhoc-dialog {
	&__intro {
		margin-block-end: .75rem;
	}

	&__group {
		margin: 0 0 1rem;
		padding: 0;
		border: 0;
		min-inline-size: 0;
	}

	&__members {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr));
		gap: .25rem .75rem;
		max-block-size: 10rem;
		overflow-y: auto;
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
}
</style>
