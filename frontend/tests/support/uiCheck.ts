import {test, type APIRequestContext, type Locator, type Page} from '@playwright/test'
import {mkdirSync} from 'fs'
import {deflateSync} from 'zlib'
import {apiRootUrl} from './apiUrl'

/**
 * Saves a full-page screenshot (or, with `focus`, the viewport scrolled to that element) when E2E_SCREENSHOT_DIR is set; does nothing otherwise, so the committed specs never
 * write outside the repo.
 */
export async function shot(page: Page, name: string, focus?: Locator) {
	const dir = process.env.E2E_SCREENSHOT_DIR
	if (!dir) return
	// The app's loading splash fades out; a capture during the fade shows it over the page.
	await page.locator('.vikunja-loading').waitFor({state: 'detached', timeout: 5000}).catch(() => {})
	// Panels and menus slide and fade; capture them once they have settled.
	await page.waitForTimeout(500)
	mkdirSync(dir, {recursive: true})
	// Inside a scrolling modal a full-page capture shows only the top: bring what matters into view and capture that.
	if (focus) await focus.scrollIntoViewIfNeeded()
	await page.screenshot({path: `${dir}/${name}.png`, fullPage: !focus})
}

// Requests the flows below make on purpose or that the app makes on its own.
const IGNORED = [/\/user\/token\/refresh/]
// Playwright blocks service workers (playwright.config.ts), so the app's registration always complains.
const IGNORED_MESSAGES = [/Service Worker registration blocked/, /Error during service worker registration/]

/**
 * Collects uncaught errors, console errors and warnings, and 4xx/5xx responses of a page. Call `finish()` at the end of
 * the test: it prints what it saw (prefixed UICHECK) and fails on uncaught errors and unexpected 4xx/5xx responses.
 * `expected` lists URL patterns of 4xx responses the flow provokes on purpose.
 */
export function watchBrowser(page: Page, expected: RegExp[] = []) {
	const uncaught: string[] = []
	const consoleMessages: string[] = []
	const badResponses: string[] = []

	page.on('pageerror', error => uncaught.push(error.message))
	page.on('console', message => {
		if (message.type() === 'error' || message.type() === 'warning') {
			const text = message.text()
			// The browser repeats every failed request as a console error; the response list says it better.
			if (/Failed to load resource/.test(text) || IGNORED_MESSAGES.some(re => re.test(text))) return
			consoleMessages.push(`${message.type()}: ${text}`)
		}
	})
	page.on('response', async response => {
		const status = response.status()
		const url = response.url()
		if (status < 400 || IGNORED.some(re => re.test(url)) || expected.some(re => re.test(url))) return
		const body = await response.text().then(text => text.slice(0, 300)).catch(() => '')
		badResponses.push(`${status} ${response.request().method()} ${url} ${body}`)
	})

	return {
		uncaught,
		consoleMessages,
		badResponses,
		finish() {
			for (const line of [...uncaught.map(m => `uncaught: ${m}`), ...consoleMessages, ...badResponses.map(m => `http: ${m}`)]) {
				console.log(`UICHECK [${test.info().title}] ${line}`)
			}
			const fatal = [...uncaught, ...badResponses]
			if (fatal.length > 0) {
				throw new Error(`Unexpected browser errors:\n${fatal.join('\n')}`)
			}
		},
	}
}

/** A call against the /api/v2 API as a user: the e2e fixtures only know the v1 base. */
export function apiV2(request: APIRequestContext, token: string) {
	const base = `${apiRootUrl(process.env.API_URL || 'http://127.0.0.1:3456')}/api/v2`
	const headers = {Authorization: `Bearer ${token}`}
	const call = async (method: 'get' | 'post' | 'put' | 'delete', path: string, data?: unknown) => {
		const response = await request[method](`${base}${path}`, {headers, data})
		if (!response.ok()) {
			throw new Error(`${method.toUpperCase()} ${path} failed: ${response.status()} ${await response.text()}`)
		}
		const text = await response.text()
		return text ? JSON.parse(text) : null
	}
	return {
		base,
		get: (path: string) => call('get', path),
		post: (path: string, data?: unknown) => call('post', path, data),
		put: (path: string, data?: unknown) => call('put', path, data),
		delete: (path: string) => call('delete', path),
	}
}

function crc32(buffer: Buffer): number {
	let crc = 0xffffffff
	for (const byte of buffer) {
		crc ^= byte
		for (let i = 0; i < 8; i++) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1))
	}
	return (crc ^ 0xffffffff) >>> 0
}

/** A solid-colour PNG, for upload tests. */
export function solidPng(width: number, height: number, [r, g, b]: [number, number, number]): Buffer {
	const chunk = (type: string, data: Buffer) => {
		const body = Buffer.concat([Buffer.from(type, 'ascii'), data])
		const length = Buffer.alloc(4)
		length.writeUInt32BE(data.length)
		const crc = Buffer.alloc(4)
		crc.writeUInt32BE(crc32(body))
		return Buffer.concat([length, body, crc])
	}
	const header = Buffer.alloc(13)
	header.writeUInt32BE(width, 0)
	header.writeUInt32BE(height, 4)
	header[8] = 8
	header[9] = 2
	const row = Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({length: width}, () => [r, g, b]).flat())])
	const raw = Buffer.concat(Array.from({length: height}, () => row))
	return Buffer.concat([
		Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
		chunk('IHDR', header),
		chunk('IDAT', deflateSync(raw)),
		chunk('IEND', Buffer.alloc(0)),
	])
}
