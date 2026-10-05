<script setup>
import { cn } from '@/lib/utils'
import { TabsRoot, TabsList, TabsTrigger, TabsContent } from 'reka-ui'

const props = defineProps({
  tabs: { type: Array, default: () => [] },
  class: { type: null, default: '' },
})
const model = defineModel({ type: String, default: '' })
</script>

<template>
  <TabsRoot v-model="model" :class="cn('flex flex-col gap-4', props.class)">
    <TabsList class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
      <TabsTrigger
        v-for="tab in tabs"
        :key="tab.value"
        :value="tab.value"
        class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50 data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm"
      >
        {{ tab.label }}
      </TabsTrigger>
    </TabsList>
    <TabsContent v-for="tab in tabs" :key="tab.value" :value="tab.value" class="outline-none">
      <slot :name="tab.value" />
    </TabsContent>
  </TabsRoot>
</template>
