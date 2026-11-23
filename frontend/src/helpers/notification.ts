import type {DatabaseNotification, User, Task, Project, Team} from '@/client/generated'
import type {RouteLocationRaw} from 'vue-router'
import {getDisplayName} from '@/helpers/user'
import {getTaskIdentifier} from '@/helpers/task'

export const NOTIFICATION_NAMES = {
	'TASK_COMMENT': 'task.comment',
	'TASK_ASSIGNED': 'task.assigned',
	'TASK_DELETED': 'task.deleted',
	'TASK_CREATED': 'task.created',
	'TASK_REMINDER': 'task.reminder',
	'PROJECT_CREATED': 'project.created',
	'TEAM_MEMBER_ADDED': 'team.member.added',
	'TASK_MENTIONED': 'task.mentioned',
	'APPROVAL_REQUESTED': 'approval.requested',
	'APPROVAL_RESOLVED': 'approval.resolved',
	'APPROVAL_REMINDER': 'approval.reminder',
	'FORM_SUBMITTED': 'form.submitted',
	'AUTOMATION_NOTIFY': 'automation.notify',
} as const

function record(value: unknown): Record<string, unknown> {
	return typeof value === 'object' && value !== null ? value as Record<string, unknown> : {}
}

export function notificationDoer(n: DatabaseNotification): User | undefined {
	// The owner of a form is no actor of a submission.
	if (n.name === NOTIFICATION_NAMES.FORM_SUBMITTED) return undefined
	// A rule sent it; the person whose change started the rule is not the sender.
	if (n.name === NOTIFICATION_NAMES.AUTOMATION_NOTIFY) return undefined
	const doer = record(record(n.notification).doer)
	return typeof doer.id === 'number' ? doer as User : undefined
}

export function notificationText(n: DatabaseNotification, user: Pick<User, 'id'> | null = null): string {
	const payload = record(n.notification)
	const task = record(payload.task) as Task
	const project = record(payload.project) as Project
	const assignee = record(payload.assignee) as User
	const member = record(payload.member) as User
	const team = record(payload.team) as Team
	const doer = record(payload.doer) as User
	const approval = record(payload.approval)
	const approvalTitle = typeof approval.title === 'string' ? approval.title : ''
	const formTitle = typeof payload.form_title === 'string' ? payload.form_title : ''
	const submitter = [payload.submitter_name, payload.submitter_email].find(v => typeof v === 'string' && v !== '')
	const message = typeof payload.message === 'string' ? payload.message.trim() : ''
	const ruleTitle = typeof payload.rule_title === 'string' ? payload.rule_title : ''
	let who: string
	switch (n.name) {
		case NOTIFICATION_NAMES.TASK_COMMENT:
			return `commented on ${getTaskIdentifier(task)}`
		case NOTIFICATION_NAMES.TASK_ASSIGNED:
			who = `${getDisplayName(assignee)}`

			if (user !== null && user.id === assignee.id) {
				who = 'you'
			}

			return `assigned ${who} to ${getTaskIdentifier(task)}`
		case NOTIFICATION_NAMES.TASK_DELETED:
			return `deleted ${getTaskIdentifier(task)}`
		case NOTIFICATION_NAMES.TASK_CREATED:
			return `created ${getTaskIdentifier(task)}`
		case NOTIFICATION_NAMES.PROJECT_CREATED:
			return `created ${project.title}`
		case NOTIFICATION_NAMES.TEAM_MEMBER_ADDED:
			who = `${getDisplayName(member)}`

			if (user !== null && user.id === member.id) {
				who = 'you'
			}

			return `added ${who} to the ${team.name} team`
		case NOTIFICATION_NAMES.TASK_REMINDER:
			return `Reminder for ${getTaskIdentifier(task)} ${task.title} (${project.title})`
		case NOTIFICATION_NAMES.TASK_MENTIONED:
			return `${getDisplayName(doer)} mentioned you on ${getTaskIdentifier(task)}`
		case NOTIFICATION_NAMES.FORM_SUBMITTED:
			// The doer is the form owner, who did nothing: the text is about the submitter.
			return typeof submitter === 'string'
				? `${submitter} submitted the form “${formTitle}”, creating ${getTaskIdentifier(task)}`
				: `The form “${formTitle}” was submitted, creating ${getTaskIdentifier(task)}`
		case NOTIFICATION_NAMES.AUTOMATION_NOTIFY:
			// The text is the message of the rule, already filled in by the server.
			return message !== '' ? message : `Automation “${ruleTitle}” sent a notification`
		case NOTIFICATION_NAMES.APPROVAL_REQUESTED:
			return `asked for your approval: “${approvalTitle}” on ${getTaskIdentifier(task)}`
		case NOTIFICATION_NAMES.APPROVAL_REMINDER:
			return `asked for your approval (overdue): “${approvalTitle}” on ${getTaskIdentifier(task)}`
		case NOTIFICATION_NAMES.APPROVAL_RESOLVED: {
			const outcome = ['approved', 'rejected', 'cancelled'].includes(String(approval.status))
				? String(approval.status)
				: 'resolved'
			// Without a doer the system ended the request (the task moved, a status was removed, ...).
			return typeof doer.id === 'number'
				? `${outcome} the approval “${approvalTitle}” on ${getTaskIdentifier(task)}`
				: `The approval “${approvalTitle}” on ${getTaskIdentifier(task)} was ${outcome}`
		}
	}

	return ''
}

export function notificationRoute(n: DatabaseNotification): RouteLocationRaw | null {
	const payload = record(n.notification)
	const taskNames = [
		NOTIFICATION_NAMES.TASK_COMMENT,
		NOTIFICATION_NAMES.TASK_ASSIGNED,
		NOTIFICATION_NAMES.TASK_REMINDER,
		NOTIFICATION_NAMES.TASK_MENTIONED,
		NOTIFICATION_NAMES.TASK_CREATED,
		NOTIFICATION_NAMES.FORM_SUBMITTED,
		NOTIFICATION_NAMES.APPROVAL_REQUESTED,
		NOTIFICATION_NAMES.APPROVAL_RESOLVED,
		NOTIFICATION_NAMES.APPROVAL_REMINDER,
	] as string[]
	if (taskNames.includes(n.name ?? '')) {
		const id = record(payload.task).id
		if (typeof id === 'number' && id > 0) return {name: 'task.detail', params: {id}}
		// A request whose task is gone still has a place to be read.
		return n.name?.startsWith('approval.') ? {name: 'approvals.index'} : null
	}
	if (n.name === NOTIFICATION_NAMES.AUTOMATION_NOTIFY) {
		const taskId = record(payload.task).id
		if (typeof taskId === 'number' && taskId > 0) return {name: 'task.detail', params: {id: taskId}}
		// A rule that runs without a task (a schedule of the project, a webhook) notifies about the project.
		const projectId = record(payload.project).id
		return typeof projectId === 'number' && projectId > 0 ? {name: 'task.index', params: {projectId}} : null
	}
	if (n.name === NOTIFICATION_NAMES.PROJECT_CREATED) {
		const projectId = record(payload.project).id
		return typeof projectId === 'number' && projectId > 0 ? {name: 'task.index', params: {projectId}} : null
	}
	if (n.name === NOTIFICATION_NAMES.TEAM_MEMBER_ADDED) {
		const id = record(payload.team).id
		return typeof id === 'number' && id > 0 ? {name: 'teams.edit', params: {id}} : null
	}
	return null
}
