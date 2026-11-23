import {computed, ref, type Ref} from 'vue'
import {useI18n} from 'vue-i18n'

import {downloadBlob} from '@/helpers/downloadBlob'
import {canExportData, canExportTasks, exportWidgetData, exportWidgetTasks, type WidgetExportFile} from '@/helpers/widgetExport'
import {error} from '@/message'

// The "Export CSV" entries of a widget menu: the numbers of the widget and the tasks behind it.
export function useWidgetExport(type: Ref<string>, config: Ref<Record<string, unknown>>) {
	const {t} = useI18n({useScope: 'global'})
	const busy = ref(false)

	async function run(job: () => Promise<WidgetExportFile>) {
		if (busy.value) {
			return
		}
		busy.value = true
		try {
			const file = await job()
			downloadBlob(URL.createObjectURL(file.blob), file.filename)
		} catch (e) {
			error(e)
		} finally {
			busy.value = false
		}
	}

	return {
		busy,
		canExportData: computed(() => canExportData(type.value)),
		canExportTasks: computed(() => canExportTasks(type.value, config.value)),
		exportData: () => run(() => exportWidgetData(type.value, config.value, {
			bucket: t('dashboards.export.bucket'),
			created: t('dashboards.createdSeries'),
			done: t('dashboards.doneSeries'),
		})),
		exportTasks: () => run(() => exportWidgetTasks(type.value, config.value)),
	}
}
