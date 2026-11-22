<script setup lang="ts">
import XButton from '@/components/input/Button.vue'
// One node of the condition tree: a group (all / any / not), a comparison or a script. Presentational: every change is
// emitted with the tree path of the node, ConditionBuilder applies it.
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

import AutomationSelect, {type SelectChoice} from '@/components/automation/AutomationSelect.vue'
import CodeEditor from '@/components/automation/CodeEditor.vue'
import ParamInput from '@/components/automation/ParamInput.vue'
import {useAutomationContext} from '@/components/automation/context'
import {
	catalogLabel,
	childrenOf,
	customFieldIdOf,
	fieldDef,
	fieldGroupOf,
	groupKind,
	isScriptNode,
	operatorDef,
	operatorsFor,
	pathToId,
	type CatalogField,
	type ConditionNode,
	type NodePath,
} from '@/helpers/automation'

const props = defineProps<{
	node: ConditionNode
	treePath: NodePath
	path: string
}>()

const emit = defineEmits<{
	change: [treePath: NodePath, node: ConditionNode]
	add: [treePath: NodePath, type: 'comparison' | 'group' | 'script']
	remove: [treePath: NodePath]
}>()

const {t, te} = useI18n({useScope: 'global'})
const context = useAutomationContext()

const kind = computed(() => groupKind(props.node))
const isRoot = computed(() => props.treePath.length === 0)
const errors = computed(() => context.errors.value)
const nodeErrors = (path: string) => errors.value.get(path) ?? []

// ---- groups --------------------------------------------------------------------------------------------------------
const kindChoices = computed<SelectChoice[]>(() => [
	{value: 'all', label: t('automation.condition.all')},
	{value: 'any', label: t('automation.condition.any')},
	{value: 'not', label: t('automation.condition.not')},
])

function setKind(next: unknown) {
	const current = kind.value
	if (!current || (next !== 'all' && next !== 'any' && next !== 'not')) return
	emit('change', props.treePath, {[next]: props.node[current] ?? []})
}

// ---- comparisons ---------------------------------------------------------------------------------------------------
const hasWorkflow = computed(() => context.statuses.length > 0)

const visibleFields = computed(() => context.catalog.fields.filter(field => {
	if (field.pattern !== '') return false
	if (!hasWorkflow.value && (field.kind === 'status' || field.name.endsWith('status_category'))) return false
	return true
}))

function fieldLabel(field: CatalogField): string {
	return field.title !== '' ? field.title : catalogLabel(t, te, field.title_key, field.name)
}

const GROUP_ORDER = ['task', 'status', 'custom', 'event']

const fieldChoices = computed<SelectChoice[]>(() => {
	const out = [...visibleFields.value]
		.sort((a, b) => GROUP_ORDER.indexOf(fieldGroupOf(a)) - GROUP_ORDER.indexOf(fieldGroupOf(b)))
		.map(field => ({
			value: field.name,
			label: fieldLabel(field),
			group: t(`automation.fieldGroup.${fieldGroupOf(field)}`),
		}))
	// A field of a deleted custom field or of a newer backend must stay selectable as it is.
	if (props.node.field && !out.some(choice => choice.value === props.node.field)) {
		out.push({value: props.node.field, label: props.node.field, group: t('automation.fieldGroup.task')})
	}
	return out
})

const field = computed(() => props.node.field ? fieldDef(context.catalog, props.node.field) : undefined)
const operatorChoices = computed<SelectChoice[]>(() => {
	const ops = operatorsFor(context.catalog, field.value)
	const out = ops.map(op => ({value: op.type, label: catalogLabel(t, te, op.title_key, op.type)}))
	if (props.node.op && !out.some(choice => choice.value === props.node.op)) {
		out.push({value: props.node.op, label: props.node.op})
	}
	return out
})
const operator = computed(() => props.node.op ? operatorDef(context.catalog, props.node.op) : undefined)

// Keywords the server accepts for user values come from the catalog's user parameters.
const userKeywords = computed(() => {
	for (const action of context.catalog.actions) {
		const param = action.params.find(p => p.kind === 'user' && p.enum.length > 0)
		if (param) return param.enum
	}
	return []
})

const customField = computed(() => {
	const id = props.node.field ? customFieldIdOf(props.node.field) : null
	return id === null ? null : context.customFields.find(f => f.id === id) ?? null
})

const valueConfig = computed(() => {
	const def = field.value
	const op = operator.value
	if (!def || !op || op.no_value) return null
	let kind = def.kind
	if (kind === 'users') kind = 'user'
	if (kind === 'custom_field') kind = 'any'
	if (op.type === 'like') kind = 'string'
	let choices: SelectChoice[] | undefined
	if (kind === 'enum' && customField.value) {
		choices = customField.value.options.map(option => ({value: option.id, label: option.title}))
	}
	return {
		kind,
		multiple: op.list,
		enumValues: kind === 'user' ? userKeywords.value : def.enum,
		choices,
	}
})

function setField(name: unknown) {
	if (typeof name !== 'string') return
	const next = fieldDef(context.catalog, name)
	const ops = operatorsFor(context.catalog, next)
	emit('change', props.treePath, {field: name, op: ops[0]?.type ?? ''})
}

function setOp(type: unknown) {
	if (typeof type !== 'string') return
	const op = operatorDef(context.catalog, type)
	const next: ConditionNode = {field: props.node.field, op: type}
	if (op && !op.no_value) {
		const value = props.node.value
		if (op.list) next.value = value === undefined || value === null ? [] : Array.isArray(value) ? value : [value]
		else if (Array.isArray(value)) next.value = value[0] ?? undefined
		else if (value !== undefined && value !== null) next.value = value
	}
	emit('change', props.treePath, next)
}

function setValue(value: unknown) {
	emit('change', props.treePath, {field: props.node.field, op: props.node.op, value})
}

function setScript(script: string) {
	emit('change', props.treePath, {script})
}

function childPath(index: number): string {
	return `${props.path}.${kind.value}[${index}]`
}
</script>

<template>
	<div
		class="cond-node"
		:class="{'is-group': kind, 'is-root': isRoot}"
	>
		<!-- group -->
		<template v-if="kind">
			<div class="cond-group__head">
				<AutomationSelect
					:id="pathToId(path)"
					:model-value="kind"
					:options="kindChoices"
					:label="$t('automation.condition.groupType')"
					@update:modelValue="setKind"
				/>
				<XButton
					type="button"
					variant="secondary"
					icon="trash-alt"
					danger
					@click="emit('remove', treePath)"
				>
					{{ isRoot ? $t('automation.condition.clear') : $t('automation.condition.removeGroup') }}
				</XButton>
			</div>
			<p
				v-for="message in nodeErrors(path)"
				:key="message"
				class="help is-danger"
				role="alert"
			>
				{{ message }}
			</p>
			<ul class="cond-group__children">
				<li
					v-for="(child, index) in childrenOf(node)"
					:key="index"
				>
					<ConditionNodeEditor
						:node="child"
						:tree-path="[...treePath, index]"
						:path="childPath(index)"
						@change="(p, n) => emit('change', p, n)"
						@add="(p, type) => emit('add', p, type)"
						@remove="p => emit('remove', p)"
					/>
				</li>
			</ul>
			<div class="cond-group__add">
				<XButton
					type="button"
					variant="secondary"
					icon="plus"
					@click="emit('add', treePath, 'comparison')"
				>
					{{ $t('automation.condition.addComparison') }}
				</XButton>
				<XButton
					type="button"
					variant="secondary"
					icon="plus"
					@click="emit('add', treePath, 'group')"
				>
					{{ $t('automation.condition.addGroup') }}
				</XButton>
				<XButton
					v-if="context.catalog.supportsScript"
					type="button"
					variant="secondary"
					icon="code"
					@click="emit('add', treePath, 'script')"
				>
					{{ $t('automation.condition.addScript') }}
				</XButton>
			</div>
		</template>

		<!-- script condition -->
		<div
			v-else-if="isScriptNode(node)"
			class="cond-script"
		>
			<div class="cond-script__head">
				<label
					:id="`${pathToId(path + '.script')}-label`"
					:for="pathToId(path + '.script')"
				>{{ $t('automation.condition.script') }}</label>
				<XButton
					type="button"
					:aria-label="$t('automation.condition.remove')"
					variant="secondary"
					icon="xmark"
					danger
					@click="emit('remove', treePath)"
				/>
			</div>
			<CodeEditor
				:id="pathToId(path + '.script')"
				:model-value="node.script ?? ''"
				:label="$t('automation.condition.script')"
				mode="condition"
				:invalid="nodeErrors(path + '.script').length > 0"
				:readonly="!context.catalog.supportsScript"
				:error-position="context.lines?.value.get(path + '.script') ?? null"
				:rows="5"
				@update:modelValue="setScript"
			/>
			<p
				v-if="!context.catalog.supportsScript"
				class="help is-danger"
				role="status"
			>
				{{ $t('automation.code.disabledNote') }}
			</p>
			<p
				v-for="message in [...nodeErrors(path), ...nodeErrors(path + '.script')]"
				:key="message"
				class="help is-danger"
				role="alert"
			>
				{{ message }}
			</p>
		</div>

		<!-- comparison -->
		<div
			v-else
			class="cond-leaf"
		>
			<div class="cond-leaf__row">
				<AutomationSelect
					:id="pathToId(path + '.field')"
					class="cond-leaf__field"
					:model-value="node.field"
					:options="fieldChoices"
					:label="$t('automation.condition.field')"
					:placeholder="$t('automation.condition.chooseField')"
					:invalid="nodeErrors(path + '.field').length > 0"
					@update:modelValue="setField"
				/>
				<AutomationSelect
					:id="pathToId(path + '.op')"
					class="cond-leaf__op"
					:model-value="node.op"
					:options="operatorChoices"
					:label="$t('automation.condition.operator')"
					:placeholder="$t('automation.condition.chooseOperator')"
					:disabled="!node.field"
					:invalid="nodeErrors(path + '.op').length > 0"
					@update:modelValue="setOp"
				/>
				<div
					v-if="valueConfig"
					:id="pathToId(path + '.value') + '-wrap'"
					class="cond-leaf__value"
				>
					<ParamInput
						:id="pathToId(path + '.value')"
						:kind="valueConfig.kind"
						:multiple="valueConfig.multiple"
						:enum-values="valueConfig.enumValues"
						:choices="valueConfig.choices"
						:model-value="node.value"
						:label="$t('automation.condition.value')"
						date-context="condition"
						:invalid="nodeErrors(path + '.value').length > 0"
						@update:modelValue="setValue"
					/>
				</div>
				<XButton
					class="cond-leaf__remove"
					type="button"
					:aria-label="$t('automation.condition.remove')"
					variant="secondary"
					icon="xmark"
					danger
					@click="emit('remove', treePath)"
				/>
			</div>
			<p
				v-for="message in [...nodeErrors(path), ...nodeErrors(path + '.field'), ...nodeErrors(path + '.op'), ...nodeErrors(path + '.value')]"
				:key="message"
				class="help is-danger"
				role="alert"
			>
				{{ message }}
			</p>
		</div>
	</div>
</template>

<style lang="scss" scoped>
.cond-node.is-group {
	display: flex;
	flex-direction: column;
	gap: .5rem;
	padding: .6rem .75rem;
	border: 1px solid var(--grey-200);
	border-inline-start: 3px solid var(--primary);
	border-radius: $radius;
	background: var(--grey-50, transparent);

	&.is-root {
		border-inline-start-width: 3px;
	}
}

.cond-group__head,
.cond-group__add,
.cond-script__head {
	display: flex;
	flex-wrap: wrap;
	gap: .5rem;
	align-items: center;
	justify-content: space-between;
}

.cond-group__add {
	justify-content: flex-start;
}

.cond-group__children {
	list-style: none;
	margin: 0;
	padding: 0;
	display: flex;
	flex-direction: column;
	gap: .5rem;
}

.cond-script__head label {
	font-weight: 600;
}

.cond-leaf__row {
	display: grid;
	grid-template-columns: minmax(10rem, 1fr) minmax(8rem, .7fr) minmax(10rem, 1.5fr) auto;
	gap: .5rem;
	align-items: start;

	.cond-leaf__remove {
		grid-column: 4;
	}

	@media screen and (max-width: $tablet) {
		grid-template-columns: minmax(0, 1fr) auto;

		.cond-leaf__field,
		.cond-leaf__op,
		.cond-leaf__value {
			grid-column: 1;
		}

		.cond-leaf__remove {
			grid-column: 2;
			grid-row: 1;
		}
	}
}
</style>
