import type {IconProp} from '@fortawesome/fontawesome-svg-core'

import {
	actionDef,
	emptyDraft,
	fieldDef,
	triggerDef,
	type ActionDraft,
	type CatalogResponse,
	type ConditionNode,
	type RuleDraft,
	type Translate,
} from '@/helpers/automation'

export type TemplateContext = {
	catalog: CatalogResponse
	// Statuses of the project workflow; empty without a workflow.
	statuses: {id: number, title: string, category: string}[]
	customFields: {id: number, title: string, type: string}[]
}

export type RuleTemplate = {
	id: string
	titleKey: string
	descriptionKey: string
	icon: IconProp
	// Returns null when the project or the backend lacks something the template needs.
	build: (ctx: TemplateContext) => Omit<RuleDraft, 'title' | 'description' | 'enabled'> | null
}

function usable(ctx: TemplateContext, trigger: string, actions: ActionDraft[], condition?: ConditionNode): boolean {
	const t = triggerDef(ctx.catalog, trigger)
	if (!t) return false
	for (const action of actions) {
		const def = actionDef(ctx.catalog, action.type)
		if (!def) return false
		// Every param the template sets has to exist in the catalog.
		if (Object.keys(action.params).some(name => !def.params.some(p => p.name === name))) return false
	}
	if (condition?.field && !fieldDef(ctx.catalog, condition.field)) return false
	return true
}

export const RULE_TEMPLATES: RuleTemplate[] = [
	{
		id: 'comment-on-completed',
		titleKey: 'automation.templates.commentOnCompleted.title',
		descriptionKey: 'automation.templates.commentOnCompleted.description',
		icon: 'check',
		build: ctx => {
			const actions: ActionDraft[] = [{type: 'task.add_comment', params: {text: 'Done: {{task.title}} ({{event.doer.name}})'}}]
			if (!usable(ctx, 'task.completed', actions)) return null
			return {trigger: {type: 'task.completed', options: {}}, conditions: null, actions}
		},
	},
	{
		id: 'field-on-done',
		titleKey: 'automation.templates.fieldOnDone.title',
		descriptionKey: 'automation.templates.fieldOnDone.description',
		icon: 'sliders',
		build: ctx => {
			const done = ctx.statuses.find(status => status.category === 'done')
			const field = ctx.customFields.find(f => f.type === 'checkbox') ?? ctx.customFields[0]
			if (!done || !field) return null
			const params: Record<string, unknown> = {field_id: field.id}
			if (field.type === 'checkbox') params.value = true
			const actions: ActionDraft[] = [{type: 'task.set_custom_field', params}]
			if (!usable(ctx, 'task.status_changed', actions)) return null
			return {trigger: {type: 'task.status_changed', options: {to_status_id: done.id}}, conditions: null, actions}
		},
	},
	{
		id: 'assign-creator',
		titleKey: 'automation.templates.assignCreator.title',
		descriptionKey: 'automation.templates.assignCreator.description',
		icon: 'user',
		build: ctx => {
			const actions: ActionDraft[] = [{type: 'task.assign', params: {user: 'task_creator'}}]
			if (!usable(ctx, 'task.created', actions)) return null
			return {trigger: {type: 'task.created', options: {}}, conditions: null, actions}
		},
	},
	{
		id: 'due-on-assigned',
		titleKey: 'automation.templates.dueOnAssigned.title',
		descriptionKey: 'automation.templates.dueOnAssigned.description',
		icon: 'calendar',
		build: ctx => {
			const actions: ActionDraft[] = [{type: 'task.set_field', params: {field: 'due_date', value: '+3d'}}]
			if (!usable(ctx, 'task.assignee_added', actions)) return null
			return {trigger: {type: 'task.assignee_added', options: {}}, conditions: null, actions}
		},
	},
	{
		id: 'due-on-created',
		titleKey: 'automation.templates.dueOnCreated.title',
		descriptionKey: 'automation.templates.dueOnCreated.description',
		icon: 'calendar',
		build: ctx => {
			const condition: ConditionNode = {field: 'due_date', op: 'is_empty'}
			const actions: ActionDraft[] = [{type: 'task.set_field', params: {field: 'due_date', value: '+7d'}}]
			if (!usable(ctx, 'task.created', actions, condition)) return null
			return {trigger: {type: 'task.created', options: {}}, conditions: {all: [condition]}, actions}
		},
	},
]

export type AvailableTemplate = {id: string, icon: IconProp, title: string, description: string, draft: RuleDraft}

export function availableTemplates(ctx: TemplateContext, t: Translate): AvailableTemplate[] {
	const out: AvailableTemplate[] = []
	for (const template of RULE_TEMPLATES) {
		const built = template.build(ctx)
		if (!built) continue
		const title = t(template.titleKey)
		out.push({
			id: template.id,
			icon: template.icon,
			title,
			description: t(template.descriptionKey),
			draft: {...emptyDraft(), ...built, title, description: '', enabled: true},
		})
	}
	return out
}
