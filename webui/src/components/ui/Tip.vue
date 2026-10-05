<script setup>
import {
  TooltipContent,
  TooltipPortal,
  TooltipProvider,
  TooltipRoot,
  TooltipTrigger,
} from 'reka-ui'
import { Info } from 'lucide-vue-next'

const props = defineProps({
  text: { type: String, required: true },
  side: { type: String, default: 'top' },
})
</script>

<template>
  <TooltipProvider :delay-duration="120">
    <TooltipRoot>
      <TooltipTrigger as-child data-tip>
        <slot>
          <span
            class="inline-flex cursor-help items-center align-middle text-muted-foreground/70 transition-colors hover:text-foreground"
            tabindex="0"
            :aria-label="props.text"
          >
            <Info class="size-3.5" />
          </span>
        </slot>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent
          :side="props.side"
          :side-offset="6"
          class="z-50 max-w-xs rounded-md border bg-popover px-2.5 py-1.5 text-xs leading-relaxed text-popover-foreground shadow-md"
        >
          {{ props.text }}
        </TooltipContent>
      </TooltipPortal>
    </TooltipRoot>
  </TooltipProvider>
</template>
