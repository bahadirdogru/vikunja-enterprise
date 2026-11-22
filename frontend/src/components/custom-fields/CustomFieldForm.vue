<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'

import XButton from '@/components/input/Button.vue'
import BaseButton from '@/components/base/BaseButton.vue'
import FancyCheckbox from '@/components/input/FancyCheckbox.vue'
import FormField from '@/components/input/FormField.vue'
import FormInput from '@/components/input/FormInput.vue'
import Message from '@/components/misc/Message.vue'
import {
	CUSTOM_FIELD_TYPES,
	MAX_MAX_RATING,
	MAX_PRECISION,
	createCustomFieldDraft,
	customFieldToDraft,
	hasDraftErrors,
	hasNumericRange,
	hasOptions,
	moveItem,
	newOptionKey,
	validateCustomFieldDraft,
	type CustomFieldDraft,
	type CustomFieldDraftErrors,
	type ValidationError,
} from '@/helpers/customFields'
import type {CustomFieldResponse} from '@/client/queries/customFields'

const props = withDefaults(defineProps<{
	// Set when editing; the type is then fixed.
	field?: CustomFieldResponse | null
	saving?: boolean
	apiError?: string | null
}>(), {
	field: null,
	saving: false,
	apiError: null,
})

const emit = defineEmits<{
	submit: [draft: CustomFieldDraft],
	cancel: [],
}>()

const {t} = useI18n({useScope: 'global'})

const isEdit = computed(() => props.field !== null)
// eslint-disable-next-line vue/no-setup-props-reactivity-loss -- seeded once; the page keys the form on the field
const draft = ref<CustomFieldDraft>(props.field ? customFieldToDraft(props.field) : createCustomFieldDraft())
const errors = ref<CustomFieldDraftErrors>({})
const submitted = ref(false)

function message(error: ValidationError | undefined): string | null {
	return error ? t(error.key, error.params ?? {}) : null
}

function validate() {
	errors.value = validateCustomFieldDraft(draft.value)
	return !hasDraftErrors(errors.value)
}

// After the first submit attempt errors follow the input, so they clear as soon as they are fixed.
function revalidate() {
	if (submitted.value) {
		validate()
	}
}

watch(draft, revalidate, {deep: true})

function submit() {
	submitted.value = true
	if (!validate()) {
		return
	}
	emit('submit', draft.value)
}

function addOption() {
	draft.value.options.push({key: newOptionKey(), title: '', color: ''})
}

function removeOption(index: number) {
	draft.value.options.splice(index, 1)
}

function moveOption(index: number, delta: -1 | 1) {
	draft.value.options = moveItem(draft.value.options, index, delta)
}

function setMaxRating(value: string | number) {
	draft.value.maxRating = Number(value)
}

const typeOptions = computed(() => CUSTOM_FIELD_TYPES.map(type => ({
	value: type,
	label: t(`customFields.types.${type}`),
})))
</script>

<template>
	<form
		class="custom-field-form"
		novalidate
		@submit.prevent="submit"
	>
		<FormField
			id="custom-field-title"
			v-model="draft.title"
			v-focus
			:label="$t('customFields.form.title')"
			:placeholder="$t('customFields.form.titlePlaceholder')"
			:error="message(errors.title)"
			required
		/>

		<FormField
			:label="$t('customFields.form.type')"
		>
			<template #default="{id, errorId}">
				<div class="select">
					<select
						:id="id"
						v-model="draft.type"
						:disabled="isEdit || undefined"
						:aria-describedby="isEdit ? `${id}-hint` : errorId"
					>
						<option
							v-for="option in typeOptions"
							:key="option.value"
							:value="option.value"
						>
							{{ option.label }}
						</option>
					</select>
				</div>
				<p
					v-if="isEdit"
					:id="`${id}-hint`"
					class="help"
				>
					{{ $t('customFields.form.typeLocked') }}
				</p>
			</template>
		</FormField>

		<FormField :label="$t('customFields.form.description')">
			<template #default="{id}">
				<textarea
					:id="id"
					v-model="draft.description"
					class="textarea"
					rows="2"
					:placeholder="$t('customFields.form.descriptionPlaceholder')"
				/>
			</template>
		</FormField>

		<div class="field">
			<FancyCheckbox v-model="draft.required">
				{{ $t('customFields.form.required') }}
			</FancyCheckbox>
			<p class="help">
				{{ $t('customFields.form.requiredHint') }}
			</p>
		</div>

		<template v-if="hasNumericRange(draft.type)">
			<div class="config-grid">
				<FormField
					:label="$t('customFields.form.min')"
					:error="message(errors.min)"
				>
					<template #default="{id, errorId}">
						<FormInput
							:id="id"
							v-model="draft.min"
							type="number"
							step="any"
							:aria-describedby="errors.min ? errorId : undefined"
						/>
					</template>
				</FormField>
				<FormField
					:label="$t('customFields.form.max')"
					:error="message(errors.max)"
				>
					<template #default="{id, errorId}">
						<FormInput
							:id="id"
							v-model="draft.max"
							type="number"
							step="any"
							:aria-describedby="errors.max ? errorId : undefined"
						/>
					</template>
				</FormField>
				<FormField
					:label="$t('customFields.form.precision')"
					:error="message(errors.precision)"
				>
					<template #default="{id, errorId}">
						<FormInput
							:id="id"
							v-model="draft.precision"
							type="number"
							min="0"
							:max="MAX_PRECISION"
							step="1"
							:aria-describedby="errors.precision ? errorId : undefined"
						/>
					</template>
				</FormField>
				<FormField
					v-if="draft.type === 'currency'"
					:label="$t('customFields.form.currency')"
					:error="message(errors.currency)"
				>
					<template #default="{id, errorId}">
						<FormInput
							:id="id"
							v-model="draft.currency"
							maxlength="3"
							placeholder="TRY"
							autocapitalize="characters"
							:aria-describedby="errors.currency ? errorId : undefined"
						/>
					</template>
				</FormField>
			</div>
			<p
				v-if="errors.range"
				class="help is-danger"
				role="alert"
			>
				{{ message(errors.range) }}
			</p>
		</template>

		<FormField
			v-if="draft.type === 'rating'"
			:label="$t('customFields.form.maxRating')"
			:error="message(errors.maxRating)"
		>
			<template #default="{id, errorId}">
				<FormInput
					:id="id"
					:model-value="draft.maxRating"
					type="number"
					min="1"
					:max="MAX_MAX_RATING"
					step="1"
					:aria-describedby="errors.maxRating ? errorId : undefined"
					@update:modelValue="setMaxRating"
				/>
			</template>
		</FormField>

		<fieldset
			v-if="hasOptions(draft.type)"
			class="options"
		>
			<legend class="label">
				{{ $t('customFields.form.options') }}
			</legend>
			<ul class="options__list">
				<li
					v-for="(option, index) in draft.options"
					:key="option.key"
					class="options__item"
				>
					<input
						v-model="option.color"
						type="color"
						class="options__color"
						:class="{'is-empty': option.color === ''}"
						:aria-label="$t('customFields.form.optionColor', {n: index + 1})"
					>
					<FormInput
						v-model="option.title"
						class="options__title"
						:aria-label="$t('customFields.form.optionTitle', {n: index + 1})"
						:placeholder="$t('customFields.form.optionPlaceholder')"
					/>
					<BaseButton
						v-if="option.color !== ''"
						class="options__action"
						:aria-label="$t('customFields.form.optionClearColor', {n: index + 1})"
						@click="option.color = ''"
					>
						<Icon icon="fill-drip" />
					</BaseButton>
					<BaseButton
						class="options__action"
						:aria-disabled="index === 0 || undefined"
						:aria-label="$t('customFields.form.optionMoveUp', {n: index + 1})"
						@click="index > 0 && moveOption(index, -1)"
					>
						<Icon icon="arrow-up" />
					</BaseButton>
					<BaseButton
						class="options__action"
						:aria-disabled="index === draft.options.length - 1 || undefined"
						:aria-label="$t('customFields.form.optionMoveDown', {n: index + 1})"
						@click="index < draft.options.length - 1 && moveOption(index, 1)"
					>
						<Icon icon="arrow-down" />
					</BaseButton>
					<BaseButton
						class="options__action is-danger"
						:aria-label="$t('customFields.form.optionRemove', {n: index + 1})"
						@click="removeOption(index)"
					>
						<Icon icon="trash-alt" />
					</BaseButton>
				</li>
			</ul>
			<p
				v-if="errors.options"
				class="help is-danger"
				role="alert"
			>
				{{ message(errors.options) }}
			</p>
			<XButton
				variant="secondary"
				icon="plus"
				@click="addOption"
			>
				{{ $t('customFields.form.addOption') }}
			</XButton>
		</fieldset>

		<Message
			v-if="apiError"
			variant="danger"
			class="mbs-4"
		>
			{{ apiError }}
		</Message>

		<div class="custom-field-form__actions">
			<XButton
				variant="secondary"
				@click="emit('cancel')"
			>
				{{ $t('misc.cancel') }}
			</XButton>
			<XButton
				type="submit"
				:loading="saving"
			>
				{{ isEdit ? $t('misc.save') : $t('customFields.add') }}
			</XButton>
		</div>
	</form>
</template>

<style lang="scss" scoped>
.custom-field-form {
	display: flex;
	flex-direction: column;
	gap: .75rem;

	&__actions {
		display: flex;
		justify-content: flex-end;
		gap: .5rem;
	}
}

.config-grid {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
	gap: .75rem;
}

.options {
	border: 0;
	margin: 0;
	padding: 0;

	&__list {
		list-style: none;
		margin: 0 0 .5rem;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: .375rem;
	}

	&__item {
		display: flex;
		align-items: center;
		gap: .25rem;
	}

	&__title {
		flex: 1 1 auto;
		min-inline-size: 0;
	}

	&__color {
		appearance: none;
		inline-size: 2rem;
		block-size: 2rem;
		padding: 0;
		border: 1px solid var(--grey-300);
		border-radius: $radius;
		background: transparent;
		cursor: pointer;

		&.is-empty {
			opacity: .45;
		}
	}

	&__action {
		padding: .375rem;
		color: var(--grey-500);
		line-height: 1;

		&:hover,
		&:focus-visible {
			color: var(--primary);
		}

		&.is-danger:hover,
		&.is-danger:focus-visible {
			color: var(--danger);
		}

		&[aria-disabled="true"] {
			opacity: .35;
			cursor: default;
		}
	}
}
</style>
