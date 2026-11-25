<script setup lang="ts">
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'

import BaseButton from '@/components/base/BaseButton.vue'

const props = defineProps<{
	modelValue: unknown
	max: number
	labelledby: string
	disabled?: boolean
}>()

const emit = defineEmits<{
	commit: [value: number | null],
}>()

const {t} = useI18n({useScope: 'global'})

const current = computed(() => typeof props.modelValue === 'number' ? props.modelValue : 0)

const group = ref<HTMLElement | null>(null)

// The radiogroup pattern: one tab stop (the chosen star, else the first), the arrow keys move and choose.
const tabStop = computed(() => current.value >= 1 && current.value <= props.max ? current.value : 1)

function onKeydown(event: KeyboardEvent) {
	if (props.disabled) {
		return
	}
	const from = (event.target as HTMLElement).closest('[role="radio"]')
	const stars = [...(group.value?.querySelectorAll<HTMLElement>('[role="radio"]') ?? [])]
	const index = from ? stars.indexOf(from as HTMLElement) : -1
	if (index < 0) {
		return
	}
	let target: number
	switch (event.key) {
		case 'ArrowRight':
		case 'ArrowDown':
			target = (index + 1) % stars.length
			break
		case 'ArrowLeft':
		case 'ArrowUp':
			target = (index - 1 + stars.length) % stars.length
			break
		case 'Home':
			target = 0
			break
		case 'End':
			target = stars.length - 1
			break
		default:
			return
	}
	event.preventDefault()
	stars[target]?.focus()
	if (target + 1 !== current.value) {
		emit('commit', target + 1)
	}
}

function choose(star: number) {
	if (props.disabled) {
		return
	}
	// Clicking the current rating again clears it.
	emit('commit', star === current.value ? null : star)
}
</script>

<template>
	<div
		ref="group"
		class="rating"
		role="radiogroup"
		:aria-labelledby="labelledby"
		@keydown="onKeydown"
	>
		<BaseButton
			v-for="star in max"
			:key="star"
			class="rating__star"
			:class="{'is-filled': star <= current}"
			role="radio"
			:aria-checked="star === current"
			:tabindex="star === tabStop ? 0 : -1"
			:aria-label="t('customFields.value.ratingStar', {n: star, max})"
			:aria-disabled="disabled || undefined"
			@click="choose(star)"
		>
			<Icon :icon="star <= current ? 'star' : ['far', 'star']" />
		</BaseButton>
	</div>
</template>

<style lang="scss" scoped>
.rating {
	display: flex;
	flex-wrap: wrap;
	gap: .125rem;

	&__star {
		padding: .25rem;
		color: var(--grey-400);
		line-height: 1;
		transition: color $transition;

		&.is-filled {
			color: var(--warning);
		}

		&:hover,
		&:focus-visible {
			color: var(--warning);
		}

		&[aria-disabled="true"] {
			cursor: default;
		}
	}
}
</style>
