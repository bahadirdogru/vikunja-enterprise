import {describe, it, expect, vi} from 'vitest'
import {notify} from '@kyvg/vue3-notification'

import {ApiError} from '@/client/problem'
import {error, getErrorText, success} from './index'

vi.mock('@kyvg/vue3-notification', () => ({notify: vi.fn()}))

describe('success', () => {
	it('merges duplicate toasts without actions', () => {
		success({message: 'Saved'})
		expect(notify).toHaveBeenLastCalledWith(expect.objectContaining({ignoreDuplicates: true}))
	})

	it('never merges toasts with actions, so each keeps its own undo', () => {
		success({message: 'Deleted'}, [{
			title: 'Undo',
			callback: () => {},
		}])
		expect(notify).toHaveBeenLastCalledWith(expect.objectContaining({ignoreDuplicates: false}))
	})

	it('leaves closing a toast with actions to its pausable timer', () => {
		success({message: 'Deleted'}, [{
			title: 'Undo',
			callback: () => {},
		}])
		expect(notify).toHaveBeenLastCalledWith(expect.objectContaining({duration: -1}))
	})

	it('keeps the default duration for toasts without actions', () => {
		success({message: 'Saved'})
		expect(notify).toHaveBeenLastCalledWith(expect.objectContaining({duration: undefined}))
	})
})

describe('error', () => {
	it('merges duplicate toasts without actions', () => {
		error({message: 'Failed'})
		expect(notify).toHaveBeenLastCalledWith(expect.objectContaining({ignoreDuplicates: true}))
	})

	it('never merges toasts with actions, so each keeps its own action', () => {
		error({message: 'Failed'}, [{
			title: 'Retry',
			callback: () => {},
		}])
		expect(notify).toHaveBeenLastCalledWith(expect.objectContaining({ignoreDuplicates: false}))
	})
})

describe('getErrorText', () => {
	it.each([null, undefined])('handles an empty rejection reason (%s)', reason => {
		expect(getErrorText(reason)).toBe('')
	})

	it('handles a string rejection reason', () => {
		expect(getErrorText('Request failed')).toBe('Request failed')
	})

	it('interpolates i18n_params into the translated error message', () => {
		const text = getErrorText({
			code: 14002,
			message: 'The permission frobnicate of group tasks is invalid.',
			i18n_params: {permission: 'frobnicate', group: 'tasks'},
		})

		expect(text).toContain('frobnicate')
		expect(text).toContain('tasks')
	})

	it('falls back to empty placeholders when i18n_params is missing, without crashing', () => {
		const text = getErrorText({
			code: 14002,
			message: 'The permission frobnicate of group tasks is invalid.',
		})

		expect(text).not.toContain('{permission}')
		expect(text).not.toContain('{group}')
		expect(text).not.toContain('undefined')
	})

	it('falls back to the message when there is no error code', () => {
		const text = getErrorText({
			message: 'Something went wrong',
		})

		expect(text).toBe('Something went wrong')
	})

	it('falls back to the message for an unknown error code', () => {
		const text = getErrorText({
			code: 99999,
			message: 'some backend message',
		})

		expect(text).toBe('some backend message')
	})

	it('appends the server message of a request error given as cause', () => {
		const text = getErrorText(Object.assign(new Error('Error while refreshing user info:'), {
			cause: {
				status: 500,
				message: 'Internal server error',
				detail: 'Internal server error',
			},
		}))

		expect(text).toBe('Error while refreshing user info: Internal server error')
	})

	it('reads a direct problem response', () => {
		const text = getErrorText({
			code: 99999,
			detail: 'direct problem detail',
		})

		expect(text).toBe('direct problem detail')
	})

	it('translates the code of an ApiError', () => {
		const text = getErrorText(new ApiError({status: 404, code: 1001, detail: 'The user does not exist.'}))

		expect(text).not.toBe('The user does not exist.')
		expect(text).not.toBe('error.1001')
	})

	it('reads the detail of an ApiError without a code', () => {
		expect(getErrorText(new ApiError({status: 409, detail: 'Conflict'}))).toBe('Conflict')
	})
})

describe('getErrorText for the automation rule limit', () => {
	it('shows the translated message of code 22014 instead of the English detail', () => {
		const text = getErrorText({status: 409, code: 22014, detail: 'This project has reached the limit of 100 automation rules.'})
		expect(text).toContain('maximum number of automation rules')
		expect(text).not.toBe('error.22014')
	})
})

describe('getErrorText without a code', () => {
	it('says a failed validation of the API in the interface language, with the fields', () => {
		const text = getErrorText(new ApiError({
			status: 422, detail: 'validation failed',
			errors: [{location: 'body.title', message: 'expected length >= 1'}, {location: 'body.due_date', message: 'bad'}],
		}))
		expect(text).toContain('\ntitle, due_date')
		expect(text).not.toContain('validation failed')
		expect(text).not.toContain('expected length')
	})

	it('says a validation failure without fields', () => {
		expect(getErrorText(new ApiError({status: 422, detail: 'validation failed'}))).toBe('Some of the values you entered are not valid.')
	})

	it.each([401, 403, 404, 429, 500, 502, 503, 504])('says the status %i in the interface language, not the English of the web server', status => {
		const text = getErrorText(new ApiError({status, detail: 'Bad Gateway'}))
		expect(text).not.toContain('Bad Gateway')
		expect(text).not.toBe('')
	})

	it('keeps the detail of a status that has no generic text', () => {
		expect(getErrorText(new ApiError({status: 409, detail: 'The title is taken.'}))).toBe('The title is taken.')
	})

	it('says a request that never reached the server', () => {
		expect(getErrorText(new Error('Network Error'))).toBe('The server could not be reached. Check your connection and try again.')
		expect(getErrorText(new TypeError('Failed to fetch'))).toBe('The server could not be reached. Check your connection and try again.')
	})

	it('prefers the text of a known code over the status', () => {
		expect(getErrorText(new ApiError({status: 404, code: 4002, detail: 'x'}))).toBe('The task does not exist.')
	})
})

describe('getErrorText keeps what a specific server detail says', () => {
	it('puts a specific detail after the generic sentence', () => {
		const text = getErrorText(new ApiError({status: 403, detail: 'API tokens cannot access MCP connection settings'}))
		expect(text).toBe('You are not allowed to do this.\nAPI tokens cannot access MCP connection settings')
	})

	it('puts the detail of a 422 without fields after the validation sentence', () => {
		expect(getErrorText(new ApiError({status: 422, detail: 'something specific'}))).toBe('Some of the values you entered are not valid.\nsomething specific')
	})

	it('does not repeat what the framework says by itself', () => {
		expect(getErrorText(new ApiError({status: 404, detail: 'Not Found'}))).toBe('The page or item was not found.')
		expect(getErrorText(new ApiError({status: 502, detail: 'Bad Gateway'}))).toBe('The server cannot be reached at the moment. Try again later.')
		expect(getErrorText(new ApiError({status: 401, detail: 'missing or malformed jwt'}))).toBe('You are not signed in or your session has expired. Sign in again.')
	})
})
