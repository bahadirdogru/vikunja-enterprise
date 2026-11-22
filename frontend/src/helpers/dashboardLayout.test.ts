import {describe, expect, it} from 'vitest'

import {
	changedItems,
	clampItem,
	collides,
	compact,
	layoutMove,
	layoutMoveBy,
	layoutResize,
	layoutResizeBy,
	placeNew,
	readingOrder,
	sameLayout,
	type LayoutItem,
} from './dashboardLayout'

const item = (id: number, x: number, y: number, w: number, h: number): LayoutItem => ({id, x, y, w, h})
const byId = (items: LayoutItem[], id: number) => items.find(i => i.id === id)!

describe('collides', () => {
	it('detects overlap but not touching edges or itself', () => {
		expect(collides(item(1, 0, 0, 4, 2), item(2, 3, 1, 4, 2))).toBe(true)
		expect(collides(item(1, 0, 0, 4, 2), item(2, 4, 0, 4, 2))).toBe(false)
		expect(collides(item(1, 0, 0, 4, 2), item(2, 0, 2, 4, 2))).toBe(false)
		expect(collides(item(1, 0, 0, 4, 2), item(1, 0, 0, 4, 2))).toBe(false)
	})
})

describe('clampItem', () => {
	it('keeps the item inside the 12 columns and above its minimum size', () => {
		expect(clampItem(item(1, 10, -3, 6, 1), {minW: 2, minH: 2})).toEqual(item(1, 6, 0, 6, 2))
		expect(clampItem(item(1, 0, 0, 20, 3))).toEqual(item(1, 0, 0, 12, 3))
	})
})

describe('compact', () => {
	it('moves everything up into free space', () => {
		const result = compact([item(1, 0, 4, 6, 2), item(2, 6, 7, 6, 2), item(3, 0, 9, 6, 2)])
		expect(result).toEqual([item(1, 0, 0, 6, 2), item(2, 6, 0, 6, 2), item(3, 0, 2, 6, 2)])
	})

	it('keeps the input order and does not mutate', () => {
		const input = [item(2, 0, 5, 6, 2), item(1, 0, 0, 6, 2)]
		const result = compact(input)
		expect(result.map(i => i.id)).toEqual([2, 1])
		expect(input[0]!.y).toBe(5)
		expect(byId(result, 2).y).toBe(2)
	})
})

describe('layoutMove', () => {
	const base = [item(1, 0, 0, 6, 2), item(2, 6, 0, 6, 2), item(3, 0, 2, 12, 2)]

	it('pushes colliding widgets down and cascades', () => {
		const result = layoutMove(base, 1, 6, 0)
		expect(byId(result, 1)).toMatchObject({x: 6, y: 0})
		expect(byId(result, 2).y).toBeGreaterThanOrEqual(2)
		result.forEach(a => result.forEach(b => expect(collides(a, b)).toBe(false)))
	})

	it('clamps to the grid', () => {
		const result = layoutMove(base, 2, 20, -4)
		expect(byId(result, 2).x).toBe(6)
		expect(byId(result, 2).y).toBe(0)
	})

	it('compacts the gap it leaves', () => {
		const result = layoutMove([item(1, 0, 0, 12, 2), item(2, 0, 2, 12, 2), item(3, 0, 4, 12, 2)], 1, 0, 4)
		expect(result.map(i => [i.id, i.y])).toEqual([[1, 2], [2, 0], [3, 4]])
	})

	it('ignores an unknown id', () => {
		expect(layoutMove(base, 99, 0, 0)).toBe(base)
	})

	it('moves by a delta (keyboard)', () => {
		const result = layoutMoveBy([item(1, 0, 0, 4, 2), item(2, 4, 0, 4, 2)], 1, 1, 0)
		expect(byId(result, 1).x).toBe(1)
		expect(byId(result, 2).y).toBe(2)
	})
})

describe('layoutResize', () => {
	it('respects the minimum and pushes widgets below it', () => {
		const base = [item(1, 0, 0, 6, 2), item(2, 0, 2, 6, 2)]
		const taller = layoutResize(base, 1, 6, 4)
		expect(byId(taller, 1).h).toBe(4)
		expect(byId(taller, 2).y).toBe(4)
		const tiny = layoutResize(base, 1, 0, 0, {minW: 3, minH: 2})
		expect(byId(tiny, 1)).toMatchObject({w: 3, h: 2})
	})

	it('does not grow past the right edge', () => {
		const result = layoutResize([item(1, 8, 0, 2, 2)], 1, 9, 2)
		expect(byId(result, 1)).toMatchObject({x: 3, w: 9})
	})

	it('resizes by a delta (keyboard)', () => {
		expect(byId(layoutResizeBy([item(1, 0, 0, 4, 2)], 1, 2, 1), 1)).toMatchObject({w: 6, h: 3})
	})
})

describe('helpers', () => {
	it('places a new widget below everything', () => {
		expect(placeNew([item(1, 0, 0, 6, 3), item(2, 6, 1, 6, 4)], 4, 3)).toEqual({x: 0, y: 5, w: 4, h: 3})
		expect(placeNew([], 20, 3)).toEqual({x: 0, y: 0, w: 12, h: 3})
	})

	it('orders for the single column by y then x', () => {
		const order = readingOrder([item(3, 6, 2, 3, 2), item(1, 6, 0, 3, 2), item(2, 0, 0, 3, 2)])
		expect(order.map(i => i.id)).toEqual([2, 1, 3])
	})

	it('compares layouts and lists what changed', () => {
		const a = [item(1, 0, 0, 4, 2), item(2, 4, 0, 4, 2)]
		const b = [item(2, 4, 0, 4, 2), item(1, 0, 0, 4, 2)]
		expect(sameLayout(a, b)).toBe(true)
		const c = [item(1, 0, 0, 4, 2), item(2, 4, 1, 4, 2)]
		expect(sameLayout(a, c)).toBe(false)
		expect(changedItems(a, c)).toEqual([item(2, 4, 1, 4, 2)])
	})
})
