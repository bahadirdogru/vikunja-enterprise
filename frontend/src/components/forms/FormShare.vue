<script setup lang="ts">
// The Share tab: the link, regenerating it and the snippet that embeds the form in another page.
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'

import XButton from '@/components/input/Button.vue'
import Message from '@/components/misc/Message.vue'
import Modal from '@/components/misc/Modal.vue'
import {useRegenerateTokenMutation, type FormResponse} from '@/client/queries/forms'
import {embedSnippet, formLink, type FormAccess} from '@/helpers/forms'
import {error, success} from '@/message'

const props = defineProps<{
	form: FormResponse | null
	dirty: boolean
	projectId: number
	access: FormAccess
}>()

const emit = defineEmits<{
	submissions: []
	tokenChanged: [token: string]
}>()

const {t} = useI18n({useScope: 'global'})

const link = computed(() => props.form ? formLink(props.form.token) : '')
const snippet = computed(() => props.form ? embedSnippet(link.value, props.form.title) : '')

async function copy(text: string) {
	try {
		await navigator.clipboard.writeText(text)
		success({message: t('forms.share.copied')})
	} catch {
		error(new Error(t('forms.share.copyFailed')))
	}
}

const confirmRegenerate = ref(false)
const regenerate = useRegenerateTokenMutation()

async function doRegenerate() {
	if (!props.form) return
	try {
		const token = await regenerate.mutateAsync({projectId: props.projectId, formId: props.form.id})
		if (token) emit('tokenChanged', token)
	} catch {
		return
	}
	confirmRegenerate.value = false
}
</script>

<template>
	<div class="fs">
		<Message v-if="!form">
			{{ $t('forms.share.saveFirst') }}
		</Message>
		<template v-else>
			<Message
				v-if="dirty"
				variant="warning"
			>
				{{ $t('forms.share.unsaved') }}
			</Message>
			<Message
				v-if="!form.enabled"
				variant="warning"
			>
				{{ $t('forms.share.off') }}
			</Message>

			<div class="field">
				<label
					class="label"
					for="fb-s-link"
				>{{ $t('forms.share.link') }}</label>
				<div class="fs__row">
					<input
						id="fb-s-link"
						class="input"
						type="text"
						readonly
						:value="link"
						@focus="($event.target as HTMLInputElement).select()"
					>
					<XButton
						type="button"
						icon="copy"
						variant="secondary"
						@click="copy(link)"
					>
						{{ $t('forms.share.copy') }}
					</XButton>
					<XButton
						type="button"
						icon="arrow-up-right-from-square"
						variant="secondary"
						:href="link"
						target="_blank"
						rel="noopener"
					>
						{{ $t('forms.share.open') }}
					</XButton>
				</div>
				<p class="help">
					{{ $t(`forms.share.who.${access}`) }}
				</p>
			</div>

			<div class="field">
				<XButton
					type="button"
					variant="secondary"
					icon="history"
					@click="confirmRegenerate = true"
				>
					{{ $t('forms.share.regenerate') }}
				</XButton>
				<p class="help">
					{{ $t('forms.share.regenerateHelp') }}
				</p>
			</div>

			<div class="field">
				<label
					class="label"
					for="fb-s-embed"
				>{{ $t('forms.share.embed') }}</label>
				<textarea
					id="fb-s-embed"
					class="textarea fs__code"
					rows="3"
					readonly
					:value="snippet"
					@focus="($event.target as HTMLTextAreaElement).select()"
				/>
				<XButton
					type="button"
					icon="copy"
					variant="secondary"
					@click="copy(snippet)"
				>
					{{ $t('forms.share.copyEmbed') }}
				</XButton>
				<p class="help">
					{{ $t('forms.share.embedHelp') }}
				</p>
			</div>

			<p class="help">
				{{ $t('forms.share.noQr') }}
			</p>

			<div class="field">
				<XButton
					type="button"
					variant="tertiary"
					@click="emit('submissions')"
				>
					{{ $t('forms.submissions.show', {count: form.submission_count}) }}
				</XButton>
			</div>
		</template>

		<Modal
			:enabled="confirmRegenerate"
			@close="confirmRegenerate = false"
			@submit="doRegenerate"
		>
			<template #header>
				<span>{{ $t('forms.share.regenerateConfirm.title') }}</span>
			</template>
			<template #text>
				<p>{{ $t('forms.share.regenerateConfirm.text') }}</p>
			</template>
		</Modal>
	</div>
</template>

<style lang="scss" scoped>
.fs {
	display: flex;
	flex-direction: column;
	gap: 1rem;

	&__row {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;

		.input {
			flex: 1 1 14rem;
			min-inline-size: 0;
		}
	}

	&__code {
		font-family: monospace;
		font-size: .85rem;
		margin-block-end: .5rem;
	}
}
</style>
