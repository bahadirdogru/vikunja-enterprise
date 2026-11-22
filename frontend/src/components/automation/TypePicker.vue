<script setup lang="ts">
// Picks a trigger or action type from the catalog: grouped by the first segment of the type, with a search field when
// the list is long.
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'

import AutomationSelect, {type SelectChoice} from '@/components/automation/AutomationSelect.vue'
import {catalogLabel, humanize} from '@/helpers/automation'

const props = withDefaults(defineProps<{
	modelValue: string
	items: {type: string, title_key: string}[]
	id: string
	label: string
	placeholder?: string
	invalid?: boolean
	// Show the search field for long lists.
	searchable?: boolean
}>(), {
	placeholder: undefined,
	invalid: false,
	searchable: true,
})

const emit = defineEmits<{
	'update:modelValue': [value: string]
}>()

const SEARCH_THRESHOLD = 8

const {t, te} = useI18n({useScope: 'global'})
const search = ref('')

const labelOf = (item: {type: string, title_key: string}) => catalogLabel(t, te, item.title_key, item.type)

const groups = computed(() => new Set(props.items.map(item => item.type.split('.')[0])))

const choices = computed<SelectChoice[]>(() => {
	const needle = search.value.trim().toLowerCase()
	const out = props.items
		.map(item => ({item, label: labelOf(item)}))
		.filter(({item, label}) => needle === '' || label.toLowerCase().includes(needle) || item.type.toLowerCase().includes(needle))
		.map(({item, label}) => {
			const segment = item.type.split('.')[0]
			const key = `automation.group.${segment}`
			return {
				value: item.type,
				label,
				group: groups.value.size > 1 ? (te(key) ? t(key) : humanize(segment)) : undefined,
			}
		})
	// The current value stays visible while a search hides it.
	const current = props.items.find(item => item.type === props.modelValue)
	if (current && !out.some(choice => choice.value === current.type)) {
		out.unshift({value: current.type, label: labelOf(current), group: undefined})
	}
	return out
})

function pick(value: unknown) {
	if (typeof value === 'string') emit('update:modelValue', value)
}
</script>

<template>
	<div class="type-picker">
		<input
			v-if="searchable && items.length > SEARCH_THRESHOLD"
			v-model="search"
			class="input"
			type="search"
			:aria-label="$t('automation.picker.search')"
			:placeholder="$t('automation.picker.search')"
		>
		<AutomationSelect
			:id="id"
			:model-value="modelValue"
			:options="choices"
			:label="label"
			:placeholder="placeholder ?? $t('automation.param.choose')"
			:invalid="invalid"
			@update:modelValue="pick"
		/>
	</div>
</template>

<style lang="scss" scoped>
.type-picker {
	display: flex;
	flex-wrap: wrap;
	gap: .4rem;
	align-items: center;

	.input {
		max-inline-size: 14rem;
	}
}
</style>
