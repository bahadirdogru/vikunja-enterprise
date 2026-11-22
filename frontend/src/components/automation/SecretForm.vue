<script setup lang="ts">
// Name and value of a project secret. The value is write-only: the field is a password input, it is cleared as soon
// as the request is done (whatever the outcome) and it is never shown, logged or kept anywhere else.
import {computed, ref} from 'vue'

import XButton from '@/components/input/Button.vue'
import {isRequestContextAbort} from '@/client/requestContext'
import {isValidSecretName, useSetSecretMutation} from '@/client/queries/automationSecrets'
import {getErrorText} from '@/message'

const props = defineProps<{
	projectId: number
	// Set when the value of an existing secret is replaced: the name is fixed.
	name?: string
	// Names that exist already (a new secret must not reuse one by accident).
	existing?: string[]
	idPrefix?: string
}>()

const emit = defineEmits<{
	saved: [name: string]
	cancel: []
}>()

const mutation = useSetSecretMutation()
const newName = ref('')
const value = ref('')
const failure = ref('')
const touched = ref(false)

const replacing = computed(() => props.name !== undefined)
const effectiveName = computed(() => props.name ?? newName.value.trim())
const prefix = computed(() => props.idPrefix ?? 'automation-secret')

const nameError = computed(() => {
	if (replacing.value || !touched.value) return ''
	if (effectiveName.value === '') return 'required'
	if (!isValidSecretName(effectiveName.value)) return 'invalid'
	if (props.existing?.includes(effectiveName.value)) return 'exists'
	return ''
})
const valueError = computed(() => touched.value && value.value === '')

async function submit() {
	touched.value = true
	failure.value = ''
	if (nameError.value !== '' || value.value === '' || effectiveName.value === '') return
	const name = effectiveName.value
	try {
		await mutation.mutateAsync({projectId: props.projectId, name, value: value.value, replacing: replacing.value})
		emit('saved', name)
		newName.value = ''
		touched.value = false
	} catch (cause) {
		if (isRequestContextAbort(cause)) return
		failure.value = getErrorText(cause)
	} finally {
		// The value must not stay in the component.
		value.value = ''
	}
}

function cancel() {
	value.value = ''
	newName.value = ''
	emit('cancel')
}
</script>

<template>
	<form
		class="secret-form"
		novalidate
		autocomplete="off"
		@submit.prevent.stop="submit"
		@keydown.esc.stop="cancel"
	>
		<div
			v-if="!replacing"
			class="secret-form__field"
		>
			<label :for="`${prefix}-name`">{{ $t('automation.secrets.name') }}</label>
			<input
				:id="`${prefix}-name`"
				v-model="newName"
				class="input"
				:class="{'is-danger': nameError !== ''}"
				type="text"
				autocomplete="off"
				autocapitalize="off"
				spellcheck="false"
				maxlength="64"
				placeholder="CHAT_TOKEN"
				:aria-invalid="nameError !== '' ? true : undefined"
			>
			<p
				v-if="nameError !== ''"
				class="help is-danger"
				role="alert"
			>
				{{ $t(`automation.secrets.nameError.${nameError}`) }}
			</p>
			<p
				v-else
				class="help"
			>
				{{ $t('automation.secrets.nameHelp') }}
			</p>
		</div>
		<div class="secret-form__field">
			<label :for="`${prefix}-value`">
				{{ replacing ? $t('automation.secrets.newValueFor', {name: props.name}) : $t('automation.secrets.value') }}
			</label>
			<input
				:id="`${prefix}-value`"
				v-model="value"
				class="input"
				:class="{'is-danger': valueError}"
				type="password"
				autocomplete="new-password"
				autocapitalize="off"
				spellcheck="false"
				:aria-invalid="valueError ? true : undefined"
			>
			<p
				v-if="valueError"
				class="help is-danger"
				role="alert"
			>
				{{ $t('automation.validation.required') }}
			</p>
			<p
				v-else
				class="help"
			>
				{{ $t('automation.secrets.valueHelp') }}
			</p>
		</div>
		<p
			v-if="failure"
			class="help is-danger"
			role="alert"
		>
			{{ failure }}
		</p>
		<div class="secret-form__buttons">
			<XButton
				type="submit"
				:loading="mutation.isPending.value"
			>
				{{ replacing ? $t('automation.secrets.replace') : $t('automation.secrets.add') }}
			</XButton>
			<XButton
				type="button"
				variant="secondary"
				@click="cancel"
			>
				{{ $t('misc.cancel') }}
			</XButton>
		</div>
	</form>
</template>

<style lang="scss" scoped>
.secret-form {
	display: flex;
	flex-direction: column;
	gap: .6rem;
	inline-size: 100%;
	max-inline-size: 28rem;
	padding: .6rem .75rem;
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	background: var(--grey-100);

	&__field {
		display: flex;
		flex-direction: column;
		gap: .2rem;

		label {
			font-weight: 600;
			font-size: .9rem;
		}

		.help {
			margin: 0;
		}
	}

	&__buttons {
		display: flex;
		gap: .5rem;
		flex-wrap: wrap;
	}
}
</style>
