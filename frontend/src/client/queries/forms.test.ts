import {beforeEach, describe, expect, it, vi} from 'vitest'
import {QueryClient} from '@tanstack/vue-query'

const sdk = vi.hoisted(() => ({
	formPublicRead: vi.fn(), formPublicSubmit: vi.fn(), formPublicSubmitMultipart: vi.fn(), formsList: vi.fn(), formsToken: vi.fn(),
	formsLogoDelete: vi.fn(), formsLogoUpload: vi.fn(), formSubmissionsList: vi.fn(), formsCreate: vi.fn(), formsDelete: vi.fn(),
	formsOptions: vi.fn(), formsUpdate: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
const auth = vi.hoisted(() => ({getToken: vi.fn()}))
vi.mock('@/helpers/auth', async importOriginal => ({...await importOriginal<typeof import('@/helpers/auth')>(), ...auth}))
vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn(), getErrorText: () => 'error'}))
vi.mock('@/i18n', () => ({i18n: {global: {t: (key: string) => key}}}))

import {
	formKeys, formsQuery, loadPublicForm, normalizeForm, publicFormLogoUrl, publicFormQuery, regenerateTokenMutationOptions, submitPublicForm,
} from './forms'
import {publicClient} from '@/client/publicClient'
import {ApiError} from '@/client/problem'

const definition = {title: 'Cover', fields: [{target: 'title', label: 'Title'}]}

beforeEach(() => {
	Object.values(sdk).forEach(mock => mock.mockReset())
	auth.getToken.mockReset().mockReturnValue(null)
})

describe('normalizeForm', () => {
	it('fills every field a partial answer lacks', () => {
		expect(normalizeForm({} as never)).toMatchObject({
			id: 0, project_id: 0, title: '', token: '', access: 'members', enabled: false, disabled_reason: '', submission_count: 0, fields: [],
		})
	})

	it('keeps what the server sent, including the nested settings', () => {
		const form = normalizeForm({id: 5, title: 'T', access: 'public', enabled: true, settings: {max_submissions: 3}, fields: [{target: 'title'}]} as never)
		expect(form).toMatchObject({id: 5, access: 'public', enabled: true, settings: {max_submissions: 3}, fields: [{target: 'title'}]})
	})
})

describe('publicFormLogoUrl', () => {
	it('encodes the token and appends the version only when there is one', () => {
		expect(publicFormLogoUrl('a/b c')).toBe('/api/v2/forms/a%2Fb%20c/logo')
		expect(publicFormLogoUrl('tok', '2026-11-20T09:00:00Z&x=1')).toBe('/api/v2/forms/tok/logo?v=2026-11-20T09%3A00%3A00Z%26x%3D1')
	})
})

describe('loadPublicForm', () => {
	it('reads the definition anonymously, through the client that never sends a session', async () => {
		sdk.formPublicRead.mockResolvedValue({data: definition})
		const result = await loadPublicForm('tok')
		expect(result.authed).toBe(false)
		expect(result.definition.title).toBe('Cover')
		expect(sdk.formPublicRead).toHaveBeenCalledTimes(1)
		expect(sdk.formPublicRead.mock.calls[0]![0]).toMatchObject({path: {token: 'tok'}, client: publicClient, baseUrl: '/api/v2'})
	})

	it('asks again with the session of a signed-in visitor when the form is for signed-in users (25004)', async () => {
		auth.getToken.mockReturnValue('jwt')
		sdk.formPublicRead
			.mockRejectedValueOnce(new ApiError({status: 401, code: 25004, detail: 'login'}))
			.mockResolvedValueOnce({data: definition})
		const result = await loadPublicForm('tok')
		expect(result.authed).toBe(true)
		expect(sdk.formPublicRead).toHaveBeenCalledTimes(2)
		// the second call is the SDK default client, which carries the session
		expect(sdk.formPublicRead.mock.calls[1]![0]).not.toHaveProperty('client')
	})

	it('does not retry without a session, and passes the 25004 on for the login prompt', async () => {
		const cause = new ApiError({status: 401, code: 25004, detail: 'login'})
		sdk.formPublicRead.mockRejectedValue(cause)
		await expect(loadPublicForm('tok')).rejects.toBe(cause)
		expect(sdk.formPublicRead).toHaveBeenCalledTimes(1)
	})

	it.each([25001, 25003, 25008, 500])('does not retry on error %i even with a session', async code => {
		auth.getToken.mockReturnValue('jwt')
		const cause = new ApiError({status: 404, code, detail: 'x'})
		sdk.formPublicRead.mockRejectedValue(cause)
		await expect(loadPublicForm('tok')).rejects.toBe(cause)
		expect(sdk.formPublicRead).toHaveBeenCalledTimes(1)
	})
})

describe('the error toast of the public form query', () => {
	const handles = (cause: unknown) => (publicFormQuery('tok').meta!.handlesError as (cause: unknown) => boolean)(cause)

	it.each([
		['closed, switched off, full or expired', new ApiError({status: 410, code: 25003, detail: 'x', details: {reason: 'full'}})],
		['missing', new ApiError({status: 404, code: 25001, detail: 'x'})],
		['private', new ApiError({status: 401, code: 25004, detail: 'x'})],
	])('is left to the page for a form that is %s', (_, cause) => {
		expect(handles(cause)).toBe(true)
	})

	it.each([
		['a server error', new ApiError({status: 500, detail: 'boom'})],
		['a network failure', new TypeError('Failed to fetch')],
	])('is still shown for %s', (_, cause) => {
		expect(handles(cause)).toBe(false)
	})
})

describe('submitPublicForm', () => {
	const body = {values: {title: 'Hi'}, name: 'Ada', email: 'a@b.c'} as never

	it('sends JSON without files, anonymously', async () => {
		sdk.formPublicSubmit.mockResolvedValue({data: {message: 'Thanks', redirect_url: '', submit_another: true}})
		const result = await submitPublicForm({token: 'tok', body, files: [], authed: false})
		expect(sdk.formPublicSubmitMultipart).not.toHaveBeenCalled()
		expect(sdk.formPublicSubmit.mock.calls[0]![0]).toMatchObject({path: {token: 'tok'}, body, client: publicClient})
		expect(result).toEqual({message: 'Thanks', redirect_url: '', submit_another: true})
	})

	it('sends multipart with the body as a JSON string when there are files', async () => {
		sdk.formPublicSubmitMultipart.mockResolvedValue({data: {}})
		const file = new File(['x'], 'a.txt')
		const result = await submitPublicForm({token: 'tok', body, files: [file], authed: false})
		expect(sdk.formPublicSubmit).not.toHaveBeenCalled()
		const call = sdk.formPublicSubmitMultipart.mock.calls[0]![0]
		expect(call.body.files).toEqual([file])
		expect(JSON.parse(call.body.data)).toEqual(body)
		expect(result).toEqual({message: '', redirect_url: '', submit_another: false})
	})

	it('uses the session client for a signed-in submission', async () => {
		sdk.formPublicSubmit.mockResolvedValue({data: {}})
		await submitPublicForm({token: 'tok', body, files: [], authed: true})
		expect(sdk.formPublicSubmit.mock.calls[0]![0]).not.toHaveProperty('client')
	})
})

describe('the list of forms of a project', () => {
	it('reads every page', async () => {
		sdk.formsList
			.mockResolvedValueOnce({data: {items: [{id: 1}], total_pages: 2}})
			.mockResolvedValueOnce({data: {items: [{id: 2}], total_pages: 2}})
		const client = new QueryClient()
		const forms = await client.fetchQuery(formsQuery(3))
		expect(forms.map(f => f.id)).toEqual([1, 2])
		expect(sdk.formsList.mock.calls.map(call => call[0].query)).toEqual([{page: 1}, {page: 2}])
	})

	it('does not run for an unsaved project', () => {
		expect(formsQuery(0).enabled).toBe(false)
	})
})

describe('regenerating the token', () => {
	it('puts the new token into the cached list of the form and nothing else', async () => {
		sdk.formsToken.mockResolvedValue({data: {token: 'new'}})
		sdk.formsList.mockResolvedValue({data: {items: [], total_pages: 1}})
		const client = new QueryClient()
		client.setQueryData(formKeys.list(3), [{id: 5, token: 'old'}, {id: 6, token: 'other'}])
		await client.getMutationCache().build(client, regenerateTokenMutationOptions()).execute({projectId: 3, formId: 5})
		expect(sdk.formsToken).toHaveBeenCalledWith({path: {project: 3, form: 5}})
		// nobody observes the list, so the invalidation after the write does not refetch and the cache write stays
		expect(client.getQueryData(formKeys.list(3))).toEqual([{id: 5, token: 'new'}, {id: 6, token: 'other'}])
		expect(sdk.formsList).not.toHaveBeenCalled()
	})
})
