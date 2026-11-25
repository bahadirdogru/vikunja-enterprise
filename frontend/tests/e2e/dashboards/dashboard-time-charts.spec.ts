import {test, expect} from '../../support/fixtures'
import type {APIRequestContext, Locator, Page} from '@playwright/test'
import {ProjectFactory} from '../../factories/project'
import {TaskFactory} from '../../factories/task'
import {TaskStatusHistoryFactory} from '../../factories/task_status_history'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {putWorkflow} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
const DAY = 86400_000

function pad(n: number) {
	return String(n).padStart(2, '0')
}

// Midday of a day some days ago, in UTC like the days of the report (the test user has no time zone of their own): far
// from any midnight, and the same day as the server counts whatever the clock of the machine says.
function middayAgo(days: number): Date {
	const d = new Date(Date.now() - days * DAY)
	d.setUTCHours(12, 0, 0, 0)
	return d
}

function sqlDateTime(d: Date): string {
	return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`
}

function dayKey(d: Date): string {
	return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
}

// Six tasks in Alpha. Created: two five days ago, one three days ago, three yesterday. Finished: two three days ago, one yesterday.
const TASKS = [
	{title: 'Write the report', created: 5, done: 3},
	{title: 'Fix the login', created: 5, done: 3},
	{title: 'Plan the release', created: 3, done: 1},
	{title: 'Ship the update', created: 1, done: null},
	{title: 'Order the cake', created: 1, done: null},
	{title: 'Restore the backup', created: 1, done: null},
]

async function seed(request: APIRequestContext, token: string, userId: number) {
	await ProjectFactory.create(1, {id: 1, title: 'Alpha', owner_id: userId}, false)
	const workflow = await putWorkflow(request, token, 1, {
		restrict_transitions: false,
		statuses: [
			{id: -1, title: 'To do', category: 'open', is_initial: true},
			{id: -2, title: 'In progress', category: 'in_progress'},
			{id: -3, title: 'Done', category: 'done'},
		],
		transitions: [],
	})
	const status = Object.fromEntries(workflow.statuses.map(s => [s.title, s.id]))
	let id = 0
	for (const task of TASKS) {
		id++
		const created = middayAgo(task.created).toISOString()
		await TaskFactory.create(1, {
			id,
			index: id,
			project_id: 1,
			created_by_id: userId,
			title: task.title,
			created,
			updated: created,
			done: task.done !== null,
			done_at: task.done === null ? null : middayAgo(task.done).toISOString(),
		}, false)
	}
	// The history of the first three tasks, with moments chosen so the times in a status are whole days:
	// task 1: To do 2 days, In progress 3 days, then Done. Task 2: 1 day, 3 days. Task 3: 6 days in To do, then Done.
	const change = (taskId: number, from: string | null, to: string, daysAgo: number) => ({
		task_id: taskId,
		from_status_id: from === null ? null : status[from],
		to_status_id: status[to],
		from_title: from ?? '',
		to_title: to,
		created: sqlDateTime(new Date(Date.now() - daysAgo * DAY)),
	})
	const history = [
		change(1, null, 'To do', 10), change(1, 'To do', 'In progress', 8), change(1, 'In progress', 'Done', 5),
		change(2, null, 'To do', 10), change(2, 'To do', 'In progress', 9), change(2, 'In progress', 'Done', 6),
		change(3, null, 'To do', 10), change(3, 'To do', 'Done', 4),
	]
	let n = 0
	for (const row of history) {
		n++
		await TaskStatusHistoryFactory.create(1, {...row, id: n}, false)
	}
	const dashboard = await apiV2(request, token).post('/dashboards', {title: 'Over time', project_id: 0})
	return {dashboard, status}
}

function widgetCard(page: Page, title: string): Locator {
	return page.locator('article.widget-card', {has: page.getByRole('heading', {name: title, exact: true})})
}

async function tableOf(card: Locator): Promise<string[][]> {
	return await card.locator('table.chart-table tbody tr').evaluateAll(trs => trs.map(tr => [...tr.children].map(cell => (cell.textContent ?? '').trim())))
}

async function addWidget(page: Page, type: string, title: string, fill: (form: Locator) => Promise<void>) {
	await page.locator('[data-test="add-widget"]').click()
	await page.locator(`.type-picker [data-type="${type}"]`).click()
	const form = page.locator('[data-test="widget-form"]')
	await form.getByLabel('Title').fill(title)
	await fill(form)
	await expect(page.locator('[data-test="widget-preview"] svg').first()).toBeVisible()
	await page.locator('[data-test="widget-save"]').click()
	await expect(widgetCard(page, title)).toBeVisible()
}

test.describe('Dashboards: charts over time', () => {
	test('line, created-vs-done and time-in-status widgets are built in the editor and show the seeded numbers', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		test.setTimeout(90_000)
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const {dashboard} = await seed(apiContext, userToken, currentUser.id)

		await page.goto(`/dashboards/${dashboard.id}`)
		await page.locator('[data-test="toggle-edit"]').click()

		await addWidget(page, 'created_vs_done', 'Created and finished', async form => {
			await form.getByLabel('Tasks from').selectOption({label: 'Alpha'})
			await form.getByLabel('Period', {exact: true}).selectOption('last7')
		})
		await addWidget(page, 'line', 'New tasks per day', async form => {
			await form.getByLabel('Tasks from').selectOption({label: 'Alpha'})
			await form.getByLabel('Period', {exact: true}).selectOption('last7')
		})
		await addWidget(page, 'status_time', 'Time in status', async form => {
			await form.getByLabel('Tasks from').selectOption({label: 'Alpha'})
		})
		await page.locator('[data-test="toggle-edit"]').click()
		await page.reload()

		const createdVsDone = widgetCard(page, 'Created and finished')
		const line = widgetCard(page, 'New tasks per day')
		const status = widgetCard(page, 'Time in status')
		await expect(createdVsDone.locator('svg').first()).toBeVisible()

		// One row per day of the last seven days and today. The table is the text alternative of the chart: the same numbers.
		const days = Array.from({length: 8}, (_, i) => middayAgo(7 - i))
		const label = (d: Date) => d.toLocaleDateString('en-US', {month: 'short', day: 'numeric'})
		const count = (n: number) => n === 0 ? 'no tasks' : n === 1 ? '1 task' : `${n} tasks`
		const createdOn = (d: Date) => TASKS.filter(t => dayKey(middayAgo(t.created)) === dayKey(d)).length
		const doneOn = (d: Date) => TASKS.filter(t => t.done !== null && dayKey(middayAgo(t.done)) === dayKey(d)).length
		// Guard the expectation itself: 6 created, 3 finished.
		expect(days.reduce((sum, d) => sum + createdOn(d), 0)).toBe(6)
		expect(days.reduce((sum, d) => sum + doneOn(d), 0)).toBe(3)

		await expect(createdVsDone.getByRole('table', {name: 'Chart data'})).toBeAttached()
		await expect(createdVsDone.locator('table.chart-table thead th')).toHaveText(['Date', 'Created', 'Completed'])
		expect(await tableOf(createdVsDone)).toEqual(days.map(d => [label(d), count(createdOn(d)), count(doneOn(d))]))
		expect(await tableOf(line)).toEqual(days.map(d => [label(d), count(createdOn(d))]))

		// The time in a status, in hours: To do 2, 1 and 6 days, In progress 3 and 3 days.
		await expect(status).toContainText('3 tasks')
		expect(await tableOf(status)).toEqual([['To do', '72 h'], ['In progress', '72 h']])
		await status.getByRole('button', {name: 'Median'}).click()
		expect(await tableOf(status)).toEqual([['To do', '48 h'], ['In progress', '72 h']])
		await status.getByRole('button', {name: 'Total'}).click()
		expect(await tableOf(status)).toEqual([['To do', '216 h'], ['In progress', '144 h']])

		// The configuration that was stored is what drew them.
		const stored = await apiV2(apiContext, userToken).get(`/dashboards/${dashboard.id}`)
		expect(stored.widgets.map((w: {type: string}) => w.type).sort()).toEqual(['created_vs_done', 'line', 'status_time'])
		watcher.finish()
	})
})
