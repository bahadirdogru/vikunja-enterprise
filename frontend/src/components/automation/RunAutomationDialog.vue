<script setup lang="ts">
// "Run automation…": the manual rules of the project for one task. The rules are the ones the server lists for the
// project (enabled, with a manual trigger); a click queues a run and closes the dialog.
import {ref} from 'vue'

import XButton from '@/components/input/Button.vue'
import Card from '@/components/misc/Card.vue'
import Modal from '@/components/misc/Modal.vue'
import {useRunRuleManuallyMutation, type ManualAutomation} from '@/client/queries/automation'
import {isRequestContextAbort} from '@/client/requestContext'

const props = defineProps<{
	taskId: number
	projectId: number
	rules: ManualAutomation[]
}>()

const emit = defineEmits<{
	close: []
}>()

const run = useRunRuleManuallyMutation()
const running = ref<number | null>(null)

async function start(rule: ManualAutomation) {
	if (running.value !== null) return
	running.value = rule.id
	try {
		await run.mutateAsync({projectId: props.projectId, ruleId: rule.id, taskId: props.taskId, title: rule.title || rule.label})
		emit('close')
	} catch (cause) {
		// The failure is toasted by the mutation; the dialog stays open so another rule can be tried.
		if (isRequestContextAbort(cause)) emit('close')
	} finally {
		running.value = null
	}
}
</script>

<template>
	<Modal
		:enabled="true"
		:aria-label="$t('automation.manual.title')"
		@close="emit('close')"
	>
		<Card
			:title="$t('automation.manual.title')"
			:shadow="false"
			:show-close="true"
			class="run-automation has-text-start"
			@close="emit('close')"
		>
			<p class="help run-automation__intro">
				{{ $t('automation.manual.intro') }}
			</p>
			<ul class="run-automation__list">
				<li
					v-for="rule in rules"
					:key="rule.id"
				>
					<XButton
						type="button"
						variant="secondary"
						icon="play"
						:loading="running === rule.id"
						:disabled="running !== null && running !== rule.id"
						@click="start(rule)"
					>
						{{ rule.label }}
					</XButton>
				</li>
			</ul>
		</Card>
	</Modal>
</template>

<style lang="scss" scoped>
.run-automation {
	&__intro {
		margin-block-end: .75rem;
	}

	&__list {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: .5rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}
}
</style>
