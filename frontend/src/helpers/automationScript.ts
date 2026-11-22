// Pure helpers of the script editing UI: the reference panel, the example snippets and the places a
// {{secrets.NAME}} placeholder is accepted.
import type {ScriptApiEntry, ScriptDoc} from '@/helpers/automation'

export type ScriptMode = 'action' | 'condition'

// A condition script has a read-only API: what changes something or reads a secret is not offered there.
export function apiFor(doc: ScriptDoc | null, mode: ScriptMode): ScriptApiEntry[] {
	if (!doc) return []
	return mode === 'condition' ? doc.api.filter(entry => entry.inCondition) : doc.api
}

export function filterApi(entries: ScriptApiEntry[], query: string): ScriptApiEntry[] {
	const needle = query.trim().toLowerCase()
	if (needle === '') return entries
	return entries.filter(entry => entry.name.toLowerCase().includes(needle)
		|| entry.signature.toLowerCase().includes(needle)
		|| entry.description.toLowerCase().includes(needle))
}

// The convention text of the catalog for the kind of script.
export function conventionFor(doc: ScriptDoc | null, mode: ScriptMode): string {
	if (!doc) return ''
	return mode === 'condition' ? doc.conditionConvention : doc.actionConvention
}

// Inserts text into a source at the selection; returns the new source and the caret position.
export function insertAt(source: string, start: number, end: number, text: string): {value: string, caret: number} {
	return {value: source.slice(0, start) + text + source.slice(end), caret: start + text.length}
}

export type ScriptSnippet = {id: string, titleKey: string, mode: ScriptMode, source: string}

// The examples of docs/enterprise/automation.md (Scripting -> Examples), plus a small condition. Titles are i18n keys
// under automation.code.snippet.*. The ids of custom fields and users are examples: the person adapts them.
export const SCRIPT_SNIPPETS: ScriptSnippet[] = [
	{
		id: 'sum_subtasks',
		titleKey: 'automation.code.snippet.sumSubtasks',
		mode: 'action',
		source: `// Trigger: a status changed to a done status. Field 12 is a number field, adjust the id.
const FIELD = 12;
const parentId = (task.relations.parenttask || [])[0];
if (!parentId) return "no parent";
let sum = 0;
for (const id of vikunja.tasks.get(parentId).relations.subtask || []) {
  const value = vikunja.fields.get(id, FIELD);
  if (typeof value === "number") sum += value;
}
vikunja.fields.set(parentId, FIELD, sum);
return {parent: parentId, sum: sum};
`,
	},
	{
		id: 'assign_from_secret',
		titleKey: 'automation.code.snippet.assignFromSecret',
		mode: 'action',
		source: `// The secret ASSIGNEES holds a JSON list of user ids, for example [4, 7, 9].
const team = JSON.parse(secrets.get("ASSIGNEES"));
const pick = team[task.id % team.length];
vikunja.assignees.add(task.id, pick);
log("assigned", pick);
return pick;
`,
	},
	{
		id: 'comment',
		titleKey: 'automation.code.snippet.comment',
		mode: 'action',
		source: `log("task", task.id, task.title);
vikunja.tasks.comment(task.id, "Created by " + event.doer.username + ": " + task.title);
return task.id;
`,
	},
	{
		id: 'condition_priority',
		titleKey: 'automation.code.snippet.conditionPriority',
		mode: 'condition',
		source: `// Must end with return true or return false.
return task.priority >= 3 && !task.done;
`,
	},
]

export function snippetsFor(mode: ScriptMode): ScriptSnippet[] {
	return SCRIPT_SNIPPETS.filter(snippet => snippet.mode === mode)
}

// The only parameters that accept {{secrets.NAME}}: http.request header values and body, and the `secret` parameter
// itself (which names a secret). The catalog does not mark these (it only says so in the parameter descriptions), so
// they are listed here; the server rejects the placeholder everywhere else when the rule is saved.
const SECRET_PLACEHOLDER_PARAMS: Record<string, string[]> = {
	'http.request': ['headers', 'body'],
}

export function allowsSecretPlaceholder(actionType: string | undefined, paramName: string): boolean {
	return !!actionType && (SECRET_PLACEHOLDER_PARAMS[actionType] ?? []).includes(paramName)
}

export function secretPlaceholder(name: string): string {
	return `secrets.${name}`
}
