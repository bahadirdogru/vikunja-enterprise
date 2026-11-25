import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

import {defaultOrientation, loadDisplay, saveDisplay} from './dashboardDisplay'

const group = (label: string) => ({key: label, label, color: '', value: 1})

describe('widget display preferences', () => {
	beforeEach(() => localStorage.clear())
	afterEach(() => vi.restoreAllMocks())

	it('is empty for a widget nobody configured', () => {
		expect(loadDisplay(1, 2)).toEqual({})
	})

	it('stores a preference per dashboard and widget', () => {
		saveDisplay(1, 2, {orientation: 'horizontal', stacked: false, legend: true})
		expect(loadDisplay(1, 2)).toEqual({orientation: 'horizontal', stacked: false, legend: true})
		expect(loadDisplay(1, 3)).toEqual({})
		expect(loadDisplay(2, 2)).toEqual({})
		expect(localStorage.getItem('dashboardWidget:1:2')).toBe('{"orientation":"horizontal","stacked":false,"legend":true}')
	})

	it('keeps false values: a deliberately hidden legend is not "unset"', () => {
		saveDisplay(1, 2, {legend: false, stacked: false})
		expect(loadDisplay(1, 2)).toEqual({legend: false, stacked: false})
	})

	it('removes the entry when the last preference is cleared', () => {
		saveDisplay(1, 2, {legend: true})
		saveDisplay(1, 2, {})
		expect(localStorage.getItem('dashboardWidget:1:2')).toBeNull()
	})

	it('drops what it does not recognise instead of passing it on to a chart', () => {
		localStorage.setItem('dashboardWidget:1:2', JSON.stringify({orientation: 'diagonal', stacked: 'yes', legend: 1, extra: true}))
		expect(loadDisplay(1, 2)).toEqual({})
		localStorage.setItem('dashboardWidget:1:2', JSON.stringify({orientation: 'vertical', stacked: 'yes'}))
		expect(loadDisplay(1, 2)).toEqual({orientation: 'vertical'})
	})

	it.each(['{not json', 'null', '[]', '"x"', '5'])('survives the stored text %s', raw => {
		localStorage.setItem('dashboardWidget:1:2', raw)
		expect(loadDisplay(1, 2)).toEqual({})
	})

	it('survives a storage that throws (private window, blocked site data)', () => {
		vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
			throw new Error('blocked')
		})
		vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
			throw new Error('full')
		})
		expect(loadDisplay(1, 2)).toEqual({})
		expect(() => saveDisplay(1, 2, {legend: true})).not.toThrow()
	})
})

describe('defaultOrientation', () => {
	it('uses vertical bars for a few short labels', () => {
		expect(defaultOrientation([])).toBe('vertical')
		expect(defaultOrientation(['To do', 'Done'].map(group))).toBe('vertical')
	})

	it('switches to horizontal bars above six groups, not at six', () => {
		expect(defaultOrientation(Array.from({length: 6}, (_, i) => group(`g${i}`)))).toBe('vertical')
		expect(defaultOrientation(Array.from({length: 7}, (_, i) => group(`g${i}`)))).toBe('horizontal')
	})

	it('switches to horizontal bars for a label longer than 14 characters, counted as characters', () => {
		expect(defaultOrientation([group('a'.repeat(14))])).toBe('vertical')
		expect(defaultOrientation([group('a'.repeat(15))])).toBe('horizontal')
		// 14 emoji are 28 UTF-16 units but 14 characters
		expect(defaultOrientation([group('😀'.repeat(14))])).toBe('vertical')
		expect(defaultOrientation([group('İ'.repeat(15))])).toBe('horizontal')
	})
})
