import {mount, type MountingOptions} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'
import type {Component} from 'vue'

import {readFileSync} from 'node:fs'
import {resolve as resolvePath} from 'node:path'

// Read from disk: the i18n plugin precompiles the messages of a JSON import.
const en = JSON.parse(readFileSync(resolvePath(process.cwd(), 'src/i18n/lang/en.json'), 'utf-8'))

export const i18n = createI18n({legacy: false, locale: 'en', fallbackLocale: 'en', messages: {en}})

export function mountChart<T extends Component>(component: T, props: Record<string, unknown>, options: MountingOptions<Record<string, unknown>> = {}) {
	return mount(component as Component, {
		attachTo: document.body,
		props,
		...options,
		global: {
			plugins: [i18n],
			stubs: {Icon: true},
			directives: {tooltip: {}},
			...(options.global ?? {}),
		},
	})
}

export const statusGroups = [
	{key: 3, label: 'In progress', color: '#3b82f6', value: 12},
	{key: 1, label: 'To do', color: '#94a3b8', value: 7},
	{key: 4, label: 'Done', color: '#22c55e', value: 3},
]
