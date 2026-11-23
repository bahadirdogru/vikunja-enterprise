import {test, expect} from '../../support/fixtures'
import type {Page} from '@playwright/test'
import {ProjectFactory} from '../../factories/project'
import {setupApiUrl} from '../../support/authenticateUser'
import {apiV2, shot, solidPng, watchBrowser} from '../../support/uiCheck'

type V2 = ReturnType<typeof apiV2>

const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:4173'

async function createForm(api: V2, overrides: Record<string, unknown> = {}) {
	return await api.post('/projects/1/forms', {
		title: 'Support request',
		description: '<p>Tell us what is wrong. Our <a href="https://example.com/policy">support policy</a> applies.</p>',
		access: 'public',
		enabled: true,
		fields: [
			{target: 'title', label: 'Subject', required: true},
			{target: 'description', label: 'Details'},
		],
		settings: {},
		...overrides,
	})
}

// WCAG contrast ratio of the colours the browser computed for an element.
async function buttonColors(page: Page) {
	return await page.getByRole('button', {name: 'Send'}).evaluate(el => {
		const parse = (value: string) => (value.match(/[\d.]+/g) ?? []).slice(0, 3).map(Number)
		const lin = (c: number) => {
			const s = c / 255
			return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
		}
		const lum = ([r, g, b]: number[]) => 0.2126 * lin(r!) + 0.7152 * lin(g!) + 0.0722 * lin(b!)
		const style = getComputedStyle(el)
		const background = parse(style.backgroundColor)
		const text = parse(style.color)
		const [hi, lo] = [lum(background), lum(text)].sort((a, b) => b - a)
		return {background: style.backgroundColor, text: style.color, ratio: (hi! + 0.05) / (lo! + 0.05)}
	})
}

test.use({viewport: {width: 1280, height: 900}})

// Contrast of a link in the description against the card it sits on.
async function linkContrast(page: Page) {
	return await page.locator('.fp__description a').evaluate(el => {
		const parse = (value: string) => (value.match(/[\d.]+/g) ?? []).slice(0, 3).map(Number)
		const lin = (c: number) => {
			const s = c / 255
			return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
		}
		const lum = ([r, g, b]: number[]) => 0.2126 * lin(r!) + 0.7152 * lin(g!) + 0.0722 * lin(b!)
		const color = getComputedStyle(el).color
		const background = getComputedStyle(el.closest('.fp__card')!).backgroundColor
		const [hi, lo] = [lum(parse(color)), lum(parse(background))].sort((a, b) => b - a)
		return {color, background, ratio: (hi! + 0.05) / (lo! + 0.05)}
	})
}

test.describe('Form branding', () => {
	test.beforeEach(async ({currentUser}) => {
		await ProjectFactory.create(1, {id: 1, owner_id: currentUser.id}, false)
	})

	test('edits the branding in the builder and shows it on the public page', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		// A project without a workflow answers 404 on purpose.
		const watcher = watchBrowser(page, [/\/projects\/1\/workflow/])
		const api = apiV2(apiContext, userToken)
		const form = await createForm(api)

		await page.goto('/projects/1/settings/forms')
		await page.getByRole('button', {name: /^Edit /}).click()
		await page.getByRole('tab', {name: 'Settings'}).click()

		const branding = page.locator('.fb__branding')
		await expect(branding).toBeVisible()
		await shot(page, 'forms-01-branding-empty')

		await branding.getByLabel('Brand name').fill('Acme Support')
		await branding.locator('#fb-s-accent_color').fill('#b3261e')
		await branding.getByRole('checkbox').check()
		await shot(page, 'forms-02-branding-filled')

		const saved = page.waitForResponse(r => r.url().includes('/projects/1/forms/') && r.request().method() === 'PUT')
		await page.getByRole('button', {name: 'Save'}).click()
		await saved

		const stored = await api.get(`/projects/1/forms/${form.id}`)
		expect(stored.brand_name).toBe('Acme Support')
		expect(stored.accent_color).toBe('#b3261e')
		expect(stored.hide_footer).toBe(true)

		// The logo is its own upload: it is stored right away.
		await expect(branding.getByRole('button', {name: 'Remove the logo'})).toHaveCount(0)
		await branding.locator('.fb__logo-input').setInputFiles({name: 'logo.png', mimeType: 'image/png', buffer: solidPng(160, 48, [25, 115, 255])})
		await expect(branding.locator('.fb__logo-preview')).toBeVisible()
		await expect(branding.getByRole('button', {name: 'Replace the logo'})).toBeVisible()
		expect((await api.get(`/projects/1/forms/${form.id}`)).has_brand_logo).toBe(true)
		await expect.poll(() => branding.locator('.fb__logo-preview').evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(160)
		await shot(page, 'forms-03-logo-uploaded')

		await branding.locator('.fb__logo-input').setInputFiles({name: 'logo2.png', mimeType: 'image/png', buffer: solidPng(80, 80, [212, 53, 28])})
		await expect.poll(() => branding.locator('.fb__logo-preview').evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(80)

		// A file that is not an image is refused before it is sent.
		await branding.locator('.fb__logo-input').setInputFiles({name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello')})
		await expect(branding.getByRole('alert')).toContainText('PNG, JPEG, GIF or WebP')
		await shot(page, 'forms-04-logo-refused')

		// The public page, as an anonymous visitor.
		const visitor = await browser.newContext({baseURL: BASE_URL, viewport: {width: 1280, height: 900}})
		const publicPage = await visitor.newPage()
		const publicWatcher = watchBrowser(publicPage)
		await setupApiUrl(publicPage)
		await publicPage.goto(`/forms/${form.token}`)
		await expect(publicPage.locator('.fp__brand-name')).toHaveText('Acme Support')
		await expect(publicPage.locator('.fp__brand-logo')).toBeVisible()
		await expect.poll(() => publicPage.locator('.fp__brand-logo').evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(80)
		// The footer is hidden.
		await expect(publicPage.locator('.fp__footer')).toHaveCount(0)
		await shot(publicPage, 'forms-05-public-brand-no-footer')

		await visitor.close()
		watcher.finish()
		publicWatcher.finish()
	})

	test('removes the logo and brings the footer back', async ({authenticatedPage: page, apiContext, userToken, browser}) => {
		const watcher = watchBrowser(page, [/\/projects\/1\/workflow/])
		const api = apiV2(apiContext, userToken)
		const form = await createForm(api, {brand_name: 'Acme Support'})
		await apiContext.put(`${api.base}/projects/1/forms/${form.id}/logo`, {
			headers: {Authorization: `Bearer ${userToken}`},
			multipart: {logo: {name: 'logo.png', mimeType: 'image/png', buffer: solidPng(120, 40, [25, 115, 255])}},
		}).then(r => expect(r.ok()).toBe(true))

		await page.goto('/projects/1/settings/forms')
		await page.getByRole('button', {name: /^Edit /}).click()
		await page.getByRole('tab', {name: 'Settings'}).click()
		const branding = page.locator('.fb__branding')
		await expect(branding.locator('.fb__logo-preview')).toBeVisible()

		await branding.getByRole('button', {name: 'Remove the logo'}).click()
		await expect(branding.locator('.fb__logo-preview')).toHaveCount(0)
		await expect(branding.getByRole('button', {name: 'Upload a logo'})).toBeVisible()
		expect((await api.get(`/projects/1/forms/${form.id}`)).has_brand_logo).toBe(false)

		const visitor = await browser.newContext({baseURL: BASE_URL})
		const publicPage = await visitor.newPage()
		await setupApiUrl(publicPage)
		await publicPage.goto(`/forms/${form.token}`)
		await expect(publicPage.locator('.fp__brand-name')).toHaveText('Acme Support')
		await expect(publicPage.locator('.fp__brand-logo')).toHaveCount(0)
		await expect(publicPage.locator('.fp__footer')).toContainText('Powered by')
		await shot(publicPage, 'forms-06-public-footer')
		await visitor.close()
		watcher.finish()
	})

	test('rejects a malformed accent colour', async ({authenticatedPage: page, apiContext, userToken}) => {
		const api = apiV2(apiContext, userToken)
		await createForm(api)
		await page.goto('/projects/1/settings/forms')
		await page.getByRole('button', {name: /^Edit /}).click()
		await page.getByRole('tab', {name: 'Settings'}).click()
		await page.locator('#fb-s-accent_color').fill('blue')
		await page.getByRole('button', {name: 'Save'}).click()
		await expect(page.locator('.fb__branding .help.is-danger')).toContainText('Enter a color like #1973ff.')
		await shot(page, 'forms-07-accent-invalid')
	})

	for (const [name, accent, colorScheme, width] of [
		['dark-red', '#b3261e', 'light', 1280],
		['pale-yellow', '#ffe066', 'light', 1280],
		['blue', '#1973ff', 'light', 1280],
		['green-dark-mode', '#0f9d58', 'dark', 1280],
		['mobile', '#b3261e', 'light', 390],
	] as const) {
		test(`recolours the public page with ${name} and keeps the button readable`, async ({apiContext, userToken, browser}) => {
			const api = apiV2(apiContext, userToken)
			const form = await createForm(api, {brand_name: 'Acme Support', accent_color: accent})

			const visitor = await browser.newContext({
				baseURL: BASE_URL,
				colorScheme,
				viewport: {width, height: width === 390 ? 844 : 900},
			})
			const page = await visitor.newPage()
			const watcher = watchBrowser(page)
			await setupApiUrl(page)
			await page.goto(`/forms/${form.token}`)
			await expect(page.getByRole('button', {name: 'Send'})).toBeVisible()

			const colors = await buttonColors(page)
			const rgb = [1, 3, 5].map(i => parseInt(accent.slice(i, i + 2), 16))
			// The accent goes through whole-number hue, saturation and lightness: a unit or two off is expected.
			const shown = colors.background.match(/\d+/g)!.map(Number)
			shown.forEach((channel, i) => expect(Math.abs(channel - rgb[i]!)).toBeLessThanOrEqual(3))
			expect(colors.ratio, `button text ${colors.text} on ${colors.background}`).toBeGreaterThanOrEqual(4.5)

			// Links take the accent too.
			const link = await linkContrast(page)
			console.log(`UICHECK [${name}] link ${link.color} on ${link.background}, ratio ${link.ratio.toFixed(2)}; button ${colors.background} / ${colors.text}, ratio ${colors.ratio.toFixed(2)}`)
			expect(link.ratio, `link ${link.color} on ${link.background}`).toBeGreaterThanOrEqual(3)

			await shot(page, `forms-08-public-${name}`)
			await visitor.close()
			watcher.finish()
		})
	}
})
