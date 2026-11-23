<script setup lang="ts">
// The cron expression of the schedule trigger: a text input, a few presets and the problems of the expression as
// the server would report them (the server stays the authority).
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import XButton from '@/components/input/Button.vue'
import {CRON_PRESETS, cronProblem} from '@/helpers/automationCron'

const props = defineProps<{
	id: string
	modelValue: unknown
	label: string
	invalid?: boolean
}>()

const emit = defineEmits<{
	'update:modelValue': [value: string]
}>()

const {t} = useI18n({useScope: 'global'})

const text = computed(() => typeof props.modelValue === 'string' ? props.modelValue : '')
// An empty input is the business of the required check, which only shows after a save attempt.
const problem = computed(() => {
	const found = text.value.trim() === '' ? null : cronProblem(text.value)
	return found && found !== 'empty' ? t(`automation.validation.cron.${found}`) : ''
})
// While the field shows messages of its own (the same problem after a failed save), the help line stays plain.
const helpProblem = computed(() => props.invalid ? '' : problem.value)
</script>

<template>
	<div class="cron-input">
		<input
			:id="id"
			class="input"
			:class="{'is-danger': problem !== '' || invalid}"
			type="text"
			autocomplete="off"
			spellcheck="false"
			placeholder="0 9 * * 1"
			:value="text"
			:aria-label="label"
			:aria-invalid="problem !== '' || invalid ? true : undefined"
			:aria-describedby="`${id}-help`"
			@input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
		>
		<div
			class="cron-input__presets"
			role="group"
			:aria-label="$t('automation.cron.presets.label')"
		>
			<XButton
				v-for="preset in CRON_PRESETS"
				:key="preset.value"
				type="button"
				variant="secondary"
				@click="emit('update:modelValue', preset.value)"
			>
				{{ $t(preset.labelKey) }}
			</XButton>
		</div>
		<p
			:id="`${id}-help`"
			class="help"
			:class="{'is-danger': helpProblem !== ''}"
		>
			{{ helpProblem || $t('automation.cron.help') }}
		</p>
	</div>
</template>

<style lang="scss" scoped>
.cron-input {
	display: flex;
	flex-direction: column;
	gap: .4rem;

	&__presets {
		display: flex;
		flex-wrap: wrap;
		gap: .4rem;
	}
}
</style>
