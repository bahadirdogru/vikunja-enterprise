<script setup lang="ts">
import {useI18n} from 'vue-i18n'

defineProps<{
	kind: 'loading' | 'empty' | 'error'
	message?: string
}>()

const {t} = useI18n({useScope: 'global'})
</script>

<template>
	<div
		class="chart-state"
		:class="`is-${kind}`"
		:role="kind === 'error' ? 'alert' : 'status'"
	>
		<template v-if="kind === 'loading'">
			<span class="visually-hidden">{{ t('charts.loading') }}</span>
			<span
				class="chart-state__skeleton"
				aria-hidden="true"
			>
				<span
					v-for="n in 6"
					:key="n"
					class="chart-state__bar"
					:style="{blockSize: `${30 + ((n * 37) % 60)}%`}"
				/>
			</span>
		</template>
		<p v-else-if="kind === 'empty'">
			{{ message ?? t('charts.noData') }}
		</p>
		<p v-else>
			{{ message ?? t('charts.error') }}
		</p>
	</div>
</template>

<style lang="scss" scoped>
.chart-state {
	display: flex;
	align-items: center;
	justify-content: center;
	block-size: 100%;
	min-block-size: 6rem;
	color: var(--text-light);
	text-align: center;
	font-size: .9rem;

	&.is-error {
		color: var(--danger);
	}
}

.chart-state__skeleton {
	display: flex;
	align-items: flex-end;
	gap: .5rem;
	inline-size: 70%;
	block-size: 60%;
}

.chart-state__bar {
	flex: 1;
	border-radius: 3px 3px 0 0;
	background: var(--grey-200);

	@media (prefers-reduced-motion: no-preference) {
		animation: chart-pulse 1.4s ease-in-out infinite;
	}
}

.visually-hidden {
	position: absolute;
	inline-size: 1px;
	block-size: 1px;
	overflow: hidden;
	clip-path: inset(50%);
	white-space: nowrap;
}

@keyframes chart-pulse {
	50% {
		opacity: .45;
	}
}
</style>
