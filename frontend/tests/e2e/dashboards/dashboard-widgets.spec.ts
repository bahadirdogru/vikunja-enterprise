import {test, expect} from '../../support/fixtures'
import type {APIRequestContext, Download, Locator, Page} from '@playwright/test'
import {ProjectFactory} from '../../factories/project'
import {TaskFactory} from '../../factories/task'
import {TimeEntryFactory} from '../../factories/time_entry'
import {updateUserSettings} from '../../support/updateUserSettings'
import {apiV1Url} from '../../support/apiUrl'
import {apiV2, shot, watchBrowser} from '../../support/uiCheck'

test.use({viewport: {width: 1280, height: 900}})

function sqlDateTime(d: Date): string {
	const pad = (n: number) => String(n).padStart(2, '0')
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// Project 1 (Alpha) has data, project 2 (Beta) has none. Three open and one finished task in the last 30 days, one task from before.
function dateOnly(daysFromToday: number): string {
	return sqlDateTime(new Date(Date.now() + daysFromToday * 86400_000)).slice(0, 10)
}

async function seed(request: APIRequestContext, token: string, userId: number) {
	await ProjectFactory.create(1, {id: 1, title: 'Alpha', owner_id: userId}, false)
	await ProjectFactory.create(1, {id: 2, title: 'Beta', owner_id: userId}, false)
	const api = apiV2(request, token)
	const workflow = await api.put('/projects/1/workflow', {
		statuses: [
			{title: 'To do', category: 'open', is_initial: true},
			{title: 'In progress', category: 'in_progress'},
			{title: 'Done', category: 'done'},
		],
		transitions: [],
		restrict_transitions: false,
	})
	const [todo, doing, done] = workflow.statuses

	const headers = {Authorization: `Bearer ${token}`}
	const taskIds: number[] = []
	for (const [title, priority] of [['Write the report', 3], ['Fix the login', 2], ['Plan the release', 1], ['Ship the update', 2]] as const) {
		const response = await request.put(`${apiV1Url()}/projects/1/tasks`, {headers, data: {title, priority}})
		expect(response.ok()).toBe(true)
		taskIds.push((await response.json()).id)
	}
	await api.post(`/tasks/${taskIds[1]}/status`, {to_status_id: doing.id})
	await api.post(`/tasks/${taskIds[2]}/status`, {to_status_id: doing.id})
	await api.post(`/tasks/${taskIds[3]}/status`, {to_status_id: doing.id})
	await api.post(`/tasks/${taskIds[3]}/status`, {to_status_id: done.id})
	expect(todo.id).toBeGreaterThan(0)

	// Created in the 30 days before the last 30 days: the "previous period" of a number widget.
	const old = new Date(Date.now() - 45 * 86400_000).toISOString()
	await TaskFactory.create(1, {id: 500, index: 500, project_id: 1, created_by_id: userId, title: 'An old task', created: old, updated: old}, false)

	const hour = 3600_000
	await TimeEntryFactory.create(3, {
		user_id: userId,
		project_id: 1,
		task_id: (i: number) => taskIds[i - 1],
		start_time: (i: number) => sqlDateTime(new Date(Date.now() - (i + 1) * 3 * hour)),
		end_time: (i: number) => sqlDateTime(new Date(Date.now() - (i + 1) * 3 * hour + i * hour)),
	}, false)

	const dashboard = await api.post('/dashboards', {title: 'Team overview', project_id: 0})
	return {api, dashboard, taskIds}
}

async function openAddWidget(page: Page) {
	await page.locator('[data-test="toggle-edit"]').click()
	await page.locator('[data-test="add-widget"]').click()
}

// Adds a widget of a type through the editor; `fill` configures it before it is saved.
async function addWidget(page: Page, type: string, title: string, fill?: (form: Locator) => Promise<void>) {
	await page.locator('[data-test="add-widget"]').click()
	await page.locator(`.type-picker [data-type="${type}"]`).click()
	const form = page.locator('[data-test="widget-form"]')
	await form.getByLabel('Title').fill(title)
	await fill?.(form)
}

async function saveWidget(page: Page, title: string) {
	await page.locator('[data-test="widget-save"]').click()
	await expect(widgetCard(page, title)).toBeVisible()
}

function widgetCard(page: Page, title: string): Locator {
	return page.locator('article.widget-card', {has: page.getByRole('heading', {name: title, exact: true})})
}

async function csvOf(download: Download): Promise<string[]> {
	const stream = await download.createReadStream()
	const chunks: Buffer[] = []
	for await (const chunk of stream) chunks.push(Buffer.from(chunk))
	return Buffer.concat(chunks).toString('utf8').replace(/^\uFEFF/, '').split(/\r?\n/).filter(line => line !== '')
}

async function exportFromMenu(page: Page, title: string, entry: 'widget-export' | 'widget-export-tasks' = 'widget-export') {
	const card = widgetCard(page, title)
	await card.getByRole('button', {name: `Options for ${title}`}).click()
	const download = page.waitForEvent('download')
	await card.locator(`[data-test="${entry}"]`).click()
	return await download
}

test.describe('Dashboard widgets', () => {
	test('adds the three new widget types in the editor and renders them with data', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page, [/\/projects\/\d+\/workflow/])
		const {dashboard} = await seed(apiContext, userToken, currentUser.id)

		await page.goto(`/dashboards/${dashboard.id}`)
		await openAddWidget(page)
		const picker = page.locator('.type-picker')
		await expect(picker.locator('[data-type="status_time"]')).toBeVisible()
		await expect(picker.locator('[data-type="workload"]')).toBeVisible()
		await expect(picker.locator('[data-type="time_tracking"]')).toBeVisible()
		await shot(page, 'dash-01-type-picker')

		await picker.locator('[data-type="status_time"]').click()
		const form = page.locator('[data-test="widget-form"]')
		await form.getByLabel('Title').fill('Time in status')
		await form.getByLabel('Tasks from').selectOption({label: 'Alpha'})
		await expect(page.locator('[data-test="widget-preview"] svg').first()).toBeVisible()
		await shot(page, 'dash-02-status-time-editor')
		await saveWidget(page, 'Time in status')

		await addWidget(page, 'workload', 'Workload', async f => {
			await f.getByLabel('Tasks from').selectOption({label: 'Alpha'})
			await expect(page.locator('[data-test="widget-preview"] svg').first()).toBeVisible()
			await shot(page, 'dash-03-workload-editor')
		})
		await saveWidget(page, 'Workload')

		await addWidget(page, 'time_tracking', 'Tracked time', async f => {
			await f.getByLabel('Tasks from').selectOption({label: 'Alpha'})
			await f.getByLabel('Add up by').selectOption('project')
			await expect(page.locator('[data-test="widget-preview"] svg').first()).toBeVisible()
			await shot(page, 'dash-04-time-tracking-editor')
		})
		await saveWidget(page, 'Tracked time')

		await page.locator('[data-test="toggle-edit"]').click()
		await page.reload()
		for (const title of ['Time in status', 'Workload', 'Tracked time']) {
			const card = widgetCard(page, title)
			await expect(card).toBeVisible()
			await expect(card.locator('svg, .number-tile').first()).toBeVisible()
			await expect(card.locator('.chart-state')).toHaveCount(0)
		}
		await shot(page, 'dash-05-three-widgets-with-data')

		// The saved configuration is what the API holds.
		const stored = await apiV2(apiContext, userToken).get(`/dashboards/${dashboard.id}`)
		expect(stored.widgets.map((w: {type: string}) => w.type).sort()).toEqual(['status_time', 'time_tracking', 'workload'])
		expect(stored.widgets.find((w: {type: string}) => w.type === 'time_tracking').config.group_by).toBe('project')
		watcher.finish()
	})

	test('shows an empty state for the new widget types when there is nothing to show', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page)
		const {api} = await seed(apiContext, userToken, currentUser.id)
		const dashboard = await api.post('/dashboards', {title: 'Nothing yet', project_id: 0})
		await api.put(`/dashboards/${dashboard.id}`, {
			title: 'Nothing yet',
			widgets: [
				{type: 'status_time', title: 'Time in status', x: 0, y: 0, w: 6, h: 4, config: {project_id: 2}},
				{type: 'workload', title: 'Workload', x: 6, y: 0, w: 6, h: 4, config: {project_id: 2}},
				{type: 'time_tracking', title: 'Tracked time', x: 0, y: 4, w: 6, h: 4, config: {project_id: 2, group_by: 'user'}},
			],
		})
		await page.goto(`/dashboards/${dashboard.id}`)
		for (const title of ['Time in status', 'Workload', 'Tracked time']) {
			await expect(widgetCard(page, title).locator('.chart-state.is-empty')).toBeVisible()
		}
		await shot(page, 'dash-06-empty-states')
		watcher.finish()
	})

	test('compares a number with the previous period', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page, [/\/projects\/\d+\/workflow/])
		const {dashboard} = await seed(apiContext, userToken, currentUser.id)
		await page.goto(`/dashboards/${dashboard.id}`)
		await openAddWidget(page)
		await page.locator('.type-picker [data-type="number"]').click()
		const form = page.locator('[data-test="widget-form"]')
		await form.getByLabel('Title').fill('New tasks')
		await form.getByLabel('Tasks from').selectOption({label: 'Alpha'})
		await form.getByLabel('Compare with the previous period').check()
		await form.getByLabel('Period', {exact: true}).selectOption('last30')
		await expect(page.locator('[data-test="widget-preview"] .number-tile__delta')).toBeVisible()
		await shot(page, 'dash-07-number-compare-editor')
		await saveWidget(page, 'New tasks')

		const card = widgetCard(page, 'New tasks')
		await expect(card.locator('.number-tile__delta')).toBeVisible()
		// Four tasks in the last 30 days against one before: up.
		await expect(card.locator('.number-tile__delta')).toHaveClass(/is-up/)
		await page.locator('[data-test="toggle-edit"]').click()
		await page.reload()
		await expect(widgetCard(page, 'New tasks').locator('.number-tile__delta')).toBeVisible()
		const stored = await apiV2(apiContext, userToken).get(`/dashboards/${dashboard.id}`)
		expect(stored.widgets[0].config.compare).toBe('previous_period')
		await shot(page, 'dash-08-number-compare')
		watcher.finish()
	})

	test('exports CSV from the widget menu and from the drill-down panel', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page, [/\/projects\/\d+\/workflow/])
		const {api, dashboard} = await seed(apiContext, userToken, currentUser.id)
		await api.put(`/dashboards/${dashboard.id}`, {
			title: 'Team overview',
			widgets: [
				{type: 'status_time', title: 'Time in status', x: 0, y: 0, w: 6, h: 4, config: {project_id: 1}},
				{type: 'workload', title: 'Workload', x: 6, y: 0, w: 6, h: 4, config: {project_id: 1}},
				{type: 'time_tracking', title: 'Tracked time', x: 0, y: 4, w: 6, h: 4, config: {project_id: 1, group_by: 'user'}},
				{type: 'bar', title: 'Tasks by priority', x: 6, y: 4, w: 6, h: 4, config: {request: {project_id: 1, group_by: 'priority'}}},
			],
		})
		await page.goto(`/dashboards/${dashboard.id}`)
		await expect(widgetCard(page, 'Tasks by priority').locator('.chart__mark').first()).toBeVisible()
		await shot(page, 'dash-09-all-widgets')

		for (const title of ['Time in status', 'Workload', 'Tracked time', 'Tasks by priority']) {
			const lines = await csvOf(await exportFromMenu(page, title))
			expect(lines.length, `${title}: a header and at least one row`).toBeGreaterThanOrEqual(2)
			expect(lines[0]!.split(',').length, `${title}: header ${lines[0]}`).toBeGreaterThanOrEqual(2)
			console.log(`UICHECK [csv ${title}] ${lines[0]} (+${lines.length - 1} rows)`)
		}
		// The tasks behind a widget.
		const tasks = await csvOf(await exportFromMenu(page, 'Workload', 'widget-export-tasks'))
		expect(tasks[0]).toMatch(/title/i)
		expect(tasks.length).toBeGreaterThanOrEqual(2)

		// Drill-down: click a bar, export what is listed.
		await widgetCard(page, 'Tasks by priority').locator('.chart__mark.is-selectable').first().click()
		const panel = page.locator('.side-panel, [role="dialog"]').filter({has: page.locator('[data-test="drilldown-export"]')})
		await expect(panel).toBeVisible()
		await shot(page, 'dash-10-drilldown')
		const download = page.waitForEvent('download')
		await panel.locator('[data-test="drilldown-export"]').click()
		const drill = await csvOf(await download)
		expect(drill.length).toBeGreaterThanOrEqual(2)
		expect(drill[0]).toMatch(/title/i)
		watcher.finish()
	})

	test('renders in dark mode and on a narrow screen', async ({authenticatedPage: page, apiContext, userToken, currentUser}) => {
		const watcher = watchBrowser(page, [/\/projects\/\d+\/workflow/])
		const {api, dashboard} = await seed(apiContext, userToken, currentUser.id)
		await api.put(`/dashboards/${dashboard.id}`, {
			title: 'Team overview',
			widgets: [
				{type: 'status_time', title: 'Time in status', x: 0, y: 0, w: 6, h: 4, config: {project_id: 1}},
				{type: 'workload', title: 'Workload', x: 6, y: 0, w: 6, h: 4, config: {project_id: 1}},
				{type: 'time_tracking', title: 'Tracked time', x: 0, y: 4, w: 6, h: 4, config: {project_id: 1, group_by: 'user'}},
				{type: 'number', title: 'New tasks', x: 6, y: 4, w: 6, h: 4, config: {compare: 'previous_period', request: {project_id: 1, time: {field: 'created', from: dateOnly(-30), to: dateOnly(0)}}}},
			],
		})
		await updateUserSettings(apiContext, userToken, {frontend_settings: {color_schema: 'dark'}})
		await page.goto(`/dashboards/${dashboard.id}`)
		await expect(widgetCard(page, 'Workload').locator('svg').first()).toBeVisible()
		await expect(page.locator('html')).toHaveClass(/dark/)
		await shot(page, 'dash-11-dark')
		await page.setViewportSize({width: 390, height: 844})
		await expect(widgetCard(page, 'Workload').locator('svg').first()).toBeVisible()
		await shot(page, 'dash-12-dark-mobile')
		watcher.finish()
	})
})
