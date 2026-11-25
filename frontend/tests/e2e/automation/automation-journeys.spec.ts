import {test, expect} from '../../support/fixtures'
import type {APIRequestContext, Page} from '@playwright/test'
import {apiV1Url} from '../../support/apiUrl'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {clickTaskAction} from '../../support/taskDetail'
import {createProject, createTask, listTasks} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]

async function commentsOf(request: APIRequestContext, token: string, taskId: number): Promise<string[]> {
	const response = await request.get(`${apiV1Url()}/tasks/${taskId}/comments`, {headers: {Authorization: `Bearer ${token}`}})
	return (await response.json()).map((c: {comment: string}) => c.comment.replace(/<[^>]*>/g, ''))
}

async function openNewRule(page: Page, projectId: number, title: string) {
	await page.goto(`/projects/${projectId}/settings/automations`)
	await page.getByRole('button', {name: 'New rule'}).first().click()
	await page.getByLabel('Title', {exact: true}).fill(title)
}

test.describe('Automation: a rule from the editor to the run history', () => {
	test('a conditional rule is built, tested, enabled and runs when a task is created', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		// These exist before the rule: the dry run has something to look at, and no run is started for them.
		await createTask(apiContext, userToken, project.id, 'urgent: fix the printer')
		await createTask(apiContext, userToken, project.id, 'water the plants')

		// Build: when a task is created, if its title contains "urgent", add a comment.
		await openNewRule(page, project.id, 'Comment on urgent tasks')
		await page.getByLabel('Trigger', {exact: true}).selectOption('task.created')
		await page.getByRole('button', {name: 'Add condition'}).click()
		await page.locator('#automation-conditions-all-0-field').selectOption('title')
		await page.locator('#automation-conditions-all-0-op').selectOption('like')
		await page.locator('#automation-conditions-all-0-value').fill('urgent')
		await page.locator('#automation-add-action').selectOption('task.add_comment')
		await page.locator('#automation-actions-0-params-text').fill('Seen: {{task.title}}')
		await expect(page.locator('.rule-editor__summary')).toContainText('with conditions (1)')

		// A rule that is not saved cannot be tested.
		await expect(page.getByText('Save the rule first.')).toBeVisible()
		await page.getByLabel('Rule is enabled').check()
		const saved = page.waitForResponse(r => /\/projects\/\d+\/automations$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('button', {name: 'Save rule'}).click()
		const rule = await (await saved).json()
		expect(rule).toMatchObject({title: 'Comment on urgent tasks', enabled: true, trigger: {type: 'task.created'}})
		expect(rule.conditions).toMatchObject({all: [{field: 'title', op: 'like', value: 'urgent'}]})

		// Dry run on the saved rule: nothing is written; the verdict follows the condition.
		await page.getByRole('button', {name: 'Back to the rules'}).click()
		await page.getByRole('button', {name: 'Comment on urgent tasks'}).first().click()
		const panel = page.getByRole('region', {name: 'Test with a task'})
		const chooseTask = async (title: string) => {
			const select = panel.getByLabel('Task', {exact: true})
			const value = await select.locator('option', {hasText: title}).getAttribute('value')
			await select.selectOption(value!)
		}
		await chooseTask('urgent: fix the printer')
		await panel.getByRole('button', {name: 'Test', exact: true}).click()
		await expect(panel.getByRole('status')).toContainText('The conditions are met')
		await expect(panel.getByRole('status')).toContainText('Seen: urgent: fix the printer')
		await chooseTask('water the plants')
		await panel.getByRole('button', {name: 'Test', exact: true}).click()
		await expect(panel.getByRole('status')).toContainText('The conditions are not met')
		const before = await listTasks(apiContext, userToken, project.id)
		for (const task of before) expect(await commentsOf(apiContext, userToken, task.id)).toEqual([])

		// The list shows the rule switched on and never run.
		await page.getByRole('button', {name: 'Back to the rules'}).click()
		await expect(page.getByLabel('Enable the rule "Comment on urgent tasks"')).toBeChecked()
		await expect(page.locator('.rule-item').filter({hasText: 'Comment on urgent tasks'})).toContainText('Has not run yet')

		// Trigger it through the UI: quick-add two tasks in the list view.
		await page.goto(`/projects/${project.id}/${project.views.list}`)
		const input = page.locator('.add-task-textarea')
		await input.fill('urgent invoice')
		await input.press('Enter')
		await expect(page.getByRole('link', {name: 'urgent invoice'})).toBeVisible()
		await input.fill('ordinary invoice')
		await input.press('Enter')
		await expect(page.getByRole('link', {name: 'ordinary invoice'})).toBeVisible()

		const urgent = (await listTasks(apiContext, userToken, project.id)).find(t => t.title === 'urgent invoice')!
		const ordinary = (await listTasks(apiContext, userToken, project.id)).find(t => t.title === 'ordinary invoice')!
		await expect.poll(() => commentsOf(apiContext, userToken, urgent.id), {timeout: 30_000}).toEqual(['Seen: urgent invoice'])
		expect(await commentsOf(apiContext, userToken, ordinary.id)).toEqual([])

		// The effect is on the task in the browser.
		await page.goto(`/tasks/${urgent.id}`)
		await expect(page.getByText('Seen: urgent invoice')).toBeVisible()

		// Run history: one success with its step, one skipped run for the other task.
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items.length, {timeout: 30_000}).toBe(2)
		await page.goto(`/projects/${project.id}/settings/automations`)
		await expect(page.locator('.rule-item').filter({hasText: 'Comment on urgent tasks'}).locator('.rule-item__last')).not.toContainText('Has not run yet')
		await page.getByRole('button', {name: 'Show the runs of "Comment on urgent tasks"'}).click()
		const rows = page.locator('.runs__table tbody tr')
		await expect(rows).toHaveCount(2)
		const success = rows.filter({hasText: 'Success'})
		const skipped = rows.filter({hasText: 'Skipped'})
		await expect(success).toHaveCount(1)
		await expect(skipped).toHaveCount(1)
		await expect(skipped).toContainText('The conditions were not met')
		await expect(success).toContainText(`#${urgent.id}`)
		await success.getByRole('button', {name: 'Steps'}).click()
		const detail = page.getByRole('region', {name: 'Run details'})
		await expect(detail).toContainText('Add a comment')
		await expect(detail).toContainText('Success')

		// The filter narrows the list.
		await page.getByLabel('Status', {exact: true}).selectOption({label: 'Skipped'})
		await expect(rows).toHaveCount(1)

		// A rule that is switched off does not run.
		await page.getByRole('button', {name: 'Back to the rules'}).click()
		await page.getByLabel('Enable the rule "Comment on urgent tasks"').uncheck()
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations`)).items[0].enabled).toBe(false)
		const quiet = await createTask(apiContext, userToken, project.id, 'urgent but quiet')
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items.length, {timeout: 10_000}).toBe(2)
		expect(await commentsOf(apiContext, userToken, quiet.id)).toEqual([])
		watcher.finish()
	})

	test('a manual rule is a button on the task and its run is recorded', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const task = await createTask(apiContext, userToken, project.id, 'Review the contract')
		const rule = await api.post(`/projects/${project.id}/automations`, {
			title: 'Ask for review',
			enabled: true,
			trigger: {type: 'manual', options: {label: 'Ask for a review'}},
			actions: [{type: 'task.add_comment', params: {text: 'Please review {{task.title}}'}}],
		})

		await page.goto(`/tasks/${task.id}`)
		await clickTaskAction(page, 'Run automation…')
		const dialog = page.getByRole('dialog', {name: 'Run automation'})
		await dialog.getByRole('button', {name: 'Ask for a review'}).click()
		await expect(page.locator('.global-notification .vue-notification.success')).toContainText('was started')

		await expect.poll(() => commentsOf(apiContext, userToken, task.id), {timeout: 30_000}).toEqual(['Please review Review the contract'])
		await page.reload()
		await expect(page.getByText('Please review Review the contract')).toBeVisible()

		await page.goto(`/projects/${project.id}/settings/automations`)
		await page.getByRole('button', {name: 'Show the runs of "Ask for review"'}).click()
		const row = page.locator('.runs__table tbody tr')
		await expect(row).toHaveCount(1)
		await expect(row).toContainText('Success')
		await expect(row).toContainText(`#${task.id}`)
		expect((await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items[0].status).toBe('success')

		// Switched off, the button is gone from the task.
		await page.getByRole('button', {name: 'Back to the rules'}).click()
		await page.getByLabel('Enable the rule "Ask for review"').uncheck()
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations`)).items[0].enabled).toBe(false)
		await page.goto(`/tasks/${task.id}`)
		await page.locator('[data-cy="taskDetail.moreActions"]').click()
		await expect(page.locator('.task-view .task-actions-menu .dropdown-menu').getByRole('button', {name: 'Run automation…'})).toHaveCount(0)
		watcher.finish()
	})
})

test.describe('Automation: secrets', () => {
	test('a secret is created, never shown again, replaced and deleted', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, [...NO_WORKFLOW, /\/automation-secrets/])
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		await page.goto(`/projects/${project.id}/settings/automations`)
		const secrets = page.getByRole('region', {name: 'Secrets'})
		await expect(secrets.getByText('This project has no secrets yet.')).toBeVisible()

		await secrets.getByRole('button', {name: 'New secret'}).click()
		// A bad name and a missing value are refused in the form.
		await secrets.getByLabel('Name', {exact: true}).fill('1bad name')
		await secrets.getByLabel('Value', {exact: true}).fill('top-secret-value-123')
		await secrets.getByRole('button', {name: 'Save secret'}).click()
		await expect(secrets.getByText('Use letters, digits and underscores only')).toBeVisible()

		await secrets.getByLabel('Name', {exact: true}).fill('CHAT_TOKEN')
		const valueInput = secrets.getByLabel('Value', {exact: true})
		await expect(valueInput).toHaveAttribute('type', 'password')
		const created = page.waitForResponse(r => /\/projects\/\d+\/automation-secrets/.test(r.url()) && ['PUT', 'POST'].includes(r.request().method()))
		await secrets.getByRole('button', {name: 'Save secret'}).click()
		expect((await created).status()).toBeLessThan(300)
		await expect(secrets.locator('.secret__name')).toHaveText('CHAT_TOKEN')

		// The value is not on the page, not in any field and not in the API's answer.
		await expect(page.locator('body')).not.toContainText('top-secret-value-123')
		expect(await page.locator('input').evaluateAll(inputs => inputs.map(i => (i as HTMLInputElement).value))).not.toContain('top-secret-value-123')
		const listed = JSON.stringify(await api.get(`/projects/${project.id}/automation-secrets`))
		expect(listed).toContain('CHAT_TOKEN')
		expect(listed).not.toContain('top-secret-value-123')
		await page.reload()
		await expect(page.locator('body')).not.toContainText('top-secret-value-123')

		// The same name again is refused; the value is replaced instead.
		await secrets.getByRole('button', {name: 'New secret'}).click()
		await secrets.getByLabel('Name', {exact: true}).fill('CHAT_TOKEN')
		await secrets.getByLabel('Value', {exact: true}).fill('another')
		await secrets.getByRole('button', {name: 'Save secret'}).click()
		await expect(secrets.getByText('A secret with this name exists already.')).toBeVisible()
		await secrets.getByRole('button', {name: 'Cancel'}).click()

		await secrets.getByRole('button', {name: 'Replace the value of CHAT_TOKEN'}).click()
		await secrets.getByLabel('New value for CHAT_TOKEN').fill('replacement-value-456')
		const replaced = page.waitForResponse(r => /\/automation-secrets/.test(r.url()) && ['PUT', 'POST', 'PATCH'].includes(r.request().method()))
		await secrets.getByRole('button', {name: 'Replace value'}).last().click()
		expect((await replaced).status()).toBeLessThan(300)
		await expect(secrets.getByLabel('New value for CHAT_TOKEN')).toHaveCount(0)
		await expect(page.locator('body')).not.toContainText('replacement-value-456')

		// Delete asks first.
		await secrets.getByRole('button', {name: 'Delete the secret CHAT_TOKEN'}).click()
		const dialog = page.getByRole('dialog').filter({hasText: 'Delete this secret?'})
		await expect(dialog).toContainText('"CHAT_TOKEN"')
		await dialog.getByRole('button', {name: 'Do it!'}).click()
		await expect(secrets.getByText('This project has no secrets yet.')).toBeVisible()
		expect((await api.get(`/projects/${project.id}/automation-secrets`)).items ?? []).toHaveLength(0)
		watcher.finish()
	})
})

test.describe('Automation: limits and validation', () => {
	test('a rule without actions is refused, and a project at its cap says so', async ({authenticatedPage: page, apiContext, userToken}) => {
		test.setTimeout(120_000)
		const watcher = watchBrowser(page, [...NO_WORKFLOW, /\/projects\/\d+\/automations/])
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')

		await openNewRule(page, project.id, 'Nothing to do')
		await page.getByLabel('Trigger', {exact: true}).selectOption('task.created')
		await page.getByRole('button', {name: 'Save rule'}).click()
		await expect(page.getByText('Add at least one action.').first()).toBeVisible()
		expect((await api.get(`/projects/${project.id}/automations`)).items ?? []).toHaveLength(0)

		// Fill the project up to the default cap of 100 rules, then try one more in the editor.
		for (let i = 1; i <= 100; i++) {
			await api.post(`/projects/${project.id}/automations`, {
				title: `Rule ${i}`,
				enabled: false,
				trigger: {type: 'task.created'},
				actions: [{type: 'task.add_comment', params: {text: 'hi'}}],
			})
		}
		await page.locator('#automation-add-action').selectOption('task.add_comment')
		await page.locator('#automation-actions-0-params-text').fill('One too many')
		await page.getByRole('button', {name: 'Save rule'}).click()
		await expect(page.getByText('This project has reached the maximum number of automation rules.').first()).toBeVisible()
		const all = [...(await api.get(`/projects/${project.id}/automations`)).items, ...(await api.get(`/projects/${project.id}/automations?page=2`)).items]
		expect(all).toHaveLength(100)
		watcher.finish()
	})
})
