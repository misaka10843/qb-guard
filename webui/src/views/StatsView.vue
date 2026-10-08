<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/lib/api'
import { store } from '@/lib/store'
import { guard } from '@/lib/toast'
import { Badge, Button, Card, Chart, Dialog, Select, Tip } from '@/components/ui'
import { bytes, speed, number, dayLabel, dateLabel, stamp, hms, datetime } from '@/lib/format'
import { cssColor, themeTick } from '@/lib/echarts'
import { Download, Globe, RefreshCw, Upload, Users } from 'lucide-vue-next'

const data = ref(null)
const torrents = ref([])
const torrentsAt = ref(0)
const loading = ref(false)
const range = ref('48h')
const detail = ref(null)

const toMs = (ts) => ts * 1000

const RANGES = [
  { value: '48h', label: '近 48 小时', slot: 'recent', span: 48 * 3600 },
  { value: '7d', label: '近 7 天', slot: 'hourly', span: 7 * 86400 },
  { value: '30d', label: '近 30 天', slot: 'hourly', span: 30 * 86400 },
  { value: '90d', label: '近 90 天', slot: 'daily', span: 90 * 86400 },
  { value: '365d', label: '近一年', slot: 'daily', span: 365 * 86400 },
  { value: 'all', label: '全部', slot: 'daily', span: null },
]

const current = computed(() => RANGES.find((r) => r.value === range.value) || RANGES[0])
const global = computed(() => data.value?.global || {})
const sites = computed(() =>
  [...(data.value?.sites || [])].sort((a, b) => b.uploaded - a.uploaded),
)
const days = computed(() => data.value?.trend?.days || [])

const points = computed(() => {
  const all = data.value?.trend?.[current.value.slot] || []
  const span = current.value.span
  if (!span || all.length < 2) return all
  const cutoff = all[all.length - 1].ts - span
  const kept = all.filter((p) => p.ts >= cutoff)
  return kept.length >= 2 ? kept : all.slice(-2)
})

function axisScale(max) {
  const [unit, suffix] =
    max >= 1024 ** 3
      ? [1024 ** 3, 'GiB']
      : max >= 1024 ** 2
        ? [1024 ** 2, 'MiB']
        : max >= 1024
          ? [1024, 'KiB']
          : [1, 'B']
  const formatter = (v) => {
    if (v === 0) return '0'
    const n = v / unit
    return `${n.toFixed(Number.isInteger(n) ? 0 : 1)} ${suffix}`
  }
  if (!(max > 0)) return { formatter }

  const raw = max / unit / 4
  const mag = 10 ** Math.floor(Math.log10(raw))
  let step = [1, 2, 2.5, 5, 10].find((m) => m * mag >= raw) * mag
  if (unit === 1) step = Math.max(1, step)
  return { max: step * 4 * unit, interval: step * unit, formatter }
}

const spanMs = computed(() => {
  const pts = points.value
  return pts.length < 2 ? 0 : (pts[pts.length - 1].ts - pts[0].ts) * 1000
})

const tickFor = (span) => (span < 10 * 60 * 1000 ? hms : span < 48 * 3600 * 1000 ? stamp : dayLabel)

const tipFor = (span) => (span < 48 * 3600 * 1000 ? stamp : dayLabel)

function palette() {
  themeTick.value
  return {
    fg: cssColor('--foreground'),
    muted: cssColor('--muted-foreground'),
    border: cssColor('--border'),
    popover: cssColor('--popover'),
    up: cssColor('--chart-1'),
    down: cssColor('--chart-2'),
    seed: cssColor('--chart-3'),
    site: cssColor('--chart-5'),
  }
}

const FONT = 'ui-sans-serif, system-ui, "PingFang SC", "Microsoft YaHei", sans-serif'

function alpha(rgb, a) {
  const [r, g, b] = rgb.match(/\d+/g).map(Number)
  return `rgba(${r},${g},${b},${a})`
}

function fade(color) {
  return {
    type: 'linear',
    x: 0,
    y: 0,
    x2: 0,
    y2: 1,
    colorStops: [
      { offset: 0, color: alpha(color, 0.28) },
      { offset: 1, color: alpha(color, 0) },
    ],
  }
}

function valueAxis(c, max) {
  const scale = axisScale(max)
  return {
    type: 'value',
    min: 0,
    max: scale.max,
    interval: scale.interval,
    axisLine: { show: false },
    axisTick: { show: false },
    splitLine: { lineStyle: { color: c.border, type: [4, 4] } },
    axisLabel: { color: c.muted, fontSize: 11, margin: 10, formatter: scale.formatter },
  }
}

function categoryAxis(c, data, formatter) {
  return {
    type: 'category',
    data,
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: { color: c.muted, fontSize: 11, margin: 10, hideOverlap: true, formatter },
  }
}

function legendStyle(c) {
  return {
    top: 0,
    right: 0,
    icon: 'circle',
    itemWidth: 8,
    itemHeight: 8,
    itemGap: 16,
    textStyle: { color: c.muted, fontSize: 12, fontFamily: FONT },
  }
}

function tooltipStyle(c, cls) {
  return {
    className: cls,
    backgroundColor: c.popover,
    borderColor: c.border,
    borderWidth: 1,
    padding: [8, 12],
    textStyle: { color: c.fg, fontSize: 12, fontFamily: FONT },
    extraCssText: 'border-radius:var(--radius);box-shadow:0 8px 24px rgb(0 0 0 / .10)',
  }
}

function tooltipRows(c, title, rows) {
  const line = ([color, name, value]) =>
    '<div style="display:flex;align-items:center;gap:6px;line-height:1.7">' +
    `<span style="width:8px;height:8px;border-radius:9999px;background:${color}"></span>` +
    `<span style="color:${c.muted}">${name}</span>` +
    `<span style="margin-left:auto;font-weight:600;font-variant-numeric:tabular-nums">${value}</span>` +
    '</div>'
  return (
    '<div style="min-width:9.5rem">' +
    `<div style="color:${c.muted};margin-bottom:3px">${title}</div>` +
    rows.map(line).join('') +
    '</div>'
  )
}

const seriesMax = (pts) =>
  Math.max(...pts.map((p) => Math.max(p.uploaded, p.downloaded)), 0)

function sparkOption(key, color) {
  const pts = points.value
  return {
    animation: false,
    grid: { left: 0, right: 0, top: 3, bottom: 0 },
    xAxis: { type: 'category', show: false, boundaryGap: false, data: pts.map((p) => p.ts) },
    yAxis: { type: 'value', show: false, min: 'dataMin', max: 'dataMax' },
    series: [
      {
        type: 'line',
        data: pts.map((p) => p[key] ?? 0),
        smooth: 0.3,
        showSymbol: false,
        silent: true,
        lineStyle: { width: 1.5, color },
        areaStyle: { color: fade(color) },
      },
    ],
  }
}

const sparkUpload = computed(() => sparkOption('uploaded', palette().up))
const sparkDownload = computed(() => sparkOption('downloaded', palette().down))
const sparkSeeding = computed(() => sparkOption('seeding', palette().seed))
const sparkSites = computed(() => sparkOption('sites', palette().site))

const trendOption = computed(() => {
  const pts = points.value
  const c = palette()
  const label = tickFor(spanMs.value)
  const tip = tipFor(spanMs.value)
  const line = (name, key, color) => ({
    name,
    type: 'line',
    smooth: 0.25,
    showSymbol: false,
    symbol: 'circle',
    symbolSize: 7,
    data: pts.map((p) => p[key]),
    lineStyle: { width: 2, color },
    itemStyle: { color, borderColor: c.popover, borderWidth: 2 },
    areaStyle: { color: fade(color) },
    emphasis: { focus: 'series' },
  })
  return {
    textStyle: { fontFamily: FONT },
    animationDuration: 400,
    animationEasing: 'cubicOut',
    grid: { left: 4, right: 12, top: 34, bottom: 2, containLabel: true },
    legend: legendStyle(c),
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'line', lineStyle: { color: c.muted, type: 'dashed' } },
      ...tooltipStyle(c, 'trend-tip'),
      formatter: (params) => {
        const point = pts[params[0].dataIndex]
        return tooltipRows(c, tip(toMs(point.ts)), [
          [c.up, '上传', bytes(point.uploaded)],
          [c.down, '下载', bytes(point.downloaded)],
        ])
      },
    },
    xAxis: categoryAxis(c, pts.map((p) => label(toMs(p.ts)))),
    yAxis: valueAxis(c, seriesMax(pts)),
    series: [line('上传', 'uploaded', c.up), line('下载', 'downloaded', c.down)],
  }
})

const barDays = computed(() => days.value.slice(-30))

const barsOption = computed(() => {
  const pts = barDays.value
  const c = palette()
  const bar = (name, key, color) => ({
    name,
    type: 'bar',
    barMaxWidth: 14,
    barGap: '25%',
    data: pts.map((p) => p[key]),
    itemStyle: { color: alpha(color, 0.85), borderRadius: [4, 4, 0, 0] },
    emphasis: { itemStyle: { color } },
  })
  return {
    textStyle: { fontFamily: FONT },
    animationDuration: 400,
    animationEasing: 'cubicOut',
    grid: { left: 4, right: 12, top: 34, bottom: 2, containLabel: true },
    legend: legendStyle(c),
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow', shadowStyle: { color: alpha(c.fg, 0.05) } },
      ...tooltipStyle(c, 'bars-tip'),
      formatter: (params) => {
        const point = pts[params[0].dataIndex]
        return tooltipRows(c, dayLabel(toMs(point.ts)), [
          [c.up, '上传', bytes(point.uploaded)],
          [c.down, '下载', bytes(point.downloaded)],
        ])
      },
    },
    xAxis: categoryAxis(c, pts.map((p) => dayLabel(toMs(p.ts)))),
    yAxis: valueAxis(c, seriesMax(pts)),
    series: [bar('上传', 'uploaded', c.up), bar('下载', 'downloaded', c.down)],
  }
})

const barMeta = computed(() => {
  const pts = barDays.value
  if (!pts.length) return null
  return {
    count: pts.length,
    first: pts[0].ts,
    last: pts[pts.length - 1].ts,
    uploaded: pts.reduce((sum, p) => sum + p.uploaded, 0),
    downloaded: pts.reduce((sum, p) => sum + p.downloaded, 0),
    peak: Math.max(...pts.map((p) => Math.max(p.uploaded, p.downloaded)), 0),
  }
})

const totalRatio = computed(() =>
  global.value.downloaded > 0 ? global.value.uploaded / global.value.downloaded : null,
)

const topSites = computed(() => sites.value.slice(0, 10))

const topSitesOption = computed(() => {
  const c = palette()
  const rows = [...topSites.value].reverse()
  return {
    textStyle: { fontFamily: FONT },
    animationDuration: 400,
    animationEasing: 'cubicOut',
    grid: { left: 4, right: 64, top: 6, bottom: 2, containLabel: true },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow', shadowStyle: { color: alpha(c.fg, 0.05) } },
      ...tooltipStyle(c, 'sites-tip'),
      formatter: (params) => {
        const row = rows[params[0].dataIndex]
        return tooltipRows(c, row.site, [
          [c.up, '上传', bytes(row.uploaded)],
          [c.down, '下载', bytes(row.downloaded)],
          [c.seed, '做种', `${number(row.seeding)} 个 · ${bytes(row.seeding_bytes)}`],
        ])
      },
    },
    xAxis: { type: 'value', show: false },
    yAxis: {
      type: 'category',
      data: rows.map((r) => r.site),
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: c.muted, fontSize: 11, width: 96, overflow: 'truncate' },
    },
    series: [
      {
        type: 'bar',
        data: rows.map((r) => r.uploaded),
        barMaxWidth: 13,
        itemStyle: { color: alpha(c.up, 0.85), borderRadius: [0, 4, 4, 0] },
        emphasis: { itemStyle: { color: c.up } },
        label: {
          show: true,
          position: 'right',
          color: c.muted,
          fontSize: 11,
          fontFamily: FONT,
          formatter: (p) => bytes(p.value),
        },
      },
    ],
  }
})

const STATES = {
  error: ['出错', 'destructive'],
  missingFiles: ['文件丢失', 'destructive'],
  uploading: ['做种中', 'success'],
  forcedUP: ['强制做种', 'success'],
  queuedUP: ['排队做种', 'secondary'],
  stalledUP: ['做种等待', 'secondary'],
  checkingUP: ['校验中', 'secondary'],
  pausedUP: ['已暂停', 'secondary'],
  downloading: ['下载中', 'default'],
  forcedDL: ['强制下载', 'default'],
  metaDL: ['获取元数据', 'secondary'],
  queuedDL: ['排队下载', 'secondary'],
  stalledDL: ['下载等待', 'secondary'],
  checkingDL: ['校验中', 'secondary'],
  pausedDL: ['已暂停', 'secondary'],
  allocating: ['分配空间', 'secondary'],
  checkingResumeData: ['校验续传', 'secondary'],
  moving: ['移动中', 'secondary'],
}

const stateLabel = (state) => STATES[state]?.[0] || state || '未知'
const stateVariant = (state) => STATES[state]?.[1] || 'secondary'

const ratioOf = (t) => (t.downloaded > 0 ? (t.uploaded / t.downloaded).toFixed(2) : '∞')

const torrentRows = computed(() =>
  [...torrents.value].sort((a, b) => b.uploaded - a.uploaded),
)

function etaLabel(secs) {
  if (!secs || secs >= 8640000) return '—'
  const d = Math.floor(secs / 86400)
  const h = Math.floor((secs % 86400) / 3600)
  const m = Math.floor((secs % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分`
  return `${m} 分 ${secs % 60} 秒`
}

function spanLabel(secs) {
  if (!secs) return '—'
  const d = Math.floor(secs / 86400)
  const h = Math.floor((secs % 86400) / 3600)
  const m = Math.floor((secs % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分`
  return `${m} 分`
}

const detailOpen = computed({
  get: () => !!detail.value,
  set: (open) => {
    if (!open) detail.value = null
  },
})

async function load() {
  loading.value = true
  await guard(async () => {
    const [stats, list] = await Promise.all([api.get('/api/stats'), api.get('/api/torrents')])
    data.value = stats
    torrents.value = list.torrents || []
    torrentsAt.value = list.now || 0
  })
  loading.value = false
}

onMounted(load)
watch(() => store.stats, (v) => v && (data.value = v))
watch(() => store.reloaded, (v) => v && load())
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center gap-2">
      <p class="flex-1 text-sm text-muted-foreground">
        按 tracker 域名归并站点。趋势点随采样间隔累积，进程重启不会丢历史。
      </p>
      <Select v-model="range" :options="RANGES" class="h-8 w-40" />
      <Button variant="ghost" size="icon-sm" :loading="loading" title="刷新" @click="load">
        <RefreshCw />
      </Button>
    </div>

    <div data-slot="stat-cards" class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Card class="flex items-start gap-3">
        <div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
          <Upload class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span>累计上传</span>
            <Tip text="下载器开机以来的总上传量（alltime_ul），不是本服务启动以来的。下面一行是当前实时上传速率。" />
          </div>
          <div class="mono truncate text-lg font-semibold">{{ bytes(global.uploaded) }}</div>
          <div class="mono truncate text-xs text-muted-foreground">{{ speed(global.upspeed) }}</div>
          <Chart data-slot="spark-upload" :option="sparkUpload" height="1.75rem" class="mt-1" />
        </div>
      </Card>
      <Card class="flex items-start gap-3">
        <div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
          <Download class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span>累计下载</span>
            <Tip text="下载器开机以来的总下载量（alltime_dl）。下面一行是当前实时下载速率。" />
          </div>
          <div class="mono truncate text-lg font-semibold">{{ bytes(global.downloaded) }}</div>
          <div class="mono truncate text-xs text-muted-foreground">{{ speed(global.dlspeed) }}</div>
          <Chart data-slot="spark-download" :option="sparkDownload" height="1.75rem" class="mt-1" />
        </div>
      </Card>
      <Card class="flex items-start gap-3">
        <div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
          <Users class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span>做种 / 全部</span>
            <Tip text="左边是正在做种的任务数，右边是下载器里所有任务的总数（含暂停与已完成）。下面一行是做种任务的体积之和。" />
          </div>
          <div class="mono truncate text-lg font-semibold">
            {{ number(global.seeding) }} / {{ number(global.total) }}
          </div>
          <div class="mono truncate text-xs text-muted-foreground">
            做种体积 {{ bytes(global.seeding_bytes) }}
          </div>
          <Chart data-slot="spark-seeding" :option="sparkSeeding" height="1.75rem" class="mt-1" />
        </div>
      </Card>
      <Card class="flex items-start gap-3">
        <div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
          <Globe class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span>站点数</span>
            <Tip text="从任务的 tracker 地址里归并出的站点个数（tracker.mikanani.me 记为 mikanani.me）。下面一行是总分享率。" />
          </div>
          <div class="mono truncate text-lg font-semibold">{{ number(global.sites) }}</div>
          <div class="mono truncate text-xs text-muted-foreground">
            总分享率 {{ totalRatio === null ? '∞' : totalRatio.toFixed(2) }}
          </div>
          <Chart data-slot="spark-sites" :option="sparkSites" height="1.75rem" class="mt-1" />
        </div>
      </Card>
    </div>

    <Card>
      <div class="flex flex-wrap items-center gap-2">
        <div class="flex items-center gap-1">
          <h2 class="text-sm font-semibold">流量趋势</h2>
          <Tip text="每个点是这段时间内的上传/下载增量，按时间排列。纵轴按本区间内的峰值自动缩放，所以换范围时形状会变。鼠标移到图上可以读具体某一点。上面的四张指标卡与这张图共用同一个时间范围。" />
        </div>
        <span class="mono text-xs text-muted-foreground">{{ points.length }} 个采样点</span>
      </div>

      <div v-if="points.length < 2" class="py-10 text-center text-sm text-muted-foreground">
        采样点不足，稍后再看
      </div>
      <Chart
        v-else
        data-slot="trend"
        :data-span="spanMs"
        :option="trendOption"
        height="15rem"
        class="mt-3"
      />
    </Card>

    <div class="grid gap-4 lg:grid-cols-2">
      <Card data-slot="daily-flow">
        <div class="flex flex-wrap items-center gap-1">
          <h2 class="text-sm font-semibold">每日流量</h2>
          <Tip text="按天汇总的上传下载量。每一天只取能覆盖到它的最细一档：最近 48 小时用采样点、再往前用小时点、更早用日点，所以进程刚起也能看到当天的量，不必等到跨过第一个自然日界。" />
          <div class="flex-1" />
          <span class="mono text-xs text-muted-foreground">
            近 {{ barMeta?.count || 0 }} 天 · ↑{{ bytes(barMeta?.uploaded) }} ↓{{
              bytes(barMeta?.downloaded)
            }}
          </span>
        </div>
        <div v-if="!barMeta" class="py-16 text-center text-sm text-muted-foreground">
          还没有采样点，稍后再看
        </div>
        <template v-else>
          <Chart data-slot="daily-bars" :option="barsOption" height="13rem" class="mt-3" />
          <div class="mt-1 flex items-center justify-between text-xs text-muted-foreground">
            <span class="mono">{{ dayLabel(toMs(barMeta.first)) }}</span>
            <span class="mono">峰值 {{ bytes(barMeta.peak) }}</span>
            <span class="mono">{{ dayLabel(toMs(barMeta.last)) }}</span>
          </div>
        </template>
      </Card>

      <Card data-slot="site-ranking">
        <div class="flex flex-wrap items-center gap-1">
          <h2 class="text-sm font-semibold">站点上传排行</h2>
          <Tip text="按各站点的累计上传量取前十。站点按任务的 tracker 域名归并，认不出域名的任务统一归到「其他」。鼠标悬停可看该站点的下载量与做种情况。" />
          <div class="flex-1" />
          <span class="mono text-xs text-muted-foreground">Top {{ topSites.length }}</span>
        </div>
        <div v-if="!topSites.length" class="py-16 text-center text-sm text-muted-foreground">
          暂无站点数据
        </div>
        <Chart v-else data-slot="site-bars" :option="topSitesOption" height="14rem" class="mt-2" />
      </Card>
    </div>

    <Card padded="false" class="overflow-hidden" data-slot="torrents">
      <div class="flex flex-wrap items-center gap-1 border-b px-4 py-3">
        <span class="text-sm font-semibold">种子明细</span>
        <Tip text="下载器里每个任务的流量与状态，按累计上传量排序。点任意一行看这个任务的完整数据。列表是快照，切页面或点右上角刷新会重新取。" />
        <span class="mono text-xs text-muted-foreground">
          {{ torrentRows.length }} 个任务
          <template v-if="torrentsAt"> · {{ datetime(toMs(torrentsAt)) }}</template>
        </span>
      </div>
      <div class="max-h-[26rem] overflow-auto">
        <table class="w-full text-sm">
          <thead class="sticky top-0 border-b bg-muted/80 text-xs text-muted-foreground backdrop-blur">
            <tr>
              <th class="px-4 py-2.5 text-left font-medium">任务</th>
              <th class="px-4 py-2.5 text-left font-medium">状态</th>
              <th class="px-4 py-2.5 text-right font-medium">大小</th>
              <th class="px-4 py-2.5 text-right font-medium">进度</th>
              <th class="px-4 py-2.5 text-right font-medium">上传</th>
              <th class="px-4 py-2.5 text-right font-medium">下载</th>
              <th class="px-4 py-2.5 text-right font-medium">分享率</th>
              <th class="px-4 py-2.5 text-right font-medium">速率</th>
            </tr>
          </thead>
          <tbody class="divide-y">
            <tr v-if="!torrentRows.length">
              <td colspan="8" class="px-4 py-10 text-center text-muted-foreground">
                暂无任务
              </td>
            </tr>
            <tr
              v-for="t in torrentRows"
              :key="t.hash"
              class="cursor-pointer hover:bg-muted/30"
              @click="detail = t"
            >
              <td class="max-w-0 px-4 py-2.5">
                <div class="truncate" :title="t.name">{{ t.name || t.hash }}</div>
                <div class="mono truncate text-xs text-muted-foreground">{{ t.site }}</div>
              </td>
              <td class="px-4 py-2.5">
                <Badge :variant="stateVariant(t.state)">{{ stateLabel(t.state) }}</Badge>
              </td>
              <td class="mono px-4 py-2.5 text-right whitespace-nowrap">{{ bytes(t.size) }}</td>
              <td class="mono px-4 py-2.5 text-right whitespace-nowrap">
                {{ (t.progress * 100).toFixed(1) }}%
              </td>
              <td class="mono px-4 py-2.5 text-right whitespace-nowrap">{{ bytes(t.uploaded) }}</td>
              <td class="mono px-4 py-2.5 text-right whitespace-nowrap">
                {{ bytes(t.downloaded) }}
              </td>
              <td class="mono px-4 py-2.5 text-right">{{ ratioOf(t) }}</td>
              <td class="mono px-4 py-2.5 text-right text-xs whitespace-nowrap">
                <span style="color: var(--chart-1)">↑{{ speed(t.upspeed) }}</span>
                <br />
                <span style="color: var(--chart-2)">↓{{ speed(t.dlspeed) }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Card>

    <Card padded="false" class="overflow-hidden">
      <div class="flex items-center gap-1 border-b px-4 py-3">
        <span class="text-sm font-semibold">站点流量</span>
        <Tip text="站点按任务的 tracker 域名归并，认不出域名的任务统一归到「其他」。上传/下载是各任务自身的累计值求和，所以任务被删除后这部分量不再出现在本表里，合计会小于上面的「累计上传」。" />
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b bg-muted/40 text-xs text-muted-foreground">
            <tr>
              <th class="px-4 py-2.5 text-left font-medium">站点</th>
              <th class="px-4 py-2.5 text-right font-medium">做种</th>
              <th class="px-4 py-2.5 text-right font-medium">体积</th>
              <th class="px-4 py-2.5 text-right font-medium">任务数</th>
              <th class="px-4 py-2.5 text-right font-medium">上传</th>
              <th class="px-4 py-2.5 text-right font-medium">下载</th>
              <th class="px-4 py-2.5 text-right font-medium">
                <span class="inline-flex items-center gap-1">
                  分享率
                  <Tip text="上传量 ÷ 下载量。下载量为 0 时显示 ∞。做种站一般要求分享率不低于 1。" />
                </span>
              </th>
            </tr>
          </thead>
          <tbody class="divide-y">
            <tr v-if="!sites.length">
              <td colspan="7" class="px-4 py-10 text-center text-muted-foreground">
                暂无站点数据
              </td>
            </tr>
            <tr v-for="s in sites" :key="s.site" class="hover:bg-muted/30">
              <td class="mono px-4 py-2.5">{{ s.site }}</td>
              <td class="mono px-4 py-2.5 text-right">{{ number(s.seeding) }}</td>
              <td class="mono px-4 py-2.5 text-right">{{ bytes(s.seeding_bytes) }}</td>
              <td class="mono px-4 py-2.5 text-right">{{ number(s.total) }}</td>
              <td class="mono px-4 py-2.5 text-right">{{ bytes(s.uploaded) }}</td>
              <td class="mono px-4 py-2.5 text-right">{{ bytes(s.downloaded) }}</td>
              <td class="mono px-4 py-2.5 text-right">
                {{ s.downloaded > 0 ? (s.uploaded / s.downloaded).toFixed(2) : '∞' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Card>

    <Dialog
      v-model:open="detailOpen"
      wide
      :title="detail?.name || detail?.hash || ''"
      :description="detail ? `${detail.site} · ${detail.hash}` : ''"
    >
      <div v-if="detail" data-slot="torrent-detail" class="grid gap-4">
        <div class="flex flex-wrap items-center gap-2 text-sm">
          <Badge :variant="stateVariant(detail.state)">{{ stateLabel(detail.state) }}</Badge>
          <span class="mono text-muted-foreground">
            {{ (detail.progress * 100).toFixed(1) }}% · {{ bytes(detail.completed) }} /
            {{ bytes(detail.size) }}
          </span>
        </div>

        <div>
          <h3 class="mb-2 text-xs font-medium text-muted-foreground">流量</h3>
          <div class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-3">
            <div>
              <div class="text-xs text-muted-foreground">累计上传</div>
              <div class="mono font-semibold">{{ bytes(detail.uploaded) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">累计下载</div>
              <div class="mono font-semibold">{{ bytes(detail.downloaded) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">分享率</div>
              <div class="mono font-semibold">{{ ratioOf(detail) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">本次会话上传</div>
              <div class="mono">{{ bytes(detail.uploaded_session) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">本次会话下载</div>
              <div class="mono">{{ bytes(detail.downloaded_session) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">当前速率</div>
              <div class="mono">
                <span style="color: var(--chart-1)">↑{{ speed(detail.upspeed) }}</span>
                <span class="mx-1 text-muted-foreground">/</span>
                <span style="color: var(--chart-2)">↓{{ speed(detail.dlspeed) }}</span>
              </div>
            </div>
          </div>
        </div>

        <div>
          <h3 class="mb-2 text-xs font-medium text-muted-foreground">连接</h3>
          <div class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
            <div>
              <div class="text-xs text-muted-foreground">已连接种子</div>
              <div class="mono">{{ number(detail.num_seeds) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">已连接下载者</div>
              <div class="mono">{{ number(detail.num_leechs) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">可用种子</div>
              <div class="mono">{{ number(detail.num_complete) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">可用下载者</div>
              <div class="mono">{{ number(detail.num_incomplete) }}</div>
            </div>
          </div>
        </div>

        <div>
          <h3 class="mb-2 text-xs font-medium text-muted-foreground">时间</h3>
          <div class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-3">
            <div>
              <div class="text-xs text-muted-foreground">添加于</div>
              <div class="mono">{{ datetime(toMs(detail.added_on)) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">完成于</div>
              <div class="mono">{{ detail.completed_on > 0 ? datetime(toMs(detail.completed_on)) : '—' }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">最后活动</div>
              <div class="mono">{{ datetime(toMs(detail.last_activity)) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">活动时长</div>
              <div class="mono">{{ spanLabel(detail.time_active) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">做种时长</div>
              <div class="mono">{{ spanLabel(detail.seeding_time) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">剩余 / 预计</div>
              <div class="mono">
                {{ detail.amount_left > 0 ? bytes(detail.amount_left) : '—' }}
                · {{ etaLabel(detail.eta) }}
              </div>
            </div>
          </div>
        </div>

        <div>
          <h3 class="mb-2 text-xs font-medium text-muted-foreground">归属</h3>
          <div class="grid gap-y-2 text-sm">
            <div class="flex gap-3">
              <span class="w-20 shrink-0 text-xs text-muted-foreground">站点</span>
              <span class="mono">{{ detail.site }}</span>
            </div>
            <div class="flex gap-3">
              <span class="w-20 shrink-0 text-xs text-muted-foreground">Tracker</span>
              <span class="mono break-all">{{ detail.tracker || '—' }}</span>
            </div>
            <div class="flex gap-3">
              <span class="w-20 shrink-0 text-xs text-muted-foreground">保存路径</span>
              <span class="mono break-all">{{ detail.save_path || '—' }}</span>
            </div>
            <div class="flex gap-3">
              <span class="w-20 shrink-0 text-xs text-muted-foreground">分类 / 标签</span>
              <span class="mono">
                {{ detail.category || '—' }}
                <template v-if="detail.tags">· {{ detail.tags }}</template>
              </span>
            </div>
            <div class="flex gap-3">
              <span class="w-20 shrink-0 text-xs text-muted-foreground">私有</span>
              <span class="mono">{{ detail.private ? '是（PT 站）' : '否' }}</span>
            </div>
          </div>
        </div>
      </div>
    </Dialog>
  </div>
</template>
