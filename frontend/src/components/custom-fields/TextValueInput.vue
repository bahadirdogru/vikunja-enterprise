<script setup lang="ts">
import {computed, nextTick, onMounted, ref, watch} from 'vue'

import FormInput from '@/components/input/FormInput.vue'
import {isValidHttpUrl} from '@/helpers/isValidHttpUrl'
import {isValidEmail} from '@/helpers/customFields'

const props = defineProps<{
	type: 'text' | 'long_text' | 'url' | 'email'
	modelValue: unknown
	labelledby: string
	disabled?: boolean
	required?: boolean
	error?: string | null
}>()

const emit = defineEmits<{
	commit: [value: string],
}>()

const text = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)

function autosize() {
	const el = textarea.value
	if (el) {
		el.style.blockSize = 'auto'
		el.style.blockSize = `${el.scrollHeight}px`
	}
}

watch(text, () => nextTick(autosize))
onMounted(autosize)

watch(
	() => props.modelValue,
	value => {
		text.value = typeof value === 'string' ? value : ''
	},
	{immediate: true},
)

const href = computed(() => {
	const value = text.value.trim()
	if (props.type === 'url' && isValidHttpUrl(value)) return value
	if (props.type === 'email' && isValidEmail(value)) return `mailto:${value}`
	return null
})

const inputType = computed(() => props.type === 'url' ? 'url' : props.type === 'email' ? 'email' : 'text')

function commit() {
	if (props.disabled) {
		return
	}
	emit('commit', text.value.trim())
}

function commitOnEnter(event: KeyboardEvent) {
	// Shift+Enter or plain Enter in a textarea keeps typing.
	if (props.type !== 'long_text') {
		event.preventDefault()
		commit()
	}
}
</script>

<template>
	<div class="text-value">
		<textarea
			v-if="type === 'long_text'"
			ref="textarea"
			v-model="text"
			class="textarea"
			rows="1"
			:placeholder="$t('misc.notSet')"
			:readonly="disabled"
			:aria-labelledby="labelledby"
			:aria-required="required || undefined"
			:aria-invalid="error ? true : undefined"
			@blur="commit"
		/>
		<FormInput
			v-else
			v-model="text"
			:type="inputType"
			:placeholder="$t('misc.notSet')"
			:readonly="disabled"
			:aria-labelledby="labelledby"
			:aria-required="required || undefined"
			:error="error"
			@blur="commit"
			@keydown.enter="commitOnEnter"
		/>
		<p
			v-if="type === 'long_text' && error"
			class="help is-danger"
			role="alert"
		>
			{{ error }}
		</p>
		<a
			v-if="href"
			:href="href"
			class="text-value__link"
			:target="type === 'url' ? '_blank' : undefined"
			rel="noopener noreferrer"
		>
			{{ type === 'url' ? $t('customFields.value.openLink') : $t('customFields.value.writeMail') }}
		</a>
	</div>
</template>

<style lang="scss" scoped>
.text-value {
	display: flex;
	flex-direction: column;
	gap: .25rem;
	inline-size: 100%;

	.textarea {
		resize: none;
		overflow: hidden;
		min-block-size: 2rem;
		padding-block: .3rem;
	}

	&__link {
		font-size: .8125rem;
		inline-size: fit-content;
	}
}
</style>
