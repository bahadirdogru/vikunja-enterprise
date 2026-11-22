<script setup lang="ts">
// The editor of a script (a `code` parameter or a script condition): a monospace textarea with a line number gutter,
// the conventions and limits of the instance, a filterable API reference that inserts at the cursor, example
// snippets, and the position of a server-side syntax error.
import {computed, nextTick, ref, useTemplateRef} from 'vue'
import {useI18n} from 'vue-i18n'

import AutomationSelect from '@/components/automation/AutomationSelect.vue'
import {useAutomationContext} from '@/components/automation/context'
import type {ScriptPosition} from '@/helpers/automation'
import {apiFor, conventionFor, filterApi, insertAt, snippetsFor, type ScriptMode} from '@/helpers/automationScript'

const props = withDefaults(defineProps<{
	modelValue: string
	id: string
	label: string
	mode?: ScriptMode
	invalid?: boolean
	rows?: number
	// Position of a syntax error reported by the server for this source.
	errorPosition?: ScriptPosition | null
	// Existing scripts stay readable when the instance has turned scripts off.
	readonly?: boolean
}>(), {
	mode: 'action',
	invalid: false,
	rows: 10,
	errorPosition: null,
	readonly: false,
})

const emit = defineEmits<{
	'update:modelValue': [value: string]
}>()

const {t} = useI18n({useScope: 'global'})
const context = useAutomationContext()
const script = computed(() => context.catalog.script)

const textarea = useTemplateRef<HTMLTextAreaElement>('textarea')
const gutter = useTemplateRef<HTMLElement>('gutter')

// ---- line numbers ------------------------------------------------------------------------------------------------
const lineCount = computed(() => props.modelValue.split('\n').length)
const errorLine = computed(() => props.errorPosition && props.errorPosition.line <= lineCount.value ? props.errorPosition.line : 0)
const scrollTop = ref(0)

function onScroll() {
	const el = textarea.value
	if (!el) return
	scrollTop.value = el.scrollTop
	if (gutter.value) gutter.value.scrollTop = el.scrollTop
}

// ---- keyboard ----------------------------------------------------------------------------------------------------
// Tab inserts a tab. Escape releases the keyboard trap: the next Tab leaves the field.
const releaseTab = ref(false)

function onKeydown(event: KeyboardEvent) {
	if (event.key === 'Escape') {
		releaseTab.value = true
		return
	}
	if (event.key !== 'Tab' || event.shiftKey || event.ctrlKey || event.metaKey || event.altKey || props.readonly) return
	if (releaseTab.value) {
		releaseTab.value = false
		return
	}
	event.preventDefault()
	insert('\t')
}

// ---- insertion at the cursor -------------------------------------------------------------------------------------
function insert(text: string, replaceAll = false) {
	if (props.readonly) return
	const el = textarea.value
	const start = replaceAll ? 0 : el?.selectionStart ?? props.modelValue.length
	const end = replaceAll ? props.modelValue.length : el?.selectionEnd ?? props.modelValue.length
	const result = insertAt(props.modelValue, start, end, text)
	emit('update:modelValue', result.value)
	nextTick(() => requestAnimationFrame(() => {
		el?.focus({preventScroll: true})
		el?.setSelectionRange(result.caret, result.caret)
	}))
}

// ---- reference ---------------------------------------------------------------------------------------------------
// eslint-disable-next-line vue/no-setup-props-reactivity-loss -- initial state only
const showReference = ref(props.mode === 'action')
const query = ref('')
const entries = computed(() => filterApi(apiFor(script.value, props.mode), query.value))
const referenceId = computed(() => `${props.id}-reference`)

// ---- snippets ----------------------------------------------------------------------------------------------------
const snippets = computed(() => snippetsFor(props.mode))
const snippetChoices = computed(() => snippets.value.map(snippet => ({value: snippet.id, label: t(snippet.titleKey)})))

function insertSnippet(value: string | number | boolean | null) {
	const snippet = snippets.value.find(s => s.id === value)
	if (!snippet) return
	// An empty editor takes the example as it is; otherwise it goes in at the cursor.
	insert(snippet.source, props.modelValue.trim() === '')
}

// ---- limits ------------------------------------------------------------------------------------------------------
const limits = computed(() => script.value?.limits ?? [])
const convention = computed(() => conventionFor(script.value, props.mode))
</script>

<template>
	<div class="code-editor">
		<p
			v-if="convention"
			class="code-editor__convention"
		>
			<Icon icon="code" />
			<span>{{ convention }}</span>
		</p>

		<div
			v-if="!readonly"
			class="code-editor__toolbar"
		>
			<AutomationSelect
				v-if="snippetChoices.length > 0"
				class="code-editor__snippets"
				:model-value="null"
				:options="snippetChoices"
				:label="$t('automation.code.insertExample')"
				:placeholder="$t('automation.code.insertExample')"
				@update:modelValue="insertSnippet"
			/>
			<button
				v-if="script"
				type="button"
				class="code-editor__toggle"
				:aria-expanded="showReference"
				:aria-controls="referenceId"
				@click="showReference = !showReference"
			>
				<Icon icon="chevron-down" />
				{{ $t('automation.code.reference') }}
			</button>
		</div>

		<div
			class="code-editor__body"
			:class="{'has-reference': showReference && script && !readonly}"
		>
			<div class="code-editor__main">
				<div
					class="code-editor__frame"
					:class="{'is-invalid': invalid || errorLine > 0}"
				>
					<div
						ref="gutter"
						class="code-editor__gutter"
						aria-hidden="true"
					>
						<div
							v-for="n in lineCount"
							:key="n"
							class="code-editor__line"
							:class="{'is-error': n === errorLine}"
						>
							{{ n }}
						</div>
					</div>
					<div class="code-editor__scroller">
						<div
							v-if="errorLine > 0"
							class="code-editor__band"
							:style="{'--line': errorLine, '--scroll': `${scrollTop}px`}"
							aria-hidden="true"
						/>
						<textarea
							:id="id"
							ref="textarea"
							class="code-editor__input"
							:value="modelValue"
							:rows="rows"
							:aria-label="label"
							:aria-invalid="invalid || errorLine > 0 ? true : undefined"
							:readonly="readonly"
							spellcheck="false"
							autocapitalize="off"
							autocomplete="off"
							wrap="off"
							@input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
							@keydown="onKeydown"
							@scroll="onScroll"
							@blur="releaseTab = false"
						/>
					</div>
				</div>
				<p
					v-if="errorPosition && errorLine > 0"
					class="help is-danger code-editor__position"
				>
					{{ $t('automation.code.errorAt', {line: errorPosition.line, column: errorPosition.column}) }}
				</p>
				<p
					v-if="!readonly"
					class="help"
				>
					{{ $t('automation.code.tabHint') }}
				</p>
			</div>

			<aside
				v-if="script && !readonly"
				v-show="showReference"
				:id="referenceId"
				class="code-editor__reference"
				:aria-label="$t('automation.code.reference')"
			>
				<input
					v-model="query"
					class="input"
					type="search"
					:aria-label="$t('automation.code.filter')"
					:placeholder="$t('automation.code.filter')"
				>
				<ul class="code-editor__api">
					<li
						v-for="entry in entries"
						:key="entry.name"
					>
						<button
							type="button"
							class="code-editor__api-entry"
							:title="$t('automation.code.insertName', {name: entry.name})"
							@mousedown.prevent
							@click="insert(entry.name)"
						>
							<code>{{ entry.signature }}</code>
							<span>{{ entry.description }}</span>
						</button>
					</li>
					<li
						v-if="entries.length === 0"
						class="help"
					>
						{{ $t('automation.code.noMatch') }}
					</li>
				</ul>
			</aside>
		</div>

		<details
			v-if="limits.length > 0 && !readonly"
			class="code-editor__limits"
		>
			<summary>{{ $t('automation.code.limits') }}</summary>
			<table>
				<thead>
					<tr>
						<th>{{ $t('automation.code.limitName') }}</th>
						<th>{{ $t('automation.code.limitValue') }}</th>
						<th>{{ $t('automation.code.limitDescription') }}</th>
					</tr>
				</thead>
				<tbody>
					<tr
						v-for="limit in limits"
						:key="limit.configKey || limit.name"
					>
						<td>
							{{ limit.name }}
							<code v-if="limit.configKey">{{ limit.configKey }}</code>
						</td>
						<td><code>{{ limit.value }}</code></td>
						<td>{{ limit.description }}</td>
					</tr>
				</tbody>
			</table>
		</details>
	</div>
</template>

<style lang="scss" scoped>
$mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;

.code-editor {
	display: flex;
	flex-direction: column;
	gap: .5rem;
	inline-size: 100%;
	min-inline-size: 0;

	&__convention {
		display: flex;
		gap: .5rem;
		align-items: flex-start;
		margin: 0;
		font-size: .8rem;
		color: var(--grey-600);

		:deep(svg) {
			margin-block-start: .2rem;
			flex: none;
		}
	}

	&__toolbar {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		align-items: center;
	}

	&__snippets {
		font-size: .85rem;
	}

	&__toggle {
		display: inline-flex;
		gap: .35rem;
		align-items: center;
		background: none;
		border: 0;
		padding: .25rem .4rem;
		font: inherit;
		font-size: .85rem;
		color: var(--link);
		cursor: pointer;

		&[aria-expanded="false"] :deep(svg) {
			transform: rotate(-90deg);
		}
	}

	&__body {
		display: grid;
		gap: .75rem;
		grid-template-columns: minmax(0, 1fr);

		&.has-reference {
			grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);

			@media screen and (max-width: $tablet) {
				grid-template-columns: minmax(0, 1fr);
			}
		}
	}

	&__main {
		min-inline-size: 0;
	}

	&__frame {
		display: flex;
		border: 1px solid var(--grey-300);
		border-radius: $radius;
		background: var(--white);
		overflow: hidden;
		font-family: $mono;
		font-size: .85rem;
		line-height: 1.45;

		&:focus-within {
			border-color: var(--primary);
		}

		&.is-invalid {
			border-color: var(--danger);
		}
	}

	&__gutter {
		flex: none;
		overflow: hidden;
		padding: .5rem .5rem calc(.5rem + 20px);
		min-inline-size: 2.6rem;
		text-align: end;
		color: var(--grey-500);
		background: var(--grey-100);
		border-inline-end: 1px solid var(--grey-200);
		user-select: none;
	}

	&__line {
		padding-inline: .15rem;

		&.is-error {
			color: var(--white);
			background: var(--danger);
			font-weight: 700;
			border-radius: 2px;
		}
	}

	&__scroller {
		position: relative;
		flex: 1 1 auto;
		min-inline-size: 0;
	}

	&__band {
		position: absolute;
		inset-inline: 0;
		inset-block-start: calc(.5rem + (var(--line) - 1) * 1.45em - var(--scroll));
		block-size: 1.45em;
		background: color-mix(in srgb, var(--danger) 18%, transparent);
		pointer-events: none;
	}

	&__input {
		position: relative;
		display: block;
		inline-size: 100%;
		box-sizing: border-box;
		padding: .5rem .6rem;
		border: 0;
		outline: 0;
		resize: vertical;
		background: transparent;
		color: var(--text);
		font: inherit;
		line-height: inherit;
		tab-size: 4;
		white-space: pre;
		overflow: auto;
		min-block-size: 8rem;

		&[readonly] {
			color: var(--grey-600);
		}
	}

	&__position {
		margin-block: .25rem 0;
		font-weight: 600;
	}

	&__reference {
		display: flex;
		flex-direction: column;
		gap: .4rem;
		font-size: .8rem;
		min-inline-size: 0;
		padding: .5rem;
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		background: var(--grey-100);
		max-block-size: 22rem;

		.input {
			flex: none;
		}
	}

	&__api {
		list-style: none;
		margin: 0;
		padding: 0;
		overflow: auto;
		min-block-size: 0;
	}

	&__api-entry {
		display: block;
		inline-size: 100%;
		text-align: start;
		background: none;
		border: 0;
		border-radius: $radius;
		padding: .3rem .4rem;
		font: inherit;
		color: var(--text);
		cursor: pointer;

		&:hover,
		&:focus-visible {
			background: var(--grey-200);
		}

		code {
			display: block;
			padding: 0;
			background: transparent;
			font-family: $mono;
			font-size: .78rem;
			white-space: pre-wrap;
			word-break: break-word;
			color: var(--text);
		}

		span {
			display: block;
			margin-block-start: .15rem;
			color: var(--grey-600);
		}
	}

	&__limits {
		font-size: .8rem;

		summary {
			cursor: pointer;
			color: var(--link);
		}

		table {
			inline-size: 100%;
			margin-block-start: .4rem;
			border-collapse: collapse;
		}

		th,
		td {
			text-align: start;
			vertical-align: top;
			padding: .25rem .5rem;
			border-block-end: 1px solid var(--grey-200);
		}

		code {
			display: block;
			padding: 0;
			background: transparent;
			font-size: .72rem;
			color: var(--grey-500);
		}

		td:nth-child(2) code {
			display: inline;
			font-size: inherit;
			color: inherit;
		}

		@media screen and (max-width: $tablet) {
			thead {
				display: none;
			}

			table,
			tbody,
			tr,
			td {
				display: block;
				inline-size: 100%;
			}

			tr {
				padding-block: .35rem;
				border-block-end: 1px solid var(--grey-200);
			}

			td {
				border: 0;
				padding-block: .1rem;
			}
		}
	}
}
</style>
