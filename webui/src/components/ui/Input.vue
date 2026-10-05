<script setup>
import { cn } from '@/lib/utils'

const props = defineProps({
  class: { type: null, default: '' },
  type: { type: String, default: 'text' },
})
const model = defineModel({ type: null })
</script>

<template>
  <input
    v-model="model"
    :type="type"
    :class="
      cn(
        'flex h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-[color,box-shadow] outline-none',
        'placeholder:text-muted-foreground',
        'focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40',
        'disabled:cursor-not-allowed disabled:opacity-50',
        props.class,
      )
    "
  />
</template>
