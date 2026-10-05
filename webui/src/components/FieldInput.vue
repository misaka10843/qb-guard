<script setup>
import { computed } from 'vue'
import { Input, Label, Select, Switch, Textarea, Badge } from '@/components/ui'

const props = defineProps({
  field: { type: Object, required: true },
  present: { type: Boolean, default: true },
})
const model = defineModel({ type: null })

const text = computed({
  get() {
    const v = model.value
    return Array.isArray(v) ? v.join('\n') : ''
  },
  set(raw) {
    model.value = raw
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean)
  },
})

const ints = computed({
  get() {
    const v = model.value
    return Array.isArray(v) ? v.join(', ') : ''
  },
  set(raw) {
    model.value = raw
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter(Boolean)
      .map(Number)
      .filter((n) => Number.isFinite(n))
  },
})

const numeric = computed({
  get() {
    return model.value === undefined || model.value === null ? '' : model.value
  },
  set(raw) {
    if (raw === '') {
      model.value = undefined
      return
    }
    const n = props.field.type === 'int' ? parseInt(raw, 10) : parseFloat(raw)
    model.value = Number.isFinite(n) ? n : undefined
  },
})

const selectOptions = computed(() =>
  (props.field.options || []).map((o) => ({ value: o, label: o })),
)
</script>

<template>
  <div class="flex flex-col gap-2">
    <div class="flex flex-wrap items-center gap-2">
      <Label :for="field.key">{{ field.label || field.key }}</Label>
      <Badge v-if="!present" variant="muted">未配置</Badge>
      <Badge v-if="field.restart" variant="warning">需重启</Badge>
      <span v-if="field.unit" class="text-xs text-muted-foreground">{{ field.unit }}</span>
    </div>

    <Switch v-if="field.type === 'bool'" v-model="model" />

    <Select v-else-if="field.type === 'select'" v-model="model" :options="selectOptions" />

    <Input
      v-else-if="field.type === 'password'"
      v-model="model"
      type="password"
      autocomplete="new-password"
    />

    <Input
      v-else-if="field.type === 'int' || field.type === 'float'"
      v-model="numeric"
      type="number"
      :min="field.min"
      :max="field.max"
      :step="field.type === 'float' ? 'any' : '1'"
      class="mono"
    />

    <Textarea
      v-else-if="field.type === 'stringlist'"
      v-model="text"
      :rows="4"
      class="mono text-xs"
      placeholder="每行一项"
    />

    <Textarea
      v-else-if="field.type === 'intlist'"
      v-model="ints"
      :rows="3"
      class="mono text-xs"
      placeholder="逗号或换行分隔"
    />

    <Input
      v-else-if="field.type === 'duration'"
      v-model="model"
      class="mono"
      placeholder="24h / 7d / 3600000"
    />

    <Input v-else v-model="model" class="mono" />

    <p v-if="field.help" class="text-xs text-muted-foreground">{{ field.help }}</p>
  </div>
</template>
