import {test, expect} from '../../support/fixtures'
import type {Page} from '@playwright/test'
import {ProjectFactory} from '../../factories/project'
import {TaskFactory} from '../../factories/task'
import {TeamFactory} from '../../factories/team'
import {TeamMemberFactory} from '../../factories/team_member'
import {apiV1Url} from '../../support/apiUrl'
import {clickTaskAction} from '../../support/taskDetail'
import {apiV2, shot, watchBrowser} from '../../support/uiCheck'

test.use({viewport: {width: 1280, height: 900}})

// The project has no workflow: its 404 is a normal state the settings pages ask for.
const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]

test.beforeEach(async ({currentUser}) => {
	await ProjectFactory.create(1, {id: 1, title: 'Alpha', owner_id: currentUser.id}, false)
	await TaskFactory.create(1, {id: 1, index: 1, project_id: 1, created_by_id: currentUser.id, title: 'Review the invoice'}, false)
})

async function openNewRule(page: Page, title: string) {
	await page.goto('/projects/1/settings/automations')
	await page.getByRole('button', {name: 'New rule'}).first().click()
	await page.getByLabel('Title', {exact: true}).fill(title)
}

async function saveRule(page: Page) {
	const saved = page.waitForResponse(r => /\/projects\/1\/automations(\/\d+)?$/.test(r.url()) && ['POST', 'PUT'].includes(r.request().method()))
	await page.getByRole('button', {name: 'Save rule'}).click()
	return await (await saved).json()
}

async function addAction(page: Page, type: string) {
	await page.locator('#automation-add-action').selectOption(type)
}

test.describe('Automation editor: triggers', () => {
	test('schedule: cron input, presets and validation', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		await openNewRule(page, 'Every morning')
		await page.getByLabel('Trigger', {exact: true}).selectOption('schedule')
		const cron = page.locator('#automation-trigger-options-cron')
		await expect(cron).toBeVisible()
		await shot(page, 'auto-01-schedule-empty')

		await cron.fill('* * *')
		await expect(page.locator('.cron-input .help.is-danger')).toContainText('Use five fields')
		await cron.fill('0 25 * * *')
		await expect(page.locator('.cron-input .help.is-danger')).toContainText('out of range')
		await cron.fill('30 2 30 2 *')
		await expect(page.locator('.cron-input .help.is-danger')).toContainText('never happens')
		await shot(page, 'auto-02-schedule-bad-cron')

		for (const [preset, expression] of [['Every hour', '@hourly'], ['Every day', '@daily'], ['Every week', '@weekly'], ['Every month', '@monthly']] as const) {
			await page.locator('.cron-input__presets').getByRole('button', {name: preset}).click()
			await expect(cron).toHaveValue(expression)
			await expect(page.locator('.cron-input .help.is-danger')).toHaveCount(0)
		}
		await cron.fill('0 9 * * 1')
		await addAction(page, 'task.create')
		await page.locator('#automation-actions-0-params-title').fill('Weekly review')
		await shot(page, 'auto-03-schedule-ready')
		const saved = await saveRule(page)
		expect(saved.trigger).toMatchObject({type: 'schedule', options: {cron: '0 9 * * 1'}})

		// Stored: reopen it from the list.
		await page.reload()
		await page.getByRole('button', {name: /Every morning/}).first().click().catch(async () => {
			await page.getByText('Every morning').first().click()
		})
		await expect(page.locator('#automation-trigger-options-cron')).toHaveValue('0 9 * * 1')
		const stored = await apiV2(apiContext, userToken).get('/projects/1/automations')
		expect(stored.items[0].trigger.options.cron).toBe('0 9 * * 1')
		watcher.finish()
	})

	test('overdue, due soon and manual triggers save and come back', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)

		await openNewRule(page, 'Nudge overdue')
		await page.getByLabel('Trigger', {exact: true}).selectOption('task.overdue')
		await addAction(page, 'wait')
		await page.locator('#automation-actions-0-params-duration').fill('2h')
		await addAction(page, 'task.create')
		await page.locator('#automation-actions-1-params-title').fill('Overdue: {{task.title}}')
		await shot(page, 'auto-04-overdue-wait-create')
		await saveRule(page)

		await openNewRule(page, 'Heads up')
		await page.getByLabel('Trigger', {exact: true}).selectOption('task.due_soon')
		const offset = page.locator('#automation-trigger-options-offset')
		await offset.fill('5x')
		await expect(page.locator('.param-input .help.is-danger').first()).toContainText('A number and a unit')
		await offset.fill('2d')
		await addAction(page, 'task.create')
		await page.locator('#automation-actions-0-params-title').fill('Due soon: {{task.title}}')
		await shot(page, 'auto-05-due-soon')
		await saveRule(page)

		await openNewRule(page, 'Create follow-up')
		await page.getByLabel('Trigger', {exact: true}).selectOption('manual')
		await page.locator('#automation-trigger-options-label').fill('Create a follow-up')
		await addAction(page, 'task.create')
		await page.locator('#automation-actions-0-params-title').fill('Follow up on {{task.title}}')
		await saveRule(page)

		const stored = (await api.get('/projects/1/automations')).items as {title: string, trigger: {type: string, options: Record<string, unknown>}, actions: {type: string, params: Record<string, unknown>}[]}[]
		const byTitle = Object.fromEntries(stored.map(r => [r.title, r]))
		expect(byTitle['Nudge overdue']!.trigger.type).toBe('task.overdue')
		expect(byTitle['Nudge overdue']!.actions.map(a => a.type)).toEqual(['wait', 'task.create'])
		expect(byTitle['Nudge overdue']!.actions[0]!.params.duration).toBe('2h')
		expect(byTitle['Heads up']!.trigger.options.offset).toBe('2d')
		expect(byTitle['Create follow-up']!.trigger.options.label).toBe('Create a follow-up')

		await page.goto('/projects/1/settings/automations')
		await expect(page.getByText('Nudge overdue').first()).toBeVisible()
		await expect(page.getByText('Heads up').first()).toBeVisible()
		await shot(page, 'auto-06-rule-list')
		watcher.finish()
	})
})

test.describe('Automation editor: webhook trigger', () => {
	test('shows the address once, asks before it is lost, rotates it and dry-runs a payload', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, /\/automation\/hooks\//])
		const api = apiV2(apiContext, userToken)

		await openNewRule(page, 'From the outside')
		await page.getByLabel('Trigger', {exact: true}).selectOption('webhook.incoming')
		await expect(page.locator('.webhook-panel')).toContainText('created when you save the rule')
		await addAction(page, 'task.create')
		await page.locator('#automation-actions-0-params-title').fill('Ticket: {{event.payload.subject}}')
		await shot(page, 'auto-07-webhook-before-save', page.locator('.webhook-panel'))
		await saveRule(page)

		const address = page.locator('#automation-webhook-url')
		await expect(address).toBeVisible()
		const first = await address.inputValue()
		expect(first).toMatch(/\/api\/v2\/automation\/hooks\/[\w-]+$/)
		await expect(page.locator('.webhook-panel')).toContainText('cannot be shown again')
		await shot(page, 'auto-08-webhook-address', page.locator('.webhook-panel'))

		// Leaving with the address uncopied asks first; staying keeps it.
		await page.getByRole('button', {name: 'Back to the rules'}).click()
		const dialog = page.getByRole('dialog').filter({hasText: 'Leave without copying the address?'})
		await expect(dialog).toBeVisible()
		await shot(page, 'auto-09-webhook-leave-confirm')
		await page.keyboard.press('Escape')
		await expect(dialog).toHaveCount(0)
		await expect(address).toBeVisible()

		// Copying counts: the clipboard gets the address and leaving no longer asks.
		await page.getByRole('button', {name: 'Copy', exact: true}).click()
		await expect(page.locator('.global-notification .vue-notification', {hasText: 'The address was copied.'})).toBeVisible()
		expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(first)

		// The address works: a POST creates the task.
		const hit = await apiContext.post(first, {data: {subject: 'Printer is on fire'}})
		expect(hit.status(), await hit.text()).toBeLessThan(300)
		await expect.poll(async () => {
			const tasks = await apiContext.get(`${apiV1Url()}/projects/1/tasks`, {headers: {Authorization: `Bearer ${userToken}`}})
			return (await tasks.json()).map((t: {title: string}) => t.title)
		}, {timeout: 20_000}).toContain('Ticket: Printer is on fire')

		await page.getByRole('button', {name: 'Back to the rules'}).click()
		await expect(page.getByRole('dialog').filter({hasText: 'Leave without copying'})).toHaveCount(0)
		await expect(page.getByText('From the outside').first()).toBeVisible()

		// Reopen: the address is not shown any more; a new one replaces the old.
		await page.getByText('From the outside').first().click()
		await expect(page.locator('#automation-webhook-url')).toHaveCount(0)
		await expect(page.locator('.webhook-panel')).toContainText('cannot be shown again')
		await page.getByRole('button', {name: 'Create a new address'}).click()
		const rotateDialog = page.getByRole('dialog').filter({hasText: 'Create a new webhook address?'})
		await expect(rotateDialog).toBeVisible()
		await shot(page, 'auto-10-webhook-rotate-confirm')
		await rotateDialog.getByRole('button', {name: 'Do it!'}).click()
		await expect(rotateDialog).toHaveCount(0)
		const second = await page.locator('#automation-webhook-url').inputValue()
		expect(second).not.toBe(first)
		const old = await apiContext.post(first, {data: {subject: 'still there?'}, failOnStatusCode: false})
		expect(old.status()).toBeGreaterThanOrEqual(400)
		const fresh = await apiContext.post(second, {data: {subject: 'second address'}})
		expect(fresh.status()).toBeLessThan(300)

		// Dry run with a JSON payload: nothing is written, the rendered title is shown.
		await page.locator('#automation-test-payload').fill('{"subject": "Dry run subject"}')
		await expect(page.locator('#automation-test-payload')).toHaveValue('{"subject": "Dry run subject"}')
		const testRequest = page.waitForRequest(r => r.method() === 'POST' && /\/automations\/\d+\/test$/.test(r.url()))
		await page.getByRole('button', {name: 'Test', exact: true}).click()
		await testRequest
		await expect(page.locator('.dry-run__result')).toContainText('Ticket: Dry run subject')
		await shot(page, 'auto-11-webhook-dry-run', page.locator('.dry-run'))
		await page.locator('#automation-test-payload').fill('{not json')
		await expect(page.locator('.dry-run__payload .help.is-danger')).toContainText('not valid JSON')
		expect((await api.get('/projects/1/automations')).items).toHaveLength(1)
		watcher.finish()
	})
})

test.describe('Automation: manual run and notify', () => {
	test('runs a manual rule from the task detail and notifies a team', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		await TeamFactory.create(1, {id: 1, name: 'Operations', created_by_id: currentUser.id}, false)
		await TeamMemberFactory.create(1, {team_id: 1, user_id: currentUser.id, admin: true}, false)

		await openNewRule(page, 'Tell operations')
		await page.getByLabel('Trigger', {exact: true}).selectOption('manual')
		await page.locator('#automation-trigger-options-label').fill('Notify operations')
		await addAction(page, 'notify')
		await page.locator('#automation-actions-0-params-team_id').selectOption({label: 'Operations'})
		await page.locator('#automation-actions-0-params-message').fill('Invoice {{task.title}} needs a look')
		await shot(page, 'auto-12-notify-action', page.locator('#automation-actions-0'))
		const saved = await saveRule(page)
		expect(saved.actions[0]).toMatchObject({type: 'notify', params: {message: 'Invoice {{task.title}} needs a look'}})
		expect(String(saved.actions[0].params.team_id)).toBe('1')

		// A notify with neither recipient nor team is refused before it is sent.
		await page.getByRole('button', {name: 'Back to the rules'}).click()

		await page.goto('/tasks/1')
		await clickTaskAction(page, 'Run automation…')
		const dialog = page.getByRole('dialog', {name: 'Run automation'})
		await expect(dialog).toBeVisible()
		await shot(page, 'auto-13-run-automation-dialog')
		await dialog.getByRole('button', {name: 'Notify operations'}).click()
		await expect(page.locator('.global-notification .vue-notification.success')).toContainText('was started')
		await shot(page, 'auto-14-run-started')

		await expect.poll(async () => {
			const response = await apiContext.get(`${apiV1Url()}/notifications`, {headers: {Authorization: `Bearer ${userToken}`}})
			return (await response.json()).map((n: {name: string}) => n.name)
		}, {timeout: 20_000}).toContain('automation.notify')

		await page.reload()
		await page.locator('.notifications .trigger-button').click()
		const list = page.locator('.notifications-list')
		await expect(list).toContainText('Invoice Review the invoice needs a look')
		await shot(page, 'auto-15-notification')
		watcher.finish()
	})

	test('refuses a notify action without recipients', async ({authenticatedPage: page}) => {
		await openNewRule(page, 'Silent notify')
		await page.getByLabel('Trigger', {exact: true}).selectOption('task.created')
		await addAction(page, 'notify')
		await page.locator('#automation-actions-0-params-message').fill('Hello')
		await page.getByRole('button', {name: 'Save rule'}).click()
		await expect(page.getByText('Name at least one recipient or a team.').first()).toBeVisible()
		await shot(page, 'auto-16-notify-no-recipient', page.getByText('Name at least one recipient or a team.').first())
	})

	test('rule editor on a narrow screen', async ({authenticatedPage: page}) => {
		await page.setViewportSize({width: 390, height: 844})
		await openNewRule(page, 'Narrow')
		await page.getByLabel('Trigger', {exact: true}).selectOption('webhook.incoming')
		await addAction(page, 'wait')
		await page.locator('#automation-actions-0-params-duration').fill('1h')
		await shot(page, 'auto-17-editor-mobile', page.locator('.webhook-panel'))
		await page.getByLabel('Trigger', {exact: true}).selectOption('schedule')
		await shot(page, 'auto-18-editor-mobile-schedule', page.locator('.cron-input'))
	})
})
