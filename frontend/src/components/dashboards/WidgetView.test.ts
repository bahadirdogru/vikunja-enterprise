import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'
import {createI18n} from 'vue-i18n'
import {readFileSync} from 'node:fs'
import {resolve as resolvePath} from 'node:path'

const sdk = vi.hoisted(() => ({
	reportsTasks: vi.fn(),
	reportsTasksOptions: vi.fn(),
	reportsWorkload: vi.fn(),
	reportsStatusTime: vi.fn(),
	reportsTimeTracking: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import WidgetView from './WidgetView.vue'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

// Read from disk: the i18n plugin precompiles the messages of a JSON import.
const load = (name: string) => JSON.parse(readFileSync(resolvePath(process.cwd(), 'src/i18n/lang', `${name}.json`), 'utf-8'))
const messages = {en: load('en'), 'tr-TR': load('tr-TR')}

const TOO_MANY_VALUES = 'This report would have too many values. Show fewer groups, group time by a larger unit or choose a shorter period.'
const TOO_MANY_VALUES_TR = 'Bu raporda çok fazla değer olurdu. Daha az grup göster, zamanı daha büyük bir birimle grupla ya da daha kısa bir dönem seç.'

function mountView(props: Record<string, unknown>, locale: 'en' | 'tr-TR' = 'en') {
	const i18n = createI18n({legacy: false, locale, fallbackLocale: 'en', messages})
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	return mount(WidgetView, {
		attachTo: document.body,
		props: {type: 'number', config: {project_id: 3}, ...props} as never,
		global: {
			plugins: [i18n, [VueQueryPlugin, {queryClient}]],
			stubs: {Icon: true, TaskListWidget: {template: '<div class="task-list-stub" />'}},
			directives: {tooltip: {}},
		},
	})
}

async function settle() {
	for (let i = 0; i < 4; i++) await flushPromises()
}

const tooMany = (status = 400) => new ApiError({status, code: 23011, detail: 'The report would have too many values.'})

describe('WidgetView', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		sdk.reportsTasksOptions.mockResolvedValue({data: {dimensions: []}})
	})

	it('shows a text widget without asking the server for anything', async () => {
		const wrapper = mountView({type: 'text', config: {text: 'Quarterly goals'}})
		await settle()
		expect(wrapper.text()).toContain('Quarterly goals')
		expect(Object.values(sdk).every(mock => mock.mock.calls.length === 0)).toBe(true)
	})

	it('says so for a widget type this version does not know', async () => {
		const wrapper = mountView({type: 'hologram', config: {}})
		await settle()
		expect(wrapper.get('[role="status"]').text()).toBe('This widget type is not supported yet.')
	})

	describe('a report that fails with error 23011', () => {
		it.each([['en', TOO_MANY_VALUES], ['tr-TR', TOO_MANY_VALUES_TR]] as const)('shows the %s translation of a chart request, not the English server text', async (locale, expected) => {
			sdk.reportsTasks.mockRejectedValue(tooMany())
			const wrapper = mountView({type: 'bar', config: {project_id: 3, group_by: 'status'}}, locale)
			await settle()
			const alert = wrapper.get('[role="alert"]')
			expect(alert.text()).toBe(expected)
			expect(alert.text()).not.toContain('The report would have')
		})

		it.each([
			['workload', 'reportsWorkload'],
			['status_time', 'reportsStatusTime'],
			['time_tracking', 'reportsTimeTracking'],
		] as const)('shows the translation in the %s widget', async (type, call) => {
			sdk[call].mockRejectedValue(tooMany())
			const wrapper = mountView({type, config: {project_id: 3}}, 'tr-TR')
			await settle()
			expect(wrapper.get('[role="alert"]').text()).toBe(TOO_MANY_VALUES_TR)
			// the editor maps the error onto its controls
			expect(wrapper.emitted('error')?.at(-1)?.[0]).toMatchObject({code: 23011})
		})

		it('keeps the server text for a code that has no translation', async () => {
			sdk.reportsTasks.mockRejectedValue(new ApiError({status: 400, code: 99999, detail: 'Something odd happened.'}))
			const wrapper = mountView({type: 'donut', config: {project_id: 3, group_by: 'status'}})
			await settle()
			expect(wrapper.get('[role="alert"]').text()).toBe('Something odd happened.')
		})
	})

	it('says the time report is not available when the server answers 404 (no licence)', async () => {
		sdk.reportsTimeTracking.mockRejectedValue(new ApiError({status: 404, detail: 'Not found'}))
		const wrapper = mountView({type: 'time_tracking', config: {project_id: 3}})
		await settle()
		expect(wrapper.get('[role="alert"]').text()).toBe('Time tracking is not available on this server.')
	})
})
