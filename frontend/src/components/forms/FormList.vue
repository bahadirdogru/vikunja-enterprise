<script setup lang="ts">
import {computed, ref} from 'vue'
import {useI18n} from 'vue-i18n'

import XButton from '@/components/input/Button.vue'
import Modal from '@/components/misc/Modal.vue'
import {
	useDeleteFormMutation,
	useToggleFormMutation,
	type FormResponse,
} from '@/client/queries/forms'
import {isRequestContextAbort} from '@/client/requestContext'
import {availablePresets, formLink, type FormOptionsResponse, type Preset} from '@/helpers/forms'
import {error, success} from '@/message'

const props = defineProps<{
	forms: FormResponse[]
	options: FormOptionsResponse
}>()

const emit = defineEmits<{
	create: []
	preset: [preset: Preset]
	edit: [form: FormResponse]
	submissions: [form: FormResponse]
	duplicate: [form: FormResponse]
}>()

const {t, te} = useI18n({useScope: 'global'})

const toggle = useToggleFormMutation()
const remove = useDeleteFormMutation()
const pending = ref<number | null>(null)
const toDelete = ref<FormResponse | null>(null)

const presets = computed(() => availablePresets(props.options, t))

async function setEnabled(form: FormResponse, enabled: boolean) {
	pending.value = form.id
	try {
		await toggle.mutateAsync({form, enabled})
	} catch (cause) {
		if (!isRequestContextAbort(cause)) pending.value = null
	}
	pending.value = null
}

async function confirmDelete() {
	const form = toDelete.value
	if (!form) return
	try {
		await remove.mutateAsync({projectId: form.project_id, formId: form.id})
	} catch {
		return
	}
	toDelete.value = null
}

async function copyLink(form: FormResponse) {
	try {
		await navigator.clipboard.writeText(formLink(form.token))
		success({message: t('forms.share.copied')})
	} catch {
		error(new Error(t('forms.share.copyFailed')))
	}
}

function reasonText(form: FormResponse): string {
	const key = `forms.disabledReason.${form.disabled_reason}`
	return te(key) ? t(key) : t('forms.disabledReason.unknown', {reason: form.disabled_reason})
}
</script>

<template>
	<div class="fl">
		<div
			v-if="forms.length === 0"
			class="fl__empty"
		>
			<Icon
				icon="file-lines"
				class="fl__empty-icon"
			/>
			<h3>{{ $t('forms.empty.title') }}</h3>
			<p>{{ $t('forms.empty.text') }}</p>
			<XButton
				type="button"
				icon="plus"
				@click="emit('create')"
			>
				{{ $t('forms.new') }}
			</XButton>
		</div>

		<template v-else>
			<div class="fl__toolbar">
				<XButton
					type="button"
					icon="plus"
					@click="emit('create')"
				>
					{{ $t('forms.new') }}
				</XButton>
			</div>
			<ul class="fl__items">
				<li
					v-for="form in forms"
					:key="form.id"
					class="fi"
				>
					<div class="fi__main">
						<div class="fi__title">
							<button
								type="button"
								class="fi__name"
								@click="emit('edit', form)"
							>
								{{ form.title }}
							</button>
							<span
								class="fi__badge"
								:class="`fi__badge--${form.access}`"
							>{{ $t(`forms.access.${form.access}Short`) }}</span>
							<span
								v-if="form.disabled_reason"
								v-tooltip="reasonText(form)"
								class="fi__badge fi__badge--warning"
								tabindex="0"
								role="img"
								:aria-label="reasonText(form)"
							>
								<Icon icon="exclamation-circle" />
								{{ $t('forms.disabledBadge') }}
							</span>
						</div>
						<p class="fi__meta">
							{{ $t('forms.submissions.count', {count: form.submission_count}, form.submission_count) }}
							<span v-if="form.disabled_reason">
								· {{ reasonText(form) }}
							</span>
						</p>
					</div>
					<div class="fi__side">
						<label class="fi__switch checkbox">
							<input
								type="checkbox"
								:checked="form.enabled"
								:disabled="pending === form.id || undefined"
								:aria-label="$t('forms.enableForm', {title: form.title})"
								@change="setEnabled(form, ($event.target as HTMLInputElement).checked)"
							>
							{{ form.enabled ? $t('forms.on') : $t('forms.off') }}
						</label>
						<span class="fi__actions">
							<XButton
								type="button"
								:aria-label="$t('forms.copyLink', {title: form.title})"
								variant="secondary"
								icon="link"
								@click="copyLink(form)"
							/>
							<XButton
								:aria-label="$t('forms.openForm', {title: form.title})"
								variant="secondary"
								icon="arrow-up-right-from-square"
								:href="formLink(form.token)"
								target="_blank"
								rel="noopener"
							/>
							<XButton
								type="button"
								:aria-label="$t('forms.edit', {title: form.title})"
								variant="secondary"
								icon="pen"
								@click="emit('edit', form)"
							/>
							<XButton
								type="button"
								:aria-label="$t('forms.submissionsOf', {title: form.title})"
								variant="secondary"
								icon="list"
								@click="emit('submissions', form)"
							/>
							<XButton
								type="button"
								:aria-label="$t('forms.duplicate', {title: form.title})"
								variant="secondary"
								icon="copy"
								@click="emit('duplicate', form)"
							/>
							<XButton
								type="button"
								:aria-label="$t('forms.delete', {title: form.title})"
								variant="secondary"
								icon="trash-alt"
								danger
								@click="toDelete = form"
							/>
						</span>
					</div>
				</li>
			</ul>
		</template>

		<section
			v-if="presets.length > 0"
			class="fl__presets"
			aria-labelledby="forms-presets"
		>
			<h4 id="forms-presets">
				{{ $t('forms.presets.heading') }}
			</h4>
			<ul>
				<li
					v-for="preset in presets"
					:key="preset.id"
				>
					<button
						type="button"
						class="preset"
						@click="emit('preset', preset)"
					>
						<Icon icon="file-lines" />
						<span class="preset__text">
							<strong>{{ preset.title }}</strong>
							<span>{{ preset.description }}</span>
						</span>
					</button>
				</li>
			</ul>
		</section>

		<Modal
			:enabled="toDelete !== null"
			@close="toDelete = null"
			@submit="confirmDelete"
		>
			<template #header>
				<span>{{ $t('forms.deleteConfirm.title') }}</span>
			</template>
			<template #text>
				<p>{{ $t('forms.deleteConfirm.text', {title: toDelete?.title ?? ''}) }}</p>
			</template>
		</Modal>
	</div>
</template>

<style lang="scss" scoped>
.fl {
	display: flex;
	flex-direction: column;
	gap: 1rem;

	&__empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: .6rem;
		text-align: center;
		padding-block: 1.5rem .5rem;

		h3 {
			margin: 0;
			font-size: 1.1rem;
		}

		p {
			margin: 0;
			max-inline-size: 40rem;
		}
	}

	&__empty-icon {
		font-size: 2rem;
		color: var(--grey-400);
	}

	&__items,
	&__presets ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: .5rem;
	}

	&__presets h4 {
		font-weight: 700;
		margin-block-end: .5rem;
	}

	&__presets li {
		display: flex;
	}

	&__presets ul {
		display: grid;
		align-items: stretch;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 16rem), 1fr));
	}
}

.fi {
	display: flex;
	flex-wrap: wrap;
	gap: .6rem 1rem;
	justify-content: space-between;
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	padding: .7rem .8rem;

	&__main {
		flex: 1 1 18rem;
		min-inline-size: 0;
	}

	&__title {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem;
		align-items: center;
	}

	&__name {
		font-weight: 700;
		font-size: 1rem;
		background: none;
		border: 0;
		padding: 0;
		color: var(--text);
		cursor: pointer;
		text-align: start;
		overflow-wrap: anywhere;

		&:hover {
			color: var(--primary);
		}
	}

	&__badge {
		display: inline-flex;
		align-items: center;
		gap: .3rem;
		font-size: .75rem;
		padding: .05rem .5rem;
		border-radius: 1rem;
		background: var(--grey-200);
		color: var(--grey-800);

		&--public {
			background: var(--warning-light, var(--grey-200));
		}

		&--warning {
			background: var(--warning, #f5a623);
			color: #000000;
		}
	}

	&__meta {
		margin: .2rem 0 0;
		color: var(--grey-600);
		font-size: .9rem;
	}

	&__side {
		display: flex;
		flex-direction: column;
		gap: .5rem;
		align-items: flex-end;

		@media screen and (max-width: $tablet) {
			flex-direction: row;
			flex-wrap: wrap;
			align-items: center;
			justify-content: space-between;
			inline-size: 100%;
		}
	}

	&__switch {
		display: flex;
		gap: .4rem;
		align-items: center;
	}

	&__actions {
		display: flex;
		flex-wrap: wrap;
		gap: .25rem;
	}
}

.preset {
	flex: 1;
	display: flex;
	gap: .6rem;
	align-items: flex-start;
	inline-size: 100%;
	text-align: start;
	padding: .6rem .75rem;
	border: 1px dashed var(--grey-300);
	border-radius: $radius;
	background: transparent;
	color: var(--text);
	cursor: pointer;

	&:hover,
	&:focus-visible {
		border-color: var(--primary);
		background: var(--grey-100);
	}

	&__text {
		display: flex;
		flex-direction: column;
		font-size: .85rem;
		gap: .15rem;

		span {
			color: var(--grey-600);
		}
	}
}
</style>
