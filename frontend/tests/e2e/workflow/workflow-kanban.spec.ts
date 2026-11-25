import {test, expect} from '../../support/fixtures'
import type {APIRequestContext, Locator, Page} from '@playwright/test'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {apiV1Url} from '../../support/apiUrl'
import {createProject, createTask, createUser, getTask, pageAs, putWorkflow, shareProject, DEFAULT_STATUSES} from '../../support/forkModules'

// Four columns and the sidebar need a wide screen: a drop target outside the window cannot be reached.
test.use({viewport: {width: 1920, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
// The drops below provoke refusals on purpose.
const REFUSED = [/\/tasks\/\d+\/status$/, /\/views\/\d+\/buckets\/-\d+\/tasks/, /\/buckets\/-\d+\/tasks/]

// A drop is driven with the mouse like the upstream kanban spec does it (hover, down, move in steps, up): the list of
// an empty column has no height of its own, so the pointer is put into the column below its header.
async function drop(page: Page, source: Locator, target: Locator) {
	await source.hover()
	await page.mouse.down()
	const box = (await target.boundingBox())!
	await page.mouse.move(box.x + box.width / 2, box.y + 60, {steps: 10})
	await page.mouse.move(box.x + box.width / 2 + 2, box.y + 62, {steps: 3})
	await page.mouse.up()
}

function column(page: Page, status: string): Locator {
	return page.locator('.kanban .bucket').filter({has: page.locator('.status-title', {hasText: status})})
}

function card(page: Page, title: string): Locator {
	return page.locator('.kanban .bucket .tasks .task').filter({hasText: title})
}

async function createStatusBoard(request: APIRequestContext, token: string, projectId: number): Promise<number> {
	const response = await request.put(`${apiV1Url()}/projects/${projectId}/views`, {
		headers: {Authorization: `Bearer ${token}`},
		data: {title: 'By status', view_kind: 'kanban', bucket_configuration_mode: 'status'},
	})
	expect(response.ok(), await response.text()).toBe(true)
	return (await response.json()).id
}

async function statusOf(request: APIRequestContext, token: string, taskId: number): Promise<string> {
	return (await apiV2(request, token).get(`/tasks/${taskId}/status`)).status.title
}

test.describe('Kanban in workflow status mode', () => {
	test('the columns are the statuses and a drop makes the transition, a forbidden one is reverted', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, ...REFUSED])
		const project = await createProject(apiContext, userToken, 'Alpha')
		await putWorkflow(apiContext, userToken, project.id, {
			restrict_transitions: true,
			statuses: DEFAULT_STATUSES,
			transitions: [
				{from_status_id: -1, to_status_id: -2},
				{from_status_id: -2, to_status_id: -3},
				{from_status_id: -3, to_status_id: -4},
			],
		})
		const view = await createStatusBoard(apiContext, userToken, project.id)
		const task = await createTask(apiContext, userToken, project.id, 'Write the report')

		await page.goto(`/projects/${project.id}/${view}`)
		await expect(page.locator('.kanban .bucket .status-title')).toHaveText(['To do', 'In progress', 'In review', 'Done'].map(s => new RegExp(s)))
		await expect(column(page, 'To do')).toContainText('Write the report')

		// An allowed drop: To do -> In progress.
		const moved = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) || /\/buckets\/-\d+\/tasks/.test(r.url()))
		await drop(page, card(page, 'Write the report'), column(page, 'In progress'))
		expect((await moved).status()).toBeLessThan(300)
		await expect(column(page, 'In progress')).toContainText('Write the report')
		await expect(column(page, 'To do')).not.toContainText('Write the report')
		expect(await statusOf(apiContext, userToken, task.id)).toBe('In progress')

		// A forbidden drop: In progress -> Done is not listed. The card goes back and nothing changed.
		const refused = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) || /\/buckets\/-\d+\/tasks/.test(r.url()))
		await drop(page, card(page, 'Write the report'), column(page, 'Done'))
		expect((await refused).status()).toBeGreaterThanOrEqual(400)
		await expect(page.locator('.global-notification .vue-notification.error')).toContainText('This status change is not allowed by the workflow.')
		await expect(column(page, 'In progress')).toContainText('Write the report')
		await expect(column(page, 'Done')).not.toContainText('Write the report')
		expect(await statusOf(apiContext, userToken, task.id)).toBe('In progress')
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(false)

		// What the server holds is what a reload shows.
		await page.reload()
		await expect(column(page, 'In progress')).toContainText('Write the report')
		watcher.finish()
	})

	test('a drop into a column whose transition has requirements opens the dialog and finishes after it', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, ...REFUSED])
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const workflow = await putWorkflow(apiContext, userToken, project.id, {
			restrict_transitions: false,
			statuses: DEFAULT_STATUSES,
			transitions: [{from_status_id: -2, to_status_id: -4, require_comment: true, required_fields: ['description']}],
		})
		const task = await createTask(apiContext, userToken, project.id, 'Close the books')
		await api.post(`/tasks/${task.id}/status`, {to_status_id: workflow.statuses.find(s => s.title === 'In progress')!.id})
		const view = await createStatusBoard(apiContext, userToken, project.id)

		await page.goto(`/projects/${project.id}/${view}`)
		await expect(column(page, 'In progress')).toContainText('Close the books')
		await drop(page, card(page, 'Close the books'), column(page, 'Done'))

		const dialog = page.getByRole('dialog', {name: 'Change status to "Done"'})
		await expect(dialog).toBeVisible()
		// Behind the dialog the card has not moved.
		expect(await statusOf(apiContext, userToken, task.id)).toBe('In progress')
		await expect(column(page, 'In progress')).toContainText('Close the books')

		await dialog.getByRole('textbox', {name: 'Description'}).fill('All books are balanced.')
		await dialog.getByLabel('Comment').fill('Checked with finance')
		await dialog.getByRole('button', {name: 'Change status'}).click()
		await expect(dialog).toHaveCount(0)
		await expect(column(page, 'Done')).toContainText('Close the books')
		await expect(column(page, 'In progress')).not.toContainText('Close the books')
		expect(await statusOf(apiContext, userToken, task.id)).toBe('Done')
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(true)
		watcher.finish()
	})

	test('a drop into a column that needs an approval asks for it and leaves the card where it is', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const approver = await createUser(apiContext, 2, 'approver-user')
		await shareProject(apiContext, userToken, project.id, approver.username, 1)
		const workflow = await putWorkflow(apiContext, userToken, project.id, {
			restrict_transitions: false,
			statuses: DEFAULT_STATUSES,
			transitions: [{
				from_status_id: -2,
				to_status_id: -4,
				requires_approval: true,
				approval: {mode: 'any', approvers: [{type: 'user', id: approver.id}]},
			}],
		})
		const task = await createTask(apiContext, userToken, project.id, 'Print the catalogue')
		await api.post(`/tasks/${task.id}/status`, {to_status_id: workflow.statuses.find(s => s.title === 'In progress')!.id})
		const view = await createStatusBoard(apiContext, userToken, project.id)

		await page.goto(`/projects/${project.id}/${view}`)
		await drop(page, card(page, 'Print the catalogue'), column(page, 'Done'))
		await expect(page.locator('.global-notification .vue-notification.success')).toContainText('Approval requested')
		await expect(column(page, 'In progress')).toContainText('Print the catalogue')
		await expect(column(page, 'Done')).not.toContainText('Print the catalogue')
		expect(await statusOf(apiContext, userToken, task.id)).toBe('In progress')
		const pending = (await api.get(`/tasks/${task.id}/approvals`)).items as {status: string}[]
		expect(pending.map(a => a.status)).toEqual(['pending'])

		// The approver decides on their own page and the board follows after a reload.
		const other = await pageAs(browser, apiContext, approver)
		await other.goto('/approvals')
		await other.locator('[data-cy="approval-row"]').getByRole('button', {name: 'Approve'}).click()
		await other.getByRole('dialog', {name: 'Approve request'}).getByRole('button', {name: 'Approve'}).click()
		await expect(other.locator('.global-notification .vue-notification.success', {hasText: 'Approved'})).toBeVisible()
		await expect.poll(() => statusOf(apiContext, userToken, task.id)).toBe('Done')
		await page.reload()
		await expect(column(page, 'Done')).toContainText('Print the catalogue')
		await other.context().close()
		watcher.finish()
	})

	test('an admin turns a view into a status board in the view settings', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const project = await createProject(apiContext, userToken, 'Alpha')
		await putWorkflow(apiContext, userToken, project.id, {restrict_transitions: false, statuses: DEFAULT_STATUSES, transitions: []})
		await createTask(apiContext, userToken, project.id, 'Write the report')

		// The kanban view every project has: edit it and let the workflow be its columns.
		await page.goto(`/projects/${project.id}/settings/views`)
		await page.getByRole('row').filter({hasText: 'Kanban'}).getByRole('button', {name: 'Edit this view', exact: true}).click()
		const form = page.locator('table form')
		await form.getByRole('radio', {name: 'Workflow statuses'}).check()
		await expect(form.getByText('One column per status of the project workflow.', {exact: false})).toBeVisible()
		const saved = page.waitForResponse(r => /\/projects\/\d+\/views\/\d+$/.test(r.url()) && r.request().method() === 'PUT')
		await form.getByRole('button', {name: 'Save', exact: true}).click()
		const response = await saved
		expect(response.ok()).toBe(true)
		const body = await response.json()
		expect(body.bucket_configuration_mode).toBe('status')

		await page.goto(`/projects/${project.id}/${body.id}`)
		await expect(page.locator('.kanban .bucket .status-title')).toHaveCount(4)
		await expect(column(page, 'To do')).toContainText('Write the report')
		watcher.finish()
	})
})
