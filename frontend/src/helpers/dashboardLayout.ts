// Pure grid layout for dashboards: 12 columns, unbounded rows, vertical compaction.

export const GRID_COLUMNS = 12
// Pixels; the CSS grid uses the same two numbers, and drag maps pointer distance to cells with them.
export const ROW_HEIGHT = 72
export const GRID_GAP = 12

export type LayoutItem = {
	id: number
	x: number
	y: number
	w: number
	h: number
}

export type LayoutLimits = {
	minW: number
	minH: number
}

const DEFAULT_LIMITS: LayoutLimits = {minW: 2, minH: 2}

export function collides(a: LayoutItem, b: LayoutItem): boolean {
	return a.id !== b.id
		&& a.x < b.x + b.w
		&& a.x + a.w > b.x
		&& a.y < b.y + b.h
		&& a.y + a.h > b.y
}

function byPosition(a: LayoutItem, b: LayoutItem): number {
	return a.y - b.y || a.x - b.x || a.id - b.id
}

export function clampItem(item: LayoutItem, limits: LayoutLimits = DEFAULT_LIMITS): LayoutItem {
	const w = Math.min(GRID_COLUMNS, Math.max(limits.minW, Math.round(item.w)))
	const h = Math.max(limits.minH, Math.round(item.h))
	const x = Math.min(GRID_COLUMNS - w, Math.max(0, Math.round(item.x)))
	const y = Math.max(0, Math.round(item.y))
	return {id: item.id, x, y, w, h}
}

// Moves every item up as far as it goes. Order of the input is kept.
export function compact(items: LayoutItem[]): LayoutItem[] {
	const placed: LayoutItem[] = []
	const result = new Map<number, LayoutItem>()
	for (const item of [...items].sort(byPosition)) {
		const next = {...item, y: 0}
		for (;;) {
			const hit = placed.find(other => collides(next, other))
			if (!hit) {
				break
			}
			next.y = hit.y + hit.h
		}
		placed.push(next)
		result.set(item.id, next)
	}
	return items.map(item => result.get(item.id)!)
}

// Everything that collides with an anchor moves below it, cascading down; the anchor stays.
function pushDown(items: LayoutItem[], anchor: LayoutItem): LayoutItem[] {
	const fixed: LayoutItem[] = [anchor]
	const moved = new Map<number, LayoutItem>([[anchor.id, anchor]])
	for (const item of items.filter(i => i.id !== anchor.id).sort(byPosition)) {
		const next = {...item}
		for (;;) {
			const hit = fixed.find(other => collides(next, other))
			if (!hit) {
				break
			}
			next.y = hit.y + hit.h
		}
		fixed.push(next)
		moved.set(item.id, next)
	}
	return items.map(item => moved.get(item.id)!)
}

function place(items: LayoutItem[], target: LayoutItem): LayoutItem[] {
	return compact(pushDown(items, target))
}

export function layoutMove(items: LayoutItem[], id: number, x: number, y: number, limits?: LayoutLimits): LayoutItem[] {
	const current = items.find(item => item.id === id)
	if (!current) {
		return items
	}
	return place(items, clampItem({...current, x, y}, limits))
}

export function layoutResize(items: LayoutItem[], id: number, w: number, h: number, limits?: LayoutLimits): LayoutItem[] {
	const current = items.find(item => item.id === id)
	if (!current) {
		return items
	}
	return place(items, clampItem({...current, w, h}, limits))
}

export function layoutMoveBy(items: LayoutItem[], id: number, dx: number, dy: number, limits?: LayoutLimits): LayoutItem[] {
	const current = items.find(item => item.id === id)
	return current ? layoutMove(items, id, current.x + dx, current.y + dy, limits) : items
}

export function layoutResizeBy(items: LayoutItem[], id: number, dw: number, dh: number, limits?: LayoutLimits): LayoutItem[] {
	const current = items.find(item => item.id === id)
	return current ? layoutResize(items, id, current.w + dw, current.h + dh, limits) : items
}

export function bottomOf(items: LayoutItem[]): number {
	return items.reduce((max, item) => Math.max(max, item.y + item.h), 0)
}

// A new widget goes below everything else, full width left aligned.
export function placeNew(items: LayoutItem[], w: number, h: number): {x: number, y: number, w: number, h: number} {
	const width = Math.min(GRID_COLUMNS, w)
	return {x: 0, y: bottomOf(items), w: width, h}
}

// Single column order used under the mobile breakpoint.
export function readingOrder<T extends LayoutItem>(items: T[]): T[] {
	return [...items].sort(byPosition)
}

export function sameLayout(a: LayoutItem[], b: LayoutItem[]): boolean {
	if (a.length !== b.length) {
		return false
	}
	const other = new Map(b.map(item => [item.id, item]))
	return a.every(item => {
		const o = other.get(item.id)
		return !!o && o.x === item.x && o.y === item.y && o.w === item.w && o.h === item.h
	})
}

export function changedItems(before: LayoutItem[], after: LayoutItem[]): LayoutItem[] {
	const old = new Map(before.map(item => [item.id, item]))
	return after.filter(item => {
		const o = old.get(item.id)
		return !o || o.x !== item.x || o.y !== item.y || o.w !== item.w || o.h !== item.h
	})
}

// Pointer position inside the grid -> cell. cellWidth includes the gap.
export function pointToCell(px: number, py: number, cellWidth: number, rowHeight: number): {col: number, row: number} {
	return {
		col: Math.max(0, Math.round(px / Math.max(1, cellWidth))),
		row: Math.max(0, Math.round(py / Math.max(1, rowHeight))),
	}
}
