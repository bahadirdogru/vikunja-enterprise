import {mount, type MountingOptions} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'
import type {Component} from 'vue'

import XButton from '@/components/input/Button.vue'
import en from '@/i18n/lang/en.json'
import optionsFixture from '@/helpers/forms.options.fixture.json'
import {normalizeOptions, type FormOptionsResponse} from '@/helpers/forms'
import type {FormResponse} from '@/client/queries/forms'

export const options: FormOptionsResponse = normalizeOptions(optionsFixture as unknown as Parameters<typeof normalizeOptions>[0])

export const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

export const statuses = [
	{id: 20, title: 'To do', category: 'open'},
	{id: 21, title: 'In progress', category: 'in_progress'},
]

export const labels = [
	{id: 1, title: 'Urgent', color: 'e8445a'},
	{id: 2, title: 'Review', color: '3b82f6'},
]

export function storedForm(over: Partial<FormResponse> = {}): FormResponse {
	return {
		id: 5, project_id: 3, title: 'Cover request', description: '', token: 'tok-1', access: 'members', enabled: true,
		owner_id: 1, disabled_reason: '', submission_count: 2,
		fields: [
			{target: 'title', label: 'What do you need?', help: '', required: true, hidden: false, default: null},
			{target: 'description', label: 'Details', help: '', required: false, hidden: false, default: null},
		],
		settings: {
			initial_status_id: 0, default_assignee_ids: [], success_message: '', redirect_url: '', submit_another: false,
			close_date: null, max_submissions: 0, notify_user_ids: [], collect_name: false, name_required: false,
			collect_email: false, email_required: false,
		},
		created: '', updated: '',
		...over,
	} as FormResponse
}

export function mountForms<T extends Component>(component: T, mountOptions: MountingOptions<Record<string, unknown>> = {}) {
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	const wrapper = mount(component as Component, {
		attachTo: document.body,
		...mountOptions,
		global: {
			plugins: [i18n, [VueQueryPlugin, {queryClient}]],
			components: {XButton},
			stubs: {Icon: true, RouterLink: {template: '<a><slot /></a>'}, ...(mountOptions.global?.stubs ?? {})},
			directives: {cy: {}, tooltip: {}, focus: {}},
			...(mountOptions.global?.mocks ? {mocks: mountOptions.global.mocks} : {}),
		},
	})
	return {wrapper, queryClient}
}
