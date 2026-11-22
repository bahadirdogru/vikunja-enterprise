<script setup lang="ts">
// The project's automation secrets: names and timestamps only. A value is typed into a password field, sent once and
// never shown again; there is no way to read one back.
import {computed, ref} from 'vue'
import {useQuery} from '@tanstack/vue-query'

import AutomationBadge from '@/components/automation/AutomationBadge.vue'
import SecretForm from '@/components/automation/SecretForm.vue'
import XButton from '@/components/input/Button.vue'
import ErrorMessage from '@/components/misc/Error.vue'
import Loading from '@/components/misc/Loading.vue'
import Modal from '@/components/misc/Modal.vue'
import {
	automationSecretsQuery,
	useDeleteSecretMutation,
	type AutomationSecretResponse,
} from '@/client/queries/automationSecrets'
import {isRequestContextAbort} from '@/client/requestContext'
import {formatDateLong, formatDateSince} from '@/helpers/time/formatDate'

const props = defineProps<{
	projectId: number
}>()

const secrets = useQuery(computed(() => automationSecretsQuery(props.projectId)))
const items = computed(() => secrets.data.value ?? [])
const names = computed(() => items.value.map(secret => secret.name))

const adding = ref(false)
const replacing = ref<string | null>(null)
const toDelete = ref<AutomationSecretResponse | null>(null)
const remove = useDeleteSecretMutation()

function startAdd() {
	replacing.value = null
	adding.value = true
}

function startReplace(name: string) {
	adding.value = false
	replacing.value = name
}

async function confirmDelete() {
	const secret = toDelete.value
	if (!secret) return
	try {
		await remove.mutateAsync({projectId: props.projectId, name: secret.name})
	} catch (cause) {
		if (isRequestContextAbort(cause)) return
	}
	toDelete.value = null
}
</script>

<template>
	<section
		class="secrets"
		aria-labelledby="automation-secrets-title"
	>
		<div class="secrets__head">
			<h3
				id="automation-secrets-title"
				class="secrets__title"
			>
				{{ $t('automation.secrets.title') }}
			</h3>
			<XButton
				v-if="!adding"
				type="button"
				variant="secondary"
				icon="plus"
				@click="startAdd"
			>
				{{ $t('automation.secrets.new') }}
			</XButton>
		</div>
		<p class="help">
			{{ $t('automation.secrets.intro') }}
		</p>
		<p class="help secrets__security">
			<Icon icon="lock" />
			{{ $t('automation.secrets.security') }}
		</p>

		<SecretForm
			v-if="adding"
			:project-id="projectId"
			:existing="names"
			@saved="adding = false"
			@cancel="adding = false"
		/>

		<ErrorMessage v-if="secrets.error.value" />
		<Loading v-else-if="secrets.isPending.value" />
		<p
			v-else-if="items.length === 0 && !adding"
			class="help"
		>
			{{ $t('automation.secrets.empty') }}
		</p>
		<ul
			v-else-if="items.length > 0"
			class="secrets__list"
		>
			<li
				v-for="secret in items"
				:key="secret.id"
				class="secret"
			>
				<div class="secret__main">
					<code class="secret__name">{{ secret.name }}</code>
					<AutomationBadge
						v-if="secret.unreadable"
						variant="warning"
						role="img"
						:aria-label="$t('automation.secrets.unreadableHelp')"
						:title="$t('automation.secrets.unreadableHelp')"
					>
						<Icon icon="exclamation-circle" />
						{{ $t('automation.secrets.unreadable') }}
					</AutomationBadge>
					<span class="help secret__dates">
						{{ $t('automation.secrets.createdAt') }}
						<span v-tooltip="formatDateLong(secret.created)">{{ formatDateSince(secret.created) }}</span>
						&middot;
						{{ $t('automation.secrets.updatedAt') }}
						<span v-tooltip="formatDateLong(secret.updated)">{{ formatDateSince(secret.updated) }}</span>
					</span>
					<p
						v-if="secret.unreadable"
						class="help is-danger secret__warning"
					>
						{{ $t('automation.secrets.unreadableHelp') }}
					</p>
				</div>
				<div class="secret__tools">
					<XButton
						type="button"
						variant="secondary"
						icon="pen"
						:aria-label="$t('automation.secrets.replaceAria', {name: secret.name})"
						@click="startReplace(secret.name)"
					>
						{{ $t('automation.secrets.replace') }}
					</XButton>
					<XButton
						type="button"
						variant="secondary"
						icon="trash-alt"
						danger
						:aria-label="$t('automation.secrets.deleteAria', {name: secret.name})"
						@click="toDelete = secret"
					/>
				</div>
				<SecretForm
					v-if="replacing === secret.name"
					class="secret__form"
					:project-id="projectId"
					:name="secret.name"
					:id-prefix="`automation-secret-${secret.id}`"
					@saved="replacing = null"
					@cancel="replacing = null"
				/>
			</li>
		</ul>

		<Modal
			:enabled="toDelete !== null"
			@close="toDelete = null"
			@submit="confirmDelete"
		>
			<template #header>
				<span>{{ $t('automation.secrets.deleteConfirm.title') }}</span>
			</template>
			<template #text>
				<p>{{ $t('automation.secrets.deleteConfirm.text', {name: toDelete?.name ?? ''}) }}</p>
			</template>
		</Modal>
	</section>
</template>

<style lang="scss" scoped>
.secrets {
	display: flex;
	flex-direction: column;
	gap: .6rem;
	margin-block-start: 2rem;
	padding-block-start: 1.25rem;
	border-block-start: 1px solid var(--grey-200);

	&__head {
		display: flex;
		flex-wrap: wrap;
		gap: .5rem 1rem;
		align-items: center;
		justify-content: space-between;
	}

	&__title {
		margin: 0;
		font-size: 1.15rem;
		font-weight: 700;
	}

	.help {
		margin: 0;
	}

	&__security {
		display: flex;
		gap: .4rem;
		align-items: flex-start;
		color: var(--grey-700);

		:deep(svg) {
			margin-block-start: .2rem;
			flex: none;
		}
	}

	&__list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: .5rem;
	}
}

.secret {
	display: flex;
	flex-wrap: wrap;
	gap: .5rem 1rem;
	align-items: center;
	justify-content: space-between;
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	padding: .6rem .8rem;

	&__main {
		display: flex;
		flex-wrap: wrap;
		gap: .3rem .75rem;
		align-items: center;
		flex: 1 1 16rem;
		min-inline-size: 0;
	}

	&__name {
		font-weight: 700;
		word-break: break-all;
	}

	&__warning {
		flex-basis: 100%;
	}

	&__tools {
		display: flex;
		gap: .4rem;
	}

	&__form {
		flex-basis: 100%;
	}
}
</style>
