<script setup lang="ts">
import {ref} from 'vue'

import type {ScriptReferenceEntry} from '@/helpers/automation'

const props = defineProps<{
	modelValue: string
	id: string
	label: string
	reference?: ScriptReferenceEntry[]
	invalid?: boolean
	rows?: number
}>()

const emit = defineEmits<{
	'update:modelValue': [value: string]
}>()

// Tab inserts a tab. Escape releases the keyboard trap: the next Tab leaves the field.
const releaseTab = ref(false)

function onKeydown(event: KeyboardEvent) {
	if (event.key === 'Escape') {
		releaseTab.value = true
		return
	}
	if (event.key !== 'Tab' || event.shiftKey || event.ctrlKey || event.metaKey || event.altKey) return
	if (releaseTab.value) {
		releaseTab.value = false
		return
	}
	event.preventDefault()
	const target = event.target as HTMLTextAreaElement
	const start = target.selectionStart
	const end = target.selectionEnd
	const next = `${props.modelValue.slice(0, start)}\t${props.modelValue.slice(end)}`
	emit('update:modelValue', next)
	requestAnimationFrame(() => target.setSelectionRange(start + 1, start + 1))
}
</script>

<template>
	<div
		class="code-editor"
		:class="{'has-reference': reference && reference.length > 0}"
	>
		<div class="code-editor__main">
			<textarea
				:id="id"
				class="textarea code-editor__input"
				:value="modelValue"
				:rows="rows ?? 8"
				:aria-label="label"
				:aria-invalid="invalid ? true : undefined"
				spellcheck="false"
				autocapitalize="off"
				autocomplete="off"
				wrap="off"
				@input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
				@keydown="onKeydown"
				@blur="releaseTab = false"
			/>
			<p class="help">
				{{ $t('automation.code.tabHint') }}
			</p>
		</div>
		<aside
			v-if="reference && reference.length > 0"
			class="code-editor__reference"
			:aria-label="$t('automation.code.reference')"
		>
			<h5>{{ $t('automation.code.reference') }}</h5>
			<dl>
				<template
					v-for="entry in reference"
					:key="entry.name"
				>
					<dt><code>{{ entry.name }}</code></dt>
					<dd v-if="entry.description">
						{{ entry.description }}
					</dd>
				</template>
			</dl>
		</aside>
	</div>
</template>

<style lang="scss" scoped>
.code-editor {
	display: grid;
	gap: .75rem;
	inline-size: 100%;

	&.has-reference {
		grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);

		@media screen and (max-width: $tablet) {
			grid-template-columns: minmax(0, 1fr);
		}
	}

	&__input {
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: .85rem;
		line-height: 1.45;
		tab-size: 4;
		white-space: pre;
		overflow: auto;
		min-block-size: 8rem;
	}

	&__reference {
		font-size: .8rem;
		max-block-size: 16rem;
		overflow: auto;
		padding: .5rem .75rem;
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		background: var(--grey-100);

		h5 {
			font-weight: 600;
			margin-block-end: .25rem;
		}

		dt {
			margin-block-start: .35rem;
		}

		dd {
			margin-inline-start: 0;
			color: var(--grey-600);
		}

		code {
			background: transparent;
			padding: 0;
			white-space: pre-wrap;
			word-break: break-word;
		}
	}
}
</style>
