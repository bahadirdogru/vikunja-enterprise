import {Factory} from '../support/factory'

// Local "YYYY-MM-DD HH:MM:SS", the format the other fixtures use.
function sqlDateTime(d: Date): string {
	const pad = (n: number) => String(n).padStart(2, '0')
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// One status change of a task, at a moment of the test's choosing: the reports on the time in a status read these rows.
export class TaskStatusHistoryFactory extends Factory {
	static table = 'task_status_history'

	static factory() {
		return {
			id: '{increment}',
			task_id: 0,
			from_status_id: null,
			to_status_id: 0,
			from_title: '',
			to_title: '',
			doer_id: 1,
			comment: '',
			created: sqlDateTime(new Date()),
		}
	}
}
