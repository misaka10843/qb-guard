<script setup>
import { cn } from '@/lib/utils'
import { SwitchRoot, SwitchThumb } from 'reka-ui'

const props = defineProps({
  disabled: Boolean,
  class: { type: null, default: '' },
})
const model = defineModel({ type: Boolean, default: false })
</script>

<template>
  <SwitchRoot
    v-model="model"
    :disabled="disabled"
    :class="
      cn(
        'peer inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border border-transparent transition-colors outline-none',
        'focus-visible:ring-2 focus-visible:ring-ring/50',
        'disabled:cursor-not-allowed disabled:opacity-50',
        'data-[state=checked]:bg-primary data-[state=unchecked]:bg-input',
        props.class,
      )
    "
  >
    <SwitchThumb
      class="pointer-events-none block size-4 rounded-full bg-background shadow-sm ring-0 transition-transform data-[state=checked]:translate-x-4 data-[state=unchecked]:translate-x-0"
    />
  </SwitchRoot>
</template>
