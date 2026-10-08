<script setup>
import { computed, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { api } from '@/lib/api'
import { store } from '@/lib/store'
import { guard, toast } from '@/lib/toast'
import { Badge, Button, Card, Chart, Select, Tip } from '@/components/ui'
import { bytes, datetime, dayLabel, number } from '@/lib/format'
import { cssColor, echarts, themeTick } from '@/lib/echarts'
import { Database, Globe2, MapPin, RefreshCw, ShieldAlert, Undo2 } from 'lucide-vue-next'

const MAP_URL = `${import.meta.env.BASE_URL}world.json`

const RANGES = [
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
  { value: '90d', label: '近 90 天' },
  { value: 'all', label: '全部历史' },
]

const SUB_REGIONS = { CN: '中国大陆', TW: '中国台湾', HK: '中国香港', MO: '中国澳门' }

const PSEUDO_REGIONS = {
  PRIVATE: '内网地址',
  GOOGLE: 'Google 云',
  CLOUDFLARE: 'Cloudflare',
  APPLE: 'Apple 网络',
  FACEBOOK: 'Meta / Facebook',
  A1: '匿名网络',
  A2: '卫星网络',
  EU: '欧洲（多国）',
  AP: '亚太（多国）',
}

const data = ref(null)
const loading = ref(false)
const refreshing = ref(false)
const range = ref('7d')
const picked = ref(null)
const mapState = shallowRef({ zh: {}, bounds: {} })
const mapError = ref('')
const mapReady = computed(() => Object.keys(mapState.value.bounds).length > 0)
const zoom = ref(1)
const center = ref(null)

const countries = computed(() => data.value?.countries || [])
const status = computed(() => data.value?.database || {})
const total = computed(() => data.value?.total || 0)
const unknown = computed(() => data.value?.unknown || 0)
const series = computed(() => data.value?.series || [])

const labelOf = (code) => SUB_REGIONS[code] || mapState.value.zh[code] || PSEUDO_REGIONS[code] || code

const regions = computed(() => {
  const out = new Map()
  for (const bucket of countries.value) {
    const key = SUB_REGIONS[bucket.code] ? 'CN' : bucket.code
    const entry = out.get(key) || { count: 0, parts: [] }
    entry.count += bucket.count
    entry.parts.push(bucket)
    out.set(key, entry)
  }
  return out
})

const onMap = computed(() => [...regions.value.keys()].filter((code) => mapState.value.bounds[code]))
const peak = computed(() => Math.max(...onMap.value.map((c) => regions.value.get(c).count), 0))
const heatLegend = computed(() => `linear-gradient(90deg,${palette().heat.join(',')})`)
const share = (n) => (total.value > 0 ? `${((n / total.value) * 100).toFixed(1)}%` : '—')

const ready = computed(() => !!status.value.ready)
const dbBuilt = computed(() => (status.value.builtAt ? datetime(Date.parse(status.value.builtAt)) : '—'))
const dbUpdated = computed(() =>
  status.value.updatedAt ? datetime(Date.parse(status.value.updatedAt)) : '—',
)

const detail = computed(() => {
  if (!picked.value) return null
  const entry = regions.value.get(picked.value)
  if (!entry) return null
  const ips = []
  const modules = {}
  for (const part of entry.parts) {
    for (const ip of part.ips || []) if (ips.length < 40) ips.push(ip)
    for (const [name, count] of Object.entries(part.modules || {})) {
      modules[name] = (modules[name] || 0) + count
    }
  }
  return {
    code: picked.value,
    label: picked.value === 'CN' ? '中国' : labelOf(picked.value),
    count: entry.count,
    parts: entry.parts,
    ips,
    modules: Object.entries(modules).sort((a, b) => b[1] - a[1]),
    lastTs: Math.max(...entry.parts.map((p) => p.lastTs || 0)),
  }
})

function featureBounds(geometry) {
  let minX = 180
  let minY = 90
  let maxX = -180
  let maxY = -90
  const walk = (node) => {
    if (typeof node[0] === 'number') {
      minX = Math.min(minX, node[0])
      maxX = Math.max(maxX, node[0])
      minY = Math.min(minY, node[1])
      maxY = Math.max(maxY, node[1])
      return
    }
    node.forEach(walk)
  }
  walk(geometry.coordinates)
  return [minX, minY, maxX, maxY]
}

let mapLoading = null
function loadMap() {
  if (!mapLoading) {
    mapLoading = fetch(MAP_URL)
      .then((resp) => {
        if (!resp.ok) throw new Error(`底图加载失败 HTTP ${resp.status}`)
        return resp.json()
      })
      .then((geo) => {
        echarts.registerMap('world', geo)
        const zh = {}
        const bounds = {}
        for (const feature of geo.features) {
          const code = feature.properties.name
          zh[code] = feature.properties.zh
          bounds[code] = featureBounds(feature.geometry)
        }
        mapState.value = { zh, bounds }
      })
      .catch((err) => {
        mapLoading = null
        throw err
      })
  }
  return mapLoading
}

function palette() {
  themeTick.value
  const dark = document.documentElement.classList.contains('dark')
  return {
    dark,
    fg: cssColor('--foreground'),
    muted: cssColor('--muted-foreground'),
    border: cssColor('--border'),
    popover: cssColor('--popover'),
    empty: cssColor('--muted'),
    heat: dark
      ? ['#78350f', '#b45309', '#ea580c', '#ef4444', '#f87171']
      : ['#fde68a', '#fdba74', '#fb923c', '#ef4444', '#b91c1c'],
  }
}

const FONT = 'ui-sans-serif, system-ui, "PingFang SC", "Microsoft YaHei", sans-serif'

function tooltipBox(c, title, rows, note) {
  const line = (color, name, value) =>
    '<div style="display:flex;align-items:center;gap:6px;line-height:1.7">' +
    (color ? `<span style="width:8px;height:8px;border-radius:9999px;background:${color}"></span>` : '') +
    `<span style="color:${c.muted}">${name}</span>` +
    `<span style="margin-left:auto;font-weight:600;font-variant-numeric:tabular-nums">${value}</span>` +
    '</div>'
  return (
    '<div style="min-width:11rem">' +
    `<div style="font-weight:600;margin-bottom:3px">${title}</div>` +
    rows.map(([n, v]) => line('', n, v)).join('') +
    (note ? `<div style="color:${c.muted};margin-top:4px">${note}</div>` : '') +
    '</div>'
  )
}

const mapOption = computed(() => {
  const c = palette()
  const items = onMap.value.map((code) => ({ name: code, value: regions.value.get(code).count }))
  return {
    textStyle: { fontFamily: FONT },
    animationDuration: 400,
    animationEasing: 'cubicOut',
    tooltip: {
      trigger: 'item',
      backgroundColor: c.popover,
      borderColor: c.border,
      borderWidth: 1,
      padding: [8, 12],
      textStyle: { color: c.fg, fontSize: 12, fontFamily: FONT },
      extraCssText: 'border-radius:var(--radius);box-shadow:0 8px 24px rgb(0 0 0 / .10)',
      className: 'geo-tip',
      formatter: (params) => {
        const code = params.name
        const title = code === 'CN' ? '中国' : labelOf(code)
        const entry = regions.value.get(code)
        if (!entry) return tooltipBox(c, title, [['封禁次数', '0']], '本区间无封禁记录')
        const rows = [['封禁次数', number(entry.count)], ['占全部封禁', share(entry.count)]]
        if (entry.parts.length > 1) {
          for (const part of [...entry.parts].sort((a, b) => b.count - a.count)) {
            rows.push([`· ${SUB_REGIONS[part.code]}`, number(part.count)])
          }
        }
        const last = Math.max(...entry.parts.map((p) => p.lastTs || 0))
        return tooltipBox(c, title, rows, `最近一次 ${datetime(last * 1000)}`)
      },
    },
    visualMap: {
      type: 'continuous',
      min: 0,
      max: Math.max(1, peak.value),
      left: 8,
      bottom: 8,
      itemWidth: 12,
      itemHeight: 110,
      calculable: true,
      text: ['多', '少'],
      textStyle: { color: c.muted, fontSize: 11, fontFamily: FONT },
      inRange: { color: c.heat },
    },
    series: [
      {
        type: 'map',
        map: 'world',
        roam: false,
        zoom: zoom.value,
        center: center.value || undefined,
        selectedMode: false,
        itemStyle: { areaColor: c.empty, borderColor: c.border, borderWidth: 0.6 },
        emphasis: {
          label: { show: true, color: c.fg, fontSize: 11, fontFamily: FONT },
          itemStyle: { areaColor: c.heat[c.heat.length - 1], borderColor: c.fg, borderWidth: 1 },
        },
        select: { disabled: true },
        data: items,
      },
    ],
  }
})

function pickRegion(code) {
  if (!mapState.value.bounds[code]) return
  picked.value = code
  const [minX, minY, maxX, maxY] = mapState.value.bounds[code]
  const span = Math.max(maxX - minX, maxY - minY)
  center.value = [(minX + maxX) / 2, (minY + maxY) / 2]
  zoom.value = Math.max(1, Math.min(12, (360 / Math.max(span, 6)) * 0.55))
}

function resetView() {
  picked.value = null
  zoom.value = 1
  center.value = null
}

const trendOption = computed(() => {
  const c = palette()
  const days = series.value
  return {
    textStyle: { fontFamily: FONT },
    animationDuration: 400,
    animationEasing: 'cubicOut',
    grid: { left: 4, right: 8, top: 16, bottom: 2, containLabel: true },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow', shadowStyle: { color: c.heat[0], opacity: 0.25 } },
      backgroundColor: c.popover,
      borderColor: c.border,
      borderWidth: 1,
      padding: [8, 12],
      textStyle: { color: c.fg, fontSize: 12, fontFamily: FONT },
      extraCssText: 'border-radius:var(--radius);box-shadow:0 8px 24px rgb(0 0 0 / .10)',
      className: 'geo-trend-tip',
      formatter: (params) => {
        const point = days[params[0].dataIndex]
        return tooltipBox(c, dayLabel(point.ts * 1000), [['封禁次数', number(point.count)]])
      },
    },
    xAxis: {
      type: 'category',
      data: days.map((p) => dayLabel(p.ts * 1000)),
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: c.muted, fontSize: 11, margin: 10, hideOverlap: true },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      axisLine: { show: false },
      axisTick: { show: false },
      splitLine: { lineStyle: { color: c.border, type: [4, 4] } },
      axisLabel: { color: c.muted, fontSize: 11, margin: 10 },
    },
    series: [
      {
        type: 'bar',
        barMaxWidth: 18,
        data: days.map((p) => p.count),
        itemStyle: { color: c.heat[3], borderRadius: [4, 4, 0, 0] },
        emphasis: { itemStyle: { color: c.heat[4] } },
      },
    ],
  }
})

async function load() {
  loading.value = true
  await guard(async () => {
    data.value = await api.get(`/api/geo?range=${range.value}`)
  })
  loading.value = false
}

async function refreshDatabase() {
  refreshing.value = true
  const ok = await guard(() => api.post('/api/geo/refresh'), '已开始更新地理库')
  refreshing.value = false
  if (ok) setTimeout(load, 1500)
}

let mapTimer = null
onMounted(async () => {
  await guard(loadMap).catch((err) => {
    mapError.value = err?.message || '底图加载失败'
  })
  await load()
})
onUnmounted(() => clearTimeout(mapTimer))

watch(
  () => store.lastBan,
  (v) => {
    if (!v) return
    clearTimeout(mapTimer)
    mapTimer = setTimeout(load, 4000)
  },
)
watch(() => store.geo, (v) => {
  if (v && data.value) data.value.database = v
})
watch(range, load)
watch(() => store.reloaded, (v) => v && load())
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center gap-2">
      <p class="flex-1 text-sm text-muted-foreground">
        被封禁的 IP 按国家/地区聚合。颜色越深代表本区间内封禁次数越多。
      </p>
      <Select v-model="range" :options="RANGES" class="h-8 w-36" />
      <Button variant="ghost" size="icon-sm" :loading="loading" title="刷新" @click="load">
        <RefreshCw />
      </Button>
    </div>

    <div data-slot="geo-cards" class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Card class="flex items-center gap-3">
        <div class="flex size-9 items-center justify-center rounded-lg bg-muted">
          <ShieldAlert class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span>区间封禁</span>
            <Tip text="当前时间范围内新产生的封禁次数。同一条封禁记录不会重复计数，解除后再被封会算新的一次。" />
          </div>
          <div class="mono truncate text-lg font-semibold">{{ number(total) }}</div>
          <div class="mono truncate text-xs text-muted-foreground">
            历史累计 {{ number(data?.logged) }} 条
          </div>
        </div>
      </Card>
      <Card class="flex items-center gap-3">
        <div class="flex size-9 items-center justify-center rounded-lg bg-muted">
          <Globe2 class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span>来源地区</span>
            <Tip text="本区间内出现过封禁的国家/地区个数（不含内网与云厂商等非国家取值）。" />
          </div>
          <div class="mono truncate text-lg font-semibold">{{ number(countries.length) }}</div>
          <div class="mono truncate text-xs text-muted-foreground">
            最多 {{ number(peak) }} 次
          </div>
        </div>
      </Card>
      <Card class="flex items-center gap-3">
        <div class="flex size-9 items-center justify-center rounded-lg bg-muted">
          <MapPin class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span>未识别</span>
            <Tip text="地理库里查不到归属的地址，多为内网、保留地址或库里尚未收录的段。这些地址不会出现在地图上。" />
          </div>
          <div class="mono truncate text-lg font-semibold">{{ number(unknown) }}</div>
          <div class="mono truncate text-xs text-muted-foreground">占比 {{ share(unknown) }}</div>
        </div>
      </Card>
      <Card class="flex items-center gap-3">
        <div class="flex size-9 items-center justify-center rounded-lg bg-muted">
          <Database class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span>地理库</span>
            <Tip text="本机缓存的国家库，启动时直接读缓存、不联网；只有缓存缺失或损坏时才自动下载。库的版本时间由数据源决定，不是下载时间。" />
          </div>
          <div class="mono truncate text-lg font-semibold">
            {{ ready ? status.database || '已就绪' : '未就绪' }}
          </div>
          <div class="mono truncate text-xs text-muted-foreground">
            {{ ready ? `${dbBuilt} · ${bytes(status.size)}` : status.enabled ? '等待下载' : '已关闭' }}
          </div>
        </div>
      </Card>
    </div>

    <Card data-slot="geo-map">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-semibold">封禁来源分布</h2>
        <Tip text="悬停任意国家/地区查看封禁次数与最近一次时间；点击有数据的国家/地区可放大到该区域，下方同时列出被封的地址明细。地图底图为中国标准国界，含南海诸岛。" />
        <span v-if="picked" class="mono text-xs text-muted-foreground">
          已聚焦 {{ detail?.label }}
        </span>
        <div class="flex-1" />
        <Button v-if="picked" variant="ghost" size="sm" @click="resetView">
          <Undo2 /> 返回全球
        </Button>
      </div>

      <div v-if="mapError" class="py-12 text-center text-sm text-destructive">{{ mapError }}</div>
      <div v-else-if="!mapReady" class="py-12 text-center text-sm text-muted-foreground">
        正在加载地图底图…
      </div>
      <template v-else>
        <Chart
          data-slot="geo-chart"
          :option="mapOption"
          height="27rem"
          class="mt-2"
          @click="(params) => pickRegion(params.name)"
        />
        <div class="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
          <span class="inline-flex items-center gap-1.5">
            <span class="size-2.5 rounded-sm" :style="{ background: 'var(--muted)' }" />
            无封禁记录
          </span>
          <span class="inline-flex items-center gap-1.5">
            <span
              data-slot="geo-heat-legend"
              class="size-2.5 rounded-sm"
              :style="{ background: heatLegend }"
            />
            封禁次数由少到多
          </span>
          <span>颜色按本区间内的最大值归一，换时间范围后深浅会变。</span>
        </div>

        <div
          v-if="detail"
          data-slot="geo-detail"
          class="mt-4 rounded-lg border bg-muted/30 p-3"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm font-semibold">{{ detail.label }}</span>
            <Badge variant="destructive">{{ number(detail.count) }} 次</Badge>
            <span class="mono text-xs text-muted-foreground">占全部 {{ share(detail.count) }}</span>
            <span class="mono text-xs text-muted-foreground">
              最近一次 {{ datetime(detail.lastTs * 1000) }}
            </span>
          </div>

          <div v-if="detail.parts.length > 1" class="mt-2 flex flex-wrap gap-2">
            <span
              v-for="part in [...detail.parts].sort((a, b) => b.count - a.count)"
              :key="part.code"
              class="mono rounded-md border bg-background px-2 py-1 text-xs"
            >
              {{ SUB_REGIONS[part.code] }} {{ number(part.count) }}
            </span>
          </div>

          <div v-if="detail.modules.length" class="mt-2 flex flex-wrap items-center gap-2 text-xs">
            <span class="text-muted-foreground">触发模块</span>
            <span
              v-for="[name, count] in detail.modules"
              :key="name"
              class="mono rounded-md border bg-background px-2 py-1"
            >
              {{ name }} · {{ number(count) }}
            </span>
          </div>

          <div class="mt-3">
            <div class="mb-1.5 text-xs text-muted-foreground">
              被封地址（最多显示 40 条）
            </div>
            <div class="flex flex-wrap gap-1.5">
              <span
                v-for="ip in detail.ips"
                :key="ip"
                class="mono rounded-md border bg-background px-2 py-0.5 text-xs"
              >
                {{ ip }}
              </span>
              <span v-if="!detail.ips.length" class="text-xs text-muted-foreground">
                没有可展示的明细
              </span>
            </div>
          </div>
        </div>
      </template>
    </Card>

    <div class="grid gap-4 lg:grid-cols-2">
      <Card data-slot="geo-ranking" padded="false" class="overflow-hidden">
        <div class="flex items-center gap-1 border-b px-4 py-3">
          <span class="text-sm font-semibold">国家 / 地区排行</span>
          <Tip text="按本区间内封禁次数排序。内网地址与云厂商这类非国家取值也列在这里，但它们不会出现在地图上。点任意一行可在地图上定位。" />
          <div class="flex-1" />
          <span class="mono text-xs text-muted-foreground">{{ countries.length }} 项</span>
        </div>
        <div class="max-h-[22rem] overflow-auto">
          <table class="w-full text-sm">
            <thead
              class="sticky top-0 border-b bg-muted/80 text-xs text-muted-foreground backdrop-blur"
            >
              <tr>
                <th class="px-4 py-2.5 text-left font-medium">地区</th>
                <th class="px-4 py-2.5 text-right font-medium">次数</th>
                <th class="px-4 py-2.5 text-left font-medium">占比</th>
                <th class="px-4 py-2.5 text-right font-medium">最近</th>
              </tr>
            </thead>
            <tbody class="divide-y">
              <tr v-if="!countries.length">
                <td colspan="4" class="px-4 py-10 text-center text-muted-foreground">
                  本区间内没有封禁记录
                </td>
              </tr>
              <tr
                v-for="row in countries"
                :key="row.code"
                :data-code="row.code"
                class="cursor-pointer hover:bg-muted/30"
                @click="pickRegion(SUB_REGIONS[row.code] ? 'CN' : row.code)"
              >
                <td class="px-4 py-2.5">
                  <span>{{ labelOf(row.code) }}</span>
                  <span class="mono ml-1.5 text-xs text-muted-foreground">{{ row.code }}</span>
                </td>
                <td class="mono px-4 py-2.5 text-right font-semibold">{{ number(row.count) }}</td>
                <td class="px-4 py-2.5">
                  <div class="flex items-center gap-2">
                    <div class="h-1.5 w-16 overflow-hidden rounded-full bg-muted">
                      <div
                        class="h-full rounded-full bg-destructive"
                        :style="{ width: `${peak > 0 ? (row.count / peak) * 100 : 0}%` }"
                      />
                    </div>
                    <span class="mono text-xs text-muted-foreground">{{ share(row.count) }}</span>
                  </div>
                </td>
                <td class="mono px-4 py-2.5 text-right text-xs whitespace-nowrap">
                  {{ row.lastTs ? datetime(row.lastTs * 1000) : '—' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </Card>

      <Card data-slot="geo-trend">
        <div class="flex flex-wrap items-center gap-1">
          <h2 class="text-sm font-semibold">封禁趋势</h2>
          <Tip text="按天统计本区间内新产生的封禁次数。只保留最近 90 天的封禁流水，更早的记录会被自动裁掉。" />
          <div class="flex-1" />
          <span class="mono text-xs text-muted-foreground">{{ series.length }} 天</span>
        </div>
        <div v-if="!series.length" class="py-16 text-center text-sm text-muted-foreground">
          本区间内没有封禁记录
        </div>
        <Chart v-else :option="trendOption" height="17rem" class="mt-3" />
      </Card>
    </div>

    <Card data-slot="geo-source">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-semibold">地理库</h2>
        <Tip text="国家库由用户配置的地址在运行时下载，不随程序分发。启动时只读本地缓存，不联网；缓存缺失或损坏才会自动拉取，之后按更新间隔定时检查。" />
        <div class="flex-1" />
        <Button
          variant="outline"
          size="sm"
          :loading="refreshing"
          :disabled="!status.enabled"
          @click="refreshDatabase"
        >
          <RefreshCw /> 立即更新
        </Button>
      </div>

      <dl class="mt-3 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
        <div class="flex gap-3">
          <dt class="w-24 shrink-0 text-xs text-muted-foreground">库名称</dt>
          <dd class="mono break-all">{{ status.database || '—' }}</dd>
        </div>
        <div class="flex gap-3">
          <dt class="w-24 shrink-0 text-xs text-muted-foreground">库版本时间</dt>
          <dd class="mono">{{ dbBuilt }}</dd>
        </div>
        <div class="flex gap-3">
          <dt class="w-24 shrink-0 text-xs text-muted-foreground">本地大小</dt>
          <dd class="mono">{{ status.size ? bytes(status.size) : '—' }}</dd>
        </div>
        <div class="flex gap-3">
          <dt class="w-24 shrink-0 text-xs text-muted-foreground">最近下载</dt>
          <dd class="mono">{{ dbUpdated }}</dd>
        </div>
        <div class="flex gap-3 sm:col-span-2">
          <dt class="w-24 shrink-0 text-xs text-muted-foreground">下载地址</dt>
          <dd class="mono break-all">{{ status.url || '—' }}</dd>
        </div>
        <div class="flex gap-3">
          <dt class="w-24 shrink-0 text-xs text-muted-foreground">封禁流水保留</dt>
          <dd class="mono">{{ data?.historyKeep || '—' }}</dd>
        </div>
        <div class="flex gap-3">
          <dt class="w-24 shrink-0 text-xs text-muted-foreground">更新状态</dt>
          <dd class="mono">
            <Badge v-if="status.running" variant="secondary">正在更新</Badge>
            <Badge v-else-if="status.lastError" variant="destructive">上次失败</Badge>
            <Badge v-else-if="status.enabled" variant="success">正常</Badge>
            <Badge v-else variant="muted">已关闭</Badge>
          </dd>
        </div>
      </dl>

      <p v-if="status.lastError" class="mt-3 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
        {{ status.lastError }}
      </p>
      <p class="mt-3 text-xs text-muted-foreground">
        更换数据源只需改设置里的「国家库地址」，改完保存即会热重载并按新地址重新拉取；地址必须是 MaxMind mmdb 格式（如
        country-lite.mmdb、GeoLite2-Country.mmdb）。
      </p>
    </Card>
  </div>
</template>
