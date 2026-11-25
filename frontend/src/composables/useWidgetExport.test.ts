import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {defineComponent, h, ref} from 'vue'
import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createI18n} from 'vue-i18n'

const sdk = vi.hoisted(() => ({reportsTasksExport: vi.fn(), reportsTasksExportRows: vi.fn(), reportsWorkloadExport: vi.fn()}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({error: vi.fn(), success: vi.fn()}))
vi.mock('@/message', () => message)
const download = vi.hoisted(() => ({downloadBlob: vi.fn()}))
vi.mock('@/helpers/downloadBlob', () => download)

import {useWidgetExport} from './useWidgetExport'
import en from '@/i18n/lang/en.json'

enableAutoUnmount(afterEach)

function setup(type: string, config: Record<string, unknown> = {}) {
	const typeRef = ref(type)
	const configRef = ref(config)
	let result!: ReturnType<typeof useWidgetExport>
	mount(defineComponent({setup() {
		result = useWidgetExport(typeRef, configRef)
		return () => h('div')
	}}), {global: {plugins: [createI18n({legacy: false, locale: 'en', messages: {en}})]}})
	return {export: result, typeRef}
}

describe('useWidgetExport', () => {
	beforeEach(() => {
		Object.values(sdk).forEach(mock => mock.mockReset())
		message.error.mockReset()
		download.downloadBlob.mockReset()
		URL.createObjectURL = vi.fn(() => 'blob:x')
	})

	it('offers nothing for a note and the tasks only for widgets that have a filter', () => {
		const note = setup('text').export
		expect([note.canExportData.value, note.canExportTasks.value]).toEqual([false, false])
		const list = setup('task_list').export
		expect([list.canExportData.value, list.canExportTasks.value]).toEqual([true, false])
		const bar = setup('bar', {request: {group_by: 'status'}}).export
		expect([bar.canExportData.value, bar.canExportTasks.value]).toEqual([true, true])
	})

	it('follows the widget when its type changes', async () => {
		const {export: ex, typeRef} = setup('bar')
		expect(ex.canExportData.value).toBe(true)
		typeRef.value = 'text'
		expect(ex.canExportData.value).toBe(false)
	})

	it('downloads the file the server wrote for a chart', async () => {
		const blob = new Blob(['a'])
		sdk.reportsTasksExport.mockResolvedValue({data: blob})
		const {export: ex} = setup('bar', {request: {project_id: 3, group_by: 'status'}})
		await ex.exportData()
		expect(sdk.reportsTasksExport.mock.calls[0]![0].body).toEqual({project_id: 3, group_by: 'status'})
		expect(download.downloadBlob).toHaveBeenCalledWith('blob:x', 'report.csv')
		expect(URL.createObjectURL).toHaveBeenCalledWith(blob)
		expect(ex.busy.value).toBe(false)
	})

	it('names a workload export after the widget', async () => {
		sdk.reportsWorkloadExport.mockResolvedValue({data: new Blob(['a'])})
		await setup('workload', {project_id: 3}).export.exportData()
		expect(download.downloadBlob).toHaveBeenCalledWith('blob:x', 'workload.csv')
	})

	it('exports the tasks behind a widget with its filter and project', async () => {
		sdk.reportsTasksExportRows.mockResolvedValue({data: new Blob(['a'])})
		await setup('bar', {request: {project_id: 3, filter: 'done = false', group_by: 'status'}}).export.exportTasks()
		expect(sdk.reportsTasksExportRows.mock.calls[0]![0].body).toEqual({project_id: 3, filter: 'done = false'})
		expect(download.downloadBlob).toHaveBeenCalledWith('blob:x', 'tasks.csv')
	})

	it('toasts a failure, downloads nothing and can be used again', async () => {
		sdk.reportsTasksExport.mockRejectedValueOnce(new Error('down'))
		const {export: ex} = setup('bar', {request: {group_by: 'status'}})
		await ex.exportData()
		expect(message.error).toHaveBeenCalledTimes(1)
		expect(download.downloadBlob).not.toHaveBeenCalled()
		expect(ex.busy.value).toBe(false)
		sdk.reportsTasksExport.mockResolvedValue({data: new Blob(['a'])})
		await ex.exportData()
		expect(download.downloadBlob).toHaveBeenCalledTimes(1)
	})

	it('ignores a second click while an export runs', async () => {
		let release!: (value: unknown) => void
		sdk.reportsTasksExport.mockReturnValue(new Promise(resolve => { release = resolve }))
		const {export: ex} = setup('bar', {request: {group_by: 'status'}})
		const first = ex.exportData()
		expect(ex.busy.value).toBe(true)
		await ex.exportData()
		expect(sdk.reportsTasksExport).toHaveBeenCalledTimes(1)
		release({data: new Blob(['a'])})
		await first
		await flushPromises()
		expect(download.downloadBlob).toHaveBeenCalledTimes(1)
	})
})
