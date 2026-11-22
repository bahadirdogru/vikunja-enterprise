<script setup lang="ts">
import {computed} from 'vue'

export type SelectChoice = {value: string | number | boolean, label: string, group?: string, disabled?: boolean}

const props = defineProps<{
	modelValue: unknown
	options: SelectChoice[]
	id?: string
	label?: string
	// Text of the empty entry; without it the select has none.
	placeholder?: string
	invalid?: boolean
	disabled?: boolean
	describedby?: string
}>()

const emit = defineEmits<{
	'update:modelValue': [value: string | number | boolean | null]
}>()

const current = computed(() => {
	const value = props.modelValue
	return value === undefined || value === null ? '' : String(value)
})

// A stored value that is not offered (a deleted status, a member outside the first page) stays visible.
const hasCurrent = computed(() => current.value === '' || props.options.some(option => String(option.value) === current.value))

const groups = computed(() => {
	const names: string[] = []
	for (const option of props.options) {
		if (option.group && !names.includes(option.group)) names.push(option.group)
	}
	return names
})

const ungrouped = computed(() => props.options.filter(option => !option.group))

function onChange(event: Event) {
	const raw = (event.target as HTMLSelectElement).value
	if (raw === '') {
		emit('update:modelValue', null)
		return
	}
	emit('update:modelValue', props.options.find(option => String(option.value) === raw)?.value ?? raw)
}
</script>

<template>
	<div
		class="select"
		:class="{'is-danger': invalid}"
	>
		<select
			:id="id"
			:value="current"
			:aria-label="label"
			:aria-invalid="invalid ? true : undefined"
			:aria-describedby="describedby"
			:disabled="disabled || undefined"
			@change="onChange"
		>
			<option
				v-if="placeholder !== undefined"
				value=""
			>
				{{ placeholder }}
			</option>
			<option
				v-if="!hasCurrent"
				:value="current"
			>
				#{{ current }}
			</option>
			<option
				v-for="option in ungrouped"
				:key="String(option.value)"
				:value="String(option.value)"
				:disabled="option.disabled || undefined"
			>
				{{ option.label }}
			</option>
			<optgroup
				v-for="group in groups"
				:key="group"
				:label="group"
			>
				<option
					v-for="option in options.filter(o => o.group === group)"
					:key="String(option.value)"
					:value="String(option.value)"
					:disabled="option.disabled || undefined"
				>
					{{ option.label }}
				</option>
			</optgroup>
		</select>
	</div>
</template>

<style lang="scss" scoped>
.select {
	max-inline-size: 100%;

	select {
		max-inline-size: 100%;
		inline-size: 100%;
	}
}
</style>
