const API_VERSION_SUFFIX = /\/api\/v[12]$/

// Stored without /api/vN ('' = same origin); values saved before that still carry one.
export function normalizeApiUrl(url: string): string {
	return url.replace(/\/+$/, '').replace(API_VERSION_SUFFIX, '')
}

export function getApiRootUrl(): string {
	if (typeof window.API_URL !== 'string') {
		throw new InvalidApiUrlProvidedError()
	}

	return normalizeApiUrl(window.API_URL)
}

export function getApiBaseUrl(): string {
	return `${getApiRootUrl()}/api/v2`
}

// The server builds the address of an incoming webhook from service.publicurl, which may be empty: then it is a path
// ("api/v2/automation/hooks/...") that is only meaningful against the API the client talks to.
export function resolveAgainstApiRoot(url: string): string {
	if (/^https?:\/\//i.test(url)) {
		return url
	}
	const root = getApiRootUrl()
	const origin = /^https?:\/\//i.test(root) ? root : `${window.location.origin}${root}`
	return `${origin}/${url.replace(/^\/+/, '')}`
}

export class NoApiUrlProvidedError extends Error {
	constructor() {
		super()
		this.message = 'No API URL provided'
		this.name = 'NoApiUrlProvidedError'
	}
}

export class InvalidApiUrlProvidedError extends Error {
	constructor() {
		super()
		this.message = 'The provided API URL is invalid.'
		this.name = 'InvalidApiUrlProvidedError'
	}
}
