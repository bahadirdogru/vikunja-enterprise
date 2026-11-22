<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import AutomationBadge from '@/components/automation/AutomationBadge.vue'

const props = defineProps<{status: string}>()
const {t, te} = useI18n({useScope: 'global'})

const VARIANTS: Record<string, 'success' | 'danger' | 'warning' | 'info' | 'neutral'> = {
	success: 'success',
	failed: 'danger',
	running: 'info',
	skipped: 'neutral',
	aborted: 'warning',
}

const variant = computed(() => VARIANTS[props.status] ?? 'neutral')
const text = computed(() => te(`automation.runStatus.${props.status}`) ? t(`automation.runStatus.${props.status}`) : props.status)
</script>

<template>
	<AutomationBadge :variant="variant">
		{{ text }}
	</AutomationBadge>
</template>
