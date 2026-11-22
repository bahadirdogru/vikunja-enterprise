<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'

import CustomFieldValueEditor from '@/components/custom-fields/CustomFieldValueEditor.vue'
import TaskPropertyRow from '@/components/tasks/partials/TaskPropertyRow.vue'
import {
	customFieldsQuery,
	customFieldValuesQuery,
	useSetCustomFieldValueMutation,
} from '@/client/queries/customFields'
import {customFieldTypeIcon, isCustomFieldValueEmpty} from '@/helpers/customFields'

const props = defineProps<{
	taskId: number
	projectId: number
	canWrite: boolean
}>()

const {t} = useI18n({useScope: 'global'})

const enabled = computed(() => props.taskId > 0 && props.projectId > 0)
const fieldsQuery = useQuery(computed(() => ({...customFieldsQuery(props.projectId), enabled: enabled.value})))
const valuesQuery = useQuery(computed(() => ({...customFieldValuesQuery(props.taskId), enabled: enabled.value})))
const setValue = useSetCustomFieldValueMutation()

const fields = computed(() => fieldsQuery.data.value ?? [])
const valuesByField = computed(() => new Map(
	(valuesQuery.data.value ?? []).map(entry => [entry.custom_field_id, entry.value]),
))

function save(fieldId: number, value: unknown) {
	setValue.mutate({taskId: props.taskId, fieldId, value})
}
</script>

<template>
	<template v-if="fields.length > 0">
		<TaskPropertyRow
			v-for="field in fields"
			:key="field.id"
			:name="`custom-field-${field.id}`"
			:icon="customFieldTypeIcon(field.type)"
			:label="field.title"
			:is-set="!isCustomFieldValueEmpty(field, valuesByField.get(field.id))"
			:can-write="canWrite"
			:remove-label="t('customFields.value.clear', {title: field.title})"
			@remove="save(field.id, null)"
		>
			<template #label-extra>
				<span
					v-if="field.required && isCustomFieldValueEmpty(field, valuesByField.get(field.id))"
					v-tooltip="t('customFields.value.required')"
					class="required-mark"
					role="img"
					:aria-label="t('customFields.value.required')"
				>*</span>
			</template>
			<CustomFieldValueEditor
				:field="field"
				:model-value="valuesByField.get(field.id) ?? null"
				:project-id="projectId"
				:labelledby="`task-property-custom-field-${field.id}-label`"
				:disabled="!canWrite"
				@update:modelValue="value => save(field.id, value)"
			/>
		</TaskPropertyRow>
	</template>
</template>

<style lang="scss" scoped>
.required-mark {
	margin-inline-start: .125rem;
	color: var(--danger);
	opacity: .7;
	cursor: help;
}
</style>
