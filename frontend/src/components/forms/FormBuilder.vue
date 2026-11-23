<script setup lang="ts">
// The form builder: a list of fields, the settings and the share tab on the left, the form as a submitter will see it
// on the right. Everything it offers comes from the options of the project.
import {computed, nextTick, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'

import FormFieldEditor from '@/components/forms/FormFieldEditor.vue'
import FormRenderer from '@/components/forms/FormRenderer.vue'
import FormShare from '@/components/forms/FormShare.vue'
import type {LabelChoice} from '@/components/forms/FormDefaultInput.vue'
import UserValueInput from '@/components/custom-fields/UserValueInput.vue'
import XButton from '@/components/input/Button.vue'
import AsyncEditor from '@/components/input/AsyncEditor'
import Message from '@/components/misc/Message.vue'
import {
	publicFormLogoUrl,
	useDeleteFormLogoMutation,
	useSaveFormMutation,
	useUploadFormLogoMutation,
	type FormResponse,
} from '@/client/queries/forms'
import {isRequestContextAbort} from '@/client/requestContext'
import {
	FORM_ACCESS_MODES,
	FORM_ERROR,
	LOGO_TYPES,
	MAX_BRAND_NAME,
	addState,
	cloneDraft,
	controlId,
	documentFromDraft,
	draftFromForm,
	draftSignature,
	emptyDraft,
	findTarget,
	issuesFromProblem,
	locateIssue,
	logoProblem,
	mapIssues,
	moveField,
	newField,
	publicBlockers,
	targetName,
	titleRuleOk,
	validateDraft,
	type BuilderTab,
	type FormAccess,
	type FormDraft,
	type FormOptionsResponse,
	type Issue,
	type SettingsDraft,
	type SubmitterMode,
	type TargetInfo,
} from '@/helpers/forms'
import {definitionFromDraft, inputToIso, isoToInput} from '@/helpers/formsPublic'
import {sanitizeProjectDescription} from '@/helpers/sanitizeProjectDescription'
import {getErrorText} from '@/message'

type StatusChoice = {id: number, title: string, category?: string}

const props = defineProps<{
	projectId: number
	// The form to edit; null starts a new one, optionally from a preset.
	form: FormResponse | null
	preset?: FormDraft | null
	options: FormOptionsResponse
	statuses: StatusChoice[]
	labels: LabelChoice[]
}>()

const emit = defineEmits<{
	saved: [form: FormResponse]
	close: []
	submissions: [form: FormResponse]
}>()

const {t} = useI18n({useScope: 'global'})

/* eslint-disable vue/no-setup-props-reactivity-loss, vue/no-ref-object-reactivity-loss -- the builder is mounted once per edit session (the page keys it) */
function startDraft(): FormDraft {
	if (props.form) return draftFromForm(props.form)
	if (props.preset) return cloneDraft(props.preset)
	const draft = emptyDraft()
	const title = findTarget(props.options, 'title')
	if (title) draft.fields.push(newField(title, t, draft.access))
	return draft
}

const draft = ref<FormDraft>(startDraft())
const savedSignature = ref(props.form ? draftSignature(draft.value) : '')
const savedForm = ref<FormResponse | null>(props.form)
const disabledReason = ref(props.form?.disabled_reason ?? '')
const introInitiallyOpen = draft.value.description !== ''
/* eslint-enable vue/no-setup-props-reactivity-loss, vue/no-ref-object-reactivity-loss */

const dirty = computed(() => draftSignature(draft.value) !== savedSignature.value)

const tab = ref<BuilderTab>('fields')
const openKeys = ref<Set<string>>(new Set())
const addOpen = ref(false)
const previewOpen = ref(true)
const topError = ref('')

// ---- validation -------------------------------------------------------------------------------------------------
const showClientErrors = ref(false)
const serverIssues = ref<Issue[]>([])

const clientIssues = computed(() => showClientErrors.value ? validateDraft(draft.value, props.options, t) : [])
const allIssues = computed(() => [...clientIssues.value, ...serverIssues.value])
const mapped = computed(() => mapIssues(allIssues.value, draft.value))
const errors = computed(() => mapped.value.byId)
const errorsFor = (id: string) => errors.value.get(id) ?? []

// Server messages describe the saved document: they go stale with the next edit.
watch(() => draftSignature(draft.value), () => {
	serverIssues.value = []
	topError.value = ''
})

async function focusFirstIssue() {
	const issues = allIssues.value
	if (issues.length === 0) return
	const rank = (issue: Issue) => {
		const locator = locateIssue(issue.path)
		const tabRank = locator.tab === null ? 0 : locator.tab === 'fields' ? 1 : 2
		return tabRank * 1000 + (locator.fieldIndex ?? -1) + 1
	}
	const first = [...issues].sort((a, b) => rank(a) - rank(b))[0] as Issue
	const locator = locateIssue(first.path)
	if (locator.tab) tab.value = locator.tab
	if (locator.fieldIndex !== null) {
		const field = draft.value.fields[locator.fieldIndex]
		if (field) openKeys.value = new Set([...openKeys.value, field.key])
	}
	await nextTick()
	const id = controlId(locator, draft.value)
	const element = document.getElementById(id)
	if (!element) return
	element.scrollIntoView?.({block: 'center'})
	const focusable = element.matches('input, select, textarea, button')
		? element
		: element.querySelector<HTMLElement>('input, select, textarea, button')
	;(focusable ?? element).focus?.({preventScroll: true})
}

// ---- saving -----------------------------------------------------------------------------------------------------
const saveMutation = useSaveFormMutation()

async function save(): Promise<boolean> {
	showClientErrors.value = true
	topError.value = ''
	serverIssues.value = []
	if (validateDraft(draft.value, props.options, t).length > 0) {
		await focusFirstIssue()
		return false
	}
	const projectId = props.projectId
	try {
		const result = await saveMutation.mutateAsync({
			projectId,
			formId: savedForm.value?.id ?? null,
			document: documentFromDraft(draft.value),
		})
		if (props.projectId !== projectId) return false
		savedForm.value = result
		disabledReason.value = result.disabled_reason
		savedSignature.value = draftSignature(draft.value)
		showClientErrors.value = false
		emit('saved', result)
		return true
	} catch (cause) {
		if (isRequestContextAbort(cause)) return false
		const issues = (cause as {code?: number} | null)?.code === FORM_ERROR.INVALID ? issuesFromProblem(cause) : null
		if (issues && issues.length > 0) {
			serverIssues.value = issues
			await nextTick()
			await focusFirstIssue()
			return false
		}
		topError.value = getErrorText(cause)
		return false
	}
}

// ---- logo -------------------------------------------------------------------------------------------------------
// The logo is its own upload and needs a saved form: it is stored right away, not with the draft.
const uploadLogo = useUploadFormLogoMutation()
const deleteLogo = useDeleteFormLogoMutation()
const logoError = ref('')
const logoVersion = ref(0)
const logoInput = ref<HTMLInputElement | null>(null)
const logoBusy = computed(() => uploadLogo.isPending.value || deleteLogo.isPending.value)
const logoPreview = computed(() => savedForm.value?.has_brand_logo && savedForm.value.token
	? publicFormLogoUrl(savedForm.value.token, String(logoVersion.value))
	: '')
const logoAccept = LOGO_TYPES.join(',')

async function onLogoChosen(event: Event) {
	const input = event.target as HTMLInputElement
	const file = input.files?.[0]
	input.value = ''
	const form = savedForm.value
	if (!file || !form) return
	logoError.value = ''
	const problem = logoProblem(file)
	if (problem) {
		logoError.value = t(`forms.branding.logoErrors.${problem}`)
		return
	}
	try {
		savedForm.value = await uploadLogo.mutateAsync({projectId: props.projectId, formId: form.id, logo: file})
		logoVersion.value += 1
	} catch (cause) {
		if (isRequestContextAbort(cause)) return
		const reason = (cause as {details?: {reason?: string}} | null)?.details?.reason
		if (reason === 'too_large') logoError.value = t('forms.branding.logoErrors.size')
		else if (reason === 'not_an_image' || reason === 'unsupported_type') logoError.value = t('forms.branding.logoErrors.type')
		else logoError.value = getErrorText(cause)
	}
}

async function removeLogo() {
	const form = savedForm.value
	if (!form) return
	logoError.value = ''
	try {
		savedForm.value = await deleteLogo.mutateAsync({projectId: props.projectId, formId: form.id})
	} catch (cause) {
		if (!isRequestContextAbort(cause)) logoError.value = getErrorText(cause)
	}
}

const colorPickerValue = computed(() => /^#[0-9a-fA-F]{6}$/.test(draft.value.accent_color) ? draft.value.accent_color : '#1973ff')

defineExpose({save, dirty, savedForm})

// ---- fields -----------------------------------------------------------------------------------------------------
const usedTargets = computed(() => new Set(draft.value.fields.map(field => field.target)))

const groups = computed(() => {
	const builtin: TargetInfo[] = []
	const custom: TargetInfo[] = []
	for (const info of props.options.targets) {
		if (info.target.startsWith('cf_')) custom.push(info)
		else builtin.push(info)
	}
	return [
		{id: 'task', title: t('forms.builder.taskFields'), items: builtin},
		{id: 'custom', title: t('forms.builder.customFields'), items: custom},
	].filter(group => group.items.length > 0)
})

function addTarget(info: TargetInfo) {
	const state = addState(info, draft.value)
	if (!state.allowed) return
	const field = newField(info, t, draft.value.access)
	draft.value.fields.push(field)
	openKeys.value = new Set([...openKeys.value, field.key])
	addOpen.value = false
	nextTick(() => document.getElementById(`fb-f-${field.key}-${field.hidden ? 'default' : 'label'}`)?.focus())
}

function addDisabledReason(info: TargetInfo): string {
	const state = addState(info, draft.value)
	if (state.reason === 'used') return t('forms.builder.alreadyAdded')
	if (state.reason === 'limit') return t('forms.errors.tooManyFields', {max: 50})
	if (state.reason === 'public') return t('forms.builder.notOnPublic')
	return state.presetOnly ? t('forms.builder.presetOnPublic') : ''
}

function patchField(index: number, patch: Partial<(typeof draft.value.fields)[number]>) {
	const field = draft.value.fields[index]
	if (field) Object.assign(field, patch)
}

function toggleOpen(key: string) {
	const next = new Set(openKeys.value)
	if (next.has(key)) next.delete(key)
	else next.add(key)
	openKeys.value = next
}

function move(index: number, delta: -1 | 1) {
	draft.value.fields = moveField(draft.value.fields, index, delta)
	const key = draft.value.fields[index + delta]?.key
	if (key) nextTick(() => document.getElementById(`fb-f-${key}-head`)?.focus())
}

function remove(index: number) {
	const [removed] = draft.value.fields.splice(index, 1)
	if (removed) {
		const next = new Set(openKeys.value)
		next.delete(removed.key)
		openKeys.value = next
	}
	nextTick(() => document.getElementById('fb-add-field')?.focus())
}

function onAddKeydown(event: KeyboardEvent) {
	if (event.key === 'Escape' && addOpen.value) {
		addOpen.value = false
		document.getElementById('fb-add-field')?.focus()
	}
}

const titleRuleBroken = computed(() => !titleRuleOk(draft.value))

// ---- access and settings ------------------------------------------------------------------------------------------
const blockers = computed(() => draft.value.access === 'public' ? publicBlockers(draft.value, props.options) : [])
const settings = computed<SettingsDraft>(() => draft.value.settings)
const isPublic = computed(() => draft.value.access === 'public')

function blockerName(blocker: {target: string}): string {
	const info = findTarget(props.options, blocker.target)
	return info ? targetName(info, t) : blocker.target
}

const hasAttachments = computed(() => draft.value.fields.some(field => field.target === 'attachments'))
const hasWorkflow = computed(() => props.statuses.length > 0)

const closeInput = computed({
	get: () => settings.value.close_date ? isoToInput(settings.value.close_date, false) : '',
	set: value => {
		settings.value.close_date = value === '' ? null : inputToIso(value) || null
	},
})

function numberSetting(event: Event, key: 'max_submissions') {
	const raw = (event.target as HTMLInputElement).value
	settings.value[key] = raw === '' ? 0 : Math.trunc(Number(raw))
}

const MODES: SubmitterMode[] = ['off', 'optional', 'required']

// ---- preview ----------------------------------------------------------------------------------------------------
const definition = computed(() => definitionFromDraft(draft.value, props.options, props.labels, t))
const previewDescription = computed(() => draft.value.description ? sanitizeProjectDescription(draft.value.description) : '')

// ---- tabs -------------------------------------------------------------------------------------------------------
const TABS: BuilderTab[] = ['fields', 'settings', 'share']

function onTabKeydown(event: KeyboardEvent) {
	const index = TABS.indexOf(tab.value)
	let next: number
	if (event.key === 'ArrowRight') next = (index + 1) % TABS.length
	else if (event.key === 'ArrowLeft') next = (index - 1 + TABS.length) % TABS.length
	else if (event.key === 'Home') next = 0
	else if (event.key === 'End') next = TABS.length - 1
	else return
	event.preventDefault()
	tab.value = TABS[next] as BuilderTab
	nextTick(() => document.getElementById(`fb-tab-${tab.value}`)?.focus())
}

function assignees(value: number[], key: 'default_assignee_ids' | 'notify_user_ids') {
	settings.value[key] = value
}

function setAccess(access: FormAccess) {
	draft.value.access = access
}
</script>

<template>
	<div class="fb">
		<Message
			v-if="disabledReason"
			variant="warning"
		>
			{{ $te(`forms.disabledReason.${disabledReason}`) ? $t(`forms.disabledReason.${disabledReason}`) : $t('forms.disabledReason.unknown', {reason: disabledReason}) }}
		</Message>
		<Message
			v-if="topError"
			variant="danger"
		>
			{{ topError }}
		</Message>

		<div class="field fb__title">
			<label
				class="label"
				for="fb-title"
			>{{ $t('forms.builder.formTitle') }}</label>
			<input
				id="fb-title"
				v-model="draft.title"
				class="input"
				type="text"
				maxlength="250"
				:placeholder="$t('forms.builder.formTitlePlaceholder')"
				:aria-invalid="errorsFor('fb-title').length > 0 || undefined"
				:aria-describedby="errorsFor('fb-title').length > 0 ? 'fb-title-error' : undefined"
			>
			<p
				v-for="message in errorsFor('fb-title')"
				id="fb-title-error"
				:key="message"
				class="help is-danger"
				role="alert"
			>
				{{ message }}
			</p>
		</div>

		<div class="fb__columns">
			<div class="fb__main">
				<div
					class="fb__tabs"
					role="tablist"
					:aria-label="$t('forms.builder.sections')"
					@keydown="onTabKeydown"
				>
					<button
						v-for="name in TABS"
						:id="`fb-tab-${name}`"
						:key="name"
						type="button"
						role="tab"
						class="fb__tab"
						:class="{'is-active': tab === name}"
						:aria-selected="tab === name"
						:aria-controls="`fb-panel-${name}`"
						:tabindex="tab === name ? 0 : -1"
						@click="tab = name"
					>
						{{ $t(`forms.tabs.${name}`) }}
						<span
							v-if="mapped.tabCounts[name] > 0"
							class="fb__tab-badge"
							:aria-label="$t('forms.builder.issueCount', {count: mapped.tabCounts[name]})"
						>{{ mapped.tabCounts[name] }}</span>
					</button>
				</div>

				<!-- fields -->
				<section
					v-show="tab === 'fields'"
					id="fb-panel-fields"
					class="fb__panel"
					role="tabpanel"
					aria-labelledby="fb-tab-fields"
				>
					<details
						class="fb__intro"
						:open="introInitiallyOpen"
					>
						<summary id="fb-description-label">
							{{ $t('forms.builder.description') }}
						</summary>
						<div
							id="fb-description"
							class="fb__editor"
						>
							<AsyncEditor
								v-model="draft.description"
								:placeholder="$t('forms.builder.descriptionPlaceholder')"
								:project-id="projectId"
							/>
						</div>
					</details>

					<div
						v-if="titleRuleBroken"
						class="fb__rule"
					>
						<Message variant="info">
							<strong>{{ $t('forms.builder.titleRuleHeading') }}</strong>
							{{ $t('forms.builder.titleRule') }}
						</Message>
					</div>

					<ul
						id="fb-fields-list"
						class="fb__fields"
						:aria-label="$t('forms.builder.fieldList')"
						tabindex="-1"
					>
						<FormFieldEditor
							v-for="(field, index) in draft.fields"
							:key="field.key"
							:field="field"
							:info="findTarget(options, field.target)"
							:index="index"
							:count="draft.fields.length"
							:open="openKeys.has(field.key)"
							:project-id="projectId"
							:access="draft.access"
							:labels="labels"
							:errors="errors"
							@patch="patchField(index, $event)"
							@toggle="toggleOpen(field.key)"
							@move="move(index, $event)"
							@remove="remove(index)"
						/>
					</ul>
					<p
						v-if="draft.fields.length === 0"
						class="help"
					>
						{{ $t('forms.builder.noFields') }}
					</p>
					<p
						v-for="message in errorsFor('fb-fields-list')"
						:key="message"
						class="help is-danger"
						role="alert"
					>
						{{ message }}
					</p>

					<div
						class="fb__add"
						@keydown="onAddKeydown"
					>
						<XButton
							id="fb-add-field"
							type="button"
							icon="plus"
							variant="secondary"
							:aria-expanded="addOpen"
							aria-controls="fb-add-list"
							@click="addOpen = !addOpen"
						>
							{{ $t('forms.builder.addField') }}
						</XButton>
						<div
							v-if="addOpen"
							id="fb-add-list"
							class="fb__add-list"
						>
							<div
								v-for="group in groups"
								:key="group.id"
								role="group"
								:aria-labelledby="`fb-add-${group.id}`"
							>
								<p
									:id="`fb-add-${group.id}`"
									class="fb__add-heading"
								>
									{{ group.title }}
								</p>
								<ul>
									<li
										v-for="info in group.items"
										:key="info.target"
									>
										<button
											type="button"
											class="fb__add-item"
											:disabled="!addState(info, draft).allowed || undefined"
											:title="addDisabledReason(info) || undefined"
											:aria-describedby="addDisabledReason(info) && !usedTargets.has(info.target) ? `fb-add-why-${info.target}` : undefined"
											@click="addTarget(info)"
										>
											{{ targetName(info, t) }}
											<span
												v-if="usedTargets.has(info.target)"
												class="fb__add-note"
											>{{ $t('forms.builder.added') }}</span>
										</button>
										<span
											v-if="addDisabledReason(info) && !usedTargets.has(info.target)"
											:id="`fb-add-why-${info.target}`"
											class="fb__add-why"
										>{{ addDisabledReason(info) }}</span>
									</li>
								</ul>
							</div>
						</div>
					</div>
				</section>

				<!-- settings -->
				<section
					v-show="tab === 'settings'"
					id="fb-panel-settings"
					class="fb__panel"
					role="tabpanel"
					aria-labelledby="fb-tab-settings"
				>
					<fieldset class="fb__fieldset">
						<legend class="label">
							{{ $t('forms.settings.access') }}
						</legend>
						<div
							id="fb-s-access"
							class="fb__radios"
							role="radiogroup"
							:aria-describedby="errorsFor('fb-s-access').length > 0 ? 'fb-s-access-error' : undefined"
						>
							<label
								v-for="mode in FORM_ACCESS_MODES"
								:key="mode"
								class="fb__radio"
							>
								<input
									type="radio"
									name="fb-access"
									:value="mode"
									:checked="draft.access === mode"
									:disabled="(mode === 'public' && !options.public_enabled) || undefined"
									@change="setAccess(mode)"
								>
								<span>
									<strong>{{ $t(`forms.access.${mode}`) }}</strong>
									<span class="help">{{ $t(`forms.access.${mode}Help`) }}</span>
								</span>
							</label>
						</div>
						<p
							v-for="message in errorsFor('fb-s-access')"
							id="fb-s-access-error"
							:key="message"
							class="help is-danger"
							role="alert"
						>
							{{ message }}
						</p>
					</fieldset>

					<Message
						v-if="isPublic"
						variant="warning"
						class="fb__public-warning"
					>
						<strong>{{ $t('forms.settings.publicWarningTitle') }}</strong>
						{{ $t('forms.settings.publicWarning') }}
					</Message>
					<Message
						v-if="blockers.length > 0"
						variant="danger"
					>
						<p>{{ $t('forms.settings.publicBlockers') }}</p>
						<ul>
							<li
								v-for="blocker in blockers"
								:key="blocker.index"
							>
								{{ blockerName(blocker) }}:
								{{ blocker.reason === 'asked' ? $t('forms.settings.blockerAsked') : $t('forms.settings.blockerHidden') }}
							</li>
						</ul>
					</Message>

					<label class="checkbox fb__enabled">
						<input
							v-model="draft.enabled"
							type="checkbox"
						>
						{{ $t('forms.settings.enabled') }}
					</label>

					<div
						v-if="hasWorkflow"
						class="field"
					>
						<label
							class="label"
							for="fb-s-initial_status_id"
						>{{ $t('forms.settings.initialStatus') }}</label>
						<select
							id="fb-s-initial_status_id"
							v-model.number="settings.initial_status_id"
							class="fb__select"
						>
							<option :value="0">
								{{ $t('forms.settings.initialStatusDefault') }}
							</option>
							<option
								v-for="status in statuses"
								:key="status.id"
								:value="status.id"
							>
								{{ status.title }}
							</option>
						</select>
						<p class="help">
							{{ $t('forms.settings.initialStatusHelp') }}
						</p>
						<p
							v-for="message in errorsFor('fb-s-initial_status_id')"
							:key="message"
							class="help is-danger"
							role="alert"
						>
							{{ message }}
						</p>
					</div>

					<div class="field">
						<span
							id="fb-s-assignees-label"
							class="label"
						>{{ $t('forms.settings.assignees') }}</span>
						<div id="fb-s-default_assignee_ids">
							<UserValueInput
								:project-id="projectId"
								:model-value="settings.default_assignee_ids"
								labelledby="fb-s-assignees-label"
								@commit="assignees($event, 'default_assignee_ids')"
							/>
						</div>
						<p class="help">
							{{ $t('forms.settings.assigneesHelp') }}
						</p>
						<p
							v-for="message in errorsFor('fb-s-default_assignee_ids')"
							:key="message"
							class="help is-danger"
							role="alert"
						>
							{{ message }}
						</p>
					</div>

					<div class="field">
						<label
							class="label"
							for="fb-s-success_message"
						>{{ $t('forms.settings.successMessage') }}</label>
						<textarea
							id="fb-s-success_message"
							v-model="settings.success_message"
							class="textarea"
							rows="2"
							maxlength="2000"
							:placeholder="$t('forms.public.thanks')"
						/>
						<p
							v-for="message in errorsFor('fb-s-success_message')"
							:key="message"
							class="help is-danger"
							role="alert"
						>
							{{ message }}
						</p>
					</div>

					<div class="field">
						<label
							class="label"
							for="fb-s-redirect_url"
						>{{ $t('forms.settings.redirectUrl') }}</label>
						<input
							id="fb-s-redirect_url"
							v-model="settings.redirect_url"
							class="input"
							type="url"
							maxlength="2000"
							placeholder="https://"
							:aria-invalid="errorsFor('fb-s-redirect_url').length > 0 || undefined"
							:aria-describedby="'fb-s-redirect_url-help'"
						>
						<p
							id="fb-s-redirect_url-help"
							class="help"
						>
							{{ $t('forms.settings.redirectHelp') }}
						</p>
						<p
							v-for="message in errorsFor('fb-s-redirect_url')"
							:key="message"
							class="help is-danger"
							role="alert"
						>
							{{ message }}
						</p>
					</div>

					<label class="checkbox">
						<input
							id="fb-s-submit_another"
							v-model="settings.submit_another"
							type="checkbox"
						>
						{{ $t('forms.settings.submitAnother') }}
					</label>

					<div class="fb__pair">
						<div class="field">
							<label
								class="label"
								for="fb-s-close_date"
							>{{ $t('forms.settings.closeDate') }}</label>
							<div class="fb__inline">
								<input
									id="fb-s-close_date"
									v-model="closeInput"
									class="input"
									type="datetime-local"
								>
								<XButton
									v-if="settings.close_date"
									type="button"
									variant="secondary"
									@click="settings.close_date = null"
								>
									{{ $t('forms.settings.clear') }}
								</XButton>
							</div>
							<p class="help">
								{{ $t('forms.settings.closeDateHelp') }}
							</p>
						</div>
						<div class="field">
							<label
								class="label"
								for="fb-s-max_submissions"
							>{{ $t('forms.settings.maxSubmissions') }}</label>
							<input
								id="fb-s-max_submissions"
								class="input"
								type="number"
								min="0"
								step="1"
								:value="settings.max_submissions"
								:aria-invalid="errorsFor('fb-s-max_submissions').length > 0 || undefined"
								@input="numberSetting($event, 'max_submissions')"
							>
							<p class="help">
								{{ $t('forms.settings.maxSubmissionsHelp') }}
							</p>
							<p
								v-for="message in errorsFor('fb-s-max_submissions')"
								:key="message"
								class="help is-danger"
								role="alert"
							>
								{{ message }}
							</p>
						</div>
					</div>

					<div class="field">
						<span
							id="fb-s-notify-label"
							class="label"
						>{{ $t('forms.settings.notify') }}</span>
						<div id="fb-s-notify_user_ids">
							<UserValueInput
								:project-id="projectId"
								:model-value="settings.notify_user_ids"
								labelledby="fb-s-notify-label"
								@commit="assignees($event, 'notify_user_ids')"
							/>
						</div>
						<p class="help">
							{{ $t('forms.settings.notifyHelp') }}
						</p>
					</div>

					<fieldset class="fb__fieldset">
						<legend class="label">
							{{ $t('forms.settings.submitter') }}
						</legend>
						<p class="help">
							{{ isPublic ? $t('forms.settings.submitterHelp') : $t('forms.settings.submitterOnlyPublic') }}
						</p>
						<div class="fb__pair">
							<div class="field">
								<label
									class="label"
									for="fb-s-name_mode"
								>{{ $t('forms.public.name') }}</label>
								<select
									id="fb-s-name_mode"
									v-model="settings.name_mode"
									class="fb__select"
									:disabled="!isPublic || undefined"
								>
									<option
										v-for="mode in MODES"
										:key="mode"
										:value="mode"
									>
										{{ $t(`forms.settings.mode.${mode}`) }}
									</option>
								</select>
							</div>
							<div class="field">
								<label
									class="label"
									for="fb-s-email_mode"
								>{{ $t('forms.public.email') }}</label>
								<select
									id="fb-s-email_mode"
									v-model="settings.email_mode"
									class="fb__select"
									:disabled="!isPublic || undefined"
								>
									<option
										v-for="mode in MODES"
										:key="mode"
										:value="mode"
									>
										{{ $t(`forms.settings.mode.${mode}`) }}
									</option>
								</select>
							</div>
						</div>
					</fieldset>

					<p
						v-if="hasAttachments"
						class="help"
					>
						{{ $t('forms.settings.attachmentsNote') }}
					</p>

					<!-- branding -->
					<fieldset class="fb__branding">
						<legend class="label">
							{{ $t('forms.branding.title') }}
						</legend>
						<p class="help">
							{{ $t('forms.branding.help') }}
						</p>

						<div class="field">
							<label
								class="label"
								for="fb-s-brand_name"
							>{{ $t('forms.branding.name') }}</label>
							<input
								id="fb-s-brand_name"
								v-model="draft.brand_name"
								class="input"
								type="text"
								:maxlength="MAX_BRAND_NAME"
								:aria-invalid="errorsFor('fb-s-brand_name').length > 0 || undefined"
							>
							<p
								v-for="message in errorsFor('fb-s-brand_name')"
								:key="message"
								class="help is-danger"
								role="alert"
							>
								{{ message }}
							</p>
						</div>

						<div class="field">
							<label
								class="label"
								for="fb-s-accent_color"
							>{{ $t('forms.branding.accent') }}</label>
							<div class="fb__accent">
								<input
									class="fb__swatch"
									type="color"
									:value="colorPickerValue"
									:aria-label="$t('forms.branding.accentPicker')"
									@input="draft.accent_color = ($event.target as HTMLInputElement).value"
								>
								<input
									id="fb-s-accent_color"
									v-model="draft.accent_color"
									class="input"
									type="text"
									maxlength="7"
									placeholder="#1973ff"
									:aria-invalid="errorsFor('fb-s-accent_color').length > 0 || undefined"
								>
								<XButton
									v-if="draft.accent_color"
									type="button"
									variant="tertiary"
									@click="draft.accent_color = ''"
								>
									{{ $t('forms.settings.clear') }}
								</XButton>
							</div>
							<p
								v-for="message in errorsFor('fb-s-accent_color')"
								:key="message"
								class="help is-danger"
								role="alert"
							>
								{{ message }}
							</p>
						</div>

						<div class="field">
							<span class="label">{{ $t('forms.branding.logo') }}</span>
							<template v-if="savedForm">
								<div class="fb__logo">
									<img
										v-if="logoPreview"
										class="fb__logo-preview"
										:src="logoPreview"
										:alt="$t('forms.branding.logo')"
									>
									<input
										ref="logoInput"
										class="fb__logo-input"
										type="file"
										:accept="logoAccept"
										tabindex="-1"
										@change="onLogoChosen"
									>
									<XButton
										type="button"
										variant="secondary"
										:loading="logoBusy"
										@click="logoInput?.click()"
									>
										{{ logoPreview ? $t('forms.branding.logoReplace') : $t('forms.branding.logoUpload') }}
									</XButton>
									<XButton
										v-if="logoPreview"
										type="button"
										variant="tertiary"
										:disabled="logoBusy"
										@click="removeLogo"
									>
										{{ $t('forms.branding.logoRemove') }}
									</XButton>
								</div>
								<p class="help">
									{{ $t('forms.branding.logoHelp') }}
								</p>
							</template>
							<p
								v-else
								class="help"
							>
								{{ $t('forms.branding.logoAfterSave') }}
							</p>
							<p
								v-if="logoError"
								class="help is-danger"
								role="alert"
							>
								{{ logoError }}
							</p>
						</div>

						<label class="checkbox">
							<input
								id="fb-s-hide_footer"
								v-model="draft.hide_footer"
								type="checkbox"
							>
							{{ $t('forms.branding.hideFooter') }}
						</label>
					</fieldset>
				</section>

				<!-- share -->
				<section
					v-show="tab === 'share'"
					id="fb-panel-share"
					class="fb__panel"
					role="tabpanel"
					aria-labelledby="fb-tab-share"
				>
					<FormShare
						:form="savedForm"
						:dirty="dirty"
						:project-id="projectId"
						:access="draft.access"
						@submissions="savedForm && emit('submissions', savedForm)"
						@tokenChanged="savedForm && (savedForm = {...savedForm, token: $event})"
					/>
				</section>
			</div>

			<aside
				class="fb__preview"
				:aria-label="$t('forms.preview.title')"
			>
				<div class="fb__preview-head">
					<h3>{{ $t('forms.preview.title') }}</h3>
					<button
						type="button"
						class="fb__preview-toggle"
						:aria-expanded="previewOpen"
						aria-controls="fb-preview-body"
						@click="previewOpen = !previewOpen"
					>
						{{ previewOpen ? $t('forms.preview.hide') : $t('forms.preview.show') }}
					</button>
				</div>
				<div
					v-show="previewOpen"
					id="fb-preview-body"
					class="fb__preview-body"
				>
					<p class="help">
						{{ $t('forms.preview.help') }}
					</p>
					<h4 class="fb__preview-title">
						{{ definition.title || $t('forms.builder.formTitlePlaceholder') }}
					</h4>
					<!-- eslint-disable vue/no-v-html -- sanitized with DOMPurify -->
					<div
						v-if="previewDescription"
						class="fb__preview-description content"
						v-html="previewDescription"
					/>
					<!-- eslint-enable vue/no-v-html -->
					<FormRenderer
						:definition="definition"
						preview
					/>
				</div>
			</aside>
		</div>
	</div>
</template>

<style lang="scss" scoped>
.fb {
	display: flex;
	flex-direction: column;
	gap: 1rem;
	min-inline-size: 0;

	&__columns {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: 1.5rem;
		align-items: start;

		@media screen and (min-width: $desktop) {
			grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
		}
	}

	&__branding {
		display: flex;
		flex-direction: column;
		gap: 1rem;
		border: 0;
		padding: 0;
		margin: 0;
		min-inline-size: 0;
	}

	&__accent,
	&__logo {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: .5rem;
	}

	&__swatch {
		inline-size: 2.5rem;
		block-size: 2.5rem;
		padding: 0;
		border: 1px solid var(--grey-300);
		border-radius: $radius;
		background: none;
		cursor: pointer;
	}

	&__logo-preview {
		max-inline-size: 8rem;
		max-block-size: 3rem;
		background: var(--grey-100);
		border-radius: $radius;
	}

	&__logo-input {
		display: none;
	}

	&__main,
	&__panel {
		min-inline-size: 0;
	}

	&__panel {
		display: flex;
		flex-direction: column;
		gap: 1rem;
		padding-block-start: 1rem;
	}

	&__tabs {
		display: flex;
		gap: .25rem;
		border-block-end: 1px solid var(--grey-200);
		overflow-x: auto;
	}

	&__tab {
		position: relative;
		min-block-size: 2.75rem;
		padding: 0 1rem;
		background: none;
		border: 0;
		border-block-end: 3px solid transparent;
		color: var(--grey-700);
		cursor: pointer;
		font: inherit;
		white-space: nowrap;

		&.is-active {
			border-block-end-color: var(--primary);
			color: var(--text);
			font-weight: 700;
		}

		&:focus-visible {
			outline: 2px solid var(--primary);
			outline-offset: -2px;
		}
	}

	&__tab-badge {
		margin-inline-start: .4rem;
		padding: 0 .4rem;
		border-radius: 1rem;
		background: var(--danger);
		color: var(--white);
		font-size: .75rem;
	}

	&__fields {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: .5rem;
	}

	&__intro {
		summary {
			font-weight: 700;
			cursor: pointer;
			min-block-size: 2.5rem;
			display: list-item;
			list-style: disclosure-closed inside;
			padding-block: .5rem;
		}

		&[open] summary {
			list-style-type: disclosure-open;
		}
	}

	&__editor {
		border-radius: $radius;
		margin-block-start: .25rem;
	}

	&__add {
		position: relative;
	}

	&__add-list {
		margin-block-start: .5rem;
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		background: var(--white);
		box-shadow: var(--shadow-md);
		padding: .5rem;
		max-block-size: 22rem;
		overflow-y: auto;

		ul {
			list-style: none;
			margin: 0;
			padding: 0;
		}
	}

	&__add-heading {
		margin: .25rem .5rem;
		font-size: .8rem;
		text-transform: uppercase;
		color: var(--grey-600);
	}

	&__add-item {
		display: flex;
		inline-size: 100%;
		align-items: center;
		justify-content: space-between;
		gap: .5rem;
		min-block-size: 2.5rem;
		padding: 0 .75rem;
		background: none;
		border: 0;
		border-radius: $radius;
		color: var(--text);
		cursor: pointer;
		text-align: start;
		font: inherit;

		&:hover:not(:disabled),
		&:focus-visible {
			background: var(--grey-100);
		}

		&:disabled {
			color: var(--grey-500);
			cursor: not-allowed;
		}
	}

	&__add-note,
	&__add-why {
		font-size: .8rem;
		color: var(--grey-600);
	}

	&__add-why {
		display: block;
		padding: 0 .75rem .35rem;
	}

	&__fieldset {
		border: 0;
		margin: 0;
		padding: 0;
		min-inline-size: 0;
	}

	&__radios {
		display: flex;
		flex-direction: column;
		gap: .5rem;
	}

	&__radio {
		display: flex;
		gap: .6rem;
		align-items: flex-start;
		padding: .5rem .75rem;
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		cursor: pointer;

		input {
			margin-block-start: .3rem;
		}

		span {
			display: flex;
			flex-direction: column;
		}

		&:has(input:checked) {
			border-color: var(--primary);
		}
	}

	&__select {
		background: var(--input-background, var(--white));
		color: var(--input-color, var(--text));
		border: 1px solid var(--input-border-color, var(--grey-300));
		border-radius: $radius;
		padding: .5rem .75rem;
		font: inherit;
		min-block-size: 2.5rem;
		inline-size: 100%;
	}

	&__pair {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr));
		gap: 1rem;
	}

	&__inline {
		display: flex;
		gap: .5rem;
	}

	&__preview {
		min-inline-size: 0;
		border: 1px solid var(--grey-200);
		border-radius: $radius;
		padding: 1rem;
		background: var(--scheme-main, var(--white));

		@media screen and (min-width: $desktop) {
			position: sticky;
			inset-block-start: 0;
			max-block-size: 80vh;
			overflow-y: auto;
		}
	}

	&__preview-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: .5rem;

		h3 {
			margin: 0;
			font-size: 1rem;
		}
	}

	&__preview-toggle {
		min-block-size: 2.75rem;
		background: none;
		border: 0;
		color: var(--link, var(--primary));
		text-decoration: underline;
		cursor: pointer;
		font: inherit;

		@media screen and (min-width: $desktop) {
			display: none;
		}
	}

	&__preview-body {
		display: flex;
		flex-direction: column;
		gap: 1rem;

		@media screen and (min-width: $desktop) {
			display: flex !important; // stylelint-disable-line declaration-no-important
		}
	}

	&__preview-title {
		margin: 0;
		font-size: 1.3rem;
		overflow-wrap: anywhere;
	}
}
</style>
