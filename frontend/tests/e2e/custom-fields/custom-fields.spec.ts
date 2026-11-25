import {test, expect} from '../../support/fixtures'
import type {Page} from '@playwright/test'
import {updateUserSettings} from '../../support/updateUserSettings'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {createProject, createTask, createUser, shareProject, pageAs} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

// The project has no workflow: its 404 is a normal state the pages ask for.
const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]

type V2 = ReturnType<typeof apiV2>

interface Field {
	id: number
	title: string
	type: string
	options?: {id: number, title: string}[]
}

async function createField(api: V2, projectId: number, body: Record<string, unknown>): Promise<Field> {
	return await api.post(`/projects/${projectId}/custom-fields`, {description: '', required: false, config: {}, ...body})
}

async function fieldValues(api: V2, taskId: number): Promise<Record<number, unknown>> {
	const list = await api.get(`/tasks/${taskId}/custom-field-values`)
	return Object.fromEntries((list.items as {custom_field_id: number, value: unknown}[]).map(v => [v.custom_field_id, v.value]))
}

async function addFieldInSettings(page: Page, title: string, type: string, extra?: (page: Page) => Promise<void>) {
	await page.getByRole('button', {name: 'Add field'}).first().click()
	await page.getByLabel('Title', {exact: true}).fill(title)
	await page.getByLabel('Type', {exact: true}).selectOption(type)
	if (extra) await extra(page)
	const saved = page.waitForResponse(r => /\/projects\/\d+\/custom-fields$/.test(r.url()) && r.request().method() === 'POST')
	await page.locator('.custom-field-form').getByRole('button', {name: 'Add field'}).click()
	expect((await saved).status()).toBeLessThan(300)
	await expect(page.locator('.custom-field-form')).toHaveCount(0)
}

test.describe('Custom fields', () => {
	test('a project admin creates fields of several types in the settings', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const api = apiV2(apiContext, userToken)

		await page.goto(`/projects/${project.id}/settings/custom-fields`)
		await expect(page.getByRole('heading', {name: 'No custom fields yet'})).toBeVisible()

		await addFieldInSettings(page, 'Customer', 'text')
		await addFieldInSettings(page, 'Budget', 'number', async p => {
			await p.getByLabel('Minimum').fill('0')
			await p.getByLabel('Decimal places').fill('2')
		})
		await addFieldInSettings(page, 'Severity', 'select', async p => {
			await p.getByRole('button', {name: 'Add option'}).click()
			await p.getByLabel('Name of option 1').fill('Minor')
			await p.getByRole('button', {name: 'Add option'}).click()
			await p.getByLabel('Name of option 2').fill('Critical')
		})
		await addFieldInSettings(page, 'Review by', 'date')
		await addFieldInSettings(page, 'Quality', 'rating', async p => {
			await p.getByLabel('Maximum rating').fill('4')
			await p.getByText('Required', {exact: true}).click()
		})

		const table = page.locator('table.table')
		for (const [title, type] of [['Customer', 'Text'], ['Budget', 'Number'], ['Severity', 'Select'], ['Review by', 'Date'], ['Quality', 'Rating']]) {
			await expect(table.locator('tbody tr').filter({hasText: title})).toContainText(type!)
		}
		await expect(table.locator('tbody tr').filter({hasText: 'Quality'})).toContainText('Required')

		const stored = (await api.get(`/projects/${project.id}/custom-fields`)).items as {title: string, type: string, required: boolean, config: Record<string, number>, options?: {title: string}[]}[]
		const byTitle = Object.fromEntries(stored.map(f => [f.title, f]))
		expect(stored).toHaveLength(5)
		expect(byTitle.Budget!.config).toMatchObject({min: 0, precision: 2})
		expect(byTitle.Severity!.options!.map(o => o.title)).toEqual(['Minor', 'Critical'])
		expect(byTitle.Quality).toMatchObject({type: 'rating', required: true})
		expect(byTitle.Quality!.config.max_rating).toBe(4)

		// The type is fixed once the field exists.
		await page.getByRole('button', {name: 'Edit the field "Budget"'}).click()
		await expect(page.getByLabel('Type', {exact: true})).toBeDisabled()
		await page.locator('.custom-field-form').getByRole('button', {name: 'Cancel'}).click()

		// A field without a title is refused in the form, before anything is sent.
		await page.getByRole('button', {name: 'Add field'}).first().click()
		await page.locator('.custom-field-form').getByRole('button', {name: 'Add field'}).click()
		await expect(page.getByText('Please provide a title.')).toBeVisible()
		await page.locator('.custom-field-form').getByRole('button', {name: 'Cancel'}).click()

		// Deleting asks first and removes the field.
		await page.getByRole('button', {name: 'Delete the field "Customer"'}).click()
		const dialog = page.getByRole('dialog').filter({hasText: 'Delete the field "Customer"?'})
		await expect(dialog).toBeVisible()
		await dialog.getByRole('button', {name: 'Do it!'}).click()
		await expect(table.locator('tbody tr').filter({hasText: 'Customer'})).toHaveCount(0)
		expect((await api.get(`/projects/${project.id}/custom-fields`)).items).toHaveLength(4)
		watcher.finish()
	})

	test('values edited on the task persist after a reload', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const api = apiV2(apiContext, userToken)
		const task = await createTask(apiContext, userToken, project.id, 'Prepare the offer')
		const text = await createField(api, project.id, {title: 'Customer', type: 'text'})
		const number = await createField(api, project.id, {title: 'Budget', type: 'number', config: {min: 0, precision: 2}})
		const select = await createField(api, project.id, {title: 'Severity', type: 'select', options: [{title: 'Minor', color: '#00aa00', position: 1}, {title: 'Critical', color: '#cc0000', position: 2}]})
		const date = await createField(api, project.id, {title: 'Review by', type: 'date'})
		const rating = await createField(api, project.id, {title: 'Quality', type: 'rating', config: {max_rating: 5}})
		const checkbox = await createField(api, project.id, {title: 'Approved', type: 'checkbox'})

		await page.goto(`/tasks/${task.id}`)
		const row = (field: Field) => page.locator(`.task-view .property-custom-field-${field.id}`)

		const saveOf = () => page.waitForResponse(r => /\/tasks\/\d+\/custom-field-values\/\d+$/.test(r.url()) && r.request().method() === 'PUT')

		let saved = saveOf()
		await row(text).getByRole('textbox').fill('Acme Ltd')
		await row(text).getByRole('textbox').press('Enter')
		await saved

		saved = saveOf()
		await row(number).getByRole('spinbutton').or(row(number).getByRole('textbox')).fill('1234.5')
		await row(number).getByRole('spinbutton').or(row(number).getByRole('textbox')).press('Enter')
		await saved

		saved = saveOf()
		await row(select).getByRole('combobox').selectOption({label: 'Critical'})
		await saved

		saved = saveOf()
		await row(rating).getByRole('radio', {name: '3 of 5'}).click()
		await saved

		saved = saveOf()
		await row(checkbox).locator('label').click()
		await saved

		await row(date).locator('.datepicker .show').click()
		await row(date).locator('.datepicker__quick-select-date').first().click()
		saved = saveOf()
		await row(date).getByRole('button', {name: 'Confirm'}).click()
		await saved

		const expectedDate = (await fieldValues(api, task.id))[date.id] as string
		expect(expectedDate).toBeTruthy()
		expect(await fieldValues(api, task.id)).toMatchObject({
			[text.id]: 'Acme Ltd',
			[number.id]: 1234.5,
			[select.id]: select.options!.find(o => o.title === 'Critical')!.id,
			[rating.id]: 3,
			[checkbox.id]: true,
		})

		await page.reload()
		await expect(row(text).getByRole('textbox')).toHaveValue('Acme Ltd')
		await expect(row(number).getByRole('spinbutton').or(row(number).getByRole('textbox'))).toHaveValue('1234.5')
		await expect(row(select).getByRole('combobox')).toHaveValue(String(select.options!.find(o => o.title === 'Critical')!.id))
		await expect(row(rating).getByRole('radio', {name: '3 of 5'})).toHaveAttribute('aria-checked', 'true')
		await expect(row(date).locator('.datepicker .show')).not.toContainText('Not set')

		// A value outside the bounds is refused in the browser and never sent.
		let sent = 0
		page.on('request', r => {
			if (/\/custom-field-values\//.test(r.url()) && r.method() === 'PUT') sent++
		})
		const field = row(number).getByRole('spinbutton').or(row(number).getByRole('textbox'))
		await field.fill('-5')
		await field.press('Enter')
		await expect(row(number).getByText('The value must be at least 0.')).toBeVisible()
		expect(sent).toBe(0)

		// Clearing with the remove button deletes the value.
		await page.reload()
		const cleared = page.waitForResponse(r => /\/custom-field-values\/\d+$/.test(r.url()) && ['PUT', 'DELETE'].includes(r.request().method()))
		await page.getByRole('button', {name: 'Clear Customer'}).click()
		await cleared
		expect((await fieldValues(api, task.id))[text.id] ?? null).toBeNull()
		await expect(row(text).getByRole('textbox')).toHaveValue('')
		watcher.finish()
	})

	test('the rating stars work with the keyboard', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const api = apiV2(apiContext, userToken)
		const task = await createTask(apiContext, userToken, project.id, 'Rate me')
		const rating = await createField(api, project.id, {title: 'Quality', type: 'rating', config: {max_rating: 5}})

		await page.goto(`/tasks/${task.id}`)
		const group = page.locator(`.task-view .property-custom-field-${rating.id}`).getByRole('radiogroup')
		const star = (n: number) => group.getByRole('radio', {name: `${n} of 5`})
		const current = async () => (await fieldValues(api, task.id))[rating.id]

		// One tab stop: the first star while nothing is chosen.
		await expect(star(1)).toHaveAttribute('tabindex', '0')
		await expect(star(2)).toHaveAttribute('tabindex', '-1')
		await star(1).focus()

		await star(1).press('ArrowRight')
		await expect(star(2)).toBeFocused()
		await expect(star(2)).toHaveAttribute('aria-checked', 'true')
		await expect.poll(current).toBe(2)

		await star(2).press('End')
		await expect(star(5)).toBeFocused()
		await expect.poll(current).toBe(5)

		// Arrow keys wrap around.
		await star(5).press('ArrowRight')
		await expect(star(1)).toBeFocused()
		await expect.poll(current).toBe(1)
		await star(1).press('ArrowLeft')
		await expect(star(5)).toBeFocused()
		await expect.poll(current).toBe(5)

		await star(5).press('ArrowUp')
		await expect(star(4)).toBeFocused()
		await expect.poll(current).toBe(4)
		await star(4).press('Home')
		await expect(star(1)).toBeFocused()
		await expect.poll(current).toBe(1)

		// The chosen star is the tab stop after a reload; clicking it again clears the rating.
		await page.reload()
		await expect(star(1)).toHaveAttribute('aria-checked', 'true')
		await expect(star(1)).toHaveAttribute('tabindex', '0')
		await star(1).click()
		await expect.poll(async () => (await current()) ?? null).toBeNull()
		watcher.finish()
	})

	test('a field is shown as a table column and on the kanban cards', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const api = apiV2(apiContext, userToken)
		const budget = await createField(api, project.id, {title: 'Budget', type: 'number', config: {precision: 0}})
		const owner = await createField(api, project.id, {title: 'Customer', type: 'text'})
		const first = await createTask(apiContext, userToken, project.id, 'Offer for Acme')
		const second = await createTask(apiContext, userToken, project.id, 'Offer for Globex')
		await api.put(`/tasks/${first.id}/custom-field-values/${budget.id}`, {value: 250})
		await api.put(`/tasks/${second.id}/custom-field-values/${budget.id}`, {value: 40})
		await api.put(`/tasks/${first.id}/custom-field-values/${owner.id}`, {value: 'Acme Ltd'})

		// Off by default: neither view asks for the values nor shows them.
		await page.goto(`/projects/${project.id}/${project.views.table}`)
		await expect(page.getByRole('link', {name: 'Offer for Acme'})).toBeVisible()
		await expect(page.locator('.project-table table th').filter({hasText: 'Budget'})).toHaveCount(0)

		const withValues = page.waitForResponse(r => /\/tasks/.test(r.url()) && r.url().includes('custom_fields'))
		await page.getByRole('button', {name: 'Columns', exact: true}).click()
		await page.locator('.columns-filter').getByText('Budget', {exact: true}).click()
		await withValues
		await page.getByRole('button', {name: 'Columns', exact: true}).click()
		await expect(page.locator('.project-table table th').filter({hasText: 'Budget'})).toBeVisible()
		const rowOf = (title: string) => page.locator('.project-table tbody tr').filter({has: page.getByRole('link', {name: title, exact: true})})
		await expect(rowOf('Offer for Acme')).toContainText('250')
		await expect(rowOf('Offer for Globex')).toContainText('40')
		await expect(page.locator('.project-table table th').filter({hasText: 'Customer'})).toHaveCount(0)

		// The choice is the browser's: it is still there after a reload.
		await page.reload()
		await expect(page.locator('.project-table table th').filter({hasText: 'Budget'})).toBeVisible()
		await expect(rowOf('Offer for Acme')).toContainText('250')

		// Kanban: card fields.
		await page.goto(`/projects/${project.id}/${project.views.kanban}`)
		const card = (title: string) => page.locator('.kanban .task').filter({hasText: title})
		await expect(card('Offer for Acme')).toBeVisible()
		await expect(card('Offer for Acme')).not.toContainText('Acme Ltd')
		await page.getByRole('button', {name: 'Card fields'}).click()
		await page.locator('.card-fields-filter').getByText('Customer', {exact: true}).click()
		await expect(card('Offer for Acme')).toContainText('Acme Ltd')
		await expect(card('Offer for Globex')).not.toContainText('Acme Ltd')
		watcher.finish()
	})

	test('tasks are filtered by a field name', async ({authenticatedPage: page, apiContext, userToken}) => {
		const watcher = watchBrowser(page, NO_WORKFLOW)
		await updateUserSettings(apiContext, userToken, {frontend_settings: {filter_editor_mode: 'advanced'}})
		const project = await createProject(apiContext, userToken, 'Alpha')
		const api = apiV2(apiContext, userToken)
		const budget = await createField(api, project.id, {title: 'Budget', type: 'number', config: {precision: 0}})
		const prices = {'Cheap job': 40, 'Mid job': 120, 'Big job': 900}
		for (const [title, value] of Object.entries(prices)) {
			const task = await createTask(apiContext, userToken, project.id, title)
			await api.put(`/tasks/${task.id}/custom-field-values/${budget.id}`, {value})
		}

		await page.goto(`/projects/${project.id}/${project.views.table}`)
		await expect(page.getByRole('link', {name: 'Big job'})).toBeVisible()
		await expect(page.getByRole('link', {name: 'Cheap job'})).toBeVisible()

		await page.locator('.filter-container button').filter({hasText: 'Filters'}).click()
		const editor = page.locator('.filter-popup .filter-input .ProseMirror')
		await editor.click()
		await page.keyboard.type('Budget > 100')
		const filtered = page.waitForResponse(r => /\/projects\/\d+\/views\/\d+\/tasks/.test(r.url()) && r.url().includes('filter='))
		await page.locator('.filter-popup').getByRole('button', {name: 'Show results'}).click()
		const response = await filtered
		// The browser sends the id, not the name.
		expect(decodeURIComponent(response.url())).toContain(`cf_${budget.id}`)
		await expect(page.getByRole('link', {name: 'Big job'})).toBeVisible()
		await expect(page.getByRole('link', {name: 'Mid job'})).toBeVisible()
		await expect(page.getByRole('link', {name: 'Cheap job'})).toHaveCount(0)
		watcher.finish()
	})

	test('a member without admin rights fills values but gets no settings', async ({apiContext, userToken, browser}) => {
		const project = await createProject(apiContext, userToken, 'Alpha')
		const api = apiV2(apiContext, userToken)
		const field = await createField(api, project.id, {title: 'Customer', type: 'text'})
		const task = await createTask(apiContext, userToken, project.id, 'Shared task')
		const member = await createUser(apiContext, 2, 'member-user')
		await shareProject(apiContext, userToken, project.id, member.username, 1)

		const memberPage = await pageAs(browser, apiContext, member)
		const watcher = watchBrowser(memberPage, [...NO_WORKFLOW, /\/custom-fields/])
		await memberPage.goto(`/projects/${project.id}/settings/custom-fields`)
		await expect(memberPage.getByText('Only project admins can manage custom fields.')).toBeVisible()
		await expect(memberPage.getByRole('button', {name: 'Add field'})).toHaveCount(0)
		await expect(memberPage.getByRole('button', {name: 'Delete the field "Customer"'})).toHaveCount(0)

		// The API agrees: creating a field is refused.
		const refused = await apiContext.post(`${apiV2(apiContext, member.token).base}/projects/${project.id}/custom-fields`, {
			headers: {Authorization: `Bearer ${member.token}`},
			data: {title: 'Sneaky', type: 'text', config: {}},
		})
		expect(refused.status()).toBe(403)

		// Writing a value works.
		await memberPage.goto(`/tasks/${task.id}`)
		const input = memberPage.locator(`.task-view .property-custom-field-${field.id}`).getByRole('textbox')
		const saved = memberPage.waitForResponse(r => /\/custom-field-values\/\d+$/.test(r.url()) && r.request().method() === 'PUT')
		await input.fill('Written by a member')
		await input.press('Enter')
		await saved
		expect((await fieldValues(api, task.id))[field.id]).toBe('Written by a member')

		// A reader sees the value but cannot change it.
		const reader = await createUser(apiContext, 3, 'reader-user')
		await shareProject(apiContext, userToken, project.id, reader.username, 0)
		const readerPage = await pageAs(browser, apiContext, reader)
		await readerPage.goto(`/tasks/${task.id}`)
		await expect(readerPage.locator(`.task-view .property-custom-field-${field.id}`).getByRole('textbox')).toHaveAttribute('readonly', '')
		watcher.finish()
		await memberPage.context().close()
		await readerPage.context().close()
	})
})
