<script setup lang="ts">
import {nextTick, onBeforeUnmount, onMounted, ref, useId} from 'vue'
import {useI18n} from 'vue-i18n'

import BaseButton from '@/components/base/BaseButton.vue'

defineProps<{
	title: string
	wide?: boolean
}>()

const emit = defineEmits<{
	close: []
}>()

const {t} = useI18n({useScope: 'global'})
const titleId = useId()
const panel = ref<HTMLElement | null>(null)
let opener: HTMLElement | null = null

const focusable = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable="true"]'

function focusables(): HTMLElement[] {
	return [...(panel.value?.querySelectorAll<HTMLElement>(focusable) ?? [])].filter(el => el.offsetParent !== null || el === document.activeElement)
}

// Focus moves into the panel and comes back to what opened it; Tab stays inside while it is open.
onMounted(async () => {
	opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
	await nextTick()
	panel.value?.focus()
})

onBeforeUnmount(() => {
	if (opener?.isConnected) {
		opener.focus()
	}
})

function onKeydown(event: KeyboardEvent) {
	if (event.key === 'Escape') {
		event.stopPropagation()
		emit('close')
		return
	}
	if (event.key !== 'Tab') {
		return
	}
	const items = focusables()
	if (items.length === 0) {
		event.preventDefault()
		return
	}
	const first = items[0]!
	const last = items[items.length - 1]!
	if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.value)) {
		event.preventDefault()
		last.focus()
	} else if (!event.shiftKey && document.activeElement === last) {
		event.preventDefault()
		first.focus()
	}
}
</script>

<template>
	<Teleport to="body">
		<div
			class="side-panel__backdrop"
			@click.self="emit('close')"
		>
			<section
				ref="panel"
				class="side-panel"
				:class="{'is-wide': wide}"
				role="dialog"
				aria-modal="true"
				:aria-labelledby="titleId"
				tabindex="-1"
				@keydown="onKeydown"
			>
				<header class="side-panel__header">
					<h2
						:id="titleId"
						class="side-panel__title"
					>
						{{ title }}
					</h2>
					<BaseButton
						class="side-panel__close"
						:aria-label="t('misc.close')"
						@click="emit('close')"
					>
						<Icon icon="times" />
					</BaseButton>
				</header>
				<div class="side-panel__body">
					<slot />
				</div>
				<footer
					v-if="$slots.footer"
					class="side-panel__footer"
				>
					<slot name="footer" />
				</footer>
			</section>
		</div>
	</Teleport>
</template>

<style lang="scss" scoped>
.side-panel__backdrop {
	position: fixed;
	inset: 0;
	z-index: 100;
	display: flex;
	justify-content: flex-end;
	background: rgba(0, 0, 0, .35);
}

.side-panel {
	display: flex;
	flex-direction: column;
	inline-size: min(34rem, 100%);
	block-size: 100%;
	background: var(--scheme-main);
	box-shadow: var(--shadow-lg);
	color: var(--text);
	outline: none;

	&.is-wide {
		inline-size: min(64rem, 100%);
	}

	@media (prefers-reduced-motion: no-preference) {
		animation: side-panel-in .18s ease-out;
	}
}

.side-panel__header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 1rem;
	padding: .9rem 1.25rem;
	border-block-end: 1px solid var(--grey-200);
}

.side-panel__title {
	margin: 0;
	font-size: 1.15rem;
	font-weight: 600;
	color: var(--text-strong);
	overflow-wrap: anywhere;
}

.side-panel__close {
	padding: .4rem;
	border-radius: var(--input-radius);
	color: var(--text-light);

	&:hover {
		background: var(--grey-100);
	}
}

.side-panel__body {
	flex: 1 1 auto;
	min-block-size: 0;
	padding: 1rem 1.25rem;
	overflow-y: auto;
}

.side-panel__footer {
	display: flex;
	flex-wrap: wrap;
	justify-content: flex-end;
	gap: .5rem;
	padding: .75rem 1.25rem;
	border-block-start: 1px solid var(--grey-200);
}

@keyframes side-panel-in {
	from {
		transform: translateX(2rem);
		opacity: 0;
	}
}
</style>
