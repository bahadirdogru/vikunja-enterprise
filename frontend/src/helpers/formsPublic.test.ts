import {describe, expect, it} from 'vitest'

import optionsFixture from './forms.options.fixture.json'
import {
	accentStyle,
	classifyPublicProblem,
	definitionFromDraft,
	initialAnswers,
	inputToIso,
	isoToInput,
	normalizeDefinition,
	redirectTarget,
	serializeValues,
	buildSubmission,
	serverErrorsFrom,
	validateAnswers,
	type PublicDefinition,
	type PublicField,
} from './formsPublic'
import {emptyDraft, findTarget, newField, normalizeOptions, type Translate} from './forms'
import {ApiError} from '@/client/problem'

const t: Translate = (key, params) => params && Object.keys(params).length > 0 ? `${key} ${JSON.stringify(params)}` : key
const options = normalizeOptions(optionsFixture as unknown as Parameters<typeof normalizeOptions>[0])

function field(over: Partial<PublicField> & {target: string, kind: string}): PublicField {
	return {label: over.target, help: '', required: false, default: null, max_length: 0, config: null, options: [], ...over}
}

function definition(...fields: PublicField[]): PublicDefinition {
	return {
		title: 'Form', description: '', fields,
		success: {message: '', redirect_url: '', submit_another: false},
		submitter: {collect_name: false, name_required: false, collect_email: false, email_required: false, stores_account: false},
		attachments: null,
		branding: {name: '', accent_color: '', has_logo: false, hide_footer: false},
	}
}

describe('normalizeDefinition', () => {
	it('fills the gaps of the API shape', () => {
		const result = normalizeDefinition({
			title: 'Cover request',
			fields: [{target: 'title', label: 'What?', required: true, kind: 'text', max_length: 250}],
			success: {message: 'Thanks', submit_another: true},
		} as never)
		expect(result.fields[0]).toMatchObject({target: 'title', default: null, options: [], config: null, help: ''})
		expect(result.success).toEqual({message: 'Thanks', redirect_url: '', submit_another: true})
		expect(result.submitter.collect_name).toBe(false)
		expect(result.attachments).toBeNull()
		expect(result.branding).toEqual({name: '', accent_color: '', has_logo: false, hide_footer: false})
	})
})

describe('value serialization per kind', () => {
	const select = field({target: 'cf_2', kind: 'select', options: [{id: 10, title: 'A', color: ''}, {id: 11, title: 'B', color: ''}]})
	const fields = [
		field({target: 'title', kind: 'text'}),
		field({target: 'description', kind: 'rich_text'}),
		field({target: 'due_date', kind: 'date'}),
		field({target: 'priority', kind: 'priority'}),
		field({target: 'labels', kind: 'labels', options: [{id: 1, title: 'Urgent', color: ''}]}),
		field({target: 'cf_1', kind: 'number'}),
		select,
		field({target: 'cf_3', kind: 'date'}),
		field({target: 'cf_4', kind: 'checkbox'}),
		field({target: 'cf_5', kind: 'rating', config: {max_rating: 5}}),
		field({target: 'cf_6', kind: 'long_text'}),
		field({target: 'cf_8', kind: 'multi_select', options: select.options}),
		field({target: 'cf_9', kind: 'progress'}),
		field({target: 'cf_10', kind: 'url'}),
		field({target: 'cf_11', kind: 'email'}),
	]

	it('sends only what was answered, typed by kind', () => {
		const def = definition(...fields)
		const answers = initialAnswers(def)
		expect(serializeValues(def, answers)).toEqual({cf_4: false})
		Object.assign(answers.values, {
			title: '  Laptop ', description: 'Details', due_date: '2027-03-05', priority: 3, labels: [1],
			cf_1: '12,5', cf_2: 11, cf_3: '2027-03-05T14:30', cf_4: true, cf_5: 4, cf_6: 'long', cf_8: [10, 11],
			cf_9: 40, cf_10: 'https://example.com', cf_11: 'a@b.co',
		})
		const body = serializeValues(def, answers)
		expect(body).toMatchObject({
			title: 'Laptop', description: 'Details', priority: 3, labels: [1], cf_1: 12.5, cf_2: 11, cf_4: true,
			cf_5: 4, cf_6: 'long', cf_8: [10, 11], cf_9: 40, cf_10: 'https://example.com', cf_11: 'a@b.co',
		})
		// A date alone is noon in the viewer's zone, a date with a time is that time.
		expect(new Date(body.due_date as string).getHours()).toBe(12)
		expect(new Date(body.cf_3 as string).getHours()).toBe(14)
		expect(new Date(body.cf_3 as string).getMinutes()).toBe(30)
	})

	it('starts from the defaults the definition carries', () => {
		const def = definition(
			field({target: 'title', kind: 'text', default: 'Hello'}),
			field({target: 'cf_1', kind: 'number', default: 7}),
			field({target: 'cf_4', kind: 'checkbox', default: true}),
			field({target: 'due_date', kind: 'date', default: '2027-03-05T12:00:00Z'}),
		)
		const values = initialAnswers(def).values
		expect(values.title).toBe('Hello')
		expect(values.cf_1).toBe('7')
		expect(values.cf_4).toBe(true)
		expect(typeof values.due_date).toBe('string')
		expect(values.due_date).toMatch(/^2027-03-0[456]$/)
	})

	it('builds the submitter block only for what the form collects', () => {
		const def = definition(field({target: 'title', kind: 'text'}))
		def.submitter = {collect_name: true, name_required: false, collect_email: false, email_required: false, stores_account: false}
		const answers = initialAnswers(def)
		answers.values.title = 'x'
		answers.name = ' Ada '
		answers.email = 'ignored@example.com'
		expect(buildSubmission(def, answers, '')).toEqual({values: {title: 'x'}, submitter: {name: 'Ada'}, honeypot: ''})
	})

	it('converts date inputs both ways', () => {
		expect(isoToInput('2027-03-05', true)).toBe('2027-03-05')
		expect(isoToInput('2027-03-05', false)).toBe('2027-03-05T00:00')
		expect(inputToIso('nonsense')).toBe('')
		expect(isoToInput(inputToIso('2027-03-05T09:15'), false)).toBe('2027-03-05T09:15')
	})
})

describe('client validation per kind', () => {
	const run = (f: PublicField, value: unknown) => {
		const def = definition(f)
		const answers = initialAnswers(def)
		answers.values[f.target] = value
		return validateAnswers(def, answers, t)
	}

	it('requires what is required, per kind of emptiness', () => {
		for (const [kind, empty] of [['text', ''], ['long_text', '  '], ['number', ''], ['select', null], ['multi_select', []], ['rating', null], ['checkbox', false], ['date', ''], ['priority', null]] as const) {
			expect(run(field({target: 'x', kind, required: true}), empty), kind).toEqual({x: 'forms.public.errors.required'})
		}
		expect(run(field({target: 'x', kind: 'text'}), '')).toEqual({})
	})

	it('checks text lengths, urls and e-mail addresses', () => {
		expect(run(field({target: 'x', kind: 'text', max_length: 3}), 'abcd')).toEqual({x: 'forms.errors.tooLong {"max":3}'})
		expect(run(field({target: 'x', kind: 'url'}), 'not a url')).toEqual({x: 'customFields.errors.value.url'})
		expect(run(field({target: 'x', kind: 'url'}), 'https://example.com')).toEqual({})
		expect(run(field({target: 'x', kind: 'email'}), 'nope')).toEqual({x: 'customFields.errors.value.email'})
	})

	it('checks numbers against min, max and their own shape', () => {
		const number = field({target: 'x', kind: 'number', config: {min: 10, max: 20}})
		expect(run(number, '5')).toEqual({x: 'customFields.errors.value.min {"min":10}'})
		expect(run(number, '25')).toEqual({x: 'customFields.errors.value.max {"max":20}'})
		expect(run(number, 'abc')).toEqual({x: 'customFields.errors.value.number'})
		expect(run(number, '15')).toEqual({})
	})

	it('checks options, ratings, progress and dates', () => {
		const options = [{id: 1, title: 'A', color: ''}]
		expect(run(field({target: 'x', kind: 'select', options}), 2)).toEqual({x: 'customFields.errors.value.option'})
		expect(run(field({target: 'x', kind: 'select', options}), 1)).toEqual({})
		expect(run(field({target: 'x', kind: 'labels', options}), [1, 2])).toEqual({x: 'customFields.errors.value.option'})
		expect(run(field({target: 'x', kind: 'rating', config: {max_rating: 3}}), 4)).toEqual({x: 'customFields.errors.value.rating {"max":3}'})
		expect(run(field({target: 'x', kind: 'progress'}), 101)).toEqual({x: 'customFields.errors.value.progress'})
		expect(run(field({target: 'x', kind: 'priority'}), 7)).toEqual({x: 'forms.errors.priority {"max":5}'})
		expect(run(field({target: 'x', kind: 'date'}), 'garbage')).toEqual({x: 'customFields.errors.value.date'})
	})

	it('checks the submitter block and the files', () => {
		const def = definition(field({target: 'attachments', kind: 'files'}))
		def.submitter = {collect_name: true, name_required: true, collect_email: true, email_required: false, stores_account: false}
		def.attachments = {max_files: 1, max_file_size: 10}
		const answers = initialAnswers(def)
		answers.email = 'bad'
		answers.files = [new File(['x'.repeat(20)], 'big.txt'), new File(['y'], 'b.txt')]
		expect(validateAnswers(def, answers, t)).toEqual({
			'submitter.name': 'forms.public.errors.required',
			'submitter.email': 'customFields.errors.value.email',
			attachments: 'forms.public.errors.tooManyFiles {"max":1}',
		})
		answers.files = [new File(['x'.repeat(20)], 'big.txt')]
		expect(validateAnswers(def, answers, t).attachments).toContain('forms.public.errors.fileTooLarge')
	})
})

describe('server errors and failures', () => {
	it('reads the per-field errors of a 25006 answer, first message per field', () => {
		const problem = new ApiError({status: 422, detail: 'x', code: 25006, details: {errors: [
			{field: 'cf_1', code: 'invalid', message: 'too small'},
			{field: 'cf_1', code: 'invalid', message: 'second'},
			{field: 'submitter.email', code: 'invalid', message: 'bad e-mail'},
		]}})
		expect(serverErrorsFrom(problem)).toEqual({cf_1: 'too small', 'submitter.email': 'bad e-mail'})
		expect(serverErrorsFrom(new ApiError({status: 422, detail: 'x'}))).toBeNull()
	})

	it('says the answer errors of the server in the language of the page, by their code', () => {
		const problem = new ApiError({status: 422, detail: 'x', code: 25006, details: {errors: [
			{field: 'cf_1', code: 'invalid', message: 'Expected a date.'},
			{field: 'title', code: 'a_code_of_a_newer_server', message: 'The English text'},
		]}})
		expect(serverErrorsFrom(problem, t)).toEqual({cf_1: 'forms.public.errors.server.invalid', title: 'The English text'})
	})

	it('says too many files, with the limit, when the answer of the files has too many values', () => {
		const problem = new ApiError({status: 422, detail: 'x', code: 25006, details: {errors: [{field: 'attachments', code: 'too_many', message: 'Too many values.'}]}})
		expect(serverErrorsFrom(problem, t, 3)).toEqual({attachments: 'forms.public.errors.tooManyFiles {"max":3}'})
		expect(serverErrorsFrom(problem, t)).toEqual({attachments: 'forms.public.errors.server.too_many_files'})
	})

	it('tells apart what the page has to show', () => {
		const make = (status: number, extra: object = {}) => new ApiError({status, detail: 'x', ...extra})
		expect(classifyPublicProblem(make(404, {code: 25001}))).toEqual({kind: 'notFound'})
		expect(classifyPublicProblem(make(410, {code: 25003, details: {reason: 'full', title: 'Cover'}})))
			.toEqual({kind: 'unavailable', reason: 'full', title: 'Cover'})
		expect(classifyPublicProblem(make(401, {code: 25004}))).toEqual({kind: 'login'})
		expect(classifyPublicProblem(make(403, {code: 25005}))).toEqual({kind: 'forbidden'})
		expect(classifyPublicProblem(make(429, {code: 25007, details: {retry_after: 90}}))).toEqual({kind: 'rate', retryAfter: 90})
		expect(classifyPublicProblem(make(429))).toEqual({kind: 'rate', retryAfter: 0})
		expect(classifyPublicProblem(new TypeError('Failed to fetch'))).toEqual({kind: 'network'})
		expect(classifyPublicProblem(make(500))).toMatchObject({kind: 'other'})
	})

	it('only follows http and https redirects', () => {
		expect(redirectTarget('https://example.com/thanks')?.host).toBe('example.com')
		expect(redirectTarget('javascript:alert(1)')).toBeNull()
		expect(redirectTarget('/relative')).toBeNull()
	})
})

describe('definitionFromDraft (the preview)', () => {
	it('shows what the public page would show', () => {
		const draft = emptyDraft()
		draft.title = 'Cover request'
		draft.access = 'public'
		draft.settings.name_mode = 'required'
		const fields = ['title', 'priority', 'labels', 'cf_2', 'attachments'].map(target => newField(findTarget(options, target)!, t, 'members'))
		fields[1]!.hidden = true
		fields[1]!.default = 3
		fields[2]!.options = [1]
		fields[0]!.default = 'From {{submitter.name}}'
		draft.fields = fields
		const result = definitionFromDraft(draft, options, [{id: 1, title: 'Urgent', color: 'ff0000'}], t)
		expect(result.fields.map(f => f.target)).toEqual(['title', 'labels', 'cf_2', 'attachments'])
		// A default with a placeholder is not exposed, like on the server.
		expect(result.fields[0]!.default).toBeNull()
		expect(result.fields[1]!.options).toEqual([{id: 1, title: 'Urgent', color: 'ff0000'}])
		expect(result.fields[2]!.options.length).toBe(3)
		expect(result.submitter).toMatchObject({collect_name: true, name_required: true, collect_email: false})
		expect(result.attachments).not.toBeNull()
	})
})

describe('accentStyle', () => {
	it('turns a hex color into primary variables with readable text', () => {
		expect(accentStyle('#ff0000')).toMatchObject({'--primary-h': '0deg', '--primary-s': '100%', '--primary-l': '50%', '--fp-button-text': '#000000'})
		expect(accentStyle('#ffff00')['--fp-button-text']).toBe('#000000')
		expect(accentStyle('#1973ff')['--fp-button-text']).toBe('#000000')
		expect(accentStyle('#b3261e')['--fp-button-text']).toBe('#ffffff')
	})

	it('emits the resolved colors, because the root variables are already resolved for descendants', () => {
		expect(accentStyle('#ff0000')).toMatchObject({
			'--primary': 'hsla(0deg, 100%, 50%, 1)',
			'--primary-dark': 'hsla(0deg, 100%, 38%, 1)',
			'--primary-light': 'hsla(0deg, 100%, 96%, 1)',
		})
	})

	it('darkens a pale accent for links on the white card and lightens a dark one for the dark card', () => {
		const lightness = (value: string | undefined) => Number(/(\d+)%, 1\)/.exec(value ?? '')?.[1])
		const pale = accentStyle('#ffe066')
		expect(lightness(pale['--fp-link'])).toBeLessThan(lightness(pale['--primary']))
		expect(lightness(pale['--fp-link-dark'])).toBe(lightness(pale['--primary']))
		const dark = accentStyle('#4a0d0a')
		expect(lightness(dark['--fp-link'])).toBe(lightness(dark['--primary']))
		expect(lightness(dark['--fp-link-dark'])).toBeGreaterThan(lightness(dark['--primary']))
		expect(accentStyle('#ff0000')['--fp-link-hover']).toMatch(/, 0\.75\)$/)
	})

	it('ignores empty and malformed values', () => {
		expect(accentStyle('')).toEqual({})
		expect(accentStyle('red')).toEqual({})
	})
})
