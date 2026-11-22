<script setup lang="ts">
import {computed} from 'vue'
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
		class="rating"
		role="radiogroup"
		:aria-labelledby="labelledby"
	>
		<BaseButton
			v-for="star in max"
			:key="star"
			class="rating__star"
			:class="{'is-filled': star <= current}"
			role="radio"
			:aria-checked="star === current"
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
