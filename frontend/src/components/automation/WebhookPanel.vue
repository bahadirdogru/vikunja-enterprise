<script setup lang="ts">
// The address of the incoming webhook of a rule. The server stores a hash of the secret token and shows the address
// once, in the answer of the save that created it or of the request that regenerated it. It is held here, in memory,
// until the editor closes: it is not cached, stored or logged.
import {ref} from 'vue'
import {useI18n} from 'vue-i18n'

import XButton from '@/components/input/Button.vue'
import Message from '@/components/misc/Message.vue'
import Modal from '@/components/misc/Modal.vue'
import {useRotateWebhookTokenMutation, type WebhookReveal} from '@/client/queries/automation'
import {isRequestContextAbort} from '@/client/requestContext'
import {error, getErrorText, success} from '@/message'

const props = defineProps<{
	projectId: number
	// The saved rule, or null while the rule is new.
	ruleId: number | null
	// The saved rule has the webhook trigger and so an address.
	saved: boolean
	// There are unsaved changes: the address belongs to the saved rule.
	dirty: boolean
	// The address, when this editor session has it.
	reveal: WebhookReveal | null
}>()

const emit = defineEmits<{
	// null: the address on screen can no longer be trusted and was dropped.
	revealed: [value: WebhookReveal | null]
	copied: []
}>()

const {t} = useI18n({useScope: 'global'})

async function copy(text: string) {
	try {
		await navigator.clipboard.writeText(text)
		emit('copied')
		success({message: t('automation.webhook.copied')})
	} catch {
		error(new Error(t('automation.webhook.copyFailed')))
	}
}

const confirmRotate = ref(false)
const failure = ref('')
// A rotate that failed after it was sent may still have replaced the address on the server.
const uncertain = ref(false)
const rotate = useRotateWebhookTokenMutation()

async function doRotate() {
	confirmRotate.value = false
	failure.value = ''
	uncertain.value = false
	if (props.ruleId === null) return
	try {
		const result = await rotate.mutateAsync({projectId: props.projectId, ruleId: props.ruleId})
		if (result.url) emit('revealed', result)
	} catch (cause) {
		// The address on screen may already be dead: never leave it under "copy this address now".
		emit('revealed', null)
		uncertain.value = true
		if (!isRequestContextAbort(cause)) failure.value = getErrorText(cause)
	}
}
</script>

<template>
	<div class="webhook-panel">
		<Message
			v-if="ruleId === null || !saved"
			variant="info"
		>
			{{ $t('automation.webhook.afterSave') }}
		</Message>

		<template v-else>
			<Message
				v-if="reveal"
				variant="warning"
			>
				<p class="webhook-panel__warning">
					{{ $t('automation.webhook.onlyOnce') }}
				</p>
				<label
					class="label"
					for="automation-webhook-url"
				>{{ $t('automation.webhook.url') }}</label>
				<div class="webhook-panel__row">
					<input
						id="automation-webhook-url"
						class="input"
						type="text"
						readonly
						autocomplete="off"
						spellcheck="false"
						:value="reveal.url"
						@focus="($event.target as HTMLInputElement).select()"
					>
					<XButton
						type="button"
						icon="copy"
						variant="secondary"
						@click="copy(reveal.url)"
					>
						{{ $t('automation.webhook.copy') }}
					</XButton>
				</div>
			</Message>
			<p
				v-else
				class="help"
			>
				{{ $t('automation.webhook.hidden') }}
			</p>

			<div>
				<XButton
					type="button"
					variant="secondary"
					icon="history"
					:loading="rotate.isPending.value"
					:disabled="dirty"
					@click="confirmRotate = true"
				>
					{{ reveal ? $t('automation.webhook.rotateAgain') : $t('automation.webhook.rotate') }}
				</XButton>
				<p class="help">
					{{ dirty ? $t('automation.webhook.saveFirst') : $t('automation.webhook.rotateHelp') }}
				</p>
				<p
					v-if="uncertain"
					class="help is-danger"
					role="alert"
				>
					{{ $t('automation.webhook.rotateUncertain') }}
				</p>
				<p
					v-if="failure"
					class="help is-danger"
					role="alert"
				>
					{{ failure }}
				</p>
			</div>

			<ul class="webhook-panel__notes help">
				<li>{{ $t('automation.webhook.usage') }}</li>
				<li>{{ $t('automation.webhook.signature') }}</li>
				<li>{{ $t('automation.webhook.noReplayProtection') }}</li>
			</ul>
		</template>

		<Modal
			:enabled="confirmRotate"
			@close="confirmRotate = false"
			@submit="doRotate"
		>
			<template #header>
				<span>{{ $t('automation.webhook.rotateConfirm.title') }}</span>
			</template>
			<template #text>
				<p>{{ $t('automation.webhook.rotateConfirm.text') }}</p>
			</template>
		</Modal>
	</div>
</template>

<style lang="scss" scoped>
.webhook-panel {
	display: flex;
	flex-direction: column;
	gap: .6rem;
	min-inline-size: 0;

	&__warning {
		font-weight: 600;
		margin-block-end: .5rem;
	}

	&__row {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;

		.input {
			flex: 1 1 16rem;
			min-inline-size: 0;
			font-family: monospace;
		}
	}

	&__notes {
		margin: 0;
		padding-inline-start: 1.2rem;
	}
}
</style>
