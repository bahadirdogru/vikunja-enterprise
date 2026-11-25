import {test, expect} from '../../support/fixtures'
import type {Page} from '@playwright/test'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {createProject, createTask, createUser, getTask, putWorkflow, shareProject, DEFAULT_STATUSES} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
// The done checkbox is refused by the server on purpose in some of these tests.
const REFUSED = [/\/tasks\/\d+$/, /\/tasks\/\d+\/status$/, /\/approvals/]

const toast = (page: Page) => page.locator('.global-notification .vue-notification')
const doneBox = (page: Page, title: string) => page.getByRole('checkbox', {name: `Mark '${title}' as done`})
// The real input is visually hidden, a click goes to the box the user sees.
const tick = (page: Page, title: string) => page.locator('.tasks .task').filter({hasText: title}).locator('.fancy-checkbox').click()

async function statusOf(page: Page, token: string, taskId: number, request: Parameters<typeof apiV2>[0]): Promise<string> {
	void page
	return (await apiV2(request, token).get(`/tasks/${taskId}/status`)).status.title
}

test.describe('The done checkbox in a workflow project', () => {
	test('ticking it moves the task to the done status and reopening it returns it to the initial one', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const project = await createProject(apiContext, userToken, 'Alpha')
		await putWorkflow(apiContext, userToken, project.id, {restrict_transitions: false, statuses: DEFAULT_STATUSES, transitions: []})
		const task = await createTask(apiContext, userToken, project.id, 'Write the report')

		await page.goto(`/projects/${project.id}/${project.views.list}`)
		const patched = page.waitForResponse(r => /\/tasks\/\d+$/.test(r.url()) && r.request().method() === 'PATCH')
		await tick(page, 'Write the report')
		// The list sends a plain task update; the server turns it into the status change.
		expect((await patched).status()).toBe(200)
		await expect(toast(page).filter({hasText: 'done'}).first()).toBeVisible()
		expect(await statusOf(page, userToken, task.id, apiContext)).toBe('Done')
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(true)

		await page.goto(`/tasks/${task.id}`)
		await expect(page.getByRole('button', {name: /^Status: .*\. Change status$/})).toHaveAccessibleName('Status: Done. Change status')

		// Reopening from the task: the initial status again. (The list hides done tasks, so there is no box to untick.)
		const reopened = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('button', {name: 'Mark as undone'}).click()
		expect((await reopened).status()).toBe(200)
		await expect(page.getByRole('button', {name: /^Status: .*\. Change status$/})).toHaveAccessibleName('Status: To do. Change status')
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(false)
		watcher.finish()
	})

	test('the done button on the task goes through the workflow too', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const project = await createProject(apiContext, userToken, 'Alpha')
		await putWorkflow(apiContext, userToken, project.id, {restrict_transitions: false, statuses: DEFAULT_STATUSES, transitions: []})
		const task = await createTask(apiContext, userToken, project.id, 'Write the report')

		await page.goto(`/tasks/${task.id}`)
		const posted = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('button', {name: 'Mark task done!'}).click()
		expect((await posted).status()).toBe(200)
		await expect(page.getByRole('button', {name: /^Status: .*\. Change status$/})).toHaveAccessibleName('Status: Done. Change status')
		expect(await statusOf(page, userToken, task.id, apiContext)).toBe('Done')
		watcher.finish()
	})

	test('a done the workflow does not allow is refused, explained and the checkbox goes back', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, ...REFUSED])
		const project = await createProject(apiContext, userToken, 'Alpha')
		await putWorkflow(apiContext, userToken, project.id, {
			restrict_transitions: true,
			statuses: DEFAULT_STATUSES,
			transitions: [{from_status_id: -1, to_status_id: -2}, {from_status_id: -2, to_status_id: -4}],
		})
		const task = await createTask(apiContext, userToken, project.id, 'Write the report')

		await page.goto(`/projects/${project.id}/${project.views.list}`)
		const refused = page.waitForResponse(r => /\/tasks\/\d+(\/status)?$/.test(r.url()) && ['POST', 'PATCH', 'PUT'].includes(r.request().method()))
		await tick(page, 'Write the report')
		expect((await refused).status()).toBeGreaterThanOrEqual(400)
		await expect(toast(page).filter({hasText: 'not allowed by the workflow'})).toBeVisible()
		await expect(doneBox(page, 'Write the report')).not.toBeChecked()
		expect(await statusOf(page, userToken, task.id, apiContext)).toBe('To do')
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(false)
		watcher.finish()
	})
})

test.describe('Approval and the done checkbox', () => {
	async function setupApproval(apiContext: Parameters<typeof createProject>[0], userToken: string) {
		const project = await createProject(apiContext, userToken, 'Alpha')
		const approver = await createUser(apiContext, 2, 'approver-user')
		await shareProject(apiContext, userToken, project.id, approver.username, 1)
		const workflow = await putWorkflow(apiContext, userToken, project.id, {
			restrict_transitions: false,
			statuses: DEFAULT_STATUSES,
			transitions: [{
				from_status_id: -1,
				to_status_id: -4,
				requires_approval: true,
				approval: {mode: 'any', approvers: [{type: 'user', id: approver.id}]},
			}],
		})
		return {project, approver, workflow}
	}

	test('ticking a done that needs an approval changes nothing and offers the request', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, ...REFUSED])
		const {project, approver} = await setupApproval(apiContext, userToken)
		const task = await createTask(apiContext, userToken, project.id, 'Print the catalogue')

		await page.goto(`/projects/${project.id}/${project.views.list}`)
		await tick(page, 'Print the catalogue')
		const refusal = toast(page).filter({hasText: 'The task was not marked as done: "Done" needs an approval first.'})
		await expect(refusal).toBeVisible()
		await expect(doneBox(page, 'Print the catalogue')).not.toBeChecked()
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(false)
		expect(await statusOf(page, userToken, task.id, apiContext)).toBe('To do')
		expect((await apiV2(apiContext, userToken).get(`/tasks/${task.id}/approvals`)).items).toHaveLength(0)

		// The toast leads to the request, which is the same flow as the status menu.
		const requested = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) && r.request().method() === 'POST')
		await refusal.getByRole('button', {name: 'Request approval'}).click()
		expect((await requested).status()).toBe(202)
		await expect(toast(page).filter({hasText: 'Approval requested'})).toBeVisible()
		const pending = (await apiV2(apiContext, userToken).get(`/tasks/${task.id}/approvals`)).items as {status: string}[]
		expect(pending.map(a => a.status)).toEqual(['pending'])

		// A second tick says the request is already waiting and points to the task.
		await page.reload()
		await tick(page, 'Print the catalogue')
		const waiting = toast(page).filter({hasText: 'An approval for "Done" is already waiting for a decision.'})
		await expect(waiting).toBeVisible()
		await waiting.getByRole('button', {name: 'Open task'}).click()
		await expect(page).toHaveURL(new RegExp(`/tasks/${task.id}$`))
		await expect(page.locator('[data-cy="approval-banner"]')).toContainText(approver.username)
		expect((await apiV2(apiContext, userToken).get(`/tasks/${task.id}/approvals`)).items).toHaveLength(1)
		watcher.finish()
	})

	test('an approver builds the requirement in the workflow editor and the done checkbox then asks for it', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, ...REFUSED])
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const approver = await createUser(apiContext, 2, 'approver-user')
		await shareProject(apiContext, userToken, project.id, approver.username, 1)
		await putWorkflow(apiContext, userToken, project.id, {
			restrict_transitions: true,
			statuses: DEFAULT_STATUSES,
			transitions: [{from_status_id: -1, to_status_id: -2}, {from_status_id: -2, to_status_id: -4}],
		})
		const task = await createTask(apiContext, userToken, project.id, 'Print the catalogue')

		await page.goto(`/projects/${project.id}/settings/workflow`)
		await page.getByRole('button', {name: 'Add transition'}).click()
		const card = page.locator('.transition-card.is-open')
		await card.getByLabel('From status').selectOption({label: 'To do'})
		await card.getByLabel('To status').selectOption({label: 'Done'})

		const approval = card.getByRole('group', {name: 'Approval'})
		await approval.getByLabel('Requires approval').check()
		await approval.getByRole('radio', {name: /All approvers/}).check()
		// Saving without an approver is refused with a reason.
		await page.getByRole('button', {name: 'Save workflow'}).click()
		await expect(card.getByText('Add at least one approver', {exact: false})).toBeVisible()
		await approval.getByLabel('Person').selectOption({label: approver.username})
		await approval.getByRole('button', {name: 'Add', exact: true}).click()
		await expect(approval.locator('.approval-settings__source-label')).toHaveText(approver.username)
		await approval.getByLabel('When rejected, move to').selectOption({label: 'In progress'})
		await approval.getByLabel('Decide within').fill('2d')

		const saved = page.waitForResponse(r => /\/projects\/\d+\/workflow$/.test(r.url()) && r.request().method() === 'PUT')
		await page.getByRole('button', {name: 'Save workflow'}).click()
		expect((await saved).status()).toBeLessThan(300)
		await expect(toast(page).filter({hasText: 'The workflow was saved.'})).toBeVisible()

		const workflow = await api.get(`/projects/${project.id}/workflow`)
		const done = workflow.statuses.find((s: {title: string}) => s.title === 'Done')
		const direct = workflow.transitions.find((t: {to_status_id: number, from_status_id: number}) => t.to_status_id === done.id && t.from_status_id !== workflow.statuses.find((s: {title: string}) => s.title === 'In progress').id)
		expect(direct).toMatchObject({requires_approval: true})
		expect(direct.approval).toMatchObject({mode: 'all', approvers: [{type: 'user', id: approver.id}]})
		expect(direct.approval.rejected_status_id).toBe(workflow.statuses.find((s: {title: string}) => s.title === 'In progress').id)

		// The checkbox on the list takes the rule into effect.
		await page.goto(`/projects/${project.id}/${project.views.list}`)
		await tick(page, 'Print the catalogue')
		await expect(toast(page).filter({hasText: 'needs an approval first'})).toBeVisible()
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(false)
		watcher.finish()
	})
})
