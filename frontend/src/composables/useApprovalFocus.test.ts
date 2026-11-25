import {beforeEach, describe, expect, it, vi} from 'vitest'
import {nextTick, watch} from 'vue'

import {focusApproval, focusedApprovalId} from './useApprovalFocus'

describe('useApprovalFocus', () => {
	beforeEach(() => {
		focusedApprovalId.value = null
	})

	it('points at the request after a microtask', async () => {
		focusApproval(11)
		expect(focusedApprovalId.value).toBeNull()
		await Promise.resolve()
		expect(focusedApprovalId.value).toBe(11)
	})

	it('wakes a watcher again when the same request is asked for twice', async () => {
		const seen = vi.fn()
		const stop = watch(focusedApprovalId, value => seen(value), {flush: 'sync'})
		focusApproval(11)
		await Promise.resolve()
		focusApproval(11)
		await Promise.resolve()
		await nextTick()
		expect(seen.mock.calls.map(c => c[0])).toEqual([11, null, 11])
		stop()
	})
})
