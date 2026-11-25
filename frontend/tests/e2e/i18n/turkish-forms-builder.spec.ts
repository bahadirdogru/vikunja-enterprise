import {test, expect} from '../../support/fixtures'
import type {Page} from '@playwright/test'
import {setupApiUrl} from '../../support/authenticateUser'
import {apiV1Url} from '../../support/apiUrl'
import {updateUserSettings} from '../../support/updateUserSettings'
import {watchBrowser} from '../../support/uiCheck'
import {createProject, listTasks} from '../../support/forkModules'

test.use({viewport: {width: 1280, height: 900}})

const NO_WORKFLOW = [/\/projects\/\d+\/workflow/]
const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:4173'
// The application talks to its user informally (sen): "gir", "seç", "kaydet". The public page is formal (siz): "girin", "seçin".
const FORMAL_IMPERATIVE = /(?<![\p{L}])(girin|seçin|kaydedin|kaldırın|yazın|verin|ekleyin|kontrol edin|yapıştırın|paylaşın|kopyalayın)(?![\p{L}])/u
const RAW_KEY = /^[a-z]+(\.[a-zA-Z_]+){2,}$/

async function texts(page: Page): Promise<string[]> {
	return await page.locator('body').evaluate(element => {
		const seen: string[] = []
		const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
		while (walker.nextNode()) {
			const node = walker.currentNode
			const text = (node.textContent ?? '').trim()
			const parent = node.parentElement
			if (text && parent && !['SCRIPT', 'STYLE'].includes(parent.tagName) && getComputedStyle(parent).display !== 'none') seen.push(text)
		}
		for (const el of element.querySelectorAll('[aria-label], [placeholder]')) {
			for (const attribute of ['aria-label', 'placeholder']) {
				const value = el.getAttribute(attribute)
				if (value) seen.push(value.trim())
			}
		}
		return seen
	})
}

test.describe('Turkish: building a form', () => {
	test('the builder speaks to the admin informally, its checks are Turkish, and the public page it makes is formal', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		test.setTimeout(90_000)
		const watcher = watchBrowser(page, NO_WORKFLOW)
		await updateUserSettings(apiContext, userToken, {language: 'tr-TR'})
		const project = await createProject(apiContext, userToken, 'Alfa')
		const label = await apiContext.put(`${apiV1Url()}/labels`, {headers: {Authorization: `Bearer ${userToken}`}, data: {title: 'Hata'}})
		expect(label.ok()).toBe(true)

		await page.goto(`/projects/${project.id}/settings/forms`)
		await page.getByRole('button', {name: 'Yeni form'}).first().click()
		await page.locator('#fb-title').fill('Destek isteği')

		// A required question left empty and a label field without labels: the checks answer in Turkish.
		await page.getByRole('button', {name: 'Alan ekle'}).click()
		await page.locator('#fb-add-list').getByRole('button', {name: /^Etiketler/}).click()
		await page.getByRole('button', {name: 'Formu kaydet'}).click()
		await expect(page.getByText('Sunulacak en az bir etiket seç.').first()).toBeVisible()
		await page.getByRole('group', {name: 'Gönderenin seçebileceği etiketler'}).getByLabel('Hata').check()

		await page.getByRole('tab', {name: 'Ayarlar'}).click()
		await page.getByRole('radio', {name: /Bağlantıya sahip herkes/}).check()
		// Labels cannot be asked on a public form.
		await expect(page.getByText('Bu alanlar herkese açık formda olamaz:')).toBeVisible()
		await page.getByRole('radio', {name: /Bu sunucuda hesabı olan herkes/}).check()
		await page.locator('#fb-s-redirect_url').fill('ftp://baska.example')
		await expect(page.getByText('http:// veya https:// ile başlayan bir web adresi gir.')).toBeVisible()
		await page.locator('#fb-s-redirect_url').fill('')
		await page.getByRole('radio', {name: /Bağlantıya sahip herkes/}).check()
		await page.getByRole('tab', {name: 'Alanlar'}).click()
		// Remove the label field to make the form public.
		await page.getByRole('button', {name: '"Etiketler" alanını formdan kaldır'}).click()
		await page.getByRole('tab', {name: 'Ayarlar'}).click()
		await page.locator('#fb-s-success_message').fill('Teşekkürler, bildirimini aldık.')

		const everything: string[] = []
		for (const tab of ['Alanlar', 'Ayarlar', 'Paylaş']) {
			await page.getByRole('tab', {name: tab}).click()
			everything.push(...await texts(page))
		}
		const saved = page.waitForResponse(r => /\/projects\/\d+\/forms$/.test(r.url()) && r.request().method() === 'POST')
		await page.getByRole('button', {name: 'Formu kaydet'}).click()
		const form = await (await saved).json()
		expect(form.access).toBe('public')
		await page.getByRole('tab', {name: 'Paylaş'}).click()
		await expect(page.locator('#fb-s-link')).toHaveValue(new RegExp(`/forms/${form.token}`))
		everything.push(...await texts(page))

		const joined = everything.join('\n')
		for (const turkish of ['Formun bölümleri', 'Kimler gönderebilir', 'Bağlantıyı kopyala', 'Yeni bağlantı oluştur']) expect(joined).toContain(turkish)
		expect(everything.filter(text => RAW_KEY.test(text)), 'raw keys in the builder').toEqual([])
		expect(joined.match(FORMAL_IMPERATIVE), 'the application must say "sen", not "siz"').toBeNull()
		for (const english of ['Who can submit', 'Copy link', 'Create a new link', 'Add field']) expect(joined).not.toContain(english)

		// The page the visitor gets is formal.
		const context = await browser.newContext({baseURL: BASE_URL, serviceWorkers: 'block', locale: 'tr-TR', viewport: {width: 1280, height: 900}})
		const visitor = await context.newPage()
		await setupApiUrl(visitor)
		const visitorWatcher = watchBrowser(visitor, [])
		await visitor.goto(`/forms/${form.token}`)
		await expect(visitor.getByRole('heading', {name: 'Destek isteği'})).toBeVisible()
		await visitor.getByRole('button', {name: 'Gönder'}).click()
		await expect(visitor.getByText('yanıtı kontrol edin', {exact: false})).toBeVisible()
		const visitorTexts = (await texts(visitor)).join('\n')
		expect(visitorTexts).toMatch(FORMAL_IMPERATIVE)
		await visitor.getByLabel(/Görev başlığı|Başlık/).first().fill('Yazıcı bozuldu')
		await visitor.getByRole('button', {name: 'Gönder'}).click()
		await expect(visitor.getByText('Teşekkürler, bildirimini aldık.')).toBeVisible()
		await expect.poll(async () => (await listTasks(apiContext, userToken, project.id)).map(t => t.title)).toEqual(['Yazıcı bozuldu'])
		visitorWatcher.finish()
		await context.close()
		watcher.finish()
	})
})
