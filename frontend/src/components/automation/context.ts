import {inject, type ComputedRef, type InjectionKey} from 'vue'

import type {CustomFieldResponse} from '@/client/queries/customFields'
import type {CatalogResponse, ScriptPosition} from '@/helpers/automation'

export type AutomationStatus = {id: number, title: string, category: string}

// What the generic inputs need to know about the project the rule belongs to.
export type AutomationContext = {
	projectId: number
	catalog: CatalogResponse
	customFields: CustomFieldResponse[]
	statuses: AutomationStatus[]
	// Names of the project secrets, or null when the backend has no secrets list.
	secrets: string[] | null
	// Messages by path (client and server validation).
	errors: ComputedRef<Map<string, string[]>>
	// Position of a script syntax error by path (the server reports line and column).
	lines?: ComputedRef<Map<string, ScriptPosition>>
}

export const automationContextKey: InjectionKey<AutomationContext> = Symbol('automation-context')

export function useAutomationContext(): AutomationContext {
	const context = inject(automationContextKey)
	if (!context) throw new Error('The automation inputs need an AutomationContext provided by the editor.')
	return context
}

// The i18n helpers every input needs.
export function placeholderLabel(placeholder: string, customFields: CustomFieldResponse[]): {value: string, label: string}[] {
	if (!placeholder.includes('<id>')) return [{value: placeholder, label: placeholder}]
	return customFields.map(field => ({value: placeholder.replace('<id>', String(field.id)), label: `${placeholder.replace('<id>', String(field.id))} (${field.title})`}))
}
