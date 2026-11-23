<script setup lang="ts">
// One question of a form as the submitter sees it. Self-contained: it makes no requests and needs no stores.
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'

import {maxRatingOf, numberStep} from '@/helpers/customFields'
import {
	PRIORITY_VALUES,
	formatBytes,
	isDateOnly,
	isLongKind,
	isTextKind,
	textLimit,
	type PublicField,
} from '@/helpers/formsPublic'

const props = defineProps<{
	field: PublicField
	modelValue: unknown
	error?: string
	disabled?: boolean
	// The limits shown with a file picker.
	maxFiles?: number
	maxFileSize?: number
}>()

const emit = defineEmits<{
	'update:modelValue': [value: unknown]
}>()

const {t} = useI18n({useScope: 'global'})

const id = computed(() => `ff-${props.field.target}`)
const helpId = computed(() => `${id.value}-help`)
const errorId = computed(() => `${id.value}-error`)
const countId = computed(() => `${id.value}-count`)
const describedBy = computed(() => [
	props.field.help ? helpId.value : '',
	props.error ? errorId.value : '',
	showCount.value ? countId.value : '',
].filter(Boolean).join(' ') || undefined)

const GROUP_LIMIT = 6

const kind = computed(() => props.field.kind)
const options = computed(() => kind.value === 'priority'
	? PRIORITY_VALUES.map(value => ({id: value, title: t(`task.priority.${PRIORITY_KEYS[value]}`), color: ''}))
	: props.field.options)
const PRIORITY_KEYS: Record<number, string> = {1: 'low', 2: 'medium', 3: 'high', 4: 'urgent', 5: 'doNow'}

const isRadioGroup = computed(() => (kind.value === 'select' || kind.value === 'priority') && options.value.length <= GROUP_LIMIT)
const isCheckGroup = computed(() => (kind.value === 'multi_select' || kind.value === 'labels') && options.value.length <= GROUP_LIMIT)
const isRating = computed(() => kind.value === 'rating')
const isGrouped = computed(() => isRadioGroup.value || isCheckGroup.value || isRating.value || kind.value === 'files')

const textValue = computed(() => typeof props.modelValue === 'string' ? props.modelValue : '')
const limit = computed(() => textLimit(props.field))
const showCount = computed(() => (isLongKind(kind.value) || kind.value === 'text') && textValue.value.length >= limit.value * 0.8)

const selectedIds = computed(() => Array.isArray(props.modelValue) ? props.modelValue as number[] : [])
const maxRating = computed(() => maxRatingOf({config: props.field.config}))
const files = computed(() => Array.isArray(props.modelValue) ? props.modelValue as File[] : [])

function set(value: unknown) {
	emit('update:modelValue', value)
}

function toggle(optionId: number, checked: boolean) {
	set(checked ? [...selectedIds.value, optionId] : selectedIds.value.filter(item => item !== optionId))
}

function multiSelectChange(event: Event) {
	const select = event.target as HTMLSelectElement
	set([...select.selectedOptions].map(option => Number(option.value)))
}

function singleSelectChange(event: Event) {
	const value = (event.target as HTMLSelectElement).value
	set(value === '' ? null : Number(value))
}

const progress = computed(() => typeof props.modelValue === 'number' ? props.modelValue : 0)

const picker = ref<HTMLInputElement | null>(null)

function addFiles(event: Event) {
	const input = event.target as HTMLInputElement
	const chosen = [...(input.files ?? [])]
	// The same file picked twice is one file.
	const fresh = chosen.filter(file => !files.value.some(known => known.name === file.name && known.size === file.size))
	set([...files.value, ...fresh])
	input.value = ''
}

function removeFile(index: number) {
	set(files.value.filter((_, position) => position !== index))
	picker.value?.focus()
}

const inputType = computed(() => {
	if (kind.value === 'email') return 'email'
	if (kind.value === 'url') return 'url'
	return 'text'
})
const colorFor = (color: string) => color && /^#?[0-9a-fA-F]{6}$/.test(color) ? (color.startsWith('#') ? color : `#${color}`) : ''
</script>

<template>
	<component
		:is="isGrouped ? 'fieldset' : 'div'"
		class="ff"
		:class="{'has-error': error}"
		:data-field="field.target"
		:aria-describedby="isGrouped ? describedBy : undefined"
	>
		<legend
			v-if="isGrouped"
			class="ff__label"
		>
			{{ field.label }}
			<template v-if="field.required">
				<span
					class="ff__star"
					aria-hidden="true"
				>*</span>
				<span class="ff__sr"> ({{ $t('forms.public.required') }})</span>
			</template>
		</legend>
		<label
			v-else-if="kind !== 'checkbox'"
			class="ff__label"
			:for="id"
		>
			{{ field.label }}
			<template v-if="field.required">
				<span
					class="ff__star"
					aria-hidden="true"
				>*</span>
				<span class="ff__sr"> ({{ $t('forms.public.required') }})</span>
			</template>
		</label>

		<!-- short text, url, e-mail -->
		<input
			v-if="isTextKind(kind)"
			:id="id"
			class="input ff__control"
			:type="inputType"
			:value="textValue"
			:maxlength="limit"
			:required="field.required || undefined"
			:disabled="disabled || undefined"
			:aria-invalid="error ? true : undefined"
			:aria-describedby="describedBy"
			:autocomplete="kind === 'email' ? 'email' : 'off'"
			@input="set(($event.target as HTMLInputElement).value)"
		>

		<!-- long text -->
		<textarea
			v-else-if="isLongKind(kind)"
			:id="id"
			class="textarea ff__control"
			rows="5"
			:value="textValue"
			:maxlength="limit"
			:required="field.required || undefined"
			:disabled="disabled || undefined"
			:aria-invalid="error ? true : undefined"
			:aria-describedby="describedBy"
			@input="set(($event.target as HTMLTextAreaElement).value)"
		/>

		<!-- numbers -->
		<input
			v-else-if="kind === 'number' || kind === 'currency'"
			:id="id"
			class="input ff__control"
			type="number"
			inputmode="decimal"
			:value="typeof modelValue === 'string' ? modelValue : ''"
			:min="field.config?.min ?? undefined"
			:max="field.config?.max ?? undefined"
			:step="numberStep({type: kind, config: field.config})"
			:required="field.required || undefined"
			:disabled="disabled || undefined"
			:aria-invalid="error ? true : undefined"
			:aria-describedby="describedBy"
			@input="set(($event.target as HTMLInputElement).value)"
		>

		<!-- progress -->
		<div
			v-else-if="kind === 'progress'"
			class="ff__progress"
		>
			<input
				:id="id"
				class="ff__range"
				type="range"
				min="0"
				max="100"
				step="1"
				:value="progress"
				:disabled="disabled || undefined"
				:aria-invalid="error ? true : undefined"
				:aria-describedby="describedBy"
				@input="set(Number(($event.target as HTMLInputElement).value))"
			>
			<output
				:for="id"
				class="ff__output"
			>{{ modelValue === null ? '–' : `${progress}%` }}</output>
			<button
				v-if="modelValue !== null && !field.required"
				type="button"
				class="ff__clear"
				@click="set(null)"
			>
				{{ $t('forms.public.clear') }}
			</button>
		</div>

		<!-- date -->
		<input
			v-else-if="kind === 'date'"
			:id="id"
			class="input ff__control"
			:type="isDateOnly(field) ? 'date' : 'datetime-local'"
			:value="textValue"
			:required="field.required || undefined"
			:disabled="disabled || undefined"
			:aria-invalid="error ? true : undefined"
			:aria-describedby="describedBy"
			@input="set(($event.target as HTMLInputElement).value)"
		>

		<!-- checkbox -->
		<label
			v-else-if="kind === 'checkbox'"
			class="ff__choice ff__choice--single"
		>
			<input
				:id="id"
				type="checkbox"
				:checked="modelValue === true"
				:required="field.required || undefined"
				:disabled="disabled || undefined"
				:aria-invalid="error ? true : undefined"
				:aria-describedby="describedBy"
				@change="set(($event.target as HTMLInputElement).checked)"
			>
			<span>
				{{ field.label }}
				<template v-if="field.required">
					<span
						class="ff__star"
						aria-hidden="true"
					>*</span>
					<span class="ff__sr"> ({{ $t('forms.public.required') }})</span>
				</template>
			</span>
		</label>

		<!-- one of a few: radios -->
		<div
			v-else-if="isRadioGroup"
			class="ff__choices"
		>
			<label
				v-for="option in options"
				:key="option.id"
				class="ff__choice"
			>
				<input
					type="radio"
					:name="id"
					:value="option.id"
					:checked="modelValue === option.id"
					:required="field.required || undefined"
					:disabled="disabled || undefined"
					:aria-invalid="error ? true : undefined"
					@change="set(option.id)"
				>
				<span
					v-if="colorFor(option.color)"
					class="ff__dot"
					:style="{background: colorFor(option.color)}"
					aria-hidden="true"
				/>
				<span>{{ option.title }}</span>
			</label>
			<button
				v-if="modelValue !== null && !field.required"
				type="button"
				class="ff__clear"
				@click="set(null)"
			>
				{{ $t('forms.public.clear') }}
			</button>
		</div>

		<!-- one of many: select -->
		<select
			v-else-if="kind === 'select' || kind === 'priority'"
			:id="id"
			class="ff__control ff__select"
			:value="modelValue === null ? '' : String(modelValue)"
			:required="field.required || undefined"
			:disabled="disabled || undefined"
			:aria-invalid="error ? true : undefined"
			:aria-describedby="describedBy"
			@change="singleSelectChange"
		>
			<option value="">
				{{ $t('forms.public.choose') }}
			</option>
			<option
				v-for="option in options"
				:key="option.id"
				:value="String(option.id)"
			>
				{{ option.title }}
			</option>
		</select>

		<!-- several of a few: checkboxes -->
		<div
			v-else-if="isCheckGroup"
			class="ff__choices"
		>
			<label
				v-for="option in options"
				:key="option.id"
				class="ff__choice"
			>
				<input
					type="checkbox"
					:value="option.id"
					:checked="selectedIds.includes(option.id)"
					:disabled="disabled || undefined"
					:aria-invalid="error ? true : undefined"
					@change="toggle(option.id, ($event.target as HTMLInputElement).checked)"
				>
				<span
					v-if="colorFor(option.color)"
					class="ff__dot"
					:style="{background: colorFor(option.color)}"
					aria-hidden="true"
				/>
				<span>{{ option.title }}</span>
			</label>
		</div>

		<!-- several of many -->
		<select
			v-else-if="kind === 'multi_select' || kind === 'labels'"
			:id="id"
			class="ff__control ff__select ff__select--multiple"
			multiple
			size="6"
			:disabled="disabled || undefined"
			:aria-invalid="error ? true : undefined"
			:aria-describedby="describedBy"
			@change="multiSelectChange"
		>
			<option
				v-for="option in options"
				:key="option.id"
				:value="String(option.id)"
				:selected="selectedIds.includes(option.id)"
			>
				{{ option.title }}
			</option>
		</select>

		<!-- rating: native radios drawn as stars -->
		<div
			v-else-if="isRating"
			class="ff__stars"
		>
			<template
				v-for="n in maxRating"
				:key="n"
			>
				<input
					:id="`${id}-${n}`"
					type="radio"
					class="ff__star-input"
					:name="id"
					:value="n"
					:checked="modelValue === n"
					:required="field.required || undefined"
					:disabled="disabled || undefined"
					:aria-invalid="error ? true : undefined"
					@change="set(n)"
				>
				<label
					:for="`${id}-${n}`"
					class="ff__star-label"
					:class="{'is-on': typeof modelValue === 'number' && n <= modelValue}"
				>
					<span aria-hidden="true">★</span>
					<span class="ff__sr">{{ $t('forms.public.stars', {n, max: maxRating}) }}</span>
				</label>
			</template>
			<button
				v-if="modelValue !== null && !field.required"
				type="button"
				class="ff__clear"
				@click="set(null)"
			>
				{{ $t('forms.public.clear') }}
			</button>
		</div>

		<!-- files -->
		<div
			v-else-if="kind === 'files'"
			class="ff__files"
		>
			<input
				:id="id"
				ref="picker"
				class="ff__file-input"
				type="file"
				multiple
				:disabled="disabled || undefined"
				:aria-invalid="error ? true : undefined"
				:aria-describedby="describedBy"
				@change="addFiles"
			>
			<p
				v-if="maxFiles || maxFileSize"
				class="ff__hint"
			>
				{{ $t('forms.public.fileLimits', {count: maxFiles ?? 0, size: formatBytes(maxFileSize ?? 0)}) }}
			</p>
			<ul
				v-if="files.length > 0"
				class="ff__file-list"
			>
				<li
					v-for="(file, index) in files"
					:key="`${file.name}-${file.size}`"
				>
					<span class="ff__file-name">{{ file.name }}</span>
					<span class="ff__hint">{{ formatBytes(file.size) }}</span>
					<button
						type="button"
						class="ff__clear"
						:aria-label="$t('forms.public.removeFile', {name: file.name})"
						@click="removeFile(index)"
					>
						{{ $t('forms.public.remove') }}
					</button>
				</li>
			</ul>
		</div>

		<p
			v-if="showCount"
			:id="countId"
			class="ff__count"
		>
			{{ textValue.length }} / {{ limit }}
		</p>
		<p
			v-if="field.help"
			:id="helpId"
			class="ff__help"
		>
			{{ field.help }}
		</p>
		<p
			v-if="error"
			:id="errorId"
			class="ff__error"
		>
			{{ error }}
		</p>
	</component>
</template>

<style lang="scss" scoped>
.ff {
	display: flex;
	flex-direction: column;
	gap: .4rem;
	border: 0;
	margin: 0;
	padding: 0;
	min-inline-size: 0;

	&__label {
		font-weight: 600;
		padding: 0;
		overflow-wrap: anywhere;
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

	&__select {
		background: var(--input-background, var(--white));
		color: var(--input-color, var(--text));
		border: 1px solid var(--input-border-color, var(--grey-300));
		border-radius: $radius;
		padding: .5rem .75rem;
		font: inherit;

		&--multiple {
			min-block-size: 9rem;
		}
	}

	&__help,
	&__hint,
	&__count {
		margin: 0;
		font-size: .875rem;
		color: var(--grey-600);
	}

	&__count {
		text-align: end;
	}

	&__error {
		margin: 0;
		font-size: .9rem;
		font-weight: 600;
		color: var(--danger);
	}

	&.has-error {
		.input,
		.textarea,
		.ff__select {
			border-color: var(--danger);
		}
	}

	&__choices {
		display: flex;
		flex-direction: column;
		gap: .25rem;
	}

	&__choice {
		display: flex;
		align-items: center;
		gap: .6rem;
		min-block-size: 44px;
		cursor: pointer;
		overflow-wrap: anywhere;

		input {
			inline-size: 1.25rem;
			block-size: 1.25rem;
			flex: none;
			margin: 0;
		}
	}

	&__dot {
		inline-size: .75rem;
		block-size: .75rem;
		border-radius: 50%;
		flex: none;
	}

	&__clear {
		align-self: flex-start;
		min-block-size: 44px;
		padding: 0 .5rem;
		background: none;
		border: 0;
		color: var(--link, var(--primary));
		text-decoration: underline;
		cursor: pointer;
		font: inherit;
	}

	&__progress {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: .5rem 1rem;
	}

	&__range {
		flex: 1 1 10rem;
		min-block-size: 44px;
		accent-color: var(--primary);
	}

	&__output {
		min-inline-size: 3rem;
		font-variant-numeric: tabular-nums;
	}

	&__stars {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .1rem;
	}

	&__star-input {
		position: absolute;
		opacity: 0;
		inline-size: 1px;
		block-size: 1px;
	}

	&__star-label {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-inline-size: 44px;
		min-block-size: 44px;
		font-size: 1.9rem;
		line-height: 1;
		color: var(--grey-300);
		cursor: pointer;

		&.is-on {
			color: var(--warning, #f5a623);
		}
	}

	&__star-input:focus-visible + &__star-label {
		outline: 2px solid var(--primary);
		outline-offset: 2px;
		border-radius: $radius;
	}

	&__files {
		display: flex;
		flex-direction: column;
		gap: .4rem;
	}

	&__file-input {
		min-block-size: 44px;
		inline-size: 100%;
		font: inherit;
	}

	&__file-list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: .25rem;

		li {
			display: flex;
			flex-wrap: wrap;
			align-items: center;
			gap: .25rem .75rem;
		}
	}

	&__file-name {
		overflow-wrap: anywhere;
		flex: 1 1 8rem;
	}

	input:focus-visible,
	textarea:focus-visible,
	select:focus-visible,
	button:focus-visible {
		outline: 2px solid var(--primary);
		outline-offset: 2px;
	}
}
</style>
