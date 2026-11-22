import {ref} from 'vue'

// The status history points at the approval that caused a change; the approvals section of the task opens it.
export const focusedApprovalId = ref<number | null>(null)

export function focusApproval(id: number) {
	// Setting the same id again must still trigger the watcher.
	focusedApprovalId.value = null
	queueMicrotask(() => {
		focusedApprovalId.value = id
	})
}
