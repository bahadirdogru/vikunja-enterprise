// Kept apart from approvals.ts so the workflow queries can invalidate approvals without importing each other.
export const approvalKeys = {
	all: ['approvals'] as const,
	lists: ['approvals', 'list'] as const,
	list: (params: {status?: string, role?: string, projectId?: number, page?: number}) =>
		['approvals', 'list', params] as const,
	count: ['approvals', 'count'] as const,
	tasks: ['approvals', 'task'] as const,
	task: (taskId: number) => ['approvals', 'task', taskId] as const,
}
