<script setup lang="ts">
import {ref, watch} from 'vue'

import Card from '@/components/misc/Card.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import Modal from '@/components/misc/Modal.vue'
import XButton from '@/components/input/Button.vue'

const props = defineProps<{
	enabled: boolean
	removedTitles: string[]
	targets: {id: number, title: string}[]
	defaultTarget: number
	busy?: boolean
}>()

const emit = defineEmits<{
	close: []
	confirm: [target: number]
}>()

const target = ref(0)

watch(() => props.enabled, enabled => {
	if (enabled) target.value = props.defaultTarget
}, {immediate: true})
</script>

<template>
	<Modal
		:enabled="enabled"
		:aria-label="$t('workflow.move.title')"
		@close="emit('close')"
	>
		<Card
			:title="$t('workflow.move.title')"
			:shadow="false"
			:show-close="true"
			class="has-text-start"
			@close="emit('close')"
		>
			<p>{{ $t('workflow.move.text', {statuses: removedTitles.join(', ')}) }}</p>
			<div class="field">
				<label
					class="label"
					for="workflow-move-target"
				>{{ $t('workflow.move.target') }}</label>
				<FormSelect
					id="workflow-move-target"
					:model-value="target"
					:options="targets.map(entry => ({value: entry.id, label: entry.title}))"
					@update:modelValue="value => target = Number(value)"
				/>
			</div>
			<template #footer>
				<XButton
					variant="secondary"
					@click="emit('close')"
				>
					{{ $t('misc.cancel') }}
				</XButton>
				<XButton
					class="mis-2"
					:loading="busy"
					@click="emit('confirm', target)"
				>
					{{ $t('workflow.move.confirm') }}
				</XButton>
			</template>
		</Card>
	</Modal>
</template>
