<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import {sanitizeProjectDescription} from '@/helpers/sanitizeProjectDescription'

const props = defineProps<{
	text: string
}>()

const {t} = useI18n({useScope: 'global'})
// The text is the editor's HTML and was written by whoever may edit the dashboard, so it is sanitized like a
// project description.
const html = computed(() => sanitizeProjectDescription(props.text ?? ''))
const empty = computed(() => html.value.replace(/<[^>]*>/g, '').trim() === '')
</script>

<template>
	<p
		v-if="empty"
		class="text-widget__empty"
	>
		{{ t('dashboards.text.empty') }}
	</p>
	<!-- eslint-disable vue/no-v-html -->
	<div
		v-else
		class="text-widget content"
		data-user-content
		v-html="html"
	/>
	<!-- eslint-enable vue/no-v-html -->
</template>

<style lang="scss" scoped>
.text-widget {
	block-size: 100%;
	overflow: auto;
	overflow-wrap: anywhere;
}

.text-widget__empty {
	color: var(--text-light);
	font-style: italic;
}
</style>
