<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import ParamField from '@/components/automation/ParamField.vue'
import TypePicker from '@/components/automation/TypePicker.vue'
import {useAutomationContext} from '@/components/automation/context'
import {
	actionDef,
	catalogLabel,
	changeActionType,
	offeredActions,
	pathToId,
	type ActionDraft,
} from '@/helpers/automation'

const props = defineProps<{
	action: ActionDraft
	index: number
	count: number
}>()

const emit = defineEmits<{
	'update:action': [action: ActionDraft]
	'move': [delta: -1 | 1]
	'duplicate': []
	'remove': []
}>()

const {t, te} = useI18n({useScope: 'global'})
const context = useAutomationContext()

const path = computed(() => `actions[${props.index}]`)
const def = computed(() => actionDef(context.catalog, props.action.type))
const title = computed(() => catalogLabel(t, te, def.value?.title_key ?? '', props.action.type))
const messages = computed(() => context.errors.value.get(path.value) ?? [])
const typeMessages = computed(() => context.errors.value.get(`${path.value}.type`) ?? [])

function setType(type: string) {
	emit('update:action', changeActionType(props.action, actionDef(context.catalog, type), def.value))
}

function setParam(name: string, value: unknown) {
	emit('update:action', {...props.action, params: {...props.action.params, [name]: value}})
}
</script>

<template>
	<article
		:id="pathToId(path)"
		class="action-card"
		tabindex="-1"
		:aria-label="$t('automation.action.n', {n: index + 1, title})"
	>
		<header class="action-card__head">
			<span class="action-card__number">{{ index + 1 }}</span>
			<TypePicker
				:id="pathToId(`${path}.type`)"
				:model-value="action.type"
				:items="offeredActions(context.catalog, action.type)"
				:searchable="false"
				:label="$t('automation.action.type')"
				:invalid="typeMessages.length > 0"
				@update:modelValue="setType"
			/>
			<span class="action-card__tools">
				<XButton
					type="button"
					:disabled="index === 0"
					:aria-label="$t('automation.action.moveUp', {n: index + 1})"
					variant="secondary"
					icon="arrow-up"
					@click="emit('move', -1)"
				/>
				<XButton
					type="button"
					:disabled="index === count - 1"
					:aria-label="$t('automation.action.moveDown', {n: index + 1})"
					variant="secondary"
					icon="arrow-down"
					@click="emit('move', 1)"
				/>
				<XButton
					type="button"
					:aria-label="$t('automation.action.duplicate', {n: index + 1})"
					variant="secondary"
					icon="copy"
					@click="emit('duplicate')"
				/>
				<XButton
					type="button"
					:aria-label="$t('automation.action.remove', {n: index + 1})"
					variant="secondary"
					icon="trash-alt"
					danger
					@click="emit('remove')"
				/>
			</span>
		</header>
		<p
			v-for="message in [...messages, ...typeMessages]"
			:key="message"
			class="help is-danger"
			role="alert"
		>
			{{ message }}
		</p>
		<div
			v-if="def && def.params.length > 0"
			class="action-card__params"
		>
			<ParamField
				v-for="param in def.params"
				:key="param.name"
				:param="param"
				:siblings="def.params"
				:values="action.params"
				:action-type="action.type"
				:path="`${path}.params.${param.name}`"
				@update="setParam"
			/>
		</div>
		<p
			v-else-if="!def && action.type"
			class="help"
		>
			{{ $t('automation.action.unknown', {type: action.type}) }}
		</p>
	</article>
</template>

<style lang="scss" scoped>
.action-card {
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	padding: .75rem;
	background: var(--white);
	display: flex;
	flex-direction: column;
	gap: .6rem;

	&__head {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		align-items: center;
	}

	&__number {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		inline-size: 1.6rem;
		block-size: 1.6rem;
		border-radius: 50%;
		background: var(--primary);
		color: var(--white);
		font-size: .8rem;
		font-weight: 700;
		flex: none;
	}

	&__tools {
		margin-inline-start: auto;
		display: flex;
		gap: .25rem;
	}

	&__params {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: .75rem;
	}
}
</style>
