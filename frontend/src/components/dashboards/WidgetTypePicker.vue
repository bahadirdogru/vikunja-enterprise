<script setup lang="ts">
import {useI18n} from 'vue-i18n'

import {OFFERED_WIDGET_TYPES, WIDGET_ICONS, type WidgetType} from '@/helpers/dashboards'

defineProps<{
	modelValue?: WidgetType | null
}>()

const emit = defineEmits<{
	'update:modelValue': [type: WidgetType]
}>()

const {t} = useI18n({useScope: 'global'})
</script>

<template>
	<ul
		class="type-picker"
		:aria-label="t('dashboards.editor.chooseType')"
	>
		<li
			v-for="type in OFFERED_WIDGET_TYPES"
			:key="type"
		>
			<button
				type="button"
				class="type-picker__item"
				:class="{'is-selected': modelValue === type}"
				:data-type="type"
				:aria-pressed="modelValue === type"
				@click="emit('update:modelValue', type)"
			>
				<span class="type-picker__icon">
					<Icon :icon="WIDGET_ICONS[type]" />
				</span>
				<span class="type-picker__text">
					<strong>{{ t(`dashboards.types.${type}.name`) }}</strong>
					<span>{{ t(`dashboards.types.${type}.description`) }}</span>
				</span>
			</button>
		</li>
	</ul>
</template>

<style lang="scss" scoped>
.type-picker {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
	gap: .75rem;
	margin: 0;
	padding: 0;
	list-style: none;
}

.type-picker__item {
	display: flex;
	align-items: flex-start;
	gap: .75rem;
	inline-size: 100%;
	padding: .75rem;
	border: 1px solid var(--grey-200);
	border-radius: var(--input-radius);
	background: var(--scheme-main);
	color: var(--text);
	font: inherit;
	text-align: start;
	cursor: pointer;

	&:hover {
		border-color: var(--primary);
	}

	&.is-selected {
		border-color: var(--primary);
		background: var(--primary-light, var(--grey-100));
	}
}

.type-picker__icon {
	flex: none;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	inline-size: 2.25rem;
	block-size: 2.25rem;
	border-radius: var(--input-radius);
	background: var(--grey-100);
	color: var(--primary);
	font-size: 1.1rem;
}

.type-picker__text {
	display: flex;
	flex-direction: column;
	gap: .15rem;
	min-inline-size: 0;
	font-size: .85rem;

	strong {
		color: var(--text-strong);
		font-size: .95rem;
	}

	span {
		color: var(--text-light);
	}
}
</style>
