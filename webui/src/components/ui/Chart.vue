<script setup>
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { echarts } from '@/lib/echarts'

const props = defineProps({
  option: { type: Object, required: true },
  height: { type: String, default: '16rem' },
})

const el = ref(null)
const chart = shallowRef(null)
let observer = null

onMounted(() => {
  chart.value = echarts.init(el.value, null, { renderer: 'svg' })
  chart.value.setOption(props.option)
  el.value.__chart = chart.value
  observer = new ResizeObserver(() => chart.value?.resize())
  observer.observe(el.value)
})

watch(
  () => props.option,
  (option) => chart.value?.setOption(option),
)

onBeforeUnmount(() => {
  observer?.disconnect()
  chart.value?.dispose()
  chart.value = null
  if (el.value) delete el.value.__chart
})
</script>

<template>
  <div ref="el" :style="{ height }" />
</template>
