<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {useQuery} from '@tanstack/vue-query'

import XButton from '@/components/input/Button.vue'
import ErrorMessage from '@/components/misc/Error.vue'
import Loading from '@/components/misc/Loading.vue'
import {formSubmissionsQuery, type FormResponse} from '@/client/queries/forms'
import {clampPage} from '@/client/queries/pagination'
import {formatDateLong, formatDateSince} from '@/helpers/time/formatDate'

const props = defineProps<{
	projectId: number
	form: FormResponse
}>()

const emit = defineEmits<{
	back: []
}>()

const page = ref(1)
const result = useQuery(computed(() => formSubmissionsQuery(props.projectId, props.form.id, page.value)))
const data = computed(() => result.data.value)
const totalPages = computed(() => Math.max(data.value?.total_pages ?? 1, 1))

watch(() => result.data.value, () => {
	page.value = clampPage(page.value, {data: result.data.value, isPlaceholderData: result.isPlaceholderData.value})
})

function submitter(row: {submitter_name: string, submitter_email: string, submitter_id: number | null}): string {
	return [row.submitter_name, row.submitter_email].filter(Boolean).join(' · ')
}
</script>

<template>
	<div class="fsub">
		<div class="fsub__head">
			<XButton
				type="button"
				variant="tertiary"
				icon="arrow-left"
				@click="emit('back')"
			>
				{{ $t('forms.submissions.back') }}
			</XButton>
			<h3>{{ $t('forms.submissions.title', {title: form.title}) }}</h3>
		</div>

		<ErrorMessage v-if="result.error.value" />
		<Loading v-else-if="result.isPending.value" />
		<p
			v-else-if="!data || data.items.length === 0"
			class="fsub__empty"
		>
			{{ $t('forms.submissions.empty') }}
		</p>
		<template v-else>
			<div class="fsub__scroll">
				<table class="table fsub__table">
					<caption class="is-sr-only">
						{{ $t('forms.submissions.title', {title: form.title}) }}
					</caption>
					<thead>
						<tr>
							<th scope="col">
								{{ $t('forms.submissions.date') }}
							</th>
							<th scope="col">
								{{ $t('forms.submissions.submitter') }}
							</th>
							<th scope="col">
								{{ $t('forms.submissions.task') }}
							</th>
						</tr>
					</thead>
					<tbody>
						<tr
							v-for="row in data.items"
							:key="row.id"
						>
							<td>
								<time
									:datetime="row.created"
									:title="formatDateLong(row.created)"
								>{{ formatDateSince(row.created) }}</time>
							</td>
							<td>
								<template v-if="submitter(row)">
									{{ submitter(row) }}
								</template>
								<span
									v-else
									class="help"
								>{{ $t('forms.submissions.anonymous') }}</span>
								<span
									v-if="row.submitter_id"
									class="help"
								> ({{ $t('forms.submissions.signedIn') }})</span>
							</td>
							<td>
								<RouterLink :to="{name: 'task.detail', params: {id: row.task_id}}">
									#{{ row.task_id }}
								</RouterLink>
							</td>
						</tr>
					</tbody>
				</table>
			</div>
			<div
				v-if="totalPages > 1"
				class="fsub__pager"
			>
				<XButton
					type="button"
					variant="secondary"
					:disabled="page <= 1"
					@click="page--"
				>
					{{ $t('misc.previous') }}
				</XButton>
				<span>{{ page }} / {{ totalPages }}</span>
				<XButton
					type="button"
					variant="secondary"
					:disabled="page >= totalPages"
					@click="page++"
				>
					{{ $t('misc.next') }}
				</XButton>
			</div>
		</template>
	</div>
</template>

<style lang="scss" scoped>
.fsub {
	display: flex;
	flex-direction: column;
	gap: 1rem;

	&__head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .5rem 1rem;

		h3 {
			margin: 0;
			font-size: 1.1rem;
			overflow-wrap: anywhere;
		}
	}

	&__empty {
		margin: 0;
		padding: 2rem 0;
		text-align: center;
		color: var(--grey-600);
	}

	&__scroll {
		overflow-x: auto;
	}

	&__table {
		inline-size: 100%;
	}

	&__pager {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 1rem;
	}
}
</style>
