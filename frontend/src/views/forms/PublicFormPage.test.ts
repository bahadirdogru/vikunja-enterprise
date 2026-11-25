import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {enableAutoUnmount, flushPromises} from '@vue/test-utils'

const sdk = vi.hoisted(() => ({formPublicRead: vi.fn(), formPublicSubmit: vi.fn(), formPublicSubmitMultipart: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
const auth = vi.hoisted(() => ({getToken: vi.fn()}))
vi.mock('@/helpers/auth', async importOriginal => ({...await importOriginal<typeof import('@/helpers/auth')>(), ...auth}))
const router = vi.hoisted(() => ({push: vi.fn()}))
vi.mock('vue-router', async importOriginal => ({...await importOriginal<typeof import('vue-router')>(), useRouter: () => router}))
// DOMPurify does not run under happy-dom; the sanitizer has its own test.
vi.mock('@/helpers/sanitizeProjectDescription', () => ({sanitizeProjectDescription: (html: string) => `<div data-clean>${html.replace(/<script.*<\/script>/, '')}</div>`}))
const lastVisited = vi.hoisted(() => ({saveLastVisited: vi.fn()}))
vi.mock('@/helpers/saveLastVisited', () => lastVisited)

import PublicFormPage from './PublicFormPage.vue'
import {mountForms} from '@/components/forms/testing'
import {ApiError} from '@/client/problem'

enableAutoUnmount(afterEach)

type Def = Record<string, unknown>

const optionsOf = (...titles: string[]) => titles.map((title, index) => ({id: index + 1, title, color: index === 0 ? '#22aa55' : ''}))

function definition(over: Def = {}): Def {
	return {
		title: 'Cover request',
		description: '<p>Tell us <strong>what</strong> you need.</p><script>alert(1)</script>',
		fields: [
			{target: 'title', label: 'What do you need?', help: 'A few words', required: true, kind: 'text', max_length: 250},
			{target: 'description', label: 'Details', help: '', required: false, kind: 'rich_text', max_length: 20},
			{target: 'due_date', label: 'Needed by', help: '', required: false, kind: 'date'},
			{target: 'priority', label: 'How urgent?', help: '', required: false, kind: 'priority'},
			{target: 'cf_1', label: 'Print run', help: '', required: true, kind: 'number', config: {min: 100, max: 5000}},
			{target: 'cf_2', label: 'Stage', help: '', required: false, kind: 'select', options: optionsOf('Editor', 'Design', 'Print')},
			{target: 'cf_3', label: 'Format', help: '', required: false, kind: 'select', options: optionsOf('A', 'B', 'C', 'D', 'E', 'F', 'G')},
			{target: 'cf_4', label: 'Rush job', help: '', required: false, kind: 'checkbox'},
			{target: 'cf_5', label: 'Quality', help: '', required: false, kind: 'rating', config: {max_rating: 4}},
			{target: 'cf_6', label: 'Progress', help: '', required: false, kind: 'progress'},
			{target: 'cf_7', label: 'Markets', help: '', required: false, kind: 'multi_select', options: optionsOf('DE', 'FR')},
			{target: 'cf_8', label: 'Sites', help: '', required: false, kind: 'multi_select', options: optionsOf('1', '2', '3', '4', '5', '6', '7')},
			{target: 'cf_9', label: 'Site', help: '', required: false, kind: 'url'},
			{target: 'cf_10', label: 'When', help: '', required: false, kind: 'date'},
		],
		success: {message: '', redirect_url: '', submit_another: true},
		submitter: {collect_name: true, name_required: false, collect_email: true, email_required: true},
		...over,
	}
}

function mountPage(token = 'tok') {
	return mountForms(PublicFormPage, {
		props: {token},
		global: {stubs: {Logo: true}},
	})
}

async function loaded(def: Def = definition(), token = 'tok') {
	sdk.formPublicRead.mockResolvedValue({data: def})
	const mounted = mountPage(token)
	await flushPromises()
	return mounted
}

function problem(status: number, code: number, details?: object) {
	return new ApiError({status, code, detail: 'x', ...(details ? {details} : {})})
}

async function fillRequired(wrapper: Awaited<ReturnType<typeof loaded>>['wrapper']) {
	await wrapper.get('#ff-title').setValue('Laptop')
	await wrapper.get('#ff-cf_1').setValue('1200')
	await wrapper.get('#fs-email-input').setValue('ada@example.com')
}

describe('PublicFormPage: rendering', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		auth.getToken.mockReset().mockReturnValue(null)
		router.push.mockReset()
		lastVisited.saveLastVisited.mockReset()
	})

	it('shows a skeleton while loading', () => {
		sdk.formPublicRead.mockReturnValue(new Promise(() => {}))
		const {wrapper} = mountPage()
		expect(wrapper.find('.fp__skeleton').exists()).toBe(true)
		expect(wrapper.find('form').exists()).toBe(false)
	})

	it('renders every kind with the right control, label, help and required marks', async () => {
		const {wrapper} = await loaded()
		expect(document.title).toBe('Cover request')
		expect(wrapper.get('h1').text()).toBe('Cover request')
		// The description is sanitized.
		expect(wrapper.get('.fp__description [data-clean]').html()).toContain('<strong>what</strong>')
		expect(wrapper.find('.fp__description script').exists()).toBe(false)

		const title = wrapper.get('#ff-title')
		expect(title.attributes('type')).toBe('text')
		expect(title.attributes('aria-describedby')).toContain('ff-title-help')
		expect(wrapper.get('label[for=ff-title]').text()).toContain('What do you need?')
		expect(wrapper.get('label[for=ff-title]').text()).toContain('(required)')
		expect(wrapper.get('label[for=ff-title] [aria-hidden=true]').text()).toBe('*')
		expect(wrapper.get('#ff-title-help').text()).toBe('A few words')

		expect(wrapper.get('#ff-description').element.tagName).toBe('TEXTAREA')
		expect(wrapper.get('#ff-due_date').attributes('type')).toBe('date')
		expect(wrapper.get('#ff-cf_10').attributes('type')).toBe('datetime-local')
		expect(wrapper.get('#ff-cf_1').attributes('type')).toBe('number')
		expect(wrapper.get('#ff-cf_1').attributes('min')).toBe('100')
		expect(wrapper.get('#ff-cf_9').attributes('type')).toBe('url')
		expect(wrapper.get('#ff-cf_6').attributes('type')).toBe('range')

		// At most six options: radio and checkbox groups with a legend; more: selects.
		const stage = wrapper.get('[data-field=cf_2]')
		expect(stage.element.tagName).toBe('FIELDSET')
		expect(stage.get('legend').text()).toContain('Stage')
		expect(stage.findAll('input[type=radio]').length).toBe(3)
		expect(wrapper.get('[data-field=priority]').findAll('input[type=radio]').length).toBe(5)
		expect(wrapper.get('[data-field=cf_7]').findAll('input[type=checkbox]').length).toBe(2)
		expect(wrapper.get('#ff-cf_3').element.tagName).toBe('SELECT')
		expect(wrapper.get('#ff-cf_8').attributes('multiple')).toBeDefined()
		// Rating: one radio per star, with text for screen readers.
		const stars = wrapper.get('[data-field=cf_5]').findAll('input[type=radio]')
		expect(stars.length).toBe(4)
		expect(wrapper.get('[data-field=cf_5]').text()).toContain('3 of 4')
		expect(wrapper.get('#ff-cf_4').attributes('type')).toBe('checkbox')
	})

	it('shows a character counter near the limit of a long text', async () => {
		const {wrapper} = await loaded()
		expect(wrapper.find('#ff-description-count').exists()).toBe(false)
		await wrapper.get('#ff-description').setValue('x'.repeat(17))
		expect(wrapper.get('#ff-description-count').text()).toBe('17 / 20')
		expect(wrapper.get('#ff-description').attributes('maxlength')).toBe('20')
	})

	it('has a honeypot nobody can reach', async () => {
		const {wrapper} = await loaded()
		const trap = wrapper.get('[data-testid=honeypot]')
		expect(trap.attributes('tabindex')).toBe('-1')
		expect(trap.attributes('autocomplete')).toBe('off')
		expect(trap.element.closest('[aria-hidden=true]')).not.toBeNull()
	})

	it('asks for the submitter only when the form collects it', async () => {
		const withoutSubmitter = await loaded(definition({submitter: {collect_name: false, collect_email: false}}))
		expect(withoutSubmitter.wrapper.find('#fs-name-input').exists()).toBe(false)
		const {wrapper} = await loaded()
		expect(wrapper.get('label[for=fs-email-input]').text()).toContain('(required)')
		expect(wrapper.get('label[for=fs-name-input]').text()).not.toContain('(required)')
	})

	it('tells the submitter of a members form that name and account address are stored', async () => {
		const members = await loaded(definition({submitter: {collect_name: false, collect_email: false, stores_account: true}}))
		expect(members.wrapper.get('[data-test=stores-account]').text()).toContain('e-mail address of your account')
		const others = await loaded(definition({submitter: {collect_name: false, collect_email: false, stores_account: false}}))
		expect(others.wrapper.find('[data-test=stores-account]').exists()).toBe(false)
	})

	it('makes no request but the definition, and shares no credentials', async () => {
		const {wrapper, queryClient} = await loaded()
		expect(sdk.formPublicRead).toHaveBeenCalledTimes(1)
		const call = sdk.formPublicRead.mock.calls[0]![0]
		expect(call.path).toEqual({token: 'tok'})
		// The public client has no Authorization interceptor.
		expect(call.client).toBeDefined()
		expect(queryClient.getQueryCache().getAll().map(query => query.queryKey)).toEqual([['forms', 'public', 'tok']])
		await fillRequired(wrapper)
		sdk.formPublicSubmit.mockResolvedValue({data: {message: 'Thanks'}})
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(Object.values(sdk).reduce((n, mock) => n + mock.mock.calls.length, 0)).toBe(2)
		expect(queryClient.getQueryCache().getAll().length).toBe(1)
		expect(auth.getToken).not.toHaveBeenCalled()
	})
})

describe('PublicFormPage: submitting', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		auth.getToken.mockReset().mockReturnValue(null)
	})

	it('validates before it sends and moves focus to the first invalid field', async () => {
		const {wrapper} = await loaded()
		await wrapper.get('#ff-cf_1').setValue('5')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(sdk.formPublicSubmit).not.toHaveBeenCalled()
		expect(wrapper.get('#ff-title-error').text()).toBe('This field is required.')
		expect(wrapper.get('#ff-cf_1-error').text()).toContain('at least 100')
		expect(wrapper.get('#fs-email-input').attributes('aria-invalid')).toBe('true')
		expect(wrapper.get('#ff-title').attributes('aria-invalid')).toBe('true')
		expect(wrapper.get('#ff-title').attributes('aria-describedby')).toContain('ff-title-error')
		expect(document.activeElement?.id).toBe('ff-title')
		expect(wrapper.get('[role=status][aria-live=assertive]').text()).toBe('Please check 3 answers.')
	})

	it('serializes the answers by kind', async () => {
		const {wrapper} = await loaded()
		sdk.formPublicSubmit.mockResolvedValue({data: {message: 'Thanks'}})
		await fillRequired(wrapper)
		await wrapper.get('#ff-description').setValue('Hi')
		await wrapper.get('#ff-due_date').setValue('2027-03-05')
		await wrapper.get('[data-field=priority] input[value="3"]').setValue(true)
		await wrapper.get('[data-field=cf_2] input[type=radio]').setValue(true)
		await wrapper.get('#ff-cf_3').setValue('2')
		await wrapper.get('#ff-cf_4').setValue(true)
		await wrapper.get('[data-field=cf_5] input[type=radio]:nth-of-type(3)').setValue(true)
		await wrapper.get('#ff-cf_6').setValue('40')
		for (const input of wrapper.findAll('[data-field=cf_7] input')) await input.setValue(true)
		await wrapper.get('#ff-cf_9').setValue('https://example.com')
		await wrapper.get('#fs-name-input').setValue('Ada')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		const call = sdk.formPublicSubmit.mock.calls[0]![0]
		expect(call.path).toEqual({token: 'tok'})
		expect(call.body.honeypot).toBe('')
		expect(call.body.submitter).toEqual({name: 'Ada', email: 'ada@example.com'})
		expect(call.body.values).toMatchObject({
			title: 'Laptop', description: 'Hi', priority: 3, cf_1: 1200, cf_2: 1, cf_3: 2, cf_4: true, cf_5: 3, cf_6: 40,
			cf_7: [1, 2], cf_9: 'https://example.com',
		})
		expect(typeof call.body.values.due_date).toBe('string')
		expect(call.body.values).not.toHaveProperty('cf_10')
		expect(sdk.formPublicSubmitMultipart).not.toHaveBeenCalled()
	})

	it('uses the multipart endpoint only when there are files', async () => {
		const withFiles = definition({
			fields: [
				{target: 'title', label: 'Title', help: '', required: true, kind: 'text', max_length: 250},
				{target: 'attachments', label: 'Files', help: '', required: false, kind: 'files'},
			],
			submitter: {},
			attachments: {max_files: 2, max_file_size: 1000},
		})
		const {wrapper} = await loaded(withFiles)
		sdk.formPublicSubmit.mockResolvedValue({data: {message: 'Thanks'}})
		sdk.formPublicSubmitMultipart.mockResolvedValue({data: {message: 'Thanks'}})
		await wrapper.get('#ff-title').setValue('No files')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(sdk.formPublicSubmit).toHaveBeenCalledTimes(1)
		expect(sdk.formPublicSubmitMultipart).not.toHaveBeenCalled()

		// Back to the form, now with a file.
		await wrapper.get('.fp__done button').trigger('click')
		await wrapper.get('#ff-title').setValue('With a file')
		const input = wrapper.get('#ff-attachments')
		const file = new File(['hello'], 'note.txt', {type: 'text/plain'})
		Object.defineProperty(input.element, 'files', {value: [file], configurable: true})
		await input.trigger('change')
		expect(wrapper.get('.ff__file-list').text()).toContain('note.txt')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(sdk.formPublicSubmit).toHaveBeenCalledTimes(1)
		const call = sdk.formPublicSubmitMultipart.mock.calls[0]![0]
		expect(JSON.parse(call.body.data).values).toEqual({title: 'With a file'})
		expect(call.body.files).toEqual([file])
	})

	it('removes a chosen file again', async () => {
		const {wrapper} = await loaded(definition({
			fields: [{target: 'attachments', label: 'Files', help: '', required: false, kind: 'files'}],
			submitter: {},
			attachments: {max_files: 2, max_file_size: 1000},
		}))
		const input = wrapper.get('#ff-attachments')
		Object.defineProperty(input.element, 'files', {value: [new File(['a'], 'a.txt'), new File(['b'], 'b.txt')], configurable: true})
		await input.trigger('change')
		expect(wrapper.findAll('.ff__file-list li').length).toBe(2)
		await wrapper.get('.ff__file-list button').trigger('click')
		expect(wrapper.findAll('.ff__file-list li').map(li => li.text())).toEqual([expect.stringContaining('b.txt')])
		expect(wrapper.get('.ff__hint').text()).toContain('Up to 2 files')
	})

	it('maps a 422 to the fields, focuses the first, announces it and keeps the values', async () => {
		const {wrapper} = await loaded()
		await fillRequired(wrapper)
		sdk.formPublicSubmit.mockRejectedValue(problem(422, 25006, {errors: [
			{field: 'cf_1', code: 'invalid', message: 'The value must be at most 5000.'},
			{field: 'submitter.email', code: 'invalid', message: 'Not an e-mail address.'},
		]}))
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(wrapper.get('#ff-cf_1-error').text()).toBe('The value must be at most 5000.')
		expect(wrapper.get('#fs-email-error').text()).toBe('Not an e-mail address.')
		expect(wrapper.get('#ff-cf_1').attributes('aria-invalid')).toBe('true')
		expect(document.activeElement?.id).toBe('ff-cf_1')
		expect(wrapper.get('[aria-live=assertive]').text()).toBe('Please check 2 answers.')
		expect((wrapper.get('#ff-title').element as HTMLInputElement).value).toBe('Laptop')
		// Editing a field clears its message.
		await wrapper.get('#ff-cf_1').setValue('4000')
		expect(wrapper.find('#ff-cf_1-error').exists()).toBe(false)
		expect(wrapper.find('#fs-email-error').exists()).toBe(true)
	})

	it('disables the button and ignores a second submit while sending', async () => {
		const {wrapper} = await loaded()
		await fillRequired(wrapper)
		let finish: (value: unknown) => void = () => {}
		sdk.formPublicSubmit.mockReturnValue(new Promise(resolve => { finish = resolve }))
		await wrapper.get('form').trigger('submit')
		await wrapper.get('form').trigger('submit')
		expect(sdk.formPublicSubmit).toHaveBeenCalledTimes(1)
		const button = wrapper.get('button[type=submit]')
		expect(button.attributes('disabled')).toBeDefined()
		expect(button.attributes('aria-disabled')).toBe('true')
		finish({data: {message: 'Done'}})
		await flushPromises()
		expect(wrapper.find('form').exists()).toBe(false)
	})

	it('shows the message, offers another response and resets the form', async () => {
		const {wrapper} = await loaded()
		await fillRequired(wrapper)
		sdk.formPublicSubmit.mockResolvedValue({data: {message: 'Thanks, Ada.', redirect_url: '', submit_another: true}})
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(wrapper.get('.fp__done').text()).toContain('Thanks, Ada.')
		expect(document.activeElement).toBe(wrapper.get('.fp__done').element)
		await wrapper.get('.fp__done button').trigger('click')
		expect((wrapper.get('#ff-title').element as HTMLInputElement).value).toBe('')
	})

	it('falls back to a default message and offers nothing more when the form does not', async () => {
		const {wrapper} = await loaded(definition({success: {message: '', redirect_url: '', submit_another: false}}))
		await fillRequired(wrapper)
		sdk.formPublicSubmit.mockResolvedValue({data: {}})
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(wrapper.get('.fp__done').text()).toContain('Thank you, your answer was sent.')
		expect(wrapper.find('.fp__done button').exists()).toBe(false)
	})

	describe('redirect', () => {
		const assign = vi.fn()
		beforeEach(() => {
			vi.useFakeTimers()
			assign.mockReset()
			vi.spyOn(window, 'location', 'get').mockReturnValue({...window.location, assign} as unknown as Location)
		})
		afterEach(() => {
			vi.useRealTimers()
			vi.restoreAllMocks()
		})

		it('shows the host and goes there after a short notice', async () => {
			const {wrapper} = await loaded()
			await fillRequired(wrapper)
			sdk.formPublicSubmit.mockResolvedValue({data: {message: 'Thanks', redirect_url: 'https://example.org/thanks?x=1', submit_another: true}})
			await wrapper.get('form').trigger('submit')
			await flushPromises()
			expect(wrapper.get('.fp__redirect').text()).toContain('example.org')
			expect(wrapper.find('.fp__done button').exists()).toBe(false)
			expect(assign).not.toHaveBeenCalled()
			await vi.advanceTimersByTimeAsync(4100)
			expect(assign).toHaveBeenCalledWith('https://example.org/thanks?x=1')
		})

		it('never follows anything but http and https', async () => {
			const {wrapper} = await loaded()
			await fillRequired(wrapper)
			sdk.formPublicSubmit.mockResolvedValue({data: {message: 'Thanks', redirect_url: 'javascript:alert(1)', submit_another: false}})
			await wrapper.get('form').trigger('submit')
			await flushPromises()
			expect(wrapper.find('.fp__redirect').exists()).toBe(false)
			await vi.advanceTimersByTimeAsync(5000)
			expect(assign).not.toHaveBeenCalled()
		})
	})
})

describe('PublicFormPage: states', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		auth.getToken.mockReset().mockReturnValue(null)
		router.push.mockReset()
		lastVisited.saveLastVisited.mockReset()
	})

	async function failing(error: unknown) {
		sdk.formPublicRead.mockRejectedValue(error)
		const mounted = mountPage()
		await flushPromises()
		return mounted.wrapper
	}

	it('404: the form does not exist', async () => {
		const wrapper = await failing(problem(404, 25001))
		expect(wrapper.get('h1').text()).toBe('This form does not exist')
		expect(wrapper.find('form').exists()).toBe(false)
	})

	it.each([
		['closed', 'closed and takes no more answers'],
		['full', 'maximum number of answers'],
		['disabled', 'switched off at the moment'],
	])('410 %s: a friendly text under the title of the form', async (reason, text) => {
		const wrapper = await failing(problem(410, 25003, {reason, title: 'Cover request'}))
		expect(wrapper.get('h1').text()).toBe('Cover request')
		expect(wrapper.get('.fp__state').text()).toContain(text)
		expect(document.title).toBe('Cover request')
	})

	it('401: asks to sign in and comes back to the form afterwards', async () => {
		const wrapper = await failing(problem(401, 25004))
		expect(wrapper.get('h1').text()).toBe('Please sign in')
		await wrapper.get('.fp__state button').trigger('click')
		expect(lastVisited.saveLastVisited).toHaveBeenCalledWith('forms.public', {token: 'tok'}, {})
		expect(router.push).toHaveBeenCalledWith({name: 'user.login'})
		// Logged out, no credentials were sent.
		expect(sdk.formPublicRead).toHaveBeenCalledTimes(1)
	})

	it('401 with a session: asks again as the signed-in user and hides the submitter block', async () => {
		auth.getToken.mockReturnValue('jwt')
		sdk.formPublicRead
			.mockRejectedValueOnce(problem(401, 25004))
			.mockResolvedValueOnce({data: definition()})
		const {wrapper} = mountPage()
		await flushPromises()
		expect(sdk.formPublicRead).toHaveBeenCalledTimes(2)
		expect(sdk.formPublicRead.mock.calls[0]![0].client).toBeDefined()
		expect(sdk.formPublicRead.mock.calls[1]![0].client).toBeUndefined()
		expect(wrapper.find('form').exists()).toBe(true)
		expect(wrapper.find('#fs-email-input').exists()).toBe(false)
		sdk.formPublicSubmit.mockResolvedValue({data: {message: 'ok'}})
		await wrapper.get('#ff-title').setValue('x')
		await wrapper.get('#ff-cf_1').setValue('200')
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		// The submission carries the session too.
		expect(sdk.formPublicSubmit.mock.calls[0]![0].client).toBeUndefined()
	})

	it('403: not allowed', async () => {
		const wrapper = await failing(problem(403, 25005))
		expect(wrapper.get('h1').text()).toBe('You cannot use this form')
	})

	it('429 on load: says when to try again', async () => {
		const wrapper = await failing(problem(429, 25007, {retry_after: 600}))
		expect(wrapper.get('.fp__state').text()).toContain('try again in 10 minutes')
	})

	it('429 on submit: keeps the form and its answers and says when to try again', async () => {
		const {wrapper} = await loaded()
		await fillRequired(wrapper)
		sdk.formPublicSubmit.mockRejectedValue(problem(429, 25007, {retry_after: 61}))
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(wrapper.get('[role=alert]').text()).toContain('try again in 2 minutes')
		expect((wrapper.get('#ff-title').element as HTMLInputElement).value).toBe('Laptop')
		expect(wrapper.get('button[type=submit]').attributes('disabled')).toBeUndefined()
	})

	it('network error: a message and a retry that loads the form', async () => {
		sdk.formPublicRead.mockRejectedValueOnce(new TypeError('Failed to fetch')).mockResolvedValueOnce({data: definition()})
		const {wrapper} = mountPage()
		await flushPromises()
		expect(wrapper.get('.fp__state').text()).toContain('could not be reached')
		await wrapper.get('.fp__state button').trigger('click')
		await flushPromises()
		expect(wrapper.find('form').exists()).toBe(true)
	})

	it('410 after the form was open: replaces the page with the state', async () => {
		const {wrapper} = await loaded()
		await fillRequired(wrapper)
		sdk.formPublicSubmit.mockRejectedValue(problem(410, 25003, {reason: 'full', title: 'Cover request'}))
		await wrapper.get('form').trigger('submit')
		await flushPromises()
		expect(wrapper.get('.fp__state').text()).toContain('maximum number of answers')
	})

	it('sets noindex while it is shown', async () => {
		const {wrapper} = await loaded()
		expect(document.head.querySelector('meta[name=robots]')?.getAttribute('content')).toContain('noindex')
		wrapper.unmount()
		expect(document.head.querySelector('meta[name=robots]')).toBeNull()
	})
})
