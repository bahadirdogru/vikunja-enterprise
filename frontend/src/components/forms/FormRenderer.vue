<script setup lang="ts">
// The form as a submitter fills it in. The public page and the builder preview render the same component; the preview
// never emits a submission.
import {computed, nextTick, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'

import FormFieldInput from '@/components/forms/FormFieldInput.vue'
import XButton from '@/components/input/Button.vue'
import {
	SUBMITTER_EMAIL,
	SUBMITTER_NAME,
	buildSubmission,
	initialAnswers,
	validateAnswers,
	type AnswerErrors,
	type Answers,
	type PublicDefinition,
	type SubmissionBody,
} from '@/helpers/formsPublic'

const props = withDefaults(defineProps<{
	definition: PublicDefinition
	preview?: boolean
	submitting?: boolean
	// Errors the server reported, by field; a new object means a new answer.
	serverErrors?: AnswerErrors | null
}>(), {
	preview: false,
	submitting: false,
	serverErrors: null,
})

const emit = defineEmits<{
	submit: [payload: {body: SubmissionBody, files: File[]}]
}>()

const {t} = useI18n({useScope: 'global'})

// eslint-disable-next-line vue/no-setup-props-reactivity-loss -- later definitions are merged by the watcher below
const answers = ref<Answers>(initialAnswers(props.definition))
const honeypot = ref('')
const clientErrors = ref<AnswerErrors>({})
const summary = ref('')
const formElement = ref<HTMLFormElement | null>(null)
const previewNote = ref(false)

// A changed definition (the builder edits it live) keeps what was typed for fields that still exist.
watch(() => props.definition, definition => {
	const next = initialAnswers(definition)
	for (const field of definition.fields) {
		if (field.target in answers.value.values && sameShape(answers.value.values[field.target], next.values[field.target])) {
			next.values[field.target] = answers.value.values[field.target]
		}
	}
	next.files = definition.attachments ? answers.value.files : []
	next.name = answers.value.name
	next.email = answers.value.email
	answers.value = next
	clientErrors.value = {}
	serverLocal.value = {}
	summary.value = ''
})

function sameShape(a: unknown, b: unknown): boolean {
	return typeof a === typeof b && Array.isArray(a) === Array.isArray(b)
}

// What the server said stays until its field is edited.
const serverLocal = ref<AnswerErrors>({})
const errors = computed<AnswerErrors>(() => ({...serverLocal.value, ...clientErrors.value}))

// Document order of everything that can carry an error.
const order = computed(() => [
	...props.definition.fields.map(field => field.target),
	SUBMITTER_NAME,
	SUBMITTER_EMAIL,
])

function elementFor(key: string): HTMLElement | null {
	const root = formElement.value
	if (!root) return null
	const container = key === SUBMITTER_NAME
		? root.querySelector('#fs-name')
		: key === SUBMITTER_EMAIL
			? root.querySelector('#fs-email')
			: root.querySelector(`[data-field="${CSS.escape(key)}"]`)
	if (!container) return null
	if (container instanceof HTMLInputElement) return container
	return container.querySelector<HTMLElement>('input:not([type=hidden]), select, textarea, button')
}

async function announceAndFocus(found: AnswerErrors) {
	const keys = order.value.filter(key => key in found)
	const unmatched = Object.keys(found).filter(key => !keys.includes(key))
	summary.value = t('forms.public.fixErrors', {count: keys.length + unmatched.length})
	await nextTick()
	for (const key of keys) {
		const element = elementFor(key)
		if (element) {
			element.focus()
			return
		}
	}
}

watch(() => props.serverErrors, found => {
	serverLocal.value = found ? {...found} : {}
	if (found) announceAndFocus({...found, ...clientErrors.value})
})

// Errors that name a field the form does not show (a stale definition) still need a place to be read.
const unmatchedErrors = computed(() => Object.entries(errors.value)
	.filter(([key]) => !order.value.includes(key))
	.map(([key, message]) => `${key}: ${message}`))

function clear(key: string) {
	if (key in clientErrors.value) {
		const {[key]: _removed, ...rest} = clientErrors.value
		clientErrors.value = rest
	}
	if (key in serverLocal.value) {
		const {[key]: _removed, ...rest} = serverLocal.value
		serverLocal.value = rest
	}
}

function setValue(target: string, value: unknown) {
	answers.value.values[target] = value
	clear(target)
}

function setFiles(target: string, value: unknown) {
	answers.value.files = Array.isArray(value) ? value as File[] : []
	clear(target)
}

async function submit() {
	if (props.submitting) return
	if (props.preview) {
		previewNote.value = true
		clientErrors.value = validateAnswers(props.definition, answers.value, t)
		return
	}
	previewNote.value = false
	const found = validateAnswers(props.definition, answers.value, t)
	clientErrors.value = found
	if (Object.keys(found).length > 0) {
		await announceAndFocus(found)
		return
	}
	summary.value = ''
	emit('submit', {
		body: buildSubmission(props.definition, answers.value, honeypot.value),
		files: props.definition.attachments ? [...answers.value.files] : [],
	})
}

function reset() {
	answers.value = initialAnswers(props.definition)
	honeypot.value = ''
	clientErrors.value = {}
	serverLocal.value = {}
	summary.value = ''
}

defineExpose({reset, answers})

const showSubmitter = computed(() => props.definition.submitter.collect_name || props.definition.submitter.collect_email)
</script>

<template>
	<form
		ref="formElement"
		class="fr"
		novalidate
		@submit.prevent="submit"
	>
		<div
			class="fr__live"
			role="status"
			aria-live="assertive"
		>
			<p
				v-if="summary"
				class="fr__summary"
			>
				{{ summary }}
			</p>
		</div>
		<ul
			v-if="unmatchedErrors.length > 0"
			class="fr__unmatched"
		>
			<li
				v-for="message in unmatchedErrors"
				:key="message"
			>
				{{ message }}
			</li>
		</ul>

		<FormFieldInput
			v-for="field in definition.fields"
			:key="field.target"
			:field="field"
			:model-value="field.kind === 'files' ? answers.files : answers.values[field.target]"
			:error="errors[field.target]"
			:disabled="submitting"
			:max-files="definition.attachments?.max_files"
			:max-file-size="definition.attachments?.max_file_size"
			@update:modelValue="field.kind === 'files' ? setFiles(field.target, $event) : setValue(field.target, $event)"
		/>

		<fieldset
			v-if="showSubmitter"
			class="fr__submitter"
		>
			<legend class="fr__legend">
				{{ $t('forms.public.aboutYou') }}
			</legend>
			<div
				v-if="definition.submitter.collect_name"
				id="fs-name"
				class="fr__row"
			>
				<label
					class="fr__label"
					for="fs-name-input"
				>
					{{ $t('forms.public.name') }}
					<template v-if="definition.submitter.name_required">
						<span
							class="fr__star"
							aria-hidden="true"
						>*</span>
						<span class="fr__sr"> ({{ $t('forms.public.required') }})</span>
					</template>
				</label>
				<input
					id="fs-name-input"
					v-model="answers.name"
					class="input fr__control"
					type="text"
					autocomplete="name"
					maxlength="250"
					:disabled="submitting || undefined"
					:aria-invalid="errors[SUBMITTER_NAME] ? true : undefined"
					:aria-describedby="errors[SUBMITTER_NAME] ? 'fs-name-error' : undefined"
					@input="clear(SUBMITTER_NAME)"
				>
				<p
					v-if="errors[SUBMITTER_NAME]"
					id="fs-name-error"
					class="fr__error"
				>
					{{ errors[SUBMITTER_NAME] }}
				</p>
			</div>
			<div
				v-if="definition.submitter.collect_email"
				id="fs-email"
				class="fr__row"
			>
				<label
					class="fr__label"
					for="fs-email-input"
				>
					{{ $t('forms.public.email') }}
					<template v-if="definition.submitter.email_required">
						<span
							class="fr__star"
							aria-hidden="true"
						>*</span>
						<span class="fr__sr"> ({{ $t('forms.public.required') }})</span>
					</template>
				</label>
				<input
					id="fs-email-input"
					v-model="answers.email"
					class="input fr__control"
					type="email"
					autocomplete="email"
					maxlength="250"
					:disabled="submitting || undefined"
					:aria-invalid="errors[SUBMITTER_EMAIL] ? true : undefined"
					:aria-describedby="errors[SUBMITTER_EMAIL] ? 'fs-email-error' : undefined"
					@input="clear(SUBMITTER_EMAIL)"
				>
				<p
					v-if="errors[SUBMITTER_EMAIL]"
					id="fs-email-error"
					class="fr__error"
				>
					{{ errors[SUBMITTER_EMAIL] }}
				</p>
			</div>
		</fieldset>

		<!-- The trap for bots: not focusable, not announced, not visible. -->
		<div
			class="fr__trap"
			aria-hidden="true"
		>
			<label>
				{{ $t('forms.public.honeypot') }}
				<input
					v-model="honeypot"
					type="text"
					name="website"
					tabindex="-1"
					autocomplete="off"
					data-testid="honeypot"
				>
			</label>
		</div>

		<p
			v-if="previewNote"
			class="fr__preview-note"
			role="status"
		>
			{{ $t('forms.preview.nothingSent') }}
		</p>

		<XButton
			type="submit"
			class="fr__submit"
			:loading="submitting"
			:disabled="submitting"
		>
			{{ $t('forms.public.submit') }}
		</XButton>
	</form>
</template>

<style lang="scss" scoped>
.fr {
	display: flex;
	flex-direction: column;
	gap: 1.25rem;
	min-inline-size: 0;

	&__live {
		min-block-size: 0;
	}

	&__summary {
		margin: 0;
		padding: .6rem .8rem;
		border-radius: $radius;
		border: 1px solid var(--danger);
		color: var(--danger);
		font-weight: 600;
	}

	&__unmatched {
		margin: 0;
		padding-inline-start: 1.2rem;
		color: var(--danger);
	}

	&__submitter {
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		padding: .75rem 1rem 1rem;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 1rem;
		min-inline-size: 0;
	}

	&__legend {
		font-weight: 600;
		padding-inline: .25rem;
	}

	&__row {
		display: flex;
		flex-direction: column;
		gap: .4rem;
	}

	&__label {
		font-weight: 600;
	}

	&__star {
		color: var(--danger);
		margin-inline-start: .15rem;
	}

	&__sr {
		position: absolute;
		inline-size: 1px;
		block-size: 1px;
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}

	&__control {
		min-block-size: 44px;
		inline-size: 100%;
	}

	&__error {
		margin: 0;
		font-size: .9rem;
		font-weight: 600;
		color: var(--danger);
	}

	&__trap {
		position: absolute;
		inset-inline-start: -10000px;
		inline-size: 1px;
		block-size: 1px;
		overflow: hidden;
	}

	&__preview-note {
		margin: 0;
		padding: .5rem .75rem;
		border-radius: $radius;
		background: var(--grey-100);
		color: var(--grey-700);
	}

	&__submit {
		align-self: flex-start;
		min-block-size: 44px;
		min-inline-size: 8rem;
	}

	input:focus-visible,
	button:focus-visible {
		outline: 2px solid var(--primary);
		outline-offset: 2px;
	}
}
</style>
