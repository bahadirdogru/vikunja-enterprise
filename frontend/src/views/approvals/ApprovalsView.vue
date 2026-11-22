<script setup lang="ts">
import {computed, ref} from 'vue'
import {useRoute, useRouter} from 'vue-router'
import {useI18n} from 'vue-i18n'
import {useQuery, useQueryClient} from '@tanstack/vue-query'

import BaseButton from '@/components/base/BaseButton.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import Loading from '@/components/misc/Loading.vue'
import Pagination from '@/components/misc/Pagination.vue'
import SwitchView from '@/components/misc/SwitchView.vue'
import ApprovalDecisionDialog from '@/components/approvals/ApprovalDecisionDialog.vue'
import ApprovalRow from '@/components/approvals/ApprovalRow.vue'
import {approvalCountQuery, approvalKeys, approvalsListQuery} from '@/client/queries/approvals'
import {useProjects} from '@/composables/useProjects'
import {useTitle} from '@/composables/useTitle'
import {
	type ApprovalDecisionValue,
	type ApprovalResponse,
	type ApprovalStatus,
} from '@/helpers/approvals'
import {useAuthStore} from '@/stores/auth'

const TABS = ['waiting', 'requested', 'history'] as const
type Tab = typeof TABS[number]
const OUTCOMES = ['approved', 'rejected', 'cancelled'] as const

const {t} = useI18n({useScope: 'global'})
const route = useRoute()
const router = useRouter()
const client = useQueryClient()
const authStore = useAuthStore()
const {projectsArray} = useProjects()
useTitle(() => t('approvals.title'))

const tab = computed<Tab>(() => (TABS as readonly string[]).includes(String(route.query.tab))
	? route.query.tab as Tab
	: 'waiting')
const page = computed(() => Math.max(1, Number(route.query.page) || 1))
const projectId = computed(() => Number(route.query.project) || 0)
const outcome = computed<ApprovalStatus>(() => (OUTCOMES as readonly string[]).includes(String(route.query.outcome))
	? route.query.outcome as ApprovalStatus
	: 'approved')

const params = computed(() => ({
	...(tab.value === 'waiting' ? {status: 'pending' as const, role: 'approver' as const} : {}),
	...(tab.value === 'requested' ? {status: 'pending' as const, role: 'requester' as const} : {}),
	// A history shows both roles; the endpoint filters one outcome at a time.
	...(tab.value === 'history' ? {status: outcome.value} : {}),
	...(projectId.value > 0 ? {projectId: projectId.value} : {}),
	page: page.value,
}))

const list = useQuery(computed(() => approvalsListQuery(params.value)))
const count = useQuery(approvalCountQuery())
const items = computed(() => list.data.value?.items ?? [])
const userId = computed(() => authStore.info?.id ?? 0)

const projectOptions = computed(() => [
	{value: 0, label: t('approvals.page.allProjects')},
	...projectsArray
		.filter(project => project.id > 0)
		.map(project => ({value: project.id, label: project.title})),
])
const outcomeOptions = computed(() => OUTCOMES.map(value => ({value, label: t(`approvals.outcome.${value}`)})))

function go(query: Record<string, string | undefined>) {
	router.replace({name: 'approvals.index', query: {...route.query, page: undefined, ...query}})
}

const tabLabels = computed(() => ({
	waiting: t('approvals.page.tabs.waiting'),
	requested: t('approvals.page.tabs.requested'),
	history: t('approvals.page.tabs.history'),
}))

const decisionFor = ref<{request: ApprovalResponse, decision: ApprovalDecisionValue} | null>(null)

function refresh() {
	return client.invalidateQueries({queryKey: approvalKeys.all})
}
</script>

<template>
	<div class="content is-max-width-desktop approvals-page">
		<h1>{{ $t('approvals.title') }}</h1>

		<div class="approvals-page__head">
			<SwitchView
				is="nav"
				:aria-label="$t('approvals.page.tabsLabel')"
			>
				<BaseButton
					v-for="entry in TABS"
					:key="entry"
					class="switch-view-button"
					:class="{'is-active': tab === entry}"
					:aria-current="tab === entry ? 'page' : undefined"
					@click="go({tab: entry === 'waiting' ? undefined : entry})"
				>
					{{ tabLabels[entry] }}
					<span
						v-if="entry === 'waiting' && (count.data.value ?? 0) > 0"
						class="approvals-page__count"
					>{{ count.data.value }}</span>
				</BaseButton>
			</SwitchView>
			<div class="approvals-page__filters">
				<div
					v-if="tab === 'history'"
					class="field"
				>
					<label
						class="label is-small"
						for="approvals-outcome"
					>{{ $t('approvals.page.outcome') }}</label>
					<FormSelect
						id="approvals-outcome"
						:model-value="outcome"
						:options="outcomeOptions"
						@update:modelValue="value => go({outcome: String(value)})"
					/>
				</div>
				<div class="field">
					<label
						class="label is-small"
						for="approvals-project"
					>{{ $t('approvals.page.project') }}</label>
					<FormSelect
						id="approvals-project"
						:model-value="projectId"
						:options="projectOptions"
						@update:modelValue="value => go({project: Number(value) > 0 ? String(value) : undefined})"
					/>
				</div>
			</div>
		</div>

		<Loading
			v-if="list.isPending.value"
			variant="small"
		/>
		<p
			v-else-if="list.isError.value"
			class="help is-danger"
			role="alert"
		>
			{{ $t('approvals.page.loadError') }}
		</p>
		<div
			v-else-if="items.length === 0"
			class="empty-state"
		>
			<Icon
				icon="user-check"
				class="empty-state__icon"
			/>
			<p class="empty-state__text">
				{{ $t(`approvals.page.empty.${tab}`) }}
			</p>
		</div>
		<ul
			v-else
			class="approvals-page__list"
		>
			<ApprovalRow
				v-for="request in items"
				:key="request.id"
				:request="request"
				:user-id="userId"
				:actions="tab === 'waiting'"
				@decide="(r, d) => decisionFor = {request: r, decision: d}"
			/>
		</ul>

		<Pagination
			v-if="(list.data.value?.total_pages ?? 0) > 1"
			:total-pages="list.data.value?.total_pages ?? 1"
			:current-page="page"
		/>

		<ApprovalDecisionDialog
			v-if="decisionFor"
			:request="decisionFor.request"
			:decision="decisionFor.decision"
			@close="decisionFor = null"
			@decided="refresh"
		/>
	</div>
</template>

<style lang="scss" scoped>
.approvals-page {
	&__head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 1rem;
		margin-block-end: 1rem;
	}

	&__filters {
		display: flex;
		flex-wrap: wrap;
		gap: .75rem;

		.field {
			margin: 0;
		}
	}

	&__count {
		margin-inline-start: .375rem;
		padding: 0 .4rem;
		background: var(--primary);
		border-radius: 999px;
		color: var(--white);
		font-size: .75rem;
	}

	&__list {
		display: flex;
		flex-direction: column;
		gap: .5rem;
		margin: 0 0 1rem;
		padding: 0;
		list-style: none;
	}
}
</style>
