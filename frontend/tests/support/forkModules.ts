import type {APIRequestContext, Browser, Page} from '@playwright/test'
import {UserFactory} from '../factories/user'
import {UserProjectFactory} from '../factories/users_project'
import {login} from './authenticateUser'
import {apiRootUrl, apiV1Url} from './apiUrl'

/**
 * Helpers the e2e specs of the fork's modules share: seeding through the real API (so default views, buckets and
 * indexes exist like in production) and a second logged-in user in an own browser context.
 */

export interface ProjectViews {
	list: number
	gantt: number
	table: number
	kanban: number
}

function headers(token: string) {
	return {Authorization: `Bearer ${token}`}
}

async function check<T>(response: {ok(): boolean, status(): number, text(): Promise<string>, json(): Promise<T>}, what: string): Promise<T> {
	if (!response.ok()) {
		throw new Error(`${what} failed: ${response.status()} ${await response.text()}`)
	}
	return await response.json()
}

export async function createProject(request: APIRequestContext, token: string, title: string): Promise<{id: number, views: ProjectViews}> {
	const project = await check<{id: number}>(await request.put(`${apiV1Url()}/projects`, {headers: headers(token), data: {title}}), 'create project')
	const views = await check<{id: number, view_kind: string}[]>(await request.get(`${apiV1Url()}/projects/${project.id}/views`, {headers: headers(token)}), 'list views')
	const byKind = (kind: string) => views.find(v => v.view_kind === kind)!.id
	return {id: project.id, views: {list: byKind('list'), gantt: byKind('gantt'), table: byKind('table'), kanban: byKind('kanban')}}
}

export async function createTask(request: APIRequestContext, token: string, projectId: number, title: string, extra: Record<string, unknown> = {}): Promise<{id: number, title: string, [key: string]: unknown}> {
	return await check(await request.put(`${apiV1Url()}/projects/${projectId}/tasks`, {headers: headers(token), data: {title, ...extra}}), `create task ${title}`)
}

export async function getTask(request: APIRequestContext, token: string, taskId: number): Promise<Record<string, unknown>> {
	return await check(await request.get(`${apiV1Url()}/tasks/${taskId}`, {headers: headers(token)}), `get task ${taskId}`)
}

export async function listTasks(request: APIRequestContext, token: string, projectId: number, query = ''): Promise<{id: number, title: string, done: boolean, [key: string]: unknown}[]> {
	return await check(await request.get(`${apiV1Url()}/projects/${projectId}/tasks?per_page=100${query}`, {headers: headers(token)}), 'list tasks')
}

export interface TestUser {
	id: number
	username: string
	token: string
}

/** A second (or third) user, seeded next to the fixture's current user, and a token for it. */
export async function createUser(request: APIRequestContext, id: number, username: string, language = 'en'): Promise<TestUser> {
	const [user] = await UserFactory.create(1, {id, username, language}, false)
	const {token} = await login(null, request, user)
	return {id, username, token}
}

/** Gives a user access to a project. permission: 0 read, 1 write, 2 admin. */
export async function shareProject(request: APIRequestContext, token: string, projectId: number, username: string, permission: 0 | 1 | 2) {
	await check(await request.put(`${apiV1Url()}/projects/${projectId}/users`, {headers: headers(token), data: {username, permission}}), 'share project')
}

export async function addProjectUser(userId: number, projectId: number, permission: 0 | 1 | 2) {
	await UserProjectFactory.create(1, {project_id: projectId, user_id: userId, permission}, false)
}

/** A page of its own, logged in as the given user, for tests with two people in the same browser. */
export async function pageAs(browser: Browser, request: APIRequestContext, user: {username: string}): Promise<Page> {
	const context = await browser.newContext({
		baseURL: process.env.BASE_URL || 'http://127.0.0.1:4173',
		serviceWorkers: 'block',
		viewport: {width: 1280, height: 900},
	})
	const page = await context.newPage()
	await login(page, request, user)
	return page
}

export interface WorkflowStatus {
	id: number
	title: string
	category: string
	is_initial: boolean
}

/** Replaces the workflow of a project through /api/v2. Statuses may be referred to by a negative id in the transitions. */
export async function putWorkflow(request: APIRequestContext, token: string, projectId: number, document: Record<string, unknown>): Promise<{statuses: WorkflowStatus[], transitions: {id: number, from_status_id: number, to_status_id: number}[]}> {
	const base = `${apiRootUrl(process.env.API_URL || 'http://127.0.0.1:3456')}/api/v2`
	return await check(await request.put(`${base}/projects/${projectId}/workflow`, {headers: headers(token), data: document}), 'put workflow')
}

export const DEFAULT_STATUSES = [
	{id: -1, title: 'To do', category: 'open', is_initial: true},
	{id: -2, title: 'In progress', category: 'in_progress'},
	{id: -3, title: 'In review', category: 'in_progress'},
	{id: -4, title: 'Done', category: 'done'},
]
