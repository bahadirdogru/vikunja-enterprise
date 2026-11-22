import {computed} from 'vue'
import {mount, type MountingOptions} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'
import type {Component} from 'vue'

import en from '@/i18n/lang/en.json'
import catalogFixture from '@/helpers/automationCatalog.fixture.json'
import {normalizeCatalog, type CatalogResponse} from '@/helpers/automation'
import {automationContextKey, type AutomationContext} from '@/components/automation/context'
import type {CustomFieldResponse} from '@/client/queries/customFields'

export const catalog: CatalogResponse = normalizeCatalog(catalogFixture as unknown as Record<string, unknown>)

export const customFields: CustomFieldResponse[] = [
	{id: 1, project_id: 3, title: 'Print run', description: '', type: 'number', required: false, position: 1, options: []},
	{
		id: 2, project_id: 3, title: 'Stage', description: '', type: 'select', required: false, position: 2,
		options: [
			{id: 11, title: 'Editor', color: '', position: 1},
			{id: 12, title: 'Design', color: '', position: 2},
		],
	},
]

export function makeContext(overrides: Partial<Omit<AutomationContext, 'errors'>> & {errors?: Map<string, string[]>} = {}): AutomationContext {
	const errors = overrides.errors ?? new Map<string, string[]>()
	return {
		projectId: 3,
		catalog,
		customFields,
		statuses: [
			{id: 20, title: 'To do', category: 'open'},
			{id: 21, title: 'Done', category: 'done'},
		],
		secrets: null,
		...overrides,
		errors: computed(() => errors),
	}
}

export const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

export function mountAutomation<T extends Component>(component: T, options: MountingOptions<Record<string, unknown>> & {context?: AutomationContext} = {}) {
	const {context, ...rest} = options
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(component as Component, {
		attachTo: document.body,
		...rest,
		global: {
			plugins: [i18n, [VueQueryPlugin, {queryClient}]],
			provide: {[automationContextKey as symbol]: context ?? makeContext()},
			stubs: {Icon: true, RouterLink: {template: '<a><slot /></a>'}, ...(rest.global?.stubs ?? {})},
			directives: {cy: {}, tooltip: {}, focus: {}},
			...(rest.global ? {mocks: rest.global.mocks} : {}),
		},
	})
}
