import {expect, type Locator, type Page} from '@playwright/test'

// What an untranslated string looks like on screen: an i18n key such as "customFields.form.title".
export const RAW_KEY = /^[a-z]+(\.[a-zA-Z_]+){2,}$/

// Common words of an English interface. A Turkish screen with Turkish test data has none of them as a whole word.
// Words that are also Turkish, product names and identifiers (API, URL, ID, Vikunja, Kanban, Gantt, CalDAV, webhook,
// token) are not listed. Keep the list to words that no Turkish text of the application uses.
export const ENGLISH_UI_WORDS = [
	'save', 'cancel', 'delete', 'create', 'loading', 'settings', 'search', 'overview', 'upcoming', 'projects', 'project', 'tasks', 'task',
	'title', 'description', 'priority', 'assignees', 'assignee', 'labels', 'comments', 'comment', 'attachments', 'attachment', 'filters',
	'password', 'username', 'login', 'logout', 'register', 'welcome', 'archive', 'archived', 'share', 'shared', 'teams', 'members',
	'notifications', 'reminders', 'reminder', 'created', 'updated', 'upload', 'download', 'close', 'confirm', 'previous', 'next', 'show',
	'hide', 'remove', 'edit', 'done', 'name', 'today', 'tomorrow', 'yesterday', 'something', 'wrong', 'please', 'error', 'failed',
	'success', 'successfully', 'invalid', 'required', 'optional', 'select', 'choose', 'click', 'enable', 'disable', 'enabled', 'disabled',
	'overdue', 'repeat', 'color', 'colour', 'background', 'language', 'timezone', 'migrate', 'help', 'about',
	'forgot', 'remember', 'account', 'profile', 'your', 'the', 'and', 'with', 'from', 'this', 'that', 'are', 'for',
]

const ENGLISH_WORD = new RegExp(`(?<![\\p{L}\\p{N}_.-])(${ENGLISH_UI_WORDS.join('|')})(?![\\p{L}\\p{N}_-])`, 'iu')

/** Every text a person can see or hear on the page, one entry per text node, plus the texts of labels, placeholders, titles and alternative texts. */
export async function visibleTexts(scope: Page | Locator): Promise<string[]> {
	// A Locator has .locator() too: only a page has .goto().
	const root = 'goto' in scope ? (scope as Page).locator('body') : scope
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
			// Code, key caps and the menu of template variables are identifiers, not words of the interface.
			if (text && node.parentElement && !['SCRIPT', 'STYLE', 'NOSCRIPT'].includes(node.parentElement.tagName) && !node.parentElement.closest('code, kbd, pre, .param-input__placeholders') && visible(node.parentElement)) seen.push(text)
		}
		for (const el of element.querySelectorAll('[aria-label], [placeholder], [title], img[alt]')) {
			if (!visible(el)) continue
			for (const attribute of ['aria-label', 'placeholder', 'title', 'alt']) {
				const value = el.getAttribute(attribute)
				if (value) seen.push(value.trim())
			}
		}
		return seen
	})
}

// Texts that look English and are not the interface's: the screen-reader description inside the logo (an upstream
// asset), and the names of the events of a webhook (identifiers like task.created, which are the API's).
export const KNOWN_NOT_INTERFACE = [/A Llama one the left/, /^(project|projects|task|tasks)(\.[a-z_]+)*$/]

/** The texts of the screen that contain a common English word, and the raw keys. `allowed` are texts that are data or names. */
export async function leaksOf(scope: Page | Locator, allowed: RegExp[] = KNOWN_NOT_INTERFACE): Promise<string[]> {
	const texts = await visibleTexts(scope)
	return texts.filter(text => (RAW_KEY.test(text) || ENGLISH_WORD.test(text)) && !allowed.some(re => re.test(text)))
}

/** Fails when the screen shows a raw key or a common English word. */
export async function expectNoEnglish(scope: Page | Locator, where: string, allowed: RegExp[] = KNOWN_NOT_INTERFACE) {
	expect(await leaksOf(scope, allowed), `English or raw keys on ${where}`).toEqual([])
}
