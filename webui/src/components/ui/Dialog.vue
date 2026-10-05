<script setup>
import { DialogRoot, DialogPortal, DialogOverlay, DialogContent, DialogTitle, DialogDescription, DialogClose } from 'reka-ui'
import { X } from 'lucide-vue-next'

defineProps({
  title: { type: String, default: '' },
  description: { type: String, default: '' },
  wide: Boolean,
})
const open = defineModel('open', { type: Boolean, default: false })
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-50 bg-black/60 backdrop-blur-[1px] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=open]:fade-in-0 data-[state=closed]:fade-out-0" />
      <DialogContent
        class="fixed top-1/2 left-1/2 z-50 flex max-h-[88vh] w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2 flex-col gap-4 overflow-y-auto rounded-xl border bg-card p-5 shadow-lg data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=open]:zoom-in-95 data-[state=closed]:zoom-out-95"
        :class="wide ? 'sm:max-w-3xl' : 'sm:max-w-lg'"
      >
        <div class="flex flex-col gap-1 pr-8">
          <DialogTitle class="text-base font-semibold">{{ title }}</DialogTitle>
          <DialogDescription v-if="description" class="text-sm text-muted-foreground">
            {{ description }}
          </DialogDescription>
        </div>
        <slot />
        <DialogClose
          class="absolute top-4 right-4 rounded-md p-1 text-muted-foreground opacity-70 transition-opacity hover:bg-accent hover:opacity-100"
        >
          <X class="size-4" />
        </DialogClose>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
