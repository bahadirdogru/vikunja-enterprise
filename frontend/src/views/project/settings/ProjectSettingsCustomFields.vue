<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {useTitle} from '@vueuse/core'
import {useQuery} from '@tanstack/vue-query'

import CreateEdit from '@/components/misc/CreateEdit.vue'
import CustomFieldForm from '@/components/custom-fields/CustomFieldForm.vue'
import ErrorMessage from '@/components/misc/Error.vue'
import Loading from '@/components/misc/Loading.vue'
import Message from '@/components/misc/Message.vue'
import XButton from '@/components/input/Button.vue'
import {
	customFieldsQuery,
	useCreateCustomFieldMutation,
	useDeleteCustomFieldMutation,
	useUpdateCustomFieldMutation,
	type CustomFieldResponse,
} from '@/client/queries/customFields'
import {PERMISSIONS} from '@/constants/permissions'
import {useProject} from '@/composables/useProject'
import {customFieldTypeIcon, draftToWritable, type CustomFieldDraft} from '@/helpers/customFields'
import {getErrorText} from '@/message'

const props = defineProps<{
	projectId: number
}>()

const {t} = useI18n({useScope: 'global'})
useTitle(() => t('customFields.title'))

// useProject snapshots its project, which is enough for the permission check.
const {project, error: projectError} = useProject(() => props.projectId)
const isAdmin = computed(() => project.value?.max_permission === PERMISSIONS.ADMIN)

const fieldsQuery = useQuery(computed(() => ({
	...customFieldsQuery(props.projectId),
	enabled: props.projectId > 0,
})))
const fields = computed(() => fieldsQuery.data.value ?? [])
const loadError = computed(() => projectError.value ?? fieldsQuery.error.value)

const create = useCreateCustomFieldMutation()
const update = useUpdateCustomFieldMutation()
const remove = useDeleteCustomFieldMutation()

// null: list, 'new': create form, a field: edit form
const editing = ref<CustomFieldResponse | 'new' | null>(null)
const apiError = ref<string | null>(null)
const fieldToDelete = ref<CustomFieldResponse | null>(null)

const isSaving = computed(() => create.isPending.value || update.isPending.value)

watch(() => props.projectId, () => {
	editing.value = null
	apiError.value = null
	fieldToDelete.value = null
})

function openForm(target: CustomFieldResponse | 'new') {
	apiError.value = null
	editing.value = target
}

async function save(draft: CustomFieldDraft) {
	const projectId = props.projectId
	const target = editing.value
	apiError.value = null
	try {
		if (target !== null && target !== 'new') {
			await update.mutateAsync({
				projectId,
				fieldId: target.id,
				field: draftToWritable(draft, target.position),
			})
		} else {
			await create.mutateAsync({projectId, field: draftToWritable(draft)})
		}
		if (props.projectId === projectId && editing.value === target) {
			editing.value = null
		}
	} catch (e) {
		if (props.projectId === projectId) {
			apiError.value = getErrorText(e)
		}
	}
}

async function confirmDelete() {
	const target = fieldToDelete.value
	if (target === null) {
		return
	}
	const projectId = props.projectId
	try {
		await remove.mutateAsync({projectId, fieldId: target.id})
		if (fieldToDelete.value === target) {
			fieldToDelete.value = null
		}
	} catch {
		// The mutation toasts the failure; the dialog stays open for a retry.
	}
}
</script>

<template>
	<CreateEdit
		:title="$t('customFields.title')"
		:has-primary-action="false"
		:wide="true"
	>
		<ErrorMessage v-if="loadError" />
		<Loading v-else-if="!project" />
		<Message v-else-if="!isAdmin">
			{{ $t('customFields.onlyAdminsCanEdit') }}
		</Message>
		<template v-else>
			<CustomFieldForm
				v-if="editing !== null"
				:key="editing === 'new' ? 'new' : editing.id"
				:field="editing === 'new' ? null : editing"
				:saving="isSaving"
				:api-error="apiError"
				@submit="save"
				@cancel="editing = null"
			/>

			<template v-else>
				<div
					v-if="fields.length === 0 && !fieldsQuery.isPending.value"
					class="empty-state"
				>
					<Icon
						icon="sliders"
						class="empty-state__icon"
					/>
					<h3 class="empty-state__title">
						{{ $t('customFields.empty.title') }}
					</h3>
					<p class="empty-state__text">
						{{ $t('customFields.empty.text') }}
					</p>
					<XButton
						icon="plus"
						@click="openForm('new')"
					>
						{{ $t('customFields.add') }}
					</XButton>
				</div>

				<template v-else>
					<div class="is-flex is-justify-content-space-between is-align-items-center mbe-4">
						<p class="field-hint">
							{{ $t('customFields.intro') }}
						</p>
						<XButton
							icon="plus"
							@click="openForm('new')"
						>
							{{ $t('customFields.add') }}
						</XButton>
					</div>

					<div class="has-horizontal-overflow">
						<table class="table has-actions is-striped is-hoverable is-fullwidth">
							<thead>
								<tr>
									<th>{{ $t('customFields.columns.title') }}</th>
									<th>{{ $t('customFields.columns.type') }}</th>
									<th class="has-text-end">
										{{ $t('customFields.columns.actions') }}
									</th>
								</tr>
							</thead>
							<tbody>
								<tr
									v-for="field in fields"
									:key="field.id"
								>
									<td>
										<span class="field-title">
											<Icon
												:icon="customFieldTypeIcon(field.type)"
												class="field-title__icon"
											/>
											{{ field.title }}
											<span
												v-if="field.required"
												class="tag is-danger is-light"
											>
												{{ $t('customFields.requiredBadge') }}
											</span>
										</span>
									</td>
									<td>{{ $t(`customFields.types.${field.type}`) }}</td>
									<td class="has-text-end actions">
										<XButton
											icon="pen"
											class="mie-2"
											:aria-label="$t('customFields.editField', {title: field.title})"
											@click="openForm(field)"
										/>
										<XButton
											class="is-danger"
											icon="trash-alt"
											:aria-label="$t('customFields.deleteField', {title: field.title})"
											@click="fieldToDelete = field"
										/>
									</td>
								</tr>
							</tbody>
						</table>
					</div>
				</template>
			</template>
		</template>
	</CreateEdit>

	<Modal
		:enabled="fieldToDelete !== null"
		@close="fieldToDelete = null"
		@submit="confirmDelete"
	>
		<template #header>
			<span>{{ $t('customFields.deleteTitle', {title: fieldToDelete?.title ?? ''}) }}</span>
		</template>

		<template #text>
			<p>{{ $t('customFields.deleteText') }}</p>
		</template>
	</Modal>
</template>

<style lang="scss" scoped>
.empty-state {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: .75rem;
	padding-block: 2rem;
	text-align: center;

	&__icon {
		font-size: 2rem;
		color: var(--grey-400);
	}

	&__title {
		margin: 0;
		font-size: 1.125rem;
	}

	&__text {
		max-inline-size: 36rem;
		margin: 0;
		color: var(--grey-600);
	}
}

.field-hint {
	margin: 0;
	color: var(--grey-600);
}

.field-title {
	display: inline-flex;
	align-items: center;
	gap: .5rem;

	&__icon {
		inline-size: 1rem;
		color: var(--grey-500);
	}
}

.actions {
	display: flex;
	align-items: center;
	justify-content: flex-end;
}
</style>
