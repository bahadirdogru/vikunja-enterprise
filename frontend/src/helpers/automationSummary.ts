import {
	actionDef,
	catalogLabel,
	enumLabel,
	triggerDef,
	type CatalogResponse,
	type ConditionNode,
	type RuleDraft,
	type Translate,
	pruneConditions,
	childrenOf,
	groupKind,
} from '@/helpers/automation'

type Te = (key: string) => boolean

function leafCount(node: ConditionNode | null): number {
	if (!node) return 0
	if (groupKind(node)) return childrenOf(node).reduce((sum, child) => sum + leafCount(child), 0)
	return 1
}

// "When "Task created" -> Set a field (Priority), Add a comment". Built only from catalog titles.
export function summarizeRule(draft: Pick<RuleDraft, 'trigger' | 'conditions' | 'actions'>, catalog: CatalogResponse, t: Translate, te: Te): string {
	const trigger = triggerDef(catalog, draft.trigger.type)
	const triggerTitle = draft.trigger.type
		? catalogLabel(t, te, trigger?.title_key ?? '', draft.trigger.type)
		: t('automation.summary.noTrigger')
	const actions = draft.actions.map(action => {
		const def = actionDef(catalog, action.type)
		const title = catalogLabel(t, te, def?.title_key ?? '', action.type)
		const detail = def?.params.find(p => p.kind === 'enum' && !p.multiple && typeof action.params[p.name] === 'string')
		return detail ? `${title} (${enumLabel(t, te, String(action.params[detail.name]))})` : title
	})
	const conditions = pruneConditions(draft.conditions)
	const parts = [t('automation.summary.line', {
		trigger: triggerTitle,
		actions: actions.length > 0 ? actions.join(', ') : t('automation.summary.noActions'),
	})]
	if (conditions) parts.push(t('automation.summary.conditions', {count: leafCount(conditions)}))
	return parts.join(' · ')
}
