import {test, expect} from '../../support/fixtures'
import type {Browser, Page} from '@playwright/test'
import {setupApiUrl} from '../../support/authenticateUser'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {createProject, createUser, listTasks, shareProject, pageAs, getTask} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:4173'

type V2 = ReturnType<typeof apiV2>

// A visitor with no account: a browser context of its own with nothing stored.
async function visitor(browser: Browser): Promise<Page> {
	const context = await browser.newContext({baseURL: BASE_URL, serviceWorkers: 'block', viewport: {width: 1280, height: 900}})
	const page = await context.newPage()
	await setupApiUrl(page)
	return page
}

async function createForm(api: V2, projectId: number, overrides: Record<string, unknown> = {}) {
	return await api.post(`/projects/${projectId}/forms`, {
		title: 'Support request',
		description: '',
		access: 'public',
		enabled: true,
		fields: [
			{target: 'title', label: 'Subject', required: true},
			{target: 'description', label: 'Details'},
		],
		settings: {},
		...overrides,
	})
}

async function addField(page: Page, name: string) {
	if (!await page.locator('#fb-add-list').isVisible()) await page.locator('#fb-add-field').click()
	await page.locator('#fb-add-list').getByRole('button', {name: new RegExp(`^${name}`)}).click()
}

test.describe('Forms: build, publish and submit', () => {
	test('an admin builds a form, publishes it and a visitor without an account submits it', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		await api.post(`/projects/${project.id}/custom-fields`, {title: 'Order number', type: 'text', description: '', required: false, config: {}})

		await page.goto(`/projects/${project.id}/settings/forms`)
		await expect(page.getByRole('heading', {name: 'No forms yet'})).toBeVisible()
		await page.getByRole('button', {name: 'New form'}).first().click()
		await page.locator('#fb-title').fill('Support request')

		// A new form starts with the task title, asked and required. Give its question a better text.
		const title = page.locator('.fe').filter({hasText: 'Task title'})
		await expect(title.getByText('Required', {exact: true})).toBeVisible()
		await title.locator('.fe__toggle').click()
		await title.getByLabel('Question').fill('Subject')
		await addField(page, 'Description')
		await page.locator('.fe.is-open').last().getByLabel('Question').fill('Details')
		await addField(page, 'Order number')
		await page.locator('.fe.is-open').last().getByLabel('Question').fill('Your order number')
		await page.locator('.fe.is-open').last().getByLabel('Required').check()

		await page.getByRole('tab', {name: 'Settings'}).click()
		await page.getByRole('radio', {name: /Anyone with the link/}).check()
		await expect(page.getByText('Anyone with the link can create tasks in this project.')).toBeVisible()
		await page.locator('#fb-s-success_message').fill('We got your request, thank you!')

		const saved = page.waitForResponse(r => /\/projects\/\d+\/forms$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('button', {name: 'Save form'}).click()
		const form = await (await saved).json()
		expect(form).toMatchObject({title: 'Support request', access: 'public', enabled: true})
		expect(form.fields.map((f: {target: string, required: boolean}) => [f.target, f.required])).toEqual([['title', true], ['description', false], [expect.stringMatching(/^cf_\d+$/), true]])

		await page.getByRole('tab', {name: 'Share'}).click()
		const link = await page.locator('#fb-s-link').inputValue()
		expect(link).toContain(`/forms/${form.token}`)

		// The visitor: no account, no token.
		const anon = await visitor(browser)
		const anonWatcher = watchBrowser(anon, [/\/forms\//])
		await anon.goto(`/forms/${form.token}`)
		await expect(anon.getByRole('heading', {name: 'Support request'})).toBeVisible()
		// Required answers are checked before anything is sent.
		let posts = 0
		anon.on('request', r => {
			if (r.method() === 'POST' && /\/forms\/.+\/submissions/.test(r.url())) posts++
		})
		await anon.getByRole('button', {name: 'Send'}).click()
		await expect(anon.getByText('This field is required.')).toHaveCount(2)
		await expect(anon.getByText('Please check 2 answers.')).toBeVisible()
		expect(posts).toBe(0)

		await anon.getByLabel('Subject').fill('The printer is on fire')
		await anon.getByLabel('Details').fill('Second floor, near the kitchen')
		await anon.getByLabel('Your order number').fill('SO-1234')
		await anon.getByRole('button', {name: 'Send'}).click()
		await expect(anon.getByText('We got your request, thank you!')).toBeVisible()
		expect(posts).toBe(1)
		// A public submitter does not learn the id of the task.
		await expect(anon.getByRole('link', {name: /task/i})).toHaveCount(0)

		// The task is in the project, with the answers in its fields.
		await expect.poll(async () => (await listTasks(apiContext, userToken, project.id)).map(t => t.title)).toEqual(['The printer is on fire'])
		const [task] = await listTasks(apiContext, userToken, project.id)
		const stored = await getTask(apiContext, userToken, task!.id)
		expect(String(stored.description)).toContain('Second floor, near the kitchen')
		const values = (await api.get(`/tasks/${task!.id}/custom-field-values`)).items as {value: unknown}[]
		expect(values.map(v => v.value)).toEqual(['SO-1234'])

		// The admin sees the submission in the list and on the task in the browser.
		await page.goto(`/projects/${project.id}/settings/forms`)
		await expect(page.locator('.fi').filter({hasText: 'Support request'})).toContainText('1 submission')
		await page.getByRole('button', {name: 'Show the submissions of "Support request"'}).click()
		await expect(page.getByRole('heading', {name: 'Submissions of "Support request"'}).or(page.getByText('Submissions of "Support request"')).first()).toBeVisible()
		await expect(page.getByText('Anonymous').first()).toBeVisible()
		await page.goto(`/tasks/${task!.id}`)
		await expect(page.getByText('The printer is on fire').first()).toBeVisible()
		await expect(page.getByRole('textbox', {name: 'Order number'})).toHaveValue('SO-1234')
		await expect(page.getByText('Second floor, near the kitchen')).toBeVisible()
		anonWatcher.finish()
		watcher.finish()
		await anon.context().close()
	})

	test('the server refuses what the page lets through: a missing required answer', async ({apiContext, userToken}) => {
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const form = await createForm(api, project.id)
		const response = await apiContext.post(`${api.base}/forms/${form.token}/submissions`, {data: {values: {description: 'No subject'}}})
		expect(response.status()).toBe(422)
		expect(await response.text()).toContain('25006')
		expect(await listTasks(apiContext, userToken, project.id)).toHaveLength(0)
	})
})

test.describe('Forms: who may submit', () => {
	test('a members form asks for a login and works for a member only', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW])
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const form = await createForm(api, project.id, {access: 'members'})

		// Logged out: the sign-in prompt, not the form.
		const anon = await visitor(browser)
		const anonWatcher = watchBrowser(anon, [/\/forms\//])
		await anon.goto(`/forms/${form.token}`)
		await expect(anon.getByRole('heading', {name: 'Please sign in'})).toBeVisible()
		await expect(anon.getByRole('button', {name: 'Sign in'}).or(anon.getByRole('link', {name: 'Sign in'}))).toBeVisible()
		await expect(anon.getByLabel('Subject')).toHaveCount(0)
		const refused = await apiContext.post(`${api.base}/forms/${form.token}/submissions`, {data: {values: {title: 'Sneaky'}}})
		expect(refused.status()).toBe(401)
		expect(await listTasks(apiContext, userToken, project.id)).toHaveLength(0)

		// A signed-in person who cannot see the project is told so.
		const outsider = await createUser(apiContext, 2, 'outsider-user')
		const outsiderPage = await pageAs(browser, apiContext, outsider)
		const outsiderWatcher = watchBrowser(outsiderPage, [/\/forms\//])
		await outsiderPage.goto(`/forms/${form.token}`)
		await expect(outsiderPage.getByRole('heading', {name: 'You cannot use this form'})).toBeVisible()
		await expect(outsiderPage.getByLabel('Subject')).toHaveCount(0)

		// A member can fill it in; the task is created, and a signed-in submitter is told that their account is stored.
		const member = await createUser(apiContext, 3, 'member-user')
		await shareProject(apiContext, userToken, project.id, member.username, 0)
		const memberPage = await pageAs(browser, apiContext, member)
		// The page asks without a session first and with it after the 401 that says "sign in".
		const memberWatcher = watchBrowser(memberPage, [...NO_WORKFLOW, /\/forms\//])
		await memberPage.goto(`/forms/${form.token}`)
		await memberPage.getByLabel('Subject').fill('Request from a member')
		await memberPage.getByRole('button', {name: 'Send'}).click()
		await expect(memberPage.getByText('Thank you, your answer was sent.')).toBeVisible()
		await expect.poll(async () => (await listTasks(apiContext, userToken, project.id)).map(t => t.title)).toEqual(['Request from a member'])
		const submissions = (await api.get(`/projects/${project.id}/forms/${form.id}/submissions`)).items as {submitter_id: number}[]
		expect(submissions[0]!.submitter_id).toBe(member.id)

		anonWatcher.finish()
		outsiderWatcher.finish()
		memberWatcher.finish()
		watcher.finish()
		for (const p of [anon, outsiderPage, memberPage]) await p.context().close()
	})
})

test.describe('Forms: closing', () => {
	test('a form that is switched off, full or past its close date says so', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, /\/forms\//])
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const open = await createForm(api, project.id)
		const full = await createForm(api, project.id, {title: 'Only one', settings: {max_submissions: 1}})
		const closed = await createForm(api, project.id, {title: 'Over', settings: {close_date: new Date(Date.now() - 86400_000).toISOString()}})

		const anon = await visitor(browser)
		const anonWatcher = watchBrowser(anon, [/\/forms\//])

		// Switch the form off in the list, as an admin.
		await page.goto(`/projects/${project.id}/settings/forms`)
		const toggle = page.getByLabel('Accept submissions for the form "Support request"')
		await expect(toggle).toBeChecked()
		const switched = page.waitForResponse(r => /\/projects\/\d+\/forms\/\d+$/.test(r.url()) && ['PUT', 'PATCH'].includes(r.request().method()))
		await toggle.uncheck()
		expect((await switched).status()).toBeLessThan(300)
		await expect(page.locator('.fi').filter({hasText: 'Support request'})).toContainText('Closed')
		await anon.goto(`/forms/${open.token}`)
		await expect(anon.getByText('This form is switched off at the moment.')).toBeVisible()
		await expect(anon.getByRole('button', {name: 'Send'})).toHaveCount(0)

		// Switch it back on: the page works again.
		const back = page.waitForResponse(r => /\/projects\/\d+\/forms\/\d+$/.test(r.url()) && ['PUT', 'PATCH'].includes(r.request().method()))
		await toggle.check()
		await back
		await anon.reload()
		await expect(anon.getByLabel('Subject')).toBeVisible()

		// Past its close date.
		await anon.goto(`/forms/${closed.token}`)
		await expect(anon.getByText('This form is closed and takes no more answers.')).toBeVisible()

		// Full after one submission.
		await anon.goto(`/forms/${full.token}`)
		await anon.getByLabel('Subject').fill('The only one')
		await anon.getByRole('button', {name: 'Send'}).click()
		await expect(anon.getByText('Thank you, your answer was sent.')).toBeVisible()
		await anon.goto(`/forms/${full.token}`)
		await expect(anon.getByText('This form has received the maximum number of answers and takes no more.')).toBeVisible()
		const second = await apiContext.post(`${api.base}/forms/${full.token}/submissions`, {data: {values: {title: 'Too late'}}})
		expect(second.status()).toBe(410)
		expect((await listTasks(apiContext, userToken, project.id)).map(t => t.title)).toEqual(['The only one'])

		// A token that does not exist.
		await anon.goto('/forms/this-token-does-not-exist')
		await expect(anon.getByRole('heading', {name: 'This form does not exist'})).toBeVisible()
		anonWatcher.finish()
		watcher.finish()
		await anon.context().close()
	})
})
