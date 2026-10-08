import { ref } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, MapChart } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
  VisualMapComponent,
} from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'

echarts.use([
  BarChart,
  LineChart,
  MapChart,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  VisualMapComponent,
  SVGRenderer,
])

export { echarts }

const probe = document.createElement('canvas')
probe.width = 1
probe.height = 1
const probeCtx = probe.getContext('2d', { willReadFrequently: true })

export function cssColor(name, fallback = '#000000') {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  if (!raw) return fallback
  probeCtx.clearRect(0, 0, 1, 1)
  probeCtx.fillStyle = raw
  probeCtx.fillRect(0, 0, 1, 1)
  const [r, g, b, a] = probeCtx.getImageData(0, 0, 1, 1).data
  if (a === 0) return fallback
  return `rgb(${r},${g},${b})`
}

export const themeTick = ref(0)

new MutationObserver(() => {
  themeTick.value += 1
}).observe(document.documentElement, { attributeFilter: ['class'] })
