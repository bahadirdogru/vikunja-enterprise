<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import type {TaskResponse} from '@/client/queries/tasks'
import {useProjects} from '@/composables/useProjects'
import {dateIsValid, formatDisplayDate} from '@/helpers/time/formatDate'
import WorkflowStatusBadge from '@/components/workflow/WorkflowStatusBadge.vue'
import User from '@/components/misc/User.vue'

const props = defineProps<{
	task: TaskResponse
}>()

const {t} = useI18n({useScope: 'global'})
const projectStore = useProjects()

const project = computed(() => projectStore.projects[props.task.project_id]?.title ?? '')
const overdue = computed(() => !props.task.done && dateIsValid(props.task.due_date) && new Date(props.task.due_date!).getTime() < Date.now())
</script>

<template>
	<li class="task-row">
		<RouterLink
			:to="{name: 'task.detail', params: {id: task.id}}"
			class="task-row__title"
			:class="{'is-done': task.done}"
		>
			{{ task.title }}
		</RouterLink>
		<span class="task-row__meta">
			<WorkflowStatusBadge
				v-if="task.status?.id"
				:status="{title: task.status.title ?? '', color: task.status.color ?? '', category: task.status.category ?? 'open'}"
			/>
			<span
				v-if="project"
				class="task-row__project"
			>{{ project }}</span>
			<span
				v-if="dateIsValid(task.due_date)"
				class="task-row__due"
				:class="{'is-overdue': overdue}"
			>
				<span class="visually-hidden">{{ t('dashboards.taskList.due') }}</span>
				{{ formatDisplayDate(task.due_date) }}
			</span>
			<span
				v-if="task.assignees.length"
				class="task-row__assignees"
			>
				<User
					v-for="assignee in task.assignees.slice(0, 3)"
					:key="assignee.id"
					:user="assignee"
					:avatar-size="20"
					:show-username="false"
				/>
			</span>
		</span>
	</li>
</template>

<style lang="scss" scoped>
.task-row {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: space-between;
	gap: .15rem .75rem;
	padding: .35rem 0;
	border-block-end: 1px solid var(--grey-100);
	font-size: .875rem;
}

.task-row__title {
	flex: 1 1 10rem;
	min-inline-size: 0;
	overflow-wrap: anywhere;
	color: var(--text);

	&.is-done {
		text-decoration: line-through;
		color: var(--text-light);
	}

	&:hover {
		color: var(--primary);
	}
}

.task-row__meta {
	display: inline-flex;
	flex-wrap: wrap;
	align-items: center;
	gap: .5rem;
	color: var(--text-light);
	font-size: .8rem;
}

.task-row__due.is-overdue {
	color: var(--danger);
	font-weight: 600;
}

.task-row__assignees {
	display: inline-flex;
}

.visually-hidden {
	position: absolute;
	inline-size: 1px;
	block-size: 1px;
	overflow: hidden;
	clip-path: inset(50%);
}
</style>
