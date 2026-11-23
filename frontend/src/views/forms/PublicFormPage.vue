<script setup lang="ts">
// The page a form link opens. It is rendered without the app shell and works logged out: the only requests are the
// definition and the submission.
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {useRouter} from 'vue-router'
import {useI18n} from 'vue-i18n'
import {useTitle} from '@vueuse/core'
import {useQuery, useQueryClient} from '@tanstack/vue-query'

import FormRenderer from '@/components/forms/FormRenderer.vue'
import Logo from '@/components/home/Logo.vue'
import XButton from '@/components/input/Button.vue'
import {formKeys, publicFormQuery, submitPublicForm, type PublicSubmitResult} from '@/client/queries/forms'
import {
	classifyPublicProblem,
	redirectTarget,
	serverErrorsFrom,
	type AnswerErrors,
	type PublicProblem,
	type SubmissionBody,
} from '@/helpers/formsPublic'
import {FORM_ERROR} from '@/helpers/forms'
import {saveLastVisited} from '@/helpers/saveLastVisited'
import {sanitizeProjectDescription} from '@/helpers/sanitizeProjectDescription'

const props = defineProps<{
	token: string
}>()

const {t} = useI18n({useScope: 'global'})
const router = useRouter()
const queryClient = useQueryClient()

const query = useQuery(computed(() => publicFormQuery(props.token)))
const loaded = computed(() => query.data.value ?? null)
// A signed-in submitter is identified by the account, so the page does not ask who they are.
const definition = computed(() => {
	const found = loaded.value
	if (!found) return null
	return found.authed
		? {...found.definition, submitter: {collect_name: false, name_required: false, collect_email: false, email_required: false}}
		: found.definition
})

// A problem that replaces the whole page (as opposed to one that only interrupts a submission).
const pageProblem = ref<PublicProblem | null>(null)
const loadProblem = computed<PublicProblem | null>(() => query.error.value ? classifyPublicProblem(query.error.value) : null)
const problem = computed(() => pageProblem.value ?? loadProblem.value)

const submitting = ref(false)
const serverErrors = ref<AnswerErrors | null>(null)
const submitProblem = ref<PublicProblem | null>(null)
const result = ref<PublicSubmitResult | null>(null)
const renderer = ref<InstanceType<typeof FormRenderer> | null>(null)
const resultBox = ref<HTMLElement | null>(null)

useTitle(computed(() => {
	if (definition.value) return definition.value.title
	if (problem.value?.kind === 'unavailable' && problem.value.title) return problem.value.title
	return t('forms.public.pageTitle')
}))

let robots: HTMLMetaElement | null = null
onMounted(() => {
	robots = document.createElement('meta')
	robots.name = 'robots'
	robots.content = 'noindex, nofollow'
	document.head.appendChild(robots)
})

const description = computed(() => definition.value?.description
	? sanitizeProjectDescription(definition.value.description)
	: '')

async function onSubmit(payload: {body: SubmissionBody, files: File[]}) {
	if (submitting.value || !loaded.value) return
	submitting.value = true
	serverErrors.value = null
	submitProblem.value = null
	try {
		result.value = await submitPublicForm({
			token: props.token,
			body: payload.body,
			files: payload.files,
			authed: loaded.value.authed,
		})
		await nextTick()
		resultBox.value?.focus()
		startRedirect()
	} catch (cause) {
		const answers = (cause as {code?: number} | null)?.code === FORM_ERROR.INVALID_ANSWERS ? serverErrorsFrom(cause) : null
		if (answers) {
			serverErrors.value = answers
			return
		}
		const found = classifyPublicProblem(cause)
		if (found.kind === 'rate' || found.kind === 'network' || found.kind === 'other') {
			submitProblem.value = found
		} else {
			pageProblem.value = found
		}
	} finally {
		submitting.value = false
	}
}

// ---- after a submission --------------------------------------------------------------------------------------------

const REDIRECT_DELAY = 4000
const redirect = computed(() => result.value && result.value.redirect_url ? redirectTarget(result.value.redirect_url) : null)
let redirectTimer: ReturnType<typeof setTimeout> | undefined

function startRedirect() {
	const target = redirect.value
	if (!target) return
	redirectTimer = setTimeout(() => window.location.assign(target.href), REDIRECT_DELAY)
}

function goNow() {
	if (redirect.value) window.location.assign(redirect.value.href)
}

onBeforeUnmount(() => {
	clearTimeout(redirectTimer)
	robots?.remove()
})

const successMessage = computed(() => result.value?.message || t('forms.public.thanks'))

function another() {
	clearTimeout(redirectTimer)
	result.value = null
	serverErrors.value = null
	submitProblem.value = null
	renderer.value?.reset()
}

// ---- problems --------------------------------------------------------------------------------------------------------

function login() {
	saveLastVisited('forms.public', {token: props.token}, {})
	router.push({name: 'user.login'})
}

async function retry() {
	pageProblem.value = null
	queryClient.removeQueries({queryKey: formKeys.public(props.token)})
	await query.refetch()
}

const retryMinutes = computed(() => {
	const found = problem.value ?? submitProblem.value
	return found?.kind === 'rate' && found.retryAfter > 0 ? Math.max(1, Math.ceil(found.retryAfter / 60)) : 0
})

const submitMessage = computed(() => {
	const found = submitProblem.value
	if (!found) return ''
	if (found.kind === 'rate') {
		return retryMinutes.value > 0
			? t('forms.public.rateLimited', {minutes: retryMinutes.value}, retryMinutes.value)
			: t('forms.public.rateLimitedNoTime')
	}
	if (found.kind === 'network') return t('forms.public.networkError')
	return found.kind === 'other' && found.message ? found.message : t('forms.public.failed')
})

watch(() => props.token, () => {
	pageProblem.value = null
	result.value = null
})
</script>

<template>
	<div class="fp">
		<a
			href="#fp-main"
			class="fp__skip"
		>{{ $t('misc.skipToContent') }}</a>
		<header class="fp__brand">
			<Logo class="fp__logo" />
		</header>

		<main
			id="fp-main"
			class="fp__card"
			tabindex="-1"
		>
			<!-- loading -->
			<div
				v-if="query.isPending.value && !problem"
				class="fp__skeleton"
				role="status"
				:aria-label="$t('forms.public.loading')"
			>
				<span class="fp__bar fp__bar--title" />
				<span class="fp__bar" />
				<span class="fp__bar fp__bar--field" />
				<span class="fp__bar fp__bar--field" />
				<span class="fp__bar fp__bar--short" />
			</div>

			<!-- terminal states -->
			<section
				v-else-if="problem"
				class="fp__state"
				role="status"
			>
				<template v-if="problem.kind === 'notFound'">
					<h1>{{ $t('forms.public.notFound.title') }}</h1>
					<p>{{ $t('forms.public.notFound.text') }}</p>
				</template>
				<template v-else-if="problem.kind === 'unavailable'">
					<h1>{{ problem.title || $t('forms.public.unavailable.title') }}</h1>
					<p>
						{{ problem.reason === 'closed' || problem.reason === 'full'
							? $t(`forms.public.unavailable.${problem.reason}`)
							: $t('forms.public.unavailable.disabled') }}
					</p>
				</template>
				<template v-else-if="problem.kind === 'login'">
					<h1>{{ $t('forms.public.login.title') }}</h1>
					<p>{{ $t('forms.public.login.text') }}</p>
					<XButton
						type="button"
						@click="login"
					>
						{{ $t('forms.public.login.button') }}
					</XButton>
				</template>
				<template v-else-if="problem.kind === 'forbidden'">
					<h1>{{ $t('forms.public.forbidden.title') }}</h1>
					<p>{{ $t('forms.public.forbidden.text') }}</p>
				</template>
				<template v-else-if="problem.kind === 'rate'">
					<h1>{{ $t('forms.public.rate.title') }}</h1>
					<p>
						{{ retryMinutes > 0
							? $t('forms.public.rateLimited', {minutes: retryMinutes}, retryMinutes)
							: $t('forms.public.rateLimitedNoTime') }}
					</p>
				</template>
				<template v-else>
					<h1>{{ $t('forms.public.error.title') }}</h1>
					<p>
						{{ problem.kind === 'network' ? $t('forms.public.networkError') : problem.kind === 'other' && problem.message ? problem.message : $t('forms.public.failed') }}
					</p>
					<XButton
						type="button"
						variant="secondary"
						@click="retry"
					>
						{{ $t('forms.public.retry') }}
					</XButton>
				</template>
			</section>

			<!-- confirmation -->
			<section
				v-else-if="result && definition"
				ref="resultBox"
				class="fp__state fp__done"
				tabindex="-1"
				role="status"
			>
				<h1>{{ definition.title }}</h1>
				<p class="fp__message">
					{{ successMessage }}
				</p>
				<p
					v-if="redirect"
					class="fp__redirect"
				>
					{{ $t('forms.public.redirecting', {host: redirect.host}) }}
					<a
						:href="redirect.href"
						rel="noopener noreferrer"
						@click.prevent="goNow"
					>{{ $t('forms.public.redirectNow') }}</a>
				</p>
				<XButton
					v-if="definition.success.submit_another && !redirect"
					type="button"
					variant="secondary"
					@click="another"
				>
					{{ $t('forms.public.another') }}
				</XButton>
			</section>

			<!-- the form -->
			<template v-else-if="definition">
				<h1 class="fp__title">
					{{ definition.title }}
				</h1>
				<!-- eslint-disable vue/no-v-html -- sanitized with DOMPurify -->
				<div
					v-if="description"
					class="fp__description content"
					v-html="description"
				/>
				<!-- eslint-enable vue/no-v-html -->
				<p
					v-if="submitMessage"
					class="fp__alert"
					role="alert"
				>
					{{ submitMessage }}
				</p>
				<FormRenderer
					ref="renderer"
					:definition="definition"
					:submitting="submitting"
					:server-errors="serverErrors"
					@submit="onSubmit"
				/>
			</template>
		</main>
	</div>
</template>

<style lang="scss" scoped>
.fp {
	min-block-size: 100vh;
	background: var(--site-background);
	color: var(--text);
	display: flex;
	flex-direction: column;
	align-items: center;
	padding: 1rem 1rem 3rem;
	box-sizing: border-box;

	&__skip {
		position: absolute;
		inset-inline-start: -10000px;

		&:focus {
			inset-inline-start: 1rem;
			inset-block-start: 1rem;
			z-index: 10;
			background: var(--white);
			padding: .5rem 1rem;
		}
	}

	&__brand {
		inline-size: 100%;
		max-inline-size: 40rem;
		padding-block: .5rem 1rem;
	}

	&__logo {
		max-inline-size: 11rem;
		:deep(svg),
		:deep(img) {
			max-inline-size: 100%;
			block-size: auto;
			max-block-size: 3.5rem;
		}
	}

	&__card {
		inline-size: 100%;
		max-inline-size: 40rem;
		box-sizing: border-box;
		background: var(--white);
		border-radius: $radius;
		box-shadow: var(--shadow-md);
		padding: 1.25rem;
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
		min-inline-size: 0;

		@media screen and (min-width: $tablet) {
			padding: 2rem;
		}

		&:focus {
			outline: none;
		}
	}

	&__title,
	&__state h1 {
		margin: 0;
		font-size: 1.6rem;
		line-height: 1.25;
		overflow-wrap: anywhere;
	}

	&__description {
		overflow-wrap: anywhere;

		:deep(p) {
			margin-block: 0 .75rem;
		}
	}

	&__state {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 1rem;

		p {
			margin: 0;
		}

		&:focus {
			outline: none;
		}
	}

	&__message {
		white-space: pre-line;
		overflow-wrap: anywhere;
	}

	&__alert {
		margin: 0;
		padding: .7rem .9rem;
		border: 1px solid var(--danger);
		border-radius: $radius;
		color: var(--danger);
		font-weight: 600;
	}

	&__skeleton {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	&__bar {
		display: block;
		block-size: 1rem;
		border-radius: $radius;
		background: var(--grey-200);
		animation: fp-pulse 1.4s ease-in-out infinite;

		&--title {
			block-size: 1.8rem;
			inline-size: 60%;
		}

		&--field {
			block-size: 2.75rem;
		}

		&--short {
			inline-size: 8rem;
			block-size: 2.75rem;
		}
	}
}

@keyframes fp-pulse {
	50% {
		opacity: .45;
	}
}

@media (prefers-reduced-motion: reduce) {
	.fp__bar {
		animation: none;
	}
}
</style>
