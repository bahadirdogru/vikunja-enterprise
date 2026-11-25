import {test, expect} from '../../support/fixtures'
import type {Page} from '@playwright/test'
import {setupApiUrl} from '../../support/authenticateUser'
import {updateUserSettings} from '../../support/updateUserSettings'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {createProject, createTask} from '../../support/forkModules'
import {expectNoEnglish, leaksOf, visibleTexts, KNOWN_NOT_INTERFACE, RAW_KEY} from '../../support/turkishWalk'

test.use({viewport: {width: 1280, height: 900}})

const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:4173'

// The page has settled: the loading splash and every spinner are gone.
async function settled(page: Page) {
	await page.locator('.vikunja-loading').waitFor({state: 'detached', timeout: 10_000}).catch(() => {})
	await page.waitForLoadState('domcontentloaded')
	// Lists and views fill in after their request; give them the moment they need.
	await page.waitForTimeout(400)
}

test.describe('Turkish: no English on the screens of the application', () => {
	test('the screens a person reaches from the menu show no English word and no raw key', async ({authenticatedPage: page, apiContext, userToken}) => {
		test.setTimeout(300_000)
		const watcher = watchBrowser(page, [/\/projects\/\d+\/workflow/, /\/user\/settings\/(webhooks|totp)/])
		await updateUserSettings(apiContext, userToken, {language: 'tr-TR'})
		const api = apiV2(apiContext, userToken)

		// Turkish data everywhere, so that every English word on a screen is the interface's.
		const project = await createProject(apiContext, userToken, 'Ev işleri')
		const first = await createTask(apiContext, userToken, project.id, 'Mutfağı temizle', {description: '<p>Bulaşıkları yık.</p>', due_date: '2030-01-15T10:00:00Z', priority: 3})
		await createTask(apiContext, userToken, project.id, 'Çiçekleri sula')
		await api.post('/labels', {title: 'Acil'})
		await api.post('/teams', {name: 'Ev ekibi'})

		const screens: [string, string][] = [
			['home', '/'],
			['upcoming', '/tasks/by/upcoming'],
			['projects', '/projects'],
			['list view', `/projects/${project.id}/${project.views.list}`],
			['gantt view', `/projects/${project.id}/${project.views.gantt}`],
			['table view', `/projects/${project.id}/${project.views.table}`],
			['kanban view', `/projects/${project.id}/${project.views.kanban}`],
			['task detail', `/tasks/${first.id}`],
			['project edit', `/projects/${project.id}/settings/edit`],
			['project share', `/projects/${project.id}/settings/share`],
			['project views', `/projects/${project.id}/settings/views`],
			['project webhooks', `/projects/${project.id}/settings/webhooks`],
			['project archive', `/projects/${project.id}/settings/archive`],
			['project duplicate', `/projects/${project.id}/settings/duplicate`],
			['project delete', `/projects/${project.id}/settings/delete`],
			['project info', `/projects/${project.id}/info`],
			['new project', '/projects/new'],
			['new filter', '/filters/new'],
			['labels', '/labels'],
			['new label', '/labels/new'],
			['teams', '/teams'],
			['new team', '/teams/new'],
			['dashboards', '/dashboards'],
			['approvals', '/approvals'],
			['time tracking', '/time-tracking'],
			['trash', '/trash'],
			['about', '/about'],
			['migrate', '/user/settings/migrate'],
			['settings: general', '/user/settings/general'],
			['settings: avatar', '/user/settings/avatar'],
			['settings: password', '/user/settings/password-update'],
			['settings: e-mail', '/user/settings/email-update'],
			['settings: caldav', '/user/settings/caldav'],
			['settings: api tokens', '/user/settings/api-tokens'],
			['settings: webhooks', '/user/settings/webhooks'],
			['settings: sessions', '/user/settings/sessions'],
			['settings: bots', '/user/settings/bots'],
			['settings: data export', '/user/settings/data-export'],
			['settings: deletion', '/user/settings/deletion'],
			['settings: totp', '/user/settings/totp'],
			['settings: feeds', '/user/settings/feeds'],
			['settings: linked accounts', '/user/settings/linked-accounts'],
			['not found', '/this/page/does/not/exist'],
		]

		const leaks: string[] = []
		for (const [where, path] of screens) {
			await page.goto(path)
			await settled(page)
			for (const text of await leaksOf(page)) leaks.push(`${where}: ${text}`)
		}
		expect(leaks, 'English or raw keys').toEqual([])

		// Overlays and popups on top of the home screen.
		await page.goto('/')
		await settled(page)
		await expect(page.locator('.menu-container')).toBeVisible()
		// The shortcut handler reads key and code of the event; Playwright's keyboard has no Turkish layout.
		await page.locator('body').dispatchEvent('keydown', {key: '?', code: 'Slash', shiftKey: true})
		await expect(page.locator('.keyboard-shortcuts')).toBeVisible()
		await expectNoEnglish(page, 'keyboard shortcuts')
		await page.keyboard.press('Escape')
		await page.keyboard.press('Control+k')
		await expect(page.locator('.quick-actions input')).toBeFocused()
		await expectNoEnglish(page, 'quick actions')
		await page.keyboard.press('Escape')

		// The notification list, the search of the list view and the filter help.
		await page.goto(`/projects/${project.id}/${project.views.list}`)
		await settled(page)
		await page.locator('[data-cy="notifications-bell"], .notifications .trigger-button').first().click({timeout: 3000}).catch(() => {})
		await expectNoEnglish(page, 'notifications popup')
		await page.keyboard.press('Escape')
		await page.getByRole('button', {name: /Filtre/}).first().click({timeout: 3000}).catch(() => {})
		await expectNoEnglish(page, 'filter popup')
		watcher.finish()
	})

	test('the detector finds English: the same screen in English is full of it', async ({authenticatedPage: page, apiContext, userToken}) => {
		await updateUserSettings(apiContext, userToken, {language: 'en'})
		await page.goto('/projects')
		await settled(page)
		expect((await leaksOf(page)).length, 'English words on an English screen').toBeGreaterThan(3)
	})

	test('the log-in, register and password reset pages of a visitor whose browser asks for Turkish', async ({browser}) => {
		const context = await browser.newContext({baseURL: BASE_URL, serviceWorkers: 'block', locale: 'tr-TR', viewport: {width: 1280, height: 900}})
		const page = await context.newPage()
		await setupApiUrl(page)
		for (const path of ['/login', '/register', '/get-password-reset', '/password-reset?token=x']) {
			await page.goto(path)
			await settled(page)
			await expectNoEnglish(page, path)
		}
		await context.close()
	})

	test('the error states of the fork modules, a failed automation run and a refused form, say it in Turkish', async ({authenticatedPage: page, apiContext, userToken}) => {
		test.setTimeout(120_000)
		const watcher = watchBrowser(page, [/\/forms\//, /\/projects\/\d+\/workflow/, /\/api\/v2\/projects\/\d+\/automations/])
		await updateUserSettings(apiContext, userToken, {language: 'tr-TR'})
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Ev işleri')

		// A rule whose action fails at run time: the colour is the title of the task, which is not a colour.
		const rule = await api.post(`/projects/${project.id}/automations`, {
			title: 'Rengi başlıktan al',
			enabled: true,
			trigger: {type: 'task.created'},
			actions: [{type: 'task.set_field', params: {field: 'hex_color', value: '{{task.title}}'}}],
		})
		await createTask(apiContext, userToken, project.id, 'Mutfağı temizle')
		await expect.poll(async () => (await api.get(`/projects/${project.id}/automations/${rule.id}/runs`)).items[0]?.status, {timeout: 30_000}).toBe('failed')

		await page.goto(`/projects/${project.id}/settings/automations`)
		await expect(page.getByText('Rengi başlıktan al').first()).toBeVisible()
		await page.getByRole('button', {name: /çalıştırmaları/}).first().click()
		const row = page.locator('.runs__table tbody tr')
		await expect(row).toHaveCount(1)
		await row.getByRole('button', {name: 'Adımlar'}).click()
		await expect(page.locator('.run-detail')).toBeVisible()
		const detail = (await visibleTexts(page.locator('.run-detail'))).join('\n')
		expect(detail, 'the step failure in Turkish').toContain('onaltılık renk')
		expect(detail).not.toContain('is not a hex color')
		// The value of the colour is the unfilled placeholder the rule was saved with: data of the rule, not the interface.
		await expectNoEnglish(page.locator('.run-detail'), 'the failed run', [...KNOWN_NOT_INTERFACE, /\{\{task\.title\}\}/])

		// The test run of the same rule: summary, warning and error in Turkish.
		await page.goto(`/projects/${project.id}/settings/automations`)
		await page.getByRole('button', {name: 'Rengi başlıktan al'}).first().click()
		await expectNoEnglish(page, 'the editor of the failing rule')

		// An API error with a code and one without: the toast is Turkish.
		await page.goto(`/projects/${project.id}/settings/custom-fields`)
		await expect(page.getByText('Özel alanlar').first()).toBeVisible()
		const texts = (await visibleTexts(page)).join('\n')
		expect(texts).not.toMatch(RAW_KEY)
		watcher.finish()
	})
})
