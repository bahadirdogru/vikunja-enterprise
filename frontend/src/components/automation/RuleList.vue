<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'

import AutomationBadge from '@/components/automation/AutomationBadge.vue'
import RunStatusBadge from '@/components/automation/RunStatusBadge.vue'
import Modal from '@/components/misc/Modal.vue'
import {
	useDeleteRuleMutation,
	useToggleRuleMutation,
	type AutomationRuleResponse,
} from '@/client/queries/automation'
import {isRequestContextAbort} from '@/client/requestContext'
import type {CatalogResponse} from '@/helpers/automation'
import {summarizeRule} from '@/helpers/automationSummary'
import type {AvailableTemplate} from '@/helpers/automationTemplates'
import {formatDateSince, formatDateLong} from '@/helpers/time/formatDate'

const props = defineProps<{
	rules: AutomationRuleResponse[]
	catalog: CatalogResponse
	templates: AvailableTemplate[]
}>()

const emit = defineEmits<{
	create: []
	template: [template: AvailableTemplate]
	edit: [rule: AutomationRuleResponse]
	runs: [rule: AutomationRuleResponse]
	duplicate: [rule: AutomationRuleResponse]
}>()

const {t, te} = useI18n({useScope: 'global'})

const toggle = useToggleRuleMutation()
const remove = useDeleteRuleMutation()
const pending = ref<number | null>(null)
const toDelete = ref<AutomationRuleResponse | null>(null)

const summaries = computed(() => new Map(props.rules.map(rule => [rule.id, summarizeRule(rule, props.catalog, t, te)])))

async function setEnabled(rule: AutomationRuleResponse, enabled: boolean) {
	pending.value = rule.id
	try {
		await toggle.mutateAsync({rule, enabled})
	} catch (cause) {
		if (!isRequestContextAbort(cause)) pending.value = null
	}
	pending.value = null
}

async function confirmDelete() {
	const rule = toDelete.value
	if (!rule) return
	try {
		await remove.mutateAsync({projectId: rule.project_id, ruleId: rule.id})
	} catch {
		return
	}
	toDelete.value = null
}

function reasonText(rule: AutomationRuleResponse): string {
	const key = `automation.disabledReason.${rule.disabled_reason}`
	return te(key) ? t(key) : t('automation.disabledReason.unknown', {reason: rule.disabled_reason})
}
</script>

<template>
	<div class="rule-list">
		<div
			v-if="rules.length === 0"
			class="rule-list__empty"
		>
			<Icon
				icon="bolt"
				class="rule-list__empty-icon"
			/>
			<h3>{{ $t('automation.empty.title') }}</h3>
			<p>{{ $t('automation.empty.text') }}</p>
			<XButton
				type="button"
				icon="plus"
				@click="emit('create')"
			>
				{{ $t('automation.new') }}
			</XButton>
		</div>

		<template v-else>
			<div class="rule-list__toolbar">
				<XButton
					type="button"
					icon="plus"
					@click="emit('create')"
				>
					{{ $t('automation.new') }}
				</XButton>
			</div>
			<ul class="rule-list__items">
				<li
					v-for="rule in rules"
					:key="rule.id"
					class="rule-item"
				>
					<div class="rule-item__main">
						<div class="rule-item__title">
							<button
								type="button"
								class="rule-item__name"
								@click="emit('edit', rule)"
							>
								{{ rule.title }}
							</button>
							<AutomationBadge
								v-if="rule.disabled_reason"
								v-tooltip="reasonText(rule)"
								variant="warning"
								tabindex="0"
								role="img"
								:aria-label="reasonText(rule)"
							>
								<Icon icon="exclamation-circle" />
								{{ $t('automation.disabledBadge') }}
							</AutomationBadge>
						</div>
						<p class="rule-item__summary">
							{{ summaries.get(rule.id) }}
						</p>
						<p class="rule-item__last">
							<template v-if="rule.last_run_at">
								<RunStatusBadge :status="rule.last_run_status" />
								<span
									v-tooltip="formatDateLong(rule.last_run_at)"
									class="mis-2"
								>{{ formatDateSince(rule.last_run_at) }}</span>
							</template>
							<span
								v-else
								class="help"
							>{{ $t('automation.neverRun') }}</span>
						</p>
					</div>
					<div class="rule-item__side">
						<label class="rule-item__switch checkbox">
							<input
								type="checkbox"
								:checked="rule.enabled"
								:disabled="pending === rule.id || undefined"
								:aria-label="$t('automation.enableRule', {title: rule.title})"
								@change="setEnabled(rule, ($event.target as HTMLInputElement).checked)"
							>
							{{ rule.enabled ? $t('automation.on') : $t('automation.off') }}
						</label>
						<span class="rule-item__actions">
							<XButton
								type="button"
								:aria-label="$t('automation.edit', {title: rule.title})"
								variant="secondary"
								icon="pen"
								@click="emit('edit', rule)"
							/>
							<XButton
								type="button"
								:aria-label="$t('automation.runsOf', {title: rule.title})"
								variant="secondary"
								icon="history"
								@click="emit('runs', rule)"
							/>
							<XButton
								type="button"
								:aria-label="$t('automation.duplicate', {title: rule.title})"
								variant="secondary"
								icon="copy"
								@click="emit('duplicate', rule)"
							/>
							<XButton
								type="button"
								:aria-label="$t('automation.delete', {title: rule.title})"
								variant="secondary"
								icon="trash-alt"
								danger
								@click="toDelete = rule"
							/>
						</span>
					</div>
				</li>
			</ul>
		</template>

		<section
			v-if="templates.length > 0"
			class="rule-list__templates"
			aria-labelledby="automation-templates"
		>
			<h4 id="automation-templates">
				{{ $t('automation.templates.title') }}
			</h4>
			<ul>
				<li
					v-for="template in templates"
					:key="template.id"
				>
					<button
						type="button"
						class="template-card"
						@click="emit('template', template)"
					>
						<Icon :icon="template.icon" />
						<span class="template-card__text">
							<strong>{{ template.title }}</strong>
							<span>{{ template.description }}</span>
						</span>
					</button>
				</li>
			</ul>
		</section>

		<Modal
			:enabled="toDelete !== null"
			@close="toDelete = null"
			@submit="confirmDelete"
		>
			<template #header>
				<span>{{ $t('automation.deleteConfirm.title') }}</span>
			</template>
			<template #text>
				<p>{{ $t('automation.deleteConfirm.text', {title: toDelete?.title ?? ''}) }}</p>
			</template>
		</Modal>
	</div>
</template>

<style lang="scss" scoped>
.rule-list {
	display: flex;
	flex-direction: column;
	gap: 1rem;

	&__empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: .6rem;
		text-align: center;
		padding-block: 1.5rem .5rem;

		h3 {
			margin: 0;
			font-size: 1.1rem;
		}

		p {
			margin: 0;
			max-inline-size: 40rem;
		}
	}

	&__empty-icon {
		font-size: 2rem;
		color: var(--grey-400);
	}

	&__items,
	&__templates ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: .5rem;
	}

	&__templates h4 {
		font-weight: 700;
		margin-block-end: .5rem;
	}

	&__templates ul {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 16rem), 1fr));
	}
}

.rule-item {
	display: flex;
	flex-wrap: wrap;
	gap: .6rem 1rem;
	justify-content: space-between;
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	padding: .7rem .8rem;

	&__main {
		flex: 1 1 18rem;
		min-inline-size: 0;
	}

	&__title {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		align-items: center;
	}

	&__name {
		font-weight: 700;
		font-size: 1rem;
		background: none;
		border: 0;
		padding: 0;
		color: var(--text);
		cursor: pointer;
		text-align: start;
		overflow-wrap: anywhere;

		&:hover {
			color: var(--primary);
		}
	}

	&__summary {
		margin: .2rem 0;
		color: var(--grey-600);
		font-size: .9rem;
		overflow-wrap: anywhere;
	}

	&__last {
		margin: 0;
		font-size: .85rem;
		display: flex;
		align-items: center;
	}

	&__side {
		display: flex;
		flex-direction: column;
		gap: .5rem;
		align-items: flex-end;

		@media screen and (max-width: $tablet) {
			flex-direction: row;
			align-items: center;
			justify-content: space-between;
			inline-size: 100%;
		}
	}

	&__switch {
		display: flex;
		gap: .4rem;
		align-items: center;
	}

	&__actions {
		display: flex;
		gap: .25rem;
	}
}

.template-card {
	display: flex;
	gap: .6rem;
	align-items: flex-start;
	inline-size: 100%;
	text-align: start;
	padding: .6rem .75rem;
	border: 1px dashed var(--grey-300);
	border-radius: $radius;
	background: transparent;
	color: var(--text);
	cursor: pointer;

	&:hover,
	&:focus-visible {
		border-color: var(--primary);
		background: var(--grey-100);
	}

	&__text {
		display: flex;
		flex-direction: column;
		font-size: .85rem;
		gap: .15rem;

		span {
			color: var(--grey-600);
		}
	}
}
</style>
