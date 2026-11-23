import {describe, expect, it} from 'vitest'

import optionsFixture from './forms.options.fixture.json'
import {
	addState,
	availablePresets,
	controlId,
	documentFromDraft,
	draftFromForm,
	draftSignature,
	emptyDraft,
	findTarget,
	issuesFromProblem,
	locateIssue,
	mapIssues,
	moveField,
	newField,
	normalizeOptions,
	publicBlockers,
	titleRuleOk,
	unknownPlaceholder,
	validateDraft,
	type FormDraft,
	type FormOptionsResponse,
	type Translate,
} from './forms'
import {ApiError} from '@/client/problem'

const options: FormOptionsResponse = normalizeOptions(optionsFixture as unknown as Parameters<typeof normalizeOptions>[0])
const t: Translate = (key, params) => params && Object.keys(params).length > 0 ? `${key} ${JSON.stringify(params)}` : key

function draftWith(...targets: string[]): FormDraft {
	const draft = emptyDraft()
	draft.title = 'Request'
	draft.fields = targets.map(target => newField(findTarget(options, target)!, t, draft.access))
	return draft
}

describe('options fixture', () => {
	it('comes from the API: built-ins, then the custom fields of the project', () => {
		expect(options.targets.slice(0, 5).map(info => info.target)).toEqual(['title', 'description', 'due_date', 'priority', 'labels'])
		expect(findTarget(options, 'cf_2')).toMatchObject({kind: 'select', title: 'Stage'})
		expect(findTarget(options, 'cf_2')!.options.length).toBe(3)
		expect(findTarget(options, 'labels')).toMatchObject({public_visible: false, public_hidden: true})
		expect(findTarget(options, 'cf_7')).toMatchObject({kind: 'user', public_visible: false})
	})
})

describe('draft and document', () => {
	it('round-trips a stored form', () => {
		const draft = draftWith('title', 'description', 'priority', 'labels')
		draft.fields[1]!.help = 'Tell us more'
		draft.fields[2]!.hidden = true
		draft.fields[2]!.default = 3
		draft.fields[3]!.options = [1, 2]
		draft.settings.close_date = '2027-01-01T10:00:00Z'
		draft.settings.name_mode = 'optional'
		draft.access = 'public'
		const document = documentFromDraft(draft)
		expect(document.fields![3]).toMatchObject({target: 'labels', options: [1, 2]})
		expect(document.fields![2]).toMatchObject({hidden: true, default: 3})
		expect(document.settings).toMatchObject({collect_name: true, name_required: false, collect_email: false})

		const again = draftFromForm({...document, id: 4} as never)
		expect(draftSignature(again)).toBe(draftSignature(draft))
	})

	it('sends no options for a hidden labels field and drops empty defaults', () => {
		const draft = draftWith('title', 'labels')
		draft.fields[1]!.hidden = true
		draft.fields[1]!.options = [1]
		draft.fields[1]!.default = []
		const field = documentFromDraft(draft).fields![1]!
		expect(field.options).toBeUndefined()
		expect(field.default).toBeNull()
	})

	it('only collects submitter details on public forms', () => {
		const draft = draftWith('title')
		draft.settings.name_mode = 'required'
		draft.settings.email_mode = 'optional'
		expect(documentFromDraft(draft).settings).toMatchObject({collect_name: false, name_required: false, collect_email: false})
		draft.access = 'public'
		expect(documentFromDraft(draft).settings).toMatchObject({collect_name: true, name_required: true, collect_email: true, email_required: false})
	})

	it('moves fields and keeps the list when it cannot move', () => {
		const draft = draftWith('title', 'description', 'priority')
		const moved = moveField(draft.fields, 2, -1)
		expect(moved.map(field => field.target)).toEqual(['title', 'priority', 'description'])
		expect(moveField(draft.fields, 0, -1)).toBe(draft.fields)
	})
})

describe('validateDraft', () => {
	const messages = (draft: FormDraft) => validateDraft(draft, options, t).map(issue => `${issue.path}: ${issue.message}`)

	it('accepts a plain form', () => {
		expect(messages(draftWith('title', 'description'))).toEqual([])
	})

	it('needs a title, and the form needs the title field', () => {
		const draft = draftWith('description')
		draft.title = ' '
		expect(messages(draft)).toEqual(['title: forms.errors.titleRequired', 'fields: forms.errors.titleRule'])
	})

	it('enforces the title rule: asked and required, or hidden with a value', () => {
		const draft = draftWith('title')
		draft.fields[0]!.required = false
		expect(titleRuleOk(draft)).toBe(false)
		expect(messages(draft)).toContain('fields[0].default: forms.errors.titleRule')
		draft.fields[0]!.hidden = true
		draft.fields[0]!.default = 'Request from {{submitter.name}}'
		expect(titleRuleOk(draft)).toBe(true)
		expect(messages(draft)).toEqual([])
		draft.fields[0]!.default = 'From {{nobody}}'
		expect(messages(draft)).toEqual(['fields[0].default: forms.errors.placeholder {"name":"nobody"}'])
	})

	it('needs a question for shown fields and a default for hidden required ones', () => {
		const draft = draftWith('title', 'priority')
		draft.fields[1]!.label = ''
		expect(messages(draft)).toEqual(['fields[1].label: forms.errors.labelRequired'])
		draft.fields[1]!.hidden = true
		draft.fields[1]!.required = true
		expect(messages(draft)).toEqual(['fields[1].default: forms.errors.hiddenNeedsDefault'])
		draft.fields[1]!.default = 3
		expect(messages(draft)).toEqual([])
	})

	it('checks defaults with the rules of their target', () => {
		const draft = draftWith('title', 'due_date', 'priority', 'cf_1', 'cf_2', 'cf_5')
		draft.fields[1]!.default = 'next week'
		draft.fields[2]!.default = 9
		draft.fields[3]!.default = 5
		draft.fields[4]!.default = 999
		draft.fields[5]!.default = 7
		expect(messages(draft)).toEqual([
			'fields[1].default: forms.errors.date',
			'fields[2].default: forms.errors.priority {"max":5}',
			'fields[3].default: customFields.errors.value.min {"min":100}',
			'fields[4].default: customFields.errors.value.option',
			'fields[5].default: customFields.errors.value.rating {"max":5}',
		])
		draft.fields[1]!.default = '2027-03-01'
		draft.fields[2]!.default = 4
		draft.fields[3]!.default = 100
		draft.fields[4]!.default = findTarget(options, 'cf_2')!.options[0]!.id
		draft.fields[5]!.default = 5
		expect(messages(draft)).toEqual([])
	})

	it('wants labels to offer, and defaults among them', () => {
		const draft = draftWith('title', 'labels')
		expect(messages(draft)).toEqual(['fields[1].options: forms.errors.labelOptions'])
		draft.fields[1]!.options = [1]
		draft.fields[1]!.default = [2]
		expect(messages(draft)).toEqual(['fields[1].default: forms.errors.labelDefaultInOptions'])
		draft.fields[1]!.default = [1]
		expect(messages(draft)).toEqual([])
	})

	it('knows what a public form may not ask for', () => {
		const draft = draftWith('title', 'labels', 'cf_7', 'attachments')
		draft.fields[1]!.options = [1]
		draft.access = 'public'
		expect(messages(draft)).toEqual([
			'fields[1].target: forms.errors.notPublic',
			'fields[2].target: forms.errors.notPublic',
		])
		expect(publicBlockers(draft, options).map(b => `${b.target}:${b.reason}`)).toEqual(['labels:asked', 'cf_7:asked'])
		// Hiding them with a value makes the form public-safe.
		draft.fields[1]!.hidden = true
		draft.fields[1]!.default = [1]
		draft.fields[2]!.hidden = true
		draft.fields[2]!.default = [1]
		expect(messages(draft)).toEqual([])
		expect(publicBlockers(draft, options)).toEqual([])
		draft.fields[3]!.hidden = true
		expect(messages(draft)).toContain('fields[3].hidden: forms.errors.hiddenNotAllowed')
	})

	it('checks the settings', () => {
		const draft = draftWith('title')
		draft.settings.redirect_url = 'ftp://example.com'
		draft.settings.max_submissions = -1
		expect(messages(draft)).toEqual([
			'settings.redirect_url: forms.errors.redirect',
			'settings.max_submissions: forms.errors.maxSubmissions',
		])
		draft.settings.redirect_url = 'https://example.com/thanks'
		draft.settings.max_submissions = 10
		expect(messages(draft)).toEqual([])
	})

	it('reports targets that left the project and double targets', () => {
		const draft = draftWith('title', 'description')
		draft.fields.push({...draft.fields[0]!, key: 'x'})
		draft.fields.push({...draft.fields[0]!, key: 'y', target: 'cf_99'})
		expect(messages(draft)).toEqual([
			'fields[2].target: forms.errors.targetTwice',
			'fields[3].target: forms.errors.targetGone',
		])
	})
})

describe('add menu', () => {
	it('lists each target at most once and limits public forms', () => {
		const draft = draftWith('title')
		expect(addState(findTarget(options, 'title')!, draft)).toMatchObject({allowed: false, reason: 'used'})
		expect(addState(findTarget(options, 'description')!, draft)).toMatchObject({allowed: true, presetOnly: false})
		draft.access = 'public'
		// Labels cannot be asked on a public form, but can be preset.
		expect(addState(findTarget(options, 'labels')!, draft)).toMatchObject({allowed: true, presetOnly: true})
		const noPreset = {...findTarget(options, 'attachments')!, public_visible: false, public_hidden: false}
		expect(addState(noPreset, draft)).toMatchObject({allowed: false, reason: 'public'})
		expect(newField(findTarget(options, 'labels')!, t, 'public')).toMatchObject({hidden: true, label: ''})
	})
})

describe('unknownPlaceholder', () => {
	it('finds the first placeholder the server would refuse', () => {
		expect(unknownPlaceholder('Hi {{ submitter.name }} on {{date}}')).toBe('')
		expect(unknownPlaceholder('Hi {{owner}}')).toBe('owner')
	})
})

describe('server issues to tab and control', () => {
	it('locates paths', () => {
		expect(locateIssue('fields[2].default')).toEqual({tab: 'fields', fieldIndex: 2, control: 'default'})
		expect(locateIssue('fields[2].default[0]')).toEqual({tab: 'fields', fieldIndex: 2, control: 'default'})
		expect(locateIssue('fields[1].options[3]')).toEqual({tab: 'fields', fieldIndex: 1, control: 'options'})
		expect(locateIssue('fields[0].target')).toEqual({tab: 'fields', fieldIndex: 0, control: 'head'})
		expect(locateIssue('fields')).toEqual({tab: 'fields', fieldIndex: null, control: 'list'})
		expect(locateIssue('title')).toEqual({tab: null, fieldIndex: null, control: 'title'})
		expect(locateIssue('access')).toEqual({tab: 'settings', fieldIndex: null, control: 'access'})
		expect(locateIssue('settings.redirect_url')).toEqual({tab: 'settings', fieldIndex: null, control: 'redirect_url'})
		expect(locateIssue('settings.default_assignee_ids[1]')).toEqual({tab: 'settings', fieldIndex: null, control: 'default_assignee_ids'})
		expect(locateIssue('settings.email_required')).toEqual({tab: 'settings', fieldIndex: null, control: 'email_mode'})
	})

	it('turns a path into the id of a control and counts issues per tab', () => {
		const draft = draftWith('title', 'description')
		const key = draft.fields[1]!.key
		expect(controlId(locateIssue('fields[1].default'), draft)).toBe(`fb-f-${key}-default`)
		expect(controlId(locateIssue('settings.max_submissions'), draft)).toBe('fb-s-max_submissions')
		expect(controlId(locateIssue('title'), draft)).toBe('fb-title')
		const mapped = mapIssues([
			{path: 'fields[1].default', message: 'bad'},
			{path: 'settings.redirect_url', message: 'worse'},
			{path: 'settings.close_date', message: 'worst'},
		], draft)
		expect(mapped.byId.get(`fb-f-${key}-default`)).toEqual(['bad'])
		expect(mapped.tabCounts).toEqual({fields: 1, settings: 2, share: 0})
	})

	it('reads the issues of a 25002 problem', () => {
		const problem = new ApiError({status: 400, detail: 'x', code: 25002, details: {issues: [{path: 'fields[2].default', message: 'must be a string'}, {path: '', message: ''}]}})
		expect(issuesFromProblem(problem)).toEqual([{path: 'fields[2].default', message: 'must be a string'}])
		expect(issuesFromProblem(new Error('x'))).toBeNull()
	})
})

describe('presets', () => {
	const tr: Translate = key => key

	it('are built from what the project offers and pass validation', () => {
		const presets = availablePresets(options, tr)
		expect(presets.map(preset => preset.id)).toEqual(['simple', 'dueDate', 'bug'])
		for (const preset of presets) {
			expect(validateDraft({...preset.draft, title: 'x'}, options, tr)).toEqual([])
			expect(titleRuleOk(preset.draft)).toBe(true)
		}
		expect(presets[2]!.draft.fields.map(field => field.target)).toEqual(['title', 'description', 'priority'])
	})

	it('leaves out targets the project does not have and offers nothing without a title target', () => {
		const withoutDue: FormOptionsResponse = {...options, targets: options.targets.filter(info => info.target !== 'due_date')}
		expect(availablePresets(withoutDue, tr)[1]!.draft.fields.map(field => field.target)).toEqual(['title', 'description'])
		expect(availablePresets({...options, targets: []}, tr)).toEqual([])
	})
})
