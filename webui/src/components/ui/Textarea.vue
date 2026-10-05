<script setup>
import { cn } from '@/lib/utils'

const props = defineProps({
  class: { type: null, default: '' },
  rows: { type: [Number, String], default: 3 },
})
const model = defineModel({ type: String, default: '' })
</script>

<template>
  <textarea
    v-model="model"
    :rows="rows"
    :class="
      cn(
        'flex w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs transition-[color,box-shadow] outline-none',
        'placeholder:text-muted-foreground',
        'focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40',
        'disabled:cursor-not-allowed disabled:opacity-50',
        props.class,
      )
    "
  />
</template>
