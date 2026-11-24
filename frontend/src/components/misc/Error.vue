<template>
	<Message variant="danger">
		<i18n-t
			keypath="loadingError.failed"
			scope="global"
		>
			<ButtonLink @click="reload">
				{{ $t('loadingError.tryAgain') }}
			</ButtonLink>
			<ButtonLink :href="FORK_ISSUES">
				{{ $t('loadingError.contact') }}
			</ButtonLink>
		</i18n-t>
	</Message>
</template>

<script lang="ts" setup>
import Message from '@/components/misc/Message.vue'
import ButtonLink from '@/components/misc/ButtonLink.vue'
import {FORK_ISSUES} from '@/urls'

defineOptions({
	inheritAttrs: false,
})

function reload() {
	window.location.reload()
}
</script>
