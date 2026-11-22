import type {ChartGroup} from '@/helpers/charts'

// The widget config is validated strictly by the server and has no room for display options, so they are a
// per-viewer preference kept in the browser.
export type WidgetDisplay = {
	orientation?: 'vertical' | 'horizontal'
	stacked?: boolean
	legend?: boolean
}

const PREFIX = 'dashboardWidget:'

function storage(): Storage | null {
	try {
		return window.localStorage
	} catch {
		return null
	}
}

export function loadDisplay(dashboardId: number, widgetId: number): WidgetDisplay {
	try {
		const raw = storage()?.getItem(`${PREFIX}${dashboardId}:${widgetId}`)
		const parsed = raw ? JSON.parse(raw) as WidgetDisplay : {}
		return {
			...(parsed.orientation === 'vertical' || parsed.orientation === 'horizontal' ? {orientation: parsed.orientation} : {}),
			...(typeof parsed.stacked === 'boolean' ? {stacked: parsed.stacked} : {}),
			...(typeof parsed.legend === 'boolean' ? {legend: parsed.legend} : {}),
		}
	} catch {
		return {}
	}
}

export function saveDisplay(dashboardId: number, widgetId: number, display: WidgetDisplay) {
	try {
		const store = storage()
		if (!store) {
			return
		}
		const key = `${PREFIX}${dashboardId}:${widgetId}`
		if (Object.keys(display).length === 0) {
			store.removeItem(key)
		} else {
			store.setItem(key, JSON.stringify(display))
		}
	} catch {
		// A full or blocked storage only loses a preference.
	}
}

// Many or long labels read better as horizontal bars.
export function defaultOrientation(groups: ChartGroup[]): 'vertical' | 'horizontal' {
	const longest = Math.max(0, ...groups.map(g => Array.from(g.label).length))
	return groups.length > 6 || longest > 14 ? 'horizontal' : 'vertical'
}
