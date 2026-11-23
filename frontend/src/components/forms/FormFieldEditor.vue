<script setup lang="ts">
// One field of the form in the builder: a header with the target and the order buttons, and the settings of the field.
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import FormDefaultInput, {type LabelChoice} from '@/components/forms/FormDefaultInput.vue'
import XButton from '@/components/input/Button.vue'
import {customFieldTypeIcon} from '@/helpers/customFields'
import {PLACEHOLDERS, targetName, type FieldDraft, type FormAccess, type TargetInfo} from '@/helpers/forms'

const props = defineProps<{
	field: FieldDraft
	// Undefined when the target no longer exists in the project.
	info: TargetInfo | undefined
	index: number
	count: number
	open: boolean
	projectId: number
	access: FormAccess
	labels: LabelChoice[]
	errors: Map<string, string[]>
}>()

const emit = defineEmits<{
	patch: [patch: Partial<FieldDraft>]
	toggle: []
	move: [delta: -1 | 1]
	remove: []
}>()

const {t} = useI18n({useScope: 'global'})

const base = computed(() => `fb-f-${props.field.key}`)
const id = (name: string) => `${base.value}-${name}`
const errorsFor = (name: string) => props.errors.get(id(name)) ?? []
const describedBy = (name: string, extra = '') => [
	errorsFor(name).length > 0 ? `${id(name)}-error` : '',
	extra,
].filter(Boolean).join(' ') || undefined

const name = computed(() => props.info ? targetName(props.info, t) : props.field.target)
const isTitle = computed(() => props.field.target === 'title')
const isAttachments = computed(() => props.field.target === 'attachments')
const isLabels = computed(() => props.field.target === 'labels')
const hasDefault = computed(() => !!props.info && !isAttachments.value)
const textTarget = computed(() => props.field.target === 'title' || props.field.target === 'description'
	|| props.info?.kind === 'text' || props.info?.kind === 'long_text')

// A target that may not be asked on a public form can only be a preset there.
const lockedHidden = computed(() => props.access === 'public' && !!props.info && !props.info.public_visible)
const headErrors = computed(() => errorsFor('head'))
const hasErrors = computed(() => ['head', 'label', 'help', 'required', 'hidden', 'default', 'options']
	.some(control => errorsFor(control).length > 0))

const icon = computed(() => {
	switch (props.field.target) {
		case 'title': return 'font'
		case 'description': return 'align-left'
		case 'due_date': return 'calendar'
		case 'priority': return 'exclamation'
		case 'labels': return 'tags'
		case 'attachments': return 'paperclip'
		default: return customFieldTypeIcon(props.info?.kind)
	}
})

const placeholderTokens = PLACEHOLDERS.map(name => '{' + '{' + name + '}' + '}')

const optionIds = computed(() => props.field.options)

function toggleOption(labelId: number, checked: boolean) {
	const options = checked ? [...optionIds.value, labelId] : optionIds.value.filter(item => item !== labelId)
	const keep = Array.isArray(props.field.default)
		? (props.field.default as number[]).filter(item => options.includes(item))
		: props.field.default
	emit('patch', {options, default: Array.isArray(keep) && keep.length === 0 ? null : keep})
}

function setHidden(hidden: boolean) {
	emit('patch', {hidden})
}
</script>

<template>
	<li
		class="fe"
		:class="{'is-open': open, 'has-error': hasErrors}"
	>
		<div class="fe__bar">
			<button
				:id="id('head')"
				type="button"
				class="fe__toggle"
				:aria-expanded="open"
				:aria-controls="id('body')"
				:aria-describedby="headErrors.length > 0 ? `${id('head')}-error` : undefined"
				@click="emit('toggle')"
			>
				<Icon
					:icon="open ? 'chevron-up' : 'chevron-down'"
					class="fe__chevron"
				/>
				<Icon
					:icon="icon"
					class="fe__icon"
				/>
				<span class="fe__name">
					<strong>{{ field.label || name }}</strong>
					<span
						v-if="field.label && field.label !== name"
						class="fe__target"
					>{{ name }}</span>
				</span>
				<span
					v-if="field.required"
					class="fe__badge"
				>{{ $t('forms.builder.required') }}</span>
				<span
					v-if="field.hidden"
					class="fe__badge fe__badge--hidden"
				>{{ $t('forms.builder.hiddenBadge') }}</span>
				<span
					v-if="hasErrors"
					class="fe__badge fe__badge--error"
				>{{ $t('forms.builder.needsAttention') }}</span>
			</button>
			<span class="fe__actions">
				<XButton
					type="button"
					variant="secondary"
					icon="arrow-up"
					:disabled="index === 0"
					:aria-label="$t('forms.builder.moveUp', {name})"
					@click="emit('move', -1)"
				/>
				<XButton
					type="button"
					variant="secondary"
					icon="arrow-down"
					:disabled="index === count - 1"
					:aria-label="$t('forms.builder.moveDown', {name})"
					@click="emit('move', 1)"
				/>
				<XButton
					type="button"
					variant="secondary"
					icon="trash-alt"
					danger
					:aria-label="$t('forms.builder.removeField', {name})"
					@click="emit('remove')"
				/>
			</span>
		</div>
		<p
			v-for="message in headErrors"
			:id="`${id('head')}-error`"
			:key="message"
			class="help is-danger fe__error"
			role="alert"
		>
			{{ message }}
		</p>

		<div
			v-if="open"
			:id="id('body')"
			class="fe__body"
		>
			<p
				v-if="!info"
				class="help is-danger"
			>
				{{ $t('forms.errors.targetGone') }}
			</p>

			<div class="field">
				<label
					class="label"
					:for="id('label')"
				>{{ $t('forms.builder.label') }}</label>
				<input
					:id="id('label')"
					class="input"
					type="text"
					maxlength="250"
					:value="field.label"
					:placeholder="field.hidden ? $t('forms.builder.labelHiddenPlaceholder') : name"
					:aria-invalid="errorsFor('label').length > 0 || undefined"
					:aria-describedby="describedBy('label')"
					@input="emit('patch', {label: ($event.target as HTMLInputElement).value})"
				>
				<p
					v-for="message in errorsFor('label')"
					:id="`${id('label')}-error`"
					:key="message"
					class="help is-danger"
					role="alert"
				>
					{{ message }}
				</p>
			</div>

			<div class="field">
				<label
					class="label"
					:for="id('help')"
				>{{ $t('forms.builder.help') }}</label>
				<input
					:id="id('help')"
					class="input"
					type="text"
					maxlength="1000"
					:value="field.help"
					:aria-invalid="errorsFor('help').length > 0 || undefined"
					:aria-describedby="describedBy('help')"
					@input="emit('patch', {help: ($event.target as HTMLInputElement).value})"
				>
				<p
					v-for="message in errorsFor('help')"
					:id="`${id('help')}-error`"
					:key="message"
					class="help is-danger"
					role="alert"
				>
					{{ message }}
				</p>
			</div>

			<div class="fe__checks">
				<label class="checkbox">
					<input
						:id="id('required')"
						type="checkbox"
						:checked="field.required"
						:aria-describedby="describedBy('required')"
						@change="emit('patch', {required: ($event.target as HTMLInputElement).checked})"
					>
					{{ $t('forms.builder.requiredLabel') }}
				</label>
				<label
					v-if="!isAttachments"
					class="checkbox"
				>
					<input
						:id="id('hidden')"
						type="checkbox"
						:checked="field.hidden"
						:disabled="lockedHidden || undefined"
						:aria-describedby="describedBy('hidden', id('hidden-help'))"
						@change="setHidden(($event.target as HTMLInputElement).checked)"
					>
					{{ $t('forms.builder.hiddenLabel') }}
				</label>
			</div>
			<p
				v-for="message in [...errorsFor('required'), ...errorsFor('hidden')]"
				:id="`${id('hidden')}-error`"
				:key="message"
				class="help is-danger"
				role="alert"
			>
				{{ message }}
			</p>
			<p
				v-if="field.hidden"
				:id="id('hidden-help')"
				class="help"
			>
				{{ lockedHidden ? $t('forms.builder.hiddenLocked') : $t('forms.builder.hiddenHelp') }}
			</p>

			<div
				v-if="isLabels && !field.hidden"
				class="field"
			>
				<span
					:id="id('options-label')"
					class="label"
				>{{ $t('forms.builder.labelOptions') }}</span>
				<div
					:id="id('options')"
					class="fe__options"
					role="group"
					:aria-labelledby="id('options-label')"
					:aria-describedby="describedBy('options')"
				>
					<p
						v-if="labels.length === 0"
						class="help"
					>
						{{ $t('forms.builder.noLabels') }}
					</p>
					<label
						v-for="label in labels"
						:key="label.id"
						class="checkbox fe__option"
					>
						<input
							type="checkbox"
							:checked="optionIds.includes(label.id)"
							@change="toggleOption(label.id, ($event.target as HTMLInputElement).checked)"
						>
						<span
							class="fe__dot"
							:style="label.color ? {background: `#${label.color.replace('#', '')}`} : {}"
						/>
						{{ label.title }}
					</label>
				</div>
				<p
					v-for="message in errorsFor('options')"
					:id="`${id('options')}-error`"
					:key="message"
					class="help is-danger"
					role="alert"
				>
					{{ message }}
				</p>
			</div>

			<div
				v-if="hasDefault && info"
				class="field"
			>
				<span
					:id="id('default-label')"
					class="label"
				>{{ field.hidden ? $t('forms.builder.defaultHidden') : $t('forms.builder.defaultShown') }}</span>
				<FormDefaultInput
					:field="field"
					:info="info"
					:project-id="projectId"
					:labels="labels"
					:control-id="id('default')"
					:labelledby="id('default-label')"
					:describedby="describedBy('default', textTarget ? id('default-help') : '')"
					:invalid="errorsFor('default').length > 0"
					@update:modelValue="emit('patch', {default: $event})"
				/>
				<p
					v-if="textTarget"
					:id="id('default-help')"
					class="help"
				>
					{{ $t('forms.builder.placeholders') }}
					<code
						v-for="token in placeholderTokens"
						:key="token"
						class="fe__placeholder"
					>{{ token }}</code>
				</p>
				<p
					v-if="isTitle"
					class="help"
				>
					{{ $t('forms.builder.titleRule') }}
				</p>
				<p
					v-for="message in errorsFor('default')"
					:id="`${id('default')}-error`"
					:key="message"
					class="help is-danger"
					role="alert"
				>
					{{ message }}
				</p>
			</div>
			<p
				v-else-if="isAttachments"
				class="help"
			>
				{{ $t('forms.builder.attachmentsHelp') }}
			</p>
		</div>
	</li>
</template>

<style lang="scss" scoped>
.fe {
	list-style: none;
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	background: var(--white);
	min-inline-size: 0;

	&.has-error {
		border-color: var(--danger);
	}

	&__bar {
		display: flex;
		align-items: center;
		gap: .25rem;
		padding: .25rem .25rem .25rem 0;
		min-inline-size: 0;
	}

	&__toggle {
		flex: 1 1 auto;
		min-inline-size: 0;
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: .25rem .6rem;
		min-block-size: 2.75rem;
		padding: .25rem .75rem;
		background: none;
		border: 0;
		color: var(--text);
		cursor: pointer;
		text-align: start;
		font: inherit;

		&:focus-visible {
			outline: 2px solid var(--primary);
			outline-offset: -2px;
			border-radius: $radius;
		}
	}

	&__chevron,
	&__icon {
		color: var(--grey-500);
		flex: none;
	}

	&__name {
		display: flex;
		flex-direction: column;
		min-inline-size: 0;
		overflow-wrap: anywhere;
	}

	&__target {
		font-size: .8rem;
		color: var(--grey-600);
	}

	&__badge {
		font-size: .75rem;
		padding: .05rem .45rem;
		border-radius: 1rem;
		background: var(--grey-200);
		color: var(--grey-800);

		&--hidden {
			background: var(--warning-light, var(--grey-200));
		}

		&--error {
			background: var(--danger);
			color: var(--white);
		}
	}

	&__actions {
		display: flex;
		gap: .25rem;
		flex: none;
	}

	&__error {
		margin: 0 .75rem .5rem;
	}

	&__body {
		padding: .25rem 1rem 1rem;
		display: flex;
		flex-direction: column;
		gap: .5rem;
		border-block-start: 1px solid var(--grey-200);
	}

	&__checks {
		display: flex;
		flex-wrap: wrap;
		gap: .25rem 1.5rem;
	}

	&__options {
		display: flex;
		flex-wrap: wrap;
		gap: .25rem 1rem;
	}

	&__option {
		display: inline-flex;
		align-items: center;
		gap: .4rem;
	}

	&__dot {
		inline-size: .75rem;
		block-size: .75rem;
		border-radius: 50%;
		background: var(--grey-400);
	}

	&__placeholder {
		margin-inline-end: .35rem;
		font-size: .8rem;
	}
}
</style>
