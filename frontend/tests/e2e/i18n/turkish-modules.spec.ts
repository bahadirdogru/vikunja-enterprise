import {test, expect} from '../../support/fixtures'
import type {Locator, Page} from '@playwright/test'
import {login, setupApiUrl} from '../../support/authenticateUser'
import {updateUserSettings} from '../../support/updateUserSettings'
import {apiV2, watchBrowser} from '../../support/uiCheck'
import {createProject, createTask, createUser, putWorkflow, shareProject, DEFAULT_STATUSES} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:4173'

// What an untranslated string looks like on screen: an i18n key such as "customFields.form.title".
const RAW_KEY = /^[a-z]+(\.[a-zA-Z_]+){2,}$/

/** Every text a person can see or hear on the page, one entry per text node, plus the texts of labels, placeholders and titles. */
async function visibleTexts(scope: Page | Locator): Promise<string[]> {
	const root = 'locator' in scope ? (scope as Page).locator('body') : scope
	return await root.evaluate(element => {
		const seen: string[] = []
		const visible = (el: Element) => {
			const style = getComputedStyle(el)
			return style.display !== 'none' && style.visibility !== 'hidden'
		}
		const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
		while (walker.nextNode()) {
			const node = walker.currentNode
			const text = (node.textContent ?? '').trim()
			// Code samples and the menu of template variables (event.doer.name, inserted as {{event.doer.name}}) look like keys but are identifiers.
			if (text && node.parentElement && !['SCRIPT', 'STYLE', 'NOSCRIPT'].includes(node.parentElement.tagName) && !node.parentElement.closest('code, kbd, pre, .param-input__placeholders') && visible(node.parentElement)) seen.push(text)
		}
		for (const el of element.querySelectorAll('[aria-label], [placeholder], [title]')) {
			if (!visible(el)) continue
			for (const attribute of ['aria-label', 'placeholder', 'title']) {
				const value = el.getAttribute(attribute)
				if (value) seen.push(value.trim())
			}
		}
		return seen
	})
}

async function expectNoRawKeys(scope: Page | Locator, where: string) {
	const raw = (await visibleTexts(scope)).filter(text => RAW_KEY.test(text))
	expect(raw, `raw i18n keys on ${where}`).toEqual([])
}

async function expectTurkish(page: Page, where: string, present: string[], absentEnglish: string[]) {
	await expectNoRawKeys(page, where)
	const text = (await visibleTexts(page)).join('\n')
	for (const expected of present) expect(text, `${where}: "${expected}" in Turkish`).toContain(expected)
	for (const english of absentEnglish) expect(text, `${where}: English "${english}" must not show`).not.toContain(english)
}

test.describe('Turkish: one screen per module without raw keys', () => {
	test('the application screens of the six modules are Turkish', async ({authenticatedPage: page, apiContext, userToken}) => {
		test.setTimeout(120_000)
		const watcher = watchBrowser(page, [...NO_WORKFLOW, /\/api\/v2\/forms\//])
		await updateUserSettings(apiContext, userToken, {language: 'tr-TR'})
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const approver = await createUser(apiContext, 2, 'approver-user')
		await shareProject(apiContext, userToken, project.id, approver.username, 1)

		// Data of every module, through the API.
		await api.post(`/projects/${project.id}/custom-fields`, {title: 'Müşteri puanı', type: 'rating', description: '', required: false, config: {max_rating: 5}})
		await api.post(`/projects/${project.id}/custom-fields`, {title: 'Önem', type: 'select', description: '', required: true, config: {}, options: [{title: 'Düşük', color: '#00aa00', position: 1}, {title: 'Yüksek', color: '#cc0000', position: 2}]})
		await putWorkflow(apiContext, userToken, project.id, {restrict_transitions: false, statuses: DEFAULT_STATUSES, transitions: [{from_status_id: -1, to_status_id: -2, title: 'Başla', require_comment: true}]})
		const task = await createTask(apiContext, userToken, project.id, 'Raporu yaz')
		await api.post(`/tasks/${task.id}/approvals`, {title: 'Kapak tasarımı', approver_ids: [approver.id]})
		await api.post(`/projects/${project.id}/automations`, {
			title: 'Acil işlere yorum ekle',
			enabled: true,
			trigger: {type: 'task.created'},
			conditions: {all: [{field: 'title', op: 'like', value: 'acil'}]},
			actions: [{type: 'task.add_comment', params: {text: 'Görüldü'}}],
		})
		await api.post(`/projects/${project.id}/forms`, {title: 'Destek isteği', description: '', access: 'public', enabled: true, fields: [{target: 'title', label: 'Konu', required: true}], settings: {}})

		// Custom fields: the settings and the form for a new field.
		await page.goto(`/projects/${project.id}/settings/custom-fields`)
		await expect(page.getByText('Özel alanlar').first()).toBeVisible()
		await expectTurkish(page, 'custom field settings', ['Özel alanlar', 'Alan ekle', 'Puan', 'Zorunlu'], ['Custom fields', 'Add field', 'Required'])
		await page.getByRole('button', {name: 'Alan ekle'}).first().click()
		await page.getByLabel('Tür', {exact: true}).selectOption('select')
		await expectTurkish(page, 'custom field form', ['Başlık', 'Seçenek ekle'], ['Add option', 'Title'])

		// Workflow: the editor.
		await page.goto(`/projects/${project.id}/settings/workflow`)
		await expect(page.getByText('Durumlar').first()).toBeVisible()
		await expectTurkish(page, 'workflow editor', ['Durumlar', 'Geçişler', 'İş akışı'], ['Statuses', 'Transitions', 'Workflow'])

		// The task: status, custom fields and the approval banner.
		await page.goto(`/tasks/${task.id}`)
		await expect(page.locator('[data-cy="approval-banner"]')).toBeVisible()
		await page.getByRole('button', {name: 'Durum geçmişi'}).click()
		await expectTurkish(page, 'task detail', ['Durum', 'Durum geçmişi', 'Müşteri puanı', 'Kapak tasarımı', 'Talep eden', 'Karar şekli', 'Talebi iptal et'], ['Status history', 'Requested by', 'Cancel request'])
		await page.getByRole('button', {name: /^Durum: .*\. Durumu değiştir$/}).click()
		await expect(page.getByRole('menuitem').first()).toBeVisible()
		await expectTurkish(page, 'status menu', ['Durumu şuna değiştir'], ['Change status to'])
		await page.keyboard.press('Escape')

		// Approvals: the page of the approver.
		const other = await page.context().browser()!.newContext({baseURL: BASE_URL, serviceWorkers: 'block', viewport: {width: 1280, height: 900}})
		const otherPage = await other.newPage()
		await updateUserSettings(apiContext, approver.token, {language: 'tr-TR'})
		await login(otherPage, apiContext, {username: approver.username})
		await otherPage.goto('/approvals')
		await expect(otherPage.locator('[data-cy="approval-row"]')).toBeVisible()
		await expectTurkish(otherPage, 'approvals', ['Onaylar', 'Beni bekleyenler', 'Onayla', 'Reddet', 'Talep eden'], ['Waiting for me', 'Approve', 'Reject', 'Requested by'])
		await otherPage.getByRole('button', {name: 'Onayla'}).click()
		await expectTurkish(otherPage, 'approval dialog', ['Yorum (isteğe bağlı)'], ['Comment (optional)'])
		await other.close()

		// Automation: the list, the editor and the (empty) run history.
		await page.goto(`/projects/${project.id}/settings/automations`)
		await expect(page.getByText('Acil işlere yorum ekle').first()).toBeVisible()
		await expectTurkish(page, 'automation list', ['Otomasyonlar', 'Yeni kural', 'Gizli anahtarlar'], ['New rule', 'Secrets'])
		await page.getByRole('button', {name: 'Acil işlere yorum ekle'}).first().click()
		await expectTurkish(page, 'automation editor', ['Bu olduğunda', 'Yalnızca şu koşulda', 'Şunu yap'], ['When this happens', 'Only if', 'Do this'])
		await page.goto(`/projects/${project.id}/settings/automations`)
		await page.getByRole('button', {name: /çalıştırmaları/}).first().click()
		await expectTurkish(page, 'automation runs', ['Bu kural henüz çalışmadı.'], ['This rule has not run yet.'])

		// Dashboards: the list and a new dashboard with its default widgets and charts.
		await page.goto('/dashboards')
		await page.locator('[data-test="new-dashboard"]').click()
		await page.getByLabel('Ad', {exact: true}).fill('Ekip panosu')
		await page.locator('[data-test="create-submit"]').click()
		await expect(page).toHaveURL(/\/dashboards\/\d+$/)
		await expect(page.locator('article.widget-card').first()).toBeVisible()
		await expect(page.locator('article.widget-card .chart__mark').first()).toBeVisible()
		await expectTurkish(page, 'dashboard', ['Açık görevler', 'Grafik verisi'], ['Open tasks', 'Chart data', 'Overdue tasks'])
		await page.locator('[data-test="toggle-edit"]').click()
		await page.locator('[data-test="add-widget"]').click()
		await expectTurkish(page, 'widget picker', ['Bileşen ekle'], ['Add widget', 'Bar chart'])

		// Forms: the list and the builder.
		await page.goto(`/projects/${project.id}/settings/forms`)
		await expect(page.getByText('Destek isteği').first()).toBeVisible()
		await expectTurkish(page, 'forms list', ['Formlar', 'Yeni form'], ['New form'])
		await page.getByRole('button', {name: 'Yeni form'}).first().click()
		await expectTurkish(page, 'form builder', ['Alanlar', 'Alan ekle'], ['Add field', 'Fields'])
		watcher.finish()
	})

	test('the public form page is Turkish and speaks to the submitter formally', async ({apiContext, userToken, browser}) => {
		const api = apiV2(apiContext, userToken)
		const project = await createProject(apiContext, userToken, 'Alpha')
		const form = await api.post(`/projects/${project.id}/forms`, {
			title: 'Destek isteği',
			description: '',
			access: 'public',
			enabled: true,
			fields: [{target: 'title', label: 'Konu', required: true}],
			settings: {collect_name: true, name_required: true, collect_email: true},
		})
		const closed = await api.post(`/projects/${project.id}/forms`, {title: 'Kapalı form', description: '', access: 'public', enabled: false, fields: [{target: 'title', label: 'Konu', required: true}], settings: {}})
		const members = await api.post(`/projects/${project.id}/forms`, {title: 'Üyeler', description: '', access: 'members', enabled: true, fields: [{target: 'title', label: 'Konu', required: true}], settings: {}})

		// A visitor whose browser asks for Turkish.
		const context = await browser.newContext({baseURL: BASE_URL, serviceWorkers: 'block', locale: 'tr-TR', viewport: {width: 1280, height: 900}})
		const page = await context.newPage()
		await setupApiUrl(page)
		const watcher = watchBrowser(page, [/\/forms\//])

		await page.goto(`/forms/${form.token}`)
		await expect(page.getByRole('heading', {name: 'Destek isteği'})).toBeVisible()
		await expectTurkish(page, 'public form', ['Hakkınızda', 'Adınız', 'E-posta adresiniz', 'Gönder'], ['About you', 'Your name', 'Send'])
		await page.getByRole('button', {name: 'Gönder'}).click()
		await expect(page.getByText('Bu alan zorunlu.').first()).toBeVisible()
		await expectTurkish(page, 'public form with errors', ['Bu alan zorunlu.', 'yanıtı kontrol edin'], ['This field is required', 'Please check'])

		// The formal register: "Adınız", "Hakkınızda", "kontrol edin", never the informal imperative of the application.
		const text = (await visibleTexts(page)).join('\n')
		expect(text).not.toMatch(/(?<![\p{L}])(Adın|E-posta adresin|kontrol et)(?![\p{L}])/u)

		await page.getByLabel('Konu').fill('Yazıcı bozuldu')
		await page.getByLabel(/Adınız/).fill('Ayşe Yılmaz')
		await page.getByRole('button', {name: 'Gönder'}).click()
		await expect(page.getByText('Teşekkürler, yanıtınız gönderildi.')).toBeVisible()
		await expectTurkish(page, 'public form thanks', ['Teşekkürler, yanıtınız gönderildi.'], ['Thank you'])

		// Closed and sign-in screens.
		await page.goto(`/forms/${closed.token}`)
		await expect(page.getByText('Bu form şu an kapalı.')).toBeVisible()
		await expectTurkish(page, 'closed form', ['Daha sonra tekrar deneyin ya da formu size gönderen kişiye sorun.'], ['switched off'])
		await page.goto(`/forms/${members.token}`)
		await expect(page.getByRole('heading', {name: 'Lütfen giriş yapın'})).toBeVisible()
		await expectTurkish(page, 'sign-in prompt', ['Giriş yaptıktan sonra forma geri döneceksiniz.', 'Giriş yap'], ['Please sign in'])
		await page.goto('/forms/this-token-does-not-exist')
		await expect(page.getByRole('heading', {name: 'Bu form yok'})).toBeVisible()
		await expectNoRawKeys(page, 'unknown form')
		watcher.finish()
		await context.close()
	})
})
