<script setup>
import { cn } from '@/lib/utils'

const props = defineProps({ class: { type: null, default: '' } })
</script>

<template>
  <label :class="cn('text-sm font-medium leading-none select-none', props.class)">
    <slot />
  </label>
</template>
