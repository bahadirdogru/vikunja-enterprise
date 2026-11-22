<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
// The condition tree of a rule (and of flow.stop_if): nested all / any / not groups over comparisons. No tree reads as
// "Always".
import ConditionNodeEditor from '@/components/automation/ConditionNodeEditor.vue'
import {useAutomationContext} from '@/components/automation/context'
import {
	addChild,
	newComparison,
	newGroup,
	pathToId,
	removeNode,
	replaceNode,
	type ConditionNode,
	type NodePath,
} from '@/helpers/automation'

const props = defineProps<{
	modelValue: ConditionNode | null
	path: string
}>()

const emit = defineEmits<{
	'update:modelValue': [value: ConditionNode | null]
}>()

const context = useAutomationContext()

function onChange(treePath: NodePath, node: ConditionNode) {
	emit('update:modelValue', replaceNode(props.modelValue, treePath, node))
}

function onAdd(treePath: NodePath, type: 'comparison' | 'group' | 'script') {
	const child = type === 'comparison' ? newComparison() : type === 'group' ? newGroup('all') : {script: ''}
	emit('update:modelValue', addChild(props.modelValue, treePath, child))
}

function onRemove(treePath: NodePath) {
	emit('update:modelValue', removeNode(props.modelValue, treePath))
}
</script>

<template>
	<div class="condition-builder">
		<ConditionNodeEditor
			v-if="modelValue"
			:node="modelValue"
			:tree-path="[]"
			:path="path"
			@change="onChange"
			@add="onAdd"
			@remove="onRemove"
		/>
		<div
			v-else
			:id="pathToId(path)"
			tabindex="-1"
			class="condition-builder__always"
		>
			<p>
				<strong>{{ $t('automation.condition.always') }}</strong>
				<span class="help">{{ $t('automation.condition.alwaysHint') }}</span>
			</p>
			<div class="condition-builder__buttons">
				<XButton
					type="button"
					variant="secondary"
					icon="plus"
					@click="onAdd([], 'comparison')"
				>
					{{ $t('automation.condition.addComparison') }}
				</XButton>
				<XButton
					v-if="context.catalog.supportsScript"
					type="button"
					variant="secondary"
					icon="code"
					@click="onAdd([], 'script')"
				>
					{{ $t('automation.condition.addScript') }}
				</XButton>
			</div>
		</div>
		<p
			v-for="message in context.errors.value.get(path) ?? []"
			v-show="!modelValue"
			:key="message"
			class="help is-danger"
			role="alert"
		>
			{{ message }}
		</p>
	</div>
</template>

<style lang="scss" scoped>
.condition-builder {
	inline-size: 100%;

	&__always {
		display: flex;
		flex-direction: column;
		gap: .5rem;
		align-items: flex-start;

		p {
			margin: 0;
			display: flex;
			flex-direction: column;
		}
	}

	&__buttons {
		display: flex;
		gap: .5rem;
		flex-wrap: wrap;
	}
}
</style>
