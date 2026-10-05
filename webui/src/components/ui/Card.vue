<script setup>
import { cn } from '@/lib/utils'

const props = defineProps({
  class: { type: null, default: '' },
  padded: { type: Boolean, default: true },
})
</script>

<template>
  <div
    :class="
      cn(
        'rounded-xl border bg-card text-card-foreground shadow-sm',
        props.padded && 'p-5',
        props.class,
      )
    "
  >
    <slot />
  </div>
</template>
