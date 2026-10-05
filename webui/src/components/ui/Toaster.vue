<script setup>
import { toasts, dismiss } from '@/lib/toast'
import { CheckCircle2, AlertCircle, Info, X } from 'lucide-vue-next'

const icons = { ok: CheckCircle2, error: AlertCircle, info: Info }
const tones = {
  ok: 'border-success/40 text-success',
  error: 'border-destructive/40 text-destructive',
  info: 'border-border text-foreground',
}
</script>

<template>
  <div class="pointer-events-none fixed right-4 bottom-4 z-[100] flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2">
    <TransitionGroup
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="translate-y-2 opacity-0"
      leave-active-class="transition duration-150 ease-in"
      leave-to-class="translate-x-4 opacity-0"
    >
      <div
        v-for="t in toasts"
        :key="t.id"
        class="pointer-events-auto flex items-start gap-2.5 rounded-lg border bg-card p-3 text-sm shadow-lg"
      >
        <component :is="icons[t.kind]" class="mt-0.5 size-4 shrink-0" :class="tones[t.kind]" />
        <span class="flex-1 break-words">{{ t.message }}</span>
        <button
          class="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground"
          @click="dismiss(t.id)"
        >
          <X class="size-3.5" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
