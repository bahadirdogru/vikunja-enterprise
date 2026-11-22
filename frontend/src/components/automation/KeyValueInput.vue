<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
import {ref, watch} from 'vue'

const props = defineProps<{
	modelValue: unknown
	id: string
	label: string
}>()

const emit = defineEmits<{
	'update:modelValue': [value: Record<string, string>]
}>()

type Row = {key: string, value: string}

function rowsOf(value: unknown): Row[] {
	if (!value || typeof value !== 'object' || Array.isArray(value)) return []
	return Object.entries(value as Record<string, unknown>).map(([key, v]) => ({key, value: String(v ?? '')}))
}

function objectOf(rows: Row[]): Record<string, string> {
	const out: Record<string, string> = {}
	for (const row of rows) {
		if (row.key.trim() !== '') out[row.key.trim()] = row.value
	}
	return out
}

// Seeded once; the watcher below re-seeds on outside changes.
// eslint-disable-next-line vue/no-setup-props-reactivity-loss
const rows = ref<Row[]>(rowsOf(props.modelValue))

// Re-seed only when the value changed from the outside (not by our own emit).
watch(() => props.modelValue, value => {
	if (JSON.stringify(objectOf(rows.value)) !== JSON.stringify(value && typeof value === 'object' ? value : {})) {
		rows.value = rowsOf(value)
	}
})

function commit() {
	emit('update:modelValue', objectOf(rows.value))
}

function add() {
	rows.value.push({key: '', value: ''})
}

function remove(index: number) {
	rows.value.splice(index, 1)
	commit()
}
</script>

<template>
	<div
		:id="id"
		class="kv"
		role="group"
		:aria-label="label"
	>
		<div
			v-for="(row, index) in rows"
			:key="index"
			class="kv__row"
		>
			<input
				v-model="row.key"
				class="input"
				:aria-label="$t('automation.map.key', {n: index + 1})"
				:placeholder="$t('automation.map.keyPlaceholder')"
				@input="commit"
			>
			<input
				v-model="row.value"
				class="input"
				:aria-label="$t('automation.map.value', {n: index + 1})"
				:placeholder="$t('automation.map.valuePlaceholder')"
				@input="commit"
			>
			<XButton
				type="button"
				:aria-label="$t('automation.map.remove', {n: index + 1})"
				variant="secondary"
				icon="xmark"
				danger
				@click="remove(index)"
			/>
		</div>
		<XButton
			type="button"
			variant="secondary"
			icon="plus"
			@click="add"
		>
			{{ $t('automation.map.add') }}
		</XButton>
	</div>
</template>

<style lang="scss" scoped>
.kv {
	display: flex;
	flex-direction: column;
	gap: .4rem;
	align-items: flex-start;
	inline-size: 100%;

	&__row {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 2fr) auto;
		gap: .4rem;
		inline-size: 100%;

		@media screen and (max-width: $tablet) {
			grid-template-columns: minmax(0, 1fr) auto;

			input:nth-child(2) {
				grid-column: 1;
			}
		}
	}
}
</style>
