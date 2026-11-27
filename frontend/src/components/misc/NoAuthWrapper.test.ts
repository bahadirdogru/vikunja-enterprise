import {describe, it, expect, vi, beforeEach, afterEach} from 'vitest'
import {mount} from '@vue/test-utils'
import {reactive} from 'vue'

import NoAuthWrapper from './NoAuthWrapper.vue'

const configStore = reactive({motd: '', show_api_config: true})

vi.mock('@/stores/config', () => ({
	useConfigStore: () => configStore,
}))

vi.mock('vue-router', async importOriginal => ({
	...await importOriginal<typeof import('vue-router')>(),
	useRoute: () => ({meta: {}}),
}))

vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

vi.mock('@/composables/useTitle', () => ({
	useTitle: () => {},
}))

const stubs = {
	Logo: true,
	Legal: true,
	Message: true,
	ApiConfig: {template: '<div class="api-config-stub" />'},
}

function mountWrapper(showApiConfig = true) {
	return mount(NoAuthWrapper, {
		props: {showApiConfig},
		global: {stubs, mocks: {$t: (key: string) => key}},
	})
}

describe('NoAuthWrapper', () => {
	beforeEach(() => {
		configStore.show_api_config = true
		delete window.vikunjaDesktop
		localStorage.removeItem('API_URL')
	})

	afterEach(() => {
		delete window.vikunjaDesktop
		localStorage.removeItem('API_URL')
	})

	it('shows the API server on the web by default', () => {
		expect(mountWrapper().find('.api-config-stub').exists()).toBe(true)
	})

	it('hides the API server on the web when the instance turns it off', () => {
		configStore.show_api_config = false
		expect(mountWrapper().find('.api-config-stub').exists()).toBe(false)
	})

	it('does not show the API server when the page does not ask for it', () => {
		expect(mountWrapper(false).find('.api-config-stub').exists()).toBe(false)
	})

	it('keeps the desktop behaviour when the instance turns it off', () => {
		configStore.show_api_config = false
		window.vikunjaDesktop = {isDesktop: true} as typeof window.vikunjaDesktop
		localStorage.setItem('API_URL', 'https://vikunja.example.com/api/v1')
		expect(mountWrapper().find('.api-config-stub').exists()).toBe(true)
	})

	it('hides the API server on the desktop until one was chosen', () => {
		window.vikunjaDesktop = {isDesktop: true} as typeof window.vikunjaDesktop
		expect(mountWrapper().find('.api-config-stub').exists()).toBe(false)
	})
})
