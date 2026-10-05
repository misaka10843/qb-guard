<script setup>
import { cn } from '@/lib/utils'
import { SelectRoot, SelectTrigger, SelectValue, SelectPortal, SelectContent, SelectViewport, SelectItem, SelectItemText, SelectIcon } from 'reka-ui'
import { ChevronDown, Check } from 'lucide-vue-next'

const props = defineProps({
  options: { type: Array, default: () => [] },
  placeholder: { type: String, default: '请选择' },
  class: { type: null, default: '' },
  disabled: Boolean,
})
const model = defineModel({ type: null })
</script>

<template>
  <SelectRoot v-model="model" :disabled="disabled">
    <SelectTrigger
      :class="
        cn(
          'flex h-9 w-full items-center justify-between gap-2 rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none',
          'focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40',
          'disabled:cursor-not-allowed disabled:opacity-50 data-[placeholder]:text-muted-foreground',
          props.class,
        )
      "
    >
      <SelectValue :placeholder="placeholder" />
      <SelectIcon><ChevronDown class="size-4 opacity-60" /></SelectIcon>
    </SelectTrigger>
    <SelectPortal>
      <SelectContent
        position="popper"
        :side-offset="4"
        class="z-50 max-h-72 min-w-[var(--reka-select-trigger-width)] overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md"
      >
        <SelectViewport class="p-1">
          <SelectItem
            v-for="opt in options"
            :key="String(opt.value)"
            :value="opt.value"
            class="relative flex cursor-pointer items-center rounded-sm py-1.5 pr-8 pl-2 text-sm outline-none select-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
          >
            <SelectItemText>{{ opt.label }}</SelectItemText>
            <span class="absolute right-2 flex size-4 items-center justify-center">
              <Check v-if="model === opt.value" class="size-4" />
            </span>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
