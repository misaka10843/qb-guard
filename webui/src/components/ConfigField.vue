<script setup>
import { computed } from 'vue'
import FieldInput from './FieldInput.vue'

const props = defineProps({
  cfg: { type: Object, required: true },
  field: { type: Object, required: true },
})

const parts = computed(() => props.field.key.split('.'))

function read() {
  return parts.value.reduce((o, k) => (o == null ? undefined : o[k]), props.cfg)
}

const value = computed({
  get: read,
  set(v) {
    const keys = parts.value
    let node = props.cfg
    for (const k of keys.slice(0, -1)) {
      if (typeof node[k] !== 'object' || node[k] === null) node[k] = {}
      node = node[k]
    }
    node[keys[keys.length - 1]] = v
  },
})

const present = computed(() => read() !== undefined)
</script>

<template>
  <FieldInput v-model="value" :field="field" :present="present" />
</template>
