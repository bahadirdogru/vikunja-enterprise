import {ref, watch, type Ref} from 'vue'
import {useI18n, type Composer} from 'vue-i18n'

import {formatNumber, formatSeconds} from '@/helpers/charts'

export type ChartSize = {width: number, height: number}

// viewBox = measured pixels, so text keeps its size and only the plot area scales.
export function useChartSize(el: Ref<HTMLElement | null | undefined>, fallback: ChartSize = {width: 480, height: 260}) {
	const size = ref<ChartSize>({...fallback})

	function measure() {
		const rect = el.value?.getBoundingClientRect()
		if (rect && rect.width > 0 && rect.height > 0) {
			size.value = {width: Math.round(rect.width), height: Math.round(rect.height)}
		}
	}

	// The plot only exists once the data has loaded, so the observer follows the element, not the mount.
	watch(el, (element, _previous, onCleanup) => {
		if (!element) {
			return
		}
		measure()
		if (typeof ResizeObserver === 'undefined') {
			return
		}
		const observer = new ResizeObserver(measure)
		observer.observe(element)
		onCleanup(() => observer.disconnect())
	}, {flush: 'post', immediate: true})

	return size
}

type ChartText = {
	t: Composer['t']
	locale: Composer['locale']
	valueText: (value: number, unit?: string) => string
	ariaLabel: (label: string, value: number, unit?: string) => string
}

export function useChartText(): ChartText {
	const {t, locale} = useI18n({useScope: 'global'})

	function valueText(value: number, unit?: string): string {
		if (unit === 'seconds') {
			return formatSeconds(value, locale.value)
		}
		const formatted = formatNumber(value, locale.value)
		return unit === 'tasks' ? t('charts.tasks', {count: formatted}, {plural: Math.round(value)}) : formatted
	}

	function ariaLabel(label: string, value: number, unit?: string): string {
		return `${label}: ${valueText(value, unit)}`
	}

	return {t, locale, valueText, ariaLabel}
}

export type TooltipState = {
	x: number
	y: number
	title: string
	rows: {label: string, value: string, color?: string}[]
	note?: string
}
