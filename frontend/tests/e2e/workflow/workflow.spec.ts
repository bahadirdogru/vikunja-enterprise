import {test, expect} from '../../support/fixtures'
import type {Locator, Page} from '@playwright/test'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {apiV1Url} from '../../support/apiUrl'
import {createProject, createTask, createUser, getTask, pageAs, putWorkflow, shareProject, DEFAULT_STATUSES} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

// A project without a workflow answers 404 on purpose.
const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
// The tests provoke refusals through the API on purpose.
const REFUSED = [/\/tasks\/\d+\/status$/]

function statusTrigger(page: Page): Locator {
	return page.getByRole('button', {name: /^Status: .*\. Change status$/})
}

async function historyOf(page: Page) {
	await page.getByRole('button', {name: 'Status history'}).click()
	return page.getByRole('region', {name: 'Status history'})
}

test.describe('Workflow editor', () => {
	test('an admin builds statuses and transitions and saves them', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')

		await page.goto(`/projects/${project.id}/settings/workflow`)
		await expect(page.getByRole('heading', {name: 'This project has no workflow yet'})).toBeVisible()
		await page.getByRole('button', {name: 'Enable workflow'}).click()
		await expect(page.getByText('This is a starting point.')).toBeVisible()

		// The starting point has three statuses; add a fourth and rename it.
		await expect(page.locator('[data-test^="workflow-status-"]')).toHaveCount(3)
		await page.getByRole('button', {name: 'Add status'}).click()
		await expect(page.locator('[data-test^="workflow-status-"]')).toHaveCount(4)
		await page.getByLabel('Title of status 4').fill('In review')

		// An empty title is refused on save.
		await page.getByLabel('Title of status 4').fill('')
		await page.getByRole('button', {name: 'Save workflow'}).click()
		await expect(page.getByText('Enter a title.')).toBeVisible()
		await page.getByLabel('Title of status 4').fill('In review')

		// Only listed transitions: To do -> In progress, In progress -> In review (with a comment), In review -> Done (with a description).
		await page.getByRole('checkbox', {name: 'Only allow listed transitions'}).check()
		const addTransition = async (from: string, to: string, rules: (card: Locator) => Promise<void> = async () => {}) => {
			await page.getByRole('button', {name: 'Add transition'}).click()
			const card = page.locator('.transition-card.is-open')
			await card.getByLabel('From status').selectOption({label: from})
			await card.getByLabel('To status').selectOption({label: to})
			await rules(card)
		}
		await addTransition('To do', 'In progress')
		await addTransition('In progress', 'In review', async card => {
			await card.getByLabel('Button label').fill('Send to review')
			await card.getByLabel('A comment is required').check()
		})
		await addTransition('In review', 'Done', async card => {
			await card.getByLabel('Description', {exact: true}).check()
		})

		// A pair twice is refused.
		await addTransition('In review', 'Done')
		await page.getByRole('button', {name: 'Save workflow'}).click()
		await expect(page.getByText('This pair of statuses is already listed.').first()).toBeVisible()
		await page.locator('.transition-card.is-open').getByRole('button', {name: 'Remove transition'}).click()

		const saved = page.waitForResponse(r => /\/projects\/\d+\/workflow$/.test(r.url()) && r.request().method() === 'PUT')
		await page.getByRole('button', {name: 'Save workflow'}).click()
		expect((await saved).status()).toBeLessThan(300)
		await expect(page.locator('.global-notification .vue-notification.success')).toContainText('The workflow was saved.')

		const workflow = await api.get(`/projects/${project.id}/workflow`)
		expect(workflow.restrict_transitions).toBe(true)
		expect(workflow.statuses.map((s: {title: string}) => s.title)).toEqual(['To do', 'In progress', 'Done', 'In review'])
		const byTitle = Object.fromEntries(workflow.statuses.map((s: {title: string, id: number}) => [s.title, s.id]))
		expect(workflow.transitions).toHaveLength(3)
		const review = workflow.transitions.find((t: {to_status_id: number}) => t.to_status_id === byTitle['In review'])
		expect(review).toMatchObject({title: 'Send to review', require_comment: true})
		const finish = workflow.transitions.find((t: {to_status_id: number}) => t.to_status_id === byTitle.Done)
		expect(finish.required_fields).toEqual(['description'])

		// The matrix shows the same three pairs.
		await page.reload()
		await page.getByRole('button', {name: 'Matrix'}).click()
		await expect(page.getByRole('grid').or(page.locator('table')).first()).toBeVisible()
		await expect(page.getByRole('checkbox', {name: 'In review to Done'})).toBeChecked()
		await expect(page.getByRole('checkbox', {name: 'Done to To do'})).not.toBeChecked()
		watcher.finish()
	})

	test('a member without admin rights gets no editor', async ({apiContext, userToken, browser}) => {
		const project = await createProject(apiContext, userToken, 'Alpha')
		await putWorkflow(apiContext, userToken, project.id, {statuses: DEFAULT_STATUSES, transitions: [], restrict_transitions: false})
		const member = await createUser(apiContext, 2, 'member-user')
		await shareProject(apiContext, userToken, project.id, member.username, 1)

		const page = await pageAs(browser, apiContext, member)
		await page.goto(`/projects/${project.id}/settings/workflow`)
		await expect(page.getByText('Only project admins can manage the workflow.')).toBeVisible()
		await expect(page.getByRole('button', {name: 'Save workflow'})).toHaveCount(0)

		const refused = await apiContext.put(`${apiV1Url().replace('/api/v1', '/api/v2')}/projects/${project.id}/workflow`, {
			headers: {Authorization: `Bearer ${member.token}`},
			data: {statuses: DEFAULT_STATUSES, transitions: []},
		})
		expect(refused.status()).toBe(403)
		await page.context().close()
	})
})

test.describe('Workflow on the task', () => {
	test('the menu offers only allowed changes, a refused one is refused and history shows the change', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, ...REFUSED])
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const workflow = await putWorkflow(apiContext, userToken, project.id, {
			restrict_transitions: true,
			statuses: DEFAULT_STATUSES,
			transitions: [
				{from_status_id: -1, to_status_id: -2},
				{from_status_id: -2, to_status_id: -3, title: 'Send to review'},
				{from_status_id: -3, to_status_id: -4},
			],
		})
		const done = workflow.statuses.find(s => s.title === 'Done')!
		const task = await createTask(apiContext, userToken, project.id, 'Write the report')

		await page.goto(`/tasks/${task.id}`)
		await expect(statusTrigger(page)).toHaveAccessibleName('Status: To do. Change status')

		// From "To do" only "In progress" is offered.
		await statusTrigger(page).click()
		await expect(page.getByRole('menuitem')).toHaveCount(1)
		await expect(page.getByRole('menuitem', {name: /In progress/})).toBeVisible()
		await expect(page.getByRole('menuitem', {name: /Done/})).toHaveCount(0)

		// The server refuses what the menu does not offer.
		const refused = await apiContext.post(`${api.base}/tasks/${task.id}/status`, {
			headers: {Authorization: `Bearer ${userToken}`},
			data: {to_status_id: done.id},
		})
		expect(refused.status()).toBeGreaterThanOrEqual(400)
		expect(await refused.text()).toContain('21005')
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(false)

		const changed = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('menuitem', {name: /In progress/}).click()
		expect((await changed).status()).toBe(200)
		await expect(page.locator('.global-notification .vue-notification.success')).toContainText('The status is now "In progress".')
		await expect(statusTrigger(page)).toHaveAccessibleName('Status: In progress. Change status')

		// The transition with a label is offered by its label.
		await statusTrigger(page).click()
		await expect(page.getByRole('menuitem', {name: /Send to review/})).toBeVisible()
		await page.keyboard.press('Escape')

		const stored = await api.get(`/tasks/${task.id}/status`)
		expect(stored.status.title).toBe('In progress')

		// Reload: the status is the task's, and the history lists the change.
		await page.reload()
		await expect(statusTrigger(page)).toHaveAccessibleName('Status: In progress. Change status')
		const history = await historyOf(page)
		// The task entered its initial status when it was created; then the change.
		await expect(history.locator('.status-history__entry')).toHaveCount(2)
		const change = history.locator('.status-history__entry').filter({hasText: 'In progress'})
		await expect(change).toHaveCount(1)
		await expect(change).toContainText('To do')
		watcher.finish()
	})

	test('a transition with required fields opens the requirements dialog', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		await putWorkflow(apiContext, userToken, project.id, {
			restrict_transitions: false,
			statuses: DEFAULT_STATUSES,
			transitions: [{from_status_id: -2, to_status_id: -4, require_comment: true, required_fields: ['description']}],
		})
		const task = await createTask(apiContext, userToken, project.id, 'Close the books')
		const workflow = await api.get(`/projects/${project.id}/workflow`)
		const progress = workflow.statuses.find((s: {title: string}) => s.title === 'In progress')
		await api.post(`/tasks/${task.id}/status`, {to_status_id: progress.id})

		await page.goto(`/tasks/${task.id}`)
		await statusTrigger(page).click()
		const item = page.getByRole('menuitem', {name: /Done/})
		await expect(item).toContainText('Needs:')
		await item.click()

		const dialog = page.getByRole('dialog', {name: 'Change status to "Done"'})
		await expect(dialog).toBeVisible()
		await expect(dialog).toContainText('Fill in the following before the task can change status.')

		// Nothing filled in: the dialog stays and says what is missing.
		await dialog.getByRole('button', {name: 'Change status'}).click()
		await expect(dialog.getByRole('alert')).toBeVisible()
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(false)

		await dialog.getByRole('textbox', {name: 'Description'}).fill('All books are balanced.')
		await dialog.getByLabel('Comment').fill('Checked with finance')
		const changed = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) && r.request().method() === 'POST')
		await dialog.getByRole('button', {name: 'Change status'}).click()
		expect((await changed).status()).toBe(200)
		await expect(dialog).toHaveCount(0)

		const stored = await getTask(apiContext, userToken, task.id)
		expect(stored.done).toBe(true)
		expect(String(stored.description)).toContain('All books are balanced.')
		await expect(statusTrigger(page)).toHaveAccessibleName('Status: Done. Change status')

		const history = await historyOf(page)
		await expect(history.locator('.status-history__entry').filter({hasText: 'Checked with finance'})).toContainText('Done')
		watcher.finish()
	})

	test('a reader sees the status but cannot change it', async ({apiContext, userToken, browser}) => {
		const project = await createProject(apiContext, userToken, 'Alpha')
		await putWorkflow(apiContext, userToken, project.id, {restrict_transitions: false, statuses: DEFAULT_STATUSES, transitions: []})
		const task = await createTask(apiContext, userToken, project.id, 'Read only')
		const reader = await createUser(apiContext, 2, 'reader-user')
		await shareProject(apiContext, userToken, project.id, reader.username, 0)

		const page = await pageAs(browser, apiContext, reader)
		await page.goto(`/tasks/${task.id}`)
		await expect(page.locator('.task-view .property-status')).toContainText('To do')
		await expect(statusTrigger(page)).toHaveCount(0)
		await page.context().close()
	})
})

test.describe('Removing a status that has tasks', () => {
	test('asks where the tasks go, moves them and explains it in the history', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const workflow = await putWorkflow(apiContext, userToken, project.id, {restrict_transitions: false, statuses: DEFAULT_STATUSES, transitions: []})
		const idOf = (title: string) => workflow.statuses.find(s => s.title === title)!.id
		const task = await createTask(apiContext, userToken, project.id, 'Stuck in review')
		const other = await createTask(apiContext, userToken, project.id, 'Still to do')
		await api.post(`/tasks/${task.id}/status`, {to_status_id: idOf('In review')})

		await page.goto(`/projects/${project.id}/settings/workflow`)
		await page.getByRole('button', {name: 'Remove the status "In review"'}).click()
		await page.getByRole('button', {name: 'Save workflow'}).click()

		const dialog = page.getByRole('dialog', {name: 'Move tasks to another status'})
		await expect(dialog).toBeVisible()
		await expect(dialog).toContainText('In review')
		await dialog.getByLabel('Move tasks to').selectOption({label: 'In progress'})
		const saved = page.waitForResponse(r => /\/projects\/\d+\/workflow$/.test(r.url()) && r.request().method() === 'PUT')
		await dialog.getByRole('button', {name: 'Move tasks and save'}).click()
		expect((await saved).status()).toBeLessThan(300)
		await expect(dialog).toHaveCount(0)

		expect((await api.get(`/tasks/${task.id}/status`)).status.title).toBe('In progress')
		expect((await api.get(`/tasks/${other.id}/status`)).status.title).toBe('To do')
		const stored = await api.get(`/projects/${project.id}/workflow`)
		expect(stored.statuses.filter((s: {archived: boolean}) => !s.archived).map((s: {title: string}) => s.title)).toEqual(['To do', 'In progress', 'Done'])

		await page.goto(`/tasks/${task.id}`)
		await expect(statusTrigger(page)).toHaveAccessibleName('Status: In progress. Change status')
		const history = await historyOf(page)
		await expect(history).toContainText('Moved because the status "In review" was removed from the workflow.')
		watcher.finish()
	})
})
