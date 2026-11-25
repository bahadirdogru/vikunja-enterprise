import {test, expect} from '../../support/fixtures'
import type {APIRequestContext, Page} from '@playwright/test'
import {apiV1Url} from '../../support/apiUrl'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {clickTaskAction} from '../../support/taskDetail'
import {createProject, createTask} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
// A script that does not compile is refused on save on purpose.
const REFUSED_SAVE = [/\/projects\/\d+\/automations$/]

async function commentsOf(request: APIRequestContext, token: string, taskId: number): Promise<string[]> {
	const response = await request.get(`${apiV1Url()}/tasks/${taskId}/comments`, {headers: {Authorization: `Bearer ${token}`}})
	return (await response.json()).map((c: {comment: string}) => c.comment.replace(/<[^>]*>/g, ''))
}

async function openNewManualRule(page: Page, projectId: number, title: string, button: string) {
	await page.goto(`/projects/${projectId}/settings/automations`)
	await page.getByRole('button', {name: 'New rule'}).first().click()
	await page.getByLabel('Title', {exact: true}).fill(title)
	await page.getByLabel('Trigger', {exact: true}).selectOption('manual')
	await page.locator('#automation-trigger-options-label').fill(button)
}

async function saveRule(page: Page) {
	const saved = page.waitForResponse(r => /\/projects\/\d+\/automations(\/\d+)?$/.test(r.url()) && ['POST', 'PUT'].includes(r.request().method()))
	await page.getByRole('button', {name: 'Save rule'}).click()
	const response = await saved
	expect(response.status()).toBeLessThan(300)
	return await response.json()
}

async function runFromTask(page: Page, taskId: number, button: string) {
	await page.goto(`/tasks/${taskId}`)
	await clickTaskAction(page, 'Run automation…')
	await page.getByRole('dialog', {name: 'Run automation'}).getByRole('button', {name: button}).click()
	await expect(page.locator('.global-notification .vue-notification.success')).toContainText('was started')
}

async function openRuns(page: Page, projectId: number, rule: string) {
	await page.goto(`/projects/${projectId}/settings/automations`)
	await page.getByRole('button', {name: `Show the runs of "${rule}"`}).click()
}

test.describe('Automation: the script action', () => {
	test('a script built in the editor is tested, runs on a task and its effect and output are visible', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const task = await createTask(apiContext, userToken, project.id, 'Review the invoice')

		await openNewManualRule(page, project.id, 'Stamp with a script', 'Stamp it')
		await page.locator('#automation-add-action').selectOption('script.run')
		await page.locator('#automation-actions-0-params-source').fill('vikunja.tasks.comment(task.id, "Script saw: " + task.title);\nreturn {commented: task.id};')
		const rule = await saveRule(page)
		expect(rule.actions[0]).toMatchObject({type: 'script.run'})

		// Dry run: the script is executed in the sandbox, nothing is written to the task.
		await page.getByRole('button', {name: 'Back to the rules'}).click()
		await page.getByRole('button', {name: 'Stamp with a script'}).first().click()
		const panel = page.getByRole('region', {name: 'Test with a task'})
		const select = panel.getByLabel('Task', {exact: true})
		await select.selectOption(await select.locator('option', {hasText: 'Review the invoice'}).getAttribute('value'))
		await panel.getByRole('button', {name: 'Test', exact: true}).click()
		await expect(panel.getByRole('status')).toBeVisible()
		expect(await commentsOf(apiContext, userToken, task.id)).toEqual([])

		// The real run: a button on the task; the comment lands on it.
		await runFromTask(page, task.id, 'Stamp it')
		await expect.poll(() => commentsOf(apiContext, userToken, task.id), {timeout: 30_000}).toEqual(['Script saw: Review the invoice'])
		await page.reload()
		await expect(page.getByText('Script saw: Review the invoice')).toBeVisible()

		await openRuns(page, project.id, 'Stamp with a script')
		const row = page.locator('.runs__table tbody tr')
		await expect(row).toHaveCount(1)
		await expect(row).toContainText('Success')
		await row.getByRole('button', {name: 'Steps'}).click()
		const detail = page.getByRole('region', {name: 'Run details'})
		await expect(detail).toContainText('Success')
		await expect(detail.getByLabel('Output of the step')).toContainText(`"commented":${task.id}`)
		expect((await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items[0].status).toBe('success')
		watcher.finish()
	})

	test('a script that throws fails the run with its error; after fixing the script a retry finishes it', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const task = await createTask(apiContext, userToken, project.id, 'Review the invoice')

		await openNewManualRule(page, project.id, 'Fragile script', 'Run the script')
		await page.locator('#automation-add-action').selectOption('script.run')
		await page.locator('#automation-actions-0-params-source').fill('throw new Error("the invoice is not ready");')
		const rule = await saveRule(page)

		await runFromTask(page, task.id, 'Run the script')
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items[0]?.status, {timeout: 30_000}).toBe('failed')
		expect(await commentsOf(apiContext, userToken, task.id)).toEqual([])

		await openRuns(page, project.id, 'Fragile script')
		const row = page.locator('.runs__table tbody tr')
		await expect(row).toContainText('Failed')
		await row.getByRole('button', {name: 'Steps'}).click()
		const detail = page.getByRole('region', {name: 'Run details'})
		await expect(detail.getByRole('alert')).toBeVisible()
		await expect(detail).toContainText('the invoice is not ready')

		// Edit the failing action, then retry the same run: it resumes at the step that failed.
		await page.getByRole('button', {name: 'Back to the rules'}).click()
		await page.getByRole('button', {name: 'Fragile script'}).first().click()
		await page.locator('#automation-actions-0-params-source').fill('vikunja.tasks.comment(task.id, "Fixed for " + task.title);\nreturn "done";')
		await saveRule(page)
		await openRuns(page, project.id, 'Fragile script')
		const retried = page.waitForResponse(r => /\/automations\/\d+\/runs\/\d+\/retry$/.test(r.url()) && r.request().method() === 'POST')
		await page.locator('.runs__table tbody tr').getByRole('button', {name: 'Retry'}).click()
		expect((await retried).status()).toBeLessThan(300)
		await expect(page.locator('.global-notification .vue-notification.success')).toContainText('The run was queued again.')
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items[0].status, {timeout: 30_000}).toBe('success')
		expect(await commentsOf(apiContext, userToken, task.id)).toEqual(['Fixed for Review the invoice'])
		// One run, retried: not a second row.
		expect((await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items).toHaveLength(1)
		await expect(page.locator('.runs__table tbody tr')).toContainText('Success')
		await expect(page.locator('.runs__table tbody tr').getByRole('button', {name: 'Retry'})).toHaveCount(0)
		watcher.finish()
	})

	test('a script that does not compile is refused on save and shows where', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, ...REFUSED_SAVE])
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')

		await openNewManualRule(page, project.id, 'Broken script', 'Break it')
		await page.locator('#automation-add-action').selectOption('script.run')
		await page.locator('#automation-actions-0-params-source').fill('var ok = true;\nvar = ;')
		await page.getByRole('button', {name: 'Save rule'}).click()
		await expect(page.locator('#automation-actions-0').getByText(/line 2/i).first()).toBeVisible()
		expect((await api.get(`/projects/${project.id}/automations`)).items).toHaveLength(0)
		watcher.finish()
	})
})

test.describe('Automation: the HTTP request action', () => {
	// The e2e server keeps the SSRF guard on (loopback and private addresses are refused), and there is no public
	// network to rely on. So the request itself cannot succeed here: what is covered is building and saving it, the dry
	// run that sends nothing, and the refusal that fails the run and shows in the history, with its retry.
	test('built in the editor, planned by the dry run, refused by the address guard and shown as a failed run', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const task = await createTask(apiContext, userToken, project.id, 'Review the invoice')

		await openNewManualRule(page, project.id, 'Tell the chat', 'Tell the chat')
		await page.locator('#automation-add-action').selectOption('http.request')
		await page.locator('#automation-actions-0-params-url').fill('http://127.0.0.1:9/hooks/vikunja')
		await page.locator('#automation-actions-0-params-method').selectOption('POST')
		await page.locator('#automation-actions-0-params-body').fill('{"text": "Look at {{task.title}}"}')
		const rule = await saveRule(page)
		expect(rule.actions[0]).toMatchObject({type: 'http.request', params: {url: 'http://127.0.0.1:9/hooks/vikunja', method: 'POST', body: '{"text": "Look at {{task.title}}"}'}})

		// Reopened, the values are the stored ones.
		await page.reload()
		await page.getByRole('button', {name: 'Tell the chat'}).first().click()
		await expect(page.locator('#automation-actions-0-params-url')).toHaveValue('http://127.0.0.1:9/hooks/vikunja')
		await expect(page.locator('#automation-actions-0-params-body')).toHaveValue('{"text": "Look at {{task.title}}"}')

		// Dry run: the request is planned (method, address, size of the filled-in body), nothing is sent.
		await page.getByRole('button', {name: 'Back to the rules'}).click()
		await page.getByRole('button', {name: 'Tell the chat'}).first().click()
		const panel = page.getByRole('region', {name: 'Test with a task'})
		const select = panel.getByLabel('Task', {exact: true})
		await select.selectOption(await select.locator('option', {hasText: 'Review the invoice'}).getAttribute('value'))
		await panel.getByRole('button', {name: 'Test', exact: true}).click()
		await expect(panel.getByRole('status')).toContainText('send POST http://127.0.0.1:9/hooks/vikunja with 38 bytes of body')
		expect((await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items).toHaveLength(0)

		// The real run is refused at once: the address is not allowed.
		await runFromTask(page, task.id, 'Tell the chat')
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items[0]?.status, {timeout: 30_000}).toBe('failed')
		await openRuns(page, project.id, 'Tell the chat')
		const row = page.locator('.runs__table tbody tr')
		await expect(row).toContainText('Failed')
		await row.getByRole('button', {name: 'Steps'}).click()
		const detail = page.getByRole('region', {name: 'Run details'})
		await expect(detail).toContainText('Send an HTTP request')
		await expect(detail.getByRole('alert').or(detail.locator('.help.is-danger')).first()).toBeVisible()

		// Retrying a run whose address stays refused fails again, without a second run.
		const retried = page.waitForResponse(r => /\/automations\/\d+\/runs\/\d+\/retry$/.test(r.url()) && r.request().method() === 'POST')
		await row.getByRole('button', {name: 'Retry'}).click()
		expect((await retried).status()).toBeLessThan(300)
		await expect(page.locator('.global-notification .vue-notification.success')).toContainText('The run was queued again.')
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items[0].status, {timeout: 30_000}).toBe('failed')
		expect((await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items).toHaveLength(1)
		watcher.finish()
	})
})
