import {test, expect} from '../../support/fixtures'
import type {APIRequestContext, Page} from '@playwright/test'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {apiV1Url} from '../../support/apiUrl'
import {createProject, createTask, createUser, getTask, pageAs, putWorkflow, shareProject, DEFAULT_STATUSES, type TestUser} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
// The tests provoke refusals through the API on purpose.
const REFUSED = [/\/approvals/, /\/tasks\/\d+\/status$/]

interface Setup {
	projectId: number
	taskId: number
	approver: TestUser
}

// A project whose "In progress -> Done" needs the approval of a second user.
async function setup(request: APIRequestContext, token: string, approval: Record<string, unknown> = {}, taskTitle = 'Print the catalogue'): Promise<Setup> {
	const project = await createProject(request, token, 'Alpha')
	const approver = await createUser(request, 2, 'approver-user')
	await shareProject(request, token, project.id, approver.username, 1)
	const workflow = await putWorkflow(request, token, project.id, {
		restrict_transitions: false,
		statuses: DEFAULT_STATUSES,
		transitions: [{
			from_status_id: -2,
			to_status_id: -4,
			requires_approval: true,
			approval: {mode: 'any', approvers: [{type: 'user', id: approver.id}], ...approval},
		}],
	})
	const task = await createTask(request, token, project.id, taskTitle)
	await apiV2(request, token).post(`/tasks/${task.id}/status`, {to_status_id: workflow.statuses.find(s => s.title === 'In progress')!.id})
	return {projectId: project.id, taskId: task.id, approver}
}

function statusTrigger(page: Page) {
	return page.getByRole('button', {name: /^Status: .*\. Change status$/})
}

async function requestFromTask(page: Page, taskId: number) {
	await page.goto(`/tasks/${taskId}`)
	await statusTrigger(page).click()
	const posted = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) && r.request().method() === 'POST')
	await page.getByRole('menuitem', {name: /Request approval/}).click()
	expect((await posted).status()).toBe(202)
}

test.describe('Approval of a status change', () => {
	test('the requester asks, the approver approves with a comment and the task moves', async ({authenticatedPage: page, apiContext, userToken, currentUser, browser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const {taskId, approver} = await setup(apiContext, userToken)

		await page.goto(`/tasks/${taskId}`)
		await statusTrigger(page).click()
		const item = page.getByRole('menuitem', {name: /Request approval/})
		await expect(item).toContainText('Done')
		await expect(item).toContainText(approver.username)
		const posted = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) && r.request().method() === 'POST')
		await item.click()
		expect((await posted).status()).toBe(202)

		// The task shows the pending request and has not moved.
		const banner = page.locator('[data-cy="approval-banner"]')
		await expect(banner).toContainText('Waiting for approval → Done')
		await expect(banner).toContainText('0/1 approved')
		await expect(banner).toContainText(approver.username)
		await expect(statusTrigger(page)).toHaveAccessibleName('Status: In progress. Change status')
		expect((await getTask(apiContext, userToken, taskId)).done).toBe(false)
		// The requester cannot decide, only cancel.
		await expect(banner.getByRole('button', {name: 'Approve'})).toHaveCount(0)
		await expect(banner.getByRole('button', {name: 'Cancel request'})).toBeVisible()

		// A second ask for the same change does not create a second request.
		await statusTrigger(page).click()
		const again = page.waitForResponse(r => /\/tasks\/\d+\/status$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('menuitem', {name: /Request approval/}).click()
		await again
		await expect(banner).toHaveCount(1)

		// The requester follows it under "Requested by me".
		await page.goto('/approvals?tab=requested')
		await expect(page.locator('[data-cy="approval-row"]')).toContainText('Print the catalogue')
		await expect(page.locator('[data-cy="approval-row"]').getByRole('button', {name: 'Approve'})).toHaveCount(0)

		// The approver finds it on the approvals page.
		const other = await pageAs(browser, apiContext, approver)
		const otherWatcher = watchBrowser(other, NO_WORKFLOW)
		await other.goto('/approvals')
		const row = other.locator('[data-cy="approval-row"]')
		await expect(row).toHaveCount(1)
		await expect(row).toContainText('Print the catalogue')
		await expect(row).toContainText(currentUser.username)
		await expect(other.getByRole('button', {name: /Waiting for me/})).toContainText('1')

		await row.getByRole('button', {name: 'Approve'}).click()
		const dialog = other.getByRole('dialog', {name: 'Approve request'})
		await dialog.getByLabel('Comment (optional)').fill('Looks good to me')
		await dialog.getByRole('button', {name: 'Approve'}).click()
		await expect(other.locator('.global-notification .vue-notification.success', {hasText: 'Approved'})).toBeVisible()
		await expect(row).toHaveCount(0)

		// The system made the change on behalf of the requester.
		await expect.poll(async () => (await getTask(apiContext, userToken, taskId)).done).toBe(true)
		await page.goto(`/tasks/${taskId}`)
		await expect(statusTrigger(page)).toHaveAccessibleName('Status: Done. Change status')
		await expect(page.locator('[data-cy="approval-banner"]')).toHaveCount(0)
		await page.getByRole('button', {name: 'Resolved approvals (1)'}).click()
		const resolved = page.locator('.approval-history__entry')
		await expect(resolved).toContainText('Approved')
		await expect(resolved).toContainText('Looks good to me')

		await page.getByRole('button', {name: 'Status history'}).click()
		await expect(page.getByRole('region', {name: 'Status history'})).toContainText(`approved by ${approver.username}`)

		// The approver's history tab lists the resolved request.
		await other.goto('/approvals?tab=history')
		await expect(other.locator('[data-cy="approval-row"]')).toContainText('Approved')
		watcher.finish()
		otherWatcher.finish()
		await other.context().close()
	})

	test('a rejection needs a reason, keeps the task where it is and the reason lands on the task', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const {taskId, approver} = await setup(apiContext, userToken)
		await requestFromTask(page, taskId)

		const other = await pageAs(browser, apiContext, approver)
		const otherWatcher = watchBrowser(other, NO_WORKFLOW)
		await other.goto(`/tasks/${taskId}`)
		const banner = other.locator('[data-cy="approval-banner"]')
		await expect(banner).toContainText('Waiting for approval → Done')
		await banner.getByRole('button', {name: 'Reject'}).click()

		const dialog = other.getByRole('dialog', {name: 'Reject request'})
		await expect(dialog.getByText('Reason for the rejection (required)')).toBeVisible()
		await dialog.getByRole('button', {name: 'Reject'}).click()
		await expect(dialog.getByRole('alert')).toBeVisible()
		await dialog.getByLabel('Reason for the rejection (required)').fill('The prices are outdated')
		await dialog.getByRole('button', {name: 'Reject'}).click()
		await expect(other.locator('.global-notification .vue-notification.success', {hasText: 'Rejected'})).toBeVisible()

		await expect(statusTrigger(other)).toHaveAccessibleName('Status: In progress. Change status')
		const task = await getTask(apiContext, userToken, taskId)
		expect(task.done).toBe(false)
		const comments = await (await apiContext.get(`${apiV1Url()}/tasks/${taskId}/comments`, {headers: {Authorization: `Bearer ${userToken}`}})).json()
		expect(comments.map((c: {comment: string}) => c.comment).join(' ')).toContain('The prices are outdated')

		// The requester sees the outcome.
		await page.goto('/approvals?tab=history')
		await page.getByLabel('Outcome').selectOption({label: 'Rejected'})
		await expect(page.locator('[data-cy="approval-row"]')).toContainText('Rejected')
		await page.goto(`/tasks/${taskId}`)
		await page.getByRole('button', {name: 'Resolved approvals (1)'}).click()
		await expect(page.locator('.approval-history__entry')).toContainText('The prices are outdated')
		watcher.finish()
		otherWatcher.finish()
		await other.context().close()
	})

	test('self-approval is refused unless the transition allows it', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, ...REFUSED])
		const api = apiV2(apiContext, userToken)

		// Only the requester is named as approver: nobody is left to ask.
		const strict = await setup(apiContext, userToken, {approvers: [{type: 'user', id: currentUser.id}]})
		await page.goto(`/tasks/${strict.taskId}`)
		await statusTrigger(page).click()
		const item = page.getByRole('menuitem', {name: /Request approval/})
		await expect(item).toContainText('Nobody to ask yet')
		const refused = await apiContext.post(`${api.base}/tasks/${strict.taskId}/status`, {
			headers: {Authorization: `Bearer ${userToken}`},
			data: {to_status_id: (await api.get(`/projects/${strict.projectId}/workflow`)).statuses.find((s: {title: string}) => s.title === 'Done').id},
		})
		expect(refused.status()).toBe(422)
		expect(await refused.text()).toContain('24006')
		expect((await api.get(`/tasks/${strict.taskId}/approvals`)).items).toHaveLength(0)
		watcher.finish()
	})

	test('with allow_self the requester approves their own request', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const {taskId} = await setup(apiContext, userToken, {approvers: [{type: 'user', id: currentUser.id}], allow_self: true})
		await requestFromTask(page, taskId)

		const banner = page.locator('[data-cy="approval-banner"]')
		await banner.getByRole('button', {name: 'Approve'}).click()
		const dialog = page.getByRole('dialog', {name: 'Approve request'})
		await dialog.getByRole('button', {name: 'Approve'}).click()
		await expect(page.locator('.global-notification .vue-notification.success', {hasText: 'Approved'})).toBeVisible()
		await expect.poll(async () => (await getTask(apiContext, userToken, taskId)).done).toBe(true)
		watcher.finish()
	})

	test('someone who is not an approver cannot decide', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const {projectId, taskId} = await setup(apiContext, userToken)
		await requestFromTask(page, taskId)

		const bystander = await createUser(apiContext, 3, 'bystander-user')
		await shareProject(apiContext, userToken, projectId, bystander.username, 1)
		const approvals = (await apiV2(apiContext, userToken).get(`/tasks/${taskId}/approvals`)).items as {id: number}[]
		expect(approvals).toHaveLength(1)

		const other = await pageAs(browser, apiContext, bystander)
		const otherWatcher = watchBrowser(other, NO_WORKFLOW)
		await other.goto(`/tasks/${taskId}`)
		await expect(other.locator('[data-cy="approval-banner"]')).toBeVisible()
		await expect(other.getByRole('button', {name: 'Approve'})).toHaveCount(0)
		await expect(other.getByRole('button', {name: 'Reject'})).toHaveCount(0)
		await other.goto('/approvals')
		await expect(other.locator('[data-cy="approval-row"]')).toHaveCount(0)

		const refused = await apiContext.post(`${apiV2(apiContext, bystander.token).base}/approvals/${approvals[0]!.id}/decision`, {
			headers: {Authorization: `Bearer ${bystander.token}`},
			data: {decision: 'approved'},
		})
		expect(refused.status()).toBe(403)
		expect((await getTask(apiContext, userToken, taskId)).done).toBe(false)
		watcher.finish()
		otherWatcher.finish()
		await other.context().close()
	})
})

test.describe('Ad-hoc approval', () => {
	test('the requester asks a colleague for a decision and cancels it', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const approver = await createUser(apiContext, 2, 'approver-user')
		await shareProject(apiContext, userToken, project.id, approver.username, 1)
		const task = await createTask(apiContext, userToken, project.id, 'Cover design')

		await page.goto(`/tasks/${task.id}`)
		await page.locator('[data-cy="taskDetail.moreActions"]').click()
		await page.locator('.task-view .task-actions-menu .dropdown-menu').getByRole('button', {name: 'Request approval', exact: true}).click()
		const dialog = page.getByRole('dialog', {name: 'Request approval'})
		await expect(dialog).toBeVisible()

		// Validation first, and the requester is not on the list.
		await dialog.getByRole('button', {name: 'Send request'}).click()
		await expect(dialog.getByText('Say what should be approved.')).toBeVisible()
		await expect(dialog.getByText('Choose at least one person.')).toBeVisible()
		await expect(dialog.getByRole('checkbox', {name: currentUser.username})).toHaveCount(0)

		await dialog.getByLabel('What should be approved?').fill('The cover design')
		await dialog.getByLabel('Details (optional)').fill('Front and back')
		await dialog.getByRole('checkbox', {name: approver.username}).check()
		const created = page.waitForResponse(r => /\/tasks\/\d+\/approvals$/.test(r.url()) && r.request().method() === 'POST')
		await dialog.getByRole('button', {name: 'Send request'}).click()
		expect((await created).status()).toBeLessThan(300)
		await expect(dialog).toHaveCount(0)

		const banner = page.locator('[data-cy="approval-banner"]')
		await expect(banner).toContainText('The cover design')
		await expect(banner).toContainText('Front and back')
		await expect(banner).toContainText(approver.username)
		// An ad-hoc request records a decision; the status does not change and the task stays open.
		expect((await getTask(apiContext, userToken, task.id)).done).toBe(false)

		// Cancel: asks first, "Keep request" keeps it.
		await banner.getByRole('button', {name: 'Cancel request'}).click()
		const confirm = page.getByRole('dialog', {name: 'Cancel request'})
		await expect(confirm).toContainText('Cancel the request "The cover design"?')
		await confirm.getByRole('button', {name: 'Keep request'}).click()
		await expect(banner).toBeVisible()

		await banner.getByRole('button', {name: 'Cancel request'}).click()
		await confirm.getByRole('button', {name: 'Cancel request'}).click()
		await expect(page.locator('.global-notification .vue-notification.success', {hasText: 'Request cancelled'})).toBeVisible()
		await expect(page.locator('[data-cy="approval-banner"]')).toHaveCount(0)
		await page.getByRole('button', {name: 'Resolved approvals (1)'}).click()
		await expect(page.locator('.approval-history__entry')).toContainText('Cancelled')

		const stored = (await apiV2(apiContext, userToken).get(`/tasks/${task.id}/approvals`)).items as {status: string, cancel_reason: string}[]
		expect(stored[0]).toMatchObject({status: 'cancelled', cancel_reason: 'cancelled_by_requester'})
		watcher.finish()
	})

	test('an ad-hoc request that names the requester is refused by the server', async ({apiContext, userToken, currentUser}) => {
		const project = await createProject(apiContext, userToken, 'Alpha')
		const task = await createTask(apiContext, userToken, project.id, 'Cover design')
		const refused = await apiContext.post(`${apiV2(apiContext, userToken).base}/tasks/${task.id}/approvals`, {
			headers: {Authorization: `Bearer ${userToken}`},
			data: {title: 'Me', approver_ids: [currentUser.id]},
		})
		expect(refused.status()).toBeGreaterThanOrEqual(400)
		expect(refused.status()).toBeLessThan(500)
	})
})
