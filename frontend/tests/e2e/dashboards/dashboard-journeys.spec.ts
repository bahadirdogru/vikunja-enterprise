import {test, expect} from '../../support/fixtures'
import type {APIRequestContext, Download, Locator, Page} from '@playwright/test'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {createProject, createTask, createUser, shareProject, pageAs} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]

// Project Alpha: six tasks. Priority: three Low (1), two High (3), one Urgent (4). Two of them are done, one open task is overdue.
const TASKS = [
	{title: 'Write the report', priority: 1, done: false},
	{title: 'Plan the release', priority: 1, done: false},
	{title: 'Order the cake', priority: 1, done: true},
	{title: 'Fix the login', priority: 3, done: false},
	{title: 'Ship the update', priority: 3, done: true},
	{title: 'Restore the backup', priority: 4, done: false, due_date: new Date(Date.now() - 2 * 86400_000).toISOString()},
]

async function seed(request: APIRequestContext, token: string) {
	const alpha = await createProject(request, token, 'Alpha')
	const beta = await createProject(request, token, 'Beta')
	await createTask(request, token, beta.id, 'Task of another project', {priority: 4})
	for (const task of TASKS) {
		await createTask(request, token, alpha.id, task.title, {priority: task.priority, done: task.done, ...(task.due_date ? {due_date: task.due_date} : {})})
	}
	return {alpha, beta}
}

function widgetCard(page: Page, title: string): Locator {
	return page.locator('article.widget-card', {has: page.getByRole('heading', {name: title, exact: true})})
}

async function addWidget(page: Page, type: string, title: string, fill?: (form: Locator) => Promise<void>) {
	await page.locator('[data-test="add-widget"]').click()
	await page.locator(`.type-picker [data-type="${type}"]`).click()
	const form = page.locator('[data-test="widget-form"]')
	await form.getByLabel('Title').fill(title)
	await fill?.(form)
	await page.locator('[data-test="widget-save"]').click()
	await expect(widgetCard(page, title)).toBeVisible()
}

async function csvOf(download: Download): Promise<string[]> {
	const stream = await download.createReadStream()
	const chunks: Buffer[] = []
	for await (const chunk of stream) chunks.push(Buffer.from(chunk))
	return Buffer.concat(chunks).toString('utf8').replace(/^\uFEFF/, '').split(/\r?\n/).filter(line => line !== '')
}

async function tableOf(card: Locator): Promise<string[][]> {
	const rows = card.locator('table.chart-table tbody tr')
	return await rows.evaluateAll(trs => trs.map(tr => [...tr.children].map(cell => (cell.textContent ?? '').trim())))
}

test.describe('Dashboards: a dashboard built in the browser', () => {
	test('creates a dashboard, adds widgets and shows the numbers of the seeded tasks', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		await seed(apiContext, userToken)

		await page.goto('/dashboards')
		await page.locator('[data-test="new-dashboard"]').click()
		// A name is required.
		await page.locator('[data-test="create-submit"]').click()
		await expect(page.getByText('Give the dashboard a name.')).toBeVisible()
		await page.getByLabel('Name', {exact: true}).fill('Team overview')
		const posts: string[] = []
		page.on('request', r => {
			if (r.method() === 'POST' && /\/api\/v2\/dashboards$/.test(r.url())) posts.push(r.url())
		})
		await page.locator('[data-test="create-submit"]').click()
		await expect(page).toHaveURL(/\/dashboards\/\d+$/)
		const dashboardId = Number(new URL(page.url()).pathname.split('/').pop())
		await expect(page.getByRole('heading', {name: 'Team overview'})).toBeVisible()
		// One click on Create makes one dashboard, not two.
		expect(posts).toHaveLength(1)
		expect((await api.get('/dashboards')).items.filter((d: {title: string}) => d.title === 'Team overview')).toHaveLength(1)

		// A new dashboard starts with default widgets; "Open tasks" counts the open tasks of all projects: 4 in Alpha and 1 in Beta.
		await expect(widgetCard(page, 'Open tasks').locator('.number-tile')).toContainText('5')

		await page.locator('[data-test="toggle-edit"]').click()
		const preview = page.locator('[data-test="widget-preview"]')
		const inAlpha = async (form: Locator) => {
			await form.getByLabel('Tasks from').selectOption({label: 'Alpha'})
		}
		await addWidget(page, 'number', 'All Alpha tasks', async form => {
			await inAlpha(form)
			await expect(preview.locator('.number-tile')).toContainText('6')
		})
		await addWidget(page, 'bar', 'By priority', async form => {
			await inAlpha(form)
			await form.locator('[data-control="group_by"]').selectOption('priority')
			await expect(preview.locator('.chart__mark').first()).toBeVisible()
		})
		await addWidget(page, 'donut', 'Done or not', async form => {
			await inAlpha(form)
			await form.locator('[data-control="group_by"]').selectOption('done')
			await expect(preview.locator('.chart__mark').first()).toBeVisible()
		})
		await addWidget(page, 'task_list', 'Tasks of Alpha', inAlpha)
		await addWidget(page, 'text', 'Notes', async form => {
			await form.locator('[contenteditable="true"]').first().fill('Reviewed every Monday')
		})
		await page.locator('[data-test="toggle-edit"]').click()

		// Every number matches the seeded tasks, also after a reload.
		const check = async () => {
			await expect(widgetCard(page, 'All Alpha tasks').locator('.number-tile')).toContainText('6')
			const bar = widgetCard(page, 'By priority')
			await expect(bar.locator('.chart__mark')).toHaveCount(3)
			expect(await tableOf(bar)).toEqual([['Low', '3 tasks'], ['High', '2 tasks'], ['Urgent', '1 task']])
			const donut = widgetCard(page, 'Done or not')
			await expect(donut.locator('.chart__mark')).toHaveCount(2)
			expect(await tableOf(donut)).toEqual([['Open', '4 tasks'], ['Done', '2 tasks']])
			const list = widgetCard(page, 'Tasks of Alpha')
			for (const task of TASKS) await expect(list).toContainText(task.title)
			await expect(list).not.toContainText('Task of another project')
			await expect(widgetCard(page, 'Notes')).toContainText('Reviewed every Monday')
		}
		await check()
		await page.reload()
		await check()

		const stored = await api.get(`/dashboards/${dashboardId}`)
		const titles = stored.widgets.map((w: {title: string}) => w.title)
		expect(titles).toEqual(expect.arrayContaining(['All Alpha tasks', 'By priority', 'Done or not', 'Tasks of Alpha', 'Notes']))
		watcher.finish()
	})

	test('a click on a segment lists its tasks, the chart has a table alternative and exports CSV', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const api = apiV2(apiContext, userToken)
		const {alpha} = await seed(apiContext, userToken)
		const dashboard = await api.post('/dashboards', {title: 'Priorities', project_id: 0})
		await api.put(`/dashboards/${dashboard.id}`, {
			title: 'Priorities',
			widgets: [
				{type: 'bar', title: 'By priority', x: 0, y: 0, w: 6, h: 4, config: {request: {project_id: alpha.id, group_by: 'priority'}}},
				{type: 'donut', title: 'Done or not', x: 6, y: 0, w: 6, h: 4, config: {request: {project_id: alpha.id, group_by: 'done'}}},
			],
		})

		await page.goto(`/dashboards/${dashboard.id}`)
		const bar = widgetCard(page, 'By priority')
		await expect(bar.locator('.chart__mark')).toHaveCount(3)

		// The table is the chart's text alternative: the same numbers, as a table with a caption and headers.
		const table = bar.getByRole('table', {name: 'Chart data'})
		await expect(table).toBeAttached()
		await expect(table.locator('thead th')).toHaveText(['Group', 'Value'])
		await expect(table.locator('caption')).toHaveText('By priority')
		expect(await tableOf(bar)).toEqual([['Low', '3 tasks'], ['High', '2 tasks'], ['Urgent', '1 task']])
		await expect(bar.getByRole('button', {name: 'High: 2 tasks'})).toBeVisible()

		// A click on a bar lists exactly the tasks of that segment.
		await bar.getByRole('button', {name: 'High: 2 tasks'}).click()
		const panel = page.locator('.side-panel, [role="dialog"]').filter({has: page.locator('[data-test="drilldown-count"]')})
		await expect(panel).toBeVisible()
		await expect(panel.locator('[data-test="drilldown-count"]')).toHaveText('2 tasks')
		await expect(panel).toContainText('Fix the login')
		await expect(panel).toContainText('Ship the update')
		await expect(panel).not.toContainText('Write the report')
		await expect(panel).not.toContainText('Restore the backup')

		// What is listed can be exported.
		const exported = page.waitForEvent('download')
		await panel.locator('[data-test="drilldown-export"]').click()
		const rows = await csvOf(await exported)
		expect(rows[0]).toMatch(/title/i)
		expect(rows.join('\n')).toContain('Fix the login')
		expect(rows.join('\n')).toContain('Ship the update')
		expect(rows.join('\n')).not.toContain('Write the report')
		expect(rows).toHaveLength(3)

		// Back, and the keyboard does the same: focus the "Urgent" bar and press Enter.
		await panel.getByRole('button', {name: 'Close'}).click()
		await expect(panel).toHaveCount(0)
		await bar.getByRole('button', {name: 'Urgent: 1 task'}).focus()
		await page.keyboard.press('Enter')
		await expect(panel.locator('[data-test="drilldown-count"]')).toHaveText('1 task')
		await expect(panel).toContainText('Restore the backup')
		await panel.getByRole('button', {name: 'Close'}).click()
		await expect(panel).toHaveCount(0)

		// The widget's own CSV has the groups and their numbers.
		const card = widgetCard(page, 'By priority')
		await card.getByRole('button', {name: 'Options for By priority'}).click()
		const widgetDownload = page.waitForEvent('download')
		await card.locator('[data-test="widget-export"]').click()
		const csv = (await csvOf(await widgetDownload)).join('\n')
		expect(csv).toMatch(/Low\D+3/)
		expect(csv).toMatch(/High\D+2/)
		expect(csv).toMatch(/Urgent\D+1/)

		// The donut: a segment lists the done tasks (by keyboard: the centre of a ring segment lies in the hole, where a click would miss).
		const donut = widgetCard(page, 'Done or not')
		await donut.getByRole('button', {name: /^Done: 2 tasks/}).focus()
		await page.keyboard.press('Enter')
		await expect(panel.locator('[data-test="drilldown-count"]')).toHaveText('2 tasks')
		await expect(panel).toContainText('Order the cake')
		await expect(panel).toContainText('Ship the update')
		watcher.finish()
	})

	test('a project dashboard is created by an admin and only read by a member', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const {alpha} = await seed(apiContext, userToken)
		const member = await createUser(apiContext, 2, 'member-user')
		await shareProject(apiContext, userToken, alpha.id, member.username, 1)

		await page.goto(`/projects/${alpha.id}/dashboards`)
		await page.locator('[data-test="create-default"]').click()
		await expect(page).toHaveURL(/\/dashboards\/\d+$/)
		const dashboardUrl = page.url()
		// Open tasks of Alpha only: four, one of them overdue.
		await expect(widgetCard(page, 'Open tasks').locator('.number-tile')).toContainText('4')
		await expect(widgetCard(page, 'Overdue tasks')).toContainText('Restore the backup')
		await expect(widgetCard(page, 'Overdue tasks')).not.toContainText('Write the report')
		await expect(page.locator('[data-test="toggle-edit"]')).toBeVisible()

		const other = await pageAs(browser, apiContext, member)
		const otherWatcher = watchBrowser(other, NO_WORKFLOW)
		// The project's only dashboard opens straight away.
		await other.goto(`/projects/${alpha.id}/dashboards`)
		await expect(other).toHaveURL(dashboardUrl)
		await expect(widgetCard(other, 'Open tasks').locator('.number-tile')).toContainText('4')
		await expect(other.locator('[data-test="toggle-edit"]')).toHaveCount(0)
		await expect(other.locator('[data-test="add-widget"]')).toHaveCount(0)

		// The server agrees: a member cannot change the dashboard.
		const id = Number(new URL(dashboardUrl).pathname.split('/').pop())
		const refused = await apiContext.put(`${apiV2(apiContext, member.token).base}/dashboards/${id}`, {
			headers: {Authorization: `Bearer ${member.token}`},
			data: {title: 'Hijacked', widgets: []},
		})
		expect(refused.status()).toBe(403)
		watcher.finish()
		otherWatcher.finish()
		await other.context().close()
	})
})
