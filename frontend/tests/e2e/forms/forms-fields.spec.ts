import {test, expect} from '../../support/fixtures'
import type {APIRequestContext, Browser, Page} from '@playwright/test'
import {setupApiUrl} from '../../support/authenticateUser'
import {apiV1Url} from '../../support/apiUrl'
import {apiV2, solidPng, watchBrowser} from '../../support/uiCheck'
import {createProject, createUser, getTask, listTasks, pageAs} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:4173'
const THANKS = 'https://thanks.example.test/received'

async function visitor(browser: Browser): Promise<Page> {
	const context = await browser.newContext({baseURL: BASE_URL, serviceWorkers: 'block', viewport: {width: 1280, height: 900}})
	const page = await context.newPage()
	await setupApiUrl(page)
	return page
}

async function createLabel(request: APIRequestContext, token: string, title: string): Promise<number> {
	const response = await request.put(`${apiV1Url()}/labels`, {headers: {Authorization: `Bearer ${token}`}, data: {title, hex_color: 'ff8800'}})
	expect(response.ok(), await response.text()).toBe(true)
	return (await response.json()).id
}

async function attachmentsOf(request: APIRequestContext, token: string, taskId: number): Promise<{file: {name: string, size: number}}[]> {
	const response = await request.get(`${apiV1Url()}/tasks/${taskId}/attachments`, {headers: {Authorization: `Bearer ${token}`}})
	expect(response.ok()).toBe(true)
	return await response.json()
}

async function addField(page: Page, name: string) {
	if (!await page.locator('#fb-add-list').isVisible()) await page.locator('#fb-add-field').click()
	await page.locator('#fb-add-list').getByRole('button', {name: new RegExp(`^${name}`)}).click()
}

test.describe('Forms: priority, labels, files and the redirect', () => {
	test('a signed-in person without access to the project answers priority, labels and a file, and is sent on', async ({apiContext, userToken, browser}) => {
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const bug = await createLabel(apiContext, userToken, 'Bug')
		const question = await createLabel(apiContext, userToken, 'Question')
		await createLabel(apiContext, userToken, 'Not offered')
		const form = await api.post(`/projects/${project.id}/forms`, {
			title: 'Report a problem',
			description: '',
			access: 'users',
			enabled: true,
			fields: [
				{target: 'title', label: 'Subject', required: true},
				{target: 'priority', label: 'How urgent?'},
				{target: 'labels', label: 'What is it about?', options: [bug, question]},
				{target: 'attachments', label: 'Screenshot'},
			],
			settings: {success_message: 'Thanks, we look at it.', redirect_url: THANKS},
		})

		// Anyone with an account: not a member of the project, no share. Without an account: asked to sign in.
		const anon = await visitor(browser)
		const anonWatcher = watchBrowser(anon, [/\/forms\//])
		await anon.goto(`/forms/${form.token}`)
		await expect(anon.getByRole('heading', {name: 'Please sign in'})).toBeVisible()
		anonWatcher.finish()
		await anon.context().close()

		const stranger = await createUser(apiContext, 2, 'stranger-user')
		const page = await pageAs(browser, apiContext, stranger)
		await page.route('https://thanks.example.test/**', route => route.fulfill({contentType: 'text/html', body: '<h1>Elsewhere</h1>'}))
		const watcher = watchBrowser(page, [...NO_WORKFLOW, /\/forms\//])
		await page.goto(`/forms/${form.token}`)
		await expect(page.getByRole('heading', {name: 'Report a problem'})).toBeVisible()

		// The labels on offer are the two chosen ones, the third is not shown.
		await expect(page.getByRole('checkbox')).toHaveCount(2)
		await expect(page.getByRole('checkbox', {name: 'Bug'})).toBeVisible()
		await expect(page.getByRole('checkbox', {name: 'Question'})).toBeVisible()
		await expect(page.getByRole('checkbox', {name: 'Not offered'})).toHaveCount(0)
		await page.getByLabel('Subject').fill('The export button does nothing')
		await page.getByRole('radio', {name: 'High'}).check()
		await page.getByRole('checkbox', {name: 'Bug'}).check()
		await page.locator('input[type="file"]').setInputFiles({name: 'screenshot.png', mimeType: 'image/png', buffer: solidPng(8, 8, [200, 30, 30])})
		await expect(page.getByText('screenshot.png')).toBeVisible()

		const posted = page.waitForResponse(r => /\/forms\/.+\/submissions\/multipart$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('button', {name: 'Send'}).click()
		expect((await posted).status()).toBeLessThan(300)
		await expect(page.getByText('Thanks, we look at it.')).toBeVisible()
		await expect(page.getByText('You will be sent to thanks.example.test in a moment.')).toBeVisible()

		// The task has what was answered.
		await expect.poll(async () => (await listTasks(apiContext, userToken, project.id)).map(t => t.title)).toEqual(['The export button does nothing'])
		const [created] = await listTasks(apiContext, userToken, project.id)
		const stored = await getTask(apiContext, userToken, created!.id)
		expect(stored.priority).toBe(3)
		expect((stored.labels as {id: number}[]).map(l => l.id)).toEqual([bug])
		const attachments = await attachmentsOf(apiContext, userToken, created!.id)
		expect(attachments.map(a => a.file.name)).toEqual(['screenshot.png'])
		expect(attachments[0]!.file.size).toBeGreaterThan(0)

		// The person is sent on: the notice offers it at once, and it also happens by itself.
		await page.getByRole('link', {name: 'Go there now'}).click()
		await expect(page).toHaveURL(THANKS)
		await expect(page.getByRole('heading', {name: 'Elsewhere'})).toBeVisible()
		watcher.finish()
		await page.context().close()
	})

	test('the redirect happens by itself after the notice', async ({apiContext, userToken, browser}) => {
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const form = await api.post(`/projects/${project.id}/forms`, {
			title: 'Quick note',
			description: '',
			access: 'public',
			enabled: true,
			fields: [{target: 'title', label: 'Subject', required: true}, {target: 'priority', label: 'How urgent?'}],
			settings: {redirect_url: THANKS},
		})
		const anon = await visitor(browser)
		await anon.route('https://thanks.example.test/**', route => route.fulfill({contentType: 'text/html', body: '<h1>Elsewhere</h1>'}))
		await anon.goto(`/forms/${form.token}`)
		await anon.getByLabel('Subject').fill('A note')
		await anon.getByRole('radio', {name: 'Urgent'}).check()
		await anon.getByRole('button', {name: 'Send'}).click()
		await expect(anon.getByText('You will be sent to thanks.example.test in a moment.')).toBeVisible()
		// No click: the page moves on after its delay.
		await anon.waitForURL(THANKS, {timeout: 15_000})
		await expect(anon.getByRole('heading', {name: 'Elsewhere'})).toBeVisible()
		const [created] = await listTasks(apiContext, userToken, project.id)
		expect((await getTask(apiContext, userToken, created!.id)).priority).toBe(4)
		await anon.context().close()
	})
})

test.describe('Forms: the builder asks for priority, labels and files', () => {
	test('an admin adds them, the builder checks the redirect and refuses files on a public form', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const bug = await createLabel(apiContext, userToken, 'Bug')
		await createLabel(apiContext, userToken, 'Question')

		await page.goto(`/projects/${project.id}/settings/forms`)
		await page.getByRole('button', {name: 'New form'}).first().click()
		await page.locator('#fb-title').fill('Report a problem')
		await addField(page, 'Priority')
		await addField(page, 'Labels')
		// A shown labels field needs the labels to offer.
		await page.getByRole('button', {name: 'Save form'}).click()
		await expect(page.getByText('Choose at least one label to offer.').first()).toBeVisible()
		await page.locator('.fe.is-open').last().getByRole('group', {name: 'Labels the submitter can choose from'}).getByLabel('Bug').check()
		await addField(page, 'Attachments')

		await page.getByRole('tab', {name: 'Settings'}).click()
		await page.getByRole('radio', {name: /Anyone with an account/}).check()
		await page.locator('#fb-s-redirect_url').fill('ftp://elsewhere.example')
		await expect(page.getByText('Enter a web address starting with http:// or https://.')).toBeVisible()
		await page.locator('#fb-s-redirect_url').fill('https://elsewhere.example/done')
		await expect(page.getByText('Enter a web address starting with http:// or https://.')).toHaveCount(0)

		// Files are not offered to the public on this server: switching to public says so.
		await page.getByRole('radio', {name: /Anyone with the link/}).check()
		await page.getByRole('button', {name: 'Save form'}).click()
		await expect(page.getByText('Attachments are not available on public forms on this server.').first()).toBeVisible()
		// The save is refused and the builder goes back to the fields; back on the settings, "anyone with an account" works.
		await page.getByRole('tab', {name: 'Settings'}).click()
		await page.getByRole('radio', {name: /Anyone with an account/}).check()

		const saved = page.waitForResponse(r => /\/projects\/\d+\/forms$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('button', {name: 'Save form'}).click()
		expect((await saved).status()).toBeLessThan(300)
		const forms = (await api.get(`/projects/${project.id}/forms`)).items as {access: string, settings: {redirect_url: string}, fields: {target: string, options?: number[]}[]}[]
		expect(forms).toHaveLength(1)
		expect(forms[0]!.access).toBe('users')
		expect(forms[0]!.settings.redirect_url).toBe('https://elsewhere.example/done')
		expect(forms[0]!.fields.map(f => f.target)).toEqual(['title', 'priority', 'labels', 'attachments'])
		expect(forms[0]!.fields[2]!.options).toEqual([bug])
		watcher.finish()
	})
})
