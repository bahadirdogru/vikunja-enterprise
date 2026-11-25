<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {useI18n} from 'vue-i18n'

import Multiselect from '@/components/input/Multiselect.vue'
import User from '@/components/misc/User.vue'
import {useProjectUserSearch} from '@/composables/useUserSearch'
import {asNumberArray} from '@/helpers/customFields'
import {getDisplayName, type UserWithId} from '@/helpers/user'

const props = defineProps<{
	projectId: number
	modelValue: unknown
	labelledby: string
	disabled?: boolean
	error?: string | null
}>()

const emit = defineEmits<{
	commit: [value: number[]],
}>()

type Member = UserWithId & {name: string}

const {t, locale} = useI18n({useScope: 'global'})

const search = ref('')
// Member lookup is also what turns stored ids back into names, so it runs from the start.
const memberSearch = useProjectUserSearch(() => props.projectId, search, true)

// The search only returns a page of members; keep every user seen so selected ids keep their name.
const known = ref(new Map<number, Member>())
watch(
	() => memberSearch.users.value,
	users => {
		for (const user of users) {
			if (user.id !== undefined) {
				known.value.set(user.id, {...user, id: user.id, name: getDisplayName(user)})
			}
		}
	},
	{immediate: true},
)

const selectedIds = computed(() => asNumberArray(props.modelValue))
const selected = computed<Member[]>(() => selectedIds.value.map(id =>
	known.value.get(id) ?? {id, name: `#${id}`},
))
const results = computed<Member[]>(() => [...known.value.values()]
	.filter(user => !selectedIds.value.includes(user.id))
	.filter(user => search.value === ''
		|| `${user.name} ${user.username ?? ''}`.toLowerCase().includes(search.value.toLowerCase()))
	.sort((a, b) => a.name.localeCompare(b.name, locale.value)))

function update(value: Member | Member[] | null) {
	const users = Array.isArray(value) ? value : []
	emit('commit', users.map(user => user.id))
}
</script>

<template>
	<div class="user-value">
		<Multiselect
			:model-value="selected"
			multiple
			show-empty
			label="name"
			:disabled="disabled"
			:loading="memberSearch.isFetching.value"
			:search-results="results"
			:autocomplete-enabled="false"
			:aria-label="t('customFields.value.userSearch')"
			:placeholder="t('customFields.value.userSearch')"
			:select-placeholder="t('task.assignee.selectPlaceholder')"
			@search="(query: string) => search = query"
			@update:modelValue="update"
		>
			<template #searchResult="{option}">
				<User
					:avatar-size="24"
					:show-username="true"
					:user="option"
				/>
			</template>
		</Multiselect>
		<p
			v-if="error"
			class="help is-danger"
			role="alert"
		>
			{{ error }}
		</p>
	</div>
</template>

<style lang="scss" scoped>
.user-value {
	inline-size: 100%;
}
</style>
