import {shallowMount} from '@vue/test-utils'
import {nextTick} from 'vue'
import {beforeEach, describe, expect, it, vi} from 'vitest'

const labelState = vi.hoisted(() => ({
	labels: undefined as {value: Array<{id?: number, title?: string, hex_color?: string}>} | undefined,
	isPending: undefined as {value: boolean} | undefined,
}))

const editorState = vi.hoisted(() => ({
	text: '',
	options: undefined as {onUpdate: (context: {editor: unknown}) => void} | undefined,
	editor: undefined as Record<string, unknown> | undefined,
	dispatch: vi.fn(),
}))

vi.mock('@/composables/useLabels', async () => {
	const {ref} = await import('vue')
	labelState.labels = ref([])
	labelState.isPending = ref(true)

	return {
		useLabels: () => ({
			labels: labelState.labels,
			isPending: labelState.isPending,
			getLabelById: (id: number) => labelState.labels?.value.find(label => label.id === id),
			getLabelByExactTitle: (title: string) => labelState.labels?.value.find(label => label.title === title),
		}),
	}
})

vi.mock('@/composables/useProjects', () => ({
	useProjects: () => ({
		projects: {},
		findProjectByExactname: () => null,
	}),
}))

vi.mock('vue-i18n', async importOriginal => ({
	...await importOriginal<typeof import('vue-i18n')>(),
	useI18n: () => ({t: (key: string) => key}),
}))

vi.mock('@tiptap/vue-3', async () => {
	const {defineComponent, h, shallowRef} = await import('vue')
	const editor = {
		getText: () => editorState.text,
		commands: {
			setContent: (content: string | {content?: Array<{content?: Array<{text?: string}>}>}) => {
				editorState.text = typeof content === 'string'
					? content
					: content.content?.[0]?.content?.[0]?.text ?? ''
			},
			setTextSelection: vi.fn(),
			focus: vi.fn(),
		},
		state: {
			selection: {from: 1},
			doc: {content: {size: 100}},
			tr: {setMeta: vi.fn().mockReturnThis()},
		},
		view: {dispatch: editorState.dispatch},
		destroy: vi.fn(),
	}
	editorState.editor = editor

	return {
		EditorContent: defineComponent({render: () => h('div')}),
		useEditor: (options: typeof editorState.options) => {
			editorState.options = options
			return shallowRef(editor)
		},
		VueRenderer: class {},
	}
})

vi.mock('@/client/queries/customFields', () => ({
	customFieldsQuery: (projectId: number) => ({
		queryKey: ['test-custom-fields', projectId],
		queryFn: async () => [
			{id: 4, project_id: projectId, title: 'Stage', type: 'select', position: 1, options: [{id: 31, title: 'Review'}]},
			{id: 5, project_id: projectId, title: 'Cost center', type: 'text', position: 2, options: []},
		],
	}),
}))

import FilterInput from './FilterInput.vue'

describe('FilterInput label loading', () => {
	beforeEach(() => {
		editorState.text = ''
		editorState.dispatch.mockReset()
		labelState.labels!.value = []
		labelState.isPending!.value = true
	})

	it('resolves label ids and refreshes highlighting when labels finish loading', async () => {
		const wrapper = shallowMount(FilterInput, {
			props: {modelValue: 'labels = 1'},
			global: {mocks: {$t: (key: string) => key}},
		})
		expect(editorState.text).toBe('labels = 1')

		labelState.labels!.value = [{id: 1, title: 'Work', hex_color: 'ff006e'}]
		labelState.isPending!.value = false
		await nextTick()

		expect(editorState.text).toBe('labels = Work')
		expect(editorState.dispatch).toHaveBeenCalled()
		wrapper.unmount()
	})

	it('does not replace an edit made before labels finish loading', async () => {
		const wrapper = shallowMount(FilterInput, {
			props: {modelValue: 'labels = 1'},
			global: {mocks: {$t: (key: string) => key}},
		})
		editorState.text = 'labels = Personal'
		editorState.options!.onUpdate({editor: editorState.editor!})

		labelState.labels!.value = [{id: 1, title: 'Work'}]
		labelState.isPending!.value = false
		await nextTick()

		expect(editorState.text).toBe('labels = Personal')
		wrapper.unmount()
	})
})

describe('FilterInput custom fields', () => {
	beforeEach(() => {
		editorState.text = ''
		editorState.dispatch.mockReset()
		labelState.labels!.value = []
		labelState.isPending!.value = false
	})

	it('shows custom field names and option titles once the fields are loaded', async () => {
		const wrapper = shallowMount(FilterInput, {
			props: {modelValue: 'cf_5 like x && cf_4 = 31', projectId: 1},
			global: {mocks: {$t: (key: string) => key}},
		})
		expect(editorState.text).toBe('cf_5 like x && cf_4 = 31')

		await vi.waitFor(() => expect(editorState.text).toBe('"Cost center" like x && Stage = Review'))
		wrapper.unmount()
	})

	it('sends names as ids and keeps the display text', async () => {
		const emitted: string[] = []
		const wrapper = shallowMount(FilterInput, {
			props: {modelValue: '', projectId: 1, 'onUpdate:modelValue': (value: string) => emitted.push(value)},
			global: {mocks: {$t: (key: string) => key}},
		})
		await vi.waitFor(() => expect(editorState.dispatch).toHaveBeenCalled())

		editorState.text = '"Cost center" like x && Stage = Review'
		editorState.options!.onUpdate({editor: editorState.editor!})

		expect(emitted.at(-1)).toBe('cf_5 like x && cf_4 = 31')
		wrapper.unmount()
	})
})
