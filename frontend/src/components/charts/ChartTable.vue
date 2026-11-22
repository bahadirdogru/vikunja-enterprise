<script setup lang="ts">
import {useI18n} from 'vue-i18n'

defineProps<{
	caption: string
	columns: string[]
	rows: string[][]
}>()

const {t} = useI18n({useScope: 'global'})
</script>

<template>
	<table
		class="chart-table"
		:aria-label="t('charts.dataTable')"
	>
		<caption>{{ caption }}</caption>
		<thead>
			<tr>
				<th
					v-for="column in columns"
					:key="column"
					scope="col"
				>
					{{ column }}
				</th>
			</tr>
		</thead>
		<tbody>
			<tr
				v-for="(row, i) in rows"
				:key="i"
			>
				<th scope="row">
					{{ row[0] }}
				</th>
				<td
					v-for="(cell, j) in row.slice(1)"
					:key="j"
				>
					{{ cell }}
				</td>
			</tr>
		</tbody>
	</table>
</template>

<style lang="scss" scoped>
.chart-table {
	position: absolute;
	inline-size: 1px;
	block-size: 1px;
	overflow: hidden;
	clip-path: inset(50%);
	white-space: nowrap;
}
</style>
