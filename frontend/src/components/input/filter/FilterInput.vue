<script setup lang="ts">
import {onBeforeUnmount, onMounted, ref, shallowRef, watch} from 'vue'
import {useI18n} from 'vue-i18n'
import DatepickerWithValues from '@/components/date/DatepickerWithValues.vue'
import {useLabels} from '@/composables/useLabels'
import {useProjects} from '@/composables/useProjects'
import {queryClient} from '@/client/queryClient'
import {workflowKeys, workflowQuery} from '@/client/queries/workflow'
import {customFieldsQuery} from '@/client/queries/customFields'
import type {WorkflowResponse} from '@/helpers/workflow'
import {
	buildCustomFieldRefs,
	customFieldFilterFromApi,
	customFieldFilterToApi,
	type CustomFieldFilterRef,
} from '@/helpers/customFieldFilter'
import {
	transformFilterStringForApi,
	transformFilterStringFromApi,
} from '@/helpers/filters'
import {useDebounceFn} from '@vueuse/core'

import {EditorContent, useEditor} from '@tiptap/vue-3'
import {Extension} from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {Placeholder} from '@tiptap/extensions'
import {Plugin, PluginKey} from '@tiptap/pm/state'
import {
	createFilterHighlighter,
	filterHighlighterKey,
} from '@/components/input/filter/highlighter.ts'
import FilterAutocomplete from '@/components/input/filter/FilterAutocomplete'
import {toISOStringOrNull} from '@/helpers/time/toISOStringOrNull'

const props = defineProps<{
	projectId?: number,
	modelValue?: string,
}>()

const emit = defineEmits(['update:modelValue'])
const {t} = useI18n()

// Services and stores for autocomplete
const {labels, isPending, getLabelByExactTitle, getLabelById} = useLabels()
const projectList = useProjects()

// The custom fields the filter can address by name: those of the project, or in saved filters
// and other cross-project places those of every project the user can see.
const customFieldRefs = shallowRef<CustomFieldFilterRef[]>([])
let customFieldsLoad: Promise<CustomFieldFilterRef[]> | undefined

// Date picker functionality
const currentOldDatepickerValue = ref('')
const currentDatepickerValue = ref('')
const currentDatepickerPos = ref(0)
const datePickerPopupOpen = ref(false)
const datePickerAnchor = ref<HTMLElement | null>(null)

// Create a custom extension for filter syntax highlighting
const FilterHighlighter = Extension.create({
	name: 'filterHighlighter',

	addProseMirrorPlugins() {
		return [
			createFilterHighlighter(() => labels.value, () => customFieldRefs.value),
		]
	},
})

// Create a custom extension for handling date clicks
const DateClickHandler = Extension.create({
	name: 'dateClickHandler',

	addProseMirrorPlugins() {
		return [
			new Plugin({
				key: new PluginKey('dateClickHandler'),
				props: {
					handleClick: (view, pos, event) => {
						const target = event.target as HTMLElement
						if (target.classList.contains('date-value')) {
							event.preventDefault()
							event.stopPropagation()

							const dateValue = target.getAttribute('data-date-value') || ''
							const position = parseInt(target.getAttribute('data-position') || '0')

							currentOldDatepickerValue.value = dateValue
							currentDatepickerValue.value = dateValue
							currentDatepickerPos.value = position
							datePickerAnchor.value = target
							// This click light-dismissed an open picker on pointerup, but its toggle
							// event only lands after us — reopen once that has settled.
							datePickerPopupOpen.value = false
							setTimeout(() => {
								datePickerPopupOpen.value = true
							})

							return true
						}
						return false
					},
				},
			}),
		]
	},
})

// Initialize TipTap editor
const editor = useEditor({
	editorProps: {
		attributes: {
			role: 'textbox',
			'aria-label': t('filters.query.label'),
		},
	},
	extensions: [
		StarterKit,
		Placeholder.configure({
			placeholder: t('filters.query.placeholder'),
		}),
		FilterHighlighter,
		DateClickHandler,
		FilterAutocomplete.configure({
			get projectId() {
				return props.projectId
			},
			getCustomFields: loadCustomFields,
		}),
		Extension.create({
			name: 'enterHandler',
			addKeyboardShortcuts() {
				return {
					'Enter': () => {
						const popup = document.getElementById('filter-autocomplete-popup')
						const isAutocompleteVisible = popup && popup.style.display !== 'none'

						if (isAutocompleteVisible) {
							// Let the autocomplete handle the Enter key
							return false
						}

						return true
					},
				}
			},
		}),
	],
	content: '',
	onUpdate: ({editor}) => {
		const content = editor.getText()
		const processed = processContent(content)
		lastEmittedValue = processed
		emit('update:modelValue', processed)
	},
})

// The status titles of the project's workflow, if it has one and it is loaded.
const workflowStatuses = () => props.projectId
	? queryClient.getQueryData<WorkflowResponse | null>(workflowKeys.project(props.projectId))?.statuses ?? []
	: []

// Process the editor content to output snake_cased filter
const processContent = (content: string) => {
	return transformFilterStringForApi(
		customFieldFilterToApi(content, customFieldRefs.value),
		labelTitle => getLabelByExactTitle(labelTitle)?.id || null,
		projectTitle => {
			const found = projectList.findProjectByExactname(projectTitle)
			return found?.id || null
		},
		statusTitle => workflowStatuses().find(status => status.title === statusTitle)?.id || null,
	)
}

let lastEmittedValue: string | undefined
let lastSetContent: string | undefined
let pendingEditorContent: string | undefined
let pendingModelValue: string | undefined
let waitingForLabels = false

// Watch for changes to the model value from external sources.
// Skip when the change originated from the editor itself (onUpdate emit)
// to avoid a destructive round-trip where whitespace normalization in
// transformFilterStringForApi changes the text and setContent resets the cursor.
watch(
	() => props.modelValue,
	value => {
		if (value === lastEmittedValue) {
			return
		}
		setEditorContentFromModelValue(value)
		lastEmittedValue = undefined
	},
	{immediate: true},
)

onMounted(() => {
	setEditorContentFromModelValue(props.modelValue)
	loadWorkflowStatuses()
	// A single project is cheap. Across all projects the fields are only fetched once the filter
	// needs them: it already uses one, or the user starts typing a name.
	if (props.projectId || /\bcf_\d+/.test(props.modelValue ?? '')) {
		loadCustomFields()
	}
})

watch(() => props.projectId, () => {
	customFieldsLoad = undefined
	customFieldRefs.value = []
	loadCustomFields()
})

function loadCustomFields(): Promise<CustomFieldFilterRef[]> {
	customFieldsLoad ??= fetchCustomFields()
	return customFieldsLoad
}

async function fetchCustomFields(): Promise<CustomFieldFilterRef[]> {
	const projectIds = props.projectId
		? [props.projectId]
		: (projectList.projectsArray ?? []).filter(project => project.id > 0 && !project.is_archived).map(project => project.id)

	const results = await Promise.allSettled(projectIds.map(id => queryClient.ensureQueryData(customFieldsQuery(id))))
	const fields = results.flatMap(result => result.status === 'fulfilled' ? result.value : [])
	const refs = buildCustomFieldRefs(fields, id => projectList.projects[id]?.title)
	if (!editor.value) {
		return refs
	}
	customFieldRefs.value = refs
	editor.value.view.dispatch(editor.value.state.tr.setMeta(filterHighlighterKey, true))

	// Names only show once the fields are there; texts the user changed meanwhile are sent again with names resolved.
	const text = editor.value.getText()
	if (text === lastSetContent) {
		setEditorContentFromModelValue(props.modelValue)
	} else if (refs.length > 0) {
		const processed = processContent(text)
		if (processed !== lastEmittedValue) {
			lastEmittedValue = processed
			emit('update:modelValue', processed)
		}
	}
	return refs
}

// Status ids in the filter only show as titles once the workflow is there; texts the user changed meanwhile stay.
async function loadWorkflowStatuses() {
	if (!props.projectId) {
		return
	}
	try {
		await queryClient.ensureQueryData(workflowQuery(props.projectId))
	} catch {
		return
	}
	if (editor.value && editor.value.getText() === lastSetContent) {
		setEditorContentFromModelValue(props.modelValue)
	}
}

watch(isPending, pending => {
	if (pending || !waitingForLabels || editor.value?.getText() !== pendingEditorContent) {
		return
	}

	waitingForLabels = false
	setEditorContentFromModelValue(pendingModelValue)
})

watch(labels, () => {
	if (!editor.value) {
		return
	}

	editor.value.view.dispatch(editor.value.state.tr.setMeta(filterHighlighterKey, true))
})

function setEditorContentFromModelValue(newValue: string | undefined) {
	if (!editor.value) return

	const content = newValue ? customFieldFilterFromApi(transformFilterStringFromApi(
		newValue,
		labelId => getLabelById(labelId)?.title || null,
		projectId => projectList.projects[projectId]?.title || null,
		statusId => workflowStatuses().find(status => status.id === statusId)?.title || null,
	), customFieldRefs.value) : ''
	lastSetContent = content

	if (editor.value.getText() !== content) {
		// Preserve cursor position before updating content
		const currentPosition = editor.value.state.selection.from

		// Use JSON content format instead of a plain string to prevent
		// TipTap from parsing the value as HTML (reflected HTML injection
		// via the ?filter= URL parameter).
		editor.value.commands.setContent(content
			? {
				type: 'doc',
				content: [{
					type: 'paragraph',
					content: [{type: 'text', text: content}],
				}],
			}
			: '', {emitUpdate: false})

		// Restore cursor position after content update
		// Ensure position is within the new content bounds
		const maxPosition = editor.value.state.doc.content.size
		const safePosition = Math.min(currentPosition, maxPosition)
		editor.value.commands.setTextSelection(safePosition)
	}

	if (isPending.value) {
		pendingEditorContent = content
		pendingModelValue = newValue
		waitingForLabels = true
	}
}

function updateDateInQuery(newDate: string | Date | null) {
	if (!editor.value || !newDate) return

	const dateStr = typeof newDate === 'string' ? newDate : toISOStringOrNull(newDate)?.split('T')[0]
	if (!dateStr) return

	const currentText = editor.value.getText()
	const newText = currentText.replace(currentOldDatepickerValue.value, dateStr)
	currentOldDatepickerValue.value = dateStr

	editor.value.commands.setContent(newText
		? {
			type: 'doc',
			content: [{
				type: 'paragraph',
				content: [{type: 'text', text: newText}],
			}],
		}
		: '', {emitUpdate: false})
	const processed = processContent(newText)
	lastEmittedValue = processed
	emit('update:modelValue', processed)
}


// The blur from the editor might happen before the replacement after autocomplete select was done.
const blurDebounced = useDebounceFn(() => {
}, 500)

function focus() {
	editor.value?.commands.focus()
}

onBeforeUnmount(() => {
	editor.value?.destroy()
})

defineExpose({
	focus,
})
</script>

<template>
	<div class="filter-input">
		<div
			class="editor-wrapper"
			@blur="blurDebounced"
		>
			<EditorContent
				:editor="editor"
				class="editor-content"
			/>
		</div>
		<DatepickerWithValues
			v-model="currentDatepickerValue"
			v-model:open="datePickerPopupOpen"
			class="filter-datepicker"
			:anchor="datePickerAnchor"
			@update:modelValue="updateDateInQuery"
		/>
	</div>
</template>

<style lang="scss">
.filter-input {
	border: 1px solid var(--input-border-color);
	border-radius: var(--input-radius);
	background: var(--white);
	transition: border-color 0.2s ease;

	&:focus-within {
		border-color: var(--primary);
	}

	.filter-datepicker {
		position: absolute;
	}

	.ProseMirror {
		outline: none;
		white-space: pre-wrap;
		padding: 0 !important;

		// The wrapper's border is the focus indicator
		&:focus-visible {
			box-shadow: none;
		}

		.field {
			color: var(--code-literal);
		}
		
		.operator {
			color: var(--code-keyword);
		}
		
		.value {
			border-radius: $radius;
			padding: .125rem .25rem;
			background: var(--grey-100);
		}
		
		.label-value {
			border-radius: $radius;
			padding: .125rem .25rem;
		}
		
		.date-value {
			background-color: var(--primary);
			color: var(--white);
			border-radius: $radius;
			padding: 0.125em 0.25em;
			cursor: pointer;
			transition: background-color var(--transition);
			
			&:hover {
				background-color: var(--primary-dark);
			}
		}
		
		.grouping, .logical {
			color: var(--code-section);
		}
		
		.user-value {
			position: relative;
			padding-inline-start: 1.5em;
			
			&::before {
				content: attr(data-user);
				position: absolute;
				inset-inline-start: 0;
				inset-block-start: 50%;
				transform: translateY(-50%);
				inline-size: 1.2em;
				block-size: 1.2em;
				background-color: #3b82f6;
				color: white;
				border-radius: 50%;
				font-size: 0.8em;
				display: flex;
				align-items: center;
				justify-content: center;
				text-transform: uppercase;
			}
		}
		
		p.is-editor-empty:first-child::before {
			color: var(--grey-500);
			content: attr(data-placeholder);
			float: inline-start;
			block-size: 0;
			pointer-events: none;
		}
	}
}

.editor-wrapper {
	position: relative;
}

.editor-content {
	line-height: 1.5;
	padding: .5rem .75rem;
}
</style>
