import {keepPreviousData, queryOptions, useMutation} from '@tanstack/vue-query'

import {
	formPublicRead,
	formsLogoDelete,
	formsLogoUpload,
	formPublicSubmit,
	formPublicSubmitMultipart,
	formSubmissionsList,
	formsCreate,
	formsDelete,
	formsList,
	formsOptions,
	formsToken,
	formsUpdate,
} from '@/client/generated'
import type {Form, FormSubmission, FormWritable} from '@/client/generated'
import {publicClient} from '@/client/publicClient'
import {getApiBaseUrl} from '@/helpers/apiUrl'
import {getToken} from '@/helpers/auth'
import {contextMutationOptions} from './contextMutation'
import {fetchAllPages} from './fetchAllPages'
import {toPaginated, type Paginated} from './pagination'
import {i18n} from '@/i18n'
import {
	FORM_ERROR,
	normalizeOptions,
	type FormAccess,
	type FormOptionsResponse,
} from '@/helpers/forms'
import {classifyPublicProblem, normalizeDefinition, type PublicDefinition, type SubmissionBody} from '@/helpers/formsPublic'

// ---- admin -------------------------------------------------------------------------------------------------------

export type FormResponse = Form & {
	id: number
	project_id: number
	title: string
	token: string
	access: FormAccess
	enabled: boolean
	disabled_reason: string
	submission_count: number
}

export function normalizeForm(form: Form): FormResponse {
	return {
		...form,
		id: form.id ?? 0,
		project_id: form.project_id ?? 0,
		title: form.title ?? '',
		token: form.token ?? '',
		access: (form.access ?? 'members') as FormAccess,
		enabled: form.enabled ?? false,
		disabled_reason: form.disabled_reason ?? '',
		submission_count: form.submission_count ?? 0,
		fields: form.fields ?? [],
	}
}

export type SubmissionResponse = {
	id: number
	task_id: number
	submitter_id: number | null
	submitter_name: string
	submitter_email: string
	created: string
}

function normalizeSubmission(row: FormSubmission): SubmissionResponse {
	return {
		id: row.id ?? 0,
		task_id: row.task_id ?? 0,
		submitter_id: row.submitter_id ?? null,
		submitter_name: row.submitter_name ?? '',
		submitter_email: row.submitter_email ?? '',
		created: row.created ?? '',
	}
}

export const formKeys = {
	all: ['forms'] as const,
	list: (projectId: number) => ['forms', 'list', projectId] as const,
	options: (projectId: number) => ['forms', 'options', projectId] as const,
	submissionsScope: (projectId: number, formId: number) => ['forms', 'submissions', projectId, formId] as const,
	submissions: (projectId: number, formId: number, page: number) => ['forms', 'submissions', projectId, formId, page] as const,
	public: (token: string) => ['forms', 'public', token] as const,
}

export function formsQuery(projectId: number) {
	return queryOptions({
		queryKey: formKeys.list(projectId),
		queryFn: async ({signal}): Promise<FormResponse[]> => {
			const forms = await fetchAllPages(async page => (await formsList({path: {project: projectId}, query: {page}, signal})).data)
			return forms.map(normalizeForm)
		},
		enabled: projectId > 0,
	})
}

export function formOptionsQuery(projectId: number) {
	return queryOptions({
		queryKey: formKeys.options(projectId),
		queryFn: async ({signal}): Promise<FormOptionsResponse> =>
			normalizeOptions((await formsOptions({path: {project: projectId}, signal})).data),
		enabled: projectId > 0,
		staleTime: 0,
	})
}

export function formSubmissionsQuery(projectId: number, formId: number, page: number) {
	return queryOptions({
		queryKey: formKeys.submissions(projectId, formId, page),
		queryFn: async ({signal}): Promise<Paginated<SubmissionResponse>> => {
			const data = (await formSubmissionsList({path: {project: projectId, form: formId}, query: {page}, signal})).data
			return {...toPaginated(data, page), items: (data.items ?? []).map(normalizeSubmission)}
		},
		enabled: projectId > 0 && formId > 0,
		placeholderData: keepPreviousData,
	})
}

export type SaveFormInput = {projectId: number, formId: number | null, document: FormWritable}

export function saveFormMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, formId, document}: SaveFormInput) => normalizeForm(
			formId
				? (await formsUpdate({path: {project: projectId, form: formId}, body: document})).data
				: (await formsCreate({path: {project: projectId}, body: document})).data,
		),
		onSuccess: (saved, {projectId}, client) => {
			client.setQueryData<FormResponse[]>(formKeys.list(projectId), current => {
				if (!current) return current
				return current.some(form => form.id === saved.id)
					? current.map(form => form.id === saved.id ? saved : form)
					: [...current, saved]
			})
		},
		onSettled: ({projectId}, client) => client.invalidateQueries({queryKey: formKeys.list(projectId), exact: true}),
		successMessage: () => i18n.global.t('forms.saved'),
		// The builder maps validation errors onto its controls.
		toastError: () => false,
	})
}

// The PUT replaces every editable part, so a toggle sends the stored document back.
export function documentOfForm(form: FormResponse, patch: Partial<FormWritable> = {}): FormWritable {
	return {
		title: form.title,
		description: form.description ?? '',
		access: form.access,
		enabled: form.enabled,
		brand_name: form.brand_name ?? '',
		accent_color: form.accent_color ?? '',
		hide_footer: form.hide_footer ?? false,
		fields: form.fields ?? [],
		settings: form.settings,
		...patch,
	}
}

export function toggleFormMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({form, enabled}: {form: FormResponse, enabled: boolean}) => normalizeForm(
			(await formsUpdate({
				path: {project: form.project_id, form: form.id},
				body: documentOfForm(form, {enabled}),
			})).data,
		),
		onSuccess: (saved, {form}, client) => {
			client.setQueryData<FormResponse[]>(formKeys.list(form.project_id),
				current => current?.map(item => item.id === saved.id ? saved : item))
		},
		onSettled: ({form}, client) => client.invalidateQueries({queryKey: formKeys.list(form.project_id), exact: true}),
	})
}

export function deleteFormMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, formId}: {projectId: number, formId: number}) => {
			await formsDelete({path: {project: projectId, form: formId}})
		},
		onSuccess: (_data, {projectId, formId}, client) => {
			client.setQueryData<FormResponse[]>(formKeys.list(projectId), current => current?.filter(form => form.id !== formId))
		},
		onSettled: ({projectId}, client) => client.invalidateQueries({queryKey: formKeys.list(projectId), exact: true}),
		successMessage: () => i18n.global.t('forms.deleted'),
	})
}

export function regenerateTokenMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, formId}: {projectId: number, formId: number}) =>
			(await formsToken({path: {project: projectId, form: formId}})).data.token ?? '',
		onSuccess: (token, {projectId, formId}, client) => {
			client.setQueryData<FormResponse[]>(formKeys.list(projectId),
				current => current?.map(form => form.id === formId ? {...form, token} : form))
		},
		onSettled: ({projectId}, client) => client.invalidateQueries({queryKey: formKeys.list(projectId), exact: true}),
		successMessage: () => i18n.global.t('forms.share.regenerated'),
	})
}

// The logo is its own upload on a saved form; the answer is the form with the new has_brand_logo.
export function uploadFormLogoMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, formId, logo}: {projectId: number, formId: number, logo: File}) =>
			normalizeForm((await formsLogoUpload({path: {project: projectId, form: formId}, body: {logo}})).data),
		onSuccess: (saved, {projectId}, client) => {
			client.setQueryData<FormResponse[]>(formKeys.list(projectId),
				current => current?.map(form => form.id === saved.id ? saved : form))
		},
		onSettled: ({projectId}, client) => client.invalidateQueries({queryKey: formKeys.list(projectId), exact: true}),
		successMessage: () => i18n.global.t('forms.branding.logoSaved'),
		// The builder shows the reason next to the control.
		toastError: () => false,
	})
}

export function deleteFormLogoMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({projectId, formId}: {projectId: number, formId: number}) =>
			normalizeForm((await formsLogoDelete({path: {project: projectId, form: formId}})).data),
		onSuccess: (saved, {projectId}, client) => {
			client.setQueryData<FormResponse[]>(formKeys.list(projectId),
				current => current?.map(form => form.id === saved.id ? saved : form))
		},
		onSettled: ({projectId}, client) => client.invalidateQueries({queryKey: formKeys.list(projectId), exact: true}),
		successMessage: () => i18n.global.t('forms.branding.logoRemoved'),
		// The builder shows the reason next to the control.
		toastError: () => false,
	})
}

export function useUploadFormLogoMutation() {
	return useMutation(uploadFormLogoMutationOptions())
}

export function useDeleteFormLogoMutation() {
	return useMutation(deleteFormLogoMutationOptions())
}

export function useSaveFormMutation() {
	return useMutation(saveFormMutationOptions())
}

export function useToggleFormMutation() {
	return useMutation(toggleFormMutationOptions())
}

export function useDeleteFormMutation() {
	return useMutation(deleteFormMutationOptions())
}

export function useRegenerateTokenMutation() {
	return useMutation(regenerateTokenMutationOptions())
}

// ---- public page -------------------------------------------------------------------------------------------------

// The logo is served by token, without credentials, so an image tag can load it.
export function publicFormLogoUrl(token: string, version = ''): string {
	return `${getApiBaseUrl()}/forms/${encodeURIComponent(token)}/logo${version ? `?v=${encodeURIComponent(version)}` : ''}`
}

export type PublicLoad = {definition: PublicDefinition, authed: boolean}

// The call goes out without credentials first, so a logged-out browser (and a public form) never sends an
// Authorization header. Only a form for signed-in users that answers 25004 is asked again with the session.
function anonymousOptions() {
	return {client: publicClient, baseUrl: getApiBaseUrl()}
}

export async function loadPublicForm(token: string, signal?: AbortSignal): Promise<PublicLoad> {
	try {
		const data = (await formPublicRead({...anonymousOptions(), path: {token}, signal})).data
		return {definition: normalizeDefinition(data), authed: false}
	} catch (cause) {
		const code = (cause as {code?: number} | null)?.code
		if (code === FORM_ERROR.LOGIN_REQUIRED && getToken()) {
			const data = (await formPublicRead({path: {token}, signal})).data
			return {definition: normalizeDefinition(data), authed: true}
		}
		throw cause
	}
}

export function publicFormQuery(token: string) {
	return queryOptions({
		queryKey: formKeys.public(token),
		queryFn: ({signal}) => loadPublicForm(token, signal),
		// A definition is never cached by the server; the page loads it once and keeps what the visitor typed.
		staleTime: Infinity,
		gcTime: 0,
		retry: false,
		refetchOnWindowFocus: false,
		refetchOnReconnect: false,
		// The page explains a form that is closed, switched off, full, missing or private in its own words; a toast
		// would say the same twice. Only a failure nobody expects (network, 5xx) still gets one.
		meta: {handlesError: (cause: unknown) => !['network', 'other'].includes(classifyPublicProblem(cause).kind)},
		enabled: token !== '',
	})
}

export type PublicSubmitResult = {message: string, redirect_url: string, submit_another: boolean}

export async function submitPublicForm(input: {
	token: string
	body: SubmissionBody
	files: File[]
	authed: boolean
}): Promise<PublicSubmitResult> {
	const options = input.authed ? {} : anonymousOptions()
	const response = input.files.length > 0
		? await formPublicSubmitMultipart({
			...options,
			path: {token: input.token},
			body: {data: JSON.stringify(input.body), files: input.files},
		})
		: await formPublicSubmit({...options, path: {token: input.token}, body: input.body})
	const data = response.data
	return {
		message: data.message ?? '',
		redirect_url: data.redirect_url ?? '',
		submit_another: data.submit_another ?? false,
	}
}
