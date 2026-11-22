<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import ProgressBar from '@/components/misc/ProgressBar.vue'
import User from '@/components/misc/User.vue'
import type {CustomFieldResponse} from '@/client/queries/customFields'
import {useProjectUserSearch} from '@/composables/useUserSearch'
import {asNumberArray, readableTextColor} from '@/helpers/customFields'
import {formatCustomFieldNumber, ratingStars} from '@/helpers/customFieldDisplay'
import {isValidHttpUrl} from '@/helpers/isValidHttpUrl'
import {getDisplayName} from '@/helpers/user'
import {dateIsValid, formatDateLong, formatDisplayDate} from '@/helpers/time/formatDate'

// Compact, read-only rendering of one custom field value (table cells, kanban cards).
// Renders nothing for an unset value, so callers can use it without a guard.
const props = defineProps<{
	field: CustomFieldResponse
	value: unknown
	// Needed to resolve user ids to names.
	projectId: number
}>()

const {t, locale} = useI18n({useScope: 'global'})

const isEmpty = computed(() => props.value === null || props.value === undefined || props.value === ''
	|| (Array.isArray(props.value) && props.value.length === 0))

const text = computed(() => typeof props.value === 'string' ? props.value : String(props.value ?? ''))

const numberText = computed(() => typeof props.value === 'number'
	? formatCustomFieldNumber(props.value, props.field, locale.value)
	: '')

const dateText = computed(() => {
	if (typeof props.value !== 'string' || !dateIsValid(props.value)) {
		return ''
	}
	return formatDisplayDate(props.value)
})
const dateTitle = computed(() => typeof props.value === 'string' && dateIsValid(props.value)
	? formatDateLong(props.value)
	: undefined)

// select stores one option id, multi_select a list
const optionIds = computed(() => typeof props.value === 'number' ? [props.value] : asNumberArray(props.value))
const options = computed(() => optionIds.value
	.map(id => props.field.options.find(option => option.id === id))
	.filter(option => option !== undefined))

function chipStyle(color: string) {
	return color
		? {backgroundColor: color, color: readableTextColor(color)}
		: undefined
}

const stars = computed(() => ratingStars(props.field, props.value))
const progress = computed(() => typeof props.value === 'number' ? Math.max(0, Math.min(100, props.value)) : 0)

const isUser = computed(() => props.field.type === 'user')
const members = useProjectUserSearch(() => props.projectId, '', () => isUser.value && !isEmpty.value)
const users = computed(() => asNumberArray(props.value).map(id => {
	const member = members.users.value.find(user => user.id === id)
	return member ?? {id, username: `#${id}`}
}))

const safeUrl = computed(() => isValidHttpUrl(text.value))
</script>

<template>
	<span
		v-if="!isEmpty"
		class="cf-value"
		:class="`cf-value--${field.type}`"
	>
		<template v-if="field.type === 'text' || field.type === 'long_text'">
			<span
				class="cf-value__text"
				:title="text"
			>{{ text }}</span>
		</template>
		<template v-else-if="field.type === 'number' || field.type === 'currency'">
			<span class="cf-value__number">{{ numberText }}</span>
		</template>
		<template v-else-if="field.type === 'date'">
			<time
				v-if="dateText"
				:datetime="text"
				:title="dateTitle"
			>{{ dateText }}</time>
		</template>
		<template v-else-if="field.type === 'checkbox'">
			<span
				v-if="value === true"
				class="cf-value__check"
				role="img"
				:aria-label="t('customFields.display.yes')"
			>
				<Icon icon="check" />
			</span>
			<span
				v-else
				class="cf-value__muted"
			>
				<span aria-hidden="true">-</span>
				<span class="is-sr-only">{{ t('customFields.display.no') }}</span>
			</span>
		</template>
		<template v-else-if="field.type === 'select' || field.type === 'multi_select'">
			<span
				v-for="option in options"
				:key="option.id"
				class="tag cf-value__chip"
				:class="{'cf-value__chip--plain': !option.color}"
				:style="chipStyle(option.color)"
			>{{ option.title }}</span>
		</template>
		<template v-else-if="field.type === 'user'">
			<User
				v-for="user in users"
				:key="user.id"
				class="cf-value__user"
				:user="user"
				:avatar-size="20"
				:show-username="true"
				is-inline
				:title="getDisplayName(user)"
			/>
		</template>
		<template v-else-if="field.type === 'url'">
			<a
				v-if="safeUrl"
				:href="text"
				:title="text"
				class="cf-value__text"
				target="_blank"
				rel="noopener noreferrer"
				@click.stop
			>{{ text }}</a>
			<span
				v-else
				class="cf-value__text"
				:title="text"
			>{{ text }}</span>
		</template>
		<template v-else-if="field.type === 'email'">
			<a
				:href="`mailto:${text}`"
				:title="text"
				class="cf-value__text"
				@click.stop
			>{{ text }}</a>
		</template>
		<template v-else-if="field.type === 'rating'">
			<span
				class="cf-value__rating"
				role="img"
				:aria-label="t('customFields.value.ratingStar', {n: stars.filled, max: stars.max})"
			>
				<Icon
					v-for="star in stars.max"
					:key="star"
					:icon="star <= stars.filled ? 'star' : ['far', 'star']"
					:class="{'is-filled': star <= stars.filled}"
				/>
			</span>
		</template>
		<template v-else-if="field.type === 'progress'">
			<span class="cf-value__progress">
				<ProgressBar
					:value="progress"
					is-small
					is-primary
				/>
				<span class="cf-value__percent">{{ progress }}%</span>
			</span>
		</template>
	</span>
</template>

<style lang="scss" scoped>
.cf-value {
	display: inline-flex;
	flex-wrap: wrap;
	align-items: center;
	gap: .25rem;
	max-inline-size: 100%;
	vertical-align: middle;

	&__text {
		display: inline-block;
		max-inline-size: 16rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	&__number {
		font-variant-numeric: tabular-nums;
	}

	&__check {
		color: var(--success);
	}

	&__muted {
		color: var(--grey-400);
	}

	&__chip {
		border-radius: 1rem;

		&--plain {
			background: var(--grey-200);
			color: var(--grey-700);
		}
	}

	&__user {
		margin: 0;
	}

	&__rating {
		display: inline-flex;
		gap: .0625rem;
		color: var(--grey-300);

		.is-filled {
			color: var(--warning);
		}
	}

	&__progress {
		display: inline-flex;
		align-items: center;
		gap: .375rem;
		min-inline-size: 6rem;

		:deep(.progress-bar) {
			--progress-height: .5rem;
			flex: 1;
			inline-size: 4.5rem;
		}
	}

	&__percent {
		font-size: .8em;
		font-variant-numeric: tabular-nums;
	}
}
</style>
